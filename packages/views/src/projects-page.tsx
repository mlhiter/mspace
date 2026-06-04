import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  Boxes,
  BookOpenText,
  Clock3,
  FolderOpen,
  GitBranch,
  HardDrive,
  Link,
  Plus,
  Save,
  Settings,
  Trash2,
  X,
} from "lucide-react";
import { controlPlaneApi, queryKeys, type Environment, type CreateProjectInput, type Project } from "@mspace/core";
import { t as translate, useMspaceTranslation } from "@mspace/i18n";
import {
  Button,
  CollectionEmptyState,
  Field,
  InlineMeta,
  Input,
  Notice,
  PageFrame,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  StatusBadge,
  cn,
} from "@mspace/ui";
import { IssueDocumentEditor } from "./issue-document-editor";
import { useMspaceAuth } from "./auth-context";
import { RelativeTime } from "./time";

const emptyProjectForm: CreateProjectInput = {
  name: "",
  sourceType: "local",
  repoPath: "",
  repoUrl: "",
  defaultBranch: "",
  kubeContext: "",
  kubeconfigPath: "",
  namespace: "",
  imageRegistryPrefix: "",
  previewDomain: "",
  ingressClass: "",
  nodeHost: "",
  defaultClusterId: "",
};

function initialProjectForm(isTeamWorkspace: boolean): CreateProjectInput {
  return {
    ...emptyProjectForm,
    sourceType: isTeamWorkspace ? "github" : "local",
  };
}

function projectNameFromPath(path: string): string {
  return path.trim().replace(/[\\/]+$/, "").split(/[\\/]/).pop() || "";
}

function projectToForm(project: Project): CreateProjectInput {
  return {
    name: project.name,
    sourceType: project.sourceType || "local",
    repoPath: project.repoPath,
    repoUrl: project.sourceType === "github" ? project.remoteUrl || project.repoPath : "",
    defaultBranch: project.defaultBranch,
    kubeContext: project.kubeContext,
    kubeconfigPath: project.kubeconfigPath,
    namespace: project.namespace,
    imageRegistryPrefix: project.imageRegistryPrefix,
    previewDomain: project.previewDomain,
    ingressClass: project.ingressClass,
    nodeHost: project.nodeHost,
    defaultClusterId: project.defaultEnvironmentId || project.defaultClusterId,
  };
}

export function ProjectsPage() {
  const { t } = useMspaceTranslation();
  const queryClient = useQueryClient();
  const auth = useMspaceAuth();
  const workspaceId = auth.workspace?.id || "";
  const isTeamWorkspace = auth.workspace?.kind === "team";
  const serverWorkspaceReady = Boolean(auth.token && workspaceId);
  const projectsQueryKey = queryKeys.workspaceProjects(workspaceId, auth.token);
  const environmentsQueryKey = queryKeys.environments(workspaceId, auth.token);
  const projectRunbookKey = (projectId: string) =>
    queryKeys.workspaceProjectRunbook(workspaceId, projectId, auth.token);
  const projectsQuery = useQuery({
    queryKey: projectsQueryKey,
    queryFn: () => controlPlaneApi.listProjects(auth.token, workspaceId),
    enabled: serverWorkspaceReady,
  });
  const environmentsQuery = useQuery({
    queryKey: environmentsQueryKey,
    queryFn: () => controlPlaneApi.listEnvironments(auth.token, workspaceId),
    enabled: serverWorkspaceReady,
  });
  const [createOpen, setCreateOpen] = useState(false);
  const [createForm, setCreateForm] = useState<CreateProjectInput>(emptyProjectForm);
  const [settingsProject, setSettingsProject] = useState<Project | null>(null);
  const [settingsForm, setSettingsForm] = useState<CreateProjectInput>(emptyProjectForm);
  const [runbookDraft, setRunbookDraft] = useState("");
  const [folderPickerError, setFolderPickerError] = useState("");

  const createProject = useMutation({
    mutationFn: (input: CreateProjectInput) => controlPlaneApi.createProject(auth.token, workspaceId, input),
    onSuccess: async () => {
      setCreateForm(emptyProjectForm);
      setCreateOpen(false);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: projectsQueryKey }),
        queryClient.invalidateQueries({ queryKey: environmentsQueryKey }),
      ]);
    },
  });
  const settingsRunbookQuery = useQuery({
    queryKey: settingsProject ? projectRunbookKey(settingsProject.id) : projectRunbookKey("__none"),
    queryFn: () => {
      if (!settingsProject) throw new Error(t("projects.settingsNotOpen"));
      return controlPlaneApi.getProjectRunbook(auth.token, workspaceId, settingsProject.id);
    },
    enabled: Boolean(settingsProject && serverWorkspaceReady),
  });
  const saveProjectSettings = useMutation({
    mutationFn: async () => {
      if (!settingsProject) throw new Error(t("projects.settingsNotOpen"));
      const input = {
        id: settingsProject.id,
        ...settingsForm,
      };
      const updatedProject = await controlPlaneApi.updateProject(auth.token, workspaceId, input);
      const runbookInput = {
        content: runbookDraft,
        status: runbookDraft.trim() ? "learned" : "empty",
      };
      await controlPlaneApi.updateProjectRunbook(auth.token, workspaceId, settingsProject.id, runbookInput);
      return updatedProject;
    },
    onSuccess: async (updatedProject) => {
      const projectID = settingsProject?.id;
      setSettingsProject(updatedProject);
      setSettingsForm(projectToForm(updatedProject));
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: projectsQueryKey }),
        queryClient.invalidateQueries({ queryKey: environmentsQueryKey }),
        projectID ? queryClient.invalidateQueries({ queryKey: projectRunbookKey(projectID) }) : Promise.resolve(),
      ]);
    },
  });
  const deleteProject = useMutation({
    mutationFn: (projectId: string) => controlPlaneApi.deleteProject(auth.token, workspaceId, projectId),
    onSuccess: async () => {
      setSettingsProject(null);
      setSettingsForm(emptyProjectForm);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: projectsQueryKey }),
        queryClient.invalidateQueries({ queryKey: environmentsQueryKey }),
      ]);
    },
  });

  const projects = projectsQuery.data || [];
  const environments = environmentsQuery.data || [];
  const kubernetesEnvironments = environments.filter((environment) => environment.kind === "kubernetes");
  const canCreate = isTeamWorkspace ? createForm.repoUrl.trim().length > 0 : createForm.repoPath.trim().length > 0 || createForm.repoUrl.trim().length > 0;
  const settingsSaveDisabled = saveProjectSettings.isPending || settingsRunbookQuery.isLoading;
  const settingsProjectHasWork = Boolean(
    settingsProject && (settingsProject.issueCount > 0 || settingsProject.sessionCount > 0),
  );

  async function pickProjectFolder() {
    setFolderPickerError("");
    if (!window.mspaceDesktop?.selectProjectFolder) {
      setFolderPickerError(t("projects.folderPickerDesktopOnly"));
      return;
    }

    const selectedPath = await window.mspaceDesktop.selectProjectFolder();
    if (!selectedPath) return;

    setCreateForm((currentForm) => ({
      ...currentForm,
      name: currentForm.name.trim() ? currentForm.name : projectNameFromPath(selectedPath),
      sourceType: "local",
      repoPath: selectedPath,
      repoUrl: "",
    }));
  }

  function openCreateModal() {
    setCreateForm(initialProjectForm(isTeamWorkspace));
    setFolderPickerError("");
    createProject.reset();
    setCreateOpen(true);
  }

  function openSettings(project: Project) {
    setSettingsProject(project);
    setSettingsForm(projectToForm(project));
    setRunbookDraft("");
    saveProjectSettings.reset();
    deleteProject.reset();
  }

  function closeSettings() {
    setSettingsProject(null);
    setSettingsForm(emptyProjectForm);
    setRunbookDraft("");
    saveProjectSettings.reset();
    deleteProject.reset();
  }

  function submitCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canCreate) return;

    createProject.mutate({
      ...createForm,
      sourceType: isTeamWorkspace ? "github" : createForm.repoPath.trim() ? "local" : "github",
      repoPath: isTeamWorkspace ? "" : createForm.repoPath,
    });
  }

  function submitSettings(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!settingsProject) return;

    saveProjectSettings.mutate();
  }

  useEffect(() => {
    if (!settingsProject) return;
    if (settingsRunbookQuery.data) {
      setRunbookDraft(settingsRunbookQuery.data.content);
    }
  }, [settingsProject, settingsRunbookQuery.data]);

  if (settingsProject) {
    const repositoryLabel = settingsProject.sourceType === "github" ? settingsProject.remoteUrl : settingsProject.repoPath;
    const runbookMeta = settingsRunbookQuery.data;
    const defaultEnvironment = kubernetesEnvironments.find((environment) => environment.id === settingsForm.defaultClusterId);

    return (
      <PageFrame
        title={settingsForm.name || settingsProject.name}
        subtitle={t("projects.settingsSubtitle")}
        actions={
          <div className="flex flex-wrap items-center justify-end gap-2">
            <Button type="button" variant="secondary" onClick={closeSettings} disabled={saveProjectSettings.isPending}>
              <ArrowLeft data-icon />
              {t("projects.backToProjects")}
            </Button>
            <Button type="button" onClick={() => saveProjectSettings.mutate()} disabled={settingsSaveDisabled}>
              <Save data-icon />
              {saveProjectSettings.isPending ? t("projects.saving") : settingsRunbookQuery.isLoading ? t("projects.loading") : t("projects.save")}
            </Button>
          </div>
        }
      >
        <form className="grid items-start gap-5 xl:grid-cols-[minmax(0,1fr)_340px]" onSubmit={submitSettings}>
          <section className="min-w-0 rounded-[10px] bg-[color:var(--surface)] p-4 shadow-[inset_0_0_0_1px_var(--line)]">
            <div className="mb-4 flex flex-wrap items-center gap-2 border-b border-[color:var(--line)] pb-3">
              <StatusBadge value={runbookStatusLabel(settingsProject)} />
              {runbookMeta?.updatedAt ? (
                <InlineMeta icon={Clock3}>
                  <RelativeTime value={runbookMeta.updatedAt} />
                </InlineMeta>
              ) : null}
              {runbookMeta?.sourceSessionId ? (
                <InlineMeta icon={BookOpenText}>session {runbookMeta.sourceSessionId.slice(0, 8)}</InlineMeta>
              ) : null}
            </div>
            <Field label={t("projects.runbook")} hint={runbookHint(settingsProject, runbookMeta?.updatedAt || "")}>
              <IssueDocumentEditor
                variant="runbook"
                ariaLabel={t("projects.runbookAria")}
                value={runbookDraft}
                editable={!settingsRunbookQuery.isLoading}
                onChange={setRunbookDraft}
                placeholder={runbookPlaceholder()}
              />
            </Field>
          </section>

          <aside className="grid gap-4">
            {(saveProjectSettings.error || deleteProject.error) ? (
              <Notice tone="danger">{saveProjectSettings.error?.message || deleteProject.error?.message}</Notice>
            ) : null}

            {settingsProjectHasWork ? (
              <Notice>
                {t("projects.hasWorkNotice", {
                  issues: settingsProject.issueCount,
                  issueSuffix: settingsProject.issueCount === 1 ? "" : "s",
                  sessions: settingsProject.sessionCount,
                  sessionSuffix: settingsProject.sessionCount === 1 ? "" : "s",
                })}
              </Notice>
            ) : null}

            <section className="rounded-[10px] bg-[color:var(--surface)] p-4 shadow-[inset_0_0_0_1px_var(--line)]">
              <div className="mb-3 text-[14px] font-semibold leading-5 text-[color:var(--text)]">{t("projects.project")}</div>
              <div className="grid gap-3">
                <Field label={t("projects.projectName")}>
                  <Input
                    value={settingsForm.name}
                    onChange={(event) => setSettingsForm({ ...settingsForm, name: event.target.value })}
                  />
                </Field>
                <div className="grid gap-1.5 rounded-[8px] bg-[color:var(--block)] p-3 text-[13px] shadow-[inset_0_0_0_1px_var(--line)]">
                  <div className="flex items-center gap-2 text-[color:var(--muted-strong)]">
                    {settingsProject.sourceType === "github" ? <GitBranch data-icon /> : <HardDrive data-icon />}
                    <span className="font-medium">{settingsProject.sourceType === "github" ? t("projects.githubRepository") : t("projects.localRepository")}</span>
                  </div>
                  <div className="break-all font-mono text-[12px] leading-4 text-[color:var(--muted)]">
                    {repositoryLabel}
                  </div>
                </div>
              </div>
            </section>

            <section className="rounded-[10px] bg-[color:var(--surface)] p-4 shadow-[inset_0_0_0_1px_var(--line)]">
              <div className="mb-3 text-[14px] font-semibold leading-5 text-[color:var(--text)]">{t("projects.runtime")}</div>
              <ClusterSelectField
                environments={kubernetesEnvironments}
                value={settingsForm.defaultClusterId}
                onChange={(defaultClusterId) => setSettingsForm({ ...settingsForm, defaultClusterId })}
                hint={defaultEnvironment ? `${defaultEnvironment.kubernetes?.kubeContext || defaultEnvironment.name}` : t("projects.noDefaultClusterSelected")}
              />
            </section>

            <section className="rounded-[10px] bg-[color:var(--surface)] p-4 shadow-[inset_0_0_0_1px_var(--line)]">
              <div className="mb-3 text-[14px] font-semibold leading-5 text-[color:var(--text)]">{t("projects.dangerZone")}</div>
              <Button
                type="button"
                variant="danger"
                disabled={settingsProjectHasWork || deleteProject.isPending || settingsSaveDisabled}
                title={settingsProjectHasWork ? t("projects.deleteDisabledTitle") : undefined}
                onClick={() => deleteProject.mutate(settingsProject.id)}
              >
                <Trash2 data-icon />
                {deleteProject.isPending ? t("projects.deleting") : t("projects.deleteProject")}
              </Button>
            </section>
          </aside>
        </form>
      </PageFrame>
    );
  }

  return (
    <PageFrame
      title={t("projects.title")}
      subtitle={t("projects.subtitle")}
      actions={
        <Button variant="secondary" onClick={openCreateModal}>
          <Plus data-icon />
          {t("projects.newProject")}
        </Button>
      }
    >
      {projects.length === 0 ? (
        <CollectionEmptyState
          icon={Boxes}
          title={t("projects.emptyTitle")}
          body={isTeamWorkspace ? t("projects.emptyBodyTeam") : t("projects.emptyBody")}
          action={
            <Button variant="secondary" onClick={openCreateModal}>
              <Plus data-icon />
              {t("projects.newProject")}
            </Button>
          }
        />
      ) : (
        <div className="rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
          <div className="grid grid-cols-[minmax(180px,1.1fr)_minmax(220px,1.5fr)_150px_56px] gap-4 border-b border-[color:var(--line)] px-4 py-2.5 text-[12px] font-medium text-[color:var(--muted)]">
            <span>{t("projects.project")}</span>
            <span>{t("projects.repository")}</span>
            <span>{t("projects.work")}</span>
            <span className="text-right">{t("projects.actions")}</span>
          </div>
          <div className="divide-y divide-[color:var(--line)]">
            {projects.map((project) => (
              <ProjectRow key={project.id} project={project} environments={kubernetesEnvironments} onSettings={() => openSettings(project)} />
            ))}
          </div>
        </div>
      )}

      {createOpen ? (
        <Modal title={t("projects.newProject")} description={isTeamWorkspace ? t("projects.createDescriptionTeam") : t("projects.createDescription")} onClose={() => setCreateOpen(false)}>
          <form className="flex flex-col gap-4" onSubmit={submitCreate}>
            {createProject.error ? <Notice tone="danger">{createProject.error.message}</Notice> : null}
            {folderPickerError ? <Notice tone="danger">{folderPickerError}</Notice> : null}

            <Field label={t("projects.projectName")} hint={t("projects.projectNameHint")}>
              <Input
                value={createForm.name}
                onChange={(event) => setCreateForm({ ...createForm, name: event.target.value })}
                placeholder="mspace"
              />
            </Field>

            <div className="grid gap-3">
              {isTeamWorkspace ? (
                <Notice>{t("projects.teamSourceNotice")}</Notice>
              ) : (
                <>
                  <SourceButton
                    icon={<FolderOpen data-icon />}
                    title={t("projects.localFolder")}
                    description={t("projects.localFolderDescription")}
                    action={t("projects.chooseFolder")}
                    active={Boolean(createForm.repoPath)}
                    onClick={pickProjectFolder}
                  />
                  {createForm.repoPath ? <PathPreview icon={<HardDrive data-icon />}>{createForm.repoPath}</PathPreview> : null}

                  <div className="flex items-center gap-3 text-[12px] text-[color:var(--faint)]">
                    <span className="h-px flex-1 bg-[color:var(--line)]" />
                    {t("projects.pasteRepositoryUrl")}
                    <span className="h-px flex-1 bg-[color:var(--line)]" />
                  </div>
                </>
              )}

              <div className="rounded-[10px] bg-[color:var(--block)] p-3 shadow-[inset_0_0_0_1px_var(--line)]">
                <Field label={t("projects.githubRepositoryUrl")} hint={isTeamWorkspace ? t("projects.githubRepositoryTeamHint") : undefined}>
                  <div className="relative">
                    <GitBranch data-icon className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-[color:var(--muted)]" />
                    <Input
                      className="pl-9"
                      value={createForm.repoUrl}
                      onChange={(event) =>
                        setCreateForm({
                          ...createForm,
                          sourceType: "github",
                          repoPath: "",
                          repoUrl: event.target.value,
                        })
                      }
                      placeholder="https://github.com/org/repo"
                    />
                  </div>
                </Field>
              </div>
            </div>

            <ClusterSelectField
              environments={kubernetesEnvironments}
              value={createForm.defaultClusterId}
              onChange={(defaultClusterId) => setCreateForm({ ...createForm, defaultClusterId })}
              hint={t("projects.optionalDefaultCluster")}
            />

            <div className="mt-1 flex justify-end gap-2">
              <Button type="button" variant="secondary" onClick={() => setCreateOpen(false)} disabled={createProject.isPending}>
                {t("common.cancel")}
              </Button>
              <Button type="submit" disabled={!canCreate || createProject.isPending}>
                {createProject.isPending ? t("projects.creating") : t("projects.createProject")}
              </Button>
            </div>
          </form>
        </Modal>
      ) : null}
    </PageFrame>
  );
}

function ProjectRow(props: { project: Project; environments: Environment[]; onSettings: () => void }) {
  const { project } = props;
  const { t } = useMspaceTranslation();
  const defaultEnvironment = props.environments.find((environment) => environment.id === (project.defaultEnvironmentId || project.defaultClusterId));
  const repositoryLocation = project.sourceType === "github" ? project.remoteUrl || project.repoPath : project.repoPath;
  const githubLabel =
    project.gitProvider === "github" && project.gitOwner && project.gitRepo
      ? `${project.gitOwner}/${project.gitRepo}`
      : repositoryLocation || t("projects.noRemoteDetected");
  const updatedAt = project.latestIssueUpdatedAt || project.updatedAt;

  return (
    <article className="grid grid-cols-[minmax(180px,1.1fr)_minmax(220px,1.5fr)_150px_56px] items-center gap-4 px-4 py-3 transition-[background-color] duration-150 ease-out hover:bg-[color:var(--hover)]">
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-2">
          <h3 className="truncate text-[15px] font-semibold leading-6">{project.name}</h3>
          <StatusBadge value={defaultEnvironment?.name || "local"} />
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-2">
          <InlineMeta icon={Clock3}><RelativeTime value={updatedAt} /></InlineMeta>
          <InlineMeta icon={BookOpenText}>{runbookStatusLabel(project)}</InlineMeta>
        </div>
      </div>

      <div className="min-w-0">
        <div className="flex items-center gap-2 text-[13px] font-medium leading-5 text-[color:var(--muted-strong)]">
          {project.sourceType === "github" ? <GitBranch data-icon /> : <HardDrive data-icon />}
          <span className="truncate">{githubLabel}</span>
        </div>
        <div className="mt-1 flex items-center gap-2 text-[12px] leading-5 text-[color:var(--muted)]">
          <Link data-icon className="shrink-0" />
          <span className="truncate">{repositoryLocation || t("projects.noRemoteDetected")}</span>
        </div>
      </div>

      <div className="text-[13px] leading-5 text-[color:var(--muted)]">
        <div>{t("projects.issues", { count: project.issueCount })}</div>
        <div>{t("projects.sessions", { count: project.sessionCount })}</div>
      </div>

      <div className="flex justify-end">
        <Button
          variant="ghost"
          size="icon"
          className="text-[color:var(--muted)] hover:text-[color:var(--text)]"
          aria-label={`${t("projects.settings")} ${project.name}`}
          title={t("projects.settings")}
          onClick={props.onSettings}
        >
          <Settings data-icon />
        </Button>
      </div>
    </article>
  );
}

function runbookStatusLabel(project: Project): string {
  if (project.runbookStatus === "stale") return translate("projects.runbookStale");
  if (project.runbookStatus === "learned") return translate("projects.runbookLearned");
  return translate("projects.noRunbookYet");
}

function runbookHint(project: Project, runbookUpdatedAt: string): string {
  if (project.runbookStatus === "stale") {
    return translate("projects.runbookStaleHint");
  }
  if (runbookUpdatedAt || project.runbookUpdatedAt) {
    return translate("projects.runbookUpdatedHint", { time: runbookUpdatedAt || project.runbookUpdatedAt });
  }
  return translate("projects.runbookEmptyHint");
}

function runbookPlaceholder(): string {
  return [
    "# Runbook",
    "",
    "## Dependencies",
    "## Local Start",
    "## Tests",
    "## Build",
    "## Image Build",
    "## Deploy",
    "## Health Check",
    "## Common Failures",
  ].join("\n");
}

function ClusterSelectField(props: {
  environments: Environment[];
  value: string;
  hint?: string;
  onChange: (clusterId: string) => void;
}) {
  const { t } = useMspaceTranslation();
  return (
    <Field label={t("projects.defaultCluster")} hint={props.hint}>
      {props.environments.length === 0 ? (
        <div className="rounded-[8px] bg-[color:var(--block)] px-3 py-2 text-[13px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
          {t("projects.createClusterFirst")}
        </div>
      ) : (
        <Select
          value={props.value || "__none"}
          onValueChange={(value) => props.onChange(value === "__none" ? "" : value)}
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__none">{t("projects.noDefaultCluster")}</SelectItem>
            {props.environments.map((environment) => (
              <SelectItem key={environment.id} value={environment.id}>
                {environment.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
    </Field>
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
      <button type="button" aria-label={t("projects.closeBackdrop")} className="absolute inset-0 cursor-default" onClick={props.onClose} />
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="project-modal-title"
        className={cn(
          "relative w-full rounded-[12px] bg-[color:var(--paper)] shadow-[0_24px_70px_rgba(0,0,0,0.18),0_0_0_1px_var(--line)]",
          props.compact
            ? "max-h-[calc(100vh-40px)] max-w-[600px] overflow-auto p-4 md:overflow-visible"
            : "max-h-[min(760px,calc(100vh-64px))] max-w-[620px] overflow-auto p-5",
        )}
      >
        <div className={cn("flex items-start justify-between gap-4", props.compact ? "mb-3" : "mb-5")}>
          <div className="min-w-0">
            <h2 id="project-modal-title" className={cn("font-semibold text-[color:var(--text)]", props.compact ? "text-[18px] leading-6" : "text-[20px] leading-7")}>
              {props.title}
            </h2>
            <p className={cn("mt-1 max-w-[58ch] text-[13px] text-[color:var(--muted)] text-pretty", props.compact ? "leading-5" : "leading-6")}>
              {props.description}
            </p>
          </div>
          <button
            type="button"
            aria-label={t("projects.closeModal")}
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

function SourceButton(props: {
  icon: ReactNode;
  title: string;
  description: string;
  action: string;
  active?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={props.onClick}
      className={cn(
        "flex min-h-[78px] w-full items-center gap-3 rounded-[10px] bg-[color:var(--block)] px-3 py-3 text-left shadow-[inset_0_0_0_1px_var(--line)] transition-[background-color,box-shadow,transform] duration-150 ease-out hover:bg-[color:var(--hover)] active:scale-[0.99]",
        props.active && "bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--accent),0_0_0_3px_var(--accent-soft)]",
      )}
    >
      <span className="grid size-10 shrink-0 place-items-center rounded-[9px] bg-[color:var(--paper)] text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]">
        {props.icon}
      </span>
      <span className="min-w-0 flex-1">
        <span className="block text-[14px] font-semibold leading-5 text-[color:var(--text)]">{props.title}</span>
        <span className="mt-1 block text-[12px] leading-5 text-[color:var(--muted)] text-pretty">{props.description}</span>
      </span>
      <span className="shrink-0 rounded-[7px] bg-[color:var(--ink)] px-3 py-2 text-[12px] font-medium text-[color:var(--paper)]">
        {props.action}
      </span>
    </button>
  );
}

function PathPreview(props: { icon: ReactNode; children: ReactNode }) {
  return (
    <div className="flex items-start gap-2 rounded-[8px] bg-[color:var(--surface)] px-3 py-2 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
      <span className="mt-0.5 shrink-0 text-[color:var(--muted-strong)]">{props.icon}</span>
      <span className="min-w-0 break-all font-mono">{props.children}</span>
    </div>
  );
}
