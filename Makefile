.PHONY: test lint build run migrate e2e

test:
	go test ./...

lint:
	golangci-lint run

build:
	go build ./...

run:
	docker compose up --build

run-detached:
	docker compose up --build -d

stop:
	docker compose down

logs:
	docker compose logs -f

migrate:
	@echo "Migrations run automatically on kitchen-api startup (MIGRATIONS_DIR)"

e2e:
	bash scripts/e2e.sh
