package check

import (
	"fmt"
	"io"

	"github.com/msgarbossa/vidqc/encodequality"
)

type colors struct {
	red, yellow, green, reset string
}

// newColors returns ANSI escapes when enabled, empty strings otherwise.
// Whether color suits the destination (a terminal, NO_COLOR unset) is the
// caller's call, made once into Options.Color.
func newColors(enabled bool) colors {
	if !enabled {
		return colors{}
	}
	return colors{
		red:    "\033[31;1m",
		yellow: "\033[33;1m",
		green:  "\033[32;1m",
		reset:  "\033[0m",
	}
}

func (c colors) verdictColor(v encodequality.Verdict) string {
	switch v {
	case encodequality.Green:
		return c.green
	case encodequality.Yellow:
		return c.yellow
	default:
		return c.red
	}
}

func formatTime(seconds float64) string {
	total := int(seconds + 0.5)
	h, rem := total/3600, total%3600
	m, s := rem/60, rem%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func printResults(out io.Writer, c colors, res encodequality.Result, top, lowest int) {
	vmafV := encodequality.VMAFVerdict(res.VMAF.Mean, res.VMAF.Min)
	psnrV := encodequality.PSNRVerdict(res.PSNR.Mean)
	ssimV := encodequality.SSIMVerdict(res.SSIM.Mean)

	fmt.Fprintln(out)
	fmt.Fprintf(out, "=== Results (%d sampled frames) ===\n", len(res.Frames))

	fmt.Fprintf(out, "VMAF  mean=%.2f  %s[%s]%s  %s\n", res.VMAF.Mean,
		c.verdictColor(vmafV.Verdict), vmafV.Verdict, c.reset, vmafV.Meaning)
	fmt.Fprintf(out, "      sd=%.2f  median=%.2f  p5=%.2f  p1=%.2f  min=%.2f  harmonic_mean=%.2f\n",
		res.VMAF.StdDev, res.VMAF.Median, res.VMAF.P5, res.VMAF.P1, res.VMAF.Min, res.VMAF.HarmonicMean)

	fmt.Fprintf(out, "PSNR  mean=%.2f dB  %s[%s]%s  %s\n", res.PSNR.Mean,
		c.verdictColor(psnrV.Verdict), psnrV.Verdict, c.reset, psnrV.Meaning)
	fmt.Fprintf(out, "      sd=%.2f  median=%.2f  p5=%.2f  p1=%.2f  min=%.2f dB\n",
		res.PSNR.StdDev, res.PSNR.Median, res.PSNR.P5, res.PSNR.P1, res.PSNR.Min)

	fmt.Fprintf(out, "SSIM  mean=%.4f  %s[%s]%s  %s\n", res.SSIM.Mean,
		c.verdictColor(ssimV.Verdict), ssimV.Verdict, c.reset, ssimV.Meaning)
	fmt.Fprintf(out, "      sd=%.4f  median=%.4f  p5=%.4f  p1=%.4f  min=%.4f\n",
		res.SSIM.StdDev, res.SSIM.Median, res.SSIM.P5, res.SSIM.P1, res.SSIM.Min)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "VMAF is the primary read (a perceptual model, calibrated to human ratings);")
	fmt.Fprintln(out, "PSNR/SSIM are pixel-level cross-checks -- see the problem areas below for how")
	fmt.Fprintln(out, "they help tell a real quality dip apart from an artifact. sd is the spread")
	fmt.Fprintln(out, "around the mean; p5/p1 are the typical worst case (5%/1% of frames score at or")
	fmt.Fprintln(out, "below them), where min is a single frame.")

	printProblemAreas(out, res, top)
	printLowestMoments(out, res, lowest)
}

func printProblemAreas(out io.Writer, res encodequality.Result, top int) {
	areas := encodequality.DetectProblemAreas(res.Frames, res.VMAF, res.PSNR, res.SSIM, top)
	fmt.Fprintln(out)
	if len(areas) == 0 {
		fmt.Fprintln(out, "=== Problem areas: none found ===")
		fmt.Fprintln(out, "No stretch of the file scored notably worse than this run's own average.")
		return
	}
	fmt.Fprintf(out, "=== Top %d problem area(s) ===\n", len(areas))
	for i, a := range areas {
		fmt.Fprintf(out, "\n%d. %s - %s (frames %d-%d)\n", i+1, formatTime(a.StartTime), formatTime(a.EndTime), a.StartFrame, a.EndFrame)
		fmt.Fprintf(out, "   Triggered by: %s\n", triggerSummary(a.Triggers))
		fmt.Fprintf(out, "   In this segment: VMAF worst=%.2f, PSNR worst=%.2f dB, SSIM worst=%.4f\n",
			a.VMAFWorst, a.PSNRWorst, a.SSIMWorst)
		if a.Note != "" {
			fmt.Fprintf(out, "   %s\n", a.Note)
		}
	}
}

// printLowestMoments lists the lowest-VMAF sampled frames, spaced apart --
// places to spot-check that exist whether or not anything was flagged,
// since a clean or uniformly soft encode flags nothing.
func printLowestMoments(out io.Writer, res encodequality.Result, n int) {
	moments := encodequality.LowestMoments(res.Frames, n)
	if len(moments) == 0 {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "=== %d lowest-scoring moment(s) ===\n", len(moments))
	fmt.Fprintln(out, "Where to look first, flagged or not: the lowest VMAF samples, spaced apart.")
	for i, f := range moments {
		fmt.Fprintf(out, "%d. %s (frame %d)  VMAF %.2f (%+.2f vs mean)  PSNR %.2f dB  SSIM %.4f\n",
			i+1, formatTime(f.Time), f.Frame, f.VMAF, f.VMAF-res.VMAF.Mean, f.PSNR, f.SSIM)
	}
}

func triggerSummary(triggers []encodequality.Trigger) string {
	s := ""
	for i, t := range triggers {
		if i > 0 {
			s += ", "
		}
		s += fmt.Sprintf("%s (worst %.3f)", t.Metric, t.Worst)
	}
	return s
}
