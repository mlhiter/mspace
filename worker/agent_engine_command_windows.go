//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var agentEnginePowerShellPath struct {
	sync.Once
	path string
	err  error
}

func newAgentEngineCommand(path string, args ...string) (*exec.Cmd, error) {
	extension := strings.ToLower(filepath.Ext(path))
	if extension != ".cmd" && extension != ".bat" {
		return exec.Command(path, args...), nil
	}

	shim := strings.TrimSuffix(path, filepath.Ext(path)) + ".ps1"
	if info, err := os.Stat(shim); err != nil || info.IsDir() {
		return nil, fmt.Errorf("agent CLI batch shim %s requires a matching PowerShell shim", path)
	}
	powershell, err := resolvedAgentEnginePowerShellPath()
	if err != nil {
		return nil, errors.New("PowerShell is required to run the Agent CLI batch shim")
	}
	commandArgs := []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", shim}
	commandArgs = append(commandArgs, args...)
	return exec.Command(powershell, commandArgs...), nil
}

func newAgentEngineCommandContext(ctx context.Context, path string, args ...string) (*exec.Cmd, error) {
	extension := strings.ToLower(filepath.Ext(path))
	if extension != ".cmd" && extension != ".bat" {
		return exec.CommandContext(ctx, path, args...), nil
	}

	shim := strings.TrimSuffix(path, filepath.Ext(path)) + ".ps1"
	if info, err := os.Stat(shim); err != nil || info.IsDir() {
		return nil, fmt.Errorf("agent CLI batch shim %s requires a matching PowerShell shim", path)
	}
	powershell, err := resolvedAgentEnginePowerShellPath()
	if err != nil {
		return nil, errors.New("PowerShell is required to run the Agent CLI batch shim")
	}
	commandArgs := []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", shim}
	commandArgs = append(commandArgs, args...)
	return exec.CommandContext(ctx, powershell, commandArgs...), nil
}

func resolvedAgentEnginePowerShellPath() (string, error) {
	agentEnginePowerShellPath.Do(func() {
		path, err := exec.LookPath("powershell.exe")
		if err != nil {
			agentEnginePowerShellPath.err = errors.New("PowerShell is required to run the Agent CLI batch shim")
			return
		}
		path, err = filepath.Abs(path)
		if err == nil {
			path, err = filepath.EvalSymlinks(path)
		}
		if err != nil {
			agentEnginePowerShellPath.err = errors.New("resolve PowerShell executable for Agent CLI batch shim")
			return
		}
		agentEnginePowerShellPath.path = filepath.Clean(path)
	})
	return agentEnginePowerShellPath.path, agentEnginePowerShellPath.err
}
