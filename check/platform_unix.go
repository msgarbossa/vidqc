//go:build unix

package check

import (
	"os"
	"syscall"
)

// sameDevice reports whether two paths live on the same filesystem device,
// used to warn when reading source+encoded at once could starve one stream
// (especially over USB/external drives). checked is false when the
// underlying stat info isn't available to compare.
func sameDevice(a, b string) (same, checked bool) {
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false, false
	}
	statA, okA := infoA.Sys().(*syscall.Stat_t)
	statB, okB := infoB.Sys().(*syscall.Stat_t)
	if !okA || !okB {
		return false, false
	}
	return statA.Dev == statB.Dev, true
}

// wasOOMKilled reports whether a *exec.Cmd exit error indicates the process
// was killed with SIGKILL -- the signature of the OS OOM killer (Linux) or
// jetsam (macOS) stepping in, as opposed to ffmpeg simply erroring out.
func wasOOMKilled(err error) bool {
	exitErr, ok := asExitError(err)
	if !ok {
		return false
	}
	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return false
	}
	return ws.Signaled() && ws.Signal() == syscall.SIGKILL
}
