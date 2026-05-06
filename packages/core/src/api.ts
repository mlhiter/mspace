import type {
  CreateCommentInput,
  CreateIssueInput,
  CreateProjectInput,
  CreateSessionInput,
  InboxItem,
  IssueDetail,
  Project,
  SessionDetail,
} from "./types";

export const queryKeys = {
  inbox: ["inbox"] as const,
  projects: ["projects"] as const,
  issue: (issueId: string) => ["issue", issueId] as const,
  session: (sessionId: string) => ["session", sessionId] as const,
};

export function getApiBaseUrl(): string {
  return window.mspaceDesktop?.apiBaseUrl || "http://127.0.0.1:7788";
}

export function buildApiUrl(path: string): string {
  return `${getApiBaseUrl()}${path}`;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(buildApiUrl(path), {
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers || {}),
    },
    ...init,
  });

  if (!response.ok) {
    const message = await response.text();
    throw new Error(message || `Request failed with status ${response.status}`);
  }

  return (await response.json()) as T;
}

export const api = {
  health: () => request<{ ok: boolean; version: string }>("/health"),
  listInbox: () => request<InboxItem[]>("/api/inbox"),
  listProjects: () => request<Project[]>("/api/projects"),
  createProject: (input: CreateProjectInput) =>
    request<Project>("/api/projects", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  createIssue: (input: CreateIssueInput) =>
    request<{ issueId: string }>("/api/issues", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  getIssue: (issueId: string) =>
    request<IssueDetail>(`/api/issues/${issueId}`),
  addComment: (issueId: string, input: CreateCommentInput) =>
    request<{ ok: boolean }>(`/api/issues/${issueId}/comments`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  createSession: (issueId: string, input: CreateSessionInput) =>
    request<{ sessionId: string }>(`/api/issues/${issueId}/sessions`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  getSession: (sessionId: string) =>
    request<SessionDetail>(`/api/sessions/${sessionId}`),
  cancelSession: (sessionId: string) =>
    request<{ ok: boolean }>(`/api/sessions/${sessionId}/cancel`, {
      method: "POST",
    }),
};
