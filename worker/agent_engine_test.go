package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseAgentSessionPayloadNormalizesEngineAndFailsClosed(t *testing.T) {
	workdir := t.TempDir()
	tests := []struct {
		name       string
		fields     map[string]any
		wantEngine string
		wantError  string
	}{
		{name: "legacy payload defaults to Codex", fields: map[string]any{"agentProfile": "custom-old-profile"}, wantEngine: agentEngineCodex},
		{name: "legacy Codex provider", fields: map[string]any{"provider": "codex"}, wantEngine: agentEngineCodex},
		{name: "unknown legacy provider", fields: map[string]any{"provider": "claude"}, wantError: `unsupported legacy agent provider "claude"`},
		{name: "legacy Claude Code provider", fields: map[string]any{"provider": "claude_code"}, wantError: `unsupported legacy agent provider "claude_code"`},
		{name: "legacy Pi provider", fields: map[string]any{"provider": "pi"}, wantError: `unsupported legacy agent provider "pi"`},
		{name: "explicit engine wins over legacy aliases", fields: map[string]any{"agentEngine": agentEnginePi, "provider": "codex", "agentProfile": "design"}, wantEngine: agentEnginePi},
		{name: "Claude Code", fields: map[string]any{"agentEngine": agentEngineClaudeCode}, wantEngine: agentEngineClaudeCode},
		{name: "Pi", fields: map[string]any{"agentEngine": agentEnginePi}, wantEngine: agentEnginePi},
		{name: "unknown explicit engine", fields: map[string]any{"agentEngine": "claude"}, wantError: `unsupported agentEngine "claude"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := map[string]any{"prompt": "test", "workdir": workdir}
			for key, value := range test.fields {
				input[key] = value
			}
			raw, err := json.Marshal(input)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			payload, err := parseAgentSessionPayload(raw)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("expected %q, got payload=%+v err=%v", test.wantError, payload, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse payload: %v", err)
			}
			if payload.AgentEngine != test.wantEngine {
				t.Fatalf("expected engine %q, got %q", test.wantEngine, payload.AgentEngine)
			}
		})
	}
}

func TestBuildAgentEngineEnvStripsControlPlaneCredentials(t *testing.T) {
	env := buildAgentEngineEnvForEngine(agentEngineCodex, []string{
		"PATH=/usr/local/bin:/usr/bin",
		"HOME=/worker-home",
		"OPENAI_API_KEY=codex-auth",
		"ANTHROPIC_API_KEY=unrelated-agent-auth",
		"NODE_OPTIONS=--require=/tmp/injected.js",
		"SSH_AUTH_SOCK=/private/ssh-agent.sock",
		"GPG_TTY=/dev/ttys001",
		"SENTRY_AUTH_TOKEN=unrelated-secret",
		"DATABASE_URL=postgres://control-plane",
		"MSPACE_RUNTIME_TOKEN=msw_secret",
		"MSPACE_RUNTIME_TOKEN_FILE=/worker/token",
		"MSPACE_GITHUB_APP_PRIVATE_KEY=private-key",
		"POSTGRES_PASSWORD=database-password",
		"GH_TOKEN=github-token",
	}, map[string]string{
		"MSPACE_SESSION_ID":         "session-123",
		"MSPACE_ISSUE_ID":           "issue-123",
		"MSPACE_AGENT_TOKEN":        "session-scoped-token",
		"MSPACE_RUNTIME_TOKEN":      "payload-msw-secret",
		"MSPACE_RUNTIME_TOKEN_FILE": "/payload/worker-token",
		"DATABASE_URL":              "postgres://payload-control-plane",
		"UNRELATED_TOKEN":           "payload-secret",
	})
	joined := "\n" + strings.Join(env, "\n") + "\n"
	for _, forbidden := range []string{
		"DATABASE_URL=",
		"MSPACE_RUNTIME_TOKEN=",
		"MSPACE_RUNTIME_TOKEN_FILE=",
		"MSPACE_GITHUB_APP_PRIVATE_KEY=",
		"POSTGRES_PASSWORD=",
		"GH_TOKEN=",
		"ANTHROPIC_API_KEY=",
		"NODE_OPTIONS=",
		"SENTRY_AUTH_TOKEN=",
		"SSH_AUTH_SOCK=",
		"GPG_TTY=",
		"UNRELATED_TOKEN=",
	} {
		if strings.Contains(joined, "\n"+forbidden) {
			t.Fatalf("agent environment leaked %s: %v", forbidden, env)
		}
	}
	for _, required := range []string{
		"PATH=/usr/local/bin:/usr/bin",
		"HOME=/worker-home",
		"OPENAI_API_KEY=codex-auth",
		"MSPACE_SESSION_ID=session-123",
		"MSPACE_ISSUE_ID=issue-123",
		"MSPACE_AGENT_TOKEN=session-scoped-token",
	} {
		if !strings.Contains(joined, "\n"+required+"\n") {
			t.Fatalf("agent environment lost %s: %v", required, env)
		}
	}
}

func TestBuildAgentEngineEnvKeepsOnlySelectedEngineAuthentication(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin",
		"HOME=/home/worker",
		"CODEX_HOME=/home/worker/.codex",
		"OPENAI_API_KEY=openai-secret",
		"CLAUDE_CODE_OAUTH_TOKEN=claude-oauth",
		"ANTHROPIC_API_KEY=anthropic-secret",
		"AWS_ACCESS_KEY_ID=bedrock-key",
		"AWS_SECRET_ACCESS_KEY=bedrock-secret",
		"SLACK_TOKEN=unrelated-secret",
	}
	claude := "\n" + strings.Join(buildAgentEngineEnvForEngine(agentEngineClaudeCode, parent, nil), "\n") + "\n"
	for _, required := range []string{"CLAUDE_CODE_OAUTH_TOKEN=claude-oauth", "ANTHROPIC_API_KEY=anthropic-secret", "AWS_SECRET_ACCESS_KEY=bedrock-secret"} {
		if !strings.Contains(claude, "\n"+required+"\n") {
			t.Fatalf("Claude environment lost %s: %s", required, claude)
		}
	}
	for _, forbidden := range []string{"CODEX_HOME=", "OPENAI_API_KEY=", "SLACK_TOKEN="} {
		if strings.Contains(claude, "\n"+forbidden) {
			t.Fatalf("Claude environment inherited %s: %s", forbidden, claude)
		}
	}
}

func TestClearWorkerCredentialEnvironmentRemovesParentDiscovery(t *testing.T) {
	t.Setenv("MSPACE_RUNTIME_TOKEN", "msw_parent_secret")
	t.Setenv("MSPACE_RUNTIME_TOKEN_FILE", "/Users/private/mspace/runtime.token")
	clearWorkerCredentialEnvironment()
	for _, key := range []string{"MSPACE_RUNTIME_TOKEN", "MSPACE_RUNTIME_TOKEN_FILE"} {
		if value, exists := os.LookupEnv(key); exists || value != "" {
			t.Fatalf("Worker credential environment %s remains discoverable: %q", key, value)
		}
	}
}

func TestClaudeCodeEngineUsesStreamJSONAndSharedSessionCore(t *testing.T) {
	installFakeEngine(t, "claude", "fake_claude.py")
	t.Setenv("MSPACE_RUNTIME_TOKEN", "msw_parent_secret")
	t.Setenv("MSPACE_RUNTIME_TOKEN_FILE", "/private/worker.token")
	t.Setenv("SENTRY_AUTH_TOKEN", "unrelated-parent-secret")
	executionContext, logs, closeServer := newAgentEngineTestContext(t)
	defer closeServer()
	sourceCapture := false
	payload := agentSessionPayload{
		AgentEngine:           agentEngineClaudeCode,
		Workdir:               t.TempDir(),
		Prompt:                "fix the source",
		DeveloperInstructions: "follow the mspace artifact contract",
		ApprovalPolicy:        "never",
		Sandbox:               "danger-full-access",
		SourceCapture:         &sourceCapture,
		Env: map[string]string{
			"MSPACE_FAKE_CLAUDE_MODE": "success",
			"MSPACE_AGENT_TOKEN":      "session-scoped-token",
			"MSPACE_RUNTIME_TOKEN":    "msw_payload_secret",
			"DATABASE_URL":            "postgres://payload-control-plane",
		},
	}
	result, err := runAgentSession(context.Background(), executionContext.RuntimeClient, executionContext.Config, executionContext.WorkerID, executionContext.TaskID, payload)
	if err != nil {
		t.Fatalf("run Claude Code session: %v", err)
	}
	if result.AgentEngine != agentEngineClaudeCode || result.EngineSessionRef != "claude-session-opaque" || result.EngineRunRef != "claude-run-opaque" {
		t.Fatalf("unexpected Claude result refs: %+v", result)
	}
	if result.ThreadID != "" || result.TurnID != "" {
		t.Fatalf("Claude result must not populate Codex legacy refs: %+v", result)
	}
	if result.ArtifactDir != filepath.Join(result.Workdir, ".mspace", "session") {
		t.Fatalf("expected shared core artifact dir, got %q", result.ArtifactDir)
	}
	if !strings.Contains(taskLogMessages(logs()), "fake Claude final result") {
		t.Fatalf("expected normalized agent log, got %s", taskLogMessages(logs()))
	}
}

func TestClaudeCodeEngineRequiresTerminalResult(t *testing.T) {
	installFakeEngine(t, "claude", "fake_claude.py")
	executionContext, _, closeServer := newAgentEngineTestContext(t)
	defer closeServer()
	payload := directEnginePayload(t, agentEngineClaudeCode)

	payload.Env = map[string]string{"MSPACE_FAKE_CLAUDE_MODE": "missing_result"}
	_, err := (claudeCodeEngineAdapter{}).Execute(context.Background(), executionContext, payload, func(agentEngineExecution) {})
	if err == nil || !strings.Contains(err.Error(), "without terminal result") {
		t.Fatalf("expected missing terminal result error, got %v", err)
	}

	payload.Env = map[string]string{"MSPACE_FAKE_CLAUDE_MODE": "failure"}
	_, err = (claudeCodeEngineAdapter{}).Execute(context.Background(), executionContext, payload, func(agentEngineExecution) {})
	if err == nil || !strings.Contains(err.Error(), "fake Claude failed") {
		t.Fatalf("expected terminal failure result, got %v", err)
	}
}

func TestPiEngineUsesRPCAndReturnsOnlyOpaqueSessionID(t *testing.T) {
	installFakeEngine(t, "pi", "fake_pi.py")
	executionContext, logs, closeServer := newAgentEngineTestContext(t)
	defer closeServer()
	payload := directEnginePayload(t, agentEnginePi)

	payload.Env = map[string]string{"MSPACE_FAKE_PI_MODE": "success"}
	result, err := (piEngineAdapter{}).Execute(context.Background(), executionContext, payload, func(agentEngineExecution) {})
	if err != nil {
		t.Fatalf("run Pi engine: %v", err)
	}
	if result.EngineSessionRef != "pi-session-opaque" || strings.Contains(result.EngineSessionRef, "/tmp/") {
		t.Fatalf("expected opaque Pi session id, got %+v", result)
	}
	piLogs := taskLogMessages(logs())
	if strings.Count(piLogs, "fake Pi completed") != 1 {
		t.Fatalf("expected exactly one normalized Pi agent log, got %s", piLogs)
	}
	if strings.Contains(piLogs, payload.DeveloperInstructions) {
		t.Fatalf("Pi developer instructions must not be echoed as user activity: %s", piLogs)
	}

	payload.Env = map[string]string{"MSPACE_FAKE_PI_MODE": "unsafe_session"}
	result, err = (piEngineAdapter{}).Execute(context.Background(), executionContext, payload, func(agentEngineExecution) {})
	if err != nil {
		t.Fatalf("run Pi engine with path-shaped session id: %v", err)
	}
	if result.EngineSessionRef != "" {
		t.Fatalf("path-shaped Pi session value must not escape the worker, got %q", result.EngineSessionRef)
	}
	if strings.Contains(taskLogMessages(logs()), "/tmp/private/pi-session.json") {
		t.Fatalf("Pi local session path must not escape through runtime logs: %s", taskLogMessages(logs()))
	}
	if !strings.Contains(taskLogMessages(logs()), "details suppressed") {
		t.Fatalf("expected safe Pi diagnostic signal, got %s", taskLogMessages(logs()))
	}
}

func TestPiEngineRedactsStructuredRuntimeErrors(t *testing.T) {
	installFakeEngine(t, "pi", "fake_pi.py")
	executionContext, logs, closeServer := newAgentEngineTestContext(t)
	defer closeServer()
	payload := directEnginePayload(t, agentEnginePi)
	payload.Env = map[string]string{"MSPACE_FAKE_PI_MODE": "runtime_error"}
	_, err := (piEngineAdapter{}).Execute(context.Background(), executionContext, payload, func(agentEngineExecution) {})
	if err == nil || err.Error() != "Pi RPC reported an error" {
		t.Fatalf("expected allowlisted Pi runtime error, got %v", err)
	}
	combined := err.Error() + "\n" + taskLogMessages(logs())
	for _, forbidden := range []string{"sessionFile", "/tmp/private", "/Users/private"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("Pi runtime diagnostics leaked %q: %s", forbidden, combined)
		}
	}
}

func TestPiEngineRequiresAgentEnd(t *testing.T) {
	installFakeEngine(t, "pi", "fake_pi.py")
	executionContext, _, closeServer := newAgentEngineTestContext(t)
	defer closeServer()
	payload := directEnginePayload(t, agentEnginePi)
	payload.Env = map[string]string{"MSPACE_FAKE_PI_MODE": "missing_agent_end"}
	_, err := (piEngineAdapter{}).Execute(context.Background(), executionContext, payload, func(agentEngineExecution) {})
	if err == nil || !strings.Contains(err.Error(), "without agent_end") {
		t.Fatalf("expected missing agent_end error, got %v", err)
	}
}

func TestPiEngineSendsAbortBeforeCancellation(t *testing.T) {
	installFakeEngine(t, "pi", "fake_pi.py")
	readyPath := filepath.Join(t.TempDir(), "ready")
	abortPath := filepath.Join(t.TempDir(), "abort.json")
	executionContext, _, closeServer := newAgentEngineTestContext(t)
	defer closeServer()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	payload := directEnginePayload(t, agentEnginePi)
	payload.Env = map[string]string{
		"MSPACE_FAKE_PI_MODE":         "slow",
		"MSPACE_FAKE_PI_READY":        readyPath,
		"MSPACE_FAKE_PI_ABORT_MARKER": abortPath,
	}
	go func() {
		_, err := (piEngineAdapter{}).Execute(ctx, executionContext, payload, func(agentEngineExecution) {})
		done <- err
	}()
	waitForTestFile(t, readyPath)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Pi adapter did not stop after cancellation")
	}
	abort, err := os.ReadFile(abortPath)
	if err != nil {
		t.Fatalf("read Pi abort marker: %v", err)
	}
	if !strings.Contains(string(abort), `"type":"abort"`) {
		t.Fatalf("expected Pi abort RPC, got %s", abort)
	}
}

func directEnginePayload(t *testing.T, engine string) agentSessionPayload {
	t.Helper()
	return agentSessionPayload{
		AgentEngine:           engine,
		Workdir:               t.TempDir(),
		Prompt:                "complete the task",
		DeveloperInstructions: "follow the mspace contract",
		ApprovalPolicy:        "never",
		Sandbox:               "danger-full-access",
	}
}

func installFakeEngine(t *testing.T, name, fixture string) {
	t.Helper()
	fixturePath, err := filepath.Abs(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatalf("resolve fake engine fixture: %v", err)
	}
	dir := t.TempDir()
	wrapper := "#!/bin/sh\nexec python3 " + shellQuote(fixturePath) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(wrapper), 0o755); err != nil {
		t.Fatalf("write fake %s executable: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func newAgentEngineTestContext(t *testing.T) (agentEngineExecutionContext, func() []appendTaskLogInput, func()) {
	t.Helper()
	var mu sync.Mutex
	var logs []appendTaskLogInput
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input appendTaskLogInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Errorf("decode agent engine log: %v", err)
		}
		mu.Lock()
		logs = append(logs, input)
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	client := &runtimeClient{baseURL: server.URL, token: "msw_test", httpClient: server.Client()}
	getLogs := func() []appendTaskLogInput {
		mu.Lock()
		defer mu.Unlock()
		return append([]appendTaskLogInput(nil), logs...)
	}
	return agentEngineExecutionContext{
		RuntimeClient: client,
		Config:        config{Name: "worker-test", Version: workerVersion},
		WorkerID:      "worker-engine-test",
		TaskID:        "task-engine-test",
	}, getLogs, server.Close
}

func waitForTestFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}
