package repository

import (
	"context"
	"errors"
	"time"

	core_database "github.com/Aam-Shaegar/kitchenflow/internal/core/database"
	core_errors "github.com/Aam-Shaegar/kitchenflow/internal/core/errors"
	"github.com/Aam-Shaegar/kitchenflow/internal/features/orders/domain"
	"github.com/google/uuid"
)

type PostgresOrderRepository struct {
	pool core_database.Pool
}

func NewOrderRepository(pool core_database.Pool) *PostgresOrderRepository {
	return &PostgresOrderRepository{pool: pool}
}

func (r *PostgresOrderRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Order, error) {
	query := `
		SELECT id, user_id, restaurant_id, external_order_id, status, total_amount, delivery_address, idempotency_key, created_at, updated_at
		FROM orders
		WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, query, id)
	order, err := r.scanOrder(row)
	if err != nil {
		if errors.Is(err, core_database.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r.loadOrderDetails(ctx, order)
}

func (r *PostgresOrderRepository) GetByIDAndUserID(ctx context.Context, id uuid.UUID, userID string) (*domain.Order, error) {
	query := `
		SELECT id, user_id, restaurant_id, external_order_id, status, total_amount, delivery_address, idempotency_key, created_at, updated_at
		FROM orders
		WHERE id = $1 AND user_id = $2
	`
	row := r.pool.QueryRow(ctx, query, id, userID)
	order, err := r.scanOrder(row)
	if err != nil {
		if errors.Is(err, core_database.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r.loadOrderDetails(ctx, order)
}

func (r *PostgresOrderRepository) GetByUserIDAndIdempotencyKey(ctx context.Context, userID, idempotencyKey string) (*domain.Order, error) {
	query := `
		SELECT id, user_id, restaurant_id, external_order_id, status, total_amount, delivery_address, idempotency_key, created_at, updated_at
		FROM orders
		WHERE user_id = $1 AND idempotency_key = $2
	`
	row := r.pool.QueryRow(ctx, query, userID, idempotencyKey)
	order, err := r.scanOrder(row)
	if err != nil {
		if errors.Is(err, core_database.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r.loadOrderDetails(ctx, order)
}

func (r *PostgresOrderRepository) GetByExternalOrderIDAndRestaurantID(ctx context.Context, externalOrderID string, restaurantID uuid.UUID) (*domain.Order, error) {
	query := `
		SELECT id, user_id, restaurant_id, external_order_id, status, total_amount, delivery_address, idempotency_key, created_at, updated_at
		FROM orders
		WHERE external_order_id = $1 AND restaurant_id = $2
	`
	row := r.pool.QueryRow(ctx, query, externalOrderID, restaurantID)
	order, err := r.scanOrder(row)
	if err != nil {
		if errors.Is(err, core_database.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r.loadOrderDetails(ctx, order)
}

func (r *PostgresOrderRepository) CreateWithIdempotencyKey(ctx context.Context, order *domain.Order) (*domain.Order, error) {
	// Everything below must succeed or fail together, so we wrap it in one transaction.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	// Rollback on the happy path is a harmless no-op (the tx is already committed),
	// so we deliberately ignore its error here.
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck // rollback after commit is a harmless no-op

	// Insert the order, but do nothing if this user already used this idempotency key.
	query := `
		INSERT INTO orders (id, user_id, restaurant_id, external_order_id, status, total_amount, delivery_address, idempotency_key, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (user_id, idempotency_key) DO NOTHING
		RETURNING id
	`
	var returnedID uuid.UUID
	err = tx.QueryRow(ctx, query,
		order.ID, order.UserID, order.RestaurantID, order.ExternalOrderID,
		order.Status, order.TotalAmount, order.DeliveryAddress,
		order.IdempotencyKey, order.CreatedAt, order.UpdatedAt,
	).Scan(&returnedID)

	if err != nil {
		if errors.Is(err, core_database.ErrNoRows) {
			// Someone already created an order with this key, so hand back the original.
			existing, getErr := r.getByUserIDAndIdempotencyKeyTx(ctx, tx, order.UserID, order.IdempotencyKey)
			if getErr != nil {
				return nil, getErr
			}
			return existing, nil
		}
		return nil, err
	}

	if returnedID != order.ID {
		// Defensive branch: DO NOTHING should never return a foreign id, but stay safe.
		existing, getErr := r.getByUserIDAndIdempotencyKeyTx(ctx, tx, order.UserID, order.IdempotencyKey)
		if getErr != nil {
			return nil, getErr
		}
		return existing, nil
	}

	// The order row exists, now persist its items and the initial history atomically.
	for _, item := range order.Items {
		if err := r.createOrderItemTx(ctx, tx, &item); err != nil {
			return nil, err
		}
	}

	for _, history := range order.StatusHistory {
		if err := r.addStatusHistoryTx(ctx, tx, &history); err != nil {
			return nil, err
		}
	}

	// Consume only the original cart when the durable order is created. HTTP
	// responses and retries never touch carts created for subsequent purchases.
	if order.CheckoutCartID != nil {
		tag, err := tx.Exec(ctx, `DELETE FROM carts WHERE id=$1 AND user_id=$2 AND restaurant_id=$3 AND revision=$4`, *order.CheckoutCartID, order.UserID, order.RestaurantID, order.CheckoutCartRevision)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() != 1 {
			return nil, core_errors.ErrConflict
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return order, nil
}

// getByUserIDAndIdempotencyKeyTx reads the idempotent duplicate inside the same tx,
// so a concurrent retry sees a consistent snapshot.
func (r *PostgresOrderRepository) getByUserIDAndIdempotencyKeyTx(ctx context.Context, tx core_database.Tx, userID, idempotencyKey string) (*domain.Order, error) {
	query := `
		SELECT id, user_id, restaurant_id, external_order_id, status, total_amount, delivery_address, idempotency_key, created_at, updated_at
		FROM orders
		WHERE user_id = $1 AND idempotency_key = $2
	`
	row := tx.QueryRow(ctx, query, userID, idempotencyKey)
	order, err := r.scanOrder(row)
	if err != nil {
		return nil, err
	}
	return r.loadOrderDetailsTx(ctx, tx, order)
}

// loadOrderDetailsTx attaches items and history while still inside the tx.
func (r *PostgresOrderRepository) loadOrderDetailsTx(ctx context.Context, tx core_database.Tx, order *domain.Order) (*domain.Order, error) {
	itemsQuery := `
		SELECT id, order_id, menu_item_id, name, unit_price, quantity, total_price, created_at, COALESCE(external_menu_item_id, '')
		FROM order_items
		WHERE order_id = $1
	`
	rows, err := tx.Query(ctx, itemsQuery, order.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var item domain.OrderItem
		if err := rows.Scan(&item.ID, &item.OrderID, &item.MenuItemID, &item.Name, &item.UnitPrice, &item.Quantity, &item.TotalPrice, &item.CreatedAt, &item.ExternalMenuItemID); err != nil {
			return nil, err
		}
		order.Items = append(order.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	history, err := r.GetStatusHistoryTx(ctx, tx, order.ID)
	if err != nil {
		return nil, err
	}
	order.StatusHistory = history

	return order, nil
}

// GetStatusHistoryTx reads an order's history inside an ongoing tx.
func (r *PostgresOrderRepository) GetStatusHistoryTx(ctx context.Context, tx core_database.Tx, orderID uuid.UUID) ([]domain.OrderStatusHistory, error) {
	query := `
		SELECT id, order_id, status, source, event_id, created_at
		FROM order_status_history
		WHERE order_id = $1
		ORDER BY created_at
	`
	rows, err := tx.Query(ctx, query, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []domain.OrderStatusHistory
	for rows.Next() {
		var h domain.OrderStatusHistory
		if err := rows.Scan(&h.ID, &h.OrderID, &h.Status, &h.Source, &h.EventID, &h.CreatedAt); err != nil {
			return nil, err
		}
		history = append(history, h)
	}
	return history, rows.Err()
}

func (r *PostgresOrderRepository) ListByUserIDPaginated(ctx context.Context, userID string, limit, offset int) ([]domain.Order, error) {
	query := `
		SELECT id, user_id, restaurant_id, external_order_id, status, total_amount, delivery_address, idempotency_key, created_at, updated_at
		FROM orders
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.pool.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []domain.Order
	for rows.Next() {
		var order domain.Order
		if err := rows.Scan(
			&order.ID, &order.UserID, &order.RestaurantID, &order.ExternalOrderID,
			&order.Status, &order.TotalAmount, &order.DeliveryAddress,
			&order.IdempotencyKey, &order.CreatedAt, &order.UpdatedAt,
		); err != nil {
			return nil, err
		}
		orders = append(orders, order)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range orders {
		loaded, err := r.loadOrderDetails(ctx, &orders[i])
		if err != nil {
			return nil, err
		}
		orders[i] = *loaded
	}
	if orders == nil {
		orders = []domain.Order{}
	}
	return orders, nil
}

func (r *PostgresOrderRepository) ListByRestaurantID(ctx context.Context, restaurantID uuid.UUID, limit, offset int) ([]domain.Order, error) {
	query := `
		SELECT id, user_id, restaurant_id, external_order_id, status, total_amount, delivery_address, idempotency_key, created_at, updated_at
		FROM orders
		WHERE restaurant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.pool.Query(ctx, query, restaurantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []domain.Order
	for rows.Next() {
		var order domain.Order
		if err := rows.Scan(
			&order.ID, &order.UserID, &order.RestaurantID, &order.ExternalOrderID,
			&order.Status, &order.TotalAmount, &order.DeliveryAddress,
			&order.IdempotencyKey, &order.CreatedAt, &order.UpdatedAt,
		); err != nil {
			return nil, err
		}
		orders = append(orders, order)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range orders {
		loaded, err := r.loadOrderDetails(ctx, &orders[i])
		if err != nil {
			return nil, err
		}
		orders[i] = *loaded
	}
	if orders == nil {
		orders = []domain.Order{}
	}
	return orders, nil
}

// PrepareIntegrationRetry publishes the correlation ID and pending state before
// calling the restaurant, so callbacks can arrive before its HTTP response.
func (r *PostgresOrderRepository) PrepareIntegrationRetry(ctx context.Context, orderID uuid.UUID, externalID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var current domain.OrderStatus
	if err = tx.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1 FOR UPDATE`, orderID).Scan(&current); err != nil {
		return err
	}
	if current != domain.OrderStatusIntegrationFailed {
		return core_errors.ErrConflict
	}
	now := time.Now().UTC()
	if _, err = tx.Exec(ctx, `UPDATE orders SET external_order_id=$2,status='pending',updated_at=$3 WHERE id=$1`, orderID, externalID, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO order_status_history(order_id,status,source,created_at) VALUES($1,'pending','kitchen',$2)`, orderID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SaveIntegrationResult reconciles the HTTP result with callbacks under the same
// row lock. Only a still-pending order may be changed by the creation response.
func (r *PostgresOrderRepository) SaveIntegrationResult(ctx context.Context, orderID uuid.UUID, externalID string, status domain.OrderStatus) error {
	if status != domain.OrderStatusPending && status != domain.OrderStatusConfirmed && status != domain.OrderStatusCancelled && status != domain.OrderStatusIntegrationFailed {
		return core_errors.ErrInvalidArgument
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var current domain.OrderStatus
	var savedExternalID *string
	if err = tx.QueryRow(ctx, `SELECT status,external_order_id FROM orders WHERE id=$1 FOR UPDATE`, orderID).Scan(&current, &savedExternalID); err != nil {
		return err
	}
	if savedExternalID == nil || *savedExternalID != externalID {
		return core_errors.ErrConflict
	}
	if current == domain.OrderStatusPending && status != current {
		now := time.Now().UTC()
		if _, err = tx.Exec(ctx, `UPDATE orders SET status=$2,updated_at=$3 WHERE id=$1`, orderID, status, now); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO order_status_history(order_id,status,source,created_at) VALUES($1,$2,$3,$4)`, orderID, status, integrationResultSource(status), now); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func integrationResultSource(status domain.OrderStatus) string {
	if status == domain.OrderStatusIntegrationFailed {
		return "kitchen"
	}
	return "restaurant"
}

func (r *PostgresOrderRepository) createOrderItemTx(ctx context.Context, tx core_database.Tx, item *domain.OrderItem) error {
	query := `
		INSERT INTO order_items (id, order_id, menu_item_id, name, unit_price, quantity, total_price, created_at, external_menu_item_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := tx.Exec(ctx, query,
		item.ID, item.OrderID, item.MenuItemID, item.Name,
		item.UnitPrice, item.Quantity, item.TotalPrice, item.CreatedAt, item.ExternalMenuItemID,
	)
	return err
}

func (r *PostgresOrderRepository) addStatusHistoryTx(ctx context.Context, tx core_database.Tx, history *domain.OrderStatusHistory) error {
	// Initial history belongs to the creation transaction. Any insert failure
	// must abort creation; callback deduplication lives in UpdateStatusWithHistory.

	query := `
		INSERT INTO order_status_history (order_id, status, source, event_id, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := tx.Exec(ctx, query, history.OrderID, history.Status, history.Source, history.EventID, history.CreatedAt)
	return err
}

func (r *PostgresOrderRepository) UpdateStatusWithHistory(ctx context.Context, orderID uuid.UUID, status domain.OrderStatus, history *domain.OrderStatusHistory) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck // rollback after commit is a harmless no-op

	// Lock the order row: two callbacks racing each other must be serialized here.
	lockQuery := `SELECT status FROM orders WHERE id = $1 FOR UPDATE`
	var currentStatus domain.OrderStatus
	err = tx.QueryRow(ctx, lockQuery, orderID).Scan(&currentStatus)
	if err != nil {
		return err
	}

	if history.OrderID != orderID || history.Status != status {
		return core_errors.ErrInvalidArgument
	}

	// Event IDs are globally unique. Only an identical event for this order
	// is a replay; reusing an ID for another order or payload is a conflict.
	if history.EventID != nil {
		var savedOrderID uuid.UUID
		var savedStatus domain.OrderStatus
		var savedSource string
		err := tx.QueryRow(ctx, `
			SELECT order_id, status, source FROM order_status_history WHERE event_id = $1
		`, *history.EventID).Scan(&savedOrderID, &savedStatus, &savedSource)
		if err == nil {
			if savedOrderID != orderID || savedStatus != status || savedSource != history.Source {
				return core_errors.ErrConflict
			}
			return nil
		}
		if !errors.Is(err, core_database.ErrNoRows) {
			return err
		}
	}

	// A new callback may report the current state. Record its event ID without
	// changing updated_at; ordinary transitions still use the domain rules.
	sameStateCallback := history.EventID != nil && currentStatus == status
	if !sameStateCallback && !domain.CanTransition(currentStatus, status) {
		return &domain.OrderStatusTransitionError{From: currentStatus, To: status}
	}

	// A different order may concurrently claim the same event ID. The unique
	// index resolves that race; a collision rolls back the entire transaction.
	var historyID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO order_status_history (order_id, status, source, event_id, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (event_id) WHERE event_id IS NOT NULL DO NOTHING
		RETURNING id
	`, history.OrderID, history.Status, history.Source, history.EventID, history.CreatedAt).Scan(&historyID)
	if errors.Is(err, core_database.ErrNoRows) {
		return core_errors.ErrConflict
	}
	if err != nil {
		return err
	}

	if !sameStateCallback {
		if _, err := tx.Exec(ctx, `UPDATE orders SET status = $2, updated_at = $3 WHERE id = $1`,
			orderID, status, history.CreatedAt); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (r *PostgresOrderRepository) GetStatusHistory(ctx context.Context, orderID uuid.UUID) ([]domain.OrderStatusHistory, error) {
	query := `
		SELECT id, order_id, status, source, event_id, created_at
		FROM order_status_history
		WHERE order_id = $1
		ORDER BY created_at
	`
	rows, err := r.pool.Query(ctx, query, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []domain.OrderStatusHistory
	for rows.Next() {
		var h domain.OrderStatusHistory
		if err := rows.Scan(&h.ID, &h.OrderID, &h.Status, &h.Source, &h.EventID, &h.CreatedAt); err != nil {
			return nil, err
		}
		history = append(history, h)
	}
	return history, rows.Err()
}

func (r *PostgresOrderRepository) scanOrder(row core_database.Row) (*domain.Order, error) {
	var order domain.Order
	err := row.Scan(
		&order.ID, &order.UserID, &order.RestaurantID, &order.ExternalOrderID,
		&order.Status, &order.TotalAmount, &order.DeliveryAddress,
		&order.IdempotencyKey, &order.CreatedAt, &order.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &order, nil
}

func (r *PostgresOrderRepository) loadOrderDetails(ctx context.Context, order *domain.Order) (*domain.Order, error) {
	itemsQuery := `
		SELECT id, order_id, menu_item_id, name, unit_price, quantity, total_price, created_at, COALESCE(external_menu_item_id, '')
		FROM order_items
		WHERE order_id = $1
	`
	rows, err := r.pool.Query(ctx, itemsQuery, order.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var item domain.OrderItem
		if err := rows.Scan(&item.ID, &item.OrderID, &item.MenuItemID, &item.Name, &item.UnitPrice, &item.Quantity, &item.TotalPrice, &item.CreatedAt, &item.ExternalMenuItemID); err != nil {
			return nil, err
		}
		order.Items = append(order.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	history, err := r.GetStatusHistory(ctx, order.ID)
	if err != nil {
		return nil, err
	}
	order.StatusHistory = history

	return order, nil
}

var _ OrderRepository = (*PostgresOrderRepository)(nil)
