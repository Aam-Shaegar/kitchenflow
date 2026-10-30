-- Each item mutation participates in the parent cart lock used by checkout.
ALTER TABLE carts ADD COLUMN IF NOT EXISTS revision BIGINT NOT NULL DEFAULT 0;

CREATE OR REPLACE FUNCTION bump_cart_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.cart_id <> OLD.cart_id THEN
        RAISE EXCEPTION 'moving cart items is not supported';
    END IF;
    IF TG_OP = 'DELETE' THEN
        UPDATE carts SET revision = revision + 1 WHERE id = OLD.cart_id;
        RETURN OLD;
    END IF;
    UPDATE carts SET revision = revision + 1 WHERE id = NEW.cart_id;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS cart_items_revision ON cart_items;
CREATE TRIGGER cart_items_revision BEFORE INSERT OR UPDATE OR DELETE ON cart_items
FOR EACH ROW EXECUTE FUNCTION bump_cart_revision();
