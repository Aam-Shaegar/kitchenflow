package repository

import (
	"context"

	"github.com/Aam-Shaegar/kitchenflow/internal/features/menu/domain"
	"github.com/google/uuid"
)

type MenuRepository interface {
	GetCategoriesByRestaurantID(ctx context.Context, restaurantID uuid.UUID) ([]domain.MenuCategory, error)
	GetItemByID(ctx context.Context, id uuid.UUID) (*domain.MenuItem, error)
	GetItemsByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.MenuItem, error)
	GetCategoryByID(ctx context.Context, id uuid.UUID) (*domain.MenuCategory, error)
	CreateCategory(ctx context.Context, cat *domain.MenuCategory) error
	CreateItem(ctx context.Context, item *domain.MenuItem) error
	UpdateItem(ctx context.Context, item *domain.MenuItem) error
	DeleteItem(ctx context.Context, id uuid.UUID) error
}
