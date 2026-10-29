-- Money in minor units (kopecks): NUMERIC(12,2) -> BIGINT.
-- Idempotent: safe to run on fresh installs where columns are already BIGINT.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'menu_items' AND column_name = 'price' AND data_type = 'numeric'
    ) THEN
        ALTER TABLE menu_items ALTER COLUMN price TYPE BIGINT USING (ROUND(price * 100))::BIGINT;
        ALTER TABLE menu_items ALTER COLUMN price SET NOT NULL;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'cart_items' AND column_name = 'unit_price' AND data_type = 'numeric'
    ) THEN
        ALTER TABLE cart_items ALTER COLUMN unit_price TYPE BIGINT USING (ROUND(unit_price * 100))::BIGINT;
        ALTER TABLE cart_items ALTER COLUMN total_price TYPE BIGINT USING (ROUND(total_price * 100))::BIGINT;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'orders' AND column_name = 'total_amount' AND data_type = 'numeric'
    ) THEN
        ALTER TABLE orders ALTER COLUMN total_amount TYPE BIGINT USING (ROUND(total_amount * 100))::BIGINT;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'order_items' AND column_name = 'unit_price' AND data_type = 'numeric'
    ) THEN
        ALTER TABLE order_items ALTER COLUMN unit_price TYPE BIGINT USING (ROUND(unit_price * 100))::BIGINT;
        ALTER TABLE order_items ALTER COLUMN total_price TYPE BIGINT USING (ROUND(total_price * 100))::BIGINT;
    END IF;
END $$;

-- Keep non-negative invariants on the new integer columns.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'menu_items_price_nonneg') THEN
        ALTER TABLE menu_items ADD CONSTRAINT menu_items_price_nonneg CHECK (price >= 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'cart_items_unit_price_nonneg') THEN
        ALTER TABLE cart_items ADD CONSTRAINT cart_items_unit_price_nonneg CHECK (unit_price >= 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'cart_items_total_price_nonneg') THEN
        ALTER TABLE cart_items ADD CONSTRAINT cart_items_total_price_nonneg CHECK (total_price >= 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'orders_total_amount_nonneg') THEN
        ALTER TABLE orders ADD CONSTRAINT orders_total_amount_nonneg CHECK (total_amount >= 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'order_items_unit_price_nonneg') THEN
        ALTER TABLE order_items ADD CONSTRAINT order_items_unit_price_nonneg CHECK (unit_price >= 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'order_items_total_price_nonneg') THEN
        ALTER TABLE order_items ADD CONSTRAINT order_items_total_price_nonneg CHECK (total_price >= 0);
    END IF;
END $$;
