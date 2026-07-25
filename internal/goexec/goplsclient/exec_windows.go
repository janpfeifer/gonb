//go:build windows

package goplsclient

import (
	"os/exec"
)

// setNewProcessGroup is a no-op for Windows, it doesn't support process groups.
func setNewProcessGroup(cmd *exec.Cmd) {
}
