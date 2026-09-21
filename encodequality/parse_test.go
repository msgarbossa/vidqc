package encodequality

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleLog = `{
  "version": "2.0.0",
  "frames": [
    {"frameNum": 0,  "metrics": {"vmaf": 95.5, "psnr_y": 44.0, "float_ssim": 0.995}},
    {"frameNum": 30, "metrics": {"vmaf": 70.0, "psnr_y": 40.0, "float_ssim": 0.980}},
    {"frameNum": 60, "metrics": {"vmaf": 96.0, "psnr_y": 45.0, "float_ssim": 0.996}}
  ],
  "pooled_metrics": {
    "vmaf": {"min": 70.0, "max": 96.0, "mean": 87.16, "harmonic_mean": 86.0},
    "psnr_y": {"min": 40.0, "max": 45.0, "mean": 43.0, "harmonic_mean": 42.9},
    "float_ssim": {"min": 0.98, "max": 0.996, "mean": 0.99, "harmonic_mean": 0.99}
  }
}`

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseLog(t *testing.T) {
	path := writeTemp(t, "vmaf.json", sampleLog)

	res, err := ParseLog(path, 30)
	if err != nil {
		t.Fatalf("ParseLog: %v", err)
	}
	if len(res.Frames) != 3 {
		t.Fatalf("got %d frames, want 3", len(res.Frames))
	}
	if res.Frames[1].Time != 1.0 {
		t.Errorf("frame 30 @ 30fps: got Time=%v, want 1.0", res.Frames[1].Time)
	}
	if res.VMAF.Mean != 87.16 {
		t.Errorf("got VMAF.Mean=%v, want 87.16", res.VMAF.Mean)
	}
	if res.PSNR.Mean != 43.0 {
		t.Errorf("got PSNR.Mean=%v, want 43.0", res.PSNR.Mean)
	}
	if res.SSIM.Mean != 0.99 {
		t.Errorf("got SSIM.Mean=%v, want 0.99", res.SSIM.Mean)
	}
}

func TestParseLogErrors(t *testing.T) {
	if _, err := ParseLog("/no/such/file.json", 30); err == nil {
		t.Error("expected error for missing file")
	}

	badJSON := writeTemp(t, "bad.json", "{not json")
	if _, err := ParseLog(badJSON, 30); err == nil {
		t.Error("expected error for invalid JSON")
	}

	empty := writeTemp(t, "empty.json", `{"frames": [], "pooled_metrics": {}}`)
	if _, err := ParseLog(empty, 30); err == nil {
		t.Error("expected error for zero frames")
	}

	if _, err := ParseLog(writeTemp(t, "z.json", sampleLog), 0); err == nil {
		t.Error("expected error for zero fps")
	}
}
