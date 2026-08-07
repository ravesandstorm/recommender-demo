# Recommendation System (Go + Nuxt)

Demo recommendation stack: Postgres + Redis + Qdrant + a Go `fastembed-go` embedding service, a Go REST API, and a Nuxt infinite-scroll UI.

## Quick start

```bash
# 1) Infrastructure + embedding service
docker compose up -d --build

# 2) API (applies migrations, ensures Qdrant collection `posts`)
cd backend && MIGRATIONS_DIR="$(pwd)/migrations" ADDR=:8090 go run ./cmd/server

# 3) Seed posts (title+content JSON → Postgres + embeddings + Qdrant)
cd seed && go run .

# 4) Frontend
cd frontend && npm install && npm run dev
```

Open http://localhost:3000 — create a user from the dropdown area, then scroll the feed.

> The API defaults to **:8090** to avoid clashing with other local services on 8080. Override with `ADDR=:8080` if needed.

## Ports

| Service | Port |
|---------|------|
| Go API | 8090 |
| Embedding (`fastembed-go`, BGESmallENV15) | 8081 |
| Postgres | 5432 |
| Redis | 6379 |
| Qdrant HTTP / gRPC | 6333 / 6334 |
| Nuxt | 3000 |

ONNX model cache for the embedding container lives in the `fastembed_cache` Docker volume (`CACHE_DIR=/models`).

## Tests

With compose services healthy:

```bash
cd backend && go test ./... -count=1
```

## Interaction vectors

User preference vectors in Redis (`user:{id}:vector`) update as:

- apply: `user += weight(type) * post_vector`
- undo/remove: `user -= weight(type) * post_vector`

`dislike` uses a negative weight (negative reinforcement). Removing an interaction only subtracts the previously applied delta.
