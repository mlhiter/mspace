import { useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowRight, Bot, Clock3, Inbox, Layers3, MessageSquareText } from "lucide-react";
import { api, buildApiUrl, controlPlaneApi, queryKeys, type InboxItem, type TeamInboxItem } from "@mspace/core";
import {
  CollectionEmptyState,
  InlineMeta,
  Panel,
  PageFrame,
  StatusBadge,
} from "@mspace/ui";
import { useMspaceAuth } from "./auth-context";
import { displayIssueStatus } from "./issue-status";
import { RelativeTime } from "./time";

type ReviewItem = {
  key: string;
  source: "team" | "local";
  issueId: string;
  eventId?: string;
  title: string;
  summary: string;
  status: string;
  projectName: string;
  assignee: string;
  assigneeType: string;
  updatedAt: string;
  unreadCount: number;
};

export function InboxPage() {
  const auth = useMspaceAuth();
  const queryClient = useQueryClient();
  const workspaceId = auth.workspace?.id || "";
  const teamInboxEnabled = auth.token !== "" && workspaceId !== "";

  const localInboxQuery = useQuery({
    queryKey: queryKeys.inbox,
    queryFn: api.listInbox,
  });
  const teamInboxQuery = useQuery({
    queryKey: queryKeys.teamInbox(workspaceId, auth.token),
    queryFn: () => controlPlaneApi.listInbox(auth.token, workspaceId),
    enabled: teamInboxEnabled,
    refetchInterval: teamInboxEnabled ? 5_000 : false,
  });

  const markReviewed = useMutation({
    mutationFn: async (item: ReviewItem) => {
      if (item.source === "team" && auth.token && workspaceId) {
        await controlPlaneApi.markIssueReadThrough(auth.token, workspaceId, item.issueId, item.eventId);
        await api.markInboxIssueRead(item.issueId).catch(() => undefined);
        return;
      }
      if (item.source === "local") {
        await api.markInboxIssueRead(item.issueId);
      }
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.inbox });
      if (teamInboxEnabled) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.teamInbox(workspaceId, auth.token) });
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.issues });
      void queryClient.invalidateQueries({ queryKey: queryKeys.projects });
    },
  });

  const teamItems = (teamInboxQuery.data || []).map(teamInboxItemToReviewItem);
  const teamIssueIds = new Set(teamItems.map((item) => item.issueId));
  const localItems = (localInboxQuery.data || [])
    .filter((item) => !teamIssueIds.has(item.issueId))
    .map(localInboxItemToReviewItem);
  const items = [...teamItems, ...localItems];

  useEffect(() => {
    const eventSource = new EventSource(buildApiUrl("/api/inbox/stream"));
    const refreshInbox = () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.inbox });
      if (teamInboxEnabled) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.teamInbox(workspaceId, auth.token) });
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.issues });
      void queryClient.invalidateQueries({ queryKey: queryKeys.projects });
    };
    eventSource.addEventListener("message", refreshInbox);
    return () => {
      eventSource.removeEventListener("message", refreshInbox);
      eventSource.close();
    };
  }, [auth.token, queryClient, teamInboxEnabled, workspaceId]);

  return (
    <PageFrame
      title="Inbox"
      subtitle="Review team issue events that need your attention. Creating and managing issues lives in the Issues tab."
    >
      {items.length === 0 ? (
        <CollectionEmptyState
          icon={Inbox}
          title="Inbox is clear"
          body="Comments, agent results, status changes, and evidence events will appear here when they need review."
        />
      ) : (
        <Panel title="Needs review">
          <div className="flex flex-col gap-1">
            {items.map((item) => (
              <Link
                key={item.key}
                to="/issues/$issueId"
                params={{ issueId: item.issueId }}
                onClick={() => markReviewed.mutate(item)}
                className="group flex items-center justify-between gap-4 rounded-[8px] px-3 py-2.5 transition-[background-color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] active:scale-[0.99]"
              >
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-[14px] font-medium text-[color:var(--text)]">{item.title}</span>
                    <span className="size-1.5 shrink-0 rounded-full bg-[color:var(--inbox-unread-dot)]" />
                  </div>
                  {item.summary ? (
                    <div className="mt-1 line-clamp-1 text-[12px] leading-5 text-[color:var(--muted)]">{item.summary}</div>
                  ) : null}
                  <div className="mt-1 flex flex-wrap items-center gap-3">
                    <InlineMeta icon={MessageSquareText}>
                      {item.source === "team" ? "Team event" : "Local update"}
                      {item.unreadCount > 1 ? ` · ${item.unreadCount} unread` : ""}
                    </InlineMeta>
                    <InlineMeta icon={Clock3}><RelativeTime value={item.updatedAt} /></InlineMeta>
                    <InlineMeta icon={Layers3}>{item.projectName}</InlineMeta>
                    <InlineMeta icon={item.assigneeType === "agent" ? Bot : Layers3}>
                      {item.assigneeType === "agent" ? "agent" : "human"} · {item.assignee || "unassigned"}
                    </InlineMeta>
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <StatusBadge value={displayIssueStatus(item.status)} />
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

function teamInboxItemToReviewItem(item: TeamInboxItem): ReviewItem {
  return {
    key: `team:${item.eventId}`,
    source: "team",
    issueId: item.issueId,
    eventId: item.eventId,
    title: payloadText(item.payload, ["issueTitle", "title"]) || item.summary || `Issue ${item.issueId.slice(0, 8)}`,
    summary: item.summary || eventKindLabel(item.kind),
    status: payloadText(item.payload, ["issueStatus", "status"]) || eventKindLabel(item.kind),
    projectName: payloadText(item.payload, ["projectName", "projectId"]) || "Team workspace",
    assignee: payloadText(item.payload, ["assignee"]) || "",
    assigneeType: payloadText(item.payload, ["assigneeType"]) || "human",
    updatedAt: item.createdAt,
    unreadCount: item.unreadCount,
  };
}

function localInboxItemToReviewItem(item: InboxItem): ReviewItem {
  return {
    key: `local:${item.id}`,
    source: "local",
    issueId: item.issueId,
    title: item.title,
    summary: "Local runner update",
    status: item.status,
    projectName: item.projectName || item.projectId.slice(0, 8),
    assignee: item.assignee,
    assigneeType: item.assigneeType,
    updatedAt: item.updatedAt,
    unreadCount: 1,
  };
}

function payloadText(payload: Record<string, unknown>, keys: string[]): string {
  for (const key of keys) {
    const value = payload[key];
    if (typeof value === "string" && value.trim()) {
      return value.trim();
    }
  }
  return "";
}

function eventKindLabel(kind: string): string {
  return kind
    .split("_")
    .filter(Boolean)
    .map((part) => part.slice(0, 1).toUpperCase() + part.slice(1))
    .join(" ");
}
