package main

import (
	"fmt"
	"os"

	"github.com/msgarbossa/vidqc/encodequality"
)

type colors struct {
	red, yellow, green, reset string
}

func newColors(enabled bool) colors {
	if !enabled || os.Getenv("NO_COLOR") != "" || !isTerminal(os.Stdout) {
		return colors{}
	}
	return colors{
		red:    "\033[31;1m",
		yellow: "\033[33;1m",
		green:  "\033[32;1m",
		reset:  "\033[0m",
	}
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
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

func printResults(c colors, res encodequality.Result, top int) {
	vmafV := encodequality.VMAFVerdict(res.VMAF.Mean, res.VMAF.Min)
	psnrV := encodequality.PSNRVerdict(res.PSNR.Mean)
	ssimV := encodequality.SSIMVerdict(res.SSIM.Mean)

	fmt.Println()
	fmt.Println("=== Results ===")

	fmt.Printf("VMAF  mean=%.2f  min=%.2f  harmonic_mean=%.2f  %s[%s]%s  %s\n",
		res.VMAF.Mean, res.VMAF.Min, res.VMAF.HarmonicMean,
		c.verdictColor(vmafV.Verdict), vmafV.Verdict, c.reset, vmafV.Meaning)

	fmt.Printf("PSNR  mean=%.2f dB  min=%.2f dB  %s[%s]%s  %s\n",
		res.PSNR.Mean, res.PSNR.Min,
		c.verdictColor(psnrV.Verdict), psnrV.Verdict, c.reset, psnrV.Meaning)

	fmt.Printf("SSIM  mean=%.4f  min=%.4f  %s[%s]%s  %s\n",
		res.SSIM.Mean, res.SSIM.Min,
		c.verdictColor(ssimV.Verdict), ssimV.Verdict, c.reset, ssimV.Meaning)

	fmt.Println()
	fmt.Println("VMAF is the primary read (a perceptual model, calibrated to human ratings);")
	fmt.Println("PSNR/SSIM are pixel-level cross-checks -- see the problem areas below for how")
	fmt.Println("they help tell a real quality dip apart from an artifact.")

	areas := encodequality.DetectProblemAreas(res.Frames, res.VMAF, res.PSNR, res.SSIM, top)
	fmt.Println()
	if len(areas) == 0 {
		fmt.Println("=== Problem areas: none found ===")
		fmt.Println("No stretch of the file scored notably worse than this run's own average.")
		return
	}
	fmt.Printf("=== Top %d problem area(s) ===\n", len(areas))
	for i, a := range areas {
		fmt.Printf("\n%d. %s - %s (frames %d-%d)\n", i+1, formatTime(a.StartTime), formatTime(a.EndTime), a.StartFrame, a.EndFrame)
		fmt.Printf("   Triggered by: %s\n", triggerSummary(a.Triggers))
		fmt.Printf("   In this segment: VMAF worst=%.2f, PSNR worst=%.2f dB, SSIM worst=%.4f\n",
			a.VMAFWorst, a.PSNRWorst, a.SSIMWorst)
		if a.Note != "" {
			fmt.Printf("   %s\n", a.Note)
		}
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
