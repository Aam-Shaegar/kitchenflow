-- Preserve the partner product identifier independently of mutable menu data.
-- Legacy identifiers cannot be reconstructed reliably: leave them NULL.
-- Retry rejects such orders until an operator reconciles their historical IDs.
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS external_menu_item_id TEXT
    CHECK (external_menu_item_id IS NULL OR length(btrim(external_menu_item_id)) > 0);
