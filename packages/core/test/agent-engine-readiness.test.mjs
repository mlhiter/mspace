import assert from "node:assert/strict";
import test from "node:test";

import {
  agentEngineDiagnosticDisplayState,
  isCurrentHostPrimaryWorker,
  isCurrentHostWorker,
  resolveAgentEngineDiagnostic,
  runtimeWorkerLiveness,
} from "../src/agent-engine-readiness.ts";

function worker(overrides = {}) {
  return {
    mode: "personal",
    status: "online",
    lastSeenAt: "2026-07-17T04:00:00.000Z",
    capabilities: {},
    labels: {},
    ...overrides,
  };
}

test("prefers explicit engine diagnostics over capability flags", () => {
  const resolved = resolveAgentEngineDiagnostic(worker({
    capabilities: { claudeCode: true },
    agentEngineDiagnostics: {
      claude_code: {
        status: "needs_setup",
        reasonCode: "auth_required",
        version: "1.2.3",
        checkedAt: "2026-07-17T03:59:00.000Z",
      },
    },
  }), "claude_code");

  assert.deepEqual(resolved, {
    status: "needs_setup",
    reasonCode: "auth_required",
    version: "1.2.3",
    checkedAt: "2026-07-17T03:59:00.000Z",
    reported: true,
    legacyCapability: false,
  });
});

test("marks old capability-only workers as unverified legacy reports", () => {
  assert.deepEqual(resolveAgentEngineDiagnostic(worker({ capabilities: { pi: true } }), "pi"), {
    status: "not_reported",
    reasonCode: "legacy_capability",
    version: "",
    checkedAt: "",
    reported: false,
    legacyCapability: true,
  });
  assert.equal(resolveAgentEngineDiagnostic(worker(), "codex").reasonCode, "not_reported");
});

test("keeps worker liveness separate from engine diagnostics", () => {
  const now = Date.parse("2026-07-17T04:01:00.000Z");
  assert.equal(runtimeWorkerLiveness(worker(), 45_000, now), "stale");
  assert.equal(runtimeWorkerLiveness(worker({ status: "draining" }), 45_000, now), "draining");
  assert.equal(runtimeWorkerLiveness(worker({ status: "offline" }), 45_000, now), "offline");
  assert.equal(runtimeWorkerLiveness(worker({ lastSeenAt: "2026-07-17T04:00:30.000Z" }), 45_000, now), "online");
});

test("presents a configured-off installed engine as disabled without changing its protocol status", () => {
  const disabled = resolveAgentEngineDiagnostic(worker({
    agentEngineDiagnostics: {
      pi: { status: "unverified", reasonCode: "disabled_by_configuration" },
    },
  }), "pi");
  const probeUnavailable = resolveAgentEngineDiagnostic(worker({
    agentEngineDiagnostics: {
      pi: { status: "unverified", reasonCode: "auth_probe_unavailable" },
    },
  }), "pi");

  assert.equal(disabled.status, "unverified");
  assert.equal(agentEngineDiagnosticDisplayState(disabled), "disabled");
  assert.equal(agentEngineDiagnosticDisplayState(probeUnavailable), "unverified");
});

test("requires trusted personal-mode host identity before labeling This Mac", () => {
  const currentHostId = "msh_current";
  const primary = worker({ labels: { provider: "desktop-local", hostId: currentHostId, runtimeRole: "primary" } });
  const companion = worker({ labels: { provider: "desktop-local", hostId: currentHostId, runtimeRole: "browser_companion" } });
  const remote = worker({ labels: { provider: "desktop-local", hostId: "msh_remote", runtimeRole: "primary" } });
  const copiedHostId = worker({ labels: { provider: "self-host", hostId: currentHostId, runtimeRole: "primary" } });

  assert.equal(isCurrentHostWorker(primary, "personal", currentHostId), true);
  assert.equal(isCurrentHostPrimaryWorker(primary, "personal", currentHostId), true);
  assert.equal(isCurrentHostPrimaryWorker(companion, "personal", currentHostId), false);
  assert.equal(isCurrentHostWorker(remote, "personal", currentHostId), false);
  assert.equal(isCurrentHostWorker(primary, "team", currentHostId), false);
  assert.equal(isCurrentHostWorker(primary, "personal", ""), false);
  assert.equal(isCurrentHostWorker(copiedHostId, "personal", currentHostId), false);
  assert.equal(isCurrentHostWorker(worker({ labels: { provider: "desktop-local", runtimeRole: "primary" } }), "personal", currentHostId), false);
});
