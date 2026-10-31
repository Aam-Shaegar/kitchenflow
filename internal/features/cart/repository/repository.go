package repository

import (
	"context"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/cart/domain"
	"github.com/google/uuid"
)

type CartRepository interface {
	GetByUserID(context.Context, string) (*domain.Cart, error)
	AddToCart(context.Context, string, uuid.UUID, domain.CartItem) (*domain.Cart, error)
	SetQuantity(context.Context, string, uuid.UUID, int) (*domain.Cart, error)
	RemoveCartItem(context.Context, string, uuid.UUID) (*domain.Cart, error)
	ClearByUser(context.Context, string) error
}
