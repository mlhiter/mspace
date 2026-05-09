import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
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
  InboxPage,
  IssueDetailPage,
  IssuesPage,
  ProjectsPage,
  SessionDetailPage,
} from "@mspace/views";
import { AppShell } from "@mspace/ui";
import mspaceLogoUrl from "../../../assets/brand/mspace-logo.svg";
import "./globals.css";

const queryClient = new QueryClient();

const rootRoute = createRootRoute({
  component: () => <AppShell brandLogoSrc={mspaceLogoUrl} />,
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
