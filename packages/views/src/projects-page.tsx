import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Boxes, Clock3, GitBranch, Pencil, Route, SquareTerminal } from "lucide-react";
import { api, queryKeys, type Project } from "@mspace/core";
import {
  Button,
  DataBlock,
  EmptyState,
  Field,
  InlineMeta,
  Input,
  Notice,
  Panel,
  PageFrame,
  StatusBadge,
  Textarea,
} from "@mspace/ui";

const emptyProject = {
  name: "",
  repoPath: "",
  defaultBranch: "main",
  deployCommand: "",
  validationCommand: "",
  kubeContext: "",
  namespace: "",
};

export function ProjectsPage() {
  const queryClient = useQueryClient();
  const projectsQuery = useQuery({
    queryKey: queryKeys.projects,
    queryFn: api.listProjects,
  });
  const [form, setForm] = useState(emptyProject);
  const [editingProjectId, setEditingProjectId] = useState<string | null>(null);
  const canCreate =
    form.name.trim().length > 0 &&
    form.repoPath.trim().length > 0 &&
    form.namespace.trim().length > 0;

  const createProject = useMutation({
    mutationFn: api.createProject,
    onSuccess: async () => {
      setForm(emptyProject);
      await queryClient.invalidateQueries({ queryKey: queryKeys.projects });
    },
  });
  const updateProject = useMutation({
    mutationFn: api.updateProject,
    onSuccess: async () => {
      setForm(emptyProject);
      setEditingProjectId(null);
      await queryClient.invalidateQueries({ queryKey: queryKeys.projects });
    },
  });
  const deleteProject = useMutation({
    mutationFn: api.deleteProject,
    onSuccess: async () => {
      if (editingProjectId) {
        setEditingProjectId(null);
        setForm(emptyProject);
      }
      await queryClient.invalidateQueries({ queryKey: queryKeys.projects });
    },
  });

  const projects = projectsQuery.data || [];
  const activeMutation = editingProjectId ? updateProject : createProject;

  function loadProjectIntoForm(project: Project) {
    setEditingProjectId(project.id);
    setForm({
      name: project.name,
      repoPath: project.repoPath,
      defaultBranch: project.defaultBranch,
      deployCommand: project.deployCommand,
      validationCommand: project.validationCommand,
      kubeContext: project.kubeContext,
      namespace: project.namespace,
    });
  }

  function resetForm() {
    setEditingProjectId(null);
    setForm(emptyProject);
    createProject.reset();
    updateProject.reset();
  }

  return (
    <PageFrame
      title="Projects"
      subtitle="Projects define the local repository path plus the Kubernetes deployment and validation contract."
    >
      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_420px]">
        <Panel title="Configured projects">
          {projects.length === 0 ? (
            <EmptyState
              icon={Boxes}
              title="No projects configured"
              body="Add a local repository path and the commands mspace should use for deployment and validation."
            />
          ) : (
            <div className="flex flex-col gap-2">
              {projects.map((project) => (
                <article
                  key={project.id}
                  className="group rounded-[9px] px-3 py-3 transition-[background-color] duration-150 ease-out hover:bg-[color:var(--hover)]"
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <h3 className="truncate text-[15px] font-semibold">{project.name}</h3>
                        <StatusBadge value={project.namespace || "namespace unset"} />
                      </div>
                      <div className="mt-1 flex flex-wrap items-center gap-3">
                        <InlineMeta icon={GitBranch}>{project.defaultBranch}</InlineMeta>
                        <InlineMeta icon={Route}>{project.repoPath}</InlineMeta>
                      </div>
                    </div>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="opacity-0 group-hover:opacity-100"
                      onClick={() => loadProjectIntoForm(project)}
                    >
                      <Pencil data-icon />
                      Edit
                    </Button>
                  </div>

                  <div className="mt-3 grid gap-2 text-[13px] md:grid-cols-3">
                    <DataBlock label="Deploy" icon={SquareTerminal}>
                      {project.deployCommand || "not set"}
                    </DataBlock>
                    <DataBlock label="Validate" icon={SquareTerminal}>
                      {project.validationCommand || "not set"}
                    </DataBlock>
                    <DataBlock label="Activity" icon={Clock3}>
                      {project.issueCount} issues · {project.sessionCount} sessions
                      <br />
                      {project.latestIssueUpdatedAt ? new Date(project.latestIssueUpdatedAt).toLocaleString() : "none yet"}
                    </DataBlock>
                  </div>
                </article>
              ))}
            </div>
          )}
        </Panel>

        <Panel title={editingProjectId ? "Edit project" : "Add project"}>
          <form
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              if (!canCreate) return;
              if (editingProjectId) {
                updateProject.mutate({
                  id: editingProjectId,
                  ...form,
                });
                return;
              }
              createProject.mutate(form);
            }}
          >
            {activeMutation.error ? (
              <Notice tone="danger">{activeMutation.error.message}</Notice>
            ) : (
              <Notice>
                {editingProjectId
                  ? "Update the local repo path or Kubernetes contract without losing the issue history already attached to this project."
                  : "Projects are local-first: point at a checked-out repo, then define how mspace should deploy and validate it in Kubernetes."}
              </Notice>
            )}
            <Field label="Name">
              <Input value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} placeholder="mspace desktop" />
            </Field>
            <Field label="Repo path" hint="Absolute path on the current machine.">
              <Input value={form.repoPath} onChange={(event) => setForm({ ...form, repoPath: event.target.value })} placeholder="/Users/you/code/project" />
            </Field>
            <div className="grid gap-3 md:grid-cols-2">
              <Field label="Default branch">
                <Input value={form.defaultBranch} onChange={(event) => setForm({ ...form, defaultBranch: event.target.value })} />
              </Field>
              <Field label="Kube context">
                <Input value={form.kubeContext} onChange={(event) => setForm({ ...form, kubeContext: event.target.value })} placeholder="optional" />
              </Field>
            </div>
            <Field label="Namespace">
              <Input value={form.namespace} onChange={(event) => setForm({ ...form, namespace: event.target.value })} placeholder="team-a-dev" />
            </Field>
            <Field label="Deploy command">
              <Textarea value={form.deployCommand} onChange={(event) => setForm({ ...form, deployCommand: event.target.value })} placeholder="pnpm build && helm upgrade --install ..." />
            </Field>
            <Field label="Validation command" hint="Optional. If left empty, mspace will still collect a Kubernetes snapshot for the configured namespace.">
              <Textarea value={form.validationCommand} onChange={(event) => setForm({ ...form, validationCommand: event.target.value })} placeholder="kubectl get pods -n team-a-dev && kubectl get ingress -n team-a-dev" />
            </Field>
            <div className="flex flex-wrap gap-2">
              <Button type="submit" disabled={!canCreate || activeMutation.isPending}>
                {activeMutation.isPending
                  ? editingProjectId
                    ? "Saving changes..."
                    : "Saving project..."
                  : editingProjectId
                    ? "Save changes"
                    : "Save project"}
              </Button>
              {editingProjectId ? (
                <>
                  <Button variant="secondary" onClick={resetForm} disabled={activeMutation.isPending || deleteProject.isPending}>
                    Cancel
                  </Button>
                  <Button
                    variant="danger"
                    disabled={deleteProject.isPending}
                    onClick={() => deleteProject.mutate(editingProjectId)}
                  >
                    {deleteProject.isPending ? "Deleting..." : "Delete project"}
                  </Button>
                </>
              ) : null}
            </div>
          </form>
        </Panel>
      </div>
    </PageFrame>
  );
}
