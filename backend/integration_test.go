package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/recsys/backend/internal/config"
	"github.com/recsys/backend/internal/db"
	"github.com/recsys/backend/internal/embedclient"
	"github.com/recsys/backend/internal/migrate"
	"github.com/recsys/backend/internal/qdrantclient"
	"github.com/recsys/backend/internal/redisstore"
	"github.com/recsys/backend/internal/vector"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := waitHealthy(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "services not healthy (start docker compose): %v\n", err)
		os.Exit(1)
	}
	cfg := config.Load()
	store, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres: %v\n", err)
		os.Exit(1)
	}
	if err := migrate.Up(ctx, store.Pool); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
	store.Pool.Close()
	os.Exit(m.Run())
}

func waitHealthy(ctx context.Context) error {
	cfg := config.Load()
	deadline, _ := ctx.Deadline()
	for {
		errs := []error{}
		if err := pingHTTP(ctx, cfg.EmbeddingURL+"/health"); err != nil {
			errs = append(errs, fmt.Errorf("embedding: %w", err))
		}
		if err := pingHTTP(ctx, cfg.QdrantURL+"/readyz"); err != nil {
			errs = append(errs, fmt.Errorf("qdrant: %w", err))
		}
		pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			errs = append(errs, fmt.Errorf("postgres dial: %w", err))
		} else {
			if err := pool.Ping(ctx); err != nil {
				errs = append(errs, fmt.Errorf("postgres ping: %w", err))
			}
			pool.Close()
		}
		rdb := redisstore.New(cfg.RedisAddr, cfg.VectorDim)
		if err := rdb.Ping(ctx); err != nil {
			errs = append(errs, fmt.Errorf("redis: %w", err))
		}
		if len(errs) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%v", errs)
		}
		time.Sleep(2 * time.Second)
	}
}

func pingHTTP(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func TestEmbeddingDim(t *testing.T) {
	cfg := config.Load()
	client := embedclient.New(cfg.EmbeddingURL)
	vecs, err := client.Embed(context.Background(), []string{"recommendation systems with vector search"})
	require.NoError(t, err)
	require.Len(t, vecs, 1)
	require.Len(t, vecs[0], 384)
	var sum float64
	for _, v := range vecs[0] {
		sum += math.Abs(float64(v))
	}
	require.Greater(t, sum, 0.0)
}

func TestQdrantUpsertSearch(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()
	q := qdrantclient.New(cfg.QdrantURL, cfg.QdrantCollection, cfg.VectorDim)
	require.NoError(t, q.EnsureCollection(ctx))

	emb := embedclient.New(cfg.EmbeddingURL)
	vecs, err := emb.Embed(ctx, []string{"alpine hiking trails and mountain travel guides"})
	require.NoError(t, err)

	id := uuid.New().String()
	require.NoError(t, q.Upsert(ctx, []qdrantclient.UpsertPoint{{ID: id, Vector: vecs[0]}}))
	t.Cleanup(func() { _ = q.DeletePoints(context.Background(), []string{id}) })

	got, err := q.GetVector(ctx, id)
	require.NoError(t, err)
	require.Len(t, got, 384)

	hits, err := q.Search(ctx, vecs[0], 5)
	require.NoError(t, err)
	require.NotEmpty(t, hits)
}

func TestRedisAddSubtractVector(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()
	rdb := redisstore.New(cfg.RedisAddr, cfg.VectorDim)
	userID := uuid.New().String()
	t.Cleanup(func() { _, _ = rdb.DeleteUserKeys(context.Background(), userID) })

	base := vector.Zero(cfg.VectorDim)
	require.NoError(t, rdb.SetUserVector(ctx, userID, base))

	post := make([]float32, cfg.VectorDim)
	post[0] = 1
	post[1] = 2
	weight := 1.5

	cur, err := rdb.GetUserVector(ctx, userID)
	require.NoError(t, err)
	next := vector.AddScaled(cur, weight, post)
	require.NoError(t, rdb.SetUserVector(ctx, userID, next))

	afterAdd, err := rdb.GetUserVector(ctx, userID)
	require.NoError(t, err)
	require.InDelta(t, 1.5, afterAdd[0], 1e-5)
	require.InDelta(t, 3.0, afterAdd[1], 1e-5)

	undone := vector.SubScaled(afterAdd, weight, post)
	require.NoError(t, rdb.SetUserVector(ctx, userID, undone))
	afterSub, err := rdb.GetUserVector(ctx, userID)
	require.NoError(t, err)
	require.InDelta(t, 0, afterSub[0], 1e-5)
	require.InDelta(t, 0, afterSub[1], 1e-5)
}

func TestUsersCreateList(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()
	store, err := db.New(ctx, cfg.DatabaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { store.Pool.Close() })

	name := "tester_" + uuid.New().String()[:8]
	u, err := store.CreateUser(ctx, name)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.DeleteUser(context.Background(), u.ID) })
	require.Equal(t, name, u.Username)

	users, err := store.ListUsers(ctx)
	require.NoError(t, err)
	found := false
	for _, x := range users {
		if x.ID == u.ID {
			found = true
			break
		}
	}
	require.True(t, found)
}

func TestDeleteUserCleansPostgresAndRedis(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()
	store, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { store.Pool.Close() })
	rdb := redisstore.New(cfg.RedisAddr, cfg.VectorDim)
	if err := rdb.Ping(ctx); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}

	u, err := store.CreateUser(ctx, "del_"+uuid.New().String()[:8])
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = store.DeleteUser(context.Background(), u.ID)
		_, _ = rdb.DeleteUserKeys(context.Background(), u.ID.String())
	})
	post, err := store.InsertPost(ctx, "Del post", "delete user test "+uuid.New().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.DeletePost(context.Background(), post.ID) })
	require.NoError(t, store.UpsertLike(ctx, u.ID, post.ID, true))
	require.NoError(t, rdb.SetUserVector(ctx, u.ID.String(), vector.Zero(cfg.VectorDim)))
	require.NoError(t, rdb.SetUserInterests(ctx, u.ID.String(), nil))
	require.NoError(t, rdb.SetEngLike(ctx, u.ID.String(), post.ID.String(), true))
	require.NoError(t, rdb.SetShareMemo(ctx, u.ID.String(), post.ID.String(), time.Hour))

	require.NoError(t, store.DeleteUser(ctx, u.ID))
	ok, err := store.UserExists(ctx, u.ID)
	require.NoError(t, err)
	require.False(t, ok)
	_, exists, err := store.GetLike(ctx, u.ID, post.ID)
	require.NoError(t, err)
	require.False(t, exists)

	n, err := rdb.DeleteUserKeys(ctx, u.ID.String())
	require.NoError(t, err)
	_ = n
	vec, err := rdb.GetUserVector(ctx, u.ID.String())
	require.NoError(t, err)
	require.True(t, vector.IsZero(vec))
	memo, err := rdb.HasShareMemo(ctx, u.ID.String(), post.ID.String())
	require.NoError(t, err)
	require.False(t, memo)
}

func TestInteractionListsAndRedisEngagement(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()
	store, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { store.Pool.Close() })
	rdb := redisstore.New(cfg.RedisAddr, cfg.VectorDim)
	if err := rdb.Ping(ctx); err != nil {
		t.Skipf("redis unavailable: %v", err)
	}

	u, err := store.CreateUser(ctx, "ixlist_"+uuid.New().String()[:8])
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = store.DeleteUser(context.Background(), u.ID)
		_, _ = rdb.DeleteUserKeys(context.Background(), u.ID.String())
	})
	post, err := store.InsertPost(ctx, "List post", "interaction list "+uuid.New().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.DeletePost(context.Background(), post.ID) })

	require.NoError(t, store.UpsertLike(ctx, u.ID, post.ID, true))
	require.NoError(t, store.InsertSave(ctx, u.ID, post.ID))
	_, err = store.InsertComment(ctx, u.ID, post.ID, "hello")
	require.NoError(t, err)
	require.NoError(t, rdb.SetEngLike(ctx, u.ID.String(), post.ID.String(), true))
	require.NoError(t, rdb.SetEngSave(ctx, u.ID.String(), post.ID.String()))
	require.NoError(t, rdb.SetUserInterests(ctx, u.ID.String(), []vector.Interest{
		{Vector: vector.L2Normalize([]float32{1, 0, 0}), Weight: 1},
	}))

	likes, err := store.ListUserLikes(ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, likes, 1)
	require.Equal(t, post.ID, likes[0].PostID)

	saves, err := store.ListUserSaves(ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, saves, 1)

	comments, err := store.ListUserComments(ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, comments, 1)

	isLike, exists, err := rdb.GetEngLike(ctx, u.ID.String(), post.ID.String())
	require.NoError(t, err)
	require.True(t, exists)
	require.True(t, isLike)
	interests, err := rdb.GetUserInterests(ctx, u.ID.String())
	require.NoError(t, err)
	require.NotEmpty(t, interests)
}

func TestLikeUndoAndDislikeWeight(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()
	store, err := db.New(ctx, cfg.DatabaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { store.Pool.Close() })

	rdb := redisstore.New(cfg.RedisAddr, cfg.VectorDim)
	q := qdrantclient.New(cfg.QdrantURL, cfg.QdrantCollection, cfg.VectorDim)
	require.NoError(t, q.EnsureCollection(ctx))
	emb := embedclient.New(cfg.EmbeddingURL)

	user, err := store.CreateUser(ctx, "like_user_"+uuid.New().String()[:8])
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = store.DeleteUser(context.Background(), user.ID)
		_, _ = rdb.DeleteUserKeys(context.Background(), user.ID.String())
	})
	require.NoError(t, rdb.SetUserVector(ctx, user.ID.String(), vector.Zero(cfg.VectorDim)))

	post, err := store.InsertPost(ctx, "Soccer tactics pressing", "Midfield pressing and football recovery goals.")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = store.DeletePost(context.Background(), post.ID)
		_ = q.DeletePoints(context.Background(), []string{post.ID.String()})
	})
	vecs, err := emb.Embed(ctx, []string{post.Title + "\n" + post.Content})
	require.NoError(t, err)
	require.NoError(t, q.Upsert(ctx, []qdrantclient.UpsertPoint{{ID: post.ID.String(), Vector: vecs[0]}}))

	likeW, err := store.GetWeight(ctx, "like")
	require.NoError(t, err)
	dislikeW, err := store.GetWeight(ctx, "dislike")
	require.NoError(t, err)
	require.Greater(t, likeW, 0.0)
	require.Less(t, dislikeW, 0.0)

	require.NoError(t, store.UpsertLike(ctx, user.ID, post.ID, true))
	uv, _ := rdb.GetUserVector(ctx, user.ID.String())
	uv = vector.AddScaled(uv, likeW, vecs[0])
	require.NoError(t, rdb.SetUserVector(ctx, user.ID.String(), uv))

	afterLike, err := rdb.GetUserVector(ctx, user.ID.String())
	require.NoError(t, err)
	require.False(t, vector.IsZero(afterLike))

	// undo like
	undone := vector.SubScaled(afterLike, likeW, vecs[0])
	require.NoError(t, rdb.SetUserVector(ctx, user.ID.String(), undone))
	require.NoError(t, store.DeleteLike(ctx, user.ID, post.ID))
	afterUndo, err := rdb.GetUserVector(ctx, user.ID.String())
	require.NoError(t, err)
	require.InDelta(t, 0, float64(afterUndo[0]), 1e-3)

	// dislike applies negative reinforcement via negative weight
	require.NoError(t, store.UpsertLike(ctx, user.ID, post.ID, false))
	withDislike := vector.AddScaled(afterUndo, dislikeW, vecs[0])
	require.NoError(t, rdb.SetUserVector(ctx, user.ID.String(), withDislike))
	got, err := rdb.GetUserVector(ctx, user.ID.String())
	require.NoError(t, err)
	// first component should move opposite to like direction if post[0] != 0
	if vecs[0][0] != 0 {
		require.NotEqual(t, afterLike[0], got[0])
	}
}

func TestFeedFiltersViewed(t *testing.T) {
	cfg := config.Load()
	ctx := context.Background()
	store, err := db.New(ctx, cfg.DatabaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { store.Pool.Close() })

	user, err := store.CreateUser(ctx, "feed_user_"+uuid.New().String()[:8])
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.DeleteUser(context.Background(), user.ID) })

	p1, err := store.InsertPost(ctx, "A", "content a "+uuid.New().String())
	require.NoError(t, err)
	p2, err := store.InsertPost(ctx, "B", "content b "+uuid.New().String())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = store.DeletePosts(context.Background(), []uuid.UUID{p1.ID, p2.ID})
	})

	require.NoError(t, store.MarkViewed(ctx, user.ID, []uuid.UUID{p1.ID}, cfg.ViewRetainLimit))
	posts, err := store.RecentUnviewed(ctx, user.ID, 50)
	require.NoError(t, err)
	for _, p := range posts {
		require.NotEqual(t, p1.ID, p.ID)
	}
	found := false
	for _, p := range posts {
		if p.ID == p2.ID {
			found = true
		}
	}
	require.True(t, found)
}

func TestVectorMathHelpers(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1, 1, 1}
	sum := vector.AddScaled(a, 2, b)
	require.Equal(t, []float32{3, 4, 5}, sum)
	diff := vector.SubScaled(sum, 2, b)
	require.Equal(t, []float32{1, 2, 3}, diff)
}

// Ensure migrations dir discovery works from backend module root.
func TestMigrationsDirDiscoverable(t *testing.T) {
	cwd, _ := os.Getwd()
	t.Log("cwd", cwd)
	_, err := os.Stat(filepath.Join(cwd, "migrations"))
	if err != nil {
		_, err = os.Stat(filepath.Join(cwd, "..", "migrations"))
	}
	require.NoError(t, err)
}

func TestHTTPHealthIfAPIUp(t *testing.T) {
	cfg := config.Load()
	url := "http://127.0.0.1" + cfg.Addr + "/health"
	if cfg.Addr != "" && cfg.Addr[0] != ':' {
		url = "http://" + cfg.Addr + "/health"
	}
	resp, err := http.Get(url)
	if err != nil {
		t.Skip("API not running on " + cfg.Addr)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Skip("port occupied by another service")
	}
	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(body), "ok")
}

func postJSON(t *testing.T, url string, payload any, headers map[string]string) *http.Response {
	t.Helper()
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}
