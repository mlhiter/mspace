import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { Clipboard, Files, GitBranch, GitCommit, GitCompareArrows, HardDrive, SquareTerminal } from "lucide-react";
import {
  agentEngineDisplayName,
  agentEngineForSession,
  controlPlaneApi,
  engineRunRef,
  engineSessionRef,
  queryKeys,
  type WorkspaceChange,
  type WorkspaceSnapshot,
} from "@mspace/core";
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
import { useMspaceTranslation } from "@mspace/i18n";
import { FileTypeIcon } from "./file-type-icon";
import { useMspaceAuth } from "./auth-context";
import { RelativeTime } from "./time";
import { visibleWorkspaceFileChanges, workspaceChangeStatusLabel, workspaceChangeStatusTone } from "./workspace-change-status";

function ChangeRow({ change }: { change: WorkspaceChange }) {
  const { t } = useMspaceTranslation();

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
        <span className="break-all text-[13px] font-medium text-[color:var(--text)]">{change.path || t("sessionDetail.unknownPath")}</span>
      </div>
      {change.previousPath ? (
        <div className="mt-1 break-all text-[12px] text-[color:var(--muted)]">
          {t("sessionDetail.renamedFrom", { path: change.previousPath })}
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

function projectRepositoryLabel(project: { sourceType?: string; remoteUrl?: string; repoPath?: string } | null | undefined): string {
  if (!project) return "";
  return project.sourceType === "github" ? project.remoteUrl || project.repoPath || "" : project.repoPath || project.remoteUrl || "";
}

export function SessionDetailPage() {
  const { t } = useMspaceTranslation();
  const { sessionId = "" } = useParams({ strict: false }) as { sessionId?: string };
  const queryClient = useQueryClient();
  const auth = useMspaceAuth();
  const workspaceId = auth.workspace?.id || auth.selectedWorkspaceId || "";
  const serverWorkspaceReady = auth.token !== "" && workspaceId !== "";
  const sessionQuery = useQuery({
    queryKey: queryKeys.session(sessionId),
    queryFn: () => controlPlaneApi.getSession(auth.token, workspaceId, sessionId),
    enabled: serverWorkspaceReady && sessionId.length > 0,
    refetchInterval: 4_000,
  });
  const [logs, setLogs] = useState<string[]>([]);
  const [summaryDraft, setSummaryDraft] = useState("");
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">("idle");

  const generatedSummary = useMemo(() => {
    if (!sessionQuery.data) return "";

    const { session } = sessionQuery.data;
    const agentEngine = agentEngineForSession(session);
    const workspace = normalizeWorkspace(sessionQuery.data.workspace);
    const evidence = listOrEmpty(sessionQuery.data.evidence);
    const lines = [
      t("sessionDetail.summaryHeading", { id: session.id.slice(0, 8) }),
      "",
      `- ${t("sessionDetail.summary.status")}: ${session.status}`,
      `- ${t("sessionDetail.summary.agentEngine")}: ${agentEngineDisplayName(agentEngine)}`,
      `- ${t("sessionDetail.summary.agentStatus")}: ${session.agentStatus || t("sessionDetail.unknown")}`,
      `- ${t("sessionDetail.summary.cleanupStatus")}: ${session.cleanupStatus || t("sessionDetail.retained")}`,
      `- ${t("sessionDetail.summary.cleanedAt")}: ${session.cleanedAt || t("sessionDetail.notCleaned")}`,
      `- ${t("sessionDetail.summary.engineSession")}: ${engineSessionRef(session) || t("sessionDetail.notStarted")}`,
      `- ${t("sessionDetail.summary.engineRun")}: ${engineRunRef(session) || t("sessionDetail.notStarted")}`,
      `- ${t("sessionDetail.summary.branch")}: ${workspace.branch || session.branch || t("sessionDetail.unknown")}`,
      `- ${t("sessionDetail.summary.workspace")}: ${session.workdir || t("sessionDetail.notReported")}`,
      `- ${t("sessionDetail.summary.baseRef")}: ${workspace.comparison.baseRef || t("sessionDetail.unknown")}`,
      `- ${t("sessionDetail.summary.aheadBehind")}: ${workspace.comparison.aheadCount}/${workspace.comparison.behindCount}`,
      `- ${t("sessionDetail.summary.workingTreeChanges")}: ${t("sessionDetail.summary.workingTreeCounts", { tracked: workspace.changedFiles, untracked: workspace.untrackedFiles })}`,
    ];

    if (workspace.comparison.commitLines.length > 0) {
      lines.push("", t("sessionDetail.summary.commitsOnBranch"));
      for (const line of workspace.comparison.commitLines.slice(0, 8)) {
        lines.push(`- ${line}`);
      }
    }

    if (workspace.comparison.changes.length > 0) {
      lines.push("", t("sessionDetail.summary.filesChangedSinceMergeBase"));
      for (const change of workspace.comparison.changes.slice(0, 12)) {
        const renameSuffix = change.previousPath ? ` (${t("sessionDetail.summary.fromPrevious", { path: change.previousPath })})` : "";
        lines.push(`- [${workspaceChangeStatusLabel(change.statusCode)}] ${change.path}${renameSuffix}`);
      }
    } else if (workspace.changes.length > 0) {
      lines.push("", t("sessionDetail.summary.uncommittedChanges"));
      for (const change of workspace.changes.slice(0, 12)) {
        const renameSuffix = change.previousPath ? ` (${t("sessionDetail.summary.fromPrevious", { path: change.previousPath })})` : "";
        lines.push(`- [${workspaceChangeStatusLabel(change.statusCode)}] ${change.path}${renameSuffix}`);
      }
    }

    if (evidence.length > 0) {
      lines.push("", t("sessionDetail.summary.validationEvidence"));
      for (const item of evidence.slice(0, 4)) {
        lines.push(`- ${item.summary} (${item.cluster || t("sessionDetail.summary.currentContext")} / ${item.namespace || t("sessionDetail.summary.namespaceUnset")})`);
      }
    }

    return lines.join("\n");
  }, [sessionQuery.data, t]);

  useEffect(() => {
    if (!sessionQuery.data) return;
    setLogs(listOrEmpty(sessionQuery.data.logs).map((log) => log.message));
    setSummaryDraft((current) => (current.trim() ? current : generatedSummary));
  }, [generatedSummary, sessionQuery.data]);

  const cancelMutation = useMutation({
    mutationFn: () => controlPlaneApi.cancelSession(auth.token, workspaceId, sessionId, { reason: t("sessionDetail.cancelReason") }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.session(sessionId) });
    },
  });

  if (!sessionQuery.data) {
    return (
      <PageFrame title={t("sessionDetail.title")} subtitle={t("sessionDetail.subtitle")}>
        <Panel>{sessionQuery.isPending ? t("sessionDetail.loading") : t("sessionDetail.notFound")}</Panel>
      </PageFrame>
    );
  }

  const { session, issue, project } = sessionQuery.data;
  const agentEngine = agentEngineForSession(session);
  const repositoryLabel = projectRepositoryLabel(project);
  const projectName = project?.name || t("sessionDetail.noProject");
  const workspace = normalizeWorkspace(sessionQuery.data.workspace);
  const sessionActive = ["queued", "running"].includes(session.status);
  const cleanupStatus = session.cleanupStatus || "retained";
  const missingWorkspaceText = workspace.error || t("sessionDetail.workspaceMissing");

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
      title={t("sessionDetail.pageTitle", { id: session.id.slice(0, 8) })}
      subtitle={t("sessionDetail.issueSubtitle", { project: projectName, issue: issue.title })}
      breadcrumbs={[
        { label: t("common.mspace"), to: "/inbox" },
        { label: t("issues.title"), to: "/issues" },
        { label: issue.title, to: "/issues/$issueId", params: { issueId: issue.id } },
        { label: t("sessionDetail.pageTitle", { id: session.id.slice(0, 8) }) },
      ]}
      actions={
        <>
          <Button asChild variant="secondary">
            <Link to="/issues/$issueId" params={{ issueId: issue.id }}>{t("sessionDetail.backToIssue")}</Link>
          </Button>
          <Button
            variant="danger"
            disabled={cancelMutation.isPending || !sessionActive}
            onClick={() => cancelMutation.mutate()}
          >
            {t("sessionDetail.cancelSession")}
          </Button>
        </>
      }
    >
      <div className="grid gap-6 xl:grid-cols-[minmax(0,0.92fr)_minmax(0,1.08fr)]">
        <div className="flex flex-col gap-6">
          <Panel title={t("sessionDetail.sessionMetadata")} aside={<StatusBadge value={session.status} />}>
            <div className="grid gap-3">
              <DataBlock label={t("sessionDetail.summary.agentEngine")} icon={SquareTerminal}>{agentEngineDisplayName(agentEngine)}</DataBlock>
              <DataBlock label={t("sessionDetail.runtimeMode")} icon={HardDrive}>{session.runtimeMode}</DataBlock>
              <DataBlock label={t("sessionDetail.summary.agentStatus")} icon={SquareTerminal}>{session.agentStatus || t("sessionDetail.notReportedYet")}</DataBlock>
              <DataBlock label={t("sessionDetail.summary.cleanupStatus")} icon={Files}>
                {cleanupStatus === "cleaned" ? (
                  session.cleanedAt ? <RelativeTime prefix={t("sessionDetail.cleaned")} value={session.cleanedAt} /> : t("sessionDetail.cleaned")
                ) : t("sessionDetail.retained")}
              </DataBlock>
              <DataBlock label={t("sessionDetail.sessionBranch")} icon={GitBranch}>{session.branch}</DataBlock>
              <DataBlock label={t("sessionDetail.agentRequest")} icon={SquareTerminal}>{session.command || t("sessionDetail.defaultAgentRequest")}</DataBlock>
              <DataBlock label={t("sessionDetail.engineSession")} icon={SquareTerminal}>{engineSessionRef(session) || t("sessionDetail.notStartedYet")}</DataBlock>
              <DataBlock label={t("sessionDetail.engineRun")} icon={SquareTerminal}>{engineRunRef(session) || t("sessionDetail.notStartedYet")}</DataBlock>
              <DataBlock label={t("sessionDetail.sessionWorkspace")} icon={Files}>{session.workdir || t("sessionDetail.notReportedYet")}</DataBlock>
              <DataBlock label={t("sessionDetail.artifactDirectory")} icon={Files}>{session.artifactDir || t("sessionDetail.notReportedYet")}</DataBlock>
              <DataBlock label={t("sessionDetail.sourceRepository")} icon={Files}>{repositoryLabel || t("sessionDetail.notConfigured")}</DataBlock>
            </div>
          </Panel>

          <Panel title={t("sessionDetail.issueSummaryDraft")}>
            {copyState === "copied" ? <Notice>{t("sessionDetail.summaryCopied")}</Notice> : null}
            {copyState === "failed" ? <Notice tone="danger">{t("sessionDetail.clipboardFailed")}</Notice> : null}
            <div className="mt-3 flex flex-col gap-3">
              <Textarea
                value={summaryDraft}
                onChange={(event) => setSummaryDraft(event.target.value)}
                className="min-h-64 font-mono text-[12px] leading-6"
              />
              <div className="flex flex-wrap gap-2">
                <Button variant="secondary" onClick={() => setSummaryDraft(generatedSummary)}>
                  {t("sessionDetail.regenerateDraft")}
                </Button>
                <Button variant="secondary" onClick={() => void handleCopySummary()}>
                  <Clipboard data-icon />
                  {t("sessionDetail.copySummary")}
                </Button>
                <Button disabled title={t("sessionDetail.postDisabledTitle")}>
                  {t("sessionDetail.postToIssue")}
                </Button>
              </div>
            </div>
          </Panel>

          <Panel title={t("sessionDetail.workspaceSnapshot")}>
            {workspace.error ? <Notice tone="danger">{workspace.error}</Notice> : null}
            <div className="grid gap-3 md:grid-cols-2">
              <DataBlock label={t("sessionDetail.workspaceBranch")} icon={GitBranch}>{workspace.branch || session.branch || t("sessionDetail.notAvailableYet")}</DataBlock>
              <DataBlock label="HEAD" icon={GitCommit}>{workspace.shortHead || workspace.head || t("sessionDetail.notAvailableYet")}</DataBlock>
              <DataBlock label={t("sessionDetail.changedTrackedFiles")} icon={Files}>{workspace.changedFiles}</DataBlock>
              <DataBlock label={t("sessionDetail.untrackedFiles")} icon={Files}>{workspace.untrackedFiles}</DataBlock>
            </div>

            <div className="mt-4 flex flex-col gap-2">
              <div className="text-[13px] font-semibold text-[color:var(--muted-strong)]">{t("sessionDetail.gitStatus")}</div>
              <CodeBlock
                empty={
                  !workspace.exists
                    ? missingWorkspaceText
                    : !workspace.isGitRepository
                      ? t("sessionDetail.workspaceNotGit")
                      : t("sessionDetail.workingTreeClean")
                }
              >
                {workspace.exists && workspace.isGitRepository && workspace.statusLines.length > 0
                  ? workspace.statusLines.map((line) => <div key={line}>{line}</div>)
                  : null}
              </CodeBlock>
            </div>

            <div className="mt-4 flex flex-col gap-2">
              <div className="text-[13px] font-semibold text-[color:var(--muted-strong)]">{t("sessionDetail.changedFiles")}</div>
              {workspace.changes.length === 0 ? (
                <DataBlock label={t("sessionDetail.noFileChanges")}>{workspace.exists ? t("sessionDetail.noFileChangesBody") : missingWorkspaceText}</DataBlock>
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
                <div className="text-[13px] font-semibold text-[color:var(--muted-strong)]">{t("sessionDetail.diffPreview")}</div>
                {workspace.diffTruncated ? <InlineMeta>{t("sessionDetail.previewTruncated")}</InlineMeta> : null}
              </div>
              <CodeBlock
                empty={
                  !workspace.exists
                    ? missingWorkspaceText
                    : !workspace.isGitRepository
                      ? t("sessionDetail.workspaceNotGit")
                      : t("sessionDetail.noDiffAgainstHead")
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
                {t("sessionDetail.relativeToBase")}
              </div>
              {workspace.comparison.error ? (
                <Notice tone="danger">{workspace.comparison.error}</Notice>
              ) : (
                <div className="flex flex-col gap-4">
                  <div className="grid gap-3 md:grid-cols-2">
                    <DataBlock label={t("sessionDetail.summary.baseRef")}>{workspace.comparison.baseRef || t("sessionDetail.notAvailableYet")}</DataBlock>
                    <DataBlock label={t("sessionDetail.mergeBase")}>
                      {workspace.comparison.mergeBaseShort || workspace.comparison.mergeBase || t("sessionDetail.notAvailableYet")}
                    </DataBlock>
                    <DataBlock label={t("sessionDetail.ahead")}>{workspace.comparison.aheadCount}</DataBlock>
                    <DataBlock label={t("sessionDetail.behind")}>{workspace.comparison.behindCount}</DataBlock>
                  </div>

                  <div className="flex flex-col gap-2">
                    <div className="text-[13px] font-semibold text-[color:var(--muted-strong)]">{t("sessionDetail.summary.commitsOnBranch")}</div>
                    <CodeBlock empty={t("sessionDetail.noCommitsAhead")}>
                      {workspace.comparison.commitLines.map((line) => <div key={line}>{line}</div>)}
                    </CodeBlock>
                  </div>

                  <div className="flex flex-col gap-2">
                    <div className="text-[13px] font-semibold text-[color:var(--muted-strong)]">{t("sessionDetail.summary.filesChangedSinceMergeBase")}</div>
                    {workspace.comparison.changes.length === 0 ? (
                      <DataBlock label={t("sessionDetail.noBranchLevelChanges")}>{t("sessionDetail.noBranchLevelChangesBody")}</DataBlock>
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
                      <div className="text-[13px] font-semibold text-[color:var(--muted-strong)]">{t("sessionDetail.baseComparisonDiff")}</div>
                      {workspace.comparison.diffTruncated ? <InlineMeta>{t("sessionDetail.previewTruncated")}</InlineMeta> : null}
                    </div>
                    <CodeBlock empty={t("sessionDetail.noDiffRelativeToMergeBase")}>
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

        <Panel title={t("sessionDetail.liveLogStream")}>
          <CodeBlock className="max-h-[70vh]" empty={t("sessionDetail.noLogsYet")}>
            {logs.map((line, index) => <div key={`${index}-${line}`}>{line}</div>)}
          </CodeBlock>
        </Panel>
      </div>
    </PageFrame>
  );
}
