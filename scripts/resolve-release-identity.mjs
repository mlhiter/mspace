import { appendFile, readFile } from "node:fs/promises";
import { spawn } from "node:child_process";
import { resolve } from "node:path";

const repoRoot = resolve(import.meta.dirname, "..");
const requestedTag = process.argv.find((arg) => arg.startsWith("--tag="))?.slice("--tag=".length);
const githubOutput = process.argv.find((arg) => arg.startsWith("--github-output="))?.slice("--github-output=".length);
const githubEnv = process.argv.find((arg) => arg.startsWith("--github-env="))?.slice("--github-env=".length);

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

const rootPackage = JSON.parse(await readFile(resolve(repoRoot, "package.json"), "utf8"));
const version = String(rootPackage.version || "").trim();
const expectedTag = `v${version}`;

if (!requestedTag) {
  throw new Error("A release tag is required");
}
if (requestedTag !== expectedTag) {
  throw new Error(`Release tag ${requestedTag} must equal ${expectedTag} from package.json`);
}
const commitSha = await capture("git", ["rev-parse", "HEAD"]);
const tagRef = `refs/tags/${requestedTag}`;
await capture("git", ["rev-parse", "--verify", tagRef]);
const tagCommitSha = await capture("git", ["rev-parse", `${tagRef}^{commit}`]);
const dirtyCheckout = await capture("git", ["status", "--porcelain", "--untracked-files=all"]);
const buildTime = (process.env.MSPACE_BUILD_TIME || new Date().toISOString()).trim();
if (tagCommitSha !== commitSha) {
  throw new Error(`Checked-out HEAD ${commitSha} does not match ${requestedTag} (${tagCommitSha})`);
}
if (dirtyCheckout) {
  throw new Error("Release checkout must be clean so packaged source matches the tagged commit");
}
if (!/^[0-9a-f]{40,64}$/.test(commitSha)) {
  throw new Error("Checked-out HEAD is not a full lowercase Git SHA");
}
if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(buildTime) || Number.isNaN(Date.parse(buildTime))) {
  throw new Error("MSPACE_BUILD_TIME must be an explicit RFC3339 timestamp");
}
if (process.env.MSPACE_BUILD_VERSION && process.env.MSPACE_BUILD_VERSION !== version) {
  throw new Error("MSPACE_BUILD_VERSION does not match package.json");
}
if (process.env.MSPACE_BUILD_COMMIT_SHA && process.env.MSPACE_BUILD_COMMIT_SHA !== commitSha) {
  throw new Error("MSPACE_BUILD_COMMIT_SHA does not match checkout HEAD");
}

const identity = { tag: requestedTag, version, commitSha, buildTime };

if (githubOutput) {
  await appendFile(
    githubOutput,
    `tag=${identity.tag}\nversion=${identity.version}\ncommit_sha=${identity.commitSha}\nbuild_time=${identity.buildTime}\n`,
  );
}
if (githubEnv) {
  await appendFile(
    githubEnv,
    `MSPACE_BUILD_VERSION=${identity.version}\nMSPACE_BUILD_COMMIT_SHA=${identity.commitSha}\nMSPACE_BUILD_TIME=${identity.buildTime}\n`,
  );
}

console.log(JSON.stringify(identity));
