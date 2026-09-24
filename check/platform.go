package check

import (
	"errors"
	"os/exec"
	"path/filepath"
)

func asExitError(err error) (*exec.ExitError, bool) {
	var exitErr *exec.ExitError
	ok := errors.As(err, &exitErr)
	return exitErr, ok
}

func volumeName(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	return filepath.VolumeName(abs)
}
