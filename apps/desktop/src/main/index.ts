import { app, BrowserWindow, dialog, ipcMain, shell, type OpenDialogOptions } from "electron";
import { join } from "node:path";
import { ensureRunnerStarted, getRunnerBaseUrl, stopRunner } from "./runner-manager";

let mainWindow: BrowserWindow | null = null;
let projectFolderPickerRegistered = false;

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

function createWindow(): void {
  mainWindow = new BrowserWindow({
    width: 1380,
    height: 900,
    minWidth: 1024,
    minHeight: 720,
    titleBarStyle: "hiddenInset",
    autoHideMenuBar: true,
    webPreferences: {
      preload: join(__dirname, "../preload/index.mjs"),
      additionalArguments: [`--mspace-runner-url=${getRunnerBaseUrl()}`],
      sandbox: false,
    },
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

app.whenReady().then(async () => {
  registerProjectFolderPicker();
  await ensureRunnerStarted();
  createWindow();

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
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
