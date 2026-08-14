package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr                  string
	DatabaseURL           string
	RedisAddr             string
	QdrantURL             string
	EmbeddingURL          string
	QdrantCollection      string
	VectorDim             int
	FeedLimit             int
	QdrantOverfetch       int
	InterestK             int
	InterestSimThreshold  float64
	MMRLambda             float64
	MMRCandidateCap       int
	InterestCollinearMin  float64
	SeenTTL               time.Duration
	SeenHydrateLimit      int
	ViewRetainLimit       int
	ViewWriteWorkers      int
	ViewWriteQueueSize    int
	ShareMemoTTL          time.Duration
	RecentCacheTTL        time.Duration
	RecentCacheSize       int
	InteractionWriteWorkers   int
	InteractionWriteQueueSize int
}

func Load() Config {
	viewRetain := envInt("VIEW_RETAIN_LIMIT", 1000)
	hydrate := envInt("SEEN_HYDRATE_LIMIT", 50)
	if hydrate > viewRetain {
		hydrate = viewRetain
	}
	return Config{
		Addr:                      envOr("ADDR", ":8090"),
		DatabaseURL:               envOr("DATABASE_URL", "postgres://recsys:recsys@localhost:5432/recsys?sslmode=disable"),
		RedisAddr:                 envOr("REDIS_ADDR", "localhost:6380"),
		QdrantURL:                 envOr("QDRANT_URL", "http://localhost:6334"),
		EmbeddingURL:              envOr("EMBEDDING_URL", "http://localhost:8082"),
		QdrantCollection:          envOr("QDRANT_COLLECTION", "posts"),
		VectorDim:                 envInt("VECTOR_DIM", 384),
		FeedLimit:                 envInt("FEED_LIMIT", 5),
		QdrantOverfetch:           envInt("QDRANT_OVERFETCH", 50),
		InterestK:                 envInt("INTEREST_K", 5),
		InterestSimThreshold:      envFloat("INTEREST_SIM_THRESHOLD", 0.55),
		MMRLambda:                 envFloat("MMR_LAMBDA", 0.7),
		MMRCandidateCap:           envInt("MMR_CANDIDATE_CAP", 30),
		InterestCollinearMin:      envFloat("INTEREST_COLLINEAR_MIN", 0.9),
		SeenTTL:                   envDuration("SEEN_TTL", 3*24*time.Hour),
		SeenHydrateLimit:          hydrate,
		ViewRetainLimit:           viewRetain,
		ViewWriteWorkers:          envInt("VIEW_WRITE_WORKERS", 8),
		ViewWriteQueueSize:        envInt("VIEW_WRITE_QUEUE_SIZE", 4096),
		ShareMemoTTL:              envDuration("SHARE_MEMO_TTL", 24*time.Hour),
		RecentCacheTTL:            envDuration("RECENT_CACHE_TTL", 30*time.Second),
		RecentCacheSize:           envInt("RECENT_CACHE_SIZE", 100),
		InteractionWriteWorkers:   envInt("INTERACTION_WRITE_WORKERS", 8),
		InteractionWriteQueueSize: envInt("INTERACTION_WRITE_QUEUE_SIZE", 4096),
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
