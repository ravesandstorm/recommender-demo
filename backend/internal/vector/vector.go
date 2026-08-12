package vector

import "math"

const Dim = 384

// normEpsilon: below this squared L2, treat the vector as zero.
const normEpsilon = 1e-12

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

// L2Normalize returns a unit-length copy of v. Near-zero inputs become a zero vector.
func L2Normalize(v []float32) []float32 {
	var sumSq float64
	for _, x := range v {
		sumSq += float64(x) * float64(x)
	}
	if sumSq < normEpsilon {
		return Zero(len(v))
	}
	inv := float32(1 / math.Sqrt(sumSq))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x * inv
	}
	return out
}

func IsZero(v []float32) bool {
	for _, x := range v {
		if x != 0 {
			return false
		}
	}
	return true
}
