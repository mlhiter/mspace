import { join } from "node:path";

export function personalWorkerRequiresBrowser(requiredCapabilities: Record<string, unknown> | undefined): boolean {
  return requiredCapabilities?.browser === true || requiredCapabilities?.chrome_cdp === true;
}

export function personalWorkerName(workspaceId: string, requiredCapabilities?: Record<string, unknown>): string {
  const baseName = `desktop-personal-${workspaceId.slice(0, 8)}`;
  return personalWorkerRequiresBrowser(requiredCapabilities) ? `${baseName}-browser` : baseName;
}

export function personalWorkerWorkRoot(baseRoot: string, requiredCapabilities?: Record<string, unknown>): string {
  return personalWorkerRequiresBrowser(requiredCapabilities) ? join(baseRoot, "browser-companion") : baseRoot;
}
