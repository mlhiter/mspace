//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func newAgentEngineCommand(path string, args ...string) (*exec.Cmd, error) {
	extension := strings.ToLower(filepath.Ext(path))
	if extension != ".cmd" && extension != ".bat" {
		return exec.Command(path, args...), nil
	}

	shim := strings.TrimSuffix(path, filepath.Ext(path)) + ".ps1"
	if info, err := os.Stat(shim); err != nil || info.IsDir() {
		return nil, fmt.Errorf("agent CLI batch shim %s requires a matching PowerShell shim", path)
	}
	powershell, err := exec.LookPath("powershell.exe")
	if err != nil {
		return nil, errors.New("PowerShell is required to run the Agent CLI batch shim")
	}
	commandArgs := []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", shim}
	commandArgs = append(commandArgs, args...)
	return exec.Command(powershell, commandArgs...), nil
}
