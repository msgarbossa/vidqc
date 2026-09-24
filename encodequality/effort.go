package encodequality

import "math"

// Effort controls how many frames get sampled: more samples costs more
// time/memory but gives a more statistically solid read. Centralized here
// (rather than in the CLI) so any caller -- the CLI's -q/--thorough flags,
// or a future integration calling this package directly -- gets the exact
// same quick/medium/thorough definitions instead of each redefining them.
type Effort int

const (
	Medium   Effort = iota // default: a solid statistical read
	Quick                  // fast triage, not a final verdict
	Thorough               // every frame: slow and memory-hungry, for a final check
)

func (e Effort) String() string {
	switch e {
	case Quick:
		return "quick"
	case Thorough:
		return "thorough"
	default:
		return "medium"
	}
}

// SubsampleFor turns an Effort level into a libvmaf n_subsample interval for
// a given source frame rate:
//
//	Quick    ~1 sample every 3 seconds.
//	Medium   ~1 sample/second (the default).
//	Thorough every frame.
func SubsampleFor(fps float64, e Effort) int {
	var n int
	switch e {
	case Quick:
		n = int(math.Round(fps * 3))
	case Thorough:
		n = 1
	default:
		n = int(math.Round(fps))
	}
	if n < 1 {
		n = 1
	}
	return n
}

// Minimum sampled frames per effort level. The time-based intervals above
// suit a feature-length file, where 1 sample/second is thousands of
// samples; on a phone clip they leave a handful (an 18-second clip at
// Medium scores 18 frames), too few for a percentile to mean anything or
// for more than one problem area to surface. Sampling is densified until
// the clip yields at least this many -- or every frame, whichever is fewer
// -- which only ever affects clips shorter than about five minutes, so the
// extra work is bounded by these counts rather than by the clip.
const (
	mediumMinSamples = 300
	quickMinSamples  = 100
)

// SubsampleForClip is SubsampleFor with the minimum-sample floor above
// applied for a clip of the given duration in seconds. A duration <= 0
// (unknown) returns SubsampleFor unchanged.
func SubsampleForClip(fps, duration float64, e Effort) int {
	n := SubsampleFor(fps, e)
	min := mediumMinSamples
	switch e {
	case Quick:
		min = quickMinSamples
	case Thorough:
		return n
	}
	if duration <= 0 {
		return n
	}
	frames := fps * duration
	if frames/float64(n) >= float64(min) {
		return n
	}
	dense := int(math.Floor(frames / float64(min)))
	if dense < 1 {
		dense = 1
	}
	return dense
}
