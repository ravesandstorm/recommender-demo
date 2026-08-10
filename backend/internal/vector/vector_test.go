package vector_test

import (
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
