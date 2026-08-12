package vector_test

import (
	"math"
	"testing"

	"github.com/recsys/backend/internal/vector"
	"github.com/stretchr/testify/require"
)

func TestAddSubScaled(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{0.5, 0.5, 0.5}
	sum := vector.AddScaled(a, 2, b)
	require.Equal(t, []float32{2, 3, 4}, sum)
	back := vector.SubScaled(sum, 2, b)
	require.Equal(t, a, back)
	require.True(t, vector.IsZero(vector.Zero(3)))
	require.False(t, vector.IsZero(a))
}

func TestL2Normalize(t *testing.T) {
	v := []float32{3, 4}
	n := vector.L2Normalize(v)
	require.InDelta(t, 1.0, math.Sqrt(float64(n[0]*n[0]+n[1]*n[1])), 1e-6)
	require.InDelta(t, 0.6, float64(n[0]), 1e-6)
	require.InDelta(t, 0.8, float64(n[1]), 1e-6)
	require.Equal(t, []float32{3, 4}, v)
	require.True(t, vector.IsZero(vector.L2Normalize(vector.Zero(4))))
	require.True(t, vector.IsZero(vector.L2Normalize([]float32{1e-8, -1e-8})))
}

func TestCosine(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	require.InDelta(t, 0.0, vector.Cosine(a, b), 1e-6)
	require.InDelta(t, 1.0, vector.Cosine(a, a), 1e-6)
}

func TestApplyInterestSpawnsAndUpdates(t *testing.T) {
	v1 := vector.L2Normalize([]float32{1, 0, 0})
	v2 := vector.L2Normalize([]float32{0, 1, 0})

	var interests []vector.Interest
	interests = vector.ApplyInterest(interests, v1, 1.0, 3, 0.55)
	require.Len(t, interests, 1)
	require.InDelta(t, 1.0, interests[0].Weight, 1e-6)

	// Far vector should spawn a second interest.
	interests = vector.ApplyInterest(interests, v2, 1.5, 3, 0.55)
	require.Len(t, interests, 2)

	// Near v1 should update first centroid, not spawn.
	near := vector.L2Normalize([]float32{0.99, 0.01, 0})
	interests = vector.ApplyInterest(interests, near, 1.0, 3, 0.55)
	require.Len(t, interests, 2)
	require.True(t, vector.HasInterests(interests))
	require.False(t, vector.IsZero(vector.BlendInterests(interests, 3)))
}

func TestAllocateQuotas(t *testing.T) {
	q := vector.AllocateQuotas([]float64{3, 1}, 10)
	require.Equal(t, 10, q[0]+q[1])
	require.Greater(t, q[0], q[1])
	require.GreaterOrEqual(t, q[1], 1)
}

func TestMMRPrefersDiversity(t *testing.T) {
	// Two near-duplicates high relevance, one diverse lower relevance.
	cands := []vector.MMRCandidate{
		{ID: "a", Relevance: 1.0, Vector: []float32{1, 0}},
		{ID: "b", Relevance: 0.95, Vector: []float32{0.99, 0.01}},
		{ID: "c", Relevance: 0.6, Vector: []float32{0, 1}},
	}
	// Pure relevance would pick a,b. With lower lambda, MMR should pick a,c.
	picked := vector.MMR(cands, 2, 0.5)
	require.Len(t, picked, 2)
	ids := map[string]bool{picked[0].ID: true, picked[1].ID: true}
	require.True(t, ids["a"])
	require.True(t, ids["c"], "MMR should prefer diverse c over near-duplicate b")
}
