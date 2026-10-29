package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	core_database "github.com/Aam-Shaegar/kitchenflow/internal/core/database"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/menu/domain"
)

type PostgresMenuRepository struct {
	pool core_database.Pool
}

func NewMenuRepository(pool core_database.Pool) *PostgresMenuRepository {
	return &PostgresMenuRepository{pool: pool}
}

func (r *PostgresMenuRepository) GetCategoriesByRestaurantID(ctx context.Context, restaurantID uuid.UUID) ([]domain.MenuCategory, error) {
	query := `
		SELECT id, restaurant_id, name, position, created_at, updated_at
		FROM menu_categories
		WHERE restaurant_id = $1
		ORDER BY position
	`
	rows, err := r.pool.Query(ctx, query, restaurantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []domain.MenuCategory
	for rows.Next() {
		var cat domain.MenuCategory
		if err := rows.Scan(&cat.ID, &cat.RestaurantID, &cat.Name, &cat.Position, &cat.CreatedAt, &cat.UpdatedAt); err != nil {
			return nil, err
		}
		categories = append(categories, cat)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(categories) > 0 {
		categoryIDs := make([]uuid.UUID, len(categories))
		for i, c := range categories {
			categoryIDs[i] = c.ID
		}
		items, err := r.GetItemsByCategoryIDs(ctx, categoryIDs)
		if err != nil {
			return nil, err
		}
		itemMap := make(map[uuid.UUID][]domain.MenuItem)
		for _, item := range items {
			itemMap[item.CategoryID] = append(itemMap[item.CategoryID], item)
		}
		for i := range categories {
			categories[i].Items = itemMap[categories[i].ID]
		}
	}

	return categories, nil
}

func (r *PostgresMenuRepository) GetItemsByCategoryIDs(ctx context.Context, categoryIDs []uuid.UUID) ([]domain.MenuItem, error) {
	if len(categoryIDs) == 0 {
		return []domain.MenuItem{}, nil
	}

	query := `
		SELECT id, restaurant_id, category_id, external_id, name, description, price, is_available, created_at, updated_at
		FROM menu_items
		WHERE category_id = ANY($1)
	`
	rows, err := r.pool.Query(ctx, query, categoryIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanMenuItems(rows)
}

func (r *PostgresMenuRepository) GetItemByID(ctx context.Context, id uuid.UUID) (*domain.MenuItem, error) {
	query := `
		SELECT id, restaurant_id, category_id, external_id, name, description, price, is_available, created_at, updated_at
		FROM menu_items
		WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, query, id)
	return r.scanMenuItem(row)
}

func (r *PostgresMenuRepository) GetItemsByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.MenuItem, error) {
	if len(ids) == 0 {
		return []domain.MenuItem{}, nil
	}

	query := `
		SELECT id, restaurant_id, category_id, external_id, name, description, price, is_available, created_at, updated_at
		FROM menu_items
		WHERE id = ANY($1)
	`
	rows, err := r.pool.Query(ctx, query, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanMenuItems(rows)
}

func (r *PostgresMenuRepository) scanMenuItems(rows core_database.Rows) ([]domain.MenuItem, error) {
	var items []domain.MenuItem
	for rows.Next() {
		var item domain.MenuItem
		if err := rows.Scan(
			&item.ID,
			&item.RestaurantID,
			&item.CategoryID,
			&item.ExternalID,
			&item.Name,
			&item.Description,
			&item.Price,
			&item.IsAvailable,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *PostgresMenuRepository) scanMenuItem(row core_database.Row) (*domain.MenuItem, error) {
	var item domain.MenuItem
	err := row.Scan(
		&item.ID,
		&item.RestaurantID,
		&item.CategoryID,
		&item.ExternalID,
		&item.Name,
		&item.Description,
		&item.Price,
		&item.IsAvailable,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, core_database.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (r *PostgresMenuRepository) GetCategoryByID(ctx context.Context, id uuid.UUID) (*domain.MenuCategory, error) {
	query := `
		SELECT id, restaurant_id, name, position, created_at, updated_at
		FROM menu_categories
		WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, query, id)
	var cat domain.MenuCategory
	if err := row.Scan(&cat.ID, &cat.RestaurantID, &cat.Name, &cat.Position, &cat.CreatedAt, &cat.UpdatedAt); err != nil {
		if errors.Is(err, core_database.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &cat, nil
}

func (r *PostgresMenuRepository) CreateCategory(ctx context.Context, cat *domain.MenuCategory) error {
	query := `
		INSERT INTO menu_categories (id, restaurant_id, name, position, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := r.pool.Exec(ctx, query, cat.ID, cat.RestaurantID, cat.Name, cat.Position, cat.CreatedAt, cat.UpdatedAt)
	return err
}

func (r *PostgresMenuRepository) UpdateCategory(ctx context.Context, cat *domain.MenuCategory) error {
	query := `
		UPDATE menu_categories SET name = $2, position = $3, updated_at = $4 WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query, cat.ID, cat.Name, cat.Position, cat.UpdatedAt)
	return err
}

func (r *PostgresMenuRepository) DeleteCategory(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM menu_categories WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

func (r *PostgresMenuRepository) CreateItem(ctx context.Context, item *domain.MenuItem) error {
	query := `
		INSERT INTO menu_items (id, restaurant_id, category_id, external_id, name, description, price, is_available, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := r.pool.Exec(ctx, query,
		item.ID, item.RestaurantID, item.CategoryID, item.ExternalID,
		item.Name, item.Description, item.Price, item.IsAvailable,
		item.CreatedAt, item.UpdatedAt,
	)
	return err
}

func (r *PostgresMenuRepository) UpdateItem(ctx context.Context, item *domain.MenuItem) error {
	query := `
		UPDATE menu_items
		SET category_id = $2, name = $3, description = $4, price = $5, is_available = $6, updated_at = $7
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query,
		item.ID, item.CategoryID, item.Name, item.Description,
		item.Price, item.IsAvailable, item.UpdatedAt,
	)
	return err
}

func (r *PostgresMenuRepository) DeleteItem(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM menu_items WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

var _ MenuRepository = (*PostgresMenuRepository)(nil)
