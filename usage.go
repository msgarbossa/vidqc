package main

const usage = `Usage: vidqc [options] <source> <encoded> [output_dir]

Compares an encoded video against its source using ffmpeg's libvmaf filter,
reporting three independently-scored metrics plus the worst-scoring moments
in the file.

What VMAF, PSNR, and SSIM each measure:
  VMAF   A perceptual model (0-100) trained on human quality ratings. It's
         the one to trust most, because its scale is calibrated to what
         people actually notice -- unlike the other two, which are plain
         signal-error math over pixels.
  PSNR   Peak Signal-to-Noise Ratio, in dB (higher = closer to the source).
         Extremely sensitive to ANY pixel difference, including completely
         imperceptible noise, so it can look "bad" on a file that looks
         fine to the eye.
  SSIM   Structural Similarity (0-1). Compares luminance/contrast/structure
         rather than raw pixel values -- more perceptually relevant than
         PSNR, but still not calibrated to human ratings the way VMAF is.

  Because they measure different things, they can disagree -- and that
  disagreement is informative. See "Problem areas" below.

Verdict thresholds (independent per metric):
  VMAF    GREEN  mean >= 93 and min >= 80
          YELLOW mean >= 85 and min >= 70
          RED    below that
  PSNR    GREEN  mean >= 40 dB
          YELLOW mean >= 35 dB
          RED    below that
  SSIM    GREEN  mean >= 0.98
          YELLOW mean >= 0.95
          RED    below that
  These are common rules of thumb, not guarantees for every source -- heavy
  grain, animation, or unusual content can score differently than they
  look. Treat a red/yellow verdict as "go look at it", not an automatic
  answer, and treat VMAF's verdict as the primary one.

Problem areas:
  Every sampled frame is checked against this run's OWN average for each
  metric (not a fixed floor) -- flagged when it falls more than 10 VMAF
  points, 5 PSNR dB, or 0.03 SSIM below that metric's mean. Adjacent
  flagged frames are merged into one segment (a scene, not a frame), and
  segments are ranked by how severe their worst trigger was. The report
  names which metric(s) triggered each segment, because the PATTERN is
  diagnostic:
    - VMAF drops, PSNR/SSIM stay normal  -> usually a genuinely harder
      scene to compress (motion, detail, grain), not a defect.
    - VMAF collapses near 0 while PSNR/SSIM barely move -> usually a
      frame-alignment/timestamp glitch, not real quality loss.
    - All three drop together -> a real, visible quality dip in that scene.
    - Only PSNR/SSIM drop, VMAF doesn't -> a pixel-level difference the
      perceptual model didn't consider significant; usually imperceptible.
  Timestamps always refer to the ENCODED file's own timeline (the file
  you'd actually open to check a problem spot), even when alignment (below)
  had to map it to a different position in the source to compare it.

Sampling effort (mutually exclusive; --subsample overrides both):
  (default)                     Medium: ~1 sample/second of source video --
                                fast, still a solid statistical read.
  -q, --quick                   ~1 sample every 3 seconds. A quick triage
                                check (e.g. right after encoding with some
                                other tool), not a final verdict.
  --thorough                    Every frame (subsample=1). Slower and much
                                more memory-hungry; use for a final, rigorous
                                check on a short clip, not a long file.

Content-based alignment:
  Source and encoded don't need to be frame-for-frame identical. If their
  durations differ by more than 0.5s, vidqc falls back automatically (no
  flag needed) to matching them by audio content -- cross-correlating a
  short-time energy envelope of each file's audio track to find which
  spans actually correspond, even when the cut points aren't known. Each
  matched span is then compared with the normal VMAF/PSNR/SSIM pass, and
  the results combined; the report shows which spans were used (and so,
  implicitly, what was cut). This needs an audio track in both files, and
  a real, honest match -- it errors out rather than guessing if there's no
  audio, or too little of the file matches confidently, rather than
  produce a plausible-looking but meaningless score. Only a genuinely
  unrelated pair, or one where a segment was itself re-edited (a color
  grade, a crop) beyond a straight cut, is likely to fail this way.

Options:
  -s, --subsample N            Evaluate every Nth frame instead of an effort
                                preset above.
  -t, --threads N               libvmaf worker threads. Default: min(8, CPU
                                cores). Lower this if you hit an OOM kill.
  -m, --allow-length-mismatch   Skip content-based alignment and fall back
                                to the old behavior: cap the comparison to
                                the shorter file's length. Only trust this
                                if you already know the difference is a
                                clean trim at the very end -- it can't tell
                                that apart from a mid-video cut, which would
                                silently desync every frame after it.
  --top N                       Number of problem areas to report (default
                                3; 0 = report all flagged segments).
  --no-color                    Disable colored output (also respects the
                                NO_COLOR env var).
  -h, --help                    Show this help and exit.
  -v, --version                 Print the version and exit.

Notes:
  - Full per-frame data is written to <output_dir>/<encoded_basename>.vmaf.json.
  - If source and encoded are on the same (especially external/USB) drive,
    reading both at once can starve one stream and blow up memory; the
    tool warns when it detects this.
  - Requires ffmpeg (built with --enable-libvmaf) and ffprobe on PATH.
`
