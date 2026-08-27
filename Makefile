.PHONY: up down api seed frontend test test-unit test-edge test-integration test-stress test-stress-light test-stress-heavy test-heavy test-go flush flush-prefs

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

flush-prefs: ## Wipe user preference vectors, interests, and seen sets
	docker exec $$(docker ps -q --filter "publish=6380") sh -c '\
		for pat in "user:*:vector" "user:*:interests" "user:*:seen"; do \
			redis-cli --scan --pattern "$$pat" | while read -r k; do \
				[ -n "$$k" ] && redis-cli DEL "$$k" >/dev/null; \
			done; \
		done; echo wiped'

test: ## Run test suite (unit, integration, edge cases & light stress test)
	cd backend && go run ./cmd/testrunner -scope=all

test-unit: ## Run unit tests with metrics
	cd backend && go run ./cmd/testrunner -scope=unit

test-edge: ## Run edge case tests with metrics
	cd backend && go run ./cmd/testrunner -scope=edge-case

test-integration: ## Run end-to-end integration tests with metrics
	cd backend && go run ./cmd/testrunner -scope=integration

test-stress: ## Run all stress tests (light + heavy load)
	cd backend && go run ./cmd/testrunner -scope=stress

test-light: ## Run light stress simulation (10 concurrent users fetching 1 feed + interacting)
	cd backend && go run ./cmd/testrunner -scope=stress-light

test-heavy: ## Run heavy stress saturation test (isolated 200 concurrent users fetching 1 feed)
	cd backend && go run ./cmd/testrunner -scope=stress-heavy

test-go: ## Run standard native Go tests
	cd backend && go test ./... -count=1 -timeout 180s
