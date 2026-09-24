package encodequality

import "testing"

func TestSubsampleFor(t *testing.T) {
	cases := []struct {
		fps  float64
		e    Effort
		want int
	}{
		{30, Medium, 30},
		{30, Quick, 90},
		{30, Thorough, 1},
		{24, Medium, 24},
		{0.2, Quick, 1}, // rounds to <1, floored at 1
	}
	for _, c := range cases {
		if got := SubsampleFor(c.fps, c.e); got != c.want {
			t.Errorf("SubsampleFor(%v, %v) = %v, want %v", c.fps, c.e, got, c.want)
		}
	}
}

func TestSubsampleForClip(t *testing.T) {
	cases := []struct {
		name          string
		fps, duration float64
		e             Effort
		want          int
	}{
		{"feature length keeps 1/s", 30, 7200, Medium, 30},
		{"five minutes is already enough", 30, 300, Medium, 30},
		{"18s phone clip scores every frame", 29.97, 17.9, Medium, 1},
		{"one-minute clip densified to >= 300 samples", 30, 60, Medium, 6},
		{"quick floor is lower", 30, 60, Quick, 18},
		{"thorough untouched", 30, 10, Thorough, 1},
		{"unknown duration falls back", 30, 0, Medium, 30},
	}
	for _, c := range cases {
		got := SubsampleForClip(c.fps, c.duration, c.e)
		if got != c.want {
			t.Errorf("%s: SubsampleForClip(%v, %v, %v) = %d, want %d", c.name, c.fps, c.duration, c.e, got, c.want)
		}
		if c.duration > 0 && c.e != Thorough && got > 1 {
			if samples := c.fps * c.duration / float64(got); samples < 100 {
				t.Errorf("%s: only %.0f samples", c.name, samples)
			}
		}
	}
}
