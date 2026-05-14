import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { ArrowRight, CheckCircle2, Clock3, Inbox, Layers3, MessageSquarePlus, Plus, Search } from "lucide-react";
import { controlPlaneApi, getStoredAuthIdentity, queryKeys, type IssueListItem } from "@mspace/core";
import {
  Button,
  CollectionEmptyState,
  InlineMeta,
  Input,
  PageFrame,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  StatusBadge,
} from "@mspace/ui";
import { CreateIssueModal } from "./create-issue-modal";
import { codexAvatarDataUrl } from "./agent-avatar";
import { useMspaceAuth } from "./auth-context";
import {
  issueLabelMatchesDimension,
  issueLabelOptionsByDimension,
  issueLabelOptionsForUI,
  selectedIssueLabelKey,
} from "./issue-labels";
import { IssueLabelBadge, IssueLabelOptionLabel, IssueLabelSelectValue } from "./issue-label-chip";
import { displayIssueStatus, issueStatusLabel, issueStatusOptions } from "./issue-status";
import { RelativeTime } from "./time";

const sortOptions = [
  { value: "updated", label: "Updated" },
  { value: "created", label: "Created" },
  { value: "priority", label: "Priority" },
  { value: "type", label: "Type" },
] as const;
const toolbarSelectClass =
  "h-7 min-h-7 w-full rounded-[6px] bg-transparent px-2 py-1 text-[12px] leading-4 text-[color:var(--muted)] shadow-none hover:bg-[color:var(--hover)] focus:bg-[color:var(--hover)] focus:shadow-[inset_0_0_0_1px_var(--line)] data-[state=open]:bg-[color:var(--hover)] data-[state=open]:shadow-[inset_0_0_0_1px_var(--line)] sm:w-auto sm:min-w-[96px] [&_svg]:size-3.5";

function issueAssigneeName(issue: IssueListItem): string {
  if (issue.assigneeType === "agent") {
    const normalized = issue.assignee.replace(/^@/, "").trim();
    return normalized ? normalized.charAt(0).toUpperCase() + normalized.slice(1) : "Codex";
  }
  const stored = getStoredAuthIdentity();
  const assignee = issue.assignee.trim();
  return !assignee || assignee === "me" ? stored.name || "mlhiter" : assignee;
}

function issueMatchesStatusFilter(issue: IssueListItem, statusFilter: string) {
  if (statusFilter === "all") return true;
  return displayIssueStatus(issue.status) === statusFilter;
}

function IssueAssigneeMeta(props: { issue: IssueListItem }) {
  const [failed, setFailed] = useState(false);
  const stored = getStoredAuthIdentity();
  const name = issueAssigneeName(props.issue);
  const avatarUrl = props.issue.assigneeType === "agent" ? codexAvatarDataUrl : stored.avatarUrl || "";

  useEffect(() => {
    setFailed(false);
  }, [avatarUrl]);

  return (
    <div className="flex min-w-0 items-center gap-1.5 text-[12px] leading-5 text-[color:var(--muted)]">
      <span className="grid size-5 shrink-0 place-items-center overflow-hidden rounded-full bg-[color:var(--paper)] text-[10px] font-semibold text-[color:var(--muted-strong)] shadow-[0_0_0_1px_var(--line)]">
        {avatarUrl && !failed ? (
          <img src={avatarUrl} alt="" className="size-full object-cover" onError={() => setFailed(true)} />
        ) : (
          <span>{name.slice(0, 1).toUpperCase() || "M"}</span>
        )}
      </span>
      <span className="min-w-0 truncate">{name}</span>
    </div>
  );
}

export function IssuesPage() {
  const search = useSearch({ strict: false }) as { new?: string };
  const navigate = useNavigate();
  const auth = useMspaceAuth();
  const workspaceId = auth.workspace?.id || "";
  const serverWorkspaceReady = Boolean(auth.token && workspaceId);
  const issuesQueryKey = queryKeys.workspaceIssues(workspaceId, auth.token);
  const issueLabelDefinitionsQueryKey = queryKeys.workspaceIssueLabelDefinitions(workspaceId, auth.token);
  const projectsQueryKey = queryKeys.workspaceProjects(workspaceId, auth.token);
  const inboxQueryKey = queryKeys.workspaceInbox(workspaceId, auth.token);
  const [createOpen, setCreateOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [typeFilter, setTypeFilter] = useState("all");
  const [priorityFilter, setPriorityFilter] = useState("all");
  const [sortBy, setSortBy] = useState<(typeof sortOptions)[number]["value"]>("updated");
  const issuesQuery = useQuery({
    queryKey: issuesQueryKey,
    queryFn: () => controlPlaneApi.listIssues(auth.token, workspaceId),
    enabled: serverWorkspaceReady,
  });
  const labelDefinitionsQuery = useQuery({
    queryKey: issueLabelDefinitionsQueryKey,
    queryFn: () => controlPlaneApi.listIssueLabelDefinitions(auth.token, workspaceId),
    enabled: serverWorkspaceReady,
    retry: false,
  });

  useEffect(() => {
    if (search.new === "1") {
      setCreateOpen(true);
      void navigate({ to: "/issues", search: {}, replace: true });
    }
  }, [navigate, search.new]);

  const issues = issuesQuery.data || [];
  const labelOptions = useMemo(() => issueLabelOptionsForUI(labelDefinitionsQuery.data), [labelDefinitionsQuery.data]);
  const typeOptions = useMemo(() => issueLabelOptionsByDimension(labelOptions, "type"), [labelOptions]);
  const priorityOptions = useMemo(() => issueLabelOptionsByDimension(labelOptions, "priority"), [labelOptions]);
  const selectedTypeFilter = typeOptions.find((label) => label.key === typeFilter);
  const selectedPriorityFilter = priorityOptions.find((label) => label.key === priorityFilter);
  const grouped = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    return issues
      .filter((issue) => {
        if (!issueMatchesStatusFilter(issue, statusFilter)) return false;
        if (typeFilter !== "all" && labelKey(issue, "type") !== typeFilter) return false;
        if (priorityFilter !== "all" && labelKey(issue, "priority") !== priorityFilter) return false;
        if (normalizedQuery === "") return true;
        const haystack = [
          issue.title,
          issue.body,
          issue.projectName,
          issueStatusLabel(issue.status),
          issue.labels.map((label) => label.name).join(" "),
        ].join(" ").toLowerCase();
        return haystack.includes(normalizedQuery);
      })
      .sort((left, right) => compareIssues(left, right, sortBy));
  }, [issues, priorityFilter, query, sortBy, statusFilter, typeFilter]);

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
      {issues.length === 0 ? (
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
        <div className="grid gap-3">
          <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
            <div className="flex min-w-0 flex-1 items-center gap-2">
              <div className="relative min-w-0 flex-1 md:max-w-[340px]">
                <Search data-icon className="pointer-events-none absolute left-2.5 top-1/2 z-10 size-3.5 -translate-y-1/2 text-[color:var(--faint)]" />
                <Input
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder="Search issues"
                  className="h-7 min-h-7 rounded-[6px] bg-transparent pl-8 pr-2 text-[12px] shadow-none hover:bg-[color:var(--hover)] focus-visible:bg-[color:var(--hover)] focus-visible:shadow-[inset_0_0_0_1px_var(--line)]"
                />
              </div>
              <span className="hidden shrink-0 text-[12px] leading-4 text-[color:var(--faint)] sm:inline">
                {grouped.length} of {issues.length}
              </span>
            </div>
            <div className="flex flex-wrap items-center gap-1.5">
              <Select value={statusFilter} onValueChange={setStatusFilter}>
                <SelectTrigger className={toolbarSelectClass}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All status</SelectItem>
                  {issueStatusOptions.map((status) => (
                    <SelectItem key={status} value={status}>
                      {issueStatusLabel(status)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select value={typeFilter} onValueChange={setTypeFilter}>
                <SelectTrigger className={toolbarSelectClass}>
                  <IssueLabelSelectValue label={selectedTypeFilter} fallback="All type" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All type</SelectItem>
                  {typeOptions.map((label) => (
                    <SelectItem key={label.key} value={label.key}>
                      <IssueLabelOptionLabel label={label} />
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select value={priorityFilter} onValueChange={setPriorityFilter}>
                <SelectTrigger className={toolbarSelectClass}>
                  <IssueLabelSelectValue label={selectedPriorityFilter} fallback="All priority" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All priority</SelectItem>
                  {priorityOptions.map((label) => (
                    <SelectItem key={label.key} value={label.key}>
                      <IssueLabelOptionLabel label={label} />
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Select value={sortBy} onValueChange={(value) => setSortBy(value as typeof sortBy)}>
                <SelectTrigger className={toolbarSelectClass}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {sortOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className="rounded-[10px] bg-[color:var(--surface)] shadow-[inset_0_0_0_1px_var(--line)]">
            <div className="grid grid-cols-[minmax(220px,1.6fr)_minmax(150px,0.7fr)_150px_130px] gap-4 border-b border-[color:var(--line)] px-4 py-2.5 text-[12px] font-medium text-[color:var(--muted)]">
              <span>Issue</span>
              <span>Project</span>
              <span>Owner</span>
              <span className="text-right">State</span>
            </div>
            {grouped.length > 0 ? (
              <div className="divide-y divide-[color:var(--line)]">
                {grouped.map((issue) => (
                  <IssueRow key={issue.id} issue={issue} />
                ))}
              </div>
            ) : (
              <div className="px-4 py-8 text-center text-[13px] text-[color:var(--muted)]">No issues match the current view.</div>
            )}
          </div>
        </div>
      )}

      {createOpen ? (
        <CreateIssueModal
          onClose={closeCreateModal}
          createIssue={(input) => controlPlaneApi.createIssue(auth.token, workspaceId, input)}
          issueQueryKey={issuesQueryKey}
          inboxQueryKey={inboxQueryKey}
          projectsQueryKey={projectsQueryKey}
        />
      ) : null}
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
        </div>
        <div className="mt-1 line-clamp-1 text-[12px] leading-5 text-[color:var(--muted)]">{issue.body}</div>
        {issue.labels?.length > 0 || issue.triageStatus === "pending" ? (
          <div className="mt-1.5 flex flex-wrap items-center gap-1">
            {issue.labels.slice(0, 4).map((label) => (
              <IssueLabelPill key={label.id} label={label} />
            ))}
            {issue.labels.length > 4 ? (
              <span className="text-[11px] leading-4 text-[color:var(--muted)]">+{issue.labels.length - 4}</span>
            ) : null}
            {issue.triageStatus === "pending" && !labelByDimension(issue, "type") ? <PendingTypePill /> : null}
          </div>
        ) : null}
        <div className="mt-1 flex flex-wrap items-center gap-3">
          <InlineMeta icon={Clock3}><RelativeTime prefix="Updated" value={issue.updatedAt} /></InlineMeta>
          <InlineMeta icon={MessageSquarePlus}>{issue.sessionCount} sessions</InlineMeta>
          {issue.childIssueCount > 0 ? (
            <InlineMeta icon={CheckCircle2}>
              {issue.completedChildIssueCount}/{issue.childIssueCount} tasks
            </InlineMeta>
          ) : null}
        </div>
      </div>

      <div className="min-w-0">
        <InlineMeta icon={Layers3}>{issue.projectName}</InlineMeta>
      </div>

      <div className="min-w-0">
        <IssueAssigneeMeta issue={issue} />
      </div>

      <div className="flex items-center justify-end gap-2">
        <StatusBadge value={displayIssueStatus(issue.status)} valueLabel={issueStatusLabel(issue.status)} />
        <ArrowRight
          data-icon
          className="text-[color:var(--faint)] opacity-0 transition-[opacity,transform] duration-150 ease-out group-hover:translate-x-0.5 group-hover:opacity-100"
        />
      </div>
    </Link>
  );
}

function labelByDimension(issue: IssueListItem, dimension: string) {
  return issue.labels.find((label) => issueLabelMatchesDimension(label, dimension));
}

function labelKey(issue: IssueListItem, dimension: string) {
  return selectedIssueLabelKey(issue.labels, dimension);
}

function priorityRank(issue: IssueListItem) {
  const key = labelKey(issue, "priority");
  if (key === "priority:p0") return 0;
  if (key === "priority:p1") return 1;
  if (key === "priority:p2") return 2;
  if (key === "priority:p3") return 3;
  return 9;
}

function compareIssues(left: IssueListItem, right: IssueListItem, sortBy: string) {
  if (sortBy === "created") return Date.parse(right.createdAt) - Date.parse(left.createdAt);
  if (sortBy === "priority") {
    const priorityDelta = priorityRank(left) - priorityRank(right);
    if (priorityDelta !== 0) return priorityDelta;
    return Date.parse(right.updatedAt) - Date.parse(left.updatedAt);
  }
  if (sortBy === "type") {
    const typeDelta = labelKey(left, "type").localeCompare(labelKey(right, "type"));
    if (typeDelta !== 0) return typeDelta;
    return Date.parse(right.updatedAt) - Date.parse(left.updatedAt);
  }
  return Date.parse(right.updatedAt) - Date.parse(left.updatedAt);
}

function IssueLabelPill(props: { label: IssueListItem["labels"][number] }) {
  return <IssueLabelBadge label={props.label} />;
}

function PendingTypePill() {
  return (
    <span className="rounded-[6px] bg-[color:var(--block)] px-1.5 py-0.5 text-[11px] leading-4 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
      Classifying...
    </span>
  );
}
