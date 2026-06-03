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
  Play,
  Plus,
  RotateCcw,
  Save,
  Search,
  Sparkles,
  type LucideIcon,
  X,
} from "lucide-react";
import {
  controlPlaneApi,
  queryKeys,
  type Project,
  type TestCase,
  type TestCaseInput,
  type TestCaseProposal,
  type TestCaseRevision,
  type TestCaseStep,
  type TestPlan,
  type TestPlanDetail,
  type TestRunDetail,
  type TestRunItem,
} from "@mspace/core";
import { useMspaceTranslation } from "@mspace/i18n";
import {
  Button,
  CollectionEmptyState,
  Field,
  Input,
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
  environment: "",
};

const tabs: TabKey[] = ["cases", "proposals", "plans", "runs"];
const statusOptions = ["draft", "needs_review", "ready", "archived"] as const;
const proposalStatusOptions = ["pending", "applied", "rejected", "invalid"] as const;
const planStatusOptions = ["draft", "ready", "archived"] as const;
const testCaseTypeOptions = ["functional", "ui", "api", "deployment"] as const;
const priorityOptions = ["", "p0", "p1", "p2", "p3"] as const;
const targetTypeOptions = ["branch", "commit", "source_session", "image", "offline_package", "version_url", "preview_url"] as const;
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

function testsTabSearch(tab: TabKey, projectId?: string) {
  return { tab, project: projectId || undefined };
}

function useTestsSearch(): { tab?: string; project?: string } {
  return useSearch({ strict: false }) as { tab?: string; project?: string };
}

function normalizeTestCaseForView(testCase: TestCase): TestCase {
  return {
    ...testCase,
    type: testCase.type || "functional",
    steps: testCase.steps ?? [],
    dependencies: testCase.dependencies ?? [],
    tags: testCase.tags ?? [],
    qualityFindings: testCase.qualityFindings ?? [],
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
    projects: projectsQuery.data || [],
  };
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
  const [generateArea, setGenerateArea] = useState("");
  const [generatePrompt, setGeneratePrompt] = useState("");
  const [actionMessage, setActionMessage] = useState("");
  const [reviewNote, setReviewNote] = useState("");

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

  const casesQuery = useQuery({
    queryKey: casesQueryKey,
    queryFn: () =>
      controlPlaneApi.listProjectTestCases(auth.token, workspaceId, effectiveProjectId, {
        status: statusFilter === "all" ? "" : statusFilter,
        query,
      }),
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

  const plansQuery = useQuery({
    queryKey: plansQueryKey,
    queryFn: () =>
      controlPlaneApi.listProjectTestPlans(auth.token, workspaceId, effectiveProjectId, {
        status: planStatusFilter === "all" ? "" : planStatusFilter,
      }),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId),
  });

  const selectedPlanQuery = useQuery({
    queryKey: queryKeys.projectTestPlan(workspaceId, effectiveProjectId, selectedPlanId || "__none", auth.token),
    queryFn: () => controlPlaneApi.getProjectTestPlan(auth.token, workspaceId, effectiveProjectId, selectedPlanId),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId && selectedPlanId),
  });

  const cases = useMemo(() => (casesQuery.data || []).filter((testCase) => testCaseMatchesQuery(testCase, query)), [casesQuery.data, query]);
  const readyCases = useMemo(() => (casesQuery.data || []).filter((testCase) => testCase.status === "ready"), [casesQuery.data]);
  const proposals = proposalsQuery.data || [];
  const plans = plansQuery.data || [];
  const selectedPlan = selectedPlanQuery.data?.plan || plans.find((plan) => plan.id === selectedPlanId);
  const selectedPlanDetail = selectedPlanQuery.data;
  const canCreateCase = Boolean(effectiveProjectId && caseForm.title.trim());
  const isExcelImport = importFormat === "xlsx";
  const canImportCases = isExcelImport ? Boolean(importFile) : Boolean(importContent.trim());

  async function invalidateCaseWorkflow() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: casesQueryKey }),
      queryClient.invalidateQueries({ queryKey: proposalQueryKey }),
      queryClient.invalidateQueries({ queryKey: plansQueryKey }),
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
    mutationFn: () => controlPlaneApi.optimizeProjectTestCases(auth.token, workspaceId, effectiveProjectId, { caseIds: selectedCaseIds }),
    onSuccess: async (result) => {
      setActionMessage(t("tests.agentSessionQueued", { issueId: result.issueId }));
      await navigate({ to: "/tests", search: testsTabSearch("proposals", effectiveProjectId) });
      await invalidateCaseWorkflow();
    },
  });

  const generateCases = useMutation({
    mutationFn: () =>
      controlPlaneApi.generateProjectTestCases(auth.token, workspaceId, effectiveProjectId, {
        area: generateArea,
        prompt: generatePrompt,
      }),
    onSuccess: async (result) => {
      setActionMessage(t("tests.agentSessionQueued", { issueId: result.issueId }));
      await navigate({ to: "/tests", search: testsTabSearch("proposals", effectiveProjectId) });
      await invalidateCaseWorkflow();
    },
  });

  const applyProposal = useMutation({
    mutationFn: (proposal: TestCaseProposal) =>
      controlPlaneApi.applyProjectTestCaseProposal(auth.token, workspaceId, effectiveProjectId, proposal.id, { note: reviewNote }),
    onSuccess: async () => {
      setReviewNote("");
      setActionMessage(t("tests.proposalApplied"));
      await invalidateCaseWorkflow();
    },
  });

  const rejectProposal = useMutation({
    mutationFn: (proposal: TestCaseProposal) =>
      controlPlaneApi.rejectProjectTestCaseProposal(auth.token, workspaceId, effectiveProjectId, proposal.id, { note: reviewNote }),
    onSuccess: async () => {
      setReviewNote("");
      setActionMessage(t("tests.proposalRejected"));
      await invalidateCaseWorkflow();
    },
  });

  const createPlan = useMutation({
    mutationFn: () =>
      controlPlaneApi.createProjectTestPlan(auth.token, workspaceId, effectiveProjectId, {
        ...planForm,
        caseIds: selectedCaseIds,
      }),
    onSuccess: async (detail) => {
      setSelectedPlanId(detail.plan.id);
      setPlanForm(emptyPlanForm);
      setActionMessage(t("tests.planCreated"));
      await navigate({ to: "/tests/plans/$planId", params: { planId: detail.plan.id }, search: testsTabSearch("plans", effectiveProjectId) });
      await invalidateCaseWorkflow();
    },
  });

  const startRun = useMutation({
    mutationFn: (plan: TestPlan) =>
      controlPlaneApi.startProjectTestRun(auth.token, workspaceId, effectiveProjectId, plan.id, {
        targetType: plan.targetType,
        targetValue: plan.targetValue,
        environment: plan.environment,
      }),
    onSuccess: async (detail) => {
      setSelectedPlanId(detail.plan.id);
      setActionMessage(t("tests.runStarted"));
      await navigate({ to: "/tests/runs/$runId", params: { runId: detail.run.id }, search: testsTabSearch("runs", effectiveProjectId) });
      await invalidateCaseWorkflow();
    },
  });

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
    setSelectedCaseIds((current) => current.filter((caseId) => (casesQuery.data || []).some((testCase) => testCase.id === caseId)));
  }, [casesQuery.data]);

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
          <section className="rounded-[10px] bg-[color:var(--surface)] p-3 shadow-[inset_0_0_0_1px_var(--line)]">
            <div className="flex flex-wrap items-center gap-2">
              <div className="min-w-[220px] flex-1">
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
              <div className="flex flex-wrap gap-1 rounded-[8px] bg-[color:var(--paper)] p-1 shadow-[inset_0_0_0_1px_var(--line)]">
                {tabs.map((tab) => (
                  <Button
                    key={tab}
                    type="button"
                    size="sm"
                    variant={activeTab === tab ? "secondary" : "ghost"}
                    onClick={() => void navigate({ to: "/tests", search: testsTabSearch(tab, effectiveProjectId) })}
                    className="h-7 px-2 text-[12px]"
                  >
                    {t(`tests.tabs.${tab}`)}
                  </Button>
                ))}
              </div>
              {actionMessage ? <span className="text-[12px] text-[color:var(--muted)]">{actionMessage}</span> : null}
            </div>
          </section>

          {activeTab === "cases" ? (
            <div className="grid min-h-[620px] gap-5">
              <section className="min-w-0 rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
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
                  <Button type="button" onClick={openCreateCaseDialog} disabled={!effectiveProjectId}>
                    <Plus data-icon />
                    {t("tests.newCase")}
                  </Button>
                </div>

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
                              <div className="text-right text-[12px] text-[color:var(--muted)]">{t("tests.notRun")}</div>
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
                    <Button type="button" variant="secondary" onClick={() => optimizeCases.mutate()} disabled={selectedCaseIds.length === 0 || optimizeCases.isPending}>
                      <Sparkles data-icon />
                      {optimizeCases.isPending ? t("tests.optimizing") : t("tests.optimizeSelected")}
                    </Button>
                    <Button type="button" onClick={() => generateCases.mutate()} disabled={generateCases.isPending || !effectiveProjectId}>
                      <Sparkles data-icon />
                      {generateCases.isPending ? t("tests.generating") : t("tests.generate")}
                    </Button>
                  </div>
                  <span className="text-[12px] text-[color:var(--muted)]">
                    {t("tests.selectedSummary", { selected: selectedCaseIds.length, total: casesQuery.data?.length || 0 })}
                  </span>
                </div>
                <div className="border-b border-[color:var(--line)] bg-[color:var(--paper)] p-4">
                  <div className="grid gap-3 lg:grid-cols-[240px_minmax(0,1fr)_minmax(0,1fr)]">
                    <div>
                      <h2 className="text-[13px] font-semibold text-[color:var(--text)]">{t("tests.generateTitle")}</h2>
                      <p className="mt-1 text-[12px] leading-5 text-[color:var(--muted)]">{t("tests.generateDescription")}</p>
                    </div>
                    <Field label={t("tests.area")}>
                      <Input value={generateArea} onChange={(event) => setGenerateArea(event.target.value)} placeholder={t("tests.areaPlaceholder")} />
                    </Field>
                    <Field label={t("tests.generatePrompt")}>
                      <Textarea value={generatePrompt} onChange={(event) => setGeneratePrompt(event.target.value)} placeholder={t("tests.generatePlaceholder")} className="min-h-20" />
                    </Field>
                    <Field label={t("tests.reviewNote")} className="lg:col-start-2 lg:col-span-2">
                      <Textarea value={reviewNote} onChange={(event) => setReviewNote(event.target.value)} placeholder={t("tests.reviewNotePlaceholder")} className="min-h-20" />
                    </Field>
                  </div>
                </div>
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
                    <Button type="button" onClick={() => createPlan.mutate()} disabled={createPlan.isPending || !planForm.title.trim() || selectedCaseIds.length === 0}>
                      <Plus data-icon />
                      {createPlan.isPending ? t("tests.creatingPlan") : t("tests.createPlan")}
                    </Button>
                  </div>
                  <span className="text-[12px] text-[color:var(--muted)]">
                    {t("tests.selectedSummary", { selected: selectedCaseIds.length, total: readyCases.length })}
                  </span>
                </div>
                <div className="border-b border-[color:var(--line)] bg-[color:var(--paper)] p-4">
                  <div className="grid gap-4">
                    <div>
                      <h2 className="text-[13px] font-semibold text-[color:var(--text)]">{t("tests.createPlan")}</h2>
                      <p className="mt-1 text-[12px] leading-5 text-[color:var(--muted)]">{t("tests.createPlanDescription")}</p>
                    </div>
                    <div className="grid gap-3 lg:grid-cols-[minmax(0,1.2fr)_minmax(0,0.8fr)_minmax(0,1fr)]">
                      <Field label={t("tests.planTitle")}>
                        <Input value={planForm.title} onChange={(event) => setPlanForm((current) => ({ ...current, title: event.target.value }))} placeholder={t("tests.planTitlePlaceholder")} />
                      </Field>
                      <Field label={t("tests.targetType")}>
                        <Select value={planForm.targetType} onValueChange={(value) => setPlanForm((current) => ({ ...current, targetType: value }))}>
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
                        <Input value={planForm.targetValue} onChange={(event) => setPlanForm((current) => ({ ...current, targetValue: event.target.value }))} />
                      </Field>
                    </div>
                    <div className="grid gap-3 lg:grid-cols-2">
                      <Field label={t("tests.planDescription")}>
                        <Textarea value={planForm.description} onChange={(event) => setPlanForm((current) => ({ ...current, description: event.target.value }))} className="min-h-20" />
                      </Field>
                      <Field label={t("tests.environment")}>
                        <Textarea value={planForm.environment} onChange={(event) => setPlanForm((current) => ({ ...current, environment: event.target.value }))} className="min-h-20" />
                      </Field>
                    </div>
                    <div className="rounded-[8px] bg-[color:var(--surface)] p-3 shadow-[inset_0_0_0_1px_var(--line)]">
                      <div className="mb-2 flex items-center justify-between gap-2">
                        <span className="text-[13px] font-medium text-[color:var(--muted-strong)]">{t("tests.readyCases")}</span>
                        <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-[12px]" onClick={selectReadyCases} disabled={readyCases.length === 0}>
                          {t("tests.selectReady")}
                        </Button>
                      </div>
                      <div className="grid max-h-52 gap-2 overflow-auto pr-1 md:grid-cols-2">
                        {readyCases.length === 0 ? (
                          <p className="text-[12px] text-[color:var(--muted)]">{t("tests.noReadyCases")}</p>
                        ) : (
                          readyCases.map((testCase) => (
                            <label key={testCase.id} className="flex items-start gap-2 text-[12px] leading-5 text-[color:var(--muted)]">
                              <input
                                type="checkbox"
                                className="mt-1 size-4 accent-[color:var(--accent)]"
                                checked={selectedCaseIds.includes(testCase.id)}
                                onChange={() => toggleCaseSelection(testCase.id)}
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
                    {createPlan.error ? <p className="text-[12px] text-[color:var(--danger)]">{createPlan.error.message}</p> : null}
                  </div>
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
                <div className="flex flex-wrap items-start justify-between gap-3 border-b border-[color:var(--line)] p-3">
                  <div>
                    <h2 className="text-[13px] font-semibold text-[color:var(--text)]">{t("tests.runs")}</h2>
                    <p className="mt-1 text-[12px] text-[color:var(--muted)]">{selectedPlan?.title || t("tests.selectPlan")}</p>
                  </div>
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
                </div>
                <div className="divide-y divide-[color:var(--line)]">
                  {selectedPlanQuery.isLoading ? (
                    <TestsLoadingRows label={t("tests.loadingPlans")} />
                  ) : !selectedPlanDetail ? (
                    <TestsPanelState icon={Play} title={t("tests.noRunsTitle")} body={t("tests.noRunsBody")} />
                  ) : selectedPlanDetail.runs.length === 0 ? (
                    <TestsPanelState icon={Play} title={t("tests.noRunsTitle")} body={t("tests.noRunsBody")} />
                  ) : (
                    selectedPlanDetail.runs.map((run) => (
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
  const revisions = revisionsQuery.data || [];
  const revisionTimeline = useMemo(() => buildTestCaseRevisionTimeline(revisions, t), [revisions, t]);
  const canSave = Boolean(effectiveProjectId && caseForm.title.trim());

  const createCase = useMutation({
    mutationFn: (input: TestCaseInput) => controlPlaneApi.createProjectTestCase(auth.token, workspaceId, effectiveProjectId, input),
    onSuccess: async (created) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.projectTestCases(workspaceId, effectiveProjectId, auth.token) });
      await navigate({ to: "/tests/cases/$caseId", params: { caseId: created.id }, search: testsTabSearch("cases", effectiveProjectId) });
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

  if (!isNew && !testCase) {
    return (
      <PageFrame
        title={t("tests.cases")}
        subtitle={caseQuery.isPending ? t("tests.loading") : t("tests.selectCase")}
        breadcrumbs={[
          { label: t("common.mspace"), to: "/inbox" },
          { label: t("tests.title"), to: "/tests", search: testsTabSearch("cases", effectiveProjectId) },
          { label: t("tests.cases") },
        ]}
      >
        <CollectionEmptyState title={t("tests.cases")} body={caseQuery.isPending ? t("tests.loading") : t("tests.selectCase")} />
      </PageFrame>
    );
  }

  return (
    <PageFrame
      title={isNew ? t("tests.newCase") : testCase?.title || t("tests.cases")}
      subtitle={selectedProject?.name || t("tests.project")}
      breadcrumbs={[
        { label: t("common.mspace"), to: "/inbox" },
        { label: t("tests.title"), to: "/tests", search: testsTabSearch("cases", effectiveProjectId) },
        { label: isNew ? t("tests.newCase") : testCase?.title || t("tests.cases") },
      ]}
      actions={
        <Button type="button" variant="secondary" asChild>
          <Link to="/tests" search={testsTabSearch("cases", effectiveProjectId)}>
            <ArrowLeft data-icon />
            {t("common.goBack")}
          </Link>
        </Button>
      }
    >
      <form className="grid gap-5 rounded-[10px] bg-[color:var(--surface)] p-5 shadow-[inset_0_0_0_1px_var(--line)]" onSubmit={submitCase}>
        <div className="flex flex-wrap items-start justify-between gap-3 border-b border-[color:var(--line)] pb-4">
          <div className="min-w-0">
            <h2 className="truncate text-[15px] font-semibold text-[color:var(--text)]">{isNew ? t("tests.newCase") : testCase?.title}</h2>
            {testCase ? (
              <p className="mt-1 text-[12px] text-[color:var(--muted)]">
                {t("tests.quality")}: <span className={cn("font-semibold", scoreTone(testCase.qualityScore))}>{testCase.qualityScore}</span>
              </p>
            ) : null}
          </div>
          <Button type="submit" disabled={!canSave || savePending}>
            <Save data-icon />
            {savePending ? t("tests.saving") : isNew ? t("tests.createCase") : t("tests.saveCase")}
          </Button>
        </div>

        <CaseFormFields
          form={caseForm}
          onChange={setCaseForm}
          onStepChange={updateStep}
        />

        {testCase ? (
          <div className="grid gap-4 border-t border-[color:var(--line)] pt-4">
            <section>
              <h3 className="mb-2 text-[13px] font-semibold text-[color:var(--muted-strong)]">{t("tests.findings")}</h3>
              {testCase.qualityFindings.length === 0 ? (
                <p className="text-[12px] text-[color:var(--muted)]">{t("tests.noFindings")}</p>
              ) : (
                <div className="grid gap-2">
                  {testCase.qualityFindings.map((finding) => (
                    <div key={finding.code} className="rounded-[8px] bg-[color:var(--paper)] px-3 py-2 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
                      <span className="font-medium text-[color:var(--muted-strong)]">{qualityFindingLabel(finding.code, finding.message, t)}</span>
                    </div>
                  ))}
                </div>
              )}
            </section>
            <section>
              <h3 className="mb-2 text-[13px] font-semibold text-[color:var(--muted-strong)]">{t("tests.revisions")}</h3>
              {revisions.length === 0 ? (
                <p className="text-[12px] text-[color:var(--muted)]">{t("tests.noRevisions")}</p>
              ) : (
                <div className="grid gap-2">
                  {revisionTimeline.map(({ revision, changes, facts, isInitial }) => (
                    <div key={revision.id} className="rounded-[8px] bg-[color:var(--paper)] px-3 py-3 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
                      <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
                        <div className="min-w-0 font-medium text-[color:var(--text)]">
                          <span className="text-[color:var(--muted-strong)]">#{revision.revisionNumber}</span>
                          <span> - </span>
                          <span className="break-words">{revision.snapshot.title}</span>
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
          </div>
        ) : null}

        {createCase.error || updateCase.error ? (
          <p className="text-[12px] text-[color:var(--danger)]">{(createCase.error || updateCase.error)?.message}</p>
        ) : null}
      </form>
    </PageFrame>
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
  const planQuery = useQuery({
    queryKey: queryKeys.projectTestPlan(workspaceId, effectiveProjectId, planId || "__none", auth.token),
    queryFn: () => controlPlaneApi.getProjectTestPlan(auth.token, workspaceId, effectiveProjectId, planId),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId && planId),
  });
  const detail = planQuery.data;
  const startRun = useMutation({
    mutationFn: (plan: TestPlan) =>
      controlPlaneApi.startProjectTestRun(auth.token, workspaceId, effectiveProjectId, plan.id, {
        targetType: plan.targetType,
        targetValue: plan.targetValue,
        environment: plan.environment,
      }),
    onSuccess: async (runDetail) => {
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
      <PageFrame title={t("tests.selectedPlan")} subtitle={planQuery.isPending ? t("tests.loadingPlans") : t("tests.selectPlan")}>
        <CollectionEmptyState title={t("tests.selectedPlan")} body={planQuery.isPending ? t("tests.loadingPlans") : t("tests.selectPlan")} />
      </PageFrame>
    );
  }

  return (
    <TestPlanDetailContent
      detail={detail}
      projectId={effectiveProjectId}
      startRun={startRun}
    />
  );
}

function TestPlanDetailContent(props: {
  detail: TestPlanDetail;
  projectId: string;
  startRun: ReturnType<typeof useMutation<TestRunDetail, Error, TestPlan>>;
}) {
  const { t } = useMspaceTranslation();
  const { detail, projectId, startRun } = props;
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
          {startRun.error ? <p className="mt-3 text-[12px] text-[color:var(--danger)]">{startRun.error.message}</p> : null}
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
  const runQuery = useQuery({
    queryKey: queryKeys.projectTestRun(workspaceId, effectiveProjectId, runId || "__none", auth.token),
    queryFn: () => controlPlaneApi.getProjectTestRun(auth.token, workspaceId, effectiveProjectId, runId),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId && runId),
  });

  async function invalidateRun() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: queryKeys.projectTestRun(workspaceId, effectiveProjectId, runId, auth.token) }),
      runQuery.data?.plan.id
        ? queryClient.invalidateQueries({ queryKey: queryKeys.projectTestPlan(workspaceId, effectiveProjectId, runQuery.data.plan.id, auth.token) })
        : Promise.resolve(),
    ]);
  }

  const retryRun = useMutation({
    mutationFn: () => controlPlaneApi.retryProjectTestRun(auth.token, workspaceId, effectiveProjectId, runId, {}),
    onSuccess: invalidateRun,
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
      <PageFrame title={t("tests.runs")} subtitle={runQuery.isPending ? t("tests.loadingPlans") : t("tests.noRunSelectedBody")}>
        <CollectionEmptyState title={t("tests.noRunSelectedTitle")} body={runQuery.isPending ? t("tests.loadingPlans") : t("tests.noRunSelectedBody")} />
      </PageFrame>
    );
  }

  const detail = runQuery.data;

  return (
    <PageFrame
      title={t("tests.runShortId", { id: detail.run.id.slice(0, 8) })}
      subtitle={detail.plan.title}
      breadcrumbs={[
        { label: t("common.mspace"), to: "/inbox" },
        { label: t("tests.title"), to: "/tests", search: testsTabSearch("runs", effectiveProjectId) },
        { label: detail.plan.title, to: "/tests/plans/$planId", params: { planId: detail.plan.id }, search: testsTabSearch("plans", effectiveProjectId) },
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
              <h2 className="text-[15px] font-semibold text-[color:var(--text)]">{detail.plan.title}</h2>
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
                {Object.keys(item.evidence || {}).length > 0 ? (
                  <pre className="mt-2 max-h-48 overflow-auto rounded-[8px] bg-[color:var(--paper)] p-2 text-[11px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
                    {JSON.stringify(item.evidence, null, 2)}
                  </pre>
                ) : null}
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
            <div key={index} className="grid gap-2 rounded-[8px] bg-[color:var(--paper)] p-2 shadow-[inset_0_0_0_1px_var(--line)]">
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

function RunMetric(props: { label: string; value: string }) {
  return (
    <div className="rounded-[8px] bg-[color:var(--paper)] px-3 py-2 shadow-[inset_0_0_0_1px_var(--line)]">
      <div className="text-[11px] font-medium uppercase text-[color:var(--muted)]">{props.label}</div>
      <div className="mt-1 text-[16px] font-semibold text-[color:var(--text)]">{props.value}</div>
    </div>
  );
}
