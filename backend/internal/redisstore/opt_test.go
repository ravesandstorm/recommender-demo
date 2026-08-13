package redisstore_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/recsys/backend/internal/config"
	"github.com/recsys/backend/internal/redisstore"
	"github.com/stretchr/testify/require"
)

func TestShareMemo(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()
	rdb := redisstore.New(cfg.RedisAddr, cfg.VectorDim)
	if err := rdb.Ping(ctx); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}

	userID := uuid.New().String()
	postID := uuid.New().String()
	ttl := time.Hour

	hit, err := rdb.HasShareMemo(ctx, userID, postID)
	require.NoError(t, err)
	require.False(t, hit)

	require.NoError(t, rdb.SetShareMemo(ctx, userID, postID, ttl))
	hit, err = rdb.HasShareMemo(ctx, userID, postID)
	require.NoError(t, err)
	require.True(t, hit)
}

func TestRecentCache(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()
	rdb := redisstore.New(cfg.RedisAddr, cfg.VectorDim)
	if err := rdb.Ping(ctx); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}

	require.NoError(t, rdb.DeleteRecentKey(ctx))
	a, b, c := uuid.New().String(), uuid.New().String(), uuid.New().String()
	require.NoError(t, rdb.SetRecentIDs(ctx, []string{a, b, c}, 10, time.Hour))

	ids, err := rdb.GetRecentIDs(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{a, b, c}, ids)

	d := uuid.New().String()
	require.NoError(t, rdb.PrependRecentID(ctx, d, 3, time.Hour))
	ids, err = rdb.GetRecentIDs(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{d, a, b}, ids)
}
