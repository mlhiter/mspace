import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { Clipboard, Files, GitBranch, GitCommit, GitCompareArrows, HardDrive, SquareTerminal, Trash2 } from "lucide-react";
import { api, buildApiUrl, queryKeys, type SessionStreamEvent, type WorkspaceChange, type WorkspaceSnapshot } from "@mspace/core";
import {
  Button,
  CodeBlock,
  DataBlock,
  InlineMeta,
  Notice,
  Panel,
  PageFrame,
  StatusBadge,
  Textarea,
  cn,
} from "@mspace/ui";
import { FileTypeIcon } from "./file-type-icon";
import { RelativeTime } from "./time";
import { visibleWorkspaceFileChanges, workspaceChangeStatusLabel, workspaceChangeStatusTone } from "./workspace-change-status";

function ChangeRow({ change }: { change: WorkspaceChange }) {
  return (
    <div className="rounded-[8px] bg-[color:var(--block)] px-3 py-2 shadow-[inset_0_0_0_1px_var(--line)]">
      <div className="flex flex-wrap items-center gap-2">
        <span
          className={cn(
            "rounded-full bg-[color:var(--accent-soft)] px-2 py-0.5 font-mono text-[12px] font-semibold",
            workspaceChangeStatusTone(change.statusCode),
          )}
        >
          {workspaceChangeStatusLabel(change.statusCode)}
        </span>
        <FileTypeIcon path={change.path} />
        <span className="break-all text-[13px] font-medium text-[color:var(--text)]">{change.path || "(unknown path)"}</span>
      </div>
      {change.previousPath ? (
        <div className="mt-1 break-all text-[12px] text-[color:var(--muted)]">
          renamed from {change.previousPath}
        </div>
      ) : null}
    </div>
  );
}

function listOrEmpty<T>(items: T[] | null | undefined): T[] {
  return Array.isArray(items) ? items : [];
}

function normalizeWorkspace(workspace: WorkspaceSnapshot): WorkspaceSnapshot {
  return {
    ...workspace,
    statusLines: listOrEmpty(workspace.statusLines),
    changes: visibleWorkspaceFileChanges(listOrEmpty(workspace.changes)),
    comparison: {
      ...workspace.comparison,
      commitLines: listOrEmpty(workspace.comparison?.commitLines),
      changes: visibleWorkspaceFileChanges(listOrEmpty(workspace.comparison?.changes)),
    },
  };
}

export function SessionDetailPage() {
  const { sessionId = "" } = useParams({ strict: false }) as { sessionId?: string };
  const queryClient = useQueryClient();
  const sessionQuery = useQuery({
    queryKey: queryKeys.session(sessionId),
    queryFn: () => api.getSession(sessionId),
    enabled: sessionId.length > 0,
  });
  const [logs, setLogs] = useState<string[]>([]);
  const [summaryDraft, setSummaryDraft] = useState("");
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">("idle");

  const generatedSummary = useMemo(() => {
    if (!sessionQuery.data) return "";

    const { session } = sessionQuery.data;
    const workspace = normalizeWorkspace(sessionQuery.data.workspace);
    const evidence = listOrEmpty(sessionQuery.data.evidence);
    const lines = [
      `Session ${session.id.slice(0, 8)} summary`,
      "",
      `- Status: ${session.status}`,
      `- Provider: ${session.provider}`,
      `- Agent profile: ${session.agentProfile || "codex"}`,
      `- Agent status: ${session.agentStatus || "unknown"}`,
      `- Cleanup status: ${session.cleanupStatus || "retained"}`,
      `- Cleaned at: ${session.cleanedAt || "not cleaned"}`,
      `- Codex thread: ${session.codexThreadId || "not started"}`,
      `- Codex turn: ${session.codexTurnId || "not started"}`,
      `- Branch: ${workspace.branch || session.branch || "unknown"}`,
      `- Workspace: ${session.workdir || "not reported"}`,
      `- Base ref: ${workspace.comparison.baseRef || "unknown"}`,
      `- Ahead/behind: ${workspace.comparison.aheadCount}/${workspace.comparison.behindCount}`,
      `- Working tree changes: ${workspace.changedFiles} tracked, ${workspace.untrackedFiles} untracked`,
    ];

    if (workspace.comparison.commitLines.length > 0) {
      lines.push("", "Commits on this session branch:");
      for (const line of workspace.comparison.commitLines.slice(0, 8)) {
        lines.push(`- ${line}`);
      }
    }

    if (workspace.comparison.changes.length > 0) {
      lines.push("", "Files changed since merge base:");
      for (const change of workspace.comparison.changes.slice(0, 12)) {
        const renameSuffix = change.previousPath ? ` (from ${change.previousPath})` : "";
        lines.push(`- [${workspaceChangeStatusLabel(change.statusCode)}] ${change.path}${renameSuffix}`);
      }
    } else if (workspace.changes.length > 0) {
      lines.push("", "Uncommitted workspace changes:");
      for (const change of workspace.changes.slice(0, 12)) {
        const renameSuffix = change.previousPath ? ` (from ${change.previousPath})` : "";
        lines.push(`- [${workspaceChangeStatusLabel(change.statusCode)}] ${change.path}${renameSuffix}`);
      }
    }

    if (evidence.length > 0) {
      lines.push("", "Validation evidence:");
      for (const item of evidence.slice(0, 4)) {
        lines.push(`- ${item.summary} (${item.cluster || "current context"} / ${item.namespace || "namespace unset"})`);
      }
    }

    return lines.join("\n");
  }, [sessionQuery.data]);

  useEffect(() => {
    if (!sessionQuery.data) return;
    setLogs(listOrEmpty(sessionQuery.data.logs).map((log) => log.message));
    setSummaryDraft((current) => (current.trim() ? current : generatedSummary));
  }, [generatedSummary, sessionQuery.data]);

  useEffect(() => {
    if (!sessionId) return;
    const eventSource = new EventSource(buildApiUrl(`/api/sessions/${sessionId}/stream`));
    const listener = (event: MessageEvent<string>) => {
      const payload = JSON.parse(event.data) as SessionStreamEvent;
      if (payload.type === "log") {
        setLogs((current) => [...current, payload.payload]);
      } else {
        void queryClient.invalidateQueries({ queryKey: queryKeys.session(sessionId) });
      }
    };
    eventSource.addEventListener("message", listener as EventListener);
    return () => {
      eventSource.removeEventListener("message", listener as EventListener);
      eventSource.close();
    };
  }, [queryClient, sessionId]);

  const cancelMutation = useMutation({
    mutationFn: () => api.cancelSession(sessionId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.session(sessionId) });
    },
  });

  const cleanupMutation = useMutation({
    mutationFn: () => api.cleanupSession(sessionId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.session(sessionId) });
    },
  });

  if (!sessionQuery.data) {
    return (
      <PageFrame title="Session" subtitle="Inspect live logs, local runtime state, and validation history.">
        <Panel>{sessionQuery.isPending ? "Loading session..." : "Session not found."}</Panel>
      </PageFrame>
    );
  }

  const { session, issue, project } = sessionQuery.data;
  const projectName = project?.name || "No project";
  const workspace = normalizeWorkspace(sessionQuery.data.workspace);
  const sessionActive = ["queued", "running"].includes(session.status);
  const cleanupStatus = session.cleanupStatus || "retained";
  const canCleanWorktree = !sessionActive && cleanupStatus !== "cleaned" && Boolean(session.workdir);
  const missingWorkspaceText = cleanupStatus === "cleaned" ? "Session worktree has been cleaned up." : "Workspace has not been created yet.";

  async function handleCopySummary() {
    try {
      await navigator.clipboard.writeText(summaryDraft);
      setCopyState("copied");
    } catch {
      setCopyState("failed");
    }
  }

  function handleCleanWorktree() {
    const confirmed = window.confirm(
      `Clean the session worktree?\n\n${session.workdir}\n\nThis removes the local worktree for this session. Logs, comments, evidence, and session metadata stay in mspace.`,
    );
    if (confirmed) {
      cleanupMutation.mutate();
    }
  }

  return (
    <PageFrame
      title={`Session ${session.id.slice(0, 8)}`}
      subtitle={`${projectName} · issue ${issue.title}`}
      breadcrumbs={[
        { label: "mspace", to: "/inbox" },
        { label: "Issues", to: "/issues" },
        { label: issue.title, to: "/issues/$issueId", params: { issueId: issue.id } },
        { label: `Session ${session.id.slice(0, 8)}` },
      ]}
      actions={
        <>
          <Button asChild variant="secondary">
            <Link to="/issues/$issueId" params={{ issueId: issue.id }}>Back to issue</Link>
          </Button>
          <Button
            variant="danger"
            disabled={cancelMutation.isPending || !sessionActive}
            onClick={() => cancelMutation.mutate()}
          >
            Cancel session
          </Button>
          <Button
            variant="secondary"
            disabled={cleanupMutation.isPending || !canCleanWorktree}
            onClick={handleCleanWorktree}
          >
            <Trash2 data-icon />
            {cleanupMutation.isPending ? "Cleaning..." : cleanupStatus === "cleaned" ? "Worktree cleaned" : "Clean worktree"}
          </Button>
        </>
      }
    >
      <div className="grid gap-6 xl:grid-cols-[minmax(0,0.92fr)_minmax(0,1.08fr)]">
        <div className="flex flex-col gap-6">
          <Panel title="Session metadata" aside={<StatusBadge value={session.status} />}>
            {cleanupMutation.error ? <Notice tone="danger">{cleanupMutation.error.message}</Notice> : null}
            <div className="grid gap-3">
              <DataBlock label="Provider" icon={SquareTerminal}>{session.provider}</DataBlock>
              <DataBlock label="Agent profile" icon={SquareTerminal}>{session.agentProfile || "codex"}</DataBlock>
              <DataBlock label="Runtime mode" icon={HardDrive}>{session.runtimeMode}</DataBlock>
              <DataBlock label="Agent status" icon={SquareTerminal}>{session.agentStatus || "not reported yet"}</DataBlock>
              <DataBlock label="Cleanup status" icon={Trash2}>
                {cleanupStatus === "cleaned" ? (
                  session.cleanedAt ? <RelativeTime prefix="cleaned" value={session.cleanedAt} /> : "cleaned"
                ) : "retained"}
              </DataBlock>
              <DataBlock label="Session branch" icon={GitBranch}>{session.branch}</DataBlock>
              <DataBlock label="Agent instructions" icon={SquareTerminal}>{session.command || "issue and project context only"}</DataBlock>
              <DataBlock label="Codex thread" icon={SquareTerminal}>{session.codexThreadId || "not started yet"}</DataBlock>
              <DataBlock label="Codex turn" icon={SquareTerminal}>{session.codexTurnId || "not started yet"}</DataBlock>
              <DataBlock label="Session workspace" icon={Files}>{session.workdir || "not reported yet"}</DataBlock>
              <DataBlock label="Artifact directory" icon={Files}>{session.artifactDir || "not reported yet"}</DataBlock>
              <DataBlock label="Source repository" icon={Files}>{project?.repoPath || "not configured"}</DataBlock>
            </div>
          </Panel>

          <Panel title="Issue summary draft">
            {copyState === "copied" ? <Notice>Summary copied to clipboard.</Notice> : null}
            {copyState === "failed" ? <Notice tone="danger">Clipboard access failed. You can still copy from the draft below.</Notice> : null}
            <div className="mt-3 flex flex-col gap-3">
              <Textarea
                value={summaryDraft}
                onChange={(event) => setSummaryDraft(event.target.value)}
                className="min-h-64 font-mono text-[12px] leading-6"
              />
              <div className="flex flex-wrap gap-2">
                <Button variant="secondary" onClick={() => setSummaryDraft(generatedSummary)}>
                  Regenerate draft
                </Button>
                <Button variant="secondary" onClick={() => void handleCopySummary()}>
                  <Clipboard data-icon />
                  Copy summary
                </Button>
                <Button disabled title="Issue comments now write through the server control plane.">
                  Post to issue
                </Button>
              </div>
            </div>
          </Panel>

          <Panel title="Workspace snapshot">
            {workspace.error ? <Notice tone="danger">{workspace.error}</Notice> : null}
            <div className="grid gap-3 md:grid-cols-2">
              <DataBlock label="Workspace branch" icon={GitBranch}>{workspace.branch || session.branch || "not available yet"}</DataBlock>
              <DataBlock label="HEAD" icon={GitCommit}>{workspace.shortHead || workspace.head || "not available yet"}</DataBlock>
              <DataBlock label="Changed tracked files" icon={Files}>{workspace.changedFiles}</DataBlock>
              <DataBlock label="Untracked files" icon={Files}>{workspace.untrackedFiles}</DataBlock>
            </div>

            <div className="mt-4 flex flex-col gap-2">
              <div className="text-[13px] font-semibold text-[color:var(--muted-strong)]">Git status</div>
              <CodeBlock
                empty={
                  !workspace.exists
                    ? missingWorkspaceText
                    : !workspace.isGitRepository
                      ? "Workspace exists, but it is not a git worktree."
                      : "Working tree clean."
                }
              >
                {workspace.exists && workspace.isGitRepository && workspace.statusLines.length > 0
                  ? workspace.statusLines.map((line) => <div key={line}>{line}</div>)
                  : null}
              </CodeBlock>
            </div>

            <div className="mt-4 flex flex-col gap-2">
              <div className="text-[13px] font-semibold text-[color:var(--muted-strong)]">Changed files</div>
              {workspace.changes.length === 0 ? (
                <DataBlock label="No file changes">{workspace.exists ? "No file changes in this workspace." : missingWorkspaceText}</DataBlock>
              ) : (
                <div className="flex flex-col gap-2">
                  {workspace.changes.map((change) => (
                    <ChangeRow key={`${change.statusCode}-${change.previousPath}-${change.path}`} change={change} />
                  ))}
                </div>
              )}
            </div>

            <div className="mt-4 flex flex-col gap-2">
              <div className="flex items-center justify-between gap-3">
                <div className="text-[13px] font-semibold text-[color:var(--muted-strong)]">Diff preview</div>
                {workspace.diffTruncated ? <InlineMeta>Preview truncated</InlineMeta> : null}
              </div>
              <CodeBlock
                empty={
                  !workspace.exists
                    ? missingWorkspaceText
                    : !workspace.isGitRepository
                      ? "Workspace exists, but it is not a git worktree."
                      : "No diff against HEAD."
                }
              >
                {workspace.exists && workspace.isGitRepository && workspace.diffPreview ? (
                  <pre className="whitespace-pre-wrap">{workspace.diffPreview}</pre>
                ) : null}
              </CodeBlock>
            </div>

            <div className="mt-4 rounded-[9px] bg-[color:var(--surface)] p-3 shadow-[0_0_0_1px_var(--line)]">
              <div className="mb-3 flex items-center gap-2 text-[13px] font-semibold text-[color:var(--muted-strong)]">
                <GitCompareArrows data-icon />
                Relative to base
              </div>
              {workspace.comparison.error ? (
                <Notice tone="danger">{workspace.comparison.error}</Notice>
              ) : (
                <div className="flex flex-col gap-4">
                  <div className="grid gap-3 md:grid-cols-2">
                    <DataBlock label="Base ref">{workspace.comparison.baseRef || "not available yet"}</DataBlock>
                    <DataBlock label="Merge base">
                      {workspace.comparison.mergeBaseShort || workspace.comparison.mergeBase || "not available yet"}
                    </DataBlock>
                    <DataBlock label="Ahead">{workspace.comparison.aheadCount}</DataBlock>
                    <DataBlock label="Behind">{workspace.comparison.behindCount}</DataBlock>
                  </div>

                  <div className="flex flex-col gap-2">
                    <div className="text-[13px] font-semibold text-[color:var(--muted-strong)]">Commits on this session branch</div>
                    <CodeBlock empty="No commits ahead of the base ref yet.">
                      {workspace.comparison.commitLines.map((line) => <div key={line}>{line}</div>)}
                    </CodeBlock>
                  </div>

                  <div className="flex flex-col gap-2">
                    <div className="text-[13px] font-semibold text-[color:var(--muted-strong)]">Files changed since merge base</div>
                    {workspace.comparison.changes.length === 0 ? (
                      <DataBlock label="No branch-level changes">No branch-level changes relative to the base ref yet.</DataBlock>
                    ) : (
                      <div className="flex flex-col gap-2">
                        {workspace.comparison.changes.map((change) => (
                          <ChangeRow key={`comparison-${change.statusCode}-${change.previousPath}-${change.path}`} change={change} />
                        ))}
                      </div>
                    )}
                  </div>

                  <div className="flex flex-col gap-2">
                    <div className="flex items-center justify-between gap-3">
                      <div className="text-[13px] font-semibold text-[color:var(--muted-strong)]">Base comparison diff</div>
                      {workspace.comparison.diffTruncated ? <InlineMeta>Preview truncated</InlineMeta> : null}
                    </div>
                    <CodeBlock empty="No diff relative to the merge base.">
                      {workspace.comparison.diffPreview ? (
                        <pre className="whitespace-pre-wrap">{workspace.comparison.diffPreview}</pre>
                      ) : null}
                    </CodeBlock>
                  </div>
                </div>
              )}
            </div>
          </Panel>
        </div>

        <Panel title="Live log stream">
          <CodeBlock className="max-h-[70vh]" empty="No logs yet.">
            {logs.map((line, index) => <div key={`${index}-${line}`}>{line}</div>)}
          </CodeBlock>
        </Panel>
      </div>
    </PageFrame>
  );
}
