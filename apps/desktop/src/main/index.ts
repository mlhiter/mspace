import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  shell,
  type BrowserWindowConstructorOptions,
  type OpenDialogOptions,
} from "electron";
import { existsSync } from "node:fs";
import { join } from "node:path";
import { ensureRunnerStarted, getRunnerBaseUrl, stopRunner } from "./runner-manager";

let mainWindow: BrowserWindow | null = null;
let projectFolderPickerRegistered = false;
let kubeconfigFilePickerRegistered = false;
let openHandlersRegistered = false;
const BRAND_ICON_PATH = join("assets", "brand", "mspace-icon.png");

function resolveBrandIconPath(): string | undefined {
  const candidates = [
    join(process.cwd(), BRAND_ICON_PATH),
    join(app.getAppPath(), BRAND_ICON_PATH),
    join(__dirname, "..", "..", BRAND_ICON_PATH),
  ];
  return candidates.find((candidate) => existsSync(candidate));
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
      additionalArguments: [`--mspace-runner-url=${getRunnerBaseUrl()}`],
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
  await ensureRunnerStarted();
  const brandIconPath = resolveBrandIconPath();
  if (brandIconPath) app.dock?.setIcon(brandIconPath);
  createWindow(brandIconPath);

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow(resolveBrandIconPath());
  });
});

app.on("window-all-closed", async () => {
  if (process.platform !== "darwin") {
    await stopRunner();
    app.quit();
  }
});

app.on("before-quit", async () => {
  await stopRunner();
});
