package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	agentEngineDiagnosticReady       = "ready"
	agentEngineDiagnosticNeedsSetup  = "needs_setup"
	agentEngineDiagnosticUnverified  = "unverified"
	agentEngineDiagnosticMissing     = "missing"
	agentEngineDiagnosticProbeError  = "probe_error"
	defaultEngineDiagnosticCadence   = 60 * time.Second
	defaultEngineDiagnosticTimeout   = 2 * time.Second
	maxEngineDiagnosticOutputBytes   = 64 * 1024
	maxEngineDiagnosticVersionLength = 128
)

type agentEngineDiagnostic struct {
	Status     string `json:"status"`
	ReasonCode string `json:"reasonCode"`
	Version    string `json:"version,omitempty"`
	CheckedAt  string `json:"checkedAt"`
}

type agentEngineDiagnostics map[string]agentEngineDiagnostic

type agentEngineDiagnoser interface {
	Diagnose(context.Context) agentEngineDiagnostics
}

type agentEngineDiagnoserFunc func(context.Context) agentEngineDiagnostics

func (fn agentEngineDiagnoserFunc) Diagnose(ctx context.Context) agentEngineDiagnostics {
	return fn(ctx)
}

type commandAgentEngineDiagnoser struct {
	commandTimeout          time.Duration
	now                     func() time.Time
	allowed                 map[string]bool
	executables             map[string]resolvedAgentEngineExecutable
	authProbeEnvironment    map[string][]string
	versionProbeEnvironment map[string][]string
}

type resolvedAgentEngineExecutable struct {
	path       string
	status     string
	reasonCode string
}

type agentEngineDiagnosticState struct {
	mu               sync.RWMutex
	refreshMu        sync.Mutex
	baseCapabilities json.RawMessage
	capabilities     json.RawMessage
	diagnostics      agentEngineDiagnostics
	diagnoser        agentEngineDiagnoser
	cadence          time.Duration
}

type engineDiagnosticCommandResult struct {
	stdout   []byte
	stderr   []byte
	err      error
	timedOut bool
}

type limitedDiagnosticBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedDiagnosticBuffer) Write(value []byte) (int, error) {
	written := len(value)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = b.buffer.Write(value)
	}
	return written, nil
}

func newAgentEngineDiagnosticState(baseCapabilities json.RawMessage, diagnoser agentEngineDiagnoser) *agentEngineDiagnosticState {
	if diagnoser == nil {
		diagnoser = newCommandAgentEngineDiagnoser(baseCapabilities)
	}
	base := append(json.RawMessage(nil), baseCapabilities...)
	return &agentEngineDiagnosticState{
		baseCapabilities: base,
		capabilities:     append(json.RawMessage(nil), base...),
		diagnostics:      agentEngineDiagnostics{},
		diagnoser:        diagnoser,
		cadence:          defaultEngineDiagnosticCadence,
	}
}

func (s *agentEngineDiagnosticState) refresh(ctx context.Context) {
	if s == nil || s.diagnoser == nil {
		return
	}
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	diagnostics := applyAgentEngineAllowlist(s.baseCapabilities, s.diagnoser.Diagnose(ctx), time.Now)
	capabilities := reconcileAgentEngineCapabilities(s.baseCapabilities, diagnostics)
	s.mu.Lock()
	s.diagnostics = cloneAgentEngineDiagnostics(diagnostics)
	s.capabilities = capabilities
	s.mu.Unlock()
}

func (s *agentEngineDiagnosticState) run(ctx context.Context) {
	if s == nil || s.cadence <= 0 {
		return
	}
	ticker := time.NewTicker(s.cadence)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refresh(ctx)
		}
	}
}

func (s *agentEngineDiagnosticState) snapshot() (json.RawMessage, agentEngineDiagnostics) {
	if s == nil {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append(json.RawMessage(nil), s.capabilities...), cloneAgentEngineDiagnostics(s.diagnostics)
}

func cloneAgentEngineDiagnostics(source agentEngineDiagnostics) agentEngineDiagnostics {
	if len(source) == 0 {
		return nil
	}
	result := make(agentEngineDiagnostics, len(source))
	for engine, diagnostic := range source {
		result[engine] = diagnostic
	}
	return result
}

func reconcileAgentEngineCapabilities(base json.RawMessage, diagnostics agentEngineDiagnostics) json.RawMessage {
	values := map[string]any{}
	if err := json.Unmarshal(base, &values); err != nil {
		values = map[string]any{}
	}
	for engine, capability := range map[string]string{
		agentEngineCodex:      "codex",
		agentEngineClaudeCode: "claudeCode",
		agentEnginePi:         "pi",
	} {
		diagnostic, ok := diagnostics[engine]
		if !ok || !capabilityEnabled(base, capability) {
			continue
		}
		switch diagnostic.Status {
		case agentEngineDiagnosticNeedsSetup, agentEngineDiagnosticMissing, agentEngineDiagnosticProbeError:
			values[capability] = false
		}
	}
	result, err := json.Marshal(values)
	if err != nil {
		return append(json.RawMessage(nil), base...)
	}
	return result
}

func applyAgentEngineAllowlist(base json.RawMessage, diagnostics agentEngineDiagnostics, now func() time.Time) agentEngineDiagnostics {
	result := cloneAgentEngineDiagnostics(diagnostics)
	if result == nil {
		result = agentEngineDiagnostics{}
	}
	for engine, capability := range agentEngineCapabilityKeys() {
		if capabilityEnabled(base, capability) {
			continue
		}
		checkedAt := ""
		if existing, ok := result[engine]; ok {
			if existing.Status == agentEngineDiagnosticMissing ||
				(existing.Status == agentEngineDiagnosticProbeError && existing.ReasonCode == "executable_resolution_failed") {
				continue
			}
			checkedAt = existing.CheckedAt
		}
		if checkedAt == "" {
			checkedAt = now().UTC().Format(time.RFC3339Nano)
		}
		result[engine] = agentEngineDiagnostic{
			Status:     agentEngineDiagnosticUnverified,
			ReasonCode: "disabled_by_configuration",
			CheckedAt:  checkedAt,
		}
	}
	return result
}

func agentEngineCapabilityKeys() map[string]string {
	return map[string]string{
		agentEngineCodex:      "codex",
		agentEngineClaudeCode: "claudeCode",
		agentEnginePi:         "pi",
	}
}

func newCommandAgentEngineDiagnoser(capabilities json.RawMessage) commandAgentEngineDiagnoser {
	diagnoser := commandAgentEngineDiagnoser{
		allowed:                 map[string]bool{},
		executables:             map[string]resolvedAgentEngineExecutable{},
		authProbeEnvironment:    map[string][]string{},
		versionProbeEnvironment: map[string][]string{},
	}
	parentEnvironment := os.Environ()
	for engine, capability := range agentEngineCapabilityKeys() {
		allowed := capabilityEnabled(capabilities, capability)
		diagnoser.allowed[engine] = allowed
		if allowed {
			diagnoser.authProbeEnvironment[engine] = buildAgentEngineProbeEnv(parentEnvironment, engine, true)
			diagnoser.versionProbeEnvironment[engine] = buildAgentEngineProbeEnv(parentEnvironment, engine, false)
		}
		path, err := exec.LookPath(agentEngineBinaryName(engine))
		if err != nil {
			diagnoser.executables[engine] = resolvedAgentEngineExecutable{
				status:     agentEngineDiagnosticMissing,
				reasonCode: "executable_not_found",
			}
			continue
		}
		resolvedPath, err := resolveAgentEngineExecutablePath(path)
		if err != nil {
			diagnoser.executables[engine] = resolvedAgentEngineExecutable{
				status:     agentEngineDiagnosticProbeError,
				reasonCode: "executable_resolution_failed",
			}
			continue
		}
		diagnoser.executables[engine] = resolvedAgentEngineExecutable{path: resolvedPath}
	}
	return diagnoser
}

func resolveAgentEngineExecutablePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func (d commandAgentEngineDiagnoser) Diagnose(ctx context.Context) agentEngineDiagnostics {
	if d.commandTimeout <= 0 {
		d.commandTimeout = defaultEngineDiagnosticTimeout
	}
	if d.now == nil {
		d.now = time.Now
	}

	type engineResult struct {
		engine     string
		diagnostic agentEngineDiagnostic
	}
	results := make(chan engineResult, 3)
	probes := []struct {
		engine string
		probe  func(context.Context, string) agentEngineDiagnostic
	}{
		{engine: agentEngineCodex, probe: d.diagnoseCodex},
		{engine: agentEngineClaudeCode, probe: d.diagnoseClaudeCode},
		{engine: agentEnginePi, probe: d.diagnosePi},
	}
	for _, probe := range probes {
		probe := probe
		go func() {
			executable := d.executables[probe.engine]
			if executable.path == "" {
				results <- engineResult{engine: probe.engine, diagnostic: d.diagnostic(executable.status, executable.reasonCode, "")}
				return
			}
			if !d.allowed[probe.engine] {
				results <- engineResult{engine: probe.engine, diagnostic: d.diagnostic(agentEngineDiagnosticUnverified, "disabled_by_configuration", "")}
				return
			}
			results <- engineResult{engine: probe.engine, diagnostic: probe.probe(ctx, executable.path)}
		}()
	}

	diagnostics := make(agentEngineDiagnostics, len(probes))
	for range probes {
		result := <-results
		diagnostics[result.engine] = result.diagnostic
	}
	return diagnostics
}

func agentEngineBinaryName(engine string) string {
	switch engine {
	case agentEngineClaudeCode:
		return "claude"
	default:
		return engine
	}
}

func (d commandAgentEngineDiagnoser) diagnoseCodex(ctx context.Context, path string) agentEngineDiagnostic {
	version := d.probeVersion(ctx, agentEngineCodex, path)
	result := d.runCommand(ctx, agentEngineCodex, true, path, "login", "status")
	switch {
	case result.timedOut:
		return d.diagnostic(agentEngineDiagnosticProbeError, "probe_timeout", version)
	case result.err == nil:
		return d.diagnostic(agentEngineDiagnosticReady, "auth_ok", version)
	default:
		var exitError *exec.ExitError
		if errors.As(result.err, &exitError) {
			return d.diagnostic(agentEngineDiagnosticNeedsSetup, "auth_required", version)
		}
		return d.diagnostic(agentEngineDiagnosticProbeError, "probe_launch_failed", version)
	}
}

func (d commandAgentEngineDiagnoser) diagnoseClaudeCode(ctx context.Context, path string) agentEngineDiagnostic {
	version := d.probeVersion(ctx, agentEngineClaudeCode, path)
	result := d.runCommand(ctx, agentEngineClaudeCode, true, path, "auth", "status", "--json")
	if result.timedOut {
		return d.diagnostic(agentEngineDiagnosticProbeError, "probe_timeout", version)
	}
	if result.err != nil {
		var exitError *exec.ExitError
		if !errors.As(result.err, &exitError) {
			return d.diagnostic(agentEngineDiagnosticProbeError, "probe_launch_failed", version)
		}
	}
	loggedIn, ok := parseClaudeLoggedIn(result.stdout)
	if !ok {
		return d.diagnostic(agentEngineDiagnosticProbeError, "probe_malformed", version)
	}
	if loggedIn {
		return d.diagnostic(agentEngineDiagnosticReady, "auth_ok", version)
	}
	return d.diagnostic(agentEngineDiagnosticNeedsSetup, "auth_required", version)
}

func (d commandAgentEngineDiagnoser) diagnosePi(ctx context.Context, path string) agentEngineDiagnostic {
	return d.diagnostic(agentEngineDiagnosticUnverified, "probe_unsupported", d.probeVersion(ctx, agentEnginePi, path))
}

func (d commandAgentEngineDiagnoser) probeVersion(ctx context.Context, engine, path string) string {
	result := d.runCommand(ctx, engine, false, path, "--version")
	if result.err != nil || result.timedOut {
		return ""
	}
	value := strings.TrimSpace(firstNonEmpty(string(result.stdout), string(result.stderr)))
	if index := strings.IndexAny(value, "\r\n"); index >= 0 {
		value = value[:index]
	}
	value = strings.Map(func(character rune) rune {
		if unicode.IsPrint(character) {
			return character
		}
		return -1
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	return truncate(value, maxEngineDiagnosticVersionLength)
}

func (d commandAgentEngineDiagnoser) runCommand(ctx context.Context, engine string, includeAuth bool, path string, args ...string) engineDiagnosticCommandResult {
	timeout := d.commandTimeout
	if timeout <= 0 {
		timeout = defaultEngineDiagnosticTimeout
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd, err := newAgentEngineCommandContext(commandCtx, path, args...)
	if err != nil {
		return engineDiagnosticCommandResult{err: err}
	}
	environment := d.versionProbeEnvironment[engine]
	if includeAuth {
		environment = d.authProbeEnvironment[engine]
	}
	cmd.Env = append([]string(nil), environment...)
	configureAgentEngineProcess(cmd)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		killAgentEngineProcess(cmd)
		return nil
	}
	cmd.WaitDelay = 250 * time.Millisecond
	stdout := &limitedDiagnosticBuffer{limit: maxEngineDiagnosticOutputBytes}
	stderr := &limitedDiagnosticBuffer{limit: maxEngineDiagnosticOutputBytes}
	process, stdoutReader, stderrReader, err := startAgentEngineProcess(cmd, nil)
	if err != nil {
		return engineDiagnosticCommandResult{err: err}
	}
	stdoutDone := drainEngineDiagnosticOutput(stdoutReader, stdout)
	stderrDone := drainEngineDiagnosticOutput(stderrReader, stderr)
	select {
	case <-process.done:
	case <-commandCtx.Done():
		process.stop(0)
	}
	err = process.waitError()
	<-stdoutDone
	<-stderrDone
	return engineDiagnosticCommandResult{
		stdout:   append([]byte(nil), stdout.buffer.Bytes()...),
		stderr:   append([]byte(nil), stderr.buffer.Bytes()...),
		err:      err,
		timedOut: commandCtx.Err() == context.DeadlineExceeded,
	}
}

func drainEngineDiagnosticOutput(reader io.Reader, destination io.Writer) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(destination, reader)
		close(done)
	}()
	return done
}

func buildAgentEngineProbeEnv(parent []string, engine string, includeAuth bool) []string {
	allowed := map[string]bool{
		"APPDATA":         true,
		"COMSPEC":         true,
		"HOME":            true,
		"HOMEDRIVE":       true,
		"HOMEPATH":        true,
		"LANG":            true,
		"LC_ALL":          true,
		"LOCALAPPDATA":    true,
		"PATH":            true,
		"PATHEXT":         true,
		"SHELL":           true,
		"SYSTEMROOT":      true,
		"TEMP":            true,
		"TMP":             true,
		"TMPDIR":          true,
		"USERPROFILE":     true,
		"WINDIR":          true,
		"XDG_CACHE_HOME":  true,
		"XDG_CONFIG_HOME": true,
		"XDG_DATA_HOME":   true,
	}
	filtered := buildAgentEngineEnvForEngine(engine, parent, nil)
	result := make([]string, 0, len(filtered))
	for _, entry := range filtered {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(strings.TrimSpace(key))
		if allowed[key] || (includeAuth && isAgentEngineAuthEnvironmentKey(engine, key)) {
			result = append(result, entry)
		}
	}
	return result
}

func (d commandAgentEngineDiagnoser) diagnostic(status, reasonCode, version string) agentEngineDiagnostic {
	return agentEngineDiagnostic{
		Status:     status,
		ReasonCode: reasonCode,
		Version:    strings.TrimSpace(version),
		CheckedAt:  d.now().UTC().Format(time.RFC3339Nano),
	}
}

func parseClaudeLoggedIn(raw []byte) (bool, bool) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return false, false
	}
	for key, value := range values {
		normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "_", ""))
		if normalized != "loggedin" {
			continue
		}
		var loggedIn bool
		if err := json.Unmarshal(value, &loggedIn); err != nil {
			return false, false
		}
		return loggedIn, true
	}
	return false, false
}
