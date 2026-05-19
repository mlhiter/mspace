import type {
  AgentProfile,
  AgentProfileInput,
  AgentSession,
  AuthMeResult,
  AuthPollResult,
  AuthStartResult,
  CancelRuntimeTaskInput,
  Cluster,
  Comment,
  ClusterInput,
  CreateCommentInput,
  CreateAgentSessionInput,
  CreateIssueInput,
  CreateIssueTaskInput,
  CreateProjectInput,
  CreatePullRequestInput,
  CreateRuntimeRegistrationTokenInput,
  CreateRuntimeTaskInput,
  CreateWorkspaceInput,
  CreateWorkspaceInvitationInput,
  CreateWorkspaceResult,
  CreateWorkspaceIssueEventInput,
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
  SuggestIssueTitleInput,
  SuggestIssueTitleResult,
  WorkspaceInboxItem,
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
  agents: (workspaceId: string, token: string) => ["agents", workspaceId, token] as const,
  authMe: (token: string) => ["auth-me", token] as const,
  authPoll: (state: string) => ["auth-github-result", state] as const,
	workspaceInbox: (workspaceId: string, token: string) => ["workspace-inbox", workspaceId, token] as const,
	workspaceMembers: (workspaceId: string, token: string) => ["workspace-members", workspaceId, token] as const,
	workspaceInvitations: (workspaceId: string, token: string) => ["workspace-invitations", workspaceId, token] as const,
	runtimeRegistrationTokens: (workspaceId: string, token: string) => ["runtime-registration-tokens", workspaceId, token] as const,
	runtimeWorkers: (workspaceId: string, token: string) => ["runtime-workers", workspaceId, token] as const,
	runtimeTasks: (workspaceId: string, token: string) => ["runtime-tasks", workspaceId, token] as const,
  runtimeTaskEvents: (workspaceId: string, taskId: string, token: string) => ["runtime-task-events", workspaceId, taskId, token] as const,
  runtimeTaskLogs: (workspaceId: string, taskId: string, token: string) => ["runtime-task-logs", workspaceId, taskId, token] as const,
  workspaceIssueLabelDefinitions: (workspaceId: string, token: string) =>
    ["workspace-issue-label-definitions", workspaceId, token] as const,
  workspaceIssues: (workspaceId: string, token: string) => ["workspace-issues", workspaceId, token] as const,
  workspaceIssue: (workspaceId: string, issueId: string, token: string) =>
    ["workspace-issue", workspaceId, issueId, token] as const,
  workspaceProjects: (workspaceId: string, token: string) => ["workspace-projects", workspaceId, token] as const,
  workspaceProjectRunbook: (workspaceId: string, projectId: string, token: string) =>
    ["workspace-project-runbook", workspaceId, projectId, token] as const,
  workspaceSettings: (workspaceId: string, token: string) => ["workspace-settings", workspaceId, token] as const,
  clusters: (workspaceId: string, token: string) => ["clusters", workspaceId, token] as const,
  issueResources: (workspaceId: string, issueId: string, token: string) =>
    ["issue-resources", workspaceId, issueId, token] as const,
  session: (sessionId: string) => ["session", sessionId] as const,
};

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

export function getStoredAuthToken(): string {
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

async function requestControlPlane<T>(path: string, init?: RequestInit): Promise<T> {
  return requestURL<T>(buildControlPlaneUrl(path), init);
}

function authHeaders(token: string): HeadersInit {
  return {
    Authorization: `Bearer ${token}`,
  };
}

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
  createWorkspace: (token: string, input: CreateWorkspaceInput) =>
    requestControlPlane<CreateWorkspaceResult>("/api/workspaces", {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
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
    requestControlPlane<WorkspaceInboxItem[]>(`/api/workspaces/${workspaceId}/inbox`, {
      headers: authHeaders(token),
    }),
  createIssueEvent: (token: string, workspaceId: string, input: CreateWorkspaceIssueEventInput) =>
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
	listProjects: (token: string, workspaceId: string) =>
		requestControlPlane<Project[]>(`/api/workspaces/${workspaceId}/projects`, {
			headers: authHeaders(token),
		}),
	createProject: (token: string, workspaceId: string, input: CreateProjectInput) =>
		requestControlPlane<Project>(`/api/workspaces/${workspaceId}/projects`, {
			method: "POST",
			headers: authHeaders(token),
			body: JSON.stringify(input),
		}),
	updateProject: (token: string, workspaceId: string, input: UpdateProjectInput) =>
		requestControlPlane<Project>(`/api/workspaces/${workspaceId}/projects/${input.id}`, {
			method: "PUT",
			headers: authHeaders(token),
			body: JSON.stringify(input),
		}),
	deleteProject: (token: string, workspaceId: string, projectId: string) =>
		requestControlPlane<{ ok: boolean }>(`/api/workspaces/${workspaceId}/projects/${projectId}`, {
			method: "DELETE",
			headers: authHeaders(token),
		}),
	getProjectRunbook: (token: string, workspaceId: string, projectId: string) =>
		requestControlPlane<ProjectRunbook>(`/api/workspaces/${workspaceId}/projects/${projectId}/runbook`, {
			headers: authHeaders(token),
		}),
	updateProjectRunbook: (token: string, workspaceId: string, projectId: string, input: UpdateProjectRunbookInput) =>
		requestControlPlane<ProjectRunbook>(`/api/workspaces/${workspaceId}/projects/${projectId}/runbook`, {
			method: "PUT",
			headers: authHeaders(token),
			body: JSON.stringify(input),
		}),
	listIssueLabelDefinitions: (token: string, workspaceId: string) =>
		requestControlPlane<IssueLabelDefinition[]>(`/api/workspaces/${workspaceId}/issue-label-definitions`, {
			headers: authHeaders(token),
		}),
  getWorkspaceSettings: (token: string, workspaceId: string) =>
    requestControlPlane<WorkspaceSettings>(`/api/workspaces/${workspaceId}/workspace/settings`, {
      headers: authHeaders(token),
    }),
  updateWorkspaceSettings: (token: string, workspaceId: string, input: UpdateWorkspaceSettingsInput) =>
    requestControlPlane<WorkspaceSettings>(`/api/workspaces/${workspaceId}/workspace/settings`, {
      method: "PUT",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  listAgents: (token: string, workspaceId: string) =>
    requestControlPlane<AgentProfile[]>(`/api/workspaces/${workspaceId}/agents`, {
      headers: authHeaders(token),
    }),
  createAgent: (token: string, workspaceId: string, input: AgentProfileInput) =>
    requestControlPlane<AgentProfile>(`/api/workspaces/${workspaceId}/agents`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  updateAgent: (token: string, workspaceId: string, agentId: string, input: AgentProfileInput) =>
    requestControlPlane<AgentProfile>(`/api/workspaces/${workspaceId}/agents/${agentId}`, {
      method: "PUT",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  listClusters: (token: string, workspaceId: string) =>
    requestControlPlane<Cluster[]>(`/api/workspaces/${workspaceId}/clusters`, {
      headers: authHeaders(token),
    }),
  createCluster: (token: string, workspaceId: string, input: ClusterInput) =>
    requestControlPlane<Cluster>(`/api/workspaces/${workspaceId}/clusters`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  updateCluster: (token: string, workspaceId: string, clusterId: string, input: ClusterInput) =>
    requestControlPlane<Cluster>(`/api/workspaces/${workspaceId}/clusters/${clusterId}`, {
      method: "PUT",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  deleteCluster: (token: string, workspaceId: string, clusterId: string) =>
    requestControlPlane<{ ok: boolean }>(`/api/workspaces/${workspaceId}/clusters/${clusterId}`, {
      method: "DELETE",
      headers: authHeaders(token),
    }),
  discoverDefaultKubeconfigs: (token: string, workspaceId: string) =>
    requestControlPlane<KubeconfigDiscoveryResult>(`/api/workspaces/${workspaceId}/clusters/discover-defaults`, {
      headers: authHeaders(token),
    }),
  importKubeconfigFiles: (token: string, workspaceId: string, paths: string[]) =>
    requestControlPlane<KubeconfigImportResult>(`/api/workspaces/${workspaceId}/clusters/import`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify({ paths }),
    }),
	listIssues: (token: string, workspaceId: string) =>
		requestControlPlane<IssueListItem[]>(`/api/workspaces/${workspaceId}/issues`, {
			headers: authHeaders(token),
		}),
	createIssue: (token: string, workspaceId: string, input: CreateIssueInput) =>
		requestControlPlane<{ issueId: string }>(`/api/workspaces/${workspaceId}/issues`, {
			method: "POST",
			headers: authHeaders(token),
			body: JSON.stringify(input),
		}),
	suggestIssueTitle: (token: string, workspaceId: string, input: SuggestIssueTitleInput) =>
		requestControlPlane<SuggestIssueTitleResult>(`/api/workspaces/${workspaceId}/issues/suggest-title`, {
			method: "POST",
			headers: authHeaders(token),
			body: JSON.stringify(input),
		}),
	getIssue: (token: string, workspaceId: string, issueId: string) =>
		requestControlPlane<IssueDetail>(`/api/workspaces/${workspaceId}/issues/${issueId}`, {
			headers: authHeaders(token),
		}),
	createAgentSession: (token: string, workspaceId: string, issueId: string, input: CreateAgentSessionInput) =>
		requestControlPlane<AgentSession>(`/api/workspaces/${workspaceId}/issues/${issueId}/sessions`, {
			method: "POST",
			headers: authHeaders(token),
			body: JSON.stringify(input),
		}),
	getSession: (token: string, workspaceId: string, sessionId: string) =>
		requestControlPlane<SessionDetail>(`/api/workspaces/${workspaceId}/sessions/${sessionId}`, {
			headers: authHeaders(token),
		}),
	cancelSession: (token: string, workspaceId: string, sessionId: string, input: CancelRuntimeTaskInput = {}) =>
		requestControlPlane<RuntimeTask>(`/api/workspaces/${workspaceId}/sessions/${sessionId}/cancel`, {
			method: "POST",
			headers: authHeaders(token),
			body: JSON.stringify(input),
		}),
  startTestDeploy: (token: string, workspaceId: string, issueId: string, input: StartTestDeployInput) =>
    requestControlPlane<{ sessionId: string; testEnvironment: IssueTestEnvironment }>(`/api/workspaces/${workspaceId}/issues/${issueId}/test-deploy`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  requestTestEnvironmentCleanup: (token: string, workspaceId: string, issueId: string, input?: { agentProfile?: string }) =>
    requestControlPlane<{ sessionId: string; testEnvironment: IssueTestEnvironment }>(`/api/workspaces/${workspaceId}/issues/${issueId}/test-environment/cleanup`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input || {}),
    }),
  retainTestEnvironment: (token: string, workspaceId: string, issueId: string) =>
    requestControlPlane<IssueTestEnvironment>(`/api/workspaces/${workspaceId}/issues/${issueId}/test-environment/retain`, {
      method: "POST",
      headers: authHeaders(token),
    }),
  getIssueTestEnvironmentResources: (token: string, workspaceId: string, issueId: string) =>
    requestControlPlane<IssueTestEnvironmentResources>(`/api/workspaces/${workspaceId}/issues/${issueId}/test-environment/resources`, {
      headers: authHeaders(token),
    }),
  probeTestEnvironment: (token: string, workspaceId: string, issueId: string) =>
    requestControlPlane<IssueTestEnvironment>(`/api/workspaces/${workspaceId}/issues/${issueId}/test-environment/probe`, {
      method: "POST",
      headers: authHeaders(token),
    }),
  createPullRequest: (token: string, workspaceId: string, issueId: string, input: CreatePullRequestInput) =>
    requestControlPlane<IssueHandoff>(`/api/workspaces/${workspaceId}/issues/${issueId}/handoffs/create-pr`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  refreshIssueHandoff: (token: string, workspaceId: string, issueId: string, handoffId: string) =>
    requestControlPlane<IssueHandoff>(`/api/workspaces/${workspaceId}/issues/${issueId}/handoffs/${handoffId}/refresh`, {
      method: "POST",
      headers: authHeaders(token),
    }),
	updateIssue: (token: string, workspaceId: string, issueId: string, input: UpdateIssueInput) =>
		requestControlPlane<Issue>(`/api/workspaces/${workspaceId}/issues/${issueId}`, {
			method: "PUT",
			headers: authHeaders(token),
			body: JSON.stringify(input),
		}),
	createIssueTask: (token: string, workspaceId: string, issueId: string, input: CreateIssueTaskInput) =>
		requestControlPlane<IssueListItem>(`/api/workspaces/${workspaceId}/issues/${issueId}/tasks`, {
			method: "POST",
			headers: authHeaders(token),
			body: JSON.stringify(input),
		}),
	deleteIssueTask: (token: string, workspaceId: string, issueId: string, taskId: string) =>
		requestControlPlane<{ ok: boolean }>(`/api/workspaces/${workspaceId}/issues/${issueId}/tasks/${taskId}`, {
			method: "DELETE",
			headers: authHeaders(token),
		}),
	updateIssueLabels: (token: string, workspaceId: string, issueId: string, input: UpdateIssueLabelsInput) =>
		requestControlPlane<IssueLabel[]>(`/api/workspaces/${workspaceId}/issues/${issueId}/labels`, {
			method: "PUT",
			headers: authHeaders(token),
			body: JSON.stringify(input),
		}),
	uploadIssueAttachment: (token: string, workspaceId: string, issueId: string, file: File) => {
		const form = new FormData();
		form.append("file", file, file.name);
		return requestControlPlane<IssueAttachment>(`/api/workspaces/${workspaceId}/issues/${issueId}/attachments`, {
			method: "POST",
			headers: authHeaders(token),
			body: form,
		});
	},
	addComment: (token: string, workspaceId: string, issueId: string, input: CreateCommentInput) =>
		requestControlPlane<{ ok: boolean; commentId: string }>(`/api/workspaces/${workspaceId}/issues/${issueId}/comments`, {
			method: "POST",
			headers: authHeaders(token),
			body: JSON.stringify(input),
		}),
	updateComment: (token: string, workspaceId: string, issueId: string, commentId: string, input: UpdateCommentInput) =>
		requestControlPlane<{ ok: boolean; comment: Comment }>(`/api/workspaces/${workspaceId}/issues/${issueId}/comments/${commentId}`, {
			method: "PUT",
			headers: authHeaders(token),
			body: JSON.stringify(input),
		}),
	setCommentReaction: (token: string, workspaceId: string, issueId: string, commentId: string, reaction: string) =>
		requestControlPlane<{ ok: boolean }>(`/api/workspaces/${workspaceId}/issues/${issueId}/comments/${commentId}/reactions/${encodeURIComponent(reaction)}`, {
			method: "PUT",
			headers: authHeaders(token),
		}),
	deleteCommentReaction: (token: string, workspaceId: string, issueId: string, commentId: string, reaction: string) =>
		requestControlPlane<{ ok: boolean }>(`/api/workspaces/${workspaceId}/issues/${issueId}/comments/${commentId}/reactions/${encodeURIComponent(reaction)}`, {
			method: "DELETE",
			headers: authHeaders(token),
		}),
};
