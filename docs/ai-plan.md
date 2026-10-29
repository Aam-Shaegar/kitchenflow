# AI Agent Implementation Plan

You are implementing the KitchenFlow food delivery MVP in Go.

## Hard constraints

- Keep two runnable services in one repository: `kitchen-api` and `restaurant-mock`.
- Preserve a feature-oriented architecture. Shared technical infrastructure belongs in `internal/core`; business capabilities belong in `internal/features`.
- A feature must not import another feature package directly. Cross-feature collaboration uses interfaces/contracts and is wired in Composition Root.
- Services contain business logic and must not depend on HTTP transport or concrete infrastructure libraries.
- Repositories own SQL/persistence concerns.
- External restaurant communication is hidden behind an integration interface/adapter.
- Context must propagate through repository/service/integration calls.
- Implement order status transition validation as a domain/service concern.
- Implement idempotent order creation using `Idempotency-Key`.
- Restaurant status callbacks must be idempotent.
- Use PostgreSQL and pgx. Keep the driver isolated behind repository interfaces/your existing pgx abstraction.
- Keep logger usage behind the project's logger abstraction; do not leak zap throughout business code.

## Functional scope

Implement the scenarios and contracts from `docs/domain.md` and `api/openapi.yaml`.

## Suggested package boundaries

```
internal/
  core/
    config/
    logger/
    errors/
    database/
    http/
  features/
    restaurants/
      domain/
      repository/
      service/
      transport/http/
    menu/
      domain/
      repository/
      service/
      transport/http/
    cart/
      domain/
      repository/
      service/
      transport/http/
    orders/
      domain/
      repository/
      service/
      transport/http/
    integrations/
      restaurant/
        client.go
        http.go
```

Adapt names to existing repository conventions rather than adding unnecessary wrappers.

## Implementation order

1. Project bootstrap and Composition Root.
2. Migrations and database models.
3. Repository interfaces and PostgreSQL implementations.
4. Restaurant/menu services and endpoints.
5. Cart service and endpoints.
6. Order domain/state machine.
7. Order repository/service with transaction support.
8. Restaurant client and mock service.
9. Callback endpoint with idempotent handling.
10. OpenAPI alignment.
11. Tests for service rules and repository integration.
12. Docker Compose, lint, README.

## Important implementation decisions

- Do not introduce Kafka/Redis/Kubernetes just to demonstrate architecture.
- Do not implement authentication.
- Do not make `external_id` the internal primary key.
- Preserve historical order item name/price snapshots.
- Do not calculate final order totals from mutable menu rows after order creation.
- Do not perform partial DB writes when creating an order; use a transaction.
- For external integration failure, preserve the local order and represent the integration failure explicitly.
- Keep the MVP synchronous. Document transactional outbox + async retries as a production evolution, not as required MVP infrastructure.
