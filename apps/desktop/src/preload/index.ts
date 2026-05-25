import { contextBridge, ipcRenderer } from "electron";

function readArgument(name: string): string | undefined {
  const prefix = `--${name}=`;
  return process.argv.find((arg) => arg.startsWith(prefix))?.slice(prefix.length);
}

const serverBaseUrl =
  readArgument("mspace-server-url") ||
  process.env.MSPACE_SERVER_URL ||
  "http://127.0.0.1:8787";
const serverBaseUrlSource =
  (readArgument("mspace-server-url-source") as "environment" | "user" | "default" | undefined) ||
  (process.env.MSPACE_SERVER_URL ? "environment" : "default");
const serverBaseUrlLocked = readArgument("mspace-server-url-locked") === "1";

const desktopAPI = {
  serverBaseUrl,
  serverBaseUrlSource,
  serverBaseUrlLocked,
  appVersion: process.env.npm_package_version || "0.1.0",
  setServerBaseUrl: (serverUrl: string) =>
    ipcRenderer.invoke("mspace:set-server-base-url", serverUrl) as Promise<{
      baseUrl: string;
      source: "environment" | "user" | "default";
      locked: boolean;
    }>,
  resetServerBaseUrl: () =>
    ipcRenderer.invoke("mspace:reset-server-base-url") as Promise<{
      baseUrl: string;
      source: "environment" | "user" | "default";
      locked: boolean;
    }>,
  selectProjectFolder: () => ipcRenderer.invoke("mspace:select-project-folder") as Promise<string | null>,
  selectKubeconfigFiles: () => ipcRenderer.invoke("mspace:select-kubeconfig-files") as Promise<string[]>,
  openExternal: (url: string) => ipcRenderer.invoke("mspace:open-external", url) as Promise<void>,
  openPath: (path: string) => ipcRenderer.invoke("mspace:open-path", path) as Promise<string>,
  startDockerWorker: (input: {
    authToken: string;
    workspaceId: string;
    mode?: "personal" | "team";
    serverUrl?: string;
    codex?: boolean;
    containerName?: string;
    workerName?: string;
  }) => ipcRenderer.invoke("mspace:start-docker-worker", input) as Promise<{
    ok: boolean;
    status: string;
    containerName: string;
    script: string;
  }>,
};

contextBridge.exposeInMainWorld("mspaceDesktop", desktopAPI);
