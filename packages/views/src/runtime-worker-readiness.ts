import type { QueryClient } from "@tanstack/react-query";
import { controlPlaneApi, getControlPlaneBaseUrl, queryKeys, type RuntimeAvailability, type RuntimeAvailabilityInput } from "@mspace/core";

type EnsurePersonalWorker = NonNullable<NonNullable<Window["mspaceDesktop"]>["ensurePersonalWorker"]>;
type EnsurePersonalWorkerResult = Awaited<ReturnType<EnsurePersonalWorker>>;

export type RuntimeReadyInput = RuntimeAvailabilityInput & {
  token: string;
  workspaceId: string;
  queryClient: QueryClient;
  unavailableMessage: string;
  startingMessage: string;
  formatUnavailableMessage?: (availability: RuntimeAvailability) => string;
  ensurePersonalWorker?: EnsurePersonalWorker;
  onStatus?: (message: string) => void;
  statusMessage?: string;
  maxAttempts?: number;
  pollIntervalMs?: number;
};

function sleep(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

async function fetchRuntimeAvailability(input: RuntimeReadyInput) {
  const availabilityInput: RuntimeAvailabilityInput = {
    runtimeMode: input.runtimeMode,
    requiredCapabilities: input.requiredCapabilities,
  };
  return input.queryClient.fetchQuery({
    queryKey: queryKeys.runtimeAvailability(input.workspaceId, input.token, availabilityInput),
    queryFn: () => controlPlaneApi.getRuntimeAvailability(input.token, input.workspaceId, availabilityInput),
    staleTime: 0,
  });
}

async function ensurePersonalWorkerProcess(input: RuntimeReadyInput) {
  if (input.runtimeMode !== "personal" || !input.ensurePersonalWorker) return undefined;
  input.onStatus?.(input.statusMessage || input.startingMessage);
  return input.ensurePersonalWorker({
    authToken: input.token,
    workspaceId: input.workspaceId,
    serverUrl: getControlPlaneBaseUrl(),
    requiredCapabilities: input.requiredCapabilities,
  });
}

function readinessError(input: RuntimeReadyInput, availability?: RuntimeAvailability) {
  if (availability?.reasonCode === "wrong_runtime_mode") return new Error(input.unavailableMessage);
  if (availability?.reasonCode === "stale_heartbeat" && input.runtimeMode === "personal") return new Error(input.startingMessage);
  if (availability?.canAutoStart && input.runtimeMode === "personal" && input.ensurePersonalWorker) return new Error(input.startingMessage);
  const formatted = availability ? input.formatUnavailableMessage?.(availability) : "";
  if (formatted) return new Error(formatted);
  return new Error(input.unavailableMessage);
}

function availabilityReadyAfterPersonalEnsure(
  availability: RuntimeAvailability,
  ensureResult: EnsurePersonalWorkerResult | undefined,
  previousLastSeenAt: string,
) {
  if (availability.state !== "ready") return false;
  if (ensureResult?.status === "starting" && previousLastSeenAt && availability.lastSeenAt === previousLastSeenAt) {
    return false;
  }
  return true;
}

export async function ensureRuntimeReady(input: RuntimeReadyInput) {
  const first = await fetchRuntimeAvailability(input);
  const ensureResult = await ensurePersonalWorkerProcess(input);
  if (ensureResult && !ensureResult.ok) throw new Error(input.unavailableMessage);
  if (!ensureResult && first.state === "ready") return first;

  if (ensureResult) {
    const previousLastSeenAt = first.state === "ready" ? first.lastSeenAt || "" : "";
    if (availabilityReadyAfterPersonalEnsure(first, ensureResult, previousLastSeenAt)) return first;
    const maxAttempts = input.maxAttempts ?? 12;
    const pollIntervalMs = input.pollIntervalMs ?? 1_000;
    for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
      await sleep(pollIntervalMs);
      const availability = await fetchRuntimeAvailability(input);
      if (availabilityReadyAfterPersonalEnsure(availability, ensureResult, previousLastSeenAt)) return availability;
    }
    throw new Error(input.startingMessage);
  }

  throw readinessError(input, first);
}
