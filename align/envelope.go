package align

import "math"

// Envelope reduces a mono PCM stream to a short-time RMS energy value per
// bin -- a small, cheap-to-search summary that's still distinctive enough
// for cross-correlation to find real matches, without needing an FFT or
// full-resolution sample-by-sample search.
func Envelope(samples []float32, sampleRate int, binSeconds float64) []float64 {
	binLen := int(float64(sampleRate) * binSeconds)
	if binLen < 1 {
		binLen = 1
	}
	if len(samples) == 0 {
		return nil
	}
	n := (len(samples) + binLen - 1) / binLen
	env := make([]float64, n)
	for i := 0; i < n; i++ {
		start := i * binLen
		end := start + binLen
		if end > len(samples) {
			end = len(samples)
		}
		var sumSq float64
		for _, s := range samples[start:end] {
			v := float64(s)
			sumSq += v * v
		}
		env[i] = math.Sqrt(sumSq / float64(end-start))
	}
	return env
}
