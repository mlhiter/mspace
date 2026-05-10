package control

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) SaveOAuthState(ctx Context, state OAuthState) error {
	_, err := s.pool.Exec(asContext(ctx), `
		INSERT INTO oauth_states (state, provider, redirect_uri, expires_at)
		VALUES ($1, $2, $3, $4)
	`, state.State, state.Provider, state.RedirectURI, state.ExpiresAt)
	return err
}

func (s *PostgresStore) ConsumeOAuthState(ctx Context, provider, state string) (OAuthState, error) {
	tx, err := s.pool.Begin(asContext(ctx))
	if err != nil {
		return OAuthState{}, err
	}
	defer tx.Rollback(asContext(ctx))

	var record OAuthState
	err = tx.QueryRow(asContext(ctx), `
		DELETE FROM oauth_states
		WHERE state = $1 AND provider = $2
		RETURNING state, provider, redirect_uri, expires_at
	`, state, provider).Scan(&record.State, &record.Provider, &record.RedirectURI, &record.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return OAuthState{}, ErrNotFound
	}
	if err != nil {
		return OAuthState{}, err
	}
	if err := tx.Commit(asContext(ctx)); err != nil {
		return OAuthState{}, err
	}
	if time.Now().After(record.ExpiresAt) {
		return OAuthState{}, ErrExpired
	}
	return record, nil
}

func (s *PostgresStore) SaveOAuthResult(ctx Context, provider, state string, result AuthResult, expiresAt time.Time) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(asContext(ctx), `
		INSERT INTO oauth_results (state, provider, result, expires_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (state) DO UPDATE
		SET provider = EXCLUDED.provider,
				result = EXCLUDED.result,
				expires_at = EXCLUDED.expires_at,
				created_at = now()
	`, state, provider, payload, expiresAt)
	return err
}

func (s *PostgresStore) ConsumeOAuthResult(ctx Context, provider, state string) (AuthResult, bool, error) {
	dbctx := asContext(ctx)
	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return AuthResult{}, false, err
	}
	defer tx.Rollback(dbctx)

	var payload []byte
	var expiresAt time.Time
	err = tx.QueryRow(dbctx, `
		DELETE FROM oauth_results
		WHERE state = $1 AND provider = $2
		RETURNING result, expires_at
	`, state, provider).Scan(&payload, &expiresAt)
	if err == nil {
		if err := tx.Commit(dbctx); err != nil {
			return AuthResult{}, false, err
		}
		if time.Now().After(expiresAt) {
			return AuthResult{}, false, ErrExpired
		}
		var result AuthResult
		if err := json.Unmarshal(payload, &result); err != nil {
			return AuthResult{}, false, err
		}
		return result, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return AuthResult{}, false, err
	}

	var stateExpiresAt time.Time
	err = tx.QueryRow(dbctx, `
		SELECT expires_at
		FROM oauth_states
		WHERE state = $1 AND provider = $2
	`, state, provider).Scan(&stateExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthResult{}, false, ErrNotFound
	}
	if err != nil {
		return AuthResult{}, false, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return AuthResult{}, false, err
	}
	if time.Now().After(stateExpiresAt) {
		return AuthResult{}, false, ErrExpired
	}
	return AuthResult{}, false, nil
}

func (s *PostgresStore) UpsertIdentity(ctx Context, profile IdentityProfile) (User, []Workspace, error) {
	dbctx := asContext(ctx)
	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return User{}, nil, err
	}
	defer tx.Rollback(dbctx)

	user, workspaces, err := findUserByIdentity(dbctx, tx, profile.Provider, profile.ProviderUserID)
	if err == nil {
		if err := updateIdentity(dbctx, tx, user.ID, profile); err != nil {
			return User{}, nil, err
		}
		if err := tx.Commit(dbctx); err != nil {
			return User{}, nil, err
		}
		return user, workspaces, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return User{}, nil, err
	}

	user, err = findOrCreateUserByEmail(dbctx, tx, profile)
	if err != nil {
		return User{}, nil, err
	}
	if err := insertIdentity(dbctx, tx, user.ID, profile); err != nil {
		return User{}, nil, err
	}
	workspaces, err = ensureDefaultWorkspace(dbctx, tx, user, profile.Login)
	if err != nil {
		return User{}, nil, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return User{}, nil, err
	}
	return user, workspaces, nil
}

func (s *PostgresStore) CreateAuthSession(ctx Context, userID string, ttl time.Duration) (string, time.Time, error) {
	token, err := newSessionToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().UTC().Add(ttl)
	_, err = s.pool.Exec(asContext(ctx), `
		INSERT INTO auth_sessions (user_id, token_hash, token_prefix, expires_at)
		VALUES ($1, $2, $3, $4)
	`, userID, tokenHash(token), tokenPrefix(token), expiresAt)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func (s *PostgresStore) GetUserBySessionToken(ctx Context, token string) (User, []Workspace, error) {
	dbctx := asContext(ctx)
	var user User
	var createdAt, updatedAt time.Time
	err := s.pool.QueryRow(dbctx, `
		SELECT u.id::text, u.name, u.email, u.avatar_url, u.created_at, u.updated_at
		FROM auth_sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.revoked = false AND s.expires_at > now()
	`, tokenHash(token)).Scan(&user.ID, &user.Name, &user.Email, &user.AvatarURL, &createdAt, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, nil, ErrNotFound
	}
	if err != nil {
		return User{}, nil, err
	}
	user.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	user.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	_, _ = s.pool.Exec(dbctx, `UPDATE auth_sessions SET last_used_at = now() WHERE token_hash = $1`, tokenHash(token))

	workspaces, err := listWorkspaces(dbctx, s.pool, user.ID)
	if err != nil {
		return User{}, nil, err
	}
	return user, workspaces, nil
}

func (s *PostgresStore) ListInbox(ctx Context, userID, workspaceID string) ([]InboxEntry, error) {
	dbctx := asContext(ctx)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(dbctx, `
		SELECT
			e.id::text,
			e.workspace_id::text,
			e.issue_id,
			COALESCE(e.actor_user_id::text, ''),
			e.kind,
			e.summary,
			e.payload,
			r.state,
			COUNT(*) OVER (PARTITION BY e.issue_id) AS unread_count,
			e.created_at
		FROM issue_event_receipts r
		JOIN issue_events e ON e.id = r.event_id
		WHERE r.workspace_id = $1
			AND r.user_id = $2
			AND r.state = 'unread'
		ORDER BY e.created_at DESC
		LIMIT 100
	`, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []InboxEntry
	for rows.Next() {
		var item InboxEntry
		var payload []byte
		var createdAt time.Time
		if err := rows.Scan(&item.EventID, &item.WorkspaceID, &item.IssueID, &item.ActorUserID, &item.Kind, &item.Summary, &payload, &item.State, &item.UnreadCount, &createdAt); err != nil {
			return nil, err
		}
		item.Payload = json.RawMessage(payload)
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *PostgresStore) CreateIssueEvent(ctx Context, requesterUserID string, input CreateIssueEventInput) (IssueEvent, error) {
	dbctx := asContext(ctx)
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.IssueID = strings.TrimSpace(input.IssueID)
	input.ActorUserID = strings.TrimSpace(input.ActorUserID)
	input.Kind = strings.TrimSpace(input.Kind)
	input.Summary = strings.TrimSpace(input.Summary)
	if input.WorkspaceID == "" || input.IssueID == "" || input.Kind == "" {
		return IssueEvent{}, errors.New("workspaceId, issueId, and kind are required")
	}
	payload, err := normalizeJSONPayload(input.Payload)
	if err != nil {
		return IssueEvent{}, err
	}

	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return IssueEvent{}, err
	}
	defer tx.Rollback(dbctx)

	if err := ensureWorkspaceMember(dbctx, tx, input.WorkspaceID, requesterUserID); err != nil {
		return IssueEvent{}, err
	}
	if input.ActorUserID != "" {
		if err := ensureWorkspaceMember(dbctx, tx, input.WorkspaceID, input.ActorUserID); err != nil {
			return IssueEvent{}, err
		}
	}

	var event IssueEvent
	var eventPayload []byte
	var createdAt time.Time
	err = tx.QueryRow(dbctx, `
		INSERT INTO issue_events (workspace_id, issue_id, actor_user_id, kind, summary, payload)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6)
		RETURNING id::text, workspace_id::text, issue_id, COALESCE(actor_user_id::text, ''), kind, summary, payload, created_at
	`, input.WorkspaceID, input.IssueID, input.ActorUserID, input.Kind, input.Summary, payload).Scan(
		&event.ID,
		&event.WorkspaceID,
		&event.IssueID,
		&event.ActorUserID,
		&event.Kind,
		&event.Summary,
		&eventPayload,
		&createdAt,
	)
	if err != nil {
		return IssueEvent{}, err
	}
	event.Payload = json.RawMessage(eventPayload)
	event.CreatedAt = createdAt.UTC().Format(time.RFC3339)

	recipients, err := resolveIssueEventRecipients(dbctx, tx, input)
	if err != nil {
		return IssueEvent{}, err
	}
	if input.ActorUserID != "" {
		delete(recipients, input.ActorUserID)
	}
	recipientIDs := make([]string, 0, len(recipients))
	for userID := range recipients {
		recipientIDs = append(recipientIDs, userID)
	}
	sort.Strings(recipientIDs)
	for _, userID := range recipientIDs {
		if _, err := tx.Exec(dbctx, `
			INSERT INTO issue_event_receipts (event_id, workspace_id, issue_id, user_id)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (event_id, user_id) DO NOTHING
		`, event.ID, input.WorkspaceID, input.IssueID, userID); err != nil {
			return IssueEvent{}, err
		}
	}

	if err := tx.Commit(dbctx); err != nil {
		return IssueEvent{}, err
	}
	return event, nil
}

func (s *PostgresStore) MarkIssueEventRead(ctx Context, userID, workspaceID, eventID string) error {
	dbctx := asContext(ctx)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return err
	}
	_, err := s.pool.Exec(dbctx, `
		UPDATE issue_event_receipts
		SET state = 'read', read_at = COALESCE(read_at, now()), updated_at = now()
		WHERE workspace_id = $1 AND event_id = $2 AND user_id = $3 AND state = 'unread'
	`, workspaceID, eventID, userID)
	return err
}

func (s *PostgresStore) MarkIssueReadThrough(ctx Context, userID, workspaceID, issueID, throughEventID string) (int, error) {
	dbctx := asContext(ctx)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return 0, err
	}
	issueID = strings.TrimSpace(issueID)
	throughEventID = strings.TrimSpace(throughEventID)
	if issueID == "" {
		return 0, errors.New("issueId is required")
	}

	var count int
	if throughEventID == "" {
		err := s.pool.QueryRow(dbctx, `
			WITH updated AS (
				UPDATE issue_event_receipts
				SET state = 'read', read_at = COALESCE(read_at, now()), updated_at = now()
				WHERE workspace_id = $1 AND issue_id = $2 AND user_id = $3 AND state = 'unread'
				RETURNING 1
			)
			SELECT COUNT(*) FROM updated
		`, workspaceID, issueID, userID).Scan(&count)
		return count, err
	}

	var boundary time.Time
	if err := s.pool.QueryRow(dbctx, `
		SELECT created_at
		FROM issue_events
		WHERE id = $1 AND workspace_id = $2 AND issue_id = $3
	`, throughEventID, workspaceID, issueID).Scan(&boundary); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	err := s.pool.QueryRow(dbctx, `
		WITH updated AS (
			UPDATE issue_event_receipts r
			SET state = 'read', read_at = COALESCE(read_at, now()), updated_at = now()
			FROM issue_events e
			WHERE r.event_id = e.id
				AND r.workspace_id = $1
				AND r.issue_id = $2
				AND r.user_id = $3
				AND r.state = 'unread'
				AND e.created_at <= $4
			RETURNING 1
		)
		SELECT COUNT(*) FROM updated
	`, workspaceID, issueID, userID, boundary).Scan(&count)
	return count, err
}

type queryer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func findUserByIdentity(ctx context.Context, q queryer, provider, providerUserID string) (User, []Workspace, error) {
	var user User
	var createdAt, updatedAt time.Time
	err := q.QueryRow(ctx, `
		SELECT u.id::text, u.name, u.email, u.avatar_url, u.created_at, u.updated_at
		FROM user_identities i
		JOIN users u ON u.id = i.user_id
		WHERE i.provider = $1 AND i.provider_user_id = $2
	`, provider, providerUserID).Scan(&user.ID, &user.Name, &user.Email, &user.AvatarURL, &createdAt, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, nil, ErrNotFound
	}
	if err != nil {
		return User{}, nil, err
	}
	user.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	user.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	workspaces, err := listWorkspaces(ctx, q, user.ID)
	return user, workspaces, err
}

func findOrCreateUserByEmail(ctx context.Context, q queryer, profile IdentityProfile) (User, error) {
	email := strings.ToLower(strings.TrimSpace(profile.Email))
	if email != "" {
		user, err := findUserByEmail(ctx, q, email)
		if err == nil {
			return user, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return User{}, err
		}
	}

	name := strings.TrimSpace(profile.Name)
	if name == "" {
		name = profile.Login
	}
	if name == "" {
		name = "GitHub user"
	}

	var user User
	var createdAt, updatedAt time.Time
	err := q.QueryRow(ctx, `
		INSERT INTO users (name, email, avatar_url)
		VALUES ($1, $2, $3)
		RETURNING id::text, name, email, avatar_url, created_at, updated_at
	`, name, email, profile.AvatarURL).Scan(&user.ID, &user.Name, &user.Email, &user.AvatarURL, &createdAt, &updatedAt)
	if err != nil {
		return User{}, err
	}
	user.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	user.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return user, nil
}

func findUserByEmail(ctx context.Context, q queryer, email string) (User, error) {
	var user User
	var createdAt, updatedAt time.Time
	err := q.QueryRow(ctx, `
		SELECT id::text, name, email, avatar_url, created_at, updated_at
		FROM users
		WHERE lower(email) = lower($1)
	`, email).Scan(&user.ID, &user.Name, &user.Email, &user.AvatarURL, &createdAt, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	user.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	user.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return user, nil
}

func insertIdentity(ctx context.Context, q queryer, userID string, profile IdentityProfile) error {
	raw := profile.RawProfile
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	_, err := q.Exec(ctx, `
		INSERT INTO user_identities (user_id, provider, provider_user_id, login, email, avatar_url, raw_profile)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, userID, profile.Provider, profile.ProviderUserID, profile.Login, profile.Email, profile.AvatarURL, raw)
	return err
}

func updateIdentity(ctx context.Context, q queryer, userID string, profile IdentityProfile) error {
	raw := profile.RawProfile
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	_, err := q.Exec(ctx, `
		UPDATE user_identities
		SET login = $1, email = $2, avatar_url = $3, raw_profile = $4, updated_at = now()
		WHERE user_id = $5 AND provider = $6 AND provider_user_id = $7
	`, profile.Login, profile.Email, profile.AvatarURL, raw, userID, profile.Provider, profile.ProviderUserID)
	return err
}

func ensureDefaultWorkspace(ctx context.Context, q queryer, user User, login string) ([]Workspace, error) {
	workspaces, err := listWorkspaces(ctx, q, user.ID)
	if err != nil {
		return nil, err
	}
	if len(workspaces) > 0 {
		return workspaces, nil
	}

	name := strings.TrimSpace(user.Name)
	if name == "" {
		name = "My"
	}
	slug := defaultWorkspaceSlug(login, user.ID)
	var workspace Workspace
	var createdAt, updatedAt time.Time
	err = q.QueryRow(ctx, `
		WITH created_workspace AS (
			INSERT INTO workspaces (name, slug)
			VALUES ($1, $2)
			RETURNING id, name, slug, created_at, updated_at
		), created_member AS (
			INSERT INTO workspace_members (workspace_id, user_id, role)
			SELECT id, $3, 'owner' FROM created_workspace
		)
		SELECT id::text, name, slug, 'owner', created_at, updated_at FROM created_workspace
	`, name+"'s workspace", slug, user.ID).Scan(&workspace.ID, &workspace.Name, &workspace.Slug, &workspace.Role, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	workspace.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	workspace.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return []Workspace{workspace}, nil
}

func listWorkspaces(ctx context.Context, q queryer, userID string) ([]Workspace, error) {
	rows, err := q.Query(ctx, `
		SELECT w.id::text, w.name, w.slug, wm.role, w.created_at, w.updated_at
		FROM workspace_members wm
		JOIN workspaces w ON w.id = wm.workspace_id
		WHERE wm.user_id = $1
		ORDER BY w.created_at ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workspaces []Workspace
	for rows.Next() {
		var workspace Workspace
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&workspace.ID, &workspace.Name, &workspace.Slug, &workspace.Role, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		workspace.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		workspace.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		workspaces = append(workspaces, workspace)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return workspaces, nil
}

func ensureWorkspaceMember(ctx context.Context, q queryer, workspaceID, userID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	userID = strings.TrimSpace(userID)
	if workspaceID == "" || userID == "" {
		return ErrNotFound
	}
	var exists bool
	err := q.QueryRow(ctx, `
		SELECT true
		FROM workspace_members
		WHERE workspace_id = $1 AND user_id = $2
	`, workspaceID, userID).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func resolveIssueEventRecipients(ctx context.Context, q queryer, input CreateIssueEventInput) (map[string]bool, error) {
	recipients := map[string]bool{}
	watcherRows, err := q.Query(ctx, `
		SELECT user_id::text
		FROM issue_watchers
		WHERE workspace_id = $1 AND issue_id = $2 AND muted = false
	`, input.WorkspaceID, input.IssueID)
	if err != nil {
		return nil, err
	}
	for watcherRows.Next() {
		var userID string
		if err := watcherRows.Scan(&userID); err != nil {
			watcherRows.Close()
			return nil, err
		}
		recipients[userID] = true
	}
	if err := watcherRows.Err(); err != nil {
		watcherRows.Close()
		return nil, err
	}
	watcherRows.Close()

	for _, recipientID := range input.RecipientUserIDs {
		recipientID = strings.TrimSpace(recipientID)
		if recipientID == "" {
			continue
		}
		if err := ensureWorkspaceMember(ctx, q, input.WorkspaceID, recipientID); err != nil {
			return nil, err
		}
		recipients[recipientID] = true
	}

	if len(recipients) > 0 {
		return recipients, nil
	}

	memberRows, err := q.Query(ctx, `
		SELECT user_id::text
		FROM workspace_members
		WHERE workspace_id = $1
	`, input.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer memberRows.Close()
	for memberRows.Next() {
		var userID string
		if err := memberRows.Scan(&userID); err != nil {
			return nil, err
		}
		recipients[userID] = true
	}
	return recipients, memberRows.Err()
}

func asContext(ctx Context) context.Context {
	if typed, ok := ctx.(context.Context); ok {
		return typed
	}
	return context.Background()
}
