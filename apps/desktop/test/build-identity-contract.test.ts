import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const repoRoot = new URL("../../../", import.meta.url);

test("Desktop version is injected by Electron main and development reports dev", async () => {
  const mainSource = await readFile(new URL("apps/desktop/src/main/index.ts", repoRoot), "utf8");
  const preloadSource = await readFile(new URL("apps/desktop/src/preload/index.ts", repoRoot), "utf8");

  assert.match(mainSource, /app\.isPackaged \? app\.getVersion\(\) : "dev"/);
  assert.match(mainSource, /--mspace-app-version=/);
  assert.match(preloadSource, /readArgument\("mspace-app-version"\) \|\| "dev"/);
  assert.doesNotMatch(preloadSource, /npm_package_version/);
});
