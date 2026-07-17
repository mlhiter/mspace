package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	agentEngineCodex      = "codex"
	agentEngineClaudeCode = "claude_code"
	agentEnginePi         = "pi"
)

type AgentEngineCatalogItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Mention    string `json:"mention"`
	Capability string `json:"capability"`
}

func fixedAgentEngineCatalog() []AgentEngineCatalogItem {
	return []AgentEngineCatalogItem{
		{ID: agentEngineCodex, Name: "Codex", Mention: "@codex", Capability: "codex"},
		{ID: agentEngineClaudeCode, Name: "Claude Code", Mention: "@claude", Capability: "claudeCode"},
		{ID: agentEnginePi, Name: "Pi", Mention: "@pi", Capability: "pi"},
	}
}

func normalizeAgentEngineInput(agentEngine, legacyProvider, legacyAgentProfile string) (string, error) {
	if engine := strings.ToLower(strings.TrimSpace(agentEngine)); engine != "" {
		if !isSupportedAgentEngine(engine) {
			return "", errors.New("agentEngine must be codex, claude_code, or pi")
		}
		return engine, nil
	}

	provider := strings.ToLower(strings.TrimSpace(legacyProvider))
	if provider != "" && provider != agentEngineCodex {
		return "", errors.New("legacy provider must be codex")
	}
	profile := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(legacyAgentProfile)), "@")
	switch profile {
	case "", "triage", "codex", "bugfix", "design":
		return agentEngineCodex, nil
	default:
		return "", errors.New("legacy agentProfile is no longer supported")
	}
}

func agentEngineFromHistoricalPayload(agentEngine, legacyProvider, legacyAgentProfile string) (string, error) {
	if strings.TrimSpace(agentEngine) != "" {
		return normalizeAgentEngineInput(agentEngine, "", "")
	}
	provider := strings.ToLower(strings.TrimSpace(legacyProvider))
	if provider == "" || provider == agentEngineCodex {
		// Old custom profiles were always executed by Codex. Keep their sessions
		// readable without preserving the deleted prompt-profile behavior.
		return agentEngineCodex, nil
	}
	return "", errors.New("historical agent payload has an unsupported provider")
}

func isSupportedAgentEngine(agentEngine string) bool {
	switch agentEngine {
	case agentEngineCodex, agentEngineClaudeCode, agentEnginePi:
		return true
	default:
		return false
	}
}

func agentEngineCapability(agentEngine string) (string, error) {
	switch agentEngine {
	case agentEngineCodex:
		return "codex", nil
	case agentEngineClaudeCode:
		return "claudeCode", nil
	case agentEnginePi:
		return "pi", nil
	default:
		return "", errors.New("unsupported agentEngine")
	}
}

func requireCodexWorkflowAgentEngine(agentEngine, legacyProvider, legacyAgentProfile string) (string, error) {
	engine, err := normalizeAgentEngineInput(agentEngine, legacyProvider, legacyAgentProfile)
	if err != nil {
		return "", err
	}
	if engine != agentEngineCodex {
		return "", errors.New("this system workflow currently requires agentEngine codex")
	}
	return engine, nil
}

func normalizeAgentSessionRuntimeTask(requiredCapabilities, payload json.RawMessage) (json.RawMessage, json.RawMessage, error) {
	var payloadObject map[string]json.RawMessage
	if err := json.Unmarshal(payload, &payloadObject); err != nil {
		return nil, nil, fmt.Errorf("agent_session payload must be a JSON object: %w", err)
	}
	agentEngine, err := optionalJSONStringField(payloadObject, "agentEngine")
	if err != nil {
		return nil, nil, err
	}
	legacyProvider, err := optionalJSONStringField(payloadObject, "provider")
	if err != nil {
		return nil, nil, err
	}
	legacyProfile, err := optionalJSONStringField(payloadObject, "agentProfile")
	if err != nil {
		return nil, nil, err
	}
	engine, err := normalizeAgentEngineInput(agentEngine, legacyProvider, legacyProfile)
	if err != nil {
		return nil, nil, err
	}
	engineJSON, _ := json.Marshal(engine)
	payloadObject["agentEngine"] = engineJSON
	delete(payloadObject, "provider")
	delete(payloadObject, "agentProfile")
	payload, err = json.Marshal(payloadObject)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal agent_session payload: %w", err)
	}

	var capabilities map[string]json.RawMessage
	if err := json.Unmarshal(requiredCapabilities, &capabilities); err != nil {
		return nil, nil, fmt.Errorf("agent_session requiredCapabilities must be a JSON object: %w", err)
	}
	capability, err := agentEngineCapability(engine)
	if err != nil {
		return nil, nil, err
	}
	for key, raw := range capabilities {
		if !isAgentEngineCapabilityKey(key) {
			continue
		}
		var enabled bool
		if err := json.Unmarshal(raw, &enabled); err != nil {
			return nil, nil, fmt.Errorf("agent engine capability %s must be boolean", key)
		}
		if enabled && key != capability {
			return nil, nil, fmt.Errorf("requiredCapabilities conflicts with agentEngine %s", engine)
		}
	}
	capabilities[capability] = json.RawMessage("true")
	requiredCapabilities, err = json.Marshal(capabilities)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal agent_session requiredCapabilities: %w", err)
	}
	return requiredCapabilities, payload, nil
}

func optionalJSONStringField(object map[string]json.RawMessage, key string) (string, error) {
	raw, exists := object[key]
	if !exists || len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("agent_session payload %s must be a string", key)
	}
	return value, nil
}
