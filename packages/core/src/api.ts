import type {
  AgentEngineCatalogItem,
  AgentSession,
  AuthMeResult,
  AuthPollResult,
  AuthStartResult,
  CancelRuntimeTaskInput,
  Cluster,
  Comment,
  ClusterInput,
  Environment,
  EnvironmentCheckInput,
  EnvironmentInput,
  CreateCommentInput,
  CreateAgentSessionInput,
  CreateIssueInput,
  CreateIssueTaskInput,
  CreateProjectInput,
  CreatePullRequestInput,
  CreateRuntimeRegistrationTokenInput,
  CreateRuntimeTaskInput,
  CreateAdHocTestRunInput,
  CreateTestRunInput,
  CreateWorkerInstallationInput,
  CreateWorkspaceInput,
  CreateWorkspaceInvitationInput,
  CreateWorkspaceResult,
  CreateWorkspaceIssueEventInput,
  GenerateTestCasesInput,
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
  ImportTestCasesInput,
  TestCaseImportMappingTaskInput,
  ImportTestCasesPreview,
  ImportTestCasesResult,
  MspaceUser,
  OptimizeTestCasesInput,
  PasswordAuthInput,
  Project,
  ProjectRunbook,
  ApplyTestCaseProposalResult,
  ReviewTestCaseProposalInput,
  ReviewTestRunInput,
  RuntimeRegistrationToken,
  RuntimeRegistrationTokenResult,
  RuntimeAvailability,
  RuntimeAvailabilityInput,
  RuntimeTask,
  RuntimeTaskEvent,
  RuntimeTaskListResult,
  RuntimeTaskLog,
  RuntimeWorker,
  SessionDetail,
  SkillCatalogItem,
  SkillDetail,
  SkillInput,
  DuplicateSkillInput,
  StartTestDeployInput,
  SuggestIssueTitleInput,
  SuggestIssueTitleResult,
  TestCase,
  TestCaseAgentSessionResult,
  TestCaseInput,
  TestCaseListResult,
  TestCaseProposal,
  TestCaseRevision,
  TestCaseRunItem,
  TestPlan,
  TestPlanDetail,
  TestPlanInput,
  TestRun,
  TestRunDetail,
  RetryTestRunInput,
  WorkspaceInboxItem,
  UpdateCommentInput,
  UpdateCurrentUserProfileInput,
  UpdateIssueLabelsInput,
  UpdateIssueInput,
  UpdateProjectInput,
  UpdateProjectRunbookInput,
  UpdateWorkspaceInput,
  UpdateWorkspaceResult,
  UpdateWorkspaceSettingsInput,
  WorkspaceGitHubAppInstallation,
  WorkspaceSettings,
  WorkspaceInvitation,
  WorkspaceInvitationPreview,
  WorkspaceInvitationResult,
  WorkspaceMember,
  AcceptWorkspaceInvitationResult,
  WorkerInstallationResult,
} from "./types";

export const AUTH_TOKEN_STORAGE_KEY = "mspace.authToken";
export const AUTH_IDENTITY_STORAGE_KEY = "mspace.authIdentity";
export const SELECTED_WORKSPACE_STORAGE_KEY = "mspace.selectedWorkspaceId";
const defaultActorName = "mlhiter";
let controlPlaneBaseUrlOverride = "";

export interface StoredAuthIdentity {
  id?: string;
  name: string;
  email?: string;
  avatarUrl?: string;
}

export const queryKeys = {
  agents: (workspaceId: string, token: string) => ["agents", workspaceId, token] as const,
  skills: (workspaceId: string, token: string) => ["skills", workspaceId, token] as const,
  authMe: (token: string) => ["auth-me", token] as const,
  authPoll: (state: string) => ["auth-github-result", state] as const,
	workspaceInbox: (workspaceId: string, token: string) => ["workspace-inbox", workspaceId, token] as const,
	workspaceMembers: (workspaceId: string, token: string) => ["workspace-members", workspaceId, token] as const,
	workspaceInvitations: (workspaceId: string, token: string) => ["workspace-invitations", workspaceId, token] as const,
	runtimeRegistrationTokens: (workspaceId: string, token: string) => ["runtime-registration-tokens", workspaceId, token] as const,
	runtimeWorkers: (workspaceId: string, token: string) => ["runtime-workers", workspaceId, token] as const,
	runtimeAvailability: (workspaceId: string, token: string, input: RuntimeAvailabilityInput = {}) =>
		["runtime-availability", workspaceId, token, input.runtimeMode || "", input.requiredCapabilities || {}] as const,
	runtimeTasks: (workspaceId: string, token: string, limit = 10, offset = 0) => ["runtime-tasks", workspaceId, token, limit, offset] as const,
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
  projectTestCasesBase: (workspaceId: string, projectId: string, token: string) =>
    ["project-test-cases", workspaceId, projectId, token] as const,
  projectTestCases: (workspaceId: string, projectId: string, token: string, status = "", query = "", limit = 0, offset = 0) =>
    ["project-test-cases", workspaceId, projectId, token, status, query, limit, offset] as const,
  projectTestCase: (workspaceId: string, projectId: string, caseId: string, token: string) =>
    ["project-test-case", workspaceId, projectId, caseId, token] as const,
  projectTestCaseRunItems: (workspaceId: string, projectId: string, caseId: string, token: string) =>
    ["project-test-case-run-items", workspaceId, projectId, caseId, token] as const,
  projectTestCaseRevisions: (workspaceId: string, projectId: string, caseId: string, token: string) =>
    ["project-test-case-revisions", workspaceId, projectId, caseId, token] as const,
  projectTestCaseProposals: (workspaceId: string, projectId: string, token: string, status = "") =>
    ["project-test-case-proposals", workspaceId, projectId, token, status] as const,
  workspaceTestPlans: (workspaceId: string, token: string, status = "") =>
    ["workspace-test-plans", workspaceId, token, status] as const,
  workspaceTestPlan: (workspaceId: string, planId: string, token: string) =>
    ["workspace-test-plan", workspaceId, planId, token] as const,
  workspaceTestRuns: (workspaceId: string, token: string, status = "", source = "") =>
    ["workspace-test-runs", workspaceId, token, status, source] as const,
  workspaceTestRun: (workspaceId: string, runId: string, token: string) =>
    ["workspace-test-run", workspaceId, runId, token] as const,
  projectTestPlans: (workspaceId: string, projectId: string, token: string, status = "") =>
    ["project-test-plans", workspaceId, projectId, token, status] as const,
  projectTestPlan: (workspaceId: string, projectId: string, planId: string, token: string) =>
    ["project-test-plan", workspaceId, projectId, planId, token] as const,
  projectTestRuns: (workspaceId: string, projectId: string, token: string, status = "", source = "") =>
    ["project-test-runs", workspaceId, projectId, token, status, source] as const,
  projectTestRun: (workspaceId: string, projectId: string, runId: string, token: string) =>
    ["project-test-run", workspaceId, projectId, runId, token] as const,
  workspaceSettings: (workspaceId: string, token: string) => ["workspace-settings", workspaceId, token] as const,
  workspaceGitHubApp: (workspaceId: string, token: string) => ["workspace-github-app", workspaceId, token] as const,
  environments: (workspaceId: string, token: string) => ["environments", workspaceId, token] as const,
  clusters: (workspaceId: string, token: string) => ["clusters", workspaceId, token] as const,
  issueResources: (workspaceId: string, issueId: string, token: string) =>
    ["issue-resources", workspaceId, issueId, token] as const,
  session: (sessionId: string) => ["session", sessionId] as const,
};

export function getControlPlaneBaseUrl(): string {
  return controlPlaneBaseUrlOverride || window.mspaceDesktop?.serverBaseUrl || "http://127.0.0.1:8787";
}

export function setControlPlaneBaseUrl(baseUrl: string): void {
  controlPlaneBaseUrlOverride = baseUrl.replace(/\/+$/, "");
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

function normalizeRuntimeTaskListResult(payload: RuntimeTaskListResult | RuntimeTask[], input: { limit?: number; offset?: number }): RuntimeTaskListResult {
	const limit = typeof input.limit === "number" && input.limit > 0 ? input.limit : 10;
	const offset = typeof input.offset === "number" && input.offset > 0 ? input.offset : 0;
	if (Array.isArray(payload)) {
		const tasks = payload.slice(offset, offset + limit);
		return {
			tasks,
			total: payload.length,
			limit,
			offset,
			statusCounts: payload.reduce<Record<string, number>>((counts, task) => {
				counts[task.status] = (counts[task.status] || 0) + 1;
				return counts;
			}, {}),
		};
	}
	const tasks = Array.isArray(payload.tasks) ? payload.tasks : [];
	return {
		tasks,
		total: typeof payload.total === "number" ? payload.total : tasks.length,
		limit: typeof payload.limit === "number" && payload.limit > 0 ? payload.limit : limit,
		offset: typeof payload.offset === "number" && payload.offset >= 0 ? payload.offset : offset,
		statusCounts: payload.statusCounts || {},
	};
}

export const controlPlaneApi = {
  startGitHubLogin: () =>
    requestControlPlane<AuthStartResult>("/api/auth/github/start"),
  pollGitHubLogin: (state: string) =>
    requestControlPlane<AuthPollResult>(`/api/auth/github/result?state=${encodeURIComponent(state)}`),
  registerWithPassword: (input: PasswordAuthInput) =>
    requestControlPlane<AuthMeResult & { token: string; expiresAt: string }>("/api/auth/password/register", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  loginWithPassword: (input: PasswordAuthInput) =>
    requestControlPlane<AuthMeResult & { token: string; expiresAt: string }>("/api/auth/password/login", {
      method: "POST",
      body: JSON.stringify(input),
    }),
  me: (token: string) =>
    requestControlPlane<AuthMeResult>("/api/auth/me", {
      headers: authHeaders(token),
    }),
  updateCurrentUserProfile: (token: string, input: UpdateCurrentUserProfileInput) =>
    requestControlPlane<AuthMeResult>("/api/auth/me", {
      method: "PUT",
      headers: authHeaders(token),
      body: JSON.stringify(input),
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
  updateWorkspace: (token: string, workspaceId: string, input: UpdateWorkspaceInput) =>
    requestControlPlane<UpdateWorkspaceResult>(`/api/workspaces/${workspaceId}`, {
      method: "PUT",
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
  previewWorkspaceInvitation: (inviteToken: string) =>
    requestControlPlane<WorkspaceInvitationPreview>(`/api/workspace-invitations/preview?token=${encodeURIComponent(inviteToken)}`),
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
	createWorkerInstallation: (token: string, workspaceId: string, input: CreateWorkerInstallationInput) =>
		requestControlPlane<WorkerInstallationResult>(`/api/workspaces/${workspaceId}/worker-installations`, {
			method: "POST",
			headers: authHeaders(token),
			body: JSON.stringify(input),
		}),
	listRuntimeWorkers: (token: string, workspaceId: string) =>
		requestControlPlane<RuntimeWorker[]>(`/api/workspaces/${workspaceId}/runtime-workers`, {
			headers: authHeaders(token),
		}),
	getRuntimeAvailability: (token: string, workspaceId: string, input: RuntimeAvailabilityInput = {}) => {
		const params = new URLSearchParams();
		if (input.runtimeMode) params.set("runtimeMode", input.runtimeMode);
		if (input.requiredCapabilities) params.set("requiredCapabilities", JSON.stringify(input.requiredCapabilities));
		const query = params.toString();
		return requestControlPlane<RuntimeAvailability>(`/api/workspaces/${workspaceId}/runtime/availability${query ? `?${query}` : ""}`, {
			headers: authHeaders(token),
		});
	},
	createRuntimeTask: (token: string, workspaceId: string, input: CreateRuntimeTaskInput) =>
		requestControlPlane<RuntimeTask>(`/api/workspaces/${workspaceId}/runtime-tasks`, {
			method: "POST",
			headers: authHeaders(token),
			body: JSON.stringify(input),
		}),
	listRuntimeTasks: (token: string, workspaceId: string, input: { limit?: number; offset?: number } = {}) => {
		const params = new URLSearchParams();
		if (typeof input.limit === "number") params.set("limit", String(input.limit));
		if (typeof input.offset === "number") params.set("offset", String(input.offset));
		const query = params.toString();
		return requestControlPlane<RuntimeTaskListResult | RuntimeTask[]>(`/api/workspaces/${workspaceId}/runtime-tasks${query ? `?${query}` : ""}`, {
			headers: authHeaders(token),
		}).then((payload) => normalizeRuntimeTaskListResult(payload, input));
	},
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
  listProjectTestCases: (token: string, workspaceId: string, projectId: string, input: { status?: string; query?: string; limit?: number; offset?: number } = {}) => {
    const params = new URLSearchParams();
    if (input.status) params.set("status", input.status);
    if (input.query) params.set("q", input.query);
    if (input.limit) params.set("limit", String(input.limit));
    if (input.offset) params.set("offset", String(input.offset));
    const query = params.toString();
    return requestControlPlane<TestCaseListResult>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-cases${query ? `?${query}` : ""}`, {
      headers: authHeaders(token),
    });
  },
  createProjectTestCase: (token: string, workspaceId: string, projectId: string, input: TestCaseInput) =>
    requestControlPlane<TestCase>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-cases`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  importProjectTestCases: (token: string, workspaceId: string, projectId: string, input: ImportTestCasesInput) =>
    requestControlPlane<ImportTestCasesResult>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-cases/import`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  previewProjectTestCasesImport: (token: string, workspaceId: string, projectId: string, input: ImportTestCasesInput) =>
    requestControlPlane<ImportTestCasesPreview>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-cases/import/preview`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  createProjectTestCasesImportMappingTask: (token: string, workspaceId: string, projectId: string, input: TestCaseImportMappingTaskInput) =>
    requestControlPlane<RuntimeTask>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-cases/import/mapping-task`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  optimizeProjectTestCases: (token: string, workspaceId: string, projectId: string, input: OptimizeTestCasesInput) =>
    requestControlPlane<TestCaseAgentSessionResult>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-cases/optimize`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  generateProjectTestCases: (token: string, workspaceId: string, projectId: string, input: GenerateTestCasesInput) =>
    requestControlPlane<TestCaseAgentSessionResult>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-cases/generate`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  getProjectTestCase: (token: string, workspaceId: string, projectId: string, caseId: string) =>
    requestControlPlane<TestCase>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-cases/${caseId}`, {
      headers: authHeaders(token),
    }),
  updateProjectTestCase: (token: string, workspaceId: string, projectId: string, caseId: string, input: TestCaseInput) =>
    requestControlPlane<TestCase>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-cases/${caseId}`, {
      method: "PUT",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  deleteProjectTestCase: (token: string, workspaceId: string, projectId: string, caseId: string) =>
    requestControlPlane<TestCase>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-cases/${caseId}`, {
      method: "DELETE",
      headers: authHeaders(token),
    }),
  deleteProjectTestCases: (token: string, workspaceId: string, projectId: string, caseIds: string[]) =>
    requestControlPlane<TestCase[]>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-cases/delete`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify({ caseIds }),
    }),
  listProjectTestCaseRevisions: (token: string, workspaceId: string, projectId: string, caseId: string) =>
    requestControlPlane<TestCaseRevision[]>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-cases/${caseId}/revisions`, {
      headers: authHeaders(token),
    }),
  listProjectTestCaseRunItems: (token: string, workspaceId: string, projectId: string, caseId: string) =>
    requestControlPlane<TestCaseRunItem[]>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-cases/${caseId}/runs`, {
      headers: authHeaders(token),
    }),
  listProjectTestCaseProposals: (token: string, workspaceId: string, projectId: string, input: { status?: string } = {}) => {
    const params = new URLSearchParams();
    if (input.status) params.set("status", input.status);
    const query = params.toString();
    return requestControlPlane<TestCaseProposal[]>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-case-proposals${query ? `?${query}` : ""}`, {
      headers: authHeaders(token),
    });
  },
  applyProjectTestCaseProposal: (
    token: string,
    workspaceId: string,
    projectId: string,
    proposalId: string,
    input: ReviewTestCaseProposalInput = {},
  ) =>
    requestControlPlane<ApplyTestCaseProposalResult>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-case-proposals/${proposalId}/apply`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  rejectProjectTestCaseProposal: (
    token: string,
    workspaceId: string,
    projectId: string,
    proposalId: string,
    input: ReviewTestCaseProposalInput = {},
  ) =>
    requestControlPlane<TestCaseProposal>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-case-proposals/${proposalId}/reject`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  listWorkspaceTestPlans: (token: string, workspaceId: string, input: { status?: string } = {}) => {
    const params = new URLSearchParams();
    if (input.status) params.set("status", input.status);
    const query = params.toString();
    return requestControlPlane<TestPlan[]>(`/api/workspaces/${workspaceId}/test-plans${query ? `?${query}` : ""}`, {
      headers: authHeaders(token),
    });
  },
  createWorkspaceTestPlan: (token: string, workspaceId: string, input: TestPlanInput) =>
    requestControlPlane<TestPlanDetail>(`/api/workspaces/${workspaceId}/test-plans`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  getWorkspaceTestPlan: (token: string, workspaceId: string, planId: string) =>
    requestControlPlane<TestPlanDetail>(`/api/workspaces/${workspaceId}/test-plans/${planId}`, {
      headers: authHeaders(token),
    }),
  updateWorkspaceTestPlan: (token: string, workspaceId: string, planId: string, input: TestPlanInput) =>
    requestControlPlane<TestPlanDetail>(`/api/workspaces/${workspaceId}/test-plans/${planId}`, {
      method: "PUT",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  startWorkspaceTestRun: (token: string, workspaceId: string, planId: string, input: CreateTestRunInput = {}) =>
    requestControlPlane<TestRunDetail>(`/api/workspaces/${workspaceId}/test-plans/${planId}/runs`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  listWorkspaceTestRuns: (token: string, workspaceId: string, options: { status?: string; source?: string } = {}) => {
    const params = new URLSearchParams();
    if (options.status) params.set("status", options.status);
    if (options.source) params.set("source", options.source);
    const query = params.toString();
    return requestControlPlane<TestRun[]>(`/api/workspaces/${workspaceId}/test-runs${query ? `?${query}` : ""}`, {
      headers: authHeaders(token),
    });
  },
  startAdHocWorkspaceTestRun: (token: string, workspaceId: string, input: CreateAdHocTestRunInput) =>
    requestControlPlane<TestRunDetail>(`/api/workspaces/${workspaceId}/test-runs`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  getWorkspaceTestRun: (token: string, workspaceId: string, runId: string) =>
    requestControlPlane<TestRunDetail>(`/api/workspaces/${workspaceId}/test-runs/${runId}`, {
      headers: authHeaders(token),
    }),
  retryWorkspaceTestRun: (token: string, workspaceId: string, runId: string, input: RetryTestRunInput = {}) =>
    requestControlPlane<TestRunDetail>(`/api/workspaces/${workspaceId}/test-runs/${runId}/retry`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  cancelWorkspaceTestRun: (token: string, workspaceId: string, runId: string, input: CancelRuntimeTaskInput = {}) =>
    requestControlPlane<TestRunDetail>(`/api/workspaces/${workspaceId}/test-runs/${runId}/cancel`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  acceptWorkspaceTestRun: (token: string, workspaceId: string, runId: string, input: ReviewTestRunInput = {}) =>
    requestControlPlane<TestRun>(`/api/workspaces/${workspaceId}/test-runs/${runId}/accept`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  blockWorkspaceTestRun: (token: string, workspaceId: string, runId: string, input: ReviewTestRunInput = {}) =>
    requestControlPlane<TestRun>(`/api/workspaces/${workspaceId}/test-runs/${runId}/block`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  listProjectTestPlans: (token: string, workspaceId: string, projectId: string, input: { status?: string } = {}) => {
    const params = new URLSearchParams();
    if (input.status) params.set("status", input.status);
    const query = params.toString();
    return requestControlPlane<TestPlan[]>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-plans${query ? `?${query}` : ""}`, {
      headers: authHeaders(token),
    });
  },
  createProjectTestPlan: (token: string, workspaceId: string, projectId: string, input: TestPlanInput) =>
    requestControlPlane<TestPlanDetail>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-plans`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  getProjectTestPlan: (token: string, workspaceId: string, projectId: string, planId: string) =>
    requestControlPlane<TestPlanDetail>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-plans/${planId}`, {
      headers: authHeaders(token),
    }),
  updateProjectTestPlan: (token: string, workspaceId: string, projectId: string, planId: string, input: TestPlanInput) =>
    requestControlPlane<TestPlanDetail>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-plans/${planId}`, {
      method: "PUT",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  startProjectTestRun: (token: string, workspaceId: string, projectId: string, planId: string, input: CreateTestRunInput = {}) =>
    requestControlPlane<TestRunDetail>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-plans/${planId}/runs`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  listProjectTestRuns: (token: string, workspaceId: string, projectId: string, options: { status?: string; source?: string } = {}) => {
    const params = new URLSearchParams();
    if (options.status) params.set("status", options.status);
    if (options.source) params.set("source", options.source);
    const query = params.toString();
    return requestControlPlane<TestRun[]>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-runs${query ? `?${query}` : ""}`, {
      headers: authHeaders(token),
    });
  },
  startAdHocProjectTestRun: (token: string, workspaceId: string, projectId: string, input: CreateAdHocTestRunInput) =>
    requestControlPlane<TestRunDetail>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-runs`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  getProjectTestRun: (token: string, workspaceId: string, projectId: string, runId: string) =>
    requestControlPlane<TestRunDetail>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-runs/${runId}`, {
      headers: authHeaders(token),
    }),
  retryProjectTestRun: (token: string, workspaceId: string, projectId: string, runId: string, input: RetryTestRunInput = {}) =>
    requestControlPlane<TestRunDetail>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-runs/${runId}/retry`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  cancelProjectTestRun: (token: string, workspaceId: string, projectId: string, runId: string, input: CancelRuntimeTaskInput = {}) =>
    requestControlPlane<TestRunDetail>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-runs/${runId}/cancel`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  acceptProjectTestRun: (token: string, workspaceId: string, projectId: string, runId: string, input: ReviewTestRunInput = {}) =>
    requestControlPlane<TestRun>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-runs/${runId}/accept`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  blockProjectTestRun: (token: string, workspaceId: string, projectId: string, runId: string, input: ReviewTestRunInput = {}) =>
    requestControlPlane<TestRun>(`/api/workspaces/${workspaceId}/projects/${projectId}/test-runs/${runId}/block`, {
      method: "POST",
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
  getWorkspaceGitHubAppInstallation: (token: string, workspaceId: string) =>
    requestControlPlane<WorkspaceGitHubAppInstallation>(`/api/workspaces/${workspaceId}/github-app`, {
      headers: authHeaders(token),
    }),
  listAgents: (token: string, workspaceId: string) =>
    requestControlPlane<AgentEngineCatalogItem[]>(`/api/workspaces/${workspaceId}/agents`, {
      headers: authHeaders(token),
    }),
  listSkills: (token: string, workspaceId: string) =>
    requestControlPlane<SkillCatalogItem[]>(`/api/workspaces/${workspaceId}/skills`, {
      headers: authHeaders(token),
    }),
  getSkill: (token: string, workspaceId: string, skillId: string) =>
    requestControlPlane<SkillDetail>(`/api/workspaces/${workspaceId}/skills/${encodeURIComponent(skillId)}`, {
      headers: authHeaders(token),
    }),
  createSkill: (token: string, workspaceId: string, input: SkillInput) =>
    requestControlPlane<SkillDetail>(`/api/workspaces/${workspaceId}/skills`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  updateSkill: (token: string, workspaceId: string, skillId: string, input: SkillInput) =>
    requestControlPlane<SkillDetail>(`/api/workspaces/${workspaceId}/skills/${encodeURIComponent(skillId)}`, {
      method: "PUT",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  deleteSkill: (token: string, workspaceId: string, skillId: string) =>
    requestControlPlane<{ ok: boolean }>(`/api/workspaces/${workspaceId}/skills/${encodeURIComponent(skillId)}`, {
      method: "DELETE",
      headers: authHeaders(token),
    }),
  duplicateSkill: (token: string, workspaceId: string, skillId: string, input: DuplicateSkillInput = {}) =>
    requestControlPlane<SkillDetail>(`/api/workspaces/${workspaceId}/skills/${encodeURIComponent(skillId)}/duplicate`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  listEnvironments: (token: string, workspaceId: string) =>
    requestControlPlane<Environment[]>(`/api/workspaces/${workspaceId}/environments`, {
      headers: authHeaders(token),
    }),
  createEnvironment: (token: string, workspaceId: string, input: EnvironmentInput) =>
    requestControlPlane<Environment>(`/api/workspaces/${workspaceId}/environments`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  updateEnvironment: (token: string, workspaceId: string, environmentId: string, input: EnvironmentInput) =>
    requestControlPlane<Environment>(`/api/workspaces/${workspaceId}/environments/${environmentId}`, {
      method: "PUT",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  checkEnvironment: (token: string, workspaceId: string, environmentId: string, input: EnvironmentCheckInput = {}) =>
    requestControlPlane<Environment>(`/api/workspaces/${workspaceId}/environments/${environmentId}/check`, {
      method: "POST",
      headers: authHeaders(token),
      body: JSON.stringify(input),
    }),
  deleteEnvironment: (token: string, workspaceId: string, environmentId: string) =>
    requestControlPlane<{ ok: boolean }>(`/api/workspaces/${workspaceId}/environments/${environmentId}`, {
      method: "DELETE",
      headers: authHeaders(token),
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
  checkCluster: (token: string, workspaceId: string, clusterId: string) =>
    requestControlPlane<Cluster>(`/api/workspaces/${workspaceId}/clusters/${clusterId}/check`, {
      method: "POST",
      headers: authHeaders(token),
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
	listIssues: (token: string, workspaceId: string, options?: { includeTestAutomation?: boolean }) => {
		const query = options?.includeTestAutomation ? "?includeTestAutomation=1" : "";
		return requestControlPlane<IssueListItem[]>(`/api/workspaces/${workspaceId}/issues${query}`, {
			headers: authHeaders(token),
		});
	},
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
  requestTestEnvironmentCleanup: (token: string, workspaceId: string, issueId: string, input?: { agentEngine?: string; provider?: string; agentProfile?: string }) =>
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
