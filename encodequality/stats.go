package encodequality

// ComputePooledStats computes mean/min/max/harmonic-mean directly from raw
// per-frame values -- for combining frames from multiple separate libvmaf
// runs (e.g. one per matched segment in an aligned comparison), where
// libvmaf's own per-run pooled_metrics can't simply be averaged together.
// Harmonic mean is 0 if any value is <= 0, matching harmonic mean's actual
// limit behavior as a term approaches zero, rather than skipping it (which
// would silently understate how much a single bad frame should matter).
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

	return PooledStat{Mean: mean, Min: min, Max: max, HarmonicMean: harmonic}
}
