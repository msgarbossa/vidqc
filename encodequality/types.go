// Package encodequality parses ffmpeg libvmaf JSON logs and turns the raw
// VMAF/PSNR/SSIM numbers into verdicts and flagged problem areas a human can
// act on. It has no dependency on ffmpeg itself -- callers run ffmpeg and
// hand this package the resulting log path.
package encodequality

// FrameMetric holds one sampled frame's scores.
type FrameMetric struct {
	Frame int
	Time  float64 // seconds, Frame/FPS
	VMAF  float64
	PSNR  float64 // psnr_y (luma only)
	SSIM  float64 // float_ssim
}

// PooledStat mirrors libvmaf's own pooled_metrics block for one metric.
type PooledStat struct {
	Mean         float64
	Min          float64
	Max          float64
	HarmonicMean float64
}

// Result is everything parsed out of one libvmaf JSON log.
type Result struct {
	Frames []FrameMetric
	VMAF   PooledStat
	PSNR   PooledStat
	SSIM   PooledStat
}

// Verdict is the traffic-light read on a metric's pooled score.
type Verdict int

const (
	Red Verdict = iota
	Yellow
	Green
)

func (v Verdict) String() string {
	switch v {
	case Green:
		return "GREEN"
	case Yellow:
		return "YELLOW"
	default:
		return "RED"
	}
}

// MetricVerdict is a metric's pooled score plus the traffic-light read on it.
type MetricVerdict struct {
	Verdict Verdict
	Meaning string // one-line human explanation of what this verdict implies
}

// Trigger names one metric that crossed its own flag threshold within a
// ProblemArea, and the worst (lowest) value it reached there.
type Trigger struct {
	Metric string // "VMAF", "PSNR", or "SSIM"
	Worst  float64
}

// ProblemArea is a contiguous stretch of flagged frames -- a candidate
// "worth reviewing before you trust this encode" moment.
type ProblemArea struct {
	StartTime, EndTime   float64
	StartFrame, EndFrame int
	Triggers             []Trigger // metrics that crossed their flag threshold here
	VMAFWorst            float64   // worst value of each metric in the segment,
	PSNRWorst            float64   // shown for context even when that metric
	SSIMWorst            float64   // itself didn't trigger
	Severity             float64
	Note                 string
}
