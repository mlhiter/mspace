import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createHashRouter, Navigate, RouterProvider } from "react-router-dom";
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

const router = createHashRouter([
  {
    path: "/",
    element: <AppShell brandLogoSrc={mspaceLogoUrl} />,
    children: [
      { index: true, element: <Navigate to="/inbox" replace /> },
      { path: "/inbox", element: <InboxPage /> },
      { path: "/issues", element: <IssuesPage /> },
      { path: "/issues/:issueId", element: <IssueDetailPage /> },
      { path: "/agents", element: <AgentsPage /> },
      { path: "/projects", element: <ProjectsPage /> },
      { path: "/sessions/:sessionId", element: <SessionDetailPage /> },
    ],
  },
]);

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </React.StrictMode>,
);
