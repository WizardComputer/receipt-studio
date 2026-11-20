//go:build windows

package printer

import (
	"os/exec"
	"syscall"
)

func configureLocalCommand(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
