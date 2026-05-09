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
  kubeconfigPath: string;
  namespace: string;
  imageRegistryPrefix: string;
  previewDomain: string;
  ingressClass: string;
  nodeHost: string;
  defaultClusterId: string;
  issueCount: number;
  sessionCount: number;
  latestIssueUpdatedAt: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface Cluster {
  id: string;
  name: string;
  kubeconfigPath: string;
  kubeContext: string;
  imageRegistryPrefix: string;
  exposureMode: "nodeport" | "ingress" | string;
  nodeHost: string;
  previewDomain: string;
  ingressClass: string;
  status: string;
  lastCheckedAt: string;
  projectCount: number;
  environmentCount: number;
  createdAt: string;
  updatedAt: string;
}

export interface ActiveWorkItem {
  issueId: string;
  projectId: string;
  projectName: string;
  title: string;
  status: string;
  namespace: string;
  namespaceStatus: string;
  cleanupStatus: string;
  sessionStatus: string;
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
  parentIssueId: string;
  sortOrder: number;
  title: string;
  body: string;
  status: string;
  triageStatus: string;
  assignee: string;
  assigneeType: string;
  labels: IssueLabel[];
  unread: boolean;
  sessionCount: number;
  childIssueCount: number;
  completedChildIssueCount: number;
  updatedAt: string;
  createdAt: string;
}

export interface Issue {
  id: string;
  projectId: string;
  parentIssueId: string;
  sortOrder: number;
  title: string;
  body: string;
  status: string;
  triageStatus: string;
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

export interface IssueLabel {
  id: string;
  issueId: string;
  labelId: string;
  key: string;
  name: string;
  dimension: string;
  color: string;
  sortOrder: number;
  createdAt: string;
}

export interface IssueLabelDefinition {
  id: string;
  key: string;
  name: string;
  dimension: string;
  color: string;
  sortOrder: number;
  builtIn: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface AgentProfile {
  id: string;
  name: string;
  mention: string;
  provider: string;
  description: string;
  instructions: string;
  enabled: boolean;
  builtIn: boolean;
  sortOrder: number;
  createdAt: string;
  updatedAt: string;
}

export interface AgentSession {
  id: string;
  issueId: string;
  provider: string;
  agentProfile: string;
  runtimeMode: string;
  command: string;
  status: string;
  branch: string;
  workdir: string;
  codexThreadId: string;
  codexTurnId: string;
  agentStatus: string;
  artifactDir: string;
  cleanupStatus: string;
  cleanedAt: string;
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

export interface IssueTestEnvironment {
  issueId: string;
  clusterId: string;
  namespace: string;
  namespaceStatus: string;
  cleanupStatus: string;
  previewUrl: string;
  imageRegistryPrefix: string;
  kubeconfigPath: string;
  kubeContext: string;
  exposureMode: string;
  previewDomain: string;
  ingressClass: string;
  nodeHost: string;
  lastDeploySessionId: string;
  lastCleanupSessionId: string;
  createdAt: string;
  updatedAt: string;
}

export interface IssueDetail {
  issue: Issue;
  project: Project;
  testEnvironment: IssueTestEnvironment | null;
  childIssues: IssueListItem[];
  labels: IssueLabel[];
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
  kubeconfigPath: string;
  namespace: string;
  imageRegistryPrefix: string;
  previewDomain: string;
  ingressClass: string;
  nodeHost: string;
  defaultClusterId: string;
}

export interface UpdateProjectInput extends CreateProjectInput {
  id: string;
}

export interface CreateIssueInput {
  projectId?: string;
  title?: string;
  body?: string;
  prompt?: string;
  tasks?: string[];
  childIssues?: CreateIssueTaskInput[];
  labels?: string[];
  labelKeys?: string[];
  assignee?: string;
  assigneeType?: string;
}

export interface UpdateIssueInput {
  title?: string;
  body?: string;
  status?: string;
}

export interface CreateIssueTaskInput {
  title: string;
  body?: string;
  status?: string;
  completed?: boolean;
}

export interface CreateCommentInput {
  body: string;
}

export interface CreateSessionInput {
  provider: string;
  agentProfile?: string;
  command?: string;
  branch?: string;
}

export interface UpdateIssueLabelsInput {
  labels?: string[];
  labelKeys?: string[];
}

export interface AgentProfileInput {
  name: string;
  mention: string;
  provider: string;
  description: string;
  instructions: string;
  enabled: boolean;
}

export interface ClusterInput {
  name: string;
  kubeconfigPath: string;
  kubeContext: string;
  imageRegistryPrefix: string;
  exposureMode: "nodeport" | "ingress";
  nodeHost: string;
  previewDomain: string;
  ingressClass: string;
  status?: string;
}

export interface KubeconfigImportSkip {
  path: string;
  context: string;
  reason: string;
}

export interface KubeconfigCandidate {
  path: string;
  contexts: string[];
}

export interface KubeconfigDiscoveryResult {
  candidates: KubeconfigCandidate[];
  skipped: KubeconfigImportSkip[];
}

export interface KubeconfigImportResult {
  imported: Cluster[];
  skipped: KubeconfigImportSkip[];
}

export interface StartTestDeployInput {
  agentProfile?: string;
  clusterId: string;
  exposureMode?: "nodeport" | "ingress" | "";
  previewDomain?: string;
  ingressClass?: string;
  nodeHost?: string;
}

export interface SessionStreamEvent {
  type: "log" | "status";
  payload: string;
}

export interface MspaceDesktopAPI {
  apiBaseUrl: string;
  appVersion: string;
  selectProjectFolder?: () => Promise<string | null>;
  selectKubeconfigFiles?: () => Promise<string[]>;
  openExternal?: (url: string) => Promise<void>;
  openPath?: (path: string) => Promise<string>;
}

declare global {
  interface Window {
    mspaceDesktop?: MspaceDesktopAPI;
  }
}
