import { contextBridge } from "electron";

const desktopAPI = {
  apiBaseUrl: process.env.MSPACE_RUNNER_URL || "http://127.0.0.1:7788",
  appVersion: process.env.npm_package_version || "0.1.0",
};

contextBridge.exposeInMainWorld("mspaceDesktop", desktopAPI);
