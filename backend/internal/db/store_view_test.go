package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/recsys/backend/internal/config"
	"github.com/recsys/backend/internal/db"
	"github.com/recsys/backend/internal/migrate"
	"github.com/stretchr/testify/require"
)

func TestMarkViewedRetainLimitAndViewCount(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()
	store, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer store.Pool.Close()
	require.NoError(t, migrate.Up(ctx, store.Pool))

	user, err := store.CreateUser(ctx, "retain_"+uuid.New().String()[:8])
	require.NoError(t, err)

	const retain = 3
	var posts []db.Post
	for i := 0; i < 5; i++ {
		p, err := store.InsertPost(ctx, "R", "retain "+uuid.New().String())
		require.NoError(t, err)
		posts = append(posts, p)
		require.NoError(t, store.MarkViewed(ctx, user.ID, []uuid.UUID{p.ID}, retain))
	}

	n, err := store.CountUserViews(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, retain, n)

	// Newest retain IDs should remain.
	kept, err := store.ListRecentViewedPostIDs(ctx, user.ID, 10)
	require.NoError(t, err)
	require.Len(t, kept, retain)
	require.Equal(t, posts[4].ID, kept[0])
	require.Equal(t, posts[3].ID, kept[1])
	require.Equal(t, posts[2].ID, kept[2])

	for _, p := range posts {
		vc, err := store.GetPostViewCount(ctx, p.ID)
		require.NoError(t, err)
		require.Equal(t, 1, vc, "view_count should increment on insert even if later trimmed")
	}

	// Re-marking same IDs must not bump view_count again.
	require.NoError(t, store.MarkViewed(ctx, user.ID, []uuid.UUID{posts[4].ID}, retain))
	vc, err := store.GetPostViewCount(ctx, posts[4].ID)
	require.NoError(t, err)
	require.Equal(t, 1, vc)
}
