package encodequality

import (
	"encoding/json"
	"fmt"
	"os"
)

// rawLog mirrors the subset of ffmpeg's libvmaf log_fmt=json output this
// package needs. Unknown fields (aggregate_metrics, version, params, the
// other psnr_cb/psnr_cr/float_ms_ssim scores, ...) are ignored by
// encoding/json rather than modeled here.
type rawLog struct {
	Frames []struct {
		FrameNum int `json:"frameNum"`
		Metrics  struct {
			VMAF      float64 `json:"vmaf"`
			PSNRY     float64 `json:"psnr_y"`
			FloatSSIM float64 `json:"float_ssim"`
		} `json:"metrics"`
	} `json:"frames"`
	PooledMetrics struct {
		VMAF      rawStat `json:"vmaf"`
		PSNRY     rawStat `json:"psnr_y"`
		FloatSSIM rawStat `json:"float_ssim"`
	} `json:"pooled_metrics"`
}

type rawStat struct {
	Mean         float64 `json:"mean"`
	Min          float64 `json:"min"`
	Max          float64 `json:"max"`
	HarmonicMean float64 `json:"harmonic_mean"`
}

// ParseLog reads a libvmaf JSON log and computes each frame's timestamp from
// its frame number and the source's frame rate.
func ParseLog(path string, fps float64) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("reading vmaf log: %w", err)
	}

	var raw rawLog
	if err := json.Unmarshal(data, &raw); err != nil {
		return Result{}, fmt.Errorf("parsing vmaf log: %w", err)
	}
	if len(raw.Frames) == 0 {
		return Result{}, fmt.Errorf("vmaf log %s contains no frames", path)
	}
	if fps <= 0 {
		return Result{}, fmt.Errorf("invalid frame rate %v", fps)
	}

	res := Result{
		Frames: make([]FrameMetric, len(raw.Frames)),
		VMAF:   PooledStat(raw.PooledMetrics.VMAF),
		PSNR:   PooledStat(raw.PooledMetrics.PSNRY),
		SSIM:   PooledStat(raw.PooledMetrics.FloatSSIM),
	}
	for i, f := range raw.Frames {
		res.Frames[i] = FrameMetric{
			Frame: f.FrameNum,
			Time:  float64(f.FrameNum) / fps,
			VMAF:  f.Metrics.VMAF,
			PSNR:  f.Metrics.PSNRY,
			SSIM:  f.Metrics.FloatSSIM,
		}
	}
	return res, nil
}
