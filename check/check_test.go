package check

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/msgarbossa/vidqc/encodequality"
)

func sampleResult() encodequality.Result {
	var frames []encodequality.FrameMetric
	for i := 0; i < 60; i++ {
		f := encodequality.FrameMetric{Frame: i * 25, Time: float64(i), VMAF: 95, PSNR: 42, SSIM: 0.99}
		if i >= 30 && i < 33 {
			f.VMAF, f.PSNR, f.SSIM = 70, 33, 0.94 // one dip, so a problem area is reported
		}
		frames = append(frames, f)
	}
	stat := func(get func(encodequality.FrameMetric) float64) encodequality.PooledStat {
		vals := make([]float64, len(frames))
		for i, f := range frames {
			vals[i] = get(f)
		}
		return encodequality.ComputePooledStats(vals)
	}
	return encodequality.Result{
		Frames: frames,
		VMAF:   stat(func(f encodequality.FrameMetric) float64 { return f.VMAF }),
		PSNR:   stat(func(f encodequality.FrameMetric) float64 { return f.PSNR }),
		SSIM:   stat(func(f encodequality.FrameMetric) float64 { return f.SSIM }),
	}
}

// The report goes to the writer it is given, and only carries ANSI escapes
// when asked to -- a server log must never see them.
func TestPrintResultsWriterAndColor(t *testing.T) {
	var plain, colored bytes.Buffer
	printResults(&plain, newColors(false), sampleResult(), 3)
	printResults(&colored, newColors(true), sampleResult(), 3)

	for _, want := range []string{"=== Results ===", "VMAF  mean=", "=== Top 1 problem area(s) ===", "00:30 - "} {
		if !strings.Contains(plain.String(), want) {
			t.Errorf("plain report missing %q:\n%s", want, plain.String())
		}
	}
	if strings.Contains(plain.String(), "\033[") {
		t.Errorf("plain report contains an ANSI escape:\n%q", plain.String())
	}
	if !strings.Contains(colored.String(), "\033[") {
		t.Errorf("colored report has no ANSI escape")
	}
}

func TestRunAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	_, err := Run(ctx, Options{Source: "a", Encoded: "b"}, &out)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if out.Len() != 0 {
		t.Errorf("wrote output before checking ctx: %q", out.String())
	}
}

// A missing input is reported before any ffmpeg call and before WorkDir is
// created, matching the CLI's original order.
func TestRunMissingFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.mp4")
	if err := os.WriteFile(src, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dir, "work")
	_, err := Run(context.Background(), Options{Source: src, Encoded: filepath.Join(dir, "nope.mp4"), WorkDir: work}, &bytes.Buffer{})
	if err == nil || !strings.HasPrefix(err.Error(), "file not found: ") {
		t.Fatalf("err = %v, want file not found", err)
	}
	if _, statErr := os.Stat(work); !os.IsNotExist(statErr) {
		t.Errorf("WorkDir was created despite the missing input")
	}
}

// requireTools skips a test that needs a real ffmpeg with libvmaf.
func requireTools(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("needs ffmpeg; skipped with -short")
	}
	if err := Available(); err != nil {
		t.Skipf("ffmpeg with libvmaf not available: %v", err)
	}
}

// makePair writes a synthetic source and a lower-quality re-encode of it.
func makePair(t *testing.T, dir string, seconds int, size string) (src, enc string) {
	t.Helper()
	src = filepath.Join(dir, "src.mp4")
	enc = filepath.Join(dir, "enc.mp4")
	gen := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi",
		"-i", "testsrc2=size="+size+":rate=25:duration="+strconv.Itoa(seconds),
		"-c:v", "libx264", "-preset", "ultrafast", "-crf", "18", "-pix_fmt", "yuv420p", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generating source: %v\n%s", err, out)
	}
	reenc := exec.Command("ffmpeg", "-v", "error", "-y", "-i", src,
		"-c:v", "libx264", "-preset", "ultrafast", "-crf", "32", enc)
	if out, err := reenc.CombinedOutput(); err != nil {
		t.Fatalf("generating encode: %v\n%s", err, out)
	}
	return src, enc
}

func TestRunWholeFile(t *testing.T) {
	requireTools(t)
	dir := t.TempDir()
	src, enc := makePair(t, dir, 4, "320x180")

	work := filepath.Join(dir, "work")
	var out bytes.Buffer
	rep, err := Run(context.Background(), Options{Source: src, Encoded: enc, Effort: encodequality.Quick, Top: 3, WorkDir: work}, &out)
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	if rep.VMAF.Mean <= 0 || len(rep.Frames) == 0 {
		t.Errorf("empty result: %+v", rep.VMAF)
	}
	if want := filepath.Join(work, "enc.mp4.vmaf.json"); rep.DataPath != want {
		t.Errorf("DataPath = %q, want %q", rep.DataPath, want)
	}
	if _, err := os.Stat(rep.DataPath); err != nil {
		t.Errorf("per-frame data not written: %v", err)
	}
	s := out.String()
	if !strings.HasPrefix(s, "Source:     "+src+"\n") || !strings.Contains(s, "=== Results ===") {
		t.Errorf("unexpected output:\n%s", s)
	}
	if strings.Contains(s, "Full per-frame data") || strings.Contains(s, "\033[") {
		t.Errorf("library output carries a CLI-only line or a color escape:\n%s", s)
	}

	// No WorkDir: a temp dir that is gone again, and no path to report.
	rep, err = Run(context.Background(), Options{Source: src, Encoded: enc, Effort: encodequality.Quick}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("Run without WorkDir: %v", err)
	}
	if rep.DataPath != "" {
		t.Errorf("DataPath = %q with no WorkDir, want empty", rep.DataPath)
	}
}

// Cancelling mid-pass stops libvmaf promptly, returns ctx.Err() rather
// than an "ffmpeg failed"/OOM error, and leaves no ffmpeg behind.
func TestRunCancel(t *testing.T) {
	requireTools(t)
	dir := t.TempDir()
	src, enc := makePair(t, dir, 60, "1280x720")

	ctx, cancel := context.WithCancel(context.Background())
	var cancelledAt time.Time
	time.AfterFunc(time.Second, func() { cancelledAt = time.Now(); cancel() })

	_, err := Run(ctx, Options{Source: src, Encoded: enc, Effort: encodequality.Thorough, Threads: 1, WorkDir: filepath.Join(dir, "work")}, &bytes.Buffer{})
	returned := time.Now()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled (did the pass finish inside a second?)", err)
	}
	if d := returned.Sub(cancelledAt); d > 3*time.Second {
		t.Errorf("Run took %v to return after cancel", d)
	}
	if _, lookErr := exec.LookPath("pgrep"); lookErr == nil {
		// The test's temp dir is in every ffmpeg command line this run made.
		if out, _ := exec.Command("pgrep", "-f", dir).Output(); len(bytes.TrimSpace(out)) > 0 {
			t.Errorf("ffmpeg still running after cancel: pids %s", out)
		}
	}
}
