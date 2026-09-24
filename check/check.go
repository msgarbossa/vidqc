// Package check runs a whole encode-quality check -- probing both files,
// running ffmpeg's libvmaf filter (falling back to content-based alignment
// when the two durations differ), and printing the report -- the same way
// the vidqc CLI does, for callers that want it as a library. The CLI is a
// thin wrapper over Run.
//
// Scoring and parsing live in encodequality, and audio alignment in align;
// this package is the part that needs ffmpeg and ffprobe on PATH.
package check

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"

	"github.com/msgarbossa/vidqc/encodequality"
)

const durationMismatchTolerance = 0.5 // seconds

// Options is one check's inputs. The zero value of every field except
// Source and Encoded is a usable default.
type Options struct {
	Source, Encoded string

	// Effort picks the sampling interval from the source frame rate.
	// Subsample > 0 overrides it with an explicit every-Nth-frame interval.
	Effort    encodequality.Effort
	Subsample int

	// Threads is libvmaf's worker count; <= 0 means min(8, CPU cores).
	Threads int

	// Top is how many problem areas to report; 0 reports every flagged
	// segment (the CLI's --top default is 3, not 0).
	Top int

	// Lowest is how many lowest-scoring moments to list after the problem
	// areas; 0 means encodequality.DefaultLowest and a negative value
	// lists none.
	Lowest int

	// WorkDir receives the per-frame libvmaf JSON (created if missing).
	// Empty means a temporary directory that Run removes before returning,
	// in which case Report.DataPath is empty too.
	WorkDir string

	// AllowLengthMismatch skips content-based alignment when the durations
	// differ, capping the comparison to the shorter file instead.
	AllowLengthMismatch bool

	// Color emits ANSI color escapes in the report. Deciding whether the
	// destination can show them (a terminal, NO_COLOR unset) is the caller's.
	Color bool

	// Stderr receives ffmpeg's own output during the libvmaf pass: its
	// -stats progress line (carriage-return-updated) and any warnings.
	// nil discards it. The CLI passes os.Stderr.
	Stderr io.Writer
}

// Report is Run's result: the parsed scores plus where the per-frame data
// was written.
type Report struct {
	encodequality.Result

	// DataPath is the per-frame JSON inside WorkDir: one file for a
	// whole-file comparison, or a "*.segment*.vmaf.json" glob when
	// alignment compared matched segments separately. Empty when WorkDir
	// was.
	DataPath string
}

// Run checks opts.Encoded against opts.Source, writing everything the vidqc
// CLI prints -- the header, notes, alignment progress and the results
// report -- to out, in the CLI's order. It does not print the CLI's closing
// "Full per-frame data" line; Report.DataPath carries that path instead.
//
// It does not verify the tools are installed; call Available first.
// Cancelling ctx kills any running ffmpeg/ffprobe and makes Run return
// ctx.Err().
func Run(ctx context.Context, opts Options, out io.Writer) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	rep, err := run(ctx, opts, out)
	if err != nil && ctx.Err() != nil {
		// A killed ffmpeg reads as a failure (or, on unix, as an OOM
		// kill); the cancellation is the real reason.
		return Report{}, ctx.Err()
	}
	return rep, err
}

func run(ctx context.Context, opts Options, out io.Writer) (Report, error) {
	source, encoded := opts.Source, opts.Encoded
	for _, f := range []string{source, encoded} {
		if _, err := os.Stat(f); err != nil {
			return Report{}, fmt.Errorf("file not found: %s", f)
		}
	}
	workDir := opts.WorkDir
	if workDir == "" {
		tmp, err := os.MkdirTemp("", "vidqc-")
		if err != nil {
			return Report{}, fmt.Errorf("creating output dir: %w", err)
		}
		defer os.RemoveAll(tmp)
		workDir = tmp
	} else if err := os.MkdirAll(workDir, 0o750); err != nil {
		return Report{}, fmt.Errorf("creating output dir: %w", err)
	}

	c := newColors(opts.Color)

	video, err := probeVideo(ctx, source)
	if err != nil {
		return Report{}, err
	}
	encVideo, err := probeVideo(ctx, encoded)
	if err != nil {
		return Report{}, err
	}
	refW, refH, fps := video.w, video.h, video.fps

	srcDur, err := probeDuration(ctx, source)
	if err != nil {
		return Report{}, err
	}
	encDur, err := probeDuration(ctx, encoded)
	if err != nil {
		return Report{}, err
	}
	durDiff := math.Abs(srcDur - encDur)

	subsample := opts.Subsample
	if subsample <= 0 {
		subsample = encodequality.SubsampleForClip(fps, srcDur, opts.Effort)
	}
	threads := opts.Threads
	if threads <= 0 {
		threads = runtime.NumCPU()
		if threads > 8 {
			threads = 8
		}
	}

	fmt.Fprintf(out, "Source:     %s\n", source)
	fmt.Fprintf(out, "Encoded:    %s\n", encoded)
	fmt.Fprintf(out, "Resolution: %dx%d   Subsample: every %d frame(s)   Threads: %d\n", refW, refH, subsample, threads)

	if same, checked := sameDevice(source, encoded); checked && same {
		fmt.Fprintf(out, "%sNote:%s source and encoded are on the same drive/volume. Reading both at once can be slow"+
			" or, on a long file, run the machine out of memory. If this hangs or gets killed, try -s/--subsample"+
			" or move one file to different storage first.\n", c.yellow, c.reset)
	}

	pass := vmafPass{
		source: source, encoded: encoded,
		refW: refW, refH: refH,
		threads: threads, subsample: subsample,
		stdout: out, stderr: opts.Stderr,
	}
	var res encodequality.Result
	var dataLocation string

	switch {
	case durDiff <= durationMismatchTolerance:
		if pairByIndex(video, encVideo) {
			pass.indexRate = video.rate
			fmt.Fprintf(out, "Pairing:    frame by frame (%d frames in each)\n", video.frames)
		} else {
			fmt.Fprintf(out, "Pairing:    by timestamp (%s)\n", frameCounts(video.frames, encVideo.frames))
		}
		res, dataLocation, err = runWholeFileComparison(ctx, out, pass, fps, workDir)
		if err != nil {
			return Report{}, err
		}

	case opts.AllowLengthMismatch:
		fmt.Fprintf(out, "%sNote:%s --allow-length-mismatch set: skipping content-based alignment. Source is %.3fs,"+
			" encoded is %.3fs (differ by %.1fs) -- comparison is capped to the shorter one's length. Only trust"+
			" this if the difference is a clean trim at the very end; a mid-video cut would desync everything"+
			" after it and this flag cannot detect or protect against that.\n", c.yellow, c.reset, srcDur, encDur, durDiff)
		res, dataLocation, err = runWholeFileComparison(ctx, out, pass, fps, workDir)
		if err != nil {
			return Report{}, err
		}

	default:
		fmt.Fprintf(out, "%sNote:%s source is %.3fs, encoded is %.3fs (differ by %.1fs) -- these aren't frame-for-frame"+
			" the same video. Falling back to content-based alignment (matching audio content) to find which"+
			" spans still correspond.\n", c.yellow, c.reset, srcDur, encDur, durDiff)
		fmt.Fprintln(out, "Extracting audio and searching for matching segments...")

		segments, err := attemptAlignment(ctx, source, encoded)
		if err != nil {
			return Report{}, fmt.Errorf("alignment failed: %w\n"+
				"Pass --allow-length-mismatch if you know this is a clean tail trim, or re-run against a"+
				" source/encoded pair with no edits between them", err)
		}
		segments = applyAlignMargin(segments)
		if len(segments) == 0 {
			return Report{}, fmt.Errorf("alignment found no confidently-matching segments -- source and encoded may not" +
				" be related content, or the match is too weak to trust.\n" +
				"Pass --allow-length-mismatch if you know this is a clean tail trim, or check that these are" +
				" really the same footage")
		}
		coverage := alignedCoverage(segments, encDur)
		fmt.Fprintf(out, "Found %d matching segment(s), covering %s%.0f%%%s of the encoded file:\n",
			len(segments), coverageColor(c, coverage), coverage*100, c.reset)

		res, err = runAlignedComparison(ctx, out, pass, fps, workDir, segments)
		if err != nil {
			return Report{}, err
		}
		dataLocation = fmt.Sprintf("%s/%s.segment*.vmaf.json", workDir, filepath.Base(encoded))
	}

	lowest := opts.Lowest
	if lowest == 0 {
		lowest = encodequality.DefaultLowest
	}
	printResults(out, c, res, opts.Top, lowest)
	if opts.WorkDir == "" {
		dataLocation = ""
	}
	return Report{Result: res, DataPath: dataLocation}, nil
}

// pairByIndex decides how the whole-file pass pairs frames. libvmaf pairs
// its two inputs by timestamp, which is right only when both files keep
// the same clock -- and a re-encode often doesn't: a phone records at a
// variable frame rate (mostly 33.3ms, with the odd 35ms frame), and an
// encoder writing constant 29.97fps restamps every frame. Where the
// restamped frame lands a hair before its original, libvmaf compares it
// with the *previous* source frame until the clocks cross back, which on
// motion reads as VMAF collapsing to single digits for a second at a time
// -- a stretch of problem areas that are not in the encode at all.
//
// When both containers declare the same frame count, the encode is a
// frame-for-frame re-encode and frame N is frame N, whatever its
// timestamp says. Any other case (a count missing, or differing -- dropped
// or duplicated frames) keeps timestamp pairing, which is what those need.
func pairByIndex(src, enc videoProbe) bool {
	return src.frames > 0 && src.frames == enc.frames
}

func frameCounts(src, enc int) string {
	if src <= 0 || enc <= 0 {
		return "frame counts not declared by the container"
	}
	return fmt.Sprintf("source has %d frames, encoded %d", src, enc)
}

// runWholeFileComparison runs a single libvmaf pass over the entirety of
// both files (the usual path, and also --allow-length-mismatch's, capped
// to the shorter file via shortest=1 inside runVMAF).
func runWholeFileComparison(ctx context.Context, out io.Writer, p vmafPass, fps float64, workDir string) (encodequality.Result, string, error) {
	logPath := filepath.Join(workDir, filepath.Base(p.encoded)+".vmaf.json")
	fmt.Fprintln(out, "Running libvmaf (VMAF + PSNR + SSIM in one pass)...")
	if err := runVMAF(ctx, p, logPath); err != nil {
		if wasOOMKilled(err) {
			return encodequality.Result{}, "", fmt.Errorf("ffmpeg was killed (signal 9, almost certainly out of memory).\n" +
				"Try: a larger -s/--subsample value, a lower -t/--threads count, or moving\n" +
				"source/encoded off a shared/external drive before retrying")
		}
		return encodequality.Result{}, "", fmt.Errorf("ffmpeg failed: %w", err)
	}
	res, err := encodequality.ParseLog(logPath, fps)
	if err != nil {
		return encodequality.Result{}, "", err
	}
	return res, logPath, nil
}
