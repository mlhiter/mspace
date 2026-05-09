import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  Bold,
  Bot,
  CheckCircle2,
  CircleDot,
  CircleStop,
  Clock3,
  Code2,
  ExternalLink,
  FileText,
  Globe2,
  Italic,
  Link as LinkIcon,
  Rocket,
  Send,
  Tag,
  Trash2,
  UserRound,
  X,
} from "lucide-react";
import {
  api,
  buildApiUrl,
  queryKeys,
  type AgentProfile,
  type AgentSession,
  type Cluster,
  type Comment,
  type DeploymentEvidence,
  type IssueLabel,
  type IssueTestEnvironment,
  type SessionLog,
  type SessionStreamEvent,
  type StartTestDeployInput,
  type WorkspaceChange,
} from "@mspace/core";
import {
  Button,
  CodeBlock,
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
  Textarea,
  cn,
} from "@mspace/ui";
import { formatAbsoluteTime, formatRelativeTime } from "./time";

type TimelineItem =
  | { kind: "opened"; createdAt: string }
  | { kind: "comment"; createdAt: string; comment: Comment }
  | { kind: "session"; createdAt: string; session: AgentSession }
  | { kind: "evidence"; createdAt: string; evidence: DeploymentEvidence };

type LogLine = Pick<SessionLog, "stream" | "message">;
type SessionSnapshot = {
  logs: LogLine[];
  changes: WorkspaceChange[];
};

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

function listOrEmpty<T>(items: T[] | null | undefined): T[] {
  return Array.isArray(items) ? items : [];
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

function insertAgentMention(value: string, agent: AgentProfile) {
  const mention = agent.mention.startsWith("@") ? agent.mention : `@${agent.mention}`;
  if (trailingMentionQuery(value) !== null) {
    return value.replace(/@([a-z0-9_-]*)$/i, `${mention} `);
  }
  const separator = value === "" || value.endsWith(" ") || value.endsWith("\n") ? "" : " ";
  return `${value}${separator}${mention} `;
}

function fallbackAgent(agentId: string): AgentProfile {
  const id = mentionKey(agentId) || "codex";
  return {
    id,
    name: id.charAt(0).toUpperCase() + id.slice(1),
    mention: `@${id}`,
    provider: "codex",
    description: "Agent profile",
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

function formatMentionPlaceholder(agents: AgentProfile[]) {
  if (agents.length === 0) return "Write a reply.";
  const mentions = agents.slice(0, 3).map((agent) => agent.mention).join(", ");
  return `Write a reply. Mention ${mentions}.`;
}

function parseLabelInput(value: string) {
  return value
    .split(/[,\n]/)
    .map((item) => item.trim().replace(/^#/, "").trim())
    .filter(Boolean);
}

function labelNames(labels: IssueLabel[]) {
  return listOrEmpty(labels).map((label) => label.name);
}

function testDeployDefaults(detail: NonNullable<Awaited<ReturnType<typeof api.getIssue>>>, clusters: Cluster[]): StartTestDeployInput {
  const clusterId = detail.testEnvironment?.clusterId || detail.project.defaultClusterId || clusters[0]?.id || "";
  const selectedCluster = clusters.find((cluster) => cluster.id === clusterId);
  return {
    agentProfile: "codex",
    clusterId,
    exposureMode: "",
    previewDomain: selectedCluster?.previewDomain || detail.testEnvironment?.previewDomain || "",
    ingressClass: selectedCluster?.ingressClass || detail.testEnvironment?.ingressClass || "",
    nodeHost: selectedCluster?.nodeHost || detail.testEnvironment?.nodeHost || "",
  };
}

function previewStrategy(environment: IssueTestEnvironment | null | undefined) {
  if (!environment) return "not requested";
  if (environment.exposureMode === "ingress" || environment.previewDomain) return environment.ingressClass ? `Ingress · ${environment.ingressClass}` : "Ingress";
  return environment.nodeHost ? `NodePort · ${environment.nodeHost}` : "NodePort";
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

function RichText(props: { children: string; basePath?: string; className?: string }) {
  const text = stringsOrEmpty(props.children);
  if (!text) return null;

  return (
    <div className={cn("rich-text text-[14px] leading-7 text-[color:var(--text)] text-pretty", props.className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          a: ({ href = "", children }) => (
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
          ),
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

function stringsOrEmpty(value: string) {
  return typeof value === "string" ? value.trim() : "";
}

function SessionStatusMark(props: { status: string }) {
  if (props.status === "completed") {
    return (
      <span
        aria-label="completed"
        title="completed"
        className="grid size-7 shrink-0 place-items-center rounded-full bg-[color:var(--success-soft)] text-[color:var(--success)]"
      >
        <CheckCircle2 data-icon />
      </span>
    );
  }
  return <StatusBadge value={props.status} />;
}

function WorkingSessionLine(props: { status: string; agentName: string }) {
  const label = props.status === "queued" ? "Waiting to start." : "Working...";
  return (
    <div className="inline-flex min-w-0 items-center gap-2 text-[13px] leading-6 text-[color:var(--muted)]">
      <span className="relative flex size-2 shrink-0">
        <span className="absolute inline-flex size-full rounded-full bg-[color:var(--accent-blue)] opacity-25 motion-safe:animate-ping" />
        <span className="relative inline-flex size-2 rounded-full bg-[color:var(--accent-blue)]" />
      </span>
      <span className="truncate">
        <span className="font-medium text-[color:var(--muted-strong)]">{props.agentName}</span> {label}
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
      {props.isStopping ? "Stopping" : "Stop"}
    </Button>
  );
}

function SessionFileChanges(props: { changes: WorkspaceChange[]; workdir: string }) {
  const changes = props.changes.slice(0, 6);
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
            <FileText data-icon className="shrink-0 text-[color:var(--faint)]" />
            <span className="shrink-0 font-mono text-[11px] text-[color:var(--faint)]">{change.statusCode || "M"}</span>
            <span className="min-w-0 truncate">{change.path}</span>
          </button>
        );
      })}
      {props.changes.length > changes.length ? (
        <span className="text-[12px] leading-5 text-[color:var(--muted)]">+{props.changes.length - changes.length} more</span>
      ) : null}
    </div>
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

function ActorMark(props: { kind: "human" | "codex" | "system" | "evidence" }) {
  const Icon =
    props.kind === "codex"
      ? Bot
      : props.kind === "human"
        ? UserRound
        : props.kind === "evidence"
          ? CheckCircle2
          : CircleDot;
  return (
    <div
      className={cn(
        "relative z-10 grid size-8 shrink-0 place-items-center rounded-full bg-[color:var(--paper)] shadow-[0_0_0_1px_var(--line)]",
        props.kind === "codex" && "text-[color:var(--accent-blue)]",
        props.kind === "human" && "text-[color:var(--text)]",
        props.kind === "system" && "text-[color:var(--muted)]",
        props.kind === "evidence" && "text-[color:var(--success)]",
      )}
    >
      <Icon data-icon />
    </div>
  );
}

function TimelineShell(props: {
  actor: "human" | "codex" | "system" | "evidence";
  title: string;
  time: string;
  children?: React.ReactNode;
}) {
  return (
    <article className="grid grid-cols-[32px_minmax(0,1fr)] gap-3">
      <ActorMark kind={props.actor} />
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

function CommentTimelineItem(props: { comment: Comment }) {
  const actor = props.comment.authorType === "human" ? "human" : "system";
  const title = props.comment.authorType === "human" ? "mlhiter commented" : "mspace updated the issue";
  return (
    <TimelineShell actor={actor} title={title} time={props.comment.createdAt}>
      <RichText>{props.comment.body}</RichText>
    </TimelineShell>
  );
}

function SessionTimelineItem(props: {
  session: AgentSession;
  logs: LogLine[];
  changes: WorkspaceChange[];
  agents: AgentProfile[];
  isSnapshotPending?: boolean;
  isStopping?: boolean;
  stopError?: Error | null;
  onStop?: () => void;
}) {
  const { session, logs } = props;
  const agent = sessionAgent(session, props.agents);
  const agentMessage = latestAgentMessage(logs);
  const isActive = ["queued", "running"].includes(session.status);

  return (
    <TimelineShell
      actor="codex"
      title={isActive ? `${agent.name} is working` : agent.name}
      time={session.updatedAt || session.createdAt}
    >
      {isActive ? (
        <div>
          <div className="flex min-w-0 items-center justify-between gap-3">
            <WorkingSessionLine status={session.status} agentName={agent.name} />
            {props.onStop ? <StopSessionButton isStopping={props.isStopping} onStop={props.onStop} /> : null}
          </div>
          {props.stopError ? <div className="mt-1 text-[12px] leading-5 text-[color:var(--danger)]">{props.stopError.message}</div> : null}
          {agentMessage ? (
            <RichText basePath={session.workdir} className="mt-3 rounded-[9px] bg-[color:var(--block-subtle)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
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

          {agentMessage ? (
            <RichText basePath={session.workdir} className="mt-2">
              {agentMessage}
            </RichText>
          ) : props.isSnapshotPending ? (
            <SessionSummarySkeleton />
          ) : (
            <div className={cn("mt-2 text-[14px] leading-6", missingSummaryTone(session.status))}>
              No final agent summary was captured for this session.
            </div>
          )}

          <SessionFileChanges changes={props.changes} workdir={session.workdir} />
        </div>
      )}
    </TimelineShell>
  );
}

function EvidenceTimelineItem(props: { evidence: DeploymentEvidence }) {
  return (
    <TimelineShell actor="evidence" title="Validation evidence attached" time={props.evidence.createdAt}>
      <RichText>{props.evidence.summary}</RichText>
      <details className="mt-2">
        <summary className="cursor-pointer select-none text-[12px] font-medium text-[color:var(--muted)] hover:text-[color:var(--text)]">
          Evidence details
        </summary>
        <CodeBlock className="mt-3">{props.evidence.details}</CodeBlock>
      </details>
    </TimelineShell>
  );
}

function MetaLine(props: { label: string; value: string; wide?: boolean }) {
  return (
    <div className={cn("min-w-0", props.wide && "md:col-span-2")}>
      <div className="text-[11px] font-medium uppercase tracking-[0.04em] text-[color:var(--faint)]">{props.label}</div>
      <div className="mt-0.5 break-words text-[12px] leading-5 text-[color:var(--muted-strong)]">{props.value}</div>
    </div>
  );
}

function SidebarSection(props: { title: string; children: React.ReactNode }) {
  return (
    <section className="border-b border-[color:var(--line)] py-4 last:border-b-0">
      <h2 className="mb-3 text-[12px] font-semibold leading-5 text-[color:var(--muted-strong)]">{props.title}</h2>
      {props.children}
    </section>
  );
}

function ComposerToolbar(props: { onInsert: (prefix: string, suffix: string, placeholder: string) => void }) {
  return (
    <div className="flex items-center gap-1 border-b border-[color:var(--line)] px-2 py-1.5">
      <ComposerTool
        label="Bold"
        icon={<Bold data-icon />}
        onClick={() => props.onInsert("**", "**", "bold text")}
      />
      <ComposerTool
        label="Italic"
        icon={<Italic data-icon />}
        onClick={() => props.onInsert("_", "_", "italic text")}
      />
      <ComposerTool
        label="Inline code"
        icon={<Code2 data-icon />}
        onClick={() => props.onInsert("`", "`", "code")}
      />
      <ComposerTool
        label="Link"
        icon={<LinkIcon data-icon />}
        onClick={() => props.onInsert("[", "](https://)", "link text")}
      />
    </div>
  );
}

function ComposerTool(props: { label: string; icon: React.ReactNode; onClick: () => void }) {
  return (
    <button
      type="button"
      aria-label={props.label}
      title={props.label}
      className="grid size-8 place-items-center rounded-[7px] text-[color:var(--muted)] transition-[background-color,color,transform] duration-150 ease-out hover:bg-[color:var(--hover)] hover:text-[color:var(--text)] active:scale-95"
      onClick={props.onClick}
    >
      {props.icon}
    </button>
  );
}

function LabelEditor(props: {
  labels: IssueLabel[];
  isPending: boolean;
  error?: Error | null;
  onChange: (labels: string[]) => void;
}) {
  const [draft, setDraft] = useState("");
  const names = labelNames(props.labels);

  function submitDraft() {
    const additions = parseLabelInput(draft);
    if (additions.length === 0) return;
    const next = [...names];
    for (const label of additions) {
      if (!next.some((item) => item.toLowerCase() === label.toLowerCase())) {
        next.push(label);
      }
    }
    props.onChange(next);
    setDraft("");
  }

  function removeLabel(name: string) {
    props.onChange(names.filter((label) => label !== name));
  }

  return (
    <div className="grid gap-2">
      <div className="flex min-h-7 flex-wrap items-center gap-1.5">
        {names.length > 0 ? (
          names.map((name) => (
            <span
              key={name}
              className="inline-flex max-w-full items-center gap-1 rounded-[6px] bg-[color:var(--block)] px-2 py-1 text-[12px] leading-4 text-[color:var(--muted-strong)] shadow-[inset_0_0_0_1px_var(--line)]"
            >
              <Tag data-icon className="shrink-0 text-[color:var(--faint)]" />
              <span className="truncate">{name}</span>
              <button
                type="button"
                className="grid size-5 shrink-0 place-items-center rounded-[5px] text-[color:var(--faint)] transition-colors hover:bg-[color:var(--hover)] hover:text-[color:var(--text)]"
                aria-label={`Remove ${name} label`}
                disabled={props.isPending}
                onClick={() => removeLabel(name)}
              >
                <X data-icon />
              </button>
            </span>
          ))
        ) : (
          <div className="text-[12px] leading-5 text-[color:var(--muted)]">No labels.</div>
        )}
      </div>
      <div className="flex items-center gap-2">
        <input
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key !== "Enter" && event.key !== ",") return;
            event.preventDefault();
            submitDraft();
          }}
          placeholder="Add label"
          className="min-h-9 min-w-0 flex-1 rounded-[7px] bg-transparent px-2 text-[13px] text-[color:var(--text)] shadow-[inset_0_0_0_1px_var(--line)] outline-none transition-[box-shadow] duration-150 ease-out placeholder:text-[color:var(--faint)] focus:shadow-[inset_0_0_0_1px_var(--accent),0_0_0_3px_var(--accent-soft)]"
        />
        <Button
          type="button"
          variant="ghost"
          size="sm"
          disabled={props.isPending || parseLabelInput(draft).length === 0}
          onClick={submitDraft}
        >
          Add
        </Button>
      </div>
      {props.error ? <div className="text-[12px] leading-5 text-[color:var(--danger)]">{props.error.message}</div> : null}
    </div>
  );
}

function TestDeployModal(props: {
  value: StartTestDeployInput;
  clusters: Cluster[];
  isPending: boolean;
  canSubmit: boolean;
  error?: Error | null;
  onChange: (value: StartTestDeployInput) => void;
  onClose: () => void;
  onSubmit: () => void;
}) {
  const selectedCluster = props.clusters.find((cluster) => cluster.id === props.value.clusterId);
  const effectiveExposure = props.value.exposureMode || selectedCluster?.exposureMode || "nodeport";

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") props.onClose();
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [props]);

  return (
    <div className="fixed inset-0 z-[80] grid place-items-center bg-[rgba(31,31,31,0.18)] px-5 py-8">
      <button type="button" aria-label="Close test deploy dialog backdrop" className="absolute inset-0 cursor-default" onClick={props.onClose} />
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="test-deploy-title"
        className="relative max-h-[calc(100vh-40px)] w-full max-w-[620px] overflow-auto rounded-[12px] bg-[color:var(--paper)] p-5 shadow-[0_24px_70px_rgba(0,0,0,0.18),0_0_0_1px_var(--line)]"
      >
        <div className="mb-5 flex items-start justify-between gap-4">
          <div className="min-w-0">
            <h2 id="test-deploy-title" className="text-[20px] font-semibold leading-7 text-[color:var(--text)]">Deploy test environment</h2>
            <p className="mt-1 max-w-[58ch] text-[13px] leading-6 text-[color:var(--muted)] text-pretty">
              The agent will create the issue namespace, build and push images, deploy resources, and return a probed preview URL.
            </p>
          </div>
          <button
            type="button"
            aria-label="Close modal"
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
            <Notice tone="danger">Create a cluster before queueing a test deployment.</Notice>
          ) : null}
          <Field label="Cluster">
            <Select
              value={props.value.clusterId || "__none"}
              onValueChange={(clusterId) => {
                const nextCluster = props.clusters.find((cluster) => cluster.id === clusterId);
                props.onChange({
                  ...props.value,
                  clusterId: clusterId === "__none" ? "" : clusterId,
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
                <SelectItem value="__none">Select cluster</SelectItem>
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
              <div>{selectedCluster.exposureMode === "ingress" ? "Ingress default" : "NodePort default"}</div>
            </div>
          ) : null}
          <div className="grid gap-3 md:grid-cols-2">
            <Field label="Exposure">
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
                  <SelectItem value="default">Use cluster default</SelectItem>
                  <SelectItem value="nodeport">NodePort</SelectItem>
                  <SelectItem value="ingress">Ingress</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field label="Node host" hint="Used for NodePort URLs.">
              <Input
                value={props.value.nodeHost || ""}
                onChange={(event) => props.onChange({ ...props.value, nodeHost: event.target.value })}
                placeholder="optional"
              />
            </Field>
            <Field label="Preview domain" hint={effectiveExposure === "ingress" ? "Required for ingress exposure." : "Optional. NodePort is used without a domain."}>
              <Input
                value={props.value.previewDomain || ""}
                onChange={(event) => props.onChange({ ...props.value, previewDomain: event.target.value })}
                placeholder="preview.example.com"
              />
            </Field>
            <Field label="Ingress class">
              <Input
                value={props.value.ingressClass || ""}
                onChange={(event) => props.onChange({ ...props.value, ingressClass: event.target.value })}
                placeholder="optional"
              />
            </Field>
          </div>
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={props.onClose} disabled={props.isPending}>
              Cancel
            </Button>
            <Button type="submit" disabled={!props.canSubmit}>
              <Rocket data-icon />
              {props.isPending ? "Queueing..." : "Queue deploy"}
            </Button>
          </div>
        </form>
      </section>
    </div>
  );
}

export function IssueDetailPage() {
  const { issueId = "" } = useParams({ strict: false }) as { issueId?: string };
  const queryClient = useQueryClient();
  const composerRef = useRef<HTMLTextAreaElement | null>(null);
  const [composerBody, setComposerBody] = useState("");
  const [sessionSnapshotsById, setSessionSnapshotsById] = useState<Record<string, SessionSnapshot>>({});
  const [testDeployOpen, setTestDeployOpen] = useState(false);
  const [testDeployForm, setTestDeployForm] = useState<StartTestDeployInput>({
    agentProfile: "codex",
    clusterId: "",
    exposureMode: "",
    previewDomain: "",
    ingressClass: "",
    nodeHost: "",
  });

  const issueQuery = useQuery({
    queryKey: queryKeys.issue(issueId),
    queryFn: () => api.getIssue(issueId),
    enabled: issueId.length > 0,
    refetchInterval: 4_000,
  });
  const agentsQuery = useQuery({
    queryKey: queryKeys.agents,
    queryFn: api.listAgents,
  });
  const clustersQuery = useQuery({
    queryKey: queryKeys.clusters,
    queryFn: api.listClusters,
  });

  const detail = issueQuery.data;
  const agents = listOrEmpty(agentsQuery.data);
  const clusters = listOrEmpty(clustersQuery.data);
  const enabledAgents = agents.filter((agent) => agent.enabled);
  const latestSession = detail?.sessions[0];
  const hasActiveSession = latestSession ? ["queued", "running"].includes(latestSession.status) : false;
  const mentionedAgent = extractAgentMention(composerBody);
  const mentionedAgentConfig = mentionedAgent ? findAgent(enabledAgents, mentionedAgent) : undefined;
  const isSupportedAgentMention = Boolean(mentionedAgentConfig);
  const isUnsupportedAgentMention = Boolean(mentionedAgent && !mentionedAgentConfig);
  const mentionQuery = trailingMentionQuery(composerBody);
  const agentSuggestions =
    mentionQuery === null
      ? []
      : enabledAgents.filter((agent) => mentionKey(agent.mention).startsWith(mentionQuery) || agent.name.toLowerCase().startsWith(mentionQuery));

  useEffect(() => {
    const sessions = detail?.sessions || [];
    if (sessions.length === 0) {
      setSessionSnapshotsById({});
      return;
    }
    let cancelled = false;
    void Promise.all(
      sessions.map(async (session) => {
        const sessionDetail = await api.getSession(session.id);
        return [
          session.id,
          {
            logs: listOrEmpty(sessionDetail.logs).map((log) => ({ stream: log.stream, message: log.message })),
            changes: listOrEmpty(sessionDetail.workspace?.changes),
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
  }, [detail?.sessions]);

  const handleSessionEvent = useCallback(
    (event: SessionStreamEvent) => {
      if (event.type === "log") {
        if (!latestSession?.id) return;
        setSessionSnapshotsById((current) => ({
          ...current,
          [latestSession.id]: {
            logs: [...(current[latestSession.id]?.logs || []), { stream: "live", message: event.payload }],
            changes: current[latestSession.id]?.changes || [],
          },
        }));
        return;
      }
      void queryClient.invalidateQueries({ queryKey: queryKeys.issue(issueId) });
      void queryClient.invalidateQueries({ queryKey: queryKeys.inbox });
      if (latestSession?.id) {
        void queryClient.invalidateQueries({ queryKey: queryKeys.session(latestSession.id) });
      }
    },
    [issueId, latestSession?.id, queryClient],
  );

  useSessionStream(latestSession?.id, handleSessionEvent);

  const sendComposer = useMutation({
    mutationFn: async (body: string) => {
      const trimmedBody = body.trim();
      if (!trimmedBody) return;
      const agent = extractAgentMention(trimmedBody);
      const agentConfig = agent ? findAgent(enabledAgents, agent) : undefined;
      if (agent && !agentConfig) {
        throw new Error(`@${agent} is not available.`);
      }
      await api.addComment(issueId, { body: trimmedBody });
      if (agentConfig) {
        const command = stripAgentMention(trimmedBody, mentionKey(agentConfig.mention));
        await api.assignAgent(issueId, {
          provider: agentConfig.provider,
          agentProfile: agentConfig.id,
          command: command || trimmedBody,
        });
      }
    },
    onSuccess: async () => {
      setComposerBody("");
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.issue(issueId) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.inbox }),
      ]);
    },
  });
  const canSendComposer =
    Boolean(composerBody.trim()) &&
    !sendComposer.isPending &&
    !isUnsupportedAgentMention &&
    !(isSupportedAgentMention && hasActiveSession);

  function insertComposerMarkup(prefix: string, suffix: string, placeholder: string) {
    const textarea = composerRef.current;
    const start = textarea?.selectionStart ?? composerBody.length;
    const end = textarea?.selectionEnd ?? composerBody.length;
    const selected = composerBody.slice(start, end) || placeholder;
    const next = `${composerBody.slice(0, start)}${prefix}${selected}${suffix}${composerBody.slice(end)}`;
    setComposerBody(next);
    window.requestAnimationFrame(() => {
      textarea?.focus();
      textarea?.setSelectionRange(start + prefix.length, start + prefix.length + selected.length);
    });
  }

  const updateLabels = useMutation({
    mutationFn: (labels: string[]) => api.updateIssueLabels(issueId, { labels }),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.issue(issueId) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.issues }),
        queryClient.invalidateQueries({ queryKey: queryKeys.inbox }),
      ]);
    },
  });

  const stopSession = useMutation({
    mutationFn: (sessionId: string) => api.cancelSession(sessionId),
    onSuccess: async (_data, sessionId) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.issue(issueId) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.session(sessionId) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.inbox }),
      ]);
    },
  });

  const startTestDeploy = useMutation({
    mutationFn: (input: StartTestDeployInput) => api.startTestDeploy(issueId, input),
    onSuccess: async (data) => {
      setTestDeployOpen(false);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.issue(issueId) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.session(data.sessionId) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.inbox }),
        queryClient.invalidateQueries({ queryKey: queryKeys.activeWork }),
      ]);
    },
  });

  const cleanupTestEnvironment = useMutation({
    mutationFn: () => api.requestTestEnvironmentCleanup(issueId, { agentProfile: "codex" }),
    onSuccess: async (data) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.issue(issueId) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.session(data.sessionId) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.inbox }),
        queryClient.invalidateQueries({ queryKey: queryKeys.activeWork }),
      ]);
    },
  });

  const retainTestEnvironment = useMutation({
    mutationFn: () => api.retainTestEnvironment(issueId),
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.issue(issueId) }),
        queryClient.invalidateQueries({ queryKey: queryKeys.inbox }),
        queryClient.invalidateQueries({ queryKey: queryKeys.activeWork }),
      ]);
    },
  });
  const canStartTestDeploy =
    Boolean(testDeployForm.clusterId.trim()) &&
    !hasActiveSession &&
    !startTestDeploy.isPending;

  function openTestDeployModal() {
    if (!detail) return;
    setTestDeployForm(testDeployDefaults(detail, clusters));
    startTestDeploy.reset();
    setTestDeployOpen(true);
  }

  const timelineItems = useMemo<TimelineItem[]>(() => {
    if (!detail) return [];
    return [
      { kind: "opened" as const, createdAt: detail.issue.createdAt },
      ...listOrEmpty(detail.comments)
        .filter((comment) => !isNoisySystemComment(comment))
        .map((comment) => ({ kind: "comment" as const, createdAt: comment.createdAt, comment })),
      ...listOrEmpty(detail.sessions).map((session) => ({ kind: "session" as const, createdAt: session.createdAt, session })),
      ...listOrEmpty(detail.evidence).map((evidence) => ({ kind: "evidence" as const, createdAt: evidence.createdAt, evidence })),
    ].sort((a, b) => new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime());
  }, [detail]);

  if (!detail) {
    return (
      <PageFrame title="Issue" subtitle="Load the durable issue page, local session history, and Kubernetes evidence.">
        <div className="text-[14px] text-[color:var(--muted)]">{issueQuery.isPending ? "Loading issue..." : "Issue not found."}</div>
      </PageFrame>
    );
  }
  const projectCluster = clusters.find((cluster) => cluster.id === detail.project.defaultClusterId);
  const testCluster = clusters.find((cluster) => cluster.id === detail.testEnvironment?.clusterId);

  return (
    <PageFrame
      title={detail.issue.title}
      subtitle={`${detail.project.name} · ${detail.issue.status}`}
      breadcrumbs={[
        { label: "mspace", to: "/inbox" },
        { label: "Issues", to: "/issues" },
        { label: detail.issue.title },
      ]}
    >
      <div className="grid gap-10 xl:grid-cols-[minmax(0,780px)_280px] xl:items-start">
        <main className="min-w-0">
          <section className="border-b border-[color:var(--line)] pb-8">
            {detail.issue.body ? (
              <RichText className="text-[15px] leading-8">{detail.issue.body}</RichText>
            ) : (
              <div className="text-[15px] leading-8 text-[color:var(--muted)]">No issue body yet.</div>
            )}
          </section>

          <section className="relative mt-8">
            <div className="absolute bottom-0 left-4 top-0 w-px bg-[color:var(--line)]" aria-hidden="true" />
            <div className="relative">
              {timelineItems.map((item) => {
                if (item.kind === "opened") {
                  return (
                    <TimelineShell key="opened" actor="system" title="Issue opened" time={item.createdAt}>
                      <div className="text-[13px] leading-6 text-[color:var(--muted)]">
                        {`mspace created this issue in ${detail.project.name}.`}
                      </div>
                    </TimelineShell>
                  );
                }
                if (item.kind === "comment") {
                  return <CommentTimelineItem key={`comment-${item.comment.id}`} comment={item.comment} />;
                }
                if (item.kind === "session") {
                  const sessionSnapshot = sessionSnapshotsById[item.session.id];
                  return (
                    <SessionTimelineItem
                      key={`session-${item.session.id}`}
                      session={item.session}
                      logs={sessionSnapshot?.logs || []}
                      changes={sessionSnapshot?.changes || []}
                      agents={agents}
                      isSnapshotPending={!sessionSnapshot}
                      isStopping={stopSession.isPending && stopSession.variables === item.session.id}
                      stopError={stopSession.error && stopSession.variables === item.session.id ? stopSession.error : null}
                      onStop={["queued", "running"].includes(item.session.status) ? () => stopSession.mutate(item.session.id) : undefined}
                    />
                  );
                }
                return <EvidenceTimelineItem key={`evidence-${item.evidence.id}`} evidence={item.evidence} />;
              })}
            </div>
          </section>

          <section className="mt-2 grid grid-cols-[32px_minmax(0,1fr)] gap-3">
            <ActorMark kind="human" />
            <form
              className="min-w-0 rounded-[10px] bg-[color:var(--paper)] shadow-[inset_0_0_0_1px_var(--line)]"
              onSubmit={(event) => {
                event.preventDefault();
                if (!canSendComposer) return;
                sendComposer.mutate(composerBody);
              }}
            >
              {sendComposer.error ? <Notice tone="danger">{sendComposer.error.message}</Notice> : null}
              <ComposerToolbar onInsert={insertComposerMarkup} />
              <Textarea
                ref={composerRef}
                value={composerBody}
                onChange={(event) => setComposerBody(event.target.value)}
                onKeyDown={(event) => {
                  const isComposing = event.nativeEvent.isComposing || event.keyCode === 229;
                  if (isComposing || event.key !== "Enter" || event.shiftKey || event.altKey) return;
                  event.preventDefault();
                  event.currentTarget.form?.requestSubmit();
                }}
                placeholder={formatMentionPlaceholder(enabledAgents)}
                className="min-h-28 rounded-none bg-transparent shadow-none focus-visible:bg-transparent focus-visible:shadow-none"
              />
              {agentSuggestions.length > 0 ? (
                <div className="border-t border-[color:var(--line)] px-2 py-2">
                  {agentSuggestions.map((agent) => (
                    <button
                      key={agent.id}
                      type="button"
                      className="flex w-full items-center gap-2 rounded-[7px] px-2 py-1.5 text-left text-[13px] transition-[background-color] duration-150 ease-out hover:bg-[color:var(--hover)]"
                      onMouseDown={(event) => event.preventDefault()}
                      onClick={() => setComposerBody((value) => insertAgentMention(value, agent))}
                    >
                      <Bot data-icon className="text-[color:var(--accent-blue)]" />
                      <span className="font-medium text-[color:var(--text)]">{agent.mention}</span>
                      <span className="min-w-0 truncate text-[12px] text-[color:var(--muted)]">{agent.description}</span>
                    </button>
                  ))}
                </div>
              ) : null}
              <div className="flex flex-wrap items-center justify-between gap-2 border-t border-[color:var(--line)] px-3 py-2">
                <div className="text-[12px] leading-5 text-[color:var(--muted)]">
                  {isSupportedAgentMention
                    ? `This comment will be saved and sent to ${mentionedAgentConfig?.name}.`
                    : isUnsupportedAgentMention
                      ? `@${mentionedAgent} is not available yet.`
                      : "Comments stay on the issue. Mention an agent when you want a turn."}
                </div>
                <Button
                  type="submit"
                  variant="secondary"
                  size="sm"
                  disabled={!canSendComposer}
                >
                  <Send data-icon />
                  {sendComposer.isPending
                    ? "Sending..."
                    : isSupportedAgentMention
                      ? hasActiveSession
                        ? "Agent is working"
                        : `Send to ${mentionedAgentConfig?.name}`
                      : "Comment"}
                </Button>
              </div>
            </form>
          </section>
        </main>

        <aside className="xl:sticky xl:top-8">
          <div className="rounded-[10px] bg-[color:var(--paper)] px-4 shadow-[inset_0_0_0_1px_var(--line)]">
            <SidebarSection title="Issue">
              <div className="grid gap-3">
                <div className="flex items-center justify-between gap-3">
                  <span className="text-[13px] text-[color:var(--muted)]">Status</span>
                  <StatusBadge value={detail.issue.status} />
                </div>
                <MetaLine label="Assignee" value={`${detail.issue.assigneeType === "agent" ? "agent" : "human"} · ${detail.issue.assignee || "unassigned"}`} />
                <MetaLine label="Updated" value={formatRelativeTime(detail.issue.updatedAt)} />
                <div>
                  <div className="mb-2 text-[11px] font-medium uppercase tracking-[0.04em] text-[color:var(--faint)]">Labels</div>
                  <LabelEditor
                    labels={listOrEmpty(detail.labels)}
                    isPending={updateLabels.isPending}
                    error={updateLabels.error}
                    onChange={(labels) => updateLabels.mutate(labels)}
                  />
                </div>
              </div>
            </SidebarSection>

            <SidebarSection title="Project">
              <div className="grid gap-3">
                <MetaLine label="Name" value={detail.project.name} />
                <MetaLine label="Repo" value={detail.project.repoPath || "not configured"} />
                <MetaLine label="Default cluster" value={projectCluster?.name || "not configured"} />
              </div>
            </SidebarSection>

            <SidebarSection title="Test environment">
              <div className="grid gap-3">
                {startTestDeploy.error ? <Notice tone="danger">{startTestDeploy.error.message}</Notice> : null}
                {cleanupTestEnvironment.error ? <Notice tone="danger">{cleanupTestEnvironment.error.message}</Notice> : null}
                {retainTestEnvironment.error ? <Notice tone="danger">{retainTestEnvironment.error.message}</Notice> : null}
                <MetaLine label="Namespace" value={detail.testEnvironment?.namespace || "not created"} />
                <MetaLine label="Cluster" value={testCluster?.name || detail.testEnvironment?.clusterId || "not selected"} />
                <MetaLine label="Status" value={detail.testEnvironment?.namespaceStatus || "not requested"} />
                <MetaLine label="Cleanup" value={detail.testEnvironment?.cleanupStatus || "not decided"} />
                <MetaLine label="Exposure" value={previewStrategy(detail.testEnvironment)} />
                {detail.testEnvironment?.previewUrl ? (
                  <button
                    type="button"
                    className="inline-flex min-w-0 items-center gap-1.5 text-left text-[12px] font-medium leading-5 text-[color:var(--accent-blue)] hover:text-[color:var(--text)]"
                    onClick={() => void openRichLink(detail.testEnvironment!.previewUrl)}
                  >
                    <Globe2 data-icon className="shrink-0" />
                    <span className="min-w-0 break-all">{detail.testEnvironment.previewUrl}</span>
                  </button>
                ) : null}
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  disabled={hasActiveSession || startTestDeploy.isPending}
                  onClick={openTestDeployModal}
                >
                  <Rocket data-icon />
                  {detail.testEnvironment ? "Deploy again" : "Deploy test env"}
                </Button>
                {detail.testEnvironment ? (
                  <div className="flex flex-wrap gap-2">
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      disabled={hasActiveSession || cleanupTestEnvironment.isPending}
                      onClick={() => cleanupTestEnvironment.mutate()}
                    >
                      <Trash2 data-icon />
                      {cleanupTestEnvironment.isPending ? "Queueing" : "Cleanup"}
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      disabled={retainTestEnvironment.isPending}
                      onClick={() => retainTestEnvironment.mutate()}
                    >
                      Retain
                    </Button>
                  </div>
                ) : null}
              </div>
            </SidebarSection>

            {latestSession ? (
              <SidebarSection title="Branch">
                <div className="break-words font-mono text-[12px] leading-5 text-[color:var(--muted-strong)]">
                  {latestSession.branch || "not reported"}
                </div>
              </SidebarSection>
            ) : null}

            <SidebarSection title="Workflow">
              <details>
                <summary className="cursor-pointer select-none text-[13px] font-medium text-[color:var(--muted-strong)]">
                  Project commands
                </summary>
                <div className="mt-3 grid gap-3">
                  <MetaLine label="Deploy" value={detail.project.deployCommand || "not configured"} />
                  <MetaLine label="Validate" value={detail.project.validationCommand || "not configured"} />
                </div>
              </details>
            </SidebarSection>
          </div>
        </aside>
      </div>

      {testDeployOpen ? (
        <TestDeployModal
          value={testDeployForm}
          clusters={clusters}
          isPending={startTestDeploy.isPending}
          canSubmit={canStartTestDeploy}
          error={startTestDeploy.error}
          onChange={setTestDeployForm}
          onClose={() => setTestDeployOpen(false)}
          onSubmit={() => startTestDeploy.mutate(testDeployForm)}
        />
      ) : null}
    </PageFrame>
  );
}
