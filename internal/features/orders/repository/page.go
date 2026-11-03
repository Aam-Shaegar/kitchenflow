package repository

import (
	"context"
	"fmt"

	"github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
	"github.com/google/uuid"
)

// column is chosen by the two public methods, never supplied by a request.
func (r *PostgresOrderRepository) listPage(ctx context.Context, column string, value any, limit, offset int) ([]domain.Order, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT id,user_id,restaurant_id,external_order_id,status,total_amount,delivery_address,idempotency_key,created_at,updated_at FROM orders WHERE %s=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, column), value, limit, offset)
	if err != nil {
		return nil, err
	}
	orders := make([]domain.Order, 0)
	var ids []uuid.UUID
	positions := make(map[uuid.UUID]int)
	for rows.Next() {
		var o domain.Order
		if err := rows.Scan(&o.ID, &o.UserID, &o.RestaurantID, &o.ExternalOrderID, &o.Status, &o.TotalAmount, &o.DeliveryAddress, &o.IdempotencyKey, &o.CreatedAt, &o.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		o.Items = []domain.OrderItem{}
		o.StatusHistory = []domain.OrderStatusHistory{}
		positions[o.ID] = len(orders)
		ids = append(ids, o.ID)
		orders = append(orders, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		rows, err = tx.Query(ctx, `SELECT id,order_id,menu_item_id,name,unit_price,quantity,total_price,created_at,COALESCE(external_menu_item_id,'') FROM order_items WHERE order_id=ANY($1::uuid[]) ORDER BY order_id,created_at,id`, ids)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var item domain.OrderItem
			if err := rows.Scan(&item.ID, &item.OrderID, &item.MenuItemID, &item.Name, &item.UnitPrice, &item.Quantity, &item.TotalPrice, &item.CreatedAt, &item.ExternalMenuItemID); err != nil {
				rows.Close()
				return nil, err
			}
			index := positions[item.OrderID]
			orders[index].Items = append(orders[index].Items, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		rows, err = tx.Query(ctx, `SELECT id,order_id,status,source,event_id,created_at FROM order_status_history WHERE order_id=ANY($1::uuid[]) ORDER BY order_id,created_at,id`, ids)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var history domain.OrderStatusHistory
			if err := rows.Scan(&history.ID, &history.OrderID, &history.Status, &history.Source, &history.EventID, &history.CreatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			index := positions[history.OrderID]
			orders[index].StatusHistory = append(orders[index].StatusHistory, history)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return orders, nil
}
