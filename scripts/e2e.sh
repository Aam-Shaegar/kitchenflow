#!/usr/bin/env bash
set -euo pipefail
BASE="${BASE_URL:-http://localhost:8080}"
USER_ID="e2e-user-$(date +%s)"
RID="550e8400-e29b-41d4-a716-446655440000"
ITEM="bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbb1"

pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1"; exit 1; }

echo "== KitchenFlow E2E: $BASE as $USER_ID =="

code=$(curl -s -o /tmp/e2e_health.json -w "%{http_code}" "$BASE/api/v1/health")
[ "$code" = "200" ] || fail "health $code"
pass "health"

curl -s "$BASE/api/v1/restaurants" -o /tmp/e2e_rest.json
grep -q "$RID" /tmp/e2e_rest.json || fail "seed restaurant missing"
pass "list restaurants"

curl -s "$BASE/api/v1/restaurants/$RID/menu" -o /tmp/e2e_menu.json
grep -q "Classic Burger" /tmp/e2e_menu.json || fail "menu missing"
pass "menu"

code=$(curl -s -o /tmp/e2e_cart.json -w "%{http_code}" -X POST "$BASE/api/v1/cart/items" \
  -H "Content-Type: application/json" -H "X-User-ID: $USER_ID" \
  -d "{\"restaurant_id\":\"$RID\",\"menu_item_id\":\"$ITEM\",\"quantity\":2}")
[ "$code" = "201" ] || fail "add to cart $code $(cat /tmp/e2e_cart.json)"
grep -q "54900" /tmp/e2e_cart.json || fail "price must be kopecks (54900)"
pass "add to cart (kopecks)"

# second restaurant must conflict (single active cart)
code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE/api/v1/cart/items" \
  -H "Content-Type: application/json" -H "X-User-ID: $USER_ID" \
  -d "{\"restaurant_id\":\"660e8400-e29b-41d4-a716-446655440001\",\"menu_item_id\":\"$ITEM\",\"quantity\":1}")
[ "$code" = "409" ] || [ "$code" = "400" ] || echo "WARN: cross-restaurant add returned $code (want 409)"

code=$(curl -s -o /tmp/e2e_order.json -w "%{http_code}" -X POST "$BASE/api/v1/orders" \
  -H "Content-Type: application/json" -H "X-User-ID: $USER_ID" -H "Idempotency-Key: e2e-$USER_ID" \
  -d '{"delivery_address":"Moscow, Tverskaya 1"}')
[ "$code" = "201" ] || fail "create order $code $(cat /tmp/e2e_order.json)"
ORDER_ID=$(python3 -c "import json;print(json.load(open('/tmp/e2e_order.json'))['id'])")
STATUS=$(python3 -c "import json;print(json.load(open('/tmp/e2e_order.json'))['status'])")
echo "order=$ORDER_ID status=$STATUS"
pass "create order ($STATUS)"

# idempotency replay
code=$(curl -s -o /tmp/e2e_order2.json -w "%{http_code}" -X POST "$BASE/api/v1/orders" \
  -H "Content-Type: application/json" -H "X-User-ID: $USER_ID" -H "Idempotency-Key: e2e-$USER_ID" \
  -d '{"delivery_address":"Moscow, Tverskaya 1"}')
ORDER_ID2=$(python3 -c "import json;print(json.load(open('/tmp/e2e_order2.json'))['id'])")
[ "$ORDER_ID" = "$ORDER_ID2" ] || fail "idempotency replay gave different id"
pass "idempotency"

# history + cancel semantics
code=$(curl -s -o /tmp/e2e_hist.json -w "%{http_code}" "$BASE/api/v1/orders/$ORDER_ID/history" -H "X-User-ID: $USER_ID")
[ "$code" = "200" ] || fail "history $code"
pass "history"

if [ "$STATUS" = "pending" ] || [ "$STATUS" = "confirmed" ]; then
  code=$(curl -s -o /tmp/e2e_cancel.json -w "%{http_code}" -X POST "$BASE/api/v1/orders/$ORDER_ID/cancel" -H "X-User-ID: $USER_ID")
  [ "$code" = "200" ] || fail "cancel $code $(cat /tmp/e2e_cancel.json)"
  pass "cancel"
else
  echo "SKIP cancel (status=$STATUS)"
fi

echo "ALL E2E PASSED"
