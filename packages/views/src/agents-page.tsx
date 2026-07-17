import { useEffect, useMemo, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  AlertCircle,
  Bot,
  CheckCircle2,
  Circle,
  CircleHelp,
  Copy,
  Eye,
  FileText,
  MoreHorizontal,
  Pencil,
  Plus,
  Power,
  Save,
  ServerCog,
  Trash2,
  Wrench,
  X,
} from "lucide-react";
import {
  agentEngineDiagnosticDisplayState,
  agentRequiredCapabilities,
  controlPlaneApi,
  isCurrentHostPrimaryWorker,
  isCurrentHostWorker,
  isFixedAgentEngineCatalogItem,
  queryKeys,
  resolveAgentEngineDiagnostic,
  runtimeWorkerLabel,
  runtimeWorkerLiveness,
  type AgentEngine,
  type AgentEngineCatalogItem,
  type AgentEngineReadinessStatus,
  type ResolvedAgentEngineDiagnostic,
  type RuntimeAvailability,
  type RuntimeSkillFile,
  type RuntimeWorker,
  type RuntimeWorkerLiveness,
  type SkillCatalogItem,
  type SkillDetail,
  type SkillInput,
} from "@mspace/core";
import { useMspaceTranslation } from "@mspace/i18n";
import {
  Button,
  CollectionEmptyState,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Field,
  Input,
  Notice,
  PageFrame,
  Textarea,
  cn,
} from "@mspace/ui";
import { codexAvatarDataUrl } from "./agent-avatar";
import { useMspaceAuth } from "./auth-context";
import { formatAbsoluteTime, RelativeTime } from "./time";

type AgentsTab = "agents" | "skills";

const DEFAULT_ACTIVE_WORKER_MAX_AGE_MS = 45_000;

type SkillForm = {
  slug: string;
  name: string;
  description: string;
  enabled: boolean;
  skillMd: string;
  files: RuntimeSkillFile[];
};

const emptySkillForm: SkillForm = {
  slug: "",
  name: "",
  description: "",
  enabled: true,
  skillMd: "---\nname: \ndescription: \n---\n# Skill\n",
  files: [],
};

function skillIdentifier(skill: SkillCatalogItem) {
  return skill.builtIn ? skill.slug : skill.id || skill.slug;
}

function skillToForm(skill: SkillDetail): SkillForm {
  const files = skill.files || [];
  const skillMd = files.find((file) => file.path === "SKILL.md")?.content || emptySkillForm.skillMd;
  return {
    slug: skill.slug,
    name: skill.name,
    description: skill.description,
    enabled: skill.enabled,
    skillMd,
    files,
  };
}

function skillFormToInput(form: SkillForm, includeSlug: boolean): SkillInput {
  const files = form.files.length > 0 ? form.files : [{ path: "SKILL.md", content: form.skillMd }];
  const nextFiles = files.some((file) => file.path === "SKILL.md")
    ? files.map((file) => (file.path === "SKILL.md" ? { ...file, content: form.skillMd } : file))
    : [{ path: "SKILL.md", content: form.skillMd }, ...files];
  return {
    slug: includeSlug ? form.slug.trim() : undefined,
    name: form.name.trim(),
    description: form.description.trim(),
    enabled: form.enabled,
    files: nextFiles.map((file) => ({ path: file.path, content: file.content })),
  };
}

export function AgentsPage() {
  const { t } = useMspaceTranslation();
  const queryClient = useQueryClient();
  const auth = useMspaceAuth();
  const workspaceId = auth.workspace?.id || "";
  const workspaceReady = auth.status === "signed-in" && Boolean(auth.token && workspaceId);
  const canManageWorkspace = auth.workspace?.role === "owner" || auth.workspace?.role === "admin";
  const runtimeMode = auth.workspace?.kind === "team" ? "team" : "personal";
  const getPersonalWorkerHostId = typeof window !== "undefined" ? window.mspaceDesktop?.getPersonalWorkerHostId : undefined;
  const canReadCurrentHostId = workspaceReady && runtimeMode === "personal" && Boolean(getPersonalWorkerHostId);
  const agentsQueryKey = queryKeys.agents(workspaceId, auth.token);
  const skillsQueryKey = queryKeys.skills(workspaceId, auth.token);
  const agentsQuery = useQuery({
    queryKey: agentsQueryKey,
    queryFn: () => controlPlaneApi.listAgents(auth.token, workspaceId),
    enabled: workspaceReady,
  });
  const workersQuery = useQuery({
    queryKey: queryKeys.runtimeWorkers(workspaceId, auth.token),
    queryFn: () => controlPlaneApi.listRuntimeWorkers(auth.token, workspaceId),
    enabled: workspaceReady,
    refetchInterval: 5_000,
  });
  const currentHostIdQuery = useQuery({
    queryKey: ["desktop-personal-worker-host-id"],
    queryFn: async () => String(await getPersonalWorkerHostId?.() || "").trim(),
    enabled: canReadCurrentHostId,
    staleTime: Number.POSITIVE_INFINITY,
    retry: false,
  });
  const skillsQuery = useQuery({
    queryKey: skillsQueryKey,
    queryFn: () => controlPlaneApi.listSkills(auth.token, workspaceId),
    enabled: workspaceReady,
  });
  const agents = useMemo(() => (agentsQuery.data || []).filter(isFixedAgentEngineCatalogItem), [agentsQuery.data]);
  const skills = useMemo(() => skillsQuery.data || [], [skillsQuery.data]);
  const workers = useMemo(
    () => (workersQuery.data || []).filter((worker) => worker.mode === runtimeMode),
    [runtimeMode, workersQuery.data],
  );
  const agentAvailability = useQueries({
    queries: agents.map((agent) => {
      const input = { runtimeMode, requiredCapabilities: agentRequiredCapabilities(agent) };
      return {
        queryKey: queryKeys.runtimeAvailability(workspaceId, auth.token, input),
        queryFn: () => controlPlaneApi.getRuntimeAvailability(auth.token, workspaceId, input),
        enabled: workspaceReady,
        refetchInterval: 5_000,
      };
    }),
  });
  const [activeTab, setActiveTab] = useState<AgentsTab>("agents");
  const [skillModalMode, setSkillModalMode] = useState<"create" | "edit" | "view" | null>(null);
  const [editingSkill, setEditingSkill] = useState<SkillCatalogItem | null>(null);
  const [skillForm, setSkillForm] = useState<SkillForm>(emptySkillForm);

  const loadSkill = useMutation({
    mutationFn: (skill: SkillCatalogItem) => controlPlaneApi.getSkill(auth.token, workspaceId, skillIdentifier(skill)),
    onSuccess: (skill) => {
      setEditingSkill(skill);
      setSkillForm(skillToForm(skill));
      setSkillModalMode(skill.editable ? "edit" : "view");
    },
  });

  const createSkill = useMutation({
    mutationFn: (input: SkillInput) => controlPlaneApi.createSkill(auth.token, workspaceId, input),
    onSuccess: async () => {
      setSkillModalMode(null);
      setSkillForm(emptySkillForm);
      await queryClient.invalidateQueries({ queryKey: skillsQueryKey });
    },
  });

  const updateSkill = useMutation({
    mutationFn: (input: SkillInput) => {
      if (!editingSkill) throw new Error("No skill selected.");
      return controlPlaneApi.updateSkill(auth.token, workspaceId, skillIdentifier(editingSkill), input);
    },
    onSuccess: async () => {
      setEditingSkill(null);
      setSkillModalMode(null);
      setSkillForm(emptySkillForm);
      await queryClient.invalidateQueries({ queryKey: skillsQueryKey });
    },
  });

  const toggleSkill = useMutation({
    mutationFn: (input: { skill: SkillCatalogItem; values: SkillInput }) =>
      controlPlaneApi.updateSkill(auth.token, workspaceId, skillIdentifier(input.skill), input.values),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: skillsQueryKey });
    },
  });

  const deleteSkill = useMutation({
    mutationFn: (skill: SkillCatalogItem) => controlPlaneApi.deleteSkill(auth.token, workspaceId, skillIdentifier(skill)),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: skillsQueryKey });
    },
  });

  const duplicateSkill = useMutation({
    mutationFn: (skill: SkillCatalogItem) => controlPlaneApi.duplicateSkill(auth.token, workspaceId, skillIdentifier(skill)),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: skillsQueryKey });
    },
  });

  function openCreateSkillModal() {
    if (!canManageWorkspace) return;
    setEditingSkill(null);
    setSkillForm(emptySkillForm);
    createSkill.reset();
    setSkillModalMode("create");
  }

  function openSkillSettings(skill: SkillCatalogItem) {
    if (!canManageWorkspace) return;
    loadSkill.reset();
    loadSkill.mutate(skill);
  }

  function submitSkill(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canManageWorkspace) return;
    if (skillModalMode === "create") {
      createSkill.mutate(skillFormToInput(skillForm, true));
      return;
    }
    updateSkill.mutate(skillFormToInput(skillForm, false));
  }

  function closeSkillModal() {
    setSkillModalMode(null);
    setEditingSkill(null);
    setSkillForm(emptySkillForm);
    createSkill.reset();
    updateSkill.reset();
  }

  const tabAction = activeTab === "skills" ? (
      canManageWorkspace ? (
        <Button variant="secondary" onClick={openCreateSkillModal}>
          <Plus data-icon />
          {t("agents.skills.newSkill")}
        </Button>
      ) : null
    ) : null;

  return (
    <PageFrame
      title={t("agents.title")}
      subtitle={t("agents.subtitle")}
    >
      {!workspaceReady ? <Notice>{t("workspace.signInRequired")}</Notice> : null}
      <div className="space-y-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <AgentsTabs activeTab={activeTab} onChange={setActiveTab} />
          {tabAction}
        </div>

        {activeTab === "agents" ? (
          <AgentsPanel
            agents={agents}
            workers={workers}
            isPending={agentsQuery.isPending}
            workersPending={workersQuery.isPending}
            localIdentityPending={canReadCurrentHostId && currentHostIdQuery.isPending}
            error={agentsQuery.error || workersQuery.error || agentAvailability.find((query) => query.error)?.error}
            availability={agentAvailability.map((query) => query.data)}
            availabilityPending={agentAvailability.map((query) => query.isPending)}
            runtimeMode={runtimeMode}
            currentHostId={runtimeMode === "personal" ? currentHostIdQuery.data || "" : ""}
          />
        ) : (
          <SkillsPanel
            skills={skills}
            isPending={skillsQuery.isPending}
            canManage={canManageWorkspace}
            error={skillsQuery.error || loadSkill.error || createSkill.error || updateSkill.error || toggleSkill.error || deleteSkill.error || duplicateSkill.error}
            isMutating={loadSkill.isPending || createSkill.isPending || updateSkill.isPending || toggleSkill.isPending || deleteSkill.isPending || duplicateSkill.isPending}
            onCreate={openCreateSkillModal}
            onEdit={openSkillSettings}
            onDuplicate={(skill) => {
              if (canManageWorkspace) duplicateSkill.mutate(skill);
            }}
            onDelete={(skill) => {
              if (canManageWorkspace && window.confirm(t("agents.skills.deleteConfirm", { name: skill.name || skill.slug }))) {
                deleteSkill.mutate(skill);
              }
            }}
            onToggleEnabled={(skill) => {
              if (canManageWorkspace) toggleSkill.mutate({ skill, values: { enabled: !skill.enabled } });
            }}
          />
        )}
      </div>

      {skillModalMode ? (
        <SkillModal
          mode={skillModalMode}
          form={skillForm}
          isPending={createSkill.isPending || updateSkill.isPending}
          error={skillModalMode === "create" ? createSkill.error : updateSkill.error}
          onClose={closeSkillModal}
          onSubmit={submitSkill}
          onChange={setSkillForm}
        />
      ) : null}
    </PageFrame>
  );
}

function AgentsTabs(props: { activeTab: AgentsTab; onChange: (tab: AgentsTab) => void }) {
  const { t } = useMspaceTranslation();
  return (
    <div className="inline-flex w-fit rounded-[8px] bg-[color:var(--block)] p-1 shadow-[inset_0_0_0_1px_var(--line)]" role="tablist">
      {(["agents", "skills"] as const).map((tab) => (
        <button
          key={tab}
          type="button"
          role="tab"
          aria-selected={props.activeTab === tab}
          onClick={() => props.onChange(tab)}
          className={cn(
            "inline-flex h-8 items-center gap-2 rounded-[6px] px-3 text-[13px] font-medium transition-colors",
            props.activeTab === tab
              ? "bg-[color:var(--surface)] text-[color:var(--text)] shadow-[0_1px_2px_rgba(31,31,31,0.08)]"
              : "text-[color:var(--muted)] hover:text-[color:var(--text)]",
          )}
        >
          {tab === "agents" ? <Bot data-icon /> : <FileText data-icon />}
          {tab === "agents" ? t("agents.tabs.agents") : t("agents.tabs.skills")}
        </button>
      ))}
    </div>
  );
}

function AgentsPanel(props: {
  agents: AgentEngineCatalogItem[];
  workers: RuntimeWorker[];
  isPending: boolean;
  workersPending: boolean;
  localIdentityPending: boolean;
  error?: Error | null;
  availability: Array<RuntimeAvailability | undefined>;
  availabilityPending: boolean[];
  runtimeMode: string;
  currentHostId: string;
}) {
  const { t } = useMspaceTranslation();
  if (props.isPending) {
    return (
      <div className="rounded-[10px] bg-[color:var(--surface)] px-4 py-6 text-[13px] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
        {t("agents.loading")}
      </div>
    );
  }
  if (props.agents.length === 0) {
    return (
      <CollectionEmptyState
        icon={Bot}
        title={t("agents.emptyTitle")}
        body={t("agents.emptyBody")}
      />
    );
  }
  const activeWorkerMaxAgeMs = props.availability.find((availability) => availability?.activeWorkerMaxAgeMs)?.activeWorkerMaxAgeMs
    || DEFAULT_ACTIVE_WORKER_MAX_AGE_MS;
  const workers = [...props.workers].sort((left, right) => {
    const leftLocal = isCurrentHostPrimaryWorker(left, props.runtimeMode, props.currentHostId)
      ? 2
      : isCurrentHostWorker(left, props.runtimeMode, props.currentHostId) ? 1 : 0;
    const rightLocal = isCurrentHostPrimaryWorker(right, props.runtimeMode, props.currentHostId)
      ? 2
      : isCurrentHostWorker(right, props.runtimeMode, props.currentHostId) ? 1 : 0;
    if (leftLocal !== rightLocal) return rightLocal - leftLocal;
    const leftOnline = runtimeWorkerLiveness(left, activeWorkerMaxAgeMs) === "online" ? 1 : 0;
    const rightOnline = runtimeWorkerLiveness(right, activeWorkerMaxAgeMs) === "online" ? 1 : 0;
    if (leftOnline !== rightOnline) return rightOnline - leftOnline;
    return new Date(right.lastSeenAt).getTime() - new Date(left.lastSeenAt).getTime();
  });
  const localPrimaryWorker = workers.find((worker) => isCurrentHostPrimaryWorker(worker, props.runtimeMode, props.currentHostId));

  return (
    <div className="space-y-5">
      <section aria-labelledby="agent-readiness-heading">
        <div className="mb-2 min-w-0">
          <h2 id="agent-readiness-heading" className="text-[14px] font-semibold leading-6 text-[color:var(--text)]">{t("agents.readiness.title")}</h2>
          <p className="text-[12px] leading-5 text-[color:var(--muted)]">{t("agents.readiness.description")}</p>
        </div>
        <div className="overflow-hidden rounded-[8px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
          {props.error ? <div className="border-b border-[color:var(--line)] px-4 py-3"><Notice tone="danger">{props.error.message}</Notice></div> : null}
          <div className="overflow-x-auto">
            <div className="min-w-[780px]">
              <div className="grid grid-cols-[minmax(300px,1.35fr)_minmax(220px,1fr)_minmax(180px,0.8fr)] gap-4 border-b border-[color:var(--line)] px-4 py-2.5 text-[12px] font-medium text-[color:var(--muted)]">
                <span>{t("agents.agent")}</span>
                <span>{t("agents.readiness.thisMac")}</span>
                <span>{t("agents.readiness.workspaceCoverage")}</span>
              </div>
              <div className="divide-y divide-[color:var(--line)]">
                {props.agents.map((agent, index) => (
                  <AgentReadinessRow
                    key={agent.id}
                    agent={agent}
                    availability={props.availability[index]}
                    availabilityPending={props.availabilityPending[index]}
                    localPrimaryWorker={localPrimaryWorker}
                    workersPending={props.workersPending}
                    localIdentityPending={props.localIdentityPending}
                    activeWorkerMaxAgeMs={activeWorkerMaxAgeMs}
                  />
                ))}
              </div>
            </div>
          </div>
        </div>
      </section>

      <section aria-labelledby="connected-workers-heading">
        <div className="mb-2 min-w-0">
          <h2 id="connected-workers-heading" className="text-[14px] font-semibold leading-6 text-[color:var(--text)]">{t("agents.workers.title")}</h2>
          <p className="text-[12px] leading-5 text-[color:var(--muted)]">{t("agents.workers.description")}</p>
        </div>
        <div className="overflow-hidden rounded-[8px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
          <div className="overflow-x-auto">
            <div className="min-w-[1120px]">
              <div className="grid grid-cols-[minmax(240px,1.35fr)_125px_150px_150px_150px_70px_120px] gap-4 border-b border-[color:var(--line)] px-4 py-2.5 text-[12px] font-medium text-[color:var(--muted)]">
                <span>{t("agents.workers.worker")}</span>
                <span>{t("agents.status")}</span>
                <span>Codex</span>
                <span>Claude Code</span>
                <span>Pi</span>
                <span>{t("agents.workers.load")}</span>
                <span>{t("agents.workers.lastSeen")}</span>
              </div>
              {props.workersPending && workers.length === 0 ? (
                <div className="px-4 py-8 text-center text-[13px] text-[color:var(--muted)]">{t("agents.workers.loading")}</div>
              ) : workers.length === 0 ? (
                <div className="px-4 py-8 text-center">
                  <ServerCog className="mx-auto size-5 text-[color:var(--faint)]" />
                  <p className="mt-2 text-[13px] font-medium text-[color:var(--text)]">{t("agents.workers.emptyTitle")}</p>
                  <p className="mt-1 text-[12px] leading-5 text-[color:var(--muted)]">{t("agents.workers.emptyBody")}</p>
                </div>
              ) : (
                <div className="divide-y divide-[color:var(--line)]">
                  {workers.map((worker) => (
                    <WorkerReadinessRow
                      key={worker.id}
                      worker={worker}
                      activeWorkerMaxAgeMs={activeWorkerMaxAgeMs}
                      runtimeMode={props.runtimeMode}
                      currentHostId={props.currentHostId}
                    />
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      </section>
    </div>
  );
}

function AgentReadinessRow(props: {
  agent: AgentEngineCatalogItem;
  localPrimaryWorker?: RuntimeWorker;
  workersPending: boolean;
  localIdentityPending: boolean;
  availability?: RuntimeAvailability;
  availabilityPending?: boolean;
  activeWorkerMaxAgeMs?: number;
}) {
  const { t } = useMspaceTranslation();
  const claimableCount = Number.isSafeInteger(props.availability?.claimableWorkerCount)
    && Number(props.availability?.claimableWorkerCount) >= 0
    ? Number(props.availability?.claimableWorkerCount)
    : undefined;
  const localDiagnostic = props.localPrimaryWorker
    ? resolveAgentEngineDiagnostic(props.localPrimaryWorker, props.agent.id)
    : undefined;
  const localLiveness = props.localPrimaryWorker
    ? runtimeWorkerLiveness(props.localPrimaryWorker, props.activeWorkerMaxAgeMs)
    : undefined;
  const overallState = props.availability?.state || (props.availabilityPending ? "checking" : "unknown");

  return (
    <article
      className="grid grid-cols-[minmax(300px,1.35fr)_minmax(220px,1fr)_minmax(180px,0.8fr)] items-center gap-4 px-4 py-3 transition-[background-color] duration-150 ease-out hover:bg-[color:var(--hover)]"
      data-testid="agents.summary.item"
      data-qa-resource-type="agent-engine"
      data-qa-resource-id={props.agent.id}
      data-qa-state={overallState}
    >
      <AgentIdentity agent={props.agent} />
      <div className="min-w-0">
        {(props.workersPending || props.localIdentityPending) && !props.localPrimaryWorker ? (
          <span className="text-[12px] text-[color:var(--muted)]">{t("agents.runtimeChecking")}</span>
        ) : props.localPrimaryWorker && localDiagnostic && localLiveness ? (
          <div className="flex min-w-0 flex-col items-start gap-1.5">
            <EngineDiagnosticPill diagnostic={localDiagnostic} />
            <span className="truncate text-[11px] leading-4 text-[color:var(--faint)]">
              {t(`agents.workerStates.${localLiveness}`)}
            </span>
          </div>
        ) : (
          <span className="text-[12px] leading-5 text-[color:var(--muted)]">{t("agents.readiness.noLocalWorker")}</span>
        )}
      </div>
      <div className="min-w-0">
        {props.availabilityPending ? (
          <span className="text-[12px] text-[color:var(--muted)]">{t("agents.runtimeChecking")}</span>
        ) : claimableCount === undefined ? (
          <span className="text-[12px] leading-5 text-[color:var(--muted)]">{t("agents.readiness.coverageUnknown")}</span>
        ) : (
          <div className="flex min-w-0 items-center gap-2">
            <span className={cn("size-2 shrink-0 rounded-full", claimableCount > 0 ? "bg-[color:var(--success)]" : "bg-[color:var(--faint)]")} />
            <span className="text-[12px] leading-5 text-[color:var(--text)]">
              {t("agents.readiness.claimableWorkers", { count: claimableCount })}
            </span>
          </div>
        )}
      </div>
    </article>
  );
}

function AgentIdentity(props: { agent: AgentEngineCatalogItem }) {
  const { t } = useMspaceTranslation();
  const isCodex = props.agent.id === "codex";
  const initial = props.agent.id === "claude_code" ? "C" : props.agent.id === "pi" ? "P" : "C";
  return (
    <div className="flex min-w-0 items-start gap-2.5">
      <span className="mt-0.5 grid size-9 shrink-0 place-items-center overflow-hidden rounded-[8px] bg-[color:var(--paper)] text-[12px] font-semibold text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]">
        {isCodex ? <img src={codexAvatarDataUrl} alt="" className="size-full p-1" /> : initial}
      </span>
      <div className="min-w-0">
        <div className="flex min-w-0 items-baseline gap-2">
          <h3 className="truncate text-[14px] font-semibold leading-5 text-[color:var(--text)]">{props.agent.name}</h3>
          <span className="shrink-0 font-mono text-[11px] leading-4 text-[color:var(--muted)]">{props.agent.mention}</span>
        </div>
        <p className="mt-0.5 line-clamp-2 text-[12px] leading-5 text-[color:var(--muted)]">{t(`agents.engineDescriptions.${props.agent.id}`)}</p>
      </div>
    </div>
  );
}

function WorkerReadinessRow(props: {
  worker: RuntimeWorker;
  activeWorkerMaxAgeMs?: number;
  runtimeMode: string;
  currentHostId: string;
}) {
  const { t } = useMspaceTranslation();
  const liveness = runtimeWorkerLiveness(props.worker, props.activeWorkerMaxAgeMs);
  const isThisMac = isCurrentHostWorker(props.worker, props.runtimeMode, props.currentHostId);
  const hostId = runtimeWorkerLabel(props.worker, "hostId");
  const runtimeRole = runtimeWorkerLabel(props.worker, "runtimeRole");
  const roleLabel = runtimeRole === "primary"
    ? t("agents.workers.primary")
    : runtimeRole === "browser_companion"
      ? t("agents.workers.browserCompanion")
      : runtimeRole
        ? t("agents.workers.secondaryRuntime")
        : "";
  const meta = [
    roleLabel,
    isThisMac ? props.worker.name : hostId ? t("agents.workers.host", { id: shortHostId(hostId) }) : props.worker.mode,
  ].filter(Boolean).join(" · ");

  return (
    <article
      className="grid grid-cols-[minmax(240px,1.35fr)_125px_150px_150px_150px_70px_120px] items-center gap-4 px-4 py-3 transition-[background-color] duration-150 ease-out hover:bg-[color:var(--hover)]"
      data-testid="agents.workers.item"
      data-qa-resource-type="runtime-worker"
      data-qa-resource-id={props.worker.id}
      data-qa-state={liveness}
    >
      <div className="flex min-w-0 items-center gap-2.5">
        <span className="grid size-8 shrink-0 place-items-center rounded-[7px] bg-[color:var(--paper)] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
          <ServerCog className="size-4" />
        </span>
        <div className="min-w-0">
          <div className="truncate text-[13px] font-medium leading-5 text-[color:var(--text)]">{isThisMac ? t("agents.workers.thisMac") : props.worker.name}</div>
          <div className="truncate text-[11px] leading-4 text-[color:var(--muted)]">{meta || props.worker.mode}</div>
        </div>
      </div>
      <div className="min-w-0">
        <WorkerLivenessPill status={liveness} />
        <div className="mt-1 truncate text-[11px] leading-4 text-[color:var(--faint)]">{props.worker.mode}</div>
      </div>
      {(["codex", "claude_code", "pi"] as const).map((engine) => (
        <EngineDiagnosticCell key={engine} engine={engine} worker={props.worker} />
      ))}
      <span className="font-mono text-[12px] tabular-nums text-[color:var(--muted)]">{props.worker.currentLoad}</span>
      <span className="text-[12px] leading-5 text-[color:var(--muted)]"><RelativeTime value={props.worker.lastSeenAt} /></span>
    </article>
  );
}

function EngineDiagnosticCell(props: { engine: AgentEngine; worker: RuntimeWorker }) {
  const diagnostic = resolveAgentEngineDiagnostic(props.worker, props.engine);
  return (
    <div
      className="min-w-0"
      data-testid="agents.workers.engine-status"
      data-qa-field={props.engine}
      data-qa-state={diagnostic.status}
      data-qa-legacy-capability={diagnostic.legacyCapability ? "true" : undefined}
    >
      <EngineDiagnosticPill diagnostic={diagnostic} />
      {diagnostic.version ? <div className="mt-1 truncate font-mono text-[10px] leading-4 text-[color:var(--faint)]">{diagnostic.version}</div> : null}
    </div>
  );
}

function EngineDiagnosticPill(props: { diagnostic: ResolvedAgentEngineDiagnostic }) {
  const { t } = useMspaceTranslation();
  const { diagnostic } = props;
  const displayState = agentEngineDiagnosticDisplayState(diagnostic);
  const label = displayState === "disabled"
    ? t("agents.diagnostics.disabled")
    : diagnostic.status === "not_reported" && diagnostic.legacyCapability
      ? t("agents.diagnostics.legacyUnverified")
      : t(`agents.diagnostics.${diagnostic.status}`);
  const hintKey = displayState === "disabled"
    ? "disabled_by_configuration"
    : diagnostic.status === "not_reported" && diagnostic.legacyCapability
      ? "legacy_capability"
      : diagnostic.status;
  const titleParts = [
    t(`agents.diagnosticHints.${hintKey}`),
    diagnostic.version ? t("agents.diagnostics.version", { version: diagnostic.version }) : "",
    diagnostic.checkedAt ? t("agents.diagnostics.checkedAt", { time: formatAbsoluteTime(diagnostic.checkedAt) }) : "",
  ].filter(Boolean);
  const tone = diagnosticTone(diagnostic.status);
  const Icon = diagnostic.status === "ready"
    ? CheckCircle2
    : displayState === "disabled"
      ? Power
      : diagnostic.status === "needs_setup"
        ? Wrench
        : diagnostic.status === "probe_error"
          ? AlertCircle
          : CircleHelp;

  return (
    <span
      className={cn("inline-flex h-6 max-w-full items-center gap-1.5 rounded-full px-2 text-[11px] font-medium leading-5", tone)}
      title={titleParts.join(" · ")}
    >
      <Icon className="size-3 shrink-0" />
      <span className="truncate">{label}</span>
    </span>
  );
}

function WorkerLivenessPill(props: { status: RuntimeWorkerLiveness }) {
  const { t } = useMspaceTranslation();
  const tone = props.status === "online"
    ? "bg-[color:var(--success-soft)] text-[color:var(--success)]"
    : props.status === "draining"
      ? "bg-[color:var(--warning-soft)] text-[color:var(--warning)]"
      : props.status === "stale"
        ? "bg-[color:var(--warning-soft)] text-[color:var(--warning)]"
        : "bg-[color:var(--block)] text-[color:var(--muted)]";
  return (
    <span className={cn("inline-flex h-6 max-w-full items-center gap-1.5 rounded-full px-2 text-[11px] font-medium leading-5", tone)}>
      <Circle className="size-2.5 shrink-0 fill-current" />
      <span className="truncate">{t(`agents.workerStates.${props.status}`)}</span>
    </span>
  );
}

function diagnosticTone(status: AgentEngineReadinessStatus) {
  if (status === "ready") return "bg-[color:var(--success-soft)] text-[color:var(--success)]";
  if (status === "needs_setup") return "bg-[color:var(--warning-soft)] text-[color:var(--warning)]";
  if (status === "probe_error") return "bg-[color:var(--danger-soft)] text-[color:var(--danger)]";
  return "bg-[color:var(--block)] text-[color:var(--muted-strong)]";
}

function shortHostId(value: string) {
  const normalized = value.trim();
  if (normalized.length <= 12) return normalized;
  return normalized.slice(-8);
}

function SkillsPanel(props: {
  skills: SkillCatalogItem[];
  isPending: boolean;
  isMutating: boolean;
  canManage: boolean;
  error?: Error | null;
  onCreate: () => void;
  onEdit: (skill: SkillCatalogItem) => void;
  onDuplicate: (skill: SkillCatalogItem) => void;
  onDelete: (skill: SkillCatalogItem) => void;
  onToggleEnabled: (skill: SkillCatalogItem) => void;
}) {
  const { t } = useMspaceTranslation();
  if (props.isPending) {
    return (
      <div className="rounded-[10px] bg-[color:var(--surface)] px-4 py-6 text-[13px] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
        {t("agents.skills.loading")}
      </div>
    );
  }
  if (props.skills.length === 0) {
    return (
      <CollectionEmptyState
        icon={FileText}
        title={t("agents.skills.emptyTitle")}
        body={t("agents.skills.emptyBody")}
        action={props.canManage ? (
          <Button variant="secondary" onClick={props.onCreate}>
            <Plus data-icon />
            {t("agents.skills.newSkill")}
          </Button>
        ) : undefined}
      />
    );
  }
  return (
    <div className="overflow-hidden rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
      {!props.canManage ? (
        <div className="border-b border-[color:var(--line)] px-4 py-3">
          <Notice>{t("agents.skills.manageRestricted")}</Notice>
        </div>
      ) : null}
      {props.error ? (
        <div className="border-b border-[color:var(--line)] px-4 py-3">
          <Notice tone="danger">{props.error.message}</Notice>
        </div>
      ) : null}
      <div className="overflow-x-auto">
        <div className="min-w-[910px]">
          <div className="grid grid-cols-[minmax(240px,1.45fr)_130px_150px_145px_190px] gap-4 border-b border-[color:var(--line)] px-4 py-2.5 text-[12px] font-medium text-[color:var(--muted)]">
            <span>{t("agents.skills.skill")}</span>
            <span>{t("agents.skills.source")}</span>
            <span>{t("agents.skills.revision")}</span>
            <span>{t("agents.status")}</span>
            <span className="text-right">{t("agents.actions")}</span>
          </div>
          <div className="divide-y divide-[color:var(--line)]">
            {props.skills.map((skill) => (
              <SkillRow
                key={`${skill.sourceType}:${skill.id || skill.slug}`}
                skill={skill}
                isMutating={props.isMutating}
                canManage={props.canManage}
                onEdit={props.onEdit}
                onDuplicate={props.onDuplicate}
                onDelete={props.onDelete}
                onToggleEnabled={props.onToggleEnabled}
              />
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function SkillRow(props: {
  skill: SkillCatalogItem;
  isMutating: boolean;
  canManage: boolean;
  onEdit: (skill: SkillCatalogItem) => void;
  onDuplicate: (skill: SkillCatalogItem) => void;
  onDelete: (skill: SkillCatalogItem) => void;
  onToggleEnabled: (skill: SkillCatalogItem) => void;
}) {
  const { t } = useMspaceTranslation();
  const { skill } = props;
  return (
    <article className="grid grid-cols-[minmax(240px,1.45fr)_130px_150px_145px_190px] items-center gap-4 px-4 py-2.5 transition-[background-color] duration-150 ease-out hover:bg-[color:var(--hover)]">
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-2">
          <span className="grid size-8 shrink-0 place-items-center rounded-[8px] bg-[color:var(--paper)] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
            <FileText data-icon />
          </span>
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-2">
              <h3 className="truncate text-[14px] font-semibold leading-5 text-[color:var(--text)]">{skill.name || skill.slug}</h3>
              {skill.builtIn ? (
                <span className="shrink-0 rounded-full bg-[color:var(--block)] px-2 py-0.5 text-[11px] font-medium leading-4 text-[color:var(--muted-strong)]">
                  {t("agents.builtIn")}
                </span>
              ) : null}
            </div>
            <div className="mt-0.5 truncate font-mono text-[12px] leading-5 text-[color:var(--muted)]">/{skill.slug}</div>
          </div>
        </div>
        <p className="mt-2 line-clamp-2 text-[12px] leading-5 text-[color:var(--muted)]">{skill.description || t("agents.noDescription")}</p>
      </div>
      <div className="min-w-0 text-[12px] leading-5 text-[color:var(--muted)]">
        <div className="truncate text-[color:var(--text)]">{skill.builtIn ? t("agents.skills.sourceBuiltin") : t("agents.skills.sourceWorkspace")}</div>
        <div className="truncate">{skill.fileCount} {t("agents.skills.files")}</div>
      </div>
      <div className="min-w-0">
        <div className="truncate font-mono text-[12px] leading-5 text-[color:var(--text)]">{skill.revision || "-"}</div>
        <div className="truncate text-[11px] leading-4 text-[color:var(--faint)]">{skill.contentHash}</div>
      </div>
      <div className="flex flex-col items-start gap-1.5">
        <SkillStatus enabled={skill.enabled} label={skill.enabled ? t("agents.enabled") : t("agents.disabled")} />
      </div>
      <div className="flex justify-end">
        {!props.canManage ? (
          <span className="self-center text-[12px] leading-5 text-[color:var(--muted)]">{t("agents.skills.viewOnly")}</span>
        ) : (
          <div className="inline-flex items-center justify-end gap-1.5">
            <Button
              variant="ghost"
              size="sm"
              className="h-7 min-h-7 rounded-[7px] px-2 text-[12px] text-[color:var(--muted-strong)] hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] [&_[data-icon]]:size-3.5"
              onClick={() => props.onEdit(skill)}
              disabled={props.isMutating}
            >
              {skill.editable ? <Pencil data-icon /> : <Eye data-icon />}
              {skill.editable ? t("agents.skills.edit") : t("agents.skills.view")}
            </Button>
            <Button
              variant="secondary"
              size="sm"
              className="h-7 min-h-7 rounded-[7px] px-2 text-[12px] shadow-[0_0_0_1px_var(--line)] [&_[data-icon]]:size-3.5"
              onClick={() => props.onToggleEnabled(skill)}
              disabled={props.isMutating}
            >
              <Power data-icon />
              {skill.enabled ? t("agents.skills.disable") : t("agents.skills.enable")}
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-7 min-h-7 rounded-[7px] text-[color:var(--muted)] hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] [&_[data-icon]]:size-3.5"
                  aria-label={t("agents.skills.moreActions")}
                  disabled={props.isMutating}
                >
                  <MoreHorizontal data-icon />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-44">
                <DropdownMenuLabel>{t("agents.skills.actionMenu")}</DropdownMenuLabel>
                <DropdownMenuItem onSelect={() => props.onDuplicate(skill)} disabled={props.isMutating}>
                  <Copy data-icon />
                  {t("agents.skills.duplicate")}
                </DropdownMenuItem>
                {skill.deletable ? (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem variant="destructive" onSelect={() => props.onDelete(skill)} disabled={props.isMutating}>
                      <Trash2 data-icon />
                      {t("agents.skills.delete")}
                    </DropdownMenuItem>
                  </>
                ) : (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem disabled className="text-[color:var(--muted)]">
                      <Trash2 data-icon />
                      {t("agents.skills.deleteUnavailable")}
                    </DropdownMenuItem>
                  </>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        )}
      </div>
    </article>
  );
}

function SkillStatus(props: { enabled: boolean; label: string }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[12px] font-medium",
        props.enabled
          ? "bg-[color:var(--success-soft)] text-[color:var(--success)]"
          : "bg-[color:var(--block)] text-[color:var(--muted-strong)]",
      )}
    >
      {props.enabled ? <CheckCircle2 data-icon /> : <Circle data-icon />}
      {props.label}
    </span>
  );
}

function SkillModal(props: {
  mode: "create" | "edit" | "view";
  form: SkillForm;
  isPending: boolean;
  error?: Error | null;
  onClose: () => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
  onChange: (form: SkillForm) => void;
}) {
  const { t } = useMspaceTranslation();
  const isCreate = props.mode === "create";
  const isView = props.mode === "view";
  const extraFileCount = props.form.files.filter((file) => file.path !== "SKILL.md").length;
  return (
    <Modal
      title={isCreate ? t("agents.skills.newSkill") : isView ? t("agents.skills.viewSkill") : t("agents.skills.editSkill")}
      description={isCreate ? t("agents.skills.newDescription") : isView ? t("agents.skills.viewDescription") : t("agents.skills.editDescription")}
      onClose={props.onClose}
    >
      <form className="flex flex-col gap-4" onSubmit={props.onSubmit}>
        {props.error ? <Notice tone="danger">{props.error.message}</Notice> : null}
        <div className="grid gap-3 md:grid-cols-2">
          <Field label={t("agents.skills.slug")}>
            <Input
              value={props.form.slug}
              disabled={!isCreate || isView}
              onChange={(event) => props.onChange({ ...props.form, slug: event.target.value })}
              placeholder="repo-map"
            />
          </Field>
          <Field label={t("agents.name")}>
            <Input
              value={props.form.name}
              disabled={isView}
              onChange={(event) => props.onChange({ ...props.form, name: event.target.value })}
              placeholder={t("agents.skills.namePlaceholder")}
            />
          </Field>
        </div>

        <Field label={t("agents.description")}>
          <Input
            value={props.form.description}
            disabled={isView}
            onChange={(event) => props.onChange({ ...props.form, description: event.target.value })}
            placeholder={t("agents.skills.descriptionPlaceholder")}
          />
        </Field>

        <label className="flex items-center gap-3 rounded-[8px] bg-[color:var(--block)] px-3 py-3 text-[13px] text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]">
          <input
            type="checkbox"
            checked={props.form.enabled}
            disabled={isView}
            onChange={(event) => props.onChange({ ...props.form, enabled: event.target.checked })}
            className="size-4 rounded border-[color:var(--line)] accent-[color:var(--ink)]"
          />
          <span className="min-w-0">
            <span className="block font-medium text-[color:var(--text)]">{t("agents.skills.enabledLabel")}</span>
            <span className="mt-1 block text-[12px] leading-5 text-[color:var(--muted)]">{t("agents.skills.enabledHint")}</span>
          </span>
        </label>

        <Field
          label="SKILL.md"
          hint={extraFileCount > 0 ? t("agents.skills.extraFilesHint", { count: extraFileCount }) : t("agents.skills.skillMdHint")}
        >
          <Textarea
            value={props.form.skillMd}
            disabled={isView}
            onChange={(event) => props.onChange({ ...props.form, skillMd: event.target.value })}
            className="h-[280px] !min-h-[280px] font-mono text-[12px] leading-5"
            placeholder={t("agents.skills.skillMdPlaceholder")}
          />
        </Field>

        <div className="mt-1 flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={props.onClose} disabled={props.isPending}>
            {isView ? t("common.close") : t("common.cancel")}
          </Button>
          {!isView ? (
            <Button type="submit" disabled={props.isPending}>
              <Save data-icon />
              {props.isPending ? t("agents.saving") : isCreate ? t("agents.skills.createSkill") : t("agents.saveSettings")}
            </Button>
          ) : null}
        </div>
      </form>
    </Modal>
  );
}

function Modal(props: { title: string; description: string; onClose: () => void; children: ReactNode; compact?: boolean }) {
  const { t } = useMspaceTranslation();

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") props.onClose();
    }

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [props]);

  return (
    <div className="fixed inset-0 z-[80] grid place-items-center bg-[rgba(31,31,31,0.18)] px-5 py-8">
      <button type="button" aria-label={t("agents.closeBackdrop")} className="absolute inset-0 cursor-default" onClick={props.onClose} />
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="agent-modal-title"
        className={cn(
          "relative w-full rounded-[12px] bg-[color:var(--paper)] shadow-[0_24px_70px_rgba(0,0,0,0.18),0_0_0_1px_var(--line)]",
          props.compact
            ? "max-h-[calc(100vh-40px)] max-w-[640px] overflow-auto p-4 md:overflow-visible"
            : "max-h-[min(760px,calc(100vh-64px))] max-w-[660px] overflow-auto p-5",
        )}
      >
        <div className={cn("flex items-start justify-between gap-4", props.compact ? "mb-3" : "mb-5")}>
          <div className="min-w-0">
            <h2
              id="agent-modal-title"
              className={cn("font-semibold text-[color:var(--text)]", props.compact ? "text-[18px] leading-6" : "text-[20px] leading-7")}
            >
              {props.title}
            </h2>
            <p className={cn("mt-1 max-w-[58ch] text-[13px] text-[color:var(--muted)] text-pretty", props.compact ? "leading-5" : "leading-6")}>
              {props.description}
            </p>
          </div>
          <button
            type="button"
            aria-label={t("agents.closeModal")}
            className="grid size-9 shrink-0 place-items-center rounded-[7px] text-[color:var(--muted)] transition-[background-color,color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] active:scale-95"
            onClick={props.onClose}
          >
            <X data-icon />
          </button>
        </div>
        {props.children}
      </section>
    </div>
  );
}
