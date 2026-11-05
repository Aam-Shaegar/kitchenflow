package repository

import (
	"context"

	"github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/domain"
	"github.com/google/uuid"
)

type RestaurantRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Restaurant, error)
	ListPaginated(ctx context.Context, onlyOpen bool, limit, offset int) ([]domain.RestaurantListItem, error)
	Create(ctx context.Context, r *domain.Restaurant) error
	Update(ctx context.Context, r *domain.Restaurant) error
}
