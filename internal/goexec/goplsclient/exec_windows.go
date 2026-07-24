//go:build windows

package goplsclient

import (
	"os/exec"
)

func setNewProcessGroup(cmd *exec.Cmd) {
}
