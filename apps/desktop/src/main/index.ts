import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  shell,
  type BrowserWindowConstructorOptions,
  type OpenDialogOptions,
} from "electron";
import { execFile, spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdir, rename, rm, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import {
  ensureServerStarted,
  getServerBaseUrl,
  getServerConfig,
  resetConfiguredServerBaseUrl,
  setConfiguredServerBaseUrl,
  stopServer,
} from "./server-manager";

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
let personalWorkerWorkspaceId = "";
let personalWorkerRestartTimer: NodeJS.Timeout | null = null;
let personalWorkerCredentialTimer: NodeJS.Timeout | null = null;
let personalWorkerOldCredentialRevokeTimers = new Set<NodeJS.Timeout>();
let personalWorkerRestartAttempts = 0;
let personalWorkerStopping = false;
let personalWorkerCredential: RuntimeRegistrationTokenResult | null = null;
let personalWorkerCredentialInput: EnsurePersonalWorkerInput | null = null;
const BRAND_ICON_PATH = join("assets", "brand", "mspace-icon.png");
const PERSONAL_WORKER_TOKEN_HOURS = 12;
const PERSONAL_WORKER_TOKEN_RENEWAL_BUFFER_MS = 15 * 60 * 1000;
const PERSONAL_WORKER_TOKEN_RETRY_MS = 60 * 1000;
const PERSONAL_WORKER_OLD_TOKEN_REVOKE_DELAY_MS = 30 * 1000;
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
};

type EnsurePersonalWorkerResult = {
  ok: boolean;
  status: string;
  workerName: string;
};

type RuntimeRegistrationTokenResult = {
  token?: string;
  registrationToken?: {
    id?: string;
    expiresAt?: string;
  };
};

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

function assertCodexAuthAvailable(): void {
  const authPath = resolveCodexAuthPath();
  if (!existsSync(authPath)) {
    throw new Error(`Codex auth was not found at ${authPath}. Run codex login first, or set CODEX_HOME before starting the Docker worker.`);
  }
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
  if (personalWorkerProcess) {
    personalWorkerProcess.kill("SIGTERM");
    personalWorkerProcess = null;
  }
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

function startPersonalWorkerProcess(input: EnsurePersonalWorkerInput, tokenFile: string, workerName: string): void {
  const serverUrl = String(input.serverUrl || getServerBaseUrl()).trim();
  const bundled = resolveBundledWorkerBinary();
  const command = bundled || "go";
  const args = bundled ? [] : ["run", "."];
  const cwd = bundled ? undefined : resolveWorkerDir();

  personalWorkerStopping = false;
  personalWorkerProcess = spawn(command, args, {
    cwd,
    env: {
      ...process.env,
      MSPACE_RUNTIME_TOKEN_FILE: tokenFile,
      MSPACE_SERVER_URL: serverUrl,
      MSPACE_WORKER_MODE: "personal",
      MSPACE_WORKER_NAME: workerName,
      MSPACE_WORKER_CAPABILITIES: '{"protocolSmoke":true,"codex":true,"dryRun":false}',
      MSPACE_WORKER_LABELS: '{"provider":"desktop-local","environment":"host"}',
      MSPACE_WORKER_WORK_ROOT: join(app.getPath("userData"), "worker"),
    },
    stdio: "pipe",
  });

  personalWorkerProcess.stdout.on("data", (chunk) => {
    process.stdout.write(`[personal-worker] ${chunk}`);
  });
  personalWorkerProcess.stderr.on("data", (chunk) => {
    process.stderr.write(`[personal-worker] ${chunk}`);
  });
  personalWorkerProcess.on("exit", () => {
    personalWorkerProcess = null;
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

async function ensurePersonalWorker(input: EnsurePersonalWorkerInput): Promise<EnsurePersonalWorkerResult> {
  if (process.env.MSPACE_AUTO_PERSONAL_WORKER === "0") {
    return { ok: false, status: "disabled", workerName: "" };
  }
  assertCodexAuthAvailable();
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
  if (personalWorkerProcess && personalWorkerWorkspaceId === workspaceId) {
    personalWorkerCredentialInput = credentialInput;
    if (personalWorkerCredential) {
      schedulePersonalWorkerCredentialRenewal(credentialInput, personalWorkerCredential);
    }
    return { ok: true, status: "running", workerName: `desktop-personal-${workspaceId.slice(0, 8)}` };
  }
  await stopPersonalWorker();
  personalWorkerStopping = false;
  personalWorkerWorkspaceId = workspaceId;
  personalWorkerCredentialInput = credentialInput;
  const token = await createWorkerBootstrapCredential({
    authToken: credentialInput.authToken,
    workspaceId,
    name: "Desktop personal worker credential",
    expiresInHours: PERSONAL_WORKER_TOKEN_HOURS,
    credentialServerUrl: credentialInput.credentialServerUrl,
  });
  const tokenPath = await writePersonalWorkerTokenFile(workspaceId, String(token.token || ""));
  personalWorkerCredential = token;
  const workerName = `desktop-personal-${workspaceId.slice(0, 8)}`;
  startPersonalWorkerProcess(credentialInput, tokenPath, workerName);
  schedulePersonalWorkerCredentialRenewal(credentialInput, token);
  return { ok: true, status: "starting", workerName };
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
