package check

import (
	"context"
	"encoding/binary"
	"encoding/json"
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

// videoProbe is the first video stream's geometry and frame rate, as the
// libvmaf pass will see it.
type videoProbe struct {
	w, h int
	fps  float64
	rate string // r_frame_rate as ffprobe wrote it ("30000/1001")
	// frames is the container's declared frame count (nb_frames); 0 when
	// the container doesn't declare one (Matroska, raw streams).
	frames int
}

// ffprobeVideo is the subset of `ffprobe -of json` probeVideo reads.
type ffprobeVideo struct {
	Streams []struct {
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		RFrameRate   string `json:"r_frame_rate"`
		NbFrames     string `json:"nb_frames"`
		SideDataList []struct {
			Rotation float64 `json:"rotation"`
		} `json:"side_data_list"`
		Tags struct {
			Rotate string `json:"rotate"` // ffprobe < 5 reported rotation here
		} `json:"tags"`
	} `json:"streams"`
}

// probeVideo reads path's first video stream. JSON, not csv: a stream that
// carries side data -- a phone video's display matrix, say -- makes the csv
// writer append an empty field ("1920,1080,"), which a fixed-width parse
// rejects. The dimensions are the displayed ones: ffmpeg autorotates both
// inputs of the libvmaf pass, so a 90-degree-rotated 1920x1080 source
// arrives as 1080x1920 and must be scaled to that, not squashed back.
func probeVideo(ctx context.Context, path string) (videoProbe, error) {
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height,r_frame_rate,nb_frames:stream_side_data=rotation:stream_tags=rotate",
		"-of", "json", path).Output()
	if err != nil {
		return videoProbe{}, fmt.Errorf("ffprobe video stream: %w", err)
	}
	return parseVideoProbe(out)
}

func parseVideoProbe(out []byte) (videoProbe, error) {
	var doc ffprobeVideo
	if err := json.Unmarshal(out, &doc); err != nil {
		return videoProbe{}, fmt.Errorf("parsing ffprobe video stream output: %w", err)
	}
	if len(doc.Streams) == 0 {
		return videoProbe{}, fmt.Errorf("ffprobe found no video stream")
	}
	s := doc.Streams[0]
	if s.Width <= 0 || s.Height <= 0 {
		return videoProbe{}, fmt.Errorf("unexpected ffprobe resolution %dx%d", s.Width, s.Height)
	}
	fps, err := parseFrameRate(s.RFrameRate)
	if err != nil {
		return videoProbe{}, err
	}
	rotation := 0.0
	for _, sd := range s.SideDataList {
		if sd.Rotation != 0 {
			rotation = sd.Rotation
			break
		}
	}
	if rotation == 0 && s.Tags.Rotate != "" {
		rotation, _ = strconv.ParseFloat(s.Tags.Rotate, 64)
	}
	w, h := s.Width, s.Height
	if q := int(math.Round(rotation/90)) % 2; q != 0 {
		w, h = h, w
	}
	frames, _ := strconv.Atoi(s.NbFrames)
	return videoProbe{w: w, h: h, fps: fps, rate: strings.TrimSpace(s.RFrameRate), frames: frames}, nil
}

// frameDuration inverts an ffprobe rational frame rate into a time base
// ("30000/1001" -> "1001/30000"), so that setpts=N stamps one frame per
// tick. A rate that isn't a clean rational (never seen from ffprobe) falls
// back to a millisecond base, which still gives both inputs the same clock.
func frameDuration(rate string) string {
	num, den, found := strings.Cut(rate, "/")
	n, errN := strconv.Atoi(num)
	d := 1
	var errD error
	if found {
		d, errD = strconv.Atoi(den)
	}
	if errN != nil || errD != nil || n <= 0 || d <= 0 {
		return "1/1000"
	}
	return fmt.Sprintf("%d/%d", d, n)
}

// parseFrameRate parses ffprobe's rational frame rate ("30000/1001", "25/1").
func parseFrameRate(s string) (float64, error) {
	num, den, found := strings.Cut(strings.TrimSpace(s), "/")
	n, errN := strconv.ParseFloat(num, 64)
	if errN != nil {
		return 0, fmt.Errorf("parsing ffprobe frame rate %q", s)
	}
	if !found {
		return n, nil
	}
	d, errD := strconv.ParseFloat(den, 64)
	if errD != nil || d == 0 {
		return 0, fmt.Errorf("parsing ffprobe frame rate %q", s)
	}
	return n / d, nil
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
	// indexRate, when set, pairs the two inputs frame N with frame N rather
	// than by timestamp: both are restamped as frame numbers in one shared
	// time base (the source's frame duration, e.g. "1001/30000"). See
	// pairByIndex.
	indexRate        string
	stdout, stderr   io.Writer // ffmpeg's -stats progress line is on stderr
	encStart, encDur float64
	srcStart, srcDur float64
}

// runVMAF execs ffmpeg to compute VMAF/PSNR/SSIM in one pass, writing the
// per-frame + pooled results to logPath as JSON. The pass is restricted to
// a matched span of each file -- encStart/encDur into encoded,
// srcStart/srcDur into source -- for per-segment comparison after
// content-based alignment. A duration <= 0 means "no trim" (the whole
// file). Cancelling ctx kills ffmpeg.
func runVMAF(ctx context.Context, p vmafPass, logPath string) error {
	pts := "setpts=PTS-STARTPTS"
	if p.indexRate != "" {
		pts = "settb=" + frameDuration(p.indexRate) + ",setpts=N"
	}
	filter := fmt.Sprintf(
		"[0:v]scale=%d:%d:flags=bicubic,%s[dist];"+
			"[1:v]scale=%d:%d:flags=bicubic,%s[ref];"+
			"[dist][ref]libvmaf=log_fmt=json:log_path=%s:feature=name=psnr|name=float_ssim:n_threads=%d:n_subsample=%d:shortest=1",
		p.refW, p.refH, pts, p.refW, p.refH, pts, ffmpegEscape(logPath), p.threads, p.subsample,
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
