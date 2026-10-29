package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/cart/domain"
)

type CartRepository interface {
	GetByUserID(ctx context.Context, userID string) (*domain.Cart, error)
	GetByUserIDAndRestaurantID(ctx context.Context, userID string, restaurantID uuid.UUID) (*domain.Cart, error)
	Create(ctx context.Context, cart *domain.Cart) error
	Update(ctx context.Context, cart *domain.Cart) error
	Delete(ctx context.Context, id uuid.UUID) error

	AddItem(ctx context.Context, item *domain.CartItem) error
	UpdateItem(ctx context.Context, item *domain.CartItem) error
	RemoveItem(ctx context.Context, cartID, itemID uuid.UUID) error
	GetItems(ctx context.Context, cartID uuid.UUID) ([]domain.CartItem, error)
}
