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
	Role      string `json:"role"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
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
	ListWorkspaceMembers(ctx Context, userID, workspaceID string) ([]WorkspaceMember, error)
	CreateWorkspaceInvitation(ctx Context, userID, workspaceID string, input CreateWorkspaceInvitationInput) (WorkspaceInvitationResult, error)
	ListWorkspaceInvitations(ctx Context, userID, workspaceID string) ([]WorkspaceInvitation, error)
	RevokeWorkspaceInvitation(ctx Context, userID, workspaceID, invitationID string) (WorkspaceInvitation, error)
	AcceptWorkspaceInvitation(ctx Context, userID string, input AcceptWorkspaceInvitationInput) (AcceptWorkspaceInvitationResult, error)
	ListInbox(ctx Context, userID, workspaceID string) ([]InboxEntry, error)
	CreateIssueEvent(ctx Context, requesterUserID string, input CreateIssueEventInput) (IssueEvent, error)
	MarkIssueEventRead(ctx Context, userID, workspaceID, eventID string) error
	MarkIssueReadThrough(ctx Context, userID, workspaceID, issueID, throughEventID string) (int, error)
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
