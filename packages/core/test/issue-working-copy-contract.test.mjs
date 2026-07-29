import assert from "node:assert/strict";
import test from "node:test";

import { controlPlaneApi, queryKeys, setControlPlaneBaseUrl } from "../src/api.ts";
import { isIssueWorkingCopyAvailabilityBlocker } from "../src/runtime-availability.ts";

test("recognizes only Issue working-copy affinity blockers", () => {
  assert.equal(isIssueWorkingCopyAvailabilityBlocker("working_copy_busy"), true);
  assert.equal(isIssueWorkingCopyAvailabilityBlocker("working_copy_storage_unavailable"), true);
  assert.equal(isIssueWorkingCopyAvailabilityBlocker("working_copy_recovery_required"), true);
  assert.equal(isIssueWorkingCopyAvailabilityBlocker("no_worker"), false);
  assert.equal(isIssueWorkingCopyAvailabilityBlocker("worktree_missing"), false);
});

test("keys runtime availability by Issue working-copy affinity", () => {
  const base = queryKeys.runtimeAvailability("workspace-1", "token-1", {
    runtimeMode: "personal",
    requiredCapabilities: { claudeCode: true },
  });
  const issue = queryKeys.runtimeAvailability("workspace-1", "token-1", {
    runtimeMode: "personal",
    requiredCapabilities: { claudeCode: true },
    issueId: "issue-1",
  });

  assert.notDeepEqual(issue, base);
  assert.equal(issue.at(-1), "issue-1");
});

test("sends issueId with runtime availability preflight", async (t) => {
  const originalFetch = globalThis.fetch;
  setControlPlaneBaseUrl("https://mspace.invalid");
  t.after(() => {
    globalThis.fetch = originalFetch;
    setControlPlaneBaseUrl("");
  });

  let requestedUrl = "";
  globalThis.fetch = async (input) => {
    requestedUrl = String(input);
    return new Response(JSON.stringify({
      workspaceId: "workspace-1",
      runtimeMode: "personal",
      requiredCapabilities: { pi: true },
      state: "blocked",
      reasonCode: "working_copy_storage_unavailable",
      canQueue: false,
      canAutoStart: false,
      retryAfterMs: 5000,
      activeWorkerMaxAgeMs: 45000,
      claimableWorkerCount: 0,
    }), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  };

  const availability = await controlPlaneApi.getRuntimeAvailability("token-1", "workspace-1", {
    runtimeMode: "personal",
    requiredCapabilities: { pi: true },
    issueId: "issue/with spaces",
  });

  const url = new URL(requestedUrl);
  assert.equal(url.pathname, "/api/workspaces/workspace-1/runtime/availability");
  assert.equal(url.searchParams.get("issueId"), "issue/with spaces");
  assert.equal(url.searchParams.get("runtimeMode"), "personal");
  assert.deepEqual(JSON.parse(url.searchParams.get("requiredCapabilities")), { pi: true });
  assert.equal(availability.reasonCode, "working_copy_storage_unavailable");
});
