// Command vidqc compares an encoded video against its source using
// ffmpeg's libvmaf filter (VMAF, PSNR, SSIM) and reports independent
// verdicts for each metric plus the worst-scoring moments in the file.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"

	"github.com/msgarbossa/vidqc/encodequality"
)

const durationMismatchTolerance = 0.5 // seconds

// Set via -ldflags at release build time (see .goreleaser.yaml); "dev"
// otherwise (e.g. a plain `go build`).
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "vidqc:", err)
		os.Exit(1)
	}
}

type options struct {
	subsample     int
	quick         bool
	thorough      bool
	threads       int
	allowMismatch bool
	top           int
	noColor       bool
	help          bool
	showVersion   bool
}

func parseFlags(args []string) (opts options, positional []string, err error) {
	fs := flag.NewFlagSet("vidqc", flag.ContinueOnError)
	fs.Usage = func() {} // we print our own usage
	fs.IntVar(&opts.subsample, "s", 0, "")
	fs.IntVar(&opts.subsample, "subsample", 0, "")
	fs.BoolVar(&opts.quick, "q", false, "")
	fs.BoolVar(&opts.quick, "quick", false, "")
	fs.BoolVar(&opts.thorough, "thorough", false, "")
	fs.IntVar(&opts.threads, "t", 0, "")
	fs.IntVar(&opts.threads, "threads", 0, "")
	fs.BoolVar(&opts.allowMismatch, "m", false, "")
	fs.BoolVar(&opts.allowMismatch, "allow-length-mismatch", false, "")
	fs.IntVar(&opts.top, "top", 3, "")
	fs.BoolVar(&opts.noColor, "no-color", false, "")
	fs.BoolVar(&opts.help, "h", false, "")
	fs.BoolVar(&opts.help, "help", false, "")
	fs.BoolVar(&opts.showVersion, "v", false, "")
	fs.BoolVar(&opts.showVersion, "version", false, "")
	if err := fs.Parse(args); err != nil {
		return opts, nil, err
	}
	return opts, fs.Args(), nil
}

func run() error {
	opts, positional, err := parseFlags(os.Args[1:])
	if err != nil {
		return err
	}
	if opts.showVersion {
		fmt.Printf("vidqc %s (commit %s, built %s)\n", version, commit, date)
		return nil
	}
	if opts.help {
		fmt.Print(usage)
		return nil
	}
	if opts.quick && opts.thorough {
		return fmt.Errorf("-q/--quick and --thorough are mutually exclusive")
	}
	if len(positional) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}
	source, encoded := positional[0], positional[1]
	outDir := "."
	if len(positional) >= 3 {
		outDir = positional[2]
	}

	if err := checkTools(); err != nil {
		return err
	}
	for _, f := range []string{source, encoded} {
		if _, err := os.Stat(f); err != nil {
			return fmt.Errorf("file not found: %s", f)
		}
	}
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	c := newColors(!opts.noColor)

	refW, refH, err := probeResolution(source)
	if err != nil {
		return err
	}
	fps, err := probeFPS(source)
	if err != nil {
		return err
	}

	srcDur, err := probeDuration(source)
	if err != nil {
		return err
	}
	encDur, err := probeDuration(encoded)
	if err != nil {
		return err
	}
	durDiff := math.Abs(srcDur - encDur)

	subsample := opts.subsample
	if subsample <= 0 {
		effort := encodequality.Medium
		switch {
		case opts.quick:
			effort = encodequality.Quick
		case opts.thorough:
			effort = encodequality.Thorough
		}
		subsample = encodequality.SubsampleFor(fps, effort)
	}
	threads := opts.threads
	if threads <= 0 {
		threads = runtime.NumCPU()
		if threads > 8 {
			threads = 8
		}
	}

	fmt.Printf("Source:     %s\n", source)
	fmt.Printf("Encoded:    %s\n", encoded)
	fmt.Printf("Resolution: %dx%d   Subsample: every %d frame(s)   Threads: %d\n", refW, refH, subsample, threads)

	if same, checked := sameDevice(source, encoded); checked && same {
		fmt.Printf("%sNote:%s source and encoded are on the same drive/volume. Reading both at once can be slow"+
			" or, on a long file, run the machine out of memory. If this hangs or gets killed, try -s/--subsample"+
			" or move one file to different storage first.\n", c.yellow, c.reset)
	}

	var res encodequality.Result
	var dataLocation string

	switch {
	case durDiff <= durationMismatchTolerance:
		res, dataLocation, err = runWholeFileComparison(source, encoded, refW, refH, threads, subsample, fps, outDir)
		if err != nil {
			return err
		}

	case opts.allowMismatch:
		fmt.Printf("%sNote:%s --allow-length-mismatch set: skipping content-based alignment. Source is %.3fs,"+
			" encoded is %.3fs (differ by %.1fs) -- comparison is capped to the shorter one's length. Only trust"+
			" this if the difference is a clean trim at the very end; a mid-video cut would desync everything"+
			" after it and this flag cannot detect or protect against that.\n", c.yellow, c.reset, srcDur, encDur, durDiff)
		res, dataLocation, err = runWholeFileComparison(source, encoded, refW, refH, threads, subsample, fps, outDir)
		if err != nil {
			return err
		}

	default:
		fmt.Printf("%sNote:%s source is %.3fs, encoded is %.3fs (differ by %.1fs) -- these aren't frame-for-frame"+
			" the same video. Falling back to content-based alignment (matching audio content) to find which"+
			" spans still correspond.\n", c.yellow, c.reset, srcDur, encDur, durDiff)
		fmt.Println("Extracting audio and searching for matching segments...")

		segments, err := attemptAlignment(source, encoded)
		if err != nil {
			return fmt.Errorf("alignment failed: %w\n"+
				"Pass --allow-length-mismatch if you know this is a clean tail trim, or re-run against a"+
				" source/encoded pair with no edits between them", err)
		}
		segments = applyAlignMargin(segments)
		if len(segments) == 0 {
			return fmt.Errorf("alignment found no confidently-matching segments -- source and encoded may not" +
				" be related content, or the match is too weak to trust.\n" +
				"Pass --allow-length-mismatch if you know this is a clean tail trim, or check that these are" +
				" really the same footage")
		}
		coverage := alignedCoverage(segments, encDur)
		fmt.Printf("Found %d matching segment(s), covering %s%.0f%%%s of the encoded file:\n",
			len(segments), coverageColor(c, coverage), coverage*100, c.reset)

		res, err = runAlignedComparison(source, encoded, refW, refH, threads, subsample, fps, outDir, segments)
		if err != nil {
			return err
		}
		dataLocation = fmt.Sprintf("%s/%s.segment*.vmaf.json", outDir, filepath.Base(encoded))
	}

	printResults(c, res, opts.top)
	fmt.Printf("\nFull per-frame data: %s\n", dataLocation)
	fmt.Println("(run with --help for what these numbers and colors mean)")
	return nil
}

// runWholeFileComparison runs a single libvmaf pass over the entirety of
// both files (the usual path, and also --allow-length-mismatch's, capped
// to the shorter file via shortest=1 inside runVMAF).
func runWholeFileComparison(source, encoded string, refW, refH, threads, subsample int, fps float64, outDir string) (encodequality.Result, string, error) {
	logPath := filepath.Join(outDir, filepath.Base(encoded)+".vmaf.json")
	fmt.Println("Running libvmaf (VMAF + PSNR + SSIM in one pass)...")
	if err := runVMAF(source, encoded, refW, refH, threads, subsample, logPath); err != nil {
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
