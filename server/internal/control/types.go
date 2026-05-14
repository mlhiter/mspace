package control

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrExpired   = errors.New("expired")
	ErrForbidden = errors.New("forbidden")
)

type Config struct {
	Addr               string
	DatabaseURL        string
	GitHubClientID     string
	GitHubClientSecret string
	GitHubRedirectURI  string
	SessionTTL         time.Duration
	OAuthStateTTL      time.Duration
	AllowMemoryStore   bool
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

type Workspace struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Kind      string `json:"kind"`
	Role      string `json:"role"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type CreateWorkspaceInput struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type CreateWorkspaceResult struct {
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
	Name                string `json:"name"`
	SourceType          string `json:"sourceType"`
	RepoPath            string `json:"repoPath"`
	RepoURL             string `json:"repoUrl"`
	DefaultBranch       string `json:"defaultBranch"`
	KubeContext         string `json:"kubeContext"`
	KubeconfigPath      string `json:"kubeconfigPath"`
	Namespace           string `json:"namespace"`
	ImageRegistryPrefix string `json:"imageRegistryPrefix"`
	PreviewDomain       string `json:"previewDomain"`
	IngressClass        string `json:"ingressClass"`
	NodeHost            string `json:"nodeHost"`
	DefaultClusterID    string `json:"defaultClusterId"`
}

type UpdateProjectInput struct {
	ID string `json:"id"`
	ProjectInput
}

type ProjectRunbookInput struct {
	Content string `json:"content"`
	Status  string `json:"status"`
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
	Issue           Issue           `json:"issue"`
	Project         Project         `json:"project"`
	TestEnvironment any             `json:"testEnvironment"`
	ChildIssues     []IssueListItem `json:"childIssues"`
	Labels          []IssueLabel    `json:"labels"`
	Comments        []Comment       `json:"comments"`
	Sessions        []AgentSession  `json:"sessions"`
	Evidence        []any           `json:"evidence"`
	Failures        []any           `json:"failures"`
	ChangeNodes     []any           `json:"changeNodes"`
	ReviewEvidence  []any           `json:"reviewEvidence"`
	Handoffs        []any           `json:"handoffs"`
}

type AgentSession struct {
	ID               string `json:"id"`
	IssueID          string `json:"issueId"`
	Provider         string `json:"provider"`
	AgentProfile     string `json:"agentProfile"`
	RuntimeMode      string `json:"runtimeMode"`
	RuntimeTaskID    string `json:"runtimeTaskId"`
	Command          string `json:"command"`
	Status           string `json:"status"`
	Branch           string `json:"branch"`
	Workdir          string `json:"workdir"`
	CodexThreadID    string `json:"codexThreadId"`
	CodexTurnID      string `json:"codexTurnId"`
	AgentStatus      string `json:"agentStatus"`
	ArtifactDir      string `json:"artifactDir"`
	SourceSessionID  string `json:"sourceSessionId"`
	SourceCommitSHA  string `json:"sourceCommitSha"`
	TriggerCommentID string `json:"triggerCommentId"`
	CleanupStatus    string `json:"cleanupStatus"`
	CleanedAt        string `json:"cleanedAt"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

type CreateIssueInput struct {
	ProjectID     string           `json:"projectId"`
	Title         string           `json:"title"`
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

type IssueTaskInput struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	Status    string `json:"status"`
	Completed bool   `json:"completed"`
}

type UpdateIssueInput struct {
	Title  *string `json:"title"`
	Body   *string `json:"body"`
	Status *string `json:"status"`
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

type RuntimeRegistrationTokenResult struct {
	Token             string                   `json:"token"`
	RegistrationToken RuntimeRegistrationToken `json:"registrationToken"`
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
	Token      string      `json:"token"`
	ExpiresAt  string      `json:"expiresAt"`
	User       User        `json:"user"`
	Workspaces []Workspace `json:"workspaces"`
}

type AuthPollResult struct {
	Pending bool `json:"pending"`
	AuthResult
}

type Store interface {
	SaveOAuthState(ctx Context, state OAuthState) error
	ConsumeOAuthState(ctx Context, provider, state string) (OAuthState, error)
	SaveOAuthResult(ctx Context, provider, state string, result AuthResult, expiresAt time.Time) error
	ConsumeOAuthResult(ctx Context, provider, state string) (AuthResult, bool, error)
	UpsertIdentity(ctx Context, profile IdentityProfile) (User, []Workspace, error)
	CreateAuthSession(ctx Context, userID string, ttl time.Duration) (token string, expiresAt time.Time, err error)
	GetUserBySessionToken(ctx Context, token string) (User, []Workspace, error)
	CreateWorkspace(ctx Context, userID string, input CreateWorkspaceInput) (Workspace, []Workspace, error)
	ListWorkspaceMembers(ctx Context, userID, workspaceID string) ([]WorkspaceMember, error)
	CreateWorkspaceInvitation(ctx Context, userID, workspaceID string, input CreateWorkspaceInvitationInput) (WorkspaceInvitationResult, error)
	ListWorkspaceInvitations(ctx Context, userID, workspaceID string) ([]WorkspaceInvitation, error)
	RevokeWorkspaceInvitation(ctx Context, userID, workspaceID, invitationID string) (WorkspaceInvitation, error)
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
	ListIssueLabelDefinitions(ctx Context, userID, workspaceID string) ([]IssueLabelDefinition, error)
	ListIssues(ctx Context, userID, workspaceID string) ([]IssueListItem, error)
	CreateIssue(ctx Context, user User, workspaceID string, input CreateIssueInput) (string, error)
	GetIssue(ctx Context, userID, workspaceID, issueID string) (IssueDetail, error)
	UpdateIssue(ctx Context, userID, workspaceID, issueID string, input UpdateIssueInput) (Issue, error)
	CreateIssueTask(ctx Context, userID, workspaceID, issueID string, input IssueTaskInput) (IssueListItem, error)
	DeleteIssueTask(ctx Context, userID, workspaceID, issueID, taskID string) error
	UpdateIssueLabels(ctx Context, userID, workspaceID, issueID string, input UpdateIssueLabelsInput) ([]IssueLabel, error)
	AddComment(ctx Context, user User, workspaceID, issueID string, input CreateCommentInput) (string, error)
	UpdateComment(ctx Context, user User, workspaceID, issueID, commentID string, input UpdateCommentInput) (Comment, error)
	SetCommentReaction(ctx Context, user User, workspaceID, issueID, commentID, reaction string) error
	DeleteCommentReaction(ctx Context, userID, workspaceID, issueID, commentID, reaction string) error
	CreateRuntimeRegistrationToken(ctx Context, userID, workspaceID string, input CreateRuntimeRegistrationTokenInput) (RuntimeRegistrationTokenResult, error)
	ListRuntimeRegistrationTokens(ctx Context, userID, workspaceID string) ([]RuntimeRegistrationToken, error)
	RevokeRuntimeRegistrationToken(ctx Context, userID, workspaceID, tokenID string) (RuntimeRegistrationToken, error)
	AuthenticateRuntimeRegistrationToken(ctx Context, token string) (RuntimeRegistration, error)
	RegisterRuntimeWorker(ctx Context, registration RuntimeRegistration, input RuntimeWorkerInput) (RuntimeWorker, error)
	UpdateRuntimeWorkerHeartbeat(ctx Context, registration RuntimeRegistration, workerID string, input RuntimeWorkerInput) (RuntimeWorker, error)
	ListRuntimeWorkers(ctx Context, userID, workspaceID string) ([]RuntimeWorker, error)
	CreateRuntimeTask(ctx Context, userID, workspaceID string, input CreateRuntimeTaskInput) (RuntimeTask, error)
	ListRuntimeTasks(ctx Context, userID, workspaceID string) ([]RuntimeTask, error)
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
