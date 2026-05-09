import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  createHashHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Navigate,
  RouterProvider,
} from "@tanstack/react-router";
import {
  AgentsPage,
  ClustersPage,
  InboxPage,
  IssueDetailPage,
  IssuesPage,
  ProjectsPage,
  SessionDetailPage,
} from "@mspace/views";
import { api, controlPlaneApi, queryKeys, setStoredAuthIdentity } from "@mspace/core";
import { AppShell } from "@mspace/ui";
import mspaceLogoUrl from "../../../assets/brand/mspace-logo.svg";
import "./globals.css";

const queryClient = new QueryClient();
const AUTH_TOKEN_STORAGE_KEY = "mspace.authToken";

function RootShell() {
  const [authToken, setAuthToken] = useState(() => window.localStorage.getItem(AUTH_TOKEN_STORAGE_KEY) || "");
  const [pendingAuthState, setPendingAuthState] = useState("");

  const activeWorkQuery = useQuery({
    queryKey: queryKeys.activeWork,
    queryFn: api.listActiveWork,
    refetchInterval: 15_000,
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
    setPendingAuthState("");
  }, [pollQuery.data]);

  useEffect(() => {
    if (meQuery.data?.user) {
      setStoredAuthIdentity(meQuery.data.user);
    }
  }, [meQuery.data?.user]);

  const handleSignOut = () => {
    window.localStorage.removeItem(AUTH_TOKEN_STORAGE_KEY);
    setStoredAuthIdentity(null);
    setAuthToken("");
    setPendingAuthState("");
  };

  const authError = signInMutation.error || pollQuery.error || (authToken !== "" ? meQuery.error : null);
  const accountStatus =
    authToken && meQuery.data
      ? "signed-in"
      : pendingAuthState || signInMutation.isPending
        ? "loading"
        : authError
          ? "error"
          : "signed-out";
  const currentWorkspace = meQuery.data?.workspaces[0];

  return (
    <AppShell
      brandLogoSrc={mspaceLogoUrl}
      activeWorkItems={activeWorkQuery.data || []}
      account={{
        status: accountStatus,
        name: meQuery.data?.user.name,
        email: meQuery.data?.user.email,
        avatarUrl: meQuery.data?.user.avatarUrl,
        workspaceName: currentWorkspace?.name,
        error: authError instanceof Error ? authError.message : undefined,
        actionLabel: pendingAuthState ? "Waiting for GitHub" : undefined,
      }}
      onSignIn={() => signInMutation.mutate()}
      onSignOut={handleSignOut}
    />
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

const sessionDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/sessions/$sessionId",
  component: SessionDetailPage,
});

const routeTree = rootRoute.addChildren([
  indexRoute,
  inboxRoute,
  issuesRoute,
  issueDetailRoute,
  agentsRoute,
  clustersRoute,
  projectsRoute,
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
