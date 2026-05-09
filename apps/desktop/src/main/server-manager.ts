import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { app } from "electron";
import { existsSync } from "node:fs";
import { join } from "node:path";

const DEFAULT_SERVER_PORT = 8787;
const DEFAULT_START_TIMEOUT_MS = 30_000;

function readServerPort(): number {
  if (process.env.MSPACE_SERVER_ADDR) {
    const port = Number(process.env.MSPACE_SERVER_ADDR.split(":").at(-1));
    if (Number.isInteger(port) && port > 0) return port;
  }

  if (process.env.MSPACE_SERVER_URL) {
    try {
      const parsed = new URL(process.env.MSPACE_SERVER_URL);
      const urlPort = Number(parsed.port);
      if (Number.isInteger(urlPort) && urlPort > 0) return urlPort;
    } catch {
      // Ignore invalid override and fall back to the local default.
    }
  }

  return DEFAULT_SERVER_PORT;
}

function readStartTimeoutMs(): number {
  const timeout = Number(process.env.MSPACE_SERVER_START_TIMEOUT_MS);
  if (Number.isInteger(timeout) && timeout > 0) return timeout;
  return DEFAULT_START_TIMEOUT_MS;
}

const SERVER_PORT = readServerPort();
const START_TIMEOUT_MS = readStartTimeoutMs();

export function getServerBaseUrl(): string {
  if (process.env.MSPACE_SERVER_URL) {
    return process.env.MSPACE_SERVER_URL.replace(/\/+$/, "");
  }
  return `http://127.0.0.1:${SERVER_PORT}`;
}

const HEALTH_URL = new URL("/health", getServerBaseUrl()).toString();

let serverProcess: ChildProcessWithoutNullStreams | null = null;
let starting: Promise<void> | null = null;

async function fetchHealth(): Promise<boolean> {
  try {
    const res = await fetch(HEALTH_URL);
    return res.ok;
  } catch {
    return false;
  }
}

function resolveServerDir(): string {
  const cwdCandidate = join(process.cwd(), "server");
  if (existsSync(cwdCandidate)) return cwdCandidate;
  return join(app.getAppPath(), "../../server");
}

function readServerAddr(): string {
  if (process.env.MSPACE_SERVER_ADDR) return process.env.MSPACE_SERVER_ADDR;
  return `127.0.0.1:${SERVER_PORT}`;
}

export async function ensureServerStarted(): Promise<void> {
  if (await fetchHealth()) return;
  if (starting) return starting;

  starting = new Promise<void>((resolve, reject) => {
    const serverDir = resolveServerDir();
    serverProcess = spawn("go", ["run", "./cmd/server"], {
      cwd: serverDir,
      env: {
        ...process.env,
        MSPACE_SERVER_ADDR: readServerAddr(),
      },
      stdio: "pipe",
    });

    serverProcess.stdout.on("data", (chunk) => {
      process.stdout.write(`[server] ${chunk}`);
    });
    serverProcess.stderr.on("data", (chunk) => {
      process.stderr.write(`[server] ${chunk}`);
    });
    serverProcess.on("exit", () => {
      serverProcess = null;
      starting = null;
    });
    serverProcess.on("error", (error) => {
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
        reject(new Error(`Timed out waiting ${START_TIMEOUT_MS}ms for the mspace server to become healthy`));
      }
    }, 500);
  });

  return starting;
}

export async function stopServer(): Promise<void> {
  if (!serverProcess) return;
  serverProcess.kill("SIGTERM");
  serverProcess = null;
}
