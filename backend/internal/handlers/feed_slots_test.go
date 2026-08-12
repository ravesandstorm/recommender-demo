package handlers

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestFeedSlots(t *testing.T) {
	e, s, h := feedSlots(5)
	require.Equal(t, 3, e)
	require.Equal(t, 1, s)
	require.Equal(t, 1, h)
	require.Equal(t, 5, e+s+h)

	e, s, h = feedSlots(10)
	require.Equal(t, 7, e)
	require.Equal(t, 2, s)
	require.Equal(t, 1, h)

	e, s, h = feedSlots(1)
	require.Equal(t, 1, e)
	require.Equal(t, 0, s)
	require.Equal(t, 0, h)

	e, s, h = feedSlots(0)
	require.Equal(t, 0, e+s+h)
}

func TestPickSoftExplore(t *testing.T) {
	ids := make([]uuid.UUID, 10)
	for i := range ids {
		ids[i] = uuid.New()
	}
	used := map[uuid.UUID]struct{}{}
	exploit := pickFromFront(ids, 3, used)
	require.Len(t, exploit, 3)

	soft := pickSoftExplore(ids, 2, used)
	require.Len(t, soft, 2)

	lower := map[uuid.UUID]struct{}{}
	for _, id := range ids[5:] {
		lower[id] = struct{}{}
	}
	for _, id := range soft {
		_, ok := lower[id]
		require.True(t, ok, "soft pick should be from lower half")
	}
}
