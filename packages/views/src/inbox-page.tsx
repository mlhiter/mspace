import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { ArrowRight, Clock3, Inbox, Layers3 } from "lucide-react";
import { api, queryKeys } from "@mspace/core";
import {
  Button,
  EmptyState,
  Field,
  InlineMeta,
  Input,
  Notice,
  Panel,
  PageFrame,
  Select,
  StatusBadge,
  Textarea,
} from "@mspace/ui";

export function InboxPage() {
  const navigate = useNavigate();
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
    onSuccess: async ({ issueId }) => {
      setTitle("");
      setBody("");
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.inbox }),
        queryClient.invalidateQueries({ queryKey: queryKeys.projects }),
      ]);
      void navigate(`/issues/${issueId}`);
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
      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.12fr)_380px]">
        <Panel title="Incoming work">
          {items.length === 0 ? (
            <EmptyState
              icon={Inbox}
              title="Nothing in the inbox yet"
              body="Create an issue on the right. It becomes the durable page for local work, session logs, and cluster evidence."
            />
          ) : (
            <div className="flex flex-col gap-1">
              {items.map((item) => (
                <Link
                  key={item.id}
                  to={`/issues/${item.issueId}`}
                  className="group flex items-center justify-between gap-4 rounded-[8px] px-3 py-2.5 transition-[background-color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] active:scale-[0.99]"
                >
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-[14px] font-medium text-[color:var(--text)]">{item.title}</span>
                      {item.unread ? <span className="size-1.5 shrink-0 rounded-full bg-[color:var(--accent)]" /> : null}
                    </div>
                    <div className="mt-1 flex flex-wrap items-center gap-3">
                      <InlineMeta icon={Clock3}>Updated {new Date(item.updatedAt).toLocaleString()}</InlineMeta>
                      <InlineMeta icon={Layers3}>{item.projectId.slice(0, 8)}</InlineMeta>
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <StatusBadge value={item.status} />
                    <ArrowRight
                      data-icon
                      className="text-[color:var(--faint)] opacity-0 transition-[opacity,transform] duration-150 ease-out group-hover:translate-x-0.5 group-hover:opacity-100"
                    />
                  </div>
                </Link>
              ))}
            </div>
          )}
        </Panel>

        <Panel title="Create issue">
          <form
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              if (!canCreate) return;
              createIssue.mutate({ projectId, title, body });
            }}
          >
            {createIssue.error ? (
              <Notice tone="danger">{createIssue.error.message}</Notice>
            ) : (
              <Notice>{createSubtitle}</Notice>
            )}
            <Field label="Project">
              <Select value={projectId} onChange={(event) => setProjectId(event.target.value)}>
                <option value="">Select a project</option>
                {projects.map((project) => (
                  <option key={project.id} value={project.id}>
                    {project.name}
                  </option>
                ))}
              </Select>
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
