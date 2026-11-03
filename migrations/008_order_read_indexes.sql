-- Deterministic page and detail ordering, including equal timestamps.
CREATE INDEX idx_orders_user_page ON orders(user_id,created_at DESC,id DESC);
CREATE INDEX idx_orders_restaurant_page ON orders(restaurant_id,created_at DESC,id DESC);
CREATE INDEX idx_order_items_order_page ON order_items(order_id,created_at,id);
CREATE INDEX idx_order_history_order_page ON order_status_history(order_id,created_at,id);
