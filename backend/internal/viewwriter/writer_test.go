package viewwriter_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/recsys/backend/internal/config"
	"github.com/recsys/backend/internal/db"
	"github.com/recsys/backend/internal/viewwriter"
	"github.com/stretchr/testify/require"
)

func TestViewWriterPersistsAsync(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()
	store, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer store.Pool.Close()

	user, err := store.CreateUser(ctx, "vw_"+uuid.New().String()[:8])
	require.NoError(t, err)
	post, err := store.InsertPost(ctx, "VW", "viewwriter "+uuid.New().String())
	require.NoError(t, err)

	w := viewwriter.New(store, 2, 16, cfg.ViewRetainLimit)
	w.Enqueue(user.ID, []uuid.UUID{post.ID})
	w.Close()

	ids, err := store.ListRecentViewedPostIDs(ctx, user.ID, 10)
	require.NoError(t, err)
	require.Contains(t, ids, post.ID)
}

func TestViewWriterDropsWhenFull(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()
	store, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer store.Pool.Close()

	// Queue size 1, no workers draining until we don't start... New always starts workers.
	// Fill queue faster than workers can write by enqueueing many tiny jobs; drop path must not panic.
	w := viewwriter.New(store, 1, 1, cfg.ViewRetainLimit)
	defer w.Close()
	for i := 0; i < 50; i++ {
		w.Enqueue(uuid.New(), []uuid.UUID{uuid.New()})
	}
	time.Sleep(50 * time.Millisecond)
}
