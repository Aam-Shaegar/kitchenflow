# KitchenFlow

KitchenFlow is a Go backend for food delivery. Customers browse a restaurant's menu, build a cart, place an order and follow its status. Restaurant staff manage their menu and incoming orders. The repository includes the Kitchen API, a restaurant simulator and PostgreSQL migrations.

## Run the demo

Use Docker with Compose v2 or later, Python 3 and curl. From the repository root, run these commands in Bash. The demo uses ports 5432, 8080 and 8081. Start with an empty demo database and keep the same shell for the order example.

```bash
RID=550e8400-e29b-41d4-a716-446655440000
ITEM=bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbb1
TOKEN=$(python3 scripts/issue-token.py --subject demo-user --role user)
python3 scripts/issue-callback-key.py --restaurant "$RID" --key-id demo-1
docker compose up -d --build --wait
curl -fsS http://localhost:8080/api/v1/health/ready
```

Issue the key once for a fresh setup. For subsequent starts reuse the existing key registry. The API loads both registries at startup. Compose mounts them read-only. PostgreSQL keeps data in a named volume across `docker compose down` and subsequent starts.

```bash
API=http://localhost:8080/api/v1
curl -fsS "$API/restaurants/$RID/menu?available=true"
curl -fsS -X POST "$API/cart/items" -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"restaurant_id\":\"$RID\",\"menu_item_id\":\"$ITEM\",\"quantity\":2}"
ORDER=$(curl -fsS -X POST "$API/orders" -H "Authorization: Bearer $TOKEN" \
  -H "Idempotency-Key: demo-$(date +%s)" -H 'Content-Type: application/json' \
  -d '{"delivery_address":"Moscow, Tverskaya 1"}' |
  python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')
curl -fsS -H "Authorization: Bearer $TOKEN" "$API/orders/$ORDER/history"
```

Two demo burgers cost 109800 kopecks. In automatic mode the mock sends signed status updates. Read the history again after about ten seconds to see delivery. For a retry of the same checkout, reuse its original idempotency key. Stop the demo with `docker compose down`.

### Run the Go processes on the host

Use Go 1.25.6, Bash and a fresh PostgreSQL 16 database. The example below uses Compose only for PostgreSQL. Generate the credentials above and stop any Compose API and mock first. Start each Go process from the repository root in a separate shell.

```bash
docker compose up -d --wait postgres
set -a; source .env.example; set +a
go run ./cmd/kitchen-api
```

```bash
set -a; source .env.example; set +a
go run ./cmd/restaurant-mock
```

The binaries read environment variables at startup. The sample uses loopback addresses and the default credential paths. Compose provides its own service hostnames. The seed points to the Compose mock. For a native run, update the endpoint in this demo database once, after API readiness. Restore `http://restaurant-mock:8081` if you later reuse this database with both services in Compose.

```bash
docker compose exec -T postgres psql -U postgres -d kitchen -v ON_ERROR_STOP=1 \
  -c "UPDATE restaurants SET integration_base_url='http://localhost:8081' WHERE id='550e8400-e29b-41d4-a716-446655440000';"
```

### Runtime settings

The sample environment lists the host settings. Compose declares its container settings separately. [Host configuration](.env.example) · [Compose configuration](docker-compose.yml)

| Setting | Effect |
| --- | --- |
| `DATABASE_URL`, `MIGRATIONS_DIR` | PostgreSQL connection and directory of SQL files applied on API startup. Keep applied migrations immutable. |
| `DB_TIMEOUT` | Database operation deadline, default 10 seconds. A shorter caller deadline applies. |
| `SERVER_ADDR`, `SHUTDOWN_TIMEOUT` | API listening address and time allowed to finish HTTP shutdown. |
| `AUTH_CREDENTIALS_FILE`, `CALLBACK_KEYS_FILE` | API credential and callback key registries. The mock reads the callback registry too. |
| `PORT`, `KITCHEN_CALLBACK_URL` | Mock listening port and base URL used to reach Kitchen. |
| `MOCK_MODE`, `MOCK_DELAYS` | Mock behavior and delays between automatic updates. In manual mode, post a JSON `status` to the mock at `/api/v1/restaurant/orders/{external_order_id}/status`. |

## Access and integration

Catalog reads and health checks are public. A user token grants access to its subject's cart and orders. Only admins onboard restaurants. Partners can manage menu items, update restaurants and view orders for the restaurant IDs in their credential. Admins can manage all restaurants. There is no customer signup or password login.

Issue a partner credential with `python3 scripts/issue-token.py --role partner --subject partner-1 --restaurant "$RID"`. Use `--role admin` to issue an operator credential. Tokens are printed once and stored as hashes. Restart API instances after changing their registry. Protect bearer tokens in transit with HTTPS. Keep the demo mock on a trusted network. Its restaurant and manual status routes are unauthenticated.

Each restaurant has its own integration base URL. The operator sets allowed origins and private network ranges with `RESTAURANT_ALLOWED_ORIGINS` and `RESTAURANT_ALLOWED_PRIVATE_CIDRS`. Restaurant writes cannot extend those lists. Keep ranges narrow and configure them for the actual network. The API rechecks destination IPs on connection and disables redirects. It sends creation to `POST /api/v1/restaurant/orders` and cancellation to `POST /api/v1/restaurant/orders/cancel` at that base URL. Partners must deduplicate the stable external order ID even after an ambiguous network failure.

### Signed restaurant callbacks

The receiver requires `CALLBACK_KEYS_FILE`. The mock also requires `CALLBACK_KEY_ID` for its `RESTAURANT_ID`. Give each sender only its own secret. Send `X-Callback-Key-ID`, `X-Callback-Timestamp` (Unix seconds) and `X-Callback-Signature` (hex HMAC-SHA256). Sign these UTF-8 fields joined with a newline, without a trailing newline:

```text
v1
{timestamp}
POST
{escaped request path including /api/v1}
{lowercase hex SHA256 of the exact body bytes}
```

The receiver checks key expiry, restaurant scope and timestamp (up to five minutes old or one minute ahead). Retry with the same event ID and payload and a fresh signature. To rotate a key, load both IDs on receivers, switch the sender, then retire the old ID after outstanding requests drain. Restart the corresponding processes when their registries change.

## Verification

- `make build`, `go vet ./...` and `make lint` check the build and Go source. Use golangci-lint v2.7.2.
- `go test ./...` runs the default suite. Database tests skip when their connection URL is unset.
- Set `KITCHENFLOW_TEST_DATABASE_URL` to a disposable PostgreSQL database and run `bash scripts/test-integration.sh` for the suite with race detection. Startup tests apply migrations to that database.
- `make e2e` builds both services and starts isolated PostgreSQL with temporary credentials. It verifies checkout, delivery via signed callbacks, rejection, retry and cancellation. Requires Go, Docker and Python 3.

GitHub Actions runs these build, analysis, database and process checks on pushes and pull requests. The API contract describes routes, request fields and responses. The architecture notes explain transaction boundaries, recovery and operational limits. [OpenAPI](api/openapi.yaml) · [Architecture](docs/ARCHITECTURE.md)
