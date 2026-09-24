# vidqc

Compares an encoded video against its source and reports VMAF, PSNR, and
SSIM as independent, explained verdicts, plus the worst-scoring moments in
the file and which metric(s) flagged each one. Built for deciding whether a
CRF/quality setting was high enough to trust an encode as a replacement for
its source.

Run `vidqc --help` for the full explanation of what each metric measures,
the verdict thresholds, and how problem areas are detected and ranked.

## Dependencies

**vidqc does not bundle ffmpeg.** It shells out to `ffmpeg` and `ffprobe`,
which must already be installed and on your `PATH`. `ffmpeg` specifically
needs to have been built with `--enable-libvmaf` (`vidqc` checks this at
startup and fails with a clear message if it's missing).

- **macOS**: `brew install ffmpeg` includes libvmaf support by default.
- **Linux**: distro packages vary — Debian/Ubuntu's `apt install ffmpeg` may
  or may not have libvmaf enabled depending on version; if not, use a
  build like the John Van Sickle static builds, or compile ffmpeg yourself
  with `--enable-libvmaf`.
- **Windows**: use a build from [gyan.dev](https://www.gyan.dev/ffmpeg/builds/)
  or [BtbN's builds](https://github.com/BtbN/FFmpeg-Builds) (both bundle
  libvmaf), or build from source with `--enable-libvmaf`.

Check with:

```sh
ffmpeg -hide_banner -filters | grep libvmaf
```

## Build

```sh
go build -o vidqc .
```

Requires Go 1.25+. No other build-time dependencies (no CGO, no C toolchain).

## Usage

```sh
./vidqc source.mp4 encoded.mp4
```

By default this samples ~1 frame/second (fast, still a solid statistical
read); `--thorough` evaluates every frame instead. See `--help` for all
options.

Source and encoded don't need to be frame-for-frame identical. If their
durations differ, vidqc automatically falls back to content-based
alignment: cross-correlating each file's audio track to find which spans
actually correspond (e.g. after a Shotcut trim or mid-timeline cut), then
comparing just those spans. This needs a real audio track in both files
and a confident match -- it errors out rather than silently producing a
misleading score if there isn't one. `--allow-length-mismatch` skips this
and falls back to the old behavior (capped to the shorter file) for when
you already know it's a clean trim at the very end.

## Releasing

Pushing a `vX.Y.Z` tag (`task tag VERSION=vX.Y.Z`, or manually) triggers a
GitHub Actions workflow that builds and publishes binaries + a
`checksums.txt` (sha256) to GitHub Releases via
[GoReleaser](https://goreleaser.com/): macOS (Apple Silicon only), Linux
(amd64 + arm64), and Windows (amd64).

To test the full release build matrix locally without publishing anything
(needs `brew install goreleaser` first):

```sh
task release:snapshot
```

`task build:local` is lighter-weight for everyday use: it just
cross-compiles for macOS arm64 + Linux amd64 (no goreleaser needed) into
`dist/`.

Run `task` with no arguments to list all available tasks.

## Library use

The scoring/analysis logic (parsing ffmpeg's libvmaf JSON log, computing
verdicts, detecting and ranking problem areas) lives in the `encodequality`
package and has no dependency on ffmpeg itself or on running as a CLI --
callers run ffmpeg (or already have a log from elsewhere) and hand it the
log path:

```go
import "github.com/msgarbossa/vidqc/encodequality"

res, err := encodequality.ParseLog(logPath, fps)
verdict := encodequality.VMAFVerdict(res.VMAF.Mean, res.VMAF.Min)
areas := encodequality.DetectProblemAreas(res.Frames, res.VMAF, res.PSNR, res.SSIM, 3)
```

Content-based alignment (matching two differently-edited files by audio)
is likewise a standalone, ffmpeg-free package, `align` -- callers hand it
PCM samples (or an already-computed energy envelope) and get back the
matching segments:

```go
import "github.com/msgarbossa/vidqc/align"

encEnv := align.Envelope(encSamples, sampleRate, 0.2)
srcEnv := align.Envelope(srcSamples, sampleRate, 0.2)
anchors := align.FindAnchors(encEnv, srcEnv, 0.2, 5.0, 4.0, 0.6)
segments := align.ReconstructSegments(anchors, 2.0, 90.0, 10.0, 3)
```

To run a whole check the way the CLI does -- the probes, the libvmaf pass,
the alignment fallback and the printed report -- use the `check` package.
The CLI is a thin wrapper over it, so a caller gets exactly the CLI's
output, written to whatever `io.Writer` it passes:

```go
import "github.com/msgarbossa/vidqc/check"

if err := check.Available(); err != nil { // ffmpeg, ffprobe, libvmaf
	return err
}
rep, err := check.Run(ctx, check.Options{
	Source:  source,
	Encoded: encoded,
	Effort:  encodequality.Quick, // or Medium (the zero value), Thorough
	Top:     3,                   // 0 = every flagged segment
	WorkDir: dir,                 // per-frame JSON; "" = a temp dir, removed after
}, w)
// rep.Result holds the scores; rep.DataPath the per-frame JSON (or glob).
```

Everything goes to `w` in the CLI's order, except the CLI's closing "Full
per-frame data" line, which is left to the caller. Color is off unless
`Options.Color` is set, and ffmpeg's own progress output is discarded
unless `Options.Stderr` is set. Cancelling `ctx` kills the running ffmpeg
and makes `Run` return `ctx.Err()`. `Run` does not check for the tools
itself: call `Available` first (once, and cache it, if you run many
checks).

## License

MIT -- see [LICENSE](LICENSE).
