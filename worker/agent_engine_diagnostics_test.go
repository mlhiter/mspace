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

func TestCommandAgentEngineDiagnoserReportsReadinessAndSanitizesEnvironment(t *testing.T) {
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
	"--offline --no-extensions --list-models")
		case "${PWD##*/}" in mspace-pi-probe-*) ;; *) exit 98 ;; esac
		printf 'provider  model         context  max-out  thinking  images\nopenai    secret-model  128K     16K      yes       no\n'
		;;
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
	assertEngineDiagnostic(t, diagnostics[agentEnginePi], agentEngineDiagnosticUnverified, "model_available", "pi 7.8.9", checkedAt)

	encoded, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatalf("marshal diagnostics: %v", err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"account", "authMethod", "secret-model", "msw_secret", "/private/worker-token", "postgres://", "github-secret", dir} {
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
	writeFakeEngine(t, dir, "pi", `
case "$*" in
	"--version") printf '0.55.4\n' ;;
	"--offline --no-extensions --list-models") printf 'No models available. Set API keys in environment variables.\n' ;;
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
	if diagnostics[agentEnginePi].Status != agentEngineDiagnosticNeedsSetup || diagnostics[agentEnginePi].ReasonCode != "model_unavailable" {
		t.Fatalf("unexpected Pi diagnostic: %+v", diagnostics[agentEnginePi])
	}
}

func TestParsePiModelList(t *testing.T) {
	for _, test := range []struct {
		name       string
		output     string
		available  bool
		recognized bool
	}{
		{name: "no models", output: "No models available. Set API keys in environment variables.\n", recognized: true},
		{name: "configured model", output: "provider  model  context  max-out  thinking  images\nopenai  gpt-5  128K  16K  yes  no\n", available: true, recognized: true},
		{name: "configured model with CRLF", output: "provider  model  context  max-out  thinking  images\r\nanthropic  claude-sonnet  200K  64K  yes  yes\r\n", available: true, recognized: true},
		{name: "header only", output: "provider  model  context  max-out  thinking  images\n"},
		{name: "warning after header", output: "provider  model  context  max-out  thinking  images\nWarning: credentials unavailable\n"},
		{name: "invalid model flags", output: "provider  model  context  max-out  thinking  images\nopenai  gpt-5  128K  16K  maybe  no\n"},
		{name: "unknown output", output: "Pi configuration status is unavailable\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			available, recognized := parsePiModelList([]byte(test.output))
			if available != test.available || recognized != test.recognized {
				t.Fatalf("parsePiModelList() = (%v, %v), want (%v, %v)", available, recognized, test.available, test.recognized)
			}
		})
	}
}

func TestCommandAgentEngineDiagnoserPreservesPiProbeCompatibility(t *testing.T) {
	t.Run("Pi before safe offline probe support skips model probe", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeEngine(t, dir, "pi", `
case "$*" in
	"--version") printf '0.55.0\n' ;;
			*) exit 97 ;;
esac`)
		t.Setenv("PATH", dir)

		diagnoser := newCommandAgentEngineDiagnoser(json.RawMessage(`{"pi":true}`))
		diagnoser.commandTimeout = 5 * time.Second
		diagnostic := diagnoser.Diagnose(context.Background())[agentEnginePi]
		if diagnostic.Status != agentEngineDiagnosticUnverified || diagnostic.ReasonCode != "probe_unsupported" {
			t.Fatalf("unexpected old Pi diagnostic: %+v", diagnostic)
		}
	})

	t.Run("known supported Pi fails closed on unknown successful output", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeEngine(t, dir, "pi", `
case "$*" in
	"--version") printf '0.55.4\n' ;;
	"--offline --no-extensions --list-models") printf 'unknown future format\n' ;;
esac`)
		t.Setenv("PATH", dir)

		diagnoser := newCommandAgentEngineDiagnoser(json.RawMessage(`{"pi":true}`))
		diagnoser.commandTimeout = 5 * time.Second
		diagnostic := diagnoser.Diagnose(context.Background())[agentEnginePi]
		if diagnostic.Status != agentEngineDiagnosticProbeError || diagnostic.ReasonCode != "probe_malformed" {
			t.Fatalf("unexpected unknown Pi diagnostic: %+v", diagnostic)
		}
	})

	t.Run("unparseable version skips model probe", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeEngine(t, dir, "pi", `
case "$*" in
	"--version") printf 'Pi development build\n' ;;
	*) exit 97 ;;
esac`)
		t.Setenv("PATH", dir)

		diagnoser := newCommandAgentEngineDiagnoser(json.RawMessage(`{"pi":true}`))
		diagnoser.commandTimeout = 5 * time.Second
		diagnostic := diagnoser.Diagnose(context.Background())[agentEnginePi]
		if diagnostic.Status != agentEngineDiagnosticUnverified || diagnostic.ReasonCode != "probe_unsupported" {
			t.Fatalf("unexpected development Pi diagnostic: %+v", diagnostic)
		}
	})
}

func TestPiModelListProbeSupportedRequiresSafeOfflineVersion(t *testing.T) {
	for _, test := range []struct {
		version   string
		supported bool
	}{
		{version: "0.55.0"},
		{version: "0.55.1", supported: true},
		{version: "pi 0.55.4", supported: true},
		{version: "1.0.0", supported: true},
		{version: "Pi development build"},
	} {
		if actual := piModelListProbeSupported(test.version); actual != test.supported {
			t.Fatalf("piModelListProbeSupported(%q) = %v, want %v", test.version, actual, test.supported)
		}
	}
}

func TestCommandAgentEngineDiagnoserReportsProbeFailures(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeEngine(t, dir, "codex", `/bin/sleep 1 & wait`)
		writeFakeEngine(t, dir, "claude", `/bin/sleep 1 & wait`)
		writeFakeEngine(t, dir, "pi", `/bin/sleep 1 & wait`)
		t.Setenv("PATH", dir)

		diagnoser := newCommandAgentEngineDiagnoser(json.RawMessage(`{"codex":true,"claudeCode":true,"pi":true}`))
		diagnoser.commandTimeout = 20 * time.Millisecond
		diagnostics := diagnoser.Diagnose(context.Background())
		for _, engine := range []string{agentEngineCodex, agentEngineClaudeCode, agentEnginePi} {
			diagnostic := diagnostics[engine]
			if diagnostic.Status != agentEngineDiagnosticProbeError || diagnostic.ReasonCode != "probe_timeout" {
				t.Fatalf("unexpected %s timeout diagnostic: %+v", engine, diagnostic)
			}
		}
	})

	t.Run("Pi model probe exits nonzero", func(t *testing.T) {
		dir := t.TempDir()
		writeFakeEngine(t, dir, "pi", `
case "$*" in
	"--version") printf '0.55.4\n' ;;
	"--offline --no-extensions --list-models") exit 7 ;;
esac`)
		t.Setenv("PATH", dir)

		diagnoser := newCommandAgentEngineDiagnoser(json.RawMessage(`{"pi":true}`))
		diagnoser.commandTimeout = 5 * time.Second
		diagnostic := diagnoser.Diagnose(context.Background())[agentEnginePi]
		if diagnostic.Status != agentEngineDiagnosticProbeError || diagnostic.ReasonCode != "probe_malformed" {
			t.Fatalf("unexpected Pi probe diagnostic: %+v", diagnostic)
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
		"ANTHROPIC_OAUTH_TOKEN=anthropic-oauth",
		"AI_GATEWAY_API_KEY=gateway-secret",
		"OPENCODE_API_KEY=opencode-secret",
		"HF_TOKEN=huggingface-secret",
		"AZURE_OPENAI_BASE_URL=https://azure.invalid",
		"AZURE_OPENAI_RESOURCE_NAME=azure-resource",
		"AZURE_OPENAI_API_VERSION=2026-01-01",
		"AZURE_OPENAI_DEPLOYMENT_NAME_MAP=model=deployment",
		"CEREBRAS_API_KEY=cerebras-secret",
		"KIMI_API_KEY=kimi-secret",
		"MINIMAX_API_KEY=minimax-secret",
		"MINIMAX_CN_API_KEY=minimax-cn-secret",
		"ZAI_API_KEY=zai-secret",
		"COPILOT_GITHUB_TOKEN=copilot-secret",
		"GOOGLE_CLOUD_PROJECT=google-project",
		"GOOGLE_CLOUD_LOCATION=us-central1",
		"NODE_OPTIONS=--require=/tmp/injected.js",
		"AWS_BEARER_TOKEN_BEDROCK=bedrock-secret",
		"AWS_WEB_IDENTITY_TOKEN_FILE=/var/run/secrets/aws/token",
		"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI=/v2/credentials/worker",
		"AWS_CONTAINER_AUTHORIZATION_TOKEN=container-secret",
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
	assertProbeEnvironment(t, agentEnginePi, true,
		[]string{
			"PATH", "HOME", "TMPDIR", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "ANTHROPIC_OAUTH_TOKEN",
			"AI_GATEWAY_API_KEY", "OPENCODE_API_KEY", "HF_TOKEN", "AZURE_OPENAI_BASE_URL", "AZURE_OPENAI_RESOURCE_NAME",
			"AZURE_OPENAI_API_VERSION", "AZURE_OPENAI_DEPLOYMENT_NAME_MAP", "CEREBRAS_API_KEY", "KIMI_API_KEY",
			"MINIMAX_API_KEY", "MINIMAX_CN_API_KEY", "ZAI_API_KEY", "COPILOT_GITHUB_TOKEN", "GOOGLE_CLOUD_PROJECT",
			"GOOGLE_CLOUD_LOCATION", "AWS_BEARER_TOKEN_BEDROCK", "AWS_WEB_IDENTITY_TOKEN_FILE",
			"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "AWS_CONTAINER_AUTHORIZATION_TOKEN", "AWS_SECRET_ACCESS_KEY",
			"PI_OFFLINE", "PI_SKIP_VERSION_CHECK",
		},
		append(commonForbidden, "CODEX_HOME", "CLAUDE_CONFIG_DIR", "CLAUDE_CODE_OAUTH_TOKEN"),
	)
	assertProbeEnvironment(t, agentEnginePi, false,
		[]string{"PATH", "HOME", "TMPDIR"},
		append(commonForbidden, "CODEX_HOME", "OPENAI_API_KEY", "CLAUDE_CONFIG_DIR", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "AWS_SECRET_ACCESS_KEY", "PI_OFFLINE", "PI_SKIP_VERSION_CHECK"),
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
	base := json.RawMessage(`{"protocolSmoke":true,"browser":true,"codex":true,"claudeCode":true,"pi":true,"custom":"keep"}`)
	diagnostics := agentEngineDiagnostics{
		agentEngineCodex:      {Status: agentEngineDiagnosticNeedsSetup},
		agentEngineClaudeCode: {Status: agentEngineDiagnosticReady},
		agentEnginePi:         {Status: agentEngineDiagnosticNeedsSetup, ReasonCode: "model_unavailable"},
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

	configuredPi := reconcileAgentEngineCapabilities(
		json.RawMessage(`{"pi":true}`),
		agentEngineDiagnostics{agentEnginePi: {Status: agentEngineDiagnosticUnverified, ReasonCode: "model_available"}},
	)
	if !capabilityEnabled(configuredPi, "pi") {
		t.Fatalf("configured but unverified Pi must preserve its allowed capability: %s", configuredPi)
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
	cfg := config{Name: "worker", Mode: "team", engineDiagnostics: state}

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
