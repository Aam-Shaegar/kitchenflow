-- Seed data for demo restaurant
-- Restaurant ID: 550e8400-e29b-41d4-a716-446655440000

INSERT INTO restaurants (id, external_id, name, description, is_open, integration_base_url)
VALUES (
    '550e8400-e29b-41d4-a716-446655440000',
    'demo-restaurant-001',
    'Demo Restaurant',
    'A demo restaurant for testing',
    true,
    'http://restaurant-mock:8081'
) ON CONFLICT (id) DO NOTHING;

-- Menu categories
INSERT INTO menu_categories (id, restaurant_id, name, position)
VALUES
    ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa1', '550e8400-e29b-41d4-a716-446655440000', 'Burgers', 1),
    ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa2', '550e8400-e29b-41d4-a716-446655440000', 'Drinks', 2),
    ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa3', '550e8400-e29b-41d4-a716-446655440000', 'Sides', 3)
ON CONFLICT (id) DO NOTHING;

-- Menu items (prices in minor units, kopecks)
-- Burgers
INSERT INTO menu_items (id, restaurant_id, category_id, external_id, name, description, price, is_available)
VALUES
    ('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbb1', '550e8400-e29b-41d4-a716-446655440000', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa1', 'burger-classic', 'Classic Burger', 'Beef patty, lettuce, tomato, cheese', 54900, true),
    ('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbb2', '550e8400-e29b-41d4-a716-446655440000', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa1', 'burger-double', 'Double Burger', 'Two beef patties, cheese, bacon', 79900, true),
    ('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbb3', '550e8400-e29b-41d4-a716-446655440000', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa1', 'burger-veggie', 'Veggie Burger', 'Plant-based patty, avocado, sprouts', 62900, true)
ON CONFLICT (id) DO NOTHING;

-- Drinks
INSERT INTO menu_items (id, restaurant_id, category_id, external_id, name, description, price, is_available)
VALUES
    ('cccccccc-cccc-cccc-cccc-cccccccccccc', '550e8400-e29b-41d4-a716-446655440000', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa2', 'drink-cola', 'Cola', 'Classic cola 0.5L', 9900, true),
    ('cccccccc-cccc-cccc-cccc-cccccccccccd', '550e8400-e29b-41d4-a716-446655440000', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa2', 'drink-water', 'Mineral Water', 'Still water 0.5L', 6900, true)
ON CONFLICT (id) DO NOTHING;

-- Sides
INSERT INTO menu_items (id, restaurant_id, category_id, external_id, name, description, price, is_available)
VALUES
    ('dddddddd-dddd-dddd-dddd-dddddddddddd', '550e8400-e29b-41d4-a716-446655440000', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa3', 'side-fries', 'French Fries', 'Crispy golden fries', 19900, true),
    ('dddddddd-dddd-dddd-dddd-ddddddddddde', '550e8400-e29b-41d4-a716-446655440000', 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaa3', 'side-onion-rings', 'Onion Rings', 'Crispy onion rings', 22900, true)
ON CONFLICT (id) DO NOTHING;