import { mkdir, copyFile, rm } from "node:fs/promises";
import { existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { spawn } from "node:child_process";
import pngToIco from "png-to-ico";

const repoRoot = resolve(import.meta.dirname, "../../..");
const desktopRoot = resolve(repoRoot, "apps/desktop");
const buildDir = resolve(desktopRoot, "build");
const serverOut = resolve(desktopRoot, "resources/bin/mspace-server");
const workerOut = resolve(desktopRoot, "resources/bin/mspace-worker");
const serverBuildDir = resolve(buildDir, "server");
const workerBuildDir = resolve(buildDir, "worker");
const pngIcon = resolve(desktopRoot, "assets/brand/mspace-icon.png");
const icnsIcon = resolve(buildDir, "icon.icns");
const icoIcon = resolve(buildDir, "icon.ico");
const releaseEnv = process.env.MSPACE_RELEASE_ENV || "local";

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

await mkdir(resolve(desktopRoot, "resources/bin"), { recursive: true });
await mkdir(buildDir, { recursive: true });

async function buildServer(output, goos, goarch) {
  await exec("go", ["build", "-o", output, "./cmd/server"], {
    cwd: resolve(repoRoot, "server"),
    env: {
      ...process.env,
      CGO_ENABLED: "0",
      GOOS: goos,
      GOARCH: goarch,
    },
  });
}

async function buildWorker(output, goos, goarch) {
  await exec("go", ["build", "-o", output, "."], {
    cwd: resolve(repoRoot, "worker"),
    env: {
      ...process.env,
      CGO_ENABLED: "0",
      GOOS: goos,
      GOARCH: goarch,
    },
  });
}

if (process.platform === "darwin") {
  await rm(serverBuildDir, { recursive: true, force: true });
  await rm(workerBuildDir, { recursive: true, force: true });
  await mkdir(serverBuildDir, { recursive: true });
  await mkdir(workerBuildDir, { recursive: true });
  const arm64Server = resolve(serverBuildDir, "mspace-server-arm64");
  const x64Server = resolve(serverBuildDir, "mspace-server-x64");
  const arm64Worker = resolve(workerBuildDir, "mspace-worker-arm64");
  const x64Worker = resolve(workerBuildDir, "mspace-worker-x64");
  await buildServer(arm64Server, "darwin", "arm64");
  await buildServer(x64Server, "darwin", "amd64");
  await buildWorker(arm64Worker, "darwin", "arm64");
  await buildWorker(x64Worker, "darwin", "amd64");
  await exec("lipo", ["-create", arm64Server, x64Server, "-output", serverOut]);
  await exec("lipo", ["-create", arm64Worker, x64Worker, "-output", workerOut]);
} else {
  const goos = goosByPlatform[process.platform];
  const goarch = goarchByArch[process.arch];
  if (!goos || !goarch) {
    throw new Error(`Unsupported release host for bundled server: ${process.platform}/${process.arch}`);
  }
  await buildServer(serverOut, goos, goarch);
  await buildWorker(workerOut, goos, goarch);
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

console.log(`Prepared mspace desktop release assets (${releaseEnv}).`);
