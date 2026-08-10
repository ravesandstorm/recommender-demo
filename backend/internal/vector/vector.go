package vector

const Dim = 384

func Zero(dim int) []float32 {
	return make([]float32, dim)
}

// AddScaled returns a + weight*b (same length).
func AddScaled(a []float32, weight float64, b []float32) []float32 {
	out := make([]float32, len(a))
	w := float32(weight)
	for i := range a {
		out[i] = a[i] + w*b[i]
	}
	return out
}

// SubScaled returns a - weight*b.
func SubScaled(a []float32, weight float64, b []float32) []float32 {
	return AddScaled(a, -weight, b)
}

func IsZero(v []float32) bool {
	for _, x := range v {
		if x != 0 {
			return false
		}
	}
	return true
}
