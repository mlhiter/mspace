package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCommandAgentEngineDiagnoserReportsReadyAndSanitizesEnvironment(t *testing.T) {
	dir := t.TempDir()
	writeFakeEngine(t, dir, "codex", `
case "$*" in
  "--version") printf 'codex-cli 1.2.3\nignored line\n' ;;
  "login status") exit 0 ;;
  *) exit 9 ;;
esac`)
	writeFakeEngine(t, dir, "claude", `
case "$*" in
  "--version") printf 'claude 4.5.6\n' ;;
  "auth status --json") printf '{"loggedIn":true,"account":"must-not-persist","authMethod":"secret"}\n' ;;
  *) exit 9 ;;
esac`)
	writeFakeEngine(t, dir, "pi", `
case "$*" in
  "--version") printf 'pi 7.8.9\n' ;;
  *) exit 9 ;;
esac`)
	t.Setenv("PATH", dir)
	t.Setenv("MSPACE_RUNTIME_TOKEN", "msw_secret")
	t.Setenv("MSPACE_RUNTIME_TOKEN_FILE", "/private/worker-token")
	t.Setenv("MSPACE_CONTROL_PLANE_URL", "https://control-plane.invalid")
	t.Setenv("DATABASE_URL", "postgres://control-plane")
	t.Setenv("POSTGRES_PASSWORD", "database-secret")
	t.Setenv("GITHUB_TOKEN", "github-secret")

	checkedAt := time.Date(2026, 7, 17, 8, 30, 0, 0, time.UTC)
	diagnoser := newCommandAgentEngineDiagnoser(json.RawMessage(`{"codex":true,"claudeCode":true,"pi":true}`))
	diagnoser.commandTimeout = 5 * time.Second
	diagnoser.now = func() time.Time { return checkedAt }
	diagnostics := diagnoser.Diagnose(context.Background())

	assertEngineDiagnostic(t, diagnostics[agentEngineCodex], agentEngineDiagnosticReady, "auth_ok", "codex-cli 1.2.3", checkedAt)
	assertEngineDiagnostic(t, diagnostics[agentEngineClaudeCode], agentEngineDiagnosticReady, "auth_ok", "claude 4.5.6", checkedAt)
	assertEngineDiagnostic(t, diagnostics[agentEnginePi], agentEngineDiagnosticUnverified, "probe_unsupported", "pi 7.8.9", checkedAt)

	encoded, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatalf("marshal diagnostics: %v", err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"account", "authMethod", "msw_secret", "/private/worker-token", "postgres://", "github-secret", dir} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("diagnostics leaked %q: %s", forbidden, text)
		}
	}
}

func TestCommandAgentEngineDiagnoserReportsMissingAndNeedsSetup(t *testing.T) {
	dir := t.TempDir()
	writeFakeEngine(t, dir, "codex", `
case "$*" in
  "--version") printf 'codex 1\n' ;;
  "login status") exit 1 ;;
esac`)
	writeFakeEngine(t, dir, "claude", `
case "$*" in
  "--version") printf 'claude 2\n' ;;
  "auth status --json") printf '{"logged_in":false}\n'; exit 1 ;;
esac`)
	t.Setenv("PATH", dir)

	diagnoser := newCommandAgentEngineDiagnoser(json.RawMessage(`{"codex":true,"claudeCode":true,"pi":true}`))
	diagnoser.commandTimeout = 5 * time.Second
	diagnostics := diagnoser.Diagnose(context.Background())
	if diagnostics[agentEngineCodex].Status != agentEngineDiagnosticNeedsSetup || diagnostics[agentEngineCodex].ReasonCode != "auth_required" {
		t.Fatalf("unexpected Codex diagnostic: %+v", diagnostics[agentEngineCodex])
	}
	if diagnostics[agentEngineClaudeCode].Status != agentEngineDiagnosticNeedsSetup || diagnostics[agentEngineClaudeCode].ReasonCode != "auth_required" {
		t.Fatalf("unexpected Claude diagnostic: %+v", diagnostics[agentEngineClaudeCode])
	}
	if diagnostics[agentEnginePi].Status != agentEngineDiagnosticMissing || diagnostics[agentEnginePi].ReasonCode != "executable_not_found" {
		t.Fatalf("unexpected Pi diagnostic: %+v", diagnostics[agentEnginePi])
	}
}

func TestCommandAgentEngineDiagnoserReportsProbeFailures(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeEngine(t, dir, "codex", `/bin/sleep 1 & wait`)
		writeFakeEngine(t, dir, "claude", `/bin/sleep 1 & wait`)
		t.Setenv("PATH", dir)

		diagnoser := newCommandAgentEngineDiagnoser(json.RawMessage(`{"codex":true,"claudeCode":true,"pi":false}`))
		diagnoser.commandTimeout = 20 * time.Millisecond
		diagnostics := diagnoser.Diagnose(context.Background())
		for _, engine := range []string{agentEngineCodex, agentEngineClaudeCode} {
			diagnostic := diagnostics[engine]
			if diagnostic.Status != agentEngineDiagnosticProbeError || diagnostic.ReasonCode != "probe_timeout" {
				t.Fatalf("unexpected %s timeout diagnostic: %+v", engine, diagnostic)
			}
		}
	})

	t.Run("malformed Claude JSON", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeEngine(t, dir, "claude", `
case "$*" in
  "--version") printf 'claude 2\n' ;;
  "auth status --json") printf '{not-json}\n' ;;
esac`)
		t.Setenv("PATH", dir)

		diagnoser := newCommandAgentEngineDiagnoser(json.RawMessage(`{"claudeCode":true}`))
		diagnoser.commandTimeout = 5 * time.Second
		diagnostic := diagnoser.Diagnose(context.Background())[agentEngineClaudeCode]
		if diagnostic.Status != agentEngineDiagnosticProbeError || diagnostic.ReasonCode != "probe_malformed" {
			t.Fatalf("unexpected malformed diagnostic: %+v", diagnostic)
		}
	})

	t.Run("launch error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "codex")
		if err := os.WriteFile(path, []byte("not an executable format"), 0o755); err != nil {
			t.Fatalf("write invalid executable: %v", err)
		}
		t.Setenv("PATH", dir)

		diagnoser := newCommandAgentEngineDiagnoser(json.RawMessage(`{"codex":true}`))
		diagnoser.commandTimeout = 5 * time.Second
		diagnostic := diagnoser.Diagnose(context.Background())[agentEngineCodex]
		if diagnostic.Status != agentEngineDiagnosticProbeError || diagnostic.ReasonCode != "probe_launch_failed" {
			t.Fatalf("unexpected launch diagnostic: %+v", diagnostic)
		}
	})
}

func TestCommandAgentEngineDiagnoserDoesNotProbeDisabledEngines(t *testing.T) {
	dir := t.TempDir()
	writeFakeEngine(t, dir, "codex", `exit 97`)
	writeFakeEngine(t, dir, "claude", `exit 97`)
	writeFakeEngine(t, dir, "pi", `exit 97`)
	t.Setenv("PATH", dir)

	diagnostics := newCommandAgentEngineDiagnoser(
		json.RawMessage(`{"codex":false,"claudeCode":false,"pi":false}`),
	).Diagnose(context.Background())
	for _, engine := range []string{agentEngineCodex, agentEngineClaudeCode, agentEnginePi} {
		diagnostic := diagnostics[engine]
		if diagnostic.Status != agentEngineDiagnosticUnverified || diagnostic.ReasonCode != "disabled_by_configuration" {
			t.Fatalf("disabled engine %s was probed or misreported: %+v", engine, diagnostic)
		}
	}

	missingDir := t.TempDir()
	t.Setenv("PATH", missingDir)
	diagnostics = newCommandAgentEngineDiagnoser(
		json.RawMessage(`{"codex":false,"claudeCode":false,"pi":false}`),
	).Diagnose(context.Background())
	for _, engine := range []string{agentEngineCodex, agentEngineClaudeCode, agentEnginePi} {
		diagnostic := diagnostics[engine]
		if diagnostic.Status != agentEngineDiagnosticMissing || diagnostic.ReasonCode != "executable_not_found" {
			t.Fatalf("disabled missing engine %s was not reported as missing: %+v", engine, diagnostic)
		}
	}
}

func TestCommandAgentEngineDiagnoserUsesStartupResolvedExecutable(t *testing.T) {
	trustedDir := t.TempDir()
	writeFakeEngine(t, trustedDir, "codex", `
case "$*" in
  "--version") printf 'trusted-codex 1\n' ;;
  "login status") exit 0 ;;
  *) exit 9 ;;
esac`)
	untrustedDir := t.TempDir()
	writeFakeEngine(t, untrustedDir, "codex", `exit 97`)
	t.Setenv("PATH", trustedDir)
	diagnoser := newCommandAgentEngineDiagnoser(json.RawMessage(`{"codex":true}`))
	diagnoser.commandTimeout = 5 * time.Second

	t.Setenv("PATH", untrustedDir)
	diagnostic := diagnoser.Diagnose(context.Background())[agentEngineCodex]
	if diagnostic.Status != agentEngineDiagnosticReady || diagnostic.Version != "trusted-codex 1" {
		t.Fatalf("diagnoser followed a changed PATH instead of its startup executable: %+v", diagnostic)
	}
}

func TestBuildAgentEngineProbeEnvUsesMinimalEngineSpecificAllowlist(t *testing.T) {
	parent := []string{
		"PATH=/trusted/bin:/usr/bin",
		"HOME=/worker-home",
		"TMPDIR=/tmp/worker",
		"CODEX_HOME=/worker/codex",
		"OPENAI_API_KEY=openai-secret",
		"CLAUDE_CONFIG_DIR=/worker/claude",
		"CLAUDE_CODE_OAUTH_TOKEN=claude-oauth",
		"ANTHROPIC_API_KEY=anthropic-secret",
		"ANTHROPIC_AUTH_TOKEN=anthropic-token",
		"NODE_OPTIONS=--require=/tmp/injected.js",
		"AWS_SECRET_ACCESS_KEY=cloud-secret",
		"RANDOM_SECRET=random-secret",
		"MSPACE_RUNTIME_TOKEN=msw_secret",
		"DATABASE_URL=postgres://control-plane",
		"GITHUB_TOKEN=github-secret",
	}

	assertProbeEnvironment := func(t *testing.T, engine string, includeAuth bool, required, forbidden []string) {
		t.Helper()
		joined := "\n" + strings.Join(buildAgentEngineProbeEnv(parent, engine, includeAuth), "\n") + "\n"
		for _, key := range required {
			if !strings.Contains(joined, "\n"+key+"=") {
				t.Fatalf("%s probe environment omitted %s: %s", engine, key, joined)
			}
		}
		for _, key := range forbidden {
			if strings.Contains(joined, "\n"+key+"=") {
				t.Fatalf("%s probe environment inherited %s: %s", engine, key, joined)
			}
		}
	}

	commonForbidden := []string{"NODE_OPTIONS", "RANDOM_SECRET", "MSPACE_RUNTIME_TOKEN", "DATABASE_URL", "GITHUB_TOKEN"}
	assertProbeEnvironment(t, agentEngineCodex, true,
		[]string{"PATH", "HOME", "TMPDIR", "CODEX_HOME", "OPENAI_API_KEY"},
		append(commonForbidden, "CLAUDE_CONFIG_DIR", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "AWS_SECRET_ACCESS_KEY"),
	)
	assertProbeEnvironment(t, agentEngineClaudeCode, true,
		[]string{"PATH", "HOME", "TMPDIR", "CLAUDE_CONFIG_DIR", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "AWS_SECRET_ACCESS_KEY"},
		append(commonForbidden, "CODEX_HOME", "OPENAI_API_KEY"),
	)
	assertProbeEnvironment(t, agentEnginePi, false,
		[]string{"PATH", "HOME", "TMPDIR"},
		append(commonForbidden, "CODEX_HOME", "OPENAI_API_KEY", "CLAUDE_CONFIG_DIR", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "AWS_SECRET_ACCESS_KEY"),
	)
	assertProbeEnvironment(t, agentEngineCodex, false,
		[]string{"PATH", "HOME", "TMPDIR"},
		append(commonForbidden, "CODEX_HOME", "OPENAI_API_KEY", "CLAUDE_CONFIG_DIR", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "AWS_SECRET_ACCESS_KEY"),
	)
	assertProbeEnvironment(t, agentEngineClaudeCode, false,
		[]string{"PATH", "HOME", "TMPDIR"},
		append(commonForbidden, "CODEX_HOME", "OPENAI_API_KEY", "CLAUDE_CONFIG_DIR", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "AWS_SECRET_ACCESS_KEY"),
	)
}

func TestAgentEngineDiagnosticStateMasksDisabledEngineResults(t *testing.T) {
	state := newAgentEngineDiagnosticState(
		json.RawMessage(`{"codex":false,"claudeCode":false,"pi":false}`),
		agentEngineDiagnoserFunc(func(context.Context) agentEngineDiagnostics {
			return agentEngineDiagnostics{
				agentEngineCodex:      {Status: agentEngineDiagnosticReady, ReasonCode: "auth_ok"},
				agentEngineClaudeCode: {Status: agentEngineDiagnosticReady, ReasonCode: "auth_ok"},
				agentEnginePi:         {Status: agentEngineDiagnosticUnverified, ReasonCode: "probe_unsupported"},
			}
		}),
	)
	state.refresh(context.Background())
	capabilities, diagnostics := state.snapshot()
	for _, engine := range []string{agentEngineCodex, agentEngineClaudeCode, agentEnginePi} {
		if diagnostics[engine].Status != agentEngineDiagnosticUnverified || diagnostics[engine].ReasonCode != "disabled_by_configuration" {
			t.Fatalf("disabled engine %s leaked probe result: %+v", engine, diagnostics[engine])
		}
	}
	for _, capability := range []string{"codex", "claudeCode", "pi"} {
		if capabilityEnabled(capabilities, capability) {
			t.Fatalf("disabled capability %s was upgraded: %s", capability, capabilities)
		}
	}
}

func TestReconcileAgentEngineCapabilitiesPreservesUnrelatedCapabilities(t *testing.T) {
	base := json.RawMessage(`{"protocolSmoke":true,"browser":true,"codex":true,"claudeCode":true,"pi":false,"custom":"keep"}`)
	diagnostics := agentEngineDiagnostics{
		agentEngineCodex:      {Status: agentEngineDiagnosticNeedsSetup},
		agentEngineClaudeCode: {Status: agentEngineDiagnosticReady},
		agentEnginePi:         {Status: agentEngineDiagnosticUnverified},
	}
	reconciled := reconcileAgentEngineCapabilities(base, diagnostics)
	var values map[string]any
	if err := json.Unmarshal(reconciled, &values); err != nil {
		t.Fatalf("decode reconciled capabilities: %v", err)
	}
	for key, expected := range map[string]any{
		"protocolSmoke": true,
		"browser":       true,
		"codex":         false,
		"claudeCode":    true,
		"pi":            false,
		"custom":        "keep",
	} {
		if values[key] != expected {
			t.Fatalf("capability %s = %#v, want %#v in %s", key, values[key], expected, reconciled)
		}
	}

	diagnostics[agentEngineClaudeCode] = agentEngineDiagnostic{Status: agentEngineDiagnosticProbeError}
	diagnostics[agentEnginePi] = agentEngineDiagnostic{Status: agentEngineDiagnosticMissing}
	reconciled = reconcileAgentEngineCapabilities(base, diagnostics)
	if capabilityEnabled(reconciled, "claudeCode") || capabilityEnabled(reconciled, "pi") {
		t.Fatalf("known probe failures must disable capabilities: %s", reconciled)
	}

	readyDiagnostics := agentEngineDiagnostics{
		agentEngineCodex:      {Status: agentEngineDiagnosticReady},
		agentEngineClaudeCode: {Status: agentEngineDiagnosticReady},
		agentEnginePi:         {Status: agentEngineDiagnosticUnverified},
	}
	disabled := reconcileAgentEngineCapabilities(json.RawMessage(`{"codex":false,"claudeCode":false,"pi":false}`), readyDiagnostics)
	for _, capability := range []string{"codex", "claudeCode", "pi"} {
		if capabilityEnabled(disabled, capability) {
			t.Fatalf("diagnostics upgraded explicitly disabled %s capability: %s", capability, disabled)
		}
	}
}

func TestWorkerInputReportsSafeCachedDiagnosticsOnEveryHeartbeat(t *testing.T) {
	checkedAt := "2026-07-17T08:30:00Z"
	diagnostics := agentEngineDiagnostics{
		agentEngineCodex: {
			Status:     agentEngineDiagnosticReady,
			ReasonCode: "auth_ok",
			Version:    "codex 1.2.3",
			CheckedAt:  checkedAt,
		},
		agentEngineClaudeCode: {
			Status:     agentEngineDiagnosticNeedsSetup,
			ReasonCode: "auth_required",
			CheckedAt:  checkedAt,
		},
		agentEnginePi: {
			Status:     agentEngineDiagnosticUnverified,
			ReasonCode: "probe_unsupported",
			CheckedAt:  checkedAt,
		},
	}
	state := newAgentEngineDiagnosticState(
		json.RawMessage(`{"codex":true,"claudeCode":true,"pi":true,"browser":true}`),
		agentEngineDiagnoserFunc(func(context.Context) agentEngineDiagnostics { return diagnostics }),
	)
	state.refresh(context.Background())
	cfg := config{Name: "worker", Mode: "team", Version: workerVersion, engineDiagnostics: state}

	for _, includeCapabilities := range []bool{true, false} {
		input := cfg.workerInput("online", 1, includeCapabilities)
		if len(input.AgentEngineDiagnostics) != 3 {
			t.Fatalf("heartbeat omitted diagnostics: %+v", input)
		}
		if !capabilityEnabled(input.Capabilities, "codex") || capabilityEnabled(input.Capabilities, "claudeCode") || !capabilityEnabled(input.Capabilities, "pi") || !capabilityEnabled(input.Capabilities, "browser") {
			t.Fatalf("heartbeat capabilities do not match diagnostics: %s", input.Capabilities)
		}
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatalf("marshal input: %v", err)
		}
		if len(encoded) >= 16*1024 {
			t.Fatalf("worker input exceeds server diagnostics limit: %d", len(encoded))
		}
		assertDiagnosticFieldsSafe(t, encoded)
	}
}

func TestAgentEngineDiagnosticStateRefreshDoesNotBlockSnapshots(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	diagnoser := agentEngineDiagnoserFunc(func(context.Context) agentEngineDiagnostics {
		if calls.Add(1) > 1 {
			close(started)
			<-release
		}
		return agentEngineDiagnostics{agentEngineCodex: {Status: agentEngineDiagnosticReady}}
	})
	state := newAgentEngineDiagnosticState(json.RawMessage(`{"codex":true}`), diagnoser)
	state.refresh(context.Background())
	refreshDone := make(chan struct{})
	go func() {
		state.refresh(context.Background())
		close(refreshDone)
	}()
	<-started

	done := make(chan struct{})
	go func() {
		state.snapshot()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("cached diagnostics snapshot blocked on a running probe")
	}
	close(release)
	<-refreshDone
}

func TestAgentEngineDiagnosticStateRefreshesOnCadence(t *testing.T) {
	var calls atomic.Int32
	state := newAgentEngineDiagnosticState(
		json.RawMessage(`{"codex":true}`),
		agentEngineDiagnoserFunc(func(context.Context) agentEngineDiagnostics {
			calls.Add(1)
			return agentEngineDiagnostics{agentEngineCodex: {Status: agentEngineDiagnosticReady}}
		}),
	)
	if state.cadence != defaultEngineDiagnosticCadence {
		t.Fatalf("default cadence = %s, want %s", state.cadence, defaultEngineDiagnosticCadence)
	}
	state.cadence = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		state.run(ctx)
		close(done)
	}()
	deadline := time.After(250 * time.Millisecond)
	for calls.Load() == 0 {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("diagnostics did not refresh on cadence")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done
}

func writeFakeEngine(t *testing.T, dir, name, body string) {
	t.Helper()
	script := `#!/bin/sh
if [ -n "$MSPACE_RUNTIME_TOKEN" ] || [ -n "$MSPACE_RUNTIME_TOKEN_FILE" ] || [ -n "$MSPACE_CONTROL_PLANE_URL" ] || [ -n "$DATABASE_URL" ] || [ -n "$POSTGRES_PASSWORD" ] || [ -n "$GITHUB_TOKEN" ]; then
  echo 'control-plane secret leaked' >&2
  exit 88
fi
` + strings.TrimSpace(body) + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
}

func assertEngineDiagnostic(t *testing.T, diagnostic agentEngineDiagnostic, status, reasonCode, version string, checkedAt time.Time) {
	t.Helper()
	if diagnostic.Status != status || diagnostic.ReasonCode != reasonCode || diagnostic.Version != version || diagnostic.CheckedAt != checkedAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected diagnostic: %+v", diagnostic)
	}
}

func assertDiagnosticFieldsSafe(t *testing.T, encoded []byte) {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("decode worker input: %v", err)
	}
	var diagnostics map[string]map[string]json.RawMessage
	if err := json.Unmarshal(payload["agentEngineDiagnostics"], &diagnostics); err != nil {
		t.Fatalf("decode diagnostics: %v", err)
	}
	allowed := map[string]bool{"status": true, "reasonCode": true, "version": true, "checkedAt": true}
	for engine, diagnostic := range diagnostics {
		for field := range diagnostic {
			if !allowed[field] {
				t.Fatalf("diagnostic %s exposed unsafe field %q", engine, field)
			}
		}
	}
}
