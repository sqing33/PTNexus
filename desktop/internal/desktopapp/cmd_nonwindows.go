//go:build !windows

package desktopapp

import "os/exec"

func configureCommandForPlatform(cmd *exec.Cmd) {
	_ = cmd
}
