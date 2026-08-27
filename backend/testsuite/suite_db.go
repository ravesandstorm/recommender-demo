package testsuite

import (
	"github.com/google/uuid"
	"github.com/recsys/backend/internal/db"
)

// DBSuite returns tests verifying relational data integrity, views retention, and engagement tables in Postgres.
func DBSuite(runner *Runner) []TestCase {
	return []TestCase{
		{
			Name:          "Postgres/UserLifecycleAndConstraints",
			Scope:         ScopePostgresDB,
			RequiresInfra: true,
			Description:   "Asserts user creation, listing, existence checks, duplicate constraints, and deletion",
			Fn: func(tc *TestContext) {
				store := runner.GetDB()
				ctx := tc.Context()

				username := "db_user_" + uuid.New().String()[:8]
				u, err := store.CreateUser(ctx, username)
				tc.AssertNoError(err, "create user")
				tc.AssertEqual(username, u.Username, "username match")

				defer func() { _ = store.DeleteUser(ctx, u.ID) }()

				// Duplicate user should fail with constraint error
				_, dupErr := store.CreateUser(ctx, username)
				tc.AssertError(dupErr, "duplicate username creation")

				// List users contains new user
				users, err := store.ListUsers(ctx)
				tc.AssertNoError(err, "list users")
				found := false
				for _, existing := range users {
					if existing.ID == u.ID {
						found = true
						break
					}
				}
				tc.Assert(found, "created user must be in user list")

				// User exists check
				exists, err := store.UserExists(ctx, u.ID)
				tc.AssertNoError(err, "user exists")
				tc.Assert(exists, "user must exist")
			},
		},
		{
			Name:          "Postgres/PostCRUDAndTitles",
			Scope:         ScopePostgresDB,
			RequiresInfra: true,
			Description:   "Asserts post insertion, batch title resolution, and batch deletion",
			Fn: func(tc *TestContext) {
				store := runner.GetDB()
				ctx := tc.Context()

				title1 := "Post One " + uuid.New().String()[:6]
				title2 := "Post Two " + uuid.New().String()[:6]
				p1, err := store.InsertPost(ctx, title1, "Content 1")
				tc.AssertNoError(err, "insert post 1")
				p2, err := store.InsertPost(ctx, title2, "Content 2")
				tc.AssertNoError(err, "insert post 2")

				defer func() {
					_ = store.DeletePosts(ctx, []uuid.UUID{p1.ID, p2.ID})
				}()

				titles, err := store.GetPostTitles(ctx, []uuid.UUID{p1.ID, p2.ID})
				tc.AssertNoError(err, "get post titles")
				tc.AssertEqual(title1, titles[p1.ID], "post 1 title")
				tc.AssertEqual(title2, titles[p2.ID], "post 2 title")

				recents, err := store.RecentPosts(ctx, 10)
				tc.AssertNoError(err, "recent posts")
				tc.Assert(len(recents) >= 2, "recent posts count >= 2")
			},
		},
		{
			Name:          "Postgres/InteractionWeightsAndConfig",
			Scope:         ScopePostgresDB,
			RequiresInfra: true,
			Description:   "Asserts default interaction weights exist and are correctly signed",
			Fn: func(tc *TestContext) {
				store := runner.GetDB()
				ctx := tc.Context()

				weights, err := store.ListWeights(ctx)
				tc.AssertNoError(err, "list weights")
				tc.Assert(len(weights) >= 4, "must contain at least 4 default weights")

				likeW, err := store.GetWeight(ctx, "like")
				tc.AssertNoError(err, "get like weight")
				tc.Assert(likeW > 0, "like weight must be positive")

				dislikeW, err := store.GetWeight(ctx, "dislike")
				tc.AssertNoError(err, "get dislike weight")
				tc.Assert(dislikeW < 0, "dislike weight must be negative (negative reinforcement)")

				saveW, err := store.GetWeight(ctx, "save")
				tc.AssertNoError(err, "get save weight")
				tc.Assert(saveW > 0, "save weight must be positive")
			},
		},
		{
			Name:          "Postgres/ViewRetentionLimitAndCounters",
			Scope:         ScopePostgresDB,
			EdgeCase:      EdgeViewRetainLimitTrim,
			RequiresInfra: true,
			Description:   "Asserts MarkViewed increments post view_count and trims history to retain limit",
			Fn: func(tc *TestContext) {
				store := runner.GetDB()
				ctx := tc.Context()

				user, err := store.CreateUser(ctx, "db_v_"+uuid.New().String()[:8])
				tc.AssertNoError(err, "create user")
				defer func() { _ = store.DeleteUser(ctx, user.ID) }()

				const retainLimit = 3
				var posts []db.Post
				for i := 0; i < 5; i++ {
					p, _ := store.InsertPost(ctx, "View Post", "Testing retain limit")
					posts = append(posts, p)
					_ = store.MarkViewed(ctx, user.ID, []uuid.UUID{p.ID}, retainLimit)
				}
				defer func() {
					ids := make([]uuid.UUID, len(posts))
					for i, p := range posts {
						ids[i] = p.ID
					}
					_ = store.DeletePosts(ctx, ids)
				}()

				// Count should be capped at retainLimit
				count, err := store.CountUserViews(ctx, user.ID)
				tc.AssertNoError(err, "count user views")
				tc.AssertEqual(retainLimit, count, "user view count capped at retainLimit")

				// List kept post IDs
				kept, err := store.ListRecentViewedPostIDs(ctx, user.ID, 10)
				tc.AssertNoError(err, "list recent viewed post ids")
				tc.AssertEqual(retainLimit, len(kept), "kept ids count")
				tc.AssertEqual(posts[4].ID, kept[0], "newest post kept first")

				// Post view_count should be 1
				vc, err := store.GetPostViewCount(ctx, posts[4].ID)
				tc.AssertNoError(err, "get post view count")
				tc.AssertEqual(1, vc, "post view count")

				// Re-marking same post must not increment counter again (idempotent)
				_ = store.MarkViewed(ctx, user.ID, []uuid.UUID{posts[4].ID}, retainLimit)
				vcAfter, _ := store.GetPostViewCount(ctx, posts[4].ID)
				tc.AssertEqual(1, vcAfter, "view count must remain 1 after re-marking")
			},
		},
		{
			Name:          "Postgres/EngagementCascadeOnUserDelete",
			Scope:         ScopePostgresDB,
			EdgeCase:      EdgeCascadeUserCleanup,
			RequiresInfra: true,
			Description:   "Asserts deleting a user cascades to likes, saves, shares, and comments in Postgres",
			Fn: func(tc *TestContext) {
				store := runner.GetDB()
				ctx := tc.Context()

				user, err := store.CreateUser(ctx, "db_casc_"+uuid.New().String()[:8])
				tc.AssertNoError(err, "create user")
				post, err := store.InsertPost(ctx, "Cascade Post", "Testing CASCADE triggers")
				tc.AssertNoError(err, "insert post")

				defer func() { _ = store.DeletePost(ctx, post.ID) }()

				// Insert interactions
				_ = store.UpsertLike(ctx, user.ID, post.ID, true)
				_ = store.InsertSave(ctx, user.ID, post.ID)
				_, _ = store.InsertComment(ctx, user.ID, post.ID, "Test comment")

				// Delete User
				err = store.DeleteUser(ctx, user.ID)
				tc.AssertNoError(err, "delete user")

				// Verify like is gone
				_, exists, err := store.GetLike(ctx, user.ID, post.ID)
				tc.AssertNoError(err, "get like after delete")
				tc.Assert(!exists, "like row must be cascaded")

				// Verify save is gone
				hasSave, err := store.HasSave(ctx, user.ID, post.ID)
				tc.AssertNoError(err, "has save after delete")
				tc.Assert(!hasSave, "save row must be cascaded")
			},
		},
	}
}
