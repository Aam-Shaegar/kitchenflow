package repository

import (
	"context"
	"errors"
	db "github.com/Aam-Shaegar/kitchenflow/internal/core/database"
	"github.com/google/uuid"
)

// ClaimDispatch uses a lease so a crashed process cannot strand a job. The
// lease exceeds the partner HTTP timeout; partner idempotency covers expiry.
func (r *PostgresOrderRepository) ClaimDispatch(ctx context.Context) (uuid.UUID, uuid.UUID, string, error) {
	token := uuid.New()
	var id uuid.UUID
	var base string
	err := r.pool.QueryRow(ctx, `UPDATE order_dispatch SET lease_token=$1,lease_until=now()+interval '60 seconds',attempts=attempts+1
 WHERE order_id=(SELECT order_id FROM order_dispatch WHERE available_at<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY available_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING order_id,base_url`, token).Scan(&id, &base)
	if errors.Is(err, db.ErrNoRows) {
		return uuid.Nil, uuid.Nil, "", nil
	}
	return id, token, base, err
}
func (r *PostgresOrderRepository) ReleaseDispatch(ctx context.Context, id, token uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE order_dispatch SET lease_token=NULL,lease_until=NULL,available_at=now()+interval '30 seconds' * LEAST(attempts,20) WHERE order_id=$1 AND lease_token=$2`, id, token)
	return err
}
