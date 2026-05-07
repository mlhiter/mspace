import { contextBridge, ipcRenderer } from "electron";

function readArgument(name: string): string | undefined {
  const prefix = `--${name}=`;
  return process.argv.find((arg) => arg.startsWith(prefix))?.slice(prefix.length);
}

const desktopAPI = {
  apiBaseUrl:
    readArgument("mspace-runner-url") ||
    process.env.MSPACE_RUNNER_URL ||
    "http://127.0.0.1:7788",
  appVersion: process.env.npm_package_version || "0.1.0",
  selectProjectFolder: () => ipcRenderer.invoke("mspace:select-project-folder") as Promise<string | null>,
};

contextBridge.exposeInMainWorld("mspaceDesktop", desktopAPI);
