package check

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// Available verifies ffmpeg/ffprobe are on PATH and that ffmpeg was built
// with libvmaf support, failing fast with an actionable message instead of
// letting a confusing exec error surface later. Run does not call it: the
// CLI calls it once up front, and a long-running caller can call it once
// and cache the answer.
func Available() error {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s not found on PATH -- install ffmpeg (with libvmaf support) and ffprobe first", tool)
		}
	}
	out, err := exec.Command("ffmpeg", "-hide_banner", "-filters").Output()
	if err != nil {
		return fmt.Errorf("running 'ffmpeg -filters': %w", err)
	}
	if !strings.Contains(string(out), "libvmaf") {
		return fmt.Errorf("ffmpeg was found but doesn't have the libvmaf filter built in -- " +
			"reinstall/rebuild ffmpeg with --enable-libvmaf (Homebrew's ffmpeg includes it by default on macOS)")
	}
	return nil
}

func probeResolution(ctx context.Context, path string) (w, h int, err error) {
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height", "-of", "csv=p=0", path).Output()
	if err != nil {
		return 0, 0, fmt.Errorf("ffprobe resolution: %w", err)
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected ffprobe resolution output: %q", out)
	}
	w, errW := strconv.Atoi(parts[0])
	h, errH := strconv.Atoi(parts[1])
	if errW != nil || errH != nil {
		return 0, 0, fmt.Errorf("parsing ffprobe resolution output %q", out)
	}
	return w, h, nil
}

func probeFPS(ctx context.Context, path string) (float64, error) {
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=r_frame_rate", "-of", "csv=p=0", path).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe frame rate: %w", err)
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "/", 2)
	num, errN := strconv.ParseFloat(parts[0], 64)
	if errN != nil {
		return 0, fmt.Errorf("parsing ffprobe frame rate output %q", out)
	}
	if len(parts) == 1 {
		return num, nil
	}
	den, errD := strconv.ParseFloat(parts[1], 64)
	if errD != nil || den == 0 {
		return 0, fmt.Errorf("parsing ffprobe frame rate output %q", out)
	}
	return num / den, nil
}

func probeDuration(ctx context.Context, path string) (float64, error) {
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-show_entries", "format=duration", "-of", "default=nw=1:nk=1", path).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe duration: %w", err)
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0, fmt.Errorf("parsing ffprobe duration output %q", out)
	}
	return d, nil
}

// vmafPass is one libvmaf invocation's fixed inputs: which two files, at
// what scale and sampling, and where ffmpeg's own output goes.
type vmafPass struct {
	source, encoded    string
	refW, refH         int
	threads, subsample int
	stdout, stderr     io.Writer // ffmpeg's -stats progress line is on stderr
	encStart, encDur   float64
	srcStart, srcDur   float64
}

// runVMAF execs ffmpeg to compute VMAF/PSNR/SSIM in one pass, writing the
// per-frame + pooled results to logPath as JSON. The pass is restricted to
// a matched span of each file -- encStart/encDur into encoded,
// srcStart/srcDur into source -- for per-segment comparison after
// content-based alignment. A duration <= 0 means "no trim" (the whole
// file). Cancelling ctx kills ffmpeg.
func runVMAF(ctx context.Context, p vmafPass, logPath string) error {
	filter := fmt.Sprintf(
		"[0:v]scale=%d:%d:flags=bicubic,setpts=PTS-STARTPTS[dist];"+
			"[1:v]scale=%d:%d:flags=bicubic,setpts=PTS-STARTPTS[ref];"+
			"[dist][ref]libvmaf=log_fmt=json:log_path=%s:feature=name=psnr|name=float_ssim:n_threads=%d:n_subsample=%d:shortest=1",
		p.refW, p.refH, p.refW, p.refH, ffmpegEscape(logPath), p.threads, p.subsample,
	)

	args := []string{"-hide_banner", "-loglevel", "warning", "-stats"}
	if p.encDur > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", p.encStart), "-t", fmt.Sprintf("%.3f", p.encDur))
	}
	args = append(args, "-i", p.encoded)
	if p.srcDur > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", p.srcStart), "-t", fmt.Sprintf("%.3f", p.srcDur))
	}
	args = append(args, "-i", p.source, "-lavfi", filter, "-f", "null", "-")

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdout = p.stdout
	cmd.Stderr = p.stderr
	return cmd.Run()
}

// hasAudioStream reports whether path has at least one audio stream --
// content-based alignment needs one in both files.
func hasAudioStream(ctx context.Context, path string) (bool, error) {
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "a",
		"-show_entries", "stream=index", "-of", "csv=p=0", path).Output()
	if err != nil {
		return false, fmt.Errorf("ffprobe audio stream check: %w", err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// extractPCM decodes path's audio to mono 8kHz 32-bit float samples via
// ffmpeg, for content-based alignment's envelope/correlation search. 8kHz
// is plenty for that -- coarse energy-envelope matching, not fidelity.
const alignSampleRate = 8000

func extractPCM(ctx context.Context, path string) ([]float32, error) {
	out, err := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error",
		"-i", path, "-vn", "-ac", "1", "-ar", strconv.Itoa(alignSampleRate), "-f", "f32le", "-").Output()
	if err != nil {
		return nil, fmt.Errorf("extracting audio from %s: %w", path, err)
	}
	n := len(out) / 4
	samples := make([]float32, n)
	for i := 0; i < n; i++ {
		bits := binary.LittleEndian.Uint32(out[i*4 : i*4+4])
		samples[i] = math.Float32frombits(bits)
	}
	return samples, nil
}

// ffmpegEscape guards a path used inside an ffmpeg filtergraph string
// against characters the filter parser treats specially.
func ffmpegEscape(path string) string {
	r := strings.NewReplacer(`\`, `\\`, `:`, `\:`, `'`, `\'`)
	return r.Replace(path)
}
