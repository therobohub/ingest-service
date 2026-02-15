.PHONY: help build test run docker-up docker-down clean

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the service binary
	go build -o bin/robohub-ingest ./cmd/robohub-ingest

test: ## Run all tests
	go test -v ./...

test-coverage: ## Run tests with coverage
	go test -cover ./...

test-integration: ## Run integration tests (requires DATABASE_URL)
	@if [ -z "$$DATABASE_URL" ]; then \
		echo "Setting up test database with docker..."; \
		docker run -d --name robohub-test-db -e POSTGRES_DB=test_db -e POSTGRES_USER=test -e POSTGRES_PASSWORD=test -p 5433:5432 postgres:15-alpine; \
		sleep 3; \
		export DATABASE_URL="postgres://test:test@localhost:5433/test_db?sslmode=disable"; \
		go test -v ./internal/db; \
		docker rm -f robohub-test-db; \
	else \
		go test -v ./internal/db; \
	fi

run: ## Run the service locally
	go run cmd/robohub-ingest/main.go

docker-build: ## Build Docker image
	docker build -t robohub-ingest:latest .

docker-up: ## Start all services with docker-compose
	docker-compose up --build

docker-down: ## Stop all services
	docker-compose down

docker-logs: ## View service logs
	docker-compose logs -f ingest-service

clean: ## Clean build artifacts
	rm -rf bin/
	go clean

fmt: ## Format code
	go fmt ./...

lint: ## Run linter (requires golangci-lint)
	golangci-lint run

.DEFAULT_GOAL := help
