import { mkdir, copyFile, readFile, rm } from "node:fs/promises";
import { existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { spawn } from "node:child_process";
import pngToIco from "png-to-ico";

const repoRoot = resolve(import.meta.dirname, "../../..");
const desktopRoot = resolve(repoRoot, "apps/desktop");
const buildDir = resolve(desktopRoot, "build");
const resourcesBinDir = resolve(desktopRoot, "resources/bin");
const serverBuildDir = resolve(buildDir, "server");
const runtimeBuildDir = resolve(buildDir, "runtime");
const pngIcon = resolve(desktopRoot, "assets/brand/mspace-icon.png");
const icnsIcon = resolve(buildDir, "icon.icns");
const icoIcon = resolve(buildDir, "icon.ico");
const releaseEnv = process.env.MSPACE_RELEASE_ENV || "local";
const targetPlatform =
  process.argv.find((arg) => arg.startsWith("--target="))?.slice("--target=".length) ||
  process.env.MSPACE_RELEASE_TARGET ||
  process.platform;
const targetArch =
  process.argv.find((arg) => arg.startsWith("--arch="))?.slice("--arch=".length) ||
  process.env.MSPACE_RELEASE_ARCH ||
  process.arch;
const hasWorkerRuntime = existsSync(resolve(repoRoot, "worker"));
const hasRunnerRuntime = existsSync(resolve(repoRoot, "runner"));
const runtimeDir = hasWorkerRuntime ? resolve(repoRoot, "worker") : resolve(repoRoot, "runner");
const runtimeName = hasWorkerRuntime ? "mspace-worker" : "mspace-runner";
const serverBinaryName = targetPlatform === "win32" ? "mspace-server.exe" : "mspace-server";
const runtimeBinaryName = targetPlatform === "win32" ? `${runtimeName}.exe` : runtimeName;
const serverOut = resolve(resourcesBinDir, serverBinaryName);
const runtimeOut = resolve(resourcesBinDir, runtimeBinaryName);
const rootPackage = JSON.parse(await readFile(resolve(repoRoot, "package.json"), "utf8"));
const buildVersion = (process.env.MSPACE_BUILD_VERSION || rootPackage.version || "").trim();
const buildCommitSha = (process.env.MSPACE_BUILD_COMMIT_SHA || (await capture("git", ["rev-parse", "HEAD"]))).trim();
const buildTime = (process.env.MSPACE_BUILD_TIME || new Date().toISOString()).trim();
const serverLdflags = `-s -w -X github.com/mlhiter/mspace/server/internal/control.version=${buildVersion} -X github.com/mlhiter/mspace/server/internal/control.commitSHA=${buildCommitSha} -X github.com/mlhiter/mspace/server/internal/control.buildTime=${buildTime}`;
const runtimeLdflags = `-s -w -X main.workerVersion=${buildVersion}`;

const goosByPlatform = {
  darwin: "darwin",
  linux: "linux",
  win32: "windows",
};

const goarchByArch = {
  arm64: "arm64",
  x64: "amd64",
};

function exec(command, args, options = {}) {
  return new Promise((resolvePromise, reject) => {
    const child = spawn(command, args, { cwd: repoRoot, stdio: "inherit", ...options });
    child.on("error", reject);
    child.on("exit", (code) => {
      if (code === 0) resolvePromise();
      else reject(new Error(`${command} ${args.join(" ")} exited with ${code}`));
    });
  });
}

function capture(command, args) {
  return new Promise((resolvePromise, reject) => {
    const child = spawn(command, args, { cwd: repoRoot, stdio: ["ignore", "pipe", "inherit"] });
    let stdout = "";
    child.stdout.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      stdout += chunk;
    });
    child.on("error", reject);
    child.on("exit", (code) => {
      if (code === 0) resolvePromise(stdout.trim());
      else reject(new Error(`${command} ${args.join(" ")} exited with ${code}`));
    });
  });
}

if (!hasWorkerRuntime && !hasRunnerRuntime) {
  throw new Error("Unsupported release source: expected worker/ or runner/ runtime directory");
}
if (!/^[0-9A-Za-z][0-9A-Za-z.+-]{0,63}$/.test(buildVersion)) {
  throw new Error("Invalid release build version");
}
if (!/^[0-9a-f]{40,64}$/.test(buildCommitSha)) {
  throw new Error("Release build commit must be checkout HEAD as a full lowercase Git SHA");
}
if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(buildTime) || Number.isNaN(Date.parse(buildTime))) {
  throw new Error("Release build time must be an explicit RFC3339 timestamp");
}

await rm(resourcesBinDir, { recursive: true, force: true });
await mkdir(resourcesBinDir, { recursive: true });
await mkdir(buildDir, { recursive: true });

async function buildServer(output, goos, goarch) {
  await exec("go", ["build", "-trimpath", "-ldflags", serverLdflags, "-o", output, "./cmd/server"], {
    cwd: resolve(repoRoot, "server"),
    env: {
      ...process.env,
      CGO_ENABLED: "0",
      GOOS: goos,
      GOARCH: goarch,
    },
  });
}

async function buildRuntime(output, goos, goarch) {
  await exec("go", ["build", "-trimpath", "-ldflags", runtimeLdflags, "-o", output, "."], {
    cwd: runtimeDir,
    env: {
      ...process.env,
      CGO_ENABLED: "0",
      GOOS: goos,
      GOARCH: goarch,
    },
  });
}

if (!goosByPlatform[targetPlatform]) {
  throw new Error(`Unsupported release target: ${targetPlatform}`);
}
if (targetPlatform !== "darwin" && !goarchByArch[targetArch]) {
  throw new Error(`Unsupported release architecture: ${targetArch}`);
}

if (targetPlatform === "darwin") {
  if (process.platform !== "darwin") {
    throw new Error("macOS desktop installers must be prepared on a macOS host");
  }
  await rm(serverBuildDir, { recursive: true, force: true });
  await rm(runtimeBuildDir, { recursive: true, force: true });
  await mkdir(serverBuildDir, { recursive: true });
  await mkdir(runtimeBuildDir, { recursive: true });
  const arm64Server = resolve(serverBuildDir, "mspace-server-arm64");
  const x64Server = resolve(serverBuildDir, "mspace-server-x64");
  const arm64Runtime = resolve(runtimeBuildDir, `${runtimeName}-arm64`);
  const x64Runtime = resolve(runtimeBuildDir, `${runtimeName}-x64`);
  await buildServer(arm64Server, "darwin", "arm64");
  await buildServer(x64Server, "darwin", "amd64");
  await buildRuntime(arm64Runtime, "darwin", "arm64");
  await buildRuntime(x64Runtime, "darwin", "amd64");
  await exec("lipo", ["-create", arm64Server, x64Server, "-output", serverOut]);
  await exec("lipo", ["-create", arm64Runtime, x64Runtime, "-output", runtimeOut]);
} else {
  const goos = goosByPlatform[targetPlatform];
  const goarch = goarchByArch[targetArch];
  if (!goos || !goarch) {
    throw new Error(`Unsupported release target for bundled server: ${targetPlatform}/${targetArch}`);
  }
  await buildServer(serverOut, goos, goarch);
  await buildRuntime(runtimeOut, goos, goarch);
}

await copyFile(pngIcon, resolve(buildDir, "icon.png"));

if (process.platform === "darwin") {
  const iconset = resolve(buildDir, "icon.iconset");
  await mkdir(iconset, { recursive: true });
  const sizes = [
    [16, "icon_16x16.png"],
    [32, "icon_16x16@2x.png"],
    [32, "icon_32x32.png"],
    [64, "icon_32x32@2x.png"],
    [128, "icon_128x128.png"],
    [256, "icon_128x128@2x.png"],
    [256, "icon_256x256.png"],
    [512, "icon_256x256@2x.png"],
    [512, "icon_512x512.png"],
    [1024, "icon_512x512@2x.png"],
  ];
  for (const [size, name] of sizes) {
    await exec("sips", ["-z", String(size), String(size), pngIcon, "--out", resolve(iconset, name)]);
  }
  await exec("iconutil", ["-c", "icns", iconset, "-o", icnsIcon]);
}

if (!existsSync(icoIcon)) {
  const ico = await pngToIco(pngIcon);
  await mkdir(dirname(icoIcon), { recursive: true });
  await import("node:fs/promises").then(({ writeFile }) => writeFile(icoIcon, ico));
}

console.log(`Prepared mspace desktop release assets (${releaseEnv}) at ${buildVersion} ${buildCommitSha} ${buildTime}.`);
