package control

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mu          sync.Mutex
	states      map[string]OAuthState
	results     map[string]memoryOAuthResult
	users       map[string]User
	identities  map[string]string
	workspaces  map[string][]Workspace
	sessionHash map[string]memorySession
	nextID      int
}

type memorySession struct {
	UserID    string
	ExpiresAt time.Time
}

type memoryOAuthResult struct {
	Provider  string
	Result    AuthResult
	ExpiresAt time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		states:      map[string]OAuthState{},
		results:     map[string]memoryOAuthResult{},
		users:       map[string]User{},
		identities:  map[string]string{},
		workspaces:  map[string][]Workspace{},
		sessionHash: map[string]memorySession{},
	}
}

func (s *MemoryStore) SaveOAuthState(_ Context, state OAuthState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[state.State] = state
	return nil
}

func (s *MemoryStore) ConsumeOAuthState(_ Context, provider, state string) (OAuthState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.states[state]
	if !ok || record.Provider != provider {
		return OAuthState{}, ErrNotFound
	}
	delete(s.states, state)
	if time.Now().After(record.ExpiresAt) {
		return OAuthState{}, ErrExpired
	}
	return record, nil
}

func (s *MemoryStore) SaveOAuthResult(_ Context, provider, state string, result AuthResult, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[state] = memoryOAuthResult{Provider: provider, Result: result, ExpiresAt: expiresAt}
	return nil
}

func (s *MemoryStore) ConsumeOAuthResult(_ Context, provider, state string) (AuthResult, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, ok := s.results[state]
	if ok {
		delete(s.results, state)
		if result.Provider != provider {
			return AuthResult{}, false, ErrNotFound
		}
		if time.Now().After(result.ExpiresAt) {
			return AuthResult{}, false, ErrExpired
		}
		return result.Result, true, nil
	}

	record, ok := s.states[state]
	if !ok || record.Provider != provider {
		return AuthResult{}, false, ErrNotFound
	}
	if time.Now().After(record.ExpiresAt) {
		return AuthResult{}, false, ErrExpired
	}
	return AuthResult{}, false, nil
}

func (s *MemoryStore) UpsertIdentity(_ Context, profile IdentityProfile) (User, []Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := profile.Provider + ":" + profile.ProviderUserID
	if userID := s.identities[key]; userID != "" {
		user := s.users[userID]
		return user, s.workspaces[userID], nil
	}

	s.nextID++
	userID := fmt.Sprintf("user-%04d", s.nextID)
	now := time.Now().UTC().Format(time.RFC3339)
	name := strings.TrimSpace(profile.Name)
	if name == "" {
		name = profile.Login
	}
	user := User{
		ID:        userID,
		Name:      name,
		Email:     profile.Email,
		AvatarURL: profile.AvatarURL,
		CreatedAt: now,
		UpdatedAt: now,
	}
	workspace := Workspace{
		ID:        "workspace-" + userID,
		Name:      name + "'s workspace",
		Slug:      defaultWorkspaceSlug(profile.Login, userID),
		Role:      "owner",
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.users[userID] = user
	s.identities[key] = userID
	s.workspaces[userID] = []Workspace{workspace}
	return user, s.workspaces[userID], nil
}

func (s *MemoryStore) CreateAuthSession(_ Context, userID string, ttl time.Duration) (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	token, err := newSessionToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().UTC().Add(ttl)
	s.sessionHash[tokenHash(token)] = memorySession{UserID: userID, ExpiresAt: expiresAt}
	return token, expiresAt, nil
}

func (s *MemoryStore) GetUserBySessionToken(_ Context, token string) (User, []Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessionHash[tokenHash(token)]
	if !ok || time.Now().After(session.ExpiresAt) {
		return User{}, nil, ErrNotFound
	}
	user, ok := s.users[session.UserID]
	if !ok {
		return User{}, nil, ErrNotFound
	}
	return user, s.workspaces[session.UserID], nil
}
