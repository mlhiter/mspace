package control

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mu               sync.Mutex
	states           map[string]OAuthState
	results          map[string]memoryOAuthResult
	users            map[string]User
	identities       map[string]string
	workspaces       map[string][]Workspace
	workspaceMembers map[string]map[string]bool
	events           map[string]IssueEvent
	receipts         map[string]memoryReceipt
	watchers         map[string]map[string]bool
	sessionHash      map[string]memorySession
	nextID           int
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

type memoryReceipt struct {
	EventID     string
	WorkspaceID string
	IssueID     string
	UserID      string
	State       string
	ReadAt      string
	ArchivedAt  string
	CreatedAt   string
	UpdatedAt   string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		states:           map[string]OAuthState{},
		results:          map[string]memoryOAuthResult{},
		users:            map[string]User{},
		identities:       map[string]string{},
		workspaces:       map[string][]Workspace{},
		workspaceMembers: map[string]map[string]bool{},
		events:           map[string]IssueEvent{},
		receipts:         map[string]memoryReceipt{},
		watchers:         map[string]map[string]bool{},
		sessionHash:      map[string]memorySession{},
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
	if s.workspaceMembers[workspace.ID] == nil {
		s.workspaceMembers[workspace.ID] = map[string]bool{}
	}
	s.workspaceMembers[workspace.ID][userID] = true
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

func (s *MemoryStore) ListInbox(_ Context, userID, workspaceID string) ([]InboxEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	unreadByIssue := map[string]int{}
	for _, receipt := range s.receipts {
		if receipt.WorkspaceID == workspaceID && receipt.UserID == userID && receipt.State == "unread" {
			unreadByIssue[receipt.IssueID]++
		}
	}
	items := make([]InboxEntry, 0)
	for _, receipt := range s.receipts {
		if receipt.WorkspaceID != workspaceID || receipt.UserID != userID || receipt.State != "unread" {
			continue
		}
		event, ok := s.events[receipt.EventID]
		if !ok {
			continue
		}
		items = append(items, InboxEntry{
			EventID:     event.ID,
			WorkspaceID: event.WorkspaceID,
			IssueID:     event.IssueID,
			ActorUserID: event.ActorUserID,
			Kind:        event.Kind,
			Summary:     event.Summary,
			Payload:     copyRawMessage(event.Payload),
			State:       receipt.State,
			UnreadCount: unreadByIssue[event.IssueID],
			CreatedAt:   event.CreatedAt,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})
	return items, nil
}

func (s *MemoryStore) CreateIssueEvent(_ Context, requesterUserID string, input CreateIssueEventInput) (IssueEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.IssueID = strings.TrimSpace(input.IssueID)
	input.ActorUserID = strings.TrimSpace(input.ActorUserID)
	input.Kind = strings.TrimSpace(input.Kind)
	input.Summary = strings.TrimSpace(input.Summary)
	if input.WorkspaceID == "" || input.IssueID == "" || input.Kind == "" {
		return IssueEvent{}, fmt.Errorf("workspaceId, issueId, and kind are required")
	}
	if !s.isWorkspaceMember(input.WorkspaceID, requesterUserID) {
		return IssueEvent{}, ErrNotFound
	}
	if input.ActorUserID != "" && !s.isWorkspaceMember(input.WorkspaceID, input.ActorUserID) {
		return IssueEvent{}, ErrNotFound
	}
	payload, err := normalizeJSONPayload(input.Payload)
	if err != nil {
		return IssueEvent{}, err
	}

	s.nextID++
	now := time.Now().UTC().Format(time.RFC3339Nano)
	event := IssueEvent{
		ID:          fmt.Sprintf("event-%04d", s.nextID),
		WorkspaceID: input.WorkspaceID,
		IssueID:     input.IssueID,
		ActorUserID: input.ActorUserID,
		Kind:        input.Kind,
		Summary:     input.Summary,
		Payload:     copyRawMessage(payload),
		CreatedAt:   now,
	}
	s.events[event.ID] = event

	recipients, err := s.resolveIssueEventRecipients(input)
	if err != nil {
		return IssueEvent{}, err
	}
	if input.ActorUserID != "" {
		delete(recipients, input.ActorUserID)
	}
	for userID := range recipients {
		key := receiptKey(event.ID, userID)
		if _, exists := s.receipts[key]; exists {
			continue
		}
		s.receipts[key] = memoryReceipt{
			EventID:     event.ID,
			WorkspaceID: input.WorkspaceID,
			IssueID:     input.IssueID,
			UserID:      userID,
			State:       "unread",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
	}
	return event, nil
}

func (s *MemoryStore) MarkIssueEventRead(_ Context, userID, workspaceID, eventID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isWorkspaceMember(workspaceID, userID) {
		return ErrNotFound
	}
	key := receiptKey(eventID, userID)
	receipt, ok := s.receipts[key]
	if !ok || receipt.WorkspaceID != workspaceID {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	receipt.State = "read"
	if receipt.ReadAt == "" {
		receipt.ReadAt = now
	}
	receipt.UpdatedAt = now
	s.receipts[key] = receipt
	return nil
}

func (s *MemoryStore) MarkIssueReadThrough(_ Context, userID, workspaceID, issueID, throughEventID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isWorkspaceMember(workspaceID, userID) {
		return 0, ErrNotFound
	}
	issueID = strings.TrimSpace(issueID)
	if issueID == "" {
		return 0, fmt.Errorf("issueId is required")
	}
	boundary := ""
	if strings.TrimSpace(throughEventID) != "" {
		event, ok := s.events[strings.TrimSpace(throughEventID)]
		if !ok || event.WorkspaceID != workspaceID || event.IssueID != issueID {
			return 0, ErrNotFound
		}
		boundary = event.CreatedAt
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	count := 0
	for key, receipt := range s.receipts {
		if receipt.WorkspaceID != workspaceID || receipt.IssueID != issueID || receipt.UserID != userID || receipt.State != "unread" {
			continue
		}
		event, ok := s.events[receipt.EventID]
		if !ok {
			continue
		}
		if boundary != "" && event.CreatedAt > boundary {
			continue
		}
		receipt.State = "read"
		if receipt.ReadAt == "" {
			receipt.ReadAt = now
		}
		receipt.UpdatedAt = now
		s.receipts[key] = receipt
		count++
	}
	return count, nil
}

func (s *MemoryStore) isWorkspaceMember(workspaceID, userID string) bool {
	return s.workspaceMembers[strings.TrimSpace(workspaceID)][strings.TrimSpace(userID)]
}

func (s *MemoryStore) resolveIssueEventRecipients(input CreateIssueEventInput) (map[string]bool, error) {
	recipients := map[string]bool{}
	watcherKey := input.WorkspaceID + ":" + input.IssueID
	for userID := range s.watchers[watcherKey] {
		if s.isWorkspaceMember(input.WorkspaceID, userID) {
			recipients[userID] = true
		}
	}
	for _, userID := range input.RecipientUserIDs {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		if !s.isWorkspaceMember(input.WorkspaceID, userID) {
			return nil, ErrNotFound
		}
		recipients[userID] = true
	}
	if len(recipients) > 0 {
		return recipients, nil
	}
	for userID := range s.workspaceMembers[input.WorkspaceID] {
		recipients[userID] = true
	}
	return recipients, nil
}

func receiptKey(eventID, userID string) string {
	return eventID + ":" + userID
}

func copyRawMessage(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(`{}`)
	}
	return append(json.RawMessage(nil), value...)
}
