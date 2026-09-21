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
