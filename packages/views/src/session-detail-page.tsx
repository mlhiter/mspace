import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { api, buildApiUrl, queryKeys, type SessionStreamEvent } from "@mspace/core";
import { Button, Panel, PageFrame, StatusBadge } from "@mspace/ui";

export function SessionDetailPage() {
  const { sessionId = "" } = useParams();
  const queryClient = useQueryClient();
  const sessionQuery = useQuery({
    queryKey: queryKeys.session(sessionId),
    queryFn: () => api.getSession(sessionId),
    enabled: sessionId.length > 0,
  });
  const [logs, setLogs] = useState<string[]>([]);

  useEffect(() => {
    if (!sessionQuery.data) return;
    setLogs(sessionQuery.data.logs.map((log) => log.message));
  }, [sessionQuery.data]);

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

  if (!sessionQuery.data) {
    return (
      <PageFrame title="Session" subtitle="Inspect live logs, local runtime state, and validation history.">
        <Panel>{sessionQuery.isPending ? "Loading session..." : "Session not found."}</Panel>
      </PageFrame>
    );
  }

  const { session, issue, project } = sessionQuery.data;

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
      <div className="grid gap-6 xl:grid-cols-[0.9fr_1.1fr]">
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
              <dt className="font-semibold">Command</dt>
              <dd className="mt-1 whitespace-pre-wrap rounded-lg bg-[color:var(--background)] px-3 py-2 text-xs text-[color:var(--muted)]">
                {session.command || "runner default"}
              </dd>
            </div>
            <div>
              <dt className="font-semibold">Workdir</dt>
              <dd className="mt-1 text-[color:var(--muted)]">{session.workdir || "not reported yet"}</dd>
            </div>
          </dl>
        </Panel>
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
