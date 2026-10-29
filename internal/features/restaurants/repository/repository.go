package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/domain"
)

type RestaurantRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Restaurant, error)
	List(ctx context.Context) ([]domain.RestaurantListItem, error)
	ListOpen(ctx context.Context, onlyOpen bool) ([]domain.RestaurantListItem, error)
	ListPaginated(ctx context.Context, onlyOpen bool, limit, offset int) ([]domain.RestaurantListItem, error)
	Create(ctx context.Context, r *domain.Restaurant) error
	Update(ctx context.Context, r *domain.Restaurant) error
}
