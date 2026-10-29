package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	core_database "github.com/Aam-Shaegar/kitchenflow/internal/core/database"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/restaurants/domain"
)

type PostgresRestaurantRepository struct {
	pool core_database.Pool
}

func NewRestaurantRepository(pool core_database.Pool) *PostgresRestaurantRepository {
	return &PostgresRestaurantRepository{pool: pool}
}

func (r *PostgresRestaurantRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Restaurant, error) {
	query := `
		SELECT id, external_id, name, description, is_open, integration_base_url, created_at, updated_at
		FROM restaurants
		WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, query, id)
	return r.scanRestaurant(row)
}

func (r *PostgresRestaurantRepository) List(ctx context.Context) ([]domain.RestaurantListItem, error) {
	return r.ListOpen(ctx, false)
}

func (r *PostgresRestaurantRepository) ListOpen(ctx context.Context, onlyOpen bool) ([]domain.RestaurantListItem, error) {
	return r.ListPaginated(ctx, onlyOpen, 100, 0)
}

func (r *PostgresRestaurantRepository) ListPaginated(ctx context.Context, onlyOpen bool, limit, offset int) ([]domain.RestaurantListItem, error) {
	query := `
		SELECT id, name, description, is_open
		FROM restaurants
	`
	if onlyOpen {
		query += ` WHERE is_open = true`
	}
	query += ` ORDER BY created_at LIMIT $1 OFFSET $2`
	rows, err := r.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	restaurants := []domain.RestaurantListItem{}
	for rows.Next() {
		var rest domain.RestaurantListItem
		if err := rows.Scan(&rest.ID, &rest.Name, &rest.Description, &rest.IsOpen); err != nil {
			return nil, err
		}
		restaurants = append(restaurants, rest)
	}
	return restaurants, rows.Err()
}

func (r *PostgresRestaurantRepository) Create(ctx context.Context, rest *domain.Restaurant) error {
	query := `
		INSERT INTO restaurants (id, external_id, name, description, is_open, integration_base_url, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := r.pool.Exec(ctx, query,
		rest.ID, rest.ExternalID, rest.Name, rest.Description,
		rest.IsOpen, rest.IntegrationBaseURL, rest.CreatedAt, rest.UpdatedAt,
	)
	return err
}

func (r *PostgresRestaurantRepository) Update(ctx context.Context, rest *domain.Restaurant) error {
	query := `
		UPDATE restaurants
		SET name = $2, description = $3, is_open = $4, integration_base_url = $5, updated_at = $6
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query,
		rest.ID, rest.Name, rest.Description,
		rest.IsOpen, rest.IntegrationBaseURL, rest.UpdatedAt,
	)
	return err
}

func (r *PostgresRestaurantRepository) scanRestaurant(row core_database.Row) (*domain.Restaurant, error) {
	var rest domain.Restaurant
	err := row.Scan(
		&rest.ID,
		&rest.ExternalID,
		&rest.Name,
		&rest.Description,
		&rest.IsOpen,
		&rest.IntegrationBaseURL,
		&rest.CreatedAt,
		&rest.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, core_database.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &rest, nil
}

var _ RestaurantRepository = (*PostgresRestaurantRepository)(nil)
