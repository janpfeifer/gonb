//go:build !linux

package goexec

import (
	"k8s.io/klog/v2"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

// CurrentWorkingDirectoryForPid returns the "cwd" or an error.
//
// It achieves this by using `lsof`, that needs to be installed.
// Hopefully this is a more portable implementation for finding the "cwd" of a process.
//
// TODO: test this at least for Darwin/MacOS. Alternatively, use the implementation described here:
//
//	https://golang.hotexamples.com/examples/c/-/proc_pidpath/golang-proc_pidpath-function-examples.html
func CurrentWorkingDirectoryForPid(pid int) (string, error) {
	cmd := exec.Command("lsof", "-p", strconv.Itoa(pid), "-a", "-d", "cwd", "-Fn")
	klog.Infof("Executing %q", cmd)
	cmd.Stderr = os.Stderr
	output, err := cmd.Output()
	if err != nil {
		return "", errors.Wrapf(err, "failed to find current-working-directory for Jupyter pid %d", pid)
	}
	if klog.V(2).Enabled() {
		klog.Infof("%s output:\n%s\n", cmd, string(output))
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "n") {
			return strings.TrimPrefix(line, "n"), nil
		}
	}
	return "", errors.Errorf("failed to find current-working-directory for Jupyter pid %d -- lsof output was %q",
		pid, output)
}
