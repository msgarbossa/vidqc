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
