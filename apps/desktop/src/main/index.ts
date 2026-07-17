import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  shell,
  type BrowserWindowConstructorOptions,
  type OpenDialogOptions,
} from "electron";
import { execFile, spawn, type ChildProcess, type ChildProcessWithoutNullStreams } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdir, rename, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:net";
import { homedir } from "node:os";
import { basename, dirname, join } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";
import {
  ensureServerStarted,
  getServerBaseUrl,
  getServerConfig,
  resetConfiguredServerBaseUrl,
  setConfiguredServerBaseUrl,
  stopServer,
} from "./server-manager";
import {
  buildAgentExecutableSearchPath,
  discoverAgentEngineCapabilities,
  missingAgentEngineExecutables,
  missingPersonalWorkerCapabilities,
  personalWorkerName,
  personalWorkerRequiresBrowser,
  personalWorkerRequiresCodexAuth,
  personalWorkerWorkRoot,
  type PersonalWorkerCapabilities,
} from "./personal-worker-runtime";

let mainWindow: BrowserWindow | null = null;
let projectFolderPickerRegistered = false;
let kubeconfigFilePickerRegistered = false;
let openHandlersRegistered = false;
let inviteHandlersRegistered = false;
let dockerWorkerHandlersRegistered = false;
let serverConfigHandlersRegistered = false;
let personalWorkerHandlersRegistered = false;
let dockerWorkerProcess: ChildProcessWithoutNullStreams | null = null;
let dockerWorkerContainer = "";
let personalWorkerProcess: ChildProcessWithoutNullStreams | null = null;
const personalWorkerCompanionProcesses = new Set<ChildProcessWithoutNullStreams>();
let personalWorkerRuntime: PersonalWorkerRuntime | null = null;
let personalWorkerBrowserProcess: ChildProcess | null = null;
let personalWorkerBrowserCdpUrl = "";
let personalWorkerBrowserSource = "";
let personalWorkerWorkspaceId = "";
let personalWorkerRestartTimer: NodeJS.Timeout | null = null;
let personalWorkerCredentialTimer: NodeJS.Timeout | null = null;
let personalWorkerOldCredentialRevokeTimers = new Set<NodeJS.Timeout>();
let personalWorkerEnsureInFlight: Promise<EnsurePersonalWorkerResult> | null = null;
let personalWorkerRestartAttempts = 0;
let personalWorkerStopping = false;
let personalWorkerCredential: RuntimeRegistrationTokenResult | null = null;
let personalWorkerCredentialInput: EnsurePersonalWorkerInput | null = null;
const BRAND_ICON_PATH = join("assets", "brand", "mspace-icon.png");
const PERSONAL_WORKER_TOKEN_HOURS = 12;
const PERSONAL_WORKER_TOKEN_RENEWAL_BUFFER_MS = 15 * 60 * 1000;
const PERSONAL_WORKER_TOKEN_RETRY_MS = 60 * 1000;
const PERSONAL_WORKER_OLD_TOKEN_REVOKE_DELAY_MS = 30 * 1000;
const PERSONAL_WORKER_BROWSER_START_TIMEOUT_MS = 10_000;
const PERSONAL_WORKER_BROWSER_CDP_HOST = "127.0.0.1";
const DEEP_LINK_PROTOCOL = "mspace";
let pendingInviteToken = "";

type StartDockerWorkerInput = {
  authToken?: string;
  workspaceId?: string;
  mode?: string;
  serverUrl?: string;
  codex?: boolean;
  containerName?: string;
  workerName?: string;
};

type StartDockerWorkerResult = {
  ok: boolean;
  status: string;
  containerName: string;
  script: string;
};

type EnsurePersonalWorkerInput = {
  authToken?: string;
  workspaceId?: string;
  serverUrl?: string;
  credentialServerUrl?: string;
  requiredCapabilities?: Record<string, unknown>;
};

type EnsurePersonalWorkerResult = {
  ok: boolean;
  status: string;
  workerName: string;
  capabilities?: PersonalWorkerCapabilities;
};

type PersonalWorkerRuntime = {
  capabilities: PersonalWorkerCapabilities;
  labels: Record<string, string>;
  env: Record<string, string>;
};

type PersonalWorkerEngineRuntime = {
  capabilities: Pick<PersonalWorkerCapabilities, "codex" | "claudeCode" | "pi">;
  executableSearchPath: string;
};

type RuntimeRegistrationTokenResult = {
  token?: string;
  registrationToken?: {
    id?: string;
    expiresAt?: string;
  };
};

function runtimeHasRequiredCapabilities(runtime: PersonalWorkerRuntime | null, requiredCapabilities: Record<string, unknown> | undefined): boolean {
  if (!requiredCapabilities) return true;
  if (!runtime) return false;
  return Object.entries(requiredCapabilities).every(([capability, required]) => required !== true || runtime.capabilities[capability] === true);
}

function assertPersonalWorkerCapabilities(runtime: PersonalWorkerRuntime, requiredCapabilities: Record<string, unknown> | undefined): void {
  const missing = missingPersonalWorkerCapabilities(runtime.capabilities, requiredCapabilities);
  if (missing.length === 0) return;
  if (missing.every((capability) => capability === "browser" || capability === "chrome_cdp")) {
    throw new Error("The local personal worker could not prepare the required browser/CDP capability.");
  }
  throw new Error(`The local personal worker is missing required capabilities: ${missing.join(", ")}.`);
}

function discoverPersonalWorkerEngineRuntime(): PersonalWorkerEngineRuntime {
  const executableSearchPath = buildAgentExecutableSearchPath({
    env: process.env,
    homeDir: homedir(),
    platform: process.platform,
  });
  return {
    capabilities: discoverAgentEngineCapabilities({
      env: process.env,
      homeDir: homedir(),
      platform: process.platform,
    }),
    executableSearchPath,
  };
}

function assertRequestedAgentExecutables(
  engineRuntime: PersonalWorkerEngineRuntime,
  requiredCapabilities: Record<string, unknown> | undefined,
): void {
  const missing = missingAgentEngineExecutables(engineRuntime.capabilities, requiredCapabilities);
  if (missing.length === 0) return;
  const commands = missing.map(({ capability, command }) => `${capability} (${command})`);
  throw new Error(`The local personal worker could not find required Agent CLI(s) on PATH: ${commands.join(", ")}.`);
}

function resolveBrandIconPath(): string | undefined {
  const candidates = [
    join(process.cwd(), BRAND_ICON_PATH),
    join(app.getAppPath(), BRAND_ICON_PATH),
    join(__dirname, "..", "..", BRAND_ICON_PATH),
  ];
  return candidates.find((candidate) => existsSync(candidate));
}

function resolveProjectRoot(): string {
  const candidates = [
    process.cwd(),
    app.getAppPath(),
    join(app.getAppPath(), "..", ".."),
  ];
  return candidates.find((candidate) => existsSync(join(candidate, "scripts", "run-server-worker-dev.sh"))) || process.cwd();
}

function resolveBundledWorkerBinary(): string | null {
  const name = process.platform === "win32" ? "mspace-worker.exe" : "mspace-worker";
  const candidates = [
    join(process.resourcesPath, "bin", name),
    join(app.getAppPath(), "bin", name),
    join(app.getAppPath(), "..", "bin", name),
  ];
  return candidates.find((candidate) => existsSync(candidate)) || null;
}

function resolveWorkerDir(): string {
  const cwdCandidate = join(process.cwd(), "worker");
  if (existsSync(cwdCandidate)) return cwdCandidate;
  return join(resolveProjectRoot(), "worker");
}

function resolveChromeExecutable(): string | null {
  const configured = String(process.env.MSPACE_CHROME_EXECUTABLE || "").trim();
  if (configured && existsSync(configured)) return configured;
  const candidates =
    process.platform === "darwin"
      ? [
          "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
          join(homedir(), "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
          "/Applications/Chromium.app/Contents/MacOS/Chromium",
          "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
        ]
      : process.platform === "win32"
        ? [
            join(process.env.ProgramFiles || "C:\\Program Files", "Google", "Chrome", "Application", "chrome.exe"),
            join(process.env["ProgramFiles(x86)"] || "C:\\Program Files (x86)", "Google", "Chrome", "Application", "chrome.exe"),
            join(process.env.LOCALAPPDATA || "", "Google", "Chrome", "Application", "chrome.exe"),
          ]
        : [
            "/usr/bin/google-chrome",
            "/usr/bin/google-chrome-stable",
            "/usr/bin/chromium",
            "/usr/bin/chromium-browser",
          ];
  return candidates.find((candidate) => candidate && existsSync(candidate)) || null;
}

function resolveElectronDistExecutable(moduleRoot: string): string {
  const executable =
    process.platform === "darwin"
      ? join("dist", "Electron.app", "Contents", "MacOS", "Electron")
      : process.platform === "win32"
        ? join("dist", "electron.exe")
        : join("dist", "electron");
  return join(moduleRoot, executable);
}

function resolveElectronExecutable(): string | null {
  const configured = String(process.env.MSPACE_ELECTRON_EXECUTABLE || "").trim();
  if (configured && existsSync(configured)) return configured;
  const candidates = [
    basename(process.execPath).toLowerCase() === (process.platform === "win32" ? "electron.exe" : "electron") ? process.execPath : "",
    resolveElectronDistExecutable(join(app.getAppPath(), "node_modules", "electron")),
    resolveElectronDistExecutable(join(resolveProjectRoot(), "apps", "desktop", "node_modules", "electron")),
  ];
  return candidates.find((candidate) => candidate && existsSync(candidate)) || null;
}

function resolvePersonalWorkerTokenPath(workspaceId: string): string {
  const safeWorkspaceId = workspaceId.replace(/[^a-zA-Z0-9_-]/g, "_");
  return join(app.getPath("userData"), "worker", "tokens", `${safeWorkspaceId || "personal"}.token`);
}

function registerProjectFolderPicker(): void {
  if (projectFolderPickerRegistered) return;
  projectFolderPickerRegistered = true;

  ipcMain.handle("mspace:select-project-folder", async () => {
    const options: OpenDialogOptions = {
      title: "Choose project folder",
      properties: ["openDirectory"],
    };
    const result = mainWindow
      ? await dialog.showOpenDialog(mainWindow, options)
      : await dialog.showOpenDialog(options);

    if (result.canceled) return null;
    return result.filePaths[0] || null;
  });
}

function registerKubeconfigFilePicker(): void {
  if (kubeconfigFilePickerRegistered) return;
  kubeconfigFilePickerRegistered = true;

  ipcMain.handle("mspace:select-kubeconfig-files", async () => {
    const options: OpenDialogOptions = {
      title: "Choose kubeconfig files",
      properties: ["openFile", "multiSelections"],
    };
    const result = mainWindow
      ? await dialog.showOpenDialog(mainWindow, options)
      : await dialog.showOpenDialog(options);

    if (result.canceled) return [];
    return result.filePaths;
  });
}

function stripLineSuffix(path: string): string {
  return path.replace(/:\d+(?::\d+)?$/, "");
}

function registerOpenHandlers(): void {
  if (openHandlersRegistered) return;
  openHandlersRegistered = true;

  ipcMain.handle("mspace:open-external", async (_event, url: string) => {
    if (!/^https?:\/\//i.test(url)) return;
    await shell.openExternal(url);
  });

  ipcMain.handle("mspace:open-path", async (_event, filePath: string) => {
    const trimmed = String(filePath || "").trim();
    if (!trimmed) return "No path provided.";
    const candidate = stripLineSuffix(trimmed);
    const target = existsSync(trimmed) ? trimmed : candidate;
    return shell.openPath(target);
  });
}

function extractInviteToken(value: string): string {
  const raw = String(value || "").trim();
  if (!raw) return "";
  if (raw.startsWith("msi_")) return raw;
  try {
    const parsed = new URL(raw);
    const candidates = [
      parsed.hostname,
      parsed.pathname.split("/").filter(Boolean).at(-1) || "",
      parsed.searchParams.get("token") || "",
    ];
    return candidates.find((candidate) => candidate.startsWith("msi_")) || "";
  } catch {
    return "";
  }
}

function sendInviteTokenToRenderer(token: string): void {
  const normalized = extractInviteToken(token);
  if (!normalized) return;
  pendingInviteToken = String(token || "").trim() || normalized;
  if (!mainWindow) return;
  mainWindow.webContents.send("mspace:invite-token", pendingInviteToken);
  if (mainWindow.isMinimized()) mainWindow.restore();
  mainWindow.focus();
}

function registerInviteHandlers(): void {
  if (inviteHandlersRegistered) return;
  inviteHandlersRegistered = true;

  ipcMain.handle("mspace:get-pending-invite-token", async () => pendingInviteToken);
  ipcMain.handle("mspace:set-pending-invite-token", async (_event, token: string) => {
    pendingInviteToken = extractInviteToken(token);
    return pendingInviteToken;
  });
}

function registerDeepLinkProtocol(): void {
  if (process.defaultApp && process.argv.length >= 2) {
    app.setAsDefaultProtocolClient(DEEP_LINK_PROTOCOL, process.execPath, [process.argv[1]]);
    return;
  }
  app.setAsDefaultProtocolClient(DEEP_LINK_PROTOCOL);
}

function handleDeepLinkArguments(argv: string[]): void {
  for (const arg of argv) {
    if (String(arg).startsWith(`${DEEP_LINK_PROTOCOL}://`)) {
      sendInviteTokenToRenderer(arg);
    }
  }
}

function registerServerConfigHandlers(): void {
  if (serverConfigHandlersRegistered) return;
  serverConfigHandlersRegistered = true;

  ipcMain.handle("mspace:set-server-base-url", async (_event, serverUrl: string) => {
    await stopPersonalWorker();
    const config = setConfiguredServerBaseUrl(String(serverUrl || ""));
    if (config.baseUrl !== "http://127.0.0.1:8787") {
      await stopServer();
    } else {
      await ensureServerStarted();
    }
    return config;
  });

  ipcMain.handle("mspace:reset-server-base-url", async () => {
    await stopPersonalWorker();
    return resetConfiguredServerBaseUrl();
  });
}

function execFileQuiet(command: string, args: string[]): Promise<void> {
  return new Promise((resolve) => {
    execFile(command, args, { timeout: 5_000 }, () => resolve());
  });
}

async function assertDockerAvailable(): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    execFile("docker", ["info", "--format", "{{.ServerVersion}}"], { timeout: 5_000 }, (error) => {
      if (error) {
        reject(new Error("Docker is not available or not running."));
        return;
      }
      resolve();
    });
  });
}

function resolveCodexAuthPath(): string {
  return join(process.env.CODEX_HOME || join(homedir(), ".codex"), "auth.json");
}

function assertCodexAuthAvailable(context = "Codex worker"): void {
  const authPath = resolveCodexAuthPath();
  if (!existsSync(authPath)) {
    throw new Error(`Codex auth was not found at ${authPath}. Run codex login first, or set CODEX_HOME before starting the ${context}.`);
  }
}

function resolvePersonalWorkerBrowserDataDir(): string {
  return join(app.getPath("userData"), "worker", "browser-profile");
}

function resolvePersonalWorkerElectronBrowserDataDir(): string {
  return join(app.getPath("userData"), "worker", "electron-browser-profile");
}

function resolvePersonalWorkerElectronBrowserAppDir(): string {
  return join(app.getPath("userData"), "worker", "electron-browser-host");
}

function findAvailablePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const server = createServer();
    server.once("error", reject);
    server.listen(0, PERSONAL_WORKER_BROWSER_CDP_HOST, () => {
      const address = server.address();
      const port = typeof address === "object" && address ? address.port : 0;
      server.close((error) => {
        if (error) {
          reject(error);
          return;
        }
        if (!port) {
          reject(new Error("Could not allocate a Chrome CDP port."));
          return;
        }
        resolve(port);
      });
    });
  });
}

async function isChromeCdpReachable(cdpUrl: string): Promise<boolean> {
  const baseUrl = String(cdpUrl || "").trim().replace(/\/+$/, "");
  if (!baseUrl) return false;
  try {
    const response = await fetch(`${baseUrl}/json/version`, { signal: AbortSignal.timeout(1_500) });
    if (!response.ok) return false;
    const payload = (await response.json()) as { webSocketDebuggerUrl?: string };
    return Boolean(String(payload.webSocketDebuggerUrl || "").trim());
  } catch {
    return false;
  }
}

async function waitForChromeCdp(cdpUrl: string): Promise<boolean> {
  const deadline = Date.now() + PERSONAL_WORKER_BROWSER_START_TIMEOUT_MS;
  while (Date.now() < deadline) {
    if (await isChromeCdpReachable(cdpUrl)) return true;
    await sleep(250);
  }
  return false;
}

function clearPersonalWorkerBrowserRuntime(browserProcess?: ChildProcess): boolean {
  if (browserProcess && personalWorkerBrowserProcess !== browserProcess) return false;
  personalWorkerBrowserProcess = null;
  personalWorkerBrowserCdpUrl = "";
  personalWorkerBrowserSource = "";
  return true;
}

function stopPersonalWorkerBrowserRuntime(browserProcess?: ChildProcess): void {
  const target = browserProcess || personalWorkerBrowserProcess;
  if (!clearPersonalWorkerBrowserRuntime(browserProcess)) return;
  if (target && !target.killed) {
    target.kill("SIGTERM");
  }
}

function refreshPersonalWorkerAfterBrowserLoss(reason: string): void {
  if (personalWorkerStopping || !personalWorkerProcess) return;
  console.warn(`[personal-worker] Chrome CDP ${reason}; restarting worker so browser capability is refreshed.`);
  personalWorkerProcess.kill("SIGTERM");
}

function setPersonalWorkerBrowserCapability(
  capabilities: PersonalWorkerCapabilities,
  labels: Record<string, string>,
  env: Record<string, string>,
  cdpUrl: string,
  source: string,
): void {
  capabilities.browser = true;
  capabilities.chrome_cdp = true;
  labels.browser = "chrome-cdp";
  labels.browserSource = source;
  env.MSPACE_CHROME_CDP_URL = cdpUrl;
}

function attachPersonalWorkerBrowserProcessHandlers(browserProcess: ChildProcess, source: string): void {
  browserProcess.once("exit", () => {
    const workerUsesBrowser = Boolean(
      personalWorkerRuntime?.capabilities.chrome_cdp &&
      personalWorkerRuntime.env.MSPACE_CHROME_CDP_URL === personalWorkerBrowserCdpUrl
    );
    if (clearPersonalWorkerBrowserRuntime(browserProcess)) {
      if (workerUsesBrowser) refreshPersonalWorkerAfterBrowserLoss(`${source} exited`);
    }
  });
  browserProcess.once("error", (error) => {
    console.warn(`[personal-worker] Chrome CDP ${source} process failed: ${error instanceof Error ? error.message : String(error)}`);
    const workerUsesBrowser = Boolean(
      personalWorkerRuntime?.capabilities.chrome_cdp &&
      personalWorkerRuntime.env.MSPACE_CHROME_CDP_URL === personalWorkerBrowserCdpUrl
    );
    if (clearPersonalWorkerBrowserRuntime(browserProcess)) {
      if (workerUsesBrowser) refreshPersonalWorkerAfterBrowserLoss(`${source} failed`);
    }
  });
}

async function writePersonalWorkerElectronBrowserHostApp(): Promise<string> {
  const appDir = resolvePersonalWorkerElectronBrowserAppDir();
  await mkdir(appDir, { recursive: true });
  await writeFile(join(appDir, "package.json"), `${JSON.stringify({ main: "main.cjs" })}\n`);
  await writeFile(join(appDir, "main.cjs"), `const { app, BrowserWindow } = require("electron");

let windowRef;

app.whenReady().then(() => {
  windowRef = new BrowserWindow({
    show: false,
    width: 1280,
    height: 900,
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
    },
  });
  windowRef.loadURL("about:blank").catch(() => {});
});
`);
  return appDir;
}

async function startElectronPersonalWorkerBrowserRuntime(
  capabilities: PersonalWorkerCapabilities,
  labels: Record<string, string>,
  env: Record<string, string>,
): Promise<boolean> {
  const electronPath = resolveElectronExecutable();
  if (!electronPath) {
    console.warn("[personal-worker] Electron executable was not found; local worker will start without browser/CDP capability.");
    return false;
  }
  const port = await findAvailablePort();
  const cdpUrl = `http://${PERSONAL_WORKER_BROWSER_CDP_HOST}:${port}`;
  const appDir = await writePersonalWorkerElectronBrowserHostApp();
  await mkdir(resolvePersonalWorkerElectronBrowserDataDir(), { recursive: true });
  const browserProcess = spawn(electronPath, [
    `--remote-debugging-address=${PERSONAL_WORKER_BROWSER_CDP_HOST}`,
    `--remote-debugging-port=${port}`,
    `--user-data-dir=${resolvePersonalWorkerElectronBrowserDataDir()}`,
    appDir,
  ], {
    detached: false,
    stdio: "ignore",
  });
  personalWorkerBrowserProcess = browserProcess;
  personalWorkerBrowserCdpUrl = cdpUrl;
  personalWorkerBrowserSource = "electron-managed";
  attachPersonalWorkerBrowserProcessHandlers(browserProcess, "electron-managed");

  if (await waitForChromeCdp(cdpUrl)) {
    setPersonalWorkerBrowserCapability(capabilities, labels, env, cdpUrl, "electron-managed");
    return true;
  }
  console.warn(`[personal-worker] Electron CDP did not become ready at ${cdpUrl}; local worker will start without browser/CDP capability.`);
  stopPersonalWorkerBrowserRuntime(browserProcess);
  return false;
}

async function preparePersonalWorkerRuntime(
  requiredCapabilities?: Record<string, unknown>,
  engineRuntime = discoverPersonalWorkerEngineRuntime(),
): Promise<PersonalWorkerRuntime> {
  const capabilities: PersonalWorkerCapabilities = {
    protocolSmoke: true,
    ...engineRuntime.capabilities,
    dryRun: false,
  };
  const labels: Record<string, string> = {
    provider: "desktop-local",
    environment: "host",
  };
  const env: Record<string, string> = engineRuntime.executableSearchPath ? { PATH: engineRuntime.executableSearchPath } : {};

  if (!personalWorkerRequiresBrowser(requiredCapabilities)) {
    return { capabilities, labels, env };
  }

  const configuredCdpUrl = String(process.env.MSPACE_CHROME_CDP_URL || "").trim().replace(/\/+$/, "");
  if (configuredCdpUrl) {
    if (await isChromeCdpReachable(configuredCdpUrl)) {
      setPersonalWorkerBrowserCapability(capabilities, labels, env, configuredCdpUrl, "configured");
      return { capabilities, labels, env };
    } else {
      console.warn(`[personal-worker] configured Chrome CDP URL is not reachable: ${configuredCdpUrl}; trying managed CDP fallback.`);
    }
  }

  if (personalWorkerBrowserProcess && personalWorkerBrowserCdpUrl && await isChromeCdpReachable(personalWorkerBrowserCdpUrl)) {
    setPersonalWorkerBrowserCapability(capabilities, labels, env, personalWorkerBrowserCdpUrl, personalWorkerBrowserSource || "desktop-managed");
    return { capabilities, labels, env };
  }

  if (personalWorkerBrowserProcess || personalWorkerBrowserCdpUrl) {
    stopPersonalWorkerBrowserRuntime();
  }

  const chromePath = resolveChromeExecutable();
  if (chromePath) {
    const port = await findAvailablePort();
    const cdpUrl = `http://${PERSONAL_WORKER_BROWSER_CDP_HOST}:${port}`;
    await mkdir(resolvePersonalWorkerBrowserDataDir(), { recursive: true });
    const browserProcess = spawn(chromePath, [
      `--remote-debugging-address=${PERSONAL_WORKER_BROWSER_CDP_HOST}`,
      `--remote-debugging-port=${port}`,
      `--user-data-dir=${resolvePersonalWorkerBrowserDataDir()}`,
      "--no-first-run",
      "--no-default-browser-check",
      "--disable-background-networking",
      "about:blank",
    ], {
      detached: false,
      stdio: "ignore",
    });
    personalWorkerBrowserProcess = browserProcess;
    personalWorkerBrowserCdpUrl = cdpUrl;
    personalWorkerBrowserSource = "desktop-managed";
    attachPersonalWorkerBrowserProcessHandlers(browserProcess, "desktop-managed");

    if (await waitForChromeCdp(cdpUrl)) {
      setPersonalWorkerBrowserCapability(capabilities, labels, env, cdpUrl, "desktop-managed");
      return { capabilities, labels, env };
    }
    console.warn(`[personal-worker] Chrome CDP did not become ready at ${cdpUrl}; trying Electron CDP fallback.`);
    stopPersonalWorkerBrowserRuntime(browserProcess);
  } else {
    console.warn("[personal-worker] Chrome executable was not found; trying Electron CDP fallback.");
  }
  await startElectronPersonalWorkerBrowserRuntime(capabilities, labels, env);
  return { capabilities, labels, env };
}

async function createWorkerBootstrapCredential(input: StartDockerWorkerInput & { name?: string; expiresInHours?: number; credentialServerUrl?: string }): Promise<RuntimeRegistrationTokenResult> {
  const authToken = String(input?.authToken || "").trim();
  const workspaceId = String(input?.workspaceId || "").trim();
  if (!authToken || !workspaceId) {
    throw new Error("A signed-in workspace is required to start a worker.");
  }
  const name = String(input?.name || "Local Docker worker").trim() || "Local Docker worker";
  const expiresInHours = Number.isFinite(input?.expiresInHours) && Number(input.expiresInHours) > 0
    ? Math.floor(Number(input.expiresInHours))
    : 12;
  const credentialServerUrl = String(input?.credentialServerUrl || getServerBaseUrl()).trim().replace(/\/+$/, "");

  const response = await fetch(`${credentialServerUrl}/api/workspaces/${encodeURIComponent(workspaceId)}/runtime-registration-tokens`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${authToken}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      name,
      expiresInHours,
    }),
  });
  if (!response.ok) {
    const message = await response.text();
    throw new Error(message || `Create worker credential failed with status ${response.status}.`);
  }

  const result = (await response.json()) as RuntimeRegistrationTokenResult;
  const token = String(result.token || "").trim();
  if (!token.startsWith("msw_")) {
    throw new Error("Server did not return a worker bootstrap credential.");
  }
  return { ...result, token };
}

function clearPersonalWorkerRestartTimer(): void {
  if (personalWorkerRestartTimer) {
    clearTimeout(personalWorkerRestartTimer);
    personalWorkerRestartTimer = null;
  }
}

function clearPersonalWorkerCredentialTimer(): void {
  if (personalWorkerCredentialTimer) {
    clearTimeout(personalWorkerCredentialTimer);
    personalWorkerCredentialTimer = null;
  }
}

function clearPersonalWorkerOldCredentialRevokeTimers(): void {
  for (const timer of personalWorkerOldCredentialRevokeTimers) {
    clearTimeout(timer);
  }
  personalWorkerOldCredentialRevokeTimers.clear();
}

async function writePersonalWorkerTokenFile(workspaceId: string, token: string): Promise<string> {
  const tokenPath = resolvePersonalWorkerTokenPath(workspaceId);
  await mkdir(dirname(tokenPath), { recursive: true });
  const tempPath = `${tokenPath}.${Date.now()}.tmp`;
  await writeFile(tempPath, `${token}\n`, { mode: 0o600 });
  await rename(tempPath, tokenPath);
  return tokenPath;
}

async function revokeRuntimeRegistrationToken(input: EnsurePersonalWorkerInput, tokenId: string): Promise<void> {
  const authToken = String(input?.authToken || "").trim();
  const workspaceId = String(input?.workspaceId || "").trim();
  if (!authToken || !workspaceId || !tokenId) return;
  const credentialServerUrl = String(input?.credentialServerUrl || input?.serverUrl || getServerBaseUrl()).trim().replace(/\/+$/, "");
  const response = await fetch(`${credentialServerUrl}/api/workspaces/${encodeURIComponent(workspaceId)}/runtime-registration-tokens/${encodeURIComponent(tokenId)}`, {
    method: "DELETE",
    headers: {
      Authorization: `Bearer ${authToken}`,
    },
  });
  if (!response.ok && response.status !== 404) {
    const message = await response.text();
    throw new Error(message || `Revoke worker credential failed with status ${response.status}.`);
  }
}

function schedulePersonalWorkerCredentialRenewal(input: EnsurePersonalWorkerInput, credential: RuntimeRegistrationTokenResult): void {
  clearPersonalWorkerCredentialTimer();
  const expiresAt = Date.parse(String(credential.registrationToken?.expiresAt || ""));
  const delay = Number.isFinite(expiresAt)
    ? Math.max(5_000, expiresAt - Date.now() - PERSONAL_WORKER_TOKEN_RENEWAL_BUFFER_MS)
    : Math.max(5_000, (PERSONAL_WORKER_TOKEN_HOURS * 60 * 60 * 1000) - PERSONAL_WORKER_TOKEN_RENEWAL_BUFFER_MS);
  schedulePersonalWorkerCredentialTimer(input, delay);
}

function schedulePersonalWorkerCredentialRetry(input: EnsurePersonalWorkerInput): void {
  schedulePersonalWorkerCredentialTimer(input, PERSONAL_WORKER_TOKEN_RETRY_MS);
}

function schedulePersonalWorkerCredentialTimer(input: EnsurePersonalWorkerInput, delay: number): void {
  clearPersonalWorkerCredentialTimer();
  personalWorkerCredentialTimer = setTimeout(() => {
    void renewPersonalWorkerCredential(input).catch((error) => {
      console.warn(`[personal-worker] credential renewal failed: ${error instanceof Error ? error.message : String(error)}`);
      if (!personalWorkerStopping && personalWorkerWorkspaceId === String(input.workspaceId || "").trim()) {
        schedulePersonalWorkerCredentialRetry(input);
      }
    });
  }, Math.max(5_000, delay));
}

async function renewPersonalWorkerCredential(input: EnsurePersonalWorkerInput): Promise<void> {
  const workspaceId = String(input.workspaceId || "").trim();
  if (!workspaceId || personalWorkerWorkspaceId !== workspaceId) return;
  const previousCredential = personalWorkerCredential;
  const nextCredential = await createWorkerBootstrapCredential({
    authToken: input.authToken,
    workspaceId,
    name: "Desktop personal worker credential",
    expiresInHours: PERSONAL_WORKER_TOKEN_HOURS,
    credentialServerUrl: input.credentialServerUrl || input.serverUrl,
  });
  await writePersonalWorkerTokenFile(workspaceId, String(nextCredential.token || ""));
  personalWorkerCredential = nextCredential;
  schedulePersonalWorkerCredentialRenewal(input, nextCredential);
  const previousTokenId = previousCredential?.registrationToken?.id;
  if (previousTokenId && previousTokenId !== nextCredential.registrationToken?.id) {
    const timer = setTimeout(() => {
      personalWorkerOldCredentialRevokeTimers.delete(timer);
      void revokeRuntimeRegistrationToken(input, previousTokenId).catch((error) => {
        console.warn(`[personal-worker] old credential revoke failed: ${error instanceof Error ? error.message : String(error)}`);
      });
    }, PERSONAL_WORKER_OLD_TOKEN_REVOKE_DELAY_MS);
    personalWorkerOldCredentialRevokeTimers.add(timer);
  }
}

async function stopPersonalWorker(): Promise<void> {
  personalWorkerStopping = true;
  clearPersonalWorkerRestartTimer();
  clearPersonalWorkerCredentialTimer();
  clearPersonalWorkerOldCredentialRevokeTimers();
  const workerProcesses = new Set(personalWorkerCompanionProcesses);
  if (personalWorkerProcess) workerProcesses.add(personalWorkerProcess);
  personalWorkerProcess = null;
  personalWorkerCompanionProcesses.clear();
  for (const workerProcess of workerProcesses) {
    if (!workerProcess.killed) workerProcess.kill("SIGTERM");
  }
  personalWorkerRuntime = null;
  stopPersonalWorkerBrowserRuntime();
  const workspaceId = personalWorkerWorkspaceId;
  const credential = personalWorkerCredential;
  const credentialInput = personalWorkerCredentialInput;
  if (workspaceId) {
    await rm(resolvePersonalWorkerTokenPath(workspaceId), { force: true }).catch(() => undefined);
  }
  const tokenId = credential?.registrationToken?.id;
  if (tokenId && credentialInput) {
    await revokeRuntimeRegistrationToken(credentialInput, tokenId).catch((error) => {
      console.warn(`[personal-worker] credential revoke failed: ${error instanceof Error ? error.message : String(error)}`);
    });
  }
  personalWorkerWorkspaceId = "";
  personalWorkerRestartAttempts = 0;
  personalWorkerCredential = null;
  personalWorkerCredentialInput = null;
}

function startPersonalWorkerProcess(input: EnsurePersonalWorkerInput, tokenFile: string, workerName: string, runtime: PersonalWorkerRuntime): void {
  const serverUrl = String(input.serverUrl || getServerBaseUrl()).trim();
  const bundled = resolveBundledWorkerBinary();
  const command = bundled || "go";
  const args = bundled ? [] : ["run", "."];
  const cwd = bundled ? undefined : resolveWorkerDir();

  personalWorkerStopping = false;
  const workerProcess = spawn(command, args, {
    cwd,
    env: {
      ...process.env,
      ...runtime.env,
      MSPACE_RUNTIME_TOKEN_FILE: tokenFile,
      MSPACE_SERVER_URL: serverUrl,
      MSPACE_WORKER_MODE: "personal",
      MSPACE_WORKER_NAME: workerName,
      MSPACE_WORKER_CAPABILITIES: JSON.stringify(runtime.capabilities),
      MSPACE_WORKER_LABELS: JSON.stringify(runtime.labels),
      MSPACE_WORKER_WORK_ROOT: personalWorkerWorkRoot(join(app.getPath("userData"), "worker"), runtime.capabilities),
    },
    stdio: "pipe",
  });
  personalWorkerProcess = workerProcess;
  personalWorkerRuntime = runtime;

  workerProcess.stdout.on("data", (chunk) => {
    process.stdout.write(`[personal-worker] ${chunk}`);
  });
  workerProcess.stderr.on("data", (chunk) => {
    process.stderr.write(`[personal-worker] ${chunk}`);
  });
  workerProcess.on("exit", () => {
    personalWorkerCompanionProcesses.delete(workerProcess);
    if (personalWorkerProcess === workerProcess) {
      personalWorkerProcess = null;
      personalWorkerRuntime = null;
    } else {
      return;
    }
    if (personalWorkerStopping || personalWorkerWorkspaceId !== String(input.workspaceId || "").trim()) return;
    const delay = Math.min(30_000, 1_000 * 2 ** personalWorkerRestartAttempts);
    personalWorkerRestartAttempts += 1;
    personalWorkerRestartTimer = setTimeout(() => {
      void ensurePersonalWorker(input).catch((error) => {
        console.warn(`[personal-worker] restart failed: ${error instanceof Error ? error.message : String(error)}`);
      });
    }, delay);
  });
}

async function ensurePersonalWorkerUnlocked(input: EnsurePersonalWorkerInput): Promise<EnsurePersonalWorkerResult> {
  if (process.env.MSPACE_AUTO_PERSONAL_WORKER === "0") {
    return { ok: false, status: "disabled", workerName: "" };
  }
  const workspaceId = String(input.workspaceId || "").trim();
  if (!workspaceId) {
    throw new Error("A signed-in personal workspace is required to start a local worker.");
  }
  const serverUrl = String(input.serverUrl || getServerBaseUrl()).trim().replace(/\/+$/, "");
  const credentialInput: EnsurePersonalWorkerInput = {
    ...input,
    workspaceId,
    serverUrl,
    credentialServerUrl: String(input.credentialServerUrl || serverUrl).trim().replace(/\/+$/, ""),
  };
  const engineRuntime = discoverPersonalWorkerEngineRuntime();
  assertRequestedAgentExecutables(engineRuntime, credentialInput.requiredCapabilities);
  if (personalWorkerRequiresCodexAuth(credentialInput.requiredCapabilities)) {
    assertCodexAuthAvailable("local personal worker");
  }
  if (
    personalWorkerProcess &&
    personalWorkerWorkspaceId === workspaceId &&
    runtimeHasRequiredCapabilities(personalWorkerRuntime, credentialInput.requiredCapabilities)
  ) {
    personalWorkerCredentialInput = credentialInput;
    if (personalWorkerCredential) {
      schedulePersonalWorkerCredentialRenewal(credentialInput, personalWorkerCredential);
    }
    return {
      ok: true,
      status: "running",
      workerName: personalWorkerName(workspaceId, credentialInput.requiredCapabilities),
      capabilities: personalWorkerRuntime?.capabilities,
    };
  }
  if (
    !personalWorkerProcess &&
    personalWorkerWorkspaceId === workspaceId &&
    personalWorkerCompanionProcesses.size > 0 &&
    !personalWorkerRequiresBrowser(credentialInput.requiredCapabilities)
  ) {
    return { ok: true, status: "running", workerName: personalWorkerName(workspaceId) };
  }
  const workerName = personalWorkerName(workspaceId, credentialInput.requiredCapabilities);
  const upgradingCurrentWorker = Boolean(personalWorkerProcess && personalWorkerWorkspaceId === workspaceId);
  const previousWorkerProcess = upgradingCurrentWorker ? personalWorkerProcess : null;
  const previousWorkerRuntime = upgradingCurrentWorker ? personalWorkerRuntime : null;
  let runtime: PersonalWorkerRuntime;
  let token: RuntimeRegistrationTokenResult | null = null;
  let tokenPath = "";
  if (upgradingCurrentWorker) {
    runtime = await preparePersonalWorkerRuntime(credentialInput.requiredCapabilities, engineRuntime);
    assertPersonalWorkerCapabilities(runtime, credentialInput.requiredCapabilities);
    const browserCdpUrl = String(runtime.env.MSPACE_CHROME_CDP_URL || "").trim();
    if (personalWorkerRequiresBrowser(credentialInput.requiredCapabilities) && !(await isChromeCdpReachable(browserCdpUrl))) {
      stopPersonalWorkerBrowserRuntime();
      throw new Error("The local personal worker could not keep the required browser/CDP capability ready.");
    }
    token = personalWorkerCredential;
    tokenPath = resolvePersonalWorkerTokenPath(workspaceId);
  } else {
    if (personalWorkerProcess || (personalWorkerWorkspaceId && personalWorkerWorkspaceId !== workspaceId)) {
      await stopPersonalWorker();
    }
    runtime = await preparePersonalWorkerRuntime(credentialInput.requiredCapabilities, engineRuntime);
    if (personalWorkerWorkspaceId === workspaceId && personalWorkerCredential && existsSync(resolvePersonalWorkerTokenPath(workspaceId))) {
      token = personalWorkerCredential;
      tokenPath = resolvePersonalWorkerTokenPath(workspaceId);
    }
  }
  assertPersonalWorkerCapabilities(runtime, credentialInput.requiredCapabilities);
  personalWorkerStopping = false;
  personalWorkerWorkspaceId = workspaceId;
  personalWorkerCredentialInput = credentialInput;
  if (!token) {
    token = await createWorkerBootstrapCredential({
      authToken: credentialInput.authToken,
      workspaceId,
      name: "Desktop personal worker credential",
      expiresInHours: PERSONAL_WORKER_TOKEN_HOURS,
      credentialServerUrl: credentialInput.credentialServerUrl,
    });
    tokenPath = await writePersonalWorkerTokenFile(workspaceId, String(token.token || ""));
  }
  personalWorkerCredential = token;
  if (previousWorkerProcess) personalWorkerCompanionProcesses.add(previousWorkerProcess);
  try {
    startPersonalWorkerProcess(credentialInput, tokenPath, workerName, runtime);
  } catch (error) {
    if (previousWorkerProcess) {
      personalWorkerCompanionProcesses.delete(previousWorkerProcess);
      personalWorkerProcess = previousWorkerProcess;
      personalWorkerRuntime = previousWorkerRuntime;
    }
    stopPersonalWorkerBrowserRuntime();
    throw error;
  }
  schedulePersonalWorkerCredentialRenewal(credentialInput, token);
  return { ok: true, status: "starting", workerName, capabilities: runtime.capabilities };
}

async function ensurePersonalWorker(input: EnsurePersonalWorkerInput): Promise<EnsurePersonalWorkerResult> {
  while (personalWorkerEnsureInFlight) {
    await personalWorkerEnsureInFlight.catch(() => undefined);
  }
  const run = ensurePersonalWorkerUnlocked(input);
  personalWorkerEnsureInFlight = run;
  try {
    return await run;
  } finally {
    if (personalWorkerEnsureInFlight === run) {
      personalWorkerEnsureInFlight = null;
    }
  }
}

function registerPersonalWorkerHandlers(): void {
  if (personalWorkerHandlersRegistered) return;
  personalWorkerHandlersRegistered = true;

  ipcMain.handle("mspace:ensure-personal-worker", async (_event, input: EnsurePersonalWorkerInput): Promise<EnsurePersonalWorkerResult> => {
    return ensurePersonalWorker(input || {});
  });

  ipcMain.handle("mspace:stop-personal-worker", async () => {
    await stopPersonalWorker();
    return { ok: true, status: "stopped" };
  });
}

async function stopDockerWorker(): Promise<void> {
  if (dockerWorkerProcess) {
    dockerWorkerProcess.kill("SIGTERM");
    dockerWorkerProcess = null;
  }
  if (dockerWorkerContainer) {
    await execFileQuiet("docker", ["rm", "-f", dockerWorkerContainer]);
  }
}

function registerDockerWorkerHandlers(): void {
  if (dockerWorkerHandlersRegistered) return;
  dockerWorkerHandlersRegistered = true;

  ipcMain.handle("mspace:start-docker-worker", async (_event, input: StartDockerWorkerInput): Promise<StartDockerWorkerResult> => {
    await assertDockerAvailable();
    const codexWorker = input?.codex !== false;
    if (codexWorker) {
      assertCodexAuthAvailable();
    }
    const credential = await createWorkerBootstrapCredential(input || {});
    const token = String(credential.token || "");

    const root = resolveProjectRoot();
    const script = codexWorker ? "scripts/run-server-worker-codex-dev.sh" : "scripts/run-server-worker-dev.sh";
    const scriptPath = join(root, script);
    if (!existsSync(scriptPath)) {
      throw new Error(`Docker worker script was not found at ${scriptPath}.`);
    }

    const mode = input?.mode === "personal" ? "personal" : "team";
    const containerName = String(input?.containerName || (codexWorker ? "mspace-worker-codex-dev" : "mspace-worker-dev")).trim();
    const serverUrl = String(input?.serverUrl || "http://host.docker.internal:8787").trim();
    const workerName = String(input?.workerName || `local-docker-${mode}-worker`).trim();

    dockerWorkerContainer = containerName;
    await stopDockerWorker();
    dockerWorkerContainer = containerName;

    dockerWorkerProcess = spawn("bash", [scriptPath], {
      cwd: root,
      env: {
        ...process.env,
        MSPACE_RUNTIME_TOKEN: token,
        MSPACE_WORKER_MODE: mode,
        MSPACE_WORKER_NAME: workerName,
        MSPACE_WORKER_CONTAINER: containerName,
        MSPACE_WORKER_DOCKER_TTY: "0",
        MSPACE_SERVER_URL: serverUrl,
      },
      stdio: "pipe",
    });

    dockerWorkerProcess.stdout.on("data", (chunk) => {
      process.stdout.write(`[docker-worker] ${chunk}`);
    });
    dockerWorkerProcess.stderr.on("data", (chunk) => {
      process.stderr.write(`[docker-worker] ${chunk}`);
    });
    dockerWorkerProcess.on("exit", () => {
      dockerWorkerProcess = null;
    });

    return {
      ok: true,
      status: "starting",
      containerName,
      script,
    };
  });
}

function createWindow(iconPath = resolveBrandIconPath()): void {
  const serverConfig = getServerConfig();
  const options: BrowserWindowConstructorOptions = {
    width: 1380,
    height: 900,
    minWidth: 1024,
    minHeight: 720,
    titleBarStyle: "hiddenInset",
    autoHideMenuBar: true,
    ...(iconPath ? { icon: iconPath } : {}),
    webPreferences: {
      preload: join(__dirname, "../preload/index.mjs"),
      additionalArguments: [
        `--mspace-server-url=${serverConfig.baseUrl}`,
        `--mspace-server-url-source=${serverConfig.source}`,
        `--mspace-server-url-locked=${serverConfig.locked ? "1" : "0"}`,
      ],
      sandbox: false,
    },
  };

  mainWindow = new BrowserWindow(options);
  mainWindow.webContents.once("did-finish-load", () => {
    if (pendingInviteToken) {
      mainWindow?.webContents.send("mspace:invite-token", pendingInviteToken);
    }
  });

  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    void shell.openExternal(url);
    return { action: "deny" };
  });

  if (!app.isPackaged && process.env.ELECTRON_RENDERER_URL) {
    void mainWindow.loadURL(process.env.ELECTRON_RENDERER_URL);
  } else {
    void mainWindow.loadFile(join(__dirname, "../renderer/index.html"));
  }
}

const gotSingleInstanceLock = app.requestSingleInstanceLock();
if (!gotSingleInstanceLock) {
  app.quit();
} else {
  app.on("second-instance", (_event, argv) => {
    handleDeepLinkArguments(argv);
    if (!mainWindow) return;
    if (mainWindow.isMinimized()) mainWindow.restore();
    mainWindow.focus();
  });

  app.on("open-url", (event, url) => {
    event.preventDefault();
    sendInviteTokenToRenderer(url);
  });

  registerDeepLinkProtocol();
  handleDeepLinkArguments(process.argv);

  app.whenReady().then(async () => {
    registerServerConfigHandlers();
    registerProjectFolderPicker();
    registerKubeconfigFilePicker();
    registerOpenHandlers();
    registerInviteHandlers();
    registerDockerWorkerHandlers();
    registerPersonalWorkerHandlers();
    try {
      await ensureServerStarted();
    } catch (error) {
      console.error("[server] failed to start", error);
      await stopPersonalWorker();
      await stopDockerWorker();
      await stopServer();
      app.exit(1);
      return;
    }
    const brandIconPath = resolveBrandIconPath();
    if (brandIconPath) app.dock?.setIcon(brandIconPath);
    createWindow(brandIconPath);

    app.on("activate", () => {
      if (BrowserWindow.getAllWindows().length === 0) createWindow(resolveBrandIconPath());
    });
  });

  app.on("window-all-closed", async () => {
    if (process.platform !== "darwin") {
      await stopPersonalWorker();
      await stopDockerWorker();
      await stopServer();
      app.quit();
    }
  });

  app.on("before-quit", async () => {
    await stopPersonalWorker();
    await stopDockerWorker();
    await stopServer();
  });
}
