//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestAgentEngineProcessStopTerminatesUnixDescendants(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	cmd := exec.Command("sh", "-c", `sleep 30 & child=$!; echo "$child" > "$PID_PATH"; wait`)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "PID_PATH=" + pidPath}
	configureAgentEngineProcess(cmd)
	process, stdout, stderr, err := startAgentEngineProcess(cmd, nil)
	if err != nil {
		t.Fatalf("start process tree fixture: %v", err)
	}
	defer stdout.(interface{ Close() error }).Close()
	defer stderr.(interface{ Close() error }).Close()
	waitForTestFile(t, pidPath)
	pidText, err := osReadFileTrimmed(pidPath)
	if err != nil {
		t.Fatalf("read child pid: %v", err)
	}
	pid, err := strconv.Atoi(pidText)
	if err != nil {
		t.Fatalf("parse child pid %q: %v", pidText, err)
	}

	process.stop(50 * time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("descendant process %d survived process-tree stop", pid)
}

func osReadFileTrimmed(path string) (string, error) {
	content, err := os.ReadFile(path)
	return strings.TrimSpace(string(content)), err
}
