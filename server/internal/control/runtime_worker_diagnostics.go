package control

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
)

const maxAgentEngineDiagnosticsBytes = 16 * 1024

var supportedAgentEngineDiagnosticStatuses = map[string]struct{}{
	"ready":       {},
	"needs_setup": {},
	"unverified":  {},
	"missing":     {},
	"probe_error": {},
}

var supportedAgentEngineDiagnosticReasonCodes = map[string]struct{}{
	"auth_ok":                      {},
	"auth_required":                {},
	"disabled_by_configuration":    {},
	"executable_not_found":         {},
	"executable_resolution_failed": {},
	"model_available":              {},
	"model_unavailable":            {},
	"probe_launch_failed":          {},
	"probe_malformed":              {},
	"probe_timeout":                {},
	"probe_unsupported":            {},
}

var supportedAgentEngineDiagnosticKeys = map[string]struct{}{
	"codex":       {},
	"claude_code": {},
	"pi":          {},
}

var agentEngineCapabilityKeys = map[string]string{
	"codex":       "codex",
	"claude_code": "claudeCode",
	"pi":          "pi",
}

type agentEngineDiagnostic struct {
	Status     string `json:"status"`
	ReasonCode string `json:"reasonCode,omitempty"`
	Version    string `json:"version,omitempty"`
	CheckedAt  string `json:"checkedAt,omitempty"`
}

func normalizeAgentEngineDiagnostics(payload json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return json.RawMessage(`{}`), nil
	}
	if len(trimmed) > maxAgentEngineDiagnosticsBytes {
		return nil, errors.New("agentEngineDiagnostics must be 16384 bytes or less")
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, errors.New("agentEngineDiagnostics must be a JSON object")
	}
	sanitized := make(map[string]agentEngineDiagnostic, len(supportedAgentEngineDiagnosticKeys))
	for engine, value := range raw {
		if _, ok := supportedAgentEngineDiagnosticKeys[engine]; !ok {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(value, &fields); err != nil {
			continue
		}
		status, ok := diagnosticString(fields["status"], 32)
		if !ok {
			continue
		}
		status = strings.ToLower(status)
		if _, ok := supportedAgentEngineDiagnosticStatuses[status]; !ok {
			continue
		}
		diagnostic := agentEngineDiagnostic{Status: status}
		if reasonCode, ok := diagnosticReasonCode(fields["reasonCode"]); ok && diagnosticReasonMatchesEngineStatus(engine, status, reasonCode) {
			diagnostic.ReasonCode = reasonCode
		}
		diagnostic.Version, _ = diagnosticVersion(fields["version"])
		diagnostic.CheckedAt, _ = diagnosticCheckedAt(fields["checkedAt"])
		sanitized[engine] = diagnostic
	}

	encoded, err := json.Marshal(sanitized)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func diagnosticReasonMatchesEngineStatus(engine, status, reasonCode string) bool {
	switch reasonCode {
	case "model_available":
		return engine == agentEnginePi && status == "unverified"
	case "model_unavailable":
		return engine == agentEnginePi && status == "needs_setup"
	default:
		return true
	}
}

func diagnosticString(raw json.RawMessage, maxRunes int) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	runes := []rune(value)
	if len(runes) > maxRunes {
		value = string(runes[:maxRunes])
	}
	return value, true
}

func diagnosticReasonCode(raw json.RawMessage) (string, bool) {
	value, ok := diagnosticString(raw, 128)
	if !ok {
		return "", false
	}
	if _, ok := supportedAgentEngineDiagnosticReasonCodes[value]; !ok {
		return "", false
	}
	return value, true
}

func diagnosticCheckedAt(raw json.RawMessage) (string, bool) {
	value, ok := diagnosticString(raw, 64)
	if !ok {
		return "", false
	}
	checkedAt, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		checkedAt, err = time.Parse(time.RFC3339, value)
	}
	if err != nil {
		return "", false
	}
	return checkedAt.UTC().Format(time.RFC3339Nano), true
}

func diagnosticVersion(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	if strings.ContainsAny(value, "\r\n/\\:") {
		return "", false
	}
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return "", false
	}
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsNumber(character) || strings.ContainsRune(" ._-+(),@", character) {
			continue
		}
		return "", false
	}
	runes := []rune(value)
	if len(runes) > 128 {
		value = string(runes[:128])
	}
	return value, true
}

func normalizedRuntimeWorkerDiagnostics(payload json.RawMessage) json.RawMessage {
	normalized, err := normalizeAgentEngineDiagnostics(payload)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return normalized
}

func downgradeUnavailableAgentEngineCapabilities(capabilities, diagnostics json.RawMessage) json.RawMessage {
	var capabilityValues map[string]any
	if err := json.Unmarshal(capabilities, &capabilityValues); err != nil || capabilityValues == nil {
		return copyRawMessage(capabilities)
	}
	var diagnosticValues map[string]agentEngineDiagnostic
	if err := json.Unmarshal(diagnostics, &diagnosticValues); err != nil {
		return copyRawMessage(capabilities)
	}
	for engine, diagnostic := range diagnosticValues {
		switch diagnostic.Status {
		case "missing", "needs_setup", "probe_error":
			if capabilityKey := agentEngineCapabilityKeys[engine]; capabilityKey != "" {
				capabilityValues[capabilityKey] = false
			}
		}
	}
	encoded, err := json.Marshal(capabilityValues)
	if err != nil {
		return copyRawMessage(capabilities)
	}
	return json.RawMessage(encoded)
}
