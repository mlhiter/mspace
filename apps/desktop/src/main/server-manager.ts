import { execFile, spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { app } from "electron";
import { existsSync } from "node:fs";
import { join } from "node:path";

const DEFAULT_SERVER_PORT = 8787;
const DEFAULT_START_TIMEOUT_MS = 30_000;
const EXPECTED_SERVER_PROTOCOL = 1;
const EXPECTED_SERVER_CAPABILITIES = [
  "workspaceInboxIssueGrouping",
  "teamWorkspaceCreation",
  "workspaceInvitations",
  "workspaceKinds",
  "workspaceCollaboration",
  "runtimeWorkerRegistration",
  "runtimeTaskQueue",
] as const;

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

type ServerReadiness = "ready" | "unavailable" | "stale";

type ServerHealth = {
  ok?: unknown;
  serverProtocol?: unknown;
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

function hasExpectedServerCapabilities(payload: ServerHealth): boolean {
  if (payload.ok !== true) return false;
  if (payload.serverProtocol !== EXPECTED_SERVER_PROTOCOL) return false;
  const capabilities = payload.capabilities;
  if (!isObjectRecord(capabilities)) return false;

  return EXPECTED_SERVER_CAPABILITIES.every((capability) => capabilities[capability] === true);
}

async function fetchReadiness(): Promise<ServerReadiness> {
  try {
    const res = await fetch(HEALTH_URL);
    if (!res.ok) return "unavailable";

    let payload: ServerHealth;
    try {
      payload = (await res.json()) as ServerHealth;
    } catch {
      return "stale";
    }
    return hasExpectedServerCapabilities(payload) ? "ready" : "stale";
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

async function readServerPortPids(): Promise<number[]> {
  const output = await execFileOutput("lsof", ["-tiTCP:" + SERVER_PORT, "-sTCP:LISTEN"]);
  const pids = output
    .split(/\s+/)
    .map((pid) => Number(pid))
    .filter((pid) => Number.isInteger(pid) && pid > 0 && pid !== process.pid);
  return [...new Set(pids)];
}

async function stopStaleLocalServer(): Promise<void> {
  if (process.env.MSPACE_SERVER_URL) {
    throw new Error(
      `mspace server at ${getServerBaseUrl()} is healthy but does not expose the expected protocol. Restart the configured server.`,
    );
  }
  if (app.isPackaged) {
    throw new Error(`mspace server at ${getServerBaseUrl()} is healthy but does not expose the expected protocol.`);
  }

  const pids = await readServerPortPids();
  if (pids.length === 0) return;

  console.warn(`[server] replacing stale local server on port ${SERVER_PORT}: ${pids.join(", ")}`);
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

  for (const pid of await readServerPortPids()) {
    try {
      process.kill(pid, "SIGKILL");
    } catch {
      // The process may have exited between lsof and kill.
    }
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
  const readiness = await fetchReadiness();
  if (readiness === "ready") return;
  if (readiness === "stale") await stopStaleLocalServer();
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
      if (await fetchReadiness() === "ready") {
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
