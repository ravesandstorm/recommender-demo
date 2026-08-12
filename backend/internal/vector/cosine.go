package vector

import "math"

// Cosine returns cosine similarity. Near-zero vectors yield 0.
func Cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		fa := float64(a[i])
		fb := float64(b[i])
		dot += fa * fb
		na += fa * fa
		nb += fb * fb
	}
	if na < normEpsilon || nb < normEpsilon {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
