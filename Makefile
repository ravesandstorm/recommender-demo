.PHONY: up down api seed frontend test

help:
	@echo "Targets:"
	@grep -E '^[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*## "}; {printf "  make %-20s - %s\n", $$1, $$2}'

up: ## Start infrastructure + embedding service (Docker Compose)
	docker compose up -d --build

down: ## Stop and remove Docker Compose services
	docker compose down

api: ## Run the Go API (applies migrations, ensures Qdrant collection)
	cd backend && MIGRATIONS_DIR=$$(pwd)/migrations ADDR=:8090 go run ./cmd/server

seed: ## Seed posts (JSON → Postgres + embeddings + Qdrant)
	cd seed && go run .

frontend: ## Install deps and start the Nuxt dev server
	cd frontend && npm install && npm run dev

test: ## Run backend tests (requires compose services healthy)
	cd backend && go test ./... -count=1 -timeout 180s
