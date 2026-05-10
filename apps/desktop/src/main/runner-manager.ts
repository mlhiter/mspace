import { execFile, spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { app } from "electron";
import { existsSync } from "node:fs";
import { join } from "node:path";

const DEFAULT_RUNNER_PORT = 7788;
const DEFAULT_START_TIMEOUT_MS = 60_000;
const EXPECTED_RUNNER_PROTOCOL = 1;
const EXPECTED_RUNNER_CAPABILITIES = ["issueAttachments"] as const;

function readRunnerPort(): number {
  const explicitPort = Number(process.env.MSPACE_RUNNER_PORT);
  if (Number.isInteger(explicitPort) && explicitPort > 0) return explicitPort;

  if (process.env.MSPACE_RUNNER_URL) {
    try {
      const parsed = new URL(process.env.MSPACE_RUNNER_URL);
      const urlPort = Number(parsed.port);
      if (Number.isInteger(urlPort) && urlPort > 0) return urlPort;
    } catch {
      // Ignore invalid override and fall back to the local default.
    }
  }

  return DEFAULT_RUNNER_PORT;
}

function readStartTimeoutMs(): number {
  const timeout = Number(process.env.MSPACE_RUNNER_START_TIMEOUT_MS);
  if (Number.isInteger(timeout) && timeout > 0) return timeout;
  return DEFAULT_START_TIMEOUT_MS;
}

const RUNNER_PORT = readRunnerPort();
const START_TIMEOUT_MS = readStartTimeoutMs();

export function getRunnerBaseUrl(): string {
  if (process.env.MSPACE_RUNNER_URL) {
    return process.env.MSPACE_RUNNER_URL.replace(/\/+$/, "");
  }
  return `http://127.0.0.1:${RUNNER_PORT}`;
}

const HEALTH_URL = new URL("/health", getRunnerBaseUrl()).toString();

let runnerProcess: ChildProcessWithoutNullStreams | null = null;
let starting: Promise<void> | null = null;

type RunnerReadiness = "ready" | "unavailable" | "stale";

type RunnerHealth = {
  ok?: unknown;
  runnerProtocol?: unknown;
  capabilities?: unknown;
};

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

function isObjectRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function hasExpectedRunnerCapabilities(payload: RunnerHealth): boolean {
  if (payload.ok !== true) return false;
  if (payload.runnerProtocol !== EXPECTED_RUNNER_PROTOCOL) return false;
  const capabilities = payload.capabilities;
  if (!isObjectRecord(capabilities)) return false;

  return EXPECTED_RUNNER_CAPABILITIES.every((capability) => capabilities[capability] === true);
}

async function fetchReadiness(): Promise<RunnerReadiness> {
  try {
    const res = await fetch(HEALTH_URL);
    if (!res.ok) return "unavailable";

    let payload: RunnerHealth;
    try {
      payload = (await res.json()) as RunnerHealth;
    } catch {
      return "stale";
    }
    return hasExpectedRunnerCapabilities(payload) ? "ready" : "stale";
  } catch {
    return "unavailable";
  }
}

function execFileOutput(command: string, args: string[]): Promise<string> {
  return new Promise((resolve) => {
    execFile(command, args, { timeout: 2_000 }, (error, stdout) => {
      if (error) {
        resolve("");
        return;
      }
      resolve(stdout);
    });
  });
}

async function readRunnerPortPids(): Promise<number[]> {
  const output = await execFileOutput("lsof", ["-tiTCP:" + RUNNER_PORT, "-sTCP:LISTEN"]);
  const pids = output
    .split(/\s+/)
    .map((pid) => Number(pid))
    .filter((pid) => Number.isInteger(pid) && pid > 0 && pid !== process.pid);
  return [...new Set(pids)];
}

async function stopStaleLocalRunner(): Promise<void> {
  if (process.env.MSPACE_RUNNER_URL) {
    throw new Error(
      `Runner at ${getRunnerBaseUrl()} is healthy but does not expose the expected protocol. Restart the configured runner.`,
    );
  }
  if (app.isPackaged) {
    throw new Error(`Runner at ${getRunnerBaseUrl()} is healthy but does not expose the expected protocol.`);
  }

  const pids = await readRunnerPortPids();
  if (pids.length === 0) return;

  console.warn(`[runner] replacing stale local runner on port ${RUNNER_PORT}: ${pids.join(", ")}`);
  for (const pid of pids) {
    try {
      process.kill(pid, "SIGTERM");
    } catch {
      // The process may have exited between lsof and kill.
    }
  }

  const deadline = Date.now() + 3_000;
  while (Date.now() < deadline) {
    if (await fetchReadiness() === "unavailable") return;
    await sleep(150);
  }

  for (const pid of await readRunnerPortPids()) {
    try {
      process.kill(pid, "SIGKILL");
    } catch {
      // The process may have exited between lsof and kill.
    }
  }
}

function resolveRunnerDir(): string {
  const cwdCandidate = join(process.cwd(), "runner");
  if (existsSync(cwdCandidate)) return cwdCandidate;
  return join(app.getAppPath(), "../../runner");
}

export async function ensureRunnerStarted(): Promise<void> {
  const readiness = await fetchReadiness();
  if (readiness === "ready") return;
  if (readiness === "stale") await stopStaleLocalRunner();
  if (starting) return starting;

  starting = new Promise<void>((resolve, reject) => {
    const runnerDir = resolveRunnerDir();
    runnerProcess = spawn("go", ["run", "."], {
      cwd: runnerDir,
      env: {
        ...process.env,
        MSPACE_PORT: String(RUNNER_PORT),
      },
      stdio: "pipe",
    });

    runnerProcess.stdout.on("data", (chunk) => {
      process.stdout.write(`[runner] ${chunk}`);
    });
    runnerProcess.stderr.on("data", (chunk) => {
      process.stderr.write(`[runner] ${chunk}`);
    });
    runnerProcess.on("exit", () => {
      runnerProcess = null;
      starting = null;
    });
    runnerProcess.on("error", (error) => {
      reject(error);
    });

    const deadline = Date.now() + START_TIMEOUT_MS;
    const timer = setInterval(async () => {
      const readiness = await fetchReadiness();
      if (readiness === "ready") {
        clearInterval(timer);
        starting = null;
        resolve();
        return;
      }
      if (readiness === "stale") {
        clearInterval(timer);
        starting = null;
        reject(new Error(`A stale runner is still listening on ${getRunnerBaseUrl()}`));
        return;
      }
      if (Date.now() > deadline) {
        clearInterval(timer);
        starting = null;
        reject(new Error(`Timed out waiting ${START_TIMEOUT_MS}ms for the local runner to become healthy`));
      }
    }, 500);
  });

  return starting;
}

export async function stopRunner(): Promise<void> {
  if (!runnerProcess) return;
  runnerProcess.kill("SIGTERM");
  runnerProcess = null;
}
