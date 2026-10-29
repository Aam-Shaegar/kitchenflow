# KitchenFlow — Domain Model & Business Rules

## Scope

MVP connects users with restaurants/stores delivering food. Authentication is out of scope.

## Actors

- User — browses restaurants and menus, manages a cart, creates orders, tracks status.
- Restaurant — provides menu/availability and processes orders.
- Kitchen API — canonical system of record for restaurants, menu snapshots, carts and orders.

## MVP scenarios

1. Browse restaurants.
2. Open a restaurant menu.
3. Add/remove/change cart items.
4. Validate restaurant availability and item availability before checkout.
5. Create an order.
6. Send order to restaurant integration.
7. Restaurant accepts/rejects and changes order state.
8. Kitchen receives restaurant callbacks and exposes current status to user.
9. User can inspect order history/details.

## Edge cases

- Restaurant is closed/unavailable.
- Menu item became unavailable between menu viewing and checkout.
- Menu price changed between browsing and checkout.
- Restaurant integration is unavailable/timeouts.
- Duplicate create-order request.
- Duplicate/out-of-order restaurant callback.
- Invalid order status transition.

## Entities

### Restaurant

- `id` — internal UUID.
- `external_id` — restaurant-owned identifier.
- `name`, `description`.
- `is_open`.
- `integration_base_url`.
- timestamps.

### MenuCategory

Belongs to a restaurant; ordered for presentation.

### MenuItem

Belongs to a category/restaurant. Stores current price and availability.

### Cart / CartItem

Server-side cart. Product rule: **one active cart per user** (single-restaurant UX like
Yandex Eda / Delivery Club). Adding an item from another restaurant while the cart is
non-empty returns `409 Conflict`; the client must `DELETE /cart` first. Quantities are
1..50. Prices are snapshots in minor units (kopecks, `BIGINT`).

### Order

Canonical order in Kitchen. Stores an internal ID and a correlation external order ID before contacting the restaurant. Restaurants echo this ID in responses and callbacks. Stores status and totals **in minor units (kopecks, `BIGINT`)**. Supports user cancel from
`pending`/`confirmed` and retry of `integration_failed` via dedicated endpoints.

### OrderItem

Stores immutable snapshot of product name and unit price at order creation, plus quantity and line total.

### OrderStatusHistory

Append-only status transition audit trail.

## Order state machine

Allowed forward path:

`pending -> confirmed -> preparing -> ready -> delivering -> delivered`

Cancellation is allowed only from states explicitly permitted by business rules. MVP should allow cancellation from `pending` and `confirmed`; after `preparing` cancellation is rejected.

A status transition is validated in the order service, never in HTTP handlers.

## External identity

Internal primary keys are owned by Kitchen. External ids are separate fields with uniqueness scoped to the external system. Never use restaurant external ids as internal primary keys.

## Price snapshot invariant

`order_items.unit_price` and `order_items.name` are copied from the menu at checkout. Historical orders never depend on mutable menu data.

## Idempotency invariant

`POST /orders` accepts an `Idempotency-Key`. The same key for the same user produces the same logical order/result and must not create duplicates.

Restaurant callbacks are also idempotent: repeating the same callback must not mutate the order twice.

## Consistency policy for MVP

Order creation and its local records are committed in a database transaction. External restaurant interaction is synchronous for MVP. If external creation fails, the order remains in a non-final integration state and the API returns a stable representation of that state.

Production evolution: transactional outbox + asynchronous delivery/retry worker.
