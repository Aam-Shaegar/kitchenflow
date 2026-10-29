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

1. Kitchen API creates order locally in `pending` status (transaction + idempotency guard)
2. Kitchen API sends `POST /api/v1/restaurant/orders` to restaurant service
3. Restaurant responds `201 {external_order_id, status}` and stores the order
4. Kitchen API stores `external_order_id`, clears the cart
5. Restaurant sends `confirmed → preparing → ready → delivering → delivered` callbacks with unique `event_id`
6. Kitchen validates transitions (`pending → confirmed → ...`, `cancelled` only early); duplicates => `204`, invalid => `409`

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
- No automatic retry in MVP (documented as future improvement)

## Running Locally

### Prerequisites

- Docker and Docker Compose
- Go 1.25+ (for local development without Docker)

### Using Docker Compose

```bash
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
| `DATABASE_URL` | `postgres://postgres:postgres@postgres:5432/kitchen?sslmode=disable` | PostgreSQL connection string |
| `DB_TIMEOUT` | `10s` | Database operation timeout |
| `SERVER_ADDR` | `:8080` | HTTP server address |
| `SHUTDOWN_TIMEOUT` | `30s` | Graceful shutdown timeout |
| `TIME_ZONE` | `UTC` | Application timezone |
| `LOGGER_FOLDER` | `/app/logs` | Log file directory |
| `LOGGER_LEVEL` | `INFO` | Log level |
| `RESTAURANT_MOCK_URL` | `http://restaurant-mock:8081` | Restaurant service URL |
| `MIGRATIONS_DIR` | `/app/migrations` | Versioned migrations dir (auto-run) |

#### Restaurant Mock

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8081` | HTTP server port |
| `KITCHEN_CALLBACK_URL` | `http://kitchen-api:8080` | Kitchen API callback URL |
| `RESTAURANT_ID` | `550e8400-...` | Demo restaurant UUID |
| `MOCK_MODE` | `auto` | `auto/manual/rejecting/failing` |
| `MOCK_DELAYS` | `2s,2s,2s,2s,2s` | Auto-progression delays |

### Local Development (without Docker)

```bash
# Start PostgreSQL
docker run -d --name postgres \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=kitchen \
  -p 5432:5432 \
  postgres:16-alpine

# Run migrations
psql -h localhost -U postgres -d kitchen -f migrations/001_init.sql

# Set environment variables
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/kitchen?sslmode=disable"
export DB_TIMEOUT=10s
export SERVER_ADDR=:8080
export RESTAURANT_MOCK_URL=http://localhost:8081
export LOGGER_FOLDER=./logs

# Run Kitchen API
go run ./cmd/kitchen-api

# Run Restaurant Mock (separate terminal)
export PORT=8081
export KITCHEN_CALLBACK_URL=http://localhost:8080
go run ./cmd/restaurant-mock
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
  -H 'Content-Type: application/json' -H 'X-User-ID: demo' \
  -d "{\"restaurant_id\":\"$RID\",\"menu_item_id\":\"$ITEM\",\"quantity\":1}"
curl -s -X POST http://localhost:8080/api/v1/orders \
  -H 'Content-Type: application/json' -H 'X-User-ID: demo' -H 'Idempotency-Key: demo-1' \
  -d '{"delivery_address":"Moscow, Tverskaya 1"}'
```

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

### No Authentication

**Decision**: No JWT/OAuth authentication. User identity passed via `X-User-ID` header.

**Rationale**: The current MVP scope excludes authentication. Focus on core business logic.

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
