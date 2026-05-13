import type {
  ActiveWorkItem,
  AgentProfile,
  AgentProfileInput,
  AuthMeResult,
  AuthPollResult,
  AuthStartResult,
	CancelRuntimeTaskInput,
  Cluster,
  Comment,
	ClusterInput,
	CreateCommentInput,
	CreateIssueInput,
	CreateIssueTaskInput,
	CreateProjectInput,
	CreatePullRequestInput,
	CreateRuntimeRegistrationTokenInput,
	CreateRuntimeTaskInput,
	CreateWorkspaceInvitationInput,
	CreateSessionInput,
	CreateTeamIssueEventInput,
  InboxItem,
  Issue,
  IssueAttachment,
  IssueDetail,
  IssueHandoff,
  IssueLabel,
  IssueLabelDefinition,
  IssueListItem,
  IssueTestEnvironment,
  IssueTestEnvironmentResources,
  KubeconfigDiscoveryResult,
  KubeconfigImportResult,
  MspaceUser,
  Project,
	ProjectRunbook,
	RuntimeRegistrationToken,
	RuntimeRegistrationTokenResult,
	RuntimeTask,
	RuntimeTaskEvent,
	RuntimeTaskLog,
	RuntimeWorker,
	SessionDetail,
  StartTestDeployInput,
  TeamInboxItem,
  UpdateCommentInput,
  UpdateIssueLabelsInput,
  UpdateIssueInput,
  UpdateProjectInput,
  UpdateProjectRunbookInput,
  UpdateWorkspaceSettingsInput,
  WorkspaceSettings,
	WorkspaceInvitation,
	WorkspaceInvitationResult,
	WorkspaceMember,
	AcceptWorkspaceInvitationResult,
} from "./types";

export const AUTH_TOKEN_STORAGE_KEY = "mspace.authToken";
export const AUTH_IDENTITY_STORAGE_KEY = "mspace.authIdentity";
export const SELECTED_WORKSPACE_STORAGE_KEY = "mspace.selectedWorkspaceId";
const defaultActorName = "mlhiter";

export interface StoredAuthIdentity {
  id?: string;
  name: string;
  email?: string;
  avatarUrl?: string;
}

export const queryKeys = {
  activeWork: ["active-work"] as const,
  agents: ["agents"] as const,
  authMe: (token: string) => ["auth-me", token] as const,
  authPoll: (state: string) => ["auth-github-result", state] as const,
	teamInbox: (workspaceId: string, token: string) => ["team-inbox", workspaceId, token] as const,
	workspaceMembers: (workspaceId: string, token: string) => ["workspace-members", workspaceId, token] as const,
	workspaceInvitations: (workspaceId: string, token: string) => ["workspace-invitations", workspaceId, token] as const,
	runtimeRegistrationTokens: (workspaceId: string, token: string) => ["runtime-registration-tokens", workspaceId, token] as const,
	runtimeWorkers: (workspaceId: string, token: string) => ["runtime-workers", workspaceId, token] as const,
	runtimeTasks: (workspaceId: string, token: string) => ["runtime-tasks", workspaceId, token] as const,
	runtimeTaskEvents: (workspaceId: string, taskId: string, token: string) => ["runtime-task-events", workspaceId, taskId, token] as const,
	runtimeTaskLogs: (workspaceId: string, taskId: string, token: string) => ["runtime-task-logs", workspaceId, taskId, token] as const,
	workspaceSettings: ["workspace-settings"] as const,
  clusters: ["clusters"] as const,
  inbox: ["inbox"] as const,
  issueLabelDefinitions: ["issue-label-definitions"] as const,
  issues: ["issues"] as const,
  projects: ["projects"] as const,
  projectRunbook: (projectId: string) => ["project-runbook", projectId] as const,
  issue: (issueId: string) => ["issue", issueId] as const,
  issueResources: (issueId: string) => ["issue-resources", issueId] as const,
  session: (sessionId: string) => ["session", sessionId] as const,
};

export function getApiBaseUrl(): string {
  return window.mspaceDesktop?.apiBaseUrl || "http://127.0.0.1:7788";
}

export function getControlPlaneBaseUrl(): string {
  return window.mspaceDesktop?.serverBaseUrl || "http://127.0.0.1:8787";
}

function browserStorage(): Storage | undefined {
  if (typeof window === "undefined") return undefined;
  return window.localStorage;
}

export function getStoredAuthIdentity(): StoredAuthIdentity {
  const fallback = { name: defaultActorName };
  try {
    const stored = browserStorage()?.getItem(AUTH_IDENTITY_STORAGE_KEY);
    if (!stored) return fallback;
    const parsed = JSON.parse(stored) as Partial<StoredAuthIdentity>;
    const name = parsed.name?.trim() || defaultActorName;
    return {
      id: parsed.id,
      name,
      email: parsed.email,
      avatarUrl: parsed.avatarUrl,
    };
  } catch {
    return fallback;
  }
}

function getStoredAuthToken(): string {
  try {
    return browserStorage()?.getItem(AUTH_TOKEN_STORAGE_KEY)?.trim() || "";
  } catch {
    return "";
  }
}

export function setStoredAuthIdentity(user: MspaceUser | null | undefined): void {
  try {
    if (!user) {
      browserStorage()?.removeItem(AUTH_IDENTITY_STORAGE_KEY);
      return;
    }
    browserStorage()?.setItem(
      AUTH_IDENTITY_STORAGE_KEY,
      JSON.stringify({
        id: user.id,
        name: user.name || defaultActorName,
        email: user.email,
        avatarUrl: user.avatarUrl,
      }),
    );
  } catch {
    // localStorage is best-effort; API calls still fall back to the local actor.
  }
}

function withIssueCreator(input: CreateIssueInput): CreateIssueInput {
  const actor = getStoredAuthIdentity();
  return {
    ...input,
    creatorName: input.creatorName || actor.name,
    creatorAvatarUrl: input.creatorAvatarUrl || actor.avatarUrl || "",
  };
}

function withCommentAuthor(input: CreateCommentInput): CreateCommentInput {
  const actor = getStoredAuthIdentity();
  return {
    ...input,
    authorName: input.authorName || actor.name,
    authorAvatarUrl: input.authorAvatarUrl || actor.avatarUrl || "",
  };
}

export function buildApiUrl(path: string): string {
  return `${getApiBaseUrl()}${path}`;
}

export function buildControlPlaneUrl(path: string): string {
  return `${getControlPlaneBaseUrl()}${path}`;
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

function mergeHeaders(...values: Array<HeadersInit | undefined>): HeadersInit {
  const merged: Record<string, string> = {};
  for (const value of values) {
    if (!value) continue;
    new Headers(value).forEach((headerValue, key) => {
      merged[key] = headerValue;
    });
  }
  return merged;
}

function isFormDataBody(body: BodyInit | null | undefined): boolean {
  return typeof FormData !== "undefined" && body instanceof FormData;
}

async function requestURL<T>(url: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers || {});
  if (!headers.has("Content-Type") && !isFormDataBody(init?.body)) {
    headers.set("Content-Type", "application/json");
  }
  const response = await fetch(url, {
    ...init,
    headers,
  });

  if (!response.ok) {
    throw new Error(await readErrorMessage(response));
  }

  return (await response.json()) as T;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const token = getStoredAuthToken();
  return requestURL<T>(buildApiUrl(path), {
    ...init,
    headers: mergeHeaders(token ? authHeaders(token) : undefined, init?.headers),
  });
}

async function requestControlPlane<T>(path: string, init?: RequestInit): Promise<T> {
  return requestURL<T>(buildControlPlaneUrl(path), init);
}

function authHeaders(token: string): HeadersInit {
  return {
    Authorization: `Bearer ${token}`,
  };
}

export const api = {
  health: () =>
    request<{
      ok: boolean;
      version: string;
      runnerProtocol?: number;
      capabilities?: Record<string, boolean>;
    }>("/health"),
  configureControlPlaneSession: (input: { serverBaseUrl: string; token: string; workspaceId: string }) =>
    request<{ ok: boolean }>("/api/control-plane/session", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  getWorkspaceSettings: () => request<WorkspaceSettings>("/api/workspace/settings"),
  updateWorkspaceSettings: (input: UpdateWorkspaceSettingsInput) =>
    request<WorkspaceSettings>("/api/workspace/settings", {
      method: "PUT",
      body: JSON.stringify(input),
    }),
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
  markInboxIssueRead: (issueId: string) =>
    request<{ ok: boolean }>(`/api/inbox/issues/${issueId}/read`, {
      method: "POST",
    }),
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
  getProjectRunbook: (projectId: string) =>
    request<ProjectRunbook>(`/api/projects/${projectId}/runbook`),
  updateProjectRunbook: (projectId: string, input: UpdateProjectRunbookInput) =>
    request<ProjectRunbook>(`/api/projects/${projectId}/runbook`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  deleteProject: (projectId: string) =>
    request<{ ok: boolean }>(`/api/projects/${projectId}`, {
      method: "DELETE",
    }),
  listIssueLabelDefinitions: () =>
    request<IssueLabelDefinition[]>("/api/issue-label-definitions"),
  uploadAttachment: (file: File) => {
    const body = new FormData();
    body.append("file", file);
    return request<IssueAttachment>("/api/attachments", {
      method: "POST",
      body,
    });
  },
  listIssues: () => request<IssueListItem[]>("/api/issues"),
  createIssue: (input: CreateIssueInput) =>
    request<{ issueId: string }>("/api/issues", {
      method: "POST",
      body: JSON.stringify(withIssueCreator(input)),
    }),
  getIssue: (issueId: string) =>
    request<IssueDetail>(`/api/issues/${issueId}`),
  updateIssue: (issueId: string, input: UpdateIssueInput) =>
    request<Issue>(`/api/issues/${issueId}`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  createIssueTask: (issueId: string, input: CreateIssueTaskInput) =>
    request<IssueListItem>(`/api/issues/${issueId}/tasks`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  deleteIssueTask: (issueId: string, taskId: string) =>
    request<{ ok: boolean }>(`/api/issues/${issueId}/tasks/${taskId}`, {
      method: "DELETE",
    }),
  updateIssueLabels: (issueId: string, input: UpdateIssueLabelsInput) =>
    request<IssueLabel[]>(`/api/issues/${issueId}/labels`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  addComment: (issueId: string, input: CreateCommentInput) =>
    request<{ ok: boolean; commentId: string }>(`/api/issues/${issueId}/comments`, {
      method: "POST",
      body: JSON.stringify(withCommentAuthor(input)),
    }),
  updateComment: (issueId: string, commentId: string, input: UpdateCommentInput) =>
    request<{ ok: boolean; comment: Comment }>(`/api/issues/${issueId}/comments/${commentId}`, {
      method: "PUT",
      body: JSON.stringify(input),
    }),
  setCommentReaction: (issueId: string, commentId: string, reaction: string) =>
    request<{ ok: boolean }>(`/api/issues/${issueId}/comments/${commentId}/reactions/${encodeURIComponent(reaction)}`, {
      method: "PUT",
    }),
  deleteCommentReaction: (issueId: string, commentId: string, reaction: string) =>
    request<{ ok: boolean }>(`/api/issues/${issueId}/comments/${commentId}/reactions/${encodeURIComponent(reaction)}`, {
      method: "DELETE",
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
  getIssueTestEnvironmentResources: (issueId: string) =>
    request<IssueTestEnvironmentResources>(`/api/issues/${issueId}/test-environment/resources`),
  probeTestEnvironment: (issueId: string) =>
    request<IssueTestEnvironment>(`/api/issues/${issueId}/test-environment/probe`, {
      method: "POST",
    }),
  createPullRequest: (issueId: string, input: CreatePullRequestInput) =>
    request<IssueHandoff>(`/api/issues/${issueId}/handoffs/create-pr`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  refreshIssueHandoff: (issueId: string, handoffId: string) =>
    request<IssueHandoff>(`/api/issues/${issueId}/handoffs/${handoffId}/refresh`, {
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

export const controlPlaneApi = {
  startGitHubLogin: () =>
    requestControlPlane<AuthStartResult>("/api/auth/github/start"),
  pollGitHubLogin: (state: string) =>
    requestControlPlane<AuthPollResult>(`/api/auth/github/result?state=${encodeURIComponent(state)}`),
  me: (token: string) =>
    requestControlPlane<AuthMeResult>("/api/auth/me", {
      headers: authHeaders(token),
    }),
  listWorkspaces: (token: string) =>
    requestControlPlane<AuthMeResult["workspaces"]>("/api/workspaces", {
      headers: authHeaders(token),
    }),
  listWorkspaceMembers: (token: string, workspaceId: string) =>
    requestControlPlane<WorkspaceMember[]>(`/api/workspaces/${workspaceId}/members`, {
      headers: authHeaders(token),
    }),
  createWorkspaceInvitation: (token: string, workspaceId: string, input: CreateWorkspaceInvitationInput) =>
    requestControlPlane<WorkspaceInvitationResult>(`/api/workspaces/${workspaceId}/invitations`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  listWorkspaceInvitations: (token: string, workspaceId: string) =>
    requestControlPlane<WorkspaceInvitation[]>(`/api/workspaces/${workspaceId}/invitations`, {
      headers: authHeaders(token),
    }),
  revokeWorkspaceInvitation: (token: string, workspaceId: string, invitationId: string) =>
    requestControlPlane<WorkspaceInvitation>(`/api/workspaces/${workspaceId}/invitations/${invitationId}`, {
      method: "DELETE",
      headers: authHeaders(token),
    }),
  acceptWorkspaceInvitation: (token: string, inviteToken: string) =>
    requestControlPlane<AcceptWorkspaceInvitationResult>("/api/workspace-invitations/accept", {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify({ token: inviteToken }),
    }),
  listInbox: (token: string, workspaceId: string) =>
    requestControlPlane<TeamInboxItem[]>(`/api/workspaces/${workspaceId}/inbox`, {
      headers: authHeaders(token),
    }),
  createIssueEvent: (token: string, workspaceId: string, input: CreateTeamIssueEventInput) =>
    requestControlPlane<{ id: string }>(`/api/workspaces/${workspaceId}/issue-events`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  markIssueEventRead: (token: string, workspaceId: string, eventId: string) =>
    requestControlPlane<{ ok: boolean }>(`/api/workspaces/${workspaceId}/issue-events/${eventId}/read`, {
      method: "POST",
      headers: authHeaders(token),
    }),
	markIssueReadThrough: (token: string, workspaceId: string, issueId: string, throughEventId?: string) =>
		requestControlPlane<{ ok: boolean; readCount: number }>(`/api/workspaces/${workspaceId}/issues/${issueId}/read-through`, {
			method: "POST",
			headers: authHeaders(token),
			body: JSON.stringify({ throughEventId: throughEventId || "" }),
		}),
	createRuntimeRegistrationToken: (token: string, workspaceId: string, input: CreateRuntimeRegistrationTokenInput) =>
		requestControlPlane<RuntimeRegistrationTokenResult>(`/api/workspaces/${workspaceId}/runtime-registration-tokens`, {
			method: "POST",
			headers: authHeaders(token),
			body: JSON.stringify(input),
		}),
	listRuntimeRegistrationTokens: (token: string, workspaceId: string) =>
		requestControlPlane<RuntimeRegistrationToken[]>(`/api/workspaces/${workspaceId}/runtime-registration-tokens`, {
			headers: authHeaders(token),
		}),
	revokeRuntimeRegistrationToken: (token: string, workspaceId: string, tokenId: string) =>
		requestControlPlane<RuntimeRegistrationToken>(`/api/workspaces/${workspaceId}/runtime-registration-tokens/${tokenId}`, {
			method: "DELETE",
			headers: authHeaders(token),
		}),
	listRuntimeWorkers: (token: string, workspaceId: string) =>
		requestControlPlane<RuntimeWorker[]>(`/api/workspaces/${workspaceId}/runtime-workers`, {
			headers: authHeaders(token),
		}),
	createRuntimeTask: (token: string, workspaceId: string, input: CreateRuntimeTaskInput) =>
		requestControlPlane<RuntimeTask>(`/api/workspaces/${workspaceId}/runtime-tasks`, {
			method: "POST",
			headers: authHeaders(token),
			body: JSON.stringify(input),
		}),
	listRuntimeTasks: (token: string, workspaceId: string) =>
		requestControlPlane<RuntimeTask[]>(`/api/workspaces/${workspaceId}/runtime-tasks`, {
			headers: authHeaders(token),
		}),
	listRuntimeTaskEvents: (token: string, workspaceId: string, taskId: string) =>
		requestControlPlane<RuntimeTaskEvent[]>(`/api/workspaces/${workspaceId}/runtime-tasks/${taskId}/events`, {
			headers: authHeaders(token),
		}),
	listRuntimeTaskLogs: (token: string, workspaceId: string, taskId: string) =>
		requestControlPlane<RuntimeTaskLog[]>(`/api/workspaces/${workspaceId}/runtime-tasks/${taskId}/logs`, {
			headers: authHeaders(token),
		}),
	cancelRuntimeTask: (token: string, workspaceId: string, taskId: string, input: CancelRuntimeTaskInput = {}) =>
		requestControlPlane<RuntimeTask>(`/api/workspaces/${workspaceId}/runtime-tasks/${taskId}/cancel`, {
			method: "POST",
			headers: authHeaders(token),
			body: JSON.stringify(input),
		}),
};
