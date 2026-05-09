package control

import (
	"context"
	"encoding/json"
	"errors"
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

func asContext(ctx Context) context.Context {
	if typed, ok := ctx.(context.Context); ok {
		return typed
	}
	return context.Background()
}
