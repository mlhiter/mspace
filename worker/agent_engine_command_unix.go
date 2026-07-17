//go:build !windows

package main

import (
	"context"
	"os/exec"
)

func newAgentEngineCommand(path string, args ...string) (*exec.Cmd, error) {
	return exec.Command(path, args...), nil
}

func newAgentEngineCommandContext(ctx context.Context, path string, args ...string) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, path, args...), nil
}
