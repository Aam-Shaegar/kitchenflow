package repository

import (
	"context"
	"errors"
	db "github.com/Aam-Shaegar/kitchenflow/internal/core/database"
	coreerrors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/cart/domain"
	"github.com/google/uuid"
)

type PostgresCartRepository struct{ pool db.Pool }

func NewCartRepository(pool db.Pool) *PostgresCartRepository {
	return &PostgresCartRepository{pool: pool}
}

// Lock order: user advisory lock, parent cart, then item rows. The parent
// lock also serializes mutations with checkout and its revision check.
func (r *PostgresCartRepository) transact(ctx context.Context, user string, change func(db.Tx, *domain.Cart) error) (*domain.Cart, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 81508))`, user); err != nil {
		return nil, err
	}
	cart, err := r.read(ctx, tx, user)
	if err != nil {
		return nil, err
	}
	if change != nil {
		if err := change(tx, cart); err != nil {
			return nil, err
		}
		cart, err = r.read(ctx, tx, user)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return cart, nil
}
func (r *PostgresCartRepository) read(ctx context.Context, tx db.Tx, user string) (*domain.Cart, error) {
	var c domain.Cart
	err := tx.QueryRow(ctx, `SELECT id,user_id,restaurant_id,created_at,updated_at,revision FROM carts WHERE user_id=$1 FOR UPDATE`, user).Scan(&c.ID, &c.UserID, &c.RestaurantID, &c.CreatedAt, &c.UpdatedAt, &c.Revision)
	if errors.Is(err, db.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id,cart_id,menu_item_id,name,unit_price,quantity,total_price,created_at,updated_at FROM cart_items WHERE cart_id=$1 ORDER BY id`, c.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	c.Items, err = r.scanCartItems(rows)
	if err != nil {
		return nil, err
	}
	c.TotalAmount = c.CalculateTotal()
	return &c, nil
}
func (r *PostgresCartRepository) GetByUserID(ctx context.Context, user string) (*domain.Cart, error) {
	return r.transact(ctx, user, nil)
}
func (r *PostgresCartRepository) AddToCart(ctx context.Context, user string, restaurant uuid.UUID, item domain.CartItem) (*domain.Cart, error) {
	if item.Quantity < 1 || item.Quantity > 50 {
		return nil, coreerrors.ErrInvalidArgument
	}
	return r.transact(ctx, user, func(tx db.Tx, c *domain.Cart) error {
		if c != nil && c.RestaurantID != restaurant {
			if len(c.Items) > 0 {
				return coreerrors.ErrConflict
			}
			if _, err := tx.Exec(ctx, `DELETE FROM carts WHERE id=$1`, c.ID); err != nil {
				return err
			}
			c = nil
		}
		if c == nil {
			c = &domain.Cart{ID: uuid.New()}
			if _, err := tx.Exec(ctx, `INSERT INTO carts(id,user_id,restaurant_id) VALUES($1,$2,$3)`, c.ID, user, restaurant); err != nil {
				return err
			}
		}
		index := c.FindItemIndex(item.MenuItemID)
		if index >= 0 {
			existing := c.Items[index]
			qty := existing.Quantity + item.Quantity
			if qty > 50 {
				return coreerrors.ErrInvalidArgument
			}
			if _, err := tx.Exec(ctx, `UPDATE cart_items SET quantity=$2::integer,total_price=unit_price*$2::integer,updated_at=now() WHERE id=$1`, existing.ID, qty); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(ctx, `INSERT INTO cart_items(id,cart_id,menu_item_id,name,unit_price,quantity,total_price) VALUES($1,$2,$3,$4,$5,$6,$7)`, uuid.New(), c.ID, item.MenuItemID, item.Name, item.UnitPrice, item.Quantity, item.UnitPrice*int64(item.Quantity)); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE carts SET updated_at=now() WHERE id=$1`, c.ID)
		return err
	})
}
func (r *PostgresCartRepository) SetQuantity(ctx context.Context, user string, id uuid.UUID, quantity int) (*domain.Cart, error) {
	if quantity < 1 || quantity > 50 {
		return nil, coreerrors.ErrInvalidArgument
	}
	return r.changeItem(ctx, user, id, func(tx db.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE cart_items SET quantity=$2::integer,total_price=unit_price*$2::integer,updated_at=now() WHERE id=$1`, id, quantity)
		return err
	})
}
func (r *PostgresCartRepository) RemoveCartItem(ctx context.Context, user string, id uuid.UUID) (*domain.Cart, error) {
	return r.changeItem(ctx, user, id, func(tx db.Tx) error { _, err := tx.Exec(ctx, `DELETE FROM cart_items WHERE id=$1`, id); return err })
}
func (r *PostgresCartRepository) changeItem(ctx context.Context, user string, id uuid.UUID, change func(db.Tx) error) (*domain.Cart, error) {
	return r.transact(ctx, user, func(tx db.Tx, c *domain.Cart) error {
		if c == nil {
			return coreerrors.ErrNotFound
		}
		found := false
		for _, item := range c.Items {
			if item.ID == id {
				found = true
				break
			}
		}
		if !found {
			return coreerrors.ErrNotFound
		}
		if err := change(tx); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE carts SET updated_at=now() WHERE id=$1`, c.ID)
		return err
	})
}
func (r *PostgresCartRepository) ClearByUser(ctx context.Context, user string) error {
	_, err := r.transact(ctx, user, func(tx db.Tx, c *domain.Cart) error {
		if c == nil {
			return nil
		}
		_, err := tx.Exec(ctx, `DELETE FROM carts WHERE id=$1`, c.ID)
		return err
	})
	return err
}
func (r *PostgresCartRepository) scanCartItems(rows db.Rows) ([]domain.CartItem, error) {
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
