import assert from "node:assert/strict";
import { copyFile, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

const repoRoot = new URL("../../../", import.meta.url);

function run(command, args, cwd, env = process.env) {
  return spawnSync(command, args, { cwd, env, encoding: "utf8" });
}

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
  const prepareRelease = await readFile(new URL("apps/desktop/scripts/prepare-release.mjs", repoRoot), "utf8");
  const settingsSource = await readFile(new URL("packages/views/src/workspace-settings-page.tsx", repoRoot), "utf8");

  assert.match(imageScript, /SOURCE_COMMIT_SHA=.*rev-parse HEAD/);
  assert.match(imageScript, /BUILD_COMMIT_SHA must match checkout HEAD/);
  assert.match(imageScript, /status --porcelain --untracked-files=all/);
  assert.match(releaseIdentity, /refs\/tags\/\$\{requestedTag\}/);
  assert.match(releaseIdentity, /rev-parse", "--verify", tagRef/);
  assert.match(releaseIdentity, /Release checkout must be clean/);
  assert.match(releaseWorkflow, /--verify-tag/);
  assert.doesNotMatch(releaseWorkflow, /git checkout "\$\{GITHUB_SHA\}" --/);
  assert.match(prepareRelease, /Release build version must match package\.json/);
  assert.match(prepareRelease, /Release build commit must match checkout HEAD/);
  assert.match(prepareRelease, /Desktop release build inputs must be clean and committed/);
  assert.match(settingsSource, /serverHealthQuery\.isError \? undefined : serverHealthQuery\.data/);
  assert.match(settingsSource, /workersQuery\.isError \? \[\] : workersQuery\.data \|\| \[\]/);
});

test("release identity accepts only a clean checkout at the exact tag", async (t) => {
  const tempRoot = await mkdtemp(join(tmpdir(), "mspace-release-identity-"));
  t.after(() => rm(tempRoot, { recursive: true, force: true }));
  const scriptDir = join(tempRoot, "scripts");
  await mkdir(scriptDir);
  await copyFile(fileURLToPath(new URL("scripts/resolve-release-identity.mjs", repoRoot)), join(scriptDir, "resolve-release-identity.mjs"));
  await writeFile(join(tempRoot, "package.json"), '{"version":"1.2.3"}\n');
  await writeFile(join(tempRoot, "README.md"), "clean\n");

  assert.equal(run("git", ["init", "--quiet"], tempRoot).status, 0);
  assert.equal(run("git", ["add", "."], tempRoot).status, 0);
  assert.equal(run("git", ["-c", "user.name=mspace", "-c", "user.email=mspace@example.invalid", "commit", "--quiet", "-m", "test"], tempRoot).status, 0);
  assert.equal(run("git", ["tag", "v1.2.3"], tempRoot).status, 0);

  const clean = run(process.execPath, [join(scriptDir, "resolve-release-identity.mjs"), "--tag=v1.2.3"], tempRoot, {
    ...process.env,
    MSPACE_BUILD_TIME: "2026-07-27T10:10:39Z",
  });
  assert.equal(clean.status, 0, clean.stderr);
  assert.equal(JSON.parse(clean.stdout).tag, "v1.2.3");

  await writeFile(join(tempRoot, "README.md"), "dirty\n");
  const dirty = run(process.execPath, [join(scriptDir, "resolve-release-identity.mjs"), "--tag=v1.2.3"], tempRoot);
  assert.notEqual(dirty.status, 0);
  assert.match(dirty.stderr, /Release checkout must be clean/);

  await writeFile(join(tempRoot, "README.md"), "clean\n");
  assert.equal(run("git", ["tag", "-d", "v1.2.3"], tempRoot).status, 0);
  assert.equal(run("git", ["branch", "v1.2.3"], tempRoot).status, 0);
  const branch = run(process.execPath, [join(scriptDir, "resolve-release-identity.mjs"), "--tag=v1.2.3"], tempRoot);
  assert.notEqual(branch.status, 0);
  assert.match(branch.stderr, /refs\/tags\/v1\.2\.3/);
});

test("desktop release preparation rejects spoofed identity before building", () => {
  const repoPath = dirname(fileURLToPath(new URL("package.json", repoRoot)));
  const scriptPath = fileURLToPath(new URL("apps/desktop/scripts/prepare-release.mjs", repoRoot));
  const result = run(process.execPath, [scriptPath, "--target=linux", "--arch=x64"], repoPath, {
    ...process.env,
    MSPACE_BUILD_VERSION: "9.9.9",
  });

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /Release build version must match package\.json/);
});
