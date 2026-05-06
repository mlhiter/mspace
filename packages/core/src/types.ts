export interface Project {
  id: string;
  name: string;
  repoPath: string;
  defaultBranch: string;
  deployCommand: string;
  validationCommand: string;
  kubeContext: string;
  namespace: string;
  createdAt: string;
  updatedAt: string;
}

export interface InboxItem {
  id: string;
  issueId: string;
  projectId: string;
  title: string;
  status: string;
  unread: boolean;
  updatedAt: string;
}

export interface Issue {
  id: string;
  projectId: string;
  title: string;
  body: string;
  status: string;
  assignee: string;
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
}

export interface CreateProjectInput {
  name: string;
  repoPath: string;
  defaultBranch: string;
  deployCommand: string;
  validationCommand: string;
  kubeContext: string;
  namespace: string;
}

export interface CreateIssueInput {
  projectId: string;
  title: string;
  body: string;
  assignee?: string;
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
}

declare global {
  interface Window {
    mspaceDesktop?: MspaceDesktopAPI;
  }
}
