package main

import (
	"sync/atomic"
	"testing"
)

type countingProcessTreeCloser struct {
	calls atomic.Int32
}

func (c *countingProcessTreeCloser) Close() error {
	c.calls.Add(1)
	return nil
}

func TestAgentEngineProcessReleasesProcessTreeOnce(t *testing.T) {
	closer := &countingProcessTreeCloser{}
	process := &agentEngineProcess{processTree: closer}
	process.closeProcessTree()
	process.closeProcessTree()
	if calls := closer.calls.Load(); calls != 1 {
		t.Fatalf("process tree close calls = %d, want 1", calls)
	}
}
