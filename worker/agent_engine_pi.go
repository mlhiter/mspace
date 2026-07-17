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

type piEngineAdapter struct{}

type piRPCEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Command   string          `json:"command"`
	Success   *bool           `json:"success"`
	Error     string          `json:"error"`
	Message   json.RawMessage `json:"message"`
	Data      json.RawMessage `json:"data"`
	SessionID string          `json:"sessionId"`
}

type piRPCRead struct {
	event piRPCEvent
	err   error
}

func (piEngineAdapter) Execute(ctx context.Context, executionContext agentEngineExecutionContext, payload agentSessionPayload, updateRefs func(agentEngineExecution)) (agentEngineExecution, error) {
	execution := agentEngineExecution{
		AgentEngine:  agentEnginePi,
		EngineRunRef: opaqueEngineRef("mspace-" + shortTaskID(executionContext.TaskID)),
	}
	updateRefs(execution)

	piPath, err := exec.LookPath("pi")
	if err != nil {
		return execution, errors.New("Pi CLI is not available on PATH")
	}
	args := []string{"--mode", "rpc"}
	if instructions := strings.TrimSpace(payload.DeveloperInstructions); instructions != "" {
		args = append(args, "--append-system-prompt", instructions)
	}
	cmd, err := newAgentEngineCommand(piPath, args...)
	if err != nil {
		return execution, errors.New("Pi CLI could not be prepared")
	}
	cmd.Dir = payload.Workdir
	cmd.Env = defaultAgentEngineEnv(agentEnginePi, payload.Env)
	configureAgentEngineProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return execution, fmt.Errorf("pi stdin pipe: %w", err)
	}
	process, stdout, stderr, err := startAgentEngineProcess(cmd, stdin)
	if err != nil {
		return execution, errors.New("start Pi RPC: launch failed")
	}
	defer process.stop(time.Second)
	go capturePiDiagnosticStream(ctx, executionContext, stderr)

	encoder := json.NewEncoder(stdin)
	if err := encoder.Encode(map[string]any{"id": "mspace-state", "type": "get_state"}); err != nil {
		return execution, fmt.Errorf("write Pi get_state request: %w", err)
	}
	if err := encoder.Encode(map[string]any{"id": execution.EngineRunRef, "type": "prompt", "message": payload.Prompt}); err != nil {
		return execution, fmt.Errorf("write Pi prompt request: %w", err)
	}

	reads := make(chan piRPCRead, 32)
	go scanPiRPC(stdout, reads)
	for {
		select {
		case <-ctx.Done():
			_ = encoder.Encode(map[string]any{"id": "mspace-abort", "type": "abort"})
			select {
			case <-process.done:
			case <-time.After(500 * time.Millisecond):
			}
			process.stop(750 * time.Millisecond)
			return execution, context.Canceled
		case read, ok := <-reads:
			if !ok {
				if err := process.waitError(); err != nil {
					return execution, fmt.Errorf("Pi RPC exited without agent_end: %w", err)
				}
				return execution, errors.New("Pi RPC exited without agent_end")
			}
			if read.err != nil {
				return execution, read.err
			}
			event := read.event
			if event.Type == "response" && event.Command == "get_state" {
				if event.Success != nil && !*event.Success {
					return execution, errors.New("Pi get_state failed")
				}
				if sessionRef := piSessionRef(event); sessionRef != "" {
					execution.EngineSessionRef = sessionRef
					updateRefs(execution)
				}
			}
			if event.Type == "response" && event.Command == "prompt" && event.Success != nil && !*event.Success {
				return execution, errors.New("Pi prompt failed")
			}
			logPiRPCEvent(ctx, executionContext, event)
			if event.Type == "agent_end" {
				return execution, nil
			}
			if event.Type == "error" {
				return execution, errors.New("Pi RPC reported an error")
			}
		}
	}
}

func scanPiRPC(reader io.Reader, reads chan<- piRPCRead) {
	defer close(reads)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), codexProtocolLineLimit)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event piRPCEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			reads <- piRPCRead{err: fmt.Errorf("decode Pi RPC event: %w", err)}
			return
		}
		reads <- piRPCRead{event: event}
	}
	if err := scanner.Err(); err != nil {
		reads <- piRPCRead{err: fmt.Errorf("read Pi RPC output: %w", err)}
	}
}

// Pi stderr is provider-local and can contain sessionFile or absolute paths.
// Runtime logs receive only this fixed allowlisted signal, never stderr text.
func capturePiDiagnosticStream(ctx context.Context, executionContext agentEngineExecutionContext, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 16*1024), 1024*1024)
	logged := false
	for scanner.Scan() {
		if !logged && strings.TrimSpace(scanner.Text()) != "" {
			executionContext.log(ctx, "pi-stderr", "Pi emitted local diagnostic output; details suppressed.")
			logged = true
		}
	}
}

func piSessionRef(event piRPCEvent) string {
	if ref := opaqueEngineRef(event.SessionID); ref != "" {
		return ref
	}
	var data struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(event.Data, &data) != nil {
		return ""
	}
	return opaqueEngineRef(data.SessionID)
}

func logPiRPCEvent(ctx context.Context, executionContext agentEngineExecutionContext, event piRPCEvent) {
	switch event.Type {
	case "message_end":
		for _, text := range piMessageText(event.Message) {
			executionContext.log(ctx, "agent", text)
		}
	case "tool_execution_start":
		executionContext.log(ctx, "tool", "Pi tool execution started.")
	case "tool_execution_end":
		executionContext.log(ctx, "tool", "Pi tool execution completed.")
	}
}

func piMessageText(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &message) != nil || (message.Role != "" && message.Role != "assistant") {
		return nil
	}
	return messageContentText(message.Content)
}
