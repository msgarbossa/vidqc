// Package align finds which spans of two related files actually
// correspond to each other in time, when one is an edited (trimmed/cut)
// derivative of the other and the cut points aren't known in advance.
// It works from audio alone: a short-time energy envelope of each file's
// audio track is cross-correlated to find matching offsets, then grouped
// into contiguous segments wherever that offset holds steady -- a jump in
// the offset between segments marks an edit point.
//
// This package has no dependency on ffmpeg or exec -- callers decode PCM
// (e.g. via ffmpeg) and hand it envelopes/samples directly.
package align

// Anchor is one confirmed correspondence point: this moment in the
// encoded file's timeline matches this moment in the source's.
type Anchor struct {
	EncTime    float64
	SrcTime    float64
	Confidence float64 // Pearson correlation coefficient of the match, -1..1
}

// Segment is a contiguous stretch where the encoded and source timelines
// run in lockstep (a constant time offset) -- genuinely comparable
// content with no cut in between.
type Segment struct {
	EncStart, EncEnd float64
	SrcStart, SrcEnd float64
}
