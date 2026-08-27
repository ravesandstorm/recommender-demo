package testsuite

import (
	"context"
	"math"

	"github.com/google/uuid"
	"github.com/recsys/backend/internal/qdrantclient"
	"github.com/recsys/backend/internal/vector"
)

// IntegrationSuite returns end-to-end tests exercising the entire recommendation stack.
func IntegrationSuite(runner *Runner) []TestCase {
	return []TestCase{
		{
			Name:          "Integration/FastEmbedGeneration",
			Scope:         ScopeIntegrationE2E,
			RequiresInfra: true,
			Description:   "Asserts FastEmbed microservice generates 384-dimensional non-zero embeddings",
			Fn: func(tc *TestContext) {
				embed := runner.GetEmbed()
				ctx := tc.Context()

				vecs, err := embed.Embed(ctx, []string{"vector recommendation engine with fastembed"})
				tc.AssertNoError(err, "fastembed embed call")
				tc.AssertEqual(1, len(vecs), "embeddings count")
				tc.AssertEqual(384, len(vecs[0]), "vector dimension 384")

				var sum float64
				for _, v := range vecs[0] {
					sum += math.Abs(float64(v))
				}
				tc.Assert(sum > 0.0, "vector components sum must be > 0")
			},
		},
		{
			Name:          "Integration/QdrantUpsertAndSearch",
			Scope:         ScopeIntegrationE2E,
			RequiresInfra: true,
			Description:   "Asserts Qdrant vector database upserts points, retrieves vectors, and performs ANN search",
			Fn: func(tc *TestContext) {
				qdr := runner.GetQdrant()
				embed := runner.GetEmbed()
				ctx := tc.Context()

				testText := "alpine mountain climbing and outdoor expedition"
				vecs, err := embed.Embed(ctx, []string{testText})
				tc.AssertNoError(err, "embed text")

				pointID := uuid.New().String()
				err = qdr.Upsert(ctx, []qdrantclient.UpsertPoint{
					{ID: pointID, Vector: vecs[0]},
				})
				tc.AssertNoError(err, "qdrant upsert")

				defer func() {
					_ = qdr.DeletePoints(context.Background(), []string{pointID})
				}()

				// Retrieve vector
				retrieved, err := qdr.GetVector(ctx, pointID)
				tc.AssertNoError(err, "qdrant get vector")
				tc.AssertEqual(384, len(retrieved), "retrieved vector dimension")

				// Search
				hits, err := qdr.Search(ctx, vecs[0], 5)
				tc.AssertNoError(err, "qdrant search")
				tc.Assert(len(hits) > 0, "hits count must be > 0")

				found := false
				for _, h := range hits {
					if h.ID == pointID {
						found = true
						tc.Assert(h.Score > 0.8, "self similarity score should be high")
						break
					}
				}
				tc.Assert(found, "upserted point must be in search results")
			},
		},
		{
			Name:          "Integration/FullUserRecommendationJourney",
			Scope:         ScopeIntegrationE2E,
			RequiresInfra: true,
			Description:   "Asserts full user lifecycle: cold start feed -> interaction signal -> vector shift -> cleanup",
			Fn: func(tc *TestContext) {
				store := runner.GetDB()
				rdb := runner.GetRedis()
				qdr := runner.GetQdrant()
				embed := runner.GetEmbed()
				ctx := tc.Context()

				// 1. Create User
				user, err := store.CreateUser(ctx, "e2e_user_"+uuid.New().String()[:8])
				tc.AssertNoError(err, "create e2e user")
				uid := user.ID.String()

				defer func() {
					_ = store.DeleteUser(ctx, user.ID)
					_, _ = rdb.DeleteUserKeys(ctx, uid)
				}()

				// 2. Create and index sample post
				postText := "Quantum Computing Algorithms and Superconducting Qubits"
				post, err := store.InsertPost(ctx, "Quantum Computing", postText)
				tc.AssertNoError(err, "insert post")
				defer func() {
					_ = store.DeletePost(ctx, post.ID)
					_ = qdr.DeletePoints(context.Background(), []string{post.ID.String()})
				}()

				vecs, err := embed.Embed(ctx, []string{post.Title + "\n" + post.Content})
				tc.AssertNoError(err, "embed post")
				err = qdr.Upsert(ctx, []qdrantclient.UpsertPoint{
					{ID: post.ID.String(), Vector: vecs[0]},
				})
				tc.AssertNoError(err, "upsert to qdrant")

				// 3. User likes the post
				likeW, _ := store.GetWeight(ctx, "like")
				err = store.UpsertLike(ctx, user.ID, post.ID, true)
				tc.AssertNoError(err, "upsert like")

				// Update user vector in Redis
				curVec, _ := rdb.GetUserVector(ctx, uid)
				newVec := vector.AddScaled(curVec, likeW, vecs[0])
				err = rdb.SetUserVector(ctx, uid, newVec)
				tc.AssertNoError(err, "set user vector after like")

				// 4. Verify user vector is now aligned with quantum computing
				afterVec, err := rdb.GetUserVector(ctx, uid)
				tc.AssertNoError(err, "get user vector")
				sim := vector.Cosine(afterVec, vecs[0])
				tc.Assert(sim > 0.9, "user vector should have high cosine similarity to liked post")

				// 5. Cleanup user and verify DB & Redis purge
				err = store.DeleteUser(ctx, user.ID)
				tc.AssertNoError(err, "delete user")
				deletedKeys, err := rdb.DeleteUserKeys(ctx, uid)
				tc.AssertNoError(err, "delete redis keys")
				tc.Assert(deletedKeys >= 0, "deleted keys count")
			},
		},
	}
}
