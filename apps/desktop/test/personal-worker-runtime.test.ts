import assert from "node:assert/strict";
import { chmod, mkdtemp, rm, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import {
  buildAgentExecutableSearchPath,
  discoverAgentEngineCapabilities,
  missingAgentEngineExecutables,
  missingPersonalWorkerCapabilities,
  personalWorkerName,
  personalWorkerRequiresBrowser,
  personalWorkerRequiresCodexAuth,
  personalWorkerWorkRoot,
} from "../src/main/personal-worker-runtime.ts";

function discoverInstalled(fileNames: string[], platform: NodeJS.Platform = "darwin") {
  return discoverAgentEngineCapabilities({
    env: platform === "win32" ? { PATH: "/tools", PATHEXT: ".EXE;.CMD" } : { PATH: "/tools" },
    homeDir: "/home/tester",
    platform,
    isExecutable: (path) => fileNames.some((fileName) => path === `/tools/${fileName}`),
  });
}

test("discovers only installed Agent engine executables without launching them", () => {
  assert.deepEqual(discoverInstalled(["claude", "pi"]), {
    codex: false,
    claudeCode: true,
    pi: true,
  });
  assert.deepEqual(discoverInstalled(["codex"]), {
    codex: true,
    claudeCode: false,
    pi: false,
  });
  assert.deepEqual(discoverInstalled([]), {
    codex: false,
    claudeCode: false,
    pi: false,
  });
});

test("filesystem discovery never executes an Agent CLI", async () => {
  const directory = await mkdtemp(join(tmpdir(), "mspace-agent-discovery-"));
  const sentinel = join(directory, "executed");
  const extension = process.platform === "win32" ? ".cmd" : "";
  try {
    for (const command of ["codex", "claude", "pi"]) {
      const executable = join(directory, `${command}${extension}`);
      const content = process.platform === "win32"
        ? `@echo executed>"${sentinel}"\r\n`
        : `#!/bin/sh\nprintf executed > "${sentinel}"\n`;
      await writeFile(executable, content);
      if (process.platform !== "win32") await chmod(executable, 0o755);
    }
    const capabilities = discoverAgentEngineCapabilities({
      env: {
        PATH: directory,
        ...(process.platform === "win32" ? { PATHEXT: ".CMD" } : {}),
      },
      homeDir: directory,
      platform: process.platform,
    });
    assert.deepEqual(capabilities, { codex: true, claudeCode: true, pi: true });
    assert.equal(existsSync(sentinel), false);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("uses PATHEXT for Windows Agent command shims", () => {
  assert.deepEqual(discoverInstalled(["claude.cmd", "pi.exe"], "win32"), {
    codex: false,
    claudeCode: true,
    pi: true,
  });
});

test("adds safe user install directories to the worker executable PATH", () => {
  const searchPath = buildAgentExecutableSearchPath({
    env: { PATH: "/custom/bin", PNPM_HOME: "/pnpm" },
    homeDir: "/home/tester",
    platform: "darwin",
  });
  const directories = searchPath.split(":");
  assert.equal(directories[0], "/custom/bin");
  assert.equal(directories.includes("/pnpm"), true);
  assert.equal(directories.includes("/home/tester/.local/bin"), true);
  assert.equal(directories.includes("/opt/homebrew/bin"), true);
  assert.equal(new Set(directories).size, directories.length);
});

test("Codex auth is gated only for an explicit Codex request", () => {
  assert.equal(personalWorkerRequiresCodexAuth(undefined), false);
  assert.equal(personalWorkerRequiresCodexAuth({}), false);
  assert.equal(personalWorkerRequiresCodexAuth({ claudeCode: true }), false);
  assert.equal(personalWorkerRequiresCodexAuth({ pi: true }), false);
  assert.equal(personalWorkerRequiresCodexAuth({ codex: false }), false);
  assert.equal(personalWorkerRequiresCodexAuth({ codex: true }), true);
});

test("reports exact missing required capabilities", () => {
  assert.deepEqual(missingPersonalWorkerCapabilities({ codex: false, claudeCode: true, pi: false }, undefined), []);
  assert.deepEqual(
    missingPersonalWorkerCapabilities(
      { codex: false, claudeCode: true, pi: false },
      { codex: true, claudeCode: true, pi: false, browser: true },
    ),
    ["codex", "browser"],
  );
});

test("maps missing Agent engine capabilities to actionable CLI names", () => {
  assert.deepEqual(
    missingAgentEngineExecutables(
      { codex: false, claudeCode: false, pi: true },
      { codex: true, claudeCode: true, pi: true, browser: true },
    ),
    [
      { capability: "codex", command: "codex" },
      { capability: "claudeCode", command: "claude" },
    ],
  );
});

test("ordinary personal worker startup does not require a browser", () => {
  assert.equal(personalWorkerRequiresBrowser(undefined), false);
  assert.equal(personalWorkerRequiresBrowser({ codex: true }), false);
  assert.equal(personalWorkerRequiresBrowser({ claudeCode: true, pi: true }), false);
  assert.equal(personalWorkerRequiresBrowser({ browser: false, chrome_cdp: false }), false);
});

test("explicit browser capabilities require a browser", () => {
  assert.equal(personalWorkerRequiresBrowser({ browser: true }), true);
  assert.equal(personalWorkerRequiresBrowser({ chrome_cdp: true }), true);
  assert.equal(personalWorkerRequiresBrowser({ browser: true, chrome_cdp: true }), true);
});

test("browser-backed work uses a separate personal worker identity", () => {
  assert.equal(personalWorkerName("workspace-123", { codex: true }), "desktop-personal-workspac");
  assert.equal(personalWorkerName("workspace-123", { claudeCode: true }), "desktop-personal-workspac");
  assert.equal(personalWorkerName("workspace-123", { pi: true }), "desktop-personal-workspac");
  assert.equal(personalWorkerName("workspace-123", { browser: true }), "desktop-personal-workspac-browser");
  assert.equal(personalWorkerName("workspace-123", { chrome_cdp: true }), "desktop-personal-workspac-browser");
  assert.equal(personalWorkerWorkRoot("/tmp/mspace-worker", { codex: true }), "/tmp/mspace-worker");
  assert.equal(personalWorkerWorkRoot("/tmp/mspace-worker", { claudeCode: true }), "/tmp/mspace-worker");
  assert.equal(personalWorkerWorkRoot("/tmp/mspace-worker", { browser: true }), "/tmp/mspace-worker/browser-companion");
});
