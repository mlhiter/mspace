import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Boxes,
  Clock3,
  FolderOpen,
  GitBranch,
  HardDrive,
  Link,
  Plus,
  Settings2,
  Trash2,
  X,
} from "lucide-react";
import { api, queryKeys, type Cluster, type CreateProjectInput, type Project } from "@mspace/core";
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
  Textarea,
  cn,
} from "@mspace/ui";
import { RelativeTime } from "./time";

const emptyProjectForm: CreateProjectInput = {
  name: "",
  sourceType: "local",
  repoPath: "",
  repoUrl: "",
  defaultBranch: "",
  deployCommand: "",
  validationCommand: "",
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
    deployCommand: project.deployCommand,
    validationCommand: project.validationCommand,
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
  const projectsQuery = useQuery({
    queryKey: queryKeys.projects,
    queryFn: api.listProjects,
  });
  const clustersQuery = useQuery({
    queryKey: queryKeys.clusters,
    queryFn: api.listClusters,
  });
  const [createOpen, setCreateOpen] = useState(false);
  const [createForm, setCreateForm] = useState<CreateProjectInput>(emptyProjectForm);
  const [settingsProject, setSettingsProject] = useState<Project | null>(null);
  const [settingsForm, setSettingsForm] = useState<CreateProjectInput>(emptyProjectForm);
  const [folderPickerError, setFolderPickerError] = useState("");

  const createProject = useMutation({
    mutationFn: api.createProject,
    onSuccess: async () => {
      setCreateForm(emptyProjectForm);
      setCreateOpen(false);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.projects }),
        queryClient.invalidateQueries({ queryKey: queryKeys.clusters }),
      ]);
    },
  });
  const updateProject = useMutation({
    mutationFn: api.updateProject,
    onSuccess: async () => {
      setSettingsProject(null);
      setSettingsForm(emptyProjectForm);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.projects }),
        queryClient.invalidateQueries({ queryKey: queryKeys.clusters }),
      ]);
    },
  });
  const deleteProject = useMutation({
    mutationFn: api.deleteProject,
    onSuccess: async () => {
      setSettingsProject(null);
      setSettingsForm(emptyProjectForm);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.projects }),
        queryClient.invalidateQueries({ queryKey: queryKeys.clusters }),
      ]);
    },
  });

  const projects = projectsQuery.data || [];
  const clusters = clustersQuery.data || [];
  const canCreate = createForm.repoPath.trim().length > 0 || createForm.repoUrl.trim().length > 0;
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
    updateProject.reset();
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

    updateProject.mutate({
      id: settingsProject.id,
      ...settingsForm,
    });
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
          <div className="grid grid-cols-[minmax(180px,1.1fr)_minmax(220px,1.5fr)_150px_116px] gap-4 border-b border-[color:var(--line)] px-4 py-2.5 text-[12px] font-medium text-[color:var(--muted)]">
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

      {settingsProject ? (
        <Modal
          compact
          title="Project settings"
          description="Set the default test cluster and optional command hints for this project."
          onClose={() => setSettingsProject(null)}
        >
          <form className="flex flex-col gap-3" onSubmit={submitSettings}>
            {updateProject.error ? <Notice tone="danger">{updateProject.error.message}</Notice> : null}
            {deleteProject.error ? <Notice tone="danger">{deleteProject.error.message}</Notice> : null}
            {settingsProjectHasWork && settingsProject ? (
              <Notice>
                This project has {settingsProject.issueCount} issue{settingsProject.issueCount === 1 ? "" : "s"} and{" "}
                {settingsProject.sessionCount} session{settingsProject.sessionCount === 1 ? "" : "s"}. Delete is disabled to keep
                history and evidence attached.
              </Notice>
            ) : null}

            <Field label="Project name">
              <Input
                value={settingsForm.name}
                onChange={(event) => setSettingsForm({ ...settingsForm, name: event.target.value })}
              />
            </Field>
            <div className="grid gap-1.5 rounded-[10px] bg-[color:var(--block)] p-2.5 text-[13px] shadow-[inset_0_0_0_1px_var(--line)]">
              <div className="flex items-center gap-2 text-[color:var(--muted-strong)]">
                {settingsProject.sourceType === "github" ? <GitBranch data-icon /> : <HardDrive data-icon />}
                <span className="font-medium">{settingsProject.sourceType === "github" ? "GitHub repository" : "Local repository"}</span>
              </div>
              <div className="break-all font-mono text-[12px] leading-4 text-[color:var(--muted)]">
                {settingsProject.sourceType === "github" ? settingsProject.remoteUrl : settingsProject.repoPath}
              </div>
            </div>

            <ClusterSelectField
              clusters={clusters}
              value={settingsForm.defaultClusterId}
              onChange={(defaultClusterId) => setSettingsForm({ ...settingsForm, defaultClusterId })}
              hint="Used as the default for issue test deployments. Deployment runs can still choose a different cluster."
            />
            <Field label="Deploy command" hint="Optional. Agents can still inspect the repo and decide from issue context.">
              <Textarea
                className="h-[64px] !min-h-[64px] resize-none leading-5"
                value={settingsForm.deployCommand}
                onChange={(event) => setSettingsForm({ ...settingsForm, deployCommand: event.target.value })}
                placeholder="Leave empty unless this project has a stable command"
              />
            </Field>
            <Field label="Validation command">
              <Textarea
                className="h-[64px] !min-h-[64px] resize-none leading-5"
                value={settingsForm.validationCommand}
                onChange={(event) => setSettingsForm({ ...settingsForm, validationCommand: event.target.value })}
                placeholder="Leave empty for agent-led validation"
              />
            </Field>

            <div className="mt-1 flex flex-wrap justify-between gap-2">
              <Button
                type="button"
                variant="danger"
                disabled={settingsProjectHasWork || deleteProject.isPending || updateProject.isPending}
                title={settingsProjectHasWork ? "Projects with issues or sessions cannot be deleted yet." : undefined}
                onClick={() => settingsProject && deleteProject.mutate(settingsProject.id)}
              >
                <Trash2 data-icon />
                {deleteProject.isPending ? "Deleting..." : "Delete project"}
              </Button>
              <div className="flex gap-2">
                <Button type="button" variant="secondary" onClick={() => setSettingsProject(null)} disabled={updateProject.isPending}>
                  Cancel
                </Button>
                <Button type="submit" disabled={updateProject.isPending}>
                  {updateProject.isPending ? "Saving..." : "Save settings"}
                </Button>
              </div>
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
    <article className="grid grid-cols-[minmax(180px,1.1fr)_minmax(220px,1.5fr)_150px_116px] items-center gap-4 px-4 py-3 transition-[background-color] duration-150 ease-out hover:bg-[color:var(--hover)]">
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-2">
          <h3 className="truncate text-[15px] font-semibold leading-6">{project.name}</h3>
          <StatusBadge value={defaultCluster?.name || "local"} />
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-2">
          <InlineMeta icon={Clock3}><RelativeTime value={updatedAt} /></InlineMeta>
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
        <Button variant="secondary" size="sm" onClick={props.onSettings}>
          <Settings2 data-icon />
          Settings
        </Button>
      </div>
    </article>
  );
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
