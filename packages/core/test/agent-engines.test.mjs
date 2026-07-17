import assert from "node:assert/strict";
import test from "node:test";

import {
  agentCapabilityForEngine,
  agentEngineForLinkedSession,
  agentEngineForSession,
  agentEngineMention,
  agentRequiredCapabilities,
  engineRunRef,
  engineSessionRef,
  isFixedAgentEngineCatalogItem,
  isNoActiveAgentWorkerError,
  parseAgentEngine,
} from "../src/agent-engines.ts";

test("normalizes fixed engines and known legacy profiles", () => {
  assert.equal(parseAgentEngine("codex"), "codex");
  assert.equal(parseAgentEngine("claude-code"), "claude_code");
  assert.equal(parseAgentEngine("Pi"), "pi");
  assert.equal(parseAgentEngine("bugfix"), "codex");
  assert.equal(parseAgentEngine("unknown"), undefined);
});

test("uses exact runtime capability keys", () => {
  assert.equal(agentCapabilityForEngine("claude_code"), "claudeCode");
  assert.deepEqual(agentRequiredCapabilities({ id: "pi" }), { pi: true });
  assert.equal(agentCapabilityForEngine("unknown"), undefined);
  assert.equal(isFixedAgentEngineCatalogItem({ id: "claude_code", name: "Claude Code", mention: "@claude", capability: "claudeCode" }), true);
  assert.equal(isFixedAgentEngineCatalogItem({ id: "bugfix", name: "Bugfix", mention: "@bugfix", capability: "codex" }), false);
});

test("reads engine identity and generic refs with legacy fallbacks", () => {
  assert.equal(agentEngineForSession({ provider: "codex", agentProfile: "design" }), "codex");
  assert.equal(agentEngineForSession({ agentEngine: "claude_code", provider: "codex" }), "claude_code");
  assert.equal(agentEngineMention("claude_code"), "@claude");
  assert.equal(agentEngineMention("unknown"), "");
  assert.equal(engineSessionRef({ codexThreadId: "thread-old" }), "thread-old");
  assert.equal(engineSessionRef({ engineSessionRef: "session-new", codexThreadId: "thread-old" }), "session-new");
  assert.equal(engineRunRef({ codexTurnId: "turn-old" }), "turn-old");
});

test("continues failures with the linked Session engine before historical fallback", () => {
  const sessions = [
    { id: "session-claude", agentEngine: "claude_code" },
    { id: "session-pi", agentEngine: "pi" },
  ];

  assert.equal(agentEngineForLinkedSession("session-claude", sessions), "claude_code");
  assert.equal(agentEngineForLinkedSession("session-pi", sessions), "pi");
  assert.equal(agentEngineMention(agentEngineForLinkedSession("session-claude", sessions)), "@claude");
  assert.equal(agentEngineMention(agentEngineForLinkedSession("session-pi", sessions)), "@pi");
  assert.equal(agentEngineForLinkedSession("historical-missing-session", sessions), "codex");
});

test("recognizes server worker races without matching unrelated errors", () => {
  assert.equal(isNoActiveAgentWorkerError(new Error("no active agent worker matches the request")), true);
  assert.equal(isNoActiveAgentWorkerError(new Error("No active Claude Code worker")), true);
  assert.equal(isNoActiveAgentWorkerError("control plane: no active pi worker"), true);
  assert.equal(isNoActiveAgentWorkerError(new Error("agent session validation failed")), false);
});
