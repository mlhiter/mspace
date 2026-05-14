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
import { api, controlPlaneApi, queryKeys, type Cluster, type CreateProjectInput, type Project } from "@mspace/core";
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

function projectNameFromPath(path: string): string {
  return path.trim().replace(/[\\/]+$/, "").split(/[\\/]/).pop() || "";
}

function projectToForm(project: Project): CreateProjectInput {
  return {
    name: project.name,
    sourceType: project.sourceType || "local",
    repoPath: project.repoPath,
    repoUrl: project.sourceType === "github" ? project.remoteUrl : "",
    defaultBranch: project.defaultBranch,
    kubeContext: project.kubeContext,
    kubeconfigPath: project.kubeconfigPath,
    namespace: project.namespace,
    imageRegistryPrefix: project.imageRegistryPrefix,
    previewDomain: project.previewDomain,
    ingressClass: project.ingressClass,
    nodeHost: project.nodeHost,
    defaultClusterId: project.defaultClusterId,
  };
}

export function ProjectsPage() {
  const queryClient = useQueryClient();
  const auth = useMspaceAuth();
  const workspaceId = auth.workspace?.id || "";
  const serverWorkspaceReady = Boolean(auth.token && workspaceId);
  const projectsQueryKey = queryKeys.workspaceProjects(workspaceId, auth.token);
  const projectRunbookKey = (projectId: string) =>
    queryKeys.workspaceProjectRunbook(workspaceId, projectId, auth.token);
  const projectsQuery = useQuery({
    queryKey: projectsQueryKey,
    queryFn: () => controlPlaneApi.listProjects(auth.token, workspaceId),
    enabled: serverWorkspaceReady,
  });
  const clustersQuery = useQuery({
    queryKey: queryKeys.clusters,
    queryFn: api.listClusters,
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
        queryClient.invalidateQueries({ queryKey: queryKeys.clusters }),
      ]);
    },
  });
  const settingsRunbookQuery = useQuery({
    queryKey: settingsProject ? projectRunbookKey(settingsProject.id) : projectRunbookKey("__none"),
    queryFn: () => {
      if (!settingsProject) throw new Error("Project settings are not open.");
      return controlPlaneApi.getProjectRunbook(auth.token, workspaceId, settingsProject.id);
    },
    enabled: Boolean(settingsProject && serverWorkspaceReady),
  });
  const saveProjectSettings = useMutation({
    mutationFn: async () => {
      if (!settingsProject) throw new Error("Project settings are not open.");
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
        queryClient.invalidateQueries({ queryKey: queryKeys.clusters }),
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
        queryClient.invalidateQueries({ queryKey: queryKeys.clusters }),
      ]);
    },
  });

  const projects = projectsQuery.data || [];
  const clusters = clustersQuery.data || [];
  const canCreate = createForm.repoPath.trim().length > 0 || createForm.repoUrl.trim().length > 0;
  const settingsSaveDisabled = saveProjectSettings.isPending || settingsRunbookQuery.isLoading;
  const settingsProjectHasWork = Boolean(
    settingsProject && (settingsProject.issueCount > 0 || settingsProject.sessionCount > 0),
  );

  async function pickProjectFolder() {
    setFolderPickerError("");
    if (!window.mspaceDesktop?.selectProjectFolder) {
      setFolderPickerError("Folder picker is only available in the desktop app.");
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
    setCreateForm(emptyProjectForm);
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
      sourceType: createForm.repoPath.trim() ? "local" : "github",
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
    const defaultCluster = clusters.find((cluster) => cluster.id === settingsForm.defaultClusterId);

    return (
      <PageFrame
        title={settingsForm.name || settingsProject.name}
        subtitle="Project settings"
        actions={
          <div className="flex flex-wrap items-center justify-end gap-2">
            <Button type="button" variant="secondary" onClick={closeSettings} disabled={saveProjectSettings.isPending}>
              <ArrowLeft data-icon />
              Projects
            </Button>
            <Button type="button" onClick={() => saveProjectSettings.mutate()} disabled={settingsSaveDisabled}>
              <Save data-icon />
              {saveProjectSettings.isPending ? "Saving..." : settingsRunbookQuery.isLoading ? "Loading..." : "Save"}
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
            <Field label="Runbook" hint={runbookHint(settingsProject, runbookMeta?.updatedAt || "")}>
              <IssueDocumentEditor
                variant="runbook"
                ariaLabel="Project runbook"
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
                This project has {settingsProject.issueCount} issue{settingsProject.issueCount === 1 ? "" : "s"} and{" "}
                {settingsProject.sessionCount} session{settingsProject.sessionCount === 1 ? "" : "s"}.
              </Notice>
            ) : null}

            <section className="rounded-[10px] bg-[color:var(--surface)] p-4 shadow-[inset_0_0_0_1px_var(--line)]">
              <div className="mb-3 text-[14px] font-semibold leading-5 text-[color:var(--text)]">Project</div>
              <div className="grid gap-3">
                <Field label="Name">
                  <Input
                    value={settingsForm.name}
                    onChange={(event) => setSettingsForm({ ...settingsForm, name: event.target.value })}
                  />
                </Field>
                <div className="grid gap-1.5 rounded-[8px] bg-[color:var(--block)] p-3 text-[13px] shadow-[inset_0_0_0_1px_var(--line)]">
                  <div className="flex items-center gap-2 text-[color:var(--muted-strong)]">
                    {settingsProject.sourceType === "github" ? <GitBranch data-icon /> : <HardDrive data-icon />}
                    <span className="font-medium">{settingsProject.sourceType === "github" ? "GitHub repository" : "Local repository"}</span>
                  </div>
                  <div className="break-all font-mono text-[12px] leading-4 text-[color:var(--muted)]">
                    {repositoryLabel}
                  </div>
                </div>
              </div>
            </section>

            <section className="rounded-[10px] bg-[color:var(--surface)] p-4 shadow-[inset_0_0_0_1px_var(--line)]">
              <div className="mb-3 text-[14px] font-semibold leading-5 text-[color:var(--text)]">Runtime</div>
              <ClusterSelectField
                clusters={clusters}
                value={settingsForm.defaultClusterId}
                onChange={(defaultClusterId) => setSettingsForm({ ...settingsForm, defaultClusterId })}
                hint={defaultCluster ? `${defaultCluster.kubeContext || defaultCluster.name}` : "No default cluster selected."}
              />
            </section>

            <section className="rounded-[10px] bg-[color:var(--surface)] p-4 shadow-[inset_0_0_0_1px_var(--line)]">
              <div className="mb-3 text-[14px] font-semibold leading-5 text-[color:var(--text)]">Danger zone</div>
              <Button
                type="button"
                variant="danger"
                disabled={settingsProjectHasWork || deleteProject.isPending || settingsSaveDisabled}
                title={settingsProjectHasWork ? "Projects with issues or sessions cannot be deleted yet." : undefined}
                onClick={() => deleteProject.mutate(settingsProject.id)}
              >
                <Trash2 data-icon />
                {deleteProject.isPending ? "Deleting..." : "Delete project"}
              </Button>
            </section>
          </aside>
        </form>
      </PageFrame>
    );
  }

  return (
    <PageFrame
      title="Projects"
      subtitle="Projects are the durable code workspaces agents can receive issues against. Creation stays light; runtime details can be adjusted after the project exists."
      actions={
        <Button variant="secondary" onClick={openCreateModal}>
          <Plus data-icon />
          New project
        </Button>
      }
    >
      {projects.length === 0 ? (
        <CollectionEmptyState
          icon={Boxes}
          title="No projects yet"
          body="Add a local folder or GitHub repository first. Issues and sessions attach after that."
          action={
            <Button variant="secondary" onClick={openCreateModal}>
              <Plus data-icon />
              New project
            </Button>
          }
        />
      ) : (
        <div className="rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
          <div className="grid grid-cols-[minmax(180px,1.1fr)_minmax(220px,1.5fr)_150px_56px] gap-4 border-b border-[color:var(--line)] px-4 py-2.5 text-[12px] font-medium text-[color:var(--muted)]">
            <span>Project</span>
            <span>Repository</span>
            <span>Work</span>
            <span className="text-right">Actions</span>
          </div>
          <div className="divide-y divide-[color:var(--line)]">
            {projects.map((project) => (
              <ProjectRow key={project.id} project={project} clusters={clusters} onSettings={() => openSettings(project)} />
            ))}
          </div>
        </div>
      )}

      {createOpen ? (
        <Modal title="New project" description="Start with the least information mspace needs. Agents can discover deployment details from the issue and repository later." onClose={() => setCreateOpen(false)}>
          <form className="flex flex-col gap-4" onSubmit={submitCreate}>
            {createProject.error ? <Notice tone="danger">{createProject.error.message}</Notice> : null}
            {folderPickerError ? <Notice tone="danger">{folderPickerError}</Notice> : null}

            <Field label="Project name" hint="Optional. Leave empty to use the folder or repository name.">
              <Input
                value={createForm.name}
                onChange={(event) => setCreateForm({ ...createForm, name: event.target.value })}
                placeholder="mspace"
              />
            </Field>

            <div className="grid gap-3">
              <SourceButton
                icon={<FolderOpen data-icon />}
                title="Local folder"
                description="Use an existing folder on this machine. mspace detects the GitHub remote automatically when it can."
                action="Choose folder"
                active={Boolean(createForm.repoPath)}
                onClick={pickProjectFolder}
              />
              {createForm.repoPath ? <PathPreview icon={<HardDrive data-icon />}>{createForm.repoPath}</PathPreview> : null}

              <div className="flex items-center gap-3 text-[12px] text-[color:var(--faint)]">
                <span className="h-px flex-1 bg-[color:var(--line)]" />
                or paste a repository URL
                <span className="h-px flex-1 bg-[color:var(--line)]" />
              </div>

              <div className="rounded-[10px] bg-[color:var(--block)] p-3 shadow-[inset_0_0_0_1px_var(--line)]">
                <Field label="GitHub repository URL">
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
              clusters={clusters}
              value={createForm.defaultClusterId}
              onChange={(defaultClusterId) => setCreateForm({ ...createForm, defaultClusterId })}
              hint="Optional. Issue deploys use this cluster by default."
            />

            <div className="mt-1 flex justify-end gap-2">
              <Button type="button" variant="secondary" onClick={() => setCreateOpen(false)} disabled={createProject.isPending}>
                Cancel
              </Button>
              <Button type="submit" disabled={!canCreate || createProject.isPending}>
                {createProject.isPending ? "Creating..." : "Create project"}
              </Button>
            </div>
          </form>
        </Modal>
      ) : null}
    </PageFrame>
  );
}

function ProjectRow(props: { project: Project; clusters: Cluster[]; onSettings: () => void }) {
  const { project } = props;
  const defaultCluster = props.clusters.find((cluster) => cluster.id === project.defaultClusterId);
  const githubLabel =
    project.gitProvider === "github" && project.gitOwner && project.gitRepo
      ? `${project.gitOwner}/${project.gitRepo}`
      : project.remoteUrl || "No remote detected";
  const updatedAt = project.latestIssueUpdatedAt || project.updatedAt;

  return (
    <article className="grid grid-cols-[minmax(180px,1.1fr)_minmax(220px,1.5fr)_150px_56px] items-center gap-4 px-4 py-3 transition-[background-color] duration-150 ease-out hover:bg-[color:var(--hover)]">
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-2">
          <h3 className="truncate text-[15px] font-semibold leading-6">{project.name}</h3>
          <StatusBadge value={defaultCluster?.name || "local"} />
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
          <span className="truncate">{project.repoPath}</span>
        </div>
      </div>

      <div className="text-[13px] leading-5 text-[color:var(--muted)]">
        <div>{project.issueCount} issues</div>
        <div>{project.sessionCount} sessions</div>
      </div>

      <div className="flex justify-end">
        <Button
          variant="ghost"
          size="icon"
          className="text-[color:var(--muted)] hover:text-[color:var(--text)]"
          aria-label={`Project settings for ${project.name}`}
          title="Project settings"
          onClick={props.onSettings}
        >
          <Settings data-icon />
        </Button>
      </div>
    </article>
  );
}

function runbookStatusLabel(project: Project): string {
  if (project.runbookStatus === "stale") return "Runbook stale";
  if (project.runbookStatus === "learned") return "Runbook learned";
  return "No runbook yet";
}

function runbookHint(project: Project, runbookUpdatedAt: string): string {
  if (project.runbookStatus === "stale") {
    return "Marked stale. Edit the Markdown or let the next agent session replace it from a session artifact.";
  }
  if (runbookUpdatedAt || project.runbookUpdatedAt) {
    return `Last updated ${runbookUpdatedAt || project.runbookUpdatedAt}. Agents receive this as advisory project memory.`;
  }
  return "Agents can create this automatically. Human edits should stay as Markdown notes, not command-form fields.";
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
  clusters: Cluster[];
  value: string;
  hint?: string;
  onChange: (clusterId: string) => void;
}) {
  return (
    <Field label="Default cluster" hint={props.hint}>
      {props.clusters.length === 0 ? (
        <div className="rounded-[8px] bg-[color:var(--block)] px-3 py-2 text-[13px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
          Create a cluster first to enable reusable test deployments.
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
            <SelectItem value="__none">No default cluster</SelectItem>
            {props.clusters.map((cluster) => (
              <SelectItem key={cluster.id} value={cluster.id}>
                {cluster.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
    </Field>
  );
}

function Modal(props: { title: string; description: string; onClose: () => void; children: ReactNode; compact?: boolean }) {
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") props.onClose();
    }

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [props]);

  return (
    <div className="fixed inset-0 z-[80] grid place-items-center bg-[rgba(31,31,31,0.18)] px-5 py-8">
      <button type="button" aria-label="Close modal backdrop" className="absolute inset-0 cursor-default" onClick={props.onClose} />
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
            aria-label="Close modal"
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
