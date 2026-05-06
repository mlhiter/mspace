import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { api, buildApiUrl, queryKeys, type SessionStreamEvent } from "@mspace/core";
import { Button, Notice, Panel, PageFrame, StatusBadge, Textarea } from "@mspace/ui";

export function SessionDetailPage() {
  const { sessionId = "" } = useParams();
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

    const { session, project, workspace, evidence } = sessionQuery.data;
    const lines = [
      `Session ${session.id.slice(0, 8)} summary`,
      "",
      `- Status: ${session.status}`,
      `- Provider: ${session.provider}`,
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
        lines.push(`- [${change.statusCode || "--"}] ${change.path}${renameSuffix}`);
      }
    } else if (workspace.changes.length > 0) {
      lines.push("", "Uncommitted workspace changes:");
      for (const change of workspace.changes.slice(0, 12)) {
        const renameSuffix = change.previousPath ? ` (from ${change.previousPath})` : "";
        lines.push(`- [${change.statusCode || "--"}] ${change.path}${renameSuffix}`);
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
    setLogs(sessionQuery.data.logs.map((log) => log.message));
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

  const postSummaryMutation = useMutation({
    mutationFn: () => api.addComment(sessionId ? sessionQuery.data!.issue.id : "", { body: summaryDraft }),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.issue(sessionQuery.data!.issue.id) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.session(sessionId) }),
      ]);
    },
  });

  if (!sessionQuery.data) {
    return (
      <PageFrame title="Session" subtitle="Inspect live logs, local runtime state, and validation history.">
        <Panel>{sessionQuery.isPending ? "Loading session..." : "Session not found."}</Panel>
      </PageFrame>
    );
  }

  const { session, issue, project, workspace } = sessionQuery.data;

  async function handleCopySummary() {
    try {
      await navigator.clipboard.writeText(summaryDraft);
      setCopyState("copied");
    } catch {
      setCopyState("failed");
    }
  }

  return (
    <PageFrame
      title={`Session ${session.id.slice(0, 8)}`}
      subtitle={`${project.name} · issue ${issue.title}`}
      actions={
        <div className="flex gap-3">
          <Link to={`/issues/${issue.id}`}>
            <Button variant="secondary">Back to issue</Button>
          </Link>
          <Button
            variant="danger"
            disabled={cancelMutation.isPending || !["queued", "running"].includes(session.status)}
            onClick={() => cancelMutation.mutate()}
          >
            Cancel session
          </Button>
        </div>
      }
    >
      <div className="grid gap-6 xl:grid-cols-[0.92fr_1.08fr]">
        <div className="space-y-6">
          <Panel title="Session Metadata" aside={<StatusBadge value={session.status} />}>
            <dl className="grid gap-4 text-sm">
              <div>
                <dt className="font-semibold">Provider</dt>
                <dd className="mt-1 text-[color:var(--muted)]">{session.provider}</dd>
              </div>
              <div>
                <dt className="font-semibold">Runtime mode</dt>
                <dd className="mt-1 text-[color:var(--muted)]">{session.runtimeMode}</dd>
              </div>
              <div>
                <dt className="font-semibold">Session branch</dt>
                <dd className="mt-1 break-all text-[color:var(--muted)]">{session.branch}</dd>
              </div>
              <div>
                <dt className="font-semibold">Command</dt>
                <dd className="mt-1 whitespace-pre-wrap rounded-lg bg-[color:var(--background)] px-3 py-2 text-xs text-[color:var(--muted)]">
                  {session.command || "runner default"}
                </dd>
              </div>
              <div>
                <dt className="font-semibold">Session workspace</dt>
                <dd className="mt-1 break-all text-[color:var(--muted)]">{session.workdir || "not reported yet"}</dd>
              </div>
              <div>
                <dt className="font-semibold">Source repository</dt>
                <dd className="mt-1 break-all text-[color:var(--muted)]">{project.repoPath}</dd>
              </div>
            </dl>
          </Panel>

          <Panel title="Issue Summary Draft">
            {copyState === "copied" ? <Notice>Summary copied to clipboard.</Notice> : null}
            {copyState === "failed" ? <Notice tone="danger">Clipboard access failed. You can still copy from the draft below.</Notice> : null}
            {postSummaryMutation.error ? <Notice tone="danger">{postSummaryMutation.error.message}</Notice> : null}
            <div className="space-y-4">
              <Textarea
                value={summaryDraft}
                onChange={(event) => setSummaryDraft(event.target.value)}
                className="min-h-64 font-mono text-xs leading-6"
              />
              <div className="flex flex-wrap gap-3">
                <Button variant="secondary" onClick={() => setSummaryDraft(generatedSummary)}>
                  Regenerate draft
                </Button>
                <Button variant="secondary" onClick={() => void handleCopySummary()}>
                  Copy summary
                </Button>
                <Button
                  disabled={postSummaryMutation.isPending || !summaryDraft.trim()}
                  onClick={() => postSummaryMutation.mutate()}
                >
                  {postSummaryMutation.isPending ? "Posting summary..." : "Post to issue"}
                </Button>
              </div>
            </div>
          </Panel>

          <Panel title="Workspace Snapshot">
            {workspace.error ? <Notice tone="danger">{workspace.error}</Notice> : null}
            <div className="grid gap-3 rounded-xl bg-[color:var(--background)] px-4 py-4 text-xs text-[color:var(--muted)] md:grid-cols-2">
              <div>
                <div className="font-semibold text-[color:var(--text)]">Workspace branch</div>
                <div className="mt-1 break-all">{workspace.branch || session.branch || "not available yet"}</div>
              </div>
              <div>
                <div className="font-semibold text-[color:var(--text)]">HEAD</div>
                <div className="mt-1 break-all">{workspace.shortHead || workspace.head || "not available yet"}</div>
              </div>
              <div>
                <div className="font-semibold text-[color:var(--text)]">Changed tracked files</div>
                <div className="mt-1">{workspace.changedFiles}</div>
              </div>
              <div>
                <div className="font-semibold text-[color:var(--text)]">Untracked files</div>
                <div className="mt-1">{workspace.untrackedFiles}</div>
              </div>
            </div>
            <div className="mt-4">
              <div className="mb-2 text-sm font-semibold">Git status</div>
              <div className="overflow-auto rounded-xl bg-[#13202b] px-4 py-4 font-mono text-xs leading-6 text-[#dbe7f2]">
                {!workspace.exists ? (
                  <div className="text-[#9fb2c5]">Workspace has not been created yet.</div>
                ) : !workspace.isGitRepository ? (
                  <div className="text-[#9fb2c5]">Workspace exists, but it is not a git worktree.</div>
                ) : workspace.statusLines.length === 0 ? (
                  <div className="text-[#9fb2c5]">Working tree clean.</div>
                ) : (
                  workspace.statusLines.map((line) => <div key={line}>{line}</div>)
                )}
              </div>
            </div>
            <div className="mt-4">
              <div className="mb-2 text-sm font-semibold">Changed files</div>
              <div className="space-y-2">
                {workspace.changes.length === 0 ? (
                  <div className="rounded-xl bg-[color:var(--background)] px-4 py-3 text-sm text-[color:var(--muted)]">
                    No file changes in this workspace.
                  </div>
                ) : (
                  workspace.changes.map((change) => (
                    <div
                      key={`${change.statusCode}-${change.previousPath}-${change.path}`}
                      className="rounded-xl border border-[color:var(--border)] bg-white px-4 py-3 text-sm"
                    >
                      <div className="flex flex-wrap items-center gap-3">
                        <span className="rounded-full bg-[color:var(--accent-soft)] px-2.5 py-1 text-xs font-medium text-[color:var(--accent)]">
                          {change.statusCode || "--"}
                        </span>
                        <span className="break-all font-medium text-[color:var(--text)]">{change.path || "(unknown path)"}</span>
                      </div>
                      {change.previousPath ? (
                        <div className="mt-2 break-all text-xs text-[color:var(--muted)]">
                          renamed from {change.previousPath}
                        </div>
                      ) : null}
                    </div>
                  ))
                )}
              </div>
            </div>
            <div className="mt-4">
              <div className="mb-2 flex items-center justify-between gap-3">
                <div className="text-sm font-semibold">Diff preview</div>
                {workspace.diffTruncated ? (
                  <div className="text-xs text-[color:var(--muted)]">Preview truncated</div>
                ) : null}
              </div>
              <div className="overflow-auto rounded-xl bg-[#13202b] px-4 py-4 font-mono text-xs leading-6 text-[#dbe7f2]">
                {!workspace.exists ? (
                  <div className="text-[#9fb2c5]">Workspace has not been created yet.</div>
                ) : !workspace.isGitRepository ? (
                  <div className="text-[#9fb2c5]">Workspace exists, but it is not a git worktree.</div>
                ) : workspace.diffPreview ? (
                  <pre className="whitespace-pre-wrap">{workspace.diffPreview}</pre>
                ) : (
                  <div className="text-[#9fb2c5]">No diff against HEAD.</div>
                )}
              </div>
            </div>
            <div className="mt-4 rounded-xl border border-[color:var(--border)] bg-white px-4 py-4">
              <div className="mb-3 text-sm font-semibold">Relative to base</div>
              {workspace.comparison.error ? (
                <Notice tone="danger">{workspace.comparison.error}</Notice>
              ) : (
                <div className="space-y-4">
                  <div className="grid gap-3 rounded-xl bg-[color:var(--background)] px-4 py-4 text-xs text-[color:var(--muted)] md:grid-cols-2">
                    <div>
                      <div className="font-semibold text-[color:var(--text)]">Base ref</div>
                      <div className="mt-1 break-all">{workspace.comparison.baseRef || "not available yet"}</div>
                    </div>
                    <div>
                      <div className="font-semibold text-[color:var(--text)]">Merge base</div>
                      <div className="mt-1 break-all">
                        {workspace.comparison.mergeBaseShort || workspace.comparison.mergeBase || "not available yet"}
                      </div>
                    </div>
                    <div>
                      <div className="font-semibold text-[color:var(--text)]">Ahead</div>
                      <div className="mt-1">{workspace.comparison.aheadCount}</div>
                    </div>
                    <div>
                      <div className="font-semibold text-[color:var(--text)]">Behind</div>
                      <div className="mt-1">{workspace.comparison.behindCount}</div>
                    </div>
                  </div>

                  <div>
                    <div className="mb-2 text-sm font-semibold">Commits on this session branch</div>
                    <div className="overflow-auto rounded-xl bg-[#13202b] px-4 py-4 font-mono text-xs leading-6 text-[#dbe7f2]">
                      {workspace.comparison.commitLines.length === 0 ? (
                        <div className="text-[#9fb2c5]">No commits ahead of the base ref yet.</div>
                      ) : (
                        workspace.comparison.commitLines.map((line) => <div key={line}>{line}</div>)
                      )}
                    </div>
                  </div>

                  <div>
                    <div className="mb-2 text-sm font-semibold">Files changed since merge base</div>
                    <div className="space-y-2">
                      {workspace.comparison.changes.length === 0 ? (
                        <div className="rounded-xl bg-[color:var(--background)] px-4 py-3 text-sm text-[color:var(--muted)]">
                          No branch-level changes relative to the base ref yet.
                        </div>
                      ) : (
                        workspace.comparison.changes.map((change) => (
                          <div
                            key={`comparison-${change.statusCode}-${change.previousPath}-${change.path}`}
                            className="rounded-xl border border-[color:var(--border)] bg-[color:var(--background)] px-4 py-3 text-sm"
                          >
                            <div className="flex flex-wrap items-center gap-3">
                              <span className="rounded-full bg-[color:var(--accent-soft)] px-2.5 py-1 text-xs font-medium text-[color:var(--accent)]">
                                {change.statusCode || "--"}
                              </span>
                              <span className="break-all font-medium text-[color:var(--text)]">{change.path || "(unknown path)"}</span>
                            </div>
                            {change.previousPath ? (
                              <div className="mt-2 break-all text-xs text-[color:var(--muted)]">
                                renamed from {change.previousPath}
                              </div>
                            ) : null}
                          </div>
                        ))
                      )}
                    </div>
                  </div>

                  <div>
                    <div className="mb-2 flex items-center justify-between gap-3">
                      <div className="text-sm font-semibold">Base comparison diff</div>
                      {workspace.comparison.diffTruncated ? (
                        <div className="text-xs text-[color:var(--muted)]">Preview truncated</div>
                      ) : null}
                    </div>
                    <div className="overflow-auto rounded-xl bg-[#13202b] px-4 py-4 font-mono text-xs leading-6 text-[#dbe7f2]">
                      {workspace.comparison.diffPreview ? (
                        <pre className="whitespace-pre-wrap">{workspace.comparison.diffPreview}</pre>
                      ) : (
                        <div className="text-[#9fb2c5]">No diff relative to the merge base.</div>
                      )}
                    </div>
                  </div>
                </div>
              )}
            </div>
          </Panel>
        </div>

        <Panel title="Live Log Stream">
          <div className="max-h-[70vh] overflow-auto rounded-xl bg-[#13202b] px-4 py-4 font-mono text-xs leading-6 text-[#dbe7f2]">
            {logs.length === 0 ? (
              <div className="text-[#9fb2c5]">No logs yet.</div>
            ) : (
              logs.map((line, index) => <div key={`${index}-${line}`}>{line}</div>)
            )}
          </div>
        </Panel>
      </div>
    </PageFrame>
  );
}
