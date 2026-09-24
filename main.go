// Command vidqc compares an encoded video against its source using
// ffmpeg's libvmaf filter (VMAF, PSNR, SSIM) and reports independent
// verdicts for each metric plus the worst-scoring moments in the file.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/msgarbossa/vidqc/check"
	"github.com/msgarbossa/vidqc/encodequality"
)

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
	lowest        int
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
	fs.IntVar(&opts.lowest, "lowest", encodequality.DefaultLowest, "")
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

	if err := check.Available(); err != nil {
		return err
	}

	effort := encodequality.Medium
	switch {
	case opts.quick:
		effort = encodequality.Quick
	case opts.thorough:
		effort = encodequality.Thorough
	}
	rep, err := check.Run(context.Background(), check.Options{
		Source:              source,
		Encoded:             encoded,
		Effort:              effort,
		Subsample:           opts.subsample,
		Threads:             opts.threads,
		Top:                 opts.top,
		Lowest:              lowestOption(opts.lowest),
		WorkDir:             outDir,
		AllowLengthMismatch: opts.allowMismatch,
		Color:               !opts.noColor && os.Getenv("NO_COLOR") == "" && isTerminal(os.Stdout),
		Stderr:              os.Stderr,
	}, os.Stdout)
	if err != nil {
		return err
	}

	// Only the CLI prints where the per-frame data went: its output
	// directory is one the reader chose and can open.
	fmt.Printf("\nFull per-frame data: %s\n", rep.DataPath)
	fmt.Println("(run with --help for what these numbers and colors mean)")
	return nil
}

// lowestOption maps the CLI's --lowest (0 = list none) onto
// check.Options.Lowest, whose zero value means the default instead.
func lowestOption(n int) int {
	if n <= 0 {
		return -1
	}
	return n
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
