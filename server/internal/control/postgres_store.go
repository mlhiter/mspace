package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

func (s *PostgresStore) CreateWorkspace(ctx Context, userID string, input CreateWorkspaceInput) (Workspace, []Workspace, error) {
	dbctx := asContext(ctx)
	userID = strings.TrimSpace(userID)
	normalized, err := normalizeCreateWorkspaceInput(input)
	if err != nil {
		return Workspace{}, nil, err
	}
	if userID == "" {
		return Workspace{}, nil, ErrNotFound
	}
	slugSuffix, err := randomHex(4)
	if err != nil {
		return Workspace{}, nil, err
	}
	slug := workspaceSlug(normalized.Name, slugSuffix)

	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return Workspace{}, nil, err
	}
	defer tx.Rollback(dbctx)

	var workspace Workspace
	var createdAt, updatedAt time.Time
	err = tx.QueryRow(dbctx, `
		WITH created_workspace AS (
			INSERT INTO workspaces (name, slug, kind)
			VALUES ($1, $2, $3)
			RETURNING id, name, slug, kind, created_at, updated_at
		), created_member AS (
			INSERT INTO workspace_members (workspace_id, user_id, role)
			SELECT id, $4, 'owner' FROM created_workspace
		)
		SELECT id::text, name, slug, kind, 'owner', created_at, updated_at FROM created_workspace
	`, normalized.Name, slug, normalized.Kind, userID).Scan(&workspace.ID, &workspace.Name, &workspace.Slug, &workspace.Kind, &workspace.Role, &createdAt, &updatedAt)
	if err != nil {
		return Workspace{}, nil, err
	}
	workspace.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	workspace.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	workspaces, err := listWorkspaces(dbctx, tx, userID)
	if err != nil {
		return Workspace{}, nil, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return Workspace{}, nil, err
	}
	return workspace, workspaces, nil
}

func (s *PostgresStore) ListWorkspaceMembers(ctx Context, userID, workspaceID string) ([]WorkspaceMember, error) {
	dbctx := asContext(ctx)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(dbctx, `
		SELECT
			wm.id::text,
			wm.workspace_id::text,
			wm.user_id::text,
			wm.role,
			u.name,
			u.email,
			u.avatar_url,
			COALESCE(identity.login, ''),
			wm.created_at,
			wm.updated_at
		FROM workspace_members wm
		JOIN users u ON u.id = wm.user_id
		LEFT JOIN LATERAL (
			SELECT login
			FROM user_identities
			WHERE user_id = u.id AND login <> ''
			ORDER BY updated_at DESC, created_at DESC
			LIMIT 1
		) identity ON true
		WHERE wm.workspace_id = $1
		ORDER BY
			CASE wm.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END,
			lower(u.name),
			wm.created_at ASC
	`, strings.TrimSpace(workspaceID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := []WorkspaceMember{}
	for rows.Next() {
		member, err := scanWorkspaceMember(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (s *PostgresStore) CreateWorkspaceInvitation(ctx Context, userID, workspaceID string, input CreateWorkspaceInvitationInput) (WorkspaceInvitationResult, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	userID = strings.TrimSpace(userID)
	normalized, err := normalizeCreateWorkspaceInvitationInput(input)
	if err != nil {
		return WorkspaceInvitationResult{}, err
	}
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, userID, "owner", "admin"); err != nil {
		return WorkspaceInvitationResult{}, err
	}
	if err := ensureTeamWorkspace(dbctx, s.pool, workspaceID); err != nil {
		return WorkspaceInvitationResult{}, err
	}
	token, err := newWorkspaceInvitationToken()
	if err != nil {
		return WorkspaceInvitationResult{}, err
	}
	expiresAt := time.Now().UTC().Add(time.Duration(normalized.ExpiresInHours) * time.Hour)
	row := s.pool.QueryRow(dbctx, `
		INSERT INTO workspace_invitations (workspace_id, email, role, token_hash, token_prefix, invited_by_user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING
			id::text,
			workspace_id::text,
			(SELECT name FROM workspaces w WHERE w.id = workspace_invitations.workspace_id),
			email,
			role,
			token_prefix,
			COALESCE(invited_by_user_id::text, ''),
			COALESCE(accepted_by_user_id::text, ''),
			accepted_at,
			expires_at,
			revoked,
			created_at,
			updated_at
	`, workspaceID, normalized.Email, normalized.Role, tokenHash(token), tokenPrefix(token), userID, expiresAt)
	invitation, err := scanWorkspaceInvitation(row)
	if err != nil {
		return WorkspaceInvitationResult{}, err
	}
	return WorkspaceInvitationResult{Token: token, Invitation: invitation}, nil
}

func (s *PostgresStore) ListWorkspaceInvitations(ctx Context, userID, workspaceID string) ([]WorkspaceInvitation, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, userID, "owner", "admin"); err != nil {
		return nil, err
	}
	if err := ensureTeamWorkspace(dbctx, s.pool, workspaceID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(dbctx, `
		SELECT
			wi.id::text,
			wi.workspace_id::text,
			w.name,
			wi.email,
			wi.role,
			wi.token_prefix,
			COALESCE(wi.invited_by_user_id::text, ''),
			COALESCE(wi.accepted_by_user_id::text, ''),
			wi.accepted_at,
			wi.expires_at,
			wi.revoked,
			wi.created_at,
			wi.updated_at
		FROM workspace_invitations wi
		JOIN workspaces w ON w.id = wi.workspace_id
		WHERE wi.workspace_id = $1
		ORDER BY wi.created_at DESC
		LIMIT 100
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	invitations := []WorkspaceInvitation{}
	for rows.Next() {
		invitation, err := scanWorkspaceInvitation(rows)
		if err != nil {
			return nil, err
		}
		invitations = append(invitations, invitation)
	}
	return invitations, rows.Err()
}

func (s *PostgresStore) RevokeWorkspaceInvitation(ctx Context, userID, workspaceID, invitationID string) (WorkspaceInvitation, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	invitationID = strings.TrimSpace(invitationID)
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, userID, "owner", "admin"); err != nil {
		return WorkspaceInvitation{}, err
	}
	if err := ensureTeamWorkspace(dbctx, s.pool, workspaceID); err != nil {
		return WorkspaceInvitation{}, err
	}
	row := s.pool.QueryRow(dbctx, `
		UPDATE workspace_invitations wi
		SET revoked = true, updated_at = now()
		FROM workspaces w
		WHERE wi.workspace_id = w.id
			AND wi.workspace_id = $1
			AND wi.id = $2
		RETURNING
			wi.id::text,
			wi.workspace_id::text,
			w.name,
			wi.email,
			wi.role,
			wi.token_prefix,
			COALESCE(wi.invited_by_user_id::text, ''),
			COALESCE(wi.accepted_by_user_id::text, ''),
			wi.accepted_at,
			wi.expires_at,
			wi.revoked,
			wi.created_at,
			wi.updated_at
	`, workspaceID, invitationID)
	invitation, err := scanWorkspaceInvitation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkspaceInvitation{}, ErrNotFound
	}
	return invitation, err
}

func (s *PostgresStore) AcceptWorkspaceInvitation(ctx Context, userID string, input AcceptWorkspaceInvitationInput) (AcceptWorkspaceInvitationResult, error) {
	dbctx := asContext(ctx)
	userID = strings.TrimSpace(userID)
	token := strings.TrimSpace(input.Token)
	if userID == "" || token == "" || !strings.HasPrefix(token, "msi_") {
		return AcceptWorkspaceInvitationResult{}, ErrNotFound
	}

	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return AcceptWorkspaceInvitationResult{}, err
	}
	defer tx.Rollback(dbctx)

	row := tx.QueryRow(dbctx, `
		SELECT
			wi.id::text,
			wi.workspace_id::text,
			w.name,
			wi.email,
			wi.role,
			wi.token_prefix,
			COALESCE(wi.invited_by_user_id::text, ''),
			COALESCE(wi.accepted_by_user_id::text, ''),
			wi.accepted_at,
			wi.expires_at,
			wi.revoked,
			wi.created_at,
			wi.updated_at
		FROM workspace_invitations wi
		JOIN workspaces w ON w.id = wi.workspace_id
		WHERE wi.token_hash = $1
		FOR UPDATE OF wi
	`, tokenHash(token))
	invitation, err := scanWorkspaceInvitation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return AcceptWorkspaceInvitationResult{}, ErrNotFound
	}
	if err != nil {
		return AcceptWorkspaceInvitationResult{}, err
	}
	if invitation.Revoked || invitation.AcceptedAt != "" {
		return AcceptWorkspaceInvitationResult{}, ErrNotFound
	}
	expiresAt, err := time.Parse(time.RFC3339, invitation.ExpiresAt)
	if err != nil || time.Now().After(expiresAt) {
		return AcceptWorkspaceInvitationResult{}, ErrExpired
	}

	var role string
	if err := tx.QueryRow(dbctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (workspace_id, user_id) DO UPDATE
		SET role = CASE
			WHEN workspace_members.role = 'owner' THEN 'owner'
			WHEN workspace_members.role = 'admin' AND EXCLUDED.role = 'member' THEN 'admin'
			ELSE EXCLUDED.role
		END,
		updated_at = now()
		RETURNING role
	`, invitation.WorkspaceID, userID, invitation.Role).Scan(&role); err != nil {
		return AcceptWorkspaceInvitationResult{}, err
	}

	row = tx.QueryRow(dbctx, `
		UPDATE workspace_invitations wi
		SET accepted_by_user_id = $2, accepted_at = now(), updated_at = now()
		FROM workspaces w
		WHERE wi.workspace_id = w.id AND wi.id = $1
		RETURNING
			wi.id::text,
			wi.workspace_id::text,
			w.name,
			wi.email,
			wi.role,
			wi.token_prefix,
			COALESCE(wi.invited_by_user_id::text, ''),
			COALESCE(wi.accepted_by_user_id::text, ''),
			wi.accepted_at,
			wi.expires_at,
			wi.revoked,
			wi.created_at,
			wi.updated_at
	`, invitation.ID, userID)
	invitation, err = scanWorkspaceInvitation(row)
	if err != nil {
		return AcceptWorkspaceInvitationResult{}, err
	}
	workspaces, err := listWorkspaces(dbctx, tx, userID)
	if err != nil {
		return AcceptWorkspaceInvitationResult{}, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return AcceptWorkspaceInvitationResult{}, err
	}
	workspace := Workspace{ID: invitation.WorkspaceID, Name: invitation.WorkspaceName, Role: role}
	for _, item := range workspaces {
		if item.ID == invitation.WorkspaceID {
			workspace = item
			break
		}
	}
	return AcceptWorkspaceInvitationResult{Workspace: workspace, Invitation: invitation, Workspaces: workspaces}, nil
}

func (s *PostgresStore) ListInbox(ctx Context, userID, workspaceID string) ([]InboxEntry, error) {
	dbctx := asContext(ctx)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(dbctx, `
			WITH unread AS (
				SELECT
					e.id,
					e.workspace_id,
					e.issue_id,
					e.actor_user_id,
					e.kind,
					e.summary,
					e.payload,
					r.state,
					e.created_at,
					COUNT(*) OVER (PARTITION BY e.issue_id) AS unread_count,
					ROW_NUMBER() OVER (PARTITION BY e.issue_id ORDER BY e.created_at DESC, e.id DESC) AS issue_rank
				FROM issue_event_receipts r
				JOIN issue_events e ON e.id = r.event_id
				WHERE r.workspace_id = $1
					AND r.user_id = $2
					AND r.state = 'unread'
			)
			SELECT
				id::text,
				workspace_id::text,
				issue_id,
				COALESCE(actor_user_id::text, ''),
				kind,
				summary,
				payload,
				state,
				unread_count,
				created_at
			FROM unread
			WHERE issue_rank = 1
			ORDER BY created_at DESC
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

func (s *PostgresStore) CreateRuntimeRegistrationToken(ctx Context, userID, workspaceID string, input CreateRuntimeRegistrationTokenInput) (RuntimeRegistrationTokenResult, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	userID = strings.TrimSpace(userID)
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "Runtime worker token"
	}
	expiresInHours := input.ExpiresInHours
	if expiresInHours <= 0 {
		expiresInHours = 24
	}
	if expiresInHours > 24*90 {
		return RuntimeRegistrationTokenResult{}, errors.New("expiresInHours must be 2160 or less")
	}
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, userID, "owner", "admin"); err != nil {
		return RuntimeRegistrationTokenResult{}, err
	}

	token, err := newRuntimeRegistrationToken()
	if err != nil {
		return RuntimeRegistrationTokenResult{}, err
	}
	expiresAt := time.Now().UTC().Add(time.Duration(expiresInHours) * time.Hour)
	row := s.pool.QueryRow(dbctx, `
		INSERT INTO runtime_registration_tokens (workspace_id, name, token_hash, token_prefix, created_by_user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text, workspace_id::text, name, token_prefix, expires_at, last_used_at, revoked, created_at, updated_at
	`, workspaceID, name, tokenHash(token), tokenPrefix(token), userID, expiresAt)
	record, err := scanRuntimeRegistrationToken(row)
	if err != nil {
		return RuntimeRegistrationTokenResult{}, err
	}
	return RuntimeRegistrationTokenResult{Token: token, RegistrationToken: record}, nil
}

func (s *PostgresStore) ListRuntimeRegistrationTokens(ctx Context, userID, workspaceID string) ([]RuntimeRegistrationToken, error) {
	dbctx := asContext(ctx)
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, userID, "owner", "admin"); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(dbctx, `
		SELECT id::text, workspace_id::text, name, token_prefix, expires_at, last_used_at, revoked, created_at, updated_at
		FROM runtime_registration_tokens
		WHERE workspace_id = $1
		ORDER BY created_at DESC
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tokens := []RuntimeRegistrationToken{}
	for rows.Next() {
		record, err := scanRuntimeRegistrationToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tokens, nil
}

func (s *PostgresStore) RevokeRuntimeRegistrationToken(ctx Context, userID, workspaceID, tokenID string) (RuntimeRegistrationToken, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	tokenID = strings.TrimSpace(tokenID)
	if tokenID == "" {
		return RuntimeRegistrationToken{}, ErrNotFound
	}
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, userID, "owner", "admin"); err != nil {
		return RuntimeRegistrationToken{}, err
	}
	row := s.pool.QueryRow(dbctx, `
		UPDATE runtime_registration_tokens
		SET revoked = true, updated_at = now()
		WHERE id = $1 AND workspace_id = $2
		RETURNING id::text, workspace_id::text, name, token_prefix, expires_at, last_used_at, revoked, created_at, updated_at
	`, tokenID, workspaceID)
	record, err := scanRuntimeRegistrationToken(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return RuntimeRegistrationToken{}, ErrNotFound
	}
	if err != nil {
		return RuntimeRegistrationToken{}, err
	}
	return record, nil
}

func (s *PostgresStore) AuthenticateRuntimeRegistrationToken(ctx Context, token string) (RuntimeRegistration, error) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, "msw_") {
		return RuntimeRegistration{}, ErrNotFound
	}
	var registration RuntimeRegistration
	err := s.pool.QueryRow(asContext(ctx), `
		SELECT rt.id::text, rt.workspace_id::text
		FROM runtime_registration_tokens rt
		WHERE rt.token_hash = $1
			AND rt.revoked = false
			AND rt.expires_at > now()
	`, tokenHash(token)).Scan(&registration.TokenID, &registration.WorkspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return RuntimeRegistration{}, ErrNotFound
	}
	return registration, err
}

func (s *PostgresStore) RegisterRuntimeWorker(ctx Context, registration RuntimeRegistration, input RuntimeWorkerInput) (RuntimeWorker, error) {
	dbctx := asContext(ctx)
	normalized, err := normalizeRuntimeWorkerInput(input)
	if err != nil {
		return RuntimeWorker{}, err
	}
	if registration.TokenID == "" || registration.WorkspaceID == "" {
		return RuntimeWorker{}, ErrNotFound
	}

	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return RuntimeWorker{}, err
	}
	defer tx.Rollback(dbctx)

	if _, err := tx.Exec(dbctx, `
		UPDATE runtime_registration_tokens
		SET last_used_at = now(), updated_at = now()
		WHERE id = $1 AND workspace_id = $2 AND revoked = false AND expires_at > now()
	`, registration.TokenID, registration.WorkspaceID); err != nil {
		return RuntimeWorker{}, err
	}
	row := tx.QueryRow(dbctx, `
		INSERT INTO runtime_workers (workspace_id, registration_token_id, name, mode, status, version, current_load, capabilities, labels, last_seen_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now(), now())
		ON CONFLICT (workspace_id, name) DO UPDATE SET
			registration_token_id = EXCLUDED.registration_token_id,
			mode = EXCLUDED.mode,
			status = EXCLUDED.status,
			version = EXCLUDED.version,
			current_load = EXCLUDED.current_load,
			capabilities = EXCLUDED.capabilities,
			labels = EXCLUDED.labels,
			last_seen_at = now(),
			updated_at = now()
		RETURNING id::text, workspace_id::text, name, mode, status, version, current_load, capabilities, labels, last_seen_at, created_at, updated_at
	`, registration.WorkspaceID, registration.TokenID, normalized.Name, normalized.Mode, normalized.Status, normalized.Version, normalized.CurrentLoad, normalized.Capabilities, normalized.Labels)
	worker, err := scanRuntimeWorker(row)
	if err != nil {
		return RuntimeWorker{}, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return RuntimeWorker{}, err
	}
	return worker, nil
}

func (s *PostgresStore) UpdateRuntimeWorkerHeartbeat(ctx Context, registration RuntimeRegistration, workerID string, input RuntimeWorkerInput) (RuntimeWorker, error) {
	dbctx := asContext(ctx)
	workerID = strings.TrimSpace(workerID)
	if workerID == "" || registration.TokenID == "" || registration.WorkspaceID == "" {
		return RuntimeWorker{}, ErrNotFound
	}
	normalized, err := normalizeRuntimeHeartbeatInput(input)
	if err != nil {
		return RuntimeWorker{}, err
	}

	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return RuntimeWorker{}, err
	}
	defer tx.Rollback(dbctx)

	if _, err := tx.Exec(dbctx, `
		UPDATE runtime_registration_tokens
		SET last_used_at = now(), updated_at = now()
		WHERE id = $1 AND workspace_id = $2 AND revoked = false AND expires_at > now()
	`, registration.TokenID, registration.WorkspaceID); err != nil {
		return RuntimeWorker{}, err
	}
	row := tx.QueryRow(dbctx, `
		UPDATE runtime_workers
		SET
			status = $1,
			version = CASE WHEN $2 <> '' THEN $2 ELSE version END,
			current_load = $3,
			capabilities = CASE WHEN $4::jsonb <> '{}'::jsonb THEN $4::jsonb ELSE capabilities END,
			labels = CASE WHEN $5::jsonb <> '{}'::jsonb THEN $5::jsonb ELSE labels END,
			last_seen_at = now(),
			updated_at = now()
		WHERE id = $6 AND workspace_id = $7
		RETURNING id::text, workspace_id::text, name, mode, status, version, current_load, capabilities, labels, last_seen_at, created_at, updated_at
	`, normalized.Status, normalized.Version, normalized.CurrentLoad, normalized.Capabilities, normalized.Labels, workerID, registration.WorkspaceID)
	worker, err := scanRuntimeWorker(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return RuntimeWorker{}, ErrNotFound
	}
	if err != nil {
		return RuntimeWorker{}, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return RuntimeWorker{}, err
	}
	return worker, nil
}

func (s *PostgresStore) ListRuntimeWorkers(ctx Context, userID, workspaceID string) ([]RuntimeWorker, error) {
	dbctx := asContext(ctx)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(dbctx, `
		SELECT id::text, workspace_id::text, name, mode, status, version, current_load, capabilities, labels, last_seen_at, created_at, updated_at
		FROM runtime_workers
		WHERE workspace_id = $1
		ORDER BY last_seen_at DESC, created_at DESC
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	workers := []RuntimeWorker{}
	for rows.Next() {
		worker, err := scanRuntimeWorker(rows)
		if err != nil {
			return nil, err
		}
		workers = append(workers, worker)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return workers, nil
}

func (s *PostgresStore) CreateRuntimeTask(ctx Context, userID, workspaceID string, input CreateRuntimeTaskInput) (RuntimeTask, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	userID = strings.TrimSpace(userID)
	normalized, err := normalizeCreateRuntimeTaskInput(input)
	if err != nil {
		return RuntimeTask{}, err
	}

	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return RuntimeTask{}, err
	}
	defer tx.Rollback(dbctx)

	if err := ensureWorkspaceMember(dbctx, tx, workspaceID, userID); err != nil {
		return RuntimeTask{}, err
	}
	row := tx.QueryRow(dbctx, `
		INSERT INTO runtime_tasks (
			workspace_id,
			issue_id,
			session_id,
			project_id,
			kind,
			priority,
			runtime_mode,
			required_capabilities,
			payload,
			created_by_user_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING
			id::text,
			workspace_id::text,
			issue_id,
			session_id,
			project_id,
			kind,
			status,
			priority,
			runtime_mode,
			required_capabilities,
			payload,
			result,
			COALESCE(claimed_by_worker_id::text, ''),
			claimed_at,
			started_at,
			finished_at,
			error,
			created_at,
			updated_at
	`, workspaceID, normalized.IssueID, normalized.SessionID, normalized.ProjectID, normalized.Kind, normalized.Priority, normalized.RuntimeMode, normalized.RequiredCapabilities, normalized.Payload, userID)
	task, err := scanRuntimeTask(row)
	if err != nil {
		return RuntimeTask{}, err
	}
	if err := insertRuntimeTaskEvent(dbctx, tx, task.WorkspaceID, task.ID, "", userID, "created", map[string]any{
		"kind":        task.Kind,
		"runtimeMode": task.RuntimeMode,
		"status":      task.Status,
	}); err != nil {
		return RuntimeTask{}, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return RuntimeTask{}, err
	}
	return task, nil
}

func (s *PostgresStore) ListRuntimeTasks(ctx Context, userID, workspaceID string) ([]RuntimeTask, error) {
	dbctx := asContext(ctx)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(dbctx, `
		SELECT
			id::text,
			workspace_id::text,
			issue_id,
			session_id,
			project_id,
			kind,
			status,
			priority,
			runtime_mode,
			required_capabilities,
			payload,
			result,
			COALESCE(claimed_by_worker_id::text, ''),
			claimed_at,
			started_at,
			finished_at,
			error,
			created_at,
			updated_at
		FROM runtime_tasks
		WHERE workspace_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 100
	`, strings.TrimSpace(workspaceID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := []RuntimeTask{}
	for rows.Next() {
		task, err := scanRuntimeTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (s *PostgresStore) ListRuntimeTaskEvents(ctx Context, userID, workspaceID, taskID string) ([]RuntimeTaskEvent, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	taskID = strings.TrimSpace(taskID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, strings.TrimSpace(userID)); err != nil {
		return nil, err
	}
	var exists bool
	if err := s.pool.QueryRow(dbctx, `
		SELECT EXISTS (
			SELECT 1
			FROM runtime_tasks
			WHERE workspace_id = $1 AND id = $2
		)
	`, workspaceID, taskID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(dbctx, `
		SELECT
			id::text,
			workspace_id::text,
			task_id::text,
			COALESCE(worker_id::text, ''),
			COALESCE(actor_user_id::text, ''),
			kind,
			payload,
			created_at
		FROM runtime_task_events
		WHERE workspace_id = $1 AND task_id = $2
		ORDER BY created_at ASC, id ASC
	`, workspaceID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []RuntimeTaskEvent{}
	for rows.Next() {
		event, err := scanRuntimeTaskEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *PostgresStore) ListRuntimeTaskLogs(ctx Context, userID, workspaceID, taskID string) ([]RuntimeTaskLog, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	taskID = strings.TrimSpace(taskID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, strings.TrimSpace(userID)); err != nil {
		return nil, err
	}
	var exists bool
	if err := s.pool.QueryRow(dbctx, `
		SELECT EXISTS (
			SELECT 1
			FROM runtime_tasks
			WHERE workspace_id = $1 AND id = $2
		)
	`, workspaceID, taskID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := s.pool.Query(dbctx, `
		SELECT
			id::text,
			workspace_id::text,
			task_id::text,
			COALESCE(worker_id::text, ''),
			stream,
			message,
			created_at
		FROM runtime_task_logs
		WHERE workspace_id = $1 AND task_id = $2
		ORDER BY created_at ASC, id ASC
	`, workspaceID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := []RuntimeTaskLog{}
	for rows.Next() {
		log, err := scanRuntimeTaskLog(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return logs, nil
}

func (s *PostgresStore) CancelRuntimeTask(ctx Context, userID, workspaceID, taskID string, input CancelRuntimeTaskInput) (RuntimeTask, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	taskID = strings.TrimSpace(taskID)
	userID = strings.TrimSpace(userID)
	if workspaceID == "" || taskID == "" || userID == "" {
		return RuntimeTask{}, ErrNotFound
	}
	reason := normalizeRuntimeTaskCancelReason(input.Reason)

	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return RuntimeTask{}, err
	}
	defer tx.Rollback(dbctx)

	if err := ensureWorkspaceMember(dbctx, tx, workspaceID, userID); err != nil {
		return RuntimeTask{}, err
	}
	row := tx.QueryRow(dbctx, `
		UPDATE runtime_tasks
		SET
			status = 'cancelled',
			started_at = CASE
				WHEN status IN ('claimed', 'running') THEN COALESCE(started_at, now())
				ELSE started_at
			END,
			finished_at = now(),
			error = $1,
			updated_at = now()
		WHERE id = $2
			AND workspace_id = $3
			AND status IN ('queued', 'claimed', 'running')
		RETURNING
			id::text,
			workspace_id::text,
			issue_id,
			session_id,
			project_id,
			kind,
			status,
			priority,
			runtime_mode,
			required_capabilities,
			payload,
			result,
			COALESCE(claimed_by_worker_id::text, ''),
			claimed_at,
			started_at,
			finished_at,
			error,
			created_at,
			updated_at
	`, reason, taskID, workspaceID)
	task, err := scanRuntimeTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return RuntimeTask{}, ErrNotFound
	}
	if err != nil {
		return RuntimeTask{}, err
	}
	if err := insertRuntimeTaskEvent(dbctx, tx, task.WorkspaceID, task.ID, "", userID, "cancel_requested", map[string]any{
		"status": task.Status,
		"reason": reason,
	}); err != nil {
		return RuntimeTask{}, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return RuntimeTask{}, err
	}
	return task, nil
}

func (s *PostgresStore) ClaimRuntimeTask(ctx Context, registration RuntimeRegistration, workerID string) (*RuntimeTask, error) {
	dbctx := asContext(ctx)
	workerID = strings.TrimSpace(workerID)
	if registration.WorkspaceID == "" || workerID == "" {
		return nil, ErrNotFound
	}
	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(dbctx)

	var workerStatus string
	if err := tx.QueryRow(dbctx, `
		SELECT status
		FROM runtime_workers
		WHERE id = $1 AND workspace_id = $2
	`, workerID, registration.WorkspaceID).Scan(&workerStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if workerStatus != "online" {
		return nil, ErrForbidden
	}

	row := tx.QueryRow(dbctx, `
		WITH next_task AS (
			SELECT t.id
			FROM runtime_tasks t
			JOIN runtime_workers w ON w.id = $1 AND w.workspace_id = t.workspace_id
			WHERE t.workspace_id = $2
				AND t.status = 'queued'
				AND t.runtime_mode = w.mode
				AND w.status = 'online'
				AND w.capabilities @> t.required_capabilities
			ORDER BY t.priority DESC, t.created_at ASC, t.id ASC
			FOR UPDATE OF t SKIP LOCKED
			LIMIT 1
		)
		UPDATE runtime_tasks t
		SET
			status = 'claimed',
			claimed_by_worker_id = $1,
			claimed_at = now(),
			updated_at = now()
		FROM next_task
		WHERE t.id = next_task.id
		RETURNING
			t.id::text,
			t.workspace_id::text,
			t.issue_id,
			t.session_id,
			t.project_id,
			t.kind,
			t.status,
			t.priority,
			t.runtime_mode,
			t.required_capabilities,
			t.payload,
			t.result,
			COALESCE(t.claimed_by_worker_id::text, ''),
			t.claimed_at,
			t.started_at,
			t.finished_at,
			t.error,
			t.created_at,
			t.updated_at
	`, workerID, registration.WorkspaceID)
	task, err := scanRuntimeTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(dbctx); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := insertRuntimeTaskEvent(dbctx, tx, task.WorkspaceID, task.ID, workerID, "", "claimed", map[string]any{
		"status": task.Status,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *PostgresStore) GetRuntimeTaskForWorker(ctx Context, registration RuntimeRegistration, workerID, taskID string) (RuntimeTask, error) {
	dbctx := asContext(ctx)
	workerID = strings.TrimSpace(workerID)
	taskID = strings.TrimSpace(taskID)
	if registration.WorkspaceID == "" || workerID == "" || taskID == "" {
		return RuntimeTask{}, ErrNotFound
	}
	row := s.pool.QueryRow(dbctx, `
		SELECT
			t.id::text,
			t.workspace_id::text,
			t.issue_id,
			t.session_id,
			t.project_id,
			t.kind,
			t.status,
			t.priority,
			t.runtime_mode,
			t.required_capabilities,
			t.payload,
			t.result,
			COALESCE(t.claimed_by_worker_id::text, ''),
			t.claimed_at,
			t.started_at,
			t.finished_at,
			t.error,
			t.created_at,
			t.updated_at
		FROM runtime_tasks t
		JOIN runtime_workers w ON w.id = $1 AND w.workspace_id = t.workspace_id
		WHERE t.id = $2
			AND t.workspace_id = $3
			AND t.claimed_by_worker_id = w.id
	`, workerID, taskID, registration.WorkspaceID)
	task, err := scanRuntimeTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return RuntimeTask{}, ErrNotFound
	}
	if err != nil {
		return RuntimeTask{}, err
	}
	return task, nil
}

func (s *PostgresStore) AppendRuntimeTaskLog(ctx Context, registration RuntimeRegistration, workerID, taskID string, input AppendRuntimeTaskLogInput) (RuntimeTaskLog, error) {
	dbctx := asContext(ctx)
	workerID = strings.TrimSpace(workerID)
	taskID = strings.TrimSpace(taskID)
	if registration.WorkspaceID == "" || workerID == "" || taskID == "" {
		return RuntimeTaskLog{}, ErrNotFound
	}
	normalized, err := normalizeAppendRuntimeTaskLogInput(input)
	if err != nil {
		return RuntimeTaskLog{}, err
	}
	row := s.pool.QueryRow(dbctx, `
		INSERT INTO runtime_task_logs (workspace_id, task_id, worker_id, stream, message)
		SELECT t.workspace_id, t.id, w.id, $4, $5
		FROM runtime_tasks t
		JOIN runtime_workers w ON w.id = $3 AND w.workspace_id = t.workspace_id
		WHERE t.id = $1
			AND t.workspace_id = $2
			AND t.claimed_by_worker_id = w.id
			AND t.status IN ('claimed', 'running')
		RETURNING id::text, workspace_id::text, task_id::text, COALESCE(worker_id::text, ''), stream, message, created_at
	`, taskID, registration.WorkspaceID, workerID, normalized.Stream, normalized.Message)
	log, err := scanRuntimeTaskLog(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return RuntimeTaskLog{}, ErrNotFound
	}
	if err != nil {
		return RuntimeTaskLog{}, err
	}
	return log, nil
}

func (s *PostgresStore) UpdateRuntimeTaskStatus(ctx Context, registration RuntimeRegistration, workerID, taskID string, input UpdateRuntimeTaskStatusInput) (RuntimeTask, error) {
	dbctx := asContext(ctx)
	workerID = strings.TrimSpace(workerID)
	taskID = strings.TrimSpace(taskID)
	if registration.WorkspaceID == "" || workerID == "" || taskID == "" {
		return RuntimeTask{}, ErrNotFound
	}
	normalized, resultProvided, err := normalizeUpdateRuntimeTaskStatusInput(input)
	if err != nil {
		return RuntimeTask{}, err
	}
	var resultValue any
	if resultProvided {
		resultValue = normalized.Result
	}

	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return RuntimeTask{}, err
	}
	defer tx.Rollback(dbctx)

	row := tx.QueryRow(dbctx, `
		UPDATE runtime_tasks
		SET
			status = $1,
			started_at = CASE
				WHEN $1 IN ('running', 'completed', 'failed', 'cancelled') THEN COALESCE(started_at, now())
				ELSE started_at
			END,
			finished_at = CASE
				WHEN $1 IN ('completed', 'failed', 'cancelled') THEN now()
				ELSE finished_at
			END,
			result = CASE
				WHEN $2::jsonb IS NULL THEN result
				ELSE $2::jsonb
			END,
			error = $3,
			updated_at = now()
		WHERE id = $4
			AND workspace_id = $5
			AND claimed_by_worker_id = $6
			AND status IN ('claimed', 'running')
		RETURNING
			id::text,
			workspace_id::text,
			issue_id,
			session_id,
			project_id,
			kind,
			status,
			priority,
			runtime_mode,
			required_capabilities,
			payload,
			result,
			COALESCE(claimed_by_worker_id::text, ''),
			claimed_at,
			started_at,
			finished_at,
			error,
			created_at,
			updated_at
	`, normalized.Status, resultValue, normalized.Error, taskID, registration.WorkspaceID, workerID)
	task, err := scanRuntimeTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return RuntimeTask{}, ErrNotFound
	}
	if err != nil {
		return RuntimeTask{}, err
	}
	if err := insertRuntimeTaskEvent(dbctx, tx, task.WorkspaceID, task.ID, workerID, "", "status_changed", map[string]any{
		"status": task.Status,
		"error":  task.Error,
	}); err != nil {
		return RuntimeTask{}, err
	}
	if isFinalRuntimeTaskStatus(task.Status) && task.Kind == "agent_session" {
		if err := s.reconcileAgentSessionRuntimeResult(dbctx, tx, task); err != nil {
			return RuntimeTask{}, err
		}
	}
	if isFinalRuntimeTaskStatus(task.Status) && task.Kind == "issue_type_triage" {
		if err := s.reconcileIssueTypeTriageRuntimeResult(dbctx, tx, task); err != nil {
			return RuntimeTask{}, err
		}
	}
	if err := tx.Commit(dbctx); err != nil {
		return RuntimeTask{}, err
	}
	return task, nil
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
				INSERT INTO workspaces (name, slug, kind)
				VALUES ($1, $2, 'personal')
				RETURNING id, name, slug, kind, created_at, updated_at
			), created_member AS (
				INSERT INTO workspace_members (workspace_id, user_id, role)
				SELECT id, $3, 'owner' FROM created_workspace
			)
			SELECT id::text, name, slug, kind, 'owner', created_at, updated_at FROM created_workspace
		`, name+"'s workspace", slug, user.ID).Scan(&workspace.ID, &workspace.Name, &workspace.Slug, &workspace.Kind, &workspace.Role, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	workspace.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	workspace.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return []Workspace{workspace}, nil
}

func listWorkspaces(ctx context.Context, q queryer, userID string) ([]Workspace, error) {
	rows, err := q.Query(ctx, `
		SELECT w.id::text, w.name, w.slug, w.kind, wm.role, w.created_at, w.updated_at
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
		if err := rows.Scan(&workspace.ID, &workspace.Name, &workspace.Slug, &workspace.Kind, &workspace.Role, &createdAt, &updatedAt); err != nil {
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

func ensureTeamWorkspace(ctx context.Context, q queryer, workspaceID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return ErrNotFound
	}
	var kind string
	err := q.QueryRow(ctx, `
		SELECT kind
		FROM workspaces
		WHERE id = $1
	`, workspaceID).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if kind != "team" {
		return ErrForbidden
	}
	return nil
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

func ensureWorkspaceRole(ctx context.Context, q queryer, workspaceID, userID string, allowed ...string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	userID = strings.TrimSpace(userID)
	if workspaceID == "" || userID == "" {
		return ErrNotFound
	}
	var role string
	err := q.QueryRow(ctx, `
		SELECT role
		FROM workspace_members
		WHERE workspace_id = $1 AND user_id = $2
	`, workspaceID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	for _, item := range allowed {
		if role == item {
			return nil
		}
	}
	return ErrForbidden
}

type scanner interface {
	Scan(...any) error
}

func scanWorkspaceMember(row scanner) (WorkspaceMember, error) {
	var member WorkspaceMember
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&member.ID,
		&member.WorkspaceID,
		&member.UserID,
		&member.Role,
		&member.Name,
		&member.Email,
		&member.AvatarURL,
		&member.IdentityLogin,
		&createdAt,
		&updatedAt,
	); err != nil {
		return WorkspaceMember{}, err
	}
	member.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	member.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return member, nil
}

func scanWorkspaceInvitation(row scanner) (WorkspaceInvitation, error) {
	var invitation WorkspaceInvitation
	var acceptedAt sql.NullTime
	var expiresAt, createdAt, updatedAt time.Time
	if err := row.Scan(
		&invitation.ID,
		&invitation.WorkspaceID,
		&invitation.WorkspaceName,
		&invitation.Email,
		&invitation.Role,
		&invitation.TokenPrefix,
		&invitation.InvitedByUserID,
		&invitation.AcceptedByUserID,
		&acceptedAt,
		&expiresAt,
		&invitation.Revoked,
		&createdAt,
		&updatedAt,
	); err != nil {
		return WorkspaceInvitation{}, err
	}
	if acceptedAt.Valid {
		invitation.AcceptedAt = acceptedAt.Time.UTC().Format(time.RFC3339)
	}
	invitation.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)
	invitation.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	invitation.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return invitation, nil
}

func scanRuntimeRegistrationToken(row scanner) (RuntimeRegistrationToken, error) {
	var record RuntimeRegistrationToken
	var expiresAt, createdAt, updatedAt time.Time
	var lastUsedAt sql.NullTime
	if err := row.Scan(
		&record.ID,
		&record.WorkspaceID,
		&record.Name,
		&record.TokenPrefix,
		&expiresAt,
		&lastUsedAt,
		&record.Revoked,
		&createdAt,
		&updatedAt,
	); err != nil {
		return RuntimeRegistrationToken{}, err
	}
	record.ExpiresAt = expiresAt.UTC().Format(time.RFC3339)
	if lastUsedAt.Valid {
		record.LastUsedAt = lastUsedAt.Time.UTC().Format(time.RFC3339)
	}
	record.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	record.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return record, nil
}

func scanRuntimeWorker(row scanner) (RuntimeWorker, error) {
	var worker RuntimeWorker
	var capabilities, labels []byte
	var lastSeenAt, createdAt, updatedAt time.Time
	if err := row.Scan(
		&worker.ID,
		&worker.WorkspaceID,
		&worker.Name,
		&worker.Mode,
		&worker.Status,
		&worker.Version,
		&worker.CurrentLoad,
		&capabilities,
		&labels,
		&lastSeenAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return RuntimeWorker{}, err
	}
	worker.Capabilities = copyRawMessage(json.RawMessage(capabilities))
	worker.Labels = copyRawMessage(json.RawMessage(labels))
	worker.LastSeenAt = lastSeenAt.UTC().Format(time.RFC3339)
	worker.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	worker.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return worker, nil
}

func scanRuntimeTask(row scanner) (RuntimeTask, error) {
	var task RuntimeTask
	var requiredCapabilities, payload, result []byte
	var claimedAt, startedAt, finishedAt sql.NullTime
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&task.ID,
		&task.WorkspaceID,
		&task.IssueID,
		&task.SessionID,
		&task.ProjectID,
		&task.Kind,
		&task.Status,
		&task.Priority,
		&task.RuntimeMode,
		&requiredCapabilities,
		&payload,
		&result,
		&task.ClaimedByWorkerID,
		&claimedAt,
		&startedAt,
		&finishedAt,
		&task.Error,
		&createdAt,
		&updatedAt,
	); err != nil {
		return RuntimeTask{}, err
	}
	task.RequiredCapabilities = copyRawMessage(json.RawMessage(requiredCapabilities))
	task.Payload = copyRawMessage(json.RawMessage(payload))
	task.Result = copyRawMessage(json.RawMessage(result))
	if claimedAt.Valid {
		task.ClaimedAt = claimedAt.Time.UTC().Format(time.RFC3339)
	}
	if startedAt.Valid {
		task.StartedAt = startedAt.Time.UTC().Format(time.RFC3339)
	}
	if finishedAt.Valid {
		task.FinishedAt = finishedAt.Time.UTC().Format(time.RFC3339)
	}
	task.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	task.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return task, nil
}

func scanRuntimeTaskEvent(row scanner) (RuntimeTaskEvent, error) {
	var event RuntimeTaskEvent
	var payload []byte
	var createdAt time.Time
	if err := row.Scan(
		&event.ID,
		&event.WorkspaceID,
		&event.TaskID,
		&event.WorkerID,
		&event.ActorUserID,
		&event.Kind,
		&payload,
		&createdAt,
	); err != nil {
		return RuntimeTaskEvent{}, err
	}
	event.Payload = copyRawMessage(json.RawMessage(payload))
	event.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	return event, nil
}

func scanRuntimeTaskLog(row scanner) (RuntimeTaskLog, error) {
	var log RuntimeTaskLog
	var createdAt time.Time
	if err := row.Scan(
		&log.ID,
		&log.WorkspaceID,
		&log.TaskID,
		&log.WorkerID,
		&log.Stream,
		&log.Message,
		&createdAt,
	); err != nil {
		return RuntimeTaskLog{}, err
	}
	log.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	return log, nil
}

func insertRuntimeTaskEvent(ctx context.Context, q queryer, workspaceID, taskID, workerID, actorUserID, kind string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		INSERT INTO runtime_task_events (workspace_id, task_id, worker_id, actor_user_id, kind, payload)
		VALUES ($1, $2, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid, $5, $6)
	`, workspaceID, taskID, workerID, actorUserID, kind, body)
	return err
}

func normalizeRuntimeWorkerInput(input RuntimeWorkerInput) (RuntimeWorkerInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return RuntimeWorkerInput{}, errors.New("worker name is required")
	}
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	if input.Mode == "" {
		input.Mode = "team"
	}
	if input.Mode != "personal" && input.Mode != "team" {
		return RuntimeWorkerInput{}, errors.New("worker mode must be personal or team")
	}
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = "online"
	}
	switch input.Status {
	case "online", "draining", "offline":
	default:
		return RuntimeWorkerInput{}, errors.New("worker status must be online, draining, or offline")
	}
	input.Version = strings.TrimSpace(input.Version)
	if input.CurrentLoad < 0 {
		return RuntimeWorkerInput{}, errors.New("currentLoad must be greater than or equal to zero")
	}
	capabilities, err := normalizeJSONPayload(input.Capabilities)
	if err != nil {
		return RuntimeWorkerInput{}, fmt.Errorf("capabilities %w", err)
	}
	labels, err := normalizeJSONPayload(input.Labels)
	if err != nil {
		return RuntimeWorkerInput{}, fmt.Errorf("labels %w", err)
	}
	input.Capabilities = capabilities
	input.Labels = labels
	return input, nil
}

func normalizeCreateRuntimeTaskInput(input CreateRuntimeTaskInput) (CreateRuntimeTaskInput, error) {
	input.IssueID = strings.TrimSpace(input.IssueID)
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	if input.Kind == "" {
		return CreateRuntimeTaskInput{}, errors.New("task kind is required")
	}
	input.RuntimeMode = strings.ToLower(strings.TrimSpace(input.RuntimeMode))
	if input.RuntimeMode == "" {
		input.RuntimeMode = "team"
	}
	if input.RuntimeMode != "personal" && input.RuntimeMode != "team" {
		return CreateRuntimeTaskInput{}, errors.New("runtimeMode must be personal or team")
	}
	if input.Priority < 0 {
		return CreateRuntimeTaskInput{}, errors.New("priority must be greater than or equal to zero")
	}
	requiredCapabilities, err := normalizeJSONObjectPayload(input.RequiredCapabilities)
	if err != nil {
		return CreateRuntimeTaskInput{}, fmt.Errorf("requiredCapabilities %w", err)
	}
	payload, err := normalizeJSONObjectPayload(input.Payload)
	if err != nil {
		return CreateRuntimeTaskInput{}, fmt.Errorf("payload %w", err)
	}
	input.RequiredCapabilities = requiredCapabilities
	input.Payload = payload
	return input, nil
}

func normalizeCreateAgentSessionInput(input CreateAgentSessionInput) CreateAgentSessionInput {
	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	if input.Provider == "" {
		input.Provider = "codex"
	}
	input.AgentProfile = strings.TrimSpace(input.AgentProfile)
	if input.AgentProfile == "" {
		input.AgentProfile = "codex"
	}
	input.RuntimeMode = strings.ToLower(strings.TrimSpace(input.RuntimeMode))
	input.Command = strings.TrimSpace(input.Command)
	input.Branch = strings.TrimSpace(input.Branch)
	input.SourceSessionID = strings.TrimSpace(input.SourceSessionID)
	input.SourceCommitSHA = strings.TrimSpace(input.SourceCommitSHA)
	input.TriggerCommentID = strings.TrimSpace(input.TriggerCommentID)
	return input
}

func normalizeUpdateRuntimeTaskStatusInput(input UpdateRuntimeTaskStatusInput) (UpdateRuntimeTaskStatusInput, bool, error) {
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	switch input.Status {
	case "running", "completed", "failed", "cancelled":
	default:
		return UpdateRuntimeTaskStatusInput{}, false, errors.New("task status must be running, completed, failed, or cancelled")
	}
	resultProvided := len(strings.TrimSpace(string(input.Result))) > 0
	if resultProvided {
		result, err := normalizeJSONObjectPayload(input.Result)
		if err != nil {
			return UpdateRuntimeTaskStatusInput{}, false, fmt.Errorf("result %w", err)
		}
		input.Result = result
	}
	input.Error = strings.TrimSpace(input.Error)
	return input, resultProvided, nil
}

func normalizeAppendRuntimeTaskLogInput(input AppendRuntimeTaskLogInput) (AppendRuntimeTaskLogInput, error) {
	input.Stream = strings.ToLower(strings.TrimSpace(input.Stream))
	if input.Stream == "" {
		input.Stream = "system"
	}
	if len(input.Stream) > 64 {
		return AppendRuntimeTaskLogInput{}, errors.New("log stream must be 64 characters or less")
	}
	for _, r := range input.Stream {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' {
			return AppendRuntimeTaskLogInput{}, errors.New("log stream may only contain lowercase letters, numbers, dash, and underscore")
		}
	}
	input.Message = strings.TrimSpace(input.Message)
	if input.Message == "" {
		return AppendRuntimeTaskLogInput{}, errors.New("log message is required")
	}
	const maxRuntimeTaskLogMessageBytes = 64 * 1024
	if len(input.Message) > maxRuntimeTaskLogMessageBytes {
		input.Message = input.Message[:maxRuntimeTaskLogMessageBytes]
	}
	return input, nil
}

func normalizeCreateWorkspaceInvitationInput(input CreateWorkspaceInvitationInput) (CreateWorkspaceInvitationInput, error) {
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if len(input.Email) > 320 {
		return CreateWorkspaceInvitationInput{}, errors.New("email must be 320 characters or less")
	}
	input.Role = strings.ToLower(strings.TrimSpace(input.Role))
	if input.Role == "" {
		input.Role = "member"
	}
	if input.Role != "member" && input.Role != "admin" {
		return CreateWorkspaceInvitationInput{}, errors.New("role must be member or admin")
	}
	if input.ExpiresInHours <= 0 {
		input.ExpiresInHours = 168
	}
	if input.ExpiresInHours > 24*90 {
		return CreateWorkspaceInvitationInput{}, errors.New("expiresInHours must be 2160 or less")
	}
	return input, nil
}

func normalizeCreateWorkspaceInput(input CreateWorkspaceInput) (CreateWorkspaceInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return CreateWorkspaceInput{}, errors.New("workspace name is required")
	}
	if len(input.Name) > 120 {
		return CreateWorkspaceInput{}, errors.New("workspace name must be 120 characters or less")
	}
	input.Kind = strings.ToLower(strings.TrimSpace(input.Kind))
	if input.Kind == "" {
		input.Kind = "team"
	}
	if input.Kind != "team" {
		return CreateWorkspaceInput{}, errors.New("only team workspaces can be created explicitly")
	}
	return input, nil
}

func normalizeRuntimeTaskCancelReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "Task cancellation requested by workspace member."
	}
	const maxRuntimeTaskCancelReasonBytes = 1024
	if len(reason) > maxRuntimeTaskCancelReasonBytes {
		reason = reason[:maxRuntimeTaskCancelReasonBytes]
	}
	return reason
}

func isFinalRuntimeTaskStatus(status string) bool {
	return status == "completed" || status == "failed" || status == "cancelled"
}

func normalizeRuntimeHeartbeatInput(input RuntimeWorkerInput) (RuntimeWorkerInput, error) {
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = "online"
	}
	switch input.Status {
	case "online", "draining", "offline":
	default:
		return RuntimeWorkerInput{}, errors.New("worker status must be online, draining, or offline")
	}
	input.Version = strings.TrimSpace(input.Version)
	if input.CurrentLoad < 0 {
		return RuntimeWorkerInput{}, errors.New("currentLoad must be greater than or equal to zero")
	}
	capabilities, err := normalizeJSONPayload(input.Capabilities)
	if err != nil {
		return RuntimeWorkerInput{}, fmt.Errorf("capabilities %w", err)
	}
	labels, err := normalizeJSONPayload(input.Labels)
	if err != nil {
		return RuntimeWorkerInput{}, fmt.Errorf("labels %w", err)
	}
	input.Capabilities = capabilities
	input.Labels = labels
	return input, nil
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
