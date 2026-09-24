package encodequality

import (
	"math"
	"sort"
)

// DefaultLowest is how many lowest-scoring moments a report lists when the
// caller doesn't say.
const DefaultLowest = 5

// minMomentGap is the least time between two listed moments: a moment is a
// place to go and look, and two frames a second apart are the same place.
const minMomentGap = 2.0 // seconds

// LowestMoments returns up to n sampled frames with the lowest VMAF, worst
// first, each at least a gap apart in time so that one hard scene cannot
// fill the whole list. The gap is minMomentGap or, on a long file,
// span/(4n) -- enough that the moments spread across the file rather than
// stacking in its single worst minute, while still letting two land in the
// same act.
//
// Unlike DetectProblemAreas this has no threshold: it always answers
// "where would I look first", which is the question a clean or uniformly
// soft encode otherwise leaves unanswered (no frame falls far enough below
// the mean to be flagged, so no area is reported at all).
func LowestMoments(frames []FrameMetric, n int) []FrameMetric {
	if n <= 0 || len(frames) == 0 {
		return nil
	}
	first, last := frames[0].Time, frames[0].Time
	for _, f := range frames {
		first, last = math.Min(first, f.Time), math.Max(last, f.Time)
	}
	gap := math.Max(minMomentGap, (last-first)/float64(4*n))

	byScore := append([]FrameMetric(nil), frames...)
	sort.SliceStable(byScore, func(i, j int) bool { return byScore[i].VMAF < byScore[j].VMAF })

	var picked []FrameMetric
	for _, f := range byScore {
		near := false
		for _, p := range picked {
			if math.Abs(f.Time-p.Time) < gap {
				near = true
				break
			}
		}
		if near {
			continue
		}
		picked = append(picked, f)
		if len(picked) == n {
			break
		}
	}
	return picked
}
