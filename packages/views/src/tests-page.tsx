import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
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
  X,
} from "lucide-react";
import {
  controlPlaneApi,
  queryKeys,
  type Project,
  type TestCase,
  type TestCaseInput,
  type TestCaseProposal,
  type TestCaseStep,
  type TestPlan,
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
const priorityOptions = ["", "p0", "p1", "p2", "p3"] as const;
const targetTypeOptions = ["branch", "commit", "source_session", "image", "offline_package", "version_url", "preview_url"] as const;
const toolbarSelectClass =
  "h-8 min-h-8 rounded-[6px] bg-transparent px-2 py-1 text-[12px] leading-4 text-[color:var(--muted)] shadow-none hover:bg-[color:var(--hover)] focus:bg-[color:var(--hover)] focus:shadow-[inset_0_0_0_1px_var(--line)] data-[state=open]:bg-[color:var(--hover)] data-[state=open]:shadow-[inset_0_0_0_1px_var(--line)] [&_svg]:size-3.5";

function caseToForm(testCase: TestCase): CaseForm {
  return {
    title: testCase.title,
    area: testCase.area,
    priority: testCase.priority || "",
    status: testCase.status || "draft",
    preconditions: testCase.preconditions,
    steps: testCase.steps.length > 0 ? testCase.steps : [{ action: "", expected: "" }],
    expectedResult: testCase.expectedResult,
    environmentRequirements: testCase.environmentRequirements,
    tagsText: testCase.tags.join(", "),
  };
}

function formToInput(form: CaseForm): TestCaseInput {
  return {
    title: form.title,
    type: "functional",
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
  return [testCase.title, testCase.area, testCase.priority, testCase.status, testCase.tags.join(" ")]
    .join(" ")
    .toLowerCase()
    .includes(normalized);
}

function runPassRate(items: TestRunItem[]) {
  if (items.length === 0) return "0%";
  const passed = items.filter((item) => item.status === "passed").length;
  return `${Math.round((passed / items.length) * 100)}%`;
}

export function TestsPage() {
  const { t } = useMspaceTranslation();
  const queryClient = useQueryClient();
  const auth = useMspaceAuth();
  const workspaceId = auth.workspace?.id || "";
  const serverWorkspaceReady = Boolean(auth.token && workspaceId);
  const projectsQueryKey = queryKeys.workspaceProjects(workspaceId, auth.token);
  const [activeTab, setActiveTab] = useState<TabKey>("cases");
  const [selectedProjectId, setSelectedProjectId] = useState("");
  const [selectedCaseId, setSelectedCaseId] = useState("");
  const [selectedCaseIds, setSelectedCaseIds] = useState<string[]>([]);
  const [selectedPlanId, setSelectedPlanId] = useState("");
  const [selectedRunId, setSelectedRunId] = useState("");
  const [creatingNew, setCreatingNew] = useState(false);
  const [caseForm, setCaseForm] = useState<CaseForm>(emptyCaseForm);
  const [planForm, setPlanForm] = useState<PlanForm>(emptyPlanForm);
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [proposalStatusFilter, setProposalStatusFilter] = useState("pending");
  const [planStatusFilter, setPlanStatusFilter] = useState("all");
  const [importOpen, setImportOpen] = useState(false);
  const [importFormat, setImportFormat] = useState("markdown");
  const [importContent, setImportContent] = useState("");
  const [importSummary, setImportSummary] = useState("");
  const [generateArea, setGenerateArea] = useState("");
  const [generatePrompt, setGeneratePrompt] = useState("");
  const [actionMessage, setActionMessage] = useState("");
  const [reviewNote, setReviewNote] = useState("");

  const projectsQuery = useQuery({
    queryKey: projectsQueryKey,
    queryFn: () => controlPlaneApi.listProjects(auth.token, workspaceId),
    enabled: serverWorkspaceReady,
  });

  const projects = projectsQuery.data || [];
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

  const selectedCaseQuery = useQuery({
    queryKey: queryKeys.projectTestCase(workspaceId, effectiveProjectId, selectedCaseId || "__none", auth.token),
    queryFn: () => controlPlaneApi.getProjectTestCase(auth.token, workspaceId, effectiveProjectId, selectedCaseId),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId && selectedCaseId),
  });

  const revisionsQuery = useQuery({
    queryKey: queryKeys.projectTestCaseRevisions(workspaceId, effectiveProjectId, selectedCaseId || "__none", auth.token),
    queryFn: () => controlPlaneApi.listProjectTestCaseRevisions(auth.token, workspaceId, effectiveProjectId, selectedCaseId),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId && selectedCaseId),
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

  const selectedRunQuery = useQuery({
    queryKey: queryKeys.projectTestRun(workspaceId, effectiveProjectId, selectedRunId || "__none", auth.token),
    queryFn: () => controlPlaneApi.getProjectTestRun(auth.token, workspaceId, effectiveProjectId, selectedRunId),
    enabled: serverWorkspaceReady && Boolean(effectiveProjectId && selectedRunId),
  });

  const cases = useMemo(() => (casesQuery.data || []).filter((testCase) => testCaseMatchesQuery(testCase, query)), [casesQuery.data, query]);
  const readyCases = useMemo(() => (casesQuery.data || []).filter((testCase) => testCase.status === "ready"), [casesQuery.data]);
  const proposals = proposalsQuery.data || [];
  const plans = plansQuery.data || [];
  const selectedCase = selectedCaseQuery.data || cases.find((testCase) => testCase.id === selectedCaseId);
  const selectedPlan = selectedPlanQuery.data?.plan || plans.find((plan) => plan.id === selectedPlanId);
  const selectedPlanDetail = selectedPlanQuery.data;
  const selectedRunDetail = selectedRunQuery.data;
  const revisions = revisionsQuery.data || [];
  const canSave = Boolean(effectiveProjectId && caseForm.title.trim());

  async function invalidateCaseWorkflow() {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: casesQueryKey }),
      queryClient.invalidateQueries({ queryKey: proposalQueryKey }),
      queryClient.invalidateQueries({ queryKey: plansQueryKey }),
      selectedPlanId
        ? queryClient.invalidateQueries({ queryKey: queryKeys.projectTestPlan(workspaceId, effectiveProjectId, selectedPlanId, auth.token) })
        : Promise.resolve(),
      selectedRunId
        ? queryClient.invalidateQueries({ queryKey: queryKeys.projectTestRun(workspaceId, effectiveProjectId, selectedRunId, auth.token) })
        : Promise.resolve(),
    ]);
  }

  const createCase = useMutation({
    mutationFn: (input: TestCaseInput) => controlPlaneApi.createProjectTestCase(auth.token, workspaceId, effectiveProjectId, input),
    onSuccess: async (created) => {
      setSelectedCaseId(created.id);
      setCreatingNew(false);
      setCaseForm(caseToForm(created));
      setActionMessage(t("tests.caseSaved"));
      await invalidateCaseWorkflow();
    },
  });

  const updateCase = useMutation({
    mutationFn: (input: TestCaseInput) =>
      controlPlaneApi.updateProjectTestCase(auth.token, workspaceId, effectiveProjectId, selectedCaseId, input),
    onSuccess: async (updated) => {
      setSelectedCaseId(updated.id);
      setCaseForm(caseToForm(updated));
      setActionMessage(t("tests.caseSaved"));
      await Promise.all([
        invalidateCaseWorkflow(),
        queryClient.invalidateQueries({ queryKey: queryKeys.projectTestCase(workspaceId, effectiveProjectId, updated.id, auth.token) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.projectTestCaseRevisions(workspaceId, effectiveProjectId, updated.id, auth.token) }),
      ]);
    },
  });

  const savePending = createCase.isPending || updateCase.isPending;

  const importCases = useMutation({
    mutationFn: () =>
      controlPlaneApi.importProjectTestCases(auth.token, workspaceId, effectiveProjectId, {
        format: importFormat,
        content: importContent,
      }),
    onSuccess: async (result) => {
      setImportSummary(t("tests.importedSummary", { created: result.created.length, skipped: result.skipped.length }));
      setImportContent("");
      if (result.created[0]) {
        setSelectedCaseId(result.created[0].id);
        setCreatingNew(false);
      }
      await invalidateCaseWorkflow();
    },
  });

  const optimizeCases = useMutation({
    mutationFn: () => controlPlaneApi.optimizeProjectTestCases(auth.token, workspaceId, effectiveProjectId, { caseIds: selectedCaseIds }),
    onSuccess: async (result) => {
      setActionMessage(t("tests.agentSessionQueued", { issueId: result.issueId }));
      setActiveTab("proposals");
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
      setActiveTab("proposals");
      await invalidateCaseWorkflow();
    },
  });

  const applyProposal = useMutation({
    mutationFn: (proposal: TestCaseProposal) =>
      controlPlaneApi.applyProjectTestCaseProposal(auth.token, workspaceId, effectiveProjectId, proposal.id, { note: reviewNote }),
    onSuccess: async (result) => {
      setReviewNote("");
      if (result.testCase) {
        setSelectedCaseId(result.testCase.id);
      }
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
      setActiveTab("plans");
      setActionMessage(t("tests.planCreated"));
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
      setSelectedRunId(detail.run.id);
      setActiveTab("runs");
      setActionMessage(t("tests.runStarted"));
      await invalidateCaseWorkflow();
    },
  });

  const retryRun = useMutation({
    mutationFn: () => controlPlaneApi.retryProjectTestRun(auth.token, workspaceId, effectiveProjectId, selectedRunId, {}),
    onSuccess: async (detail) => {
      setSelectedRunId(detail.run.id);
      setActionMessage(t("tests.runRetried"));
      await invalidateCaseWorkflow();
    },
  });

  const acceptRun = useMutation({
    mutationFn: () => controlPlaneApi.acceptProjectTestRun(auth.token, workspaceId, effectiveProjectId, selectedRunId, { note: reviewNote }),
    onSuccess: async () => {
      setReviewNote("");
      setActionMessage(t("tests.runAccepted"));
      await invalidateCaseWorkflow();
    },
  });

  const blockRun = useMutation({
    mutationFn: () => controlPlaneApi.blockProjectTestRun(auth.token, workspaceId, effectiveProjectId, selectedRunId, { note: reviewNote }),
    onSuccess: async () => {
      setReviewNote("");
      setActionMessage(t("tests.runBlocked"));
      await invalidateCaseWorkflow();
    },
  });

  useEffect(() => {
    if (!selectedProjectId && projects[0]) {
      setSelectedProjectId(projects[0].id);
    }
  }, [projects, selectedProjectId]);

  useEffect(() => {
    if (creatingNew || !selectedCaseQuery.data) return;
    setCaseForm(caseToForm(selectedCaseQuery.data));
  }, [creatingNew, selectedCaseQuery.data]);

  useEffect(() => {
    setSelectedCaseIds((current) => current.filter((caseId) => (casesQuery.data || []).some((testCase) => testCase.id === caseId)));
  }, [casesQuery.data]);

  useEffect(() => {
    if (!selectedPlanId && plans[0]) {
      setSelectedPlanId(plans[0].id);
    }
  }, [plans, selectedPlanId]);

  useEffect(() => {
    if (!selectedRunId && selectedPlanDetail?.runs[0]) {
      setSelectedRunId(selectedPlanDetail.runs[0].id);
    }
  }, [selectedPlanDetail, selectedRunId]);

  function startNewCase() {
    setCreatingNew(true);
    setSelectedCaseId("");
    setCaseForm(emptyCaseForm);
    createCase.reset();
    updateCase.reset();
  }

  function selectCase(testCase: TestCase) {
    setCreatingNew(false);
    setSelectedCaseId(testCase.id);
    setCaseForm(caseToForm(testCase));
    createCase.reset();
    updateCase.reset();
  }

  function toggleCaseSelection(caseId: string) {
    setSelectedCaseIds((current) => (current.includes(caseId) ? current.filter((id) => id !== caseId) : [...current, caseId]));
  }

  function selectReadyCases() {
    setSelectedCaseIds(readyCases.map((testCase) => testCase.id));
  }

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
    if (creatingNew || !selectedCaseId) {
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
                    setSelectedCaseId("");
                    setSelectedCaseIds([]);
                    setSelectedPlanId("");
                    setSelectedRunId("");
                    setCreatingNew(false);
                    setActionMessage("");
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
                    onClick={() => setActiveTab(tab)}
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
            <div className="grid min-h-[620px] gap-5 xl:grid-cols-[minmax(0,1fr)_430px]">
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
                  <Button type="button" variant="secondary" onClick={() => setImportOpen((open) => !open)} disabled={!effectiveProjectId}>
                    <FileUp data-icon />
                    {t("tests.importCases")}
                  </Button>
                  <Button type="button" variant="secondary" onClick={() => optimizeCases.mutate()} disabled={selectedCaseIds.length === 0 || optimizeCases.isPending}>
                    <Sparkles data-icon />
                    {optimizeCases.isPending ? t("tests.optimizing") : t("tests.optimize")}
                  </Button>
                  <Button type="button" onClick={startNewCase} disabled={!effectiveProjectId}>
                    <Plus data-icon />
                    {t("tests.newCase")}
                  </Button>
                </div>

                {importOpen ? (
                  <div className="border-b border-[color:var(--line)] bg-[color:var(--paper)] p-4">
                    <div className="mb-3 flex items-start justify-between gap-3">
                      <div>
                        <h2 className="text-[13px] font-semibold text-[color:var(--text)]">{t("tests.importCases")}</h2>
                        <p className="mt-1 text-[12px] leading-5 text-[color:var(--muted)]">{t("tests.importDescription")}</p>
                      </div>
                      <Button type="button" variant="ghost" size="icon" aria-label={t("tests.closePanel")} onClick={() => setImportOpen(false)}>
                        <X data-icon />
                      </Button>
                    </div>
                    <div className="grid gap-3">
                      <Field label={t("tests.importFormat")}>
                        <Select value={importFormat} onValueChange={setImportFormat}>
                          <SelectTrigger className="h-9">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="markdown">Markdown</SelectItem>
                            <SelectItem value="text">Text</SelectItem>
                            <SelectItem value="csv">CSV</SelectItem>
                          </SelectContent>
                        </Select>
                      </Field>
                      <Field label={t("tests.importContent")}>
                        <Textarea
                          value={importContent}
                          onChange={(event) => setImportContent(event.target.value)}
                          placeholder={t("tests.importPlaceholder")}
                          className="min-h-32"
                        />
                      </Field>
                      <div className="flex flex-wrap items-center gap-2">
                        <Button type="button" onClick={() => importCases.mutate()} disabled={importCases.isPending || !importContent.trim()}>
                          <FileUp data-icon />
                          {importCases.isPending ? t("tests.importing") : t("tests.importCases")}
                        </Button>
                        {importSummary ? <span className="text-[12px] text-[color:var(--muted)]">{importSummary}</span> : null}
                        {importCases.error ? <span className="text-[12px] text-[color:var(--danger)]">{importCases.error.message}</span> : null}
                      </div>
                    </div>
                  </div>
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

                <div className="divide-y divide-[color:var(--line)]">
                  {casesQuery.isLoading ? (
                    <div className="p-6 text-[13px] text-[color:var(--muted)]">{t("tests.loading")}</div>
                  ) : cases.length === 0 ? (
                    <CollectionEmptyState title={query || statusFilter !== "all" ? t("tests.noMatch") : t("tests.emptyTitle")} body={t("tests.emptyBody")} />
                  ) : (
                    cases.map((testCase) => (
                      <div
                        key={testCase.id}
                        className={cn(
                          "grid w-full items-start gap-3 px-4 py-3 transition-colors hover:bg-[color:var(--hover)] md:grid-cols-[24px_minmax(0,1fr)_120px_88px]",
                          selectedCaseId === testCase.id && !creatingNew ? "bg-[color:var(--hover)]" : null,
                        )}
                      >
                        <input
                          type="checkbox"
                          className="mt-1 size-4 accent-[color:var(--accent)]"
                          checked={selectedCaseIds.includes(testCase.id)}
                          aria-label={t("tests.selectCaseAria", { title: testCase.title })}
                          onChange={() => toggleCaseSelection(testCase.id)}
                        />
                        <button type="button" onClick={() => selectCase(testCase)} className="min-w-0 text-left">
                          <div className="flex min-w-0 items-center gap-2">
                            <ClipboardCheck data-icon className="size-4 shrink-0 text-[color:var(--muted)]" />
                            <span className="truncate text-[13px] font-medium text-[color:var(--text)]">{testCase.title}</span>
                          </div>
                          <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[12px] text-[color:var(--muted)]">
                            <span>{testCase.area || t("common.unknown")}</span>
                            <span>
                              {t("tests.source")}: {t(`tests.sourceValue.${testCase.source}`, { defaultValue: testCase.source })}
                            </span>
                            <span>
                              {t("tests.updated")} <RelativeTime value={testCase.updatedAt} />
                            </span>
                          </div>
                        </button>
                        <div className="flex items-center md:justify-end">
                          <StatusBadge value={testCase.status} valueLabel={t(`tests.statusValue.${testCase.status}`, { defaultValue: testCase.status })} />
                        </div>
                        <div className={cn("flex items-center text-[13px] font-semibold md:justify-end", scoreTone(testCase.qualityScore))}>
                          {testCase.qualityScore}
                        </div>
                      </div>
                    ))
                  )}
                </div>
              </section>

              <aside className="min-w-0 rounded-[10px] bg-[color:var(--surface)] p-4 shadow-[inset_0_0_0_1px_var(--line)]">
                {creatingNew || selectedCase ? (
                  <form className="grid gap-4" onSubmit={submitCase}>
                    <div className="flex items-start justify-between gap-3 border-b border-[color:var(--line)] pb-3">
                      <div className="min-w-0">
                        <h2 className="truncate text-[15px] font-semibold text-[color:var(--text)]">
                          {creatingNew ? t("tests.newCase") : selectedCase?.title}
                        </h2>
                        {selectedCase ? (
                          <p className="mt-1 text-[12px] text-[color:var(--muted)]">
                            {t("tests.quality")}: <span className={cn("font-semibold", scoreTone(selectedCase.qualityScore))}>{selectedCase.qualityScore}</span>
                          </p>
                        ) : null}
                      </div>
                      <Button type="submit" disabled={!canSave || savePending}>
                        <Save data-icon />
                        {savePending ? t("tests.saving") : creatingNew ? t("tests.createCase") : t("tests.saveCase")}
                      </Button>
                    </div>

                    <Field label={t("tests.titleLabel")}>
                      <Input value={caseForm.title} onChange={(event) => setCaseForm((current) => ({ ...current, title: event.target.value }))} />
                    </Field>
                    <div className="grid gap-3 sm:grid-cols-2">
                      <Field label={t("tests.area")}>
                        <Input
                          value={caseForm.area}
                          onChange={(event) => setCaseForm((current) => ({ ...current, area: event.target.value }))}
                          placeholder={t("tests.areaPlaceholder")}
                        />
                      </Field>
                      <Field label={t("tests.priority")}>
                        <Select value={caseForm.priority || "__none"} onValueChange={(value) => setCaseForm((current) => ({ ...current, priority: value === "__none" ? "" : value }))}>
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
                    </div>
                    <Field label={t("tests.status")}>
                      <Select value={caseForm.status} onValueChange={(value) => setCaseForm((current) => ({ ...current, status: value }))}>
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
                    <Field label={t("tests.preconditions")}>
                      <Textarea value={caseForm.preconditions} onChange={(event) => setCaseForm((current) => ({ ...current, preconditions: event.target.value }))} />
                    </Field>
                    <Field label={t("tests.steps")}>
                      <div className="grid gap-2">
                        {caseForm.steps.map((step, index) => (
                          <div key={index} className="grid gap-2 rounded-[8px] bg-[color:var(--paper)] p-2 shadow-[inset_0_0_0_1px_var(--line)]">
                            <Input
                              value={step.action}
                              onChange={(event) => updateStep(index, { action: event.target.value })}
                              placeholder={t("tests.stepAction", { index: index + 1 })}
                            />
                            <Input
                              value={step.expected || ""}
                              onChange={(event) => updateStep(index, { expected: event.target.value })}
                              placeholder={t("tests.stepExpected", { index: index + 1 })}
                            />
                          </div>
                        ))}
                        <Button
                          type="button"
                          variant="secondary"
                          onClick={() => setCaseForm((current) => ({ ...current, steps: [...current.steps, { action: "", expected: "" }] }))}
                        >
                          <Plus data-icon />
                          {t("tests.addStep")}
                        </Button>
                      </div>
                    </Field>
                    <Field label={t("tests.expectedResult")}>
                      <Textarea value={caseForm.expectedResult} onChange={(event) => setCaseForm((current) => ({ ...current, expectedResult: event.target.value }))} />
                    </Field>
                    <Field label={t("tests.environmentRequirements")}>
                      <Textarea value={caseForm.environmentRequirements} onChange={(event) => setCaseForm((current) => ({ ...current, environmentRequirements: event.target.value }))} />
                    </Field>
                    <Field label={t("tests.tags")} hint={t("tests.tagsHint")}>
                      <Input value={caseForm.tagsText} onChange={(event) => setCaseForm((current) => ({ ...current, tagsText: event.target.value }))} />
                    </Field>

                    {selectedCase ? (
                      <div className="grid gap-4 border-t border-[color:var(--line)] pt-4">
                        <section>
                          <h3 className="mb-2 text-[13px] font-semibold text-[color:var(--muted-strong)]">{t("tests.findings")}</h3>
                          {selectedCase.qualityFindings.length === 0 ? (
                            <p className="text-[12px] text-[color:var(--muted)]">{t("tests.noFindings")}</p>
                          ) : (
                            <div className="grid gap-2">
                              {selectedCase.qualityFindings.map((finding) => (
                                <div key={finding.code} className="rounded-[8px] bg-[color:var(--paper)] px-3 py-2 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
                                  <span className="font-medium text-[color:var(--muted-strong)]">{finding.code}</span>: {finding.message}
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
                              {revisions.map((revision) => (
                                <div key={revision.id} className="rounded-[8px] bg-[color:var(--paper)] px-3 py-2 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
                                  <div className="font-medium text-[color:var(--text)]">#{revision.revisionNumber} - {revision.snapshot.title}</div>
                                  <div>
                                    <RelativeTime value={revision.createdAt} />
                                  </div>
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
                ) : (
                  <CollectionEmptyState title={t("tests.cases")} body={t("tests.selectCase")} />
                )}
              </aside>
            </div>
          ) : null}

          {activeTab === "proposals" ? (
            <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_360px]">
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
                  </div>
                  <span className="text-[12px] text-[color:var(--muted)]">
                    {t("tests.selectedSummary", { selected: selectedCaseIds.length, total: casesQuery.data?.length || 0 })}
                  </span>
                </div>
                <div className="divide-y divide-[color:var(--line)]">
                  {proposalsQuery.isLoading ? (
                    <div className="p-6 text-[13px] text-[color:var(--muted)]">{t("tests.loadingProposals")}</div>
                  ) : proposals.length === 0 ? (
                    <CollectionEmptyState title={t("tests.noProposalsTitle")} body={t("tests.noProposalsBody")} />
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
                        <div className="grid gap-3 md:grid-cols-2">
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

              <aside className="min-w-0 rounded-[10px] bg-[color:var(--surface)] p-4 shadow-[inset_0_0_0_1px_var(--line)]">
                <div className="grid gap-4">
                  <div>
                    <h2 className="text-[15px] font-semibold text-[color:var(--text)]">{t("tests.generateTitle")}</h2>
                    <p className="mt-1 text-[12px] leading-5 text-[color:var(--muted)]">{t("tests.generateDescription")}</p>
                  </div>
                  <Field label={t("tests.area")}>
                    <Input value={generateArea} onChange={(event) => setGenerateArea(event.target.value)} placeholder={t("tests.areaPlaceholder")} />
                  </Field>
                  <Field label={t("tests.generatePrompt")}>
                    <Textarea value={generatePrompt} onChange={(event) => setGeneratePrompt(event.target.value)} placeholder={t("tests.generatePlaceholder")} />
                  </Field>
                  <Field label={t("tests.reviewNote")}>
                    <Textarea value={reviewNote} onChange={(event) => setReviewNote(event.target.value)} placeholder={t("tests.reviewNotePlaceholder")} className="min-h-20" />
                  </Field>
                  <Button type="button" onClick={() => generateCases.mutate()} disabled={generateCases.isPending || !effectiveProjectId}>
                    <Sparkles data-icon />
                    {generateCases.isPending ? t("tests.generating") : t("tests.generate")}
                  </Button>
                  {(optimizeCases.error || generateCases.error || applyProposal.error || rejectProposal.error) ? (
                    <p className="text-[12px] text-[color:var(--danger)]">
                      {(optimizeCases.error || generateCases.error || applyProposal.error || rejectProposal.error)?.message}
                    </p>
                  ) : null}
                </div>
              </aside>
            </div>
          ) : null}

          {activeTab === "plans" ? (
            <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_420px]">
              <section className="min-w-0 rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
                <div className="flex flex-wrap items-center justify-between gap-2 border-b border-[color:var(--line)] p-3">
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
                  <span className="text-[12px] text-[color:var(--muted)]">
                    {t("tests.selectedSummary", { selected: selectedCaseIds.length, total: readyCases.length })}
                  </span>
                </div>
                <div className="divide-y divide-[color:var(--line)]">
                  {plansQuery.isLoading ? (
                    <div className="p-6 text-[13px] text-[color:var(--muted)]">{t("tests.loadingPlans")}</div>
                  ) : plans.length === 0 ? (
                    <CollectionEmptyState title={t("tests.noPlansTitle")} body={t("tests.noPlansBody")} />
                  ) : (
                    plans.map((plan) => (
                      <button
                        key={plan.id}
                        type="button"
                        onClick={() => setSelectedPlanId(plan.id)}
                        className={cn(
                          "grid w-full gap-3 px-4 py-3 text-left transition-colors hover:bg-[color:var(--hover)] md:grid-cols-[minmax(0,1fr)_120px_88px]",
                          selectedPlanId === plan.id ? "bg-[color:var(--hover)]" : null,
                        )}
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
                      </button>
                    ))
                  )}
                </div>
              </section>

              <aside className="min-w-0 rounded-[10px] bg-[color:var(--surface)] p-4 shadow-[inset_0_0_0_1px_var(--line)]">
                <div className="grid gap-5">
                  <section className="grid gap-3">
                    <div>
                      <h2 className="text-[15px] font-semibold text-[color:var(--text)]">{t("tests.createPlan")}</h2>
                      <p className="mt-1 text-[12px] leading-5 text-[color:var(--muted)]">{t("tests.createPlanDescription")}</p>
                    </div>
                    <Field label={t("tests.planTitle")}>
                      <Input value={planForm.title} onChange={(event) => setPlanForm((current) => ({ ...current, title: event.target.value }))} placeholder={t("tests.planTitlePlaceholder")} />
                    </Field>
                    <Field label={t("tests.planDescription")}>
                      <Textarea value={planForm.description} onChange={(event) => setPlanForm((current) => ({ ...current, description: event.target.value }))} className="min-h-20" />
                    </Field>
                    <div className="grid gap-3 sm:grid-cols-2">
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
                    <Field label={t("tests.environment")}>
                      <Textarea value={planForm.environment} onChange={(event) => setPlanForm((current) => ({ ...current, environment: event.target.value }))} className="min-h-20" />
                    </Field>
                    <div className="rounded-[8px] bg-[color:var(--paper)] p-3 shadow-[inset_0_0_0_1px_var(--line)]">
                      <div className="mb-2 flex items-center justify-between gap-2">
                        <span className="text-[13px] font-medium text-[color:var(--muted-strong)]">{t("tests.readyCases")}</span>
                        <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-[12px]" onClick={selectReadyCases} disabled={readyCases.length === 0}>
                          {t("tests.selectReady")}
                        </Button>
                      </div>
                      <div className="grid max-h-52 gap-2 overflow-auto pr-1">
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
                    <Button type="button" onClick={() => createPlan.mutate()} disabled={createPlan.isPending || !planForm.title.trim() || selectedCaseIds.length === 0}>
                      <Plus data-icon />
                      {createPlan.isPending ? t("tests.creatingPlan") : t("tests.createPlan")}
                    </Button>
                    {createPlan.error ? <p className="text-[12px] text-[color:var(--danger)]">{createPlan.error.message}</p> : null}
                  </section>

                  <section className="grid gap-3 border-t border-[color:var(--line)] pt-4">
                    <div>
                      <h2 className="text-[15px] font-semibold text-[color:var(--text)]">{selectedPlan?.title || t("tests.selectedPlan")}</h2>
                      <p className="mt-1 text-[12px] leading-5 text-[color:var(--muted)]">
                        {selectedPlan ? `${selectedPlan.targetValue || t("common.unknown")} - ${t("tests.caseCount", { count: selectedPlan.caseCount })}` : t("tests.selectPlan")}
                      </p>
                    </div>
                    {selectedPlanDetail ? (
                      <div className="grid gap-2">
                        {selectedPlanDetail.cases.map((planCase) => (
                          <div key={planCase.id} className="rounded-[8px] bg-[color:var(--paper)] px-3 py-2 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
                            <div className="font-medium text-[color:var(--text)]">{planCase.testCase.title}</div>
                            <div>{planCase.testCase.area || t("common.unknown")}</div>
                          </div>
                        ))}
                      </div>
                    ) : null}
                    <Button type="button" onClick={() => selectedPlan && startRun.mutate(selectedPlan)} disabled={!selectedPlan || startRun.isPending}>
                      <Play data-icon />
                      {startRun.isPending ? t("tests.startingRun") : t("tests.startRun")}
                    </Button>
                    {startRun.error ? <p className="text-[12px] text-[color:var(--danger)]">{startRun.error.message}</p> : null}
                  </section>
                </div>
              </aside>
            </div>
          ) : null}

          {activeTab === "runs" ? (
            <div className="grid gap-5 xl:grid-cols-[360px_minmax(0,1fr)]">
              <aside className="min-w-0 rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
                <div className="border-b border-[color:var(--line)] p-3">
                  <h2 className="text-[13px] font-semibold text-[color:var(--text)]">{t("tests.runs")}</h2>
                  <p className="mt-1 text-[12px] text-[color:var(--muted)]">{selectedPlan?.title || t("tests.selectPlan")}</p>
                </div>
                <div className="divide-y divide-[color:var(--line)]">
                  {!selectedPlanDetail ? (
                    <CollectionEmptyState title={t("tests.noRunsTitle")} body={t("tests.noRunsBody")} />
                  ) : selectedPlanDetail.runs.length === 0 ? (
                    <CollectionEmptyState title={t("tests.noRunsTitle")} body={t("tests.noRunsBody")} />
                  ) : (
                    selectedPlanDetail.runs.map((run) => (
                      <button
                        key={run.id}
                        type="button"
                        onClick={() => setSelectedRunId(run.id)}
                        className={cn("grid w-full gap-2 px-4 py-3 text-left transition-colors hover:bg-[color:var(--hover)]", selectedRunId === run.id ? "bg-[color:var(--hover)]" : null)}
                      >
                        <div className="flex items-center justify-between gap-2">
                          <span className="text-[13px] font-medium text-[color:var(--text)]">{t("tests.runShortId", { id: run.id.slice(0, 8) })}</span>
                          <StatusBadge value={run.status} valueLabel={t(`tests.runStatusValue.${run.status}`, { defaultValue: run.status })} />
                        </div>
                        <div className="text-[12px] text-[color:var(--muted)]">
                          {t("tests.runCounts", { passed: run.passedCount, failed: run.failedCount, blocked: run.blockedCount, skipped: run.skippedCount })}
                        </div>
                      </button>
                    ))
                  )}
                </div>
              </aside>

              <section className="min-w-0 rounded-[10px] bg-[color:var(--surface)] p-4 shadow-[inset_0_0_0_1px_var(--line)]">
                {!selectedRunDetail ? (
                  <CollectionEmptyState title={t("tests.noRunSelectedTitle")} body={t("tests.noRunSelectedBody")} />
                ) : (
                  <div className="grid gap-4">
                    <div className="flex flex-wrap items-start justify-between gap-3 border-b border-[color:var(--line)] pb-4">
                      <div className="min-w-0">
                        <h2 className="text-[15px] font-semibold text-[color:var(--text)]">{selectedRunDetail.plan.title}</h2>
                        <p className="mt-1 text-[12px] leading-5 text-[color:var(--muted)]">
                          {t("tests.runTarget", {
                            type: t(`tests.targetTypeValue.${selectedRunDetail.run.targetType}`, { defaultValue: selectedRunDetail.run.targetType }),
                            value: selectedRunDetail.run.targetValue || t("common.unknown"),
                          })}
                        </p>
                      </div>
                      <StatusBadge value={selectedRunDetail.run.status} valueLabel={t(`tests.runStatusValue.${selectedRunDetail.run.status}`, { defaultValue: selectedRunDetail.run.status })} />
                    </div>

                    <div className="grid gap-3 md:grid-cols-5">
                      <RunMetric label={t("tests.total")} value={String(selectedRunDetail.items.length)} />
                      <RunMetric label={t("tests.passed")} value={String(selectedRunDetail.run.passedCount)} />
                      <RunMetric label={t("tests.failed")} value={String(selectedRunDetail.run.failedCount)} />
                      <RunMetric label={t("tests.blocked")} value={String(selectedRunDetail.run.blockedCount)} />
                      <RunMetric label={t("tests.passRate")} value={runPassRate(selectedRunDetail.items)} />
                    </div>

                    <Field label={t("tests.reviewNote")}>
                      <Textarea value={reviewNote} onChange={(event) => setReviewNote(event.target.value)} placeholder={t("tests.reviewNotePlaceholder")} className="min-h-20" />
                    </Field>

                    <div className="flex flex-wrap items-center gap-2">
                      <Button type="button" variant="secondary" onClick={() => retryRun.mutate()} disabled={retryRun.isPending || !selectedRunId}>
                        <RotateCcw data-icon />
                        {retryRun.isPending ? t("tests.retryingRun") : t("tests.retryRun")}
                      </Button>
                      <Button type="button" variant="secondary" onClick={() => blockRun.mutate()} disabled={blockRun.isPending || !selectedRunId}>
                        <Ban data-icon />
                        {blockRun.isPending ? t("tests.blockingRun") : t("tests.blockRun")}
                      </Button>
                      <Button type="button" onClick={() => acceptRun.mutate()} disabled={acceptRun.isPending || !selectedRunId}>
                        <Check data-icon />
                        {acceptRun.isPending ? t("tests.acceptingRun") : t("tests.acceptRun")}
                      </Button>
                    </div>
                    {(retryRun.error || acceptRun.error || blockRun.error) ? (
                      <p className="text-[12px] text-[color:var(--danger)]">{(retryRun.error || acceptRun.error || blockRun.error)?.message}</p>
                    ) : null}

                    <div className="divide-y divide-[color:var(--line)] rounded-[10px] shadow-[inset_0_0_0_1px_var(--line)]">
                      {selectedRunDetail.items.map((item) => (
                        <div key={item.id} className="grid gap-3 p-3 md:grid-cols-[minmax(0,1fr)_140px]">
                          <div className="min-w-0">
                            <div className="flex min-w-0 items-center gap-2">
                              <ClipboardCheck data-icon className="size-4 shrink-0 text-[color:var(--muted)]" />
                              <span className="truncate text-[13px] font-medium text-[color:var(--text)]">{item.testCase.title}</span>
                            </div>
                            <p className="mt-1 text-[12px] leading-5 text-[color:var(--muted)]">{item.actualResult || item.failureSummary || t("tests.noResultYet")}</p>
                            {Object.keys(item.evidence || {}).length > 0 ? (
                              <pre className="mt-2 max-h-28 overflow-auto rounded-[8px] bg-[color:var(--paper)] p-2 text-[11px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
                                {JSON.stringify(item.evidence, null, 2)}
                              </pre>
                            ) : null}
                          </div>
                          <div className="flex items-start md:justify-end">
                            <StatusBadge value={item.status} valueLabel={t(`tests.runItemStatusValue.${item.status}`, { defaultValue: item.status })} />
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </section>
            </div>
          ) : null}
        </div>
      )}
    </PageFrame>
  );
}

function ProposalCasePreview(props: { title: string; testCase?: TestCase; input?: TestCaseInput; emptyText: string }) {
  const subject = props.testCase || props.input;
  return (
    <div className="rounded-[8px] bg-[color:var(--paper)] px-3 py-2 text-[12px] leading-5 shadow-[inset_0_0_0_1px_var(--line)]">
      <div className="mb-1 font-medium text-[color:var(--muted-strong)]">{props.title}</div>
      {subject ? (
        <div className="grid gap-1 text-[color:var(--muted)]">
          <div className="font-medium text-[color:var(--text)]">{subject.title}</div>
          <div>{subject.preconditions}</div>
          <div>{subject.expectedResult}</div>
        </div>
      ) : (
        <div className="text-[color:var(--muted)]">{props.emptyText}</div>
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
