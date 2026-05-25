import { execFile, spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { app } from "electron";
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const DEFAULT_SERVER_PORT = 8787;
const DEFAULT_SERVER_BASE_URL = `http://127.0.0.1:${DEFAULT_SERVER_PORT}`;
const DEFAULT_START_TIMEOUT_MS = 30_000;
const EXPECTED_SERVER_PROTOCOL = 1;
const SERVER_CONFIG_FILE = "server-config.json";
const EXPECTED_SERVER_CAPABILITIES = [
  "workspaceInboxIssueGrouping",
  "teamWorkspaceCreation",
  "workspaceInvitations",
  "workspaceKinds",
  "workspaceCollaboration",
  "runtimeWorkerRegistration",
  "runtimeTaskQueue",
] as const;

type ServerConfigFile = {
  serverUrl?: unknown;
};

export type ServerConfig = {
  baseUrl: string;
  source: "environment" | "user" | "default";
  locked: boolean;
};

function normalizeServerBaseUrl(value: string): string {
  const trimmed = value.trim();
  if (!trimmed) throw new Error("server URL is required");
  const parsed = new URL(trimmed);
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    throw new Error("server URL must use http or https");
  }
  return parsed.origin.replace(/\/+$/, "");
}

function serverConfigPath(): string {
  return join(app.getPath("userData"), SERVER_CONFIG_FILE);
}

function readUserConfiguredServerBaseUrl(): string {
  try {
    const payload = JSON.parse(readFileSync(serverConfigPath(), "utf8")) as ServerConfigFile;
    if (typeof payload.serverUrl !== "string") return "";
    return normalizeServerBaseUrl(payload.serverUrl);
  } catch {
    return "";
  }
}

function readServerConfig(): ServerConfig {
  if (process.env.MSPACE_SERVER_URL) {
    return {
      baseUrl: normalizeServerBaseUrl(process.env.MSPACE_SERVER_URL),
      source: "environment",
      locked: true,
    };
  }

  const userConfiguredUrl = readUserConfiguredServerBaseUrl();
  if (userConfiguredUrl) {
    return {
      baseUrl: userConfiguredUrl,
      source: "user",
      locked: false,
    };
  }

  return {
    baseUrl: DEFAULT_SERVER_BASE_URL,
    source: "default",
    locked: false,
  };
}

export function getServerConfig(): ServerConfig {
  return readServerConfig();
}

export function setConfiguredServerBaseUrl(value: string): ServerConfig {
  if (process.env.MSPACE_SERVER_URL) {
    throw new Error("MSPACE_SERVER_URL controls the server for this launch.");
  }
  const baseUrl = normalizeServerBaseUrl(value);
  mkdirSync(app.getPath("userData"), { recursive: true });
  writeFileSync(serverConfigPath(), `${JSON.stringify({ serverUrl: baseUrl }, null, 2)}\n`, "utf8");
  return readServerConfig();
}

export async function resetConfiguredServerBaseUrl(): Promise<ServerConfig> {
  if (process.env.MSPACE_SERVER_URL) {
    throw new Error("MSPACE_SERVER_URL controls the server for this launch.");
  }
  try {
    rmSync(serverConfigPath(), { force: true });
  } catch {
    // The config file may not exist yet.
  }
  await ensureServerStarted();
  return readServerConfig();
}

function readServerPort(): number {
  if (process.env.MSPACE_SERVER_ADDR) {
    const port = Number(process.env.MSPACE_SERVER_ADDR.split(":").at(-1));
    if (Number.isInteger(port) && port > 0) return port;
  }

  const configuredBaseUrl = getServerBaseUrl();
  if (configuredBaseUrl !== DEFAULT_SERVER_BASE_URL) {
    try {
      const parsed = new URL(configuredBaseUrl);
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

export function getServerBaseUrl(): string {
  return readServerConfig().baseUrl;
}

function hasConfiguredRemoteServer(): boolean {
  return readServerConfig().source !== "default";
}

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
    const res = await fetch(new URL("/health", getServerBaseUrl()).toString());
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
  const serverPort = readServerPort();
  const output = await execFileOutput("lsof", ["-tiTCP:" + serverPort, "-sTCP:LISTEN"]);
  const pids = output
    .split(/\s+/)
    .map((pid) => Number(pid))
    .filter((pid) => Number.isInteger(pid) && pid > 0 && pid !== process.pid);
  return [...new Set(pids)];
}

async function stopStaleLocalServer(): Promise<void> {
  if (hasConfiguredRemoteServer()) {
    throw new Error(
      `mspace server at ${getServerBaseUrl()} is healthy but does not expose the expected protocol. Restart the configured server.`,
    );
  }
  if (app.isPackaged) {
    throw new Error(`mspace server at ${getServerBaseUrl()} is healthy but does not expose the expected protocol.`);
  }

  const pids = await readServerPortPids();
  if (pids.length === 0) return;

  console.warn(`[server] replacing stale local server on port ${readServerPort()}: ${pids.join(", ")}`);
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
  return `127.0.0.1:${readServerPort()}`;
}

export async function ensureServerStarted(): Promise<void> {
  const readiness = await fetchReadiness();
  if (readiness === "ready") return;
  if (readiness === "stale") await stopStaleLocalServer();
  if (hasConfiguredRemoteServer()) {
    throw new Error(`Configured mspace server at ${getServerBaseUrl()} is not reachable.`);
  }
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

    const startTimeoutMs = readStartTimeoutMs();
    const deadline = Date.now() + startTimeoutMs;
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
        reject(new Error(`Timed out waiting ${startTimeoutMs}ms for the mspace server to become healthy`));
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
