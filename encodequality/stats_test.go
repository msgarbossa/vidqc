package encodequality

import "testing"

func TestComputePooledStats(t *testing.T) {
	stats := ComputePooledStats([]float64{80, 90, 100})
	if stats.Mean != 90 {
		t.Errorf("Mean = %v, want 90", stats.Mean)
	}
	if stats.Min != 80 {
		t.Errorf("Min = %v, want 80", stats.Min)
	}
	if stats.Max != 100 {
		t.Errorf("Max = %v, want 100", stats.Max)
	}
	// harmonic mean of 80,90,100 = 3 / (1/80+1/90+1/100) ~= 89.256
	if stats.HarmonicMean < 89.2 || stats.HarmonicMean > 89.3 {
		t.Errorf("HarmonicMean = %v, want ~89.256", stats.HarmonicMean)
	}
}

func TestComputePooledStats_ZeroDragsHarmonicToZero(t *testing.T) {
	stats := ComputePooledStats([]float64{80, 90, 0})
	if stats.HarmonicMean != 0 {
		t.Errorf("HarmonicMean = %v, want 0 when any value is 0", stats.HarmonicMean)
	}
	if stats.Mean == 0 {
		t.Errorf("Mean should not be affected by the zero-harmonic special case")
	}
}

func TestComputePooledStats_Empty(t *testing.T) {
	if stats := ComputePooledStats(nil); stats != (PooledStat{}) {
		t.Errorf("ComputePooledStats(nil) = %+v, want zero value", stats)
	}
}

func TestWithSpread(t *testing.T) {
	vals := make([]float64, 200)
	for i := range vals {
		vals[i] = 90
	}
	vals[0], vals[1], vals[2] = 10, 50, 60 // a lone collapse plus two dips
	s := ComputePooledStats(vals)
	if s.N != 200 {
		t.Errorf("N = %d, want 200", s.N)
	}
	if s.Min != 10 || s.P1 != 50 || s.P5 != 90 || s.Median != 90 {
		t.Errorf("min/p1/p5/median = %v/%v/%v/%v, want 10/50/90/90", s.Min, s.P1, s.P5, s.Median)
	}
	if s.StdDev < 6.5 || s.StdDev > 6.9 {
		t.Errorf("StdDev = %v, want ~6.7", s.StdDev)
	}

	// Pooled numbers from libvmaf are kept; only the spread is added.
	lv := PooledStat{Mean: 50, Min: 1, Max: 99, HarmonicMean: 7}.WithSpread([]float64{40, 60})
	if lv.Mean != 50 || lv.Min != 1 || lv.HarmonicMean != 7 || lv.StdDev != 10 {
		t.Errorf("WithSpread changed pooled fields or miscomputed: %+v", lv)
	}
}
