package control

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound            = errors.New("not found")
	ErrExpired             = errors.New("expired")
	ErrForbidden           = errors.New("forbidden")
	ErrConflict            = errors.New("conflict")
	ErrNoActiveCodexWorker = errors.New("no active codex worker")
	ErrNoActiveAgentWorker = errors.New("no active agent worker")
)

type Config struct {
	Addr                       string
	DatabaseURL                string
	StoreMode                  string
	SQLitePath                 string
	GitHubClientID             string
	GitHubClientSecret         string
	GitHubRedirectURI          string
	GitHubAppID                string
	GitHubAppClientID          string
	GitHubAppPrivateKey        string
	ServerAdminLogins          []string
	BootstrapAdminLogin        string
	BootstrapAdminPassword     string
	BootstrapAdminName         string
	BootstrapAdminEmail        string
	BootstrapTeamWorkspaceName string
	BootstrapRuntimeToken      string
	BootstrapRuntimeTokenName  string
	BootstrapRuntimeTokenTTL   time.Duration
	SessionTTL                 time.Duration
	OAuthStateTTL              time.Duration
	AllowMemoryStore           bool
}

func (c Config) withDefaults() Config {
	if c.Addr == "" {
		c.Addr = "127.0.0.1:8787"
	}
	if c.SessionTTL <= 0 {
		c.SessionTTL = 30 * 24 * time.Hour
	}
	if c.OAuthStateTTL <= 0 {
		c.OAuthStateTTL = 10 * time.Minute
	}
	return c
}

type User struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatarUrl"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type UpdateCurrentUserProfileInput struct {
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
}

type Workspace struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Kind        string `json:"kind"`
	Role        string `json:"role"`
	Icon        string `json:"icon"`
	Description string `json:"description"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type CreateWorkspaceInput struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type CreateWorkspaceResult struct {
	Workspace  Workspace   `json:"workspace"`
	Workspaces []Workspace `json:"workspaces"`
}

type UpdateWorkspaceInput struct {
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	Description string `json:"description"`
}

type UpdateWorkspaceResult struct {
	Workspace  Workspace   `json:"workspace"`
	Workspaces []Workspace `json:"workspaces"`
}

type WorkspaceMember struct {
	ID            string `json:"id"`
	WorkspaceID   string `json:"workspaceId"`
	UserID        string `json:"userId"`
	Role          string `json:"role"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	AvatarURL     string `json:"avatarUrl"`
	IdentityLogin string `json:"identityLogin"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

type WorkspaceInvitation struct {
	ID               string `json:"id"`
	WorkspaceID      string `json:"workspaceId"`
	WorkspaceName    string `json:"workspaceName"`
	Email            string `json:"email"`
	Role             string `json:"role"`
	TokenPrefix      string `json:"tokenPrefix"`
	InvitedByUserID  string `json:"invitedByUserId"`
	AcceptedByUserID string `json:"acceptedByUserId"`
	AcceptedAt       string `json:"acceptedAt"`
	ExpiresAt        string `json:"expiresAt"`
	Revoked          bool   `json:"revoked"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

type WorkspaceInvitationPreview struct {
	WorkspaceName      string `json:"workspaceName"`
	Role               string `json:"role"`
	InvitedByName      string `json:"invitedByName"`
	InvitedByAvatarURL string `json:"invitedByAvatarUrl"`
	InvitedByLogin     string `json:"invitedByLogin"`
	ExpiresAt          string `json:"expiresAt"`
	Status             string `json:"status"`
}

type CreateWorkspaceInvitationInput struct {
	Email          string `json:"email"`
	Role           string `json:"role"`
	ExpiresInHours int    `json:"expiresInHours"`
}

type WorkspaceInvitationResult struct {
	Token      string              `json:"token"`
	Invitation WorkspaceInvitation `json:"invitation"`
}

type AcceptWorkspaceInvitationInput struct {
	Token string `json:"token"`
}

type AcceptWorkspaceInvitationResult struct {
	Workspace  Workspace           `json:"workspace"`
	Invitation WorkspaceInvitation `json:"invitation"`
	Workspaces []Workspace         `json:"workspaces"`
}

type IssueEvent struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspaceId"`
	IssueID     string          `json:"issueId"`
	ActorUserID string          `json:"actorUserId"`
	Kind        string          `json:"kind"`
	Summary     string          `json:"summary"`
	Payload     json.RawMessage `json:"payload"`
	CreatedAt   string          `json:"createdAt"`
}

type InboxEntry struct {
	EventID     string          `json:"eventId"`
	WorkspaceID string          `json:"workspaceId"`
	IssueID     string          `json:"issueId"`
	ActorUserID string          `json:"actorUserId"`
	Kind        string          `json:"kind"`
	Summary     string          `json:"summary"`
	Payload     json.RawMessage `json:"payload"`
	State       string          `json:"state"`
	UnreadCount int             `json:"unreadCount"`
	CreatedAt   string          `json:"createdAt"`
}

type CreateIssueEventInput struct {
	WorkspaceID      string          `json:"workspaceId"`
	IssueID          string          `json:"issueId"`
	ActorUserID      string          `json:"actorUserId"`
	Kind             string          `json:"kind"`
	Summary          string          `json:"summary"`
	Payload          json.RawMessage `json:"payload"`
	RecipientUserIDs []string        `json:"recipientUserIds"`
}

type SuggestIssueTitleInput struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	Prompt string `json:"prompt"`
}

type SuggestIssueTitleResult struct {
	Title  string `json:"title"`
	Source string `json:"source"`
}

type ReadThroughInput struct {
	ThroughEventID string `json:"throughEventId"`
}

type Project struct {
	ID                     string `json:"id"`
	WorkspaceID            string `json:"workspaceId"`
	Name                   string `json:"name"`
	RepoPath               string `json:"repoPath"`
	SourceType             string `json:"sourceType"`
	RemoteURL              string `json:"remoteUrl"`
	GitProvider            string `json:"gitProvider"`
	GitOwner               string `json:"gitOwner"`
	GitRepo                string `json:"gitRepo"`
	DefaultBranch          string `json:"defaultBranch"`
	DeployCommand          string `json:"deployCommand"`
	ValidationCommand      string `json:"validationCommand"`
	KubeContext            string `json:"kubeContext"`
	KubeconfigPath         string `json:"kubeconfigPath"`
	Namespace              string `json:"namespace"`
	ImageRegistryPrefix    string `json:"imageRegistryPrefix"`
	PreviewDomain          string `json:"previewDomain"`
	IngressClass           string `json:"ingressClass"`
	NodeHost               string `json:"nodeHost"`
	DefaultClusterID       string `json:"defaultClusterId"`
	DefaultEnvironmentID   string `json:"defaultEnvironmentId"`
	RunbookStatus          string `json:"runbookStatus"`
	RunbookUpdatedAt       string `json:"runbookUpdatedAt"`
	RunbookSource          string `json:"runbookSource"`
	RunbookSourceSessionID string `json:"runbookSourceSessionId"`
	IssueCount             int    `json:"issueCount"`
	SessionCount           int    `json:"sessionCount"`
	LatestIssueUpdatedAt   string `json:"latestIssueUpdatedAt"`
	CreatedAt              string `json:"createdAt"`
	UpdatedAt              string `json:"updatedAt"`
}

type ProjectRunbook struct {
	ProjectID       string `json:"projectId"`
	Content         string `json:"content"`
	Status          string `json:"status"`
	Source          string `json:"source"`
	SourceSessionID string `json:"sourceSessionId"`
	ContentHash     string `json:"contentHash"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type ProjectInput struct {
	Name                 string `json:"name"`
	SourceType           string `json:"sourceType"`
	RepoPath             string `json:"repoPath"`
	RepoURL              string `json:"repoUrl"`
	DefaultBranch        string `json:"defaultBranch"`
	KubeContext          string `json:"kubeContext"`
	KubeconfigPath       string `json:"kubeconfigPath"`
	Namespace            string `json:"namespace"`
	ImageRegistryPrefix  string `json:"imageRegistryPrefix"`
	PreviewDomain        string `json:"previewDomain"`
	IngressClass         string `json:"ingressClass"`
	NodeHost             string `json:"nodeHost"`
	DefaultClusterID     string `json:"defaultClusterId"`
	DefaultEnvironmentID string `json:"defaultEnvironmentId"`
}

type UpdateProjectInput struct {
	ID string `json:"id"`
	ProjectInput
}

type ProjectRunbookInput struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

type TestCaseStep struct {
	Action   string `json:"action"`
	Expected string `json:"expected"`
}

type TestCaseQualityFinding struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type TestCase struct {
	ID                      string                   `json:"id"`
	WorkspaceID             string                   `json:"workspaceId"`
	ProjectID               string                   `json:"projectId"`
	Title                   string                   `json:"title"`
	Type                    string                   `json:"type"`
	Area                    string                   `json:"area"`
	Priority                string                   `json:"priority"`
	Status                  string                   `json:"status"`
	Source                  string                   `json:"source"`
	Preconditions           string                   `json:"preconditions"`
	Steps                   []TestCaseStep           `json:"steps"`
	ExpectedResult          string                   `json:"expectedResult"`
	EnvironmentRequirements string                   `json:"environmentRequirements"`
	Dependencies            []string                 `json:"dependencies"`
	Tags                    []string                 `json:"tags"`
	QualityScore            int                      `json:"qualityScore"`
	QualityFindings         []TestCaseQualityFinding `json:"qualityFindings"`
	LatestResult            *TestCaseLatestResult    `json:"latestResult,omitempty"`
	CreatedByUserID         string                   `json:"createdByUserId"`
	CreatedAt               string                   `json:"createdAt"`
	UpdatedAt               string                   `json:"updatedAt"`
}

type TestCaseLatestResult struct {
	ItemID         string          `json:"itemId"`
	RunID          string          `json:"runId"`
	RunStatus      string          `json:"runStatus"`
	RunSource      string          `json:"runSource"`
	Status         string          `json:"status"`
	ActualResult   string          `json:"actualResult"`
	FailureSummary string          `json:"failureSummary"`
	Evidence       json.RawMessage `json:"evidence,omitempty"`
	UpdatedAt      string          `json:"updatedAt"`
}

type TestCaseRevision struct {
	ID             string   `json:"id"`
	WorkspaceID    string   `json:"workspaceId"`
	ProjectID      string   `json:"projectId"`
	TestCaseID     string   `json:"testCaseId"`
	AuthorUserID   string   `json:"authorUserId"`
	RevisionNumber int      `json:"revisionNumber"`
	Snapshot       TestCase `json:"snapshot"`
	CreatedAt      string   `json:"createdAt"`
}

type TestCaseInput struct {
	Title                   string         `json:"title"`
	Type                    string         `json:"type"`
	Area                    string         `json:"area"`
	Priority                string         `json:"priority"`
	Status                  string         `json:"status"`
	Source                  string         `json:"source"`
	Preconditions           string         `json:"preconditions"`
	Steps                   []TestCaseStep `json:"steps"`
	ExpectedResult          string         `json:"expectedResult"`
	EnvironmentRequirements string         `json:"environmentRequirements"`
	Dependencies            []string       `json:"dependencies"`
	Tags                    []string       `json:"tags"`
}

type TestCaseListOptions struct {
	Status string `json:"status"`
	Query  string `json:"query"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

type TestCaseListResult struct {
	Cases  []TestCase `json:"cases"`
	Total  int        `json:"total"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

type DeleteProjectTestCasesInput struct {
	CaseIDs []string `json:"caseIds"`
}

type ImportTestCasesInput struct {
	Format         string                        `json:"format"`
	Content        string                        `json:"content"`
	FileName       string                        `json:"fileName"`
	ColumnMappings []TestCaseImportColumnMapping `json:"columnMappings"`
}

type TestCaseImportSkip struct {
	Line    int    `json:"line"`
	Reason  string `json:"reason"`
	Content string `json:"content"`
}

type ImportTestCasesResult struct {
	Created []TestCase           `json:"created"`
	Skipped []TestCaseImportSkip `json:"skipped"`
}

type TestCaseImportPreviewCase struct {
	Title           string                   `json:"title"`
	Type            string                   `json:"type"`
	Status          string                   `json:"status"`
	QualityScore    int                      `json:"qualityScore"`
	MissingFields   []string                 `json:"missingFields"`
	QualityFindings []TestCaseQualityFinding `json:"qualityFindings"`
}

type TestCaseImportColumnMapping struct {
	Source     string  `json:"source"`
	Field      string  `json:"field"`
	Index      int     `json:"index"`
	Matched    bool    `json:"matched"`
	Required   bool    `json:"required"`
	Confidence float64 `json:"confidence,omitempty"`
	Reason     string  `json:"reason,omitempty"`
	Strategy   string  `json:"strategy,omitempty"`
}

type TestCaseImportMappingTaskInput struct {
	Format      string `json:"format"`
	Content     string `json:"content"`
	FileName    string `json:"fileName"`
	RuntimeMode string `json:"runtimeMode"`
}

type TestCaseImportMappingSuggestion struct {
	Source     string  `json:"source"`
	Field      string  `json:"field"`
	Index      int     `json:"index"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

type TestCaseImportMappingResult struct {
	Format      string                            `json:"format"`
	FileName    string                            `json:"fileName"`
	Suggestions []TestCaseImportMappingSuggestion `json:"suggestions"`
	Warnings    []string                          `json:"warnings"`
	ThreadID    string                            `json:"threadId,omitempty"`
	TurnID      string                            `json:"turnId,omitempty"`
}

type ImportTestCasesPreview struct {
	Format                 string                        `json:"format"`
	FileName               string                        `json:"fileName"`
	ContentBytes           int                           `json:"contentBytes"`
	MaxContentBytes        int                           `json:"maxContentBytes"`
	MaxWorkbookBytes       int                           `json:"maxWorkbookBytes"`
	MaxImportableCases     int                           `json:"maxImportableCases"`
	ParsedCount            int                           `json:"parsedCount"`
	ImportableCount        int                           `json:"importableCount"`
	SkippedCount           int                           `json:"skippedCount"`
	ReadyCount             int                           `json:"readyCount"`
	NeedsReviewCount       int                           `json:"needsReviewCount"`
	ReachedImportCaseLimit bool                          `json:"reachedImportCaseLimit"`
	MissingFieldCounts     map[string]int                `json:"missingFieldCounts"`
	QualityFindingCounts   map[string]int                `json:"qualityFindingCounts"`
	ColumnMappings         []TestCaseImportColumnMapping `json:"columnMappings"`
	ImportableCaseSamples  []TestCaseImportPreviewCase   `json:"importableCaseSamples"`
	SkippedSamples         []TestCaseImportSkip          `json:"skippedSamples"`
}

func normalizeImportTestCasesResult(value ImportTestCasesResult) ImportTestCasesResult {
	if value.Created == nil {
		value.Created = []TestCase{}
	}
	if value.Skipped == nil {
		value.Skipped = []TestCaseImportSkip{}
	}
	return value
}

func normalizeImportTestCasesPreview(value ImportTestCasesPreview) ImportTestCasesPreview {
	if value.MissingFieldCounts == nil {
		value.MissingFieldCounts = map[string]int{}
	}
	if value.QualityFindingCounts == nil {
		value.QualityFindingCounts = map[string]int{}
	}
	if value.ColumnMappings == nil {
		value.ColumnMappings = []TestCaseImportColumnMapping{}
	}
	if value.ImportableCaseSamples == nil {
		value.ImportableCaseSamples = []TestCaseImportPreviewCase{}
	}
	if value.SkippedSamples == nil {
		value.SkippedSamples = []TestCaseImportSkip{}
	}
	return value
}

func testCaseImportSkipsOrEmpty(values []TestCaseImportSkip) []TestCaseImportSkip {
	if values == nil {
		return []TestCaseImportSkip{}
	}
	return values
}

type OptimizeTestCasesInput struct {
	CaseIDs            []string `json:"caseIds"`
	Prompt             string   `json:"prompt"`
	AgentEngine        string   `json:"agentEngine"`
	LegacyProvider     string   `json:"provider,omitempty"`
	LegacyAgentProfile string   `json:"agentProfile,omitempty"`
	RuntimeMode        string   `json:"runtimeMode"`
}

type GenerateTestCasesInput struct {
	Prompt             string `json:"prompt"`
	Area               string `json:"area"`
	AgentEngine        string `json:"agentEngine"`
	LegacyProvider     string `json:"provider,omitempty"`
	LegacyAgentProfile string `json:"agentProfile,omitempty"`
	RuntimeMode        string `json:"runtimeMode"`
}

type TestCaseAgentSessionResult struct {
	IssueID string       `json:"issueId"`
	Session AgentSession `json:"session"`
}

type TestCaseProposal struct {
	ID               string                   `json:"id"`
	WorkspaceID      string                   `json:"workspaceId"`
	ProjectID        string                   `json:"projectId"`
	SourceIssueID    string                   `json:"sourceIssueId"`
	SourceSessionID  string                   `json:"sourceSessionId"`
	TargetCaseID     string                   `json:"targetCaseId"`
	ProposalType     string                   `json:"proposalType"`
	Status           string                   `json:"status"`
	Title            string                   `json:"title"`
	Summary          string                   `json:"summary"`
	Rationale        string                   `json:"rationale"`
	CurrentCase      *TestCase                `json:"currentCase,omitempty"`
	ProposedCase     TestCaseInput            `json:"proposedCase"`
	QualityScore     int                      `json:"qualityScore"`
	QualityFindings  []TestCaseQualityFinding `json:"qualityFindings"`
	ValidationErrors []string                 `json:"validationErrors"`
	CreatedByUserID  string                   `json:"createdByUserId"`
	ReviewedByUserID string                   `json:"reviewedByUserId"`
	AppliedCaseID    string                   `json:"appliedCaseId"`
	ReviewNote       string                   `json:"reviewNote"`
	ReviewedAt       string                   `json:"reviewedAt"`
	CreatedAt        string                   `json:"createdAt"`
	UpdatedAt        string                   `json:"updatedAt"`
}

type TestCaseProposalListOptions struct {
	Status string `json:"status"`
}

type ReviewTestCaseProposalInput struct {
	Note string `json:"note"`
}

type ApplyTestCaseProposalResult struct {
	Proposal TestCaseProposal `json:"proposal"`
	TestCase *TestCase        `json:"testCase,omitempty"`
}

type TestPlan struct {
	ID                  string          `json:"id"`
	WorkspaceID         string          `json:"workspaceId"`
	ProjectID           string          `json:"projectId"`
	Title               string          `json:"title"`
	Description         string          `json:"description"`
	SetupSteps          string          `json:"setupSteps"`
	Status              string          `json:"status"`
	TargetType          string          `json:"targetType"`
	TargetValue         string          `json:"targetValue"`
	Environment         string          `json:"environment"`
	EnvironmentID       string          `json:"environmentId"`
	EnvironmentKind     string          `json:"environmentKind"`
	EnvironmentSnapshot json.RawMessage `json:"environmentSnapshot,omitempty"`
	CaseCount           int             `json:"caseCount"`
	CreatedByUserID     string          `json:"createdByUserId"`
	CreatedAt           string          `json:"createdAt"`
	UpdatedAt           string          `json:"updatedAt"`
}

type TestPlanCase struct {
	ID          string   `json:"id"`
	WorkspaceID string   `json:"workspaceId"`
	ProjectID   string   `json:"projectId"`
	PlanID      string   `json:"planId"`
	TestCaseID  string   `json:"testCaseId"`
	SortOrder   int      `json:"sortOrder"`
	TestCase    TestCase `json:"testCase"`
}

type TestPlanDetail struct {
	Plan                 TestPlan        `json:"plan"`
	Cases                []TestPlanCase  `json:"cases"`
	Runs                 []TestRun       `json:"runs"`
	RequiredCapabilities json.RawMessage `json:"requiredCapabilities"`
}

type TestPlanCaseInput struct {
	ProjectID string `json:"projectId"`
	CaseID    string `json:"caseId"`
}

type TestPlanInput struct {
	Title               string              `json:"title"`
	Description         string              `json:"description"`
	SetupSteps          string              `json:"setupSteps"`
	Status              string              `json:"status"`
	TargetType          string              `json:"targetType"`
	TargetValue         string              `json:"targetValue"`
	Environment         string              `json:"environment"`
	EnvironmentID       string              `json:"environmentId"`
	EnvironmentKind     string              `json:"environmentKind"`
	EnvironmentSnapshot json.RawMessage     `json:"environmentSnapshot,omitempty"`
	CaseIDs             []string            `json:"caseIds"`
	Cases               []TestPlanCaseInput `json:"cases"`
}

type TestPlanListOptions struct {
	Status string `json:"status"`
}

type TestRunListOptions struct {
	Status string `json:"status"`
	Source string `json:"source"`
}

type TestCaseRunItem struct {
	Item TestRunItem `json:"item"`
	Run  TestRun     `json:"run"`
}

type TestRun struct {
	ID                  string          `json:"id"`
	WorkspaceID         string          `json:"workspaceId"`
	ProjectID           string          `json:"projectId"`
	PlanID              string          `json:"planId"`
	Source              string          `json:"source"`
	ParentIssueID       string          `json:"parentIssueId"`
	Status              string          `json:"status"`
	SetupSteps          string          `json:"setupSteps"`
	SetupStatus         string          `json:"setupStatus"`
	SetupIssueID        string          `json:"setupIssueId"`
	SetupSessionID      string          `json:"setupSessionId"`
	SetupResult         json.RawMessage `json:"setupResult,omitempty"`
	RunContext          json.RawMessage `json:"runContext,omitempty"`
	ResultLocale        string          `json:"resultLocale"`
	TargetType          string          `json:"targetType"`
	TargetValue         string          `json:"targetValue"`
	Environment         string          `json:"environment"`
	EnvironmentID       string          `json:"environmentId"`
	EnvironmentKind     string          `json:"environmentKind"`
	EnvironmentSnapshot json.RawMessage `json:"environmentSnapshot,omitempty"`
	TotalCount          int             `json:"totalCount"`
	PassedCount         int             `json:"passedCount"`
	FailedCount         int             `json:"failedCount"`
	BlockedCount        int             `json:"blockedCount"`
	SkippedCount        int             `json:"skippedCount"`
	AcceptanceStatus    string          `json:"acceptanceStatus"`
	AcceptanceNote      string          `json:"acceptanceNote"`
	CreatedByUserID     string          `json:"createdByUserId"`
	AcceptedByUserID    string          `json:"acceptedByUserId"`
	CompletedAt         string          `json:"completedAt"`
	AcceptedAt          string          `json:"acceptedAt"`
	CreatedAt           string          `json:"createdAt"`
	UpdatedAt           string          `json:"updatedAt"`
}

type TestRunItem struct {
	ID               string          `json:"id"`
	WorkspaceID      string          `json:"workspaceId"`
	ProjectID        string          `json:"projectId"`
	RunID            string          `json:"runId"`
	TestCaseID       string          `json:"testCaseId"`
	SortOrder        int             `json:"sortOrder"`
	ExecutionIssueID string          `json:"executionIssueId"`
	AgentSessionID   string          `json:"agentSessionId"`
	Status           string          `json:"status"`
	ActualResult     string          `json:"actualResult"`
	FailureSummary   string          `json:"failureSummary"`
	Evidence         json.RawMessage `json:"evidence"`
	TestCase         TestCase        `json:"testCase"`
	CreatedAt        string          `json:"createdAt"`
	UpdatedAt        string          `json:"updatedAt"`
}

type TestArtifact struct {
	ID              string          `json:"id"`
	WorkspaceID     string          `json:"workspaceId"`
	ProjectID       string          `json:"projectId"`
	RunID           string          `json:"runId"`
	RunItemID       string          `json:"runItemId"`
	CaseID          string          `json:"caseId"`
	SourceIssueID   string          `json:"sourceIssueId"`
	SourceTaskID    string          `json:"sourceTaskId"`
	SourceSessionID string          `json:"sourceSessionId"`
	Kind            string          `json:"kind"`
	Role            string          `json:"role"`
	Filename        string          `json:"filename"`
	ContentType     string          `json:"contentType"`
	SizeBytes       int64           `json:"sizeBytes"`
	SHA256          string          `json:"sha256"`
	StorageBackend  string          `json:"storageBackend"`
	StorageKey      string          `json:"storageKey"`
	Content         []byte          `json:"-"`
	Metadata        json.RawMessage `json:"metadata"`
	CreatedAt       string          `json:"createdAt"`
}

type TestArtifactRef struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	Role         string          `json:"role"`
	Filename     string          `json:"filename"`
	ContentType  string          `json:"contentType"`
	SizeBytes    int64           `json:"sizeBytes"`
	SHA256       string          `json:"sha256"`
	URL          string          `json:"url"`
	ThumbnailURL string          `json:"thumbnailUrl"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
	CreatedAt    string          `json:"createdAt"`
}

type CreateTestArtifactInput struct {
	WorkspaceID     string
	ProjectID       string
	RunID           string
	RunItemID       string
	CaseID          string
	SourceIssueID   string
	SourceTaskID    string
	SourceSessionID string
	Kind            string
	Role            string
	Filename        string
	ContentType     string
	Content         []byte
	Metadata        json.RawMessage
}

type TestRunDetail struct {
	Run                  TestRun         `json:"run"`
	Plan                 *TestPlan       `json:"plan,omitempty"`
	Items                []TestRunItem   `json:"items"`
	RequiredCapabilities json.RawMessage `json:"requiredCapabilities"`
}

type CreateTestRunInput struct {
	TargetType          string          `json:"targetType"`
	TargetValue         string          `json:"targetValue"`
	Environment         string          `json:"environment"`
	EnvironmentID       string          `json:"environmentId"`
	EnvironmentKind     string          `json:"environmentKind"`
	EnvironmentSnapshot json.RawMessage `json:"environmentSnapshot,omitempty"`
	AgentEngine         string          `json:"agentEngine"`
	LegacyProvider      string          `json:"provider,omitempty"`
	LegacyAgentProfile  string          `json:"agentProfile,omitempty"`
	RuntimeMode         string          `json:"runtimeMode"`
	BatchSize           int             `json:"batchSize"`
	ResultLocale        string          `json:"resultLocale"`
}

type CreateAdHocTestRunInput struct {
	CaseIDs            []string            `json:"caseIds"`
	Cases              []TestPlanCaseInput `json:"cases"`
	TargetType         string              `json:"targetType"`
	TargetValue        string              `json:"targetValue"`
	Environment        string              `json:"environment"`
	EnvironmentID      string              `json:"environmentId"`
	EnvironmentKind    string              `json:"environmentKind"`
	AgentEngine        string              `json:"agentEngine"`
	LegacyProvider     string              `json:"provider,omitempty"`
	LegacyAgentProfile string              `json:"agentProfile,omitempty"`
	RuntimeMode        string              `json:"runtimeMode"`
	BatchSize          int                 `json:"batchSize"`
	ResultLocale       string              `json:"resultLocale"`
}

type ReviewTestRunInput struct {
	Note string `json:"note"`
}

type RetryTestRunInput struct {
	ItemIDs            []string `json:"itemIds"`
	AgentEngine        string   `json:"agentEngine"`
	LegacyProvider     string   `json:"provider,omitempty"`
	LegacyAgentProfile string   `json:"agentProfile,omitempty"`
	RuntimeMode        string   `json:"runtimeMode"`
	ResultLocale       string   `json:"resultLocale"`
}

type Issue struct {
	ID             string `json:"id"`
	WorkspaceID    string `json:"workspaceId"`
	ProjectID      string `json:"projectId"`
	ParentIssueID  string `json:"parentIssueId"`
	SortOrder      int    `json:"sortOrder"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	Status         string `json:"status"`
	CloseReason    string `json:"closeReason"`
	TriageStatus   string `json:"triageStatus"`
	Assignee       string `json:"assignee"`
	AssigneeType   string `json:"assigneeType"`
	CreatorName    string `json:"creatorName"`
	CreatorAvatar  string `json:"creatorAvatarUrl"`
	EnvironmentURL string `json:"environmentUrl"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

type IssueListItem struct {
	ID                       string       `json:"id"`
	WorkspaceID              string       `json:"workspaceId"`
	ProjectID                string       `json:"projectId"`
	ProjectName              string       `json:"projectName"`
	ParentIssueID            string       `json:"parentIssueId"`
	SortOrder                int          `json:"sortOrder"`
	Title                    string       `json:"title"`
	Body                     string       `json:"body"`
	Status                   string       `json:"status"`
	CloseReason              string       `json:"closeReason"`
	TriageStatus             string       `json:"triageStatus"`
	Assignee                 string       `json:"assignee"`
	AssigneeType             string       `json:"assigneeType"`
	Labels                   []IssueLabel `json:"labels"`
	Unread                   bool         `json:"unread"`
	SessionCount             int          `json:"sessionCount"`
	ChildIssueCount          int          `json:"childIssueCount"`
	CompletedChildIssueCount int          `json:"completedChildIssueCount"`
	UpdatedAt                string       `json:"updatedAt"`
	CreatedAt                string       `json:"createdAt"`
}

type Comment struct {
	ID           string                   `json:"id"`
	IssueID      string                   `json:"issueId"`
	AuthorType   string                   `json:"authorType"`
	AuthorUserID string                   `json:"authorUserId"`
	AuthorName   string                   `json:"authorName"`
	AuthorAvatar string                   `json:"authorAvatarUrl"`
	Body         string                   `json:"body"`
	CreatedAt    string                   `json:"createdAt"`
	UpdatedAt    string                   `json:"updatedAt"`
	EditedAt     string                   `json:"editedAt"`
	Reactions    []CommentReactionSummary `json:"reactions"`
}

type CommentReactionSummary struct {
	Reaction    string `json:"reaction"`
	Count       int    `json:"count"`
	ReactedByMe bool   `json:"reactedByMe"`
}

type IssueAttachment struct {
	ID             string `json:"id"`
	WorkspaceID    string `json:"workspaceId"`
	IssueID        string `json:"issueId"`
	CommentID      string `json:"commentId"`
	Filename       string `json:"filename"`
	ContentType    string `json:"contentType"`
	SizeBytes      int64  `json:"sizeBytes"`
	StorageBackend string `json:"storageBackend"`
	StorageKey     string `json:"storageKey"`
	Content        []byte `json:"-"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
	BoundAt        string `json:"boundAt"`
}

type CreateIssueAttachmentInput struct {
	Filename    string
	ContentType string
	Content     []byte
}

type IssueLabel struct {
	ID        string `json:"id"`
	IssueID   string `json:"issueId"`
	LabelID   string `json:"labelId"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	Dimension string `json:"dimension"`
	Color     string `json:"color"`
	SortOrder int    `json:"sortOrder"`
	CreatedAt string `json:"createdAt"`
}

type IssueLabelDefinition struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	Dimension string `json:"dimension"`
	Color     string `json:"color"`
	SortOrder int    `json:"sortOrder"`
	BuiltIn   bool   `json:"builtIn"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type IssueDetail struct {
	Issue           Issue                   `json:"issue"`
	Project         Project                 `json:"project"`
	TestEnvironment *IssueTestEnvironment   `json:"testEnvironment"`
	ChildIssues     []IssueListItem         `json:"childIssues"`
	Labels          []IssueLabel            `json:"labels"`
	Comments        []Comment               `json:"comments"`
	Sessions        []AgentSession          `json:"sessions"`
	Evidence        []DeploymentEvidence    `json:"evidence"`
	Failures        []SessionFailure        `json:"failures"`
	ChangeNodes     []IssueChangeNode       `json:"changeNodes"`
	ReviewEvidence  []SessionReviewEvidence `json:"reviewEvidence"`
	Handoffs        []IssueHandoff          `json:"handoffs"`
}

type AgentSession struct {
	ID               string                       `json:"id"`
	IssueID          string                       `json:"issueId"`
	AgentEngine      string                       `json:"agentEngine"`
	RuntimeMode      string                       `json:"runtimeMode"`
	RuntimeTaskID    string                       `json:"runtimeTaskId"`
	Command          string                       `json:"command"`
	Status           string                       `json:"status"`
	Branch           string                       `json:"branch"`
	Workdir          string                       `json:"workdir"`
	CodexThreadID    string                       `json:"codexThreadId"`
	CodexTurnID      string                       `json:"codexTurnId"`
	EngineSessionRef string                       `json:"engineSessionRef"`
	EngineRunRef     string                       `json:"engineRunRef"`
	AgentStatus      string                       `json:"agentStatus"`
	ArtifactDir      string                       `json:"artifactDir"`
	SourceSessionID  string                       `json:"sourceSessionId"`
	SourceCommitSHA  string                       `json:"sourceCommitSha"`
	TriggerCommentID string                       `json:"triggerCommentId"`
	Automation       string                       `json:"automation,omitempty"`
	Payload          json.RawMessage              `json:"payload,omitempty"`
	RequiredSkills   []AgentSessionSkillReference `json:"requiredSkills,omitempty"`
	Skills           []AgentSessionSkillReference `json:"skills,omitempty"`
	CleanupStatus    string                       `json:"cleanupStatus"`
	CleanedAt        string                       `json:"cleanedAt"`
	CreatedAt        string                       `json:"createdAt"`
	UpdatedAt        string                       `json:"updatedAt"`
}

type AgentSessionSkillReference struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Source      string `json:"source"`
	Revision    string `json:"revision"`
	ContentHash string `json:"contentHash"`
	BuiltIn     bool   `json:"builtIn"`
}

type SessionLog struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Stream    string `json:"stream"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
}

type WorkspaceChange struct {
	StatusCode   string `json:"statusCode"`
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath"`
}

type WorkspaceComparison struct {
	BaseRef        string            `json:"baseRef"`
	MergeBase      string            `json:"mergeBase"`
	MergeBaseShort string            `json:"mergeBaseShort"`
	AheadCount     int               `json:"aheadCount"`
	BehindCount    int               `json:"behindCount"`
	CommitLines    []string          `json:"commitLines"`
	Changes        []WorkspaceChange `json:"changes"`
	DiffPreview    string            `json:"diffPreview"`
	DiffTruncated  bool              `json:"diffTruncated"`
	Error          string            `json:"error"`
}

type WorkspaceSnapshot struct {
	Exists          bool                `json:"exists"`
	IsGitRepository bool                `json:"isGitRepository"`
	HasChanges      bool                `json:"hasChanges"`
	ChangedFiles    int                 `json:"changedFiles"`
	UntrackedFiles  int                 `json:"untrackedFiles"`
	Head            string              `json:"head"`
	ShortHead       string              `json:"shortHead"`
	Branch          string              `json:"branch"`
	StatusLines     []string            `json:"statusLines"`
	Changes         []WorkspaceChange   `json:"changes"`
	DiffPreview     string              `json:"diffPreview"`
	DiffTruncated   bool                `json:"diffTruncated"`
	Comparison      WorkspaceComparison `json:"comparison"`
	Error           string              `json:"error"`
}

type IssueChangeNode struct {
	ID             string            `json:"id"`
	IssueID        string            `json:"issueId"`
	SessionID      string            `json:"sessionId"`
	CommitSHA      string            `json:"commitSha"`
	ShortCommitSHA string            `json:"shortCommitSha"`
	Branch         string            `json:"branch"`
	Subject        string            `json:"subject"`
	FilesChanged   int               `json:"filesChanged"`
	Changes        []WorkspaceChange `json:"changes"`
	DiffPreview    string            `json:"diffPreview"`
	DiffTruncated  bool              `json:"diffTruncated"`
	Error          string            `json:"error"`
	Source         string            `json:"source"`
	RemoteWorkdir  string            `json:"remoteWorkdir"`
	ArtifactDir    string            `json:"artifactDir"`
	CreatedAt      string            `json:"createdAt"`
}

type DeploymentEvidence struct {
	ID        string `json:"id"`
	IssueID   string `json:"issueId"`
	SessionID string `json:"sessionId"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Summary   string `json:"summary"`
	Details   string `json:"details"`
	CreatedAt string `json:"createdAt"`
}

type SessionFailure struct {
	ID                 string `json:"id"`
	IssueID            string `json:"issueId"`
	SessionID          string `json:"sessionId"`
	Phase              string `json:"phase"`
	Status             string `json:"status"`
	FailedCommand      string `json:"failedCommand"`
	ErrorSummary       string `json:"errorSummary"`
	ErrorExcerpt       string `json:"errorExcerpt"`
	Cluster            string `json:"cluster"`
	Namespace          string `json:"namespace"`
	ResourceKind       string `json:"resourceKind"`
	ResourceName       string `json:"resourceName"`
	EvidenceID         string `json:"evidenceId"`
	ReviewEvidenceID   string `json:"reviewEvidenceId"`
	RetrySessionID     string `json:"retrySessionId"`
	ContinuedSessionID string `json:"continuedSessionId"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt"`
}

type ReviewEvidenceCommand struct {
	Command   string `json:"command"`
	Status    string `json:"status"`
	Category  string `json:"category"`
	Summary   string `json:"summary"`
	CreatedAt string `json:"createdAt"`
}

type ReviewEvidenceCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type ReviewEvidenceResult struct {
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Details string `json:"details"`
}

type SessionReviewEvidence struct {
	ID               string                  `json:"id"`
	IssueID          string                  `json:"issueId"`
	SessionID        string                  `json:"sessionId"`
	SourceSessionID  string                  `json:"sourceSessionId"`
	SourceCommitSHA  string                  `json:"sourceCommitSha"`
	Branch           string                  `json:"branch"`
	AgentSummary     string                  `json:"agentSummary"`
	CommandsRun      []ReviewEvidenceCommand `json:"commandsRun"`
	Tests            []ReviewEvidenceCheck   `json:"tests"`
	BuildResult      ReviewEvidenceResult    `json:"buildResult"`
	DeploymentResult ReviewEvidenceResult    `json:"deploymentResult"`
	Risks            []string                `json:"risks"`
	FollowUps        []string                `json:"followUps"`
	PreviewURL       string                  `json:"previewUrl"`
	Cluster          string                  `json:"cluster"`
	Namespace        string                  `json:"namespace"`
	NamespaceStatus  string                  `json:"namespaceStatus"`
	CleanupStatus    string                  `json:"cleanupStatus"`
	CreatedAt        string                  `json:"createdAt"`
	UpdatedAt        string                  `json:"updatedAt"`
}

type RuntimeTaskArtifactResult struct {
	TestEnvironment   *RuntimeTaskTestEnvironmentArtifact `json:"testEnvironment,omitempty"`
	ReviewEvidence    *SessionReviewEvidenceArtifact      `json:"reviewEvidence,omitempty"`
	TestCaseProposals *TestCaseProposalArtifact           `json:"testCaseProposals,omitempty"`
	TestSetup         *TestSetupResultArtifact            `json:"testSetup,omitempty"`
	TestResult        *TestResultArtifact                 `json:"testResult,omitempty"`
}

type RuntimeTaskTestEnvironmentArtifact struct {
	PreviewURL      string `json:"previewUrl"`
	PreviewURLSnake string `json:"preview_url"`
	URL             string `json:"url"`
}

type SessionReviewEvidenceArtifact struct {
	AgentSummary     string                  `json:"agentSummary"`
	CommandsRun      []ReviewEvidenceCommand `json:"commandsRun"`
	Tests            []ReviewEvidenceCheck   `json:"tests"`
	BuildResult      ReviewEvidenceResult    `json:"buildResult"`
	DeploymentResult ReviewEvidenceResult    `json:"deploymentResult"`
	Risks            []string                `json:"risks"`
	FollowUps        []string                `json:"followUps"`
}

type TestCaseProposalArtifact struct {
	Proposals []TestCaseProposalArtifactItem `json:"proposals"`
	Summary   string                         `json:"summary"`
}

type TestCaseProposalArtifactItem struct {
	Type         string        `json:"type"`
	CaseID       string        `json:"caseId"`
	Title        string        `json:"title"`
	Summary      string        `json:"summary"`
	Rationale    string        `json:"rationale"`
	ProposedCase TestCaseInput `json:"proposedCase"`
}

type TestSetupResultArtifact struct {
	RunID          string                `json:"runId"`
	Status         string                `json:"status"`
	Summary        string                `json:"summary"`
	FailureSummary string                `json:"failureSummary"`
	Outputs        json.RawMessage       `json:"outputs"`
	Evidence       json.RawMessage       `json:"evidence"`
	Steps          []TestSetupResultStep `json:"steps"`
}

type TestSetupResultStep struct {
	Title          string          `json:"title"`
	Status         string          `json:"status"`
	Command        string          `json:"command"`
	Summary        string          `json:"summary"`
	FailureSummary string          `json:"failureSummary"`
	Evidence       json.RawMessage `json:"evidence"`
}

type TestResultArtifact struct {
	RunID   string                   `json:"runId"`
	Items   []TestResultArtifactItem `json:"items"`
	Summary string                   `json:"summary"`
}

type TestResultArtifactItem struct {
	CaseID         string          `json:"caseId"`
	Status         string          `json:"status"`
	ActualResult   string          `json:"actualResult"`
	FailureSummary string          `json:"failureSummary"`
	Evidence       json.RawMessage `json:"evidence"`
}

type IssueHandoffCommit struct {
	SHA      string `json:"sha"`
	ShortSHA string `json:"shortSha"`
	Subject  string `json:"subject"`
}

type IssueHandoff struct {
	ID              string               `json:"id"`
	IssueID         string               `json:"issueId"`
	SourceSessionID string               `json:"sourceSessionId"`
	SourceCommitSHA string               `json:"sourceCommitSha"`
	Branch          string               `json:"branch"`
	HeadCommitSHA   string               `json:"headCommitSha"`
	Commits         []IssueHandoffCommit `json:"commits"`
	Kind            string               `json:"kind"`
	PRURL           string               `json:"prUrl"`
	PRNumber        int                  `json:"prNumber"`
	PRState         string               `json:"prState"`
	PRTitle         string               `json:"prTitle"`
	PreviewURL      string               `json:"previewUrl"`
	EvidenceSummary string               `json:"evidenceSummary"`
	CreatedVia      string               `json:"createdVia"`
	LastCheckedAt   string               `json:"lastCheckedAt"`
	Error           string               `json:"error"`
	CreatedAt       string               `json:"createdAt"`
	UpdatedAt       string               `json:"updatedAt"`
}

type IssueTestEnvironment struct {
	IssueID              string          `json:"issueId"`
	ClusterID            string          `json:"clusterId"`
	EnvironmentID        string          `json:"environmentId"`
	EnvironmentKind      string          `json:"environmentKind"`
	EnvironmentSnapshot  json.RawMessage `json:"environmentSnapshot,omitempty"`
	Namespace            string          `json:"namespace"`
	NamespaceStatus      string          `json:"namespaceStatus"`
	CleanupStatus        string          `json:"cleanupStatus"`
	PreviewURL           string          `json:"previewUrl"`
	ImageRegistryPrefix  string          `json:"imageRegistryPrefix"`
	KubeconfigPath       string          `json:"kubeconfigPath"`
	KubeContext          string          `json:"kubeContext"`
	ExposureMode         string          `json:"exposureMode"`
	PreviewDomain        string          `json:"previewDomain"`
	IngressClass         string          `json:"ingressClass"`
	NodeHost             string          `json:"nodeHost"`
	LastDeploySessionID  string          `json:"lastDeploySessionId"`
	LastCleanupSessionID string          `json:"lastCleanupSessionId"`
	SourceSessionID      string          `json:"sourceSessionId"`
	SourceCommitSHA      string          `json:"sourceCommitSha"`
	CreatedAt            string          `json:"createdAt"`
	UpdatedAt            string          `json:"updatedAt"`
}

type IssueTestEnvironmentResources struct {
	IssueID         string                         `json:"issueId"`
	ClusterID       string                         `json:"clusterId"`
	ClusterName     string                         `json:"clusterName"`
	Context         string                         `json:"context"`
	Namespace       string                         `json:"namespace"`
	NamespaceStatus string                         `json:"namespaceStatus"`
	CleanupStatus   string                         `json:"cleanupStatus"`
	ExposureMode    string                         `json:"exposureMode"`
	PreviewURL      string                         `json:"previewUrl"`
	NodeHost        string                         `json:"nodeHost"`
	RefreshedAt     string                         `json:"refreshedAt"`
	Pods            []KubernetesPodResource        `json:"pods"`
	Services        []KubernetesServiceResource    `json:"services"`
	Deployments     []KubernetesDeploymentResource `json:"deployments"`
	Ingresses       []KubernetesIngressResource    `json:"ingresses"`
	Events          []KubernetesEventResource      `json:"events"`
	Errors          []KubernetesResourceFetchError `json:"errors"`
}

type KubernetesResourceFetchError struct {
	Section string `json:"section"`
	Message string `json:"message"`
}

type KubernetesPodResource struct {
	Name            string                   `json:"name"`
	Phase           string                   `json:"phase"`
	ReadyContainers int                      `json:"readyContainers"`
	TotalContainers int                      `json:"totalContainers"`
	Restarts        int32                    `json:"restarts"`
	NodeName        string                   `json:"nodeName"`
	PodIP           string                   `json:"podIp"`
	HostIP          string                   `json:"hostIp"`
	CreatedAt       string                   `json:"createdAt"`
	Containers      []KubernetesPodContainer `json:"containers"`
}

type KubernetesPodContainer struct {
	Name         string `json:"name"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restartCount"`
	State        string `json:"state"`
	Reason       string `json:"reason"`
}

type KubernetesServiceResource struct {
	Name       string                  `json:"name"`
	Type       string                  `json:"type"`
	ClusterIP  string                  `json:"clusterIp"`
	ExternalIP string                  `json:"externalIp"`
	CreatedAt  string                  `json:"createdAt"`
	Ports      []KubernetesServicePort `json:"ports"`
}

type KubernetesServicePort struct {
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`
	Port       int32  `json:"port"`
	TargetPort string `json:"targetPort"`
	NodePort   int32  `json:"nodePort"`
	URL        string `json:"url"`
}

type KubernetesDeploymentResource struct {
	Name              string                `json:"name"`
	Replicas          int32                 `json:"replicas"`
	ReadyReplicas     int32                 `json:"readyReplicas"`
	UpdatedReplicas   int32                 `json:"updatedReplicas"`
	AvailableReplicas int32                 `json:"availableReplicas"`
	CreatedAt         string                `json:"createdAt"`
	Conditions        []KubernetesCondition `json:"conditions"`
}

type KubernetesCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type KubernetesIngressResource struct {
	Name      string   `json:"name"`
	ClassName string   `json:"className"`
	Hosts     []string `json:"hosts"`
	Addresses []string `json:"addresses"`
	CreatedAt string   `json:"createdAt"`
}

type KubernetesEventResource struct {
	Type         string `json:"type"`
	Reason       string `json:"reason"`
	Message      string `json:"message"`
	InvolvedKind string `json:"involvedKind"`
	InvolvedName string `json:"involvedName"`
	Count        int32  `json:"count"`
	FirstSeen    string `json:"firstSeen"`
	LastSeen     string `json:"lastSeen"`
	CreatedAt    string `json:"createdAt"`
}

type SessionDetail struct {
	Session   AgentSession         `json:"session"`
	Issue     Issue                `json:"issue"`
	Project   Project              `json:"project"`
	Logs      []SessionLog         `json:"logs"`
	Evidence  []DeploymentEvidence `json:"evidence"`
	Failures  []SessionFailure     `json:"failures"`
	Workspace WorkspaceSnapshot    `json:"workspace"`
}

type CreateAgentSessionInput struct {
	AgentEngine          string               `json:"agentEngine"`
	LegacyProvider       string               `json:"provider,omitempty"`
	LegacyAgentProfile   string               `json:"agentProfile,omitempty"`
	RuntimeMode          string               `json:"runtimeMode"`
	Command              string               `json:"command"`
	Branch               string               `json:"branch"`
	SourceSessionID      string               `json:"sourceSessionId"`
	SourceCommitSHA      string               `json:"sourceCommitSha"`
	TriggerCommentID     string               `json:"triggerCommentId"`
	Automation           string               `json:"automation"`
	TestRunID            string               `json:"testRunId"`
	TestRunBatchSize     int                  `json:"testRunBatchSize"`
	RequiredCapabilities json.RawMessage      `json:"-"`
	SkillSlugs           []string             `json:"skillSlugs,omitempty"`
	SkillBundles         []RuntimeSkillBundle `json:"-"`
}

type SkillCatalogItem struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	SourceType  string `json:"sourceType"`
	Revision    string `json:"revision"`
	ContentHash string `json:"contentHash"`
	Enabled     bool   `json:"enabled"`
	Invocable   bool   `json:"invocable"`
	BuiltIn     bool   `json:"builtIn"`
	Editable    bool   `json:"editable"`
	Deletable   bool   `json:"deletable"`
	FileCount   int    `json:"fileCount"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type SkillDetail struct {
	SkillCatalogItem
	Files []RuntimeSkillFile `json:"files,omitempty"`
}

type SkillInput struct {
	Slug        string             `json:"slug"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Enabled     *bool              `json:"enabled,omitempty"`
	Invocable   *bool              `json:"invocable,omitempty"`
	Files       []RuntimeSkillFile `json:"files,omitempty"`
}

type DuplicateSkillInput struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled,omitempty"`
	Invocable   *bool  `json:"invocable,omitempty"`
}

type WorkspaceSkill struct {
	ID                string `json:"id"`
	WorkspaceID       string `json:"workspaceId"`
	Slug              string `json:"slug"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	SourceType        string `json:"sourceType"`
	Enabled           bool   `json:"enabled"`
	Invocable         bool   `json:"invocable"`
	CurrentRevisionID string `json:"currentRevisionId"`
	CreatedBy         string `json:"createdBy"`
	DeletedAt         string `json:"deletedAt"`
	CreatedAt         string `json:"createdAt"`
	UpdatedAt         string `json:"updatedAt"`
}

type WorkspaceSkillRevision struct {
	ID          string             `json:"id"`
	WorkspaceID string             `json:"workspaceId"`
	SkillID     string             `json:"skillId"`
	Revision    string             `json:"revision"`
	ContentHash string             `json:"contentHash"`
	Files       []RuntimeSkillFile `json:"files"`
	CreatedBy   string             `json:"createdBy"`
	CreatedAt   string             `json:"createdAt"`
}

type WorkspaceBuiltinSkillSetting struct {
	WorkspaceID string `json:"workspaceId"`
	Slug        string `json:"slug"`
	Enabled     bool   `json:"enabled"`
	Invocable   bool   `json:"invocable"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type RuntimeSkillBundle struct {
	Slug        string             `json:"slug"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Source      string             `json:"source"`
	Revision    string             `json:"revision"`
	ContentHash string             `json:"contentHash"`
	BuiltIn     bool               `json:"builtIn"`
	Files       []RuntimeSkillFile `json:"files"`
}

type RuntimeSkillFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
}

type CreateIssueInput struct {
	ProjectID     string           `json:"projectId"`
	Title         string           `json:"title"`
	TitleSource   string           `json:"titleSource"`
	Body          string           `json:"body"`
	Prompt        string           `json:"prompt"`
	Tasks         []string         `json:"tasks"`
	ChildIssues   []IssueTaskInput `json:"childIssues"`
	Labels        []string         `json:"labels"`
	LabelKeys     []string         `json:"labelKeys"`
	Assignee      string           `json:"assignee"`
	AssigneeType  string           `json:"assigneeType"`
	CreatorName   string           `json:"creatorName"`
	CreatorAvatar string           `json:"creatorAvatarUrl"`
	AttachmentIDs []string         `json:"attachmentIds"`
}

type IssueListOptions struct {
	IncludeTestAutomation bool `json:"includeTestAutomation"`
}

type IssueTaskInput struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	Status    string `json:"status"`
	Completed bool   `json:"completed"`
}

type UpdateIssueInput struct {
	ProjectID     *string `json:"projectId"`
	Title         *string `json:"title"`
	ExpectedTitle *string `json:"expectedTitle"`
	Body          *string `json:"body"`
	Status        *string `json:"status"`
}

type UpdateIssueLabelsInput struct {
	Labels    []string `json:"labels"`
	LabelKeys []string `json:"labelKeys"`
}

type CreateCommentInput struct {
	Body          string   `json:"body"`
	AuthorName    string   `json:"authorName"`
	AuthorAvatar  string   `json:"authorAvatarUrl"`
	AttachmentIDs []string `json:"attachmentIds"`
}

type UpdateCommentInput struct {
	Body          string   `json:"body"`
	AttachmentIDs []string `json:"attachmentIds"`
}

type WorkspaceSettings struct {
	AutoCreateDraftPR         bool   `json:"autoCreateDraftPr"`
	AutoDeployTestEnvironment bool   `json:"autoDeployTestEnvironment"`
	CreatedAt                 string `json:"createdAt"`
	UpdatedAt                 string `json:"updatedAt"`
}

type WorkspaceSettingsInput struct {
	AutoCreateDraftPR         bool `json:"autoCreateDraftPr"`
	AutoDeployTestEnvironment bool `json:"autoDeployTestEnvironment"`
}

const (
	WorkspaceGitHubAppStatusUnavailable  = "unavailable"
	WorkspaceGitHubAppStatusNotConnected = "not_connected"
	WorkspaceGitHubAppStatusConnected    = "connected"
	WorkspaceGitHubAppStatusNeedsAction  = "needs_attention"
)

type WorkspaceGitHubAppInstallation struct {
	Available           bool              `json:"available"`
	Status              string            `json:"status"`
	InstallationID      string            `json:"installationId"`
	AccountLogin        string            `json:"accountLogin"`
	AccountType         string            `json:"accountType"`
	RepositorySelection string            `json:"repositorySelection"`
	Permissions         map[string]string `json:"permissions"`
	RequiredPermissions map[string]string `json:"requiredPermissions"`
	MissingPermissions  []string          `json:"missingPermissions"`
	HTMLURL             string            `json:"htmlUrl"`
	RepositoriesURL     string            `json:"repositoriesUrl"`
	Error               string            `json:"error"`
	LastSyncedAt        string            `json:"lastSyncedAt"`
	CreatedAt           string            `json:"createdAt"`
	UpdatedAt           string            `json:"updatedAt"`
}

type Cluster struct {
	ID                  string `json:"id"`
	WorkspaceID         string `json:"workspaceId"`
	Name                string `json:"name"`
	KubeconfigPath      string `json:"kubeconfigPath"`
	KubeContext         string `json:"kubeContext"`
	ImageRegistryPrefix string `json:"imageRegistryPrefix"`
	ExposureMode        string `json:"exposureMode"`
	NodeHost            string `json:"nodeHost"`
	PreviewDomain       string `json:"previewDomain"`
	IngressClass        string `json:"ingressClass"`
	Status              string `json:"status"`
	LastCheckedAt       string `json:"lastCheckedAt"`
	ProjectCount        int    `json:"projectCount"`
	EnvironmentCount    int    `json:"environmentCount"`
	CreatedAt           string `json:"createdAt"`
	UpdatedAt           string `json:"updatedAt"`
}

type ClusterInput struct {
	Name                string `json:"name"`
	KubeconfigPath      string `json:"kubeconfigPath"`
	KubeContext         string `json:"kubeContext"`
	ImageRegistryPrefix string `json:"imageRegistryPrefix"`
	ExposureMode        string `json:"exposureMode"`
	NodeHost            string `json:"nodeHost"`
	PreviewDomain       string `json:"previewDomain"`
	IngressClass        string `json:"ingressClass"`
	Status              string `json:"status"`
}

type Environment struct {
	ID                    string                           `json:"id"`
	WorkspaceID           string                           `json:"workspaceId"`
	Name                  string                           `json:"name"`
	Kind                  string                           `json:"kind"`
	Status                string                           `json:"status"`
	ProjectCount          int                              `json:"projectCount"`
	IssueEnvironmentCount int                              `json:"issueEnvironmentCount"`
	TestPlanCount         int                              `json:"testPlanCount"`
	TestRunCount          int                              `json:"testRunCount"`
	Kubernetes            *KubernetesEnvironmentConfig     `json:"kubernetes,omitempty"`
	VirtualMachine        *VirtualMachineEnvironmentConfig `json:"virtualMachine,omitempty"`
	LastCheckedAt         string                           `json:"lastCheckedAt"`
	CreatedAt             string                           `json:"createdAt"`
	UpdatedAt             string                           `json:"updatedAt"`
}

type KubernetesEnvironmentConfig struct {
	ClusterID           string `json:"clusterId"`
	KubeconfigPath      string `json:"kubeconfigPath"`
	KubeContext         string `json:"kubeContext"`
	ImageRegistryPrefix string `json:"imageRegistryPrefix"`
	ExposureMode        string `json:"exposureMode"`
	NodeHost            string `json:"nodeHost"`
	PreviewDomain       string `json:"previewDomain"`
	IngressClass        string `json:"ingressClass"`
}

type VirtualMachineEnvironmentConfig struct {
	SSHHost           string          `json:"sshHost"`
	SSHPort           int             `json:"sshPort"`
	SSHUser           string          `json:"sshUser"`
	SSHAuthRef        string          `json:"sshAuthRef"`
	SSHAuthConfigured bool            `json:"sshAuthConfigured"`
	Workdir           string          `json:"workdir"`
	ServiceHint       string          `json:"serviceHint"`
	Labels            json.RawMessage `json:"labels,omitempty"`
}

type VirtualMachineSSHAuthInput struct {
	Method     string `json:"method"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"privateKey,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

type EnvironmentInput struct {
	Name           string                           `json:"name"`
	Kind           string                           `json:"kind"`
	Status         string                           `json:"status"`
	SSHHost        string                           `json:"sshHost"`
	SSHPort        int                              `json:"sshPort"`
	SSHUser        string                           `json:"sshUser"`
	SSHAuthRef     string                           `json:"sshAuthRef"`
	Workdir        string                           `json:"workdir"`
	ServiceHint    string                           `json:"serviceHint"`
	Labels         json.RawMessage                  `json:"labels,omitempty"`
	SSHAuth        *VirtualMachineSSHAuthInput      `json:"sshAuth,omitempty"`
	Kubernetes     *KubernetesEnvironmentConfig     `json:"kubernetes,omitempty"`
	VirtualMachine *VirtualMachineEnvironmentConfig `json:"virtualMachine,omitempty"`
}

type EnvironmentCheckInput struct {
	SSHAuth *VirtualMachineSSHAuthInput `json:"sshAuth,omitempty"`
}

type virtualMachineStoredSSHAuth struct {
	Method     string `json:"method"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"privateKey,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

type KubeconfigImportSkip struct {
	Path    string `json:"path"`
	Context string `json:"context"`
	Reason  string `json:"reason"`
}

type KubeconfigCandidate struct {
	Path     string   `json:"path"`
	Contexts []string `json:"contexts"`
}

type KubeconfigDiscoveryResult struct {
	Candidates []KubeconfigCandidate  `json:"candidates"`
	Skipped    []KubeconfigImportSkip `json:"skipped"`
}

type KubeconfigImportResult struct {
	Imported []Cluster              `json:"imported"`
	Skipped  []KubeconfigImportSkip `json:"skipped"`
}

type StartTestDeployInput struct {
	AgentEngine        string `json:"agentEngine"`
	LegacyProvider     string `json:"provider,omitempty"`
	LegacyAgentProfile string `json:"agentProfile,omitempty"`
	ClusterID          string `json:"clusterId"`
	EnvironmentID      string `json:"environmentId"`
	ExposureMode       string `json:"exposureMode"`
	PreviewDomain      string `json:"previewDomain"`
	IngressClass       string `json:"ingressClass"`
	NodeHost           string `json:"nodeHost"`
	SourceSessionID    string `json:"sourceSessionId"`
	SourceCommitSHA    string `json:"sourceCommitSha"`
}

type TestEnvironmentSessionResult struct {
	SessionID       string               `json:"sessionId"`
	TestEnvironment IssueTestEnvironment `json:"testEnvironment"`
}

type CreatePullRequestInput struct {
	SourceSessionID string `json:"sourceSessionId"`
	SourceCommitSHA string `json:"sourceCommitSha"`
	Title           string `json:"title"`
	Draft           bool   `json:"draft"`
}

type RuntimeRegistrationToken struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`
	TokenPrefix string `json:"tokenPrefix"`
	ExpiresAt   string `json:"expiresAt"`
	LastUsedAt  string `json:"lastUsedAt"`
	Revoked     bool   `json:"revoked"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type CreateRuntimeRegistrationTokenInput struct {
	Name           string `json:"name"`
	ExpiresInHours int    `json:"expiresInHours"`
}

type EnsureRuntimeRegistrationTokenInput struct {
	Token          string
	Name           string
	ExpiresInHours int
}

type RuntimeRegistrationTokenResult struct {
	Token             string                   `json:"token"`
	RegistrationToken RuntimeRegistrationToken `json:"registrationToken"`
}

type CreateWorkerInstallationInput struct {
	Name           string `json:"name"`
	ExpiresInHours int    `json:"expiresInHours"`
}

type WorkerInstallationResult struct {
	InstallCommand   string `json:"installCommand"`
	InstallScriptURL string `json:"installScriptUrl"`
	ServerURL        string `json:"serverUrl"`
	RuntimeMode      string `json:"runtimeMode"`
	WorkerName       string `json:"workerName"`
	CredentialPrefix string `json:"credentialPrefix"`
	ExpiresAt        string `json:"expiresAt"`
}

type RuntimeRegistration struct {
	TokenID     string
	WorkspaceID string
}

type RuntimeWorker struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspaceId"`
	Name         string          `json:"name"`
	Mode         string          `json:"mode"`
	Status       string          `json:"status"`
	Version      string          `json:"version"`
	CurrentLoad  int             `json:"currentLoad"`
	Capabilities json.RawMessage `json:"capabilities"`
	Labels       json.RawMessage `json:"labels"`
	LastSeenAt   string          `json:"lastSeenAt"`
	CreatedAt    string          `json:"createdAt"`
	UpdatedAt    string          `json:"updatedAt"`
}

type RuntimeAvailability struct {
	WorkspaceID          string          `json:"workspaceId"`
	RuntimeMode          string          `json:"runtimeMode"`
	RequiredCapabilities json.RawMessage `json:"requiredCapabilities"`
	State                string          `json:"state"`
	ReasonCode           string          `json:"reasonCode"`
	CanQueue             bool            `json:"canQueue"`
	CanAutoStart         bool            `json:"canAutoStart"`
	RetryAfterMs         int             `json:"retryAfterMs"`
	MatchedWorker        *RuntimeWorker  `json:"matchedWorker,omitempty"`
	LastSeenAt           string          `json:"lastSeenAt,omitempty"`
	MissingCapabilities  []string        `json:"missingCapabilities,omitempty"`
	ActiveWorkerMaxAgeMs int64           `json:"activeWorkerMaxAgeMs"`
}

type RuntimeWorkerInput struct {
	Name         string          `json:"name"`
	Mode         string          `json:"mode"`
	Status       string          `json:"status"`
	Version      string          `json:"version"`
	CurrentLoad  int             `json:"currentLoad"`
	Capabilities json.RawMessage `json:"capabilities"`
	Labels       json.RawMessage `json:"labels"`
}

type RuntimeTask struct {
	ID                   string          `json:"id"`
	WorkspaceID          string          `json:"workspaceId"`
	IssueID              string          `json:"issueId"`
	SessionID            string          `json:"sessionId"`
	ProjectID            string          `json:"projectId"`
	Kind                 string          `json:"kind"`
	Status               string          `json:"status"`
	Priority             int             `json:"priority"`
	RuntimeMode          string          `json:"runtimeMode"`
	RequiredCapabilities json.RawMessage `json:"requiredCapabilities"`
	Payload              json.RawMessage `json:"payload"`
	Result               json.RawMessage `json:"result"`
	ClaimedByWorkerID    string          `json:"claimedByWorkerId"`
	ClaimedAt            string          `json:"claimedAt"`
	StartedAt            string          `json:"startedAt"`
	FinishedAt           string          `json:"finishedAt"`
	Error                string          `json:"error"`
	CreatedAt            string          `json:"createdAt"`
	UpdatedAt            string          `json:"updatedAt"`
}

type RuntimeTaskListOptions struct {
	Limit  int
	Offset int
}

type RuntimeTaskListResult struct {
	Tasks        []RuntimeTask  `json:"tasks"`
	Total        int            `json:"total"`
	Limit        int            `json:"limit"`
	Offset       int            `json:"offset"`
	StatusCounts map[string]int `json:"statusCounts"`
}

type RuntimeTaskEvent struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspaceId"`
	TaskID      string          `json:"taskId"`
	WorkerID    string          `json:"workerId"`
	ActorUserID string          `json:"actorUserId"`
	Kind        string          `json:"kind"`
	Payload     json.RawMessage `json:"payload"`
	CreatedAt   string          `json:"createdAt"`
}

type RuntimeTaskLog struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	TaskID      string `json:"taskId"`
	WorkerID    string `json:"workerId"`
	Stream      string `json:"stream"`
	Message     string `json:"message"`
	CreatedAt   string `json:"createdAt"`
}

type CreateRuntimeTaskInput struct {
	IssueID              string          `json:"issueId"`
	SessionID            string          `json:"sessionId"`
	ProjectID            string          `json:"projectId"`
	Kind                 string          `json:"kind"`
	Priority             int             `json:"priority"`
	RuntimeMode          string          `json:"runtimeMode"`
	RequiredCapabilities json.RawMessage `json:"requiredCapabilities"`
	Payload              json.RawMessage `json:"payload"`
}

type UpdateRuntimeTaskStatusInput struct {
	Status string          `json:"status"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

type AppendRuntimeTaskLogInput struct {
	Stream  string `json:"stream"`
	Message string `json:"message"`
}

type CancelRuntimeTaskInput struct {
	Reason string `json:"reason"`
}

type OAuthState struct {
	State       string
	Provider    string
	RedirectURI string
	ExpiresAt   time.Time
}

type AuthStartResult struct {
	AuthorizeURL string `json:"authorizeUrl"`
	State        string `json:"state"`
	PollURL      string `json:"pollUrl"`
}

type IdentityProfile struct {
	Provider       string
	ProviderUserID string
	Login          string
	Name           string
	Email          string
	AvatarURL      string
	RawProfile     json.RawMessage
}

type AuthResult struct {
	Token         string           `json:"token"`
	ExpiresAt     string           `json:"expiresAt"`
	User          User             `json:"user"`
	Workspaces    []Workspace      `json:"workspaces"`
	IsServerAdmin bool             `json:"isServerAdmin"`
	Identity      AuthIdentityInfo `json:"identity"`
}

type AuthIdentityInfo struct {
	Provider string `json:"provider"`
	Login    string `json:"login"`
}

type AuthPollResult struct {
	Pending bool `json:"pending"`
	AuthResult
}

type PasswordAuthInput struct {
	Login    string `json:"login"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type Store interface {
	EnsureBootstrapAdmin(ctx Context, input PasswordAuthInput) (User, []Workspace, bool, error)
	SaveOAuthState(ctx Context, state OAuthState) error
	ConsumeOAuthState(ctx Context, provider, state string) (OAuthState, error)
	SaveOAuthResult(ctx Context, provider, state string, result AuthResult, expiresAt time.Time) error
	ConsumeOAuthResult(ctx Context, provider, state string) (AuthResult, bool, error)
	UpsertIdentity(ctx Context, profile IdentityProfile) (User, []Workspace, error)
	CreatePasswordIdentity(ctx Context, input PasswordAuthInput) (User, []Workspace, error)
	AuthenticatePassword(ctx Context, input PasswordAuthInput) (User, []Workspace, error)
	GetUserAuthIdentity(ctx Context, userID string) (AuthIdentityInfo, error)
	UpdateCurrentUserProfile(ctx Context, userID string, input UpdateCurrentUserProfileInput) (User, error)
	CreateAuthSession(ctx Context, userID string, ttl time.Duration) (token string, expiresAt time.Time, err error)
	GetUserBySessionToken(ctx Context, token string) (User, []Workspace, error)
	CreateWorkspace(ctx Context, userID string, input CreateWorkspaceInput) (Workspace, []Workspace, error)
	UpdateWorkspace(ctx Context, userID, workspaceID string, input UpdateWorkspaceInput) (Workspace, []Workspace, error)
	ListWorkspaceMembers(ctx Context, userID, workspaceID string) ([]WorkspaceMember, error)
	CreateWorkspaceInvitation(ctx Context, userID, workspaceID string, input CreateWorkspaceInvitationInput) (WorkspaceInvitationResult, error)
	ListWorkspaceInvitations(ctx Context, userID, workspaceID string) ([]WorkspaceInvitation, error)
	RevokeWorkspaceInvitation(ctx Context, userID, workspaceID, invitationID string) (WorkspaceInvitation, error)
	PreviewWorkspaceInvitation(ctx Context, token string) (WorkspaceInvitationPreview, error)
	AcceptWorkspaceInvitation(ctx Context, userID string, input AcceptWorkspaceInvitationInput) (AcceptWorkspaceInvitationResult, error)
	ListInbox(ctx Context, userID, workspaceID string) ([]InboxEntry, error)
	CreateIssueEvent(ctx Context, requesterUserID string, input CreateIssueEventInput) (IssueEvent, error)
	MarkIssueEventRead(ctx Context, userID, workspaceID, eventID string) error
	MarkIssueReadThrough(ctx Context, userID, workspaceID, issueID, throughEventID string) (int, error)
	ListProjects(ctx Context, userID, workspaceID string) ([]Project, error)
	CreateProject(ctx Context, userID, workspaceID string, input ProjectInput) (Project, error)
	UpdateProject(ctx Context, userID, workspaceID, projectID string, input ProjectInput) (Project, error)
	DeleteProject(ctx Context, userID, workspaceID, projectID string) error
	GetProjectRunbook(ctx Context, userID, workspaceID, projectID string) (ProjectRunbook, error)
	UpdateProjectRunbook(ctx Context, userID, workspaceID, projectID string, input ProjectRunbookInput) (ProjectRunbook, error)
	ListProjectTestCases(ctx Context, userID, workspaceID, projectID string, options TestCaseListOptions) (TestCaseListResult, error)
	CreateProjectTestCase(ctx Context, userID, workspaceID, projectID string, input TestCaseInput) (TestCase, error)
	ImportProjectTestCases(ctx Context, userID, workspaceID, projectID string, input ImportTestCasesInput) (ImportTestCasesResult, error)
	EnsureActiveCodexWorker(ctx Context, userID, workspaceID, runtimeMode string) (string, error)
	GetProjectTestCase(ctx Context, userID, workspaceID, projectID, caseID string) (TestCase, error)
	UpdateProjectTestCase(ctx Context, userID, workspaceID, projectID, caseID string, input TestCaseInput) (TestCase, error)
	DeleteProjectTestCase(ctx Context, userID, workspaceID, projectID, caseID string) (TestCase, error)
	DeleteProjectTestCases(ctx Context, userID, workspaceID, projectID string, input DeleteProjectTestCasesInput) ([]TestCase, error)
	ListProjectTestCaseRevisions(ctx Context, userID, workspaceID, projectID, caseID string) ([]TestCaseRevision, error)
	ListProjectTestCaseProposals(ctx Context, userID, workspaceID, projectID string, options TestCaseProposalListOptions) ([]TestCaseProposal, error)
	ApplyProjectTestCaseProposal(ctx Context, userID, workspaceID, projectID, proposalID string, input ReviewTestCaseProposalInput) (ApplyTestCaseProposalResult, error)
	RejectProjectTestCaseProposal(ctx Context, userID, workspaceID, projectID, proposalID string, input ReviewTestCaseProposalInput) (TestCaseProposal, error)
	ListWorkspaceTestPlans(ctx Context, userID, workspaceID string, options TestPlanListOptions) ([]TestPlan, error)
	CreateWorkspaceTestPlan(ctx Context, userID, workspaceID string, input TestPlanInput) (TestPlanDetail, error)
	GetWorkspaceTestPlan(ctx Context, userID, workspaceID, planID string) (TestPlanDetail, error)
	UpdateWorkspaceTestPlan(ctx Context, userID, workspaceID, planID string, input TestPlanInput) (TestPlanDetail, error)
	ListWorkspaceTestRuns(ctx Context, userID, workspaceID string, options TestRunListOptions) ([]TestRun, error)
	StartWorkspaceTestRun(ctx Context, user User, workspaceID, planID string, input CreateTestRunInput) (TestRunDetail, error)
	StartAdHocWorkspaceTestRun(ctx Context, user User, workspaceID string, input CreateAdHocTestRunInput) (TestRunDetail, error)
	GetWorkspaceTestRun(ctx Context, userID, workspaceID, runID string) (TestRunDetail, error)
	ListWorkspaceTestRunArtifacts(ctx Context, userID, workspaceID, runID string) ([]TestArtifact, error)
	RetryWorkspaceTestRun(ctx Context, user User, workspaceID, runID string, input RetryTestRunInput) (TestRunDetail, error)
	CancelWorkspaceTestRun(ctx Context, user User, workspaceID, runID string, input CancelRuntimeTaskInput) (TestRunDetail, error)
	AcceptWorkspaceTestRun(ctx Context, userID, workspaceID, runID string, input ReviewTestRunInput) (TestRun, error)
	BlockWorkspaceTestRun(ctx Context, userID, workspaceID, runID string, input ReviewTestRunInput) (TestRun, error)
	ListProjectTestPlans(ctx Context, userID, workspaceID, projectID string, options TestPlanListOptions) ([]TestPlan, error)
	CreateProjectTestPlan(ctx Context, userID, workspaceID, projectID string, input TestPlanInput) (TestPlanDetail, error)
	GetProjectTestPlan(ctx Context, userID, workspaceID, projectID, planID string) (TestPlanDetail, error)
	UpdateProjectTestPlan(ctx Context, userID, workspaceID, projectID, planID string, input TestPlanInput) (TestPlanDetail, error)
	ListProjectTestRuns(ctx Context, userID, workspaceID, projectID string, options TestRunListOptions) ([]TestRun, error)
	StartProjectTestRun(ctx Context, user User, workspaceID, projectID, planID string, input CreateTestRunInput) (TestRunDetail, error)
	StartAdHocProjectTestRun(ctx Context, user User, workspaceID, projectID string, input CreateAdHocTestRunInput) (TestRunDetail, error)
	GetProjectTestRun(ctx Context, userID, workspaceID, projectID, runID string) (TestRunDetail, error)
	ListProjectTestCaseRunItems(ctx Context, userID, workspaceID, projectID, caseID string) ([]TestCaseRunItem, error)
	ListProjectTestRunArtifacts(ctx Context, userID, workspaceID, projectID, runID string) ([]TestArtifact, error)
	GetTestArtifact(ctx Context, userID, artifactID string) (TestArtifact, error)
	RetryProjectTestRun(ctx Context, user User, workspaceID, projectID, runID string, input RetryTestRunInput) (TestRunDetail, error)
	CancelProjectTestRun(ctx Context, user User, workspaceID, projectID, runID string, input CancelRuntimeTaskInput) (TestRunDetail, error)
	AcceptProjectTestRun(ctx Context, userID, workspaceID, projectID, runID string, input ReviewTestRunInput) (TestRun, error)
	BlockProjectTestRun(ctx Context, userID, workspaceID, projectID, runID string, input ReviewTestRunInput) (TestRun, error)
	ListIssueLabelDefinitions(ctx Context, userID, workspaceID string) ([]IssueLabelDefinition, error)
	GetWorkspaceSettings(ctx Context, userID, workspaceID string) (WorkspaceSettings, error)
	UpdateWorkspaceSettings(ctx Context, userID, workspaceID string, input WorkspaceSettingsInput) (WorkspaceSettings, error)
	GetWorkspaceGitHubAppInstallation(ctx Context, userID, workspaceID string) (WorkspaceGitHubAppInstallation, error)
	UpsertWorkspaceGitHubAppInstallation(ctx Context, workspaceID string, input WorkspaceGitHubAppInstallation) (WorkspaceGitHubAppInstallation, error)
	ListSkills(ctx Context, userID, workspaceID string) ([]SkillCatalogItem, error)
	GetSkill(ctx Context, userID, workspaceID, skillID string) (SkillDetail, error)
	CreateSkill(ctx Context, userID, workspaceID string, input SkillInput) (SkillDetail, error)
	UpdateSkill(ctx Context, userID, workspaceID, skillID string, input SkillInput) (SkillDetail, error)
	DeleteSkill(ctx Context, userID, workspaceID, skillID string) error
	DuplicateSkill(ctx Context, userID, workspaceID, skillID string, input DuplicateSkillInput) (SkillDetail, error)
	ListEnvironments(ctx Context, userID, workspaceID string) ([]Environment, error)
	CreateEnvironment(ctx Context, userID, workspaceID string, input EnvironmentInput) (Environment, error)
	UpdateEnvironment(ctx Context, userID, workspaceID, environmentID string, input EnvironmentInput) (Environment, error)
	CheckEnvironment(ctx Context, userID, workspaceID, environmentID string, input EnvironmentCheckInput) (Environment, error)
	DeleteEnvironment(ctx Context, userID, workspaceID, environmentID string) error
	ListClusters(ctx Context, userID, workspaceID string) ([]Cluster, error)
	CreateCluster(ctx Context, userID, workspaceID string, input ClusterInput) (Cluster, error)
	UpdateCluster(ctx Context, userID, workspaceID, clusterID string, input ClusterInput) (Cluster, error)
	CheckCluster(ctx Context, userID, workspaceID, clusterID string) (Cluster, error)
	DeleteCluster(ctx Context, userID, workspaceID, clusterID string) error
	DiscoverDefaultKubeconfigs(ctx Context, userID, workspaceID string) (KubeconfigDiscoveryResult, error)
	ImportKubeconfigs(ctx Context, userID, workspaceID string, paths []string) (KubeconfigImportResult, error)
	ListIssues(ctx Context, userID, workspaceID string, options IssueListOptions) ([]IssueListItem, error)
	CreateIssue(ctx Context, user User, workspaceID string, input CreateIssueInput) (string, error)
	GetIssue(ctx Context, userID, workspaceID, issueID string) (IssueDetail, error)
	CreateAgentSession(ctx Context, userID, workspaceID, issueID string, input CreateAgentSessionInput) (AgentSession, error)
	GetSession(ctx Context, userID, workspaceID, sessionID string) (SessionDetail, error)
	StartIssueTestDeploy(ctx Context, userID, workspaceID, issueID string, input StartTestDeployInput) (TestEnvironmentSessionResult, error)
	RequestIssueTestEnvironmentCleanup(ctx Context, userID, workspaceID, issueID string, input StartTestDeployInput) (TestEnvironmentSessionResult, error)
	RetainIssueTestEnvironment(ctx Context, userID, workspaceID, issueID string) (IssueTestEnvironment, error)
	GetIssueTestEnvironmentResources(ctx Context, userID, workspaceID, issueID string) (IssueTestEnvironmentResources, error)
	ProbeIssueTestEnvironment(ctx Context, userID, workspaceID, issueID string) (IssueTestEnvironment, error)
	CreateIssuePullRequestHandoff(ctx Context, userID, workspaceID, issueID string, input CreatePullRequestInput) (IssueHandoff, error)
	RefreshIssueHandoff(ctx Context, userID, workspaceID, issueID, handoffID string) (IssueHandoff, error)
	UpdateIssue(ctx Context, userID, workspaceID, issueID string, input UpdateIssueInput) (Issue, error)
	CreateIssueTask(ctx Context, userID, workspaceID, issueID string, input IssueTaskInput) (IssueListItem, error)
	DeleteIssueTask(ctx Context, userID, workspaceID, issueID, taskID string) error
	UpdateIssueLabels(ctx Context, userID, workspaceID, issueID string, input UpdateIssueLabelsInput) ([]IssueLabel, error)
	ApplyIssueTypeClassification(ctx Context, workspaceID, issueID string, labelKey string) error
	MarkIssueTriageFailed(ctx Context, workspaceID, issueID string) error
	AddComment(ctx Context, user User, workspaceID, issueID string, input CreateCommentInput) (string, error)
	UpdateComment(ctx Context, user User, workspaceID, issueID, commentID string, input UpdateCommentInput) (Comment, error)
	CreateIssueAttachment(ctx Context, userID, workspaceID, issueID string, input CreateIssueAttachmentInput) (IssueAttachment, error)
	GetIssueAttachment(ctx Context, userID, attachmentID string) (IssueAttachment, error)
	SetCommentReaction(ctx Context, user User, workspaceID, issueID, commentID, reaction string) error
	DeleteCommentReaction(ctx Context, userID, workspaceID, issueID, commentID, reaction string) error
	CreateRuntimeRegistrationToken(ctx Context, userID, workspaceID string, input CreateRuntimeRegistrationTokenInput) (RuntimeRegistrationTokenResult, error)
	EnsureRuntimeRegistrationToken(ctx Context, userID, workspaceID string, input EnsureRuntimeRegistrationTokenInput) (RuntimeRegistrationToken, error)
	ListRuntimeRegistrationTokens(ctx Context, userID, workspaceID string) ([]RuntimeRegistrationToken, error)
	RevokeRuntimeRegistrationToken(ctx Context, userID, workspaceID, tokenID string) (RuntimeRegistrationToken, error)
	AuthenticateRuntimeRegistrationToken(ctx Context, token string) (RuntimeRegistration, error)
	RegisterRuntimeWorker(ctx Context, registration RuntimeRegistration, input RuntimeWorkerInput) (RuntimeWorker, error)
	UpdateRuntimeWorkerHeartbeat(ctx Context, registration RuntimeRegistration, workerID string, input RuntimeWorkerInput) (RuntimeWorker, error)
	ListRuntimeWorkers(ctx Context, userID, workspaceID string) ([]RuntimeWorker, error)
	CreateRuntimeTask(ctx Context, userID, workspaceID string, input CreateRuntimeTaskInput) (RuntimeTask, error)
	ListRuntimeTasks(ctx Context, userID, workspaceID string) ([]RuntimeTask, error)
	ListRuntimeTasksPage(ctx Context, userID, workspaceID string, options RuntimeTaskListOptions) (RuntimeTaskListResult, error)
	ListRuntimeTaskEvents(ctx Context, userID, workspaceID, taskID string) ([]RuntimeTaskEvent, error)
	ListRuntimeTaskLogs(ctx Context, userID, workspaceID, taskID string) ([]RuntimeTaskLog, error)
	CancelRuntimeTask(ctx Context, userID, workspaceID, taskID string, input CancelRuntimeTaskInput) (RuntimeTask, error)
	ClaimRuntimeTask(ctx Context, registration RuntimeRegistration, workerID string) (*RuntimeTask, error)
	GetRuntimeTaskForWorker(ctx Context, registration RuntimeRegistration, workerID, taskID string) (RuntimeTask, error)
	AppendRuntimeTaskLog(ctx Context, registration RuntimeRegistration, workerID, taskID string, input AppendRuntimeTaskLogInput) (RuntimeTaskLog, error)
	UpdateRuntimeTaskStatus(ctx Context, registration RuntimeRegistration, workerID, taskID string, input UpdateRuntimeTaskStatusInput) (RuntimeTask, error)
}

type Context interface {
	Done() <-chan struct{}
	Err() error
	Value(key any) any
}

func normalizeJSONPayload(payload json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return json.RawMessage(`{}`), nil
	}
	if !json.Valid(trimmed) {
		return nil, errors.New("payload must be valid JSON")
	}
	return append(json.RawMessage(nil), trimmed...), nil
}

func normalizeJSONObjectPayload(payload json.RawMessage) (json.RawMessage, error) {
	normalized, err := normalizeJSONPayload(payload)
	if err != nil {
		return nil, err
	}
	var object map[string]any
	if err := json.Unmarshal(normalized, &object); err != nil {
		return nil, errors.New("payload must be a JSON object")
	}
	return normalized, nil
}
