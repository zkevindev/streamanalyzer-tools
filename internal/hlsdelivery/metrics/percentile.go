package metrics

import (
	"math"
	"sort"
)

// Quantile 用最近秩。p 取 0 到 1。
// 吞吐的最差 5% 传 0.05。三个样本时这个值就是最小值。
func Quantile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	s := append([]float64(nil), values...)
	sort.Float64s(s)
	if p <= 0 {
		return s[0]
	}
	if p >= 1 {
		return s[len(s)-1]
	}
	rank := int(math.Ceil(p * float64(len(s))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(s) {
		rank = len(s)
	}
	return s[rank-1]
}
