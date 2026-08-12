package vector

// MMRCandidate is one retrieval hit with a relevance score and embedding.
type MMRCandidate struct {
	ID        string
	Relevance float64
	Vector    []float32
}

// MMR selects up to k items maximizing λ*relevance − (1−λ)*max_sim_to_selected.
// Higher λ favors relevance; lower λ favors diversity. λ in [0,1].
func MMR(cands []MMRCandidate, k int, lambda float64) []MMRCandidate {
	if k <= 0 || len(cands) == 0 {
		return nil
	}
	if k > len(cands) {
		k = len(cands)
	}
	if lambda < 0 {
		lambda = 0
	}
	if lambda > 1 {
		lambda = 1
	}

	selected := make([]MMRCandidate, 0, k)
	used := make([]bool, len(cands))

	for len(selected) < k {
		bestIdx := -1
		bestScore := mathInfNeg
		for i, c := range cands {
			if used[i] {
				continue
			}
			maxSim := 0.0
			for _, s := range selected {
				sim := Cosine(c.Vector, s.Vector)
				if sim > maxSim {
					maxSim = sim
				}
			}
			score := lambda*c.Relevance - (1-lambda)*maxSim
			if bestIdx < 0 || score > bestScore {
				bestScore = score
				bestIdx = i
			}
		}
		if bestIdx < 0 {
			break
		}
		used[bestIdx] = true
		selected = append(selected, cands[bestIdx])
	}
	return selected
}

const mathInfNeg = -1e18
