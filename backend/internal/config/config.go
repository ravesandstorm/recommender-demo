package config

import (
	"os"
	"strconv"
)

type Config struct {
	Addr            string
	DatabaseURL     string
	RedisAddr       string
	QdrantURL       string
	EmbeddingURL    string
	QdrantCollection string
	VectorDim       int
	FeedLimit       int
	QdrantOverfetch int
}

func Load() Config {
	return Config{
		Addr:             envOr("ADDR", ":8090"),
		DatabaseURL:      envOr("DATABASE_URL", "postgres://recsys:recsys@localhost:5432/recsys?sslmode=disable"),
		RedisAddr:        envOr("REDIS_ADDR", "localhost:6379"),
		QdrantURL:        envOr("QDRANT_URL", "http://localhost:6333"),
		EmbeddingURL:     envOr("EMBEDDING_URL", "http://localhost:8081"),
		QdrantCollection: envOr("QDRANT_COLLECTION", "posts"),
		VectorDim:        envInt("VECTOR_DIM", 384),
		FeedLimit:        envInt("FEED_LIMIT", 5),
		QdrantOverfetch:  envInt("QDRANT_OVERFETCH", 50),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return fallback
}
