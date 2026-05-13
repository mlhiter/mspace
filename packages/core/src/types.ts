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
  runbookStatus: string;
  runbookUpdatedAt: string;
  runbookSource: string;
  runbookSourceSessionId: string;
  issueCount: number;
  sessionCount: number;
  latestIssueUpdatedAt: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface ProjectRunbook {
  projectId: string;
  content: string;
  status: string;
  source: string;
  sourceSessionId: string;
  contentHash: string;
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

export interface WorkspaceSettings {
	autoCreateDraftPr: boolean;
	createdAt: string;
	updatedAt: string;
}

export interface UpdateWorkspaceSettingsInput {
  autoCreateDraftPr: boolean;
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

export interface TeamInboxItem {
  eventId: string;
  workspaceId: string;
  issueId: string;
  actorUserId: string;
  kind: string;
  summary: string;
  payload: Record<string, unknown>;
  state: string;
  unreadCount: number;
  createdAt: string;
}

export interface CreateTeamIssueEventInput {
	issueId: string;
	actorUserId?: string;
	kind: string;
  summary?: string;
  payload?: Record<string, unknown>;
	recipientUserIds?: string[];
}

export interface RuntimeRegistrationToken {
	id: string;
	workspaceId: string;
	name: string;
	tokenPrefix: string;
	expiresAt: string;
	lastUsedAt: string;
	revoked: boolean;
	createdAt: string;
	updatedAt: string;
}

export interface CreateRuntimeRegistrationTokenInput {
	name: string;
	expiresInHours: number;
}

export interface RuntimeRegistrationTokenResult {
	token: string;
	registrationToken: RuntimeRegistrationToken;
}

export interface RuntimeWorker {
	id: string;
	workspaceId: string;
	name: string;
	mode: string;
	status: string;
	version: string;
	currentLoad: number;
	capabilities: Record<string, unknown>;
	labels: Record<string, unknown>;
	lastSeenAt: string;
	createdAt: string;
	updatedAt: string;
}

export interface RuntimeTask {
	id: string;
	workspaceId: string;
	issueId: string;
	sessionId: string;
	projectId: string;
	kind: string;
	status: string;
	priority: number;
	runtimeMode: string;
	requiredCapabilities: Record<string, unknown>;
	payload: Record<string, unknown>;
	result: Record<string, unknown>;
	claimedByWorkerId: string;
	claimedAt: string;
	startedAt: string;
	finishedAt: string;
	error: string;
	createdAt: string;
	updatedAt: string;
}

export interface CancelRuntimeTaskInput {
	reason?: string;
}

export interface RuntimeTaskEvent {
	id: string;
	workspaceId: string;
	taskId: string;
	workerId: string;
	actorUserId: string;
	kind: string;
	payload: Record<string, unknown>;
	createdAt: string;
}

export interface RuntimeTaskLog {
	id: string;
	workspaceId: string;
	taskId: string;
	workerId: string;
	stream: string;
	message: string;
	createdAt: string;
}

export interface CreateRuntimeTaskInput {
	issueId?: string;
	sessionId?: string;
	projectId?: string;
	kind: string;
	priority?: number;
	runtimeMode?: "personal" | "team" | string;
	requiredCapabilities?: Record<string, unknown>;
	payload?: Record<string, unknown>;
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
  closeReason: string;
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
  closeReason: string;
  triageStatus: string;
  assignee: string;
  assigneeType: string;
  creatorName: string;
  creatorAvatarUrl: string;
  environmentUrl: string;
  createdAt: string;
  updatedAt: string;
}

export interface Comment {
  id: string;
  issueId: string;
  authorType: string;
  authorUserId: string;
  authorName: string;
  authorAvatarUrl: string;
  body: string;
  createdAt: string;
  updatedAt: string;
  editedAt: string;
  reactions: CommentReactionSummary[];
}

export interface CommentReactionSummary {
  reaction: string;
  count: number;
  reactedByMe: boolean;
}

export interface IssueAttachment {
  id: string;
  issueId: string;
  commentId: string;
  filename: string;
  contentType: string;
  sizeBytes: number;
  storageBackend: string;
  url: string;
  createdAt: string;
  updatedAt: string;
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
  runtimeTaskId: string;
  command: string;
  status: string;
  branch: string;
  workdir: string;
  codexThreadId: string;
  codexTurnId: string;
  agentStatus: string;
  artifactDir: string;
  sourceSessionId: string;
  sourceCommitSha: string;
  triggerCommentId: string;
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

export interface SessionFailure {
  id: string;
  issueId: string;
  sessionId: string;
  phase: string;
  status: string;
  failedCommand: string;
  errorSummary: string;
  errorExcerpt: string;
  cluster: string;
  namespace: string;
  resourceKind: string;
  resourceName: string;
  evidenceId: string;
  reviewEvidenceId: string;
  retrySessionId: string;
  continuedSessionId: string;
  createdAt: string;
  updatedAt: string;
}

export interface ReviewEvidenceCommand {
  command: string;
  status: string;
  category: string;
  summary: string;
  createdAt: string;
}

export interface ReviewEvidenceCheck {
  name: string;
  status: string;
  summary: string;
}

export interface ReviewEvidenceResult {
  status: string;
  summary: string;
  details: string;
}

export interface SessionReviewEvidence {
  id: string;
  issueId: string;
  sessionId: string;
  sourceSessionId: string;
  sourceCommitSha: string;
  branch: string;
  agentSummary: string;
  commandsRun: ReviewEvidenceCommand[];
  tests: ReviewEvidenceCheck[];
  buildResult: ReviewEvidenceResult;
  deploymentResult: ReviewEvidenceResult;
  risks: string[];
  followUps: string[];
  previewUrl: string;
  cluster: string;
  namespace: string;
  namespaceStatus: string;
  cleanupStatus: string;
  createdAt: string;
  updatedAt: string;
}

export interface IssueChangeNode {
  id: string;
  issueId: string;
  sessionId: string;
  commitSha: string;
  shortCommitSha: string;
  branch: string;
  subject: string;
  filesChanged: number;
  changes: WorkspaceChange[];
  diffPreview: string;
  diffTruncated: boolean;
  error: string;
  source: string;
  remoteWorkdir: string;
  artifactDir: string;
  createdAt: string;
}

export interface IssueHandoffCommit {
  sha: string;
  shortSha: string;
  subject: string;
}

export interface IssueHandoff {
  id: string;
  issueId: string;
  sourceSessionId: string;
  sourceCommitSha: string;
  branch: string;
  headCommitSha: string;
  commits: IssueHandoffCommit[];
  kind: string;
  prUrl: string;
  prNumber: number;
  prState: string;
  prTitle: string;
  previewUrl: string;
  evidenceSummary: string;
  createdVia: string;
  lastCheckedAt: string;
  error: string;
  createdAt: string;
  updatedAt: string;
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
  sourceSessionId: string;
  sourceCommitSha: string;
  createdAt: string;
  updatedAt: string;
}

export interface IssueTestEnvironmentResources {
  issueId: string;
  clusterId: string;
  clusterName: string;
  context: string;
  namespace: string;
  namespaceStatus: string;
  cleanupStatus: string;
  exposureMode: string;
  previewUrl: string;
  nodeHost: string;
  refreshedAt: string;
  pods: KubernetesPodResource[];
  services: KubernetesServiceResource[];
  deployments: KubernetesDeploymentResource[];
  ingresses: KubernetesIngressResource[];
  events: KubernetesEventResource[];
  errors: KubernetesResourceFetchError[];
}

export interface KubernetesResourceFetchError {
  section: string;
  message: string;
}

export interface KubernetesPodResource {
  name: string;
  phase: string;
  readyContainers: number;
  totalContainers: number;
  restarts: number;
  nodeName: string;
  podIp: string;
  hostIp: string;
  createdAt: string;
  containers: KubernetesPodContainer[];
}

export interface KubernetesPodContainer {
  name: string;
  ready: boolean;
  restartCount: number;
  state: string;
  reason: string;
}

export interface KubernetesServiceResource {
  name: string;
  type: string;
  clusterIp: string;
  externalIp: string;
  createdAt: string;
  ports: KubernetesServicePort[];
}

export interface KubernetesServicePort {
  name: string;
  protocol: string;
  port: number;
  targetPort: string;
  nodePort: number;
  url: string;
}

export interface KubernetesDeploymentResource {
  name: string;
  replicas: number;
  readyReplicas: number;
  updatedReplicas: number;
  availableReplicas: number;
  createdAt: string;
  conditions: KubernetesCondition[];
}

export interface KubernetesCondition {
  type: string;
  status: string;
  reason: string;
  message: string;
}

export interface KubernetesIngressResource {
  name: string;
  className: string;
  hosts: string[];
  addresses: string[];
  createdAt: string;
}

export interface KubernetesEventResource {
  type: string;
  reason: string;
  message: string;
  involvedKind: string;
  involvedName: string;
  count: number;
  firstSeen: string;
  lastSeen: string;
  createdAt: string;
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
  failures: SessionFailure[];
  changeNodes: IssueChangeNode[];
  reviewEvidence: SessionReviewEvidence[];
  handoffs: IssueHandoff[];
}

export interface SessionDetail {
  session: AgentSession;
  issue: Issue;
  project: Project;
  logs: SessionLog[];
  evidence: DeploymentEvidence[];
  failures: SessionFailure[];
  workspace: WorkspaceSnapshot;
}

export interface CreateProjectInput {
  name: string;
  sourceType: string;
  repoPath: string;
  repoUrl: string;
  defaultBranch: string;
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

export interface UpdateProjectRunbookInput {
  content: string;
  status?: string;
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
  creatorName?: string;
  creatorAvatarUrl?: string;
  attachmentIds?: string[];
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
  authorName?: string;
  authorAvatarUrl?: string;
  attachmentIds?: string[];
}

export interface UpdateCommentInput {
  body: string;
  attachmentIds?: string[];
}

export interface CreateSessionInput {
  provider: string;
  agentProfile?: string;
  runtimeMode?: "local" | "team" | string;
  command?: string;
  branch?: string;
  sourceSessionId?: string;
  sourceCommitSha?: string;
  triggerCommentId?: string;
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
  sourceSessionId?: string;
  sourceCommitSha?: string;
}

export interface CreatePullRequestInput {
  sourceSessionId?: string;
  sourceCommitSha?: string;
  title?: string;
  draft?: boolean;
}

export interface SessionStreamEvent {
  type: "log" | "status" | "deploy-stage";
  payload: string;
  stream?: string;
}

export interface MspaceUser {
  id: string;
  name: string;
  email: string;
  avatarUrl: string;
  createdAt: string;
  updatedAt: string;
}

export interface MspaceWorkspace {
  id: string;
  name: string;
  slug: string;
  role: string;
  createdAt: string;
  updatedAt: string;
}

export interface WorkspaceMember {
  id: string;
  workspaceId: string;
  userId: string;
  role: string;
  name: string;
  email: string;
  avatarUrl: string;
  identityLogin: string;
  createdAt: string;
  updatedAt: string;
}

export interface WorkspaceInvitation {
  id: string;
  workspaceId: string;
  workspaceName: string;
  email: string;
  role: string;
  tokenPrefix: string;
  invitedByUserId: string;
  acceptedByUserId: string;
  acceptedAt: string;
  expiresAt: string;
  revoked: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface CreateWorkspaceInvitationInput {
  email?: string;
  role: "member" | "admin";
  expiresInHours: number;
}

export interface WorkspaceInvitationResult {
  token: string;
  invitation: WorkspaceInvitation;
}

export interface AcceptWorkspaceInvitationResult {
  workspace: MspaceWorkspace;
  invitation: WorkspaceInvitation;
  workspaces: MspaceWorkspace[];
}

export interface AuthStartResult {
  authorizeUrl: string;
  state: string;
  pollUrl: string;
}

export interface AuthResult {
  token: string;
  expiresAt: string;
  user: MspaceUser;
  workspaces: MspaceWorkspace[];
}

export type AuthPollResult =
  | { pending: true }
  | ({ pending: false } & AuthResult);

export interface AuthMeResult {
  user: MspaceUser;
  workspaces: MspaceWorkspace[];
}

export interface MspaceDesktopAPI {
  apiBaseUrl: string;
  serverBaseUrl: string;
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
