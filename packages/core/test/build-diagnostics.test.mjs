import assert from "node:assert/strict";
import test from "node:test";

import { formatBuildDiagnostics } from "../src/build-diagnostics.ts";

test("formats only normalized build identity and fixed capabilities", () => {
  const output = formatBuildDiagnostics({
    desktopVersion: " 0.2.0-rc.1 ",
    serverHealth: {
      version: "v0.2.0",
      commitSha: "ABCDEF0123456789ABCDEF0123456789ABCDEF01",
      buildTime: "2026-07-27T09:30:00+08:00",
      serverProtocol: 2,
      capabilities: { githubAuth: true, runtimeTaskQueue: false, arbitrary: true },
    },
    workers: [{ version: "dev" }, { version: "0.2.0" }, { version: "dev" }],
  });

  assert.match(output, /desktop\.version=0\.2\.0-rc\.1/);
  assert.match(output, /server\.commitSha=abcdef0123456789abcdef0123456789abcdef01/);
  assert.match(output, /server\.buildTime=2026-07-27T01:30:00\.000Z/);
  assert.match(output, /server\.protocol=2/);
  assert.match(output, /server\.capability\.githubAuth=true/);
  assert.match(output, /worker\.versions=0\.2\.0,dev/);
  assert.doesNotMatch(output, /arbitrary/);
});

test("drops poisoned diagnostics fields and bounds oversized values", () => {
  const poisons = [
    "/Users/alice/.config/mspace/token",
    "C:\\Users\\alice\\AppData\\mspace.db",
    "https://user:password@example.com/api",
    "msp_secret-session",
    "msw_secret-worker",
    "MSPACE_TOKEN=secret",
    "safe\ninjected=true",
    "v".repeat(10_000),
  ];
  const output = formatBuildDiagnostics({
    desktopVersion: poisons[0],
    serverHealth: {
      version: poisons[1],
      commitSha: poisons[2],
      buildTime: poisons[3],
      serverProtocol: "2",
      capabilities: { githubAuth: true, secretCapability: true },
      url: poisons[2],
      error: poisons[5],
      token: poisons[3],
      raw: { environment: poisons[5] },
    },
    workers: poisons.map((version, index) => ({
      version,
      id: `worker-${index}`,
      name: `worker-name-${index}`,
      labels: { path: poisons[0], token: poisons[4] },
    })),
  });

  for (const poison of poisons) assert.equal(output.includes(poison), false);
  assert.doesNotMatch(output, /secretCapability|worker-name|worker-|labels|url=|error=|token=|environment|raw/);
  assert.equal(output.length < 2_000, true);
  assert.match(output, /desktop\.version=unknown/);
  assert.match(output, /server\.version=unknown/);
  assert.match(output, /worker\.versions=unknown/);
});
