import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, queryKeys } from "@mspace/core";
import { Button, EmptyState, Field, Input, Panel, PageFrame, Textarea } from "@mspace/ui";

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

  const createProject = useMutation({
    mutationFn: api.createProject,
    onSuccess: async () => {
      setForm(emptyProject);
      await queryClient.invalidateQueries({ queryKey: queryKeys.projects });
    },
  });

  const projects = projectsQuery.data || [];

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
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <div className="text-sm font-semibold">{project.name}</div>
                      <div className="mt-1 text-xs text-[color:var(--muted)]">
                        {project.repoPath} · branch {project.defaultBranch}
                      </div>
                    </div>
                    <div className="rounded-full bg-[color:var(--accent-soft)] px-3 py-1 text-xs font-medium text-[color:var(--accent)]">
                      {project.namespace || "namespace unset"}
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
                  </div>
                </div>
              ))}
            </div>
          )}
        </Panel>

        <Panel title="Add Project">
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              createProject.mutate(form);
            }}
          >
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
              <Textarea value={form.deployCommand} onChange={(event) => setForm({ ...form, deployCommand: event.target.value })} placeholder="helm upgrade --install ..." />
            </Field>
            <Field label="Validation command">
              <Textarea value={form.validationCommand} onChange={(event) => setForm({ ...form, validationCommand: event.target.value })} placeholder="kubectl get pods -n team-a-dev" />
            </Field>
            <Button type="submit" disabled={createProject.isPending}>
              {createProject.isPending ? "Saving project..." : "Save project"}
            </Button>
          </form>
        </Panel>
      </div>
    </PageFrame>
  );
}
