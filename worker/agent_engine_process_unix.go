//go:build !windows

package main

import (
	"io"
	"os/exec"
	"syscall"
)

func attachAgentEngineProcessTree(_ *exec.Cmd) (io.Closer, error) {
	return nil, nil
}

func configureAgentEngineProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func interruptAgentEngineProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
	}
}

func killAgentEngineProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
