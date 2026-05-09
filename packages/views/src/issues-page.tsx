import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { ArrowRight, Bot, Clock3, Inbox, Layers3, MessageSquarePlus, Plus } from "lucide-react";
import { api, queryKeys, type IssueListItem } from "@mspace/core";
import { Button, CollectionEmptyState, InlineMeta, PageFrame, StatusBadge } from "@mspace/ui";
import { CreateIssueModal } from "./create-issue-modal";
import { RelativeTime } from "./time";

export function IssuesPage() {
  const search = useSearch({ strict: false }) as { new?: string };
  const navigate = useNavigate();
  const [createOpen, setCreateOpen] = useState(false);
  const issuesQuery = useQuery({
    queryKey: queryKeys.issues,
    queryFn: api.listIssues,
  });
  const projectsQuery = useQuery({
    queryKey: queryKeys.projects,
    queryFn: api.listProjects,
  });

  useEffect(() => {
    if (search.new === "1") {
      setCreateOpen(true);
      void navigate({ to: "/issues", search: {}, replace: true });
    }
  }, [navigate, search.new]);

  const issues = issuesQuery.data || [];
  const projects = projectsQuery.data || [];
  const grouped = useMemo(() => issues, [issues]);

  function closeCreateModal() {
    setCreateOpen(false);
  }

  return (
    <PageFrame
      title="Issues"
      subtitle="Manage durable work items, assign agents, and keep session evidence attached to the issue."
      actions={
        <Button variant="secondary" onClick={() => setCreateOpen(true)}>
          <Plus data-icon />
          New issue
        </Button>
      }
    >
      {grouped.length === 0 ? (
        <CollectionEmptyState
          icon={Inbox}
          title="No issues yet"
          body="Start with a short issue note. Sessions, comments, and evidence will collect here later."
          action={
            <Button variant="secondary" onClick={() => setCreateOpen(true)}>
              <Plus data-icon />
              New issue
            </Button>
          }
        />
      ) : (
        <div className="rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
          <div className="grid grid-cols-[minmax(220px,1.6fr)_minmax(150px,0.7fr)_150px_130px] gap-4 border-b border-[color:var(--line)] px-4 py-2.5 text-[12px] font-medium text-[color:var(--muted)]">
            <span>Issue</span>
            <span>Project</span>
            <span>Owner</span>
            <span className="text-right">State</span>
          </div>
          <div className="divide-y divide-[color:var(--line)]">
            {grouped.map((issue) => (
              <IssueRow key={issue.id} issue={issue} />
            ))}
          </div>
        </div>
      )}

      {createOpen ? <CreateIssueModal projects={projects} onClose={closeCreateModal} /> : null}
    </PageFrame>
  );
}

function IssueRow(props: { issue: IssueListItem }) {
  const { issue } = props;
  return (
    <Link
      to="/issues/$issueId"
      params={{ issueId: issue.id }}
      className="group grid grid-cols-[minmax(220px,1.6fr)_minmax(150px,0.7fr)_150px_130px] items-center gap-4 px-4 py-3 transition-[background-color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] active:scale-[0.995]"
    >
      <div className="min-w-0">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-[15px] font-semibold leading-6 text-[color:var(--text)]">{issue.title}</span>
          {issue.unread ? <span className="size-1.5 shrink-0 rounded-full bg-[color:var(--accent)]" /> : null}
        </div>
        <div className="mt-1 line-clamp-1 text-[12px] leading-5 text-[color:var(--muted)]">{issue.body}</div>
        {issue.labels?.length > 0 ? (
          <div className="mt-1.5 flex flex-wrap items-center gap-1">
            {issue.labels.slice(0, 4).map((label) => (
              <span
                key={label.id}
                className="rounded-[6px] bg-[color:var(--block)] px-1.5 py-0.5 text-[11px] leading-4 text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]"
              >
                {label.name}
              </span>
            ))}
            {issue.labels.length > 4 ? (
              <span className="text-[11px] leading-4 text-[color:var(--muted)]">+{issue.labels.length - 4}</span>
            ) : null}
          </div>
        ) : null}
        <div className="mt-1 flex flex-wrap items-center gap-3">
          <InlineMeta icon={Clock3}><RelativeTime prefix="Updated" value={issue.updatedAt} /></InlineMeta>
          <InlineMeta icon={MessageSquarePlus}>{issue.sessionCount} sessions</InlineMeta>
        </div>
      </div>

      <div className="min-w-0">
        <InlineMeta icon={Layers3}>{issue.projectName}</InlineMeta>
      </div>

      <div className="min-w-0">
        <InlineMeta icon={issue.assigneeType === "agent" ? Bot : Layers3}>
          {issue.assigneeType === "agent" ? "agent" : "human"} · {issue.assignee || "unassigned"}
        </InlineMeta>
      </div>

      <div className="flex items-center justify-end gap-2">
        <StatusBadge value={issue.status} />
        <ArrowRight
          data-icon
          className="text-[color:var(--faint)] opacity-0 transition-[opacity,transform] duration-150 ease-out group-hover:translate-x-0.5 group-hover:opacity-100"
        />
      </div>
    </Link>
  );
}
