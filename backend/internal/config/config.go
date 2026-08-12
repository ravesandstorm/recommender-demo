package config

import (
	"os"
	"strconv"
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
