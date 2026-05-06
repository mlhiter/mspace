import { useCallback, useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { api, buildApiUrl, queryKeys, type SessionStreamEvent } from "@mspace/core";
import { Button, EmptyState, Field, Input, Panel, PageFrame, StatusBadge, Textarea } from "@mspace/ui";

function useSessionStream(sessionId: string | undefined, onEvent: (event: SessionStreamEvent) => void) {
  useEffect(() => {
    if (!sessionId) return;
    const eventSource = new EventSource(buildApiUrl(`/api/sessions/${sessionId}/stream`));
    const listener = (event: MessageEvent<string>) => {
      onEvent(JSON.parse(event.data) as SessionStreamEvent);
    };
    eventSource.addEventListener("message", listener as EventListener);
    return () => {
      eventSource.removeEventListener("message", listener as EventListener);
      eventSource.close();
    };
  }, [sessionId, onEvent]);
}

export function IssueDetailPage() {
  const { issueId = "" } = useParams();
  const queryClient = useQueryClient();
  const [commentBody, setCommentBody] = useState("");
  const [sessionCommand, setSessionCommand] = useState("");
  const [sessionLogs, setSessionLogs] = useState<string[]>([]);

  const issueQuery = useQuery({
    queryKey: queryKeys.issue(issueId),
    queryFn: () => api.getIssue(issueId),
    enabled: issueId.length > 0,
    refetchInterval: 4_000,
  });

  const detail = issueQuery.data;
  const latestSession = detail?.sessions[0];

  useEffect(() => {
    const latestSessionId = detail?.sessions[0]?.id;
    if (!latestSessionId) {
      setSessionLogs([]);
      return;
    }
    void api.getSession(latestSessionId).then((sessionDetail) => {
      setSessionLogs(sessionDetail.logs.map((log) => log.message));
    });
  }, [detail?.sessions]);

  const handleSessionEvent = useCallback(
    (event: SessionStreamEvent) => {
      if (event.type === "log") {
        setSessionLogs((current) => [...current, event.payload]);
        return;
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.issue(issueId) });
      if (latestSession?.id) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.session(latestSession.id) });
      }
    },
    [issueId, latestSession?.id, queryClient],
  );

  useSessionStream(latestSession?.id, handleSessionEvent);

  const addComment = useMutation({
    mutationFn: (body: string) => api.addComment(issueId, { body }),
    onSuccess: async () => {
      setCommentBody("");
      await queryClient.invalidateQueries({ queryKey: queryKeys.issue(issueId) });
    },
  });

  const startSession = useMutation({
    mutationFn: () =>
      api.createSession(issueId, {
        provider: "codex",
        command: sessionCommand.trim() || undefined,
      }),
    onSuccess: async () => {
      setSessionCommand("");
      await queryClient.invalidateQueries({ queryKey: queryKeys.issue(issueId) });
    },
  });

  if (!detail) {
    return (
      <PageFrame title="Issue" subtitle="Load the durable issue page, local session history, and Kubernetes evidence.">
        <Panel>{issueQuery.isPending ? "Loading issue..." : "Issue not found."}</Panel>
      </PageFrame>
    );
  }

  return (
    <PageFrame
      title={detail.issue.title}
      subtitle={`${detail.project.name} · local development with Kubernetes validation in ${detail.project.namespace || "the configured namespace"}`}
      actions={
        latestSession ? (
          <Link to={`/sessions/${latestSession.id}`}>
            <Button variant="secondary">Open session detail</Button>
          </Link>
        ) : undefined
      }
    >
      <div className="grid gap-6 xl:grid-cols-[1.08fr_0.92fr]">
        <div className="space-y-6">
          <Panel
            title="Issue Document"
            aside={<StatusBadge value={detail.issue.status} />}
          >
            <div className="space-y-4">
              <p className="whitespace-pre-wrap text-sm leading-7 text-[color:var(--text)]">
                {detail.issue.body || "No issue body yet."}
              </p>
              <div className="grid gap-3 rounded-xl bg-[color:var(--background)] px-4 py-4 text-xs text-[color:var(--muted)] md:grid-cols-2">
                <div>
                  <div className="font-semibold text-[color:var(--text)]">Repo path</div>
                  <div className="mt-1">{detail.project.repoPath || "not configured"}</div>
                </div>
                <div>
                  <div className="font-semibold text-[color:var(--text)]">Namespace</div>
                  <div className="mt-1">{detail.project.namespace || "not configured"}</div>
                </div>
              </div>
            </div>
          </Panel>

          <Panel title="Activity">
            <div className="space-y-4">
              {detail.comments.length === 0 ? (
                <EmptyState
                  title="No comments yet"
                  body="Add progress notes, blockers, or validation findings directly on the issue."
                />
              ) : (
                detail.comments.map((comment) => (
                  <div key={comment.id} className="rounded-xl border border-[color:var(--border)] bg-white px-4 py-4">
                    <div className="text-xs uppercase tracking-[0.12em] text-[color:var(--muted)]">
                      {comment.authorType} · {new Date(comment.createdAt).toLocaleString()}
                    </div>
                    <div className="mt-3 whitespace-pre-wrap text-sm leading-7">{comment.body}</div>
                  </div>
                ))
              )}
            </div>
            <form
              className="mt-5 space-y-3"
              onSubmit={(event) => {
                event.preventDefault();
                if (!commentBody.trim()) return;
                addComment.mutate(commentBody);
              }}
            >
              <Field label="Add comment">
                <Textarea value={commentBody} onChange={(event) => setCommentBody(event.target.value)} placeholder="What changed, what blocked, or what did Kubernetes prove?" />
              </Field>
              <Button type="submit" disabled={addComment.isPending}>
                {addComment.isPending ? "Posting..." : "Post comment"}
              </Button>
            </form>
          </Panel>
        </div>

        <div className="space-y-6">
          <Panel title="Session Panel">
            <form
              className="space-y-4"
              onSubmit={(event) => {
                event.preventDefault();
                startSession.mutate();
              }}
            >
              <div className="rounded-xl bg-[color:var(--background)] px-4 py-3 text-sm text-[color:var(--muted)]">
                Sessions run locally in the MVP, then deploy and validate against the configured Kubernetes namespace.
              </div>
              <Field label="Command override" hint="Leave empty to run the project's validation command or the runner default.">
                <Input value={sessionCommand} onChange={(event) => setSessionCommand(event.target.value)} placeholder="optional: make test, pnpm test, kubectl get pods ..." />
              </Field>
              <Button type="submit" disabled={startSession.isPending}>
                {startSession.isPending ? "Starting session..." : "Start local session"}
              </Button>
            </form>

            {latestSession ? (
              <div className="mt-5 space-y-4">
                <div className="flex items-center justify-between gap-3 rounded-xl border border-[color:var(--border)] bg-white px-4 py-4">
                  <div>
                    <div className="text-sm font-semibold">{latestSession.provider}</div>
                    <div className="mt-1 text-xs text-[color:var(--muted)]">
                      {latestSession.runtimeMode} · started {new Date(latestSession.createdAt).toLocaleString()}
                    </div>
                  </div>
                  <StatusBadge value={latestSession.status} />
                </div>
                <div className="max-h-72 overflow-auto rounded-xl bg-[#13202b] px-4 py-4 font-mono text-xs leading-6 text-[#dbe7f2]">
                  {sessionLogs.length === 0 ? (
                    <div className="text-[#9fb2c5]">No session logs yet.</div>
                  ) : (
                    sessionLogs.map((line, index) => <div key={`${index}-${line}`}>{line}</div>)
                  )}
                </div>
              </div>
            ) : null}
          </Panel>

          <Panel title="Evidence Panel">
            {detail.evidence.length === 0 ? (
              <EmptyState
                title="No Kubernetes evidence yet"
                body="Once the local session deploys or inspects the configured namespace, pods, events, and rollout summaries will appear here."
              />
            ) : (
              <div className="space-y-4">
                {detail.evidence.map((evidence) => (
                  <div key={evidence.id} className="rounded-xl border border-[color:var(--border)] bg-white px-4 py-4">
                    <div className="flex items-center justify-between gap-3">
                      <div className="text-sm font-semibold">
                        {evidence.cluster || "current context"} / {evidence.namespace || "namespace unset"}
                      </div>
                      <div className="text-xs text-[color:var(--muted)]">
                        {new Date(evidence.createdAt).toLocaleString()}
                      </div>
                    </div>
                    <div className="mt-3 text-sm text-[color:var(--text)]">{evidence.summary}</div>
                    <pre className="mt-3 overflow-auto rounded-lg bg-[color:var(--background)] px-3 py-3 text-xs text-[color:var(--muted)]">
                      {evidence.details}
                    </pre>
                  </div>
                ))}
              </div>
            )}
          </Panel>
        </div>
      </div>
    </PageFrame>
  );
}
