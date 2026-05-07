import { useCallback, useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { Clock3, Files, MessageSquareText, Play, SquareTerminal } from "lucide-react";
import { api, buildApiUrl, queryKeys, type SessionStreamEvent } from "@mspace/core";
import {
  Button,
  CodeBlock,
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
  const defaultWorkflowDescription =
    detail && (detail.project.deployCommand || detail.project.validationCommand || detail.project.namespace)
      ? [
          detail.project.deployCommand ? "deploy command" : null,
          detail.project.validationCommand
            ? "validation command"
            : detail.project.namespace
              ? "cluster snapshot"
              : null,
        ]
          .filter(Boolean)
          .join(" -> ")
      : "session command override only";

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
          <Button asChild variant="secondary">
            <Link to={`/sessions/${latestSession.id}`}>Open session detail</Link>
          </Button>
        ) : undefined
      }
    >
      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.04fr)_400px]">
        <div className="flex flex-col gap-6">
          <Panel title="Issue document" aside={<StatusBadge value={detail.issue.status} />}>
            <div className="flex flex-col gap-4">
              <p className="whitespace-pre-wrap text-[15px] leading-7 text-[color:var(--text)] text-pretty">
                {detail.issue.body || "No issue body yet."}
              </p>
              <div className="grid gap-3 md:grid-cols-2">
                <DataBlock label="Repo path" icon={Files}>
                  {detail.project.repoPath || "not configured"}
                </DataBlock>
                <DataBlock label="Namespace" icon={SquareTerminal}>
                  {detail.project.namespace || "not configured"}
                </DataBlock>
              </div>
            </div>
          </Panel>

          <Panel title="Activity">
            <div className="flex flex-col gap-3">
              {detail.comments.length === 0 ? (
                <EmptyState
                  icon={MessageSquareText}
                  title="No comments yet"
                  body="Add progress notes, blockers, or validation findings directly on the issue."
                />
              ) : (
                detail.comments.map((comment) => (
                  <article key={comment.id} className="rounded-[9px] bg-[color:var(--block)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
                    <InlineMeta icon={Clock3}>
                      {comment.authorType} · {new Date(comment.createdAt).toLocaleString()}
                    </InlineMeta>
                    <div className="mt-2 whitespace-pre-wrap text-[14px] leading-7 text-[color:var(--text)] text-pretty">{comment.body}</div>
                  </article>
                ))
              )}
            </div>
            <form
              className="mt-5 flex flex-col gap-3"
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

        <div className="flex flex-col gap-6">
          <Panel title="Session panel">
            <form
              className="flex flex-col gap-4"
              onSubmit={(event) => {
                event.preventDefault();
                startSession.mutate();
              }}
            >
              {startSession.error ? (
                <Notice tone="danger">{startSession.error.message}</Notice>
              ) : (
                <Notice>
                  Sessions run locally in the MVP, then deploy and validate against the configured Kubernetes namespace.
                </Notice>
              )}
              <DataBlock label="Default workflow" icon={Play}>
                {defaultWorkflowDescription || "No default workflow configured yet."}
                <div className="mt-2 grid gap-1">
                  <div>
                    <span className="font-medium text-[color:var(--text)]">Deploy:</span>{" "}
                    {detail.project.deployCommand || "not configured"}
                  </div>
                  <div>
                    <span className="font-medium text-[color:var(--text)]">Validate:</span>{" "}
                    {detail.project.validationCommand || "not configured"}
                  </div>
                </div>
              </DataBlock>
              <Field label="Command override" hint="Leave empty to run the project's validation command or the runner default.">
                <Input value={sessionCommand} onChange={(event) => setSessionCommand(event.target.value)} placeholder="optional: override the full default workflow" />
              </Field>
              <Button type="submit" disabled={startSession.isPending}>
                {startSession.isPending ? "Starting session..." : "Start local session"}
              </Button>
            </form>

            {latestSession ? (
              <div className="mt-5 flex flex-col gap-3">
                <div className="flex items-center justify-between gap-3 rounded-[9px] bg-[color:var(--block)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
                  <div>
                    <div className="text-[13px] font-semibold">{latestSession.provider}</div>
                    <InlineMeta icon={Clock3}>
                      {latestSession.runtimeMode} · started {new Date(latestSession.createdAt).toLocaleString()}
                    </InlineMeta>
                  </div>
                  <StatusBadge value={latestSession.status} />
                </div>
                <CodeBlock className="max-h-72" empty="No session logs yet.">
                  {sessionLogs.map((line, index) => <div key={`${index}-${line}`}>{line}</div>)}
                </CodeBlock>
              </div>
            ) : null}
          </Panel>

          <Panel title="Evidence panel">
            {detail.evidence.length === 0 ? (
              <EmptyState
                icon={SquareTerminal}
                title="No Kubernetes evidence yet"
                body="Once the local session deploys or inspects the configured namespace, pods, events, and rollout summaries will appear here."
              />
            ) : (
              <div className="flex flex-col gap-3">
                {detail.evidence.map((evidence) => (
                  <article key={evidence.id} className="rounded-[9px] bg-[color:var(--block)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
                    <div className="flex items-center justify-between gap-3">
                      <div className="text-[13px] font-semibold">
                        {evidence.cluster || "current context"} / {evidence.namespace || "namespace unset"}
                      </div>
                      <InlineMeta icon={Clock3}>{new Date(evidence.createdAt).toLocaleString()}</InlineMeta>
                    </div>
                    <div className="mt-2 text-[13px] text-[color:var(--text)]">{evidence.summary}</div>
                    <CodeBlock className="mt-3">{evidence.details}</CodeBlock>
                  </article>
                ))}
              </div>
            )}
          </Panel>
        </div>
      </div>
    </PageFrame>
  );
}
