import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowRight, Bot, Clock3, Inbox, Layers3, MessageSquareText } from "lucide-react";
import { api, buildApiUrl, queryKeys } from "@mspace/core";
import {
  CollectionEmptyState,
  InlineMeta,
  Panel,
  PageFrame,
  StatusBadge,
} from "@mspace/ui";
import { RelativeTime } from "./time";

export function InboxPage() {
  const queryClient = useQueryClient();
  const inboxQuery = useQuery({
    queryKey: queryKeys.inbox,
    queryFn: api.listInbox,
  });

  const items = inboxQuery.data || [];

  useEffect(() => {
    const eventSource = new EventSource(buildApiUrl("/api/inbox/stream"));
    const refreshInbox = () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.inbox });
      void queryClient.invalidateQueries({ queryKey: queryKeys.issues });
      void queryClient.invalidateQueries({ queryKey: queryKeys.projects });
    };
    eventSource.addEventListener("message", refreshInbox);
    return () => {
      eventSource.removeEventListener("message", refreshInbox);
      eventSource.close();
    };
  }, [queryClient]);

  return (
    <PageFrame
      title="Inbox"
      subtitle="Review issue updates that need attention. Creating and managing issues lives in the Issues tab."
    >
      {items.length === 0 ? (
        <CollectionEmptyState
          icon={Inbox}
          title="Inbox is clear"
          body="Comments, status changes, and agent progress will appear here when they need review."
        />
      ) : (
        <Panel title="Needs review">
          <div className="flex flex-col gap-1">
            {items.map((item) => (
              <Link
                key={item.id}
                to="/issues/$issueId"
                params={{ issueId: item.issueId }}
                className="group flex items-center justify-between gap-4 rounded-[8px] px-3 py-2.5 transition-[background-color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] active:scale-[0.99]"
              >
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-[14px] font-medium text-[color:var(--text)]">{item.title}</span>
                    <span className="size-1.5 shrink-0 rounded-full bg-[color:var(--accent)]" />
                  </div>
                  <div className="mt-1 flex flex-wrap items-center gap-3">
                    <InlineMeta icon={MessageSquareText}>Review update</InlineMeta>
                    <InlineMeta icon={Clock3}><RelativeTime value={item.updatedAt} /></InlineMeta>
                    <InlineMeta icon={Layers3}>{item.projectName || item.projectId.slice(0, 8)}</InlineMeta>
                    <InlineMeta icon={item.assigneeType === "agent" ? Bot : Layers3}>
                      {item.assigneeType === "agent" ? "agent" : "human"} · {item.assignee || "unassigned"}
                    </InlineMeta>
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
        </Panel>
      )}
    </PageFrame>
  );
}
