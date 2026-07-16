import assert from "node:assert/strict";
import test from "node:test";
import { personalWorkerName, personalWorkerRequiresBrowser, personalWorkerWorkRoot } from "../src/main/personal-worker-runtime.ts";

test("ordinary personal worker startup does not require a browser", () => {
  assert.equal(personalWorkerRequiresBrowser(undefined), false);
  assert.equal(personalWorkerRequiresBrowser({ codex: true }), false);
  assert.equal(personalWorkerRequiresBrowser({ browser: false, chrome_cdp: false }), false);
});

test("explicit browser capabilities require a browser", () => {
  assert.equal(personalWorkerRequiresBrowser({ browser: true }), true);
  assert.equal(personalWorkerRequiresBrowser({ chrome_cdp: true }), true);
  assert.equal(personalWorkerRequiresBrowser({ browser: true, chrome_cdp: true }), true);
});

test("browser-backed work uses a separate personal worker identity", () => {
  assert.equal(personalWorkerName("workspace-123", { codex: true }), "desktop-personal-workspac");
  assert.equal(personalWorkerName("workspace-123", { browser: true }), "desktop-personal-workspac-browser");
  assert.equal(personalWorkerName("workspace-123", { chrome_cdp: true }), "desktop-personal-workspac-browser");
  assert.equal(personalWorkerWorkRoot("/tmp/mspace-worker", { codex: true }), "/tmp/mspace-worker");
  assert.equal(personalWorkerWorkRoot("/tmp/mspace-worker", { browser: true }), "/tmp/mspace-worker/browser-companion");
});
