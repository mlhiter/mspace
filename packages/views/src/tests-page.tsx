import { useEffect, useId, useMemo, useState, type Dispatch, type FormEvent, type ReactNode, type SetStateAction } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import {
  ArrowLeft,
  ArrowRight,
  Ban,
  Check,
  ClipboardCheck,
  FileUp,
  ListChecks,
  Maximize2,
  Network,
  Play,
  Plus,
  RotateCcw,
  Save,
  ShieldCheck,
  Search,
  Sparkles,
  TerminalSquare,
  type LucideIcon,
  X,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import {
  buildControlPlaneUrl,
  controlPlaneApi,
  getControlPlaneBaseUrl,
  getStoredAuthToken,
  queryKeys,
  type Project,
  type Environment,
  type RuntimeWorker,
  type TestCase,
  type TestCaseInput,
  type TestCaseLatestResult,
  type TestCaseProposal,
  type TestCaseRevision,
  type TestCaseRunItem,
  type TestCaseStep,
  type TestPlan,
  type TestPlanDetail,
  type TestRun,
  type TestRunDetail,
  type TestRunItem,
} from "@mspace/core";
import { useMspaceTranslation } from "@mspace/i18n";
import {
  Button,
  CollectionEmptyState,
  Field,
  Input,
  Notice,
  PageFrame,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  StatusBadge,
  Textarea,
  cn,
} from "@mspace/ui";
import { useMspaceAuth } from "./auth-context";
import { RelativeTime } from "./time";

type TabKey = "cases" | "proposals" | "plans" | "runs";
type TestCaseDetailTab = "details" | "runs" | "revisions";

type CaseForm = {
  title: string;
  type: string;
  area: string;
  priority: string;
  status: string;
  preconditions: string;
  steps: TestCaseStep[];
  expectedResult: string;
  environmentRequirements: string;
  tagsText: string;
};

type PlanForm = {
  title: string;
  description: string;
  status: string;
  targetType: string;
  targetValue: string;
  environmentId: string;
  environment: string;
};

const emptyCaseForm: CaseForm = {
  title: "",
  type: "functional",
  area: "",
  priority: "",
  status: "draft",
  preconditions: "",
  steps: [{ action: "", expected: "" }],
  expectedResult: "",
  environmentRequirements: "",
  tagsText: "",
};

const emptyPlanForm: PlanForm = {
  title: "",
  description: "",
  status: "ready",
  targetType: "branch",
  targetValue: "",
  environmentId: "",
  environment: "",
};

const tabs: TabKey[] = ["cases", "proposals", "plans", "runs"];
const caseDetailTabs: TestCaseDetailTab[] = ["details", "runs", "revisions"];
const statusOptions = ["draft", "needs_review", "ready", "archived"] as const;
const proposalStatusOptions = ["pending", "applied", "rejected", "invalid"] as const;
const planStatusOptions = ["draft", "ready", "archived"] as const;
const testCaseTypeOptions = ["functional", "ui", "api", "deployment"] as const;
const priorityOptions = ["", "p0", "p1", "p2", "p3"] as const;
const screenshotPreviewMinZoom = 0.5;
const screenshotPreviewMaxZoom = 3;
const screenshotPreviewZoomStep = 0.25;
const targetTypeOptions = ["branch", "commit", "source_session", "image", "offline_package", "version_url", "preview_url"] as const;
const emptyProjects: Project[] = [];
const emptyEnvironments: Environment[] = [];
const emptyTestCases: TestCase[] = [];
const emptyTestCaseProposals: TestCaseProposal[] = [];
const emptyTestPlans: TestPlan[] = [];
const emptyTestRuns: TestRun[] = [];
const emptyTestCaseRevisions: TestCaseRevision[] = [];
const toolbarSelectClass =
  "h-8 min-h-8 rounded-[6px] bg-transparent px-2 py-1 text-[12px] leading-4 text-[color:var(--muted)] shadow-none hover:bg-[color:var(--hover)] focus:bg-[color:var(--hover)] focus:shadow-[inset_0_0_0_1px_var(--line)] data-[state=open]:bg-[color:var(--hover)] data-[state=open]:shadow-[inset_0_0_0_1px_var(--line)] [&_svg]:size-3.5";
const revisionPreviewLimit = 96;

type TestCaseRevisionChange = {
  key: string;
  label: string;
  before: string;
  after: string;
};

type TestCaseRevisionFact = {
  key: string;
  label: string;
  value: string;
  compareValue?: string;
};

function TestsPanelState(props: {
  icon: LucideIcon;
  title: string;
  body?: string;
  children?: ReactNode;
}) {
  const Icon = props.icon;
  return (
    <div className="grid min-h-[320px] place-items-center px-6 py-12 text-center sm:min-h-[360px]">
      <div className="flex max-w-[420px] flex-col items-center">
        <div className="grid size-9 place-items-center rounded-[8px] bg-[color:var(--paper)] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
          <Icon data-icon className="size-4" />
        </div>
        <h2 className="mt-3 text-[15px] font-semibold leading-6 text-[color:var(--text)]">{props.title}</h2>
        {props.body ? <p className="mt-1.5 max-w-[44ch] text-pretty text-[13px] leading-6 text-[color:var(--muted)]">{props.body}</p> : null}
        {props.children ? <div className="mt-4 flex flex-wrap justify-center gap-2">{props.children}</div> : null}
      </div>
    </div>
  );
}

function TestsLoadingRows(props: { label: string }) {
  return (
    <div role="status" aria-label={props.label} className="grid animate-pulse divide-y divide-[color:var(--line)] motion-reduce:animate-none">
      <span className="sr-only">{props.label}</span>
      {Array.from({ length: 4 }).map((_, index) => (
        <div key={index} className="grid gap-2 px-4 py-3 md:grid-cols-[minmax(0,1fr)_120px_88px_24px]">
          <div className="grid gap-2">
            <div className="h-4 w-2/5 rounded-[4px] bg-[color:var(--block)]" />
            <div className="h-3 w-3/5 rounded-[4px] bg-[color:var(--block)]" />
          </div>
          <div className="hidden h-6 rounded-[4px] bg-[color:var(--block)] md:block" />
          <div className="hidden h-6 rounded-[4px] bg-[color:var(--block)] md:block" />
          <div className="hidden h-6 rounded-[4px] bg-[color:var(--block)] md:block" />
        </div>
      ))}
    </div>
  );
}

type WorkflowStageState = "ready" | "blocked" | "active" | "waiting";

function workflowStageValue(state: WorkflowStageState) {
  if (state === "active") return "running";
  if (state === "blocked") return "blocked";
  if (state === "waiting") return "needs_review";
  return "completed";
}

function WorkflowStageButton(props: {
  index: number;
  icon: LucideIcon;
  title: string;
  count: string;
  status: WorkflowStageState;
  statusLabel: string;
  body: string;
  dependency: string;
  active: boolean;
  onClick: () => void;
}) {
  const Icon = props.icon;
  return (
    <button
      type="button"
      onClick={props.onClick}
      className={cn(
        "grid min-h-[164px] gap-3 rounded-[10px] bg-[color:var(--paper)] p-4 text-left shadow-[inset_0_0_0_1px_var(--line)] transition-colors hover:bg-[color:var(--hover)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)]",
        props.active ? "bg-[color:var(--selection)] shadow-[inset_0_0_0_1px_var(--text)]" : "",
      )}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <span className="grid size-6 shrink-0 place-items-center rounded-full bg-[color:var(--block)] text-[11px] font-semibold tabular-nums text-[color:var(--muted-strong)]">
            {props.index}
          </span>
          <Icon data-icon className="size-4 shrink-0 text-[color:var(--muted)]" />
          <span className="truncate text-[13px] font-semibold text-[color:var(--text)]">{props.title}</span>
        </div>
        <span className="shrink-0 text-[12px] font-medium tabular-nums text-[color:var(--muted)]">{props.count}</span>
      </div>
      <StatusBadge value={workflowStageValue(props.status)} valueLabel={props.statusLabel} className="w-fit" />
      <p className="text-pretty text-[12px] leading-5 text-[color:var(--muted)]">{props.body}</p>
      <p className="mt-auto border-t border-[color:var(--line)] pt-2 text-[11px] leading-4 text-[color:var(--faint)]">{props.dependency}</p>
    </button>
  );
}

function StageIntro(props: {
  icon: LucideIcon;
  title: string;
  body: string;
  meta?: string;
  children?: ReactNode;
}) {
  const Icon = props.icon;
  return (
    <div className="flex flex-wrap items-start justify-between gap-3 border-b border-[color:var(--line)] p-4">
      <div className="flex min-w-0 gap-3">
        <div className="grid size-8 shrink-0 place-items-center rounded-[8px] bg-[color:var(--block)] text-[color:var(--muted)]">
          <Icon data-icon className="size-4" />
        </div>
        <div className="min-w-0">
          <h2 className="text-[15px] font-semibold leading-6 text-[color:var(--text)]">{props.title}</h2>
          <p className="mt-1 max-w-[72ch] text-pretty text-[12px] leading-5 text-[color:var(--muted)]">{props.body}</p>
          {props.meta ? <p className="mt-1 text-[12px] text-[color:var(--faint)]">{props.meta}</p> : null}
        </div>
      </div>
      {props.children ? <div className="flex flex-wrap items-center gap-2">{props.children}</div> : null}
    </div>
  );
}

function testsTabSearch(tab: TabKey, projectId?: string) {
  return { tab, project: projectId || undefined };
}

function testCaseDetailSearch(
  projectId?: string,
  caseTab: TestCaseDetailTab = "details",
  focus?: { runId?: string; itemId?: string },
) {
  return {
    ...testsTabSearch("cases", projectId),
    caseTab,
    run: focus?.runId || undefined,
    item: focus?.itemId || undefined,
  };
}

function useTestsSearch(): { tab?: string; project?: string; caseTab?: string; run?: string; item?: string } {
  return useSearch({ strict: false }) as { tab?: string; project?: string; caseTab?: string; run?: string; item?: string };
}

function normalizeTestCaseForView(testCase: Partial<TestCase> | null | undefined): TestCase {
  const value = testCase || {};
  return {
    id: value.id || "",
    workspaceId: value.workspaceId || "",
    projectId: value.projectId || "",
    title: value.title || "",
    type: value.type || "functional",
    area: value.area || "",
    priority: value.priority || "",
    status: value.status || "draft",
    source: value.source || "manual",
    preconditions: value.preconditions || "",
    steps: value.steps ?? [],
    expectedResult: value.expectedResult || "",
    environmentRequirements: value.environmentRequirements || "",
    dependencies: value.dependencies ?? [],
    tags: value.tags ?? [],
    qualityScore: value.qualityScore ?? 0,
    qualityFindings: value.qualityFindings ?? [],
    latestResult: value.latestResult,
    createdByUserId: value.createdByUserId || "",
    createdAt: value.createdAt || "",
    updatedAt: value.updatedAt || "",
  };
}

function caseToForm(testCase: TestCase): CaseForm {
  const normalized = normalizeTestCaseForView(testCase);
  return {
    title: normalized.title,
    type: normalized.type || "functional",
    area: normalized.area,
    priority: normalized.priority || "",
    status: normalized.status || "draft",
    preconditions: normalized.preconditions,
    steps: normalized.steps.length > 0 ? normalized.steps : [{ action: "", expected: "" }],
    expectedResult: normalized.expectedResult,
    environmentRequirements: normalized.environmentRequirements,
    tagsText: normalized.tags.join(", "),
  };
}

function formToInput(form: CaseForm): TestCaseInput {
  return {
    title: form.title,
    type: form.type || "functional",
    area: form.area,
    priority: form.priority,
    status: form.status,
    source: "manual",
    preconditions: form.preconditions,
    steps: form.steps
      .map((step) => ({ action: step.action.trim(), expected: (step.expected || "").trim() }))
      .filter((step) => step.action || step.expected),
    expectedResult: form.expectedResult,
    environmentRequirements: form.environmentRequirements,
    tags: form.tagsText
      .split(",")
      .map((tag) => tag.trim())
      .filter(Boolean),
  };
}

function scoreTone(score: number) {
  if (score >= 85) return "text-[color:var(--success)]";
  if (score >= 70) return "text-[color:var(--warning)]";
  return "text-[color:var(--danger)]";
}

function testCaseMatchesQuery(testCase: TestCase, query: string) {
  const normalized = query.trim().toLowerCase();
  if (!normalized) return true;
  const viewCase = normalizeTestCaseForView(testCase);
  return [viewCase.title, viewCase.type, viewCase.area, viewCase.priority, viewCase.status, viewCase.tags.join(" ")]
    .join(" ")
    .toLowerCase()
    .includes(normalized);
}

function runPassRate(items: TestRunItem[]) {
  if (items.length === 0) return "0%";
  const passed = items.filter((item) => item.status === "passed").length;
  return `${Math.round((passed / items.length) * 100)}%`;
}

function latestResultLabel(result: TestCaseLatestResult | undefined, t: ReturnType<typeof useMspaceTranslation>["t"]) {
  if (!result) return t("tests.notRun");
  return t(`tests.runItemStatusValue.${result.status}`, { defaultValue: result.status });
}

function latestResultTone(result: TestCaseLatestResult | undefined) {
  switch (result?.status) {
    case "passed":
      return "text-[color:var(--success)]";
    case "failed":
      return "text-[color:var(--danger)]";
    case "blocked":
      return "text-[color:var(--warning)]";
    case "skipped":
      return "text-[color:var(--muted-strong)]";
    default:
      return "text-[color:var(--muted)]";
  }
}

type TestEvidenceAssertion = {
  name: string;
  passed?: boolean;
  status?: number;
  url?: string;
  error?: unknown;
};

type TestEvidenceNetworkStatus = {
  url: string;
  status?: number;
  method?: string;
  resourceType?: string;
};

type TestEvidenceScreenshotImage = {
  path: string;
  dataUrl: string;
  url: string;
  artifactUrl: string;
  artifactId: string;
  mime?: string;
  title?: string;
};

type StructuredTestEvidence = {
  screenshot?: string;
  screenshots: string[];
  screenshotImages: TestEvidenceScreenshotImage[];
  domSnapshot?: string;
  postSubmitSnapshot?: string;
  networkStatuses: TestEvidenceNetworkStatus[];
  assertions: TestEvidenceAssertion[];
  previewUrl?: string;
  finalUrl?: string;
  raw: Record<string, unknown>;
};

type TestCaseRunHistoryEntry = {
  id: string;
  itemId: string;
  runId: string;
  runStatus: string;
  runSource: string;
  status: string;
  actualResult: string;
  failureSummary: string;
  evidence?: Record<string, unknown>;
  targetType?: string;
  targetValue?: string;
  createdAt?: string;
  updatedAt: string;
};

function structuredTestEvidence(evidence: Record<string, unknown> | undefined): StructuredTestEvidence | undefined {
  if (!evidence || Object.keys(evidence).length === 0) return undefined;
  const screenshot = stringValue(evidence.screenshot);
  const screenshotImages = uniqueScreenshotImages([
    ...screenshotReferencesValue(evidence.artifacts),
    ...screenshotReferencesValue(evidence.screenshot),
    ...screenshotReferencesValue(evidence.screenshots),
    ...screenshotReferencesValue(evidence.screenshotPaths, "path"),
    ...screenshotImagesValue(evidence.screenshotImages),
  ]);
  const screenshots = screenshotImages.map(screenshotImageOpenTarget).filter(Boolean);
  return {
    screenshot,
    screenshots: [...new Set(screenshots)],
    screenshotImages,
    domSnapshot: stringValue(evidence.domSnapshot),
    postSubmitSnapshot: stringValue(evidence.postSubmitSnapshot),
    networkStatuses: networkStatusesValue(evidence.networkStatuses),
    assertions: assertionsValue(evidence.assertions),
    previewUrl: stringValue(evidence.previewUrl),
    finalUrl: stringValue(evidence.finalUrl),
    raw: evidence,
  };
}

function screenshotReferencesValue(value: unknown, defaultKind: "auto" | "path" = "auto"): TestEvidenceScreenshotImage[] {
  if (Array.isArray(value)) return value.flatMap((item) => screenshotReferencesValue(item, defaultKind));
  if (!value) return [];
  if (typeof value === "string") {
    const reference = screenshotReferenceFromString(value, defaultKind);
    return reference ? [reference] : [];
  }
  if (typeof value !== "object") return [];
  const record = value as Record<string, unknown>;
  const dataUrl = stringValue(record.dataUrl) || stringValue(record.dataURL) || stringValue(record.data_url);
  const url = stringValue(record.url);
  const artifactUrl = stringValue(record.artifactUrl) || stringValue(record.artifactURL) || stringValue(record.artifact_url);
  const thumbnailUrl = stringValue(record.thumbnailUrl) || stringValue(record.thumbnailURL) || stringValue(record.thumbnail_url);
  const path = stringValue(record.path);
  const artifactId = stringValue(record.artifactId) || stringValue(record.artifactID) || stringValue(record.artifact_id) || stringValue(record.id);
  if (!dataUrl && !url && !artifactUrl && !thumbnailUrl && !path && !artifactId) return [];
  return [
    {
      dataUrl: dataUrl.startsWith("data:image/") ? dataUrl : "",
      url: url || thumbnailUrl,
      artifactUrl: artifactUrl || thumbnailUrl,
      artifactId,
      path,
      mime: stringValue(record.mime),
      title: stringValue(record.title) || stringValue(record.name),
    },
  ];
}

function screenshotReferenceFromString(value: string, defaultKind: "auto" | "path"): TestEvidenceScreenshotImage | undefined {
  const reference = value.trim();
  if (!reference) return undefined;
  if (reference.startsWith("data:image/")) {
    return { dataUrl: reference, url: "", artifactUrl: "", artifactId: "", path: "" };
  }
  if (defaultKind !== "path" && isHttpUrl(reference)) {
    return { dataUrl: "", url: reference, artifactUrl: "", artifactId: "", path: "" };
  }
  if (defaultKind !== "path" && (reference.startsWith("/api/") || reference.startsWith("/artifacts/"))) {
    return { dataUrl: "", url: "", artifactUrl: reference, artifactId: "", path: "" };
  }
  return { dataUrl: "", url: "", artifactUrl: "", artifactId: "", path: reference };
}

function screenshotImagesValue(value: unknown): TestEvidenceScreenshotImage[] {
  if (!Array.isArray(value)) return [];
  const images: TestEvidenceScreenshotImage[] = [];
  for (const item of value) {
    images.push(...screenshotReferencesValue(item));
  }
  return images;
}

function uniqueScreenshotImages(images: TestEvidenceScreenshotImage[]) {
  const seen = new Set<string>();
  const unique: TestEvidenceScreenshotImage[] = [];
  for (const image of images) {
    const key = screenshotImageTarget(image) || image.artifactId;
    if (!key || seen.has(key)) continue;
    seen.add(key);
    unique.push(image);
  }
  return unique;
}

function screenshotImageSource(image: TestEvidenceScreenshotImage) {
  const source = image.dataUrl || image.url || image.artifactUrl;
  if (isAbsoluteLocalEvidencePath(source)) return "";
  if (source.startsWith("/")) return `${getControlPlaneBaseUrl()}${source}`;
  return source;
}

function isAbsoluteLocalEvidencePath(value: string) {
  const target = value.trim();
  if (!target || target.startsWith("/api/") || target.startsWith("/artifacts/")) return false;
  return /^(\/tmp\/|\/private\/|\/var\/|\/Users\/|\/home\/|\/Volumes\/|[A-Za-z]:[\\/])/.test(target);
}

function isProtectedEvidenceApiPath(value: string) {
  return value.startsWith("/api/test-artifacts/") || value.includes("/api/test-artifacts/");
}

function evidenceApiUrl(value: string) {
  const target = value.trim();
  if (!target) return "";
  if (target.startsWith("http")) return target;
  if (target.startsWith("/")) return buildControlPlaneUrl(target);
  return "";
}

function useResolvedEvidenceImageSrc(image: TestEvidenceScreenshotImage) {
  const rawSource = screenshotImageSource(image);
  const token = getStoredAuthToken();
  const rawApiSource = image.artifactUrl || image.url || rawSource;
  const protectedPath = isProtectedEvidenceApiPath(rawApiSource) ? rawApiSource : "";
  const [resolvedSrc, setResolvedSrc] = useState(protectedPath ? "" : rawSource);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!protectedPath) {
      setResolvedSrc(rawSource);
      setError("");
      return;
    }
    if (!token) {
      setResolvedSrc("");
      setError("missing authorization");
      return;
    }
    const controller = new AbortController();
    let objectUrl = "";
    setResolvedSrc("");
    setError("");
    const url = evidenceApiUrl(protectedPath);
    fetch(url, {
      headers: { Authorization: `Bearer ${token}` },
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) throw new Error((await response.text()) || `Request failed with status ${response.status}`);
        return response.blob();
      })
      .then((blob) => {
        if (controller.signal.aborted) return;
        objectUrl = URL.createObjectURL(blob);
        setResolvedSrc(objectUrl);
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        setError(cause instanceof Error ? cause.message : "image unavailable");
      });
    return () => {
      controller.abort();
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [protectedPath, rawSource, token]);

  return {
    src: resolvedSrc,
    loading: Boolean(protectedPath && !resolvedSrc && !error),
    error,
  };
}

function screenshotImageTarget(image: TestEvidenceScreenshotImage) {
  return image.artifactUrl || image.url || image.path || image.dataUrl || image.artifactId;
}

function screenshotImageOpenTarget(image: TestEvidenceScreenshotImage) {
  return image.artifactUrl || image.url || image.path;
}

function screenshotImageIsLegacyLocalPath(image: TestEvidenceScreenshotImage) {
  const target = screenshotImageOpenTarget(image);
  return Boolean(target && isAbsoluteLocalEvidencePath(target) && !image.dataUrl && !image.url && !image.artifactUrl && !image.artifactId);
}

function screenshotImageLabel(image: TestEvidenceScreenshotImage, fallback: string) {
  return image.title || image.path || image.artifactId || image.artifactUrl || image.url || fallback;
}

function normalizeTestCaseRunHistoryEntry(value: TestCaseRunItem): TestCaseRunHistoryEntry | undefined {
  const { item, run } = value;
  const itemId = item.id;
  const runId = item.runId || run.id;
  const updatedAt = item.updatedAt || run.updatedAt;
  if (!runId && !itemId) return undefined;
  return {
    id: itemId || runId,
    itemId,
    runId,
    runStatus: run.status,
    runSource: run.source,
    status: item.status,
    actualResult: item.actualResult,
    failureSummary: item.failureSummary,
    evidence: item.evidence,
    targetType: run.targetType,
    targetValue: run.targetValue,
    createdAt: item.createdAt || run.createdAt,
    updatedAt,
  };
}

function latestResultToHistoryEntry(result: TestCaseLatestResult | undefined): TestCaseRunHistoryEntry | undefined {
  if (!result) return undefined;
  return {
    id: result.itemId || result.runId,
    itemId: result.itemId,
    runId: result.runId,
    runStatus: result.runStatus,
    runSource: result.runSource,
    status: result.status,
    actualResult: result.actualResult,
    failureSummary: result.failureSummary,
    evidence: result.evidence,
    updatedAt: result.updatedAt,
  };
}

function useTestCaseRunHistory(params: {
  token: string;
  workspaceId: string;
  projectId: string;
  caseId: string;
  enabled: boolean;
  latestResult?: TestCaseLatestResult;
}) {
  const historyQuery = useQuery({
    queryKey: queryKeys.projectTestCaseRunItems(params.workspaceId, params.projectId, params.caseId || "__none", params.token),
    queryFn: async () =>
      (await controlPlaneApi.listProjectTestCaseRunItems(params.token, params.workspaceId, params.projectId, params.caseId))
        .map(normalizeTestCaseRunHistoryEntry)
        .filter((entry): entry is TestCaseRunHistoryEntry => Boolean(entry)),
    enabled: params.enabled,
  });
  const latestEntry = useMemo(() => latestResultToHistoryEntry(params.latestResult), [params.latestResult]);
  const apiEntries = historyQuery.data || [];
  const entries = apiEntries.length > 0 ? apiEntries : latestEntry ? [latestEntry] : [];
  return {
    query: historyQuery,
    entries,
  };
}

function stringValue(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

function networkStatusesValue(value: unknown): TestEvidenceNetworkStatus[] {
  if (!Array.isArray(value)) return [];
  const statuses: TestEvidenceNetworkStatus[] = [];
  for (const item of value) {
    if (!item || typeof item !== "object") continue;
    const record = item as Record<string, unknown>;
    const url = stringValue(record.url);
    if (!url) continue;
    statuses.push({
      url,
      status: typeof record.status === "number" ? record.status : undefined,
      method: stringValue(record.method),
      resourceType: stringValue(record.resourceType),
    });
  }
  return statuses;
}

function assertionsValue(value: unknown): TestEvidenceAssertion[] {
  if (!Array.isArray(value)) return [];
  const assertions: TestEvidenceAssertion[] = [];
  for (const item of value) {
    if (!item || typeof item !== "object") continue;
    const record = item as Record<string, unknown>;
    const name = stringValue(record.name);
    if (!name) continue;
    assertions.push({
      name,
      passed: typeof record.passed === "boolean" ? record.passed : undefined,
      status: typeof record.status === "number" ? record.status : undefined,
      url: stringValue(record.url),
      error: record.error,
    });
  }
  return assertions;
}

function evidencePreviewText(value: string, limit = 560) {
  const normalized = value.replace(/\s+/g, " ").trim();
  if (normalized.length <= limit) return normalized;
  return `${normalized.slice(0, limit)}...`;
}

function isHttpUrl(value: string) {
  return /^https?:\/\//i.test(value);
}

function resolveEvidenceUrl(value: string) {
  const target = value.trim();
  if (!target) return "";
  if (isAbsoluteLocalEvidencePath(target)) return target;
  if (target.startsWith("/")) return `${getControlPlaneBaseUrl()}${target}`;
  return target;
}

function evidenceStatusTone(status?: number) {
  if (!status) return "text-[color:var(--muted)]";
  if (status >= 200 && status < 400) return "text-[color:var(--success)]";
  if (status >= 400) return "text-[color:var(--danger)]";
  return "text-[color:var(--warning)]";
}

async function openEvidenceTarget(value: string) {
  const target = resolveEvidenceUrl(value);
  if (!target) return;
  if (isProtectedEvidenceApiPath(value) || isProtectedEvidenceApiPath(target)) {
    const token = getStoredAuthToken();
    if (!token) return;
    const response = await fetch(evidenceApiUrl(value) || target, {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (!response.ok) {
      console.warn(await response.text());
      return;
    }
    const objectUrl = URL.createObjectURL(await response.blob());
    window.open(objectUrl, "_blank", "noopener,noreferrer");
    window.setTimeout(() => URL.revokeObjectURL(objectUrl), 60_000);
    return;
  }
  if (value.startsWith("data:image/")) {
    window.open(value, "_blank", "noopener,noreferrer");
    return;
  }
  if (isHttpUrl(target) && window.mspaceDesktop?.openExternal) {
    await window.mspaceDesktop.openExternal(target);
    return;
  }
  if (window.mspaceDesktop?.openPath) {
    const error = await window.mspaceDesktop.openPath(target);
    if (error) console.warn(error);
    return;
  }
  if (isAbsoluteLocalEvidencePath(target)) return;
  window.open(target, "_blank", "noopener,noreferrer");
}

function hasText(value?: string) {
  return (value || "").trim().length > 0;
}

function hasRunnableStep(steps: TestCaseStep[]) {
  return steps.some((step) => hasText(step.action));
}

function bytesToBase64(bytes: Uint8Array) {
  const chunkSize = 0x8000;
  let binary = "";
  for (let index = 0; index < bytes.length; index += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(index, index + chunkSize));
  }
  return btoa(binary);
}

async function fileToBase64(file: File) {
  const buffer = await file.arrayBuffer();
  return bytesToBase64(new Uint8Array(buffer));
}

function qualityFindingLabel(code: string, message: string, t: ReturnType<typeof useMspaceTranslation>["t"]) {
  return t(`tests.qualityFinding.${code}`, { defaultValue: message || code });
}

function testCaseTypeLabel(type: string, t: ReturnType<typeof useMspaceTranslation>["t"]) {
  const value = type || "functional";
  return t(`tests.typeValue.${value}`, { defaultValue: value });
}

function testCaseStatusLabel(status: string, t: ReturnType<typeof useMspaceTranslation>["t"]) {
  const value = status || "draft";
  return t(`tests.statusValue.${value}`, { defaultValue: value });
}

function normalizeRevisionText(value: string | undefined) {
  return (value || "").replace(/\s+/g, " ").trim();
}

function compactRevisionText(value: string) {
  const compact = value.replace(/\s+/g, " ").trim();
  if (compact.length <= revisionPreviewLimit) return compact;
  return `${compact.slice(0, revisionPreviewLimit - 1)}...`;
}

function revisionEmptyValue(t: ReturnType<typeof useMspaceTranslation>["t"]) {
  return t("tests.revisionEmpty");
}

function revisionTextValue(value: string | undefined, t: ReturnType<typeof useMspaceTranslation>["t"]) {
  const compact = compactRevisionText(normalizeRevisionText(value));
  return compact || revisionEmptyValue(t);
}

function revisionPriorityLabel(priority: string | undefined, t: ReturnType<typeof useMspaceTranslation>["t"]) {
  return priority ? priority.toUpperCase() : t("tests.noPriority");
}

function normalizedRevisionSteps(steps: TestCaseStep[] | undefined) {
  return (steps || [])
    .map((step) => ({
      action: (step.action || "").replace(/\s+/g, " ").trim(),
      expected: (step.expected || "").replace(/\s+/g, " ").trim(),
    }))
    .filter((step) => step.action || step.expected);
}

function revisionStepsValue(steps: TestCaseStep[] | undefined, t: ReturnType<typeof useMspaceTranslation>["t"]) {
  const normalizedSteps = normalizedRevisionSteps(steps);
  if (normalizedSteps.length === 0) return t("tests.revisionNoSteps");
  const firstStep = normalizedSteps[0];
  const first = revisionTextValue([firstStep.action, firstStep.expected].filter(Boolean).join(" -> "), t);
  if (normalizedSteps.length === 1) {
    return t("tests.revisionOneStep", { first });
  }
  return t("tests.revisionStepSummary", { count: normalizedSteps.length, first });
}

function revisionStepsCompareValue(steps: TestCaseStep[] | undefined) {
  return JSON.stringify(normalizedRevisionSteps(steps));
}

function revisionListValue(values: string[] | undefined, t: ReturnType<typeof useMspaceTranslation>["t"]) {
  const normalizedValues = (values || []).map((value) => value.trim()).filter(Boolean);
  return normalizedValues.length > 0 ? compactRevisionText(normalizedValues.join(", ")) : revisionEmptyValue(t);
}

function revisionNumberValue(value: number | undefined, t: ReturnType<typeof useMspaceTranslation>["t"]) {
  return Number.isFinite(value) ? String(value) : revisionEmptyValue(t);
}

function testCaseRevisionFields(snapshot: TestCase, t: ReturnType<typeof useMspaceTranslation>["t"]): TestCaseRevisionFact[] {
  const normalized = normalizeTestCaseForView(snapshot);
  return [
    { key: "title", label: t("tests.revisionField.title"), value: revisionTextValue(normalized.title, t), compareValue: normalizeRevisionText(normalized.title) },
    { key: "type", label: t("tests.revisionField.type"), value: testCaseTypeLabel(normalized.type, t) },
    { key: "area", label: t("tests.revisionField.area"), value: revisionTextValue(normalized.area, t), compareValue: normalizeRevisionText(normalized.area) },
    { key: "priority", label: t("tests.revisionField.priority"), value: revisionPriorityLabel(normalized.priority, t) },
    { key: "status", label: t("tests.revisionField.status"), value: testCaseStatusLabel(normalized.status, t) },
    { key: "source", label: t("tests.revisionField.source"), value: t(`tests.sourceValue.${normalized.source}`, { defaultValue: normalized.source || revisionEmptyValue(t) }) },
    { key: "preconditions", label: t("tests.revisionField.preconditions"), value: revisionTextValue(normalized.preconditions, t), compareValue: normalizeRevisionText(normalized.preconditions) },
    { key: "steps", label: t("tests.revisionField.steps"), value: revisionStepsValue(normalized.steps, t), compareValue: revisionStepsCompareValue(normalized.steps) },
    { key: "expectedResult", label: t("tests.revisionField.expectedResult"), value: revisionTextValue(normalized.expectedResult, t), compareValue: normalizeRevisionText(normalized.expectedResult) },
    { key: "environmentRequirements", label: t("tests.revisionField.environmentRequirements"), value: revisionTextValue(normalized.environmentRequirements, t), compareValue: normalizeRevisionText(normalized.environmentRequirements) },
    { key: "tags", label: t("tests.revisionField.tags"), value: revisionListValue(normalized.tags, t), compareValue: JSON.stringify((normalized.tags || []).map((tag) => tag.trim()).filter(Boolean)) },
    { key: "qualityScore", label: t("tests.revisionField.qualityScore"), value: revisionNumberValue(normalized.qualityScore, t) },
  ];
}

function describeTestCaseRevisionChanges(
  previousSnapshot: TestCase,
  nextSnapshot: TestCase,
  t: ReturnType<typeof useMspaceTranslation>["t"],
): TestCaseRevisionChange[] {
  const previousFields = testCaseRevisionFields(previousSnapshot, t);
  const nextFields = testCaseRevisionFields(nextSnapshot, t);
  const previousByKey = new Map(previousFields.map((field) => [field.key, field]));

  return nextFields.reduce<TestCaseRevisionChange[]>((changes, nextField) => {
    const previousField = previousByKey.get(nextField.key);
    if (!previousField || (previousField.compareValue || previousField.value) === (nextField.compareValue || nextField.value)) {
      return changes;
    }
    changes.push({
      key: nextField.key,
      label: nextField.label,
      before: previousField.value,
      after: nextField.value,
    });
    return changes;
  }, []);
}

function buildTestCaseRevisionTimeline(revisions: TestCaseRevision[], t: ReturnType<typeof useMspaceTranslation>["t"]) {
  const ascending = [...revisions].sort((left, right) => left.revisionNumber - right.revisionNumber);
  const previousByRevisionId = new Map<string, TestCaseRevision>();

  ascending.forEach((revision, index) => {
    const previous = ascending[index - 1];
    if (previous) {
      previousByRevisionId.set(revision.id, previous);
    }
  });

  return revisions.map((revision) => {
    const previous = previousByRevisionId.get(revision.id);
    const snapshot = normalizeTestCaseForView(revision.snapshot);
    const changes = previous ? describeTestCaseRevisionChanges(normalizeTestCaseForView(previous.snapshot), snapshot, t) : [];
    const facts = previous
      ? []
      : testCaseRevisionFields(snapshot, t).filter((fact) => ["type", "status", "priority", "source", "steps", "expectedResult"].includes(fact.key));

    return {
      revision: {
        ...revision,
        snapshot,
      },
      changes,
      facts,
      isInitial: !previous,
    };
  });
}

function testCaseExecutability(testCase: TestCase) {
  const normalized = normalizeTestCaseForView(testCase);
  const checks = [
    hasText(normalized.preconditions),
    hasRunnableStep(normalized.steps),
    hasText(normalized.expectedResult),
    hasText(normalized.environmentRequirements),
  ];

  return {
    done: checks.filter(Boolean).length,
    total: checks.length,
    issues: normalized.qualityFindings.length,
  };
}

function executabilityTone(done: number, total: number, issues: number) {
  if (done === total && issues === 0) return "text-[color:var(--success)]";
  if (done >= total - 1) return "text-[color:var(--warning)]";
  return "text-[color:var(--danger)]";
}

function executabilityIssueLabel(count: number, t: ReturnType<typeof useMspaceTranslation>["t"]) {
  if (count === 0) return t("tests.noExecutableIssues");
  if (count === 1) return t("tests.oneExecutableIssue");
  return t("tests.executableIssueCount", { count });
}

function hasCodexCapability(worker: RuntimeWorker) {
  return worker.capabilities?.codex === true;
}

function isFreshWorkerHeartbeat(value: string) {
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) && Date.now() - timestamp <= 45_000;
}

function activeCodexWorker(workers: RuntimeWorker[], workspaceId: string, runtimeMode: string) {
  return workers.find(
    (worker) =>
      worker.workspaceId === workspaceId &&
      worker.mode === runtimeMode &&
      worker.status === "online" &&
      hasCodexCapability(worker) &&
      isFreshWorkerHeartbeat(worker.lastSeenAt),
  );
}

function TestDetailUnavailableState(props: {
  title: string;
  body: string;
  error?: Error | null;
  projectId: string;
  tab: TabKey;
}) {
  const { t } = useMspaceTranslation();
  return (
    <PageFrame
      title={props.title}
      subtitle={props.body}
      breadcrumbs={[
        { label: t("common.mspace"), to: "/inbox" },
        { label: t("tests.title"), to: "/tests", search: testsTabSearch(props.tab, props.projectId) },
        { label: props.title },
      ]}
    >
      <div className="grid gap-3">
        {props.error ? <Notice tone="danger">{props.error.message}</Notice> : null}
        <CollectionEmptyState title={props.title} body={props.error ? t("tests.detailLoadFailed") : props.body} />
      </div>
    </PageFrame>
  );
}

function sleep(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function useWorkspaceProjects() {
  const auth = useMspaceAuth();
  const workspaceId = auth.workspace?.id || "";
  const serverWorkspaceReady = Boolean(auth.token && workspaceId);
  const projectsQueryKey = queryKeys.workspaceProjects(workspaceId, auth.token);
  const projectsQuery = useQuery({
    queryKey: projectsQueryKey,
    queryFn: () => controlPlaneApi.listProjects(auth.token, workspaceId),
    enabled: serverWorkspaceReady,
  });

  return {
    auth,
    workspaceId,
    serverWorkspaceReady,
    projectsQuery,
    projects: projectsQuery.data || emptyProjects,
  };
}

function useTestsWorkerReadiness(
  auth: ReturnType<typeof useMspaceAuth>,
  workspaceId: string,
  onStatus?: (message: string) => void,
) {
  const { t } = useMspaceTranslation();
  const queryClient = useQueryClient();
  const runtimeMode = auth.workspace?.kind === "team" ? "team" : "personal";
  const runtimeWorkersQueryKey = queryKeys.runtimeWorkers(workspaceId, auth.token);
  const workerUnavailableText =
    runtimeMode === "personal"
      ? t("tests.personalWorkerUnavailable")
      : t("tests.teamWorkerUnavailable");
  const workerStartingText = t("tests.personalWorkerStarting");

  return useMemo(
    () => ({
      runtimeMode,
      ensureReady: async () => {
        const workers = await queryClient.fetchQuery({
          queryKey: runtimeWorkersQueryKey,
          queryFn: () => controlPlaneApi.listRuntimeWorkers(auth.token, workspaceId),
          staleTime: 0,
        });
        if (activeCodexWorker(workers, workspaceId, runtimeMode)) return;
        if (runtimeMode !== "personal" || !window.mspaceDesktop?.ensurePersonalWorker) {
          throw new Error(workerUnavailableText);
        }
        onStatus?.(t("tests.startingPersonalWorker"));
        await window.mspaceDesktop.ensurePersonalWorker({
          authToken: auth.token,
          workspaceId,
          serverUrl: getControlPlaneBaseUrl(),
        });
        for (let attempt = 0; attempt < 12; attempt += 1) {
          await sleep(1_000);
          const refreshed = await queryClient.fetchQuery({
            queryKey: runtimeWorkersQueryKey,
            queryFn: () => controlPlaneApi.listRuntimeWorkers(auth.token, workspaceId),
            staleTime: 0,
          });
          if (activeCodexWorker(refreshed, workspaceId, runtimeMode)) return;
        }
        throw new Error(workerStartingText);
      },
    }),
    [auth.token, onStatus, queryClient, runtimeMode, runtimeWorkersQueryKey, t, workerStartingText, workerUnavailableText, workspaceId],
  );
}

function projectFromSearch(projects: Project[], searchProjectId?: string): Project | undefined {
  return projects.find((project) => project.id === searchProjectId) || projects[0];
}

export function TestsPage() {
  const { t } = useMspaceTranslation();
  const navigate = useNavigate();
  const search = useTestsSearch();
  const queryClient = useQueryClient();
  const { auth, workspaceId, serverWorkspaceReady, projectsQuery, projects } = useWorkspaceProjects();
  const activeTab = tabs.includes(search.tab as TabKey) ? (search.tab as TabKey) : "cases";
  const [selectedProjectId, setSelectedProjectId] = useState("");
  const [selectedCaseIds, setSelectedCaseIds] = useState<string[]>([]);
  const [selectedPlanId, setSelectedPlanId] = useState("");
  const [createCaseOpen, setCreateCaseOpen] = useState(false);
  const [createPlanOpen, setCreatePlanOpen] = useState(false);
  const [caseForm, setCaseForm] = useState<CaseForm>(emptyCaseForm);
  const [planForm, setPlanForm] = useState<PlanForm>(emptyPlanForm);
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [proposalStatusFilter, setProposalStatusFilter] = useState("pending");
  const [planStatusFilter, setPlanStatusFilter] = useState("all");
  const [importOpen, setImportOpen] = useState(false);
  const [importFormat, setImportFormat] = useState("markdown");
  const [importContent, setImportContent] = useState("");
  const [importFile, setImportFile] = useState<File | null>(null);
  const [importFileError, setImportFileError] = useState("");
  const [importSummary, setImportSummary] = useState("");
  const [actionMessage, setActionMessage] = useState("");
  const workerReadiness = useTestsWorkerReadiness(auth, workspaceId, setActionMessage);

  const selectedProject = projects.find((project) => project.id === selectedProjectId) || projects[0];
  const effectiveProjectId = selectedProject?.id || "";
  const casesQueryKey = queryKeys.projectTestCases(
    workspaceId,
    effectiveProjectId,
    auth.token,
    statusFilter === "all" ? "" : statusFilter,
    query,
  );
  const proposalQueryKey = queryKeys.projectTestCaseProposals(
    workspaceId,
    effectiveProjectId,
    auth.token,
    proposalStatusFilter === "all" ? "" : proposalStatusFilter,
  );
  const plansQueryKey = queryKeys.projectTestPlans(
    workspaceId,
    effectiveProjectId,
    auth.token,
    planStatusFilter === "all" ? "" : planStatusFilter,
  );
  const allCasesQueryKey = queryKeys.projectTestCases(workspaceId, effectiveProjectId, auth.token);
  const allProposalsQueryKey = queryKeys.projectTestCaseProposals(workspaceId, effectiveProjectId, auth.token);
  const allPlansQueryKey = queryKeys.projectTestPlans(workspaceId, effectiveProjectId, auth.token);
  const allRunsQueryKey = queryKeys.projectTestRuns(workspaceId, effectiveProjectId, auth.token);
  const environmentsQueryKey = queryKeys.environments(workspaceId, auth.token);

  const casesQuery = useQuery({
    queryKey: casesQueryKey,
    queryFn: () =>
      controlPlaneApi.listProjectTestCases(auth.token, workspaceId, effectiveProjectId, {
        status: statusFilter === "all" ? "" : statusFilter,
        query,
      }),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId),
  });

  const allCasesQuery = useQuery({
    queryKey: allCasesQueryKey,
    queryFn: () => controlPlaneApi.listProjectTestCases(auth.token, workspaceId, effectiveProjectId),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId),
  });

  const proposalsQuery = useQuery({
    queryKey: proposalQueryKey,
    queryFn: () =>
      controlPlaneApi.listProjectTestCaseProposals(auth.token, workspaceId, effectiveProjectId, {
        status: proposalStatusFilter === "all" ? "" : proposalStatusFilter,
      }),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId),
  });

  const allProposalsQuery = useQuery({
    queryKey: allProposalsQueryKey,
    queryFn: () => controlPlaneApi.listProjectTestCaseProposals(auth.token, workspaceId, effectiveProjectId),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId),
  });

  const plansQuery = useQuery({
    queryKey: plansQueryKey,
    queryFn: () =>
      controlPlaneApi.listProjectTestPlans(auth.token, workspaceId, effectiveProjectId, {
        status: planStatusFilter === "all" ? "" : planStatusFilter,
      }),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId),
  });

  const allPlansQuery = useQuery({
    queryKey: allPlansQueryKey,
    queryFn: () => controlPlaneApi.listProjectTestPlans(auth.token, workspaceId, effectiveProjectId),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId),
  });

  const selectedPlanQuery = useQuery({
    queryKey: queryKeys.projectTestPlan(workspaceId, effectiveProjectId, selectedPlanId || "__none", auth.token),
    queryFn: () => controlPlaneApi.getProjectTestPlan(auth.token, workspaceId, effectiveProjectId, selectedPlanId),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId && selectedPlanId),
  });

  const allRunsQuery = useQuery({
    queryKey: allRunsQueryKey,
    queryFn: () => controlPlaneApi.listProjectTestRuns(auth.token, workspaceId, effectiveProjectId),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId),
  });

  const environmentsQuery = useQuery({
    queryKey: environmentsQueryKey,
    queryFn: () => controlPlaneApi.listEnvironments(auth.token, workspaceId),
    enabled: serverWorkspaceReady,
  });

  const cases = useMemo(() => (casesQuery.data || emptyTestCases).filter((testCase) => testCaseMatchesQuery(testCase, query)), [casesQuery.data, query]);
  const allCases = allCasesQuery.data || casesQuery.data || emptyTestCases;
  const allProposals = allProposalsQuery.data || proposalsQuery.data || emptyTestCaseProposals;
  const allPlans = allPlansQuery.data || plansQuery.data || emptyTestPlans;
  const allRuns = allRunsQuery.data || emptyTestRuns;
  const environments = environmentsQuery.data || emptyEnvironments;
  const readyCases = useMemo(() => allCases.filter((testCase) => testCase.status === "ready"), [allCases]);
  const readyCaseIdSet = useMemo(() => new Set(readyCases.map((testCase) => testCase.id)), [readyCases]);
  const selectedReadyCaseIds = useMemo(() => selectedCaseIds.filter((caseId) => readyCaseIdSet.has(caseId)), [readyCaseIdSet, selectedCaseIds]);
  const pendingProposals = useMemo(() => allProposals.filter((proposal) => proposal.status === "pending"), [allProposals]);
  const readyPlans = useMemo(() => allPlans.filter((plan) => plan.status === "ready"), [allPlans]);
  const proposals = proposalsQuery.data || emptyTestCaseProposals;
  const plans = plansQuery.data || emptyTestPlans;
  const selectedPlan = selectedPlanQuery.data?.plan || plans.find((plan) => plan.id === selectedPlanId);
  const selectedPlanDetail = selectedPlanQuery.data;
  const latestRun = useMemo(() => [...allRuns].sort((left, right) => Date.parse(right.updatedAt) - Date.parse(left.updatedAt))[0], [allRuns]);
  const canCreateCase = Boolean(effectiveProjectId && caseForm.title.trim());
  const canCreatePlan = Boolean(effectiveProjectId && planForm.title.trim() && selectedCaseIds.length > 0);
  const isExcelImport = importFormat === "xlsx";
  const canImportCases = isExcelImport ? Boolean(importFile) : Boolean(importContent.trim());

  async function invalidateCaseWorkflow() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: casesQueryKey }),
      queryClient.invalidateQueries({ queryKey: proposalQueryKey }),
      queryClient.invalidateQueries({ queryKey: plansQueryKey }),
      queryClient.invalidateQueries({ queryKey: allCasesQueryKey }),
      queryClient.invalidateQueries({ queryKey: allProposalsQueryKey }),
      queryClient.invalidateQueries({ queryKey: allPlansQueryKey }),
      queryClient.invalidateQueries({ queryKey: allRunsQueryKey }),
      selectedPlanId
        ? queryClient.invalidateQueries({ queryKey: queryKeys.projectTestPlan(workspaceId, effectiveProjectId, selectedPlanId, auth.token) })
        : Promise.resolve(),
    ]);
  }

  const createCase = useMutation({
    mutationFn: (input: TestCaseInput) => controlPlaneApi.createProjectTestCase(auth.token, workspaceId, effectiveProjectId, input),
    onSuccess: async (created) => {
      setCaseForm(emptyCaseForm);
      setCreateCaseOpen(false);
      setSelectedCaseIds([created.id]);
      setActionMessage(t("tests.caseSaved"));
      await invalidateCaseWorkflow();
    },
  });

  const importCases = useMutation({
    mutationFn: async () => {
      const content = isExcelImport && importFile ? await fileToBase64(importFile) : importContent;
      return controlPlaneApi.importProjectTestCases(auth.token, workspaceId, effectiveProjectId, {
        format: importFormat,
        content,
        fileName: importFile?.name,
      });
    },
    onSuccess: async (result) => {
      const created = result.created ?? [];
      const skipped = result.skipped ?? [];
      const summary = t("tests.importedSummary", { created: created.length, skipped: skipped.length });
      setImportSummary(summary);
      setActionMessage(summary);
      setImportContent("");
      setImportFile(null);
      setImportFileError("");
      setImportOpen(false);
      if (created[0]) {
        setSelectedCaseIds([created[0].id]);
      }
      await invalidateCaseWorkflow();
    },
  });

  const optimizeCases = useMutation({
    mutationFn: async () => {
      await workerReadiness.ensureReady();
      return controlPlaneApi.optimizeProjectTestCases(auth.token, workspaceId, effectiveProjectId, {
        caseIds: selectedCaseIds,
        runtimeMode: workerReadiness.runtimeMode,
      });
    },
    onSuccess: async (result) => {
      setActionMessage(t("tests.agentSessionQueued", { issueId: result.issueId }));
      await navigate({ to: "/tests", search: testsTabSearch("proposals", effectiveProjectId) });
      await invalidateCaseWorkflow();
    },
  });

  const generateCases = useMutation({
    mutationFn: async () => {
      await workerReadiness.ensureReady();
      return controlPlaneApi.generateProjectTestCases(auth.token, workspaceId, effectiveProjectId, {
        runtimeMode: workerReadiness.runtimeMode,
      });
    },
    onSuccess: async (result) => {
      setActionMessage(t("tests.agentSessionQueued", { issueId: result.issueId }));
      await navigate({ to: "/tests", search: testsTabSearch("proposals", effectiveProjectId) });
      await invalidateCaseWorkflow();
    },
  });

  const applyProposal = useMutation({
    mutationFn: (proposal: TestCaseProposal) =>
      controlPlaneApi.applyProjectTestCaseProposal(auth.token, workspaceId, effectiveProjectId, proposal.id),
    onSuccess: async () => {
      setActionMessage(t("tests.proposalApplied"));
      await invalidateCaseWorkflow();
    },
  });

  const rejectProposal = useMutation({
    mutationFn: (proposal: TestCaseProposal) =>
      controlPlaneApi.rejectProjectTestCaseProposal(auth.token, workspaceId, effectiveProjectId, proposal.id),
    onSuccess: async () => {
      setActionMessage(t("tests.proposalRejected"));
      await invalidateCaseWorkflow();
    },
  });

  const createPlan = useMutation({
    mutationFn: () =>
      controlPlaneApi.createProjectTestPlan(auth.token, workspaceId, effectiveProjectId, {
        ...planForm,
        environmentId: planForm.environmentId || "",
        caseIds: selectedCaseIds,
      }),
    onSuccess: async (detail) => {
      setSelectedPlanId(detail.plan.id);
      setPlanForm(emptyPlanForm);
      setCreatePlanOpen(false);
      setActionMessage(t("tests.planCreated"));
      await navigate({ to: "/tests/plans/$planId", params: { planId: detail.plan.id }, search: testsTabSearch("plans", effectiveProjectId) });
      await invalidateCaseWorkflow();
    },
  });

  const startAdHocRun = useMutation({
    mutationFn: async () => {
      if (selectedReadyCaseIds.length === 0) {
        throw new Error(t("tests.readySelectedRequired"));
      }
      await workerReadiness.ensureReady();
      return controlPlaneApi.startAdHocProjectTestRun(auth.token, workspaceId, effectiveProjectId, {
        caseIds: selectedReadyCaseIds,
        environmentId: selectedProject?.defaultEnvironmentId || selectedProject?.defaultClusterId || "",
        runtimeMode: workerReadiness.runtimeMode,
      });
    },
    onSuccess: async (detail) => {
      setActionMessage(t("tests.adHocRunStarted"));
      await navigate({ to: "/tests/runs/$runId", params: { runId: detail.run.id }, search: testsTabSearch("runs", effectiveProjectId) });
      await invalidateCaseWorkflow();
    },
  });

  const startRun = useMutation({
    mutationFn: async (plan: TestPlan) => {
      await workerReadiness.ensureReady();
      return controlPlaneApi.startProjectTestRun(auth.token, workspaceId, effectiveProjectId, plan.id, {
        targetType: plan.targetType,
        targetValue: plan.targetValue,
        environment: plan.environment,
        environmentId: plan.environmentId,
        environmentKind: plan.environmentKind,
        runtimeMode: workerReadiness.runtimeMode,
      });
    },
    onSuccess: async (detail) => {
      if (detail.plan?.id) {
        setSelectedPlanId(detail.plan.id);
      }
      setActionMessage(t("tests.runStarted"));
      await navigate({ to: "/tests/runs/$runId", params: { runId: detail.run.id }, search: testsTabSearch("runs", effectiveProjectId) });
      await invalidateCaseWorkflow();
    },
  });

  const agentActionError = optimizeCases.error || generateCases.error || startAdHocRun.error || startRun.error;
  const agentActionMessage = agentActionError?.message || actionMessage;
  const agentActionMessageClass = agentActionError ? "text-[color:var(--danger)]" : "text-[color:var(--muted)]";
  const workflowStages = useMemo(
    () => [
      {
        key: "cases" as const,
        icon: ClipboardCheck,
        count: t("tests.workflow.caseCount", { count: allCases.length }),
        status: allCases.length > 0 ? ("ready" as const) : ("active" as const),
        statusLabel: allCases.length > 0 ? t("tests.workflow.caseReady") : t("tests.workflow.caseStart"),
        body: t("tests.workflow.caseBody"),
        dependency: allCases.length > 0 ? t("tests.workflow.caseDependencyDone") : t("tests.workflow.caseDependency"),
      },
      {
        key: "proposals" as const,
        icon: Sparkles,
        count: t("tests.workflow.suggestionCount", { count: pendingProposals.length }),
        status: pendingProposals.length > 0 ? ("waiting" as const) : allCases.length > 0 ? ("ready" as const) : ("blocked" as const),
        statusLabel:
          pendingProposals.length > 0
            ? t("tests.workflow.suggestionWaiting")
            : allCases.length > 0
              ? t("tests.workflow.suggestionReady")
              : t("tests.workflow.blocked"),
        body: t("tests.workflow.suggestionBody"),
        dependency: allCases.length > 0 ? t("tests.workflow.suggestionDependencyDone") : t("tests.workflow.suggestionDependency"),
      },
      {
        key: "plans" as const,
        icon: ListChecks,
        count: t("tests.workflow.planCount", { count: allPlans.length }),
        status: readyCases.length > 0 ? ("ready" as const) : ("blocked" as const),
        statusLabel: readyCases.length > 0 ? t("tests.workflow.planReady") : t("tests.workflow.blocked"),
        body: t("tests.workflow.planBody"),
        dependency: readyCases.length > 0 ? t("tests.workflow.planDependencyDone", { count: readyCases.length }) : t("tests.workflow.planDependency"),
      },
      {
        key: "runs" as const,
        icon: Play,
        count: latestRun ? t("tests.workflow.runLatest", { status: t(`tests.runStatusValue.${latestRun.status}`, { defaultValue: latestRun.status }) }) : t("tests.workflow.runCountEmpty"),
        status: latestRun?.status === "running" || latestRun?.status === "queued" ? ("active" as const) : readyCases.length > 0 ? ("ready" as const) : ("blocked" as const),
        statusLabel:
          latestRun?.status === "running" || latestRun?.status === "queued"
            ? t("tests.workflow.runActive")
            : readyCases.length > 0
              ? t("tests.workflow.runReady")
              : t("tests.workflow.blocked"),
        body: t("tests.workflow.runBody"),
        dependency: readyCases.length > 0 ? t("tests.workflow.runDependencyDone", { count: readyCases.length }) : t("tests.workflow.runDependency"),
      },
    ],
    [allCases.length, allPlans.length, latestRun, pendingProposals.length, readyCases.length, t],
  );
  const activeStage = workflowStages.find((stage) => stage.key === activeTab) || workflowStages[0];
  const workflowNextAction =
    pendingProposals.length > 0
      ? t("tests.workflow.nextReview", { count: pendingProposals.length })
      : readyCases.length > 0
        ? t("tests.workflow.nextRunOrPlan", { count: readyCases.length })
          : allCases.length > 0
            ? t("tests.workflow.nextOptimize")
            : t("tests.workflow.nextCases");

  useEffect(() => {
    if (search.project && projects.some((project) => project.id === search.project)) {
      setSelectedProjectId(search.project);
      return;
    }
    if (!selectedProjectId && projects[0]) {
      setSelectedProjectId(projects[0].id);
    }
  }, [projects, search.project, selectedProjectId]);

  useEffect(() => {
    setSelectedCaseIds((current) => {
      const next = current.filter((caseId) => allCases.some((testCase) => testCase.id === caseId));
      return next.length === current.length ? current : next;
    });
  }, [allCases]);

  useEffect(() => {
    if (!selectedPlanId && plans[0]) {
      setSelectedPlanId(plans[0].id);
    }
  }, [plans, selectedPlanId]);

  function toggleCaseSelection(caseId: string) {
    setSelectedCaseIds((current) => (current.includes(caseId) ? current.filter((id) => id !== caseId) : [...current, caseId]));
  }

  function selectReadyCases() {
    setSelectedCaseIds(readyCases.map((testCase) => testCase.id));
  }

  function updateCreateStep(index: number, patch: Partial<TestCaseStep>) {
    setCaseForm((current) => ({
      ...current,
      steps: current.steps.map((step, stepIndex) => (stepIndex === index ? { ...step, ...patch } : step)),
    }));
  }

  function openCreateCaseDialog() {
    setCaseForm(emptyCaseForm);
    setCreateCaseOpen(true);
    createCase.reset();
  }

  function closeCreateCaseDialog() {
    setCreateCaseOpen(false);
    createCase.reset();
  }

  function openCreatePlanDialog() {
    setPlanForm(emptyPlanForm);
    setCreatePlanOpen(true);
    createPlan.reset();
  }

  function closeCreatePlanDialog() {
    setCreatePlanOpen(false);
    createPlan.reset();
  }

  function closeImportDialog() {
    setImportOpen(false);
    setImportFileError("");
    importCases.reset();
  }

  function updateImportFormat(value: string) {
    setImportFormat(value);
    setImportContent("");
    setImportFile(null);
    setImportFileError("");
    importCases.reset();
  }

  function selectImportFile(file: File | undefined) {
    setImportFileError("");
    importCases.reset();
    if (!file) {
      setImportFile(null);
      return;
    }
    if (!file.name.toLowerCase().endsWith(".xlsx")) {
      setImportFile(null);
      setImportFileError(t("tests.importExcelInvalidFile"));
      return;
    }
    setImportFile(file);
  }

  function submitCreateCase(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canCreateCase) return;
    createCase.mutate(formToInput(caseForm));
  }

  if (!serverWorkspaceReady) {
    return (
      <PageFrame title={t("tests.title")} subtitle={t("workspace.signInRequired")}>
        <CollectionEmptyState title={t("workspace.notSignedIn")} body={t("workspace.signInRequired")} />
      </PageFrame>
    );
  }

  return (
    <PageFrame title={t("tests.title")} subtitle={t("tests.subtitle")}>
      {projects.length === 0 && !projectsQuery.isLoading ? (
        <CollectionEmptyState title={t("tests.noProjectTitle")} body={t("tests.noProjectBody")} />
      ) : (
        <div className="grid gap-4">
          <section className="grid gap-4 rounded-[10px] bg-[color:var(--surface)] p-4 shadow-[inset_0_0_0_1px_var(--line)]">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-[220px] flex-1">
                <div className="mb-2 text-[12px] font-medium text-[color:var(--muted)]">{t("tests.project")}</div>
                <Select
                  value={effectiveProjectId}
                  onValueChange={(value) => {
                    setSelectedProjectId(value);
                    setSelectedCaseIds([]);
                    setSelectedPlanId("");
                    setActionMessage("");
                    void navigate({ to: "/tests", search: testsTabSearch(activeTab, value) });
                  }}
                >
                  <SelectTrigger className={toolbarSelectClass} aria-label={t("tests.project")}>
                    <SelectValue placeholder={t("tests.project")} />
                  </SelectTrigger>
                  <SelectContent>
                    {projects.map((project: Project) => (
                      <SelectItem key={project.id} value={project.id}>
                        {project.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="max-w-[520px] text-left sm:text-right">
                <div className="text-[13px] font-semibold text-[color:var(--text)]">{t("tests.workflow.title")}</div>
                <p className="mt-1 text-pretty text-[12px] leading-5 text-[color:var(--muted)]">{workflowNextAction}</p>
              </div>
            </div>
            {actionMessage ? <p className="text-[12px] text-[color:var(--muted)]">{actionMessage}</p> : null}
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
              {workflowStages.map((stage, index) => (
                <WorkflowStageButton
                  key={stage.key}
                  index={index + 1}
                  icon={stage.icon}
                  title={t(`tests.tabs.${stage.key}`)}
                  count={stage.count}
                  status={stage.status}
                  statusLabel={stage.statusLabel}
                  body={stage.body}
                  dependency={stage.dependency}
                  active={activeTab === stage.key}
                  onClick={() => void navigate({ to: "/tests", search: testsTabSearch(stage.key, effectiveProjectId) })}
                />
              ))}
            </div>
          </section>

          {activeTab === "cases" ? (
            <div className="grid min-h-[620px] gap-5">
              <section className="min-w-0 rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
                <StageIntro
                  icon={activeStage.icon}
                  title={t("tests.stage.casesTitle")}
                  body={t("tests.stage.casesBody")}
                  meta={activeStage.dependency}
                />
                <div className="flex flex-wrap items-center gap-2 border-b border-[color:var(--line)] p-3">
                  <div className="relative min-w-[220px] flex-1">
                    <Search data-icon className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-[color:var(--faint)]" />
                    <Input
                      value={query}
                      onChange={(event) => setQuery(event.target.value)}
                      placeholder={t("tests.searchPlaceholder")}
                      className="h-8 min-h-8 pl-8 text-[12px]"
                    />
                  </div>
                  <Select value={statusFilter} onValueChange={setStatusFilter}>
                    <SelectTrigger className={cn(toolbarSelectClass, "w-[150px]")} aria-label={t("tests.status")}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">{t("tests.allStatus")}</SelectItem>
                      {statusOptions.map((status) => (
                        <SelectItem key={status} value={status}>
                          {t(`tests.statusValue.${status}`)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Button type="button" variant="secondary" onClick={() => setImportOpen(true)} disabled={!effectiveProjectId}>
                    <FileUp data-icon />
                    {t("tests.importCases")}
                  </Button>
                  <Button type="button" variant="secondary" onClick={() => optimizeCases.mutate()} disabled={selectedCaseIds.length === 0 || optimizeCases.isPending}>
                    <Sparkles data-icon />
                    {optimizeCases.isPending ? t("tests.optimizing") : t("tests.optimize")}
                  </Button>
                  <Button type="button" variant="secondary" onClick={() => startAdHocRun.mutate()} disabled={selectedReadyCaseIds.length === 0 || startAdHocRun.isPending}>
                    <Play data-icon />
                    {startAdHocRun.isPending ? t("tests.startingAdHocRun") : t("tests.runSelected")}
                  </Button>
                  <Button type="button" onClick={openCreateCaseDialog} disabled={!effectiveProjectId}>
                    <Plus data-icon />
                    {t("tests.newCase")}
                  </Button>
                </div>
                {agentActionMessage ? (
                  <p className={cn("border-b border-[color:var(--line)] bg-[color:var(--paper)] px-4 py-2 text-[12px]", agentActionMessageClass)}>
                    {agentActionMessage}
                  </p>
                ) : null}

                <div className="flex items-center justify-between border-b border-[color:var(--line)] px-4 py-2 text-[12px] text-[color:var(--muted)]">
                  <span>{t("tests.selectedSummary", { selected: selectedCaseIds.length, total: cases.length })}</span>
                  <div className="flex items-center gap-2">
                    <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-[12px]" onClick={selectReadyCases} disabled={readyCases.length === 0}>
                      {t("tests.selectReady")}
                    </Button>
                    <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-[12px]" onClick={() => setSelectedCaseIds([])} disabled={selectedCaseIds.length === 0}>
                      {t("tests.clearSelection")}
                    </Button>
                  </div>
                </div>

                {casesQuery.isLoading ? (
                  <div className="divide-y divide-[color:var(--line)]">
                    <TestsLoadingRows label={t("tests.loading")} />
                  </div>
                ) : cases.length === 0 ? (
                  <TestsPanelState
                    icon={ClipboardCheck}
                    title={query || statusFilter !== "all" ? t("tests.noMatch") : t("tests.emptyTitle")}
                    body={query || statusFilter !== "all" ? undefined : t("tests.emptyBody")}
                  />
                ) : (
                  <div className="overflow-x-auto">
                    <div className="min-w-[940px]">
                      <div className="grid grid-cols-[24px_minmax(260px,1fr)_96px_120px_88px_132px_108px_88px_24px] items-center gap-3 border-b border-[color:var(--line)] px-4 py-2 text-[11px] font-medium text-[color:var(--muted)]">
                        <span className="sr-only">{t("tests.selectedSummary", { selected: selectedCaseIds.length, total: cases.length })}</span>
                        <span aria-hidden="true" />
                        <span>{t("tests.titleLabel")}</span>
                        <span className="text-right">{t("tests.type")}</span>
                        <span className="text-right">{t("tests.status")}</span>
                        <span className="text-right">{t("tests.priority")}</span>
                        <span className="text-right">{t("tests.quality")}</span>
                        <span className="text-right">{t("tests.latestResult")}</span>
                        <span className="text-right">{t("tests.updated")}</span>
                        <span className="sr-only">{t("tests.selectCase")}</span>
                      </div>
                      <div className="divide-y divide-[color:var(--line)]">
                        {cases.map((testCase) => {
                          const executability = testCaseExecutability(testCase);
                          return (
                            <div
                              key={testCase.id}
                              className="grid w-full grid-cols-[24px_minmax(260px,1fr)_96px_120px_88px_132px_108px_88px_24px] items-center gap-3 px-4 py-3 transition-colors hover:bg-[color:var(--hover)]"
                            >
                              <input
                                type="checkbox"
                                className="size-4 accent-[color:var(--accent)]"
                                checked={selectedCaseIds.includes(testCase.id)}
                                aria-label={t("tests.selectCaseAria", { title: testCase.title })}
                                onChange={() => toggleCaseSelection(testCase.id)}
                              />
                              <Link
                                to="/tests/cases/$caseId"
                                params={{ caseId: testCase.id }}
                                search={testsTabSearch("cases", effectiveProjectId)}
                                className="group min-w-0 text-left"
                              >
                                <div className="flex min-w-0 items-center gap-2">
                                  <ClipboardCheck data-icon className="size-4 shrink-0 text-[color:var(--muted)]" />
                                  <span className="truncate text-[13px] font-medium text-[color:var(--text)]">{testCase.title}</span>
                                </div>
                                <div className="mt-1 truncate text-[12px] text-[color:var(--muted)]">{testCase.area || t("common.unknown")}</div>
                              </Link>
                              <div className="truncate text-right text-[12px] text-[color:var(--muted-strong)]">
                                {testCaseTypeLabel(testCase.type, t)}
                              </div>
                              <div className="flex items-center justify-end">
                                <StatusBadge value={testCase.status} valueLabel={t(`tests.statusValue.${testCase.status}`, { defaultValue: testCase.status })} />
                              </div>
                              <div className={cn("text-right text-[12px] font-medium", testCase.priority ? "text-[color:var(--text)]" : "text-[color:var(--muted)]")}>
                                {testCase.priority ? testCase.priority.toUpperCase() : t("tests.noPriority")}
                              </div>
                              <div className="grid justify-items-end gap-0.5">
                                <span className={cn("text-[13px] font-semibold tabular-nums", executabilityTone(executability.done, executability.total, executability.issues))}>
                                  {executability.done}/{executability.total}
                                </span>
                                <span className="text-[11px] text-[color:var(--muted)]">{executabilityIssueLabel(executability.issues, t)}</span>
                              </div>
                              <div className="min-w-0 text-right text-[12px]">
                                {testCase.latestResult?.runId ? (
                                  <Link
                                    to="/tests/cases/$caseId"
                                    params={{ caseId: testCase.id }}
                                    search={testCaseDetailSearch(effectiveProjectId, "runs", {
                                      runId: testCase.latestResult.runId,
                                      itemId: testCase.latestResult.itemId,
                                    })}
                                    className={cn("font-medium hover:underline", latestResultTone(testCase.latestResult))}
                                  >
                                    {latestResultLabel(testCase.latestResult, t)}
                                  </Link>
                                ) : (
                                  <span className={latestResultTone(undefined)}>{t("tests.notRun")}</span>
                                )}
                              </div>
                              <div className="text-right text-[12px] text-[color:var(--muted)]">
                                <RelativeTime value={testCase.updatedAt} />
                              </div>
                              <div className="flex items-center justify-end">
                                <ArrowRight data-icon className="text-[color:var(--faint)]" />
                              </div>
                            </div>
                          );
                        })}
                      </div>
                    </div>
                  </div>
                )}
              </section>
            </div>
          ) : null}

          {activeTab === "proposals" ? (
            <div className="grid gap-5">
              <section className="min-w-0 rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
                <StageIntro
                  icon={activeStage.icon}
                  title={t("tests.stage.proposalsTitle")}
                  body={t("tests.stage.proposalsBody")}
                  meta={activeStage.dependency}
                >
                  <Button type="button" variant="secondary" onClick={() => optimizeCases.mutate()} disabled={selectedCaseIds.length === 0 || optimizeCases.isPending}>
                    <Sparkles data-icon />
                    {optimizeCases.isPending ? t("tests.optimizing") : t("tests.optimizeSelected")}
                  </Button>
                  <Button type="button" onClick={() => generateCases.mutate()} disabled={generateCases.isPending || !effectiveProjectId}>
                    <Sparkles data-icon />
                    {generateCases.isPending ? t("tests.generating") : t("tests.generate")}
                  </Button>
                </StageIntro>
                <div className="flex flex-wrap items-center justify-between gap-2 border-b border-[color:var(--line)] p-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <Select value={proposalStatusFilter} onValueChange={setProposalStatusFilter}>
                      <SelectTrigger className={cn(toolbarSelectClass, "w-[150px]")} aria-label={t("tests.proposalStatus")}>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="all">{t("tests.allProposalStatus")}</SelectItem>
                        {proposalStatusOptions.map((status) => (
                          <SelectItem key={status} value={status}>
                            {t(`tests.proposalStatusValue.${status}`)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <span className="text-[12px] text-[color:var(--muted)]">
                    {t("tests.selectedSummary", { selected: selectedCaseIds.length, total: casesQuery.data?.length || 0 })}
                  </span>
                </div>
                {agentActionMessage ? (
                  <p className={cn("border-b border-[color:var(--line)] bg-[color:var(--paper)] px-4 py-2 text-[12px]", agentActionMessageClass)}>
                    {agentActionMessage}
                  </p>
                ) : null}
                <div className="divide-y divide-[color:var(--line)]">
                  {proposalsQuery.isLoading ? (
                    <TestsLoadingRows label={t("tests.loadingProposals")} />
                  ) : proposals.length === 0 ? (
                    <TestsPanelState icon={Sparkles} title={t("tests.noProposalsTitle")} body={t("tests.noProposalsBody")} />
                  ) : (
                    proposals.map((proposal) => (
                      <article key={proposal.id} className="grid gap-3 p-4">
                        <div className="flex flex-wrap items-start justify-between gap-3">
                          <div className="min-w-0">
                            <div className="flex min-w-0 items-center gap-2">
                              <Sparkles data-icon className="size-4 shrink-0 text-[color:var(--muted)]" />
                              <h2 className="truncate text-[13px] font-semibold text-[color:var(--text)]">{proposal.title || proposal.proposedCase.title}</h2>
                            </div>
                            <p className="mt-1 text-[12px] leading-5 text-[color:var(--muted)]">{proposal.summary || proposal.rationale || t("tests.noProposalSummary")}</p>
                          </div>
                          <StatusBadge value={proposal.status} valueLabel={t(`tests.proposalStatusValue.${proposal.status}`, { defaultValue: proposal.status })} />
                        </div>
                        <div className="grid gap-3">
                          <ProposalCasePreview title={t("tests.currentCase")} testCase={proposal.currentCase} emptyText={t("tests.newCaseProposal")} />
                          <ProposalCasePreview title={t("tests.proposedCase")} input={proposal.proposedCase} emptyText={t("tests.noProposedCase")} />
                        </div>
                        {proposal.validationErrors.length > 0 ? (
                          <div className="rounded-[8px] bg-[color:var(--danger-soft)] px-3 py-2 text-[12px] leading-5 text-[color:var(--danger)]">
                            {proposal.validationErrors.join("; ")}
                          </div>
                        ) : null}
                        <div className="flex flex-wrap items-center justify-between gap-2">
                          <div className="text-[12px] text-[color:var(--muted)]">
                            {t("tests.quality")}: <span className={cn("font-semibold", scoreTone(proposal.qualityScore))}>{proposal.qualityScore}</span>
                          </div>
                          <div className="flex flex-wrap items-center gap-2">
                            <Button
                              type="button"
                              variant="secondary"
                              onClick={() => rejectProposal.mutate(proposal)}
                              disabled={proposal.status !== "pending" || rejectProposal.isPending}
                            >
                              <Ban data-icon />
                              {t("tests.rejectProposal")}
                            </Button>
                            <Button
                              type="button"
                              onClick={() => applyProposal.mutate(proposal)}
                              disabled={proposal.status !== "pending" || proposal.validationErrors.length > 0 || applyProposal.isPending}
                            >
                              <Check data-icon />
                              {t("tests.applyProposal")}
                            </Button>
                          </div>
                        </div>
                      </article>
                    ))
                  )}
                </div>
              </section>
              {(optimizeCases.error || generateCases.error || applyProposal.error || rejectProposal.error) ? (
                <p className="text-[12px] text-[color:var(--danger)]">
                  {(optimizeCases.error || generateCases.error || applyProposal.error || rejectProposal.error)?.message}
                </p>
              ) : null}
            </div>
          ) : null}

          {activeTab === "plans" ? (
            <div className="grid gap-5">
              <section className="min-w-0 rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
                <StageIntro
                  icon={activeStage.icon}
                  title={t("tests.stage.plansTitle")}
                  body={t("tests.stage.plansBody")}
                  meta={activeStage.dependency}
                />
                <div className="flex flex-wrap items-center justify-between gap-2 border-b border-[color:var(--line)] p-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <Select value={planStatusFilter} onValueChange={setPlanStatusFilter}>
                      <SelectTrigger className={cn(toolbarSelectClass, "w-[150px]")} aria-label={t("tests.planStatus")}>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="all">{t("tests.allPlanStatus")}</SelectItem>
                        {planStatusOptions.map((status) => (
                          <SelectItem key={status} value={status}>
                            {t(`tests.planStatusValue.${status}`)}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <Button type="button" onClick={openCreatePlanDialog} disabled={!effectiveProjectId}>
                      <Plus data-icon />
                      {t("tests.createPlan")}
                    </Button>
                  </div>
                  <span className="text-[12px] text-[color:var(--muted)]">
                    {t("tests.selectedSummary", { selected: selectedCaseIds.length, total: readyCases.length })}
                  </span>
                </div>
                <div className="divide-y divide-[color:var(--line)]">
                  {plansQuery.isLoading ? (
                    <TestsLoadingRows label={t("tests.loadingPlans")} />
                  ) : plans.length === 0 ? (
                    <TestsPanelState icon={ListChecks} title={t("tests.noPlansTitle")} body={t("tests.noPlansBody")} />
                  ) : (
                    plans.map((plan) => (
                      <Link
                        key={plan.id}
                        to="/tests/plans/$planId"
                        params={{ planId: plan.id }}
                        search={testsTabSearch("plans", effectiveProjectId)}
                        className="grid w-full gap-3 px-4 py-3 text-left transition-colors hover:bg-[color:var(--hover)] md:grid-cols-[minmax(0,1fr)_120px_88px_24px]"
                      >
                        <div className="min-w-0">
                          <div className="flex min-w-0 items-center gap-2">
                            <ListChecks data-icon className="size-4 shrink-0 text-[color:var(--muted)]" />
                            <span className="truncate text-[13px] font-medium text-[color:var(--text)]">{plan.title}</span>
                          </div>
                          <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[12px] text-[color:var(--muted)]">
                            <span>{t(`tests.targetTypeValue.${plan.targetType}`, { defaultValue: plan.targetType })}</span>
                            <span>{plan.targetValue || t("common.unknown")}</span>
                            <span>{t("tests.caseCount", { count: plan.caseCount })}</span>
                          </div>
                        </div>
                        <div className="flex items-center md:justify-end">
                          <StatusBadge value={plan.status} valueLabel={t(`tests.planStatusValue.${plan.status}`, { defaultValue: plan.status })} />
                        </div>
                        <div className="flex items-center text-[12px] text-[color:var(--muted)] md:justify-end">
                          <RelativeTime value={plan.updatedAt} />
                        </div>
                        <div className="hidden items-center justify-end md:flex">
                          <ArrowRight data-icon className="text-[color:var(--faint)]" />
                        </div>
                      </Link>
                    ))
                  )}
                </div>
              </section>
            </div>
          ) : null}

          {activeTab === "runs" ? (
            <div className="grid gap-5">
              <section className="min-w-0 rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
                <StageIntro
                  icon={activeStage.icon}
                  title={t("tests.stage.runsTitle")}
                  body={t("tests.stage.runsBody")}
                  meta={activeStage.dependency}
                />
                <div className="flex flex-wrap items-start justify-between gap-3 border-b border-[color:var(--line)] p-3">
                  <div>
                    <h2 className="text-[13px] font-semibold text-[color:var(--text)]">{t("tests.runs")}</h2>
                    <p className="mt-1 text-[12px] text-[color:var(--muted)]">{t("tests.runSelectedDescription")}</p>
                  </div>
                  <div className="flex flex-wrap items-center justify-end gap-2">
                    <Button type="button" variant="secondary" onClick={() => startAdHocRun.mutate()} disabled={selectedReadyCaseIds.length === 0 || startAdHocRun.isPending}>
                      <Play data-icon />
                      {startAdHocRun.isPending ? t("tests.startingAdHocRun") : t("tests.runSelected")}
                    </Button>
                    <Select value={selectedPlanId || "__none"} onValueChange={(value) => setSelectedPlanId(value === "__none" ? "" : value)}>
                      <SelectTrigger className={cn(toolbarSelectClass, "w-[240px]")} aria-label={t("tests.selectedPlan")}>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="__none">{t("tests.selectPlan")}</SelectItem>
                        {plans.map((plan) => (
                          <SelectItem key={plan.id} value={plan.id}>
                            {plan.title}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <Button type="button" onClick={() => selectedPlan && startRun.mutate(selectedPlan)} disabled={!selectedPlan || startRun.isPending}>
                      <ListChecks data-icon />
                      {startRun.isPending ? t("tests.startingRun") : t("tests.startRun")}
                    </Button>
                  </div>
                </div>
                <div className="divide-y divide-[color:var(--line)]">
                  {allRunsQuery.isLoading ? (
                    <TestsLoadingRows label={t("tests.loadingRuns")} />
                  ) : allRuns.length === 0 ? (
                    <TestsPanelState icon={Play} title={t("tests.noRunsTitle")} body={t("tests.noRunsBody")} />
                  ) : (
                    allRuns.map((run) => (
                      <Link
                        key={run.id}
                        to="/tests/runs/$runId"
                        params={{ runId: run.id }}
                        search={testsTabSearch("runs", effectiveProjectId)}
                        className="grid w-full gap-3 px-4 py-3 text-left transition-colors hover:bg-[color:var(--hover)] md:grid-cols-[minmax(0,1fr)_220px_24px]"
                      >
                        <div className="min-w-0">
                          <div className="flex items-center gap-2">
                            <Play data-icon className="size-4 shrink-0 text-[color:var(--muted)]" />
                            <span className="text-[13px] font-medium text-[color:var(--text)]">{t("tests.runShortId", { id: run.id.slice(0, 8) })}</span>
                            <span className="text-[12px] text-[color:var(--muted)]">{t(`tests.runSourceValue.${run.source || "ad_hoc"}`, { defaultValue: run.source || "ad_hoc" })}</span>
                            <StatusBadge value={run.status} valueLabel={t(`tests.runStatusValue.${run.status}`, { defaultValue: run.status })} />
                          </div>
                          <div className="mt-1 text-[12px] text-[color:var(--muted)]">
                            {t("tests.runCounts", { passed: run.passedCount, failed: run.failedCount, blocked: run.blockedCount, skipped: run.skippedCount })}
                          </div>
                        </div>
                        <div className="flex items-center text-[12px] text-[color:var(--muted)] md:justify-end">
                          <RelativeTime value={run.updatedAt} />
                        </div>
                        <div className="hidden items-center justify-end md:flex">
                          <ArrowRight data-icon className="text-[color:var(--faint)]" />
                        </div>
                      </Link>
                    ))
                  )}
                </div>
              </section>
            </div>
          ) : null}

          {importOpen ? (
            <TestsModal title={t("tests.importDialogTitle")} description={t("tests.importDescription")} onClose={closeImportDialog}>
              <form
                className="grid min-w-0 gap-4"
                onSubmit={(event) => {
                  event.preventDefault();
                  if (!canImportCases) return;
                  importCases.mutate();
                }}
              >
                <Field label={t("tests.importFormat")}>
                  <Select value={importFormat} onValueChange={updateImportFormat}>
                    <SelectTrigger className="h-9">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="markdown">Markdown</SelectItem>
                      <SelectItem value="text">Text</SelectItem>
                      <SelectItem value="csv">CSV</SelectItem>
                      <SelectItem value="xlsx">{t("tests.importExcelFormat")}</SelectItem>
                    </SelectContent>
                  </Select>
                </Field>
                {isExcelImport ? (
                  <Field label={t("tests.importExcelFile")}>
                    <div className="grid gap-2">
                      <Input
                        type="file"
                        accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
                        onChange={(event) => selectImportFile(event.target.files?.[0])}
                      />
                      {importFile ? (
                        <p className="text-[12px] text-[color:var(--muted)]">{t("tests.importExcelSelected", { name: importFile.name })}</p>
                      ) : null}
                      {importFileError ? <p className="text-[12px] text-[color:var(--danger)]">{importFileError}</p> : null}
                    </div>
                  </Field>
                ) : (
                  <Field label={t("tests.importContent")} className="min-w-0">
                    <Textarea
                      value={importContent}
                      onChange={(event) => setImportContent(event.target.value)}
                      placeholder={t("tests.importPlaceholder")}
                      className="field-sizing-fixed min-h-44 min-w-0 max-w-full overflow-x-auto whitespace-pre"
                      wrap="off"
                    />
                  </Field>
                )}
                <div className="min-w-0 rounded-[8px] bg-[color:var(--paper)] px-3 py-2 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
                  {isExcelImport ? t("tests.importExcelHint") : t("tests.importFormatHint")}
                </div>
                {importSummary ? <p className="text-[12px] text-[color:var(--muted)]">{importSummary}</p> : null}
                {importCases.error ? <p className="text-[12px] text-[color:var(--danger)]">{importCases.error.message}</p> : null}
                <div className="flex flex-wrap items-center justify-end gap-2 border-t border-[color:var(--line)] pt-3">
                  <Button type="button" variant="secondary" onClick={closeImportDialog}>
                    {t("common.cancel")}
                  </Button>
                  <Button type="submit" disabled={importCases.isPending || !canImportCases}>
                    <FileUp data-icon />
                    {importCases.isPending ? t("tests.importing") : t("tests.importCases")}
                  </Button>
                </div>
              </form>
            </TestsModal>
          ) : null}

          {createCaseOpen ? (
            <TestsModal title={t("tests.newCaseDialogTitle")} description={t("tests.newCaseDialogDescription")} onClose={closeCreateCaseDialog} wide>
              <form className="grid gap-4" onSubmit={submitCreateCase}>
                <CaseFormFields
                  form={caseForm}
                  onChange={setCaseForm}
                  onStepChange={updateCreateStep}
                />
                {createCase.error ? <p className="text-[12px] text-[color:var(--danger)]">{createCase.error.message}</p> : null}
                <div className="flex flex-wrap items-center justify-end gap-2 border-t border-[color:var(--line)] pt-3">
                  <Button type="button" variant="secondary" onClick={closeCreateCaseDialog}>
                    {t("common.cancel")}
                  </Button>
                  <Button type="submit" disabled={!canCreateCase || createCase.isPending}>
                    <Save data-icon />
                    {createCase.isPending ? t("tests.saving") : t("tests.createCase")}
                  </Button>
                </div>
              </form>
            </TestsModal>
          ) : null}

          {createPlanOpen ? (
            <TestsModal title={t("tests.createPlan")} description={t("tests.createPlanDescription")} onClose={closeCreatePlanDialog} wide>
              <form
                className="grid gap-4"
                onSubmit={(event) => {
                  event.preventDefault();
                  if (!canCreatePlan) return;
                  createPlan.mutate();
                }}
              >
                <PlanFormFields
                  form={planForm}
                  onChange={setPlanForm}
                  environments={environments}
                  readyCases={readyCases}
                  selectedCaseIds={selectedCaseIds}
                  onCaseToggle={toggleCaseSelection}
                  onSelectReadyCases={selectReadyCases}
                />
                {createPlan.error ? <p className="text-[12px] text-[color:var(--danger)]">{createPlan.error.message}</p> : null}
                <div className="flex flex-wrap items-center justify-end gap-2 border-t border-[color:var(--line)] pt-3">
                  <Button type="button" variant="secondary" onClick={closeCreatePlanDialog}>
                    {t("common.cancel")}
                  </Button>
                  <Button type="submit" disabled={createPlan.isPending || !canCreatePlan}>
                    <Plus data-icon />
                    {createPlan.isPending ? t("tests.creatingPlan") : t("tests.createPlan")}
                  </Button>
                </div>
              </form>
            </TestsModal>
          ) : null}
        </div>
      )}
    </PageFrame>
  );
}

export function TestCaseDetailPage() {
  const { t } = useMspaceTranslation();
  const navigate = useNavigate();
  const search = useTestsSearch();
  const { caseId = "" } = useParams({ strict: false }) as { caseId?: string };
  const isNew = caseId === "new";
  const queryClient = useQueryClient();
  const { auth, workspaceId, serverWorkspaceReady, projectsQuery, projects } = useWorkspaceProjects();
  const selectedProject = projectFromSearch(projects, search.project);
  const effectiveProjectId = selectedProject?.id || "";
  const [caseForm, setCaseForm] = useState<CaseForm>(emptyCaseForm);
  const [actionMessage, setActionMessage] = useState("");
  const workerReadiness = useTestsWorkerReadiness(auth, workspaceId, setActionMessage);

  const caseQuery = useQuery({
    queryKey: queryKeys.projectTestCase(workspaceId, effectiveProjectId, caseId || "__none", auth.token),
    queryFn: () => controlPlaneApi.getProjectTestCase(auth.token, workspaceId, effectiveProjectId, caseId),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId && caseId && !isNew),
  });
  const revisionsQuery = useQuery({
    queryKey: queryKeys.projectTestCaseRevisions(workspaceId, effectiveProjectId, caseId || "__none", auth.token),
    queryFn: () => controlPlaneApi.listProjectTestCaseRevisions(auth.token, workspaceId, effectiveProjectId, caseId),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId && caseId && !isNew),
  });
  const testCase = useMemo(() => (caseQuery.data ? normalizeTestCaseForView(caseQuery.data) : undefined), [caseQuery.data]);
  const revisions = revisionsQuery.data || emptyTestCaseRevisions;
  const revisionTimeline = useMemo(() => buildTestCaseRevisionTimeline(revisions, t), [revisions, t]);
  const activeCaseTab = !isNew && caseDetailTabs.includes(search.caseTab as TestCaseDetailTab) ? (search.caseTab as TestCaseDetailTab) : "details";
  const runHistory = useTestCaseRunHistory({
    token: auth.token,
    workspaceId,
    projectId: effectiveProjectId,
    caseId,
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId && caseId && !isNew),
    latestResult: testCase?.latestResult,
  });
  const canSave = Boolean(effectiveProjectId && caseForm.title.trim());
  const canRunCase = Boolean(testCase && testCase.status === "ready");

  const createCase = useMutation({
    mutationFn: (input: TestCaseInput) => controlPlaneApi.createProjectTestCase(auth.token, workspaceId, effectiveProjectId, input),
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.projectTestCases(workspaceId, effectiveProjectId, auth.token) });
      await navigate({ to: "/tests/cases/$caseId", params: { caseId: created.id }, search: testCaseDetailSearch(effectiveProjectId, "details") });
    },
  });
  const updateCase = useMutation({
    mutationFn: (input: TestCaseInput) => controlPlaneApi.updateProjectTestCase(auth.token, workspaceId, effectiveProjectId, caseId, input),
    onSuccess: async (updated) => {
      setCaseForm(caseToForm(updated));
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.projectTestCases(workspaceId, effectiveProjectId, auth.token) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.projectTestCase(workspaceId, effectiveProjectId, updated.id, auth.token) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.projectTestCaseRevisions(workspaceId, effectiveProjectId, updated.id, auth.token) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.projectTestCaseRunItems(workspaceId, effectiveProjectId, updated.id, auth.token) }),
      ]);
    },
  });
  const startCaseRun = useMutation({
    mutationFn: async () => {
      if (!testCase || testCase.status !== "ready") {
        throw new Error(t("tests.readyCaseRequired"));
      }
      await workerReadiness.ensureReady();
      return controlPlaneApi.startAdHocProjectTestRun(auth.token, workspaceId, effectiveProjectId, {
        caseIds: [testCase.id],
        environmentId: selectedProject?.defaultEnvironmentId || selectedProject?.defaultClusterId || "",
        runtimeMode: workerReadiness.runtimeMode,
      });
    },
    onSuccess: async (detail) => {
      const runningCaseId = testCase?.id || detail.items[0]?.testCaseId || caseId;
      setActionMessage(t("tests.adHocRunStarted"));
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.projectTestRuns(workspaceId, effectiveProjectId, auth.token) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.projectTestCase(workspaceId, effectiveProjectId, runningCaseId, auth.token) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.projectTestCaseRunItems(workspaceId, effectiveProjectId, runningCaseId, auth.token) }),
        navigate({
          to: "/tests/cases/$caseId",
          params: { caseId: runningCaseId },
          search: testCaseDetailSearch(effectiveProjectId, "runs", { runId: detail.run.id, itemId: detail.items[0]?.id }),
        }),
      ]);
    },
  });
  const savePending = createCase.isPending || updateCase.isPending;

  useEffect(() => {
    if (isNew) {
      setCaseForm(emptyCaseForm);
      return;
    }
    if (testCase) {
      setCaseForm(caseToForm(testCase));
    }
  }, [testCase, isNew]);

  function updateStep(index: number, patch: Partial<TestCaseStep>) {
    setCaseForm((current) => ({
      ...current,
      steps: current.steps.map((step, stepIndex) => (stepIndex === index ? { ...step, ...patch } : step)),
    }));
  }

  function submitCase(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canSave) return;
    const input = formToInput(caseForm);
    if (isNew) {
      createCase.mutate(input);
    } else {
      updateCase.mutate(input);
    }
  }

  if (!serverWorkspaceReady) {
    return (
      <PageFrame title={t("tests.title")} subtitle={t("workspace.signInRequired")}>
        <CollectionEmptyState title={t("workspace.notSignedIn")} body={t("workspace.signInRequired")} />
      </PageFrame>
    );
  }

  if (!selectedProject && !projectsQuery.isLoading) {
    return (
      <PageFrame title={t("tests.title")} subtitle={t("tests.subtitle")}>
        <CollectionEmptyState title={t("tests.noProjectTitle")} body={t("tests.noProjectBody")} />
      </PageFrame>
    );
  }

  const caseDetailError = caseQuery.error || revisionsQuery.error;
  const caseDetailFormId = "test-case-detail-form";
  const caseTitle = isNew ? t("tests.newCase") : testCase?.title || t("tests.cases");

  if (!isNew && !testCase) {
    return (
      <TestDetailUnavailableState
        title={t("tests.cases")}
        body={caseQuery.isPending || revisionsQuery.isPending ? t("tests.loading") : t("tests.selectCase")}
        error={caseDetailError}
        projectId={effectiveProjectId}
        tab="cases"
      />
    );
  }

  return (
    <PageFrame
      title={caseTitle}
      subtitle={<CaseHeaderMeta projectName={selectedProject?.name || t("tests.project")} testCase={testCase} />}
      breadcrumbs={[
        { label: t("common.mspace"), to: "/inbox" },
        { label: t("tests.title"), to: "/tests", search: testsTabSearch("cases", effectiveProjectId) },
        { label: caseTitle },
      ]}
      actions={
        <div className="flex flex-wrap items-center justify-end gap-2">
          {!isNew ? (
            <Button type="button" variant="secondary" onClick={() => startCaseRun.mutate()} disabled={!canRunCase || startCaseRun.isPending}>
              <Play data-icon />
              {startCaseRun.isPending ? t("tests.startingAdHocRun") : t("tests.runCase")}
            </Button>
          ) : null}
          {isNew || activeCaseTab === "details" ? (
            <Button type="submit" form={caseDetailFormId} disabled={!canSave || savePending}>
              <Save data-icon />
              {savePending ? t("tests.saving") : isNew ? t("tests.createCase") : t("tests.saveCase")}
            </Button>
          ) : null}
        </div>
      }
    >
      {!isNew && testCase ? (
        <CaseDetailTabs activeTab={activeCaseTab} projectId={effectiveProjectId} caseId={testCase.id} />
      ) : null}
      <form id={caseDetailFormId} className="grid gap-5" onSubmit={submitCase}>
        {startCaseRun.error ? (
          <p className="text-[12px] text-[color:var(--danger)]">{startCaseRun.error.message}</p>
        ) : actionMessage ? (
          <p className="text-[12px] text-[color:var(--muted)]">{actionMessage}</p>
        ) : null}

        {isNew || activeCaseTab === "details" ? (
          <CaseDetailsTab
            form={caseForm}
            testCase={testCase}
            onChange={setCaseForm}
            onStepChange={updateStep}
          />
        ) : null}

        {!isNew && testCase && activeCaseTab === "runs" ? (
          <CaseRunHistoryTab
            testCase={testCase}
            projectId={effectiveProjectId}
            entries={runHistory.entries}
            queryPending={runHistory.query.isPending}
            queryError={runHistory.query.error}
            focusedRunId={search.run}
            focusedItemId={search.item}
          />
        ) : null}

        {!isNew && activeCaseTab === "revisions" ? (
          <CaseRevisionHistoryTab
            revisions={revisions}
            revisionTimeline={revisionTimeline}
            error={revisionsQuery.error}
          />
        ) : null}

        {createCase.error || updateCase.error ? (
          <p className="text-[12px] text-[color:var(--danger)]">{(createCase.error || updateCase.error)?.message}</p>
        ) : null}
      </form>
    </PageFrame>
  );
}

function CaseHeaderMeta(props: { projectName: string; testCase?: TestCase }) {
  const { t } = useMspaceTranslation();
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-2 text-[12px] leading-5 text-[color:var(--muted)]">
      <span className="max-w-[28rem] truncate text-[14px] leading-6">{props.projectName}</span>
      {props.testCase ? (
        <>
          <span aria-hidden="true" className="text-[color:var(--faint)]">/</span>
          <span>
            {t("tests.quality")}: <span className={cn("font-semibold", scoreTone(props.testCase.qualityScore))}>{props.testCase.qualityScore}</span>
          </span>
          <StatusBadge value={props.testCase.status} valueLabel={testCaseStatusLabel(props.testCase.status, t)} />
          <span className="rounded-[6px] bg-[color:var(--surface)] px-2 py-0.5 text-[11px] font-medium text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]">
            {testCaseTypeLabel(props.testCase.type, t)}
          </span>
        </>
      ) : null}
    </div>
  );
}

export function TestPlanDetailPage() {
  const { t } = useMspaceTranslation();
  const navigate = useNavigate();
  const search = useTestsSearch();
  const { planId = "" } = useParams({ strict: false }) as { planId?: string };
  const queryClient = useQueryClient();
  const { auth, workspaceId, serverWorkspaceReady, projectsQuery, projects } = useWorkspaceProjects();
  const selectedProject = projectFromSearch(projects, search.project);
  const effectiveProjectId = selectedProject?.id || "";
  const [actionMessage, setActionMessage] = useState("");
  const workerReadiness = useTestsWorkerReadiness(auth, workspaceId, setActionMessage);
  const planQuery = useQuery({
    queryKey: queryKeys.projectTestPlan(workspaceId, effectiveProjectId, planId || "__none", auth.token),
    queryFn: () => controlPlaneApi.getProjectTestPlan(auth.token, workspaceId, effectiveProjectId, planId),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId && planId),
  });
  const detail = planQuery.data;
  const startRun = useMutation({
    mutationFn: async (plan: TestPlan) => {
      await workerReadiness.ensureReady();
      return controlPlaneApi.startProjectTestRun(auth.token, workspaceId, effectiveProjectId, plan.id, {
        targetType: plan.targetType,
        targetValue: plan.targetValue,
        environment: plan.environment,
        environmentId: plan.environmentId,
        environmentKind: plan.environmentKind,
        runtimeMode: workerReadiness.runtimeMode,
      });
    },
    onSuccess: async (runDetail) => {
      setActionMessage(t("tests.runStarted"));
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.projectTestPlans(workspaceId, effectiveProjectId, auth.token) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.projectTestPlan(workspaceId, effectiveProjectId, planId, auth.token) }),
      ]);
      await navigate({ to: "/tests/runs/$runId", params: { runId: runDetail.run.id }, search: testsTabSearch("runs", effectiveProjectId) });
    },
  });

  if (!serverWorkspaceReady) {
    return (
      <PageFrame title={t("tests.title")} subtitle={t("workspace.signInRequired")}>
        <CollectionEmptyState title={t("workspace.notSignedIn")} body={t("workspace.signInRequired")} />
      </PageFrame>
    );
  }

  if (!selectedProject && !projectsQuery.isLoading) {
    return (
      <PageFrame title={t("tests.title")} subtitle={t("tests.subtitle")}>
        <CollectionEmptyState title={t("tests.noProjectTitle")} body={t("tests.noProjectBody")} />
      </PageFrame>
    );
  }

  if (!detail) {
    return (
      <TestDetailUnavailableState
        title={t("tests.selectedPlan")}
        body={planQuery.isPending ? t("tests.loadingPlans") : t("tests.selectPlan")}
        error={planQuery.error}
        projectId={effectiveProjectId}
        tab="plans"
      />
    );
  }

  return (
    <TestPlanDetailContent
      detail={detail}
      projectId={effectiveProjectId}
      actionMessage={actionMessage}
      startRun={startRun}
    />
  );
}

function TestPlanDetailContent(props: {
  detail: TestPlanDetail;
  projectId: string;
  actionMessage: string;
  startRun: ReturnType<typeof useMutation<TestRunDetail, Error, TestPlan>>;
}) {
  const { t } = useMspaceTranslation();
  const { detail, projectId, actionMessage, startRun } = props;
  const { plan } = detail;

  return (
    <PageFrame
      title={plan.title}
      subtitle={t("tests.runTarget", { type: t(`tests.targetTypeValue.${plan.targetType}`, { defaultValue: plan.targetType }), value: plan.targetValue || t("common.unknown") })}
      breadcrumbs={[
        { label: t("common.mspace"), to: "/inbox" },
        { label: t("tests.title"), to: "/tests", search: testsTabSearch("plans", projectId) },
        { label: plan.title },
      ]}
      actions={
        <>
          <Button type="button" variant="secondary" asChild>
            <Link to="/tests" search={testsTabSearch("plans", projectId)}>
              <ArrowLeft data-icon />
              {t("common.goBack")}
            </Link>
          </Button>
          <Button type="button" onClick={() => startRun.mutate(plan)} disabled={startRun.isPending}>
            <Play data-icon />
            {startRun.isPending ? t("tests.startingRun") : t("tests.startRun")}
          </Button>
        </>
      }
    >
      <div className="grid gap-5">
        <section className="rounded-[10px] bg-[color:var(--surface)] p-4 shadow-[inset_0_0_0_1px_var(--line)]">
          <div className="grid gap-3 md:grid-cols-4">
            <RunMetric label={t("tests.status")} value={t(`tests.planStatusValue.${plan.status}`, { defaultValue: plan.status })} />
            <RunMetric label={t("tests.targetType")} value={t(`tests.targetTypeValue.${plan.targetType}`, { defaultValue: plan.targetType })} />
            <RunMetric label={t("tests.caseCount", { count: detail.cases.length })} value={String(detail.cases.length)} />
            <RunMetric label={t("tests.runs")} value={String(detail.runs.length)} />
          </div>
          {plan.description ? <p className="mt-4 text-[13px] leading-6 text-[color:var(--muted)]">{plan.description}</p> : null}
          {plan.environment ? (
            <pre className="mt-4 max-h-52 overflow-auto rounded-[8px] bg-[color:var(--paper)] p-3 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
              {plan.environment}
            </pre>
          ) : null}
          {plan.environmentId ? (
            <p className="mt-3 text-[12px] text-[color:var(--muted)]">
              {t("tests.environment")}: {plan.environmentKind || "environment"} · <span className="font-mono">{plan.environmentId}</span>
            </p>
          ) : null}
          {startRun.error ? (
            <p className="mt-3 text-[12px] text-[color:var(--danger)]">{startRun.error.message}</p>
          ) : actionMessage ? (
            <p className="mt-3 text-[12px] text-[color:var(--muted)]">{actionMessage}</p>
          ) : null}
        </section>

        <section className="rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
          <div className="border-b border-[color:var(--line)] px-4 py-3 text-[13px] font-semibold text-[color:var(--text)]">{t("tests.cases")}</div>
          <div className="divide-y divide-[color:var(--line)]">
            {detail.cases.map((planCase) => (
              <Link
                key={planCase.id}
                to="/tests/cases/$caseId"
                params={{ caseId: planCase.testCase.id }}
                search={testsTabSearch("cases", projectId)}
                className="grid gap-3 px-4 py-3 transition-colors hover:bg-[color:var(--hover)] md:grid-cols-[minmax(0,1fr)_120px_24px]"
              >
                <div className="min-w-0">
                  <div className="flex min-w-0 items-center gap-2">
                    <ClipboardCheck data-icon className="size-4 shrink-0 text-[color:var(--muted)]" />
                    <span className="truncate text-[13px] font-medium text-[color:var(--text)]">{planCase.testCase.title}</span>
                  </div>
                  <p className="mt-1 text-[12px] text-[color:var(--muted)]">{planCase.testCase.area || t("common.unknown")}</p>
                </div>
                <div className="flex items-center md:justify-end">
                  <StatusBadge value={planCase.testCase.status} valueLabel={t(`tests.statusValue.${planCase.testCase.status}`, { defaultValue: planCase.testCase.status })} />
                </div>
                <div className="hidden items-center justify-end md:flex">
                  <ArrowRight data-icon className="text-[color:var(--faint)]" />
                </div>
              </Link>
            ))}
          </div>
        </section>

        <section className="rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
          <div className="border-b border-[color:var(--line)] px-4 py-3 text-[13px] font-semibold text-[color:var(--text)]">{t("tests.runs")}</div>
          <div className="divide-y divide-[color:var(--line)]">
            {detail.runs.length === 0 ? (
              <TestsPanelState icon={Play} title={t("tests.noRunsTitle")} body={t("tests.noRunsBody")} />
            ) : (
              detail.runs.map((run) => (
                <Link
                  key={run.id}
                  to="/tests/runs/$runId"
                  params={{ runId: run.id }}
                  search={testsTabSearch("runs", projectId)}
                  className="grid gap-3 px-4 py-3 transition-colors hover:bg-[color:var(--hover)] md:grid-cols-[minmax(0,1fr)_220px_24px]"
                >
                  <div className="min-w-0">
                    <div className="flex min-w-0 items-center gap-2">
                      <Play data-icon className="size-4 shrink-0 text-[color:var(--muted)]" />
                      <span className="truncate text-[13px] font-medium text-[color:var(--text)]">{t("tests.runShortId", { id: run.id.slice(0, 8) })}</span>
                      <StatusBadge value={run.status} valueLabel={t(`tests.runStatusValue.${run.status}`, { defaultValue: run.status })} />
                    </div>
                    <p className="mt-1 text-[12px] text-[color:var(--muted)]">
                      {t("tests.runCounts", { passed: run.passedCount, failed: run.failedCount, blocked: run.blockedCount, skipped: run.skippedCount })}
                    </p>
                  </div>
                  <div className="flex items-center text-[12px] text-[color:var(--muted)] md:justify-end">
                    <RelativeTime value={run.updatedAt} />
                  </div>
                  <div className="hidden items-center justify-end md:flex">
                    <ArrowRight data-icon className="text-[color:var(--faint)]" />
                  </div>
                </Link>
              ))
            )}
          </div>
        </section>
      </div>
    </PageFrame>
  );
}

export function TestRunDetailPage() {
  const { t } = useMspaceTranslation();
  const search = useTestsSearch();
  const { runId = "" } = useParams({ strict: false }) as { runId?: string };
  const queryClient = useQueryClient();
  const { auth, workspaceId, serverWorkspaceReady, projectsQuery, projects } = useWorkspaceProjects();
  const selectedProject = projectFromSearch(projects, search.project);
  const effectiveProjectId = selectedProject?.id || "";
  const [reviewNote, setReviewNote] = useState("");
  const [actionMessage, setActionMessage] = useState("");
  const workerReadiness = useTestsWorkerReadiness(auth, workspaceId, setActionMessage);
  const runQuery = useQuery({
    queryKey: queryKeys.projectTestRun(workspaceId, effectiveProjectId, runId || "__none", auth.token),
    queryFn: () => controlPlaneApi.getProjectTestRun(auth.token, workspaceId, effectiveProjectId, runId),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId && runId),
  });

  async function invalidateRun() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: queryKeys.projectTestRun(workspaceId, effectiveProjectId, runId, auth.token) }),
      runQuery.data?.plan?.id
        ? queryClient.invalidateQueries({ queryKey: queryKeys.projectTestPlan(workspaceId, effectiveProjectId, runQuery.data.plan.id, auth.token) })
        : Promise.resolve(),
    ]);
  }

  const retryRun = useMutation({
    mutationFn: async () => {
      await workerReadiness.ensureReady();
      return controlPlaneApi.retryProjectTestRun(auth.token, workspaceId, effectiveProjectId, runId, {
        runtimeMode: workerReadiness.runtimeMode,
      });
    },
    onSuccess: async () => {
      setActionMessage(t("tests.runRetried"));
      await invalidateRun();
    },
  });
  const acceptRun = useMutation({
    mutationFn: () => controlPlaneApi.acceptProjectTestRun(auth.token, workspaceId, effectiveProjectId, runId, { note: reviewNote }),
    onSuccess: async () => {
      setReviewNote("");
      await invalidateRun();
    },
  });
  const blockRun = useMutation({
    mutationFn: () => controlPlaneApi.blockProjectTestRun(auth.token, workspaceId, effectiveProjectId, runId, { note: reviewNote }),
    onSuccess: async () => {
      setReviewNote("");
      await invalidateRun();
    },
  });

  if (!serverWorkspaceReady) {
    return (
      <PageFrame title={t("tests.title")} subtitle={t("workspace.signInRequired")}>
        <CollectionEmptyState title={t("workspace.notSignedIn")} body={t("workspace.signInRequired")} />
      </PageFrame>
    );
  }

  if (!selectedProject && !projectsQuery.isLoading) {
    return (
      <PageFrame title={t("tests.title")} subtitle={t("tests.subtitle")}>
        <CollectionEmptyState title={t("tests.noProjectTitle")} body={t("tests.noProjectBody")} />
      </PageFrame>
    );
  }

  if (!runQuery.data) {
    return (
      <TestDetailUnavailableState
        title={t("tests.runs")}
        body={runQuery.isPending ? t("tests.loadingRuns") : t("tests.noRunSelectedBody")}
        error={runQuery.error}
        projectId={effectiveProjectId}
        tab="runs"
      />
    );
  }

  const detail = runQuery.data;
  const runTitle = detail.plan?.title || t("tests.adHocRun");

  return (
    <PageFrame
      title={t("tests.runShortId", { id: detail.run.id.slice(0, 8) })}
      subtitle={runTitle}
      breadcrumbs={[
        { label: t("common.mspace"), to: "/inbox" },
        { label: t("tests.title"), to: "/tests", search: testsTabSearch("runs", effectiveProjectId) },
        ...(detail.plan
          ? [{ label: detail.plan.title, to: "/tests/plans/$planId" as const, params: { planId: detail.plan.id }, search: testsTabSearch("plans", effectiveProjectId) }]
          : [{ label: t("tests.adHocRun"), to: "/tests" as const, search: testsTabSearch("runs", effectiveProjectId) }]),
        { label: t("tests.runShortId", { id: detail.run.id.slice(0, 8) }) },
      ]}
      actions={
        <Button type="button" variant="secondary" asChild>
          <Link to="/tests" search={testsTabSearch("runs", effectiveProjectId)}>
            <ArrowLeft data-icon />
            {t("common.goBack")}
          </Link>
        </Button>
      }
    >
      <div className="grid gap-5">
        <section className="rounded-[10px] bg-[color:var(--surface)] p-4 shadow-[inset_0_0_0_1px_var(--line)]">
          <div className="flex flex-wrap items-start justify-between gap-3 border-b border-[color:var(--line)] pb-4">
            <div className="min-w-0">
              <h2 className="text-[15px] font-semibold text-[color:var(--text)]">{runTitle}</h2>
              <p className="mt-1 text-[12px] leading-5 text-[color:var(--muted)]">
                {t("tests.runTarget", {
                  type: t(`tests.targetTypeValue.${detail.run.targetType}`, { defaultValue: detail.run.targetType }),
                  value: detail.run.targetValue || t("common.unknown"),
                })}
              </p>
            </div>
            <StatusBadge value={detail.run.status} valueLabel={t(`tests.runStatusValue.${detail.run.status}`, { defaultValue: detail.run.status })} />
          </div>

          <div className="mt-4 grid gap-3 md:grid-cols-5">
            <RunMetric label={t("tests.total")} value={String(detail.items.length)} />
            <RunMetric label={t("tests.passed")} value={String(detail.run.passedCount)} />
            <RunMetric label={t("tests.failed")} value={String(detail.run.failedCount)} />
            <RunMetric label={t("tests.blocked")} value={String(detail.run.blockedCount)} />
            <RunMetric label={t("tests.passRate")} value={runPassRate(detail.items)} />
          </div>

          <Field label={t("tests.reviewNote")}>
            <Textarea value={reviewNote} onChange={(event) => setReviewNote(event.target.value)} placeholder={t("tests.reviewNotePlaceholder")} className="min-h-20" />
          </Field>

          <div className="mt-4 flex flex-wrap items-center gap-2">
            <Button type="button" variant="secondary" onClick={() => retryRun.mutate()} disabled={retryRun.isPending}>
              <RotateCcw data-icon />
              {retryRun.isPending ? t("tests.retryingRun") : t("tests.retryRun")}
            </Button>
            <Button type="button" variant="secondary" onClick={() => blockRun.mutate()} disabled={blockRun.isPending}>
              <Ban data-icon />
              {blockRun.isPending ? t("tests.blockingRun") : t("tests.blockRun")}
            </Button>
            <Button type="button" onClick={() => acceptRun.mutate()} disabled={acceptRun.isPending}>
              <Check data-icon />
              {acceptRun.isPending ? t("tests.acceptingRun") : t("tests.acceptRun")}
            </Button>
          </div>
          {(retryRun.error || acceptRun.error || blockRun.error) ? (
            <p className="mt-3 text-[12px] text-[color:var(--danger)]">{(retryRun.error || acceptRun.error || blockRun.error)?.message}</p>
          ) : actionMessage ? (
            <p className="mt-3 text-[12px] text-[color:var(--muted)]">{actionMessage}</p>
          ) : null}
        </section>

        <section className="divide-y divide-[color:var(--line)] rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
          {detail.items.map((item) => (
            <div key={item.id} className="grid gap-3 p-3 md:grid-cols-[minmax(0,1fr)_140px]">
              <div className="min-w-0">
                <div className="flex min-w-0 items-center gap-2">
                  <ClipboardCheck data-icon className="size-4 shrink-0 text-[color:var(--muted)]" />
                  <span className="truncate text-[13px] font-medium text-[color:var(--text)]">{item.testCase.title}</span>
                </div>
                <p className="mt-1 text-[12px] leading-5 text-[color:var(--muted)]">{item.actualResult || item.failureSummary || t("tests.noResultYet")}</p>
                <TestRunEvidencePanel evidence={item.evidence} />
              </div>
              <div className="flex items-start md:justify-end">
                <StatusBadge value={item.status} valueLabel={t(`tests.runItemStatusValue.${item.status}`, { defaultValue: item.status })} />
              </div>
            </div>
          ))}
        </section>
      </div>
    </PageFrame>
  );
}

function ProposalCasePreview(props: { title: string; testCase?: TestCase; input?: TestCaseInput; emptyText: string }) {
  const { t } = useMspaceTranslation();
  const subject = props.testCase || props.input;
  return (
    <div className="rounded-[8px] bg-[color:var(--paper)] px-3 py-2 text-[12px] leading-5 shadow-[inset_0_0_0_1px_var(--line)]">
      <div className="mb-1 font-medium text-[color:var(--muted-strong)]">{props.title}</div>
      {subject ? (
        <div className="grid gap-1 text-[color:var(--muted)]">
          <div className="font-medium text-[color:var(--text)]">{subject.title}</div>
          <div>{t("tests.caseTypeBadge", { type: testCaseTypeLabel(subject.type || "functional", t) })}</div>
          <div>{subject.preconditions}</div>
          <div>{subject.expectedResult}</div>
        </div>
      ) : (
        <div className="text-[color:var(--muted)]">{props.emptyText}</div>
      )}
    </div>
  );
}

function CaseDetailTabs(props: {
  activeTab: TestCaseDetailTab;
  projectId: string;
  caseId: string;
}) {
  const { t } = useMspaceTranslation();
  return (
    <div className="mb-7 flex min-w-0 flex-wrap gap-1 border-b border-[color:var(--line)] pb-2" role="tablist" aria-label={t("tests.caseDetailSections")}>
      {caseDetailTabs.map((tab) => (
        <Link
          key={tab}
          to="/tests/cases/$caseId"
          params={{ caseId: props.caseId }}
          search={testCaseDetailSearch(props.projectId, tab)}
          role="tab"
          aria-selected={props.activeTab === tab}
          className={cn(
            "inline-flex h-8 max-w-full items-center rounded-[7px] px-2.5 text-[13px] font-medium leading-5 transition-[background-color,color,transform] duration-150 ease-out active:scale-95",
            props.activeTab === tab
              ? "bg-[color:var(--selection)] text-[color:var(--text)]"
              : "text-[color:var(--muted)] hover:bg-[color:var(--hover)] hover:text-[color:var(--muted-strong)]",
          )}
        >
          <span className="truncate">{t(`tests.caseDetailTabs.${tab}`)}</span>
        </Link>
      ))}
    </div>
  );
}

function CaseDetailsTab(props: {
  form: CaseForm;
  testCase?: TestCase;
  onChange: Dispatch<SetStateAction<CaseForm>>;
  onStepChange: (index: number, patch: Partial<TestCaseStep>) => void;
}) {
  const { t } = useMspaceTranslation();
  return (
    <div className="grid gap-5">
      <CaseFormFields
        form={props.form}
        onChange={props.onChange}
        onStepChange={props.onStepChange}
      />

      {props.testCase ? (
        <section className="border-t border-[color:var(--line)] pt-4">
          <h3 className="mb-2 text-[13px] font-semibold text-[color:var(--muted-strong)]">{t("tests.findings")}</h3>
          {props.testCase.qualityFindings.length === 0 ? (
            <p className="text-[12px] text-[color:var(--muted)]">{t("tests.noFindings")}</p>
          ) : (
            <div className="grid gap-2">
              {props.testCase.qualityFindings.map((finding) => (
                <div key={finding.code} className="rounded-[8px] bg-[color:var(--paper)] px-3 py-2 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
                  <span className="font-medium text-[color:var(--muted-strong)]">{qualityFindingLabel(finding.code, finding.message, t)}</span>
                </div>
              ))}
            </div>
          )}
        </section>
      ) : null}
    </div>
  );
}

function CaseRunHistoryTab(props: {
  testCase: TestCase;
  projectId: string;
  entries: TestCaseRunHistoryEntry[];
  queryPending: boolean;
  queryError: Error | null;
  focusedRunId?: string;
  focusedItemId?: string;
}) {
  const { t } = useMspaceTranslation();

  if (props.queryPending && props.entries.length === 0) {
    return (
      <div className="divide-y divide-[color:var(--line)] rounded-[8px] bg-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--line)]">
        <TestsLoadingRows label={t("tests.loadingCaseRuns")} />
      </div>
    );
  }

  return (
    <section id="case-run-history" className="grid gap-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="text-[13px] font-semibold text-[color:var(--muted-strong)]">{t("tests.caseRunHistoryTitle")}</h3>
          <p className="mt-1 text-[12px] leading-5 text-[color:var(--muted)]">{t("tests.caseRunHistoryDescription")}</p>
        </div>
        {props.testCase.latestResult ? (
          <StatusBadge value={props.testCase.latestResult.status} valueLabel={latestResultLabel(props.testCase.latestResult, t)} />
        ) : null}
      </div>

      {props.queryError ? <Notice tone="danger">{props.queryError.message}</Notice> : null}

      {props.entries.length === 0 ? (
        <div className="rounded-[8px] bg-[color:var(--paper)] px-3 py-4 text-[12px] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
          {t("tests.noCaseRuns")}
        </div>
      ) : (
        <div className="grid gap-2">
          {props.entries.map((entry, index) => {
            const focused = (props.focusedItemId && entry.itemId === props.focusedItemId) || (props.focusedRunId && entry.runId === props.focusedRunId);
            return (
              <CaseRunHistoryItem
                key={`${entry.runId || "run"}-${entry.itemId || index}`}
                entry={entry}
                projectId={props.projectId}
                focused={Boolean(focused)}
              />
            );
          })}
        </div>
      )}
    </section>
  );
}

function CaseRunHistoryItem(props: {
  entry: TestCaseRunHistoryEntry;
  projectId: string;
  focused: boolean;
}) {
  const { t } = useMspaceTranslation();
  const resultText = props.entry.actualResult || props.entry.failureSummary || t("tests.noResultYet");
  const updatedAt = props.entry.updatedAt || props.entry.createdAt;

  return (
    <article
      id={props.entry.itemId ? `run-item-${props.entry.itemId}` : props.entry.runId ? `run-${props.entry.runId}` : undefined}
      className={cn(
        "grid gap-3 rounded-[8px] bg-[color:var(--paper)] px-3 py-3 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]",
        props.focused ? "bg-[color:var(--selection)] shadow-[inset_0_0_0_1px_var(--text)]" : "",
      )}
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex min-w-0 items-center gap-2">
            <StatusBadge value={props.entry.status} valueLabel={t(`tests.runItemStatusValue.${props.entry.status}`, { defaultValue: props.entry.status || t("common.unknown") })} />
            <span className="truncate text-[12px] font-medium text-[color:var(--muted-strong)]">
              {props.entry.runId ? t("tests.runShortId", { id: props.entry.runId.slice(0, 8) }) : t("tests.adHocRun")}
            </span>
          </div>
          <p className="mt-2 text-[12px] text-[color:var(--muted)]">{resultText}</p>
          <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-[color:var(--faint)]">
            {updatedAt ? <span>{t("tests.latestResultUpdated", { time: new Date(updatedAt).toLocaleString() })}</span> : null}
            {props.entry.runSource ? <span>{t(`tests.runSourceValue.${props.entry.runSource}`, { defaultValue: props.entry.runSource })}</span> : null}
            {props.entry.targetType ? (
              <span>
                {t("tests.runTarget", {
                  type: t(`tests.targetTypeValue.${props.entry.targetType}`, { defaultValue: props.entry.targetType }),
                  value: props.entry.targetValue || t("common.unknown"),
                })}
              </span>
            ) : null}
          </div>
        </div>
        {props.entry.runId ? (
          <Button type="button" variant="secondary" size="sm" asChild>
            <Link to="/tests/runs/$runId" params={{ runId: props.entry.runId }} search={testsTabSearch("runs", props.projectId)}>
              <ArrowRight data-icon />
              {t("tests.openRun")}
            </Link>
          </Button>
        ) : null}
      </div>
      <TestRunEvidencePanel evidence={props.entry.evidence} />
    </article>
  );
}

function CaseRevisionHistoryTab(props: {
  revisions: TestCaseRevision[];
  revisionTimeline: ReturnType<typeof buildTestCaseRevisionTimeline>;
  error: Error | null;
}) {
  const { t } = useMspaceTranslation();
  return (
    <section className="grid gap-3">
      <div className="min-w-0">
        <h3 className="text-[13px] font-semibold text-[color:var(--muted-strong)]">{t("tests.revisions")}</h3>
        <p className="mt-1 text-[12px] leading-5 text-[color:var(--muted)]">{t("tests.revisionHistoryDescription")}</p>
      </div>
      {props.error ? (
        <Notice tone="danger">{props.error.message}</Notice>
      ) : props.revisions.length === 0 ? (
        <p className="text-[12px] text-[color:var(--muted)]">{t("tests.noRevisions")}</p>
      ) : (
        <div className="grid gap-2">
          {props.revisionTimeline.map(({ revision, changes, facts, isInitial }) => (
            <div key={revision.id} className="rounded-[8px] bg-[color:var(--paper)] px-3 py-3 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
              <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                <div className="min-w-0 font-medium text-[color:var(--text)]">
                  <span className="text-[color:var(--muted-strong)]">#{revision.revisionNumber}</span>
                  <span> - </span>
                  <span className="break-words">{revision.snapshot.title || t("tests.untitledCase")}</span>
                </div>
                <div className="shrink-0 text-[11px] text-[color:var(--muted)]">
                  <RelativeTime value={revision.createdAt} />
                </div>
              </div>

              {isInitial ? (
                <div className="mt-2 flex flex-wrap gap-1.5">
                  <span className="rounded-[6px] bg-[color:var(--surface)] px-2 py-1 text-[11px] font-medium text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]">
                    {t("tests.revisionInitial")}
                  </span>
                  {facts.map((fact) => (
                    <span key={fact.key} className="rounded-[6px] bg-[color:var(--surface)] px-2 py-1 text-[11px] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
                      <span className="font-medium text-[color:var(--muted-strong)]">{fact.label}</span>: {fact.value}
                    </span>
                  ))}
                </div>
              ) : null}

              {!isInitial && changes.length > 0 ? (
                <div className="mt-2 grid gap-1.5">
                  {changes.map((change) => (
                    <div key={change.key} className="grid gap-1 rounded-[6px] bg-[color:var(--surface)] px-2.5 py-2 shadow-[inset_0_0_0_1px_var(--line)]">
                      <div className="text-[11px] font-medium text-[color:var(--muted-strong)]">{change.label}</div>
                      <div className="grid gap-1 text-[11px] leading-5 text-[color:var(--muted)] sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
                        <span className="min-w-0 break-words">{t("tests.revisionBefore", { value: change.before })}</span>
                        <span className="min-w-0 break-words text-[color:var(--text)]">{t("tests.revisionAfter", { value: change.after })}</span>
                      </div>
                    </div>
                  ))}
                </div>
              ) : null}

              {!isInitial && changes.length === 0 ? (
                <p className="mt-2 rounded-[6px] bg-[color:var(--surface)] px-2.5 py-2 text-[11px] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
                  {t("tests.revisionNoVisibleChanges")}
                </p>
              ) : null}
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function CaseFormFields(props: {
  form: CaseForm;
  onChange: Dispatch<SetStateAction<CaseForm>>;
  onStepChange: (index: number, patch: Partial<TestCaseStep>) => void;
}) {
  const { t } = useMspaceTranslation();
  const { form, onChange, onStepChange } = props;
  return (
    <>
      <CaseReadinessPanel form={form} />
      <Field label={t("tests.titleLabel")} hint={t("tests.titleHint")}>
        <Input
          value={form.title}
          onChange={(event) => onChange((current) => ({ ...current, title: event.target.value }))}
          placeholder={t("tests.titlePlaceholder")}
          aria-required="true"
        />
      </Field>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Field label={t("tests.type")} hint={t("tests.typeHint")}>
          <Select value={form.type || "functional"} onValueChange={(value) => onChange((current) => ({ ...current, type: value }))}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {testCaseTypeOptions.map((type) => (
                <SelectItem key={type} value={type}>
                  {testCaseTypeLabel(type, t)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field label={t("tests.area")} hint={t("tests.areaHint")}>
          <Input
            value={form.area}
            onChange={(event) => onChange((current) => ({ ...current, area: event.target.value }))}
            placeholder={t("tests.areaPlaceholder")}
          />
        </Field>
        <Field label={t("tests.priority")} hint={t("tests.priorityHint")}>
          <Select value={form.priority || "__none"} onValueChange={(value) => onChange((current) => ({ ...current, priority: value === "__none" ? "" : value }))}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {priorityOptions.map((priority) => (
                <SelectItem key={priority || "__none"} value={priority || "__none"}>
                  {priority ? priority.toUpperCase() : t("tests.noPriority")}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field label={t("tests.status")} hint={t("tests.statusHint")}>
          <Select value={form.status} onValueChange={(value) => onChange((current) => ({ ...current, status: value }))}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {statusOptions.map((status) => (
                <SelectItem key={status} value={status}>
                  {t(`tests.statusValue.${status}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      </div>
      <Field label={t("tests.preconditions")} hint={t("tests.preconditionsHint")}>
        <Textarea
          value={form.preconditions}
          onChange={(event) => onChange((current) => ({ ...current, preconditions: event.target.value }))}
          placeholder={t("tests.preconditionsPlaceholder")}
        />
      </Field>
      <Field label={t("tests.steps")} hint={t("tests.stepsHint")}>
        <div className="grid gap-2">
          {form.steps.map((step, index) => (
            <div key={index} className="grid grid-cols-[24px_minmax(0,1fr)] gap-2 rounded-[8px] bg-[color:var(--paper)] p-2 shadow-[inset_0_0_0_1px_var(--line)]">
              <span
                aria-hidden="true"
                className="mt-1 flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-[color:var(--surface)] text-[11px] font-medium tabular-nums text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]"
              >
                {index + 1}
              </span>
              <div className="grid min-w-0 gap-2">
                <Input
                  value={step.action}
                  onChange={(event) => onStepChange(index, { action: event.target.value })}
                  placeholder={t("tests.stepAction", { index: index + 1 })}
                />
                <Input
                  value={step.expected || ""}
                  onChange={(event) => onStepChange(index, { expected: event.target.value })}
                  placeholder={t("tests.stepExpected", { index: index + 1 })}
                />
              </div>
            </div>
          ))}
          <Button
            type="button"
            variant="secondary"
            onClick={() => onChange((current) => ({ ...current, steps: [...current.steps, { action: "", expected: "" }] }))}
          >
            <Plus data-icon />
            {t("tests.addStep")}
          </Button>
        </div>
      </Field>
      <Field label={t("tests.expectedResult")} hint={t("tests.expectedResultHint")}>
        <Textarea
          value={form.expectedResult}
          onChange={(event) => onChange((current) => ({ ...current, expectedResult: event.target.value }))}
          placeholder={t("tests.expectedResultPlaceholder")}
        />
      </Field>
      <Field label={t("tests.environmentRequirements")} hint={t("tests.environmentRequirementsHint")}>
        <Textarea
          value={form.environmentRequirements}
          onChange={(event) => onChange((current) => ({ ...current, environmentRequirements: event.target.value }))}
          placeholder={t("tests.environmentRequirementsPlaceholder")}
        />
      </Field>
      <Field label={t("tests.tags")} hint={t("tests.tagsHint")}>
        <Input value={form.tagsText} onChange={(event) => onChange((current) => ({ ...current, tagsText: event.target.value }))} />
      </Field>
    </>
  );
}

function PlanFormFields(props: {
  form: PlanForm;
  onChange: Dispatch<SetStateAction<PlanForm>>;
  environments: Environment[];
  readyCases: TestCase[];
  selectedCaseIds: string[];
  onCaseToggle: (caseId: string) => void;
  onSelectReadyCases: () => void;
}) {
  const { t } = useMspaceTranslation();
  const { form, onChange, environments, readyCases, selectedCaseIds, onCaseToggle, onSelectReadyCases } = props;

  return (
    <>
      <div className="grid gap-3 lg:grid-cols-[minmax(0,1.2fr)_minmax(0,0.8fr)_minmax(0,1fr)]">
        <Field label={t("tests.planTitle")}>
          <Input
            value={form.title}
            onChange={(event) => onChange((current) => ({ ...current, title: event.target.value }))}
            placeholder={t("tests.planTitlePlaceholder")}
          />
        </Field>
        <Field label={t("tests.targetType")}>
          <Select value={form.targetType} onValueChange={(value) => onChange((current) => ({ ...current, targetType: value }))}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {targetTypeOptions.map((type) => (
                <SelectItem key={type} value={type}>
                  {t(`tests.targetTypeValue.${type}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field label={t("tests.targetValue")}>
          <Input value={form.targetValue} onChange={(event) => onChange((current) => ({ ...current, targetValue: event.target.value }))} />
        </Field>
      </div>
      <div className="grid gap-3 lg:grid-cols-2">
        <Field label={t("tests.planDescription")}>
          <Textarea value={form.description} onChange={(event) => onChange((current) => ({ ...current, description: event.target.value }))} className="min-h-24" />
        </Field>
        <div className="grid gap-3">
          <Field label={t("tests.environment")}>
            <Select value={form.environmentId || "__none"} onValueChange={(value) => onChange((current) => ({ ...current, environmentId: value === "__none" ? "" : value }))}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__none">{t("tests.noEnvironment")}</SelectItem>
                {environments.map((environment) => (
                  <SelectItem key={environment.id} value={environment.id}>
                    {environment.name} · {environment.kind === "virtual_machine" ? t("clusters.kindVirtualMachine") : t("clusters.kindKubernetes")}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          <Field label={t("tests.environmentNotes")}>
            <Textarea value={form.environment} onChange={(event) => onChange((current) => ({ ...current, environment: event.target.value }))} className="min-h-16" />
          </Field>
        </div>
      </div>
      <div className="rounded-[8px] bg-[color:var(--paper)] p-3 shadow-[inset_0_0_0_1px_var(--line)]">
        <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
          <div className="min-w-0">
            <span className="block text-[13px] font-medium text-[color:var(--muted-strong)]">{t("tests.readyCases")}</span>
            <span className="block text-[12px] text-[color:var(--muted)]">
              {t("tests.selectedSummary", { selected: selectedCaseIds.length, total: readyCases.length })}
            </span>
          </div>
          <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-[12px]" onClick={onSelectReadyCases} disabled={readyCases.length === 0}>
            {t("tests.selectReady")}
          </Button>
        </div>
        <div className="grid max-h-64 gap-2 overflow-auto pr-1 md:grid-cols-2">
          {readyCases.length === 0 ? (
            <p className="text-[12px] text-[color:var(--muted)]">{t("tests.noReadyCases")}</p>
          ) : (
            readyCases.map((testCase) => (
              <label key={testCase.id} className="flex min-w-0 items-start gap-2 rounded-[6px] px-1 py-1 text-[12px] leading-5 text-[color:var(--muted)] hover:bg-[color:var(--hover)]">
                <input
                  type="checkbox"
                  className="mt-1 size-4 shrink-0 accent-[color:var(--accent)]"
                  checked={selectedCaseIds.includes(testCase.id)}
                  onChange={() => onCaseToggle(testCase.id)}
                />
                <span className="min-w-0">
                  <span className="block truncate font-medium text-[color:var(--text)]">{testCase.title}</span>
                  <span>{testCase.area || t("common.unknown")}</span>
                </span>
              </label>
            ))
          )}
        </div>
      </div>
    </>
  );
}

function CaseReadinessPanel(props: { form: CaseForm }) {
  const { t } = useMspaceTranslation();
  const checks = [
    { key: "title", done: hasText(props.form.title), label: t("tests.readiness.title") },
    { key: "preconditions", done: hasText(props.form.preconditions), label: t("tests.readiness.preconditions") },
    { key: "steps", done: hasRunnableStep(props.form.steps), label: t("tests.readiness.steps") },
    { key: "expected", done: hasText(props.form.expectedResult), label: t("tests.readiness.expectedResult") },
    { key: "environment", done: hasText(props.form.environmentRequirements), label: t("tests.readiness.environment") },
  ];
  const doneCount = checks.filter((check) => check.done).length;

  return (
    <section className="rounded-[8px] bg-[color:var(--paper)] p-3 shadow-[inset_0_0_0_1px_var(--line)]">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="text-[13px] font-semibold leading-5 text-[color:var(--text)]">{t("tests.readinessTitle")}</h3>
          <p className="mt-1 max-w-[64ch] text-pretty text-[12px] leading-5 text-[color:var(--muted)]">{t("tests.readinessDescription")}</p>
        </div>
        <span className={cn("shrink-0 text-[12px] font-semibold tabular-nums", doneCount === checks.length ? "text-[color:var(--success)]" : "text-[color:var(--warning)]")}>
          {t("tests.readinessProgress", { done: doneCount, total: checks.length })}
        </span>
      </div>
      <div className="mt-3 grid gap-2 sm:grid-cols-2">
        {checks.map((check) => (
          <div key={check.key} className="flex min-w-0 items-center gap-2 text-[12px] leading-5 text-[color:var(--muted)]">
            <span
              className={cn(
                "grid size-4 shrink-0 place-items-center rounded-full shadow-[inset_0_0_0_1px_var(--line)]",
                check.done ? "bg-[color:var(--success-soft)] text-[color:var(--success)]" : "bg-[color:var(--surface)] text-[color:var(--faint)]",
              )}
            >
              {check.done ? <Check data-icon className="size-3" /> : <span className="size-1 rounded-full bg-current" />}
            </span>
            <span className="min-w-0 truncate">{check.label}</span>
          </div>
        ))}
      </div>
      <div className="mt-3 flex flex-wrap gap-2 text-[11px] leading-4 text-[color:var(--muted)]">
        <span className="rounded-[999px] bg-[color:var(--surface)] px-2 py-1 shadow-[inset_0_0_0_1px_var(--line)]">
          {t("tests.caseTypeBadge", { type: testCaseTypeLabel(props.form.type, t) })}
        </span>
        <span className="rounded-[999px] bg-[color:var(--surface)] px-2 py-1 shadow-[inset_0_0_0_1px_var(--line)]">{t("tests.readyPlanHint")}</span>
      </div>
    </section>
  );
}

function TestsModal(props: { title: string; description: string; onClose: () => void; children: ReactNode; wide?: boolean }) {
  const { t } = useMspaceTranslation();
  const { onClose } = props;
  const titleId = useId();
  const descriptionId = useId();

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        onClose();
      }
    }
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [onClose]);

  return (
    <div className="fixed inset-0 z-[80] grid min-w-0 place-items-center overflow-hidden bg-[rgba(31,31,31,0.18)] px-5 py-8">
      <button type="button" aria-label={t("tests.closeDialogBackdrop")} className="absolute inset-0 cursor-default" onClick={props.onClose} />
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={descriptionId}
        className={cn(
          "relative flex max-h-[min(820px,calc(100vh-64px))] min-w-0 w-full flex-col overflow-hidden rounded-[12px] bg-[color:var(--surface)] shadow-[0_24px_80px_rgba(31,31,31,0.20),inset_0_0_0_1px_var(--line)]",
          props.wide ? "max-w-[760px]" : "max-w-[560px]",
        )}
      >
        <div className="flex min-w-0 items-start justify-between gap-4 border-b border-[color:var(--line)] px-5 py-4">
          <div className="min-w-0">
            <h2 id={titleId} className="text-[17px] font-semibold leading-6 text-[color:var(--text)]">{props.title}</h2>
            <p id={descriptionId} className="mt-1 max-w-[62ch] text-pretty text-[13px] leading-5 text-[color:var(--muted)]">{props.description}</p>
          </div>
          <Button type="button" variant="ghost" size="icon" aria-label={t("tests.closeDialog")} onClick={props.onClose} className="shrink-0">
            <X data-icon />
          </Button>
        </div>
        <div className="min-h-0 min-w-0 overflow-auto px-5 py-4">
          {props.children}
        </div>
      </section>
    </div>
  );
}

function TestRunEvidencePanel(props: { evidence?: Record<string, unknown> }) {
  const { t } = useMspaceTranslation();
  const evidence = structuredTestEvidence(props.evidence);
  const [previewImage, setPreviewImage] = useState<TestEvidenceScreenshotImage | null>(null);
  if (!evidence) return null;
  const snapshot = evidence.postSubmitSnapshot || evidence.domSnapshot;
  const screenshotCount = Math.max(evidence.screenshots.length, evidence.screenshotImages.length);

  return (
    <>
      <div className="mt-3 grid gap-3 rounded-[8px] bg-[color:var(--paper)] p-3 text-[12px] leading-5 shadow-[inset_0_0_0_1px_var(--line)]">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-1.5 font-medium text-[color:var(--muted-strong)]">
            <ShieldCheck data-icon className="size-3.5" />
            {t("tests.evidenceTitle")}
          </div>
          <span className="text-[11px] text-[color:var(--faint)]">
            {t("tests.evidenceSummary", {
              screenshots: screenshotCount,
              assertions: evidence.assertions.length,
              network: evidence.networkStatuses.length,
            })}
          </span>
        </div>

        {evidence.screenshots.length > 0 || evidence.screenshotImages.length > 0 ? (
          <div className="grid gap-1.5">
            <div className="text-[11px] font-medium uppercase text-[color:var(--muted)]">{t("tests.evidenceScreenshots")}</div>
            {evidence.screenshotImages.length > 0 ? (
              <div className="grid gap-2 sm:grid-cols-2">
                {evidence.screenshotImages.slice(0, 4).map((image, index) => (
                  <EvidenceScreenshotThumb
                    key={`${screenshotImageTarget(image)}-${index}`}
                    image={image}
                    index={index}
                    onPreview={() => setPreviewImage(image)}
                  />
                ))}
              </div>
            ) : null}
          </div>
        ) : null}

        {evidence.assertions.length > 0 ? (
          <div className="grid gap-1.5">
            <div className="text-[11px] font-medium uppercase text-[color:var(--muted)]">{t("tests.evidenceAssertions")}</div>
            <div className="grid gap-1">
              {evidence.assertions.map((assertion, index) => (
                <div key={`${assertion.name}-${index}`} className="flex min-w-0 items-start justify-between gap-3 rounded-[7px] bg-[color:var(--surface)] px-2 py-1">
                  <span className="min-w-0 truncate text-[color:var(--muted-strong)]">{assertion.name}</span>
                  <span className={cn("shrink-0 font-medium", assertion.passed === false ? "text-[color:var(--danger)]" : "text-[color:var(--success)]")}>
                    {assertion.passed === false ? t("tests.assertionFailed") : t("tests.assertionPassed")}
                    {assertion.status ? ` · ${assertion.status}` : ""}
                  </span>
                </div>
              ))}
            </div>
          </div>
        ) : null}

        {evidence.networkStatuses.length > 0 ? (
          <div className="grid gap-1.5">
            <div className="flex items-center gap-1.5 text-[11px] font-medium uppercase text-[color:var(--muted)]">
              <Network data-icon className="size-3.5" />
              {t("tests.evidenceNetwork")}
            </div>
            <div className="grid gap-1">
              {evidence.networkStatuses.slice(0, 6).map((entry, index) => (
                <div key={`${entry.url}-${index}`} className="grid grid-cols-[56px_52px_minmax(0,1fr)] items-center gap-2 rounded-[7px] bg-[color:var(--surface)] px-2 py-1 font-mono text-[11px]">
                  <span className="text-[color:var(--muted)]">{entry.method || "-"}</span>
                  <span className={cn("font-semibold", evidenceStatusTone(entry.status))}>{entry.status || "-"}</span>
                  <span className="truncate text-[color:var(--muted-strong)]" title={entry.url}>{entry.url}</span>
                </div>
              ))}
            </div>
          </div>
        ) : null}

        {(evidence.previewUrl || evidence.finalUrl) ? (
          <div className="grid gap-1.5 sm:grid-cols-2">
            {evidence.previewUrl ? <EvidenceValue label={t("tests.evidencePreviewUrl")} value={evidence.previewUrl} openable /> : null}
            {evidence.finalUrl ? <EvidenceValue label={t("tests.evidenceFinalUrl")} value={evidence.finalUrl} openable /> : null}
          </div>
        ) : null}

        {snapshot ? (
          <div className="grid gap-1.5">
            <div className="text-[11px] font-medium uppercase text-[color:var(--muted)]">{t("tests.evidenceDomSnapshot")}</div>
            <div className="max-h-24 overflow-auto rounded-[7px] bg-[color:var(--surface)] px-2 py-1.5 text-[11px] leading-5 text-[color:var(--muted)]">
              {evidencePreviewText(snapshot)}
            </div>
          </div>
        ) : null}

        <details>
          <summary className="inline-flex cursor-pointer items-center gap-1.5 text-[11px] font-medium text-[color:var(--muted)] hover:text-[color:var(--text)]">
            <TerminalSquare data-icon className="size-3.5" />
            {t("tests.evidenceRaw")}
          </summary>
          <pre className="mt-2 max-h-48 overflow-auto rounded-[8px] bg-[color:var(--surface)] p-2 text-[11px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
            {JSON.stringify(evidence.raw, null, 2)}
          </pre>
        </details>
      </div>
      {previewImage ? <EvidenceLightbox image={previewImage} onClose={() => setPreviewImage(null)} /> : null}
    </>
  );
}

function EvidenceScreenshotThumb(props: {
  image: TestEvidenceScreenshotImage;
  index: number;
  onPreview: () => void;
}) {
  const { t } = useMspaceTranslation();
  const imageSource = useResolvedEvidenceImageSrc(props.image);
  const [imageFailed, setImageFailed] = useState(false);
  const label = screenshotImageLabel(props.image, t("tests.openScreenshotN", { index: props.index + 1 }));
  const isLegacyLocalPath = screenshotImageIsLegacyLocalPath(props.image);
  const imageUnavailable = imageFailed || Boolean(imageSource.error);

  useEffect(() => {
    setImageFailed(false);
  }, [imageSource.src]);

  return (
    <div className="overflow-hidden rounded-[8px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
      <button
        type="button"
        className="group relative block h-44 w-full bg-[color:var(--block)] text-left"
        title={t("tests.previewScreenshot")}
        onClick={props.onPreview}
      >
        {imageSource.src && !imageFailed ? (
          <img
            src={imageSource.src}
            alt={t("tests.screenshotAlt", { index: props.index + 1 })}
            className="h-full w-full object-cover object-top"
            onError={() => setImageFailed(true)}
            onLoad={() => setImageFailed(false)}
          />
        ) : imageSource.loading ? (
          <span className="grid h-full place-items-center px-4 text-center text-[12px] text-[color:var(--muted)]">{t("tests.screenshotLoading")}</span>
        ) : isLegacyLocalPath ? (
          <span className="grid h-full place-items-center px-4 text-center text-[12px] text-[color:var(--muted)]">{t("tests.screenshotLegacyLocalPath")}</span>
        ) : imageUnavailable ? (
          <span className="grid h-full place-items-center px-4 text-center text-[12px] text-[color:var(--muted)]">{t("tests.screenshotUnavailable")}</span>
        ) : (
          <span className="grid h-full place-items-center px-4 text-center text-[12px] text-[color:var(--muted)]">{t("tests.screenshotArtifactOnly")}</span>
        )}
        <span className="absolute right-2 top-2 grid size-7 place-items-center rounded-[7px] bg-[rgba(255,255,255,0.86)] text-[color:var(--muted-strong)] opacity-0 shadow-[inset_0_0_0_1px_var(--line)] transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100">
          <Maximize2 data-icon className="size-3.5" />
        </span>
      </button>
      <div className="min-w-0 px-2 py-1">
        <div className="min-w-0 truncate font-mono text-[11px] text-[color:var(--faint)]" title={label}>{label}</div>
      </div>
    </div>
  );
}

function EvidenceLightbox(props: { image: TestEvidenceScreenshotImage; onClose: () => void }) {
  const { t } = useMspaceTranslation();
  const { image, onClose } = props;
  const imageSource = useResolvedEvidenceImageSrc(image);
  const [imageFailed, setImageFailed] = useState(false);
  const [zoom, setZoom] = useState(1);
  const label = screenshotImageLabel(image, t("tests.previewScreenshot"));
  const isLegacyLocalPath = screenshotImageIsLegacyLocalPath(image);
  const imageUnavailable = imageFailed || Boolean(imageSource.error);
  const canZoomOut = zoom > screenshotPreviewMinZoom;
  const canZoomIn = zoom < screenshotPreviewMaxZoom;
  const zoomLabel = t("tests.screenshotZoomLevel", { zoom: Math.round(zoom * 100) });

  function updateZoom(nextZoom: number) {
    setZoom(Math.min(screenshotPreviewMaxZoom, Math.max(screenshotPreviewMinZoom, nextZoom)));
  }

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        onClose();
        return;
      }
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      if (event.key === "+" || event.key === "=") {
        event.preventDefault();
        updateZoom(zoom + screenshotPreviewZoomStep);
      } else if (event.key === "-") {
        event.preventDefault();
        updateZoom(zoom - screenshotPreviewZoomStep);
      } else if (event.key === "0") {
        event.preventDefault();
        updateZoom(1);
      }
    }
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [onClose, zoom]);

  useEffect(() => {
    setImageFailed(false);
    setZoom(1);
  }, [imageSource.src]);

  return (
    <div className="fixed inset-0 z-[90] grid min-w-0 place-items-center bg-[rgba(31,31,31,0.70)] px-5 py-8">
      <button type="button" aria-label={t("tests.closeScreenshotPreview")} className="absolute inset-0 cursor-default" onClick={onClose} />
      <section
        role="dialog"
        aria-modal="true"
        aria-label={t("tests.screenshotPreviewTitle")}
        className="relative grid h-[calc(100vh-64px)] max-h-[calc(100vh-64px)] w-full max-w-[1120px] grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-[12px] bg-[color:var(--surface)] shadow-[0_24px_80px_rgba(31,31,31,0.30),inset_0_0_0_1px_var(--line)]"
      >
        <div className="flex min-w-0 items-center justify-between gap-3 border-b border-[color:var(--line)] px-4 py-3">
          <div className="min-w-0">
            <h2 className="truncate text-[14px] font-semibold text-[color:var(--text)]">{t("tests.screenshotPreviewTitle")}</h2>
            <p className="mt-0.5 truncate font-mono text-[11px] text-[color:var(--muted)]" title={label}>{label}</p>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <div className="flex items-center rounded-[8px] bg-[color:var(--paper)] p-0.5 shadow-[inset_0_0_0_1px_var(--line)]">
              <Button type="button" variant="ghost" size="icon" aria-label={t("tests.zoomOutScreenshot")} disabled={!canZoomOut} onClick={() => updateZoom(zoom - screenshotPreviewZoomStep)}>
                <ZoomOut data-icon />
              </Button>
              <span className="w-14 select-none text-center font-mono text-[11px] text-[color:var(--muted-strong)]" aria-live="polite" aria-label={zoomLabel}>
                {Math.round(zoom * 100)}%
              </span>
              <Button type="button" variant="ghost" size="icon" aria-label={t("tests.zoomInScreenshot")} disabled={!canZoomIn} onClick={() => updateZoom(zoom + screenshotPreviewZoomStep)}>
                <ZoomIn data-icon />
              </Button>
              <Button type="button" variant="ghost" size="icon" aria-label={t("tests.resetScreenshotZoom")} disabled={zoom === 1} onClick={() => updateZoom(1)}>
                <RotateCcw data-icon />
              </Button>
            </div>
            <Button type="button" variant="ghost" size="icon" aria-label={t("tests.closeScreenshotPreview")} onClick={onClose}>
              <X data-icon />
            </Button>
          </div>
        </div>
        <div className="min-h-0 overflow-auto overscroll-contain bg-[color:var(--paper)] p-3">
          {imageSource.src && !imageFailed ? (
            <img
              src={imageSource.src}
              alt={t("tests.screenshotPreviewTitle")}
              className="mx-auto w-auto max-w-none rounded-[8px] bg-[color:var(--surface)] object-contain shadow-[inset_0_0_0_1px_var(--line)]"
              style={{ height: `calc(${Math.round(zoom * 100)}vh - ${Math.round(168 * zoom)}px)` }}
              onError={() => setImageFailed(true)}
              onLoad={() => setImageFailed(false)}
            />
          ) : imageSource.loading ? (
            <div className="grid min-h-[360px] place-items-center rounded-[8px] bg-[color:var(--surface)] px-6 text-center text-[13px] leading-6 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
              {t("tests.screenshotLoading")}
            </div>
          ) : isLegacyLocalPath ? (
            <div className="grid min-h-[360px] place-items-center rounded-[8px] bg-[color:var(--surface)] px-6 text-center text-[13px] leading-6 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
              {t("tests.screenshotLegacyLocalPath")}
            </div>
          ) : imageUnavailable ? (
            <div className="grid min-h-[360px] place-items-center rounded-[8px] bg-[color:var(--surface)] px-6 text-center text-[13px] leading-6 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
              {t("tests.screenshotUnavailable")}
            </div>
          ) : (
            <div className="grid min-h-[360px] place-items-center rounded-[8px] bg-[color:var(--surface)] px-6 text-center text-[13px] leading-6 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
              {t("tests.screenshotArtifactOnly")}
            </div>
          )}
        </div>
      </section>
    </div>
  );
}

function EvidenceValue(props: { label: string; value: string; openable?: boolean }) {
  return (
    <div className="min-w-0 rounded-[7px] bg-[color:var(--surface)] px-2 py-1.5">
      <div className="text-[11px] font-medium text-[color:var(--muted)]">{props.label}</div>
      {props.openable ? (
        <button type="button" className="mt-0.5 max-w-full truncate text-left font-mono text-[11px] text-[color:var(--accent)] hover:underline" title={props.value} onClick={() => void openEvidenceTarget(props.value)}>
          {props.value}
        </button>
      ) : (
        <div className="mt-0.5 truncate font-mono text-[11px] text-[color:var(--muted-strong)]" title={props.value}>{props.value}</div>
      )}
    </div>
  );
}

function RunMetric(props: { label: string; value: string }) {
  return (
    <div className="rounded-[8px] bg-[color:var(--paper)] px-3 py-2 shadow-[inset_0_0_0_1px_var(--line)]">
      <div className="text-[11px] font-medium uppercase text-[color:var(--muted)]">{props.label}</div>
      <div className="mt-1 text-[16px] font-semibold text-[color:var(--text)]">{props.value}</div>
    </div>
  );
}
