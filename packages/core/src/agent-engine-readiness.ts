import type {
  AgentEngine,
  AgentEngineDiagnostic,
  AgentEngineDiagnosticStatus,
  RuntimeWorker,
} from "./types";

const engineCapability = {
  codex: "codex",
  claude_code: "claudeCode",
  pi: "pi",
} as const;

const diagnosticStatuses = new Set<AgentEngineDiagnosticStatus>([
  "ready",
  "needs_setup",
  "unverified",
  "missing",
  "probe_error",
]);

export type AgentEngineReadinessStatus = AgentEngineDiagnosticStatus | "not_reported";
export type AgentEngineDiagnosticDisplayState = AgentEngineReadinessStatus | "configured" | "disabled";
export type RuntimeWorkerLiveness = "online" | "draining" | "offline" | "stale";

export interface ResolvedAgentEngineDiagnostic {
  status: AgentEngineReadinessStatus;
  reasonCode: string;
  version: string;
  checkedAt: string;
  reported: boolean;
  legacyCapability: boolean;
}

function isAgentEngineDiagnostic(value: unknown): value is AgentEngineDiagnostic {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  return diagnosticStatuses.has((value as AgentEngineDiagnostic).status);
}

function stringValue(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

export function resolveAgentEngineDiagnostic(
  worker: Pick<RuntimeWorker, "agentEngineDiagnostics" | "capabilities">,
  engine: AgentEngine,
): ResolvedAgentEngineDiagnostic {
  const diagnostic = worker.agentEngineDiagnostics?.[engine];
  if (isAgentEngineDiagnostic(diagnostic)) {
    return {
      status: diagnostic.status,
      reasonCode: stringValue(diagnostic.reasonCode) || diagnostic.status,
      version: stringValue(diagnostic.version),
      checkedAt: stringValue(diagnostic.checkedAt),
      reported: true,
      legacyCapability: false,
    };
  }

  const legacyCapability = worker.capabilities?.[engineCapability[engine]] === true;
  return {
    status: "not_reported",
    reasonCode: legacyCapability ? "legacy_capability" : "not_reported",
    version: "",
    checkedAt: "",
    reported: false,
    legacyCapability,
  };
}

export function runtimeWorkerLiveness(
  worker: Pick<RuntimeWorker, "status" | "lastSeenAt">,
  activeWorkerMaxAgeMs?: number,
  now = Date.now(),
): RuntimeWorkerLiveness {
  const status = worker.status.trim().toLowerCase();
  if (status === "draining") return "draining";
  if (status !== "online") return "offline";

  const lastSeenAt = new Date(worker.lastSeenAt).getTime();
  if (!Number.isFinite(lastSeenAt)) return "stale";
  if (activeWorkerMaxAgeMs && activeWorkerMaxAgeMs > 0 && now - lastSeenAt > activeWorkerMaxAgeMs) return "stale";
  return "online";
}

export function agentEngineDiagnosticDisplayState(
  engine: AgentEngine,
  diagnostic: Pick<ResolvedAgentEngineDiagnostic, "status" | "reasonCode">,
): AgentEngineDiagnosticDisplayState {
  if (diagnostic.status === "unverified" && diagnostic.reasonCode === "disabled_by_configuration") return "disabled";
  if (engine === "pi" && diagnostic.status === "unverified" && diagnostic.reasonCode === "model_available") return "configured";
  return diagnostic.status;
}

export function runtimeWorkerLabel(worker: Pick<RuntimeWorker, "labels">, key: string) {
  return stringValue(worker.labels?.[key]);
}

export function isCurrentHostWorker(
  worker: Pick<RuntimeWorker, "labels">,
  runtimeMode: string,
  currentHostId: string,
) {
  const trustedHostId = stringValue(currentHostId);
  return runtimeMode === "personal"
    && trustedHostId !== ""
    && runtimeWorkerLabel(worker, "provider") === "desktop-local"
    && runtimeWorkerLabel(worker, "hostId") === trustedHostId;
}

export function isCurrentHostPrimaryWorker(
  worker: Pick<RuntimeWorker, "labels">,
  runtimeMode: string,
  currentHostId: string,
) {
  return isCurrentHostWorker(worker, runtimeMode, currentHostId)
    && runtimeWorkerLabel(worker, "runtimeRole") === "primary";
}
