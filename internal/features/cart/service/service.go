package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	cart_domain "github.com/Aam-Shaegar/kitchenflow/internal/features/cart/domain"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/cart/repository"
	menu_domain "github.com/Aam-Shaegar/kitchenflow/internal/features/menu/domain"
)

type MenuProvider interface {
	GetItemByID(ctx context.Context, id uuid.UUID) (*menu_domain.MenuItem, error)
}

type CartService struct {
	repo         repository.CartRepository
	menuProvider MenuProvider
}

func NewCartService(repo repository.CartRepository, menuProvider MenuProvider) *CartService {
	return &CartService{repo: repo, menuProvider: menuProvider}
}

func (s *CartService) GetCart(ctx context.Context, userID string) (*cart_domain.Cart, error) {
	cart, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if cart == nil {
		return nil, errors.ErrNotFound
	}
	return cart, nil
}

func (s *CartService) AddItem(ctx context.Context, userID string, restaurantID, menuItemID uuid.UUID, quantity int) (*cart_domain.Cart, error) {
	if quantity <= 0 || quantity > 50 {
		return nil, errors.ErrInvalidArgument
	}

	menuItem, err := s.menuProvider.GetItemByID(ctx, menuItemID)
	if err != nil {
		return nil, err
	}
	if menuItem == nil {
		return nil, errors.ErrNotFound
	}

	if !menuItem.IsAvailable {
		return nil, errors.ErrConflict
	}

	if menuItem.RestaurantID != restaurantID {
		return nil, errors.ErrConflict
	}

	// One user means one active cart. Mixing restaurants in a single checkout
	// makes no sense, so we ask the client to clear the cart explicitly.
	currentCart, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if currentCart != nil && len(currentCart.Items) > 0 && currentCart.RestaurantID != restaurantID {
		return nil, errors.ErrConflict
	}

	var cart *cart_domain.Cart
	existingCart, err := s.repo.GetByUserIDAndRestaurantID(ctx, userID, restaurantID)
	if err != nil {
		return nil, err
	}

	if existingCart == nil {
		// A leftover empty cart from another restaurant would violate UNIQUE(user_id),
		// so drop it before creating the fresh one.
		if currentCart != nil && currentCart.RestaurantID != restaurantID {
			_ = s.repo.Delete(ctx, currentCart.ID)
		}
		cart = &cart_domain.Cart{
			ID:           uuid.New(),
			UserID:       userID,
			RestaurantID: restaurantID,
			Items:        []cart_domain.CartItem{},
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		if err := s.repo.Create(ctx, cart); err != nil {
			return nil, err
		}
	} else {
		cart = existingCart
	}

	idx := cart.FindItemIndex(menuItemID)
	if idx != -1 {
		existingItem := &cart.Items[idx]
		newQty := existingItem.Quantity + quantity
		if newQty > 50 {
			return nil, errors.ErrInvalidArgument
		}
		existingItem.Quantity = newQty
		existingItem.TotalPrice = existingItem.UnitPrice * int64(existingItem.Quantity)
		existingItem.UpdatedAt = time.Now()
		if err := s.repo.UpdateItem(ctx, existingItem); err != nil {
			return nil, err
		}
	} else {
		item := &cart_domain.CartItem{
			ID:         uuid.New(),
			CartID:     cart.ID,
			MenuItemID: menuItemID,
			Name:       menuItem.Name,
			UnitPrice:  menuItem.Price,
			Quantity:   quantity,
			TotalPrice: menuItem.Price * int64(quantity),
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		if err := s.repo.AddItem(ctx, item); err != nil {
			return nil, err
		}
		cart.Items = append(cart.Items, *item)
	}

	cart.TotalAmount = cart.CalculateTotal()
	cart.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, cart); err != nil {
		return nil, err
	}

	return s.repo.GetByUserID(ctx, userID)
}

func (s *CartService) UpdateItemQuantity(ctx context.Context, userID string, cartItemID uuid.UUID, quantity int) (*cart_domain.Cart, error) {
	if quantity <= 0 || quantity > 50 {
		return nil, errors.ErrInvalidArgument
	}

	cart, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if cart == nil {
		return nil, errors.ErrNotFound
	}

	found := false
	for i := range cart.Items {
		if cart.Items[i].ID == cartItemID {
			cart.Items[i].Quantity = quantity
			cart.Items[i].TotalPrice = cart.Items[i].UnitPrice * int64(quantity)
			cart.Items[i].UpdatedAt = time.Now()
			found = true
			if err := s.repo.UpdateItem(ctx, &cart.Items[i]); err != nil {
				return nil, err
			}
			break
		}
	}

	if !found {
		return nil, errors.ErrNotFound
	}

	cart.TotalAmount = cart.CalculateTotal()
	cart.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, cart); err != nil {
		return nil, err
	}

	return s.repo.GetByUserID(ctx, userID)
}

func (s *CartService) RemoveItem(ctx context.Context, userID string, cartItemID uuid.UUID) (*cart_domain.Cart, error) {
	cart, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if cart == nil {
		return nil, errors.ErrNotFound
	}

	found := false
	for i := range cart.Items {
		if cart.Items[i].ID == cartItemID {
			found = true
			break
		}
	}

	if !found {
		return nil, errors.ErrNotFound
	}

	if err := s.repo.RemoveItem(ctx, cart.ID, cartItemID); err != nil {
		return nil, err
	}

	return s.repo.GetByUserID(ctx, userID)
}

func (s *CartService) ClearCart(ctx context.Context, userID string) error {
	cart, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return err
	}
	if cart == nil {
		return nil
	}
	return s.repo.Delete(ctx, cart.ID)
}
