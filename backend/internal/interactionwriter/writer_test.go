package interactionwriter_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/recsys/backend/internal/config"
	"github.com/recsys/backend/internal/db"
	"github.com/recsys/backend/internal/interactionwriter"
	"github.com/stretchr/testify/require"
)

func TestInteractionWriterPersistsAsync(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()
	store, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { store.Pool.Close() })

	user, err := store.CreateUser(ctx, "ix_"+uuid.New().String()[:8])
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.DeleteUser(context.Background(), user.ID) })
	post, err := store.InsertPost(ctx, "IX", "interactionwriter "+uuid.New().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.DeletePost(context.Background(), post.ID) })

	w := interactionwriter.New(store, 2, 16)
	w.Enqueue(interactionwriter.Job{
		Kind: interactionwriter.KindUpsertLike, UserID: user.ID, PostID: post.ID, IsLike: true,
	})
	w.Enqueue(interactionwriter.Job{
		Kind: interactionwriter.KindInsertSave, UserID: user.ID, PostID: post.ID,
	})
	w.Close()

	isLike, exists, err := store.GetLike(ctx, user.ID, post.ID)
	require.NoError(t, err)
	require.True(t, exists)
	require.True(t, isLike)

	ok, err := store.HasSave(ctx, user.ID, post.ID)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestInteractionWriterDropsWhenFull(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()
	store, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { store.Pool.Close() })

	w := interactionwriter.New(store, 1, 1)
	defer w.Close()
	for i := 0; i < 50; i++ {
		w.Enqueue(interactionwriter.Job{
			Kind: interactionwriter.KindUpsertLike, UserID: uuid.New(), PostID: uuid.New(), IsLike: true,
		})
	}
	time.Sleep(50 * time.Millisecond)
}
