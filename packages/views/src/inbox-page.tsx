import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api, queryKeys } from "@mspace/core";
import { Button, EmptyState, Field, Input, Panel, PageFrame, StatusBadge, Textarea } from "@mspace/ui";

export function InboxPage() {
  const queryClient = useQueryClient();
  const inboxQuery = useQuery({
    queryKey: queryKeys.inbox,
    queryFn: api.listInbox,
  });
  const projectsQuery = useQuery({
    queryKey: queryKeys.projects,
    queryFn: api.listProjects,
  });

  const [projectId, setProjectId] = useState("");
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");

  const createIssue = useMutation({
    mutationFn: api.createIssue,
    onSuccess: async () => {
      setTitle("");
      setBody("");
      await queryClient.invalidateQueries({ queryKey: queryKeys.inbox });
    },
  });

  const canCreate = title.trim().length > 0 && projectId.length > 0;
  const items = inboxQuery.data || [];
  const projects = projectsQuery.data || [];
  const createSubtitle = useMemo(() => {
    if (projects.length === 0) return "Create a project first so new issues know where local development and Kubernetes validation should happen.";
    return "Turn incoming work into issues, then attach local agent sessions and Kubernetes validation evidence.";
  }, [projects.length]);

  return (
    <PageFrame
      title="Inbox"
      subtitle="Triage incoming work, create issues, and keep the path from local development to Kubernetes validation visible."
    >
      <div className="grid gap-6 xl:grid-cols-[1.15fr_0.85fr]">
        <Panel title="Incoming Work">
          {items.length === 0 ? (
            <EmptyState
              title="Nothing in the inbox yet"
              body="Create an issue on the right. It will appear here as the durable entry point for local work and cluster validation."
            />
          ) : (
            <div className="space-y-3">
              {items.map((item) => (
                <Link
                  key={item.id}
                  to={`/issues/${item.issueId}`}
                  className="flex items-center justify-between rounded-xl border border-[color:var(--border)] bg-white px-4 py-4 transition hover:border-[color:var(--accent)]"
                >
                  <div>
                    <div className="text-sm font-semibold">{item.title}</div>
                    <div className="mt-1 text-xs text-[color:var(--muted)]">
                      Updated {new Date(item.updatedAt).toLocaleString()}
                    </div>
                  </div>
                  <StatusBadge value={item.status} />
                </Link>
              ))}
            </div>
          )}
        </Panel>

        <Panel title="Create Issue">
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              if (!canCreate) return;
              createIssue.mutate({ projectId, title, body });
            }}
          >
            <div className="rounded-xl bg-[color:var(--background)] px-4 py-3 text-sm text-[color:var(--muted)]">
              {createSubtitle}
            </div>
            <Field label="Project">
              <select
                value={projectId}
                onChange={(event) => setProjectId(event.target.value)}
                className="w-full rounded-lg border border-[color:var(--border)] bg-white px-3 py-2 text-sm"
              >
                <option value="">Select a project</option>
                {projects.map((project) => (
                  <option key={project.id} value={project.id}>
                    {project.name}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Title">
              <Input value={title} onChange={(event) => setTitle(event.target.value)} placeholder="What needs to happen?" />
            </Field>
            <Field label="Body" hint="Use this as the durable problem statement and acceptance criteria.">
              <Textarea value={body} onChange={(event) => setBody(event.target.value)} placeholder="Context, constraints, and what should be proven in Kubernetes." />
            </Field>
            <Button type="submit" disabled={!canCreate || createIssue.isPending}>
              {createIssue.isPending ? "Creating issue..." : "Create issue"}
            </Button>
          </form>
        </Panel>
      </div>
    </PageFrame>
  );
}
