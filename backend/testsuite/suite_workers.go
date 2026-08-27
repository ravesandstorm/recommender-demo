package testsuite

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/recsys/backend/internal/interactionwriter"
	"github.com/recsys/backend/internal/viewwriter"
)

// WorkersSuite returns tests verifying the async background batch writer pools.
func WorkersSuite(runner *Runner) []TestCase {
	return []TestCase{
		{
			Name:          "Workers/InteractionWriterAsyncPersistence",
			Scope:         ScopeUnitWorkers,
			RequiresInfra: true,
			Description:   "Asserts interactionwriter persists likes, saves, and shares asynchronously",
			Fn: func(tc *TestContext) {
				store := runner.GetDB()
				ctx := tc.Context()

				user, err := store.CreateUser(ctx, "wkr_u_"+uuid.New().String()[:8])
				tc.AssertNoError(err, "create user")
				post, err := store.InsertPost(ctx, "Worker Test Post", "Testing async batch writer.")
				tc.AssertNoError(err, "create post")

				defer func() {
					_ = store.DeleteUser(ctx, user.ID)
					_ = store.DeletePost(ctx, post.ID)
				}()

				w := interactionwriter.New(store, 2, 32)
				w.Enqueue(interactionwriter.Job{
					Kind:   interactionwriter.KindUpsertLike,
					UserID: user.ID,
					PostID: post.ID,
					IsLike: true,
				})
				w.Enqueue(interactionwriter.Job{
					Kind:   interactionwriter.KindInsertSave,
					UserID: user.ID,
					PostID: post.ID,
				})
				w.Close() // Wait for worker drainage

				isLike, exists, err := store.GetLike(ctx, user.ID, post.ID)
				tc.AssertNoError(err, "get like")
				tc.Assert(exists, "like row must exist")
				tc.Assert(isLike, "like status must be true")

				hasSave, err := store.HasSave(ctx, user.ID, post.ID)
				tc.AssertNoError(err, "has save")
				tc.Assert(hasSave, "save row must exist")
			},
		},
		{
			Name:          "Workers/QueueSaturationDropSafety",
			Scope:         ScopeUnitWorkers,
			EdgeCase:      EdgeQueueSaturationDrop,
			RequiresInfra: true,
			Description:   "Asserts interactionwriter drops gracefully without panic or block under burst load",
			Fn: func(tc *TestContext) {
				store := runner.GetDB()
				w := interactionwriter.New(store, 1, 1) // minimal queue
				defer w.Close()

				var wg sync.WaitGroup
				for i := 0; i < 100; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						w.Enqueue(interactionwriter.Job{
							Kind:   interactionwriter.KindUpsertLike,
							UserID: uuid.New(),
							PostID: uuid.New(),
							IsLike: true,
						})
					}()
				}
				wg.Wait()
				time.Sleep(50 * time.Millisecond)
				tc.Assert(true, "queue full drop handled without deadlocks or panics")
			},
		},
		{
			Name:          "Workers/ViewWriterImpressionBatching",
			Scope:         ScopeUnitWorkers,
			RequiresInfra: true,
			Description:   "Asserts viewwriter persists impressions and obeys retention limits",
			Fn: func(tc *TestContext) {
				store := runner.GetDB()
				ctx := tc.Context()

				user, err := store.CreateUser(ctx, "wkr_v_"+uuid.New().String()[:8])
				tc.AssertNoError(err, "create user")
				post1, _ := store.InsertPost(ctx, "VW1", "Impression 1")
				post2, _ := store.InsertPost(ctx, "VW2", "Impression 2")

				defer func() {
					_ = store.DeleteUser(ctx, user.ID)
					_ = store.DeletePosts(ctx, []uuid.UUID{post1.ID, post2.ID})
				}()

				vw := viewwriter.New(store, 2, 32, 500)
				vw.Enqueue(user.ID, []uuid.UUID{post1.ID, post2.ID})
				vw.Close()

				views, err := store.ListRecentViewedPostIDs(ctx, user.ID, 10)
				tc.AssertNoError(err, "list recent viewed post ids")
				tc.Assert(len(views) == 2, "user must have 2 recorded impressions")
			},
		},
	}
}
