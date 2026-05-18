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
  serverBaseUrl:
    readArgument("mspace-server-url") ||
    process.env.MSPACE_SERVER_URL ||
    "http://127.0.0.1:8787",
  appVersion: process.env.npm_package_version || "0.1.0",
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
