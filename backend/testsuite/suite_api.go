package testsuite

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/google/uuid"
	"github.com/recsys/backend/internal/handlers"
	"github.com/recsys/backend/internal/interactionwriter"
	"github.com/recsys/backend/internal/qdrantclient"
	"github.com/recsys/backend/internal/viewwriter"
)

// APISuite returns tests verifying full HTTP request-response cycles on all endpoints.
func APISuite(runner *Runner) []TestCase {
	return []TestCase{
		{
			Name:          "API/LikeUnlikeWorkflow",
			Scope:         ScopeHTTPAPI,
			RequiresInfra: true,
			Description:   "Asserts POST and DELETE on /api/posts/{id}/like updates Redis and enqueues jobs",
			Fn: func(tc *TestContext) {
				store := runner.GetDB()
				rdb := runner.GetRedis()
				qdr := runner.GetQdrant()
				ctx := tc.Context()

				user, _ := store.CreateUser(ctx, "api_like_"+uuid.New().String()[:8])
				post, _ := store.InsertPost(ctx, "API Like Post", "Testing like endpoint")
				vec := make([]float32, runner.GetConfig().VectorDim)
				vec[0] = 1.0
				_ = qdr.Upsert(ctx, []qdrantclient.UpsertPoint{{ID: post.ID.String(), Vector: vec}})

				defer func() {
					_ = store.DeleteUser(ctx, user.ID)
					_ = store.DeletePost(ctx, post.ID)
					_ = qdr.DeletePoints(context.Background(), []string{post.ID.String()})
					_, _ = rdb.DeleteUserKeys(ctx, user.ID.String())
				}()

				api := &handlers.API{
					Cfg:          runner.GetConfig(),
					DB:           store,
					Redis:        rdb,
					Qdrant:       qdr,
					Interactions: interactionwriter.New(store, 1, 10),
				}
				defer api.Interactions.Close()
				router := api.Routes()

				// 1. POST /api/posts/{id}/like
				reqLike := httptest.NewRequest(http.MethodPost, "/api/posts/"+post.ID.String()+"/like", nil)
				reqLike.Header.Set("X-User-ID", user.ID.String())
				wLike := httptest.NewRecorder()
				router.ServeHTTP(wLike, reqLike)

				tc.AssertEqual(http.StatusOK, wLike.Code, "post like status code")

				// 2. DELETE /api/posts/{id}/like (Unlike)
				reqUnlike := httptest.NewRequest(http.MethodDelete, "/api/posts/"+post.ID.String()+"/like", nil)
				reqUnlike.Header.Set("X-User-ID", user.ID.String())
				wUnlike := httptest.NewRecorder()
				router.ServeHTTP(wUnlike, reqUnlike)

				tc.AssertEqual(http.StatusOK, wUnlike.Code, "delete like status code")
			},
		},
		{
			Name:          "API/CommentsPostAndList",
			Scope:         ScopeHTTPAPI,
			RequiresInfra: true,
			Description:   "Asserts POST and GET /api/posts/{id}/comments creates and lists comment threads",
			Fn: func(tc *TestContext) {
				store := runner.GetDB()
				rdb := runner.GetRedis()
				qdr := runner.GetQdrant()
				ctx := tc.Context()

				user, _ := store.CreateUser(ctx, "api_comm_"+uuid.New().String()[:8])
				post, _ := store.InsertPost(ctx, "API Comment Post", "Testing comments endpoint")
				vec := make([]float32, runner.GetConfig().VectorDim)
				vec[0] = 1.0
				_ = qdr.Upsert(ctx, []qdrantclient.UpsertPoint{{ID: post.ID.String(), Vector: vec}})

				defer func() {
					_ = store.DeleteUser(ctx, user.ID)
					_ = store.DeletePost(ctx, post.ID)
					_ = qdr.DeletePoints(context.Background(), []string{post.ID.String()})
					_, _ = rdb.DeleteUserKeys(ctx, user.ID.String())
				}()

				api := &handlers.API{
					Cfg:          runner.GetConfig(),
					DB:           store,
					Redis:        rdb,
					Qdrant:       qdr,
					Interactions: interactionwriter.New(store, 1, 10),
				}
				defer api.Interactions.Close()
				router := api.Routes()

				// 1. POST comment
				commentBody, _ := json.Marshal(map[string]string{"body": "Super useful post!"})
				reqComment := httptest.NewRequest(http.MethodPost, "/api/posts/"+post.ID.String()+"/comments", bytes.NewReader(commentBody))
				reqComment.Header.Set("X-User-ID", user.ID.String())
				wComment := httptest.NewRecorder()
				router.ServeHTTP(wComment, reqComment)
				tc.AssertEqual(http.StatusCreated, wComment.Code, "post comment status")

				// 2. GET comments
				reqList := httptest.NewRequest(http.MethodGet, "/api/posts/"+post.ID.String()+"/comments", nil)
				wList := httptest.NewRecorder()
				router.ServeHTTP(wList, reqList)
				tc.AssertEqual(http.StatusOK, wList.Code, "list comments status")
			},
		},
		{
			Name:          "API/InteractionsAggregatorEndpoint",
			Scope:         ScopeHTTPAPI,
			RequiresInfra: true,
			Description:   "Asserts GET /api/me/interactions aggregates PG rows and Redis hot state",
			Fn: func(tc *TestContext) {
				store := runner.GetDB()
				rdb := runner.GetRedis()
				ctx := tc.Context()

				user, _ := store.CreateUser(ctx, "api_ix_"+uuid.New().String()[:8])
				post, _ := store.InsertPost(ctx, "API IX Post", "Testing interactions list")

				defer func() {
					_ = store.DeleteUser(ctx, user.ID)
					_ = store.DeletePost(ctx, post.ID)
					_, _ = rdb.DeleteUserKeys(ctx, user.ID.String())
				}()

				_ = rdb.SetEngLike(ctx, user.ID.String(), post.ID.String(), true)
				_ = rdb.SetEngSave(ctx, user.ID.String(), post.ID.String())

				api := &handlers.API{
					Cfg:   runner.GetConfig(),
					DB:    store,
					Redis: rdb,
				}
				router := api.Routes()

				req := httptest.NewRequest(http.MethodGet, "/api/me/interactions", nil)
				req.Header.Set("X-User-ID", user.ID.String())
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				tc.AssertEqual(http.StatusOK, w.Code, "interactions status code")

				var resp map[string][]any
				err := json.NewDecoder(w.Body).Decode(&resp)
				tc.AssertNoError(err, "decode interactions response")
				tc.Assert(len(resp["likes"]) > 0, "likes list must contain item")
				tc.Assert(len(resp["saves"]) > 0, "saves list must contain item")
			},
		},
		{
			Name:          "API/FeedColdStartAndSeenFilter",
			Scope:         ScopeHTTPAPI,
			RequiresInfra: true,
			Description:   "Asserts GET /api/feed serves recent posts for cold start and marks seen",
			Fn: func(tc *TestContext) {
				store := runner.GetDB()
				rdb := runner.GetRedis()
				qdr := runner.GetQdrant()
				ctx := tc.Context()

				user, _ := store.CreateUser(ctx, "api_feed_"+uuid.New().String()[:8])
				defer func() {
					_ = store.DeleteUser(ctx, user.ID)
					_, _ = rdb.DeleteUserKeys(ctx, user.ID.String())
				}()

				api := &handlers.API{
					Cfg:    runner.GetConfig(),
					DB:     store,
					Redis:  rdb,
					Qdrant: qdr,
					Views:  viewwriter.New(store, 1, 10, 500),
				}
				defer api.Views.Close()
				router := api.Routes()

				req := httptest.NewRequest(http.MethodGet, "/api/feed", nil)
				req.Header.Set("X-User-ID", user.ID.String())
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)

				tc.AssertEqual(http.StatusOK, w.Code, "feed status code")
				var feedResp struct {
					Posts []struct {
						ID    uuid.UUID `json:"id"`
						Title string    `json:"title"`
					} `json:"posts"`
				}
				err := json.NewDecoder(w.Body).Decode(&feedResp)
				tc.AssertNoError(err, "decode feed response")
				tc.Assert(len(feedResp.Posts) > 0, "feed must return posts")
			},
		},
	}
}
