import type { RuntimeWorker, ServerHealth } from "./types";

export const diagnosticCapabilityKeys = [
  "workspaceInboxIssueGrouping",
  "teamWorkspaceCreation",
  "workspaceInvitations",
  "workspaceInvitationPreview",
  "workspaceKinds",
  "workspaceCollaboration",
  "githubAuth",
  "githubApp",
  "passwordAuth",
  "testCaseLibrary",
  "testCaseWorkflow",
  "runtimeWorkerRegistration",
  "runtimeAvailability",
  "runtimeTaskQueue",
] as const;

export const diagnosticWorkerCapabilityKeys = [
  "protocolSmoke",
  "codex",
  "claudeCode",
  "pi",
  "browser",
  "chrome_cdp",
  "dryRun",
] as const;

const MAX_VERSION_LENGTH = 64;
const MAX_WORKER_VERSIONS = 8;
const MAX_DIAGNOSTIC_WORKERS = 1_000;
const SAFE_VERSION_PATTERN = /^(?:dev|unknown|v?(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:-(?:0|[1-9]\d*|[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?)$/;
const TOKEN_PREFIX_PATTERN = /^(?:msp|msw|msi|msh)_/i;
const COMMIT_SHA_PATTERN = /^[0-9a-f]{7,64}$/i;
const RFC3339_PATTERN = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/;

export interface BuildDiagnosticsInput {
  desktopVersion?: unknown;
  serverHealth?: unknown;
  workers?: unknown;
}

function objectRecord(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : undefined;
}

export function normalizeBuildVersion(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  const normalized = value.trim();
  if (
    normalized.length === 0 ||
    normalized.length > MAX_VERSION_LENGTH ||
    TOKEN_PREFIX_PATTERN.test(normalized) ||
    !SAFE_VERSION_PATTERN.test(normalized)
  ) {
    return undefined;
  }
  return normalized;
}

function normalizeCommitSha(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  const normalized = value.trim().toLowerCase();
  return COMMIT_SHA_PATTERN.test(normalized) ? normalized : undefined;
}

function normalizeBuildTime(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  const normalized = value.trim();
  if (!RFC3339_PATTERN.test(normalized)) return undefined;
  const timestamp = Date.parse(normalized);
  return Number.isFinite(timestamp) ? new Date(timestamp).toISOString() : undefined;
}

function normalizeProtocol(value: unknown): number | undefined {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0 && value <= 1_000_000
    ? value
    : undefined;
}

function normalizeWorkers(value: unknown): Array<Record<string, unknown>> {
  if (!Array.isArray(value)) return [];
  return value.flatMap((worker) => {
    const record = objectRecord(worker);
    return record ? [record] : [];
  }).slice(0, MAX_DIAGNOSTIC_WORKERS);
}

function normalizeWorkerVersions(workers: Array<Record<string, unknown>>): string[] {
  const versions = new Set<string>();
  for (const worker of workers) {
    const version = normalizeBuildVersion(worker.version);
    if (version) versions.add(version);
  }
  return [...versions].sort().slice(0, MAX_WORKER_VERSIONS);
}

export function formatBuildDiagnostics(input: BuildDiagnosticsInput): string {
  const server = objectRecord(input.serverHealth);
  const capabilities = objectRecord(server?.capabilities);
  const lines = [
    "mspace diagnostics",
    `desktop.version=${normalizeBuildVersion(input.desktopVersion) || "unknown"}`,
    `server.version=${normalizeBuildVersion(server?.version) || "unknown"}`,
    `server.commitSha=${normalizeCommitSha(server?.commitSha) || "unknown"}`,
    `server.buildTime=${normalizeBuildTime(server?.buildTime) || "unknown"}`,
    `server.protocol=${normalizeProtocol(server?.serverProtocol) ?? "unknown"}`,
  ];
  for (const key of diagnosticCapabilityKeys) {
    const enabled = capabilities?.[key];
    lines.push(`server.capability.${key}=${typeof enabled === "boolean" ? enabled : "unknown"}`);
  }
  const workers = normalizeWorkers(input.workers);
  const workerVersions = normalizeWorkerVersions(workers);
  lines.push(`workers.count=${workers.length}`);
  lines.push(`worker.versions=${workerVersions.length > 0 ? workerVersions.join(",") : "unknown"}`);
  for (const key of diagnosticWorkerCapabilityKeys) {
    const enabledCount = workers.reduce((count, worker) => {
      return objectRecord(worker.capabilities)?.[key] === true ? count + 1 : count;
    }, 0);
    lines.push(`worker.capability.${key}.enabled=${enabledCount}`);
  }
  return lines.join("\n");
}

export function buildInformation(input: {
  desktopVersion?: unknown;
  serverHealth?: ServerHealth;
  workers?: RuntimeWorker[];
}): { desktopVersion: string; serverVersion: string; serverCommitSha: string; serverBuildTime: string; workerVersions: string[] } {
  const workers = normalizeWorkers(input.workers);
  return {
    desktopVersion: normalizeBuildVersion(input.desktopVersion) || "unknown",
    serverVersion: normalizeBuildVersion(input.serverHealth?.version) || "unknown",
    serverCommitSha: normalizeCommitSha(input.serverHealth?.commitSha) || "unknown",
    serverBuildTime: normalizeBuildTime(input.serverHealth?.buildTime) || "unknown",
    workerVersions: normalizeWorkerVersions(workers),
  };
}
