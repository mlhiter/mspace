import type {
  ActiveWorkItem,
  AgentProfile,
  AgentProfileInput,
  Cluster,
  ClusterInput,
  CreateCommentInput,
  CreateIssueInput,
  CreateProjectInput,
  CreateSessionInput,
  InboxItem,
  IssueDetail,
  IssueLabel,
  IssueListItem,
  IssueTestEnvironment,
  KubeconfigDiscoveryResult,
  KubeconfigImportResult,
  Project,
  SessionDetail,
  StartTestDeployInput,
  UpdateIssueLabelsInput,
  UpdateProjectInput,
} from "./types";

export const queryKeys = {
  activeWork: ["active-work"] as const,
  agents: ["agents"] as const,
  clusters: ["clusters"] as const,
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
  listActiveWork: () => request<ActiveWorkItem[]>("/api/active-work"),
  listAgents: () => request<AgentProfile[]>("/api/agents"),
  createAgent: (input: AgentProfileInput) =>
    request<AgentProfile>("/api/agents", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  updateAgent: (agentId: string, input: AgentProfileInput) =>
    request<AgentProfile>(`/api/agents/${agentId}`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  listClusters: () => request<Cluster[]>("/api/clusters"),
  createCluster: (input: ClusterInput) =>
    request<Cluster>("/api/clusters", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  updateCluster: (clusterId: string, input: ClusterInput) =>
    request<Cluster>(`/api/clusters/${clusterId}`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  deleteCluster: (clusterId: string) =>
    request<{ ok: boolean }>(`/api/clusters/${clusterId}`, {
      method: "DELETE",
    }),
  discoverDefaultKubeconfigs: () =>
    request<KubeconfigDiscoveryResult>("/api/clusters/discover-defaults"),
  importDefaultKubeconfigs: () =>
    request<KubeconfigImportResult>("/api/clusters/import-defaults", {
      method: "POST",
    }),
  importKubeconfigFiles: (paths: string[]) =>
    request<KubeconfigImportResult>("/api/clusters/import", {
      method: "POST",
      body: JSON.stringify({ paths }),
    }),
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
  updateIssueLabels: (issueId: string, input: UpdateIssueLabelsInput) =>
    request<IssueLabel[]>(`/api/issues/${issueId}/labels`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
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
  startTestDeploy: (issueId: string, input: StartTestDeployInput) =>
    request<{ sessionId: string; testEnvironment: IssueTestEnvironment }>(`/api/issues/${issueId}/test-deploy`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  requestTestEnvironmentCleanup: (issueId: string, input?: { agentProfile?: string }) =>
    request<{ sessionId: string; testEnvironment: IssueTestEnvironment }>(`/api/issues/${issueId}/test-environment/cleanup`, {
      method: "POST",
      body: JSON.stringify(input || {}),
    }),
  retainTestEnvironment: (issueId: string) =>
    request<IssueTestEnvironment>(`/api/issues/${issueId}/test-environment/retain`, {
      method: "POST",
    }),
  getSession: (sessionId: string) =>
    request<SessionDetail>(`/api/sessions/${sessionId}`),
  cancelSession: (sessionId: string) =>
    request<{ ok: boolean }>(`/api/sessions/${sessionId}/cancel`, {
      method: "POST",
    }),
  cleanupSession: (sessionId: string) =>
    request<{ ok: boolean }>(`/api/sessions/${sessionId}/cleanup`, {
      method: "POST",
    }),
};
