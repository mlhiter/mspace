package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	agentEngineCodex      = "codex"
	agentEngineClaudeCode = "claude_code"
	agentEnginePi         = "pi"
)

type agentEngineExecution struct {
	AgentEngine      string
	EngineSessionRef string
	EngineRunRef     string
	LegacyThreadID   string
	LegacyTurnID     string
}

type agentEngineAdapter interface {
	Execute(context.Context, agentEngineExecutionContext, agentSessionPayload, func(agentEngineExecution)) (agentEngineExecution, error)
}

type agentEngineExecutionContext struct {
	RuntimeClient *runtimeClient
	Config        config
	WorkerID      string
	TaskID        string
}

func (c agentEngineExecutionContext) log(ctx context.Context, stream, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	_ = c.RuntimeClient.appendTaskLog(ctx, c.WorkerID, c.TaskID, appendTaskLogInput{Stream: stream, Message: message})
}

func normalizeAgentEngine(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case agentEngineCodex:
		return agentEngineCodex, nil
	case agentEngineClaudeCode:
		return agentEngineClaudeCode, nil
	case agentEnginePi:
		return agentEnginePi, nil
	default:
		return "", fmt.Errorf("unsupported agentEngine %q", strings.TrimSpace(value))
	}
}

func agentEngineAdapterFor(engine string) (agentEngineAdapter, error) {
	switch engine {
	case agentEngineCodex:
		return codexEngineAdapter{}, nil
	case agentEngineClaudeCode:
		return claudeCodeEngineAdapter{}, nil
	case agentEnginePi:
		return piEngineAdapter{}, nil
	default:
		return nil, fmt.Errorf("unsupported agentEngine %q", engine)
	}
}

func agentEngineCapabilityKey(engine string) (string, error) {
	switch engine {
	case agentEngineCodex:
		return "codex", nil
	case agentEngineClaudeCode:
		return "claudeCode", nil
	case agentEnginePi:
		return "pi", nil
	default:
		return "", fmt.Errorf("unsupported agentEngine %q", engine)
	}
}

func runAgentSession(ctx context.Context, runtimeClient *runtimeClient, cfg config, workerID, taskID string, payload agentSessionPayload) (agentSessionResult, error) {
	prepared, err := prepareAgentSessionWorkspace(ctx, runtimeClient, cfg, workerID, taskID, payload)
	if err != nil {
		return agentSessionResult{}, err
	}
	return runPreparedAgentSession(ctx, runtimeClient, cfg, workerID, taskID, prepared)
}

func runPreparedAgentSession(ctx context.Context, runtimeClient *runtimeClient, cfg config, workerID, taskID string, payload agentSessionPayload) (agentSessionResult, error) {
	if testArtifactsReady(payload) {
		return completeAgentSessionFromArtifacts(ctx, runtimeClient, workerID, taskID, payload, agentEngineExecution{AgentEngine: payload.AgentEngine})
	}

	adapter, err := agentEngineAdapterFor(payload.AgentEngine)
	if err != nil {
		return agentSessionResult{}, err
	}
	executionContext := agentEngineExecutionContext{
		RuntimeClient: runtimeClient,
		Config:        cfg,
		WorkerID:      workerID,
		TaskID:        taskID,
	}
	execution, artifactCompleted, err := executeAgentEngine(ctx, adapter, executionContext, payload)
	if err != nil {
		if testArtifactsReady(payload) {
			return completeAgentSessionFromArtifacts(context.WithoutCancel(ctx), runtimeClient, workerID, taskID, payload, execution)
		}
		result := newAgentSessionResult(payload, execution)
		result.Status = "failed"
		if errors.Is(err, context.Canceled) {
			result.Status = "cancelled"
		}
		result.WorkingCopy = inspectIssueWorkingCopy(context.WithoutCancel(ctx), payload, issueWorkingCopyRecoveryReason(err))
		return result, err
	}
	if artifactCompleted {
		return completeAgentSessionFromArtifacts(context.WithoutCancel(ctx), runtimeClient, workerID, taskID, payload, execution)
	}
	return completeAgentSession(ctx, runtimeClient, workerID, taskID, payload, execution)
}

type agentEngineExecutionUpdate struct {
	result agentEngineExecution
	err    error
}

func executeAgentEngine(ctx context.Context, adapter agentEngineAdapter, executionContext agentEngineExecutionContext, payload agentSessionPayload) (agentEngineExecution, bool, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var refsMu sync.Mutex
	refs := agentEngineExecution{AgentEngine: payload.AgentEngine}
	updateRefs := func(update agentEngineExecution) {
		refsMu.Lock()
		refs = mergeAgentEngineExecution(refs, update)
		refsMu.Unlock()
	}
	currentRefs := func() agentEngineExecution {
		refsMu.Lock()
		defer refsMu.Unlock()
		return refs
	}

	done := make(chan agentEngineExecutionUpdate, 1)
	go func() {
		result, err := adapter.Execute(runCtx, executionContext, payload, updateRefs)
		updateRefs(result)
		done <- agentEngineExecutionUpdate{result: result, err: err}
	}()

	if !canCompleteFromTestArtifacts(payload) {
		select {
		case <-ctx.Done():
			cancel()
			update := waitForAgentEngineStop(done, currentRefs())
			if update.err != nil && !errors.Is(update.err, context.Canceled) {
				return update.result, false, update.err
			}
			return update.result, false, context.Canceled
		case update := <-done:
			return mergeAgentEngineExecution(currentRefs(), update.result), false, update.err
		}
	}

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if testArtifactsReady(payload) {
			cancel()
			update := waitForAgentEngineStop(done, currentRefs())
			if update.err != nil && !errors.Is(update.err, context.Canceled) {
				return mergeAgentEngineExecution(currentRefs(), update.result), false, update.err
			}
			return mergeAgentEngineExecution(currentRefs(), update.result), true, nil
		}
		select {
		case <-ctx.Done():
			cancel()
			update := waitForAgentEngineStop(done, currentRefs())
			if update.err != nil && !errors.Is(update.err, context.Canceled) {
				return mergeAgentEngineExecution(currentRefs(), update.result), false, update.err
			}
			return mergeAgentEngineExecution(currentRefs(), update.result), false, context.Canceled
		case <-ticker.C:
		case update := <-done:
			execution := mergeAgentEngineExecution(currentRefs(), update.result)
			if update.err != nil {
				return execution, false, update.err
			}
			if testArtifactsReadiness(payload) == testArtifactPending {
				ready, waitErr := waitForTestArtifactsReady(ctx, payload, testArtifactCompletionSettleTimeout)
				if waitErr != nil {
					return execution, false, waitErr
				}
				if ready {
					return execution, true, nil
				}
				return execution, false, fmt.Errorf("test result references screenshot files that are not available: %s", strings.Join(testResultArtifactPendingScreenshotPaths(payload), ", "))
			}
			if testArtifactsReady(payload) {
				return execution, true, nil
			}
			return execution, false, missingTestCompletionArtifactError(payload)
		}
	}
}

func waitForAgentEngineStop(done <-chan agentEngineExecutionUpdate, fallback agentEngineExecution) agentEngineExecutionUpdate {
	select {
	case update := <-done:
		update.result = mergeAgentEngineExecution(fallback, update.result)
		return update
	case <-time.After(5 * time.Second):
		return agentEngineExecutionUpdate{result: fallback, err: errors.New("agent engine did not stop after cancellation")}
	}
}

func mergeAgentEngineExecution(base, update agentEngineExecution) agentEngineExecution {
	if update.AgentEngine != "" {
		base.AgentEngine = update.AgentEngine
	}
	if update.EngineSessionRef != "" {
		base.EngineSessionRef = update.EngineSessionRef
	}
	if update.EngineRunRef != "" {
		base.EngineRunRef = update.EngineRunRef
	}
	if update.LegacyThreadID != "" {
		base.LegacyThreadID = update.LegacyThreadID
	}
	if update.LegacyTurnID != "" {
		base.LegacyTurnID = update.LegacyTurnID
	}
	return base
}

func completeAgentSession(ctx context.Context, runtimeClient *runtimeClient, workerID, taskID string, payload agentSessionPayload, execution agentEngineExecution) (agentSessionResult, error) {
	result := newAgentSessionResult(payload, execution)
	if sourceCaptureEnabled(payload) {
		source, err := captureAgentSessionSource(ctx, runtimeClient, workerID, taskID, payload)
		if err != nil {
			result.WorkingCopy = inspectIssueWorkingCopy(context.WithoutCancel(ctx), payload, issueWorkingCopyRecoveryReason(err))
			return result, err
		}
		result.Source = source
	} else {
		_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Source capture disabled for this agent session."})
	}
	result.attachArtifacts(payload)
	if pullRequestHandoffArtifactRequired(payload) && result.PullRequest == nil {
		return result, missingPullRequestHandoffArtifactError(payload)
	}
	result.WorkingCopy = inspectIssueWorkingCopy(context.WithoutCancel(ctx), payload, "")
	if result.WorkingCopy != nil && result.WorkingCopy.ContentState == "recovery_required" {
		return result, errors.New("issue working copy could not be verified after source capture")
	}
	if result.WorkingCopy != nil && result.WorkingCopy.ContentState == "dirty" {
		return result, errors.New("issue working copy remains dirty after source capture")
	}
	return result, nil
}

func completeAgentSessionFromArtifacts(ctx context.Context, runtimeClient *runtimeClient, workerID, taskID string, payload agentSessionPayload, execution agentEngineExecution) (agentSessionResult, error) {
	result := newAgentSessionResult(payload, execution)
	if !result.attachMatchingTestCompletionArtifact(payload) {
		return agentSessionResult{}, errors.New("test artifact completion was detected but no matching artifact could be attached")
	}
	if sourceCaptureEnabled(payload) {
		source, err := captureAgentSessionSource(ctx, runtimeClient, workerID, taskID, payload)
		if err != nil {
			_ = runtimeClient.appendTaskLog(context.WithoutCancel(ctx), workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Artifact completion fallback skipped source capture: " + err.Error()})
		} else {
			result.Source = source
		}
	} else {
		_ = runtimeClient.appendTaskLog(context.WithoutCancel(ctx), workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Source capture disabled for this agent session."})
	}
	_ = runtimeClient.appendTaskLog(context.WithoutCancel(ctx), workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Completing task from session artifacts after the agent engine ended without terminal completion."})
	result.WorkingCopy = inspectIssueWorkingCopy(context.WithoutCancel(ctx), payload, "")
	return result, nil
}

func newAgentSessionResult(payload agentSessionPayload, execution agentEngineExecution) agentSessionResult {
	result := agentSessionResult{
		AgentEngine:      execution.AgentEngine,
		EngineSessionRef: execution.EngineSessionRef,
		EngineRunRef:     execution.EngineRunRef,
		Status:           "completed",
		CompletedAt:      time.Now().UTC().Format(time.RFC3339),
		DryRun:           false,
		Workdir:          payload.Workdir,
		ArtifactDir:      payload.ArtifactDir,
	}
	if execution.AgentEngine == agentEngineCodex {
		result.ThreadID = firstNonEmpty(execution.LegacyThreadID, execution.EngineSessionRef)
		result.TurnID = firstNonEmpty(execution.LegacyTurnID, execution.EngineRunRef)
	}
	return result
}

func opaqueEngineRef(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 512 || strings.ContainsRune(value, '\x00') || strings.ContainsAny(value, `/\\`) {
		return ""
	}
	return value
}
