package encodequality

import "sort"

// A frame is flagged when a metric falls more than this far below *this
// run's own pooled mean* for that metric -- relative to the video's own
// average rather than a fixed absolute floor, so the detector adapts to
// both a mediocre encode (where only its worst moments stand out) and an
// excellent one (where even a modest dip is worth a look). Units match each
// metric's own scale.
const (
	vmafDropTrigger = 10.0
	psnrDropTrigger = 5.0
	ssimDropTrigger = 0.03
)

// mergeGapSamples is how many consecutive non-flagged samples are allowed
// inside one problem area before it's considered to have ended -- a scene
// with a couple of clean frames in the middle of a rough patch is still one
// scene, not two.
const mergeGapSamples = 3

type flaggedFrame struct {
	idx      int
	triggers []Trigger
}

type segment struct {
	startIdx, endIdx int
	vmafWorst        float64
	psnrWorst        float64
	ssimWorst        float64
	triggered        map[string]bool
}

func newSegment(frames []FrameMetric, i int, trig []Trigger) *segment {
	s := &segment{
		startIdx: i, endIdx: i,
		vmafWorst: frames[i].VMAF, psnrWorst: frames[i].PSNR, ssimWorst: frames[i].SSIM,
		triggered: map[string]bool{},
	}
	for _, t := range trig {
		s.triggered[t.Metric] = true
	}
	return s
}

func (s *segment) extend(frames []FrameMetric, i int, trig []Trigger) {
	if frames[i].VMAF < s.vmafWorst {
		s.vmafWorst = frames[i].VMAF
	}
	if frames[i].PSNR < s.psnrWorst {
		s.psnrWorst = frames[i].PSNR
	}
	if frames[i].SSIM < s.ssimWorst {
		s.ssimWorst = frames[i].SSIM
	}
	s.endIdx = i
	for _, t := range trig {
		s.triggered[t.Metric] = true
	}
}

func (s *segment) finish(frames []FrameMetric, vmaf, psnr, ssim PooledStat) ProblemArea {
	var triggers []Trigger
	if s.triggered["VMAF"] {
		triggers = append(triggers, Trigger{"VMAF", s.vmafWorst})
	}
	if s.triggered["PSNR"] {
		triggers = append(triggers, Trigger{"PSNR", s.psnrWorst})
	}
	if s.triggered["SSIM"] {
		triggers = append(triggers, Trigger{"SSIM", s.ssimWorst})
	}

	severity := 0.0
	for _, t := range triggers {
		var norm float64
		switch t.Metric {
		case "VMAF":
			norm = (vmaf.Mean - t.Worst) / vmafDropTrigger
		case "PSNR":
			norm = (psnr.Mean - t.Worst) / psnrDropTrigger
		case "SSIM":
			norm = (ssim.Mean - t.Worst) / ssimDropTrigger
		}
		if norm > severity {
			severity = norm
		}
	}

	return ProblemArea{
		StartTime:  frames[s.startIdx].Time,
		EndTime:    frames[s.endIdx].Time,
		StartFrame: frames[s.startIdx].Frame,
		EndFrame:   frames[s.endIdx].Frame,
		Triggers:   triggers,
		VMAFWorst:  s.vmafWorst,
		PSNRWorst:  s.psnrWorst,
		SSIMWorst:  s.ssimWorst,
		Severity:   severity,
		Note:       note(triggers, vmaf.Mean, s.vmafWorst),
	}
}

// DetectProblemAreas finds up to top contiguous stretches of frames that
// scored notably worse than the run's own average, ranks them by severity,
// and returns the worst ones first.
func DetectProblemAreas(frames []FrameMetric, vmaf, psnr, ssim PooledStat, top int) []ProblemArea {
	var flagged []flaggedFrame
	for i, f := range frames {
		var trig []Trigger
		if f.VMAF < vmaf.Mean-vmafDropTrigger {
			trig = append(trig, Trigger{"VMAF", f.VMAF})
		}
		if f.PSNR < psnr.Mean-psnrDropTrigger {
			trig = append(trig, Trigger{"PSNR", f.PSNR})
		}
		if f.SSIM < ssim.Mean-ssimDropTrigger {
			trig = append(trig, Trigger{"SSIM", f.SSIM})
		}
		if len(trig) > 0 {
			flagged = append(flagged, flaggedFrame{i, trig})
		}
	}
	if len(flagged) == 0 {
		return nil
	}

	var areas []ProblemArea
	cur := newSegment(frames, flagged[0].idx, flagged[0].triggers)
	lastFlaggedIdx := flagged[0].idx
	for _, fl := range flagged[1:] {
		if fl.idx-lastFlaggedIdx <= mergeGapSamples {
			cur.extend(frames, fl.idx, fl.triggers)
		} else {
			areas = append(areas, cur.finish(frames, vmaf, psnr, ssim))
			cur = newSegment(frames, fl.idx, fl.triggers)
		}
		lastFlaggedIdx = fl.idx
	}
	areas = append(areas, cur.finish(frames, vmaf, psnr, ssim))

	sort.Slice(areas, func(i, j int) bool { return areas[i].Severity > areas[j].Severity })
	if top > 0 && len(areas) > top {
		areas = areas[:top]
	}
	return areas
}

// note gives a short, rule-based read on what a segment's trigger pattern
// suggests -- distinguishing a genuinely hard scene (everything dips
// together) from a likely artifact (VMAF alone collapses while pixel-level
// metrics barely move) from an imperceptible pixel difference (PSNR/SSIM
// dip but VMAF doesn't).
func note(triggers []Trigger, vmafMean, vmafWorst float64) string {
	has := map[string]bool{}
	for _, t := range triggers {
		has[t.Metric] = true
	}
	switch {
	case has["VMAF"] && vmafWorst < 20 && vmafMean-vmafWorst > 40:
		return "VMAF collapsed far below its usual range here while pixel-level metrics moved far less -- " +
			"this pattern often means a frame-alignment or timestamp glitch (e.g. mismatched source/encoded " +
			"content at this point) rather than genuine encode quality loss. Worth double-checking that " +
			"source and encoded cover the exact same content."
	case has["VMAF"] && !has["PSNR"] && !has["SSIM"]:
		return "VMAF dropped while PSNR/SSIM stayed close to normal -- likely a scene that's genuinely " +
			"harder to compress (motion, fine detail, grain) rather than a decode artifact."
	case has["VMAF"] && (has["PSNR"] || has["SSIM"]):
		return "VMAF and at least one pixel-level metric dropped together -- a real, visible quality dip in this scene."
	case !has["VMAF"] && (has["PSNR"] || has["SSIM"]):
		return "PSNR/SSIM show a pixel-level difference here, but VMAF's perceptual model didn't penalize it " +
			"much -- often imperceptible; worth a quick look to confirm."
	default:
		return ""
	}
}
