package encodequality

import (
	"strings"
	"testing"
)

func baseline(n int) []FrameMetric {
	frames := make([]FrameMetric, n)
	for i := range frames {
		frames[i] = FrameMetric{Frame: i, Time: float64(i), VMAF: 95, PSNR: 42, SSIM: 0.99}
	}
	return frames
}

func TestDetectProblemAreas_NoDips(t *testing.T) {
	frames := baseline(20)
	vmaf := PooledStat{Mean: 95}
	psnr := PooledStat{Mean: 42}
	ssim := PooledStat{Mean: 0.99}

	areas := DetectProblemAreas(frames, vmaf, psnr, ssim, 3)
	if len(areas) != 0 {
		t.Fatalf("got %d problem areas for a flat clean run, want 0", len(areas))
	}
}

func TestDetectProblemAreas_MergesAndRanks(t *testing.T) {
	frames := baseline(20)

	// A genuinely hard scene: all three metrics dip together.
	frames[5] = FrameMetric{Frame: 5, Time: 5, VMAF: 70, PSNR: 30, SSIM: 0.90}
	frames[6] = FrameMetric{Frame: 6, Time: 6, VMAF: 68, PSNR: 29, SSIM: 0.89}
	frames[7] = FrameMetric{Frame: 7, Time: 7, VMAF: 71, PSNR: 31, SSIM: 0.91}

	// A likely alignment glitch: VMAF collapses, PSNR/SSIM barely move.
	frames[12] = FrameMetric{Frame: 12, Time: 12, VMAF: 0, PSNR: 41, SSIM: 0.985}
	frames[13] = FrameMetric{Frame: 13, Time: 13, VMAF: 0, PSNR: 41, SSIM: 0.985}

	vmaf := PooledStat{Mean: 95}
	psnr := PooledStat{Mean: 42}
	ssim := PooledStat{Mean: 0.99}

	areas := DetectProblemAreas(frames, vmaf, psnr, ssim, 3)
	if len(areas) != 2 {
		t.Fatalf("got %d problem areas, want 2 (%+v)", len(areas), areas)
	}

	// Higher severity (the collapse) ranks first.
	collapse, hard := areas[0], areas[1]

	if collapse.StartFrame != 12 || collapse.EndFrame != 13 {
		t.Errorf("collapse segment frames = [%d,%d], want [12,13]", collapse.StartFrame, collapse.EndFrame)
	}
	if len(collapse.Triggers) != 1 || collapse.Triggers[0].Metric != "VMAF" {
		t.Errorf("collapse segment triggers = %+v, want only VMAF", collapse.Triggers)
	}
	if !strings.Contains(collapse.Note, "collapsed") {
		t.Errorf("collapse segment note = %q, want it to flag the collapse pattern", collapse.Note)
	}

	if hard.StartFrame != 5 || hard.EndFrame != 7 {
		t.Errorf("hard-scene segment frames = [%d,%d], want [5,7]", hard.StartFrame, hard.EndFrame)
	}
	if len(hard.Triggers) != 3 {
		t.Errorf("hard-scene segment triggers = %+v, want all 3 metrics", hard.Triggers)
	}
	if !strings.Contains(hard.Note, "dropped together") {
		t.Errorf("hard-scene segment note = %q, want it to flag the joint-drop pattern", hard.Note)
	}

	if collapse.Severity <= hard.Severity {
		t.Errorf("collapse severity %v should rank above hard-scene severity %v", collapse.Severity, hard.Severity)
	}
}

func TestDetectProblemAreas_RespectsTop(t *testing.T) {
	frames := baseline(20)
	frames[5] = FrameMetric{Frame: 5, Time: 5, VMAF: 70, PSNR: 30, SSIM: 0.90}
	frames[12] = FrameMetric{Frame: 12, Time: 12, VMAF: 0, PSNR: 41, SSIM: 0.985}

	areas := DetectProblemAreas(frames, PooledStat{Mean: 95}, PooledStat{Mean: 42}, PooledStat{Mean: 0.99}, 1)
	if len(areas) != 1 {
		t.Fatalf("got %d areas with top=1, want 1", len(areas))
	}
	if areas[0].StartFrame != 12 {
		t.Errorf("top=1 kept frame %d, want the higher-severity collapse at frame 12", areas[0].StartFrame)
	}
}

func TestDetectProblemAreas_PixelOnlyDip(t *testing.T) {
	frames := baseline(10)
	// PSNR/SSIM dip but VMAF barely moves -- imperceptible-noise case.
	frames[4] = FrameMetric{Frame: 4, Time: 4, VMAF: 94, PSNR: 30, SSIM: 0.90}

	areas := DetectProblemAreas(frames, PooledStat{Mean: 95}, PooledStat{Mean: 42}, PooledStat{Mean: 0.99}, 3)
	if len(areas) != 1 {
		t.Fatalf("got %d areas, want 1", len(areas))
	}
	if len(areas[0].Triggers) != 2 {
		t.Errorf("triggers = %+v, want PSNR+SSIM only (not VMAF)", areas[0].Triggers)
	}
	for _, tr := range areas[0].Triggers {
		if tr.Metric == "VMAF" {
			t.Errorf("VMAF should not have triggered here")
		}
	}
	if !strings.Contains(areas[0].Note, "didn't penalize") {
		t.Errorf("note = %q, want it to flag the pixel-only-dip pattern", areas[0].Note)
	}
}
