package align

import "math"

// bestMatch searches every possible alignment of window against haystack
// and returns the offset (into haystack, in bins) and confidence (Pearson
// correlation coefficient, -1..1) of the best match. ok is false if window
// has no meaningful variance to match on (e.g. near-silence).
func bestMatch(window, haystack []float64) (offset int, confidence float64, ok bool) {
	wn := len(window)
	if wn == 0 || len(haystack) < wn {
		return 0, 0, false
	}
	wMean, wVar := meanVar(window)
	if wVar <= 0 {
		return 0, 0, false
	}
	wStd := math.Sqrt(wVar)

	best := -2.0 // below any valid correlation, so the first candidate always wins
	bestOffset := 0
	for start := 0; start+wn <= len(haystack); start++ {
		seg := haystack[start : start+wn]
		sMean, sVar := meanVar(seg)
		if sVar <= 0 {
			continue
		}
		var cov float64
		for i := 0; i < wn; i++ {
			cov += (window[i] - wMean) * (seg[i] - sMean)
		}
		cov /= float64(wn)
		corr := cov / (wStd * math.Sqrt(sVar))
		if corr > best {
			best = corr
			bestOffset = start
		}
	}
	if best < -1 {
		return 0, 0, false
	}
	return bestOffset, best, true
}

func meanVar(xs []float64) (mean, variance float64) {
	n := float64(len(xs))
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean = sum / n
	var sq float64
	for _, x := range xs {
		d := x - mean
		sq += d * d
	}
	return mean, sq / n
}

// FindAnchors slides a window across the encoded envelope at regular
// checkpoints, and for each one searches the full source envelope for its
// best match, keeping only matches at or above minConfidence.
func FindAnchors(encEnv, srcEnv []float64, binSeconds, checkpointSeconds, windowSeconds, minConfidence float64) []Anchor {
	windowBins := int(windowSeconds / binSeconds)
	stepBins := int(checkpointSeconds / binSeconds)
	if windowBins < 1 || stepBins < 1 {
		return nil
	}

	var anchors []Anchor
	for start := 0; start+windowBins <= len(encEnv); start += stepBins {
		window := encEnv[start : start+windowBins]
		offset, conf, ok := bestMatch(window, srcEnv)
		if !ok || conf < minConfidence {
			continue
		}
		anchors = append(anchors, Anchor{
			EncTime:    float64(start) * binSeconds,
			SrcTime:    float64(offset) * binSeconds,
			Confidence: conf,
		})
	}
	return anchors
}
