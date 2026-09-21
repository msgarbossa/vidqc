package align

import (
	"math"
	"math/rand"
	"testing"
)

func randEnvelope(n int, seed int64) []float64 {
	r := rand.New(rand.NewSource(seed))
	env := make([]float64, n)
	for i := range env {
		env[i] = r.Float64()
	}
	return env
}

func TestBestMatchFindsExactSlice(t *testing.T) {
	haystack := randEnvelope(300, 1)
	window := append([]float64(nil), haystack[120:150]...)

	offset, conf, ok := bestMatch(window, haystack)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if offset != 120 {
		t.Errorf("offset = %d, want 120", offset)
	}
	if conf < 0.999 {
		t.Errorf("confidence = %v, want ~1.0 for an exact slice", conf)
	}
}

func TestBestMatchRejectsFlatWindow(t *testing.T) {
	haystack := randEnvelope(100, 2)
	flat := make([]float64, 10) // all zeros -- no variance
	_, _, ok := bestMatch(flat, haystack)
	if ok {
		t.Error("expected ok=false for a zero-variance window")
	}
}

func TestBestMatchHaystackShorterThanWindow(t *testing.T) {
	_, _, ok := bestMatch(make([]float64, 10), make([]float64, 5))
	if ok {
		t.Error("expected ok=false when haystack is shorter than window")
	}
}

// TestFindAnchorsAndReconstructSegments_MiddleCut is the core scenario
// this package exists for: the encoded timeline is the source with a
// contiguous chunk removed from the middle (a Shotcut-style cut), and we
// don't know where. Anchors should reveal two segments with a shift in
// source-time exactly matching the length of the removed chunk.
func TestFindAnchorsAndReconstructSegments_MiddleCut(t *testing.T) {
	const (
		srcLen   = 400
		cutStart = 150
		cutEnd   = 250 // 100 bins (seconds, since binSeconds=1 below) removed
		binSecs  = 1.0
	)
	srcEnv := randEnvelope(srcLen, 42)
	encEnv := append(append([]float64(nil), srcEnv[:cutStart]...), srcEnv[cutEnd:]...)

	anchors := FindAnchors(encEnv, srcEnv, binSecs, 5, 10, 0.95)
	if len(anchors) < 10 {
		t.Fatalf("got only %d anchors, expected plenty across ~300 bins", len(anchors))
	}

	segments := ReconstructSegments(anchors, 1.0, 15, 20, 3)
	if len(segments) != 2 {
		t.Fatalf("got %d segments, want 2 (before/after the cut): %+v", len(segments), segments)
	}

	first, second := segments[0], segments[1]

	// First segment should map 1:1 (no offset) and stay within the
	// pre-cut region.
	if first.EncStart > 5 || first.SrcStart > 5 {
		t.Errorf("first segment should start near 0: %+v", first)
	}
	if first.EncEnd > cutStart {
		t.Errorf("first segment (enc) shouldn't cross the cut point %d: %+v", cutStart, first)
	}

	// Second segment's source-time should be shifted by exactly the
	// removed chunk's length (cutEnd-cutStart = 100) relative to its
	// encoded-time, since that's how encEnv was constructed.
	wantOffset := float64(cutEnd - cutStart)
	gotOffset := second.SrcStart - second.EncStart
	if math.Abs(gotOffset-wantOffset) > 1.0 {
		t.Errorf("second segment offset = %v, want ~%v (the cut length): %+v", gotOffset, wantOffset, second)
	}
}

func TestReconstructSegments_NoAnchors(t *testing.T) {
	if segs := ReconstructSegments(nil, 1.0, 10.0, 1.0, 1); segs != nil {
		t.Errorf("ReconstructSegments(nil, ...) = %v, want nil", segs)
	}
}

// TestReconstructSegments_NoDriftChaining guards against a real bug found
// during development: comparing each candidate against the *previous
// accepted* anchor's offset (rather than a fixed cluster reference) lets a
// chain of small, individually-within-tolerance steps drift arbitrarily
// far, silently merging anchors whose offsets differ by far more than
// offsetTolerance into one bogus "segment".
func TestReconstructSegments_NoDriftChaining(t *testing.T) {
	var anchors []Anchor
	for i := 0; i < 10; i++ {
		enc := float64(i * 10)
		anchors = append(anchors, Anchor{EncTime: enc, SrcTime: enc + float64(i)}) // offsets 0..9, step 1
	}

	segs := ReconstructSegments(anchors, 1.5, 15.0, 5.0, 1)
	if len(segs) < 2 {
		t.Fatalf("got %d segment(s), want multiple -- offsets spanning 0..9 with tolerance 1.5"+
			" must not all merge into one via drift chaining: %+v", len(segs), segs)
	}
	for _, s := range segs {
		gotSpread := math.Abs((s.SrcEnd - s.EncEnd) - (s.SrcStart - s.EncStart))
		if gotSpread > 1.5 {
			t.Errorf("segment %+v has internal offset spread %v, want <= 1.5", s, gotSpread)
		}
	}
}

func TestReconstructSegments_DropsShortSegments(t *testing.T) {
	anchors := []Anchor{
		{EncTime: 0, SrcTime: 0},
		{EncTime: 2, SrcTime: 2}, // 2s segment, below minSegmentSeconds
		{EncTime: 10, SrcTime: 50},
		{EncTime: 30, SrcTime: 70},
	}
	segs := ReconstructSegments(anchors, 1.0, 25.0, 5.0, 1)
	if len(segs) != 1 {
		t.Fatalf("got %d segments, want 1 (short one dropped): %+v", len(segs), segs)
	}
}

// TestReconstructSegments_RejectsDistantCoincidence guards against a real
// bug found during development: offset-based clustering, on its own, will
// merge two isolated anchors that are far apart in time but coincidentally
// share a similar (wrong) offset into one bogus "segment" spanning the
// entire gap between them. maxGapSeconds should reject this.
func TestReconstructSegments_RejectsDistantCoincidence(t *testing.T) {
	anchors := []Anchor{
		{EncTime: 100, SrcTime: 100 - 23.4},
		{EncTime: 500, SrcTime: 500 - 25.2}, // similar offset, but 400s away with nothing in between
	}
	segs := ReconstructSegments(anchors, 2.0, 60.0, 10.0, 1)
	if len(segs) != 0 {
		t.Errorf("got %d segments, want 0 -- two isolated anchors 400s apart must not form a segment: %+v", len(segs), segs)
	}
}

// TestReconstructSegments_RejectsWeakPair guards against another real bug:
// two anchors close enough together (within maxGapSeconds) and agreeing
// on offset can still just be a coincidental pair of independent false
// matches -- weak evidence regardless of the span between them. minAnchors
// should require more corroboration than that before trusting a segment.
func TestReconstructSegments_RejectsWeakPair(t *testing.T) {
	anchors := []Anchor{
		{EncTime: 100, SrcTime: 100 + 771.0},
		{EncTime: 190, SrcTime: 190 + 770.2}, // 90s apart, within maxGapSeconds, only 2 anchors
	}
	segs := ReconstructSegments(anchors, 2.0, 90.0, 10.0, 3)
	if len(segs) != 0 {
		t.Errorf("got %d segments, want 0 -- a lone pair of anchors is too weak to trust: %+v", len(segs), segs)
	}
}
