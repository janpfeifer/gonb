//go:build linux

package goexec

import (
	"fmt"
	"github.com/pkg/errors"
	"os"
)

// CurrentWorkingDirectoryForPid returns the "cwd" or an error.
func CurrentWorkingDirectoryForPid(pid int) (string, error) {
	cwdPath := fmt.Sprintf("/proc/%d/cwd", pid)
	data, err := os.Readlink(cwdPath)
	if err != nil {
		return "", errors.Wrapf(err, "cannot find current working directory (cwd) for pid %d",
			pid)
	}
	return string(data), nil
}
