package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/menu/domain"
)

type MenuRepository interface {
	GetCategoriesByRestaurantID(ctx context.Context, restaurantID uuid.UUID) ([]domain.MenuCategory, error)
	GetItemsByCategoryIDs(ctx context.Context, categoryIDs []uuid.UUID) ([]domain.MenuItem, error)
	GetItemByID(ctx context.Context, id uuid.UUID) (*domain.MenuItem, error)
	GetItemsByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.MenuItem, error)
	GetCategoryByID(ctx context.Context, id uuid.UUID) (*domain.MenuCategory, error)
	CreateCategory(ctx context.Context, cat *domain.MenuCategory) error
	UpdateCategory(ctx context.Context, cat *domain.MenuCategory) error
	DeleteCategory(ctx context.Context, id uuid.UUID) error
	CreateItem(ctx context.Context, item *domain.MenuItem) error
	UpdateItem(ctx context.Context, item *domain.MenuItem) error
	DeleteItem(ctx context.Context, id uuid.UUID) error
}
