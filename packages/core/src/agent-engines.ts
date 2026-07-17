import type { AgentEngine, AgentEngineCapability, AgentEngineCatalogItem, AgentSession } from "./types";

export const agentEngineCapabilities: Record<AgentEngine, AgentEngineCapability> = {
  codex: "codex",
  claude_code: "claudeCode",
  pi: "pi",
};

const legacyCodexProfiles = new Set(["triage", "bugfix", "design"]);

function engineToken(value: unknown) {
  return typeof value === "string"
    ? value.trim().toLowerCase().replace(/[^a-z0-9]+/g, "_").replace(/^_+|_+$/g, "")
    : "";
}

export function parseAgentEngine(value: unknown): AgentEngine | undefined {
  const token = engineToken(value);
  if (token === "codex" || legacyCodexProfiles.has(token)) return "codex";
  if (token === "claude" || token === "claude_code" || token === "claudecode") return "claude_code";
  if (token === "pi" || token === "pie") return "pi";
  return undefined;
}

export function agentEngineForSession(session: Pick<AgentSession, "agentEngine" | "provider" | "agentProfile">): AgentEngine {
  return parseAgentEngine(session.agentEngine)
    || parseAgentEngine(session.provider)
    || parseAgentEngine(session.agentProfile)
    || "codex";
}

export function agentCapabilityForEngine(value: unknown): AgentEngineCapability | undefined {
  const engine = parseAgentEngine(value);
  return engine ? agentEngineCapabilities[engine] : undefined;
}

export function isFixedAgentEngineCatalogItem(agent: AgentEngineCatalogItem): boolean {
  const engine = parseAgentEngine(agent.id);
  return Boolean(
    engine
    && engine === agent.id
    && agent.capability === agentEngineCapabilities[engine]
    && agent.mention === agentEngineMention(engine),
  );
}

export function agentRequiredCapabilities(agent: Pick<AgentEngineCatalogItem, "id">): Partial<Record<AgentEngineCapability, boolean>> {
  return { [agentEngineCapabilities[agent.id]]: true };
}

export function agentEngineMention(value: unknown): "@codex" | "@claude" | "@pi" | "" {
  const engine = parseAgentEngine(value);
  if (!engine) return "";
  if (engine === "claude_code") return "@claude";
  return engine === "pi" ? "@pi" : "@codex";
}

export function agentEngineDisplayName(value: unknown): "Codex" | "Claude Code" | "Pi" {
  const engine = parseAgentEngine(value) || "codex";
  if (engine === "claude_code") return "Claude Code";
  return engine === "pi" ? "Pi" : "Codex";
}

export function engineSessionRef(session: Pick<AgentSession, "engineSessionRef" | "codexThreadId">) {
  return session.engineSessionRef || session.codexThreadId || "";
}

export function engineRunRef(session: Pick<AgentSession, "engineRunRef" | "codexTurnId">) {
  return session.engineRunRef || session.codexTurnId || "";
}
