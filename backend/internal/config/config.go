package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr                 string
	DatabaseURL          string
	RedisAddr            string
	QdrantURL            string
	EmbeddingURL         string
	QdrantCollection     string
	VectorDim            int
	FeedLimit            int
	QdrantOverfetch      int
	InterestK            int
	InterestSimThreshold float64
	MMRLambda            float64
	SeenTTL              time.Duration
	SeenHydrateLimit     int
	ViewWriteWorkers     int
	ViewWriteQueueSize   int
}

func Load() Config {
	return Config{
		Addr:                 envOr("ADDR", ":8090"),
		DatabaseURL:          envOr("DATABASE_URL", "postgres://recsys:recsys@localhost:5432/recsys?sslmode=disable"),
		RedisAddr:            envOr("REDIS_ADDR", "localhost:6380"),
		QdrantURL:            envOr("QDRANT_URL", "http://localhost:6334"),
		EmbeddingURL:         envOr("EMBEDDING_URL", "http://localhost:8082"),
		QdrantCollection:     envOr("QDRANT_COLLECTION", "posts"),
		VectorDim:            envInt("VECTOR_DIM", 384),
		FeedLimit:            envInt("FEED_LIMIT", 5),
		QdrantOverfetch:      envInt("QDRANT_OVERFETCH", 50),
		InterestK:            envInt("INTEREST_K", 5),
		InterestSimThreshold: envFloat("INTEREST_SIM_THRESHOLD", 0.55),
		MMRLambda:            envFloat("MMR_LAMBDA", 0.7),
		SeenTTL:              envDuration("SEEN_TTL", 3*24*time.Hour),
		SeenHydrateLimit:     envInt("SEEN_HYDRATE_LIMIT", 50),
		ViewWriteWorkers:     envInt("VIEW_WRITE_WORKERS", 8),
		ViewWriteQueueSize:   envInt("VIEW_WRITE_QUEUE_SIZE", 4096),
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

func envFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return n
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return fallback
}
