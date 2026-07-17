package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

type claudeCodeEngineAdapter struct{}

type claudeStreamEvent struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	UUID      string `json:"uuid"`
	Result    string `json:"result"`
	IsError   bool   `json:"is_error"`
	Error     string `json:"error"`
	Message   struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type claudeStreamRead struct {
	event claudeStreamEvent
	err   error
}

func (claudeCodeEngineAdapter) Execute(ctx context.Context, executionContext agentEngineExecutionContext, payload agentSessionPayload, updateRefs func(agentEngineExecution)) (agentEngineExecution, error) {
	execution := agentEngineExecution{AgentEngine: agentEngineClaudeCode}
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return execution, errors.New("Claude Code CLI is not available on PATH")
	}

	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"}
	if strings.TrimSpace(payload.DeveloperInstructions) != "" {
		args = append(args, "--append-system-prompt", payload.DeveloperInstructions)
	}
	if payload.Sandbox == "read-only" {
		args = append(args, "--permission-mode", "plan")
	} else if payload.ApprovalPolicy == "never" && payload.Sandbox == "danger-full-access" {
		args = append(args, "--dangerously-skip-permissions")
	}

	cmd, err := newAgentEngineCommand(claudePath, args...)
	if err != nil {
		return execution, err
	}
	cmd.Dir = payload.Workdir
	cmd.Env = defaultAgentEngineEnv(payload.Env)
	configureAgentEngineProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return execution, fmt.Errorf("claude stdin pipe: %w", err)
	}
	process, stdout, stderr, err := startAgentEngineProcess(cmd, stdin)
	if err != nil {
		return execution, fmt.Errorf("start Claude Code: %w", err)
	}
	defer process.stop(time.Second)
	go captureAgentEngineDiagnosticStream(ctx, executionContext, "claude-stderr", stderr)

	input := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": payload.Prompt,
		},
	}
	if err := json.NewEncoder(stdin).Encode(input); err != nil {
		return execution, fmt.Errorf("write Claude Code stream input: %w", err)
	}
	if err := stdin.Close(); err != nil {
		return execution, fmt.Errorf("close Claude Code stream input: %w", err)
	}
	process.stdin = nil

	reads := make(chan claudeStreamRead, 32)
	go scanClaudeStream(stdout, reads)
	for {
		select {
		case <-ctx.Done():
			process.stop(750 * time.Millisecond)
			return execution, context.Canceled
		case read, ok := <-reads:
			if !ok {
				if err := process.waitError(); err != nil {
					return execution, fmt.Errorf("Claude Code exited without terminal result: %w", err)
				}
				return execution, errors.New("Claude Code exited without terminal result")
			}
			if read.err != nil {
				return execution, read.err
			}
			event := read.event
			if sessionRef := opaqueEngineRef(event.SessionID); sessionRef != "" {
				execution.EngineSessionRef = sessionRef
			}
			if runRef := opaqueEngineRef(event.UUID); runRef != "" {
				execution.EngineRunRef = runRef
			}
			updateRefs(execution)
			logClaudeStreamEvent(ctx, executionContext, event)
			if event.Type != "result" {
				continue
			}
			if event.IsError || strings.Contains(strings.ToLower(event.Subtype), "error") {
				message := strings.TrimSpace(firstNonEmpty(event.Error, event.Result, "Claude Code returned an error result"))
				return execution, errors.New(message)
			}
			return execution, nil
		}
	}
}

func scanClaudeStream(reader io.Reader, reads chan<- claudeStreamRead) {
	defer close(reads)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), codexProtocolLineLimit)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event claudeStreamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			reads <- claudeStreamRead{err: fmt.Errorf("decode Claude Code stream-json event: %w", err)}
			return
		}
		reads <- claudeStreamRead{event: event}
	}
	if err := scanner.Err(); err != nil {
		reads <- claudeStreamRead{err: fmt.Errorf("read Claude Code stream-json output: %w", err)}
	}
}

func logClaudeStreamEvent(ctx context.Context, executionContext agentEngineExecutionContext, event claudeStreamEvent) {
	switch event.Type {
	case "assistant":
		for _, text := range messageContentText(event.Message.Content) {
			executionContext.log(ctx, "agent", text)
		}
	case "result":
		if strings.TrimSpace(event.Result) != "" {
			executionContext.log(ctx, "agent", event.Result)
		}
	}
}

func messageContentText(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil && strings.TrimSpace(text) != "" {
		return []string{strings.TrimSpace(text)}
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	result := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if (block.Type == "" || block.Type == "text") && strings.TrimSpace(block.Text) != "" {
			result = append(result, strings.TrimSpace(block.Text))
		}
	}
	return result
}

func captureAgentEngineDiagnosticStream(ctx context.Context, executionContext agentEngineExecutionContext, stream string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 16*1024), 1024*1024)
	for scanner.Scan() {
		executionContext.log(ctx, stream, truncate(scanner.Text(), 4000))
	}
}
