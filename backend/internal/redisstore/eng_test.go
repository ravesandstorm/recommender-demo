package redisstore_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/recsys/backend/internal/config"
	"github.com/recsys/backend/internal/redisstore"
	"github.com/stretchr/testify/require"
)

func TestEngagementAndDeleteUserKeys(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()
	rdb := redisstore.New(cfg.RedisAddr, cfg.VectorDim)
	if err := rdb.Ping(ctx); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}

	uid := uuid.New().String()
	pid := uuid.New().String()
	t.Cleanup(func() { _, _ = rdb.DeleteUserKeys(context.Background(), uid) })

	require.NoError(t, rdb.SetEngLike(ctx, uid, pid, true))
	isLike, exists, err := rdb.GetEngLike(ctx, uid, pid)
	require.NoError(t, err)
	require.True(t, exists)
	require.True(t, isLike)

	require.NoError(t, rdb.ClearEngLike(ctx, uid, pid))
	_, exists, err = rdb.GetEngLike(ctx, uid, pid)
	require.NoError(t, err)
	require.False(t, exists)
	val, found, err := rdb.GetEngLikeField(ctx, uid, pid)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "x", val)

	require.NoError(t, rdb.SetEngSave(ctx, uid, pid))
	require.NoError(t, rdb.SetEngShare(ctx, uid, pid))
	require.NoError(t, rdb.SetShareMemo(ctx, uid, pid, 0))
	require.NoError(t, rdb.SetUserVector(ctx, uid, make([]float32, cfg.VectorDim)))

	n, err := rdb.DeleteUserKeys(ctx, uid)
	require.NoError(t, err)
	require.Greater(t, n, 0)

	_, exists, err = rdb.GetEngLike(ctx, uid, pid)
	require.NoError(t, err)
	require.False(t, exists)
	ok, err := rdb.HasShareMemo(ctx, uid, pid)
	require.NoError(t, err)
	require.False(t, ok)
}
