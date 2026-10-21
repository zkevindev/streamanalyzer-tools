package metrics

import "testing"

func TestQuantile(t *testing.T) {
	vals := make([]float64, 100)
	for i := range vals {
		vals[i] = float64(i + 1)
	}
	if got := Quantile(vals, 0.05); got != 5 {
		t.Fatalf("p05 = %v", got)
	}
	if got := Quantile(vals, 0.5); got != 50 {
		t.Fatalf("p50 = %v", got)
	}
	three := []float64{86034, 21347, 58839}
	if got := Quantile(three, 0.05); got != 21347 {
		t.Fatalf("worst of three = %v", got)
	}
	if got := Quantile(three, 0.5); got != 58839 {
		t.Fatalf("median of three = %v", got)
	}
	if Quantile(nil, 0.5) != 0 {
		t.Fatal("empty")
	}
}
