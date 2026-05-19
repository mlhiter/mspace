import { useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowRight, Bot, Clock3, Inbox, Layers3, MessageSquareText } from "lucide-react";
import { controlPlaneApi, queryKeys, type WorkspaceInboxItem } from "@mspace/core";
import { t as translate, useMspaceTranslation } from "@mspace/i18n";
import {
  CollectionEmptyState,
  InlineMeta,
  Panel,
  PageFrame,
  StatusBadge,
} from "@mspace/ui";
import { useMspaceAuth } from "./auth-context";
import { displayIssueStatus, issueStatusLabel } from "./issue-status";
import { RelativeTime } from "./time";

type ReviewItem = {
  key: string;
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
  const { t } = useMspaceTranslation();
  const auth = useMspaceAuth();
  const queryClient = useQueryClient();
  const workspaceId = auth.workspace?.id || "";
  const inboxEnabled = auth.token !== "" && workspaceId !== "";

  const inboxQuery = useQuery({
    queryKey: queryKeys.workspaceInbox(workspaceId, auth.token),
    queryFn: () => controlPlaneApi.listInbox(auth.token, workspaceId),
    enabled: inboxEnabled,
    refetchInterval: inboxEnabled ? 5_000 : false,
  });

  const markReviewed = useMutation({
    mutationFn: async (item: ReviewItem) => {
      if (auth.token && workspaceId) {
        await controlPlaneApi.markIssueReadThrough(auth.token, workspaceId, item.issueId, item.eventId);
      }
    },
    onSuccess: () => {
      if (inboxEnabled) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.workspaceInbox(workspaceId, auth.token) });
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.workspaceIssues(workspaceId, auth.token) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.workspaceProjects(workspaceId, auth.token) });
    },
  });

  const items = (inboxQuery.data || []).map(workspaceInboxItemToReviewItem);

  useEffect(() => {
    if (!inboxEnabled) return;
    const timer = window.setInterval(() => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.workspaceInbox(workspaceId, auth.token) });
    }, 5_000);
    return () => window.clearInterval(timer);
  }, [auth.token, inboxEnabled, queryClient, workspaceId]);

  return (
    <PageFrame
      title={t("inbox.title")}
      subtitle={t("inbox.subtitle")}
    >
      {items.length === 0 ? (
        <CollectionEmptyState
          icon={Inbox}
          title={t("inbox.emptyTitle")}
          body={t("inbox.emptyBody")}
        />
      ) : (
        <Panel title={t("inbox.needsReview")}>
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
                      {t("inbox.workspaceEvent")}
                      {item.unreadCount > 1 ? ` · ${t("inbox.unread", { count: item.unreadCount })}` : ""}
                    </InlineMeta>
                    <InlineMeta icon={Clock3}><RelativeTime value={item.updatedAt} /></InlineMeta>
                    <InlineMeta icon={Layers3}>{item.projectName || t("common.noProject")}</InlineMeta>
                    <InlineMeta icon={item.assigneeType === "agent" ? Bot : Layers3}>
                      {item.assigneeType === "agent" ? t("inbox.agent") : t("inbox.human")} · {item.assignee || t("inbox.unassigned")}
                    </InlineMeta>
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <StatusBadge value={displayIssueStatus(item.status)} valueLabel={issueStatusLabel(item.status)} />
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

function workspaceInboxItemToReviewItem(item: WorkspaceInboxItem): ReviewItem {
  return {
    key: `workspace:${item.eventId}`,
    issueId: item.issueId,
    eventId: item.eventId,
    title: payloadText(item.payload, ["issueTitle", "title"]) || item.summary || `Issue ${item.issueId.slice(0, 8)}`,
    summary: item.summary || eventKindLabel(item.kind),
    status: payloadText(item.payload, ["issueStatus", "status"]) || eventKindLabel(item.kind),
    projectName: payloadText(item.payload, ["projectName", "projectId"]) || translate("common.noProject"),
    assignee: payloadText(item.payload, ["assignee"]) || "",
    assigneeType: payloadText(item.payload, ["assigneeType"]) || "human",
    updatedAt: item.createdAt,
    unreadCount: item.unreadCount,
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
