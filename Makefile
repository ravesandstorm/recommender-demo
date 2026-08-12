.PHONY: up down api seed frontend test flush flush-prefs

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
	cd frontend && pnpm install && npm run dev

flush: ## Flush entire Redis DB
	docker exec $$(docker ps -q --filter "publish=6380") redis-cli FLUSHALL

flush-prefs: ## Wipe user preference vectors + multi-interest profiles
	docker exec $$(docker ps -q --filter "publish=6380") sh -c '\
		for pat in "user:*:vector" "user:*:interests"; do \
			redis-cli --scan --pattern "$$pat" | while read -r k; do \
				[ -n "$$k" ] && redis-cli DEL "$$k" >/dev/null; \
			done; \
		done; echo wiped'

test: ## Run backend tests (requires compose services healthy)
	cd backend && go test ./... -count=1 -timeout 180s
