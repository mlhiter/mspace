import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, queryKeys, type Project } from "@mspace/core";
import { Button, EmptyState, Field, Input, Notice, Panel, PageFrame, Textarea } from "@mspace/ui";

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
      <div className="grid gap-6 xl:grid-cols-[1.05fr_0.95fr]">
        <Panel title="Configured Projects">
          {projects.length === 0 ? (
            <EmptyState
              title="No projects configured"
              body="Add a local repository path and the commands mspace should use for deployment and validation."
            />
          ) : (
            <div className="space-y-4">
              {projects.map((project) => (
                <div key={project.id} className="rounded-xl border border-[color:var(--border)] bg-white px-4 py-4">
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <div className="text-sm font-semibold">{project.name}</div>
                      <div className="mt-1 text-xs text-[color:var(--muted)]">
                        {project.repoPath} · branch {project.defaultBranch}
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <div className="rounded-full bg-[color:var(--accent-soft)] px-3 py-1 text-xs font-medium text-[color:var(--accent)]">
                        {project.namespace || "namespace unset"}
                      </div>
                      <Button
                        variant="secondary"
                        className="px-3 py-1.5 text-xs"
                        onClick={() => loadProjectIntoForm(project)}
                      >
                        Edit
                      </Button>
                    </div>
                  </div>
                  <div className="mt-4 grid gap-3 text-xs text-[color:var(--muted)]">
                    <div>
                      <span className="font-semibold text-[color:var(--text)]">Deploy:</span> {project.deployCommand || "not set"}
                    </div>
                    <div>
                      <span className="font-semibold text-[color:var(--text)]">Validate:</span> {project.validationCommand || "not set"}
                    </div>
                    <div>
                      <span className="font-semibold text-[color:var(--text)]">Kube context:</span> {project.kubeContext || "current context"}
                    </div>
                    <div className="flex flex-wrap gap-4">
                      <span>
                        <span className="font-semibold text-[color:var(--text)]">Issues:</span> {project.issueCount}
                      </span>
                      <span>
                        <span className="font-semibold text-[color:var(--text)]">Sessions:</span> {project.sessionCount}
                      </span>
                      <span>
                        <span className="font-semibold text-[color:var(--text)]">Last activity:</span>{" "}
                        {project.latestIssueUpdatedAt ? new Date(project.latestIssueUpdatedAt).toLocaleString() : "none yet"}
                      </span>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Panel>

        <Panel title={editingProjectId ? "Edit Project" : "Add Project"}>
          <form
            className="space-y-4"
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
            <div className="grid gap-4 md:grid-cols-2">
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
            <div className="flex gap-3">
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
