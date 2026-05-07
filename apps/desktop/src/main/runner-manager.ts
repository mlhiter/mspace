import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { app } from "electron";
import { existsSync } from "node:fs";
import { join } from "node:path";

const DEFAULT_RUNNER_PORT = 7788;
const DEFAULT_START_TIMEOUT_MS = 60_000;

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

async function fetchHealth(): Promise<boolean> {
  try {
    const res = await fetch(HEALTH_URL);
    return res.ok;
  } catch {
    return false;
  }
}

function resolveRunnerDir(): string {
  const cwdCandidate = join(process.cwd(), "runner");
  if (existsSync(cwdCandidate)) return cwdCandidate;
  return join(app.getAppPath(), "../../runner");
}

export async function ensureRunnerStarted(): Promise<void> {
  if (await fetchHealth()) return;
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
      if (await fetchHealth()) {
        clearInterval(timer);
        starting = null;
        resolve();
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
