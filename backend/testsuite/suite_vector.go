package testsuite

import (
	"math"

	"github.com/recsys/backend/internal/vector"
)

// VectorSuite returns all unit tests for the vector math and multi-interest engine.
func VectorSuite() []TestCase {
	return []TestCase{
		{
			Name:        "Vector/AddSubScaledLinearity",
			Scope:       ScopeUnitVector,
			Description: "Asserts AddScaled and SubScaled mathematical inversion and linearity",
			Fn: func(tc *TestContext) {
				a := []float32{1.0, 2.0, 3.0}
				b := []float32{0.5, 0.5, 0.5}
				sum := vector.AddScaled(a, 2.0, b)
				tc.Assert(len(sum) == 3, "sum length must be 3")
				tc.Assert(math.Abs(float64(sum[0]-2.0)) < 1e-6, "sum[0] should be 2.0")
				tc.Assert(math.Abs(float64(sum[1]-3.0)) < 1e-6, "sum[1] should be 3.0")
				tc.Assert(math.Abs(float64(sum[2]-4.0)) < 1e-6, "sum[2] should be 4.0")

				// Reverse operation
				back := vector.SubScaled(sum, 2.0, b)
				tc.Assert(math.Abs(float64(back[0]-a[0])) < 1e-6, "back[0] should equal original a[0]")
				tc.Assert(math.Abs(float64(back[1]-a[1])) < 1e-6, "back[1] should equal original a[1]")
				tc.Assert(math.Abs(float64(back[2]-a[2])) < 1e-6, "back[2] should equal original a[2]")
			},
		},
		{
			Name:        "Vector/L2NormalizeUnitLength",
			Scope:       ScopeUnitVector,
			Description: "Asserts L2 normalization produces exact unit length for 2D and 384D vectors",
			Fn: func(tc *TestContext) {
				v := []float32{3.0, 4.0}
				norm := vector.L2Normalize(v)
				mag := math.Sqrt(float64(norm[0]*norm[0] + norm[1]*norm[1]))
				tc.Assert(math.Abs(mag-1.0) < 1e-6, "magnitude of normalized vector must be 1.0")
				tc.Assert(math.Abs(float64(norm[0])-0.6) < 1e-6, "norm[0] must be 0.6")
				tc.Assert(math.Abs(float64(norm[1])-0.8) < 1e-6, "norm[1] must be 0.8")

				// 384-dimensional vector
				dim384 := make([]float32, 384)
				for i := range dim384 {
					dim384[i] = float32(i + 1)
				}
				n384 := vector.L2Normalize(dim384)
				var sumSq float64
				for _, val := range n384 {
					sumSq += float64(val) * float64(val)
				}
				tc.Assert(math.Abs(math.Sqrt(sumSq)-1.0) < 1e-5, "384D norm magnitude must be 1.0")
			},
		},
		{
			Name:        "Vector/CosineOrthogonalAndIdentical",
			Scope:       ScopeUnitVector,
			Description: "Asserts Cosine similarity is 0 for orthogonal, 1 for identical, and -1 for opposite",
			Fn: func(tc *TestContext) {
				orthoA := []float32{1.0, 0.0}
				orthoB := []float32{0.0, 1.0}
				simOrtho := vector.Cosine(orthoA, orthoB)
				tc.Assert(math.Abs(simOrtho) < 1e-6, "orthogonal cosine must be 0.0")

				simSelf := vector.Cosine(orthoA, orthoA)
				tc.Assert(math.Abs(simSelf-1.0) < 1e-6, "self cosine must be 1.0")

				oppB := []float32{-1.0, 0.0}
				simOpp := vector.Cosine(orthoA, oppB)
				tc.Assert(math.Abs(simOpp+1.0) < 1e-6, "opposite cosine must be -1.0")
			},
		},
		{
			Name:        "Vector/ApplyInterestSpawnAndDecay",
			Scope:       ScopeUnitVector,
			Description: "Asserts multi-interest centroid spawning for divergent vectors and shifting for nearby vectors",
			Fn: func(tc *TestContext) {
				v1 := vector.L2Normalize([]float32{1, 0, 0})
				v2 := vector.L2Normalize([]float32{0, 1, 0})

				var interests []vector.Interest
				// 1. First positive signal spawns centroid 1
				interests = vector.ApplyInterest(interests, v1, 1.0, 3, 0.55)
				tc.Assert(len(interests) == 1, "first signal should spawn exactly 1 interest")
				tc.Assert(math.Abs(interests[0].Weight-1.0) < 1e-6, "weight should be 1.0")

				// 2. Orthogonal signal spawns centroid 2 (sim 0.0 < 0.55 threshold)
				interests = vector.ApplyInterest(interests, v2, 1.5, 3, 0.55)
				tc.Assert(len(interests) == 2, "orthogonal signal should spawn 2nd interest")

				// 3. Near signal to v1 updates centroid 1 without spawning
				nearV1 := vector.L2Normalize([]float32{0.99, 0.01, 0})
				interests = vector.ApplyInterest(interests, nearV1, 0.8, 3, 0.55)
				tc.Assert(len(interests) == 2, "near signal should update existing centroid, not spawn")
				tc.Assert(interests[0].Weight > 1.0, "centroid 1 weight should increase")

				// 4. HasInterests & BlendInterests
				tc.Assert(vector.HasInterests(interests), "profile must have active interests")
				blend := vector.BlendInterests(interests, 3)
				tc.Assert(!vector.IsZero(blend), "blended vector must not be zero")
			},
		},
		{
			Name:        "Vector/CollinearCentroidsCollapse",
			Scope:       ScopeUnitVector,
			EdgeCase:    EdgeCollinearInterests,
			Description: "Asserts ShouldCollapseInterests detects when multi-interests can be merged into a single query",
			Fn: func(tc *TestContext) {
				a := vector.L2Normalize([]float32{1, 0, 0})
				b := vector.L2Normalize([]float32{0.99, 0.01, 0}) // cosine ~ 0.999
				c := vector.L2Normalize([]float32{0, 1, 0})       // cosine = 0.0

				tc.Assert(vector.ShouldCollapseInterests(nil, 0.9), "nil profile should collapse to single query")
				tc.Assert(vector.ShouldCollapseInterests([]vector.Interest{{Vector: a, Weight: 1}}, 0.9), "single interest should collapse")

				// Collinear pair (a and b)
				collinear := []vector.Interest{
					{Vector: a, Weight: 1.0},
					{Vector: b, Weight: 1.0},
				}
				tc.Assert(vector.ShouldCollapseInterests(collinear, 0.9), "collinear centroids must collapse")

				// Divergent pair (a and c)
				divergent := []vector.Interest{
					{Vector: a, Weight: 1.0},
					{Vector: c, Weight: 1.0},
				}
				tc.Assert(!vector.ShouldCollapseInterests(divergent, 0.9), "divergent centroids must NOT collapse")
			},
		},
		{
			Name:        "Vector/QuotaAllocationProportionality",
			Scope:       ScopeUnitVector,
			Description: "Asserts AllocateQuotas divides query slots strictly according to centroid weights",
			Fn: func(tc *TestContext) {
				// 75% mass to interest 1, 25% to interest 2, total 20 slots
				weights := []float64{3.0, 1.0}
				quotas := vector.AllocateQuotas(weights, 20)
				tc.Assert(len(quotas) == 2, "quotas length must match weights")
				tc.Assert(quotas[0]+quotas[1] == 20, "sum of quotas must equal total slots")
				tc.Assert(quotas[0] >= 14 && quotas[0] <= 16, "interest 1 quota should be ~15")
				tc.Assert(quotas[1] >= 4 && quotas[1] <= 6, "interest 2 quota should be ~5")

				// Edge: total slots smaller than active interests
				smallQuotas := vector.AllocateQuotas([]float64{10.0, 1.0, 0.5}, 2)
				tc.Assert(smallQuotas[0]+smallQuotas[1]+smallQuotas[2] <= 2, "must not exceed total")
				tc.Assert(smallQuotas[0] == 1, "strongest interest must get slot first")
			},
		},
		{
			Name:        "Vector/MMRDiversityAndRelevanceBalance",
			Scope:       ScopeUnitVector,
			Description: "Asserts MMR algorithm balances relevance score and cosine diversity",
			Fn: func(tc *TestContext) {
				cands := []vector.MMRCandidate{
					{ID: "cand_a", Relevance: 1.00, Vector: []float32{1, 0}},
					{ID: "cand_b", Relevance: 0.98, Vector: []float32{0.999, 0.001}}, // Duplicate of A
					{ID: "cand_c", Relevance: 0.70, Vector: []float32{0, 1}},         // Orthogonal to A
				}

				// Lambda = 0.5 (balanced): should pick cand_a, then diverse cand_c over near-duplicate cand_b
				picked := vector.MMR(cands, 2, 0.5)
				tc.Assert(len(picked) == 2, "picked count must be 2")
				tc.Assert(picked[0].ID == "cand_a", "1st pick must be cand_a (highest relevance)")
				tc.Assert(picked[1].ID == "cand_c", "2nd pick must be diverse cand_c instead of redundant cand_b")

				// Lambda = 1.0 (pure relevance): should pick cand_a and cand_b
				pureRel := vector.MMR(cands, 2, 1.0)
				tc.Assert(pureRel[0].ID == "cand_a" && pureRel[1].ID == "cand_b", "pure relevance must select cand_a and cand_b")
			},
		},
	}
}
