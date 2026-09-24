package check

import (
	"context"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"sort"

	"github.com/msgarbossa/vidqc/align"
	"github.com/msgarbossa/vidqc/encodequality"
)

// Defaults for content-based alignment. These come from one real
// validation (a heavily-edited ~25 minute clip with a front trim and a
// ~115s mid-timeline cut, see the project's own history) rather than
// exhaustive tuning across content types -- expect to revisit if a very
// different kind of source (near-silent audio, music-only, very short
// clips) needs different values.
const (
	alignBinSeconds = 0.02 // energy-envelope bin size for the correlation search -- needs to
	// resolve well under one video frame's duration (e.g. 40ms at 25fps),
	// since content with continuous motion can have a genuinely sharp
	// correlation peak: even one frame of misalignment can collapse VMAF
	// even though the coarser offset "looks" like a near-match
	alignCheckpointSeconds = 5.0  // how often to try to place an anchor along the encoded timeline
	alignWindowSeconds     = 4.0  // length of audio compared at each checkpoint
	alignMinConfidence     = 0.6  // minimum correlation to accept an anchor at all
	alignOffsetTolerance   = 2.0  // how close two anchors' offsets must be to belong to the same segment
	alignMaxGapSeconds     = 90.0 // max time between anchors within one segment before treating it as two
	alignMinSegmentSeconds = 10.0 // segments shorter than this aren't worth comparing
	alignMinAnchors        = 3    // minimum corroborating anchors to trust a segment at all
	alignSegmentMargin     = 2.0  // trimmed off both ends of each segment -- edges are the least certain part of a match
)

// Coverage is reported, not gated on -- cutting away half a file (or more)
// isn't common, but it does happen, and refusing outright would be more
// paternalistic than useful. These just decide how the percentage is
// colored, as a signal of how much of the encoded file the verdict below
// is actually informed by.
const (
	coverageGreenThreshold  = 0.8
	coverageYellowThreshold = 0.5
)

func coverageColor(c colors, coverage float64) string {
	switch {
	case coverage >= coverageGreenThreshold:
		return c.green
	case coverage >= coverageYellowThreshold:
		return c.yellow
	default:
		return c.red
	}
}

// alignSampleRate is defined alongside extractPCM in ffmpeg.go.

// attemptAlignment finds which spans of source and encoded correspond to
// each other, from audio content alone, when their lengths don't match.
func attemptAlignment(ctx context.Context, source, encoded string) ([]align.Segment, error) {
	srcHasAudio, err := hasAudioStream(ctx, source)
	if err != nil {
		return nil, err
	}
	if !srcHasAudio {
		return nil, fmt.Errorf("source has no audio track -- content-based alignment needs audio in both files")
	}
	encHasAudio, err := hasAudioStream(ctx, encoded)
	if err != nil {
		return nil, err
	}
	if !encHasAudio {
		return nil, fmt.Errorf("encoded has no audio track -- content-based alignment needs audio in both files")
	}

	srcSamples, err := extractPCM(ctx, source)
	if err != nil {
		return nil, err
	}
	encSamples, err := extractPCM(ctx, encoded)
	if err != nil {
		return nil, err
	}

	srcEnv := align.Envelope(srcSamples, alignSampleRate, alignBinSeconds)
	encEnv := align.Envelope(encSamples, alignSampleRate, alignBinSeconds)

	anchors := align.FindAnchors(encEnv, srcEnv, alignBinSeconds, alignCheckpointSeconds, alignWindowSeconds, alignMinConfidence)
	segments := align.ReconstructSegments(anchors, alignOffsetTolerance, alignMaxGapSeconds, alignMinSegmentSeconds, alignMinAnchors)
	return segments, nil
}

// applyAlignMargin shrinks each segment inward by alignSegmentMargin on
// both ends -- a matched segment's boundary is its least certain part --
// and drops any that become too short to bother comparing.
func applyAlignMargin(segments []align.Segment) []align.Segment {
	var out []align.Segment
	for _, s := range segments {
		s.EncStart += alignSegmentMargin
		s.EncEnd -= alignSegmentMargin
		s.SrcStart += alignSegmentMargin
		s.SrcEnd -= alignSegmentMargin
		if s.EncEnd-s.EncStart < alignMinSegmentSeconds {
			continue
		}
		out = append(out, s)
	}
	return out
}

// alignedCoverage is the fraction of the encoded file's duration covered
// by the given (already margin-applied) segments.
func alignedCoverage(segments []align.Segment, totalEncDur float64) float64 {
	if totalEncDur <= 0 {
		return 0
	}
	var covered float64
	for _, s := range segments {
		covered += s.EncEnd - s.EncStart
	}
	return covered / totalEncDur
}

// runAlignedComparison runs VMAF/PSNR/SSIM separately over each matched
// segment and combines the results into one Result, with every frame's
// time/frame number shifted back to its absolute position in the full
// encoded file (not relative to that segment's own trimmed clip) -- so
// reported problem-area timestamps always refer to the encoded file a
// viewer would actually open.
func runAlignedComparison(ctx context.Context, out io.Writer, p vmafPass, fps float64,
	workDir string, segments []align.Segment) (encodequality.Result, error) {
	base := filepath.Base(p.encoded)
	var allFrames []encodequality.FrameMetric

	for i, seg := range segments {
		dur := seg.EncEnd - seg.EncStart
		fmt.Fprintf(out, "Segment %d/%d: encoded [%s-%s] <-> source [%s-%s] (%.0fs)\n",
			i+1, len(segments), formatTime(seg.EncStart), formatTime(seg.EncEnd),
			formatTime(seg.SrcStart), formatTime(seg.SrcEnd), dur)

		logPath := filepath.Join(workDir, fmt.Sprintf("%s.segment%d.vmaf.json", base, i+1))
		sp := p
		sp.encStart, sp.encDur, sp.srcStart, sp.srcDur = seg.EncStart, dur, seg.SrcStart, dur
		if err := runVMAF(ctx, sp, logPath); err != nil {
			if wasOOMKilled(err) {
				return encodequality.Result{}, fmt.Errorf("ffmpeg was killed (out of memory) on segment %d/%d", i+1, len(segments))
			}
			return encodequality.Result{}, fmt.Errorf("ffmpeg failed on segment %d/%d: %w", i+1, len(segments), err)
		}

		res, err := encodequality.ParseLog(logPath, fps)
		if err != nil {
			return encodequality.Result{}, fmt.Errorf("segment %d/%d: %w", i+1, len(segments), err)
		}

		frameOffset := int(math.Round(seg.EncStart * fps))
		for j := range res.Frames {
			res.Frames[j].Time += seg.EncStart
			res.Frames[j].Frame += frameOffset
		}
		allFrames = append(allFrames, res.Frames...)
	}

	sort.Slice(allFrames, func(i, j int) bool { return allFrames[i].Time < allFrames[j].Time })

	vmafVals := make([]float64, len(allFrames))
	psnrVals := make([]float64, len(allFrames))
	ssimVals := make([]float64, len(allFrames))
	for i, f := range allFrames {
		vmafVals[i] = f.VMAF
		psnrVals[i] = f.PSNR
		ssimVals[i] = f.SSIM
	}

	return encodequality.Result{
		Frames: allFrames,
		VMAF:   encodequality.ComputePooledStats(vmafVals),
		PSNR:   encodequality.ComputePooledStats(psnrVals),
		SSIM:   encodequality.ComputePooledStats(ssimVals),
	}, nil
}
