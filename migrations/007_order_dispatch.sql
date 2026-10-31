CREATE TABLE order_dispatch (
 order_id UUID PRIMARY KEY REFERENCES orders(id),
 base_url TEXT NOT NULL,
 available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 lease_token UUID,
 lease_until TIMESTAMPTZ,
 attempts INTEGER NOT NULL DEFAULT 0
);
-- Legacy orders without complete product snapshots need manual reconciliation.
INSERT INTO order_dispatch(order_id,base_url)
SELECT o.id,r.integration_base_url FROM orders o JOIN restaurants r ON r.id=o.restaurant_id
WHERE o.status IN ('pending','integration_failed') AND o.external_order_id IS NOT NULL
AND EXISTS(SELECT 1 FROM order_items i WHERE i.order_id=o.id)
AND NOT EXISTS(SELECT 1 FROM order_items i WHERE i.order_id=o.id AND i.external_menu_item_id IS NULL);

CREATE INDEX order_dispatch_available ON order_dispatch(available_at);
