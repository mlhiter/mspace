//go:build windows

package main

import (
	"os"
	"os/exec"
	"strconv"
)

func configureAgentEngineProcess(_ *exec.Cmd) {}

func interruptAgentEngineProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(os.Interrupt)
	}
}

func killAgentEngineProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		if err := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F").Run(); err != nil {
			_ = cmd.Process.Kill()
		}
	}
}
