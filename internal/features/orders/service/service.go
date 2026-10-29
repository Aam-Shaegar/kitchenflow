package service

import (
	"context"
	"strings"
	"time"

	integrations_domain "github.com/Aam-Shaegar/kitchenflow/internal/features/integrations/restaurant/domain"
	menu_domain "github.com/Aam-Shaegar/kitchenflow/internal/features/menu/domain"
	orders_domain "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
	restaurants_domain "github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/domain"

	cart_repository "github.com/Aam-Shaegar/kitchenflow/internal/features/cart/repository"
	orders_repository "github.com/Aam-Shaegar/kitchenflow/internal/features/orders/repository"

	"github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	"github.com/google/uuid"
)

type MenuProvider interface {
	GetItemsByIDs(ctx context.Context, ids []uuid.UUID) ([]menu_domain.MenuItem, error)
}

type RestaurantProvider interface {
	GetByIDForOrder(ctx context.Context, id uuid.UUID) (*restaurants_domain.RestaurantInfo, error)
}

type OrderStatusCallback struct {
	ExternalOrderID string
	Status          string
	EventID         string
}

type OrderService struct {
	repo               orders_repository.OrderRepository
	cartRepo           cart_repository.CartRepository
	menuProvider       MenuProvider
	restaurantProvider RestaurantProvider
	restaurantClient   integrations_domain.RestaurantClient
}

func NewOrderService(
	repo orders_repository.OrderRepository,
	cartRepo cart_repository.CartRepository,
	menuProvider MenuProvider,
	restaurantProvider RestaurantProvider,
	restaurantClient integrations_domain.RestaurantClient,
) *OrderService {
	return &OrderService{
		repo:               repo,
		cartRepo:           cartRepo,
		menuProvider:       menuProvider,
		restaurantProvider: restaurantProvider,
		restaurantClient:   restaurantClient,
	}
}

func (s *OrderService) CreateOrder(ctx context.Context, userID, deliveryAddress, idempotencyKey string) (*orders_domain.Order, error) {
	if idempotencyKey == "" || len(idempotencyKey) > 128 {
		return nil, errors.ErrInvalidArgument
	}
	if len(strings.TrimSpace(deliveryAddress)) < 5 || len(deliveryAddress) > 500 {
		return nil, errors.ErrInvalidArgument
	}
	if strings.TrimSpace(userID) == "" || len(userID) > 128 {
		return nil, errors.ErrInvalidArgument
	}

	existing, err := s.repo.GetByUserIDAndIdempotencyKey(ctx, userID, idempotencyKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	cart, err := s.cartRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if cart == nil || len(cart.Items) == 0 {
		return nil, errors.ErrConflict
	}

	menuItemIDs := make([]uuid.UUID, len(cart.Items))
	for i, item := range cart.Items {
		menuItemIDs[i] = item.MenuItemID
	}

	menuItems, err := s.menuProvider.GetItemsByIDs(ctx, menuItemIDs)
	if err != nil {
		return nil, err
	}

	menuItemMap := make(map[uuid.UUID]menu_domain.MenuItem)
	for _, item := range menuItems {
		menuItemMap[item.ID] = item
	}

	for _, cartItem := range cart.Items {
		menuItem, ok := menuItemMap[cartItem.MenuItemID]
		if !ok {
			return nil, errors.ErrNotFound
		}
		if !menuItem.IsAvailable {
			return nil, errors.ErrConflict
		}
		if menuItem.RestaurantID != cart.RestaurantID {
			return nil, errors.ErrConflict
		}
		if cartItem.Quantity <= 0 || cartItem.Quantity > 50 {
			return nil, errors.ErrInvalidArgument
		}
	}

	restaurant, err := s.restaurantProvider.GetByIDForOrder(ctx, cart.RestaurantID)
	if err != nil {
		return nil, err
	}
	if restaurant == nil {
		return nil, errors.ErrNotFound
	}
	if !restaurant.IsOpen {
		return nil, errors.ErrConflict
	}

	// One timestamp for the whole order keeps created_at/updated_at/items/history
	// consistent instead of drifting by milliseconds between time.Now() calls.
	now := time.Now()
	var totalAmount int64
	orderID := uuid.New()
	externalID := orderID.String()
	orderItems := make([]orders_domain.OrderItem, len(cart.Items))
	integrationItems := make([]integrations_domain.CreateOrderItem, len(cart.Items))

	for i, cartItem := range cart.Items {
		menuItem := menuItemMap[cartItem.MenuItemID]
		lineTotal := menuItem.Price * int64(cartItem.Quantity)
		totalAmount += lineTotal

		orderItems[i] = orders_domain.OrderItem{
			ID:         uuid.New(),
			OrderID:    orderID,
			MenuItemID: cartItem.MenuItemID,
			Name:       menuItem.Name,
			UnitPrice:  menuItem.Price,
			Quantity:   cartItem.Quantity,
			TotalPrice: lineTotal,
			CreatedAt:  now,
		}

		integrationItems[i] = integrations_domain.CreateOrderItem{
			ExternalMenuItemID: menuItem.ExternalID,
			Name:               menuItem.Name,
			UnitPrice:          menuItem.Price,
			Quantity:           cartItem.Quantity,
		}
	}

	order := &orders_domain.Order{
		ID:              orderID,
		ExternalOrderID: &externalID,
		UserID:          userID,
		RestaurantID:    cart.RestaurantID,
		Status:          orders_domain.OrderStatusPending,
		TotalAmount:     totalAmount,
		DeliveryAddress: deliveryAddress,
		IdempotencyKey:  idempotencyKey,
		Items:           orderItems,
		StatusHistory:   []orders_domain.OrderStatusHistory{},
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	history := orders_domain.OrderStatusHistory{
		OrderID:   orderID,
		Status:    orders_domain.OrderStatusPending,
		Source:    "kitchen",
		EventID:   nil,
		CreatedAt: now,
	}
	order.StatusHistory = append(order.StatusHistory, history)

	// The database is the referee here: UNIQUE(user_id, idempotency_key) turns
	// a double-click or a retried request into a single order.
	createdOrder, err := s.repo.CreateWithIdempotencyKey(ctx, order)
	if err != nil {
		return nil, err
	}

	// We lost the race to a concurrent duplicate, so return the winner untouched.
	if createdOrder.ID != order.ID {
		return createdOrder, nil
	}

	integrationReq := integrations_domain.CreateOrderRequest{
		ExternalOrderID: order.ID.String(),
		Items:           integrationItems,
		DeliveryAddress: deliveryAddress,
	}

	if err := s.sendOrder(ctx, order.ID, integrationReq, &cart.ID); err != nil {
		return nil, err
	}

	// Same re-read here: history rows get their BIGSERIAL ids only in Postgres.
	fresh, err := s.repo.GetByIDAndUserID(ctx, order.ID, userID)
	if err != nil {
		return nil, err
	}
	if fresh != nil {
		return fresh, nil
	}

	return order, nil
}

func (s *OrderService) GetOrder(ctx context.Context, id uuid.UUID, userID string) (*orders_domain.Order, error) {
	order, err := s.repo.GetByIDAndUserID(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, errors.ErrNotFound
	}
	return order, nil
}

func (s *OrderService) GetOrdersByUserIDPaginated(ctx context.Context, userID string, limit, offset int) ([]orders_domain.Order, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.ListByUserIDPaginated(ctx, userID, limit, offset)
}

func (s *OrderService) GetOrdersByRestaurantID(ctx context.Context, restaurantID uuid.UUID, limit, offset int) ([]orders_domain.Order, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.ListByRestaurantID(ctx, restaurantID, limit, offset)
}

func (s *OrderService) GetOrderStatus(ctx context.Context, id uuid.UUID, userID string) (orders_domain.OrderStatus, error) {
	order, err := s.repo.GetByIDAndUserID(ctx, id, userID)
	if err != nil {
		return "", err
	}
	if order == nil {
		return "", errors.ErrNotFound
	}
	return order.Status, nil
}

func (s *OrderService) GetOrderHistory(ctx context.Context, id uuid.UUID, userID string) ([]orders_domain.OrderStatusHistory, error) {
	order, err := s.repo.GetByIDAndUserID(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, errors.ErrNotFound
	}
	return s.repo.GetStatusHistory(ctx, id)
}

// CancelOrder stops an order that the kitchen has not started cooking yet.
// Anything past confirmed already costs the restaurant money, so we refuse it.
func (s *OrderService) CancelOrder(ctx context.Context, id uuid.UUID, userID string) (*orders_domain.Order, error) {
	order, err := s.repo.GetByIDAndUserID(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, errors.ErrNotFound
	}
	if !orders_domain.CanTransition(order.Status, orders_domain.OrderStatusCancelled) {
		return nil, errors.ErrConflict
	}
	history := &orders_domain.OrderStatusHistory{
		OrderID:   order.ID,
		Status:    orders_domain.OrderStatusCancelled,
		Source:    "kitchen",
		EventID:   nil,
		CreatedAt: time.Now(),
	}
	if err := s.repo.UpdateStatusWithHistory(ctx, order.ID, orders_domain.OrderStatusCancelled, history); err != nil {
		return nil, err
	}
	return s.repo.GetByIDAndUserID(ctx, id, userID)
}

// RetryIntegration gives an integration_failed order a second chance against the restaurant.
func (s *OrderService) RetryIntegration(ctx context.Context, id uuid.UUID, userID string) (*orders_domain.Order, error) {
	order, err := s.repo.GetByIDAndUserID(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, errors.ErrNotFound
	}
	if order.Status != orders_domain.OrderStatusIntegrationFailed {
		return nil, errors.ErrConflict
	}

	integrationItems := make([]integrations_domain.CreateOrderItem, len(order.Items))
	for i, item := range order.Items {
		integrationItems[i] = integrations_domain.CreateOrderItem{
			ExternalMenuItemID: item.MenuItemID.String(),
			Name:               item.Name,
			UnitPrice:          item.UnitPrice,
			Quantity:           item.Quantity,
		}
	}
	externalID := order.ID.String()
	if order.ExternalOrderID != nil {
		externalID = *order.ExternalOrderID
	}
	if err := s.repo.PrepareIntegrationRetry(ctx, order.ID, externalID); err != nil {
		return nil, err
	}
	if err := s.sendOrder(ctx, order.ID, integrations_domain.CreateOrderRequest{
		ExternalOrderID: externalID, Items: integrationItems, DeliveryAddress: order.DeliveryAddress,
	}, nil); err != nil {
		return nil, err
	}

	return s.repo.GetByIDAndUserID(ctx, id, userID)
}

// The restaurant must echo our correlation ID, including in early callbacks.
// Malformed responses are treated as failed attempts rather than accepted orders.
func (s *OrderService) sendOrder(ctx context.Context, orderID uuid.UUID, req integrations_domain.CreateOrderRequest, cartID *uuid.UUID) error {
	resp, err := s.restaurantClient.CreateOrder(ctx, req)
	status := orders_domain.OrderStatusIntegrationFailed
	if err == nil && resp != nil && resp.ExternalOrderID == req.ExternalOrderID {
		switch orders_domain.OrderStatus(resp.Status) {
		case orders_domain.OrderStatusPending, orders_domain.OrderStatusConfirmed, orders_domain.OrderStatusCancelled:
			status = orders_domain.OrderStatus(resp.Status)
		}
	}
	return s.repo.SaveIntegrationResult(ctx, orderID, req.ExternalOrderID, status, cartID)
}

func (s *OrderService) HandleRestaurantCallback(ctx context.Context, restaurantID uuid.UUID, callback OrderStatusCallback) error {
	if strings.TrimSpace(callback.ExternalOrderID) == "" || strings.TrimSpace(callback.EventID) == "" {
		return errors.ErrInvalidArgument
	}
	newStatus := orders_domain.OrderStatus(callback.Status)
	if !isKnownStatus(newStatus) {
		return errors.ErrInvalidArgument
	}
	order, err := s.repo.GetByExternalOrderIDAndRestaurantID(ctx, callback.ExternalOrderID, restaurantID)
	if err != nil {
		return err
	}
	if order == nil {
		return errors.ErrNotFound
	}

	// Deduplication and transition validation must share the repository's row
	// lock. A service-level status check would reject replays of older events
	// and race with callbacks advancing the same order.

	eventID := callback.EventID
	history := orders_domain.OrderStatusHistory{
		OrderID:   order.ID,
		Status:    newStatus,
		Source:    "restaurant",
		EventID:   &eventID,
		CreatedAt: time.Now(),
	}

	return s.repo.UpdateStatusWithHistory(ctx, order.ID, newStatus, &history)
}

func isKnownStatus(s orders_domain.OrderStatus) bool {
	switch s {
	case orders_domain.OrderStatusPending,
		orders_domain.OrderStatusConfirmed,
		orders_domain.OrderStatusPreparing,
		orders_domain.OrderStatusReady,
		orders_domain.OrderStatusDelivering,
		orders_domain.OrderStatusDelivered,
		orders_domain.OrderStatusCancelled,
		orders_domain.OrderStatusIntegrationFailed:
		return true
	default:
		return false
	}
}
