# KitchenFlow — готовое решение для доставки еды

Product-ready backend маркетплейса доставки: витрина ресторанов, корзина, заказы с идемпотентностью,
интеграция заведений через HTTP + колбэки, портал партнёра (онбординг, меню, заказы).

## Project Overview

KitchenFlow is a food delivery platform backend that connects users with restaurants. It provides APIs for browsing restaurants, viewing menus, managing a single active cart, creating orders, cancelling/retrying them, and tracking status history. Partner restaurants are onboarded via a closed portal API, manage their menu, receive orders over HTTP and report progress via idempotent callbacks.

### Business Scenarios

1. **Browse Restaurants** - Users list restaurants (`?open=true`), open cards
2. **View Menu** - Menu by categories, `?available=true` filter
3. **Manage Cart** - Single active cart per user; cross-restaurant add => 409, clear via `DELETE /cart`
4. **Create Order** - Checkout with address + `Idempotency-Key`; price snapshot in kopecks
5. **Order Tracking** - Status + full history; cancel from pending/confirmed; retry on `integration_failed`
6. **Restaurant Integration** - Mock or real restaurant receives `POST /restaurant/orders`, sends callbacks
7. **Partner Portal** - Onboard restaurant, toggle `is_open`, CRUD categories/items, list incoming orders

### Key Features

- **Single active cart** - One cart per user; explicit clear, quantity 1..50
- **Money in kopecks** - All prices/totals are `BIGINT` minor units, no float errors
- **Price snapshots** - Order items store immutable price/name at checkout time
- **Order state machine** - `pending → confirmed → preparing → ready → delivering → delivered`, `cancelled` from pending/confirmed only; invalid transitions => 409
- **Idempotency** - `Idempotency-Key` for orders, `event_id` for callbacks
- **Retry** - `POST /orders/{id}/retry` replays `integration_failed`
- **Restaurant portal** - CRUD restaurants/menu, `GET /restaurants/{id}/orders`
- **Realistic mock** - `MOCK_MODE=auto|manual|rejecting|failing`, in-memory orders, manual status triggers
- **Versioned migrations** - `schema_migrations` table, auto-run on startup, seed demo data

## Architecture

### C4 Container Diagram

```plantuml
@startuml
!include https://raw.githubusercontent.com/plantuml-stdlib/C4-PlantUML/master/C4_Container.puml
LAYOUT_WITH_LEGEND()

Person(user, "User", "Browses restaurants and creates food orders")
System_Boundary(kitchen, "KitchenFlow") {
  Container(web, "Web Client", "Browser", "Calls Kitchen API")
  Container(api, "Kitchen API", "Go", "REST API, business logic and orchestration")
  ContainerDb(db, "PostgreSQL", "PostgreSQL", "Restaurants, menus, carts and orders")
}
System(restaurant, "Restaurant Service", "Example integrated restaurant")

Rel(user, web, "Uses")
Rel(web, api, "HTTP/JSON")
Rel(api, db, "SQL")
Rel(api, restaurant, "HTTP/JSON")
Rel(restaurant, api, "Order status callbacks")
@enduml
```

### C4 Component Diagram

```plantuml
@startuml
!include https://raw.githubusercontent.com/plantuml-stdlib/C4-PlantUML/master/C4_Component.puml
LAYOUT_WITH_LEGEND()

Container_Boundary(api, "Kitchen API") {
  Component(http, "HTTP Handlers", "Go net/http", "Serialization, validation, transport")
  Component(restaurants, "Restaurant Service", "Go", "Restaurant business rules")
  Component(menu, "Menu Service", "Go", "Menu queries and availability")
  Component(cart, "Cart Service", "Go", "Cart operations and validation")
  Component(orders, "Order Service", "Go", "Checkout, state machine and idempotency")
  Component(repo, "Repository Implementations", "Go + pgx", "Persistence")
  Component(integration, "Restaurant Client", "Go", "External restaurant API adapter")
}

ContainerDb(db, "PostgreSQL", "PostgreSQL")
System(restaurant, "Restaurant Service", "External restaurant")

Rel(http, restaurants, "calls")
Rel(http, menu, "calls")
Rel(http, cart, "calls")
Rel(http, orders, "calls")
Rel(restaurants, repo, "uses")
Rel(menu, repo, "uses")
Rel(cart, repo, "uses")
Rel(orders, repo, "uses")
Rel(orders, integration, "uses")
Rel(repo, db, "SQL")
Rel(integration, restaurant, "HTTP")
@enduml
```

### Database ERD

```plantuml
@startuml
hide methods
hide stereotypes

entity restaurants {
  * id : uuid <<PK>>
  --
  * external_id : text <<UQ>>
  * name : text
  description : text
  * is_open : boolean
  * integration_base_url : text
}

entity menu_categories {
  * id : uuid <<PK>>
  --
  * restaurant_id : uuid <<FK>>
  * name : text
  position : int
}

entity menu_items {
  * id : uuid <<PK>>
  --
  * restaurant_id : uuid <<FK>>
  * category_id : uuid <<FK>>
  * external_id : text
  * name : text
  * price : bigint <<kopecks>>
  * is_available : boolean
}

entity carts {
  * id : uuid <<PK>>
  --
  * user_id : text <<UQ single cart>>
  * restaurant_id : uuid <<FK>>
}

entity cart_items {
  * id : uuid <<PK>>
  --
  * cart_id : uuid <<FK>>
  * menu_item_id : uuid <<FK>>
  * quantity : int
  * unit_price : bigint <<kopecks>>
  * total_price : bigint <<kopecks>>
}

entity orders {
  * id : uuid <<PK>>
  --
  * user_id : text
  * restaurant_id : uuid <<FK>>
  external_order_id : text
  * status : order_status
  * total_amount : bigint <<kopecks>>
  * delivery_address : text
  * idempotency_key : text
}

entity order_items {
  * id : uuid <<PK>>
  --
  * order_id : uuid <<FK>>
  * menu_item_id : uuid <<FK>>
  * name : text <<snapshot>>
  * unit_price : bigint <<snapshot, kopecks>>
  * quantity : int
  * total_price : bigint <<kopecks>>
}

entity order_status_history {
  * id : bigint <<PK>>
  --
  * order_id : uuid <<FK>>
  * status : order_status
  * source : text
  * created_at : timestamptz
}

restaurants ||--o{ menu_categories
menu_categories ||--o{ menu_items
restaurants ||--o{ menu_items
restaurants ||--o{ carts
carts ||--o{ cart_items
menu_items ||--o{ cart_items
restaurants ||--o{ orders
orders ||--o{ order_items
menu_items ||--o{ order_items
orders ||--o{ order_status_history
@enduml
```

### CJM - User Journey

```plantuml
@startuml
|User|
start
:Open KitchenFlow;
:View restaurants (filter open);
:Choose restaurant;
:View menu (available only);
:Add dishes to cart;
note right: single active cart; cross-restaurant => 409
:Open cart (update qty / remove / clear);
:Enter delivery address;
:Create order with Idempotency-Key;
if (Integration ok?) then (yes)
  :Track confirmed → preparing → ready → delivering → delivered;
  :Cancel from pending/confirmed if needed;
else (no)
  :See integration_failed;
  :Retry integration;
endif
:View status history;
stop
@enduml
```

### CJM - Restaurant Journey

```plantuml
@startuml
|Restaurant|
start
:Onboard via partner API;
:Manage menu and availability;
:Receive order from Kitchen API;
:Validate order data;
if (Can fulfill?) then (yes)
  :Accept (201 confirmed);
  :Send callbacks confirmed → preparing → ready → delivering → delivered;
else (no)
  :Return 201 cancelled + callback cancelled;
endif
:Monitor incoming orders via GET /restaurants/{id}/orders;
stop
@enduml
```

## Order Lifecycle

### State Machine

```
pending → confirmed → preparing → ready → delivering → delivered
    ↘ cancelled           ↘ cancelled
```

- **pending** - Order created, awaiting restaurant confirmation
- **confirmed** - Restaurant accepted the order
- **preparing** - Restaurant is cooking the order
- **ready** - Order is ready for delivery
- **delivering** - Order is out for delivery
- **delivered** - Order completed
- **cancelled** - Order cancelled (only from pending/confirmed)
- **integration_failed** - Restaurant API unavailable at creation time

### Status History

Every status change is recorded in `order_status_history` with:
- Order ID
- New status
- Source (`kitchen` or `restaurant`)
- Timestamp

## API Contract

See `api/openapi.yaml` for the complete OpenAPI 3.0 specification.

### Key Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/health` | Health check |
| GET | `/api/v1/restaurants?open=true` | List restaurants (filter open) |
| POST | `/api/v1/restaurants` | Onboard restaurant (partner) |
| GET | `/api/v1/restaurants/{id}` | Get restaurant |
| PATCH | `/api/v1/restaurants/{id}` | Update / open-close restaurant |
| GET | `/api/v1/restaurants/{id}/menu?available=true` | Get menu |
| POST | `/api/v1/restaurants/{id}/menu/categories` | Create category |
| POST | `/api/v1/restaurants/{id}/menu/items` | Create menu item (kopecks) |
| PATCH/DELETE | `/api/v1/menu-items/{id}` | Update / delete item |
| GET | `/api/v1/menu-items/{id}` | Get menu item |
| GET | `/api/v1/restaurants/{id}/orders` | Incoming orders for restaurant |
| GET | `/api/v1/cart` | Get user's cart (single active) |
| POST | `/api/v1/cart/items` | Add item to cart |
| PATCH | `/api/v1/cart/items/{id}` | Update quantity (1..50) |
| DELETE | `/api/v1/cart/items/{id}` | Remove item |
| DELETE | `/api/v1/cart` | Clear cart |
| POST | `/api/v1/orders` | Create order (requires `Idempotency-Key`) |
| GET | `/api/v1/orders?limit&offset` | List user's orders (paginated) |
| GET | `/api/v1/orders/{id}` | Get order details |
| GET | `/api/v1/orders/{id}/status` | Get order status |
| GET | `/api/v1/orders/{id}/history` | Status history |
| POST | `/api/v1/orders/{id}/cancel` | Cancel (pending/confirmed only) |
| POST | `/api/v1/orders/{id}/retry` | Retry `integration_failed` |
| POST | `/api/v1/integrations/restaurants/{id}/orders/status` | Restaurant status callback (`event_id` idempotent) |

## Restaurant Integration

### Integration Flow

1. Kitchen API creates the order in `pending`, persisting its correlation `external_order_id` (the order UUID) before any HTTP request.
2. Kitchen API sends `POST /api/v1/restaurant/orders` to restaurant service
3. Restaurant stores the order and responds `201 {external_order_id, status}`. It must echo the supplied ID; accepted initial statuses are `pending`, `confirmed`, and `cancelled`.
4. Kitchen API atomically consumes the original cart when persisting the order, before restaurant dispatch. The response status and history are then recorded atomically. A callback may arrive before the HTTP response; reconciliation never overwrites a status already advanced by a callback.
5. Restaurant sends status callbacks using the supplied `external_order_id` and unique `event_id`. If the HTTP response already confirmed the order, callbacks may continue with `preparing → ready → delivering → delivered`.
6. Kitchen validates transitions (`pending → confirmed → ...`, `cancelled` only early); duplicates => `204`, invalid => `409`

Timeouts and malformed responses leave a still-pending order in `integration_failed`.
If a callback has already advanced the order, its status is preserved instead.
Retry publishes the pending state and correlation ID before contacting the restaurant,
then reconciles the response under the same rules. Existing records without an
external ID receive one when retried. Recovery after process crashes and callbacks
arriving only after a failed attempt remain separate reliability work.

### Mock Modes (`MOCK_MODE`)

- `auto` (default) - accepts and auto-delivers in ~10s
- `manual` - accepts but waits for `POST /api/v1/restaurant/orders/{id}/status`
- `rejecting` - accepts as `cancelled` (tests reject path)
- `failing` - returns `503` (tests `integration_failed` + retry)
- Address containing `reject` also triggers the reject path

### Idempotency

- **Order Creation**: `Idempotency-Key` header ensures same key + user = same order
- **Restaurant Callbacks**: `event_id` in callback prevents duplicate processing

### Failure Handling

If restaurant integration fails at order creation:
- Order remains in `integration_failed` status
- User can see the order and its status
- Durable dispatch automatically retries unfinished sends with the original correlation ID

## Running Locally

### Prerequisites

- Docker and Docker Compose
- Python 3 (local credential/key issuers)
- Go 1.25.6+ (for local development without Docker)

### Using Docker Compose

On the first start, generate local credentials and the demo callback key before starting containers:

```bash
export TEST_USER_TOKEN="$(python3 scripts/issue-token.py --subject local-user --role user)"
python3 scripts/issue-callback-key.py --restaurant 550e8400-e29b-41d4-a716-446655440000 --key-id demo-1
docker compose up --build
```

This starts:
- **PostgreSQL** on port 5432
- **Kitchen API** on port 8080
- **Restaurant Mock** on port 8081

### Environment Variables

#### Kitchen API

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | required for native execution | PostgreSQL connection string; Compose supplies its container address |
| `DB_TIMEOUT` | `10s` | Database operation timeout |
| `SERVER_ADDR` | `:8080` | HTTP server address |
| `SHUTDOWN_TIMEOUT` | `30s` | Graceful shutdown timeout |
| `TIME_ZONE` | `UTC` | Application timezone |
| `LOGGER_FOLDER` | required for native execution | Log directory; Compose uses `/app/logs` |
| `LOGGER_LEVEL` | `DEBUG` (Compose: `INFO`) | Log level |
| `RESTAURANT_ALLOWED_ORIGINS` | empty (dispatch disabled) | Comma-separated trusted restaurant origins, including scheme and port |
| `RESTAURANT_ALLOWED_PRIVATE_CIDRS` | empty | Explicit private/loopback networks allowed for those origins; Compose demo uses `172.16.0.0/12` |
| `MIGRATIONS_DIR` | `/app/migrations` | Versioned migrations dir (auto-run) |

#### Restaurant Mock

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8081` | HTTP server port |
| `KITCHEN_CALLBACK_URL` | `http://kitchen-api:8080` | Kitchen API callback URL |
| `RESTAURANT_ID` | `550e8400-...` | Demo restaurant UUID |
| `MOCK_MODE` | `auto` | `auto/manual/rejecting/failing` |
| `MOCK_DELAYS` | `2s,2s,2s,2s,2s` | Auto-progression delays |

Orders and retries use the selected restaurant’s `integration_base_url`. There is no global URL override. Missing, malformed or unapproved URLs return HTTP 503 before creating an order or preparing a retry. Network/DNS failures during dispatch produce `integration_failed`.

The operator must approve each origin in `RESTAURANT_ALLOWED_ORIGINS`; restaurant API writes cannot extend that list. Use HTTPS for external integrations. Credentials, queries and fragments in base URLs are rejected. Redirects and environment proxies are disabled. DNS results are checked and the validated IP is dialed directly; private/loopback addresses additionally require explicit CIDRs. Link-local (including metadata services), multicast and unspecified addresses are always blocked. Keep private CIDRs narrow; adjust the Compose CIDR if your Docker network uses a different subnet.

### Local Development (without Docker)

```bash
# Start PostgreSQL
docker run -d --name kitchenflow-local-postgres \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=kitchen \
  -p 5432:5432 \
  postgres:16-alpine

# Do not apply individual SQL files manually; the API applies and records all migrations.

# Generate local credentials (store the first command's token privately)
python3 scripts/issue-token.py --subject local-user --role user
python3 scripts/issue-callback-key.py --restaurant 550e8400-e29b-41d4-a716-446655440000 --key-id demo-1
export AUTH_CREDENTIALS_FILE=./secrets/kitchenflow-auth.json
export CALLBACK_KEYS_FILE=./secrets/callback-keys.json
export CALLBACK_KEY_ID=demo-1
export MIGRATIONS_DIR=./migrations

# Set environment variables
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/kitchen?sslmode=disable"
export DB_TIMEOUT=10s
export SERVER_ADDR=:8080
export RESTAURANT_ALLOWED_ORIGINS=http://localhost:8081
export RESTAURANT_ALLOWED_PRIVATE_CIDRS=127.0.0.0/8,::1/128
export LOGGER_FOLDER=./logs

# Run Kitchen API
go run ./cmd/kitchen-api

# Run Restaurant Mock in a separate terminal; export CALLBACK_KEYS_FILE and CALLBACK_KEY_ID there too.
export PORT=8081
export KITCHEN_CALLBACK_URL=http://localhost:8080
go run ./cmd/restaurant-mock
```

After the API has started and seeded the demo restaurant, update its integration URL for local execution (separate terminal):

```bash
psql "$DATABASE_URL" -c "UPDATE restaurants SET integration_base_url='http://localhost:8081' WHERE id='550e8400-e29b-41d4-a716-446655440000';"
```

## Testing

```bash
# Unit tests
go test ./...

# E2E against a running stack (needs BASE_URL)
BASE_URL=http://localhost:8080 bash scripts/e2e.sh

# Manual demo (prices in kopecks: 54900 = 549.00 ₽)
RID=550e8400-e29b-41d4-a716-446655440000
ITEM=bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbb1
curl -s http://localhost:8080/api/v1/restaurants | head -c 200
curl -s -X POST http://localhost:8080/api/v1/cart/items \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TEST_USER_TOKEN" \
  -d "{\"restaurant_id\":\"$RID\",\"menu_item_id\":\"$ITEM\",\"quantity\":1}"
curl -s -X POST http://localhost:8080/api/v1/orders \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TEST_USER_TOKEN" -H 'Idempotency-Key: demo-1' \
  -d '{"delivery_address":"Moscow, Tverskaya 1"}'
```

### PostgreSQL callback regression tests

Set `KITCHENFLOW_TEST_DATABASE_URL` to a disposable PostgreSQL database and run:

```bash
KITCHENFLOW_TEST_DATABASE_URL='postgres://postgres:password@localhost:5432/kitchenflow_test?sslmode=disable' \
  go test -race ./internal/features/orders/repository ./internal/features/orders/service
```

The database tests create and drop a unique test schema and require schema/extension
creation privileges. Without this variable, PostgreSQL tests are explicitly skipped.
They cover callback replays after later transitions, concurrent duplicates and event ID collisions.

## Static Analysis

```bash
# Run linter
golangci-lint run

# Run go vet
go vet ./...

# Check formatting
gofmt -l .
```

## Configuration

The application uses environment variables for configuration. See the Environment Variables section above.

Key configuration packages:
- `internal/core/config` - Application config (DB, server, timezone)
- `internal/core/logger` - Logger config (level, output folder)
- `internal/core/database/pgx` - Database pool config (URL, timeout)

## Trade-offs and Known Limitations

### Synchronous Restaurant Integration (MVP)

**Decision**: Restaurant order creation is synchronous. If the restaurant service is unavailable, the order is created in `integration_failed` status.

**Rationale**: Simplicity for MVP. Avoids need for message queues, outbox pattern, and retry workers.

**Future Evolution**:
- Implement transactional outbox pattern
- Add async worker for restaurant communication
- Add retry logic with exponential backoff
- Implement circuit breaker for restaurant service

### Operator-issued authentication

**Decision**: Personal/API Bearer tokens verified against an operator-owned registry of hashes, roles, restaurant scopes and expiry. Customer identity comes from authenticated subject.

**Rationale**: Explicit provisioning and scoped permissions support this backend without a public signup service; credential rotation and revocation require registry update and API restart.

### No Redis/Caching

**Decision**: No Redis for caching or sessions.

**Rationale**: MVP scale doesn't require caching. PostgreSQL handles the load.

### Single Active Cart per User

**Decision**: One cart per user (`UNIQUE(user_id)`). Cross-restaurant add returns `409`.

**Rationale**: Matches real delivery UX, removes ambiguous "latest cart" reads, simplifies checkout and idempotency. Migration `004_single_cart_per_user.sql` deduplicates old data.

### Restaurant Mock

**Decision**: Stateful mock with `auto/manual/rejecting/failing` modes, order store, manual triggers.

**Rationale**: Lets sales/support demo happy path, rejections, outages and slow kitchens without external deps.

### Money Representation

**Decision**: All money is `BIGINT` minor units (kopecks) end-to-end (DB, Go `int64`, OpenAPI `integer`).

**Rationale**: Float `NUMERIC/float64` causes rounding errors and cannot be sold to business. Migration `003_money_minor_units.sql` converts legacy installs (`*100`); fresh installs start on `BIGINT`.

### Versioned Migrations

**Decision**: `internal/core/database/migrate` + `schema_migrations` table, auto-run on boot.

**Rationale**: `docker-entrypoint-initdb.d` only runs on first volume init and breaks upgrades. The migrator makes every `docker compose up` safe for fresh and existing volumes.

## AI Usage

AI tools were used during implementation for code generation, refactoring, testing and documentation. The architecture, technical decisions and final review were performed by the author.

The implementation plan used during development is available in `docs/ai-plan.md`.

## Project Structure

```
.
├── api/
│   └── openapi.yaml          # OpenAPI 3.0 specification
├── cmd/
│   ├── kitchen-api/          # Main API service entry point
│   └── restaurant-mock/      # Restaurant integration mock
├── docs/
│   ├── domain.md             # Domain model and business rules
│   ├── ai-plan.md            # AI implementation plan
│   ├── cjm-user.puml         # User customer journey map
│   ├── cjm-restaurant.puml   # Restaurant customer journey map
│   ├── c4-container.puml     # C4 container diagram
│   ├── c4-components.puml    # C4 component diagram
│   └── erd.puml              # Entity relationship diagram
├── internal/
│   ├── core/                 # Shared infrastructure
│   │   ├── config/           # Configuration
│   │   ├── logger/           # Logger abstraction (zap)
│   │   ├── errors/           # Domain errors
│   │   ├── database/         # Database abstraction (pgx) + migrate runner
│   │   └── http/             # HTTP infrastructure
│   └── features/             # Business features (vertical slices)
│       ├── restaurants/      # Partner portal: CRUD + open/close
│       ├── menu/             # Menu + category/item CRUD
│       ├── cart/             # Single active cart
│       ├── orders/           # Checkout, cancel, retry, history
│       └── integrations/restaurant/
├── migrations/
│   ├── 001_init.sql          # Schema (BIGINT kopecks, single cart)
│   ├── 002_seed.sql          # Demo restaurant + menu
│   ├── 003_money_minor_units.sql # NUMERIC -> BIGINT for upgrades
│   └── 004_single_cart_per_user.sql # Multi-cart -> single cart
├── scripts/
│   └── e2e.sh                # End-to-end smoke test
├── .golangci.yml             # Linter configuration
├── docker-compose.yml        # Docker Compose for local dev
├── Dockerfile.kitchen-api    # Kitchen API Dockerfile
├── Dockerfile.restaurant-mock # Restaurant Mock Dockerfile
├── go.mod
├── go.sum
└── README.md
```

## Project Background

KitchenFlow is a food delivery backend carried over from an earlier project under a neutral name.
Functional requirements are documented in `docs/requirements.md`.

### Order item integration IDs

Migration `005_order_item_external_id.sql` stores the partner product ID with each new order item. Checkout and retry use the same frozen ID, name and price even after menu edits. Legacy rows remain NULL because the historical ID cannot safely be inferred from the current menu. Their retry returns HTTP 409 before sending anything; an operator must reconcile every missing ID against the original partner order/catalog before retrying. Do not substitute internal UUIDs or blindly copy current menu IDs. Apply migrations before starting the updated API.

Checkout consumes the original cart in the same transaction as the order, items and initial history. If that transaction fails, the cart remains. A restaurant timeout leaves a durable `integration_failed` order to retry, rather than a second checkout from the same cart. New carts are never deleted by integration responses or retries. Legacy failed orders have no reliable cart association: retries leave their current cart untouched; users can explicitly clear any old duplicate contents. Concurrent retries are serialized by the pending-state transition and reuse the original correlation ID; partner-side deduplication is still required for ambiguous network outcomes.

### Concurrent checkout

Migration `006_cart_revision.sql` adds a cart revision advanced by a database trigger on every item insert, update or deletion. Checkout consumes the cart only if its revision still matches the snapshot read by the service. A stale checkout returns HTTP 409 and rolls back the order; reload the cart before trying again. Competing checkouts with different idempotency keys can commit at most one order for the original cart. Cart edits and checkout share the parent-row lock; writes targeting already consumed rows fail instead of reporting a successful edit. Purchases added after checkout commit use a new cart and are independent of the restaurant HTTP response. Cart mutations serialize on a user-scoped transaction lock and the parent cart row. Relative additions accumulate without lost quantities; absolute quantity updates take effect in transaction order.

### Restaurant cancellation contract

User cancellation calls the selected partner at `POST {integration_base_url}/api/v1/restaurant/orders/cancel` with `{"external_order_id":"..."}`. Partner success is HTTP 200 with the same ID and `status: cancelled`; repeated cancellation must return the same result. Cancellation and beginning preparation must be mutually exclusive at the partner: pending/confirmed may be cancelled, preparing or later returns 409. Kitchen records cancellation only on a valid confirmation or callback. A transport error, timeout or malformed reply returns 503 without claiming success; repeat cancellation to reconcile a lost response. An already cancelled local order returns success. No database lock is held during partner HTTP. The mock implements this contract and stops automatic progression after cancellation. Existing partner integrations must implement this endpoint before cancellation can succeed. Recovery after a process crash still requires a client retry (durable recovery is tracked separately).

Cart creation, restaurant switching for an empty cart, item changes and clearing each run in one transaction. The returned cart is read under the same lock before commit. Quantity remains in 1–50; an overflowing add rolls back with 400. A nonempty cart rejects a second restaurant with 409; missing or foreign item IDs return 404. Reads also take the cart lock so revision, items and total form a consistent snapshot. User locks are PostgreSQL transaction advisory locks shared across API processes; they do not require in-memory coordination.

Persistence failures abort the affected transaction and are returned to the caller. Order status/history/callback deduplication are written together; checkout also consumes its cart in that transaction. Empty-cart replacement and clear propagate deletion failures. Standalone order status and history writes are not exposed by the order repository. PostgreSQL regression tests inject history-insert and cart-delete failures and verify rollback and subsequent successful retries.

### Durable order dispatch

Migration 007 creates a PostgreSQL dispatch queue, inserted atomically with each new order and each explicit retry. The API still attempts synchronous dispatch; a background worker polls every second and recovers unfinished work after 30 seconds. Workers claim jobs with a 60-second lease and SKIP LOCKED, then retry with a capped delay (30 seconds per attempt, up to 10 minutes). Expired leases survive process restarts; release is fenced by a unique token. Success removes the job in the status/history transaction. Failed or ambiguous calls retain it. Late partner callbacks can reconcile integration_failed orders. Delivery is at least once: partners must atomically deduplicate external_order_id and reject mismatched payloads. The mock now implements creation replay without restarting cooking; its in-memory state still does not survive mock process restarts.

Recovery reuses the stored partner endpoint and immutable item IDs, prices and address. Existing pending/failed orders with complete snapshots are enqueued by migration 007; legacy orders lacking snapshots need manual reconciliation. Update operator-approved origins when partner endpoints change. This worker recovers order creation dispatch; cancellation after a process crash still requires an explicit client retry.

### Migration execution

Startup reads and validates all SQL migration files before touching the database. Missing paths, directories with no SQL files and empty SQL files are errors. Pending migrations execute in filename order in one transaction, together with their schema_migrations entries. A transaction advisory lock serializes parallel API startup before the ledger is created or inspected. SQL or ledger errors roll back the whole pending batch; previously committed migrations remain applied. Restart then retries the pending batch. Migration files must support transactions and must not contain their own BEGIN/COMMIT/ROLLBACK or commands such as CREATE INDEX CONCURRENTLY. Keep applied files immutable; the ledger uses their filenames.

### Database constraint responses

Repository writes translate duplicate keys to HTTP 409 and missing foreign-key references to HTTP 404. Deleting a referenced category or menu item returns 409. The same rules apply to constraints deferred until commit. Database causes remain available in internal errors for diagnostics; responses omit SQL and constraint details. Unexpected database failures continue to return 500. No schema migration is needed.

### Menu and restaurant input validation

Creating a menu item requires an explicit integer price (0 is allowed), category_id and nonblank name. Both creation and patch enforce price 0–1,000,000,000 kopecks, names up to 200 Unicode characters, descriptions up to 2000, external IDs up to 128 and integration URLs up to 512. Category names are limited to 120 characters and position must be nonnegative. Names must contain non-whitespace characters and are trimmed. Menu and restaurant requests must be JSON objects: explicit null values and unknown properties are rejected with 400. Omitted patch properties leave existing values unchanged. No database migration is required.

### Restaurant mock creation replays

The mock atomically creates or retrieves an order by the required `external_order_id` (nonblank, at most 128 bytes). The first request returns 201 and alone starts automatic processing or emits a rejection callback; a matching replay returns 200 with the current saved status. Address and ordered item snapshots must match exactly; a changed payload returns 409 without modifying timestamps, items or status. Matching replays still succeed when the mock is switched to failing mode after the order was accepted. Store reads and creation results are copies, so callers cannot overwrite stored state. Deduplication lasts for the lifetime of the mock process; its in-memory store is cleared on restart.

### Mock callback delivery and shutdown

The mock queues status events in memory in the order accepted under its store lock. One dispatcher retries the head event until HTTP 2xx; transport errors, redirects and other HTTP statuses are not acknowledgements. Each event retains its payload and event_id across retries. Requests have a 5-second timeout; retry delays grow from 100 ms to a 5-second cap. Later events wait for the earlier acknowledgement. A persistently rejected event therefore requires fixing the receiver before the queue can progress. The queue is intentionally in memory and is lost on mock restart.

Manual status commands return 400 for unknown statuses, 409 for invalid transitions and 404 for missing orders. Status change and enqueue are atomic. Server shutdown first drains HTTP handlers, then cancels and joins background processing and callback delivery, including active requests and sleeps. Store snapshots are copies.

### Authentication and partner access

Before starting the API, issue credentials locally (Python 3 required):

```bash
python3 scripts/issue-token.py --subject customer-1 --role user
python3 scripts/issue-token.py --subject partner-1 --role partner --restaurant 550e8400-e29b-41d4-a716-446655440000
python3 scripts/issue-token.py --subject operator-1 --role admin
export AUTH_CREDENTIALS_FILE=./secrets/kitchenflow-auth.json
```

Each command prints a new random token once. Keep it private and send `Authorization: Bearer <token>` over HTTPS. The local registry contains only SHA-256 hashes and expires_at; it is excluded from Git. The API refuses to start without a valid registry. Compose mounts the file read-only. Issue credentials before `docker compose up`.

This is an operator-issued personal/API credential model, without public signup or password login. Subject is the stable customer identity; X-User-ID is ignored. User tokens can access only their own cart/orders. Partner tokens manage only explicitly listed restaurant UUIDs, including ownership lookup for menu-item mutation and incoming orders. Only admins create restaurants; admins cannot impersonate customer accounts. Catalog GETs and health remain public. Callbacks require the dedicated HMAC keys described below.

Missing/invalid/expired tokens return 401; authenticated but disallowed roles or restaurants return 403. Rotate by issuing a replacement and removing the old hash; restart API instances to load revocations. Tokens default to 30 days (configurable up to 365). Protect the registry from untrusted writes. For E2E, set TEST_USER_TOKEN to a user credential.

### Signed restaurant callbacks

Issue the demo key before startup:

```bash
python3 scripts/issue-callback-key.py --restaurant 550e8400-e29b-41d4-a716-446655440000 --key-id demo-1
export CALLBACK_KEYS_FILE=./secrets/callback-keys.json
export CALLBACK_KEY_ID=demo-1 # mock sender only
```

The API requires CALLBACK_KEYS_FILE; the mock additionally requires CALLBACK_KEY_ID matching RESTAURANT_ID. The issuer writes a random 32-byte secret into an ignored local file with permissions 0600 and prints only its ID. Compose mounts that file read-only. In deployments with several partners, give each sender only its own key, not the API's complete key registry. Use HTTPS and protect these shared secrets from untrusted reads/writes.

Requests carry X-Callback-Key-ID, X-Callback-Timestamp (Unix seconds) and X-Callback-Signature (hex HMAC-SHA256). Sign UTF-8 `v1\n{timestamp}\nPOST\n{escaped URL path}\n{hex SHA256 of exact body bytes}` without a final newline. The key registry binds each key ID to exactly one restaurant UUID and an expiry. Signatures are compared in constant time. Requests older than five minutes or over one minute in the future, missing/invalid signatures, wrong restaurant and expired keys return 401; callback bodies over 64 KiB return 413. User/partner Bearer tokens do not replace the signature.

Retries reuse the event_id and payload but receive a fresh timestamp/signature each attempt. Valid replays within the window are safe through the existing atomic event_id deduplication. For rotation: add a new key ID, restart receivers accepting old and new keys, switch/restart the sender with the new ID, then remove the old key and restart receivers after in-flight requests have drained. No database migration is required.

### HTTP boundaries

Both API and restaurant mock enforce header read timeout 5s, total request read timeout 15s, response write timeout 30s, idle keep-alive timeout 60s and maximum headers 32 KiB. Request bodies are capped at 1 MiB (413), including chunked requests; callbacks keep their stricter 64 KiB cap. JSON input must be exactly one object: trailing documents or garbage, unknown fields and explicit null fields are rejected with 400. The integration client also bounds restaurant JSON responses to 64 KiB and rejects trailing data. HMAC verification uses the original escaped request path, including /api/v1, even when routing strips that prefix.

### Liveness, readiness and process exit

Public `GET /api/v1/health/live` returns 200 while the HTTP process can serve requests. `GET /api/v1/health/ready` returns 200 only when the database responds, all migrations required by the deployed files are recorded and runtime tables/versioned columns are queryable. Checks have a 2-second database deadline and return a generic 503 on failure or during shutdown; they do not scan application data or call restaurant services. `/api/v1/health` is a compatibility alias for readiness. Compose uses readiness for its API healthcheck. These checks do not replace full schema validation or restaurant delivery monitoring.

Binding the HTTP port happens synchronously. Startup/listener and shutdown errors propagate through the application; resources are closed before the outer main exits with code 1. Normal signal-driven shutdown exits successfully. No new migration is needed.

### Configuration and database deadlines

SERVER_ADDR and SHUTDOWN_TIMEOUT are the only API HTTP address/shutdown settings; legacy HTTP_SERVER_ADDR/HTTP_SHUTDOWN_TIMEOUT overrides are no longer used. Invalid addresses, nonpositive timeouts, unknown timezones, invalid mock modes and malformed/negative mock delays stop startup. DB_TIMEOUT defaults to 10s and applies to connection establishment/ping, each pool or transaction operation, and result consumption until Rows.Close or QueryRow.Scan. The caller deadline wins when shorter. It is an operation deadline, not an overall transaction deadline; slow migrations may require a larger value. Always close rows. Ping failure closes the newly created pool.

.env.example now describes native execution on localhost. Copy it to .env and explicitly load it for native runs (`set -a; source .env; set +a`); the binaries do not implicitly read .env. Compose supplies its own container hostnames and paths, using the host credential/key paths for read-only mounts. Do not source native .env values inside containers. Start with an empty database using the local instructions above; startup performs all migrations. After startup, update the demo restaurant URL to localhost as shown, since seed data uses the Compose hostname.

DB_TIMEOUT also caps background connection dial time; a shorter connect_timeout in the PostgreSQL URL is preserved. Docker build context excludes secrets/, environment files and the local issue list; credentials are runtime mounts only.
