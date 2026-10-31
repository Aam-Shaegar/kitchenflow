package repository

import (
	"context"

	"github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
	"github.com/google/uuid"
)

type OrderRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Order, error)
	GetByIDAndUserID(ctx context.Context, id uuid.UUID, userID string) (*domain.Order, error)
	GetByUserIDAndIdempotencyKey(ctx context.Context, userID, idempotencyKey string) (*domain.Order, error)
	GetByExternalOrderIDAndRestaurantID(ctx context.Context, externalOrderID string, restaurantID uuid.UUID) (*domain.Order, error)
	ListByUserIDPaginated(ctx context.Context, userID string, limit, offset int) ([]domain.Order, error)
	ListByRestaurantID(ctx context.Context, restaurantID uuid.UUID, limit, offset int) ([]domain.Order, error)
	CreateWithIdempotencyKey(ctx context.Context, order *domain.Order) (*domain.Order, error)
	UpdateStatusWithHistory(ctx context.Context, orderID uuid.UUID, status domain.OrderStatus, history *domain.OrderStatusHistory) error
	GetStatusHistory(ctx context.Context, orderID uuid.UUID) ([]domain.OrderStatusHistory, error)
	PrepareIntegrationRetry(ctx context.Context, orderID uuid.UUID, externalID string) error
	SaveIntegrationResult(ctx context.Context, orderID uuid.UUID, externalID string, status domain.OrderStatus) error
}
