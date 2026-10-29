-- Single active cart per user (product rule).
-- Keeps the most recently updated cart per user, drops the rest,
-- then enforces UNIQUE(user_id).
DELETE FROM cart_items WHERE cart_id IN (
    SELECT id FROM carts WHERE id NOT IN (
        SELECT DISTINCT ON (user_id) id FROM carts ORDER BY user_id, updated_at DESC
    )
);
DELETE FROM carts WHERE id NOT IN (
    SELECT DISTINCT ON (user_id) id FROM carts ORDER BY user_id, updated_at DESC
);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'carts_user_id_restaurant_id_key') THEN
        ALTER TABLE carts DROP CONSTRAINT carts_user_id_restaurant_id_key;
    END IF;
    -- Postgres auto-names UNIQUE(user_id, restaurant_id); drop by index if constraint name differs.
    IF EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = 'carts_user_id_restaurant_id_key') THEN
        DROP INDEX IF EXISTS carts_user_id_restaurant_id_key;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'carts_user_id_key') THEN
        ALTER TABLE carts ADD CONSTRAINT carts_user_id_key UNIQUE (user_id);
    END IF;
END $$;
