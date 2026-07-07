import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import type { Editor } from "@tiptap/core";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  ArrowLeft,
  BookOpenText,
  Bot,
  Boxes,
  Bug,
  CheckCircle2,
  ChevronDown,
  CircleAlert,
  CircleDot,
  CircleStop,
  Clock3,
  ExternalLink,
  Files,
  GitCommit,
  GitPullRequest,
  Globe2,
  History,
  ListChecks,
  Pencil,
  Plus,
  RefreshCw,
  Rocket,
  Save,
  Search,
  Send,
  SmilePlus,
  Trash2,
  WandSparkles,
  X,
} from "lucide-react";
import {
  controlPlaneApi,
  getStoredAuthIdentity,
  queryKeys,
  type AgentProfile,
  type AgentSession,
  type Cluster,
  type Comment,
  type CommentReactionSummary,
  type DeploymentEvidence,
  type IssueChangeNode,
  type IssueHandoff,
  type IssueLabel,
  type IssueLabelDefinition,
  type IssueListItem,
  type IssueDetail,
  type IssueTestEnvironment,
  type IssueTestEnvironmentResources,
  type Project,
  type ProjectRunbook,
  type ReviewEvidenceCheck,
  type ReviewEvidenceCommand,
  type ReviewEvidenceResult,
  type SessionFailure,
  type SessionLog,
  type SessionReviewEvidence,
  type StartTestDeployInput,
  type UpdateIssueLabelsInput,
  type WorkspaceChange,
} from "@mspace/core";
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
  Field,
  InlineMeta,
  Input,
  Notice,
  PageFrame,
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
  StatusBadge,
  cn,
} from "@mspace/ui";
import { t as translate, useMspaceTranslation } from "@mspace/i18n";
import { FileTypeIcon } from "./file-type-icon";
import { IssueDocumentEditor } from "./issue-document-editor";
import { useResolvedIssueImageSrc } from "./attachment-image";
import { codexAvatarDataUrl } from "./agent-avatar";
import { useMspaceAuth } from "./auth-context";
import { ensureRuntimeReady } from "./runtime-worker-readiness";
import {
  buildIssueLabelSelectionInput,
  issueLabelMatchesDimension,
  issueLabelOptionsByDimension,
  issueLabelOptionsForUI,
  nextIssueLabelSelection,
  selectedIssueLabelKey,
} from "./issue-labels";
import { IssueLabelBadge, IssueLabelOptionLabel, IssueLabelSelectValue } from "./issue-label-chip";
import { displayIssueStatus, issueStatusLabel } from "./issue-status";
import { formatAbsoluteTime, formatRelativeTime } from "./time";
import { visibleWorkspaceFileChanges, workspaceChangeStatusLabel, workspaceChangeStatusTone } from "./workspace-change-status";

type TimelineItem =
  | { kind: "opened"; createdAt: string }
  | { kind: "comment"; createdAt: string; comment: Comment }
  | { kind: "session"; createdAt: string; session: AgentSession }
  | { kind: "failure"; createdAt: string; failure: SessionFailure };

type IssueTab = "overview" | "commits" | "sessions" | "resources" | "evidence";
type ActorKind = "human" | "codex" | "system" | "evidence";

function isIssueTab(value: unknown): value is IssueTab {
  return value === "overview" || value === "commits" || value === "sessions" || value === "resources" || value === "evidence";
}

function issueTabSearch(tab: IssueTab) {
  return tab === "overview" ? {} : { tab };
}

function issueSessionDomId(sessionId: string) {
  return `issue-session-${sessionId}`;
}

interface ActorIdentity {
  kind: ActorKind;
  name?: string;
  avatarUrl?: string;
}

type LogLine = Pick<SessionLog, "stream" | "message">;
type SessionSnapshot = {
  logs: LogLine[];
  changes: WorkspaceChange[];
};
type EvidenceTone = "healthy" | "warning" | "failed" | "collected";
type MentionMenuPosition = {
  top: number;
  left: number;
  width: number;
};
type EditorMentionMatch = {
  query: string;
  from: number;
  to: number;
};

const AUTO_PREVIEW_CHECK_INTERVAL_MS = 60_000;
const autoPreviewCheckAtByIssue = new Map<string, number>();

type EvidenceResource = {
  kind: string;
  name: string;
  primaryLabel: string;
  primaryValue: string;
  fields: Array<{ label: string; value: string }>;
  tone: EvidenceTone;
};
type EvidenceEvent = {
  lastSeen: string;
  type: string;
  reason: string;
  object: string;
  message: string;
};
type ParsedEvidence = {
  resources: EvidenceResource[];
  events: EvidenceEvent[];
  tone: EvidenceTone;
};

function isClosedIssueStatus(status: string) {
  return status === "closed" || status === "completed";
}

function isIssueClosedForLifecycle(status: string) {
  return isClosedIssueStatus(status) || status === "cancelled";
}

function listOrEmpty<T>(items: T[] | null | undefined): T[] {
  return Array.isArray(items) ? items : [];
}

const COMMENT_REACTION_OPTIONS = [
  { reaction: "thumbs_up", emoji: "👍", labelKey: "issueDetail.reactions.thumbsUp" },
  { reaction: "thumbs_down", emoji: "👎", labelKey: "issueDetail.reactions.thumbsDown" },
  { reaction: "laugh", emoji: "😄", labelKey: "issueDetail.reactions.laugh" },
  { reaction: "hooray", emoji: "🎉", labelKey: "issueDetail.reactions.hooray" },
  { reaction: "confused", emoji: "😕", labelKey: "issueDetail.reactions.confused" },
  { reaction: "heart", emoji: "❤️", labelKey: "issueDetail.reactions.heart" },
  { reaction: "rocket", emoji: "🚀", labelKey: "issueDetail.reactions.rocket" },
  { reaction: "eyes", emoji: "👀", labelKey: "issueDetail.reactions.eyes" },
] as const;

function commentReactionOption(reaction: string) {
  return COMMENT_REACTION_OPTIONS.find((option) => option.reaction === reaction);
}

function mentionKey(value: string) {
  return value.trim().replace(/^@/, "").toLowerCase();
}

function findAgent(agents: AgentProfile[], agentId: string) {
  const key = mentionKey(agentId);
  return agents.find((agent) => agent.id.toLowerCase() === key || mentionKey(agent.mention) === key);
}

function extractAgentMention(value: string) {
  return value.match(/(?:^|[^\w])@([a-z][\w-]*)/i)?.[1].toLowerCase() || "";
}

function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function stripAgentMention(value: string, agent: string) {
  const mentionPattern = new RegExp(`(^|[^\\w])@${escapeRegExp(agent)}\\b`, "gi");
  return value
    .replace(mentionPattern, "$1")
    .replace(/[ \t]{2,}/g, " ")
    .replace(/[ \t]+([,.;:!?，。！？；、])/g, "$1")
    .trim();
}

function trailingMentionQuery(value: string) {
  return value.match(/(?:^|[^\w])@([a-z0-9_-]*)$/i)?.[1].toLowerCase() ?? null;
}

function mentionMatchInEditor(editor: Editor): EditorMentionMatch | null {
  const { selection } = editor.state;
  if (!selection.empty) return null;
  const beforeCursor = editor.state.doc.textBetween(0, selection.from, "\n", "\n");
  const match = beforeCursor.match(/(?:^|[^\w])@([a-z0-9_-]*)$/i);
  if (!match || match.index === undefined) return null;
  const mentionStartInMatch = match[0].lastIndexOf("@");
  const mentionStartInText = match.index + mentionStartInMatch;
  const mentionLength = beforeCursor.length - mentionStartInText;
  return {
    query: match[1].toLowerCase(),
    from: Math.max(1, selection.from - mentionLength),
    to: selection.from,
  };
}

function agentMentionText(agent: AgentProfile) {
  return agent.mention.startsWith("@") ? agent.mention : `@${agent.mention}`;
}

function insertAgentMention(value: string, agent: AgentProfile) {
  const mention = agentMentionText(agent);
  if (trailingMentionQuery(value) !== null) {
    return value.replace(/@([a-z0-9_-]*)$/i, `${mention} `);
  }
  const separator = value === "" || value.endsWith(" ") || value.endsWith("\n") ? "" : " ";
  return `${value}${separator}${mention} `;
}

function agentMentionOptionId(agent: AgentProfile) {
  return `issue-agent-mention-${agent.id.replace(/[^a-z0-9_-]/gi, "-")}`;
}

function mentionMenuPositionForEditor(editor: Editor, match: EditorMentionMatch): MentionMenuPosition {
  const gutter = 10;
  const container = editor.view.dom.closest("[data-comment-composer='true']") as HTMLElement | null;
  const containerRect = container?.getBoundingClientRect() || editor.view.dom.getBoundingClientRect();
  const caret = editor.view.coordsAtPos(match.to);
  const width = Math.min(384, Math.max(240, containerRect.width - gutter * 2));
  return {
    top: Math.max(8, caret.bottom - containerRect.top + 6),
    left: Math.max(gutter, Math.min(caret.left - containerRect.left, containerRect.width - width - gutter)),
    width,
  };
}

function fallbackAgent(agentId: string): AgentProfile {
  const id = mentionKey(agentId) || "codex";
  return {
    id,
    name: id.charAt(0).toUpperCase() + id.slice(1),
    mention: `@${id}`,
    provider: "codex",
    description: translate("agents.agent"),
    instructions: "",
    enabled: true,
    builtIn: false,
    sortOrder: 999,
    createdAt: "",
    updatedAt: "",
  };
}

function sessionAgent(session: AgentSession, agents: AgentProfile[]) {
  return findAgent(agents, session.agentProfile || session.provider) || fallbackAgent(session.agentProfile || session.provider);
}

function objectValue(value: unknown, key: string) {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? (value as Record<string, unknown>)[key] : undefined;
}

function normalizedMarker(value: unknown) {
  return typeof value === "string" ? value.trim().toLowerCase().replace(/[^a-z0-9]+/g, "_").replace(/^_+|_+$/g, "") : "";
}

function markerIndicatesIssueAnalysis(value: unknown) {
  const marker = normalizedMarker(value);
  if (!marker) return false;
  if (["issue_analysis", "automatic_issue_analysis", "issue_created_analysis", "created_issue_analysis"].includes(marker)) return true;
  return marker.includes("issue") && (marker.includes("analysis") || marker.includes("analyze") || marker.includes("analyse"));
}

function skillReferenceName(value: unknown): string {
  if (typeof value === "string") return value;
  if (typeof value !== "object" || value === null || Array.isArray(value)) return "";
  const record = value as Record<string, unknown>;
  for (const key of ["skill", "name", "slug", "key", "id"]) {
    const candidate = record[key];
    if (typeof candidate === "string" && candidate.trim()) return candidate;
  }
  return "";
}

function skillToken(value: unknown) {
  const name = skillReferenceName(value).trim().toLowerCase();
  if (!name) return "";
  const withoutRevision = name.split("@")[0] || name;
  return withoutRevision.split(/[/:]/).filter(Boolean).at(-1)?.replace(/[^a-z0-9_-]+/g, "") || "";
}

function sessionUsesSkill(session: AgentSession, skill: string) {
  const payload = session.payload || {};
  const skillSources = [
    session.requiredSkills,
    session.skills,
    objectValue(payload, "requiredSkills"),
    objectValue(payload, "skills"),
    objectValue(payload, "workflowSkills"),
    objectValue(payload, "skill"),
    objectValue(payload, "defaultSkill"),
  ];
  return skillSources.some((source) => {
    const values = Array.isArray(source) ? source : [source];
    return values.some((value) => skillToken(value) === skill);
  });
}

function isIssueAnalysisSession(session: AgentSession) {
  const payload = session.payload || {};
  const markers = [
    session.automation,
    objectValue(payload, "automation"),
    objectValue(payload, "purpose"),
    objectValue(payload, "workflow"),
    objectValue(payload, "workflowKind"),
    objectValue(payload, "taskKind"),
    objectValue(payload, "sessionType"),
    objectValue(payload, "analysisType"),
  ];
  if (markers.some(markerIndicatesIssueAnalysis)) return true;
  return sessionUsesSkill(session, "think") && [session.command, ...markers].some(markerIndicatesIssueAnalysis);
}

function formatMentionPlaceholder(agents: AgentProfile[]) {
  if (agents.length === 0) return translate("issueDetail.composer.noAgentsPlaceholder");
  const mentions = agents.slice(0, 3).map((agent) => agent.mention).join(", ");
  return translate("issueDetail.composer.placeholderWithMentions", { mentions });
}

function testDeployDefaults(detail: IssueDetail, clusters: Cluster[]): StartTestDeployInput {
  const clusterId = detail.testEnvironment?.environmentId || detail.testEnvironment?.clusterId || detail.project?.defaultEnvironmentId || detail.project?.defaultClusterId || clusters[0]?.id || "";
  const selectedCluster = clusters.find((cluster) => cluster.id === clusterId);
  const changeNodes = listOrEmpty(detail.changeNodes);
  const selectedSource =
    changeNodes.find((node) => node.commitSha === detail.testEnvironment?.sourceCommitSha) ||
    changeNodes.find((node) => node.sessionId === detail.testEnvironment?.sourceSessionId) ||
    changeNodes[0];
  return {
    agentProfile: "codex",
    clusterId,
    environmentId: clusterId,
    exposureMode: "",
    previewDomain: selectedCluster?.previewDomain || detail.testEnvironment?.previewDomain || "",
    ingressClass: selectedCluster?.ingressClass || detail.testEnvironment?.ingressClass || "",
    nodeHost: selectedCluster?.nodeHost || detail.testEnvironment?.nodeHost || "",
    sourceSessionId: selectedSource?.sessionId || "",
    sourceCommitSha: selectedSource?.commitSha || "",
  };
}

function previewStrategy(environment: IssueTestEnvironment | null | undefined) {
  if (!environment) return translate("issueDetail.environment.notRequested");
  if (environment.exposureMode === "ingress" || environment.previewDomain) return environment.ingressClass ? `Ingress · ${environment.ingressClass}` : "Ingress";
  return environment.nodeHost ? `NodePort · ${environment.nodeHost}` : "NodePort";
}

function cleanupDecisionLabel(status: string) {
  if (status === "retained") return translate("issueDetail.environment.retainedForDebug");
  if (status === "not_decided") return translate("issueDetail.environment.notDecided");
  if (status === "cleanup_requested") return translate("issueDetail.environment.cleanupRequested");
  if (status === "cleaned") return translate("issueDetail.environment.cleaned");
  if (status === "cleanup_failed") return translate("issueDetail.environment.cleanupFailed");
  return status ? status.replace(/[_-]+/g, " ") : translate("issueDetail.environment.notDecided");
}

function isNoActiveCodexWorkerError(error: unknown) {
  return error instanceof Error && /no active codex worker/i.test(error.message);
}

function namespaceStatusLabel(status: string) {
  if (!status) return translate("issueDetail.environment.notRequested");
  if (status === "not_requested") return translate("issueDetail.environment.notRequested");
  if (status === "active") return translate("issueDetail.environment.active");
  if (status === "planned") return translate("issueDetail.environment.planned");
  if (["deploying", "preview_unverified", "deploy_failed", "deploy_interrupted", "cleanup_requested", "cleanup_failed", "cleaned", "retained"].includes(status)) return translate(`issueStatus.${status}`);
  return status.replace(/[_-]+/g, " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function environmentHasRetainableNamespace(environment: IssueTestEnvironment | null | undefined) {
  if (!environment) return false;
  return !["cleaned", "cleanup_requested"].includes(environment.namespaceStatus) && environment.cleanupStatus !== "cleaned";
}

function isNoisySystemComment(comment: Comment) {
  if (comment.authorType !== "system") return false;
  return [
    "Issue created and ready",
    "Assigned to agent",
    "Queued local session",
    "Started local session",
    "Session `",
  ].some((prefix) => comment.body.startsWith(prefix));
}

function latestAgentMessage(logs: LogLine[]) {
  return [...logs].reverse().find((log) => log.stream === "agent")?.message || "";
}

const ansiEscapePattern = /\u001b(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])/g;
const sessionFailureNoisePrefixes = [
  "Preparing workspace",
  "Runner starting",
  "Source repository:",
  "Session branch:",
  "Agent profile:",
  "Session context:",
  "Agent instructions:",
  "Starting Codex app-server",
  "Codex app-server ready:",
  "Codex thread:",
  "Codex turn:",
  "Project runbook updated",
  "Collecting Kubernetes evidence",
];

function normalizeSessionLogMessage(message: string) {
  return message.replace(ansiEscapePattern, "").replace(/\s+/g, " ").trim();
}

function latestSessionFailureMessage(logs: LogLine[]) {
  for (const log of [...logs].reverse()) {
    const message = normalizeSessionLogMessage(log.message);
    if (!message || sessionFailureNoisePrefixes.some((prefix) => message.startsWith(prefix))) continue;
    const stream = log.stream.toLowerCase();
    const isErrorish = /record source commit|fatal:|error|failed|unable to|command failed|prepare workspace|write session context|exit status|permission denied|no such file/i.test(
      message,
    );
    if ((stream === "system" || stream.includes("stderr") || stream === "live") && isErrorish) {
      return message.length > 560 ? `${message.slice(0, 557)}...` : message;
    }
  }
  return "";
}

function isHttpUrl(value: string) {
  return /^https?:\/\//i.test(value);
}

function stripLineSuffix(path: string) {
  return path.replace(/:\d+(?::\d+)?$/, "");
}

function missingSummaryTone(status: string) {
  if (status === "failed") return "text-[color:var(--danger)]";
  return "text-[color:var(--muted)]";
}

function resolveLocalLink(href: string, basePath?: string) {
  const value = href.trim();
  if (!value || value.startsWith("#") || isHttpUrl(value) || /^[a-z][a-z0-9+.-]*:/i.test(value) && !value.startsWith("file://")) {
    return "";
  }
  const withoutFileScheme = value.startsWith("file://") ? value.replace(/^file:\/\//, "") : value;
  const decoded = decodeURIComponent(withoutFileScheme);
  if (decoded.startsWith("/")) return stripLineSuffix(decoded);
  if (!basePath) return "";
  return stripLineSuffix(`${basePath.replace(/\/$/, "")}/${decoded.replace(/^\.\//, "")}`);
}

function joinLocalPath(basePath: string, filePath: string) {
  if (!filePath) return "";
  if (filePath.startsWith("/")) return stripLineSuffix(filePath);
  return stripLineSuffix(`${basePath.replace(/\/$/, "")}/${filePath}`);
}

async function openRichLink(href: string, basePath?: string) {
  const localPath = resolveLocalLink(href, basePath);
  if (localPath && window.mspaceDesktop?.openPath) {
    const error = await window.mspaceDesktop.openPath(localPath);
    if (error) console.warn(error);
    return;
  }
  if (isHttpUrl(href) && window.mspaceDesktop?.openExternal) {
    await window.mspaceDesktop.openExternal(href);
    return;
  }
  window.open(href, "_blank", "noopener,noreferrer");
}

function TimeMeta(props: { value: string }) {
  return (
    <InlineMeta icon={Clock3}>
      <span title={formatAbsoluteTime(props.value)}>{formatRelativeTime(props.value)}</span>
    </InlineMeta>
  );
}

type MarkdownNode = {
  type?: string;
  value?: string;
  url?: string;
  title?: string | null;
  children?: MarkdownNode[];
};

function RichText(props: { children: string; basePath?: string; className?: string; agents?: AgentProfile[] }) {
  const text = stringsOrEmpty(props.children);
  const mentionPlugin = useMemo(() => createAgentMentionRemarkPlugin(props.agents || []), [props.agents]);
  if (!text) return null;

  return (
    <div className={cn("rich-text text-[14px] leading-7 text-[color:var(--text)] text-pretty", props.className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm, mentionPlugin]}
        components={{
          a: ({ href = "", children }) => {
            const agentID = agentMentionHrefID(href);
            const agent = agentID ? findAgent(props.agents || [], agentID) : undefined;
            if (agent) {
              return <AgentMentionPill agent={agent} label={plainText(children) || agentMentionText(agent)} />;
            }
            return (
              <a
                href={href}
                className="font-medium text-[color:var(--accent-blue)] underline underline-offset-2 transition-colors hover:text-[color:var(--text)]"
                onClick={(event) => {
                  event.preventDefault();
                  void openRichLink(href, props.basePath);
                }}
              >
                {children}
                {isHttpUrl(href) ? <ExternalLink data-icon className="ml-1 inline-block align-[-2px]" /> : null}
              </a>
            );
          },
          img: MarkdownImage,
          p: ({ children }) => <p className="my-2 first:mt-0 last:mb-0">{children}</p>,
          ul: ({ children }) => <ul className="my-2 list-disc space-y-1 pl-5">{children}</ul>,
          ol: ({ children }) => <ol className="my-2 list-decimal space-y-1 pl-5">{children}</ol>,
          li: ({ children }) => <li className="pl-1">{children}</li>,
          blockquote: ({ children }) => (
            <blockquote className="my-2 rounded-[7px] bg-[color:var(--block)] px-3 py-2 text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]">
              {children}
            </blockquote>
          ),
          code: ({ className, children }) => {
            const isBlock = /language-/.test(className || "");
            if (isBlock) {
              return <code className={className}>{children}</code>;
            }
            const value = plainText(children).trim();
            if (isHttpUrl(value)) {
              return (
                <a
                  href={value}
                  className="font-medium text-[color:var(--accent-blue)] underline underline-offset-2 transition-colors hover:text-[color:var(--text)]"
                  onClick={(event) => {
                    event.preventDefault();
                    void openRichLink(value, props.basePath);
                  }}
                >
                  {children}
                  <ExternalLink data-icon className="ml-1 inline-block align-[-2px]" />
                </a>
              );
            }
            return (
              <code className="rounded-[5px] bg-[color:var(--block)] px-1.5 py-0.5 font-mono text-[12px] text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]">
                {children}
              </code>
            );
          },
          pre: ({ children }) => (
            <pre className="my-3 overflow-auto rounded-[9px] bg-[color:var(--code-bg)] px-4 py-3 font-mono text-[12px] leading-6 text-[color:var(--code-text)]">
              {children}
            </pre>
          ),
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}

function createAgentMentionRemarkPlugin(agents: AgentProfile[]) {
  const mentionLookup = new Map<string, AgentProfile>();
  for (const agent of agents) {
    mentionLookup.set(mentionKey(agent.id), agent);
    mentionLookup.set(mentionKey(agent.mention), agent);
  }
  return function agentMentionRemarkPlugin() {
    return function transformAgentMentions(tree: MarkdownNode) {
      transformMentionTextNodes(tree, mentionLookup);
    };
  };
}

function transformMentionTextNodes(node: MarkdownNode | undefined, mentionLookup: Map<string, AgentProfile>) {
  if (!node) return;
  if (mentionLookup.size === 0 || !node.children || node.type === "link") return;
  node.children = node.children.flatMap((child) => {
    if (child.type !== "text" || typeof child.value !== "string") {
      transformMentionTextNodes(child, mentionLookup);
      return [child];
    }
    return splitAgentMentionText(child.value, mentionLookup);
  });
}

function splitAgentMentionText(value: string, mentionLookup: Map<string, AgentProfile>): MarkdownNode[] {
  const pieces: MarkdownNode[] = [];
  const mentionPattern = /(^|[^\w])@([a-z][\w-]*)/gi;
  let cursor = 0;
  for (let match = mentionPattern.exec(value); match; match = mentionPattern.exec(value)) {
    const prefix = match[1] || "";
    const key = mentionKey(match[2] || "");
    const agent = mentionLookup.get(key);
    if (!agent || match.index === undefined) continue;

    const mentionStart = match.index + prefix.length;
    const mentionText = `@${match[2]}`;
    if (mentionStart > cursor) {
      pieces.push({ type: "text", value: value.slice(cursor, mentionStart) });
    }
    pieces.push({
      type: "link",
      url: `#mspace-agent:${encodeURIComponent(agent.id)}`,
      title: null,
      children: [{ type: "text", value: mentionText }],
    });
    cursor = mentionStart + mentionText.length;
  }
  if (cursor === 0) return [{ type: "text", value }];
  if (cursor < value.length) {
    pieces.push({ type: "text", value: value.slice(cursor) });
  }
  return pieces;
}

function agentMentionHrefID(href: string) {
  const prefix = "#mspace-agent:";
  if (!href.startsWith(prefix)) return "";
  return decodeURIComponent(href.slice(prefix.length));
}

function plainText(value: React.ReactNode): string {
  if (typeof value === "string" || typeof value === "number") return String(value);
  if (Array.isArray(value)) return value.map(plainText).join("");
  return "";
}

function AgentMentionPill(props: { agent: AgentProfile; label: string }) {
  const isCodex = props.agent.provider === "codex" || mentionKey(props.agent.id) === "codex";
  return (
    <span
      title={props.agent.name}
      className="mx-0.5 inline-flex h-6 max-w-full items-center gap-1.5 rounded-full bg-[color:var(--block)] px-1.5 pr-2 align-middle text-[12px] font-semibold leading-none text-[color:var(--text)]"
    >
      <span className="grid size-4 shrink-0 place-items-center overflow-hidden rounded-full bg-[color:var(--paper)] text-[color:var(--accent-blue)] shadow-[inset_0_0_0_1px_var(--line)]">
        {isCodex ? <img src={codexAvatarDataUrl} alt="" className="size-full p-0.5" /> : <Bot data-icon className="size-3" />}
      </span>
      <span className="truncate">{props.label}</span>
    </span>
  );
}

function stringsOrEmpty(value: string) {
  return typeof value === "string" ? value.trim() : "";
}

function MarkdownImage(props: { src?: string; alt?: string }) {
  const image = useResolvedIssueImageSrc(String(props.src || ""));
  if (!image.src) {
    return (
      <span className="my-3 block rounded-[8px] bg-[color:var(--block)] px-3 py-2 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
        {image.loading ? "Loading image" : "Image unavailable"}
      </span>
    );
  }
  return (
    <img
      src={image.src}
      alt={props.alt || ""}
      className="my-3 max-h-[520px] max-w-full rounded-[8px] object-contain shadow-[0_0_0_1px_var(--line)]"
    />
  );
}

function splitColumns(line: string) {
  return line.trim().split(/\s{2,}/).filter(Boolean);
}

function normalizeResourceKind(value: string) {
  const kind = value.split("/")[0] || "resource";
  return kind
    .replace(/\.apps$/, "")
    .replace(/^pods?$/, "pod")
    .replace(/^deployments?$/, "deployment")
    .replace(/^services?$/, "service")
    .replace(/^ingresses?$/, "ingress");
}

function resourceName(value: string) {
  return value.includes("/") ? value.slice(value.indexOf("/") + 1) : value;
}

function resourceKindLabel(kind: string) {
  const normalized = normalizeResourceKind(kind);
  if (normalized === "pod") return translate("issueDetail.resources.pods");
  if (normalized === "deployment") return translate("issueDetail.resources.deployments");
  if (normalized === "service") return translate("issueDetail.resources.services");
  if (normalized === "ingress") return translate("issueDetail.resources.ingresses");
  return `${normalized.charAt(0).toUpperCase()}${normalized.slice(1)}s`;
}

function resourceOrder(kind: string) {
  const normalized = normalizeResourceKind(kind);
  if (normalized === "pod") return 0;
  if (normalized === "deployment") return 1;
  if (normalized === "service") return 2;
  if (normalized === "ingress") return 3;
  return 10;
}

function readyLooksHealthy(value: string) {
  const match = value.match(/^(\d+)\/(\d+)$/);
  return !match || match[1] === match[2];
}

function inferResourceTone(fields: Array<{ label: string; value: string }>, primaryLabel: string, primaryValue: string): EvidenceTone {
  const status = primaryLabel === "STATUS" ? primaryValue.toLowerCase() : "";
  const ready = fields.find((field) => field.label === "READY")?.value || "";
  const restarts = Number(fields.find((field) => field.label === "RESTARTS")?.value || "0");

  if (/failed|error|crash|imagepull|backoff|evicted/.test(status)) return "failed";
  if (/pending|terminating|unknown|containercreating/.test(status)) return "warning";
  if (Number.isFinite(restarts) && restarts > 0) return "warning";
  if (ready && !readyLooksHealthy(ready)) return "warning";
  if (status === "running" || status === "succeeded" || primaryLabel === "READY") return "healthy";
  return "collected";
}

function parseResourceTables(text: string) {
  const resources: EvidenceResource[] = [];
  const blocks = text.split(/\n\s*\n/).map((block) => block.trim()).filter(Boolean);

  for (const block of blocks) {
    const lines = block.split("\n").map((line) => line.trimEnd()).filter(Boolean);
    if (lines.length < 2 || !lines[0].trimStart().startsWith("NAME")) continue;

    const headers = splitColumns(lines[0]);
    for (const rowLine of lines.slice(1)) {
      const columns = splitColumns(rowLine);
      if (columns.length < 2) continue;

      const nameValue = columns[0] || "";
      const fields = headers.map((label, index) => ({ label, value: columns[index] || "" })).filter((field) => field.label && field.value);
      const primaryLabel =
        fields.find((field) => field.label === "STATUS")?.label ||
        fields.find((field) => field.label === "TYPE")?.label ||
        fields.find((field) => field.label === "READY")?.label ||
        fields[1]?.label ||
        "STATE";
      const primaryValue = fields.find((field) => field.label === primaryLabel)?.value || "";
      const kind = normalizeResourceKind(nameValue);

      resources.push({
        kind,
        name: resourceName(nameValue),
        primaryLabel,
        primaryValue,
        fields: fields.filter((field) => field.label !== "NAME" && field.label !== primaryLabel),
        tone: inferResourceTone(fields, primaryLabel, primaryValue),
      });
    }
  }

  return resources.sort((a, b) => resourceOrder(a.kind) - resourceOrder(b.kind) || a.name.localeCompare(b.name));
}

function parseEvidenceEvents(text: string) {
  const lines = text.split("\n").map((line) => line.trim()).filter(Boolean);
  const events: EvidenceEvent[] = [];

  for (const line of lines) {
    if (/^LAST\s+SEEN\s+TYPE\s+REASON\s+OBJECT\s+MESSAGE/.test(line)) continue;
    const match = line.match(/^(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(.+)$/);
    if (match) {
      events.push({
        lastSeen: match[1],
        type: match[2],
        reason: match[3],
        object: match[4],
        message: match[5],
      });
    } else {
      events.push({ lastSeen: "", type: "Info", reason: "Output", object: "", message: line });
    }
  }

  return events;
}

function parseEvidenceDetails(evidence: DeploymentEvidence): ParsedEvidence {
  const details = stringsOrEmpty(evidence.details);
  const [resourceText = "", eventText = ""] = details.split(/\n\s*--- events ---\s*\n/);
  const resources = parseResourceTables(resourceText);
  const events = parseEvidenceEvents(eventText);
  const summary = evidence.summary.toLowerCase();
  const failed = summary.includes("failed") || summary.includes("could not") || summary.includes("interrupted");
  const warning = summary.includes("no preview") || summary.includes("unverified");
  const hasWarningEvent = events.some((event) => event.type.toLowerCase() === "warning");
  const hasFailedResource = resources.some((resource) => resource.tone === "failed");
  const hasWarningResource = resources.some((resource) => resource.tone === "warning");

  return {
    resources,
    events,
    tone:
      failed || hasFailedResource
        ? "failed"
        : warning || hasWarningEvent || hasWarningResource
          ? "warning"
          : resources.length > 0
            ? "healthy"
            : "collected",
  };
}

function EvidenceStatusPill(props: { tone: EvidenceTone }) {
  const label =
    props.tone === "healthy"
      ? translate("issueDetail.evidence.healthy")
      : props.tone === "warning"
        ? translate("issueDetail.evidence.needsAttention")
        : props.tone === "failed"
          ? translate("issueDetail.evidence.collectionFailed")
          : translate("issueDetail.evidence.collected");
  const Icon = props.tone === "failed" || props.tone === "warning" ? CircleAlert : props.tone === "healthy" ? CheckCircle2 : CircleDot;
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1.5 rounded-full px-2 py-0.5 text-[12px] font-medium leading-5",
        props.tone === "healthy" && "bg-[color:var(--success-soft)] text-[color:var(--success)]",
        props.tone === "warning" && "bg-[color:var(--warning-soft)] text-[color:var(--warning)]",
        props.tone === "failed" && "bg-[color:var(--danger-soft)] text-[color:var(--danger)]",
        props.tone === "collected" && "bg-[color:var(--block)] text-[color:var(--muted-strong)]",
      )}
    >
      <Icon data-icon />
      {label}
    </span>
  );
}

function EvidenceMeta(props: { label: string; value: string }) {
  if (!props.value) return null;
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5">
      <span className="shrink-0 text-[color:var(--faint)]">{props.label}</span>
      <span className="min-w-0 truncate font-mono text-[11px] text-[color:var(--muted-strong)]">{props.value}</span>
    </span>
  );
}

function groupResources(resources: EvidenceResource[]) {
  const groups = new Map<string, EvidenceResource[]>();
  for (const resource of resources) {
    const key = normalizeResourceKind(resource.kind);
    groups.set(key, [...(groups.get(key) || []), resource]);
  }
  return Array.from(groups.entries()).sort(([a], [b]) => resourceOrder(a) - resourceOrder(b));
}

function ResourceStatePill(props: { resource: EvidenceResource }) {
  return (
    <span
      className={cn(
        "inline-flex w-fit max-w-full items-center rounded-full px-2 py-0.5 font-mono text-[11px] font-medium leading-5",
        props.resource.tone === "healthy" && "bg-[color:var(--success-soft)] text-[color:var(--success)]",
        props.resource.tone === "warning" && "bg-[color:var(--warning-soft)] text-[color:var(--warning)]",
        props.resource.tone === "failed" && "bg-[color:var(--danger-soft)] text-[color:var(--danger)]",
        props.resource.tone === "collected" && "bg-[color:var(--block)] text-[color:var(--muted-strong)]",
      )}
      title={props.resource.primaryLabel}
    >
      {props.resource.primaryValue || translate("common.unknown")}
    </span>
  );
}

function EvidenceResourceGroups(props: { resources: EvidenceResource[] }) {
  if (props.resources.length === 0) return null;
  return (
    <div className="mt-3 grid gap-2">
      {groupResources(props.resources).map(([kind, resources]) => (
        <section key={kind} className="overflow-hidden rounded-[8px] bg-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--line)]">
          <div className="flex items-center justify-between gap-3 px-3 py-2">
            <div className="text-[12px] font-semibold leading-5 text-[color:var(--muted-strong)]">{resourceKindLabel(kind)}</div>
            <div className="font-mono text-[11px] tabular-nums text-[color:var(--faint)]">{resources.length}</div>
          </div>
          <div className="divide-y divide-[color:var(--line)]">
            {resources.map((resource) => (
              <div key={`${resource.kind}-${resource.name}`} className="grid gap-2 px-3 py-2 md:grid-cols-[minmax(140px,1fr)_auto_minmax(220px,1.4fr)] md:items-center">
                <div className="min-w-0 truncate font-mono text-[12px] leading-5 text-[color:var(--text)]" title={resource.name}>
                  {resource.name}
                </div>
                <ResourceStatePill resource={resource} />
                <div className="flex min-w-0 flex-wrap gap-x-3 gap-y-1 text-[12px] leading-5 text-[color:var(--muted)]">
                  {resource.fields.slice(0, 4).map((field) => (
                    <span key={`${resource.name}-${field.label}`} className="inline-flex items-center gap-1.5">
                      <span className="text-[color:var(--faint)]">{field.label}</span>
                      <span className="font-mono text-[11px] text-[color:var(--muted-strong)]">{field.value}</span>
                    </span>
                  ))}
                </div>
              </div>
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}

function EvidenceEvents(props: { events: EvidenceEvent[] }) {
  if (props.events.length === 0) return null;
  const visibleEvents = props.events.slice(-8).reverse();
  return (
    <section className="mt-4">
      <div className="mb-2 flex items-center justify-between gap-3">
        <div className="text-[12px] font-semibold leading-5 text-[color:var(--muted-strong)]">{translate("issueDetail.evidence.recentEvents")}</div>
        {props.events.length > visibleEvents.length ? (
          <div className="text-[12px] leading-5 text-[color:var(--faint)]">{translate("issueDetail.evidence.older", { count: props.events.length - visibleEvents.length })}</div>
        ) : null}
      </div>
      <div className="overflow-hidden rounded-[8px] bg-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--line)]">
        {visibleEvents.map((event, index) => {
          const warning = event.type.toLowerCase() === "warning";
          return (
            <div key={`${event.lastSeen}-${event.reason}-${event.object}-${index}`} className="grid gap-1 px-3 py-2 text-[12px] leading-5 text-[color:var(--muted)] md:grid-cols-[72px_112px_minmax(0,1fr)] md:gap-3">
              <div className="font-mono text-[11px] tabular-nums text-[color:var(--faint)]">{event.lastSeen || translate("issueDetail.evidence.event")}</div>
              <div className={cn("inline-flex min-w-0 items-center gap-1.5 font-medium", warning ? "text-[color:var(--warning)]" : "text-[color:var(--muted-strong)]")}>
                {warning ? <CircleAlert data-icon /> : <CircleDot data-icon />}
                <span className="truncate">{event.reason || event.type}</span>
              </div>
              <div className="min-w-0">
                {event.object ? <span className="mr-2 font-mono text-[11px] text-[color:var(--muted-strong)]">{event.object}</span> : null}
                <span>{event.message}</span>
              </div>
            </div>
          );
        })}
      </div>
    </section>
  );
}

function SessionStatusMark(props: { status: string }) {
  if (props.status === "completed") {
    return (
      <span
        aria-label={translate("issueDetail.timeline.completed")}
        title={translate("issueDetail.timeline.completed")}
        className="grid size-7 shrink-0 place-items-center rounded-full bg-[color:var(--success-soft)] text-[color:var(--success)]"
      >
        <CheckCircle2 data-icon />
      </span>
    );
  }
  return <StatusBadge value={props.status} />;
}

function WorkingSessionLine(props: { status: string; agentName: string; runtimeMode?: string; agentStatus?: string; runtimeTaskId?: string }) {
  const team = props.runtimeMode === "team";
  const status = (props.agentStatus || props.status).trim().toLowerCase();
  const label = team
    ? status === "team-runtime-queued" || props.status === "queued"
      ? translate("issueDetail.timeline.waitingForTeamWorker")
      : status === "team-runtime-claimed"
        ? translate("issueDetail.timeline.claimedByTeamWorker")
        : translate("issueDetail.timeline.runningOnTeamWorker")
    : props.status === "queued"
      ? translate("issueDetail.timeline.waitingForPersonalWorker")
      : status === "personal-runtime-claimed"
        ? translate("issueDetail.timeline.claimedByPersonalWorker")
        : translate("issueDetail.timeline.runningOnPersonalWorker");
  return (
    <div className="inline-flex min-w-0 items-center gap-2 text-[13px] leading-6 text-[color:var(--muted)]">
      <span className="relative flex size-2 shrink-0">
        <span className="absolute inline-flex size-full rounded-full bg-[color:var(--accent-blue)] opacity-25 motion-safe:animate-ping" />
        <span className="relative inline-flex size-2 rounded-full bg-[color:var(--accent-blue)]" />
      </span>
      <span className="truncate">
        <span className="font-medium text-[color:var(--muted-strong)]">{props.agentName}</span> {label}
        {props.runtimeTaskId ? <span className="ml-2 font-mono text-[12px] text-[color:var(--faint)]">{props.runtimeTaskId.slice(0, 8)}</span> : null}
      </span>
    </div>
  );
}

function StopSessionButton(props: { isStopping?: boolean; onStop: () => void }) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      className="h-7 min-h-0 shrink-0 px-1.5 text-[12px] text-[color:var(--danger)] hover:bg-[color:var(--danger-soft)] hover:text-[color:var(--danger)]"
      disabled={props.isStopping}
      onClick={props.onStop}
    >
      <CircleStop data-icon />
      {props.isStopping ? translate("issueDetail.timeline.stopping") : translate("issueDetail.timeline.stop")}
    </Button>
  );
}

function SessionFileChanges(props: { changes: WorkspaceChange[]; workdir: string }) {
  const visibleChanges = visibleWorkspaceFileChanges(props.changes);
  const changes = visibleChanges.slice(0, 6);
  if (changes.length === 0 || !props.workdir) return null;

  return (
    <div className="mt-3 flex flex-wrap items-center gap-1.5">
      {changes.map((change) => {
        const targetPath = joinLocalPath(props.workdir, change.path);
        return (
          <button
            key={`${change.statusCode}-${change.path}-${change.previousPath}`}
            type="button"
            className="inline-flex max-w-full items-center gap-1.5 rounded-[7px] bg-[color:var(--paper)] px-2 py-1 text-[12px] leading-5 text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)] transition-[background-color,color] hover:bg-[color:var(--hover)] hover:text-[color:var(--text)]"
            onClick={() => {
              void window.mspaceDesktop?.openPath?.(targetPath);
            }}
            title={targetPath}
          >
            <span
              className={cn(
                "shrink-0 font-mono text-[11px] font-semibold leading-5",
                workspaceChangeStatusTone(change.statusCode),
              )}
            >
              {workspaceChangeStatusLabel(change.statusCode)}
            </span>
            <FileTypeIcon path={change.path} />
            <span className="min-w-0 truncate">{change.path}</span>
          </button>
        );
      })}
      {visibleChanges.length > changes.length ? (
        <span className="text-[12px] leading-5 text-[color:var(--muted)]">{translate("issueDetail.timeline.moreFiles", { count: visibleChanges.length - changes.length })}</span>
      ) : null}
    </div>
  );
}

function changeNodeSession(node: IssueChangeNode, sessions: AgentSession[]) {
  return sessions.find((session) => session.id === node.sessionId);
}

function sourceNodeLabel(node: IssueChangeNode) {
  const commit = node.shortCommitSha || node.commitSha.slice(0, 12) || "commit";
  const subject = node.subject ? ` · ${node.subject}` : "";
  return `${commit}${subject}`;
}

function IssueSubTabs(props: {
  active: IssueTab;
  onChange: (tab: IssueTab) => void;
}) {
  const { t } = useMspaceTranslation();
  const tabs: Array<{ value: IssueTab; label: string; icon: typeof CircleDot }> = [
    { value: "overview", label: t("issueDetail.tabs.overview"), icon: CircleDot },
    { value: "commits", label: t("issueDetail.tabs.commits"), icon: GitCommit },
    { value: "sessions", label: t("issueDetail.tabs.sessions"), icon: History },
    { value: "resources", label: t("issueDetail.tabs.resources"), icon: Boxes },
    { value: "evidence", label: t("issueDetail.tabs.evidence"), icon: CheckCircle2 },
  ];

  return (
    <div className="mb-7 flex min-w-0 flex-wrap gap-1 border-b border-[color:var(--line)] pb-2" role="tablist" aria-label={t("issueDetail.tabs.sections")}>
      {tabs.map((tab) => {
        const Icon = tab.icon;
        const active = props.active === tab.value;
        return (
          <button
            key={tab.value}
            type="button"
            role="tab"
            aria-selected={active}
            className={cn(
              "inline-flex h-8 max-w-full items-center gap-1.5 rounded-[7px] px-2.5 text-[13px] font-medium leading-5 transition-[background-color,color,transform] duration-150 ease-out active:scale-95",
              active
                ? "bg-[color:var(--selection)] text-[color:var(--text)]"
                : "text-[color:var(--muted)] hover:bg-[color:var(--hover)] hover:text-[color:var(--muted-strong)]",
            )}
            onClick={() => props.onChange(tab.value)}
          >
            <Icon data-icon className="shrink-0" />
            <span className="truncate">{tab.label}</span>
          </button>
        );
      })}
    </div>
  );
}

function ChangeNodeFileList(props: { changes: WorkspaceChange[]; workdir: string }) {
  const visibleChanges = visibleWorkspaceFileChanges(props.changes);
  if (visibleChanges.length === 0) {
    return <div className="text-[13px] leading-6 text-[color:var(--muted)]">{translate("issueDetail.commits.noChangedFiles")}</div>;
  }
  return (
    <div className="grid gap-1.5">
      {visibleChanges.map((change) => {
        const targetPath = props.workdir ? joinLocalPath(props.workdir, change.path) : "";
        return (
          <button
            key={`${change.statusCode}-${change.previousPath}-${change.path}`}
            type="button"
            className="grid min-h-9 grid-cols-[42px_18px_minmax(0,1fr)] items-center gap-2 rounded-[7px] px-2 text-left text-[12px] leading-5 transition-[background-color,color] hover:bg-[color:var(--hover)]"
            disabled={!targetPath}
            onClick={() => {
              if (targetPath) void window.mspaceDesktop?.openPath?.(targetPath);
            }}
            title={targetPath || change.path}
          >
            <span className={cn("font-mono text-[11px] font-semibold", workspaceChangeStatusTone(change.statusCode))}>
              {workspaceChangeStatusLabel(change.statusCode)}
            </span>
            <FileTypeIcon path={change.path} />
            <span className="min-w-0 truncate text-[color:var(--muted-strong)]">
              {change.previousPath ? `${change.previousPath} -> ${change.path}` : change.path}
            </span>
          </button>
        );
      })}
    </div>
  );
}

type DiffRowKind = "add" | "remove" | "context" | "hunk" | "gap" | "meta";

type ParsedDiffRow = {
  kind: DiffRowKind;
  content: string;
  oldLine?: number;
  newLine?: number;
};

type ParsedDiffFile = {
  oldPath: string;
  newPath: string;
  displayPath: string;
  additions: number;
  deletions: number;
  rows: ParsedDiffRow[];
};

function cleanDiffPath(value: string) {
  const trimmed = value.trim().replace(/^"|"$/g, "");
  if (trimmed === "/dev/null") return trimmed;
  if (trimmed.startsWith("a/") || trimmed.startsWith("b/")) return trimmed.slice(2);
  return trimmed;
}

function parseDiffGitLine(line: string) {
  const match = line.match(/^diff --git a\/(.+) b\/(.+)$/);
  if (!match) return undefined;
  return { oldPath: match[1], newPath: match[2] };
}

function displayDiffPath(file: ParsedDiffFile) {
  if (file.newPath && file.newPath !== "/dev/null") return file.newPath;
  if (file.oldPath && file.oldPath !== "/dev/null") return file.oldPath;
  return translate("issueDetail.commits.changedFile");
}

function parseUnifiedDiffPreview(text: string) {
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  if (lines[lines.length - 1] === "") lines.pop();

  const leadRows: string[] = [];
  const files: ParsedDiffFile[] = [];
  let currentFile: ParsedDiffFile | null = null;
  let oldLine = 0;
  let newLine = 0;
  let inHunk = false;

  const pushCurrentFile = () => {
    if (!currentFile) return;
    currentFile.displayPath = displayDiffPath(currentFile);
    files.push(currentFile);
    currentFile = null;
    oldLine = 0;
    newLine = 0;
    inHunk = false;
  };

  for (const line of lines) {
    const diffPaths = parseDiffGitLine(line);
    if (diffPaths) {
      pushCurrentFile();
      currentFile = {
        oldPath: cleanDiffPath(diffPaths.oldPath),
        newPath: cleanDiffPath(diffPaths.newPath),
        displayPath: cleanDiffPath(diffPaths.newPath),
        additions: 0,
        deletions: 0,
        rows: [],
      };
      continue;
    }

    if (!currentFile) {
      if (line.trim()) leadRows.push(line);
      continue;
    }

    if (!inHunk && line.startsWith("--- ")) {
      currentFile.oldPath = cleanDiffPath(line.slice(4));
      continue;
    }
    if (!inHunk && line.startsWith("+++ ")) {
      currentFile.newPath = cleanDiffPath(line.slice(4));
      currentFile.displayPath = displayDiffPath(currentFile);
      continue;
    }

    const hunkMatch = line.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)$/);
    if (hunkMatch) {
      const nextOldLine = Number(hunkMatch[1]);
      const nextNewLine = Number(hunkMatch[2]);
      const gap = inHunk && oldLine > 0 && newLine > 0 ? Math.max(nextOldLine - oldLine, nextNewLine - newLine) : 0;
      if (gap > 1) currentFile.rows.push({ kind: "gap", content: `${gap} unchanged lines` });
      currentFile.rows.push({ kind: "hunk", content: line });
      oldLine = nextOldLine;
      newLine = nextNewLine;
      inHunk = true;
      continue;
    }

    if (inHunk && line.startsWith("+")) {
      currentFile.rows.push({ kind: "add", content: line.slice(1), newLine });
      currentFile.additions += 1;
      newLine += 1;
      continue;
    }
    if (inHunk && line.startsWith("-")) {
      currentFile.rows.push({ kind: "remove", content: line.slice(1), oldLine });
      currentFile.deletions += 1;
      oldLine += 1;
      continue;
    }
    if (inHunk && line.startsWith(" ")) {
      currentFile.rows.push({ kind: "context", content: line.slice(1), oldLine, newLine });
      oldLine += 1;
      newLine += 1;
      continue;
    }
    if (inHunk && line === "") {
      currentFile.rows.push({ kind: "context", content: "", oldLine, newLine });
      oldLine += 1;
      newLine += 1;
      continue;
    }
    if (line.trim()) currentFile.rows.push({ kind: "meta", content: line });
  }

  pushCurrentFile();

  const fallbackRows: ParsedDiffRow[] = leadRows.map((line) => ({ kind: "meta", content: line }));
  return { leadRows, files, fallbackRows };
}

function diffFileDomId(prefix: string, path: string) {
  return `${prefix}-${path.replace(/[^a-zA-Z0-9_-]+/g, "-")}`;
}

function DiffPreview(props: { text: string; truncated: boolean; fileIdPrefix?: string; constrained?: boolean }) {
  const parsed = useMemo(() => parseUnifiedDiffPreview(props.text), [props.text]);
  const files = parsed.files;
  const rows = files.length > 0 ? undefined : parsed.fallbackRows;

  return (
    <div className="overflow-hidden rounded-[9px] bg-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--line)]">
      {props.truncated ? (
        <div className="border-b border-[color:var(--line)] bg-[color:var(--warning-soft)] px-3 py-2 text-[12px] leading-5 text-[color:var(--warning)]">
          {translate("issueDetail.commits.diffPreviewTruncated")}
        </div>
      ) : null}
      <div className={cn("overflow-auto", props.constrained === false ? "" : "max-h-[680px]")}>
        {files.length > 0 ? (
          files.map((file) => <DiffFileSection key={`${file.oldPath}->${file.newPath}`} file={file} fileIdPrefix={props.fileIdPrefix} />)
        ) : (
          <DiffFallback rows={rows || []} />
        )}
      </div>
    </div>
  );
}

function DiffFallback(props: { rows: ParsedDiffRow[] }) {
  if (props.rows.length === 0) {
    return <div className="px-3 py-2 text-[13px] leading-6 text-[color:var(--muted)]">{translate("issueDetail.commits.noDiffRows")}</div>;
  }
  return (
    <div className="grid min-w-[720px] gap-0 py-1 font-mono text-[12px] leading-5">
      {props.rows.map((row, index) => (
        <div key={`${index}-${row.content}`} className="px-3 py-0.5 text-[color:var(--muted-strong)]">
          <span className="whitespace-pre">{row.content || " "}</span>
        </div>
      ))}
    </div>
  );
}

function DiffFileSection(props: { file: ParsedDiffFile; fileIdPrefix?: string }) {
  const file = props.file;
  return (
    <section
      id={props.fileIdPrefix ? diffFileDomId(props.fileIdPrefix, file.displayPath) : undefined}
      className="scroll-mt-6 border-b border-[color:var(--line)] last:border-b-0"
    >
      <div className="sticky top-0 z-10 flex min-w-[720px] items-center justify-between gap-3 border-b border-[color:var(--line)] bg-[color:var(--paper)] px-3 py-2">
        <div className="flex min-w-0 items-center gap-2">
          <FileTypeIcon path={file.displayPath} />
          <span className="min-w-0 truncate font-mono text-[12px] font-semibold text-[color:var(--text)]">{file.displayPath}</span>
        </div>
        <div className="flex shrink-0 items-center gap-2 font-mono text-[12px] leading-5">
          <span className="text-[color:var(--success)]">+{file.additions}</span>
          <span className="text-[color:var(--danger)]">-{file.deletions}</span>
        </div>
      </div>
      <table className="w-full min-w-[720px] border-collapse font-mono text-[12px] leading-5">
        <tbody>
          {file.rows.map((row, index) => (
            <DiffLineRow key={`${index}-${row.kind}-${row.oldLine || ""}-${row.newLine || ""}`} row={row} />
          ))}
        </tbody>
      </table>
    </section>
  );
}

function DiffLineRow(props: { row: ParsedDiffRow }) {
  const row = props.row;
  if (row.kind === "hunk" || row.kind === "gap" || row.kind === "meta") {
    return (
      <tr
        className={cn(
          "border-y border-[color:var(--line)]",
          row.kind === "hunk" && "bg-[color:var(--blue-soft)] text-[color:var(--accent-blue)]",
          row.kind === "gap" && "bg-[color:var(--block)] text-[color:var(--muted)]",
          row.kind === "meta" && "bg-[color:var(--block)] text-[color:var(--faint)]",
        )}
      >
        <td className="w-[52px] select-none border-r border-[color:var(--line)] px-2 py-1 text-right tabular-nums" />
        <td className="w-[52px] select-none border-r border-[color:var(--line)] px-2 py-1 text-right tabular-nums" />
        <td className="px-3 py-1">
          <span className="whitespace-pre">{row.content || " "}</span>
        </td>
      </tr>
    );
  }

  const sign = row.kind === "add" ? "+" : row.kind === "remove" ? "-" : " ";
  const rowTone =
    row.kind === "add"
      ? "bg-[color:var(--success-soft)]"
      : row.kind === "remove"
        ? "bg-[color:var(--danger-soft)]"
        : "bg-[color:var(--paper)]";
  const numberTone = row.kind === "add" ? "text-[color:var(--success)]" : row.kind === "remove" ? "text-[color:var(--danger)]" : "text-[color:var(--faint)]";
  const codeTone = row.kind === "add" ? "text-[color:var(--success)]" : row.kind === "remove" ? "text-[color:var(--danger)]" : "text-[color:var(--muted-strong)]";

  return (
    <tr className={rowTone}>
      <td className={cn("w-[52px] select-none border-r border-[color:var(--line)] px-2 py-0.5 text-right tabular-nums", numberTone)}>
        {row.oldLine || ""}
      </td>
      <td className={cn("w-[52px] select-none border-r border-[color:var(--line)] px-2 py-0.5 text-right tabular-nums", numberTone)}>
        {row.newLine || ""}
      </td>
      <td className={cn("px-3 py-0.5", codeTone)}>
        <span className="inline-block w-4 select-none text-right">{sign}</span>
        <span className="whitespace-pre">{row.content || " "}</span>
      </td>
    </tr>
  );
}

function handoffMatchesNode(handoff: IssueHandoff, node: IssueChangeNode) {
  return (
    handoff.sourceCommitSha === node.commitSha ||
    Boolean(handoff.sourceCommitSha && node.commitSha.startsWith(handoff.sourceCommitSha)) ||
    Boolean(handoff.sourceSessionId && handoff.sourceSessionId === node.sessionId) ||
    Boolean(handoff.branch && handoff.branch === node.branch)
  );
}

function handoffTitle(handoff: IssueHandoff) {
  if (handoff.prNumber > 0) return `PR #${handoff.prNumber}`;
  if (handoff.prUrl) return translate("issueDetail.handoff.pullRequest");
  return translate("issueDetail.handoff.branch");
}

function handoffStateLabel(handoff: IssueHandoff) {
  if (handoff.error) return translate("issueDetail.handoff.needsAttention");
  if (handoff.prState) return handoff.prState.replace(/[_-]+/g, " ").toLowerCase();
  if (handoff.prUrl) return translate("issueDetail.handoff.synced");
  return translate("issueDetail.handoff.branch");
}

function HandoffStatusPill(props: { handoff: IssueHandoff }) {
  const state = (props.handoff.error ? "error" : props.handoff.prState || props.handoff.kind).toLowerCase();
  const tone =
    state === "open" || state === "draft"
      ? "bg-[color:var(--blue-soft)] text-[color:var(--accent-blue)]"
      : state === "merged"
        ? "bg-[color:var(--success-soft)] text-[color:var(--success)]"
        : state === "closed" || state === "error"
          ? "bg-[color:var(--danger-soft)] text-[color:var(--danger)]"
          : "bg-[color:var(--block)] text-[color:var(--muted-strong)]";
  return <span className={cn("inline-flex w-fit rounded-full px-2 py-0.5 text-[11px] font-medium leading-4 capitalize", tone)}>{handoffStateLabel(props.handoff)}</span>;
}

function issuePullRequestHandoff(handoffs: IssueHandoff[]) {
  return handoffs.find((handoff) => handoff.kind === "pr" && handoff.prUrl) || handoffs.find((handoff) => handoff.prUrl);
}

function changeNodeSourceLabel(node: IssueChangeNode) {
  return node.branch || translate("issueDetail.handoff.noBranchCaptured");
}

function prSourceBranchNodes(changeNodes: IssueChangeNode[]) {
  const nodes: IssueChangeNode[] = [];
  const seen = new Set<string>();
  for (const node of changeNodes) {
    const branch = node.branch.trim();
    if (!branch || node.error || seen.has(branch)) continue;
    seen.add(branch);
    nodes.push(node);
  }
  return nodes;
}

function HandoffMeta(props: { label: string; value: string; mono?: boolean; title?: string }) {
  return (
    <div className="min-w-0">
      <div className="text-[11px] leading-4 text-[color:var(--faint)]">{props.label}</div>
      <div
        className={cn(
          "mt-0.5 min-w-0 truncate text-[12px] leading-5 text-[color:var(--muted-strong)]",
          props.mono && "font-mono tabular-nums",
        )}
        title={props.title || props.value}
      >
        {props.value || translate("issueDetail.handoff.notRecorded")}
      </div>
    </div>
  );
}

function IssueHandoffPanel(props: {
  changeNodes: IssueChangeNode[];
  handoffs: IssueHandoff[];
  disabled?: boolean;
  disabledReason?: string;
  isCreatingPr: boolean;
  refreshingHandoffId: string;
  createError: Error | null;
  refreshError: Error | null;
  onCreatePr: (node: IssueChangeNode) => void;
  onRefresh: (handoff: IssueHandoff) => void;
}) {
  const { t } = useMspaceTranslation();
  const nodes = useMemo(() => prSourceBranchNodes(props.changeNodes), [props.changeNodes]);
  const [sourceBranch, setSourceBranch] = useState(nodes[0]?.branch || "");
  useEffect(() => {
    if (nodes.length === 0) {
      setSourceBranch("");
      return;
    }
    if (!nodes.some((node) => node.branch === sourceBranch)) {
      setSourceBranch(nodes[0].branch);
    }
  }, [nodes, sourceBranch]);
  const selectedNode = nodes.find((node) => node.branch === sourceBranch) || nodes[0];
  const syncedPR = issuePullRequestHandoff(props.handoffs);
  const primaryHandoff = syncedPR || props.handoffs[0];
  const canCreate = Boolean(selectedNode?.branch) && !syncedPR?.prUrl && !props.isCreatingPr && !props.disabled;
  const displayBranch = primaryHandoff?.branch || selectedNode?.branch || translate("issueDetail.handoff.noBranchCaptured");
  const sourceCommitValue =
    primaryHandoff?.headCommitSha ||
    primaryHandoff?.sourceCommitSha ||
    selectedNode?.commitSha ||
    "";
  const commitCount = primaryHandoff?.commits.length || (selectedNode ? 1 : 0);
  const checkedAt = primaryHandoff?.lastCheckedAt || primaryHandoff?.updatedAt || "";
  return (
    <div className="grid gap-4 rounded-[10px] bg-[color:var(--paper)] p-4 shadow-[inset_0_0_0_1px_var(--line)]">
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-start gap-2">
          <span className="mt-0.5 grid size-7 shrink-0 place-items-center rounded-[7px] bg-[color:var(--block)] text-[color:var(--muted-strong)]">
            <GitPullRequest data-icon />
          </span>
          <div className="flex min-w-0 items-center gap-2 text-[13px] font-semibold leading-5 text-[color:var(--text)]">
            <div>
              <div>{t("issueDetail.handoff.pullRequest")}</div>
              <div className="mt-0.5 text-[12px] font-normal leading-5 text-[color:var(--muted)]">
                {props.disabledReason || t("issueDetail.handoff.description")}
              </div>
            </div>
          </div>
        </div>
        {syncedPR?.prUrl ? (
          <div className="flex shrink-0 flex-wrap items-center gap-1.5">
            <Button type="button" variant="ghost" size="sm" onClick={() => void openRichLink(syncedPR.prUrl)}>
              <ExternalLink data-icon />
              {t("issueDetail.handoff.openPr")}
            </Button>
            <Button type="button" variant="ghost" size="sm" disabled={props.disabled || props.refreshingHandoffId === syncedPR.id} onClick={() => props.onRefresh(syncedPR)}>
              <RefreshCw data-icon />
              {props.refreshingHandoffId === syncedPR.id ? t("issueDetail.handoff.refreshing") : t("issueDetail.handoff.refresh")}
            </Button>
          </div>
        ) : (
          <Button
            type="button"
            variant="secondary"
            size="sm"
            disabled={!canCreate}
            title={props.disabledReason || (!selectedNode?.branch ? t("issueDetail.handoff.branchRequired") : undefined)}
            onClick={() => selectedNode && props.onCreatePr(selectedNode)}
          >
            <GitPullRequest data-icon />
            {props.isCreatingPr ? t("issueDetail.handoff.syncing") : t("issueDetail.handoff.createOrSyncPr")}
          </Button>
        )}
      </div>

      {syncedPR ? (
        <div className="grid gap-3 border-t border-[color:var(--line)] pt-3">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <HandoffStatusPill handoff={syncedPR} />
            <a
              href={syncedPR.prUrl}
              className="inline-flex min-w-0 items-center gap-1 font-mono text-[12px] font-semibold leading-5 text-[color:var(--accent-blue)] underline underline-offset-2 transition-colors hover:text-[color:var(--text)]"
              onClick={(event) => {
                event.preventDefault();
                void openRichLink(syncedPR.prUrl);
              }}
            >
              {handoffTitle(syncedPR)}
              <ExternalLink data-icon className="shrink-0" />
            </a>
            {syncedPR.prTitle ? <span className="min-w-0 truncate text-[14px] font-medium leading-6 text-[color:var(--text)]">{syncedPR.prTitle}</span> : null}
          </div>
          <div className="grid gap-3 sm:grid-cols-4">
            <HandoffMeta label={t("issueDetail.handoff.sourceBranch")} value={displayBranch} mono />
            <HandoffMeta label={t("issueDetail.handoff.headCommit")} value={sourceCommitValue ? sourceCommitValue.slice(0, 12) : ""} mono title={sourceCommitValue} />
            <HandoffMeta label={t("issueDetail.handoff.commits")} value={commitCount ? `${commitCount}` : ""} mono />
            <HandoffMeta label={t("issueDetail.handoff.checked")} value={checkedAt ? formatRelativeTime(checkedAt) : ""} title={checkedAt ? formatAbsoluteTime(checkedAt) : undefined} />
          </div>
          {syncedPR.error ? <Notice tone="danger">{syncedPR.error}</Notice> : null}
        </div>
      ) : (
        <div className="grid gap-3 border-t border-[color:var(--line)] pt-3">
          {nodes.length > 1 ? (
            <div className="grid gap-1.5">
              <div className="text-[11px] font-medium leading-4 text-[color:var(--faint)]">{t("issueDetail.handoff.sourceBranchForPr")}</div>
              <Select value={selectedNode?.branch || "__none"} onValueChange={(value) => setSourceBranch(value === "__none" ? "" : value)}>
                <SelectTrigger className="max-w-full sm:max-w-[380px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__none">{t("issueDetail.handoff.selectSourceBranch")}</SelectItem>
                  {nodes.map((node) => (
                    <SelectItem key={node.branch} value={node.branch}>
                      {changeNodeSourceLabel(node)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          ) : null}
          <div className="grid gap-3 sm:grid-cols-3">
            <HandoffMeta label={t("issueDetail.handoff.sourceBranch")} value={displayBranch} mono />
            <HandoffMeta label={t("issueDetail.handoff.sourceCommit")} value={sourceCommitValue ? sourceCommitValue.slice(0, 12) : ""} mono title={sourceCommitValue} />
            <HandoffMeta label={t("issueDetail.handoff.status")} value={selectedNode?.branch ? t("issueDetail.handoff.readyToSync") : t("issueDetail.handoff.waitingForBranch")} />
          </div>
          {primaryHandoff?.error ? <Notice tone="danger">{primaryHandoff.error}</Notice> : null}
          <div className="text-[12px] leading-5 text-[color:var(--muted)]">{t("issueDetail.handoff.githubLookupHint")}</div>
        </div>
      )}

      {props.createError ? <Notice tone="danger">{props.createError.message}</Notice> : null}
      {props.refreshError ? <Notice tone="danger">{props.refreshError.message}</Notice> : null}
    </div>
  );
}

function IssueCommitsTab(props: {
  issueId: string;
  changeNodes: IssueChangeNode[];
  sessions: AgentSession[];
  agents: AgentProfile[];
  handoffs: IssueHandoff[];
  actionsDisabled?: boolean;
  actionsDisabledReason?: string;
  isCreatingPr: boolean;
  refreshingHandoffId: string;
  createPrError: Error | null;
  refreshHandoffError: Error | null;
  onCreatePr: (node: IssueChangeNode) => void;
  onRefreshHandoff: (handoff: IssueHandoff) => void;
}) {
  const { t } = useMspaceTranslation();
  const nodes = listOrEmpty(props.changeNodes);

  if (nodes.length === 0) {
    return (
      <section className="grid gap-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex min-w-0 items-center gap-2">
            <GitCommit data-icon className="shrink-0 text-[color:var(--muted)]" />
            <div className="min-w-0">
              <h2 className="text-[14px] font-semibold leading-6 text-[color:var(--text)]">{t("issueDetail.commits.title")}</h2>
              <div className="text-[12px] leading-5 text-[color:var(--muted)]">{t("issueDetail.commits.emptyDescription")}</div>
            </div>
          </div>
          <InlineMeta>{t("issueDetail.commits.captured", { count: 0 })}</InlineMeta>
        </div>
        <IssueHandoffPanel
          changeNodes={nodes}
          handoffs={props.handoffs}
          disabled={props.actionsDisabled}
          disabledReason={props.actionsDisabledReason}
          isCreatingPr={props.isCreatingPr}
          refreshingHandoffId={props.refreshingHandoffId}
          createError={props.createPrError}
          refreshError={props.refreshHandoffError}
          onCreatePr={props.onCreatePr}
          onRefresh={props.onRefreshHandoff}
        />
        <Notice>{t("issueDetail.commits.empty")}</Notice>
      </section>
    );
  }

  return (
    <section className="grid gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <GitCommit data-icon className="shrink-0 text-[color:var(--muted)]" />
          <div className="min-w-0">
            <h2 className="text-[14px] font-semibold leading-6 text-[color:var(--text)]">{t("issueDetail.commits.title")}</h2>
            <div className="text-[12px] leading-5 text-[color:var(--muted)]">{t("issueDetail.commits.description")}</div>
          </div>
        </div>
        <InlineMeta>{t("issueDetail.commits.captured", { count: nodes.length })}</InlineMeta>
      </div>

      <IssueHandoffPanel
        changeNodes={nodes}
        handoffs={props.handoffs}
        disabled={props.actionsDisabled}
        disabledReason={props.actionsDisabledReason}
        isCreatingPr={props.isCreatingPr}
        refreshingHandoffId={props.refreshingHandoffId}
        createError={props.createPrError}
        refreshError={props.refreshHandoffError}
        onCreatePr={props.onCreatePr}
        onRefresh={props.onRefreshHandoff}
      />

      <div className="grid gap-2 rounded-[10px] bg-[color:var(--paper)] p-1 shadow-[inset_0_0_0_1px_var(--line)]">
        {nodes.map((node) => (
          <CommitListRow
            key={node.id || node.commitSha}
            issueId={props.issueId}
            node={node}
            session={changeNodeSession(node, props.sessions)}
            agents={props.agents}
          />
        ))}
      </div>
    </section>
  );
}

function CommitListRow(props: {
  issueId: string;
  node: IssueChangeNode;
  session?: AgentSession;
  agents: AgentProfile[];
}) {
  const { t } = useMspaceTranslation();
  const agent = props.session ? sessionAgent(props.session, props.agents) : undefined;
  return (
    <Link
      to="/issues/$issueId/commits/$commitSha"
      params={{ issueId: props.issueId, commitSha: props.node.commitSha }}
      className="grid min-w-0 gap-2 rounded-[8px] px-3 py-3 text-left transition-[background-color,color] duration-150 ease-out hover:bg-[color:var(--hover)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)]"
    >
      <div className="flex min-w-0 items-center gap-2">
        <GitCommit data-icon className={cn("shrink-0", props.node.error ? "text-[color:var(--danger)]" : "text-[color:var(--accent-blue)]")} />
        <span className="min-w-0 truncate font-mono text-[12px] font-semibold text-[color:var(--text)]">
          {props.node.shortCommitSha || props.node.commitSha.slice(0, 12)}
        </span>
        <span className="ml-auto shrink-0 rounded-full bg-[color:var(--block)] px-2 py-0.5 text-[11px] leading-4 text-[color:var(--muted-strong)]">
          {t("issueDetail.commits.files", { count: props.node.filesChanged })}
        </span>
      </div>
      <div className="line-clamp-2 text-[13px] leading-5 text-[color:var(--muted-strong)]">{props.node.subject || t("issueDetail.commits.noSubject")}</div>
      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-[11px] leading-4 text-[color:var(--faint)]">
        <span>{agent?.name || "Codex"}</span>
        <span>{t("issueDetail.commits.session")} {props.node.sessionId.slice(0, 8)}</span>
        <span>{props.node.branch || t("issueDetail.commits.detached")}</span>
        <span title={formatAbsoluteTime(props.node.createdAt)}>{formatRelativeTime(props.node.createdAt)}</span>
      </div>
    </Link>
  );
}

function CommitReviewFilesNav(props: { node: IssueChangeNode; workdir: string; fileIdPrefix: string }) {
  const { t } = useMspaceTranslation();
  const [query, setQuery] = useState("");
  const changes = visibleWorkspaceFileChanges(listOrEmpty(props.node.changes));
  const normalizedQuery = query.trim().toLowerCase();
  const filteredChanges = normalizedQuery
    ? changes.filter((change) => `${change.previousPath || ""} ${change.path}`.toLowerCase().includes(normalizedQuery))
    : changes;

  return (
    <div className="rounded-[10px] bg-[color:var(--paper)] p-3 shadow-[inset_0_0_0_1px_var(--line)]">
      <div className="flex items-center gap-2">
        <div className="relative min-w-0 flex-1">
          <Search data-icon className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-[color:var(--faint)]" />
          <Input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t("issueDetail.commits.filterFiles")}
            className="h-9 pl-8"
          />
        </div>
      </div>
      <div className="mt-3 max-h-[calc(100vh-260px)] overflow-auto">
        {filteredChanges.length > 0 ? (
          <div className="grid gap-1">
            {filteredChanges.map((change) => {
              const targetPath = props.workdir ? joinLocalPath(props.workdir, change.path) : "";
              return (
                <button
                  key={`${change.statusCode}-${change.previousPath}-${change.path}`}
                  type="button"
                  className="grid min-h-9 grid-cols-[36px_18px_minmax(0,1fr)] items-center gap-2 rounded-[7px] px-2 text-left text-[12px] leading-5 transition-[background-color,color] hover:bg-[color:var(--hover)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)]"
                  title={targetPath || change.path}
                  onClick={() => {
                    document.getElementById(diffFileDomId(props.fileIdPrefix, change.path))?.scrollIntoView({ block: "start", behavior: "smooth" });
                  }}
                >
                  <span className={cn("font-mono text-[11px] font-semibold", workspaceChangeStatusTone(change.statusCode))}>
                    {workspaceChangeStatusLabel(change.statusCode)}
                  </span>
                  <FileTypeIcon path={change.path} />
                  <span className="min-w-0 truncate text-[color:var(--muted-strong)]">
                    {change.previousPath ? `${change.previousPath} -> ${change.path}` : change.path}
                  </span>
                </button>
              );
            })}
          </div>
        ) : (
          <div className="rounded-[8px] bg-[color:var(--block)] px-3 py-2 text-[12px] leading-5 text-[color:var(--muted)]">
            {t("issueDetail.commits.noFilterMatches")}
          </div>
        )}
      </div>
    </div>
  );
}

export function IssueCommitDetailPage() {
  const { t } = useMspaceTranslation();
  const { issueId = "", commitSha = "" } = useParams({ strict: false }) as { issueId?: string; commitSha?: string };
  const commitRef = decodeURIComponent(commitSha);
  const auth = useMspaceAuth();
  const workspaceId = auth.workspace?.id || "";
  const serverWorkspaceReady = Boolean(auth.token && workspaceId);
  const issueQuery = useQuery({
    queryKey: queryKeys.workspaceIssue(workspaceId, issueId, auth.token),
    queryFn: () => controlPlaneApi.getIssue(auth.token, workspaceId, issueId),
    enabled: serverWorkspaceReady && issueId.length > 0,
    refetchInterval: 4_000,
  });
  const agentsQuery = useQuery({
    queryKey: queryKeys.agents(workspaceId, auth.token),
    queryFn: () => controlPlaneApi.listAgents(auth.token, workspaceId),
    enabled: serverWorkspaceReady,
  });

  const detail = issueQuery.data;
  const agents = listOrEmpty(agentsQuery.data);
  const nodes = listOrEmpty(detail?.changeNodes);
  const selectedNode = nodes.find((node) => node.commitSha === commitRef || node.shortCommitSha === commitRef || node.commitSha.startsWith(commitRef));
  const selectedSession = selectedNode ? changeNodeSession(selectedNode, listOrEmpty(detail?.sessions)) : undefined;
  const selectedAgent = selectedSession ? sessionAgent(selectedSession, agents) : undefined;
  const fileIdPrefix = selectedNode ? `commit-diff-${selectedNode.shortCommitSha || selectedNode.commitSha.slice(0, 12)}` : "commit-diff";

  if (!detail) {
    return (
      <PageFrame title={t("issueDetail.commits.commit")} subtitle={t("issueDetail.commits.detailSubtitle")}>
        <div className="text-[14px] text-[color:var(--muted)]">{issueQuery.isPending ? t("issueDetail.commits.loading") : t("issueDetail.page.notFound")}</div>
      </PageFrame>
    );
  }

  if (!selectedNode) {
    return (
      <PageFrame
        title={t("issueDetail.commits.notFound")}
        subtitle={t("issueDetail.commits.notFoundSubtitle")}
        breadcrumbs={[
          { label: "mspace", to: "/inbox" },
          { label: t("issueDetail.page.issues"), to: "/issues" },
          { label: detail.issue.title, to: "/issues/$issueId", params: { issueId }, search: issueTabSearch("commits") },
          { label: t("issueDetail.commits.commit") },
        ]}
      >
        <Notice>{t("issueDetail.commits.noMatch", { commitRef: commitRef || t("issueDetail.commits.thisRoute") })}</Notice>
      </PageFrame>
    );
  }

  return (
    <PageFrame
      title={selectedNode.shortCommitSha || selectedNode.commitSha.slice(0, 12)}
      subtitle={selectedNode.subject || t("issueDetail.commits.noSubject")}
      breadcrumbs={[
        { label: "mspace", to: "/inbox" },
        { label: t("issueDetail.page.issues"), to: "/issues" },
        { label: detail.issue.title, to: "/issues/$issueId", params: { issueId }, search: issueTabSearch("commits") },
        { label: selectedNode.shortCommitSha || selectedNode.commitSha.slice(0, 12) },
      ]}
    >
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <Button type="button" variant="ghost" size="sm" asChild>
          <Link to="/issues/$issueId" params={{ issueId }} search={issueTabSearch("commits")}>
            <ArrowLeft data-icon />
            {t("issueDetail.page.backToIssue")}
          </Link>
        </Button>
        {selectedNode.diffTruncated ? <InlineMeta>{t("issueDetail.commits.diffTruncated")}</InlineMeta> : null}
      </div>

      <section className="mb-4 rounded-[10px] bg-[color:var(--paper)] px-4 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <GitCommit data-icon className="shrink-0 text-[color:var(--accent-blue)]" />
          <span className="min-w-0 truncate font-mono text-[13px] font-semibold text-[color:var(--text)]">{selectedNode.commitSha}</span>
        </div>
        <div className="mt-2 flex min-w-0 flex-wrap gap-x-3 gap-y-1 text-[12px] leading-5 text-[color:var(--muted)]">
          <span>{selectedAgent?.name || "Codex"}</span>
          <span>{t("issueDetail.commits.session")} {selectedNode.sessionId.slice(0, 8)}</span>
          <span>{selectedNode.branch || t("issueDetail.commits.detached")}</span>
          <span>{t("issueDetail.commits.files", { count: selectedNode.filesChanged })}</span>
          <span title={formatAbsoluteTime(selectedNode.createdAt)}>{formatRelativeTime(selectedNode.createdAt)}</span>
        </div>
      </section>

      {selectedNode.error ? <Notice tone="danger">{selectedNode.error}</Notice> : null}

      <div className="grid gap-4 xl:grid-cols-[320px_minmax(0,1fr)] xl:items-start">
        <aside className="min-w-0 xl:sticky xl:top-6">
          <CommitReviewFilesNav node={selectedNode} workdir={selectedSession?.workdir || ""} fileIdPrefix={fileIdPrefix} />
        </aside>
        <main className="min-w-0">
          {selectedNode.diffPreview ? (
            <DiffPreview text={selectedNode.diffPreview} truncated={selectedNode.diffTruncated} fileIdPrefix={fileIdPrefix} constrained={false} />
          ) : (
            <div className="rounded-[8px] bg-[color:var(--block)] px-3 py-2 text-[13px] leading-6 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
              {t("issueDetail.commits.noDiffPreview")}
            </div>
          )}
        </main>
      </div>
    </PageFrame>
  );
}

function IssueSessionsTab(props: { sessions: AgentSession[]; agents: AgentProfile[] }) {
  const { t } = useMspaceTranslation();
  const sessions = listOrEmpty(props.sessions);
  if (sessions.length === 0) {
    return <Notice>{t("issueDetail.sessions.empty")}</Notice>;
  }
  return (
    <section className="grid gap-2">
      {sessions.map((session) => {
        const agent = sessionAgent(session, props.agents);
        const isAnalysisSession = isIssueAnalysisSession(session);
        const title = isAnalysisSession ? t("issueDetail.sessions.analysisTitle") : agent.name;
        const sessionDetail = isAnalysisSession ? t("issueDetail.sessions.analysisDescription") : session.branch || session.workdir;
        return (
          <div id={issueSessionDomId(session.id)} key={session.id} className="scroll-mt-8 grid gap-1 rounded-[9px] bg-[color:var(--paper)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <ActorMark actor={codexActor(agent.name)} size="sm" />
              <span className="font-medium text-[13px] leading-5 text-[color:var(--text)]">{title}</span>
              <StatusBadge value={session.status} />
              {isAnalysisSession ? (
                <span className="rounded-full bg-[color:var(--block)] px-2 py-0.5 text-[11px] font-medium leading-4 text-[color:var(--muted-strong)]">
                  {t("issueDetail.sessions.thinkSkill")}
                </span>
              ) : null}
              {session.sourceCommitSha ? <span className="font-mono text-[11px] text-[color:var(--faint)]">deploy {session.sourceCommitSha.slice(0, 12)}</span> : null}
            </div>
            <div className={cn("min-w-0 text-[12px] leading-5 text-[color:var(--muted)]", isAnalysisSession ? "" : "break-all font-mono")}>
              {sessionDetail}
            </div>
            <TimeMeta value={session.updatedAt || session.createdAt} />
          </div>
        );
      })}
    </section>
  );
}

function IssueResourcesTab(props: {
  environment: IssueTestEnvironment | null;
  cluster?: Cluster;
  resources?: IssueTestEnvironmentResources;
  isLoading: boolean;
  isFetching: boolean;
  error?: Error | null;
  onRefresh: () => void;
}) {
  const { t } = useMspaceTranslation();
  const environment = props.environment;
  const resources = props.resources;
  if (!environment) {
    return <Notice>{t("issueDetail.resources.noEnvironment")}</Notice>;
  }

  return (
    <section className="grid min-w-0 gap-6 overflow-hidden">
      <div className="grid gap-3">
        <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
          <div className="flex min-w-0 items-start gap-3">
            <div className="mt-0.5 grid size-8 shrink-0 place-items-center rounded-[8px] bg-[color:var(--block)] text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]">
              <Boxes data-icon />
            </div>
            <div className="min-w-0">
              <h2 className="text-[15px] font-semibold leading-6 text-[color:var(--text)]">{t("issueDetail.resources.namespaceResources")}</h2>
              <div className="mt-1 min-w-0 break-all font-mono text-[12px] leading-5 text-[color:var(--muted)]">
                {environment.namespace || t("issueDetail.resources.namespacePending")}
              </div>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {resources?.refreshedAt ? (
              <span className="inline-flex items-center gap-1.5 text-[12px] leading-5 text-[color:var(--muted)]">
                <Clock3 data-icon className="text-[color:var(--faint)]" />
                {t("issueDetail.resources.refreshed", { time: formatRelativeTime(resources.refreshedAt) })}
              </span>
            ) : null}
            <Button type="button" variant="secondary" size="sm" disabled={props.isFetching} onClick={props.onRefresh}>
              <RefreshCw data-icon className={cn(props.isFetching && "motion-safe:animate-spin")} />
              {props.isFetching ? t("issueDetail.resources.refreshing") : t("issueDetail.resources.refresh")}
            </Button>
          </div>
        </div>
        <ResourceContextBar environment={environment} cluster={props.cluster} resources={resources} />
      </div>

      {props.error ? <Notice tone="danger">{props.error.message}</Notice> : null}
      {props.isLoading && !resources ? <Notice>{t("issueDetail.resources.loading")}</Notice> : null}
      {resources?.errors.length ? (
        <Notice>
          {resources.errors.map((item) => `${item.section}: ${item.message}`).join(" · ")}
        </Notice>
      ) : null}

      {resources ? (
        <>
          <ResourceMetricStrip resources={resources} />
          <KubernetesResourceSection title={t("issueDetail.resources.pods")} count={resources.pods.length} empty={t("issueDetail.resources.noPods")}>
            <div className="grid gap-2">
              {resources.pods.map((pod) => (
                <article key={pod.name} className="overflow-hidden rounded-[10px] bg-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--line)]">
                  <ResourceRowHeader
                    title={pod.name}
                    status={pod.phase || t("common.unknown")}
                    icon={pod.phase === "Running" || pod.phase === "Succeeded" ? CheckCircle2 : pod.phase === "Failed" ? CircleAlert : CircleDot}
                    tone={pod.phase === "Running" || pod.phase === "Succeeded" ? "success" : pod.phase === "Failed" ? "danger" : "neutral"}
                  />
                  <div className="grid md:grid-cols-4">
                    <ResourceFact label={t("issueDetail.resources.ready")} value={`${pod.readyContainers}/${pod.totalContainers}`} />
                    <ResourceFact label={t("issueDetail.resources.restarts")} value={String(pod.restarts)} />
                    <ResourceFact label={t("issueDetail.resources.node")} value={pod.nodeName || t("issueDetail.resources.notScheduled")} mono />
                    <ResourceFact label={t("issueDetail.resources.podIp")} value={pod.podIp || t("issueDetail.resources.notAssigned")} mono />
                  </div>
                </article>
              ))}
            </div>
          </KubernetesResourceSection>

          <KubernetesResourceSection title={t("issueDetail.resources.services")} count={resources.services.length} empty={t("issueDetail.resources.noServices")}>
            <div className="grid gap-2">
              {resources.services.map((service) => (
                <article key={service.name} className="overflow-hidden rounded-[10px] bg-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--line)]">
                  <ResourceRowHeader title={service.name} status={service.type || t("issueDetail.resources.service")} icon={Globe2} tone="neutral" />
                  <div className="grid md:grid-cols-3">
                    <ResourceFact label={t("issueDetail.resources.clusterIp")} value={service.clusterIp || t("issueDetail.resources.notAssigned")} mono />
                    <ResourceFact label={t("issueDetail.resources.externalIp")} value={service.externalIp || t("issueDetail.resources.none")} mono />
                    <ResourceFact label={t("issueDetail.resources.created")} value={service.createdAt ? formatRelativeTime(service.createdAt) : t("issueDetail.resources.unknown")} />
                  </div>
                  <div className="grid divide-y divide-[color:var(--line)] border-t border-[color:var(--line)]">
                    {service.ports.map((port) => (
                      <div key={`${port.name}-${port.port}-${port.nodePort}`} className="flex min-w-0 flex-wrap items-center gap-2 px-3 py-2 text-[12px] leading-5">
                        <code className="font-mono text-[color:var(--text)]">{port.name || t("issueDetail.resources.port")}</code>
                        <span className="text-[color:var(--muted)]">{port.protocol}</span>
                        <span className="font-mono text-[color:var(--muted-strong)]">{port.port} -&gt; {port.targetPort || t("issueDetail.resources.target")}</span>
                        {port.nodePort > 0 ? <span className="font-mono text-[color:var(--muted-strong)]">node {port.nodePort}</span> : null}
                        {port.url ? (
                          <Button type="button" variant="ghost" size="sm" className="ml-auto h-7" onClick={() => void openRichLink(port.url)}>
                            <ExternalLink data-icon />
                            {t("issueDetail.resources.open")}
                          </Button>
                        ) : null}
                      </div>
                    ))}
                  </div>
                </article>
              ))}
            </div>
          </KubernetesResourceSection>

          <KubernetesResourceSection title={t("issueDetail.resources.deployments")} count={resources.deployments.length} empty={t("issueDetail.resources.noDeployments")}>
            <div className="grid gap-2">
              {resources.deployments.map((deployment) => (
                <article key={deployment.name} className="overflow-hidden rounded-[10px] bg-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--line)]">
                  <ResourceRowHeader
                    title={deployment.name}
                    status={`${deployment.readyReplicas}/${deployment.replicas || 0} ${t("issueDetail.resources.ready")}`}
                    icon={deployment.readyReplicas >= deployment.replicas && deployment.replicas > 0 ? CheckCircle2 : CircleAlert}
                    tone={deployment.readyReplicas >= deployment.replicas && deployment.replicas > 0 ? "success" : "warning"}
                  />
                  <div className="grid md:grid-cols-4">
                    <ResourceFact label={t("issueDetail.resources.replicas")} value={String(deployment.replicas)} />
                    <ResourceFact label={t("issueDetail.resources.ready")} value={String(deployment.readyReplicas)} />
                    <ResourceFact label={t("issueDetail.resources.updated")} value={String(deployment.updatedReplicas)} />
                    <ResourceFact label={t("issueDetail.resources.available")} value={String(deployment.availableReplicas)} />
                  </div>
                  {deployment.conditions.length > 0 ? (
                    <div className="grid gap-1.5 border-t border-[color:var(--line)] px-3 py-2">
                      {deployment.conditions.map((condition) => (
                        <div key={`${condition.type}-${condition.reason}`} className="rounded-[7px] bg-[color:var(--block)] px-3 py-2 text-[12px] leading-5 text-[color:var(--muted-strong)]">
                          <span className="font-medium text-[color:var(--text)]">{condition.type}</span>
                          <span className="ml-2 text-[color:var(--muted)]">{condition.status}</span>
                          {condition.reason ? <span className="ml-2 text-[color:var(--faint)]">{condition.reason}</span> : null}
                          {condition.message ? <div className="mt-1 text-[color:var(--muted)] [overflow-wrap:anywhere]">{condition.message}</div> : null}
                        </div>
                      ))}
                    </div>
                  ) : null}
                </article>
              ))}
            </div>
          </KubernetesResourceSection>

          <KubernetesResourceSection title={t("issueDetail.resources.ingresses")} count={resources.ingresses.length} empty={t("issueDetail.resources.noIngresses")}>
            <div className="grid gap-2">
              {resources.ingresses.map((ingress) => (
                <article key={ingress.name} className="overflow-hidden rounded-[10px] bg-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--line)]">
                  <ResourceRowHeader title={ingress.name} status={ingress.className || "Ingress"} icon={Globe2} tone="neutral" />
                  <div className="grid md:grid-cols-3">
                    <ResourceFact label={t("issueDetail.resources.class")} value={ingress.className || t("issueDetail.resources.default")} />
                    <ResourceFact label={t("issueDetail.resources.hosts")} value={ingress.hosts.join(", ") || t("issueDetail.resources.none")} mono />
                    <ResourceFact label={t("issueDetail.resources.addresses")} value={ingress.addresses.join(", ") || t("issueDetail.resources.pending")} mono />
                  </div>
                </article>
              ))}
            </div>
          </KubernetesResourceSection>

          <KubernetesResourceSection title={t("issueDetail.resources.events")} count={resources.events.length} empty={t("issueDetail.resources.noEvents")}>
            <div className="grid gap-2">
              {resources.events.map((event, index) => (
                <article key={`${event.involvedKind}-${event.involvedName}-${event.reason}-${index}`} className="grid gap-1 rounded-[10px] bg-[color:var(--paper)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
                  <div className="flex min-w-0 flex-wrap items-center gap-2">
                    <StatusBadge value={event.type || t("issueDetail.resources.event")} className="h-5 px-2 py-0 text-[11px]" />
                    <span className="font-medium text-[13px] leading-5 text-[color:var(--text)]">{event.reason || t("issueDetail.resources.event")}</span>
                    <span className="font-mono text-[11px] leading-4 text-[color:var(--faint)]">{event.involvedKind}/{event.involvedName}</span>
                    {event.lastSeen ? <span className="ml-auto text-[11px] leading-4 text-[color:var(--faint)]">{formatRelativeTime(event.lastSeen)}</span> : null}
                  </div>
                  <div className="text-[12px] leading-5 text-[color:var(--muted-strong)] [overflow-wrap:anywhere]">{event.message || t("issueDetail.resources.noEventMessage")}</div>
                  {event.count > 1 ? <div className="text-[11px] leading-4 text-[color:var(--faint)]">{t("issueDetail.resources.count", { count: event.count })}</div> : null}
                </article>
              ))}
            </div>
          </KubernetesResourceSection>
        </>
      ) : null}
    </section>
  );
}

function ResourceContextBar(props: {
  environment: IssueTestEnvironment;
  cluster?: Cluster;
  resources?: IssueTestEnvironmentResources;
}) {
  const { t } = useMspaceTranslation();
  const environment = props.environment;
  const rows = [
    { label: t("issueDetail.resources.cluster"), value: props.resources?.clusterName || props.cluster?.name || environment.clusterId || t("issueDetail.environment.notSelected") },
    { label: t("issueDetail.resources.context"), value: props.resources?.context || environment.kubeContext || t("issueDetail.environment.defaultContext") },
    { label: t("issueDetail.resources.lifecycle"), value: namespaceStatusLabel(props.resources?.namespaceStatus || environment.namespaceStatus) },
    { label: t("issueDetail.resources.exposure"), value: previewStrategy(environment) },
    { label: t("issueDetail.resources.cleanup"), value: cleanupDecisionLabel(environment.cleanupStatus) },
    { label: t("issueDetail.resources.preview"), value: props.resources?.previewUrl || environment.previewUrl || t("issueDetail.environment.notAvailable"), mono: true },
  ];
  return (
    <div className="grid min-w-0 overflow-hidden rounded-[10px] bg-[color:var(--block-subtle)] shadow-[inset_0_0_0_1px_var(--line)] md:grid-cols-[1fr_1.15fr_0.85fr_0.85fr_0.85fr_1.4fr]">
      {rows.map((row) => (
        <div key={row.label} className="min-w-0 border-b border-[color:var(--line)] px-3 py-2 last:border-b-0 md:border-b-0 md:border-r md:last:border-r-0">
          <div className="text-[11px] leading-4 text-[color:var(--faint)]">{row.label}</div>
          <div className={cn("mt-0.5 min-w-0 truncate text-[12px] leading-5 text-[color:var(--muted-strong)]", row.mono && "font-mono")} title={row.value}>
            {row.value}
          </div>
        </div>
      ))}
    </div>
  );
}

function ResourceMetricStrip(props: { resources: IssueTestEnvironmentResources }) {
  const { t } = useMspaceTranslation();
  const counts = [
    { label: t("issueDetail.resources.pods"), value: props.resources.pods.length },
    { label: t("issueDetail.resources.services"), value: props.resources.services.length },
    { label: t("issueDetail.resources.deployments"), value: props.resources.deployments.length },
    { label: t("issueDetail.resources.ingresses"), value: props.resources.ingresses.length },
    { label: t("issueDetail.resources.events"), value: props.resources.events.length },
  ];
  return (
    <div className="grid overflow-hidden rounded-[10px] bg-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--line)] sm:grid-cols-2 lg:grid-cols-5">
      {counts.map((item) => (
        <button
          key={item.label}
          type="button"
          className="grid min-h-14 grid-cols-[minmax(0,1fr)_auto] items-center gap-3 border-b border-[color:var(--line)] px-3 py-2 text-left transition-[background-color,color,transform] duration-150 ease-out last:border-b-0 hover:bg-[color:var(--hover)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)] active:scale-[0.99] lg:border-b-0 lg:border-r lg:last:border-r-0"
          onClick={() => {
            document.getElementById(resourceSectionId(item.label))?.scrollIntoView({ block: "start", behavior: "smooth" });
          }}
        >
          <span className="min-w-0 truncate text-[12px] leading-5 text-[color:var(--muted)]">{item.label}</span>
          <span className="font-mono text-[18px] font-semibold leading-6 tabular-nums text-[color:var(--text)]">{item.value}</span>
        </button>
      ))}
    </div>
  );
}

function resourceSectionId(title: string) {
  return `resource-section-${title.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`;
}

function KubernetesResourceSection(props: { title: string; count: number; empty: string; children: React.ReactNode }) {
  return (
    <section id={resourceSectionId(props.title)} className="grid scroll-mt-6 min-w-0 gap-3 border-t border-[color:var(--line)] pt-5">
      <div className="flex min-w-0 items-center justify-between gap-3">
        <h3 className="text-[14px] font-semibold leading-6 text-[color:var(--text)]">{props.title}</h3>
        <span className="font-mono text-[12px] leading-5 tabular-nums text-[color:var(--muted)]">{props.count}</span>
      </div>
      {props.count > 0 ? props.children : <div className="rounded-[8px] bg-[color:var(--block)] px-3 py-2 text-[13px] leading-6 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">{props.empty}</div>}
    </section>
  );
}

function ResourceRowHeader(props: { title: string; status: string; icon: typeof CircleDot; tone?: "success" | "warning" | "danger" | "neutral" }) {
  const Icon = props.icon;
  const tone = props.tone || "neutral";
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-2 bg-[color:var(--paper)] px-3 py-3" title={props.status}>
      <Icon
        data-icon
        className={cn(
          "shrink-0",
          tone === "success" && "text-[color:var(--success)]",
          tone === "warning" && "text-[color:var(--warning)]",
          tone === "danger" && "text-[color:var(--danger)]",
          tone === "neutral" && "text-[color:var(--muted)]",
        )}
      />
      <span className="min-w-0 truncate font-mono text-[13px] font-semibold leading-5 tracking-normal text-[color:var(--text)]">{props.title}</span>
    </div>
  );
}

function ResourceFact(props: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="min-w-0 border-t border-[color:var(--line)] px-3 py-2 md:border-r md:last:border-r-0">
      <div className="text-[11px] leading-4 text-[color:var(--faint)]">{props.label}</div>
      <div className={cn("mt-0.5 min-w-0 text-[12px] leading-5 text-[color:var(--muted-strong)] [overflow-wrap:anywhere]", props.mono && "font-mono tabular-nums")}>{props.value}</div>
    </div>
  );
}

function ReviewStatusPill(props: { status: string }) {
  const status = props.status || "not_reported";
  const label = translate(`issueDetail.evidence.status.${status}`, { defaultValue: status.replace(/[_-]+/g, " ") });
  const tone =
    status === "passed" || status === "completed"
      ? "bg-[color:var(--success-soft)] text-[color:var(--success)]"
      : status === "failed"
        ? "bg-[color:var(--danger-soft)] text-[color:var(--danger)]"
        : status === "warning"
          ? "bg-[color:var(--warning-soft)] text-[color:var(--warning)]"
        : status === "running"
          ? "bg-[color:var(--blue-soft)] text-[color:var(--accent-blue)]"
          : "bg-[color:var(--block)] text-[color:var(--muted-strong)]";
  return (
    <span className={cn("inline-flex w-fit items-center rounded-full px-2 py-0.5 text-[12px] font-medium leading-5", tone)}>
      {label}
    </span>
  );
}

function EvidenceSection(props: { title: string; children: React.ReactNode; aside?: React.ReactNode }) {
  return (
    <section className="grid min-w-0 gap-3 border-t border-[color:var(--line)] pt-4">
      <div className="flex min-w-0 items-center justify-between gap-3">
        <h3 className="text-[13px] font-semibold leading-5 text-[color:var(--muted-strong)]">{props.title}</h3>
        {props.aside}
      </div>
      {props.children}
    </section>
  );
}

function ReviewMetaGrid(props: { rows: Array<{ label: string; value: string; mono?: boolean }> }) {
  const rows = props.rows.filter((row) => row.value);
  if (rows.length === 0) return <div className="text-[13px] leading-6 text-[color:var(--muted)]">{translate("issueDetail.sidebar.notReported")}.</div>;
  return (
    <div className="grid min-w-0 gap-2 md:grid-cols-2">
      {rows.map((row) => (
        <div key={row.label} className="min-w-0 overflow-hidden rounded-[8px] bg-[color:var(--block)] px-3 py-2 shadow-[inset_0_0_0_1px_var(--line)]">
          <div className="text-[11px] leading-4 text-[color:var(--faint)]">{row.label}</div>
          <div className={cn("mt-0.5 min-w-0 text-[12px] leading-5 text-[color:var(--muted-strong)] [overflow-wrap:anywhere]", row.mono && "font-mono")}>{row.value}</div>
        </div>
      ))}
    </div>
  );
}

function ReviewResultBlock(props: { result: ReviewEvidenceResult; empty: string }) {
  const result = props.result || { status: "not_reported", summary: props.empty, details: "" };
  const status = result.status || "not_reported";
  const summary = result.summary || props.empty;
  return (
    <div className="grid min-w-0 gap-2">
      <ReviewStatusPill status={status} />
      <div className="min-w-0 text-[13px] leading-6 text-[color:var(--muted-strong)] [overflow-wrap:anywhere]">{summary}</div>
      {result.details ? (
        <ReviewDetailsDisclosure label={translate("issueDetail.evidence.showRawOutput")}>
          <pre className="max-h-56 max-w-full overflow-auto rounded-[8px] bg-[color:var(--code-bg)] px-3 py-2 font-mono text-[12px] leading-6 text-[color:var(--code-text)] whitespace-pre-wrap [overflow-wrap:anywhere]">
            {result.details}
          </pre>
        </ReviewDetailsDisclosure>
      ) : null}
    </div>
  );
}

function testStatusForChecks(checks: ReviewEvidenceCheck[]) {
  const statuses = listOrEmpty(checks).map((check) => check.status).filter(Boolean);
  if (statuses.length === 0) return "not_reported";
  if (statuses.some((status) => status === "failed")) return "failed";
  if (statuses.some((status) => status === "warning")) return "warning";
  if (statuses.some((status) => status === "running")) return "running";
  if (statuses.some((status) => status === "passed" || status === "completed")) return "passed";
  return statuses[0] || "not_reported";
}

function testSummaryForChecks(checks: ReviewEvidenceCheck[]) {
  const items = listOrEmpty(checks);
  if (items.length === 0) return translate("issueDetail.evidence.noTestResult");
  if (items.length === 1) return items[0].name || items[0].summary || translate("issueDetail.evidence.testResultCaptured");
  const failedCount = items.filter((item) => item.status === "failed").length;
  if (failedCount > 0) return translate("issueDetail.evidence.checksFailed", { failed: failedCount, total: items.length });
  const passedCount = items.filter((item) => item.status === "passed" || item.status === "completed").length;
  if (passedCount === items.length) return translate("issueDetail.evidence.checksPassed", { count: items.length });
  return translate("issueDetail.evidence.checksCaptured", { count: items.length });
}

function reviewResultForDisplay(result: ReviewEvidenceResult, commands: ReviewEvidenceCommand[], category: string) {
  const matches = listOrEmpty(commands).filter((command) => command.category === category && command.status !== "running");
  if (matches.length === 0) return result;
  const latest = matches[matches.length - 1];
  if (result.status === "failed" && latest.status === "passed") {
    return {
      ...result,
      status: "passed",
      summary: translate("issueDetail.evidence.latestCommandPassed", { category }),
      details: latest.summary || result.details,
    };
  }
  return result;
}

function commandEvidenceIsKey(command: ReviewEvidenceCommand) {
  if (command.status === "failed") return true;
  if (command.category !== "command") return true;
  const value = command.command.toLowerCase();
  return (
    value.includes("git commit") ||
    value.includes("git diff --check") ||
    value.includes("git status") ||
    value.includes("npm ci") ||
    value.includes("pnpm install") ||
    value.includes("playwright") ||
    value.includes("curl -sS -X PUT".toLowerCase())
  );
}

function compactCommandText(value: string) {
  let command = value.trim();
  const zshPrefix = "/bin/zsh -lc ";
  if (command.startsWith(zshPrefix)) {
    command = command.slice(zshPrefix.length).trim();
  }
  if ((command.startsWith('"') && command.endsWith('"')) || (command.startsWith("'") && command.endsWith("'"))) {
    command = command.slice(1, -1);
  }
  return command;
}

function ReviewDetailsDisclosure(props: { label: string; children: React.ReactNode }) {
  return (
    <details className="group min-w-0">
      <summary className="cursor-pointer select-none text-[12px] font-medium leading-5 text-[color:var(--muted)] transition-colors hover:text-[color:var(--text)]">
        {props.label}
      </summary>
      <div className="mt-2 min-w-0">{props.children}</div>
    </details>
  );
}

function CommandEvidenceList(props: { commands: ReviewEvidenceCommand[] }) {
  const commands = listOrEmpty(props.commands);
  if (commands.length === 0) return <div className="text-[13px] leading-6 text-[color:var(--muted)]">{translate("issueDetail.evidence.noCommands")}</div>;
  const keyCommands = commands.filter(commandEvidenceIsKey).slice(-8);
  const hiddenCount = Math.max(0, commands.length - keyCommands.length);
  return (
    <div className="grid min-w-0 gap-3">
      <div className="rounded-[8px] bg-[color:var(--block)] px-3 py-2 text-[13px] leading-6 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
        {translate("issueDetail.evidence.capturedCommands", { count: commands.length, suffix: commands.length === 1 ? "" : "s" })}
      </div>
      {keyCommands.length > 0 ? (
        <div className="grid min-w-0 gap-1.5">
          {keyCommands.map((command, index) => (
            <div key={`${command.command}-${index}`} className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] items-start gap-x-2 gap-y-1 rounded-[8px] bg-[color:var(--block)] px-3 py-2 shadow-[inset_0_0_0_1px_var(--line)]">
              <ReviewStatusPill status={command.status} />
              <div className="min-w-0">
                <div className="flex min-w-0 flex-wrap items-center gap-2 text-[11px] leading-4 text-[color:var(--faint)]">
                  <span>{command.category || "command"}</span>
                  {command.createdAt ? <span>{formatRelativeTime(command.createdAt)}</span> : null}
                </div>
                <code className="mt-0.5 block min-w-0 max-w-full font-mono text-[12px] leading-5 text-[color:var(--text)] whitespace-pre-wrap [overflow-wrap:anywhere]">
                  {compactCommandText(command.command)}
                </code>
              </div>
            </div>
          ))}
        </div>
      ) : null}
      {hiddenCount > 0 ? (
        <ReviewDetailsDisclosure label={translate("issueDetail.evidence.showRawCommandTrail", { count: commands.length })}>
          <div className="grid min-w-0 gap-2">
            {commands.map((command, index) => (
              <div key={`${command.command}-${index}`} className="grid min-w-0 gap-2 overflow-hidden rounded-[8px] bg-[color:var(--block)] px-3 py-2 shadow-[inset_0_0_0_1px_var(--line)]">
                <div className="flex min-w-0 flex-wrap items-center gap-2">
                  <ReviewStatusPill status={command.status} />
                  <span className="rounded-full bg-[color:var(--surface)] px-2 py-0.5 text-[11px] leading-4 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">{command.category || "command"}</span>
                  {command.createdAt ? <span className="text-[11px] leading-4 text-[color:var(--faint)]">{formatRelativeTime(command.createdAt)}</span> : null}
                </div>
                <code className="block min-w-0 max-w-full font-mono text-[12px] leading-5 text-[color:var(--text)] whitespace-pre-wrap [overflow-wrap:anywhere]">{compactCommandText(command.command)}</code>
                {command.summary ? (
                  <ReviewDetailsDisclosure label={translate("issueDetail.evidence.showCommandOutput")}>
                    <div className="max-h-44 min-w-0 overflow-auto border-t border-[color:var(--line)] pt-2 text-[12px] leading-5 text-[color:var(--muted)] whitespace-pre-wrap [overflow-wrap:anywhere]">
                      {command.summary}
                    </div>
                  </ReviewDetailsDisclosure>
                ) : null}
              </div>
            ))}
          </div>
        </ReviewDetailsDisclosure>
      ) : null}
    </div>
  );
}

function ReviewStringList(props: { items: string[]; empty: string }) {
  const items = listOrEmpty(props.items);
  if (items.length === 0) return <div className="text-[13px] leading-6 text-[color:var(--muted)]">{props.empty}</div>;
  return (
    <ul className="grid gap-1.5">
      {items.map((item, index) => (
        <li key={`${index}-${item}`} className="min-w-0 rounded-[8px] bg-[color:var(--block)] px-3 py-2 text-[13px] leading-6 text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)] [overflow-wrap:anywhere]">
          {item}
        </li>
      ))}
    </ul>
  );
}

function reviewStatusForPacket(review?: SessionReviewEvidence) {
  if (!review) return "not_reported";
  const statuses = [review.deploymentResult?.status, review.buildResult?.status, ...listOrEmpty(review.tests).map((test) => test.status)].filter(Boolean);
  if (statuses.some((status) => status === "failed")) return "failed";
  if (statuses.some((status) => status === "warning")) return "warning";
  if (statuses.some((status) => status === "running")) return "running";
  if (statuses.some((status) => status === "passed" || status === "completed")) return "passed";
  return review.sourceCommitSha || review.agentSummary ? "collected" : "not_reported";
}

function reviewMatchesEnvironment(review: SessionReviewEvidence, environment: IssueTestEnvironment | null) {
  if (!environment) return false;
  const sourceCommit = review.sourceCommitSha.trim();
  const environmentCommit = environment.sourceCommitSha.trim();
  if (sourceCommit && environmentCommit && (sourceCommit === environmentCommit || sourceCommit.startsWith(environmentCommit) || environmentCommit.startsWith(sourceCommit))) {
    return true;
  }
  const sourceSession = review.sourceSessionId.trim();
  return Boolean(sourceSession && sourceSession === environment.sourceSessionId.trim());
}

function currentReviewEvidence(reviews: SessionReviewEvidence[], environment: IssueTestEnvironment | null) {
  return reviews.find((review) => reviewMatchesEnvironment(review, environment)) || reviews[0];
}

function reviewSourceNode(review: SessionReviewEvidence | undefined, changeNodes: IssueChangeNode[]) {
  const sourceCommit = review?.sourceCommitSha.trim() || "";
  if (!sourceCommit) return undefined;
  return changeNodes.find((node) => node.commitSha === sourceCommit || node.commitSha.startsWith(sourceCommit) || sourceCommit.startsWith(node.commitSha));
}

function reviewSourceSession(review: SessionReviewEvidence | undefined, sessions: AgentSession[]) {
  if (!review) return undefined;
  return sessions.find((session) => session.id === review.sourceSessionId || session.id === review.sessionId);
}

function uniqueEvidenceSessions(evidence: DeploymentEvidence[]) {
  return new Set(evidence.map((item) => item.sessionId).filter(Boolean)).size;
}

function EvidenceSignal(props: { label: string; status: string; summary: string }) {
  return (
    <div className="min-w-0 border-t border-[color:var(--line)] px-0 py-3 md:border-r md:px-3 md:first:pl-0 md:last:border-r-0 md:last:pr-0">
      <div className="flex min-w-0 items-center gap-2">
        <ReviewStatusPill status={props.status} />
        <span className="min-w-0 truncate text-[12px] font-semibold leading-5 text-[color:var(--text)]">{props.label}</span>
      </div>
      <div className="mt-1 min-w-0 truncate text-[12px] leading-5 text-[color:var(--muted)]" title={props.summary}>
        {props.summary}
      </div>
    </div>
  );
}

function EvidenceOutcomePacket(props: {
  review?: SessionReviewEvidence;
  latestEvidence?: DeploymentEvidence;
  testEnvironment: IssueTestEnvironment | null;
}) {
  const { t } = useMspaceTranslation();
  const review = props.review;
  const environment = props.testEnvironment;
  const hasPreview = Boolean(review?.previewUrl || environment?.previewUrl);
  const packetStatus = environment?.namespaceStatus === "active" ? "passed" : reviewStatusForPacket(review);
  const PacketIcon = packetStatus === "failed" || packetStatus === "warning" ? CircleAlert : packetStatus === "passed" ? CheckCircle2 : CircleDot;
  const build = review ? reviewResultForDisplay(review.buildResult, review.commandsRun, "build") : undefined;
  const deployment = review?.deploymentResult;
  const testStatus = review ? testStatusForChecks(review.tests) : "not_reported";
  const testSummary = review ? testSummaryForChecks(review.tests) : t("issueDetail.evidence.noTestsReported");
  const summary =
    review?.deploymentResult?.summary ||
    props.latestEvidence?.summary ||
    (environment ? t("issueDetail.evidence.namespaceStateSummary", { state: environment.namespaceStatus || "state" }) : t("issueDetail.evidence.noCurrentValidationSummary"));
  return (
    <article className="grid min-w-0 gap-4 overflow-hidden rounded-[12px] bg-[color:var(--paper)] px-4 py-4 shadow-[inset_0_0_0_1px_var(--line)]">
      <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <PacketIcon
              data-icon
              className={cn(
                "shrink-0",
                packetStatus === "passed" && "text-[color:var(--success)]",
                packetStatus === "failed" && "text-[color:var(--danger)]",
                packetStatus === "warning" && "text-[color:var(--warning)]",
                packetStatus !== "passed" && packetStatus !== "failed" && packetStatus !== "warning" && "text-[color:var(--muted)]",
              )}
            />
            <h2 className="text-[15px] font-semibold leading-6 text-[color:var(--text)]">{t("issueDetail.evidence.currentReviewPacket")}</h2>
          </div>
          <div className="mt-1 max-w-[74ch] text-[14px] leading-6 text-[color:var(--muted-strong)] [overflow-wrap:anywhere]">{summary}</div>
        </div>
      </div>
      <div className="grid min-w-0 md:grid-cols-4">
        <EvidenceSignal label={t("issueDetail.evidence.preview")} status={packetStatus} summary={hasPreview ? t("issueDetail.evidence.previewUrlRecorded") : t("issueDetail.evidence.noPreviewUrl")} />
        <EvidenceSignal label={t("issueDetail.evidence.deployment")} status={deployment?.status || packetStatus} summary={deployment?.summary || summary} />
        <EvidenceSignal label={t("issueDetail.evidence.build")} status={build?.status || "not_reported"} summary={build?.summary || t("issueDetail.evidence.noBuildResult")} />
        <EvidenceSignal label={t("issueDetail.evidence.tests")} status={testStatus} summary={testSummary} />
      </div>
    </article>
  );
}

function EvidenceFactPanel(props: {
  review?: SessionReviewEvidence;
  latestEvidence?: DeploymentEvidence;
  testEnvironment: IssueTestEnvironment | null;
  sourceNode?: IssueChangeNode;
  sourceSession?: AgentSession;
  evidenceCount: number;
}) {
  const { t } = useMspaceTranslation();
  const review = props.review;
  const environment = props.testEnvironment;
  const rows = [
    { label: t("issueDetail.evidence.sourceCommit"), value: review?.sourceCommitSha || environment?.sourceCommitSha || props.sourceNode?.commitSha || "", mono: true },
    { label: t("issueDetail.evidence.branch"), value: review?.branch || props.sourceSession?.branch || "", mono: true },
    { label: t("issueDetail.evidence.sourceSession"), value: review?.sourceSessionId || environment?.sourceSessionId || props.sourceSession?.id || "", mono: true },
    { label: t("issueDetail.evidence.capturedBy"), value: review?.sessionId || "", mono: true },
    { label: t("issueDetail.evidence.namespace"), value: review?.namespace || environment?.namespace || props.latestEvidence?.namespace || "", mono: true },
    { label: t("issueDetail.evidence.cleanup"), value: review?.cleanupStatus || environment?.cleanupStatus || "" },
    { label: t("issueDetail.evidence.snapshots"), value: props.evidenceCount > 0 ? String(props.evidenceCount) : "" },
  ].filter((row) => row.value);
  if (rows.length === 0) return null;
  return (
    <EvidenceSection title={t("issueDetail.evidence.reviewFacts")}>
      <div className="overflow-hidden rounded-[10px] bg-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--line)]">
        {rows.map((row, index) => (
          <div key={row.label} className={cn("grid min-w-0 grid-cols-[112px_minmax(0,1fr)] gap-3 px-3 py-2.5", index > 0 && "border-t border-[color:var(--line)]")}>
            <div className="text-[11px] leading-5 text-[color:var(--faint)]">{row.label}</div>
            <div className={cn("min-w-0 truncate text-[12px] leading-5 text-[color:var(--muted-strong)]", row.mono && "font-mono tabular-nums")} title={row.value}>
              {row.value}
            </div>
          </div>
        ))}
      </div>
    </EvidenceSection>
  );
}

function AgentSummaryBlock(props: { summary: string }) {
  const { t } = useMspaceTranslation();
  return (
    <EvidenceSection title={t("issueDetail.evidence.agentSummary")}>
      {props.summary ? (
        <div className="rounded-[10px] bg-[color:var(--paper)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
          <RichText className="text-[13px] leading-6">{props.summary}</RichText>
        </div>
      ) : (
        <div className="text-[13px] leading-6 text-[color:var(--muted)]">{t("issueDetail.evidence.noAgentSummary")}</div>
      )}
    </EvidenceSection>
  );
}

function EvidenceCommandsPanel(props: { commands: ReviewEvidenceCommand[] }) {
  const { t } = useMspaceTranslation();
  return (
    <EvidenceSection title={t("issueDetail.evidence.commandEvidence")} aside={<InlineMeta>{t("issueDetail.evidence.captured", { count: props.commands.length })}</InlineMeta>}>
      <ReviewDetailsDisclosure label={t("issueDetail.evidence.showCommandEvidence")}>
        <CommandEvidenceList commands={props.commands} />
      </ReviewDetailsDisclosure>
    </EvidenceSection>
  );
}

function KubernetesEvidenceDigest(props: { issueId: string; evidence: DeploymentEvidence[] }) {
  const { t } = useMspaceTranslation();
  const evidence = listOrEmpty(props.evidence);
  if (evidence.length === 0) return null;
  return (
    <EvidenceSection title={t("issueDetail.evidence.kubernetesEvidence")}>
      <div className="rounded-[10px] bg-[color:var(--paper)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
          <div className="min-w-0">
            <div className="text-[13px] font-semibold leading-5 text-[color:var(--text)]">{t("issueDetail.evidence.snapshotHistory")}</div>
            <div className="mt-0.5 text-[12px] leading-5 text-[color:var(--muted)]">{t("issueDetail.evidence.openFullHistory")}</div>
          </div>
          <Button type="button" variant="secondary" size="sm" asChild>
            <Link to="/issues/$issueId/evidence/snapshots" params={{ issueId: props.issueId }}>
              <History data-icon />
              {t("issueDetail.evidence.open")}
            </Link>
          </Button>
        </div>
      </div>
    </EvidenceSection>
  );
}

function ReviewHistoryList(props: {
  issueId: string;
  reviews: SessionReviewEvidence[];
  failures: SessionFailure[];
}) {
  const { t } = useMspaceTranslation();
  const reviews = listOrEmpty(props.reviews);
  const failures = listOrEmpty(props.failures);
  if (reviews.length === 0 && failures.length === 0) return null;
  return (
    <EvidenceSection title={t("issueDetail.evidence.previousAttempts")}>
      <div className="rounded-[10px] bg-[color:var(--paper)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
          <div className="min-w-0">
            <div className="text-[13px] font-semibold leading-5 text-[color:var(--text)]">{t("issueDetail.evidence.olderReviewsAndBlockers")}</div>
            <div className="mt-0.5 text-[12px] leading-5 text-[color:var(--muted)]">{t("issueDetail.evidence.openOlderReviews")}</div>
          </div>
          <Button type="button" variant="secondary" size="sm" asChild>
            <Link to="/issues/$issueId/evidence/history" params={{ issueId: props.issueId }}>
              <History data-icon />
              {t("issueDetail.evidence.open")}
            </Link>
          </Button>
        </div>
      </div>
    </EvidenceSection>
  );
}

function IssueEvidenceTab(props: {
  issueId: string;
  reviewEvidence: SessionReviewEvidence[];
  evidence: DeploymentEvidence[];
  failures: SessionFailure[];
  testEnvironment: IssueTestEnvironment | null;
  sessions: AgentSession[];
  changeNodes: IssueChangeNode[];
}) {
  const { t } = useMspaceTranslation();
  const reviews = listOrEmpty(props.reviewEvidence);
  const evidence = listOrEmpty(props.evidence);
  const failures = listOrEmpty(props.failures);
  const currentReview = currentReviewEvidence(reviews, props.testEnvironment);
  const historicalReviews = currentReview ? reviews.filter((review) => review.id !== currentReview.id) : reviews;
  const latestEvidence = evidence[0];
  const sourceNode = reviewSourceNode(currentReview, props.changeNodes);
  const sourceSession = reviewSourceSession(currentReview, props.sessions);
  if (reviews.length === 0 && evidence.length === 0 && failures.length === 0 && !props.testEnvironment) {
    return <Notice>{t("issueDetail.evidence.noReviewEvidence")}</Notice>;
  }
  return (
    <section className="grid min-w-0 gap-5 overflow-hidden">
      <EvidenceOutcomePacket
        review={currentReview}
        latestEvidence={latestEvidence}
        testEnvironment={props.testEnvironment}
      />

      <div className="grid min-w-0 gap-6 xl:grid-cols-[minmax(0,1.35fr)_360px]">
        <div className="grid min-w-0 content-start gap-5">
          {currentReview ? (
            <>
              <AgentSummaryBlock summary={currentReview.agentSummary} />
              {[...listOrEmpty(currentReview.risks), ...listOrEmpty(currentReview.followUps)].length > 0 ? (
                <EvidenceSection title={t("issueDetail.evidence.risksFollowUps")}>
                  <ReviewStringList items={[...listOrEmpty(currentReview.risks), ...listOrEmpty(currentReview.followUps)]} empty={t("issueDetail.evidence.noRisksFollowUps")} />
                </EvidenceSection>
              ) : null}
              <EvidenceCommandsPanel commands={currentReview.commandsRun} />
            </>
          ) : (
            <Notice>{t("issueDetail.evidence.noCurrentReviewSnapshot")}</Notice>
          )}
        </div>
        <aside className="grid min-w-0 content-start gap-5">
          <EvidenceFactPanel
            review={currentReview}
            latestEvidence={latestEvidence}
            testEnvironment={props.testEnvironment}
            sourceNode={sourceNode}
            sourceSession={sourceSession}
            evidenceCount={evidence.length}
          />
          <KubernetesEvidenceDigest issueId={props.issueId} evidence={evidence} />
          <ReviewHistoryList
            issueId={props.issueId}
            reviews={historicalReviews}
            failures={failures}
          />
        </aside>
      </div>
    </section>
  );
}

function SessionSummarySkeleton() {
  return (
    <div className="mt-2 grid gap-2" aria-hidden="true">
      <div className="h-3 w-5/6 rounded-full bg-[color:var(--line)] shadow-[inset_0_0_0_1px_rgba(0,0,0,0.02)] motion-safe:animate-pulse" />
      <div className="h-3 w-2/3 rounded-full bg-[color:var(--line)] shadow-[inset_0_0_0_1px_rgba(0,0,0,0.02)] motion-safe:animate-pulse" />
    </div>
  );
}

function storedHumanActor(): ActorIdentity {
  const stored = getStoredAuthIdentity();
  return {
    kind: "human",
    name: stored.name || "mlhiter",
    avatarUrl: stored.avatarUrl || "",
  };
}

function commentMatchesStoredIdentity(comment: Comment): boolean {
  if (comment.authorType !== "human") return false;
  const stored = getStoredAuthIdentity();
  if (comment.authorUserId && stored.id) return comment.authorUserId === stored.id;
  return (comment.authorName || "").trim().toLowerCase() === (stored.name || "mlhiter").trim().toLowerCase();
}

function humanActor(name?: string, avatarUrl?: string): ActorIdentity {
  const stored = storedHumanActor();
  const normalizedName = name?.trim();
  return {
    kind: "human",
    name: !normalizedName || normalizedName === "me" ? stored.name : normalizedName,
    avatarUrl: avatarUrl?.trim() || stored.avatarUrl,
  };
}

function systemActor(name = "mspace"): ActorIdentity {
  return { kind: "system", name };
}

function codexActor(name = "Codex"): ActorIdentity {
  return { kind: "codex", name, avatarUrl: codexAvatarDataUrl };
}

function evidenceActor(): ActorIdentity {
  return { kind: "evidence", name: translate("issueDetail.tabs.evidence") };
}

function actorForComment(comment: Comment): ActorIdentity {
  if (comment.authorType === "human") return humanActor(comment.authorName, comment.authorAvatarUrl);
  if (comment.authorType === "agent") return codexActor(comment.authorName || "Codex");
  return systemActor(comment.authorName || "mspace");
}

function actorForAssignee(assigneeType: string, assignee: string): ActorIdentity {
  if (assigneeType === "agent") return codexActor(displayAgentName(assignee));
  return humanActor(assignee);
}

function displayAgentName(value: string): string {
  const normalized = mentionKey(value) || value.trim();
  return normalized ? normalized.charAt(0).toUpperCase() + normalized.slice(1) : "Codex";
}

function actorInitial(actor: ActorIdentity): string {
  return (actor.name?.trim().slice(0, 1).toUpperCase() || (actor.kind === "human" ? "M" : "C"));
}

function ActorMark(props: { actor: ActorIdentity; size?: "sm" | "md" }) {
  const [failed, setFailed] = useState(false);
  const size = props.size || "md";
  const imageUrl = props.actor.kind === "codex" ? codexAvatarDataUrl : props.actor.avatarUrl;
  const Icon = props.actor.kind === "evidence" ? CheckCircle2 : props.actor.kind === "system" ? CircleDot : Bot;

  useEffect(() => {
    setFailed(false);
  }, [imageUrl]);

  return (
    <div
      className={cn(
        "relative z-10 grid shrink-0 place-items-center overflow-hidden rounded-full bg-[color:var(--paper)] shadow-[0_0_0_1px_var(--line)]",
        size === "sm" ? "size-5 text-[10px] [&_[data-icon]]:size-3" : "size-8 text-[12px] [&_[data-icon]]:size-4",
        props.actor.kind === "codex" && "text-[color:var(--accent-blue)]",
        props.actor.kind === "human" && "font-semibold text-[color:var(--text)]",
        props.actor.kind === "system" && "text-[color:var(--muted)]",
        props.actor.kind === "evidence" && "text-[color:var(--success)]",
      )}
    >
      {imageUrl && !failed ? (
        <img src={imageUrl} alt="" className={cn("size-full object-cover", props.actor.kind === "codex" && "p-1")} onError={() => setFailed(true)} />
      ) : props.actor.kind === "human" ? (
        <span>{actorInitial(props.actor)}</span>
      ) : (
        <Icon data-icon />
      )}
    </div>
  );
}

function TimelineShell(props: {
  actor: ActorIdentity;
  title: React.ReactNode;
  time: string;
  children?: React.ReactNode;
  id?: string;
}) {
  return (
    <article id={props.id} className="scroll-mt-8 grid grid-cols-[32px_minmax(0,1fr)] gap-3">
      <ActorMark actor={props.actor} />
      <div className="min-w-0 pb-8">
        <div className="mb-2 flex flex-wrap items-center gap-x-2 gap-y-1">
          <div className="text-[13px] font-semibold leading-5 text-[color:var(--text)]">{props.title}</div>
          <TimeMeta value={props.time} />
        </div>
        {props.children}
      </div>
    </article>
  );
}

function CommentReactionBar(props: {
  reactions?: CommentReactionSummary[];
  pendingReaction?: string;
  onToggleReaction?: (reaction: string, reactedByMe: boolean) => void;
}) {
  const { t } = useMspaceTranslation();
  const reactions = listOrEmpty(props.reactions);
  if (reactions.length === 0) return null;

  return (
    <div className="mt-1.5 flex flex-wrap items-center gap-1">
      {reactions.map((reaction) => {
        const option = commentReactionOption(reaction.reaction);
        if (!option) return null;
        return (
          <button
            key={reaction.reaction}
            type="button"
            className={cn(
              "inline-flex h-6 items-center gap-1 rounded-full px-1.5 text-[12px] font-medium leading-none text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)] transition-[background-color,color,box-shadow,transform] duration-150 ease-out hover:bg-[color:var(--hover)] active:scale-95",
              reaction.reactedByMe && "bg-[color:var(--hover)] text-[color:var(--text)] shadow-[inset_0_0_0_1px_var(--accent-blue)]",
            )}
            aria-pressed={reaction.reactedByMe}
            title={t(option.labelKey)}
            disabled={!props.onToggleReaction || props.pendingReaction === reaction.reaction}
            onClick={() => props.onToggleReaction?.(reaction.reaction, reaction.reactedByMe)}
          >
            <span aria-hidden="true">{option.emoji}</span>
            <span>{reaction.count}</span>
          </button>
        );
      })}
      {props.onToggleReaction ? (
        <CommentReactionPicker
          reactions={reactions}
          pendingReaction={props.pendingReaction}
          onToggleReaction={props.onToggleReaction}
          triggerClassName="size-6"
          menuClassName="left-0 top-7"
        />
      ) : null}
    </div>
  );
}

function CommentReactionPicker(props: {
  reactions?: CommentReactionSummary[];
  pendingReaction?: string;
  onToggleReaction?: (reaction: string, reactedByMe: boolean) => void;
  triggerClassName?: string;
  menuClassName?: string;
}) {
  const { t } = useMspaceTranslation();
  const [menuOpen, setMenuOpen] = useState(false);
  const reactions = listOrEmpty(props.reactions);
  const reactionsByKey = useMemo(() => {
    const entries = new Map<string, CommentReactionSummary>();
    for (const reaction of reactions) {
      entries.set(reaction.reaction, reaction);
    }
    return entries;
  }, [reactions]);

  return (
    <div className="relative">
      <button
        type="button"
        className={cn(
          "grid place-items-center rounded-full text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)] transition-[background-color,color,opacity,transform] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] active:scale-95",
          props.triggerClassName || "size-7",
        )}
        aria-label={t("issueDetail.reactions.addReaction")}
        aria-expanded={menuOpen}
        onClick={() => setMenuOpen((open) => !open)}
      >
        <SmilePlus data-icon />
      </button>
      {menuOpen ? (
        <div className={cn("absolute z-30 flex gap-1 rounded-[10px] bg-[color:var(--paper)] p-1 shadow-[0_12px_36px_rgba(0,0,0,0.14),0_0_0_1px_var(--line)]", props.menuClassName || "right-0 top-8")}>
          {COMMENT_REACTION_OPTIONS.map((option) => {
            const existing = reactionsByKey.get(option.reaction);
            return (
              <button
                key={option.reaction}
                type="button"
                className={cn(
                  "grid size-7 place-items-center rounded-[7px] text-[16px] transition-[background-color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] active:scale-95",
                  existing?.reactedByMe && "bg-[color:var(--hover)] shadow-[inset_0_0_0_1px_var(--accent-blue)]",
                )}
                title={t(option.labelKey)}
                aria-pressed={Boolean(existing?.reactedByMe)}
                disabled={props.pendingReaction === option.reaction}
                onClick={() => {
                  props.onToggleReaction?.(option.reaction, Boolean(existing?.reactedByMe));
                  setMenuOpen(false);
                }}
              >
                <span aria-hidden="true">{option.emoji}</span>
              </button>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}

function CommentTimelineItem(props: {
  comment: Comment;
  agents?: AgentProfile[];
  sessions?: AgentSession[];
  canEdit?: boolean;
  isEditing?: boolean;
  editBody?: string;
  isSaving?: boolean;
  editError?: Error | null;
  onStartEdit?: () => void;
  onCancelEdit?: () => void;
  onEditBodyChange?: (value: string) => void;
  onEditReady?: (editor: Editor | null) => void;
  onEditEditorStateChange?: (editor: Editor) => void;
  onEditFocus?: (editor: Editor) => void;
  onEditBlur?: (editor: Editor) => void;
  onEditKeyDown?: (event: React.KeyboardEvent<HTMLDivElement>, editor: Editor) => void;
  onEditImageUpload?: (file: File) => Promise<{ id: string; url: string; filename: string }>;
  onSaveEdit?: () => void;
  mentionMenu?: React.ReactNode;
  helperText?: string;
  saveLabel?: string;
  canSave?: boolean;
  pendingReaction?: string;
  onToggleReaction?: (reaction: string, reactedByMe: boolean) => void;
}) {
  const { t } = useMspaceTranslation();
  const actor = actorForComment(props.comment);
  const sessionAction = parseSessionActionComment(props.comment.body);
  if (sessionAction) {
    const eventActor = actorForSessionActionComment(actor, sessionAction);
    const actionSession = props.sessions?.find((session) => sessionActionMatchesSession(sessionAction.sessionID, session.id));
    const actionAgent = actionSession ? sessionAgent(actionSession, props.agents || []) : null;
    return (
      <TimelineShell
        actor={eventActor}
        title={
          <SessionActionTitle
            actorName={eventActor.name || sessionAction.actorName || t("issueDetail.comments.someone")}
            action={sessionAction}
            agentName={actionAgent?.name || t("issueDetail.comments.agentWork")}
          />
        }
        time={props.comment.createdAt}
      />
    );
  }
  const statusTransition = parseStatusTransitionComment(props.comment.body);
  if (statusTransition) {
    const eventActor = actorForStatusTransitionComment(actor, statusTransition);
    return (
      <TimelineShell
        actor={eventActor}
        title={<StatusTransitionTitle actorName={eventActor.name || statusTransition.actorName} transition={statusTransition} />}
        time={props.comment.createdAt}
      />
    );
  }

  const title =
    actor.kind === "human" || actor.kind === "codex"
      ? t("issueDetail.comments.commented", { name: actor.name })
      : t("issueDetail.comments.updatedIssue", { name: actor.name || "mspace" });
  return (
    <TimelineShell actor={actor} title={title} time={props.comment.createdAt}>
      {props.isEditing ? (
        <div className="rounded-[10px] bg-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--line)]">
          {props.editError ? <Notice tone="danger">{props.editError.message}</Notice> : null}
          <div className="relative" data-comment-composer="true">
            <IssueDocumentEditor
              variant="comment"
              ariaLabel={t("issueDetail.comments.editAria")}
              value={props.editBody || ""}
              placeholder={t("issueDetail.comments.editPlaceholder")}
              autoFocus
              onChange={(value) => props.onEditBodyChange?.(value)}
              onReady={props.onEditReady}
              onEditorStateChange={props.onEditEditorStateChange}
              onFocus={props.onEditFocus}
              onBlur={props.onEditBlur}
              onKeyDown={props.onEditKeyDown}
              onImageUpload={props.onEditImageUpload}
            />
            {props.mentionMenu}
          </div>
          <div className="flex flex-wrap items-center justify-between gap-2 border-t border-[color:var(--line)] px-3 py-2">
            <div className="min-w-0 flex-1 text-[12px] leading-5 text-[color:var(--muted)]">{props.helperText}</div>
            <div className="flex shrink-0 items-center gap-2">
              <Button type="button" variant="ghost" size="sm" onClick={props.onCancelEdit} disabled={props.isSaving}>
                <X data-icon />
                {t("issueDetail.comments.cancel")}
              </Button>
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={props.onSaveEdit}
                disabled={props.isSaving || !props.editBody?.trim() || props.canSave === false}
              >
                <Save data-icon />
                {props.isSaving ? t("issueDetail.comments.saving") : props.saveLabel || t("issueDetail.comments.saveEdit")}
              </Button>
            </div>
          </div>
        </div>
      ) : (
        <div className="group/comment flex min-w-0 items-start justify-between gap-3">
          <div className="min-w-0 flex-1">
            <RichText agents={props.agents}>{props.comment.body}</RichText>
            {props.comment.editedAt ? (
              <div className="mt-2 text-[12px] leading-5 text-[color:var(--muted)]" title={formatAbsoluteTime(props.comment.editedAt)}>
                {t("issueDetail.comments.edited", { time: formatRelativeTime(props.comment.editedAt) })}
              </div>
            ) : null}
            <CommentReactionBar
              reactions={props.comment.reactions}
              pendingReaction={props.pendingReaction}
              onToggleReaction={props.onToggleReaction}
            />
          </div>
          <div className="flex shrink-0 items-center gap-1 opacity-70 transition-opacity duration-150 group-hover/comment:opacity-100 focus-within:opacity-100">
            {listOrEmpty(props.comment.reactions).length === 0 && props.onToggleReaction ? (
              <CommentReactionPicker
                reactions={props.comment.reactions}
                pendingReaction={props.pendingReaction}
                onToggleReaction={props.onToggleReaction}
                triggerClassName="size-7"
              />
            ) : null}
            {props.canEdit ? (
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="h-7 w-7 shrink-0"
                onClick={props.onStartEdit}
                aria-label={t("issueDetail.comments.editComment")}
              >
                <Pencil data-icon />
              </Button>
            ) : null}
          </div>
        </div>
      )}
    </TimelineShell>
  );
}

type StatusTransition = {
  target: "issue" | "task";
  taskTitle?: string;
  from: string;
  to: string;
  actorName: string;
};

type SessionAction = {
  kind: "stopped";
  sessionID: string;
  actorName: string;
  beforeStart: boolean;
};

function parseStatusTransitionComment(body: string): StatusTransition | null {
  const firstLine = body.trim().split(/\r?\n/, 1)[0] || "";
  const match = firstLine.match(/^(Issue|Task `(.+)`) status changed from `([^`]+)` to `([^`]+)` by (.+)\.$/);
  if (!match) return null;
  return {
    target: match[1].startsWith("Task") ? "task" : "issue",
    taskTitle: match[2],
    from: match[3],
    to: match[4],
    actorName: match[5],
  };
}

function parseSessionActionComment(body: string): SessionAction | null {
  const firstLine = body.trim().split(/\r?\n/, 1)[0] || "";
  const stoppedByMatch = firstLine.match(/^Stopped session `([^`]+)` by (.+)\.$/);
  if (stoppedByMatch) {
    return {
      kind: "stopped",
      sessionID: stoppedByMatch[1],
      actorName: stoppedByMatch[2].trim(),
      beforeStart: false,
    };
  }
  const legacyStoppedMatch = firstLine.match(/^Stopped session `([^`]+)`( before it started)?\.$/);
  if (!legacyStoppedMatch) return null;
  return {
    kind: "stopped",
    sessionID: legacyStoppedMatch[1],
    actorName: "",
    beforeStart: Boolean(legacyStoppedMatch[2]),
  };
}

function sessionActionMatchesSession(actionSessionID: string, sessionID: string): boolean {
  const actionID = actionSessionID.trim();
  const fullID = sessionID.trim();
  if (!actionID || !fullID) return false;
  return actionID === fullID || (actionID.length >= 8 && fullID.startsWith(actionID));
}

function actorForStatusTransitionComment(actor: ActorIdentity, transition: StatusTransition): ActorIdentity {
  if (actor.kind !== "system") return actor;
  const actorName = transition.actorName.trim();
  if (!actorName || actorName === actor.name) return actor;
  if (actorName.toLowerCase() === "codex") return codexActor(actorName);
  return { kind: "human", name: actorName };
}

function actorForSessionActionComment(actor: ActorIdentity, action: SessionAction): ActorIdentity {
  if (actor.kind !== "system") return actor;
  const actorName = action.actorName.trim();
  if (!actorName || actorName === actor.name) return actor;
  return { kind: "human", name: actorName };
}

function StatusTransitionTitle(props: { actorName: string; transition: StatusTransition }) {
  const { t } = useMspaceTranslation();
  return (
    <span className="inline-flex min-w-0 flex-wrap items-center gap-1.5">
      <span className="font-semibold text-[color:var(--text)]">{props.actorName}</span>
      <span className="font-normal text-[color:var(--muted)]">
        {props.transition.target === "task" ? t("issueDetail.comments.changedTaskStatusFrom") : t("issueDetail.comments.changedStatusFrom")}
      </span>
      <StatusBadge
        value={displayIssueStatus(props.transition.from)}
        valueLabel={issueStatusLabel(props.transition.from)}
        className="h-5 px-2 py-0 text-[11px]"
      />
      <span className="font-normal text-[color:var(--muted)]">{t("issueDetail.comments.to")}</span>
      <StatusBadge
        value={displayIssueStatus(props.transition.to)}
        valueLabel={issueStatusLabel(props.transition.to)}
        className="h-5 px-2 py-0 text-[11px]"
      />
    </span>
  );
}

function SessionActionTitle(props: { actorName: string; action: SessionAction; agentName: string }) {
  const { t } = useMspaceTranslation();
  return (
    <span className="inline-flex min-w-0 flex-wrap items-center gap-1.5" title={`Session ${props.action.sessionID}`}>
      <span className="font-semibold text-[color:var(--text)]">{props.actorName}</span>
      <span className="font-normal text-[color:var(--muted)]">{t("issueDetail.comments.stopped")}</span>
      <span className="font-semibold text-[color:var(--text)]">{props.agentName}</span>
      {props.action.beforeStart ? <span className="font-normal text-[color:var(--muted)]">{t("issueDetail.comments.beforeItStarted")}</span> : null}
    </span>
  );
}

function SessionFailureCallout(props: { logs: LogLine[]; hasAgentMessage: boolean }) {
  const { t } = useMspaceTranslation();
  const failureMessage = latestSessionFailureMessage(props.logs);
  const isPostProcessingFailure =
    props.hasAgentMessage &&
    /record source commit|constraint failed|review evidence snapshot|kubernetes evidence|collecting kubernetes evidence/i.test(failureMessage);
  const title = isPostProcessingFailure
    ? t("issueDetail.failures.postProcessingTitle")
    : props.hasAgentMessage
      ? t("issueDetail.failures.afterAgentTitle")
      : t("issueDetail.failures.sessionFailed");
  return (
    <div className="mt-3 rounded-[8px] bg-[color:var(--danger-soft)] px-3 py-2.5 text-[12px] leading-5 text-[color:var(--danger)] shadow-[inset_0_0_0_1px_var(--line)]">
      <div className="flex min-w-0 items-center gap-2 font-semibold">
        <CircleAlert data-icon className="shrink-0" />
        <span>{title}</span>
      </div>
      <p className="mt-1 text-[color:var(--danger)]">
        {isPostProcessingFailure
          ? t("issueDetail.failures.postProcessingBody")
          : t("issueDetail.failures.failedBody")}
      </p>
      {failureMessage ? (
        <div className="mt-2 grid gap-1 rounded-[7px] bg-[color:var(--paper)] px-2.5 py-2 text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]">
          <span className="text-[11px] font-medium text-[color:var(--danger)]">{t("issueDetail.failures.lastRunnerError")}</span>
          <span className="break-words font-mono text-[11px] leading-5 text-[color:var(--text)]">{failureMessage}</span>
        </div>
      ) : null}
    </div>
  );
}

function DeployAttentionCallout(props: {
  environment: IssueTestEnvironment;
  session: AgentSession;
  logs: LogLine[];
  hasAgentMessage: boolean;
}) {
  const { t } = useMspaceTranslation();
  if (props.session.status === "failed") {
    return <SessionFailureCallout logs={props.logs} hasAgentMessage={props.hasAgentMessage} />;
  }

  const namespaceStatus = props.environment.namespaceStatus || "";
  const isFailed = namespaceStatus === "deploy_failed";
  const failureMessage = latestSessionFailureMessage(props.logs);
  const title =
    namespaceStatus === "deploy_interrupted"
      ? t("issueDetail.failures.deploymentInterrupted")
      : namespaceStatus === "preview_unverified"
        ? t("issueDetail.failures.previewNotVerified")
        : t("issueDetail.failures.deploymentFailed");
  const body =
    namespaceStatus === "deploy_interrupted"
      ? t("issueDetail.failures.deploymentInterruptedBody")
      : namespaceStatus === "preview_unverified"
        ? t("issueDetail.failures.previewNotVerifiedBody")
        : t("issueDetail.failures.deploymentFailedBody");

  return (
    <div
      className={cn(
        "mt-3 rounded-[8px] px-3 py-2.5 text-[12px] leading-5 shadow-[inset_0_0_0_1px_var(--line)]",
        isFailed
          ? "bg-[color:var(--danger-soft)] text-[color:var(--danger)]"
          : "bg-[color:var(--warning-soft)] text-[color:var(--warning)]",
      )}
    >
      <div className="flex min-w-0 items-center gap-2 font-semibold">
        <CircleAlert data-icon className="shrink-0" />
        <span>{title}</span>
      </div>
      <p className={cn("mt-1", isFailed ? "text-[color:var(--danger)]" : "text-[color:var(--warning)]")}>
        {body}
      </p>
      {failureMessage ? (
        <div className="mt-2 grid gap-1 rounded-[7px] bg-[color:var(--paper)] px-2.5 py-2 text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]">
          <span className={cn("text-[11px] font-medium", isFailed ? "text-[color:var(--danger)]" : "text-[color:var(--warning)]")}>
            {t("issueDetail.failures.lastRunnerSignal")}
          </span>
          <span className="break-words font-mono text-[11px] leading-5 text-[color:var(--text)]">{failureMessage}</span>
        </div>
      ) : null}
    </div>
  );
}

function failurePhaseLabel(phase: string) {
  return translate(`issueDetail.failures.phases.${phase}`, { defaultValue: translate("issueDetail.failures.phases.default") });
}

function failureStatusLabel(status: string) {
  return translate(`issueDetail.failures.status.${status}`, { defaultValue: translate("issueDetail.failures.status.open") });
}

function failureMetaRows(failure: SessionFailure) {
  return [
    { label: translate("issueDetail.failures.failedCommand"), value: failure.failedCommand, mono: true },
    { label: translate("issueDetail.failures.cluster"), value: failure.cluster },
    { label: translate("issueDetail.failures.namespace"), value: failure.namespace, mono: true },
    { label: translate("issueDetail.failures.resource"), value: [failure.resourceKind, failure.resourceName].filter(Boolean).join("/") },
  ];
}

function failureCanRetryDeploy(failure: SessionFailure, environment: IssueTestEnvironment | null | undefined) {
  if (!environment) return false;
  if (environment.lastDeploySessionId === failure.sessionId || environment.lastCleanupSessionId === failure.sessionId) return true;
  return ["image_push", "pod_startup", "network_exposure", "preview_probe", "cleanup"].includes(failure.phase);
}

function failureContinueDraft(failure: SessionFailure, agent: AgentProfile) {
  const mention = mentionKey(agent.mention);
  const lines = [
    `@${mention} ${translate("issueDetail.failures.continueDraftTitle")}`,
    "",
    `${translate("issueDetail.failures.continueDraftPhase")}: ${failurePhaseLabel(failure.phase)}`,
    failure.failedCommand ? `${translate("issueDetail.failures.failedCommand")}: \`${failure.failedCommand}\`` : "",
    failure.errorSummary ? `${translate("issueDetail.failures.continueDraftErrorSummary")}: ${failure.errorSummary}` : "",
    failure.namespace ? `${translate("issueDetail.failures.namespace")}: \`${failure.namespace}\`` : "",
    failure.resourceName ? `${translate("issueDetail.failures.resource")}: \`${[failure.resourceKind, failure.resourceName].filter(Boolean).join("/")}\`` : "",
    "",
    translate("issueDetail.failures.continueDraftInstruction"),
  ].filter(Boolean);
  return lines.join("\n");
}

function SessionFailureCard(props: {
  failure: SessionFailure;
  session?: AgentSession;
  evidence?: DeploymentEvidence;
  review?: SessionReviewEvidence;
  compact?: boolean;
  canContinue?: boolean;
  canRetry?: boolean;
  isRetrying?: boolean;
  canStop?: boolean;
  isStopping?: boolean;
  onContinue?: () => void;
  onRetry?: () => void;
  onStop?: () => void;
}) {
  const { t } = useMspaceTranslation();
  const active = props.session ? ["queued", "running"].includes(props.session.status) : false;
  return (
    <div className={cn("grid gap-3 rounded-[10px] bg-[color:var(--danger-soft)] p-3 text-[13px] leading-5 text-[color:var(--danger)] shadow-[inset_0_0_0_1px_var(--line)]", props.compact && "bg-[color:var(--block-subtle)] text-[color:var(--text)]")}>
      <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex min-w-0 items-center gap-2 font-semibold">
            <CircleAlert data-icon className="shrink-0" />
            <span className="min-w-0 truncate">{failurePhaseLabel(props.failure.phase)}</span>
            {active ? <span className="text-[12px] font-normal text-[color:var(--warning)]">{t("issueDetail.failures.active")}</span> : null}
          </div>
          <div className="mt-1 flex min-w-0 flex-wrap gap-x-3 gap-y-1 text-[12px] text-[color:var(--muted)]">
            <span>{t("issueDetail.failures.session")} {props.failure.sessionId.slice(0, 8)}</span>
            <span>{failureStatusLabel(props.failure.status)}</span>
            {props.failure.updatedAt ? <span title={formatAbsoluteTime(props.failure.updatedAt)}>{formatRelativeTime(props.failure.updatedAt)}</span> : null}
          </div>
        </div>
        <div className="flex flex-wrap justify-end gap-2">
          {props.onContinue ? (
            <Button type="button" variant="secondary" size="sm" disabled={!props.canContinue} onClick={props.onContinue}>
              <Send data-icon />
              {t("issueDetail.failures.continue")}
            </Button>
          ) : null}
          {props.onRetry ? (
            <Button type="button" variant="secondary" size="sm" disabled={!props.canRetry || props.isRetrying} onClick={props.onRetry}>
              <Rocket data-icon />
              {props.isRetrying ? t("issueDetail.failures.queueing") : t("issueDetail.failures.retryDeploy")}
            </Button>
          ) : null}
          {props.onStop ? (
            <Button type="button" variant="ghost" size="sm" disabled={!props.canStop || props.isStopping} onClick={props.onStop}>
              <CircleStop data-icon />
              {props.isStopping ? t("issueDetail.failures.stopping") : t("issueDetail.failures.stop")}
            </Button>
          ) : null}
        </div>
      </div>
      {props.failure.errorSummary ? (
        <div className="rounded-[8px] bg-[color:var(--paper)] px-3 py-2 text-[12px] leading-5 text-[color:var(--text)] shadow-[inset_0_0_0_1px_var(--line)] [overflow-wrap:anywhere]">
          {props.failure.errorSummary}
        </div>
      ) : null}
      <ReviewMetaGrid rows={failureMetaRows(props.failure)} />
      {props.failure.errorExcerpt && !props.compact ? (
        <ReviewDetailsDisclosure label={t("issueDetail.failures.showErrorExcerpt")}>
          <div className="max-h-44 overflow-auto whitespace-pre-wrap text-[12px] leading-5 text-[color:var(--muted)] [overflow-wrap:anywhere]">{props.failure.errorExcerpt}</div>
        </ReviewDetailsDisclosure>
      ) : null}
      {(props.evidence || props.review) && !props.compact ? (
        <div className="flex flex-wrap gap-2">
          {props.evidence ? <span className="text-[12px] text-[color:var(--muted)]">{t("issueDetail.failures.kubernetesEvidenceCaptured")}</span> : null}
          {props.review ? <span className="text-[12px] text-[color:var(--muted)]">{t("issueDetail.failures.reviewEvidenceCaptured")}</span> : null}
        </div>
      ) : null}
    </div>
  );
}

function SessionFailureTimelineItem(props: {
  failure: SessionFailure;
  session?: AgentSession;
  evidence?: DeploymentEvidence;
  review?: SessionReviewEvidence;
  onContinue?: () => void;
  canContinue?: boolean;
  onRetry?: () => void;
  canRetry?: boolean;
  isRetrying?: boolean;
  onStop?: () => void;
  canStop?: boolean;
  isStopping?: boolean;
}) {
  const { t } = useMspaceTranslation();
  return (
    <TimelineShell
      actor={{ kind: "system", name: "mspace" }}
      title={
        <span className="inline-flex min-w-0 flex-wrap items-center gap-1.5">
          <span>{t("issueDetail.failures.needsAttention")}</span>
          <StatusBadge value={props.failure.phase} valueLabel={failurePhaseLabel(props.failure.phase)} className="h-5 px-2 py-0 text-[11px]" />
        </span>
      }
      time={props.failure.updatedAt || props.failure.createdAt}
    >
      <SessionFailureCard {...props} />
    </TimelineShell>
  );
}

type DeployStage = {
  id: string;
  label: string;
  status: string;
  summary: string;
  time: string;
};

const defaultDeployStages: DeployStage[] = [
  { id: "capture-evidence", label: "issueDetail.deployStages.captureEvidence", status: "pending", summary: "", time: "" },
  { id: "discover-preview", label: "issueDetail.deployStages.discoverPreview", status: "pending", summary: "", time: "" },
  { id: "probe-preview", label: "issueDetail.deployStages.probePreview", status: "pending", summary: "", time: "" },
  { id: "reconcile", label: "issueDetail.deployStages.reconcile", status: "pending", summary: "", time: "" },
];

function deployStageLabel(stage: DeployStage) {
  return stage.label.startsWith("issueDetail.") ? translate(stage.label) : stage.label;
}

function deployStagesFromLogs(logs: LogLine[]) {
  const stagesById = new Map(defaultDeployStages.map((stage) => [stage.id, stage]));
  for (const log of logs) {
    if (log.stream !== "deploy-stage") continue;
    try {
      const parsed = JSON.parse(log.message) as Partial<DeployStage>;
      if (!parsed.id) continue;
      stagesById.set(parsed.id, {
        id: parsed.id,
        label: parsed.label || stagesById.get(parsed.id)?.label || parsed.id,
        status: parsed.status || "completed",
        summary: parsed.summary || "",
        time: parsed.time || "",
      });
    } catch {
      continue;
    }
  }
  return Array.from(stagesById.values());
}

function deployStageIcon(status: string) {
  if (status === "passed" || status === "completed") return CheckCircle2;
  if (status === "failed" || status === "warning") return CircleAlert;
  return CircleDot;
}

function deployStageTone(status: string) {
  if (status === "passed" || status === "completed") return "text-[color:var(--success)]";
  if (status === "failed") return "text-[color:var(--danger)]";
  if (status === "warning") return "text-[color:var(--warning)]";
  if (status === "running") return "text-[color:var(--accent-blue)]";
  return "text-[color:var(--faint)]";
}

function deploymentNeedsAttention(environment: IssueTestEnvironment | null | undefined, session: AgentSession) {
  const status = environment?.namespaceStatus || "";
  return session.status === "failed" || ["deploy_failed", "deploy_interrupted", "preview_unverified"].includes(status);
}

function DeployTimelineItem(props: {
  session: AgentSession;
  logs: LogLine[];
  changes: WorkspaceChange[];
  agents: AgentProfile[];
  testEnvironment: IssueTestEnvironment;
  isSnapshotPending?: boolean;
  onRetry?: () => void;
  isRetrying?: boolean;
  canRetry?: boolean;
  anchorId?: string;
}) {
  const { t } = useMspaceTranslation();
  const agent = sessionAgent(props.session, props.agents);
  const stages = deployStagesFromLogs(props.logs);
  const isActive = ["queued", "running"].includes(props.session.status);
  const needsAttention = deploymentNeedsAttention(props.testEnvironment, props.session);
  const attentionTone =
    props.session.status === "failed" || props.testEnvironment.namespaceStatus === "deploy_failed" ? "danger" : "warning";
  const previewUrl = props.testEnvironment.previewUrl;
  return (
    <TimelineShell
      id={props.anchorId}
      actor={codexActor(agent.name)}
      title={
        <span className="inline-flex min-w-0 flex-wrap items-center gap-1.5">
          <span>{agent.name}</span>
          <span className="font-normal text-[color:var(--muted)]">{t("issueDetail.timeline.deployment")}</span>
          <StatusBadge value={props.testEnvironment.namespaceStatus || props.session.status} className="h-5 px-2 py-0 text-[11px]" />
        </span>
      }
      time={props.session.updatedAt || props.session.createdAt}
    >
      <div className="rounded-[10px] bg-[color:var(--block-subtle)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-2 text-[13px] font-semibold leading-5 text-[color:var(--text)]">
              {isActive ? (
                <span className="relative flex size-2 shrink-0">
                  <span className="absolute inline-flex size-full rounded-full bg-[color:var(--accent-blue)] opacity-25 motion-safe:animate-ping" />
                  <span className="relative inline-flex size-2 rounded-full bg-[color:var(--accent-blue)]" />
                </span>
              ) : needsAttention ? (
                <CircleAlert data-icon className={cn("shrink-0", attentionTone === "danger" ? "text-[color:var(--danger)]" : "text-[color:var(--warning)]")} />
              ) : (
                <CheckCircle2 data-icon className="shrink-0 text-[color:var(--success)]" />
              )}
              <span className="truncate">{isActive ? t("issueDetail.timeline.deployingTestEnvironment") : needsAttention ? t("issueDetail.timeline.deploymentNeedsAttention") : t("issueDetail.timeline.deploymentReady")}</span>
            </div>
            <div className="mt-1 flex min-w-0 flex-wrap gap-x-3 gap-y-1 text-[12px] leading-5 text-[color:var(--muted)]">
              <span className="font-mono">{props.testEnvironment.namespace || t("issueDetail.timeline.namespacePending")}</span>
              {props.testEnvironment.sourceCommitSha ? <span>{t("issueDetail.timeline.source")} {props.testEnvironment.sourceCommitSha.slice(0, 12)}</span> : null}
              <span>{t("issueDetail.timeline.session")} {props.session.id.slice(0, 8)}</span>
            </div>
          </div>
          <div className="flex flex-wrap justify-end gap-2">
            {previewUrl ? (
              <Button type="button" variant="ghost" size="sm" onClick={() => void openRichLink(previewUrl)}>
                <Globe2 data-icon />
                {t("issueDetail.timeline.preview")}
              </Button>
            ) : null}
            {props.onRetry ? (
              <Button type="button" variant="secondary" size="sm" disabled={!props.canRetry || props.isRetrying} onClick={props.onRetry}>
                <Rocket data-icon />
                {props.isRetrying ? t("issueDetail.failures.queueing") : t("issueDetail.timeline.retryDeploy")}
              </Button>
            ) : null}
          </div>
        </div>

        <div className="mt-3 grid gap-2">
          {props.isSnapshotPending ? (
            <SessionSummarySkeleton />
          ) : (
            stages.map((stage) => {
              const Icon = deployStageIcon(stage.status);
              return (
                <div key={stage.id} className="grid grid-cols-[18px_minmax(0,1fr)] gap-2 text-[12px] leading-5">
                  <Icon data-icon className={cn("mt-0.5 shrink-0", deployStageTone(stage.status))} />
                  <div className="min-w-0">
                    <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                      <span className="font-medium text-[color:var(--muted-strong)]">{deployStageLabel(stage)}</span>
                      {stage.status !== "pending" ? <span className="text-[color:var(--faint)]">{stage.status}</span> : null}
                    </div>
                    {stage.summary ? <div className="mt-0.5 text-[color:var(--muted)] [overflow-wrap:anywhere]">{stage.summary}</div> : null}
                  </div>
                </div>
              );
            })
          )}
        </div>

        {needsAttention ? (
          <DeployAttentionCallout
            environment={props.testEnvironment}
            session={props.session}
            logs={props.logs}
            hasAgentMessage={Boolean(latestAgentMessage(props.logs))}
          />
        ) : null}
        <SessionFileChanges changes={props.changes} workdir={props.session.workdir} />
      </div>
    </TimelineShell>
  );
}

function AnalysisSessionNote(props: { compact?: boolean }) {
  const { t } = useMspaceTranslation();
  return (
    <div
      className={cn(
        "flex min-w-0 items-start gap-2 rounded-[8px] bg-[color:var(--paper)] px-2.5 py-2 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]",
        props.compact ? "mt-2 py-1.5" : "mt-2",
      )}
    >
      <WandSparkles data-icon className="mt-0.5 shrink-0 text-[color:var(--muted-strong)]" />
      <span>{t("issueDetail.timeline.analysisUsesThink")}</span>
    </div>
  );
}

function SessionTimelineItem(props: {
  session: AgentSession;
  logs: LogLine[];
  changes: WorkspaceChange[];
  agents: AgentProfile[];
  isSnapshotPending?: boolean;
  hasStopAction?: boolean;
  isStopping?: boolean;
  stopError?: Error | null;
  onStop?: () => void;
  anchorId?: string;
}) {
  const { t } = useMspaceTranslation();
  const { session, logs } = props;
  const agent = sessionAgent(session, props.agents);
  const agentMessage = latestAgentMessage(logs);
  const isActive = ["queued", "running"].includes(session.status);
  const isAnalysisSession = isIssueAnalysisSession(session);
  const sessionDisplayName = isAnalysisSession ? t("issueDetail.timeline.issueAnalysis") : agent.name;
  const isEmptyCancelledSession = session.status === "cancelled" && !agentMessage && props.changes.length === 0;
  const title = isAnalysisSession
    ? isActive
      ? t("issueDetail.timeline.analysisIsRunning")
      : t("issueDetail.timeline.issueAnalysis")
    : isActive
      ? t("issueDetail.timeline.agentIsWorking", { name: agent.name })
      : agent.name;
  if (isEmptyCancelledSession && props.hasStopAction) {
    return null;
  }
  if (isEmptyCancelledSession && !props.isSnapshotPending) {
    return (
      <TimelineShell
        id={props.anchorId}
        actor={codexActor(agent.name)}
        title={
          <span className="inline-flex min-w-0 flex-wrap items-center gap-1.5">
            <span className="font-semibold text-[color:var(--text)]">{sessionDisplayName}</span>
            <span className="font-normal text-[color:var(--muted)]">{t("issueDetail.timeline.runWasCancelled")}</span>
            <StatusBadge value="cancelled" className="h-5 px-2 py-0 text-[11px]" />
          </span>
        }
        time={session.updatedAt || session.createdAt}
      />
    );
  }

  return (
    <TimelineShell
      id={props.anchorId}
      actor={codexActor(agent.name)}
      title={title}
      time={session.updatedAt || session.createdAt}
    >
      {isActive ? (
        <div>
          <div className="flex min-w-0 items-center justify-between gap-3">
            <WorkingSessionLine
              status={session.status}
              agentName={sessionDisplayName}
              runtimeMode={session.runtimeMode}
              agentStatus={session.agentStatus}
              runtimeTaskId={isAnalysisSession ? undefined : session.runtimeTaskId}
            />
            {props.onStop ? <StopSessionButton isStopping={props.isStopping} onStop={props.onStop} /> : null}
          </div>
          {props.stopError ? <div className="mt-1 text-[12px] leading-5 text-[color:var(--danger)]">{props.stopError.message}</div> : null}
          {isAnalysisSession ? <AnalysisSessionNote /> : null}
          {agentMessage ? (
            <RichText agents={props.agents} basePath={session.workdir} className="mt-3 rounded-[9px] bg-[color:var(--block-subtle)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
              {agentMessage}
            </RichText>
          ) : null}
          <SessionFileChanges changes={props.changes} workdir={session.workdir} />
        </div>
      ) : (
        <div className="rounded-[9px] bg-[color:var(--block-subtle)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
          <div className="flex items-start justify-end gap-2">
            <SessionStatusMark status={session.status} />
          </div>

          {isAnalysisSession ? <AnalysisSessionNote compact /> : null}
          {agentMessage ? (
            <RichText agents={props.agents} basePath={session.workdir} className={cn("mt-2", isAnalysisSession ? "pt-1" : "")}>
              {agentMessage}
            </RichText>
          ) : props.isSnapshotPending ? (
            <SessionSummarySkeleton />
          ) : (
            <div className={cn("mt-2 text-[14px] leading-6", missingSummaryTone(session.status))}>
              {t("issueDetail.timeline.noFinalSummary")}
            </div>
          )}

          {session.status === "failed" ? <SessionFailureCallout logs={logs} hasAgentMessage={Boolean(agentMessage)} /> : null}

          <SessionFileChanges changes={props.changes} workdir={session.workdir} />
        </div>
      )}
    </TimelineShell>
  );
}

function EvidenceTimelineItem(props: { evidence: DeploymentEvidence }) {
  const { t } = useMspaceTranslation();
  const parsed = parseEvidenceDetails(props.evidence);
  const SnapshotIcon = parsed.tone === "failed" || parsed.tone === "warning" ? CircleAlert : parsed.tone === "healthy" ? CheckCircle2 : CircleDot;
  return (
    <TimelineShell actor={evidenceActor()} title={t("issueDetail.evidence.validationEvidenceAttached")} time={props.evidence.createdAt}>
      <div className="rounded-[10px] bg-[color:var(--block-subtle)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-2">
              <SnapshotIcon
                data-icon
                className={cn(
                  "shrink-0",
                  parsed.tone === "failed" && "text-[color:var(--danger)]",
                  parsed.tone === "warning" && "text-[color:var(--warning)]",
                  parsed.tone === "healthy" && "text-[color:var(--success)]",
                  parsed.tone === "collected" && "text-[color:var(--muted)]",
                )}
              />
              <div className="min-w-0 truncate text-[13px] font-semibold leading-5 text-[color:var(--text)]">{t("issueDetail.evidence.kubernetesSnapshot")}</div>
            </div>
            <div className="mt-1 flex min-w-0 flex-wrap gap-x-3 gap-y-1 text-[12px] leading-5 text-[color:var(--muted)]">
              <EvidenceMeta label={t("issueDetail.evidence.namespace")} value={props.evidence.namespace} />
              <EvidenceMeta label={t("issueDetail.resources.context")} value={props.evidence.cluster} />
              <EvidenceMeta label={t("issueDetail.timeline.session")} value={props.evidence.sessionId.slice(0, 8)} />
            </div>
          </div>
          <EvidenceStatusPill tone={parsed.tone} />
        </div>

        {parsed.resources.length > 0 ? <EvidenceResourceGroups resources={parsed.resources} /> : null}

        {parsed.resources.length === 0 && parsed.events.length === 0 ? (
          <div
            className={cn(
              "mt-3 rounded-[8px] px-3 py-2 text-[12px] leading-5 shadow-[inset_0_0_0_1px_var(--line)]",
              parsed.tone === "failed" ? "bg-[color:var(--danger-soft)] text-[color:var(--danger)]" : "bg-[color:var(--paper)] text-[color:var(--muted-strong)]",
            )}
          >
            {parsed.tone === "failed"
              ? t("issueDetail.evidence.evidenceCollectionFailed")
              : t("issueDetail.evidence.noResourcesCaptured")}
          </div>
        ) : null}

        <EvidenceEvents events={parsed.events} />
      </div>
    </TimelineShell>
  );
}

export function IssueEvidenceSnapshotsPage() {
  const { t } = useMspaceTranslation();
  const { issueId = "" } = useParams({ strict: false }) as { issueId?: string };
  const auth = useMspaceAuth();
  const workspaceId = auth.workspace?.id || "";
  const serverWorkspaceReady = Boolean(auth.token && workspaceId);
  const issueQuery = useQuery({
    queryKey: queryKeys.workspaceIssue(workspaceId, issueId, auth.token),
    queryFn: () => controlPlaneApi.getIssue(auth.token, workspaceId, issueId),
    enabled: serverWorkspaceReady && issueId.length > 0,
    refetchInterval: 4_000,
  });

  const detail = issueQuery.data;
  const evidence = listOrEmpty(detail?.evidence);
  const sessionCount = uniqueEvidenceSessions(evidence);

  if (!detail) {
    return (
      <PageFrame title={t("issueDetail.evidence.snapshotsTitle")} subtitle={t("issueDetail.evidence.snapshotsSubtitle")}>
        <div className="text-[14px] text-[color:var(--muted)]">{issueQuery.isPending ? t("issueDetail.evidence.loadingSnapshots") : t("issueDetail.page.notFound")}</div>
      </PageFrame>
    );
  }

  return (
    <PageFrame
      title={t("issueDetail.evidence.snapshotsTitle")}
      subtitle={t("issueDetail.evidence.historicalNamespaceEvidence", { title: detail.issue.title })}
      breadcrumbs={[
        { label: "mspace", to: "/inbox" },
        { label: t("issueDetail.page.issues"), to: "/issues" },
        { label: detail.issue.title, to: "/issues/$issueId", params: { issueId }, search: issueTabSearch("evidence") },
        { label: t("issueDetail.evidence.snapshotsTitle") },
      ]}
    >
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <Button type="button" variant="ghost" size="sm" asChild>
          <Link to="/issues/$issueId" params={{ issueId }} search={issueTabSearch("evidence")}>
            <ArrowLeft data-icon />
            {t("issueDetail.page.backToEvidence")}
          </Link>
        </Button>
        {evidence.length > 0 ? (
          <InlineMeta>{t("issueDetail.evidence.snapshotsCount", { snapshots: evidence.length, snapshotSuffix: evidence.length === 1 ? "" : "s", sessions: sessionCount, sessionSuffix: sessionCount === 1 ? "" : "s" })}</InlineMeta>
        ) : null}
      </div>

      {evidence.length > 0 ? (
        <section className="relative">
          <div className="absolute bottom-0 left-4 top-0 w-px bg-[color:var(--line)]" aria-hidden="true" />
          <div className="relative">
            {evidence.map((item) => (
              <EvidenceTimelineItem key={item.id} evidence={item} />
            ))}
          </div>
        </section>
      ) : (
        <Notice>{t("issueDetail.evidence.noSnapshots")}</Notice>
      )}
    </PageFrame>
  );
}

function ReviewEvidenceTimelineItem(props: { review: SessionReviewEvidence }) {
  const { t } = useMspaceTranslation();
  const review = props.review;
  return (
    <TimelineShell actor={evidenceActor()} title={t("issueDetail.evidence.reviewEvidenceAttached")} time={review.updatedAt || review.createdAt}>
      <div className="grid gap-3 rounded-[10px] bg-[color:var(--block-subtle)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <ReviewStatusPill status={reviewStatusForPacket(review)} />
          <span className="font-mono text-[12px] leading-5 text-[color:var(--text)]">{t("issueDetail.timeline.session")} {review.sessionId.slice(0, 8)}</span>
          {review.sourceCommitSha ? <span className="font-mono text-[12px] leading-5 text-[color:var(--muted)]">{review.sourceCommitSha.slice(0, 12)}</span> : null}
        </div>
        {review.agentSummary ? (
          <div className="rounded-[8px] bg-[color:var(--paper)] px-3 py-2 shadow-[inset_0_0_0_1px_var(--line)]">
            <RichText className="text-[13px] leading-6">{review.agentSummary}</RichText>
          </div>
        ) : null}
        <ReviewMetaGrid
          rows={[
            { label: t("issueDetail.evidence.sourceCommit"), value: review.sourceCommitSha, mono: true },
            { label: t("issueDetail.evidence.sourceSession"), value: review.sourceSessionId, mono: true },
            { label: t("issueDetail.evidence.branch"), value: review.branch, mono: true },
            { label: t("issueDetail.evidence.namespace"), value: review.namespace, mono: true },
            { label: t("issueDetail.evidence.cleanup"), value: review.cleanupStatus },
          ]}
        />
        {[...listOrEmpty(review.risks), ...listOrEmpty(review.followUps)].length > 0 ? (
          <ReviewDetailsDisclosure label={t("issueDetail.evidence.showRisksFollowUps")}>
            <ReviewStringList items={[...listOrEmpty(review.risks), ...listOrEmpty(review.followUps)]} empty={t("issueDetail.evidence.noRisksFollowUps")} />
          </ReviewDetailsDisclosure>
        ) : null}
        {review.commandsRun.length > 0 ? (
          <ReviewDetailsDisclosure label={t("issueDetail.evidence.showCommandEvidence")}>
            <CommandEvidenceList commands={review.commandsRun} />
          </ReviewDetailsDisclosure>
        ) : null}
      </div>
    </TimelineShell>
  );
}

export function IssueEvidenceHistoryPage() {
  const { t } = useMspaceTranslation();
  const { issueId = "" } = useParams({ strict: false }) as { issueId?: string };
  const auth = useMspaceAuth();
  const workspaceId = auth.workspace?.id || "";
  const serverWorkspaceReady = Boolean(auth.token && workspaceId);
  const issueQuery = useQuery({
    queryKey: queryKeys.workspaceIssue(workspaceId, issueId, auth.token),
    queryFn: () => controlPlaneApi.getIssue(auth.token, workspaceId, issueId),
    enabled: serverWorkspaceReady && issueId.length > 0,
    refetchInterval: 4_000,
  });

  const detail = issueQuery.data;
  const reviews = listOrEmpty(detail?.reviewEvidence);
  const failures = listOrEmpty(detail?.failures);
  const evidence = listOrEmpty(detail?.evidence);
  const sessions = listOrEmpty(detail?.sessions);
  const entries = [
    ...failures.map((failure) => ({ kind: "failure" as const, id: failure.id, time: failure.updatedAt || failure.createdAt, failure })),
    ...reviews.map((review) => ({ kind: "review" as const, id: review.id || review.sessionId, time: review.updatedAt || review.createdAt, review })),
  ].sort((a, b) => new Date(b.time || 0).getTime() - new Date(a.time || 0).getTime());

  if (!detail) {
    return (
      <PageFrame title={t("issueDetail.evidence.historyTitle")} subtitle={t("issueDetail.evidence.historySubtitle")}>
        <div className="text-[14px] text-[color:var(--muted)]">{issueQuery.isPending ? t("issueDetail.evidence.loadingHistory") : t("issueDetail.page.notFound")}</div>
      </PageFrame>
    );
  }

  return (
    <PageFrame
      title={t("issueDetail.evidence.historyTitle")}
      subtitle={t("issueDetail.evidence.olderReviewEvidenceFor", { title: detail.issue.title })}
      breadcrumbs={[
        { label: "mspace", to: "/inbox" },
        { label: t("issueDetail.page.issues"), to: "/issues" },
        { label: detail.issue.title, to: "/issues/$issueId", params: { issueId }, search: issueTabSearch("evidence") },
        { label: t("issueDetail.evidence.historyTitle") },
      ]}
    >
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <Button type="button" variant="ghost" size="sm" asChild>
          <Link to="/issues/$issueId" params={{ issueId }} search={issueTabSearch("evidence")}>
            <ArrowLeft data-icon />
            {t("issueDetail.page.backToEvidence")}
          </Link>
        </Button>
        {entries.length > 0 ? <InlineMeta>{t("issueDetail.evidence.itemCount", { count: entries.length, suffix: entries.length === 1 ? "" : "s" })}</InlineMeta> : null}
      </div>

      {entries.length > 0 ? (
        <section className="relative">
          <div className="absolute bottom-0 left-4 top-0 w-px bg-[color:var(--line)]" aria-hidden="true" />
          <div className="relative">
            {entries.map((entry) => {
              if (entry.kind === "failure") {
                const session = sessions.find((item) => item.id === entry.failure.sessionId);
                const failureEvidence = evidence.find((item) => item.id === entry.failure.evidenceId || item.sessionId === entry.failure.sessionId);
                const review = reviews.find((item) => item.id === entry.failure.reviewEvidenceId || item.sessionId === entry.failure.sessionId);
                return (
                  <SessionFailureTimelineItem
                    key={`failure-${entry.id}`}
                    failure={entry.failure}
                    session={session}
                    evidence={failureEvidence}
                    review={review}
                  />
                );
              }
              return <ReviewEvidenceTimelineItem key={`review-${entry.id}`} review={entry.review} />;
            })}
          </div>
        </section>
      ) : (
        <Notice>{t("issueDetail.evidence.noEarlierAttempts")}</Notice>
      )}
    </PageFrame>
  );
}

function IssueTaskList(props: {
  tasks: IssueListItem[];
  completedCount: number;
  newTaskTitle: string;
  isCreating: boolean;
  createError?: Error | null;
  updatingTaskId: string;
  updateError?: Error | null;
  deletingTaskId: string;
  deleteError?: Error | null;
  canCreate: boolean;
  onNewTaskTitleChange: (value: string) => void;
  onCreateTask: () => void;
  onToggleTask: (task: IssueListItem) => void;
  onDeleteTask: (task: IssueListItem) => void;
}) {
  const { t } = useMspaceTranslation();

  return (
    <div className="mt-6 border-t border-[color:var(--line)] pt-4">
      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        <div className="inline-flex min-w-0 items-center gap-2">
          <ListChecks data-icon className="text-[color:var(--muted)]" />
          <h2 className="text-[13px] font-semibold leading-5 text-[color:var(--muted-strong)]">{t("issueDetail.tasks.title")}</h2>
        </div>
        <div className="text-[12px] leading-5 text-[color:var(--muted)]">
          {props.completedCount}/{props.tasks.length}
        </div>
      </div>

      {props.updateError ? <Notice tone="danger">{props.updateError.message}</Notice> : null}
      {props.deleteError ? <Notice tone="danger">{props.deleteError.message}</Notice> : null}
      {props.tasks.length > 0 ? (
        <div className="divide-y divide-[color:var(--line)]">
          {props.tasks.map((task) => {
            const completed = isClosedIssueStatus(task.status);
            const updating = props.updatingTaskId === task.id;
            const deleting = props.deletingTaskId === task.id;
            const busy = updating || deleting;
            return (
              <div key={task.id} className="grid grid-cols-[28px_minmax(0,1fr)_auto_auto] items-center gap-2 py-2">
                <button
                  type="button"
                  aria-label={completed ? t("issueDetail.tasks.markOpen") : t("issueDetail.tasks.markComplete")}
                  title={completed ? t("issueDetail.tasks.markOpenTitle") : t("issueDetail.tasks.completeTitle")}
                  className={cn(
                    "grid size-7 place-items-center rounded-[7px] transition-[background-color,color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] active:scale-95",
                    completed ? "text-[color:var(--success)]" : "text-[color:var(--muted)]",
                    updating && "opacity-60",
                  )}
                  disabled={busy}
                  onClick={() => props.onToggleTask(task)}
                >
                  {completed ? <CheckCircle2 data-icon /> : <CircleDot data-icon />}
                </button>
                <div className="min-w-0">
                  <div className={cn("truncate text-[14px] leading-6", completed ? "text-[color:var(--muted)] line-through decoration-[color:var(--faint)]" : "text-[color:var(--text)]")}>
                    {task.title}
                  </div>
                  {task.body ? <div className="line-clamp-1 text-[12px] leading-5 text-[color:var(--muted)]">{task.body}</div> : null}
                </div>
                <StatusBadge value={displayIssueStatus(task.status)} valueLabel={issueStatusLabel(task.status)} />
                <button
                  type="button"
                  aria-label={t("issueDetail.tasks.deleteTask")}
                  title={deleting ? t("issueDetail.tasks.deletingTask") : t("issueDetail.tasks.deleteTask")}
                  className={cn(
                    "grid size-7 place-items-center rounded-[7px] text-[color:var(--faint)] transition-[background-color,color,transform,opacity] duration-150 ease-out hover:bg-[color:var(--danger-soft)] hover:text-[color:var(--danger)] active:scale-95",
                    busy && "opacity-60",
                  )}
                  disabled={busy}
                  onClick={() => props.onDeleteTask(task)}
                >
                  <Trash2 data-icon />
                </button>
              </div>
            );
          })}
        </div>
      ) : (
        <div className="py-2 text-[13px] leading-6 text-[color:var(--muted)]">{t("issueDetail.tasks.noTasks")}</div>
      )}

      <form
        className="mt-3 flex gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          props.onCreateTask();
        }}
      >
        <Input
          value={props.newTaskTitle}
          onChange={(event) => props.onNewTaskTitleChange(event.target.value)}
          placeholder={t("issueDetail.tasks.addPlaceholder")}
        />
        <Button type="submit" variant="secondary" size="sm" disabled={!props.canCreate}>
          <Plus data-icon />
          {props.isCreating ? t("issueDetail.tasks.adding") : t("issueDetail.tasks.add")}
        </Button>
      </form>
      {props.createError ? <div className="mt-2 text-[12px] leading-5 text-[color:var(--danger)]">{props.createError.message}</div> : null}
    </div>
  );
}

function MetaLine(props: { label: string; value: string }) {
  return (
    <div className="grid min-w-0 grid-cols-[86px_minmax(0,1fr)] items-baseline gap-2">
      <div className="text-[12px] leading-5 text-[color:var(--muted)]">{props.label}</div>
      <div className="min-w-0 break-words text-[12px] leading-5 text-[color:var(--muted-strong)]">{props.value}</div>
    </div>
  );
}

function MetaIdentityLine(props: { label: string; actor: ActorIdentity }) {
  const { t } = useMspaceTranslation();

  return (
    <div className="grid min-w-0 grid-cols-[86px_minmax(0,1fr)] items-center gap-2">
      <div className="text-[12px] leading-5 text-[color:var(--muted)]">{props.label}</div>
      <div className="flex min-w-0 items-center gap-1.5 text-[12px] leading-5 text-[color:var(--muted-strong)]">
        <ActorMark actor={props.actor} size="sm" />
        <span className="min-w-0 truncate">{props.actor.name || t("issueDetail.header.unassigned")}</span>
      </div>
    </div>
  );
}

function SidebarSection(props: { title: string; children: React.ReactNode }) {
  return (
    <section className="grid gap-2.5">
      <h2 className="text-[12px] font-medium leading-5 text-[color:var(--faint)]">{props.title}</h2>
      {props.children}
    </section>
  );
}

function EnvironmentMetaLine(props: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid min-w-0 grid-cols-[88px_minmax(0,1fr)] items-baseline gap-2">
      <div className="text-[12px] leading-5 text-[color:var(--muted)]">{props.label}</div>
      <div className="min-w-0 text-[12px] leading-5 text-[color:var(--muted-strong)]">{props.children}</div>
    </div>
  );
}

function EnvironmentSessionLink(props: { label: string; sessionId: string; sessions: AgentSession[] }) {
  const { t } = useMspaceTranslation();
  const sessionId = props.sessionId.trim();
  const session = props.sessions.find((item) => item.id === sessionId);
  if (!sessionId) {
    return <span className="text-[color:var(--faint)]">{t("issueDetail.environment.notQueued")}</span>;
  }
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-1.5">
      <Button asChild variant="ghost" size="sm" className="-ml-2 h-7 max-w-full px-2">
        <Link to="/sessions/$sessionId" params={{ sessionId }} title={`${props.label} session ${sessionId}`}>
          <History data-icon />
          <span className="font-mono">{sessionId.slice(0, 8)}</span>
        </Link>
      </Button>
      {session ? <StatusBadge value={session.status} className="h-5 px-2 py-0 text-[11px]" /> : null}
    </div>
  );
}

function IssueTestEnvironmentPanel(props: {
  environment: IssueTestEnvironment | null;
  cluster?: Cluster;
  sessions: AgentSession[];
  hasActiveSession: boolean;
  disabled?: boolean;
  disabledReason?: string;
  startError?: Error | null;
  cleanupError?: Error | null;
  retainError?: Error | null;
  isStarting: boolean;
  isCleaning: boolean;
  isRetaining: boolean;
  onStartDeploy: () => void;
  onCleanup: () => void;
  onRetain: () => void;
}) {
  const { t } = useMspaceTranslation();
  const environment = props.environment;
  const namespaceStatus = environment?.namespaceStatus || "not_requested";
  const namespaceBadgeValue = namespaceStatus === "active" ? "open" : namespaceStatus;
  const cleanupStatus = environment?.cleanupStatus || "not_decided";
  const failed = ["deploy_failed", "cleanup_failed"].includes(namespaceStatus);
  const needsAttention = failed || ["deploy_interrupted", "preview_unverified"].includes(namespaceStatus);
  const changing = ["planned", "deploying", "cleanup_requested"].includes(namespaceStatus);
  const StatusIcon = failed || needsAttention ? CircleAlert : namespaceStatus === "active" ? CheckCircle2 : changing ? Clock3 : CircleDot;
  const canCleanup =
    Boolean(environment) &&
    !props.disabled &&
    !props.hasActiveSession &&
    !props.isCleaning &&
    environment?.namespaceStatus !== "cleaned" &&
    environment?.cleanupStatus !== "cleaned";
  const canRetain =
    Boolean(environment) &&
    !props.disabled &&
    !props.hasActiveSession &&
    !props.isRetaining &&
    environmentHasRetainableNamespace(environment);

  return (
    <div className="grid gap-2.5">
      {props.startError ? <Notice tone="danger">{props.startError.message}</Notice> : null}
      {props.cleanupError ? <Notice tone="danger">{props.cleanupError.message}</Notice> : null}
      {props.retainError ? <Notice tone="danger">{props.retainError.message}</Notice> : null}

      <div className="rounded-[10px] bg-[color:var(--block-subtle)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
        <div className="flex min-w-0 items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-2 text-[12px] font-semibold leading-5 text-[color:var(--muted-strong)]">
              <StatusIcon
                data-icon
                className={cn(
                  "shrink-0",
                  namespaceStatus === "active"
                    ? "text-[color:var(--success)]"
                    : failed
                      ? "text-[color:var(--danger)]"
                      : needsAttention || changing
                        ? "text-[color:var(--warning)]"
                        : "text-[color:var(--faint)]",
                )}
              />
              <span>{t("issueDetail.environment.issueTestNamespace")}</span>
            </div>
            <div className="mt-1 min-w-0 break-all font-mono text-[13px] leading-5 text-[color:var(--text)]">
              {environment?.namespace || t("issueDetail.environment.notCreated")}
            </div>
          </div>
          <StatusBadge value={namespaceBadgeValue} valueLabel={namespaceStatusLabel(namespaceStatus)} className="h-6 shrink-0 px-2 py-0 text-[11px]" />
        </div>

        <div className="mt-3 grid gap-2">
          <EnvironmentMetaLine label={t("issueDetail.environment.cluster")}>
            <span className="break-words">{props.cluster?.name || environment?.clusterId || t("issueDetail.environment.notSelected")}</span>
          </EnvironmentMetaLine>
          <EnvironmentMetaLine label={t("issueDetail.environment.context")}>
            <span className="break-words">{environment?.kubeContext || t("issueDetail.environment.defaultContext")}</span>
          </EnvironmentMetaLine>
          <EnvironmentMetaLine label={t("issueDetail.environment.exposure")}>
            <span>{previewStrategy(environment)}</span>
          </EnvironmentMetaLine>
          <EnvironmentMetaLine label={t("issueDetail.environment.decision")}>
            <StatusBadge value={cleanupStatus} valueLabel={cleanupDecisionLabel(cleanupStatus)} className="h-5 px-2 py-0 text-[11px]" />
          </EnvironmentMetaLine>
          <EnvironmentMetaLine label={t("issueDetail.environment.deploy")}>
            <EnvironmentSessionLink label={t("issueDetail.environment.deploy")} sessionId={environment?.lastDeploySessionId || ""} sessions={props.sessions} />
          </EnvironmentMetaLine>
          <EnvironmentMetaLine label={t("issueDetail.environment.cleanup")}>
            <EnvironmentSessionLink label={t("issueDetail.environment.cleanup")} sessionId={environment?.lastCleanupSessionId || ""} sessions={props.sessions} />
          </EnvironmentMetaLine>
          <EnvironmentMetaLine label={t("issueDetail.environment.source")}>
            <span className="font-mono">{environment?.sourceCommitSha ? environment.sourceCommitSha.slice(0, 12) : t("issueDetail.environment.notSelected")}</span>
          </EnvironmentMetaLine>
          <EnvironmentMetaLine label={t("issueDetail.environment.preview")}>
            {environment?.previewUrl ? (
              <button
                type="button"
                className="inline-flex min-w-0 max-w-full items-center gap-1.5 rounded-[6px] px-1 py-1 text-left text-[12px] font-medium leading-5 text-[color:var(--accent-blue)] hover:bg-[color:var(--hover)] hover:text-[color:var(--text)]"
                onClick={() => void openRichLink(environment.previewUrl)}
              >
                <Globe2 data-icon className="shrink-0" />
                <span className="min-w-0 break-all">{environment.previewUrl}</span>
              </button>
            ) : (
              <span className="text-[color:var(--faint)]">{t("issueDetail.environment.notAvailable")}</span>
            )}
          </EnvironmentMetaLine>
          <EnvironmentMetaLine label={t("issueDetail.environment.checked")}>
            {environment?.updatedAt ? (
              <span title={formatAbsoluteTime(environment.updatedAt)}>{formatRelativeTime(environment.updatedAt)}</span>
            ) : (
              <span className="text-[color:var(--faint)]">{t("issueDetail.environment.notChecked")}</span>
            )}
          </EnvironmentMetaLine>
        </div>
      </div>

      <div className="flex flex-wrap gap-2">
        <Button type="button" variant="secondary" size="sm" disabled={props.disabled || props.hasActiveSession || props.isStarting} onClick={props.onStartDeploy}>
          <Rocket data-icon />
          {environment ? (props.isStarting ? t("issueDetail.environment.queueing") : t("issueDetail.environment.deployAgain")) : t("issueDetail.environment.deployTestEnv")}
        </Button>
        {environment ? (
          <>
            <Button type="button" variant="ghost" size="sm" disabled={!canCleanup} onClick={props.onCleanup}>
              <Trash2 data-icon />
              {props.isCleaning ? t("issueDetail.environment.queueing") : t("issueDetail.environment.cleanupNamespace")}
            </Button>
            <Button type="button" variant="ghost" size="sm" disabled={!canRetain} onClick={props.onRetain}>
              <Bug data-icon />
              {props.isRetaining ? t("issueDetail.environment.saving") : t("issueDetail.environment.retainForDebug")}
            </Button>
          </>
        ) : null}
      </div>
      {props.disabled ? (
        <div className="text-[12px] leading-5 text-[color:var(--muted)]">
          {props.disabledReason || t("issueDetail.environment.bridgeDisabled")}
        </div>
      ) : null}
    </div>
  );
}

function IssueLifecycleActions(props: {
  status: string;
  isPending: boolean;
  pendingStatus?: string;
  onCloseIssue: () => void;
  onCloseNotPlanned: () => void;
  onReopenForChanges: () => void;
}) {
  const { t } = useMspaceTranslation();
  const displayStatus = displayIssueStatus(props.status);
  const isClosed = isIssueClosedForLifecycle(displayStatus);
  const pendingStatus = props.isPending ? props.pendingStatus : "";

  return (
    <div className="flex min-w-0 items-center" aria-label={t("issueDetail.lifecycle.issueActions")}>
      {isClosed ? (
        <Button
          type="button"
          variant="secondary"
          size="sm"
          className="h-8 min-h-8"
          disabled={props.isPending}
          onClick={props.onReopenForChanges}
        >
          <CircleDot data-icon />
          {pendingStatus === "changes_requested" ? t("issueDetail.lifecycle.reopening") : t("issueDetail.lifecycle.reopenForChanges")}
        </Button>
      ) : (
        <div className="inline-flex overflow-hidden rounded-[7px] bg-[color:var(--surface)] shadow-[0_0_0_1px_var(--line)]">
          <button
            type="button"
            className="inline-flex h-8 items-center gap-1.5 px-3 text-[13px] font-medium leading-5 text-[color:var(--text)] transition-[background-color,color,opacity] duration-150 ease-out hover:bg-[color:var(--hover)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)] disabled:pointer-events-none disabled:opacity-50"
            disabled={props.isPending}
            onClick={props.onCloseIssue}
          >
            <CheckCircle2 data-icon />
            {pendingStatus === "closed" ? t("issueDetail.lifecycle.closing") : t("issueDetail.lifecycle.close")}
          </button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button
                type="button"
                className="grid h-8 w-8 place-items-center border-l border-[color:var(--line)] text-[color:var(--muted)] transition-[background-color,color,opacity] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)] disabled:pointer-events-none disabled:opacity-50"
                disabled={props.isPending}
                aria-label={t("issueDetail.lifecycle.moreCloseActions")}
              >
                <ChevronDown data-icon />
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-64">
              <DropdownMenuLabel>{t("issueDetail.lifecycle.closeIssueAs")}</DropdownMenuLabel>
              <DropdownMenuItem
                disabled={props.isPending}
                onSelect={props.onCloseNotPlanned}
              >
                <X data-icon />
                <span className="grid gap-0.5">
                  <span>{pendingStatus === "cancelled" ? t("issueDetail.lifecycle.closing") : t("issueDetail.lifecycle.closeAsNotPlanned")}</span>
                  <span className="text-[12px] leading-4 text-[color:var(--muted)]">{t("issueDetail.lifecycle.closeAsNotPlannedDescription")}</span>
                </span>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      )}
    </div>
  );
}

function AgentMentionMenu(props: {
  agents: AgentProfile[];
  activeIndex: number;
  position: MentionMenuPosition;
  onActiveIndexChange: (index: number) => void;
  onSelect: (agent: AgentProfile) => void;
}) {
  const { t } = useMspaceTranslation();
  return (
    <div
      id="issue-agent-mention-menu"
      role="listbox"
      aria-label={t("issueDetail.mentions.ariaLabel")}
      style={{ top: props.position.top, left: props.position.left, width: props.position.width }}
      className="absolute z-[90] max-h-72 overflow-y-auto overflow-x-hidden rounded-[9px] bg-[color:var(--paper)] p-1 shadow-[0_18px_56px_rgba(0,0,0,0.16),0_0_0_1px_var(--line)]"
    >
      <div className="px-2 py-1 text-[11px] font-medium leading-4 text-[color:var(--faint)]">{t("issueDetail.mentions.mentionAgent")}</div>
      {props.agents.map((agent, index) => {
        const active = index === props.activeIndex;
        return (
          <button
            id={agentMentionOptionId(agent)}
            key={agent.id}
            type="button"
            role="option"
            aria-selected={active}
            className={cn(
              "flex min-h-10 w-full items-center gap-2 rounded-[7px] px-2 py-1.5 text-left text-[13px] outline-none transition-[background-color] duration-150 ease-out",
              active ? "bg-[color:var(--selection)]" : "hover:bg-[color:var(--hover)]",
            )}
            onMouseEnter={() => props.onActiveIndexChange(index)}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => props.onSelect(agent)}
          >
            <span className="grid size-7 shrink-0 place-items-center rounded-[7px] bg-[color:var(--block)] text-[color:var(--muted)]">
              <Bot data-icon />
            </span>
            <span className="grid min-w-0 flex-1">
              <span className="truncate font-medium leading-5 text-[color:var(--text)]">{agent.mention}</span>
              <span className="truncate text-[12px] leading-4 text-[color:var(--muted)]">{agent.description || agent.name}</span>
            </span>
          </button>
        );
      })}
    </div>
  );
}

function LabelEditor(props: {
  labels: IssueLabel[];
  options: IssueLabelDefinition[];
  triageStatus: string;
  isPending: boolean;
  error?: Error | null;
  onChange: (labelKeys: string[]) => void;
}) {
  const { t } = useMspaceTranslation();
  const typeOptions = issueLabelOptionsByDimension(props.options, "type");
  const priorityOptions = issueLabelOptionsByDimension(props.options, "priority");
  const selectedType = selectedIssueLabelKey(props.labels, "type");
  const selectedPriority = selectedIssueLabelKey(props.labels, "priority");

  function setDimension(dimension: string, key: string) {
    props.onChange(nextIssueLabelSelection(props.labels, dimension, key));
  }

  return (
    <div className="grid gap-1.5">
      <LabelDimensionPicker
        title={t("issueDetail.header.type")}
        labels={typeOptions}
        value={selectedType}
        emptyLabel={t("issueDetail.header.noType")}
        pending={!selectedType && props.triageStatus === "pending"}
        disabled={props.isPending}
        onChange={(key) => setDimension("type", key)}
      />
      <LabelDimensionPicker
        title={t("issueDetail.header.priority")}
        labels={priorityOptions}
        value={selectedPriority}
        emptyLabel={t("issueDetail.header.noPriority")}
        disabled={props.isPending}
        onChange={(key) => setDimension("priority", key)}
      />
      {props.error ? <div className="text-[12px] leading-5 text-[color:var(--danger)]">{props.error.message}</div> : null}
    </div>
  );
}

function LabelDimensionPicker(props: {
  title: string;
  labels: IssueLabelDefinition[];
  value: string;
  emptyLabel: string;
  pending?: boolean;
  disabled: boolean;
  onChange: (key: string) => void;
}) {
  const { t } = useMspaceTranslation();
  const selectValue = props.value || "none";
  const selectedLabel = props.labels.find((label) => label.key === props.value);

  return (
    <div className="grid grid-cols-[86px_minmax(0,1fr)] items-center gap-2">
      <div className="text-[12px] leading-5 text-[color:var(--muted)]">{props.title}</div>
      <div className="min-w-0">
        {props.pending ? (
          <span className="inline-flex h-7 max-w-full items-center gap-1.5 rounded-[6px] px-1 text-[12px] leading-4 text-[color:var(--muted)]">
            <span className="size-1.5 shrink-0 rounded-full bg-[color:var(--faint)]" />
            <span className="truncate">{t("issues.classifying")}</span>
          </span>
        ) : (
          <Select value={selectValue} onValueChange={(key) => props.onChange(key === "none" ? "" : key)} disabled={props.disabled}>
            <SelectTrigger className={labelSelectClass(Boolean(props.value))}>
              <IssueLabelSelectValue label={selectedLabel} fallback={props.emptyLabel} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="none">{props.emptyLabel}</SelectItem>
              {props.labels.map((label) => (
                <SelectItem key={label.key} value={label.key}>
                  <IssueLabelOptionLabel label={label} />
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </div>
    </div>
  );
}

function labelSelectClass(hasValue: boolean) {
  return cn(
    "h-7 min-h-7 w-full rounded-[6px] bg-transparent px-1 py-1 text-[12px] leading-4 shadow-none hover:bg-[color:var(--hover)] focus:bg-[color:var(--hover)] focus:shadow-[inset_0_0_0_1px_var(--line)] data-[state=open]:bg-[color:var(--hover)] data-[state=open]:shadow-[inset_0_0_0_1px_var(--line)] [&_svg]:size-3.5",
    hasValue ? "font-medium text-[color:var(--muted-strong)]" : "font-normal text-[color:var(--faint)]",
  );
}

function IssueHeaderMeta(props: {
  projectName: string;
  status: string;
  typeLabel?: IssueLabel;
  priorityLabel?: IssueLabel;
  triageStatus: string;
  assignee: ActorIdentity;
  onProjectClick?: () => void;
}) {
  const { t } = useMspaceTranslation();

  return (
    <div className="flex flex-wrap items-center gap-2">
      <HeaderMetaBadge label={t("issueDetail.header.project")} value={props.projectName} onClick={props.onProjectClick} />
      <StatusBadge
        value={displayIssueStatus(props.status)}
        valueLabel={issueStatusLabel(props.status)}
        label={t("issueDetail.header.status")}
        className="h-6 px-2.5 text-[12px]"
      />
      {props.typeLabel ? (
        <IssueLabelBadge label={props.typeLabel} prefix={t("issueDetail.header.type")} className="h-6 px-2.5 text-[12px]" />
      ) : (
        <HeaderMetaBadge value={props.triageStatus === "pending" ? t("issueDetail.header.classifyingType") : t("issueDetail.header.noType")} muted />
      )}
      {props.priorityLabel ? (
        <IssueLabelBadge label={props.priorityLabel} prefix={t("issueDetail.header.priority")} className="h-6 px-2.5 text-[12px]" />
      ) : (
        <HeaderMetaBadge value={t("issueDetail.header.noPriority")} muted />
      )}
      <HeaderMetaBadge label={t("issueDetail.header.assignee")} value={props.assignee.name || t("issueDetail.header.unassigned")} />
    </div>
  );
}

function HeaderMetaBadge(props: { label?: string; value: string; muted?: boolean; onClick?: () => void }) {
  const className = cn(
    "inline-flex h-6 max-w-full items-center gap-1.5 rounded-full px-2.5 text-[12px] font-medium leading-4 shadow-[inset_0_0_0_1px_var(--line)]",
    props.muted
      ? "bg-[color:var(--block)] text-[color:var(--muted)]"
      : "bg-[color:var(--surface)] text-[color:var(--muted-strong)]",
    props.onClick && "transition-colors hover:bg-[color:var(--hover)]",
  );
  const content = (
    <>
      {props.label ? <span className="shrink-0 text-[color:var(--faint)]">{props.label}</span> : null}
      <span className="truncate">{props.value}</span>
    </>
  );
  if (props.onClick) {
    return (
      <button type="button" className={className} title={props.label ? `${props.label}: ${props.value}` : props.value} onClick={props.onClick}>
        {content}
      </button>
    );
  }
  return (
    <span className={className} title={props.label ? `${props.label}: ${props.value}` : props.value}>
      {content}
    </span>
  );
}

function runbookStatusLabel(status: string) {
  if (status === "learned") return translate("issueDetail.runbook.learned");
  if (status === "stale") return translate("issueDetail.runbook.stale");
  return translate("issueDetail.runbook.notLearned");
}

function runbookUpdatedLabel(value: string) {
  return value ? formatRelativeTime(value) : translate("issueDetail.runbook.notAvailable");
}

function projectDisplayName(project: Project | null | undefined) {
  return project?.name || translate("common.noProject");
}

function projectRepositoryLabel(project: Project | null | undefined) {
  if (!project) return "";
  return project.sourceType === "github" ? project.remoteUrl || project.repoPath : project.repoPath || project.remoteUrl;
}

function detectGitHubRepoUrl(text: string): string {
  const normalized = text.replace(/[()[\]<>`"']/g, " ");
  const candidates = normalized.split(/\s+/);
  for (const candidate of candidates) {
    const trimmed = candidate.replace(/[.,;:!?]+$/g, "");
    if (!trimmed.includes("github.com/")) continue;
    try {
      const parsed = new URL(trimmed.startsWith("http") ? trimmed : `https://${trimmed}`);
      if (parsed.hostname.toLowerCase() !== "github.com") continue;
      const parts = parsed.pathname.replace(/^\/+|\/+$/g, "").split("/");
      if (parts.length < 2) continue;
      return `https://github.com/${parts[0]}/${parts[1].replace(/\.git$/, "")}.git`;
    } catch {
      continue;
    }
  }
  return "";
}

function ProjectRunbookModal(props: {
  projectName: string;
  projectStatus: string;
  projectUpdatedAt: string;
  runbook?: ProjectRunbook;
  isLoading: boolean;
  error?: Error | null;
  onClose: () => void;
}) {
  const { t } = useMspaceTranslation();
  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") props.onClose();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [props.onClose]);

  const content = props.runbook?.content || "";
  const hasContent = Boolean(content.trim());
  const status = runbookStatusLabel(props.runbook?.status || props.projectStatus);
  const updatedAt = props.runbook?.updatedAt || props.projectUpdatedAt;

  return (
    <div className="fixed inset-0 z-[80] grid place-items-center bg-[rgba(31,31,31,0.18)] px-5 py-8">
      <button type="button" aria-label={t("issueDetail.runbook.closeBackdrop")} className="absolute inset-0 cursor-default" onClick={props.onClose} />
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="project-runbook-title"
        className="relative max-h-[calc(100vh-40px)] w-full max-w-[780px] overflow-auto rounded-[12px] bg-[color:var(--paper)] p-5 shadow-[0_24px_70px_rgba(0,0,0,0.18),0_0_0_1px_var(--line)]"
      >
        <div className="mb-4 flex items-start justify-between gap-4">
          <div className="min-w-0">
            <h2 id="project-runbook-title" className="text-[20px] font-semibold leading-7 text-[color:var(--text)]">{t("issueDetail.runbook.title")}</h2>
            <div className="mt-1 flex flex-wrap items-center gap-2 text-[12px] leading-5 text-[color:var(--muted)]">
              <span className="font-medium text-[color:var(--muted-strong)]">{props.projectName}</span>
              <span aria-hidden="true">/</span>
              <span>{status}</span>
              <span aria-hidden="true">/</span>
              <span>{updatedAt ? formatRelativeTime(updatedAt) : t("issueDetail.runbook.notAvailable")}</span>
            </div>
          </div>
          <button
            type="button"
            aria-label={t("issueDetail.runbook.closeModal")}
            className="grid size-9 shrink-0 place-items-center rounded-[7px] text-[color:var(--muted)] transition-[background-color,color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] active:scale-95"
            onClick={props.onClose}
          >
            <X data-icon />
          </button>
        </div>

        {props.error ? <Notice tone="danger">{props.error.message}</Notice> : null}
        {props.isLoading ? (
          <div className="grid min-h-[220px] place-items-center rounded-[10px] bg-[color:var(--block)] text-[13px] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
            {t("issueDetail.runbook.loading")}
          </div>
        ) : hasContent ? (
          <div className="border-t border-[color:var(--line)] pt-5">
            <IssueDocumentEditor
              variant="runbook-viewer"
              ariaLabel={t("issueDetail.runbook.contentAria")}
              value={content}
              editable={false}
              onChange={() => undefined}
              placeholder={t("issueDetail.runbook.noRunbookPlaceholder")}
            />
          </div>
        ) : (
          <div className="grid min-h-[220px] place-items-center rounded-[10px] bg-[color:var(--block)] px-6 text-center shadow-[inset_0_0_0_1px_var(--line)]">
            <div>
              <BookOpenText data-icon className="mx-auto mb-3 text-[color:var(--muted)]" />
              <div className="text-[14px] font-medium leading-6 text-[color:var(--text)]">{t("issueDetail.runbook.noRunbookTitle")}</div>
              <p className="mt-1 max-w-[44ch] text-[13px] leading-6 text-[color:var(--muted)] text-pretty">
                {t("issueDetail.runbook.noRunbookBody")}
              </p>
            </div>
          </div>
        )}
      </section>
    </div>
  );
}

function TestDeployModal(props: {
  value: StartTestDeployInput;
  clusters: Cluster[];
  changeNodes: IssueChangeNode[];
  sessions: AgentSession[];
  agents: AgentProfile[];
  isPending: boolean;
  canSubmit: boolean;
  error?: Error | null;
  onChange: (value: StartTestDeployInput) => void;
  onClose: () => void;
  onSubmit: () => void;
}) {
  const { t } = useMspaceTranslation();
  const selectedCluster = props.clusters.find((cluster) => cluster.id === (props.value.environmentId || props.value.clusterId));
  const effectiveExposure = props.value.exposureMode || selectedCluster?.exposureMode || "nodeport";
  const selectedNode = props.changeNodes.find((node) => node.commitSha === props.value.sourceCommitSha);
  const selectedSession = selectedNode ? changeNodeSession(selectedNode, props.sessions) : undefined;
  const selectedAgent = selectedSession ? sessionAgent(selectedSession, props.agents) : undefined;

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") props.onClose();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [props]);

  return (
    <div className="fixed inset-0 z-[80] grid place-items-center bg-[rgba(31,31,31,0.18)] px-5 py-8">
      <button type="button" aria-label={t("issueDetail.testDeploy.closeBackdrop")} className="absolute inset-0 cursor-default" onClick={props.onClose} />
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="test-deploy-title"
        className="relative max-h-[calc(100vh-40px)] w-full max-w-[620px] overflow-auto rounded-[12px] bg-[color:var(--paper)] p-5 shadow-[0_24px_70px_rgba(0,0,0,0.18),0_0_0_1px_var(--line)]"
      >
        <div className="mb-5 flex items-start justify-between gap-4">
          <div className="min-w-0">
            <h2 id="test-deploy-title" className="text-[20px] font-semibold leading-7 text-[color:var(--text)]">{t("issueDetail.testDeploy.title")}</h2>
            <p className="mt-1 max-w-[58ch] text-[13px] leading-6 text-[color:var(--muted)] text-pretty">
              {t("issueDetail.testDeploy.description")}
            </p>
          </div>
          <button
            type="button"
            aria-label={t("issueDetail.runbook.closeModal")}
            className="grid size-9 shrink-0 place-items-center rounded-[7px] text-[color:var(--muted)] transition-[background-color,color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] active:scale-95"
            onClick={props.onClose}
          >
            <X data-icon />
          </button>
        </div>
        <form
          className="grid gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (props.canSubmit) props.onSubmit();
          }}
        >
          {props.error ? <Notice tone="danger">{props.error.message}</Notice> : null}
          {props.clusters.length === 0 ? (
            <Notice tone="danger">{t("issueDetail.testDeploy.createClusterFirst")}</Notice>
          ) : null}
          {props.changeNodes.length === 0 ? (
            <Notice tone="danger">{t("issueDetail.testDeploy.runAgentFirst")}</Notice>
          ) : null}
          <Field label={t("issueDetail.testDeploy.sourceCommit")}>
            <Select
              value={props.value.sourceCommitSha || "__none"}
              onValueChange={(commitSha) => {
                const node = props.changeNodes.find((item) => item.commitSha === commitSha);
                props.onChange({
                  ...props.value,
                  sourceCommitSha: node?.commitSha || "",
                  sourceSessionId: node?.sessionId || "",
                });
              }}
              disabled={props.changeNodes.length === 0}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__none">{t("issueDetail.testDeploy.selectCommit")}</SelectItem>
                {props.changeNodes.map((node) => (
                  <SelectItem key={node.id || node.commitSha} value={node.commitSha}>
                    {sourceNodeLabel(node)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          {selectedNode ? (
            <div className="grid gap-1.5 rounded-[10px] bg-[color:var(--block)] p-3 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
              <div className="flex min-w-0 items-center gap-2 font-medium text-[color:var(--muted-strong)]">
                <GitCommit data-icon className="shrink-0 text-[color:var(--accent-blue)]" />
                <span className="min-w-0 truncate font-mono">{selectedNode.commitSha}</span>
              </div>
              <div className="line-clamp-2">{selectedNode.subject || t("issueDetail.commits.noSubject")}</div>
              <div className="flex min-w-0 flex-wrap gap-x-3 gap-y-1">
                <span>{selectedAgent?.name || "Codex"}</span>
                <span>{t("issueDetail.commits.files", { count: selectedNode.filesChanged })}</span>
                <span>{t("issueDetail.timeline.session")} {selectedNode.sessionId.slice(0, 8)}</span>
              </div>
              {selectedNode.error ? <div className="text-[color:var(--danger)]">{selectedNode.error}</div> : null}
            </div>
          ) : null}
          <Field label={t("issueDetail.testDeploy.cluster")}>
            <Select
              value={props.value.environmentId || props.value.clusterId || "__none"}
              onValueChange={(clusterId) => {
                const nextCluster = props.clusters.find((cluster) => cluster.id === clusterId);
                props.onChange({
                  ...props.value,
                  clusterId: clusterId === "__none" ? "" : clusterId,
                  environmentId: clusterId === "__none" ? "" : clusterId,
                  exposureMode: "",
                  previewDomain: nextCluster?.previewDomain || "",
                  ingressClass: nextCluster?.ingressClass || "",
                  nodeHost: nextCluster?.nodeHost || "",
                });
              }}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__none">{t("issueDetail.testDeploy.selectCluster")}</SelectItem>
                {props.clusters.map((cluster) => (
                  <SelectItem key={cluster.id} value={cluster.id}>
                    {cluster.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          {selectedCluster ? (
            <div className="grid gap-1.5 rounded-[10px] bg-[color:var(--block)] p-3 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
              <div className="font-medium text-[color:var(--muted-strong)]">{selectedCluster.name}</div>
              <div className="break-all font-mono">{selectedCluster.kubeconfigPath}</div>
              <div className="break-all font-mono">{selectedCluster.imageRegistryPrefix}</div>
              <div>{selectedCluster.exposureMode === "ingress" ? t("issueDetail.testDeploy.ingressDefault") : t("issueDetail.testDeploy.nodePortDefault")}</div>
            </div>
          ) : null}
          <div className="grid gap-3 md:grid-cols-2">
            <Field label={t("issueDetail.testDeploy.exposure")}>
              <Select
                value={props.value.exposureMode || "default"}
                onValueChange={(value) =>
                  props.onChange({
                    ...props.value,
                    exposureMode: value === "default" ? "" : (value as StartTestDeployInput["exposureMode"]),
                  })
                }
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="default">{t("issueDetail.testDeploy.useClusterDefault")}</SelectItem>
                  <SelectItem value="nodeport">NodePort</SelectItem>
                  <SelectItem value="ingress">Ingress</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field label={t("issueDetail.testDeploy.nodeHost")} hint={t("issueDetail.testDeploy.nodeHostHint")}>
              <Input
                value={props.value.nodeHost || ""}
                onChange={(event) => props.onChange({ ...props.value, nodeHost: event.target.value })}
                placeholder={t("issueDetail.testDeploy.optional")}
              />
            </Field>
            <Field label={t("issueDetail.testDeploy.previewDomain")} hint={effectiveExposure === "ingress" ? t("issueDetail.testDeploy.previewDomainIngressHint") : t("issueDetail.testDeploy.previewDomainOptionalHint")}>
              <Input
                value={props.value.previewDomain || ""}
                onChange={(event) => props.onChange({ ...props.value, previewDomain: event.target.value })}
                placeholder="preview.example.com"
              />
            </Field>
            <Field label={t("issueDetail.testDeploy.ingressClass")}>
              <Input
                value={props.value.ingressClass || ""}
                onChange={(event) => props.onChange({ ...props.value, ingressClass: event.target.value })}
                placeholder={t("issueDetail.testDeploy.optional")}
              />
            </Field>
          </div>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={props.onClose} disabled={props.isPending}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={!props.canSubmit}>
              <Rocket data-icon />
              {props.isPending ? t("issueDetail.testDeploy.queueing") : t("issueDetail.testDeploy.queueDeploy")}
            </Button>
          </div>
        </form>
      </section>
    </div>
  );
}

function ProjectAttachModal(props: {
  projects: Project[];
  currentProjectId: string;
  suggestedRepoUrl: string;
  repoUrl: string;
  projectName: string;
  attachError?: Error | null;
  createError?: Error | null;
  isAttaching: boolean;
  onRepoUrlChange: (value: string) => void;
  onProjectNameChange: (value: string) => void;
  onAttachProject: (projectId: string) => void;
  onCreateAndAttach: () => void;
  onClose: () => void;
}) {
  const { t } = useMspaceTranslation();
  const canCreate = props.repoUrl.trim().length > 0 && !props.isAttaching;

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") props.onClose();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [props]);

  return (
    <div className="fixed inset-0 z-[80] grid place-items-center bg-[rgba(31,31,31,0.18)] px-5 py-8">
      <button type="button" aria-label={t("issueDetail.projectAttach.closeBackdrop")} className="absolute inset-0 cursor-default" onClick={props.onClose} />
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="project-attach-title"
        className="relative max-h-[calc(100vh-40px)] w-full max-w-[620px] overflow-auto rounded-[12px] bg-[color:var(--paper)] p-5 shadow-[0_24px_70px_rgba(0,0,0,0.18),0_0_0_1px_var(--line)]"
      >
        <div className="mb-5 flex items-start justify-between gap-4">
          <div className="min-w-0">
            <div className="mb-2 flex items-center gap-2 text-[12px] text-[color:var(--muted)]">
              <Boxes data-icon />
              {t("issueDetail.projectAttach.eyebrow")}
            </div>
            <h2 id="project-attach-title" className="text-[20px] font-semibold leading-7 text-[color:var(--text)]">{t("issueDetail.projectAttach.title")}</h2>
            <p className="mt-1 max-w-[58ch] text-[13px] leading-6 text-[color:var(--muted)] text-pretty">
              {t("issueDetail.projectAttach.description")}
            </p>
          </div>
          <button
            type="button"
            aria-label={t("issueDetail.runbook.closeModal")}
            className="grid size-9 shrink-0 place-items-center rounded-[7px] text-[color:var(--muted)] transition-[background-color,color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] active:scale-95"
            onClick={props.onClose}
          >
            <X data-icon />
          </button>
        </div>

        <div className="grid gap-4">
          {(props.attachError || props.createError) ? (
            <Notice tone="danger">{props.attachError?.message || props.createError?.message}</Notice>
          ) : null}

          <Field label={t("issueDetail.projectAttach.existingProject")} hint={props.projects.length > 0 ? t("issueDetail.projectAttach.existingProjectHint") : t("issueDetail.projectAttach.noProjectsHint")}>
            <Select
              value={props.currentProjectId || "__none"}
              onValueChange={(projectId) => {
                if (projectId !== "__none") props.onAttachProject(projectId);
              }}
              disabled={props.projects.length === 0 || props.isAttaching}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__none">{t("issueDetail.projectAttach.selectProject")}</SelectItem>
                {props.projects.map((project) => (
                  <SelectItem key={project.id} value={project.id}>
                    {project.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>

          <div className="flex items-center gap-3 text-[12px] text-[color:var(--faint)]">
            <span className="h-px flex-1 bg-[color:var(--line)]" />
            {t("issueDetail.projectAttach.orCreateFromGitHub")}
            <span className="h-px flex-1 bg-[color:var(--line)]" />
          </div>

          {props.suggestedRepoUrl ? (
            <div className="flex min-w-0 items-center gap-2 rounded-[8px] bg-[color:var(--block)] px-3 py-2 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
              <WandSparkles data-icon className="shrink-0" />
              <span className="min-w-0 truncate">{t("issueDetail.projectAttach.detectedRepo", { repoUrl: props.suggestedRepoUrl })}</span>
            </div>
          ) : null}

          <div className="grid gap-3">
            <Field label={t("issueDetail.projectAttach.githubRepositoryUrl")}>
              <Input
                value={props.repoUrl}
                onChange={(event) => props.onRepoUrlChange(event.target.value)}
                placeholder="https://github.com/org/repo"
              />
            </Field>
            <Field label={t("issueDetail.projectAttach.projectName")} hint={t("issueDetail.projectAttach.projectNameHint")}>
              <Input
                value={props.projectName}
                onChange={(event) => props.onProjectNameChange(event.target.value)}
                placeholder={t("issueDetail.projectAttach.optional")}
              />
            </Field>
          </div>

          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={props.onClose} disabled={props.isAttaching}>
              {t("common.cancel")}
            </Button>
            <Button type="button" onClick={props.onCreateAndAttach} disabled={!canCreate}>
              <Plus data-icon />
              {props.isAttaching ? t("issueDetail.projectAttach.attaching") : t("issueDetail.projectAttach.createAndAttach")}
            </Button>
          </div>
        </div>
      </section>
    </div>
  );
}

export function IssueDetailPage() {
  const { t } = useMspaceTranslation();
  const { issueId = "" } = useParams({ strict: false }) as { issueId?: string };
  const search = useSearch({ strict: false }) as { tab?: string; sessionId?: string; runtimeTaskId?: string };
  const navigate = useNavigate();
  const searchTab = isIssueTab(search.tab) ? search.tab : "overview";
  const linkedSessionId = typeof search.sessionId === "string" ? search.sessionId.trim() : "";
  const linkedRuntimeTaskId = typeof search.runtimeTaskId === "string" ? search.runtimeTaskId.trim() : "";
  const queryClient = useQueryClient();
  const auth = useMspaceAuth();
  const workspaceId = auth.workspace?.id || "";
  const serverWorkspaceReady = Boolean(auth.token && workspaceId);
  const runtimeMode = auth.workspace?.kind === "team" ? "team" : "personal";
  const agentRequiredCapabilities = useMemo(() => ({ codex: true }), []);
  const agentRuntimeAvailabilityInput = useMemo(
    () => ({ runtimeMode, requiredCapabilities: agentRequiredCapabilities }),
    [agentRequiredCapabilities, runtimeMode],
  );
  const issueQueryKey = queryKeys.workspaceIssue(workspaceId, issueId, auth.token);
  const issuesQueryKey = queryKeys.workspaceIssues(workspaceId, auth.token);
  const inboxQueryKey = queryKeys.workspaceInbox(workspaceId, auth.token);
  const runtimeAvailabilityQueryKey = queryKeys.runtimeAvailability(workspaceId, auth.token, agentRuntimeAvailabilityInput);
  const labelDefinitionsQueryKey = queryKeys.workspaceIssueLabelDefinitions(workspaceId, auth.token);
  const projectsQueryKey = queryKeys.workspaceProjects(workspaceId, auth.token);
  const agentsQueryKey = queryKeys.agents(workspaceId, auth.token);
  const clustersQueryKey = queryKeys.clusters(workspaceId, auth.token);
  const issueResourcesQueryKey = queryKeys.issueResources(workspaceId, issueId, auth.token);
  const projectRunbookKey = (projectId: string) =>
    queryKeys.workspaceProjectRunbook(workspaceId, projectId, auth.token);
  const [composerEditor, setComposerEditor] = useState<Editor | null>(null);
  const [composerBody, setComposerBody] = useState("");
  const [composerFocused, setComposerFocused] = useState(false);
  const [editingCommentId, setEditingCommentId] = useState<string | null>(null);
  const [editingCommentBody, setEditingCommentBody] = useState("");
  const [editingCommentEditor, setEditingCommentEditor] = useState<Editor | null>(null);
  const [editingCommentFocused, setEditingCommentFocused] = useState(false);
  const [composerMentionMatch, setComposerMentionMatch] = useState<EditorMentionMatch | null>(null);
  const [editingCommentMentionMatch, setEditingCommentMentionMatch] = useState<EditorMentionMatch | null>(null);
  const [mentionMenuDismissed, setMentionMenuDismissed] = useState(false);
  const [editingMentionMenuDismissed, setEditingMentionMenuDismissed] = useState(false);
  const [mentionMenuPosition, setMentionMenuPosition] = useState<MentionMenuPosition>({ top: 38, left: 10, width: 384 });
  const [editingMentionMenuPosition, setEditingMentionMenuPosition] = useState<MentionMenuPosition>({ top: 38, left: 10, width: 384 });
  const [activeMentionIndex, setActiveMentionIndex] = useState(0);
  const [activeEditingMentionIndex, setActiveEditingMentionIndex] = useState(0);
  const [newTaskTitle, setNewTaskTitle] = useState("");
  const [sessionSnapshotsById, setSessionSnapshotsById] = useState<Record<string, SessionSnapshot>>({});
  const [issueTab, setIssueTab] = useState<IssueTab>(searchTab);
  const [testDeployOpen, setTestDeployOpen] = useState(false);
  const [runbookOpen, setRunbookOpen] = useState(false);
  const [editingIssue, setEditingIssue] = useState(false);
  const [issueTitleDraft, setIssueTitleDraft] = useState("");
  const [issueBodyDraft, setIssueBodyDraft] = useState("");
  const [projectAttachOpen, setProjectAttachOpen] = useState(false);
  const [newProjectRepoUrl, setNewProjectRepoUrl] = useState("");
  const [newProjectName, setNewProjectName] = useState("");
  const scrolledLinkedSessionTarget = useRef("");
  const [testDeployForm, setTestDeployForm] = useState<StartTestDeployInput>({
    agentProfile: "codex",
    clusterId: "",
    exposureMode: "",
    previewDomain: "",
    ingressClass: "",
    nodeHost: "",
    sourceSessionId: "",
    sourceCommitSha: "",
  });

  const issueQuery = useQuery({
    queryKey: issueQueryKey,
    queryFn: () => controlPlaneApi.getIssue(auth.token, workspaceId, issueId),
    enabled: serverWorkspaceReady && issueId.length > 0,
    refetchInterval: 4_000,
  });
  const agentsQuery = useQuery({
    queryKey: agentsQueryKey,
    queryFn: () => controlPlaneApi.listAgents(auth.token, workspaceId),
    enabled: serverWorkspaceReady,
  });
  const clustersQuery = useQuery({
    queryKey: clustersQueryKey,
    queryFn: () => controlPlaneApi.listClusters(auth.token, workspaceId),
    enabled: serverWorkspaceReady,
  });
  const projectsQuery = useQuery({
    queryKey: projectsQueryKey,
    queryFn: () => controlPlaneApi.listProjects(auth.token, workspaceId),
    enabled: serverWorkspaceReady,
  });
  const labelDefinitionsQuery = useQuery({
    queryKey: labelDefinitionsQueryKey,
    queryFn: () => controlPlaneApi.listIssueLabelDefinitions(auth.token, workspaceId),
    enabled: serverWorkspaceReady,
    retry: false,
  });
  const runtimeAvailabilityQuery = useQuery({
    queryKey: runtimeAvailabilityQueryKey,
    queryFn: () => controlPlaneApi.getRuntimeAvailability(auth.token, workspaceId, agentRuntimeAvailabilityInput),
    enabled: serverWorkspaceReady,
    refetchInterval: 5_000,
  });

  useEffect(() => {
    setIssueTab(searchTab);
  }, [searchTab]);

  const detail = issueQuery.data;
  const resourcesQuery = useQuery({
    queryKey: issueResourcesQueryKey,
    queryFn: () => controlPlaneApi.getIssueTestEnvironmentResources(auth.token, workspaceId, issueId),
    enabled: false,
    retry: false,
  });
  const projectRunbookQuery = useQuery({
    queryKey: detail?.project?.id ? projectRunbookKey(detail.project.id) : projectRunbookKey("__none"),
    queryFn: () => {
      if (!detail?.project?.id) throw new Error(t("issueDetail.page.projectNotLoaded"));
      return controlPlaneApi.getProjectRunbook(auth.token, workspaceId, detail.project.id);
    },
    enabled: serverWorkspaceReady && runbookOpen && Boolean(detail?.project?.id),
  });
  const agents = listOrEmpty(agentsQuery.data);
  const clusters = listOrEmpty(clustersQuery.data);
  const projects = listOrEmpty(projectsQuery.data);
  const labelOptions = issueLabelOptionsForUI(labelDefinitionsQuery.data);
  const enabledAgents = agents.filter((agent) => agent.enabled);
  const continueAgent = enabledAgents.find((agent) => mentionKey(agent.mention) === "codex") || enabledAgents[0];
  const hasProject = Boolean(detail?.project?.id);
  const projectName = projectDisplayName(detail?.project);
  const projectRepository = projectRepositoryLabel(detail?.project);
  const childIssues = listOrEmpty(detail?.childIssues);
  const changeNodes = listOrEmpty(detail?.changeNodes);
  const handoffs = listOrEmpty(detail?.handoffs);
  const latestHandoff = handoffs[0];
  const completedChildIssueCount = childIssues.filter((task) => isClosedIssueStatus(task.status)).length;
  const latestSession = detail?.sessions[0];
  const hasActiveSession = latestSession ? ["queued", "running"].includes(latestSession.status) : false;
  const mentionedAgent = extractAgentMention(composerBody);
  const mentionedAgentConfig = mentionedAgent ? findAgent(enabledAgents, mentionedAgent) : undefined;
  const isSupportedAgentMention = Boolean(mentionedAgentConfig);
  const isUnsupportedAgentMention = Boolean(mentionedAgent && !mentionedAgentConfig);
  const mentionQuery = composerMentionMatch?.query ?? null;
  const agentSuggestions =
    mentionQuery === null
      ? []
      : enabledAgents.filter((agent) => mentionKey(agent.mention).startsWith(mentionQuery) || agent.name.toLowerCase().startsWith(mentionQuery));
  const selectedMentionIndex = agentSuggestions.length === 0 ? 0 : Math.min(activeMentionIndex, agentSuggestions.length - 1);
  const mentionMenuOpen = composerFocused && !mentionMenuDismissed && agentSuggestions.length > 0;
  const editingMentionedAgent = extractAgentMention(editingCommentBody);
  const editingMentionedAgentConfig = editingMentionedAgent ? findAgent(enabledAgents, editingMentionedAgent) : undefined;
  const isSupportedEditingAgentMention = Boolean(editingMentionedAgentConfig);
  const isUnsupportedEditingAgentMention = Boolean(editingMentionedAgent && !editingMentionedAgentConfig);
  const editingMentionQuery = editingCommentMentionMatch?.query ?? null;
  const editingAgentSuggestions =
    editingMentionQuery === null
      ? []
      : enabledAgents.filter((agent) => mentionKey(agent.mention).startsWith(editingMentionQuery) || agent.name.toLowerCase().startsWith(editingMentionQuery));
  const selectedEditingMentionIndex = editingAgentSuggestions.length === 0 ? 0 : Math.min(activeEditingMentionIndex, editingAgentSuggestions.length - 1);
  const editingMentionMenuOpen = Boolean(editingCommentId) && editingCommentFocused && !editingMentionMenuDismissed && editingAgentSuggestions.length > 0;
  const runtimeLabel = runtimeMode === "team" ? t("issueDetail.composer.teamWorker") : t("issueDetail.composer.personalWorker");
  const hasMatchingCodexWorker = runtimeAvailabilityQuery.data?.state === "ready";
  const workerUnavailableText =
    runtimeMode === "personal"
      ? t("issueDetail.composer.personalWorkerUnavailable")
      : t("issueDetail.composer.teamWorkerUnavailable");
  const workerStartingText = t("issueDetail.composer.personalWorkerStarting");
  const canSaveEditingComment =
    Boolean(editingCommentId) &&
    Boolean(editingCommentBody.trim()) &&
    !isUnsupportedEditingAgentMention &&
    (!isSupportedEditingAgentMention || hasProject) &&
    !(isSupportedEditingAgentMention && hasActiveSession);
  const editHelperText = isSupportedEditingAgentMention
    ? hasActiveSession
      ? t("issueDetail.composer.agentAlreadyWorking", { name: editingMentionedAgentConfig?.name })
      : !hasProject
        ? t("issueDetail.composer.attachProjectBeforeAgent")
        : !hasMatchingCodexWorker
          ? workerUnavailableText
        : t("issueDetail.composer.willRunAfterSave", { name: editingMentionedAgentConfig?.name, runtime: runtimeLabel })
    : isUnsupportedEditingAgentMention
      ? t("issueDetail.composer.agentUnavailable", { mention: editingMentionedAgent })
      : t("issueDetail.composer.editLatest");
  const editSaveLabel = isSupportedEditingAgentMention
    ? hasActiveSession
      ? t("issueDetail.composer.agentWorking")
      : !hasProject
        ? t("issueDetail.composer.attachProject")
        : !hasMatchingCodexWorker
          ? runtimeMode === "personal"
            ? t("issueDetail.composer.startPersonalWorker")
            : t("issueDetail.composer.connectWorker")
        : t("issueDetail.composer.saveAndStart")
    : t("issueDetail.composer.saveEdit");
  const composerHelperText = isSupportedAgentMention
    ? hasActiveSession
      ? t("issueDetail.composer.agentAlreadyWorking", { name: mentionedAgentConfig?.name })
      : !hasProject
        ? t("issueDetail.composer.attachProjectBeforeAgent")
        : !hasMatchingCodexWorker
          ? workerUnavailableText
        : t("issueDetail.composer.willRunOnRuntime", { name: mentionedAgentConfig?.name, runtime: runtimeLabel })
    : isUnsupportedAgentMention
      ? t("issueDetail.composer.agentUnavailable", { mention: mentionedAgent })
      : t("issueDetail.composer.commentsStay");
  const ensureAgentWorkerReady = useCallback(async () => {
    await ensureRuntimeReady({
      token: auth.token,
      workspaceId,
      queryClient,
      runtimeMode,
      requiredCapabilities: agentRequiredCapabilities,
      unavailableMessage: workerUnavailableText,
      startingMessage: workerStartingText,
      ensurePersonalWorker: window.mspaceDesktop?.ensurePersonalWorker,
    });
  }, [agentRequiredCapabilities, auth.token, queryClient, runtimeMode, workerStartingText, workerUnavailableText, workspaceId]);
  const syncEditingCommentEditorSnapshot = useCallback((editor: Editor) => {
    const match = mentionMatchInEditor(editor);
    setEditingCommentMentionMatch(match);
    if (match) {
      setEditingMentionMenuPosition(mentionMenuPositionForEditor(editor, match));
    }
  }, []);
  const handleEditingCommentReady = useCallback(
    (editor: Editor | null) => {
      setEditingCommentEditor(editor);
      if (editor) {
        syncEditingCommentEditorSnapshot(editor);
      }
    },
    [syncEditingCommentEditorSnapshot],
  );
  const handleEditingCommentFocus = useCallback(
    (editor: Editor) => {
      setEditingCommentFocused(true);
      syncEditingCommentEditorSnapshot(editor);
    },
    [syncEditingCommentEditorSnapshot],
  );
  const handleEditingCommentBlur = useCallback(() => {
    setEditingCommentFocused(false);
  }, []);

  useEffect(() => {
    const sessions = detail?.sessions || [];
    if (sessions.length === 0) {
      setSessionSnapshotsById({});
      return;
    }
    let cancelled = false;
    void Promise.all(
      sessions.map(async (session) => {
        const sessionDetail = await controlPlaneApi.getSession(auth.token, workspaceId, session.id);
        return [
          session.id,
          {
            logs: listOrEmpty(sessionDetail.logs).map((log) => ({ stream: log.stream, message: log.message })),
            changes: listOrEmpty(sessionDetail.workspace?.changes).length > 0
              ? listOrEmpty(sessionDetail.workspace?.changes)
              : listOrEmpty(sessionDetail.workspace?.comparison?.changes),
          },
        ] as const;
      }),
    ).then((entries) => {
      if (cancelled) return;
      setSessionSnapshotsById(Object.fromEntries(entries));
    });
    return () => {
      cancelled = true;
    };
  }, [auth.token, detail?.sessions, workspaceId]);

  useEffect(() => {
    if (issueTab !== "resources" || !issueId || !detail?.testEnvironment) return;
    void resourcesQuery.refetch();
  }, [detail?.testEnvironment?.namespace, issueId, issueTab, resourcesQuery.refetch]);

  useEffect(() => {
    if (!detail || issueTab !== "overview") return;
    const targetKey = linkedSessionId ? `session:${linkedSessionId}` : linkedRuntimeTaskId ? `task:${linkedRuntimeTaskId}` : "";
    if (!targetKey || scrolledLinkedSessionTarget.current === targetKey) return;
    const targetSession = linkedSessionId
      ? listOrEmpty(detail.sessions).find((session) => session.id === linkedSessionId)
      : linkedRuntimeTaskId
        ? listOrEmpty(detail.sessions).find((session) => session.runtimeTaskId === linkedRuntimeTaskId)
        : undefined;
    if (!targetSession) return;
    scrolledLinkedSessionTarget.current = targetKey;
    window.requestAnimationFrame(() => {
      document.getElementById(issueSessionDomId(targetSession.id))?.scrollIntoView({ block: "center", behavior: "smooth" });
    });
  }, [detail, issueTab, linkedRuntimeTaskId, linkedSessionId]);

  const invalidateIssueHandoffSurfaces = useCallback(async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: issueQueryKey }),
      queryClient.invalidateQueries({ queryKey: issuesQueryKey }),
      queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
    ]);
  }, [inboxQueryKey, issueQueryKey, issuesQueryKey, queryClient]);

  useEffect(() => {
    setActiveMentionIndex(0);
    setMentionMenuDismissed(false);
  }, [mentionQuery]);

  useEffect(() => {
    setActiveEditingMentionIndex(0);
    setEditingMentionMenuDismissed(false);
  }, [editingMentionQuery]);

  const sendComposer = useMutation({
    mutationFn: async (body: string) => {
      const trimmedBody = body.trim();
      if (!trimmedBody) return;
      const agent = extractAgentMention(trimmedBody);
      const agentConfig = agent ? findAgent(enabledAgents, agent) : undefined;
      if (agent && !agentConfig) {
        throw new Error(t("issueDetail.composer.agentUnavailable", { mention: agent }));
      }
      const commentInput = {
        body: trimmedBody,
      };
      if (agentConfig) {
        if (!detail) {
          throw new Error(t("issueDetail.page.issueNotLoaded"));
        }
        const project = detail.project;
        if (!project?.id) {
          throw new Error(t("issueDetail.composer.attachProjectBeforeAgent"));
        }
        await ensureAgentWorkerReady();
        const comment = await controlPlaneApi.addComment(auth.token, workspaceId, issueId, commentInput);
        try {
          await controlPlaneApi.createAgentSession(auth.token, workspaceId, issueId, {
            provider: agentConfig.provider,
            agentProfile: agentConfig.id,
            runtimeMode,
            command: trimmedBody,
            triggerCommentId: comment.commentId,
          });
        } catch (error) {
          if (isNoActiveCodexWorkerError(error)) {
            throw new Error(workerUnavailableText);
          }
          throw error;
        }
        return;
      }
      await controlPlaneApi.addComment(auth.token, workspaceId, issueId, commentInput);
    },
    onSuccess: async () => {
      setComposerBody("");
      composerEditor?.commands.clearContent(false);
      setComposerMentionMatch(null);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: issueQueryKey }),
        queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
      ]);
    },
  });
  const canSendComposer =
    Boolean(composerBody.trim()) &&
    !sendComposer.isPending &&
    !isUnsupportedAgentMention &&
    (!isSupportedAgentMention || hasProject) &&
    !(isSupportedAgentMention && hasActiveSession) &&
    !(isSupportedAgentMention && runtimeMode === "team" && !hasMatchingCodexWorker);
  const sendAgentLabel = sendComposer.isPending
    ? t("issueDetail.composer.sending")
    : isSupportedAgentMention && !hasMatchingCodexWorker
      ? runtimeMode === "personal"
        ? t("issueDetail.composer.startPersonalWorker")
        : t("issueDetail.composer.connectWorker")
      : isSupportedAgentMention
        ? hasActiveSession
          ? t("issueDetail.composer.agentWorking")
          : t("issueDetail.composer.sendTo", { name: mentionedAgentConfig?.name })
        : t("issueDetail.composer.comment");

  const updateComment = useMutation({
    mutationFn: async (input: { commentId: string; body: string; agentConfig?: AgentProfile }) => {
      const trimmedBody = input.body.trim();
      const commentInput = {
        body: trimmedBody,
      };
      if (input.agentConfig) {
        if (!detail) {
          throw new Error(t("issueDetail.page.issueNotLoaded"));
        }
        const project = detail.project;
        if (!project?.id) {
          throw new Error(t("issueDetail.composer.attachProjectBeforeAgent"));
        }
        await ensureAgentWorkerReady();
      }
      const comment = await controlPlaneApi.updateComment(auth.token, workspaceId, issueId, input.commentId, commentInput);
      if (input.agentConfig) {
        try {
          await controlPlaneApi.createAgentSession(auth.token, workspaceId, issueId, {
            provider: input.agentConfig.provider,
            agentProfile: input.agentConfig.id,
            runtimeMode,
            command: trimmedBody,
            triggerCommentId: input.commentId,
          });
        } catch (error) {
          if (isNoActiveCodexWorkerError(error)) {
            throw new Error(workerUnavailableText);
          }
          throw error;
        }
      }
      return comment;
    },
    onSuccess: async () => {
      resetEditingCommentState();
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: issueQueryKey }),
        queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
      ]);
    },
  });

  const toggleCommentReaction = useMutation({
    mutationFn: async (input: { commentId: string; reaction: string; reactedByMe: boolean }) => {
      if (input.reactedByMe) {
        return controlPlaneApi.deleteCommentReaction(auth.token, workspaceId, issueId, input.commentId, input.reaction);
      }
      return controlPlaneApi.setCommentReaction(auth.token, workspaceId, issueId, input.commentId, input.reaction);
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: issueQueryKey });
    },
  });

  function resetEditingCommentState() {
    setEditingCommentId(null);
    setEditingCommentBody("");
    setEditingCommentEditor(null);
    setEditingCommentFocused(false);
    setEditingCommentMentionMatch(null);
    setEditingMentionMenuDismissed(false);
    setEditingMentionMenuPosition({ top: 38, left: 10, width: 384 });
    setActiveEditingMentionIndex(0);
  }

  function syncComposerEditorState(editor: Editor) {
    const match = mentionMatchInEditor(editor);
    setComposerMentionMatch(match);
    if (match) {
      setMentionMenuPosition(mentionMenuPositionForEditor(editor, match));
    }
  }

  function selectAgentSuggestion(agent: AgentProfile) {
    const mention = agentMentionText(agent);
    const match = composerEditor ? mentionMatchInEditor(composerEditor) || composerMentionMatch : null;
    if (composerEditor) {
      if (match) {
        composerEditor.chain().focus().insertContentAt({ from: match.from, to: match.to }, `${mention} `).run();
      } else {
        const separator = composerBody === "" || composerBody.endsWith(" ") || composerBody.endsWith("\n") ? "" : " ";
        composerEditor.chain().focus().insertContent(`${separator}${mention} `).run();
      }
      window.requestAnimationFrame(() => syncComposerEditorState(composerEditor));
    } else {
      setComposerBody(insertAgentMention(composerBody, agent));
    }
    setActiveMentionIndex(0);
    setMentionMenuDismissed(false);
  }

  function syncEditingCommentEditorState(editor: Editor) {
    syncEditingCommentEditorSnapshot(editor);
  }

  function selectEditingAgentSuggestion(agent: AgentProfile) {
    const mention = agentMentionText(agent);
    const match = editingCommentEditor ? mentionMatchInEditor(editingCommentEditor) || editingCommentMentionMatch : null;
    if (editingCommentEditor) {
      if (match) {
        editingCommentEditor.chain().focus().insertContentAt({ from: match.from, to: match.to }, `${mention} `).run();
      } else {
        const separator = editingCommentBody === "" || editingCommentBody.endsWith(" ") || editingCommentBody.endsWith("\n") ? "" : " ";
        editingCommentEditor.chain().focus().insertContent(`${separator}${mention} `).run();
      }
      window.requestAnimationFrame(() => syncEditingCommentEditorState(editingCommentEditor));
    } else {
      setEditingCommentBody(insertAgentMention(editingCommentBody, agent));
    }
    setActiveEditingMentionIndex(0);
    setEditingMentionMenuDismissed(false);
  }

  function handleComposerKeyDown(event: React.KeyboardEvent<HTMLDivElement>, editor: Editor) {
    const isComposing = event.nativeEvent.isComposing || event.keyCode === 229;
    if (isComposing) return;

    if (mentionMenuOpen) {
      if (event.key === "ArrowDown") {
        event.preventDefault();
        setActiveMentionIndex((index) => (index + 1) % agentSuggestions.length);
        return;
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        setActiveMentionIndex((index) => (index - 1 + agentSuggestions.length) % agentSuggestions.length);
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        setMentionMenuDismissed(true);
        return;
      }
      if ((event.key === "Enter" && !event.shiftKey && !event.altKey) || (event.key === "Tab" && !event.shiftKey)) {
        const agent = agentSuggestions[selectedMentionIndex];
        if (agent) {
          event.preventDefault();
          selectAgentSuggestion(agent);
          return;
        }
      }
    }

    if (event.key !== "Enter" || (!event.metaKey && !event.ctrlKey)) return;
    event.preventDefault();
    (editor.view.dom.closest("form") as HTMLFormElement | null)?.requestSubmit();
  }

  function handleEditingCommentKeyDown(event: React.KeyboardEvent<HTMLDivElement>, _editor: Editor) {
    const isComposing = event.nativeEvent.isComposing || event.keyCode === 229;
    if (isComposing) return;

    if (editingMentionMenuOpen) {
      if (event.key === "ArrowDown") {
        event.preventDefault();
        setActiveEditingMentionIndex((index) => (index + 1) % editingAgentSuggestions.length);
        return;
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        setActiveEditingMentionIndex((index) => (index - 1 + editingAgentSuggestions.length) % editingAgentSuggestions.length);
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        setEditingMentionMenuDismissed(true);
        return;
      }
      if ((event.key === "Enter" && !event.shiftKey && !event.altKey) || (event.key === "Tab" && !event.shiftKey)) {
        const agent = editingAgentSuggestions[selectedEditingMentionIndex];
        if (agent) {
          event.preventDefault();
          selectEditingAgentSuggestion(agent);
          return;
        }
      }
    }

    if (event.key !== "Enter" || (!event.metaKey && !event.ctrlKey)) return;
    event.preventDefault();
    if (!editingCommentId || !canSaveEditingComment || updateComment.isPending) return;
    updateComment.mutate({
      commentId: editingCommentId,
      body: editingCommentBody,
      agentConfig: editingMentionedAgentConfig,
    });
  }

  const updateLabels = useMutation({
    mutationFn: (input: UpdateIssueLabelsInput) =>
      controlPlaneApi.updateIssueLabels(auth.token, workspaceId, issueId, input),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: issueQueryKey }),
        queryClient.invalidateQueries({ queryKey: issuesQueryKey }),
        queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
      ]);
    },
  });

  const createTask = useMutation({
    mutationFn: (title: string) =>
      controlPlaneApi.createIssueTask(auth.token, workspaceId, issueId, { title }),
    onSuccess: async () => {
      setNewTaskTitle("");
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: issueQueryKey }),
        queryClient.invalidateQueries({ queryKey: issuesQueryKey }),
        queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
      ]);
    },
  });

  const updateTaskStatus = useMutation({
    mutationFn: (input: { taskId: string; status: string }) =>
      controlPlaneApi.updateIssue(auth.token, workspaceId, input.taskId, { status: input.status }),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: issueQueryKey }),
        queryClient.invalidateQueries({ queryKey: issuesQueryKey }),
        queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
      ]);
    },
  });

  const deleteTask = useMutation({
    mutationFn: (taskId: string) =>
      controlPlaneApi.deleteIssueTask(auth.token, workspaceId, issueId, taskId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: issueQueryKey }),
        queryClient.invalidateQueries({ queryKey: issuesQueryKey }),
        queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
      ]);
    },
  });

  const updateIssueStatus = useMutation({
    mutationFn: (status: string) =>
      controlPlaneApi.updateIssue(auth.token, workspaceId, issueId, { status }),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: issueQueryKey }),
        queryClient.invalidateQueries({ queryKey: issuesQueryKey }),
        queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
      ]);
    },
  });

  const updateIssueContent = useMutation({
    mutationFn: (input: { title: string; body: string }) =>
      controlPlaneApi.updateIssue(auth.token, workspaceId, issueId, input),
    onSuccess: async () => {
      setEditingIssue(false);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: issueQueryKey }),
        queryClient.invalidateQueries({ queryKey: issuesQueryKey }),
        queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
      ]);
    },
  });

  const attachProject = useMutation({
    mutationFn: (projectId: string) =>
      controlPlaneApi.updateIssue(auth.token, workspaceId, issueId, { projectId }),
    onSuccess: async () => {
      setProjectAttachOpen(false);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: issueQueryKey }),
        queryClient.invalidateQueries({ queryKey: issuesQueryKey }),
        queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
        queryClient.invalidateQueries({ queryKey: projectsQueryKey }),
      ]);
    },
  });

  const createAndAttachProject = useMutation({
    mutationFn: async (input: { name: string; repoUrl: string }) => {
      const project = await controlPlaneApi.createProject(auth.token, workspaceId, {
        name: input.name,
        sourceType: "github",
        repoPath: "",
        repoUrl: input.repoUrl,
        defaultBranch: "",
        kubeContext: "",
        kubeconfigPath: "",
        namespace: "",
        imageRegistryPrefix: "",
        previewDomain: "",
        ingressClass: "",
        nodeHost: "",
        defaultClusterId: "",
      });
      await controlPlaneApi.updateIssue(auth.token, workspaceId, issueId, { projectId: project.id });
      return project;
    },
    onSuccess: async () => {
      setProjectAttachOpen(false);
      setNewProjectRepoUrl("");
      setNewProjectName("");
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: issueQueryKey }),
        queryClient.invalidateQueries({ queryKey: issuesQueryKey }),
        queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
        queryClient.invalidateQueries({ queryKey: projectsQueryKey }),
      ]);
    },
  });

  const stopSession = useMutation({
    mutationFn: (sessionId: string) => controlPlaneApi.cancelSession(auth.token, workspaceId, sessionId, { reason: t("issueDetail.timeline.stoppedFromIssueDetail") }),
    onSuccess: async (_data, sessionId) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: issueQueryKey }),
        queryClient.invalidateQueries({ queryKey: queryKeys.session(sessionId) }),
        queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
      ]);
    },
  });

  const startTestDeploy = useMutation({
    mutationFn: (input: StartTestDeployInput) => controlPlaneApi.startTestDeploy(auth.token, workspaceId, issueId, input),
    onSuccess: async (data) => {
      setTestDeployOpen(false);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: issueQueryKey }),
        queryClient.invalidateQueries({ queryKey: issueResourcesQueryKey }),
        queryClient.invalidateQueries({ queryKey: queryKeys.session(data.sessionId) }),
        queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
      ]);
    },
  });

  const cleanupTestEnvironment = useMutation({
    mutationFn: () => controlPlaneApi.requestTestEnvironmentCleanup(auth.token, workspaceId, issueId, { agentProfile: "codex" }),
    onSuccess: async (data) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: issueQueryKey }),
        queryClient.invalidateQueries({ queryKey: issueResourcesQueryKey }),
        queryClient.invalidateQueries({ queryKey: queryKeys.session(data.sessionId) }),
        queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
      ]);
    },
  });

  const retainTestEnvironment = useMutation({
    mutationFn: () => controlPlaneApi.retainTestEnvironment(auth.token, workspaceId, issueId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: issueQueryKey }),
        queryClient.invalidateQueries({ queryKey: issueResourcesQueryKey }),
        queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
      ]);
    },
  });
  const probeTestEnvironment = useMutation({
    mutationFn: () => controlPlaneApi.probeTestEnvironment(auth.token, workspaceId, issueId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: issueQueryKey }),
        queryClient.invalidateQueries({ queryKey: issueResourcesQueryKey }),
        queryClient.invalidateQueries({ queryKey: inboxQueryKey }),
      ]);
    },
  });
  const uploadIssueImage = useCallback(
    async (file: File) => {
      if (!serverWorkspaceReady) {
        throw new Error(t("workspace.signInRequired"));
      }
      const attachment = await controlPlaneApi.uploadIssueAttachment(auth.token, workspaceId, issueId, file);
      return {
        id: attachment.id,
        url: `/api/attachments/${attachment.id}`,
        filename: attachment.filename || file.name,
      };
    },
    [auth.token, issueId, serverWorkspaceReady, t, workspaceId],
  );
  const createPullRequest = useMutation({
    mutationFn: (node: IssueChangeNode) => {
      if (!hasProject) {
        throw new Error(t("issueDetail.handoff.requiresProject"));
      }
      return controlPlaneApi.createPullRequest(auth.token, workspaceId, issueId, {
        sourceSessionId: node.sessionId,
        sourceCommitSha: node.commitSha,
      });
    },
    onSettled: async () => {
      await invalidateIssueHandoffSurfaces();
    },
  });
  const refreshIssueHandoff = useMutation({
    mutationFn: (handoff: IssueHandoff) => {
      if (!hasProject) {
        throw new Error(t("issueDetail.handoff.requiresProject"));
      }
      return controlPlaneApi.refreshIssueHandoff(auth.token, workspaceId, issueId, handoff.id);
    },
    onSettled: async () => {
      await invalidateIssueHandoffSurfaces();
    },
  });
  const checkPreviewStatus = probeTestEnvironment.mutate;
  const previewStatusCheckPending = probeTestEnvironment.isPending;
  useEffect(() => {
    const environment = detail?.testEnvironment;
    if (!environment || hasActiveSession || previewStatusCheckPending) return;
    const namespaceStatus = environment.namespaceStatus || "";
    const shouldCheckPreview =
      Boolean(environment.previewUrl) || ["deploy_failed", "deploy_interrupted", "preview_unverified"].includes(namespaceStatus);
    if (!shouldCheckPreview) return;

    const checkKey = [
      issueId,
      environment.lastDeploySessionId || "",
      environment.previewUrl || "",
      namespaceStatus,
    ].join(":");
    const now = Date.now();
    const lastCheckedAt = autoPreviewCheckAtByIssue.get(checkKey) || 0;
    if (now - lastCheckedAt < AUTO_PREVIEW_CHECK_INTERVAL_MS) return;
    autoPreviewCheckAtByIssue.set(checkKey, now);
    checkPreviewStatus();
  }, [
    checkPreviewStatus,
    detail?.testEnvironment?.lastDeploySessionId,
    detail?.testEnvironment?.namespaceStatus,
    detail?.testEnvironment?.previewUrl,
    hasActiveSession,
    issueId,
    previewStatusCheckPending,
  ]);
  const canStartTestDeploy =
    hasProject &&
    Boolean((testDeployForm.environmentId || testDeployForm.clusterId || "").trim()) &&
    Boolean(testDeployForm.sourceCommitSha?.trim()) &&
    changeNodes.length > 0 &&
    !hasActiveSession &&
    !startTestDeploy.isPending;
  const canCreateTask = Boolean(newTaskTitle.trim()) && !createTask.isPending;
  const stoppedSessionActionRefs = useMemo(
    () =>
      listOrEmpty(detail?.comments)
        .map((comment) => parseSessionActionComment(comment.body))
        .filter((action): action is SessionAction => action?.kind === "stopped")
        .map((action) => action.sessionID),
    [detail?.comments],
  );

  function openTestDeployModal() {
    if (!detail) return;
    setTestDeployForm(testDeployDefaults(detail, clusters));
    startTestDeploy.reset();
    setTestDeployOpen(true);
  }

  function handleIssueTabChange(tab: IssueTab) {
    setIssueTab(tab);
    void navigate({
      to: "/issues/$issueId",
      params: { issueId },
      search: issueTabSearch(tab),
      replace: true,
    });
  }

  function continueFromFailure(failure: SessionFailure) {
    if (!continueAgent || hasActiveSession) return;
    const draft = failureContinueDraft(failure, continueAgent);
    setComposerBody(draft);
    setMentionMenuDismissed(true);
    window.requestAnimationFrame(() => {
      composerEditor?.commands.focus("end");
    });
  }

  const timelineItems = useMemo<TimelineItem[]>(() => {
    if (!detail) return [];
    return [
      { kind: "opened" as const, createdAt: detail.issue.createdAt },
      ...listOrEmpty(detail.comments)
        .filter((comment) => !isNoisySystemComment(comment))
        .map((comment) => ({ kind: "comment" as const, createdAt: comment.createdAt, comment })),
      ...listOrEmpty(detail.sessions).map((session) => ({ kind: "session" as const, createdAt: session.createdAt, session })),
      ...listOrEmpty(detail.failures).map((failure) => ({ kind: "failure" as const, createdAt: failure.createdAt, failure })),
    ].sort((a, b) => new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime());
  }, [detail]);

  if (!detail) {
    return (
      <PageFrame title={t("issueDetail.page.title")} subtitle={t("issueDetail.page.subtitle")}>
        <div className="text-[14px] text-[color:var(--muted)]">{issueQuery.isPending ? t("issueDetail.page.loading") : t("issueDetail.page.notFound")}</div>
      </PageFrame>
    );
  }
  const loadedDetail = detail;
  const creatorActor = humanActor(loadedDetail.issue.creatorName, loadedDetail.issue.creatorAvatarUrl);
  const composerActor = storedHumanActor();
  const assigneeActor = actorForAssignee(loadedDetail.issue.assigneeType, loadedDetail.issue.assignee);
  const projectCluster = clusters.find((cluster) => cluster.id === (loadedDetail.project?.defaultEnvironmentId || loadedDetail.project?.defaultClusterId));
  const testCluster = clusters.find((cluster) => cluster.id === loadedDetail.testEnvironment?.clusterId);
  const issueLabels = listOrEmpty(loadedDetail.labels);
  const rawComments = listOrEmpty(loadedDetail.comments);
  const latestRawComment = rawComments[0];
  const editableCommentId =
    latestRawComment &&
    latestRawComment.authorType === "human" &&
    commentMatchesStoredIdentity(latestRawComment) &&
    !parseSessionActionComment(latestRawComment.body) &&
    !parseStatusTransitionComment(latestRawComment.body) &&
    !hasActiveSession &&
    !listOrEmpty(loadedDetail.sessions).some((session) => session.triggerCommentId === latestRawComment.id)
      ? latestRawComment.id
      : "";
  const selectedTypeLabel = issueLabels.find((label) => issueLabelMatchesDimension(label, "type"));
  const selectedPriorityLabel = issueLabels.find((label) => issueLabelMatchesDimension(label, "priority"));
  const showIssueSidebar = issueTab === "overview";
  const suggestedRepoUrl = detectGitHubRepoUrl(`${loadedDetail.issue.title}\n${loadedDetail.issue.body}`);
  const projectAttachRepoUrl = newProjectRepoUrl;
  const canSaveIssueContent = issueTitleDraft.trim().length > 0 && !updateIssueContent.isPending;

  function startIssueEditing() {
    setIssueTitleDraft(loadedDetail.issue.title);
    setIssueBodyDraft(loadedDetail.issue.body);
    updateIssueContent.reset();
    setEditingIssue(true);
  }

  function cancelIssueEditing() {
    setEditingIssue(false);
    setIssueTitleDraft("");
    setIssueBodyDraft("");
    updateIssueContent.reset();
  }

  function saveIssueContent() {
    if (!canSaveIssueContent) return;
    updateIssueContent.mutate({
      title: issueTitleDraft,
      body: issueBodyDraft,
    });
  }

  function openProjectAttach() {
    setNewProjectRepoUrl(suggestedRepoUrl);
    setNewProjectName("");
    attachProject.reset();
    createAndAttachProject.reset();
    setProjectAttachOpen(true);
  }

  return (
    <PageFrame
      title={editingIssue ? issueTitleDraft || detail.issue.title : detail.issue.title}
      subtitle={
        <IssueHeaderMeta
          projectName={projectName}
          status={detail.issue.status}
          typeLabel={selectedTypeLabel}
          priorityLabel={selectedPriorityLabel}
          triageStatus={detail.issue.triageStatus}
          assignee={assigneeActor}
          onProjectClick={openProjectAttach}
        />
      }
      actions={
        issueTab === "overview" ? (
          editingIssue ? (
            <div className="flex flex-wrap items-center justify-end gap-2">
              <Button type="button" variant="secondary" onClick={cancelIssueEditing} disabled={updateIssueContent.isPending}>
                <X data-icon />
                {t("common.cancel")}
              </Button>
              <Button type="button" onClick={saveIssueContent} disabled={!canSaveIssueContent}>
                <Save data-icon />
                {updateIssueContent.isPending ? t("issueDetail.page.saving") : t("issueDetail.page.saveIssue")}
              </Button>
            </div>
          ) : (
            <Button type="button" variant="secondary" onClick={startIssueEditing}>
              <Pencil data-icon />
              {t("issueDetail.page.editIssue")}
            </Button>
          )
        ) : undefined
      }
      breadcrumbs={[
        { label: t("common.mspace"), to: "/inbox" },
        { label: t("navigation.issues"), to: "/issues" },
        { label: detail.issue.title },
      ]}
    >
      <IssueSubTabs
        active={issueTab}
        onChange={handleIssueTabChange}
      />
      <div
        className={cn(
          "grid gap-10 xl:items-start",
          showIssueSidebar ? "xl:grid-cols-[minmax(0,780px)_280px]" : "xl:grid-cols-[minmax(0,1fr)]",
        )}
      >
        <main className="min-w-0">
          {issueTab === "overview" ? (
            <>
              <section className="border-b border-[color:var(--line)] pb-8">
                {editingIssue ? (
                  <div className="grid gap-4">
                    {updateIssueContent.error ? <Notice tone="danger">{updateIssueContent.error.message}</Notice> : null}
                    <Field label={t("issueDetail.page.titleField")}>
                      <Input
                        value={issueTitleDraft}
                        onChange={(event) => setIssueTitleDraft(event.target.value)}
                        className="text-[16px] font-medium"
                      />
                    </Field>
                    <Field label={t("issueDetail.page.bodyField")}>
                      <IssueDocumentEditor
                        value={issueBodyDraft}
                        onChange={setIssueBodyDraft}
                        placeholder={t("createIssue.placeholder")}
                      />
                    </Field>
                  </div>
                ) : detail.issue.body ? (
                  <RichText agents={agents} className="text-[15px] leading-8">{detail.issue.body}</RichText>
                ) : (
                  <div className="text-[15px] leading-8 text-[color:var(--muted)]">{t("issueDetail.page.noBody")}</div>
                )}
                {detail.issue.parentIssueId === "" ? (
                  <IssueTaskList
                    tasks={childIssues}
                    completedCount={completedChildIssueCount}
                    newTaskTitle={newTaskTitle}
                    isCreating={createTask.isPending}
                    createError={createTask.error}
                    updatingTaskId={updateTaskStatus.isPending ? updateTaskStatus.variables?.taskId || "" : ""}
                    updateError={updateTaskStatus.error}
                    deletingTaskId={deleteTask.isPending ? deleteTask.variables || "" : ""}
                    deleteError={deleteTask.error}
                    canCreate={canCreateTask}
                    onNewTaskTitleChange={setNewTaskTitle}
                    onCreateTask={() => {
                      if (!canCreateTask) return;
                      createTask.mutate(newTaskTitle.trim());
                    }}
                    onToggleTask={(task) => {
                      updateTaskStatus.mutate({
                        taskId: task.id,
                        status: isClosedIssueStatus(task.status) ? "open" : "closed",
                      });
                    }}
                    onDeleteTask={(task) => {
                      deleteTask.mutate(task.id);
                    }}
                  />
                ) : null}
              </section>

              <section className="relative mt-8">
                <div className="absolute bottom-0 left-4 top-0 w-px bg-[color:var(--line)]" aria-hidden="true" />
                <div className="relative">
                  {timelineItems.map((item) => {
                    if (item.kind === "opened") {
                      return (
                        <TimelineShell key="opened" actor={creatorActor} title={t("issueDetail.timeline.openedIssue", { name: creatorActor.name || "mlhiter" })} time={item.createdAt}>
                          <div className="text-[13px] leading-6 text-[color:var(--muted)]">
                            {detail.project?.id ? t("issueDetail.timeline.createdIn", { project: projectName }) : t("issueDetail.timeline.createdWithoutProject")}
                          </div>
                        </TimelineShell>
                      );
                    }
                    if (item.kind === "comment") {
                      const isEditing = editingCommentId === item.comment.id;
                      return (
                        <CommentTimelineItem
                          key={`comment-${item.comment.id}`}
                          comment={item.comment}
                          agents={agents}
                          sessions={listOrEmpty(detail.sessions)}
                          canEdit={item.comment.id === editableCommentId}
                          isEditing={isEditing}
                          editBody={isEditing ? editingCommentBody : item.comment.body}
                          isSaving={updateComment.isPending && updateComment.variables?.commentId === item.comment.id}
                          editError={isEditing ? updateComment.error : null}
                          helperText={isEditing ? editHelperText : undefined}
                          saveLabel={isEditing ? editSaveLabel : undefined}
                          canSave={isEditing ? canSaveEditingComment : undefined}
                          pendingReaction={
                            toggleCommentReaction.isPending && toggleCommentReaction.variables?.commentId === item.comment.id
                              ? toggleCommentReaction.variables.reaction
                              : ""
                          }
                          onToggleReaction={(reaction, reactedByMe) => {
                            toggleCommentReaction.mutate({ commentId: item.comment.id, reaction, reactedByMe });
                          }}
                          mentionMenu={
                            isEditing && editingMentionMenuOpen ? (
                              <AgentMentionMenu
                                agents={editingAgentSuggestions}
                                activeIndex={selectedEditingMentionIndex}
                                position={editingMentionMenuPosition}
                                onActiveIndexChange={setActiveEditingMentionIndex}
                                onSelect={selectEditingAgentSuggestion}
                              />
                            ) : null
                          }
                          onStartEdit={() => {
                            updateComment.reset();
                            setEditingCommentId(item.comment.id);
                            setEditingCommentBody(item.comment.body);
                            setEditingCommentEditor(null);
                            setEditingCommentFocused(false);
                            setEditingCommentMentionMatch(null);
                            setEditingMentionMenuDismissed(false);
                            setEditingMentionMenuPosition({ top: 38, left: 10, width: 384 });
                            setActiveEditingMentionIndex(0);
                          }}
                          onCancelEdit={() => {
                            updateComment.reset();
                            resetEditingCommentState();
                          }}
                          onEditBodyChange={(value) => {
                            setEditingCommentBody(value);
                            setEditingMentionMenuDismissed(false);
                          }}
                          onEditReady={handleEditingCommentReady}
                          onEditEditorStateChange={syncEditingCommentEditorState}
                          onEditFocus={handleEditingCommentFocus}
                          onEditBlur={handleEditingCommentBlur}
                          onEditKeyDown={handleEditingCommentKeyDown}
                          onEditImageUpload={uploadIssueImage}
                          onSaveEdit={() => {
                            if (!canSaveEditingComment) return;
                            updateComment.mutate({
                              commentId: item.comment.id,
                              body: editingCommentBody,
                              agentConfig: editingMentionedAgentConfig,
                            });
                          }}
                        />
                      );
                    }
                    if (item.kind === "session") {
                      const sessionSnapshot = sessionSnapshotsById[item.session.id];
                      const isDeploySession = detail.testEnvironment?.lastDeploySessionId === item.session.id;
                      if (isDeploySession && detail.testEnvironment) {
                        return (
                          <DeployTimelineItem
                            key={`session-${item.session.id}`}
                            anchorId={issueSessionDomId(item.session.id)}
                            session={item.session}
                            logs={sessionSnapshot?.logs || []}
                            changes={sessionSnapshot?.changes || []}
                            agents={agents}
                            testEnvironment={detail.testEnvironment}
                            isSnapshotPending={!sessionSnapshot}
                            isRetrying={startTestDeploy.isPending}
                            canRetry={!hasActiveSession && !startTestDeploy.isPending && changeNodes.length > 0}
                            onRetry={() => {
                              if (!detail || hasActiveSession) return;
                              startTestDeploy.mutate(testDeployDefaults(detail, clusters));
                            }}
                          />
                        );
                      }
	                      return (
	                        <SessionTimelineItem
	                          key={`session-${item.session.id}`}
                            anchorId={issueSessionDomId(item.session.id)}
	                          session={item.session}
                          logs={sessionSnapshot?.logs || []}
                          changes={sessionSnapshot?.changes || []}
                          agents={agents}
                          isSnapshotPending={!sessionSnapshot}
                          hasStopAction={stoppedSessionActionRefs.some((sessionRef) => sessionActionMatchesSession(sessionRef, item.session.id))}
                          isStopping={stopSession.isPending && stopSession.variables === item.session.id}
                          stopError={stopSession.error && stopSession.variables === item.session.id ? stopSession.error : null}
                          onStop={["queued", "running"].includes(item.session.status) ? () => stopSession.mutate(item.session.id) : undefined}
	                        />
	                      );
	                    }
	                    if (item.kind === "failure") {
	                      const session = listOrEmpty(detail.sessions).find((candidate) => candidate.id === item.failure.sessionId);
	                      const failureEvidence = listOrEmpty(detail.evidence).find((candidate) => candidate.id === item.failure.evidenceId || candidate.sessionId === item.failure.sessionId);
	                      const review = listOrEmpty(detail.reviewEvidence).find((candidate) => candidate.id === item.failure.reviewEvidenceId || candidate.sessionId === item.failure.sessionId);
	                      const canRetryFailure = failureCanRetryDeploy(item.failure, detail.testEnvironment) && !hasActiveSession && !startTestDeploy.isPending && changeNodes.length > 0;
	                      const canStopFailure = Boolean(session && ["queued", "running"].includes(session.status));
	                      const failureSessionId = session?.id || "";
	                      return (
	                        <SessionFailureTimelineItem
	                          key={`failure-${item.failure.id}`}
	                          failure={item.failure}
	                          session={session}
	                          evidence={failureEvidence}
	                          review={review}
	                          canContinue={Boolean(continueAgent) && !hasActiveSession}
	                          onContinue={() => continueFromFailure(item.failure)}
	                          canRetry={canRetryFailure}
	                          isRetrying={startTestDeploy.isPending}
	                          onRetry={() => {
	                            if (!detail || hasActiveSession) return;
	                            startTestDeploy.mutate(testDeployDefaults(detail, clusters));
	                          }}
	                          canStop={canStopFailure}
	                          isStopping={Boolean(failureSessionId) && stopSession.isPending && stopSession.variables === failureSessionId}
	                          onStop={failureSessionId && canStopFailure ? () => stopSession.mutate(failureSessionId) : undefined}
	                        />
	                      );
	                    }
	                    return null;
	                  })}
                </div>
              </section>

              <section className="mt-3 grid grid-cols-[32px_minmax(0,1fr)] gap-3">
                <ActorMark actor={composerActor} />
                <form
                  className="min-w-0 rounded-[10px] bg-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--line)]"
                  onSubmit={(event) => {
                    event.preventDefault();
                    if (!canSendComposer) return;
                    sendComposer.mutate(composerBody);
                  }}
                >
                  {sendComposer.error ? <Notice tone="danger">{sendComposer.error.message}</Notice> : null}
                  <div className="relative" data-comment-composer="true">
                    <IssueDocumentEditor
                      variant="comment"
                      ariaLabel={t("issueDetail.composer.ariaLabel")}
                      value={composerBody}
                      onChange={(value) => {
                        setComposerBody(value);
                        setMentionMenuDismissed(false);
                      }}
                      onImageUpload={uploadIssueImage}
                      onReady={setComposerEditor}
                      onEditorStateChange={syncComposerEditorState}
                      onFocus={(editor) => {
                        setComposerFocused(true);
                        syncComposerEditorState(editor);
                      }}
                      onBlur={() => {
                        setComposerFocused(false);
                      }}
                      onKeyDown={handleComposerKeyDown}
                      placeholder={formatMentionPlaceholder(enabledAgents)}
                    />
                    {mentionMenuOpen ? (
                      <AgentMentionMenu
                        agents={agentSuggestions}
                        activeIndex={selectedMentionIndex}
                        position={mentionMenuPosition}
                        onActiveIndexChange={setActiveMentionIndex}
                        onSelect={selectAgentSuggestion}
                      />
                    ) : null}
                  </div>
                  {updateIssueStatus.error ? <div className="border-t border-[color:var(--line)] px-3 py-2"><Notice tone="danger">{updateIssueStatus.error.message}</Notice></div> : null}
                  <div className="flex flex-wrap items-center justify-between gap-2 border-t border-[color:var(--line)] px-3 py-2">
                    <div className="flex min-w-[220px] flex-1 flex-wrap items-center gap-2 text-[12px] leading-5 text-[color:var(--muted)]">
                      {isSupportedAgentMention ? (
                        <span className="inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-[color:var(--line)] bg-[color:var(--surface)] px-2 text-[11px] font-medium text-[color:var(--ink)]">
                          <Bot className="size-3.5" />
                          {hasMatchingCodexWorker ? runtimeLabel : t("issueDetail.composer.noActiveWorker")}
                        </span>
                      ) : null}
                      <span className="min-w-[180px] flex-1">{composerHelperText}</span>
                    </div>
                    <div className="flex flex-wrap items-center justify-end gap-2">
                      <IssueLifecycleActions
                        status={detail.issue.status}
                        isPending={updateIssueStatus.isPending}
                        pendingStatus={updateIssueStatus.variables}
                        onCloseIssue={() => updateIssueStatus.mutate("closed")}
                        onCloseNotPlanned={() => updateIssueStatus.mutate("cancelled")}
                        onReopenForChanges={() => updateIssueStatus.mutate("changes_requested")}
                      />
                      <Button
                        type="submit"
                        variant="secondary"
                        size="sm"
                        disabled={!canSendComposer}
                      >
                        <Send data-icon />
                        {sendAgentLabel}
                      </Button>
                    </div>
                  </div>
                </form>
              </section>
            </>
          ) : issueTab === "commits" ? (
            <IssueCommitsTab
              issueId={issueId}
              changeNodes={changeNodes}
              sessions={listOrEmpty(detail.sessions)}
              agents={agents}
              handoffs={handoffs}
              actionsDisabled={!serverWorkspaceReady || !hasProject}
              actionsDisabledReason={!hasProject ? t("issueDetail.handoff.requiresProject") : undefined}
              isCreatingPr={createPullRequest.isPending}
              refreshingHandoffId={refreshIssueHandoff.isPending ? refreshIssueHandoff.variables?.id || "" : ""}
              createPrError={createPullRequest.error}
              refreshHandoffError={refreshIssueHandoff.error}
              onCreatePr={(node) => {
                createPullRequest.reset();
                createPullRequest.mutate(node);
              }}
              onRefreshHandoff={(handoff) => {
                refreshIssueHandoff.reset();
                refreshIssueHandoff.mutate(handoff);
              }}
            />
          ) : issueTab === "sessions" ? (
            <IssueSessionsTab sessions={listOrEmpty(detail.sessions)} agents={agents} />
          ) : issueTab === "resources" ? (
            <IssueResourcesTab
              environment={detail.testEnvironment}
              cluster={testCluster}
              resources={resourcesQuery.data}
              isLoading={resourcesQuery.isLoading}
              isFetching={resourcesQuery.isFetching}
              error={resourcesQuery.error}
              onRefresh={() => {
                void resourcesQuery.refetch();
              }}
            />
          ) : (
	            <IssueEvidenceTab
              issueId={issueId}
	              reviewEvidence={listOrEmpty(detail.reviewEvidence)}
	              evidence={listOrEmpty(detail.evidence)}
	              failures={listOrEmpty(detail.failures)}
	              testEnvironment={detail.testEnvironment}
	              sessions={listOrEmpty(detail.sessions)}
              changeNodes={changeNodes}
            />
          )}
        </main>

        {showIssueSidebar ? (
        <aside className="xl:sticky xl:top-8">
          <div className="grid gap-6 px-1 text-[13px]">
            <SidebarSection title={t("issueDetail.sidebar.issue")}>
              <div className="grid gap-2">
                <div className="grid grid-cols-[86px_minmax(0,1fr)] items-center gap-2">
                  <span className="text-[12px] leading-5 text-[color:var(--muted)]">{t("issueDetail.header.status")}</span>
                  <div className="flex min-w-0 items-center">
                    <StatusBadge value={displayIssueStatus(detail.issue.status)} valueLabel={issueStatusLabel(detail.issue.status)} />
                  </div>
                </div>
                <MetaIdentityLine label={t("issueDetail.header.assignee")} actor={assigneeActor} />
                <MetaLine label={t("issueDetail.sidebar.updated")} value={formatRelativeTime(detail.issue.updatedAt)} />
                <LabelEditor
                  labels={issueLabels}
                  options={labelOptions}
                  triageStatus={detail.issue.triageStatus}
                  isPending={updateLabels.isPending}
                  error={updateLabels.error}
                  onChange={(labelKeys) => updateLabels.mutate(buildIssueLabelSelectionInput(labelKeys, labelOptions))}
                />
              </div>
            </SidebarSection>

            <SidebarSection title={t("issueDetail.sidebar.project")}>
              {detail.project?.id ? (
                <div className="grid gap-2">
                  <div className="grid min-w-0 grid-cols-[86px_minmax(0,1fr)] items-center gap-2">
                    <span className="text-[12px] leading-5 text-[color:var(--muted)]">{t("issueDetail.sidebar.name")}</span>
                    <button
                      type="button"
                      className="min-w-0 rounded-[6px] px-1 py-1 text-left text-[12px] font-medium leading-5 text-[color:var(--muted-strong)] transition-colors hover:bg-[color:var(--hover)]"
                      onClick={openProjectAttach}
                    >
                      <span className="block truncate">{detail.project.name}</span>
                    </button>
                  </div>
                  <MetaLine label={t("issueDetail.sidebar.repo")} value={projectRepository || t("issueDetail.sidebar.notConfigured")} />
                  <MetaLine label={t("issueDetail.sidebar.defaultCluster")} value={projectCluster?.name || t("issueDetail.sidebar.notConfigured")} />
                </div>
              ) : (
                <div className="grid gap-3 rounded-[8px] bg-[color:var(--block-subtle)] px-3 py-2.5 text-[12px] leading-5 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
                  <span className="font-medium text-[color:var(--muted-strong)]">{t("issueDetail.sidebar.noProjectAttached")}</span>
                  <span>{t("issueDetail.sidebar.attachProjectDescription")}</span>
                  <Button type="button" variant="secondary" size="sm" onClick={openProjectAttach}>
                    <Plus data-icon />
                    {t("issueDetail.sidebar.attachProject")}
                  </Button>
                  {attachProject.error ? <Notice tone="danger">{attachProject.error.message}</Notice> : null}
                </div>
              )}
            </SidebarSection>

            <SidebarSection title={t("issueDetail.sidebar.testEnvironment")}>
              <IssueTestEnvironmentPanel
                environment={detail.testEnvironment}
                cluster={testCluster}
                sessions={listOrEmpty(detail.sessions)}
                hasActiveSession={hasActiveSession}
                disabled={!serverWorkspaceReady || !hasProject}
                disabledReason={!hasProject ? t("issueDetail.environment.requiresProject") : undefined}
                startError={startTestDeploy.error}
                cleanupError={cleanupTestEnvironment.error}
                retainError={retainTestEnvironment.error}
                isStarting={startTestDeploy.isPending}
                isCleaning={cleanupTestEnvironment.isPending}
                isRetaining={retainTestEnvironment.isPending}
                onStartDeploy={openTestDeployModal}
                onCleanup={() => cleanupTestEnvironment.mutate()}
                onRetain={() => retainTestEnvironment.mutate()}
              />
            </SidebarSection>

            {latestSession ? (
              <SidebarSection title={t("issueDetail.sidebar.branch")}>
                <div className="break-words font-mono text-[12px] leading-5 text-[color:var(--muted-strong)]">
                  {latestSession.branch || t("issueDetail.sidebar.notReported")}
                </div>
              </SidebarSection>
            ) : null}

            <SidebarSection title={t("issueDetail.sidebar.handoff")}>
              {latestHandoff ? (
                <div className="grid gap-2">
                  <div className="flex min-w-0 items-center justify-between gap-2">
                    <HandoffStatusPill handoff={latestHandoff} />
                    {latestHandoff.prUrl ? (
                      <Button type="button" variant="ghost" size="sm" onClick={() => void openRichLink(latestHandoff.prUrl)}>
                        <ExternalLink data-icon />
                        {t("issueDetail.handoff.openPr")}
                      </Button>
                    ) : null}
                  </div>
                  <MetaLine label={t("issueDetail.sidebar.branch")} value={latestHandoff.branch || t("issueDetail.sidebar.notRecorded")} />
                  <MetaLine label={t("issueDetail.handoff.pullRequest")} value={latestHandoff.prNumber > 0 ? `#${latestHandoff.prNumber}` : latestHandoff.prUrl ? t("issueDetail.sidebar.synced") : t("issueDetail.sidebar.notDetected")} />
                  <MetaLine label={t("issueDetail.handoff.headCommit")} value={latestHandoff.headCommitSha ? latestHandoff.headCommitSha.slice(0, 12) : t("issueDetail.sidebar.notRecorded")} />
                  <MetaLine label={t("issueDetail.handoff.checked")} value={latestHandoff.lastCheckedAt ? formatRelativeTime(latestHandoff.lastCheckedAt) : t("issueDetail.sidebar.notChecked")} />
                  {latestHandoff.prUrl || latestHandoff.branch ? (
                    <Button
                      type="button"
                      variant="secondary"
                      size="sm"
                      disabled={refreshIssueHandoff.isPending && refreshIssueHandoff.variables?.id === latestHandoff.id}
                      onClick={() => {
                        refreshIssueHandoff.reset();
                        refreshIssueHandoff.mutate(latestHandoff);
                      }}
                    >
                      <RefreshCw data-icon />
                      {refreshIssueHandoff.isPending && refreshIssueHandoff.variables?.id === latestHandoff.id ? t("issueDetail.handoff.refreshing") : t("issueDetail.handoff.refresh")}
                    </Button>
                  ) : null}
                  {latestHandoff.error ? <Notice tone="danger">{latestHandoff.error}</Notice> : null}
                  {refreshIssueHandoff.error && refreshIssueHandoff.variables?.id === latestHandoff.id ? <Notice tone="danger">{refreshIssueHandoff.error.message}</Notice> : null}
                </div>
              ) : (
                <div className="text-[12px] leading-5 text-[color:var(--muted)]">{t("issueDetail.sidebar.noPrDetected")}</div>
              )}
            </SidebarSection>

            {detail.project?.id ? (
              <SidebarSection title={t("issueDetail.sidebar.workflow")}>
                <button
                  type="button"
                  className="grid w-full gap-2 rounded-[8px] px-2 py-2 text-left transition-[background-color,box-shadow,transform] duration-150 ease-out hover:bg-[color:var(--hover)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)] active:scale-[0.99]"
                  onClick={() => setRunbookOpen(true)}
                >
                  <span className="flex items-center justify-between gap-2">
                    <span className="flex min-w-0 items-center gap-1.5 text-[13px] font-medium leading-5 text-[color:var(--muted-strong)]">
                      <BookOpenText data-icon className="shrink-0" />
                      <span className="truncate">{t("issueDetail.sidebar.projectRunbook")}</span>
                    </span>
                    <span className="shrink-0 text-[12px] font-medium leading-5 text-[color:var(--muted)]">{t("issueDetail.sidebar.view")}</span>
                  </span>
                  <div className="grid gap-1.5">
                    <MetaLine label={t("issueDetail.header.status")} value={runbookStatusLabel(detail.project.runbookStatus)} />
                    <MetaLine label={t("issueDetail.sidebar.updated")} value={runbookUpdatedLabel(detail.project.runbookUpdatedAt)} />
                  </div>
                </button>
              </SidebarSection>
            ) : null}
          </div>
        </aside>
        ) : null}
      </div>

      {runbookOpen && detail.project?.id ? (
        <ProjectRunbookModal
          projectName={detail.project.name}
          projectStatus={detail.project.runbookStatus}
          projectUpdatedAt={detail.project.runbookUpdatedAt}
          runbook={projectRunbookQuery.data}
          isLoading={projectRunbookQuery.isLoading}
          error={projectRunbookQuery.error}
          onClose={() => setRunbookOpen(false)}
        />
      ) : null}

      {testDeployOpen ? (
        <TestDeployModal
          value={testDeployForm}
          clusters={clusters}
          changeNodes={changeNodes}
          sessions={listOrEmpty(detail.sessions)}
          agents={agents}
          isPending={startTestDeploy.isPending}
          canSubmit={canStartTestDeploy}
          error={startTestDeploy.error}
          onChange={setTestDeployForm}
          onClose={() => setTestDeployOpen(false)}
          onSubmit={() => startTestDeploy.mutate(testDeployForm)}
        />
      ) : null}
      {projectAttachOpen ? (
        <ProjectAttachModal
          projects={projects}
          currentProjectId={detail.project?.id || ""}
          suggestedRepoUrl={suggestedRepoUrl}
          repoUrl={projectAttachRepoUrl}
          projectName={newProjectName}
          attachError={attachProject.error}
          createError={createAndAttachProject.error}
          isAttaching={attachProject.isPending || createAndAttachProject.isPending}
          onRepoUrlChange={setNewProjectRepoUrl}
          onProjectNameChange={setNewProjectName}
          onAttachProject={(projectId) => attachProject.mutate(projectId)}
          onCreateAndAttach={() => createAndAttachProject.mutate({ name: newProjectName, repoUrl: projectAttachRepoUrl })}
          onClose={() => setProjectAttachOpen(false)}
        />
      ) : null}
    </PageFrame>
  );
}
