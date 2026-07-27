//go:build !windows

package goplsclient

import (
	"os/exec"
	"syscall"
)

func setNewProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}
}
