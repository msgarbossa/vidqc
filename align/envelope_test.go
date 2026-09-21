package align

import (
	"math"
	"testing"
)

func TestEnvelope(t *testing.T) {
	// 3 full bins of a constant-amplitude signal: RMS of a constant is
	// just its absolute value.
	sampleRate := 100
	binSeconds := 1.0
	samples := make([]float32, 300)
	for i := range samples {
		samples[i] = 2.0
	}

	env := Envelope(samples, sampleRate, binSeconds)
	if len(env) != 3 {
		t.Fatalf("got %d bins, want 3", len(env))
	}
	for i, v := range env {
		if math.Abs(v-2.0) > 1e-9 {
			t.Errorf("bin %d = %v, want 2.0", i, v)
		}
	}
}

func TestEnvelopePartialLastBin(t *testing.T) {
	samples := make([]float32, 150) // 1.5 bins at binLen=100
	for i := range samples {
		samples[i] = 3.0
	}
	env := Envelope(samples, 100, 1.0)
	if len(env) != 2 {
		t.Fatalf("got %d bins, want 2 (including partial)", len(env))
	}
	if math.Abs(env[1]-3.0) > 1e-9 {
		t.Errorf("partial bin = %v, want 3.0", env[1])
	}
}

func TestEnvelopeEmpty(t *testing.T) {
	if env := Envelope(nil, 100, 1.0); env != nil {
		t.Errorf("Envelope(nil, ...) = %v, want nil", env)
	}
}
