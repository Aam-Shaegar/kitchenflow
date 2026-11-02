package health

import (
	"context"
	"encoding/json"
	"fmt"
	db "github.com/Aam-Shaegar/kitchenflow/internal/core/database"
	"net/http"
	"os"
	"strings"
	"time"
)

func Live(w http.ResponseWriter, r *http.Request) { reply(w, 200, "alive") }
func Ready(pool db.Pool, dir string, lifecycle context.Context) (http.HandlerFunc, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no required migrations")
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if lifecycle.Err() != nil {
			reply(w, 503, "not ready")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations WHERE filename=ANY($1::text[])`, names).Scan(&count); err != nil || count != len(names) {
			reply(w, 503, "not ready")
			return
		}
		// Parse/authorize the runtime tables and versioned columns, without scanning data.
		rows, err := pool.Query(ctx, `SELECT r.id,mc.id,mi.external_id,mi.price,c.revision,ci.quantity,o.status,o.external_order_id,oi.external_menu_item_id,oh.event_id,d.lease_token
 FROM restaurants r,menu_categories mc,menu_items mi,carts c,cart_items ci,orders o,order_items oi,order_status_history oh,order_dispatch d WHERE false`)
		if err != nil {
			reply(w, 503, "not ready")
			return
		}
		defer rows.Close()
		for rows.Next() {
		}
		if rows.Err() != nil || lifecycle.Err() != nil {
			reply(w, 503, "not ready")
			return
		}
		reply(w, 200, "ready")
	}, nil
}
func reply(w http.ResponseWriter, code int, status string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
}
