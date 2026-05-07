export interface Project {
  id: string;
  name: string;
  repoPath: string;
  sourceType: string;
  remoteUrl: string;
  gitProvider: string;
  gitOwner: string;
  gitRepo: string;
  defaultBranch: string;
  deployCommand: string;
  validationCommand: string;
  kubeContext: string;
  namespace: string;
  issueCount: number;
  sessionCount: number;
  latestIssueUpdatedAt: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface InboxItem {
  id: string;
  issueId: string;
  projectId: string;
  projectName: string;
  title: string;
  status: string;
  assignee: string;
  assigneeType: string;
  unread: boolean;
  updatedAt: string;
}

export interface IssueListItem {
  id: string;
  projectId: string;
  projectName: string;
  title: string;
  body: string;
  status: string;
  assignee: string;
  assigneeType: string;
  unread: boolean;
  sessionCount: number;
  updatedAt: string;
  createdAt: string;
}

export interface Issue {
  id: string;
  projectId: string;
  title: string;
  body: string;
  status: string;
  assignee: string;
  assigneeType: string;
  environmentUrl: string;
  createdAt: string;
  updatedAt: string;
}

export interface Comment {
  id: string;
  issueId: string;
  authorType: string;
  body: string;
  createdAt: string;
}

export interface AgentSession {
  id: string;
  issueId: string;
  provider: string;
  runtimeMode: string;
  command: string;
  status: string;
  branch: string;
  workdir: string;
  createdAt: string;
  updatedAt: string;
}

export interface SessionLog {
  id: number;
  sessionId: string;
  stream: string;
  message: string;
  createdAt: string;
}

export interface WorkspaceSnapshot {
  exists: boolean;
  isGitRepository: boolean;
  hasChanges: boolean;
  changedFiles: number;
  untrackedFiles: number;
  head: string;
  shortHead: string;
  branch: string;
  statusLines: string[];
  changes: WorkspaceChange[];
  diffPreview: string;
  diffTruncated: boolean;
  comparison: WorkspaceComparison;
  error: string;
}

export interface WorkspaceChange {
  statusCode: string;
  path: string;
  previousPath: string;
}

export interface WorkspaceComparison {
  baseRef: string;
  mergeBase: string;
  mergeBaseShort: string;
  aheadCount: number;
  behindCount: number;
  commitLines: string[];
  changes: WorkspaceChange[];
  diffPreview: string;
  diffTruncated: boolean;
  error: string;
}

export interface DeploymentEvidence {
  id: string;
  issueId: string;
  sessionId: string;
  cluster: string;
  namespace: string;
  summary: string;
  details: string;
  createdAt: string;
}

export interface IssueDetail {
  issue: Issue;
  project: Project;
  comments: Comment[];
  sessions: AgentSession[];
  evidence: DeploymentEvidence[];
}

export interface SessionDetail {
  session: AgentSession;
  issue: Issue;
  project: Project;
  logs: SessionLog[];
  evidence: DeploymentEvidence[];
  workspace: WorkspaceSnapshot;
}

export interface CreateProjectInput {
  name: string;
  sourceType: string;
  repoPath: string;
  repoUrl: string;
  defaultBranch: string;
  deployCommand: string;
  validationCommand: string;
  kubeContext: string;
  namespace: string;
}

export interface UpdateProjectInput extends CreateProjectInput {
  id: string;
}

export interface CreateIssueInput {
  projectId?: string;
  title?: string;
  body?: string;
  prompt?: string;
  assignee?: string;
  assigneeType?: string;
}

export interface CreateCommentInput {
  body: string;
}

export interface CreateSessionInput {
  provider: string;
  command?: string;
  branch?: string;
}

export interface SessionStreamEvent {
  type: "log" | "status";
  payload: string;
}

export interface MspaceDesktopAPI {
  apiBaseUrl: string;
  appVersion: string;
  selectProjectFolder?: () => Promise<string | null>;
}

declare global {
  interface Window {
    mspaceDesktop?: MspaceDesktopAPI;
  }
}
