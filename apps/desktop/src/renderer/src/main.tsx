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
import { UsersRound, X } from "lucide-react";
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
  api,
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
import mspaceLogoUrl from "../../../assets/brand/mspace-logo.svg";
import "./globals.css";

const queryClient = new QueryClient();
function defaultTeamWorkspaceName(name: string | undefined) {
  const owner = name?.trim();
  return owner ? `${owner}'s team` : "Engineering team";
}

function joinSearchSubtitle(values: Array<string | number | null | undefined>): string {
  return values
    .map((value) => String(value || "").trim())
    .filter(Boolean)
    .join(" - ");
}

function RootShell() {
  const [authToken, setAuthToken] = useState(() => window.localStorage.getItem(AUTH_TOKEN_STORAGE_KEY) || "");
  const [selectedWorkspaceId, setSelectedWorkspaceId] = useState(() => window.localStorage.getItem(SELECTED_WORKSPACE_STORAGE_KEY) || "");
  const [pendingAuthState, setPendingAuthState] = useState("");
  const [teamWorkspaceModalOpen, setTeamWorkspaceModalOpen] = useState(false);
  const [teamWorkspaceName, setTeamWorkspaceName] = useState("");

  const activeWorkQuery = useQuery({
    queryKey: queryKeys.activeWork,
    queryFn: api.listActiveWork,
    refetchInterval: 15_000,
  });
  const issuesQuery = useQuery({
    queryKey: queryKeys.issues,
    queryFn: api.listIssues,
  });
  const projectsQuery = useQuery({
    queryKey: queryKeys.projects,
    queryFn: api.listProjects,
  });
  const localInboxQuery = useQuery({
    queryKey: queryKeys.inbox,
    queryFn: api.listInbox,
    refetchInterval: 5_000,
  });
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
  const isTeamWorkspace = currentWorkspace?.kind === "team";
  const teamInboxEnabled = authToken !== "" && Boolean(currentWorkspace?.id) && isTeamWorkspace;
  const teamInboxQuery = useQuery({
    queryKey: queryKeys.teamInbox(currentWorkspace?.id || "", authToken),
    queryFn: () => controlPlaneApi.listInbox(authToken, currentWorkspace?.id || ""),
    enabled: teamInboxEnabled,
    refetchInterval: teamInboxEnabled ? 5_000 : false,
  });

  useEffect(() => {
    void api.configureControlPlaneSession({
      serverBaseUrl: getControlPlaneBaseUrl(),
      token: authToken,
      workspaceId: currentWorkspace?.id || "",
    }).catch(() => {
      // The local runner may not be ready yet; Inbox still has server and local query fallbacks.
    });
  }, [authToken, currentWorkspace?.id]);

  const handleSignOut = () => {
    window.localStorage.removeItem(AUTH_TOKEN_STORAGE_KEY);
    window.localStorage.removeItem(SELECTED_WORKSPACE_STORAGE_KEY);
    setStoredAuthIdentity(null);
    setAuthToken("");
    setSelectedWorkspaceId("");
    setPendingAuthState("");
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
    const teamItems = teamInboxQuery.data || [];
    const teamIssueIds = new Set(teamItems.map((item) => item.issueId));
    const localOnlyItems = (localInboxQuery.data || []).filter((item) => !teamIssueIds.has(item.issueId));
    return teamItems.length + localOnlyItems.length;
  }, [localInboxQuery.data, teamInboxQuery.data]);
  const searchItems = useMemo<ShellSearchItem[]>(() => {
    const issueItems: ShellSearchItem[] = (issuesQuery.data || []).map((issue) => ({
      id: `issue:${issue.id}`,
      kind: "Issue",
      title: issue.title,
      subtitle: joinSearchSubtitle([issue.projectName, issue.status, issue.labels.map((label) => label.name).join(", ")]),
      keywords: [
        issue.body,
        issue.projectName,
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
        `${project.issueCount} issues`,
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
  }, [issuesQuery.data, projectsQuery.data]);

  return (
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
        activeWorkItems={activeWorkQuery.data || []}
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
          actionLabel: pendingAuthState ? "Waiting for GitHub" : undefined,
        }}
        onSignIn={() => signInMutation.mutate()}
        onSignOut={handleSignOut}
        onSelectWorkspace={handleSelectWorkspace}
        onCreateTeamWorkspace={() => {
          setTeamWorkspaceName((value) => value.trim() || defaultTeamWorkspaceName(meQuery.data?.user.name));
          setTeamWorkspaceModalOpen(true);
        }}
      />
      {teamWorkspaceModalOpen ? (
        <Modal
          title="Create team workspace"
          description="Team workspaces enable member invitations, shared Inbox receipts, worker registration, and team-mode sessions."
          onClose={() => setTeamWorkspaceModalOpen(false)}
        >
          <form className="grid gap-4" onSubmit={submitTeamWorkspace}>
            {createTeamWorkspace.error ? <Notice tone="danger">{createTeamWorkspace.error.message}</Notice> : null}
            <Field label="Workspace name">
              <Input
                value={teamWorkspaceName}
                onChange={(event) => setTeamWorkspaceName(event.target.value)}
                placeholder="Engineering team"
                autoFocus
              />
            </Field>
            <div className="flex justify-end gap-2 border-t border-[color:var(--line)] pt-4">
              <Button type="button" variant="secondary" onClick={() => setTeamWorkspaceModalOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={!authToken || createTeamWorkspace.isPending || teamWorkspaceName.trim() === ""}>
                <UsersRound data-icon />
                {createTeamWorkspace.isPending ? "Creating" : "Create workspace"}
              </Button>
            </div>
          </form>
        </Modal>
      ) : null}
    </MspaceAuthProvider>
  );
}

function Modal(props: { title: string; description: string; onClose: () => void; children: ReactNode }) {
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-black/20 px-4 py-6" role="dialog" aria-modal="true" aria-labelledby="workspace-shell-modal-title">
      <div className="w-full max-w-[640px] rounded-[12px] bg-[color:var(--paper)] shadow-[0_24px_80px_rgba(0,0,0,0.18),inset_0_0_0_1px_var(--line)]">
        <div className="flex items-start justify-between gap-4 border-b border-[color:var(--line)] px-5 py-4">
          <div className="min-w-0">
            <h2 id="workspace-shell-modal-title" className="text-[17px] font-semibold leading-6 text-[color:var(--text)]">{props.title}</h2>
            <p className="mt-1 text-[13px] leading-5 text-[color:var(--muted)] text-pretty">{props.description}</p>
          </div>
          <Button type="button" variant="ghost" size="icon" aria-label="Close" onClick={props.onClose}>
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
