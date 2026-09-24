//go:build windows

package check

import "strings"

// sameDevice compares drive/share roots as a proxy for "same device" on
// Windows, where there's no direct equivalent of a Unix device id exposed
// via os.Stat. Both paths should already be absolute.
func sameDevice(a, b string) (same, checked bool) {
	va, vb := volumeName(a), volumeName(b)
	if va == "" || vb == "" {
		return false, false
	}
	return strings.EqualFold(va, vb), true
}

// wasOOMKilled: Windows doesn't terminate processes via SIGKILL, and its
// default policy under memory pressure isn't an instant kill the way
// Linux's OOM killer or macOS's jetsam is -- so there's no equivalent
// signal to detect here.
func wasOOMKilled(err error) bool {
	return false
}
