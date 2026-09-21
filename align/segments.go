package align

import (
	"math"
	"sort"
)

// ReconstructSegments groups anchors that share the same (source_time -
// encoded_time) offset, within tolerance, into matching segments -- a
// distinct offset value marks a cut between segments.
//
// Grouping starts from offset value alone, not time-adjacency: real audio
// has stretches (silence, uniform ambient noise) where no individual
// window correlates confidently enough to produce an anchor, so anchors
// belonging to one genuine segment are rarely perfectly consecutive in
// time. But offset alone isn't sufficient either -- two isolated,
// unrelated false-match anchors can coincidentally share a similar wrong
// offset purely by chance, and grouping by offset alone would then merge
// them into one bogus "segment" spanning the entire gap between them.
// So each offset-based group is additionally split wherever consecutive
// members (by encoded time) are more than maxGapSeconds apart: a genuine
// segment has anchors reasonably densely spread across it, not just two
// isolated points far apart agreeing by coincidence.
//
// Segments shorter than minSegmentSeconds, or backed by fewer than
// minAnchors corroborating points, are dropped: a pair of anchors close
// enough in time to pass the gap check can still just be two independent
// false matches that coincidentally agree -- weak evidence on its own,
// however long the span between them looks. A genuine segment should have
// several anchors agreeing across its span, not just a lone pair of them.
func ReconstructSegments(anchors []Anchor, offsetTolerance, maxGapSeconds, minSegmentSeconds float64, minAnchors int) []Segment {
	if len(anchors) == 0 {
		return nil
	}

	sorted := append([]Anchor(nil), anchors...)
	sort.Slice(sorted, func(i, j int) bool {
		return (sorted[i].SrcTime - sorted[i].EncTime) < (sorted[j].SrcTime - sorted[j].EncTime)
	})

	var segments []Segment
	emit := func(cluster []Anchor) {
		for _, run := range splitByTimeGap(cluster, maxGapSeconds) {
			encMin, encMax := run[0].EncTime, run[0].EncTime
			srcMin, srcMax := run[0].SrcTime, run[0].SrcTime
			for _, a := range run[1:] {
				encMin, encMax = math.Min(encMin, a.EncTime), math.Max(encMax, a.EncTime)
				srcMin, srcMax = math.Min(srcMin, a.SrcTime), math.Max(srcMax, a.SrcTime)
			}
			if encMax-encMin < minSegmentSeconds || len(run) < minAnchors {
				continue
			}
			segments = append(segments, Segment{EncStart: encMin, EncEnd: encMax, SrcStart: srcMin, SrcEnd: srcMax})
		}
	}

	clusterStart := 0
	// refOffset is fixed to the cluster's first (== smallest, since sorted
	// ascending) member, not updated per-accepted-anchor -- comparing new
	// candidates against a moving previous value lets a chain of small
	// steps drift arbitrarily far, silently merging unrelated offsets.
	refOffset := sorted[0].SrcTime - sorted[0].EncTime

	for i := 1; i <= len(sorted); i++ {
		if i < len(sorted) {
			offset := sorted[i].SrcTime - sorted[i].EncTime
			if math.Abs(offset-refOffset) <= offsetTolerance {
				continue
			}
		}
		emit(sorted[clusterStart:i])
		clusterStart = i
		if i < len(sorted) {
			refOffset = sorted[i].SrcTime - sorted[i].EncTime
		}
	}

	sort.Slice(segments, func(i, j int) bool { return segments[i].EncStart < segments[j].EncStart })
	return segments
}

// splitByTimeGap sorts a cluster by encoded time and breaks it into runs
// wherever consecutive members are more than maxGapSeconds apart.
func splitByTimeGap(cluster []Anchor, maxGapSeconds float64) [][]Anchor {
	byTime := append([]Anchor(nil), cluster...)
	sort.Slice(byTime, func(i, j int) bool { return byTime[i].EncTime < byTime[j].EncTime })

	var runs [][]Anchor
	runStart := 0
	for i := 1; i <= len(byTime); i++ {
		if i < len(byTime) && byTime[i].EncTime-byTime[i-1].EncTime <= maxGapSeconds {
			continue
		}
		runs = append(runs, byTime[runStart:i])
		runStart = i
	}
	return runs
}
