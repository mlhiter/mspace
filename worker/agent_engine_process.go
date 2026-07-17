package main

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type agentEngineProcess struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	done        chan struct{}
	mu          sync.Mutex
	err         error
	outputs     []io.Closer
	outputsOnce sync.Once
}

func startAgentEngineProcess(cmd *exec.Cmd, stdin io.WriteCloser) (*agentEngineProcess, io.Reader, io.Reader, error) {
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter
	if err := cmd.Start(); err != nil {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		_ = stderrReader.Close()
		_ = stderrWriter.Close()
		return nil, nil, nil, err
	}
	process := &agentEngineProcess{
		cmd:     cmd,
		stdin:   stdin,
		done:    make(chan struct{}),
		outputs: []io.Closer{stdoutWriter, stderrWriter},
	}
	go func() {
		err := cmd.Wait()
		process.closeOutputs()
		process.mu.Lock()
		process.err = err
		process.mu.Unlock()
		close(process.done)
	}()
	return process, stdoutReader, stderrReader, nil
}

func (p *agentEngineProcess) waitError() error {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *agentEngineProcess) stop(grace time.Duration) {
	if p == nil || p.cmd == nil {
		return
	}
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	select {
	case <-p.done:
		return
	default:
	}
	interruptAgentEngineProcess(p.cmd)
	p.closeOutputs()
	if grace > 0 {
		select {
		case <-p.done:
			return
		case <-time.After(grace):
		}
	}
	killAgentEngineProcess(p.cmd)
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
	}
}

func (p *agentEngineProcess) closeOutputs() {
	if p == nil {
		return
	}
	p.outputsOnce.Do(func() {
		for _, output := range p.outputs {
			_ = output.Close()
		}
	})
}

func defaultAgentEngineEnv(extra map[string]string) []string {
	return buildAgentEngineEnv(os.Environ(), extra)
}

func buildAgentEngineEnv(parent []string, extra map[string]string) []string {
	values := make([]string, 0, len(parent)+len(extra))
	for _, entry := range parent {
		key, _, _ := strings.Cut(entry, "=")
		if isControlPlaneEnvironmentKey(key) {
			continue
		}
		values = append(values, entry)
	}
	return append(values, payloadEnv(extra)...)
}

func isControlPlaneEnvironmentKey(key string) bool {
	key = strings.ToUpper(strings.TrimSpace(key))
	if strings.HasPrefix(key, "MSPACE_") || strings.HasPrefix(key, "POSTGRES_") {
		return true
	}
	switch key {
	case "DATABASE_URL", "PGPASSWORD", "PGPASSFILE", "GH_TOKEN", "GITHUB_TOKEN":
		return true
	default:
		return false
	}
}

func drainAgentEngineDiagnosticStream(reader io.Reader) {
	_, _ = io.Copy(io.Discard, reader)
}
