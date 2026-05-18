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
import { homedir } from "node:os";
import { join } from "node:path";
import { ensureRunnerStarted, getRunnerBaseUrl, stopRunner } from "./runner-manager";
import { ensureServerStarted, getServerBaseUrl, stopServer } from "./server-manager";

let mainWindow: BrowserWindow | null = null;
let projectFolderPickerRegistered = false;
let kubeconfigFilePickerRegistered = false;
let openHandlersRegistered = false;
let dockerWorkerHandlersRegistered = false;
let dockerWorkerProcess: ChildProcessWithoutNullStreams | null = null;
let dockerWorkerContainer = "";
const BRAND_ICON_PATH = join("assets", "brand", "mspace-icon.png");

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

type RuntimeRegistrationTokenResult = {
  token?: string;
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

async function createWorkerBootstrapCredential(input: StartDockerWorkerInput): Promise<string> {
  const authToken = String(input?.authToken || "").trim();
  const workspaceId = String(input?.workspaceId || "").trim();
  if (!authToken || !workspaceId) {
    throw new Error("A signed-in workspace is required to start a worker.");
  }

  const response = await fetch(`${getServerBaseUrl()}/api/workspaces/${encodeURIComponent(workspaceId)}/runtime-registration-tokens`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${authToken}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      name: "Local Docker worker",
      expiresInHours: 12,
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
  return token;
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
    const token = await createWorkerBootstrapCredential(input || {});

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
        `--mspace-runner-url=${getRunnerBaseUrl()}`,
        `--mspace-server-url=${getServerBaseUrl()}`,
      ],
      sandbox: false,
    },
  };

  mainWindow = new BrowserWindow(options);

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

app.whenReady().then(async () => {
  registerProjectFolderPicker();
  registerKubeconfigFilePicker();
  registerOpenHandlers();
  registerDockerWorkerHandlers();
  await ensureRunnerStarted();
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
    await stopDockerWorker();
    await stopServer();
    await stopRunner();
    app.quit();
  }
});

app.on("before-quit", async () => {
  await stopDockerWorker();
  await stopServer();
  await stopRunner();
});
