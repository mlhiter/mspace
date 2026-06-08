import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  createHashHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Navigate,
  RouterProvider,
} from "@tanstack/react-router";
import { GitBranch, LoaderCircle, LogIn, Server, UsersRound, X } from "lucide-react";
import {
  AgentsPage,
  ClustersPage,
  IssueCommitDetailPage,
  IssueEvidenceHistoryPage,
  IssueEvidenceSnapshotsPage,
  InboxPage,
  IssueDetailPage,
  IssuesPage,
  MspaceAuthProvider,
  ProjectsPage,
  SessionDetailPage,
  TestCaseDetailPage,
  TestPlanDetailPage,
  TestRunDetailPage,
  TestsPage,
  WorkspaceSettingsPage,
  WorkspaceInvitePage,
} from "@mspace/views";
import {
  AUTH_TOKEN_STORAGE_KEY,
  controlPlaneApi,
  getControlPlaneBaseUrl,
  queryKeys,
  SELECTED_WORKSPACE_STORAGE_KEY,
  setControlPlaneBaseUrl,
  setStoredAuthIdentity,
  type AuthMeResult,
  type CreateWorkspaceInput,
  type IssueListItem,
  type TestPlan,
  type TestRun,
  type WorkspaceInvitationPreview,
} from "@mspace/core";
import { AppShell, Button, Field, Input, MspaceToastProvider, Notice, type ShellActiveWorkItem, type ShellSearchItem } from "@mspace/ui";
import { initializeMspaceI18n, t, useMspaceTranslation } from "@mspace/i18n";
import mspaceLogoUrl from "../../../assets/brand/mspace-logo.svg";
import "./globals.css";

const queryClient = new QueryClient();
initializeMspaceI18n();

function defaultTeamWorkspaceName(name: string | undefined) {
  const owner = name?.trim();
  return owner ? t("workspace.defaultTeamWorkspaceName", { name: owner }) : t("workspace.defaultTeamWorkspaceFallback");
}

function joinSearchSubtitle(values: Array<string | number | null | undefined>): string {
  return values
    .map((value) => String(value || "").trim())
    .filter(Boolean)
    .join(" - ");
}

const inactiveIssueStatuses = new Set(["closed", "cancelled"]);
const activeTestRunStatuses = new Set(["queued", "setup_running", "setup_failed", "running", "needs_acceptance", "blocked"]);
const reviewIssueStatuses = new Set(["needs_review", "changes_requested", "ready_for_test", "blocked"]);
const testRunStatusPriority = new Map([
  ["needs_acceptance", 100],
  ["blocked", 95],
  ["setup_failed", 90],
  ["running", 70],
  ["setup_running", 70],
  ["queued", 50],
]);

function normalizeServerInput(value: string): string {
  const trimmed = value.trim();
  if (!trimmed) throw new Error(t("auth.serverUrlRequired"));
  let parsed: URL;
  try {
    parsed = new URL(trimmed);
  } catch {
    throw new Error(t("auth.serverUrlInvalid"));
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    throw new Error(t("auth.serverUrlInvalid"));
  }
  return parsed.origin.replace(/\/+$/, "");
}

const expectedServerCapabilities = [
  "workspaceInboxIssueGrouping",
  "teamWorkspaceCreation",
  "workspaceInvitations",
  "workspaceInvitationPreview",
  "workspaceKinds",
  "workspaceCollaboration",
  "runtimeWorkerRegistration",
  "runtimeTaskQueue",
  "testCaseLibrary",
  "testCaseWorkflow",
] as const;

type ServerHealthPayload = {
  ok?: unknown;
  serverProtocol?: unknown;
  capabilities?: unknown;
};

type ServerBaseUrlSource = "environment" | "user" | "default";
type PasswordAuthMode = "login" | "register";

function isObjectRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function hasExpectedServerHealth(payload: ServerHealthPayload): boolean {
  if (payload.ok !== true || payload.serverProtocol !== 1) return false;
  const capabilities = payload.capabilities;
  if (!isObjectRecord(capabilities)) return false;
  return expectedServerCapabilities.every((capability) => capabilities[capability] === true);
}

function serverSupportsGitHubAuth(payload: ServerHealthPayload | undefined): boolean {
  const capabilities = payload?.capabilities;
  return isObjectRecord(capabilities) && capabilities.githubAuth === true;
}

function isConfiguredTeamServer(source: ServerBaseUrlSource): boolean {
  return source !== "default";
}

function isActiveIssueWork(issue: IssueListItem): boolean {
  const status = issue.status.trim().toLowerCase();
  return !inactiveIssueStatuses.has(status);
}

function activeIssuePriority(issue: IssueListItem): number {
  const status = issue.status.trim().toLowerCase();
  if (reviewIssueStatuses.has(status)) return 90;
  if (issue.unread) return 85;
  if (issue.sessionCount > 0) return 75;
  if (issue.childIssueCount > issue.completedChildIssueCount) return 70;
  return 40;
}

function issueActiveWorkItem(issue: IssueListItem, statusLabel: string, noProjectLabel: string, taskProgressLabel: string): ShellActiveWorkItem {
  const projectName = issue.projectName || noProjectLabel;
  const detailLabel = issue.childIssueCount > 0 ? taskProgressLabel : "";
  return {
    id: `issue:${issue.id}`,
    kind: "issue",
    projectName,
    title: issue.title,
    status: issue.status,
    statusLabel,
    contextLabel: projectName,
    detailLabel,
    priority: activeIssuePriority(issue),
    updatedAt: issue.updatedAt,
    to: "/issues/$issueId",
    params: { issueId: issue.id },
  };
}

function isActiveTestRunWork(run: TestRun): boolean {
  return activeTestRunStatuses.has(run.status.trim().toLowerCase());
}

type ActiveTestRunGroup = {
  key: string;
  plan?: TestPlan;
  runs: TestRun[];
};

function statusPriority(status: string): number {
  return testRunStatusPriority.get(status.trim().toLowerCase()) || 0;
}

function latestTimestamp(items: Array<{ updatedAt?: string }>): number {
  return Math.max(...items.map((item) => Date.parse(item.updatedAt || "") || 0), 0);
}

function compareActiveWorkPriority(left: ShellActiveWorkItem, right: ShellActiveWorkItem): number {
  const leftPriority = typeof left.priority === "number" ? left.priority : statusPriority(left.status);
  const rightPriority = typeof right.priority === "number" ? right.priority : statusPriority(right.status);
  if (leftPriority !== rightPriority) return rightPriority - leftPriority;
  return (Date.parse(right.updatedAt || "") || 0) - (Date.parse(left.updatedAt || "") || 0);
}

function groupActiveTestRuns(runs: TestRun[], plans: TestPlan[]): ActiveTestRunGroup[] {
  const planById = new Map(plans.map((plan) => [plan.id, plan]));
  const groups = new Map<string, ActiveTestRunGroup>();
  for (const run of runs.filter(isActiveTestRunWork)) {
    const plan = planById.get(run.planId);
    const key = plan ? `plan:${plan.id}` : `run:${run.id}`;
    const group = groups.get(key) || { key, plan, runs: [] };
    group.runs.push(run);
    groups.set(key, group);
  }
  return Array.from(groups.values());
}

function bestRunForGroup(runs: TestRun[]): TestRun {
  return [...runs].sort((left, right) => {
    const priorityDelta = statusPriority(right.status) - statusPriority(left.status);
    if (priorityDelta !== 0) return priorityDelta;
    return (Date.parse(right.updatedAt) || 0) - (Date.parse(left.updatedAt) || 0);
  })[0];
}

function testRunActiveWorkItem(
  group: ActiveTestRunGroup,
  labels: {
    fallbackTitle: string;
    statusLabel: (status: string) => string;
    counts: (run: TestRun) => string;
    runContext: (count: number) => string;
    projectCount: (count: number) => string;
    noProject: string;
  },
  projectNameById: Map<string, string>,
): ShellActiveWorkItem {
  const run = bestRunForGroup(group.runs);
  const projectNames = Array.from(new Set(group.runs.map((item) => projectNameById.get(item.projectId)).filter(Boolean))) as string[];
  const projectLabel =
    projectNames.length === 1
      ? projectNames[0]
      : projectNames.length > 1
        ? labels.projectCount(projectNames.length)
        : labels.noProject;
  const statusLabel = group.runs.length > 1 ? labels.runContext(group.runs.length) : labels.statusLabel(run.status);
  return {
    id: group.key,
    kind: "test-run",
    projectName: projectLabel,
    title: group.plan?.title || labels.fallbackTitle,
    status: run.status,
    statusLabel,
    contextLabel: projectLabel,
    detailLabel: labels.counts(run),
    priority: statusPriority(run.status),
    updatedAt: new Date(latestTimestamp(group.runs)).toISOString(),
    to: "/tests/runs/$runId",
    params: { runId: run.id },
    search: { tab: "runs" },
  };
}

function selectActiveWorkItems(issueItems: ShellActiveWorkItem[], testItems: ShellActiveWorkItem[]): ShellActiveWorkItem[] {
  const sortedIssues = [...issueItems].sort(compareActiveWorkPriority);
  const sortedTests = [...testItems].sort(compareActiveWorkPriority);
  const selected = [...sortedIssues.slice(0, 3), ...sortedTests.slice(0, 2)];
  if (selected.length < 5) {
    const selectedIds = new Set(selected.map((item) => item.id));
    for (const item of [...sortedIssues, ...sortedTests].sort(compareActiveWorkPriority)) {
      if (selected.length >= 5) break;
      if (selectedIds.has(item.id)) continue;
      selected.push(item);
      selectedIds.add(item.id);
    }
  }
  return selected.sort(compareActiveWorkPriority);
}

function defaultPasswordAuthMode(source: ServerBaseUrlSource): PasswordAuthMode {
  return isConfiguredTeamServer(source) ? "login" : "register";
}

function normalizeInviteToken(value: string): string {
  const raw = String(value || "").trim();
  if (!raw) return "";
  if (raw.startsWith("msi_")) return raw;
  try {
    const parsed = new URL(raw);
    const candidates = [
      parsed.hostname,
      parsed.pathname.split("/").filter(Boolean).at(-1) || "",
      parsed.searchParams.get("token") || "",
    ];
    return candidates.find((candidate) => candidate.startsWith("msi_")) || "";
  } catch {
    return "";
  }
}

function normalizeInviteServer(value: string): string {
  const raw = String(value || "").trim();
  if (!raw) return "";
  try {
    const parsed = new URL(raw);
    const server = parsed.searchParams.get("server") || parsed.searchParams.get("serverUrl") || "";
    if (!server) return "";
    const serverUrl = new URL(server);
    if (serverUrl.protocol !== "http:" && serverUrl.protocol !== "https:") return "";
    return serverUrl.origin.replace(/\/+$/, "");
  } catch {
    return "";
  }
}

function inviteHashForToken(token: string): string {
  return `#/invite/${encodeURIComponent(token)}`;
}

function inviteTokenFromHash(hash: string): string {
  const match = hash.match(/#\/invite\/([^/?#]+)/);
  return normalizeInviteToken(match?.[1] ? decodeURIComponent(match[1]) : "");
}

function RootShell() {
  const { t } = useMspaceTranslation();
  const initialServerBaseUrl = getControlPlaneBaseUrl();
  const initialServerBaseUrlSource = window.mspaceDesktop?.serverBaseUrlSource || "default";
  const [authToken, setAuthToken] = useState(() => window.localStorage.getItem(AUTH_TOKEN_STORAGE_KEY) || "");
  const [selectedWorkspaceId, setSelectedWorkspaceId] = useState(() => window.localStorage.getItem(SELECTED_WORKSPACE_STORAGE_KEY) || "");
  const [serverBaseUrl, setServerBaseUrlState] = useState(initialServerBaseUrl);
  const [serverBaseUrlSource, setServerBaseUrlSource] = useState<ServerBaseUrlSource>(initialServerBaseUrlSource);
  const [serverBaseUrlLocked, setServerBaseUrlLocked] = useState(Boolean(window.mspaceDesktop?.serverBaseUrlLocked));
  const [serverUrlDraft, setServerUrlDraft] = useState(initialServerBaseUrl);
  const [serverUrlSaved, setServerUrlSaved] = useState(false);
  const [serverUrlError, setServerUrlError] = useState("");
  const [serverUrlChecking, setServerUrlChecking] = useState(false);
  const [serverUrlSaving, setServerUrlSaving] = useState(false);
  const [pendingAuthState, setPendingAuthState] = useState("");
  const [passwordAuthMode, setPasswordAuthMode] = useState<PasswordAuthMode>(() => defaultPasswordAuthMode(initialServerBaseUrlSource));
  const [passwordAuthLogin, setPasswordAuthLogin] = useState("");
  const [passwordAuthPassword, setPasswordAuthPassword] = useState("");
  const [passwordAuthName, setPasswordAuthName] = useState("");
  const [pendingInviteToken, setPendingInviteToken] = useState("");
  const [acceptedInviteToken, setAcceptedInviteToken] = useState("");
  const [inviteAcceptError, setInviteAcceptError] = useState("");
  const [teamWorkspaceModalOpen, setTeamWorkspaceModalOpen] = useState(false);
  const [teamWorkspaceName, setTeamWorkspaceName] = useState("");
  const authTokenRef = useRef(authToken);
  const serverBaseUrlRef = useRef(serverBaseUrl);

  useEffect(() => {
    authTokenRef.current = authToken;
  }, [authToken]);

  useEffect(() => {
    serverBaseUrlRef.current = serverBaseUrl;
  }, [serverBaseUrl]);

  function clearAuthState() {
    void window.mspaceDesktop?.stopPersonalWorker?.().catch((error) => {
      console.warn(`Failed to stop personal worker while clearing auth state: ${error instanceof Error ? error.message : String(error)}`);
    });
    window.localStorage.removeItem(AUTH_TOKEN_STORAGE_KEY);
    window.localStorage.removeItem(SELECTED_WORKSPACE_STORAGE_KEY);
    setStoredAuthIdentity(null);
    setAuthToken("");
    setSelectedWorkspaceId("");
    setPendingAuthState("");
    void queryClient.clear();
  }

  function updatePendingInviteToken(token: string, options?: { navigate?: boolean }) {
    const normalized = normalizeInviteToken(token);
    const inviteServer = normalizeInviteServer(token);
    setAcceptedInviteToken("");
    setInviteAcceptError("");
    void window.mspaceDesktop?.setPendingInviteToken?.(normalized);
    if (!normalized) {
      setPendingInviteToken("");
      return;
    }

    if (inviteServer && inviteServer !== serverBaseUrlRef.current) {
      void (async () => {
        try {
          const result = await window.mspaceDesktop?.setServerBaseUrl?.(inviteServer);
          applyServerConfig(result || { baseUrl: inviteServer, source: "user", locked: false });
          setPendingInviteToken(normalized);
        } catch (error) {
          setInviteAcceptError(error instanceof Error ? error.message : t("auth.invitePreviewError"));
          setPendingInviteToken(normalized);
        }
      })();
      return;
    }

    const shouldNavigate = options?.navigate ?? authTokenRef.current !== "";
    if (shouldNavigate) {
      setPendingInviteToken("");
      const inviteHash = inviteHashForToken(normalized);
      if (window.location.hash !== inviteHash) {
        window.location.hash = inviteHash;
      }
      return;
    }

    setPendingInviteToken(normalized);
    if (inviteTokenFromHash(window.location.hash) === normalized) {
      window.location.hash = "#/inbox";
    }
  }

  function applyServerConfig(config: { baseUrl: string; source: ServerBaseUrlSource; locked: boolean }) {
    setControlPlaneBaseUrl(config.baseUrl);
    setServerBaseUrlState(config.baseUrl);
    setServerBaseUrlSource(config.source);
    setServerBaseUrlLocked(config.locked);
    setServerUrlDraft(config.baseUrl);
    setServerUrlError("");
    setPasswordAuthMode(defaultPasswordAuthMode(config.source));
    setPasswordAuthPassword("");
    clearAuthState();
  }

  const meQuery = useQuery({
    queryKey: [...queryKeys.authMe(authToken), serverBaseUrl],
    queryFn: () => controlPlaneApi.me(authToken),
    enabled: authToken !== "",
    retry: false,
  });
  const serverHealthQuery = useQuery({
    queryKey: ["server-health", serverBaseUrl],
    queryFn: async () => {
      const response = await fetch(new URL("/health", serverBaseUrl).toString());
      if (!response.ok) throw new Error(t("auth.serverHealthFailed"));
      const payload = (await response.json()) as ServerHealthPayload;
      if (!hasExpectedServerHealth(payload)) {
        throw new Error(t("auth.serverHealthInvalid"));
      }
      return payload;
    },
    enabled: authToken === "",
    retry: false,
    refetchOnWindowFocus: false,
  });
  const invitePreviewQuery = useQuery({
    queryKey: ["workspace-invitation-preview", serverBaseUrl, pendingInviteToken],
    queryFn: () => controlPlaneApi.previewWorkspaceInvitation(pendingInviteToken),
    enabled: pendingInviteToken !== "" && authToken === "" && serverHealthQuery.isSuccess,
    retry: false,
    refetchOnWindowFocus: false,
  });
  const acceptInviteMutation = useMutation({
    mutationFn: (input: { authToken: string; inviteToken: string }) =>
      controlPlaneApi.acceptWorkspaceInvitation(input.authToken, input.inviteToken),
    onSuccess: async (result, input) => {
      const authMeKey = queryKeys.authMe(input.authToken);
      setAcceptedInviteToken(input.inviteToken);
      setPendingInviteToken("");
      setInviteAcceptError("");
      void window.mspaceDesktop?.setPendingInviteToken?.("");
      queryClient.setQueriesData<AuthMeResult | undefined>({ queryKey: authMeKey }, (current) => {
        if (!current) return current;
        const hasAcceptedWorkspace = result.workspaces.some((workspace) => workspace.id === result.workspace.id);
        return {
          ...current,
          workspaces: hasAcceptedWorkspace ? result.workspaces : [result.workspace, ...result.workspaces],
        };
      });
      window.localStorage.setItem(SELECTED_WORKSPACE_STORAGE_KEY, result.workspace.id);
      setSelectedWorkspaceId(result.workspace.id);
      await queryClient.invalidateQueries({ queryKey: authMeKey });
      window.localStorage.setItem(SELECTED_WORKSPACE_STORAGE_KEY, result.workspace.id);
      setSelectedWorkspaceId(result.workspace.id);
      window.location.hash = "#/inbox";
    },
    onError: (error, input) => {
      setPendingInviteToken("");
      setInviteAcceptError(error instanceof Error ? error.message : t("auth.acceptInviteFailed"));
      void window.mspaceDesktop?.setPendingInviteToken?.("");
      if (input.inviteToken) {
        window.location.hash = inviteHashForToken(input.inviteToken);
      }
    },
  });
  const signInMutation = useMutation({
    mutationFn: controlPlaneApi.startGitHubLogin,
    onSuccess: async (result) => {
      setPendingAuthState(result.state);
      if (window.mspaceDesktop?.openExternal) {
        await window.mspaceDesktop.openExternal(result.authorizeUrl);
        return;
      }
      window.open(result.authorizeUrl, "_blank", "noopener,noreferrer");
    },
  });
  const pollQuery = useQuery({
    queryKey: [...queryKeys.authPoll(pendingAuthState), serverBaseUrl],
    queryFn: () => controlPlaneApi.pollGitHubLogin(pendingAuthState),
    enabled: pendingAuthState !== "",
    refetchInterval: (query) => (query.state.data?.pending === false ? false : 1_500),
    retry: false,
  });

  function applyAuthResult(result: AuthMeResult & { token: string }) {
    window.localStorage.setItem(AUTH_TOKEN_STORAGE_KEY, result.token);
    setStoredAuthIdentity(result.user);
    setAuthToken(result.token);
    const firstWorkspaceId = result.workspaces[0]?.id || "";
    const inviteToken = pendingInviteToken && acceptedInviteToken !== pendingInviteToken ? pendingInviteToken : "";
    if (firstWorkspaceId && !inviteToken) {
      window.localStorage.setItem(SELECTED_WORKSPACE_STORAGE_KEY, firstWorkspaceId);
      setSelectedWorkspaceId(firstWorkspaceId);
    }
    setPendingAuthState("");
    if (inviteToken) {
      window.location.hash = "#/inbox";
      acceptInviteMutation.mutate({ authToken: result.token, inviteToken });
    }
  }

  const passwordAuthMutation = useMutation({
    mutationFn: () => {
      signInMutation.reset();
      const input = {
        login: passwordAuthLogin.trim(),
        password: passwordAuthPassword,
        name: passwordAuthName.trim(),
      };
      return passwordAuthMode === "register"
        ? controlPlaneApi.registerWithPassword(input)
        : controlPlaneApi.loginWithPassword(input);
    },
    onSuccess: (result) => {
      applyAuthResult(result);
      setPasswordAuthPassword("");
    },
  });

  useEffect(() => {
    if (!pollQuery.data || pollQuery.data.pending) return;
    applyAuthResult(pollQuery.data);
  }, [pollQuery.data]);

  useEffect(() => {
    let disposed = false;
    void window.mspaceDesktop?.getPendingInviteToken?.().then((token) => {
      if (!disposed && token) updatePendingInviteToken(token, { navigate: authTokenRef.current !== "" });
    });
    const unsubscribe = window.mspaceDesktop?.onInviteToken?.((token) => {
      updatePendingInviteToken(token, { navigate: authTokenRef.current !== "" });
    });
    return () => {
      disposed = true;
      unsubscribe?.();
    };
  }, []);

  useEffect(() => {
    const token = inviteTokenFromHash(window.location.hash);
    if (token && token !== pendingInviteToken) {
      updatePendingInviteToken(token, { navigate: authToken !== "" });
    }
  }, [authToken, pendingInviteToken]);

  useEffect(() => {
    if (meQuery.data?.user) {
      setStoredAuthIdentity(meQuery.data.user);
    }
  }, [meQuery.data?.user]);

  async function checkServerUrlCandidate(url: string) {
    const baseUrl = normalizeServerInput(url);
    const response = await fetch(new URL("/health", baseUrl).toString());
    if (!response.ok) throw new Error(t("auth.serverHealthFailed"));
    const payload = (await response.json()) as ServerHealthPayload;
    if (!hasExpectedServerHealth(payload)) {
      throw new Error(t("auth.serverHealthInvalid"));
    }
    return baseUrl;
  }

  async function handleTestServerUrl() {
    setServerUrlChecking(true);
    setServerUrlSaved(false);
    setServerUrlError("");
    try {
      await checkServerUrlCandidate(serverUrlDraft);
      setServerUrlSaved(true);
    } catch (error) {
      setServerUrlError(error instanceof Error ? error.message : t("auth.serverCheckFailed"));
    } finally {
      setServerUrlChecking(false);
    }
  }

  async function handleSaveServerUrl() {
    setServerUrlSaving(true);
    setServerUrlSaved(false);
    setServerUrlError("");
    try {
      const baseUrl = await checkServerUrlCandidate(serverUrlDraft);
      const result = await window.mspaceDesktop?.setServerBaseUrl?.(baseUrl);
      applyServerConfig(result || { baseUrl, source: "user", locked: false });
      setServerUrlSaved(true);
    } catch (error) {
      setServerUrlError(error instanceof Error ? error.message : t("auth.serverSaveFailed"));
    } finally {
      setServerUrlSaving(false);
    }
  }

  async function handleResetServerUrl() {
    setServerUrlSaving(true);
    setServerUrlSaved(false);
    setServerUrlError("");
    try {
      const result = await window.mspaceDesktop?.resetServerBaseUrl?.();
      applyServerConfig(result || { baseUrl: "http://127.0.0.1:8787", source: "default", locked: false });
      setServerUrlSaved(true);
    } catch (error) {
      setServerUrlError(error instanceof Error ? error.message : t("auth.serverResetFailed"));
    } finally {
      setServerUrlSaving(false);
    }
  }

  useEffect(() => {
    if (!authToken || !meQuery.error) return;
    const message = meQuery.error instanceof Error ? meQuery.error.message.toLowerCase() : "";
    if (!message.includes("invalid authorization") && !message.includes("missing authorization")) return;

    clearAuthState();
  }, [authToken, meQuery.error]);

  useEffect(() => {
    if (!meQuery.data?.user?.name) return;
    setTeamWorkspaceName((value) => value.trim() || defaultTeamWorkspaceName(meQuery.data?.user.name));
  }, [meQuery.data?.user?.name]);

  const workspaces = meQuery.data?.workspaces || [];
  const currentWorkspace = useMemo(() => {
    if (workspaces.length === 0) return undefined;
    return workspaces.find((workspace) => workspace.id === selectedWorkspaceId) || workspaces[0];
  }, [selectedWorkspaceId, workspaces]);

  useEffect(() => {
    if (workspaces.length === 0) return;
    if (currentWorkspace?.id && currentWorkspace.id !== selectedWorkspaceId) {
      window.localStorage.setItem(SELECTED_WORKSPACE_STORAGE_KEY, currentWorkspace.id);
      setSelectedWorkspaceId(currentWorkspace.id);
    }
  }, [currentWorkspace?.id, selectedWorkspaceId, workspaces.length]);
  const serverWorkspaceReady = authToken !== "" && Boolean(currentWorkspace?.id);
  const workspaceIssuesQueryKey = queryKeys.workspaceIssues(currentWorkspace?.id || "", authToken);
  const workspaceProjectsQueryKey = queryKeys.workspaceProjects(currentWorkspace?.id || "", authToken);
  const workspaceTestPlansQueryKey = queryKeys.workspaceTestPlans(currentWorkspace?.id || "", authToken);
  const workspaceTestRunsQueryKey = queryKeys.workspaceTestRuns(currentWorkspace?.id || "", authToken);
  const issuesQuery = useQuery({
    queryKey: workspaceIssuesQueryKey,
    queryFn: () => controlPlaneApi.listIssues(authToken, currentWorkspace?.id || ""),
    enabled: serverWorkspaceReady,
  });
  const projectsQuery = useQuery({
    queryKey: workspaceProjectsQueryKey,
    queryFn: () => controlPlaneApi.listProjects(authToken, currentWorkspace?.id || ""),
    enabled: serverWorkspaceReady,
  });
  const inboxQuery = useQuery({
    queryKey: queryKeys.workspaceInbox(currentWorkspace?.id || "", authToken),
    queryFn: () => controlPlaneApi.listInbox(authToken, currentWorkspace?.id || ""),
    enabled: serverWorkspaceReady,
    refetchInterval: serverWorkspaceReady ? 5_000 : false,
  });
  const testPlansQuery = useQuery({
    queryKey: workspaceTestPlansQueryKey,
    queryFn: () => controlPlaneApi.listWorkspaceTestPlans(authToken, currentWorkspace?.id || ""),
    enabled: serverWorkspaceReady,
    refetchInterval: serverWorkspaceReady ? 5_000 : false,
  });
  const testRunsQuery = useQuery({
    queryKey: workspaceTestRunsQueryKey,
    queryFn: () => controlPlaneApi.listWorkspaceTestRuns(authToken, currentWorkspace?.id || ""),
    enabled: serverWorkspaceReady,
    refetchInterval: serverWorkspaceReady ? 5_000 : false,
  });

  useEffect(() => {
    if (!serverWorkspaceReady) return;
    void fetch(`${getControlPlaneBaseUrl()}/health`).catch(() => undefined);
  }, [serverWorkspaceReady]);

  const handleSignOut = () => {
    clearAuthState();
  };

  const handleSelectWorkspace = (workspaceId: string) => {
    window.localStorage.setItem(SELECTED_WORKSPACE_STORAGE_KEY, workspaceId);
    setSelectedWorkspaceId(workspaceId);
  };

  const createTeamWorkspace = useMutation({
    mutationFn: (input: CreateWorkspaceInput) => controlPlaneApi.createWorkspace(authToken, input),
    onSuccess: async (result) => {
      queryClient.setQueryData<AuthMeResult | undefined>(queryKeys.authMe(authToken), (current) =>
        current ? { ...current, workspaces: result.workspaces } : current,
      );
      handleSelectWorkspace(result.workspace.id);
      await queryClient.invalidateQueries({ queryKey: queryKeys.authMe(authToken) });
      setTeamWorkspaceModalOpen(false);
      setTeamWorkspaceName(defaultTeamWorkspaceName(meQuery.data?.user.name));
    },
  });

  function submitTeamWorkspace(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!authToken || !meQuery.data?.isServerAdmin) return;
    createTeamWorkspace.mutate({
      name: teamWorkspaceName.trim(),
      kind: "team",
    });
  }

  const authError = passwordAuthMutation.error || signInMutation.error || pollQuery.error || (authToken !== "" ? meQuery.error : null);
  const configuredTeamServer = isConfiguredTeamServer(serverBaseUrlSource);
  const githubAuthAvailable = configuredTeamServer && serverSupportsGitHubAuth(serverHealthQuery.data);
  const inviteAccepting = pendingInviteToken !== "" && acceptInviteMutation.isPending;
  const accountStatus =
    authToken && meQuery.data && !inviteAccepting
      ? "signed-in"
      : inviteAccepting || pendingAuthState || signInMutation.isPending
        ? "loading"
        : authError
          ? "error"
          : "signed-out";
  const inboxUnreadCount = useMemo(() => {
    return (inboxQuery.data || []).length;
  }, [inboxQuery.data]);
  const invitePreview = invitePreviewQuery.data;
  const invitePreviewError =
    serverHealthQuery.error instanceof Error
      ? serverHealthQuery.error.message
      : invitePreviewQuery.error instanceof Error
        ? invitePreviewQuery.error.message
        : "";
  const inviteAuthPrompt = pendingInviteToken
    ? {
        token: pendingInviteToken,
        preview: invitePreview,
        error: invitePreviewError,
        accepting: acceptInviteMutation.isPending,
        acceptError: inviteAcceptError,
      }
    : undefined;
  const searchItems = useMemo<ShellSearchItem[]>(() => {
    const issueItems: ShellSearchItem[] = (issuesQuery.data || []).map((issue) => ({
      id: `issue:${issue.id}`,
      kind: "Issue",
      title: issue.title,
      subtitle: joinSearchSubtitle([issue.projectName || t("common.noProject"), issue.status, issue.labels.map((label) => label.name).join(", ")]),
      keywords: [
        issue.body,
        issue.projectName || t("common.noProject"),
        issue.status,
        issue.triageStatus,
        issue.assignee,
        issue.assigneeType,
        issue.labels.map((label) => `${label.name} ${label.key} ${label.dimension}`).join(" "),
      ],
      to: "/issues/$issueId",
      params: { issueId: issue.id },
    }));
    const projectItems: ShellSearchItem[] = (projectsQuery.data || []).map((project) => ({
      id: `project:${project.id}`,
      kind: "Project",
      title: project.name,
      subtitle: joinSearchSubtitle([
        project.gitOwner && project.gitRepo ? `${project.gitOwner}/${project.gitRepo}` : project.repoPath,
        t("projects.issues", { count: project.issueCount }),
      ]),
      keywords: [
        project.repoPath,
        project.remoteUrl,
        project.gitProvider,
        project.gitOwner,
        project.gitRepo,
        project.defaultBranch,
        project.namespace,
        project.kubeContext,
      ],
      to: "/projects",
    }));

    return [...issueItems, ...projectItems];
  }, [issuesQuery.data, projectsQuery.data, t]);
  const activeWorkItems = useMemo<ShellActiveWorkItem[]>(() => {
    const projectNameById = new Map((projectsQuery.data || []).map((project) => [project.id, project.name]));
    const issueItems = (issuesQuery.data || [])
      .filter(isActiveIssueWork)
      .map((issue) =>
        issueActiveWorkItem(
          issue,
          t(`issueStatus.${issue.status}`, { defaultValue: issue.status }),
          t("common.noProject"),
          t("navigation.activeIssueTasks", { completed: issue.completedChildIssueCount, total: issue.childIssueCount }),
        ),
      );
    const testRunItems = groupActiveTestRuns(testRunsQuery.data || [], testPlansQuery.data || []).map((group) =>
        testRunActiveWorkItem(
          group,
          {
            fallbackTitle: t("navigation.activeAdHocTestRun"),
            statusLabel: (status) => t(`tests.runStatusValue.${status}`, { defaultValue: status }),
            counts: (run) =>
              t("tests.runCounts", {
                passed: run.passedCount,
                failed: run.failedCount,
                blocked: run.blockedCount,
                skipped: run.skippedCount,
              }),
            runContext: (count) => t("navigation.activeTestRunCount", { count }),
            projectCount: (count) => t("navigation.activeProjectCount", { count }),
            noProject: t("common.noProject"),
          },
          projectNameById,
        ),
      );

    return selectActiveWorkItems(issueItems, testRunItems);
  }, [issuesQuery.data, projectsQuery.data, testPlansQuery.data, testRunsQuery.data, t]);

  const shell = (
    <MspaceAuthProvider
      value={{
        token: authToken,
        user: meQuery.data?.user,
        workspaces,
        workspace: currentWorkspace,
        selectedWorkspaceId: currentWorkspace?.id,
        selectWorkspace: handleSelectWorkspace,
        refreshAuth: () => queryClient.invalidateQueries({ queryKey: queryKeys.authMe(authToken) }),
        status: accountStatus,
      }}
    >
      <AppShell
        brandLogoSrc={mspaceLogoUrl}
        activeWorkItems={activeWorkItems}
        inboxUnreadCount={inboxUnreadCount}
        searchItems={searchItems}
        searchLoading={issuesQuery.isLoading || projectsQuery.isLoading}
        account={{
          status: accountStatus,
          name: meQuery.data?.user.name,
          email: meQuery.data?.user.email,
          avatarUrl: meQuery.data?.user.avatarUrl,
          identityProvider: meQuery.data?.identity?.provider,
          identityLogin: meQuery.data?.identity?.login,
          isServerAdmin: meQuery.data?.isServerAdmin,
          workspaceId: currentWorkspace?.id,
          workspaceName: currentWorkspace?.name,
          workspaceIcon: currentWorkspace?.icon,
          workspaceDescription: currentWorkspace?.description,
          workspaceKind: currentWorkspace?.kind,
          workspaceRole: currentWorkspace?.role,
          workspaces: workspaces.map((workspace) => ({
            id: workspace.id,
            name: workspace.name,
            role: workspace.role,
            kind: workspace.kind,
            icon: workspace.icon,
            description: workspace.description,
          })),
          error: authError instanceof Error ? authError.message : undefined,
          actionLabel: pendingAuthState ? t("workspace.waitingForGitHub") : undefined,
        }}
        onSignIn={() => {
          if (githubAuthAvailable) {
            signInMutation.mutate();
          }
        }}
        onSignOut={handleSignOut}
        onSelectWorkspace={handleSelectWorkspace}
        onCreateTeamWorkspace={() => {
          if (!meQuery.data?.isServerAdmin) return;
          setTeamWorkspaceName((value) => value.trim() || defaultTeamWorkspaceName(meQuery.data?.user.name));
          setTeamWorkspaceModalOpen(true);
        }}
      />
      {accountStatus !== "signed-in" ? (
        <AuthRequiredOverlay
          status={accountStatus}
          error={authError instanceof Error ? authError.message : undefined}
          actionLabel={pendingAuthState ? t("workspace.waitingForGitHub") : undefined}
          mode={passwordAuthMode}
          login={passwordAuthLogin}
          password={passwordAuthPassword}
          name={passwordAuthName}
          onModeChange={(mode) => {
            setPasswordAuthMode(mode);
            passwordAuthMutation.reset();
          }}
          onLoginChange={setPasswordAuthLogin}
          onPasswordChange={setPasswordAuthPassword}
          onNameChange={setPasswordAuthName}
          onPasswordSubmit={(event) => {
            event.preventDefault();
            passwordAuthMutation.mutate();
          }}
          onGitHubSignIn={() => {
            if (!githubAuthAvailable) return;
            passwordAuthMutation.reset();
            signInMutation.mutate();
          }}
          isBusy={pendingAuthState !== "" || signInMutation.isPending || passwordAuthMutation.isPending}
          isGitHubBusy={pendingAuthState !== "" || signInMutation.isPending}
          githubAuthAvailable={githubAuthAvailable}
          configuredTeamServer={configuredTeamServer}
          serverBaseUrl={serverBaseUrl}
          serverBaseUrlSource={serverBaseUrlSource}
          serverBaseUrlLocked={serverBaseUrlLocked}
          serverUrlDraft={serverUrlDraft}
          serverUrlError={serverUrlError}
          serverUrlSaved={serverUrlSaved}
          serverUrlChecking={serverUrlChecking}
          serverUrlSaving={serverUrlSaving}
          onServerUrlDraftChange={(value) => {
            setServerUrlDraft(value);
            setServerUrlSaved(false);
            setServerUrlError("");
          }}
          onTestServerUrl={() => void handleTestServerUrl()}
          onSaveServerUrl={() => void handleSaveServerUrl()}
          onResetServerUrl={() => void handleResetServerUrl()}
          invite={inviteAuthPrompt}
        />
      ) : null}
      {teamWorkspaceModalOpen ? (
        <Modal
          title={t("workspace.createTeamWorkspace")}
          description={t("workspace.createTeamDescription")}
          onClose={() => setTeamWorkspaceModalOpen(false)}
        >
          <form className="grid gap-4" onSubmit={submitTeamWorkspace}>
            {createTeamWorkspace.error ? <Notice tone="danger">{createTeamWorkspace.error.message}</Notice> : null}
            <Field label={t("workspace.workspaceName")}>
              <Input
                value={teamWorkspaceName}
                onChange={(event) => setTeamWorkspaceName(event.target.value)}
                placeholder={t("workspace.defaultTeamWorkspaceFallback")}
                autoFocus
              />
            </Field>
            <div className="flex justify-end gap-2 border-t border-[color:var(--line)] pt-4">
              <Button type="button" variant="secondary" onClick={() => setTeamWorkspaceModalOpen(false)}>
                {t("common.cancel")}
              </Button>
              <Button type="submit" disabled={!authToken || !meQuery.data?.isServerAdmin || createTeamWorkspace.isPending || teamWorkspaceName.trim() === ""}>
                <UsersRound data-icon />
                {createTeamWorkspace.isPending ? t("common.creating") : t("workspace.createTeamWorkspace")}
              </Button>
            </div>
          </form>
        </Modal>
      ) : null}
    </MspaceAuthProvider>
  );
  return shell;
}

function AuthRequiredOverlay(props: {
  status: "signed-in" | "signed-out" | "loading" | "error";
  error?: string;
  actionLabel?: string;
  mode: "login" | "register";
  login: string;
  password: string;
  name: string;
  isBusy?: boolean;
  isGitHubBusy?: boolean;
  githubAuthAvailable: boolean;
  configuredTeamServer: boolean;
  onModeChange: (mode: PasswordAuthMode) => void;
  onLoginChange: (value: string) => void;
  onPasswordChange: (value: string) => void;
  onNameChange: (value: string) => void;
  onPasswordSubmit: (event: FormEvent<HTMLFormElement>) => void;
  onGitHubSignIn: () => void;
  serverBaseUrl: string;
  serverBaseUrlSource: ServerBaseUrlSource;
  serverBaseUrlLocked: boolean;
  serverUrlDraft: string;
  serverUrlError: string;
  serverUrlSaved: boolean;
  serverUrlChecking: boolean;
  serverUrlSaving: boolean;
  onServerUrlDraftChange: (value: string) => void;
  onTestServerUrl: () => void;
  onSaveServerUrl: () => void;
  onResetServerUrl: () => void;
  invite?: {
    token: string;
    preview?: WorkspaceInvitationPreview;
    error: string;
    accepting: boolean;
    acceptError: string;
  };
}) {
  const busy = props.status === "loading" || props.isBusy;
  const { t } = useMspaceTranslation();
  const [teamServerSettingsOpen, setTeamServerSettingsOpen] = useState(props.serverBaseUrlLocked);
  const passwordDisabled = busy || props.login.trim() === "" || props.password === "" || (props.mode === "register" && props.password.length < 8);
  const serverUrlDirty = props.serverUrlDraft.trim().replace(/\/+$/, "") !== props.serverBaseUrl;
  const serverUrlControlsDisabled = props.serverBaseUrlLocked || props.serverUrlChecking || props.serverUrlSaving;

  useEffect(() => {
    if (props.serverBaseUrlLocked || props.serverUrlError) {
      setTeamServerSettingsOpen(true);
    }
  }, [props.serverBaseUrlLocked, props.serverUrlError]);

  const serverSourceLabel =
    props.serverBaseUrlSource === "environment"
      ? t("auth.serverSource.environment")
      : props.serverBaseUrlSource === "user"
        ? t("auth.serverSource.user")
        : t("auth.serverSource.default");

  return (
    <div className="fixed inset-0 z-[90] grid place-items-center bg-[color:var(--canvas)] px-6">
      <section className="w-full max-w-[480px] rounded-[12px] bg-[color:var(--paper)] px-6 py-6 shadow-[0_24px_80px_rgba(0,0,0,0.14),inset_0_0_0_1px_var(--line)]">
        <div className="flex items-center gap-3">
          <span className="grid size-10 place-items-center rounded-[10px] bg-[color:var(--block)] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
            {busy ? <LoaderCircle data-icon className="animate-spin" /> : <LogIn data-icon />}
          </span>
          <div className="min-w-0">
            <h1 className="text-[17px] font-semibold leading-6 text-[color:var(--text)]">
              {props.invite?.preview?.workspaceName
                ? t("auth.inviteTitle", { workspace: props.invite.preview.workspaceName })
                : props.configuredTeamServer
                  ? t("auth.teamServerTitle")
                  : t("auth.localAccountTitle")}
            </h1>
            <p className="mt-1 text-[13px] leading-5 text-[color:var(--muted)]">
              {props.invite
                ? t("auth.inviteDescription")
                : props.configuredTeamServer
                  ? t("auth.teamServerDescription")
                  : t("auth.localAccountDescription")}
            </p>
          </div>
        </div>
        {props.error ? <div className="mt-4"><Notice tone="danger">{props.error}</Notice></div> : null}
        {props.invite?.accepting ? (
          <div className="mt-4 inline-flex items-center gap-2 text-[12px] font-medium leading-5 text-[color:var(--muted-strong)]">
            <LoaderCircle data-icon className="animate-spin" />
            {t("auth.acceptingInvite")}
          </div>
        ) : null}
        {props.invite?.error || props.invite?.acceptError ? (
          <div className="mt-4">
            <Notice tone="danger">{props.invite.acceptError || props.invite.error || t("auth.invitePreviewError")}</Notice>
          </div>
        ) : null}
        <div className="mt-5 grid grid-cols-2 rounded-[8px] bg-[color:var(--block)] p-1 shadow-[inset_0_0_0_1px_var(--line)]">
          {(["login", "register"] as const).map((mode) => (
            <button
              key={mode}
              type="button"
              className={`h-8 rounded-[6px] text-[12px] font-medium transition-colors ${
                props.mode === mode ? "bg-[color:var(--paper)] text-[color:var(--text)] shadow-[inset_0_0_0_1px_var(--line)]" : "text-[color:var(--muted)]"
              }`}
              disabled={busy}
              onClick={() => props.onModeChange(mode)}
            >
              {mode === "login" ? t("auth.passwordLoginTab") : t("auth.passwordRegisterTab")}
            </button>
          ))}
        </div>
        <form className="mt-4 grid gap-3" onSubmit={props.onPasswordSubmit}>
          <Field label={t("auth.loginLabel")}>
            <Input
              value={props.login}
              onChange={(event) => props.onLoginChange(event.target.value)}
              placeholder={t("auth.loginPlaceholder")}
              autoComplete="username"
              autoFocus
            />
          </Field>
          {props.mode === "register" ? (
            <Field label={t("auth.nameLabel")}>
              <Input
                value={props.name}
                onChange={(event) => props.onNameChange(event.target.value)}
                placeholder={t("auth.namePlaceholder")}
                autoComplete="name"
              />
            </Field>
          ) : null}
          <Field label={t("auth.passwordLabel")}>
            <Input
              type="password"
              value={props.password}
              onChange={(event) => props.onPasswordChange(event.target.value)}
              placeholder={t("auth.passwordPlaceholder")}
              autoComplete={props.mode === "register" ? "new-password" : "current-password"}
            />
          </Field>
          <Button type="submit" className="w-full justify-center" disabled={passwordDisabled}>
            {busy && !props.isGitHubBusy ? <LoaderCircle data-icon className="animate-spin" /> : <LogIn data-icon />}
            {props.mode === "register" ? t("auth.createAccount") : t("auth.signIn")}
          </Button>
        </form>
        {props.githubAuthAvailable ? (
          <>
            <div className="my-4 h-px bg-[color:var(--line)]" />
            <Button type="button" variant="secondary" className="w-full justify-center" disabled={busy} onClick={props.onGitHubSignIn}>
              {props.isGitHubBusy ? <LoaderCircle data-icon className="animate-spin" /> : <GitBranch data-icon />}
              {props.actionLabel || (props.isGitHubBusy ? t("workspace.waitingForGitHub") : t("workspace.signInWithGitHub"))}
            </Button>
          </>
        ) : null}
        <div className="mt-4 border-t border-[color:var(--line)] pt-3">
          {teamServerSettingsOpen ? (
            <div className="rounded-[10px] bg-[color:var(--block)] px-3 py-3 shadow-[inset_0_0_0_1px_var(--line)]">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="text-[12px] font-semibold leading-5 text-[color:var(--muted-strong)]">{t("auth.serverSettingsTitle")}</div>
                  <p className="mt-0.5 text-[12px] leading-5 text-[color:var(--muted)] text-pretty">
                    {t("auth.serverSettingsDescription")}
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <span className="rounded-[6px] bg-[color:var(--paper)] px-1.5 py-0.5 text-[11px] font-medium leading-4 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
                    {serverSourceLabel}
                  </span>
                  <Button type="button" variant="ghost" size="sm" onClick={() => setTeamServerSettingsOpen(false)}>
                    {t("auth.hideServerSettings")}
                  </Button>
                </div>
              </div>
              <div className="mt-3">
                <Field label={t("auth.serverUrlLabel")} hint={props.serverBaseUrlLocked ? t("auth.serverUrlLockedHint") : t("auth.serverUrlHint")}>
                  <Input
                    value={props.serverUrlDraft}
                    onChange={(event) => props.onServerUrlDraftChange(event.target.value)}
                    placeholder="https://mspace.example.com"
                    disabled={props.serverBaseUrlLocked}
                    aria-invalid={Boolean(props.serverUrlError)}
                  />
                </Field>
              </div>
              {props.serverUrlError ? <div className="mt-3"><Notice tone="danger">{props.serverUrlError}</Notice></div> : null}
              {props.serverUrlSaved ? <p className="mt-2 text-[12px] leading-5 text-[color:var(--success)]">{t("auth.serverSaved")}</p> : null}
              <div className="mt-3 flex flex-wrap justify-end gap-2">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={serverUrlControlsDisabled || props.serverBaseUrlSource === "default"}
                  onClick={props.onResetServerUrl}
                >
                  {props.serverUrlSaving && !serverUrlDirty ? <LoaderCircle data-icon className="animate-spin" /> : null}
                  {t("auth.useLocalServer")}
                </Button>
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  disabled={serverUrlControlsDisabled || props.serverUrlDraft.trim() === ""}
                  onClick={props.onTestServerUrl}
                >
                  {props.serverUrlChecking ? <LoaderCircle data-icon className="animate-spin" /> : null}
                  {t("auth.testServer")}
                </Button>
                <Button
                  type="button"
                  size="sm"
                  disabled={serverUrlControlsDisabled || !serverUrlDirty || props.serverUrlDraft.trim() === ""}
                  onClick={props.onSaveServerUrl}
                >
                  {props.serverUrlSaving && serverUrlDirty ? <LoaderCircle data-icon className="animate-spin" /> : null}
                  {t("auth.saveServer")}
                </Button>
              </div>
            </div>
          ) : (
            <div className="flex justify-center">
              <button
                type="button"
                className="inline-flex min-h-8 max-w-full items-center gap-2 rounded-[7px] px-2 py-1 text-[12px] leading-5 text-[color:var(--muted)] transition-colors hover:bg-[color:var(--block)] hover:text-[color:var(--text)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[color:var(--focus)]"
                aria-expanded={teamServerSettingsOpen}
                onClick={() => setTeamServerSettingsOpen(true)}
              >
                <Server data-icon className="shrink-0" />
                <span className="truncate">
                  {props.configuredTeamServer ? t("auth.teamServerActive", { url: props.serverBaseUrl }) : t("auth.teamServerLink")}
                </span>
                {props.configuredTeamServer ? (
                  <span className="shrink-0 rounded-[6px] bg-[color:var(--block)] px-1.5 py-0.5 text-[11px] font-medium leading-4 text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
                    {serverSourceLabel}
                  </span>
                ) : null}
              </button>
            </div>
          )}
        </div>
      </section>
    </div>
  );
}

function Modal(props: { title: string; description: string; onClose: () => void; children: ReactNode }) {
  const { t } = useMspaceTranslation();
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/20 px-4 py-6" role="dialog" aria-modal="true" aria-labelledby="workspace-shell-modal-title">
      <div className="w-full max-w-[640px] rounded-[12px] bg-[color:var(--paper)] shadow-[0_24px_80px_rgba(0,0,0,0.18),inset_0_0_0_1px_var(--line)]">
        <div className="flex items-start justify-between gap-4 border-b border-[color:var(--line)] px-5 py-4">
          <div className="min-w-0">
            <h2 id="workspace-shell-modal-title" className="text-[17px] font-semibold leading-6 text-[color:var(--text)]">{props.title}</h2>
            <p className="mt-1 text-[13px] leading-5 text-[color:var(--muted)] text-pretty">{props.description}</p>
          </div>
          <Button type="button" variant="ghost" size="icon" aria-label={t("common.close")} onClick={props.onClose}>
            <X data-icon />
          </Button>
        </div>
        <div className="px-5 py-5">{props.children}</div>
      </div>
    </div>
  );
}

const rootRoute = createRootRoute({
  component: RootShell,
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: () => <Navigate to="/inbox" replace />,
});

const inboxRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/inbox",
  component: InboxPage,
});

const issuesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/issues",
  component: IssuesPage,
});

const testsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/tests",
  component: TestsPage,
});

const testCaseDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/tests/cases/$caseId",
  component: TestCaseDetailPage,
});

const testPlanDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/tests/plans/$planId",
  component: TestPlanDetailPage,
});

const testRunDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/tests/runs/$runId",
  component: TestRunDetailPage,
});

const issueDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/issues/$issueId",
  component: IssueDetailPage,
});

const issueCommitDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/issues/$issueId/commits/$commitSha",
  component: IssueCommitDetailPage,
});

const issueEvidenceSnapshotsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/issues/$issueId/evidence/snapshots",
  component: IssueEvidenceSnapshotsPage,
});

const issueEvidenceHistoryRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/issues/$issueId/evidence/history",
  component: IssueEvidenceHistoryPage,
});

const agentsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/agents",
  component: AgentsPage,
});

const clustersRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/clusters",
  component: ClustersPage,
});

const environmentsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/environments",
  component: ClustersPage,
});

const projectsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/projects",
  component: ProjectsPage,
});

const workspaceSettingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/settings",
  component: WorkspaceSettingsPage,
});

const workspaceInviteRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/invite/$token",
  component: WorkspaceInvitePage,
});

const sessionDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/sessions/$sessionId",
  component: SessionDetailPage,
});

const routeTree = rootRoute.addChildren([
  indexRoute,
  inboxRoute,
  issuesRoute,
  testsRoute,
  testCaseDetailRoute,
  testPlanDetailRoute,
  testRunDetailRoute,
  issueCommitDetailRoute,
  issueEvidenceSnapshotsRoute,
  issueEvidenceHistoryRoute,
  issueDetailRoute,
  agentsRoute,
  environmentsRoute,
  clustersRoute,
  projectsRoute,
  workspaceSettingsRoute,
  workspaceInviteRoute,
  sessionDetailRoute,
]);

const router = createRouter({
  routeTree,
  history: createHashHistory(),
});

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <MspaceToastProvider>
        <RouterProvider router={router} />
      </MspaceToastProvider>
    </QueryClientProvider>
  </React.StrictMode>,
);
