package control

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
	ErrExpired  = errors.New("expired")
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
	ListInbox(ctx Context, userID, workspaceID string) ([]InboxEntry, error)
	CreateIssueEvent(ctx Context, requesterUserID string, input CreateIssueEventInput) (IssueEvent, error)
	MarkIssueEventRead(ctx Context, userID, workspaceID, eventID string) error
	MarkIssueReadThrough(ctx Context, userID, workspaceID, issueID, throughEventID string) (int, error)
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
