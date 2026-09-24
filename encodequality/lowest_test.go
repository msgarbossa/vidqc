package encodequality

import "testing"

func TestLowestMoments_SpacedWorstFirst(t *testing.T) {
	frames := baseline(60) // 1 sample/second, VMAF 95
	frames[10].VMAF = 70   // one hard scene: three adjacent low frames
	frames[11].VMAF = 71
	frames[12].VMAF = 72
	frames[40].VMAF = 80
	frames[50].VMAF = 85

	got := LowestMoments(frames, 3)
	want := []int{10, 40, 50} // 11 and 12 are within 2s of 10: the same place
	if len(got) != len(want) {
		t.Fatalf("got %d moments, want %d: %+v", len(got), len(want), got)
	}
	for i, f := range got {
		if f.Frame != want[i] {
			t.Errorf("moment %d = frame %d, want %d", i, f.Frame, want[i])
		}
	}
}

// A uniformly clean run flags no problem area, but still has places to look.
func TestLowestMoments_NoThreshold(t *testing.T) {
	frames := baseline(20)
	if got := LowestMoments(frames, 5); len(got) != 5 {
		t.Errorf("got %d moments on a flat run, want 5", len(got))
	}
	if got := LowestMoments(frames, 0); got != nil {
		t.Errorf("n=0 returned %+v", got)
	}
	if got := LowestMoments(baseline(3), 5); len(got) != 2 {
		t.Errorf("samples at 0,1,2s: got %d moments, want 2 (0s and 2s)", len(got))
	}
}

// On a long file the gap widens so the list spreads across it.
func TestLowestMoments_LongFileGap(t *testing.T) {
	frames := baseline(7200) // two hours at 1 sample/second
	for i := 1000; i < 1100; i++ {
		frames[i].VMAF = 60 + float64(i-1000)*0.01 // one bad 100s stretch
	}
	frames[5000].VMAF = 90
	got := LowestMoments(frames, 5)
	if len(got) < 2 || got[1].Frame != 5000 {
		t.Errorf("second moment should leave the bad stretch (gap 360s), got %+v", got)
	}
}
