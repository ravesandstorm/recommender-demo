package testsuite

import (
	"math"

	"github.com/google/uuid"
	"github.com/recsys/backend/internal/db"
	"github.com/recsys/backend/internal/vector"
)

// EdgeCasesSuite returns deep edge case and boundary tests covering failure modes and extreme inputs.
func EdgeCasesSuite(runner *Runner) []TestCase {
	return []TestCase{
		{
			Name:        "Edge/ZeroAndNilVectorMath",
			Scope:       ScopeEdgeCases,
			EdgeCase:    EdgeZeroOrNilVector,
			Description: "Asserts vector functions gracefully handle zero vectors, nil inputs, and epsilon magnitudes",
			Fn: func(tc *TestContext) {
				// 1. Zero vector creation
				z := vector.Zero(384)
				tc.Assert(vector.IsZero(z), "Zero(384) must report IsZero true")

				// 2. Normalizing zero vector returns exact zero vector without NaN
				normZero := vector.L2Normalize(z)
				tc.Assert(vector.IsZero(normZero), "L2Normalize on zero vector must return zero vector")
				for _, val := range normZero {
					tc.Assert(!math.IsNaN(float64(val)), "normalized value must not be NaN")
					tc.Assert(!math.IsInf(float64(val), 0), "normalized value must not be Inf")
				}

				// 3. Normalizing sub-epsilon vector (magnitude < 1e-12)
				tiny := []float32{1e-8, -1e-8}
				normTiny := vector.L2Normalize(tiny)
				tc.Assert(vector.IsZero(normTiny), "sub-epsilon vector should normalize to zero vector")

				// 4. Cosine with zero vector returns 0 without NaN/panic
				a := []float32{1, 0, 0}
				cosZero := vector.Cosine(a, z[:3])
				tc.Assert(math.Abs(cosZero) < 1e-6, "cosine with zero vector should be 0.0")
				tc.Assert(!math.IsNaN(cosZero), "cosine must not be NaN")
			},
		},
		{
			Name:        "Edge/MultiInterestMaxKCentroidCap",
			Scope:       ScopeEdgeCases,
			EdgeCase:    EdgeMaxKCentroidsCap,
			Description: "Asserts ApplyInterest strictly respects maxK limit and updates closest centroid instead of overflowing",
			Fn: func(tc *TestContext) {
				const maxK = 3
				v1 := vector.L2Normalize([]float32{1, 0, 0, 0})
				v2 := vector.L2Normalize([]float32{0, 1, 0, 0})
				v3 := vector.L2Normalize([]float32{0, 0, 1, 0})
				v4 := vector.L2Normalize([]float32{0, 0, 0, 1}) // 4th orthogonal vector

				var interests []vector.Interest
				interests = vector.ApplyInterest(interests, v1, 1.0, maxK, 0.55)
				interests = vector.ApplyInterest(interests, v2, 1.0, maxK, 0.55)
				interests = vector.ApplyInterest(interests, v3, 1.0, maxK, 0.55)
				tc.AssertEqual(3, len(interests), "should have exactly maxK=3 centroids")

				// 4th orthogonal signal attempts to spawn when K is full
				interests = vector.ApplyInterest(interests, v4, 1.0, maxK, 0.55)
				tc.AssertEqual(maxK, len(interests), "interests count must NEVER exceed maxK")
			},
		},
		{
			Name:        "Edge/NegativeMassPruning",
			Scope:       ScopeEdgeCases,
			EdgeCase:    EdgeNegativeMassPrune,
			Description: "Asserts negative feedback prunes centroids when accumulated weight drops <= 0",
			Fn: func(tc *TestContext) {
				v1 := vector.L2Normalize([]float32{1, 0, 0})
				v2 := vector.L2Normalize([]float32{0, 1, 0})

				interests := []vector.Interest{
					{Vector: v1, Weight: 1.0},
					{Vector: v2, Weight: 1.0},
				}

				// Strong negative signal on centroid 1 (weight = -1.5)
				interests = vector.ApplyInterest(interests, v1, -1.5, 3, 0.55)
				tc.AssertEqual(1, len(interests), "centroid 1 should be pruned after weight dropped below 0")
				tc.Assert(vector.Cosine(interests[0].Vector, v2) > 0.99, "remaining centroid must be v2")
			},
		},
		{
			Name:        "Edge/MMRLambdaBoundariesAndClamping",
			Scope:       ScopeEdgeCases,
			EdgeCase:    EdgeMMRLambdaExtremes,
			Description: "Asserts MMR handles boundary values: lambda=0 (pure diversity), lambda=1 (pure relevance), negative/overflow lambda",
			Fn: func(tc *TestContext) {
				cands := []vector.MMRCandidate{
					{ID: "c1", Relevance: 0.99, Vector: []float32{1, 0}},
					{ID: "c2", Relevance: 0.95, Vector: []float32{0.99, 0.01}},
					{ID: "c3", Relevance: 0.50, Vector: []float32{0, 1}},
				}

				// 1. Lambda = 0.0 (Pure Diversity)
				pureDiv := vector.MMR(cands, 2, 0.0)
				tc.AssertEqual(2, len(pureDiv), "pure diversity picked count")
				tc.Assert(pureDiv[1].ID == "c3", "pure diversity must prioritize orthogonal item c3")

				// 2. Lambda = 1.0 (Pure Relevance)
				pureRel := vector.MMR(cands, 2, 1.0)
				tc.AssertEqual(2, len(pureRel), "pure relevance picked count")
				tc.Assert(pureRel[0].ID == "c1" && pureRel[1].ID == "c2", "pure relevance picks c1 and c2")

				// 3. Lambda < 0 (clamped to 0)
				negLam := vector.MMR(cands, 2, -5.0)
				tc.AssertEqual(2, len(negLam), "negative lambda clamped without error")

				// 4. Lambda > 1 (clamped to 1)
				overLam := vector.MMR(cands, 2, 99.0)
				tc.AssertEqual(2, len(overLam), "overflow lambda clamped without error")

				// 5. k > len(candidates)
				overK := vector.MMR(cands, 100, 0.7)
				tc.AssertEqual(3, len(overK), "k > len(candidates) returns all candidates")

				// 6. k <= 0 or empty candidates
				tc.Assert(vector.MMR(nil, 5, 0.5) == nil, "nil candidates returns nil")
				tc.Assert(vector.MMR(cands, 0, 0.5) == nil, "k=0 returns nil")
			},
		},
		{
			Name:          "Edge/RapidInteractionToggleCycles",
			Scope:         ScopeEdgeCases,
			EdgeCase:      EdgeRapidToggleCycle,
			RequiresInfra: true,
			Description:   "Asserts preference vector consistency during rapid like -> dislike -> unlike cycles",
			Fn: func(tc *TestContext) {
				store := runner.GetDB()
				rdb := runner.GetRedis()
				ctx := tc.Context()

				user, _ := store.CreateUser(ctx, "edge_cyc_"+uuid.New().String()[:8])
				uid := user.ID.String()
				defer func() {
					_ = store.DeleteUser(ctx, user.ID)
					_, _ = rdb.DeleteUserKeys(ctx, uid)
				}()

				postVec := make([]float32, runner.GetConfig().VectorDim)
				postVec[0] = 1.0
				postVec = vector.L2Normalize(postVec)
				weight := 1.5

				// Initial vector = 0
				base := vector.Zero(runner.GetConfig().VectorDim)
				_ = rdb.SetUserVector(ctx, uid, base)

				// Step 1: Like (apply +weight)
				v1 := vector.AddScaled(base, weight, postVec)
				_ = rdb.SetUserVector(ctx, uid, v1)
				tc.Assert(!vector.IsZero(v1), "vector after like must be non-zero")

				// Step 2: Undo Like (subtract weight)
				v2 := vector.SubScaled(v1, weight, postVec)
				_ = rdb.SetUserVector(ctx, uid, v2)
				tc.Assert(vector.IsZero(v2), "vector after undo like must return to exact zero")

				// Step 3: Dislike (apply negative weight)
				dislikeW, _ := store.GetWeight(ctx, "dislike")
				v3 := vector.AddScaled(v2, dislikeW, postVec)
				_ = rdb.SetUserVector(ctx, uid, v3)
				tc.Assert(v3[0] < 0, "first component must be negative after dislike")

				// Step 4: Undo Dislike (subtract negative weight)
				v4 := vector.SubScaled(v3, dislikeW, postVec)
				_ = rdb.SetUserVector(ctx, uid, v4)
				tc.Assert(vector.IsZero(v4), "vector after undo dislike must return to exact zero")
			},
		},
		{
			Name:          "Edge/DuplicateViewIdempotency",
			Scope:         ScopeEdgeCases,
			EdgeCase:      EdgeViewRetainLimitTrim,
			RequiresInfra: true,
			Description:   "Asserts repeated view impressions for the same post do not duplicate view_count",
			Fn: func(tc *TestContext) {
				store := runner.GetDB()
				ctx := tc.Context()

				user, _ := store.CreateUser(ctx, "edge_dupv_"+uuid.New().String()[:8])
				post, _ := store.InsertPost(ctx, "Edge Dup View", "Idempotency test")
				defer func() {
					_ = store.DeleteUser(ctx, user.ID)
					_ = store.DeletePost(ctx, post.ID)
				}()

				// First view
				err := store.MarkViewed(ctx, user.ID, []uuid.UUID{post.ID}, 500)
				tc.AssertNoError(err, "first mark viewed")
				vc1, _ := store.GetPostViewCount(ctx, post.ID)
				tc.AssertEqual(1, vc1, "view count after first view")

				// Second view (same user, same post)
				err = store.MarkViewed(ctx, user.ID, []uuid.UUID{post.ID}, 500)
				tc.AssertNoError(err, "second mark viewed")
				vc2, _ := store.GetPostViewCount(ctx, post.ID)
				tc.AssertEqual(1, vc2, "view count must remain 1 on duplicate impression")
			},
		},
		{
			Name:        "Edge/QuotaZeroTotalBoundary",
			Scope:       ScopeEdgeCases,
			Description: "Asserts AllocateQuotas handles zero slots, empty weights, or all zero weights without panic",
			Fn: func(tc *TestContext) {
				// 1. Zero total
				q1 := vector.AllocateQuotas([]float64{1.0, 2.0}, 0)
				tc.AssertEqual(2, len(q1), "quotas length")
				tc.AssertEqual(0, q1[0]+q1[1], "sum should be 0")

				// 2. All zero weights
				q2 := vector.AllocateQuotas([]float64{0.0, 0.0}, 10)
				tc.AssertEqual(2, len(q2), "quotas length")
				tc.AssertEqual(0, q2[0]+q2[1], "sum should be 0 for zero weights")

				// 3. Empty weights
				q3 := vector.AllocateQuotas(nil, 10)
				tc.AssertEqual(0, len(q3), "nil weights returns empty slice")
			},
		},
		{
			Name:        "Edge/CollinearCosineThresholdBoundary",
			Scope:       ScopeEdgeCases,
			EdgeCase:    EdgeCollinearInterests,
			Description: "Asserts ShouldCollapseInterests precision at boundary (0.899 not collapsed vs 0.901 collapsed)",
			Fn: func(tc *TestContext) {
				// 0.899 similarity (below 0.9 threshold)
				thetaSub := math.Acos(0.899)
				vSub := []float32{float32(math.Cos(thetaSub)), float32(math.Sin(thetaSub))}
				pSub := []vector.Interest{
					{Vector: []float32{1, 0}, Weight: 1.0},
					{Vector: vSub, Weight: 1.0},
				}
				tc.Assert(!vector.ShouldCollapseInterests(pSub, 0.90), "0.899 cosine must NOT collapse with 0.90 min")

				// 0.901 similarity (above 0.9 threshold)
				thetaSuper := math.Acos(0.901)
				vSuper := []float32{float32(math.Cos(thetaSuper)), float32(math.Sin(thetaSuper))}
				pSuper := []vector.Interest{
					{Vector: []float32{1, 0}, Weight: 1.0},
					{Vector: vSuper, Weight: 1.0},
				}
				tc.Assert(vector.ShouldCollapseInterests(pSuper, 0.90), "0.901 cosine MUST collapse with 0.90 min")
			},
		},
		{
			Name:          "Edge/BatchViewRetentionLimitTrim",
			Scope:         ScopeEdgeCases,
			EdgeCase:      EdgeViewRetainLimitTrim,
			RequiresInfra: true,
			Description:   "Asserts atomic batch view insert exceeding retain limit trims oldest in single step",
			Fn: func(tc *TestContext) {
				store := runner.GetDB()
				ctx := tc.Context()

				user, _ := store.CreateUser(ctx, "edge_bv_"+uuid.New().String()[:8])
				defer func() { _ = store.DeleteUser(ctx, user.ID) }()

				const retain = 5
				posts := make([]db.Post, 10)
				ids := make([]uuid.UUID, 10)
				for i := range posts {
					posts[i], _ = store.InsertPost(ctx, "Batch Post", "Testing batch retention")
					ids[i] = posts[i].ID
				}
				defer func() { _ = store.DeletePosts(ctx, ids) }()

				// Insert all 10 at once with retain limit 5
				err := store.MarkViewed(ctx, user.ID, ids, retain)
				tc.AssertNoError(err, "batch mark viewed")

				count, err := store.CountUserViews(ctx, user.ID)
				tc.AssertNoError(err, "count user views")
				tc.AssertEqual(retain, count, "views count must be pruned to retain limit 5")
			},
		},
	}
}
