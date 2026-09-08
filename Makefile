-include .env
export

MIGRATIONS_DIR := migrations

.PHONY: run build test vet tidy tools \
	migrate-up migrate-down migrate-status migrate-create sqlc \
	test-db-setup

run: ## Run the API server
	go run ./cmd/server

build: ## Build the API server binary
	go build -o bin/server ./cmd/server

test: ## Run tests
	go test ./...

# test-db-setup: (Re)applies migrations/001_init.sql to TEST_DATABASE_URL
# for manual inspection (psql, etc). internal/store's own tests do NOT
# need this - each test applies the same file itself into a fresh,
# isolated schema it drops when done, so TEST_DATABASE_URL's public
# schema staying empty between test runs is expected, not broken. This
# target is only for looking at a populated copy directly. Safe to
# re-run: drops and recreates public first, since 001_init.sql has no IF
# NOT EXISTS guards of its own. Requires TEST_DATABASE_URL to be set
# (.env or the environment) - psql will fail with its own clear error if
# it isn't.
test-db-setup: ## Apply migrations/001_init.sql to TEST_DATABASE_URL
	psql "$(TEST_DATABASE_URL)" -v ON_ERROR_STOP=1 -c "DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;"
	psql "$(TEST_DATABASE_URL)" -v ON_ERROR_STOP=1 -f $(MIGRATIONS_DIR)/001_init.sql

vet: ## Run go vet
	go vet ./...

tidy: ## Tidy go.mod/go.sum
	go mod tidy

tools: ## Install sqlc and goose CLIs into $(go env GOPATH)/bin
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	go install github.com/pressly/goose/v3/cmd/goose@latest

migrate-up: ## Apply all pending migrations
	goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" up

migrate-down: ## Roll back the last migration
	goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" down

migrate-status: ## Show migration status
	goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" status

migrate-create: ## Create a new migration: make migrate-create name=add_foo
	goose -dir $(MIGRATIONS_DIR) create $(name) sql

sqlc: ## Generate Go code from db/queries/*.sql
	sqlc generate
