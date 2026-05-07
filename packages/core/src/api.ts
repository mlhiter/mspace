import type {
  CreateCommentInput,
  CreateIssueInput,
  CreateProjectInput,
  CreateSessionInput,
  InboxItem,
  IssueDetail,
  IssueListItem,
  Project,
  SessionDetail,
  UpdateProjectInput,
} from "./types";

export const queryKeys = {
  inbox: ["inbox"] as const,
  issues: ["issues"] as const,
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

async function readErrorMessage(response: Response): Promise<string> {
  const fallback = `Request failed with status ${response.status}`;
  const contentType = response.headers.get("content-type") || "";

  if (contentType.includes("application/json")) {
    const payload = (await response.json()) as { error?: string };
    return payload.error || fallback;
  }

  const message = await response.text();
  return message || fallback;
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
    throw new Error(await readErrorMessage(response));
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
  updateProject: (input: UpdateProjectInput) =>
    request<Project>(`/api/projects/${input.id}`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  deleteProject: (projectId: string) =>
    request<{ ok: boolean }>(`/api/projects/${projectId}`, {
      method: "DELETE",
    }),
  listIssues: () => request<IssueListItem[]>("/api/issues"),
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
  assignAgent: (issueId: string, input: CreateSessionInput) =>
    request<{ sessionId: string }>(`/api/issues/${issueId}/assign-agent`, {
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
