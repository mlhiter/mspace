import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  createHashHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Navigate,
  RouterProvider,
} from "@tanstack/react-router";
import { GitBranch, LoaderCircle, UsersRound, X } from "lucide-react";
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
  WorkspaceSettingsPage,
  WorkspaceInvitePage,
} from "@mspace/views";
import {
  AUTH_TOKEN_STORAGE_KEY,
  controlPlaneApi,
  getControlPlaneBaseUrl,
  queryKeys,
  SELECTED_WORKSPACE_STORAGE_KEY,
  setStoredAuthIdentity,
  type AuthMeResult,
  type CreateWorkspaceInput,
} from "@mspace/core";
import { AppShell, Button, Field, Input, Notice, type ShellSearchItem } from "@mspace/ui";
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

function RootShell() {
  const { t } = useMspaceTranslation();
  const [authToken, setAuthToken] = useState(() => window.localStorage.getItem(AUTH_TOKEN_STORAGE_KEY) || "");
  const [selectedWorkspaceId, setSelectedWorkspaceId] = useState(() => window.localStorage.getItem(SELECTED_WORKSPACE_STORAGE_KEY) || "");
  const [pendingAuthState, setPendingAuthState] = useState("");
  const [teamWorkspaceModalOpen, setTeamWorkspaceModalOpen] = useState(false);
  const [teamWorkspaceName, setTeamWorkspaceName] = useState("");

  const meQuery = useQuery({
    queryKey: queryKeys.authMe(authToken),
    queryFn: () => controlPlaneApi.me(authToken),
    enabled: authToken !== "",
    retry: false,
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
    queryKey: queryKeys.authPoll(pendingAuthState),
    queryFn: () => controlPlaneApi.pollGitHubLogin(pendingAuthState),
    enabled: pendingAuthState !== "",
    refetchInterval: (query) => (query.state.data?.pending === false ? false : 1_500),
    retry: false,
  });

  useEffect(() => {
    if (!pollQuery.data || pollQuery.data.pending) return;
    window.localStorage.setItem(AUTH_TOKEN_STORAGE_KEY, pollQuery.data.token);
    setStoredAuthIdentity(pollQuery.data.user);
    setAuthToken(pollQuery.data.token);
    const firstWorkspaceId = pollQuery.data.workspaces[0]?.id || "";
    if (firstWorkspaceId) {
      window.localStorage.setItem(SELECTED_WORKSPACE_STORAGE_KEY, firstWorkspaceId);
      setSelectedWorkspaceId(firstWorkspaceId);
    }
    setPendingAuthState("");
  }, [pollQuery.data]);

  useEffect(() => {
    if (meQuery.data?.user) {
      setStoredAuthIdentity(meQuery.data.user);
    }
  }, [meQuery.data?.user]);

  useEffect(() => {
    if (!authToken || !meQuery.error) return;
    const message = meQuery.error instanceof Error ? meQuery.error.message.toLowerCase() : "";
    if (!message.includes("invalid authorization") && !message.includes("missing authorization")) return;

    window.localStorage.removeItem(AUTH_TOKEN_STORAGE_KEY);
    window.localStorage.removeItem(SELECTED_WORKSPACE_STORAGE_KEY);
    setStoredAuthIdentity(null);
    setAuthToken("");
    setSelectedWorkspaceId("");
    setPendingAuthState("");
    void queryClient.clear();
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

  useEffect(() => {
    if (!serverWorkspaceReady) return;
    void fetch(`${getControlPlaneBaseUrl()}/health`).catch(() => undefined);
  }, [serverWorkspaceReady]);

  const handleSignOut = () => {
    window.localStorage.removeItem(AUTH_TOKEN_STORAGE_KEY);
    window.localStorage.removeItem(SELECTED_WORKSPACE_STORAGE_KEY);
    setStoredAuthIdentity(null);
    setAuthToken("");
    setSelectedWorkspaceId("");
    setPendingAuthState("");
    void queryClient.clear();
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
    if (!authToken) return;
    createTeamWorkspace.mutate({
      name: teamWorkspaceName.trim(),
      kind: "team",
    });
  }

  const authError = signInMutation.error || pollQuery.error || (authToken !== "" ? meQuery.error : null);
  const accountStatus =
    authToken && meQuery.data
      ? "signed-in"
      : pendingAuthState || signInMutation.isPending
        ? "loading"
        : authError
          ? "error"
          : "signed-out";
  const inboxUnreadCount = useMemo(() => {
    return (inboxQuery.data || []).length;
  }, [inboxQuery.data]);
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

  const shell = (
    <MspaceAuthProvider
      value={{
        token: authToken,
        user: meQuery.data?.user,
        workspaces,
        workspace: currentWorkspace,
        selectedWorkspaceId: currentWorkspace?.id,
        selectWorkspace: handleSelectWorkspace,
        status: accountStatus,
      }}
    >
      <AppShell
        brandLogoSrc={mspaceLogoUrl}
        activeWorkItems={[]}
        inboxUnreadCount={inboxUnreadCount}
        searchItems={searchItems}
        searchLoading={issuesQuery.isLoading || projectsQuery.isLoading}
        account={{
          status: accountStatus,
          name: meQuery.data?.user.name,
          email: meQuery.data?.user.email,
          avatarUrl: meQuery.data?.user.avatarUrl,
          workspaceId: currentWorkspace?.id,
          workspaceName: currentWorkspace?.name,
          workspaceKind: currentWorkspace?.kind,
          workspaceRole: currentWorkspace?.role,
          workspaces: workspaces.map((workspace) => ({ id: workspace.id, name: workspace.name, role: workspace.role, kind: workspace.kind })),
          error: authError instanceof Error ? authError.message : undefined,
          actionLabel: pendingAuthState ? t("workspace.waitingForGitHub") : undefined,
        }}
        onSignIn={() => signInMutation.mutate()}
        onSignOut={handleSignOut}
        onSelectWorkspace={handleSelectWorkspace}
        onCreateTeamWorkspace={() => {
          setTeamWorkspaceName((value) => value.trim() || defaultTeamWorkspaceName(meQuery.data?.user.name));
          setTeamWorkspaceModalOpen(true);
        }}
      />
      {accountStatus !== "signed-in" ? (
        <AuthRequiredOverlay
          status={accountStatus}
          error={authError instanceof Error ? authError.message : undefined}
          actionLabel={pendingAuthState ? t("workspace.waitingForGitHub") : undefined}
          onSignIn={() => signInMutation.mutate()}
          isBusy={pendingAuthState !== "" || signInMutation.isPending}
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
              <Button type="submit" disabled={!authToken || createTeamWorkspace.isPending || teamWorkspaceName.trim() === ""}>
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
  isBusy?: boolean;
  onSignIn: () => void;
}) {
  const busy = props.status === "loading" || props.isBusy;
  const { t } = useMspaceTranslation();
  return (
    <div className="fixed inset-0 z-[90] grid place-items-center bg-[color:var(--canvas)] px-6">
      <section className="w-full max-w-[420px] rounded-[12px] bg-[color:var(--paper)] px-6 py-6 shadow-[0_24px_80px_rgba(0,0,0,0.14),inset_0_0_0_1px_var(--line)]">
        <div className="flex items-center gap-3">
          <span className="grid size-10 place-items-center rounded-[10px] bg-[color:var(--block)] text-[color:var(--muted)] shadow-[inset_0_0_0_1px_var(--line)]">
            {busy ? <LoaderCircle data-icon className="animate-spin" /> : <GitBranch data-icon />}
          </span>
          <div className="min-w-0">
            <h1 className="text-[17px] font-semibold leading-6 text-[color:var(--text)]">{t("auth.signInTitle")}</h1>
            <p className="mt-1 text-[13px] leading-5 text-[color:var(--muted)]">
              {t("auth.signInDescription")}
            </p>
          </div>
        </div>
        {props.error ? <div className="mt-4"><Notice tone="danger">{props.error}</Notice></div> : null}
        <Button type="button" className="mt-5 w-full justify-center" disabled={busy} onClick={props.onSignIn}>
          {busy ? <LoaderCircle data-icon className="animate-spin" /> : <GitBranch data-icon />}
          {props.actionLabel || (busy ? t("workspace.waitingForGitHub") : t("workspace.signInWithGitHub"))}
        </Button>
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
  issueCommitDetailRoute,
  issueEvidenceSnapshotsRoute,
  issueEvidenceHistoryRoute,
  issueDetailRoute,
  agentsRoute,
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
      <RouterProvider router={router} />
    </QueryClientProvider>
  </React.StrictMode>,
);
