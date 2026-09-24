package encodequality

import (
	"math"
	"sort"
)

// ComputePooledStats computes mean/min/max/harmonic-mean directly from raw
// per-frame values -- for combining frames from multiple separate libvmaf
// runs (e.g. one per matched segment in an aligned comparison), where
// libvmaf's own per-run pooled_metrics can't simply be averaged together.
// Harmonic mean is 0 if any value is <= 0, matching harmonic mean's actual
// limit behavior as a term approaches zero, rather than skipping it (which
// would silently understate how much a single bad frame should matter).
// The spread fields are filled too (see WithSpread).
func ComputePooledStats(values []float64) PooledStat {
	if len(values) == 0 {
		return PooledStat{}
	}
	min, max := values[0], values[0]
	var sum float64
	harmonicValid := true
	for _, v := range values {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
		sum += v
		if v <= 0 {
			harmonicValid = false
		}
	}
	mean := sum / float64(len(values))

	var harmonic float64
	if harmonicValid {
		var recip float64
		for _, v := range values {
			recip += 1 / v
		}
		harmonic = float64(len(values)) / recip
	}

	return PooledStat{Mean: mean, Min: min, Max: max, HarmonicMean: harmonic}.WithSpread(values)
}

// WithSpread returns s with its standard deviation, percentiles, median and
// sample count computed from values -- the frames s summarizes. The
// deviation is taken about s.Mean, so a stat whose pooled numbers came from
// libvmaf keeps them and gains only the spread.
func (s PooledStat) WithSpread(values []float64) PooledStat {
	s.N = len(values)
	if s.N == 0 {
		return s
	}
	var sq float64
	for _, v := range values {
		sq += (v - s.Mean) * (v - s.Mean)
	}
	s.StdDev = math.Sqrt(sq / float64(s.N))

	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	s.P1 = percentile(sorted, 1)
	s.P5 = percentile(sorted, 5)
	s.Median = percentile(sorted, 50)
	return s
}

// percentile is the nearest-rank p-th percentile of an ascending slice: the
// smallest value with at least p% of the samples at or below it. With fewer
// than 100 samples P1 is simply the minimum, which is honest -- there is no
// finer tail to report.
func percentile(sorted []float64, p float64) float64 {
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	return sorted[rank-1]
}
