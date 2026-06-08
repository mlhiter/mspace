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
  defaultEnvironmentId: string;
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

export interface TestCaseStep {
  action: string;
  expected?: string;
}

export interface TestCaseQualityFinding {
  code: string;
  message: string;
}

export type TestCaseStatus = "draft" | "needs_review" | "ready" | "archived" | string;
export type TestCaseType = "functional" | "ui" | "api" | "deployment" | string;
export type TestCaseSource = "manual" | "import" | "codex_generated" | "codex_refined" | string;

export interface TestCase {
  id: string;
  workspaceId: string;
  projectId: string;
  title: string;
  type: TestCaseType;
  area: string;
  priority: string;
  status: TestCaseStatus;
  source: TestCaseSource;
  preconditions: string;
  steps: TestCaseStep[];
  expectedResult: string;
  environmentRequirements: string;
  dependencies: string[];
  tags: string[];
  qualityScore: number;
  qualityFindings: TestCaseQualityFinding[];
  latestResult?: TestCaseLatestResult;
  createdByUserId: string;
  createdAt: string;
  updatedAt: string;
}

export interface TestCaseLatestResult {
  itemId: string;
  runId: string;
  runStatus: string;
  runSource: string;
  status: string;
  actualResult: string;
  failureSummary: string;
  evidence?: Record<string, unknown>;
  updatedAt: string;
}

export interface TestCaseRevision {
  id: string;
  workspaceId: string;
  projectId: string;
  testCaseId: string;
  authorUserId: string;
  revisionNumber: number;
  snapshot: TestCase;
  createdAt: string;
}

export interface TestCaseInput {
  title: string;
  type?: TestCaseType;
  area?: string;
  priority?: string;
  status?: TestCaseStatus;
  source?: TestCaseSource;
  preconditions?: string;
  steps?: TestCaseStep[];
  expectedResult?: string;
  environmentRequirements?: string;
  dependencies?: string[];
  tags?: string[];
}

export interface ImportTestCasesInput {
  format?: "markdown" | "text" | "csv" | "xlsx" | string;
  content: string;
  fileName?: string;
  columnMappings?: TestCaseImportColumnMapping[];
}

export interface TestCaseImportSkip {
  line?: number;
  reason: string;
  content: string;
}

export interface ImportTestCasesResult {
  created: TestCase[];
  skipped: TestCaseImportSkip[];
}

export interface TestCaseListResult {
  cases: TestCase[];
  total: number;
  limit: number;
  offset: number;
}

export interface TestCaseImportPreviewCase {
  title: string;
  type: string;
  status: string;
  qualityScore: number;
  missingFields: string[];
  qualityFindings: TestCaseQualityFinding[];
}

export interface TestCaseImportColumnMapping {
  source: string;
  field: string;
  index: number;
  matched: boolean;
  required: boolean;
  confidence?: number;
  reason?: string;
  strategy?: string;
}

export interface TestCaseImportMappingTaskInput {
  format?: "csv" | "xlsx" | string;
  content: string;
  fileName?: string;
  runtimeMode?: string;
}

export interface TestCaseImportMappingSuggestion {
  source: string;
  field: string;
  index: number;
  confidence: number;
  reason: string;
}

export interface TestCaseImportMappingResult {
  format: string;
  fileName: string;
  suggestions: TestCaseImportMappingSuggestion[];
  warnings: string[];
  threadId?: string;
  turnId?: string;
}

export interface ImportTestCasesPreview {
  format: string;
  fileName: string;
  contentBytes: number;
  maxContentBytes: number;
  maxWorkbookBytes: number;
  maxImportableCases: number;
  parsedCount: number;
  importableCount: number;
  skippedCount: number;
  readyCount: number;
  needsReviewCount: number;
  reachedImportCaseLimit: boolean;
  missingFieldCounts: Record<string, number>;
  qualityFindingCounts: Record<string, number>;
  columnMappings: TestCaseImportColumnMapping[];
  importableCaseSamples: TestCaseImportPreviewCase[];
  skippedSamples: TestCaseImportSkip[];
}

export interface OptimizeTestCasesInput {
  caseIds: string[];
  prompt?: string;
  agentProfile?: string;
  runtimeMode?: string;
}

export interface GenerateTestCasesInput {
  prompt?: string;
  area?: string;
  agentProfile?: string;
  runtimeMode?: string;
}

export interface TestCaseAgentSessionResult {
  issueId: string;
  session: AgentSession;
}

export interface TestCaseProposal {
  id: string;
  workspaceId: string;
  projectId: string;
  sourceIssueId: string;
  sourceSessionId: string;
  targetCaseId: string;
  proposalType: "create" | "update" | "archive" | string;
  status: "pending" | "applied" | "rejected" | "invalid" | string;
  title: string;
  summary: string;
  rationale: string;
  currentCase?: TestCase;
  proposedCase: TestCaseInput;
  qualityScore: number;
  qualityFindings: TestCaseQualityFinding[];
  validationErrors: string[];
  createdByUserId: string;
  reviewedByUserId: string;
  appliedCaseId: string;
  reviewNote: string;
  reviewedAt: string;
  createdAt: string;
  updatedAt: string;
}

export interface ApplyTestCaseProposalResult {
  proposal: TestCaseProposal;
  testCase?: TestCase;
}

export interface ReviewTestCaseProposalInput {
  note?: string;
}

export interface TestPlan {
  id: string;
  workspaceId: string;
  projectId: string;
  title: string;
  description: string;
  setupSteps: string;
  status: "draft" | "ready" | "archived" | string;
  targetType: string;
  targetValue: string;
  environment: string;
  environmentId: string;
  environmentKind: EnvironmentKind | string;
  environmentSnapshot?: Record<string, unknown>;
  caseCount: number;
  createdByUserId: string;
  createdAt: string;
  updatedAt: string;
}

export interface TestPlanCase {
  id: string;
  workspaceId: string;
  projectId: string;
  planId: string;
  testCaseId: string;
  sortOrder: number;
  testCase: TestCase;
}

export interface TestPlanDetail {
  plan: TestPlan;
  cases: TestPlanCase[];
  runs: TestRun[];
}

export interface TestPlanCaseInput {
  projectId: string;
  caseId: string;
}

export interface TestPlanInput {
  title: string;
  description?: string;
  setupSteps?: string;
  status?: string;
  targetType?: string;
  targetValue?: string;
  environment?: string;
  environmentId?: string;
  environmentKind?: EnvironmentKind | string;
  caseIds?: string[];
  cases?: TestPlanCaseInput[];
}

export interface TestRun {
  id: string;
  workspaceId: string;
  projectId: string;
  planId: string;
  source: string;
  parentIssueId: string;
  status: string;
  setupSteps: string;
  setupStatus: string;
  setupIssueId: string;
  setupSessionId: string;
  setupResult?: Record<string, unknown>;
  runContext?: Record<string, unknown>;
  resultLocale: string;
  targetType: string;
  targetValue: string;
  environment: string;
  environmentId: string;
  environmentKind: EnvironmentKind | string;
  environmentSnapshot?: Record<string, unknown>;
  totalCount: number;
  passedCount: number;
  failedCount: number;
  blockedCount: number;
  skippedCount: number;
  acceptanceStatus: string;
  acceptanceNote: string;
  createdByUserId: string;
  acceptedByUserId: string;
  completedAt: string;
  acceptedAt: string;
  createdAt: string;
  updatedAt: string;
}

export interface TestRunItem {
  id: string;
  workspaceId: string;
  projectId: string;
  runId: string;
  testCaseId: string;
  executionIssueId: string;
  agentSessionId: string;
  status: string;
  actualResult: string;
  failureSummary: string;
  evidence: Record<string, unknown>;
  testCase: TestCase;
  createdAt: string;
  updatedAt: string;
}

export interface TestCaseRunItem {
  item: TestRunItem;
  run: TestRun;
}

export interface TestRunDetail {
  run: TestRun;
  plan?: TestPlan;
  items: TestRunItem[];
}

export interface CreateTestRunInput {
  targetType?: string;
  targetValue?: string;
  environment?: string;
  environmentId?: string;
  environmentKind?: EnvironmentKind | string;
  agentProfile?: string;
  runtimeMode?: string;
  batchSize?: number;
  resultLocale?: string;
}

export interface CreateAdHocTestRunInput {
  caseIds?: string[];
  cases?: TestPlanCaseInput[];
  targetType?: string;
  targetValue?: string;
  environment?: string;
  environmentId?: string;
  environmentKind?: EnvironmentKind | string;
  agentProfile?: string;
  runtimeMode?: string;
  batchSize?: number;
  resultLocale?: string;
}

export interface RetryTestRunInput {
  itemIds?: string[];
  agentProfile?: string;
  runtimeMode?: string;
  resultLocale?: string;
}

export interface ReviewTestRunInput {
  note?: string;
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

export type EnvironmentKind = "kubernetes" | "virtual_machine";

export interface KubernetesEnvironmentConfig {
  clusterId: string;
  kubeconfigPath: string;
  kubeContext: string;
  imageRegistryPrefix: string;
  exposureMode: "nodeport" | "ingress" | string;
  nodeHost: string;
  previewDomain: string;
  ingressClass: string;
}

export interface VirtualMachineEnvironmentConfig {
  sshHost: string;
  sshPort: number;
  sshUser: string;
  sshAuthRef: string;
  sshAuthConfigured: boolean;
  workdir: string;
  serviceHint: string;
  labels?: Record<string, unknown>;
}

export interface VirtualMachineSSHAuthInput {
  method: "password" | "private_key" | string;
  password?: string;
  privateKey?: string;
  passphrase?: string;
}

export interface Environment {
  id: string;
  workspaceId: string;
  name: string;
  kind: EnvironmentKind | string;
  status: string;
  projectCount: number;
  issueEnvironmentCount: number;
  testPlanCount: number;
  testRunCount: number;
  kubernetes?: KubernetesEnvironmentConfig;
  virtualMachine?: VirtualMachineEnvironmentConfig;
  lastCheckedAt: string;
  createdAt: string;
  updatedAt: string;
}

export interface ActiveWorkItem {
  issueId: string;
  projectId?: string;
  projectName?: string;
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
	autoDeployTestEnvironment: boolean;
	createdAt: string;
	updatedAt: string;
}

export interface UpdateWorkspaceSettingsInput {
  autoCreateDraftPr: boolean;
  autoDeployTestEnvironment: boolean;
}

export interface InboxItem {
  id: string;
  issueId: string;
  projectId?: string;
  projectName?: string;
  title: string;
  status: string;
  assignee: string;
  assigneeType: string;
  unread: boolean;
  updatedAt: string;
}

export interface WorkspaceInboxItem {
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

export interface CreateWorkspaceIssueEventInput {
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

export interface CreateWorkerInstallationInput {
	name: string;
	expiresInHours: number;
}

export interface WorkerInstallationResult {
	installCommand: string;
	installScriptUrl: string;
	serverUrl: string;
	runtimeMode: string;
	workerName: string;
	credentialPrefix: string;
	expiresAt: string;
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
	projectId?: string;
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

export interface RuntimeTaskListResult {
	tasks: RuntimeTask[];
	total: number;
	limit: number;
	offset: number;
	statusCounts: Record<string, number>;
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
	projectId?: string;
  projectName?: string;
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
  projectId?: string;
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
  workspaceId: string;
  issueId: string;
  commentId: string;
  filename: string;
  contentType: string;
  sizeBytes: number;
  storageBackend: string;
  storageKey?: string;
  createdAt: string;
  updatedAt: string;
  boundAt?: string;
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
  id: number | string;
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
  environmentId: string;
  environmentKind: EnvironmentKind | string;
  environmentSnapshot?: Record<string, unknown>;
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
  project?: Project | null;
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
  project?: Project | null;
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

export interface SuggestIssueTitleInput {
  title?: string;
  body?: string;
  prompt?: string;
}

export interface SuggestIssueTitleResult {
  title: string;
  source: "user" | "ai" | "fallback" | string;
}

export interface UpdateIssueInput {
  projectId?: string;
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

export interface CreateAgentSessionInput {
  provider: string;
  agentProfile?: string;
  runtimeMode?: "personal" | "team" | string;
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

export interface EnvironmentInput {
  name: string;
  kind: EnvironmentKind | string;
  status?: string;
  kubernetes?: Partial<KubernetesEnvironmentConfig>;
  virtualMachine?: Partial<VirtualMachineEnvironmentConfig>;
  sshHost?: string;
  sshPort?: number;
  sshUser?: string;
  sshAuthRef?: string;
  workdir?: string;
  serviceHint?: string;
  labels?: Record<string, unknown>;
  sshAuth?: VirtualMachineSSHAuthInput;
}

export interface EnvironmentCheckInput {
  sshAuth?: VirtualMachineSSHAuthInput;
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
  clusterId?: string;
  environmentId?: string;
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
  kind: "personal" | "team" | string;
  role: string;
  icon: string;
  description: string;
  createdAt: string;
  updatedAt: string;
}

export interface CreateWorkspaceInput {
  name: string;
  kind?: "team" | string;
}

export interface CreateWorkspaceResult {
  workspace: MspaceWorkspace;
  workspaces: MspaceWorkspace[];
}

export interface UpdateWorkspaceInput {
  name: string;
  icon: string;
  description: string;
}

export interface UpdateWorkspaceResult {
  workspace: MspaceWorkspace;
  workspaces: MspaceWorkspace[];
}

export interface PasswordAuthInput {
  login: string;
  password: string;
  name?: string;
  email?: string;
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

export interface WorkspaceInvitationPreview {
  workspaceName: string;
  role: string;
  invitedByName: string;
  invitedByAvatarUrl: string;
  invitedByLogin: string;
  expiresAt: string;
  status: "pending" | "accepted" | "expired" | "revoked";
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

export interface AuthIdentityInfo {
  provider: string;
  login: string;
}

export interface AuthResult {
  token: string;
  expiresAt: string;
  user: MspaceUser;
  workspaces: MspaceWorkspace[];
  isServerAdmin: boolean;
  identity: AuthIdentityInfo;
}

export type AuthPollResult =
  | { pending: true }
  | ({ pending: false } & AuthResult);

export interface AuthMeResult {
  user: MspaceUser;
  workspaces: MspaceWorkspace[];
  isServerAdmin: boolean;
  identity: AuthIdentityInfo;
}

export interface MspaceDesktopAPI {
  serverBaseUrl: string;
  serverBaseUrlSource?: "environment" | "user" | "default";
  serverBaseUrlLocked?: boolean;
  appVersion: string;
  setServerBaseUrl?: (serverUrl: string) => Promise<{
    baseUrl: string;
    source: "environment" | "user" | "default";
    locked: boolean;
  }>;
  resetServerBaseUrl?: () => Promise<{
    baseUrl: string;
    source: "environment" | "user" | "default";
    locked: boolean;
  }>;
  selectProjectFolder?: () => Promise<string | null>;
  selectKubeconfigFiles?: () => Promise<string[]>;
  openExternal?: (url: string) => Promise<void>;
  openPath?: (path: string) => Promise<string>;
  getPendingInviteToken?: () => Promise<string>;
  setPendingInviteToken?: (token: string) => Promise<string>;
  onInviteToken?: (callback: (token: string) => void) => () => void;
  startDockerWorker?: (input: {
    authToken: string;
    workspaceId: string;
    mode?: "personal" | "team";
    serverUrl?: string;
    codex?: boolean;
    containerName?: string;
    workerName?: string;
  }) => Promise<{
    ok: boolean;
    status: string;
    containerName: string;
    script: string;
  }>;
  ensurePersonalWorker?: (input: {
    authToken: string;
    workspaceId: string;
    serverUrl?: string;
  }) => Promise<{
    ok: boolean;
    status: string;
    workerName: string;
  }>;
  stopPersonalWorker?: () => Promise<{
    ok: boolean;
    status: string;
  }>;
}

declare global {
  interface Window {
    mspaceDesktop?: MspaceDesktopAPI;
  }
}
