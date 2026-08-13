package redisstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/recsys/backend/internal/config"
	"github.com/recsys/backend/internal/db"
	"github.com/recsys/backend/internal/redisstore"
	"github.com/stretchr/testify/require"
)

func TestSeenFilterMarkAndHydrate(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()
	rdb := redisstore.New(cfg.RedisAddr, cfg.VectorDim)
	if err := rdb.Ping(ctx); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}
	store, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer store.Pool.Close()

	user, err := store.CreateUser(ctx, "seen_"+uuid.New().String()[:8])
	require.NoError(t, err)
	userID := user.ID.String()
	ttl := time.Hour

	a := uuid.New().String()
	b := uuid.New().String()
	c := uuid.New().String()

	require.NoError(t, rdb.DeleteSeenKey(ctx, userID))
	require.NoError(t, rdb.EnsureSeen(ctx, userID, ttl, func(context.Context) ([]string, error) {
		return nil, nil
	}))

	unseen, err := rdb.FilterUnseen(ctx, userID, []string{a, b, c})
	require.NoError(t, err)
	require.Equal(t, []string{a, b, c}, unseen)

	require.NoError(t, rdb.MarkSeen(ctx, userID, []string{b}, ttl))
	unseen, err = rdb.FilterUnseen(ctx, userID, []string{a, b, c})
	require.NoError(t, err)
	require.Equal(t, []string{a, c}, unseen)

	// Durable PG row + delete Redis key → hydrate restores filter.
	post, err := store.InsertPost(ctx, "Seen", "hydrate "+uuid.New().String())
	require.NoError(t, err)
	require.NoError(t, store.MarkViewed(ctx, user.ID, []uuid.UUID{post.ID}, cfg.ViewRetainLimit))
	require.NoError(t, rdb.DeleteSeenKey(ctx, userID))

	require.NoError(t, rdb.EnsureSeen(ctx, userID, ttl, func(ctx context.Context) ([]string, error) {
		ids, err := store.ListRecentViewedPostIDs(ctx, user.ID, cfg.SeenHydrateLimit)
		if err != nil {
			return nil, err
		}
		out := make([]string, len(ids))
		for i, id := range ids {
			out[i] = id.String()
		}
		return out, nil
	}))

	unseen, err = rdb.FilterUnseen(ctx, userID, []string{post.ID.String(), a})
	require.NoError(t, err)
	require.Equal(t, []string{a}, unseen)
}
