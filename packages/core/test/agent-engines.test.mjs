import assert from "node:assert/strict";
import test from "node:test";

import {
  agentCapabilityForEngine,
  agentEngineForSession,
  agentEngineMention,
  agentRequiredCapabilities,
  engineRunRef,
  engineSessionRef,
  isFixedAgentEngineCatalogItem,
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
