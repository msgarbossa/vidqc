package encodequality

// Each metric gets its own independent verdict. VMAF is the one to trust
// most: it's a perceptual model trained on human ratings, so its scale is
// calibrated to what people actually notice. PSNR and SSIM are plain
// signal-error math over pixels -- useful as a cross-check, and especially
// useful for telling apart *why* VMAF moved (see problemareas.go), but not
// as reliable a judge of perceived quality on their own. Treat a red/yellow
// verdict as "go look at it", not an automatic verdict -- heavy grain,
// animation, and unusual content can score lower than they look.

const (
	vmafGreenMean  = 93.0 // mean >= this AND min >= vmafGreenMin -> green
	vmafGreenMin   = 80.0
	vmafYellowMean = 85.0 // mean >= this AND min >= vmafYellowMin -> yellow
	vmafYellowMin  = 70.0

	// PSNR (dB, luma only). Rough, widely-cited engineering rules of thumb:
	// ~40dB+ is considered visually near-lossless, ~35-40dB good, below
	// that increasingly visible. PSNR is far more sensitive to imperceptible
	// pixel noise than VMAF, so don't expect these bands to line up with
	// VMAF's on the same file.
	psnrGreenMean  = 40.0
	psnrYellowMean = 35.0

	// SSIM (0..1, luma structural similarity).
	ssimGreenMean  = 0.98
	ssimYellowMean = 0.95
)

// VMAFVerdict judges VMAF's pooled mean and min together -- a high mean can
// hide a single bad stretch, which is exactly what min catches.
func VMAFVerdict(mean, min float64) MetricVerdict {
	switch {
	case mean >= vmafGreenMean && min >= vmafGreenMin:
		return MetricVerdict{Green, "Visually transparent for normal viewing; generally safe to treat the encode as a replacement for the source."}
	case mean >= vmafYellowMean && min >= vmafYellowMin:
		return MetricVerdict{Yellow, "Minor compromises somewhere in the file; check the problem areas below before deleting the source."}
	default:
		return MetricVerdict{Red, "Visible quality loss is likely; keep the source and consider a lower CRF (more bits) or check the encode settings."}
	}
}

// PSNRVerdict judges PSNR's pooled mean alone -- PSNR's min is much noisier
// frame-to-frame than VMAF's and isn't a reliable gate on its own.
func PSNRVerdict(mean float64) MetricVerdict {
	switch {
	case mean >= psnrGreenMean:
		return MetricVerdict{Green, "Very little pixel-level difference from the source."}
	case mean >= psnrYellowMean:
		return MetricVerdict{Yellow, "A moderate, generally unremarkable amount of pixel-level difference."}
	default:
		return MetricVerdict{Red, "A large amount of pixel-level difference -- cross-check against VMAF and the problem areas below."}
	}
}

// SSIMVerdict judges SSIM's pooled mean alone, for the same reason as PSNR.
func SSIMVerdict(mean float64) MetricVerdict {
	switch {
	case mean >= ssimGreenMean:
		return MetricVerdict{Green, "Structure (edges, luminance, contrast) very well preserved."}
	case mean >= ssimYellowMean:
		return MetricVerdict{Yellow, "Structure mostly preserved, with some measurable softening somewhere in the file."}
	default:
		return MetricVerdict{Red, "Noticeable structural difference from the source -- cross-check against VMAF and the problem areas below."}
	}
}
