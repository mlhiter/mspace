import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { app } from "electron";
import { existsSync } from "node:fs";
import { join } from "node:path";

const RUNNER_PORT = Number(process.env.MSPACE_RUNNER_PORT || 7788);
const HEALTH_URL = `http://127.0.0.1:${RUNNER_PORT}/health`;

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

    const deadline = Date.now() + 15_000;
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
        reject(new Error("Timed out waiting for the local runner to become healthy"));
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
