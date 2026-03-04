//go:build !windows

package main

import "os/exec"

func configureCommandForPlatform(cmd *exec.Cmd) {
	_ = cmd
}
