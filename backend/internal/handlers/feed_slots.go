package handlers

import "github.com/google/uuid"

// feedSlots returns exploit / soft-explore / hard-explore counts for a ~70/20/10 mix.
// For FEED_LIMIT >= 5, soft and hard are at least 1 each.
func feedSlots(limit int) (exploit, soft, hard int) {
	if limit <= 0 {
		return 0, 0, 0
	}
	soft = (limit * 20) / 100
	hard = (limit * 10) / 100
	if limit >= 5 {
		if soft == 0 {
			soft = 1
		}
		if hard == 0 {
			hard = 1
		}
	}
	exploit = limit - soft - hard
	for exploit < 0 && soft > 0 {
		soft--
		exploit++
	}
	for exploit < 0 && hard > 0 {
		hard--
		exploit++
	}
	return exploit, soft, hard
}

// pickFromFront takes up to n unused IDs from the start of ranked (highest similarity).
func pickFromFront(ranked []uuid.UUID, n int, used map[uuid.UUID]struct{}) []uuid.UUID {
	if n <= 0 {
		return nil
	}
	out := make([]uuid.UUID, 0, n)
	for _, id := range ranked {
		if len(out) >= n {
			break
		}
		if _, ok := used[id]; ok {
			continue
		}
		used[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// pickSoftExplore takes from the lower-similarity half of ranked (soft explore band).
// Falls back upward if the lower band is exhausted.
func pickSoftExplore(ranked []uuid.UUID, n int, used map[uuid.UUID]struct{}) []uuid.UUID {
	if n <= 0 || len(ranked) == 0 {
		return nil
	}
	start := len(ranked) / 2
	out := make([]uuid.UUID, 0, n)
	for i := start; i < len(ranked) && len(out) < n; i++ {
		id := ranked[i]
		if _, ok := used[id]; ok {
			continue
		}
		used[id] = struct{}{}
		out = append(out, id)
	}
	for i := start - 1; i >= 0 && len(out) < n; i-- {
		id := ranked[i]
		if _, ok := used[id]; ok {
			continue
		}
		used[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
