package encodequality

import "testing"

func TestVMAFVerdict(t *testing.T) {
	cases := []struct {
		mean, min float64
		want      Verdict
	}{
		{95, 85, Green},
		{93, 80, Green},  // boundary
		{92, 85, Yellow}, // mean just under green
		{93, 79, Yellow}, // min just under green's floor
		{85, 70, Yellow}, // boundary
		{84, 70, Red},    // mean just under yellow
		{85, 69, Red},    // min just under yellow's floor
		{50, 50, Red},
	}
	for _, c := range cases {
		got := VMAFVerdict(c.mean, c.min)
		if got.Verdict != c.want {
			t.Errorf("VMAFVerdict(%v, %v) = %v, want %v", c.mean, c.min, got.Verdict, c.want)
		}
	}
}

func TestPSNRVerdict(t *testing.T) {
	cases := []struct {
		mean float64
		want Verdict
	}{
		{45, Green},
		{40, Green},
		{39, Yellow},
		{35, Yellow},
		{34, Red},
	}
	for _, c := range cases {
		if got := PSNRVerdict(c.mean); got.Verdict != c.want {
			t.Errorf("PSNRVerdict(%v) = %v, want %v", c.mean, got.Verdict, c.want)
		}
	}
}

func TestSSIMVerdict(t *testing.T) {
	cases := []struct {
		mean float64
		want Verdict
	}{
		{0.99, Green},
		{0.98, Green},
		{0.97, Yellow},
		{0.95, Yellow},
		{0.90, Red},
	}
	for _, c := range cases {
		if got := SSIMVerdict(c.mean); got.Verdict != c.want {
			t.Errorf("SSIMVerdict(%v) = %v, want %v", c.mean, got.Verdict, c.want)
		}
	}
}
