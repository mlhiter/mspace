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

const MAX_VERSION_LENGTH = 64;
const MAX_WORKER_VERSIONS = 8;
const SAFE_VERSION_PATTERN = /^[A-Za-z0-9][A-Za-z0-9.+_-]*$/;
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

function normalizeWorkerVersions(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  const versions = new Set<string>();
  for (const worker of value) {
    const version = normalizeBuildVersion(objectRecord(worker)?.version);
    if (version) versions.add(version);
    if (versions.size >= MAX_WORKER_VERSIONS) break;
  }
  return [...versions].sort();
}

export function formatBuildDiagnostics(input: BuildDiagnosticsInput): string {
  const server = objectRecord(input.serverHealth);
  const capabilities = objectRecord(server?.capabilities);
  const lines = [
    "mspace diagnostics",
    `desktop.version=${normalizeBuildVersion(input.desktopVersion) || "unknown"}`,
    `server.version=${normalizeBuildVersion(server?.version) || "unknown"}`,
  ];
  const commitSha = normalizeCommitSha(server?.commitSha);
  const buildTime = normalizeBuildTime(server?.buildTime);
  const protocol = normalizeProtocol(server?.serverProtocol);
  if (commitSha) lines.push(`server.commitSha=${commitSha}`);
  if (buildTime) lines.push(`server.buildTime=${buildTime}`);
  if (protocol !== undefined) lines.push(`server.protocol=${protocol}`);
  for (const key of diagnosticCapabilityKeys) {
    const enabled = capabilities?.[key];
    if (typeof enabled === "boolean") lines.push(`server.capability.${key}=${enabled}`);
  }
  const workerVersions = normalizeWorkerVersions(input.workers);
  lines.push(`worker.versions=${workerVersions.length > 0 ? workerVersions.join(",") : "unknown"}`);
  return lines.join("\n");
}

export function buildInformation(input: {
  desktopVersion?: unknown;
  serverHealth?: ServerHealth;
  workers?: RuntimeWorker[];
}): { desktopVersion: string; serverVersion: string; workerVersions: string[] } {
  return {
    desktopVersion: normalizeBuildVersion(input.desktopVersion) || "unknown",
    serverVersion: normalizeBuildVersion(input.serverHealth?.version) || "unknown",
    workerVersions: normalizeWorkerVersions(input.workers),
  };
}
