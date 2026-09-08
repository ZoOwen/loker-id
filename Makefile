-include .env
export

MIGRATIONS_DIR := migrations

.PHONY: run build test vet tidy tools \
	migrate-up migrate-down migrate-status migrate-create sqlc

run: ## Run the API server
	go run ./cmd/server

build: ## Build the API server binary
	go build -o bin/server ./cmd/server

test: ## Run tests
	go test ./...

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
