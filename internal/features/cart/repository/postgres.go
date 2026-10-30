package repository

import (
	"context"
	"errors"

	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	"github.com/google/uuid"

	core_database "github.com/Aam-Shaegar/kitchenflow/internal/core/database"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/cart/domain"
)

type PostgresCartRepository struct {
	pool core_database.Pool
}

func NewCartRepository(pool core_database.Pool) *PostgresCartRepository {
	return &PostgresCartRepository{pool: pool}
}

func (r *PostgresCartRepository) GetByUserID(ctx context.Context, userID string) (*domain.Cart, error) {
	query := `
		SELECT id, user_id, restaurant_id, created_at, updated_at, revision
		FROM carts
		WHERE user_id = $1
		ORDER BY updated_at DESC
		LIMIT 1
	`
	row := r.pool.QueryRow(ctx, query, userID)
	cart, err := r.scanCart(row)
	if err != nil {
		if errors.Is(err, core_database.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r.loadCartItems(ctx, cart)
}

func (r *PostgresCartRepository) GetByUserIDAndRestaurantID(ctx context.Context, userID string, restaurantID uuid.UUID) (*domain.Cart, error) {
	query := `
		SELECT id, user_id, restaurant_id, created_at, updated_at, revision
		FROM carts
		WHERE user_id = $1 AND restaurant_id = $2
	`
	row := r.pool.QueryRow(ctx, query, userID, restaurantID)
	cart, err := r.scanCart(row)
	if err != nil {
		if errors.Is(err, core_database.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r.loadCartItems(ctx, cart)
}

func (r *PostgresCartRepository) Create(ctx context.Context, cart *domain.Cart) error {
	query := `
		INSERT INTO carts (id, user_id, restaurant_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := r.pool.Exec(ctx, query, cart.ID, cart.UserID, cart.RestaurantID, cart.CreatedAt, cart.UpdatedAt)
	return err
}

func (r *PostgresCartRepository) Update(ctx context.Context, cart *domain.Cart) error {
	query := `
		UPDATE carts
		SET restaurant_id = $2, updated_at = $3
		WHERE id = $1
	`
	tag, err := r.pool.Exec(ctx, query, cart.ID, cart.RestaurantID, cart.UpdatedAt)
	if err == nil && tag.RowsAffected() == 0 {
		return coreerrors.ErrConflict
	}
	return err
}

func (r *PostgresCartRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM carts WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

func (r *PostgresCartRepository) AddItem(ctx context.Context, item *domain.CartItem) error {
	query := `
		INSERT INTO cart_items (id, cart_id, menu_item_id, name, unit_price, quantity, total_price, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := r.pool.Exec(ctx, query, item.ID, item.CartID, item.MenuItemID, item.Name, item.UnitPrice, item.Quantity, item.TotalPrice, item.CreatedAt, item.UpdatedAt)
	return err
}

func (r *PostgresCartRepository) UpdateItem(ctx context.Context, item *domain.CartItem) error {
	query := `
		UPDATE cart_items
		SET quantity = $2, total_price = $3, updated_at = $4
		WHERE id = $1
	`
	tag, err := r.pool.Exec(ctx, query, item.ID, item.Quantity, item.TotalPrice, item.UpdatedAt)
	if err == nil && tag.RowsAffected() == 0 {
		return coreerrors.ErrConflict
	}
	return err
}

func (r *PostgresCartRepository) RemoveItem(ctx context.Context, cartID, itemID uuid.UUID) error {
	query := `DELETE FROM cart_items WHERE cart_id = $1 AND id = $2`
	tag, err := r.pool.Exec(ctx, query, cartID, itemID)
	if err == nil && tag.RowsAffected() == 0 {
		return coreerrors.ErrConflict
	}
	return err
}

func (r *PostgresCartRepository) GetItems(ctx context.Context, cartID uuid.UUID) ([]domain.CartItem, error) {
	query := `
		SELECT id, cart_id, menu_item_id, name, unit_price, quantity, total_price, created_at, updated_at
		FROM cart_items
		WHERE cart_id = $1
	`
	rows, err := r.pool.Query(ctx, query, cartID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanCartItems(rows)
}

func (r *PostgresCartRepository) scanCart(row core_database.Row) (*domain.Cart, error) {
	var cart domain.Cart
	err := row.Scan(&cart.ID, &cart.UserID, &cart.RestaurantID, &cart.CreatedAt, &cart.UpdatedAt, &cart.Revision)
	if err != nil {
		return nil, err
	}
	return &cart, nil
}

func (r *PostgresCartRepository) loadCartItems(ctx context.Context, cart *domain.Cart) (*domain.Cart, error) {
	items, err := r.GetItems(ctx, cart.ID)
	if err != nil {
		return nil, err
	}
	cart.Items = items
	cart.TotalAmount = cart.CalculateTotal()
	return cart, nil
}

func (r *PostgresCartRepository) scanCartItems(rows core_database.Rows) ([]domain.CartItem, error) {
	var items []domain.CartItem
	for rows.Next() {
		var item domain.CartItem
		if err := rows.Scan(&item.ID, &item.CartID, &item.MenuItemID, &item.Name, &item.UnitPrice, &item.Quantity, &item.TotalPrice, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

var _ CartRepository = (*PostgresCartRepository)(nil)
