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

test("container and release identity are bound to real Git objects", async () => {
  const imageScript = await readFile(new URL("deploy/scripts/build-images.sh", repoRoot), "utf8");
  const releaseIdentity = await readFile(new URL("scripts/resolve-release-identity.mjs", repoRoot), "utf8");
  const releaseWorkflow = await readFile(new URL(".github/workflows/release.yml", repoRoot), "utf8");

  assert.match(imageScript, /SOURCE_COMMIT_SHA=.*rev-parse HEAD/);
  assert.match(imageScript, /BUILD_COMMIT_SHA must match checkout HEAD/);
  assert.match(imageScript, /status --porcelain --untracked-files=all/);
  assert.match(releaseIdentity, /refs\/tags\/\$\{requestedTag\}/);
  assert.match(releaseIdentity, /rev-parse", "--verify", tagRef/);
  assert.match(releaseWorkflow, /--verify-tag/);
  assert.match(releaseWorkflow, /git checkout "\$\{GITHUB_SHA\}" --/);
});
