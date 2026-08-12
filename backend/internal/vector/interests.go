package vector

// Interest is one preference centroid with an accumulated mass weight.
type Interest struct {
	Vector []float32 `json:"vector"`
	Weight float64   `json:"weight"`
}

const weightEpsilon = 1e-6

// ApplyInterest updates the multi-interest profile with ±weight * postVec (then L2-normalizes).
// Positive signals create a new centroid when far from all existing ones and K is not full.
// Negative signals only nudge the closest centroid (no new interest).
func ApplyInterest(interests []Interest, postVec []float32, weight float64, maxK int, simThreshold float64) []Interest {
	if len(postVec) == 0 || weight == 0 || maxK <= 0 {
		return interests
	}
	postUnit := L2Normalize(postVec)
	if IsZero(postUnit) {
		return interests
	}

	bestIdx := -1
	bestSim := -2.0
	for i, it := range interests {
		if IsZero(it.Vector) {
			continue
		}
		sim := Cosine(it.Vector, postUnit)
		if sim > bestSim {
			bestSim = sim
			bestIdx = i
		}
	}

	// Spawn a new interest for positive evidence that is far from existing centroids.
	if weight > 0 && (len(interests) == 0 || (bestSim < simThreshold && len(interests) < maxK)) {
		out := make([]Interest, len(interests)+1)
		copy(out, interests)
		out[len(interests)] = Interest{Vector: postUnit, Weight: weight}
		return out
	}

	if bestIdx < 0 {
		// Negative signal with no interests: ignore.
		return interests
	}

	cur := interests[bestIdx]
	nextVec := L2Normalize(AddScaled(cur.Vector, weight, postUnit))
	nextW := cur.Weight + weight
	if nextW < weightEpsilon || IsZero(nextVec) {
		out := make([]Interest, 0, len(interests)-1)
		out = append(out, interests[:bestIdx]...)
		out = append(out, interests[bestIdx+1:]...)
		return out
	}

	out := make([]Interest, len(interests))
	copy(out, interests)
	out[bestIdx] = Interest{Vector: nextVec, Weight: nextW}
	return out
}

// BlendInterests returns an L2-normalized weighted sum of centroids (legacy single-vector view).
func BlendInterests(interests []Interest, dim int) []float32 {
	if len(interests) == 0 {
		return Zero(dim)
	}
	acc := Zero(dim)
	for _, it := range interests {
		if it.Weight <= 0 || IsZero(it.Vector) {
			continue
		}
		acc = AddScaled(acc, it.Weight, it.Vector)
	}
	return L2Normalize(acc)
}

// HasInterests reports whether the profile has at least one usable centroid.
func HasInterests(interests []Interest) bool {
	for _, it := range interests {
		if it.Weight > weightEpsilon && !IsZero(it.Vector) {
			return true
		}
	}
	return false
}

// AllocateQuotas distributes total slots across interests proportional to weight (min 1 if weight > 0).
func AllocateQuotas(weights []float64, total int) []int {
	n := len(weights)
	out := make([]int, n)
	if n == 0 || total <= 0 {
		return out
	}
	var sum float64
	active := 0
	for _, w := range weights {
		if w > weightEpsilon {
			sum += w
			active++
		}
	}
	if active == 0 {
		return out
	}
	if total < active {
		// Give one slot to the strongest interests first.
		type idxW struct {
			i int
			w float64
		}
		order := make([]idxW, 0, active)
		for i, w := range weights {
			if w > weightEpsilon {
				order = append(order, idxW{i, w})
			}
		}
		for i := 0; i < len(order); i++ {
			for j := i + 1; j < len(order); j++ {
				if order[j].w > order[i].w {
					order[i], order[j] = order[j], order[i]
				}
			}
		}
		for k := 0; k < total && k < len(order); k++ {
			out[order[k].i] = 1
		}
		return out
	}

	assigned := 0
	for i, w := range weights {
		if w <= weightEpsilon {
			continue
		}
		q := int((w / sum) * float64(total))
		if q < 1 {
			q = 1
		}
		out[i] = q
		assigned += q
	}
	// Fix over/under assignment on the largest weight.
	largest := 0
	for i, w := range weights {
		if w > weights[largest] {
			largest = i
		}
	}
	for assigned > total {
		if out[largest] > 1 {
			out[largest]--
			assigned--
		} else {
			break
		}
	}
	for assigned < total {
		out[largest]++
		assigned++
	}
	return out
}
