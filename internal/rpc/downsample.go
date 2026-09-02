package rpc

import (
	"sort"

	"github.com/bcypher01/assay/internal/core"
)

// ViewportPoint retains high and low separately, so a visual extreme cannot be
// discarded simply because its closing price was ordinary.
type ViewportPoint struct {
	Time             core.Time
	Low, High, Close core.Price
}

// DownsampleViewport bounds a chart response while preserving the first/last
// bars and the high and low extrema of every interior bucket.
func DownsampleViewport(s *core.Series, maxPoints int) []ViewportPoint {
	if s == nil || s.Len() == 0 || maxPoints <= 0 {
		return nil
	}
	n := s.Len()
	if n <= maxPoints {
		return viewportRange(s, 0, n)
	}
	if maxPoints == 1 {
		return []ViewportPoint{viewportAt(s, 0)}
	}
	if maxPoints == 2 {
		return []ViewportPoint{viewportAt(s, 0), viewportAt(s, n-1)}
	}
	buckets := (maxPoints - 2) / 2
	if buckets == 0 {
		return []ViewportPoint{viewportAt(s, 0), viewportAt(s, n-1)}
	}
	out := make([]ViewportPoint, 0, maxPoints)
	out = append(out, viewportAt(s, 0))
	interior := n - 2
	for b := 0; b < buckets; b++ {
		from, to := 1+b*interior/buckets, 1+(b+1)*interior/buckets
		if from >= to {
			continue
		}
		lo, hi := from, from
		for i := from + 1; i < to; i++ {
			if s.Low[i] < s.Low[lo] {
				lo = i
			}
			if s.High[i] > s.High[hi] {
				hi = i
			}
		}
		switch {
		case lo == hi:
			out = append(out, viewportAt(s, lo))
		case lo < hi:
			out = append(out, viewportAt(s, lo), viewportAt(s, hi))
		default:
			out = append(out, viewportAt(s, hi), viewportAt(s, lo))
		}
	}
	out = append(out, viewportAt(s, n-1))
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time < out[j].Time })
	return out
}
func viewportRange(s *core.Series, from, to int) []ViewportPoint {
	out := make([]ViewportPoint, 0, to-from)
	for i := from; i < to; i++ {
		out = append(out, viewportAt(s, i))
	}
	return out
}
func viewportAt(s *core.Series, i int) ViewportPoint {
	return ViewportPoint{Time: s.Time[i], Low: s.Low[i], High: s.High[i], Close: s.Close[i]}
}
