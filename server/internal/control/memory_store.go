package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mu                   sync.Mutex
	states               map[string]OAuthState
	results              map[string]memoryOAuthResult
	users                map[string]User
	identities           map[string]string
	workspaces           map[string][]Workspace
	workspaceMembers     map[string]map[string]string
	workspaceInvitations map[string]memoryWorkspaceInvitation
	events               map[string]IssueEvent
	receipts             map[string]memoryReceipt
	watchers             map[string]map[string]bool
	projects             map[string]Project
	projectRunbooks      map[string]ProjectRunbook
	issues               map[string]Issue
	comments             map[string]Comment
	commentReactions     map[string]map[string]CommentReactionSummary
	attachments          map[string]IssueAttachment
	issueLabels          map[string][]IssueLabel
	workspaceSettings    map[string]WorkspaceSettings
	agentProfiles        map[string]AgentProfile
	clusters             map[string]Cluster
	testEnvironments     map[string]IssueTestEnvironment
	reviewEvidence       map[string]SessionReviewEvidence
	sessionFailures      map[string]SessionFailure
	handoffs             map[string]IssueHandoff
	sessionHash          map[string]memorySession
	runtimeTokens        map[string]memoryRuntimeRegistrationToken
	runtimeWorkers       map[string]RuntimeWorker
	runtimeTasks         map[string]RuntimeTask
	runtimeTaskEvents    map[string]RuntimeTaskEvent
	runtimeTaskLogs      map[string]RuntimeTaskLog
	nextID               int
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

type memoryRuntimeRegistrationToken struct {
	TokenHash string
	Record    RuntimeRegistrationToken
}

type memoryWorkspaceInvitation struct {
	TokenHash string
	Record    WorkspaceInvitation
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		states:               map[string]OAuthState{},
		results:              map[string]memoryOAuthResult{},
		users:                map[string]User{},
		identities:           map[string]string{},
		workspaces:           map[string][]Workspace{},
		workspaceMembers:     map[string]map[string]string{},
		workspaceInvitations: map[string]memoryWorkspaceInvitation{},
		events:               map[string]IssueEvent{},
		receipts:             map[string]memoryReceipt{},
		watchers:             map[string]map[string]bool{},
		projects:             map[string]Project{},
		projectRunbooks:      map[string]ProjectRunbook{},
		issues:               map[string]Issue{},
		comments:             map[string]Comment{},
		commentReactions:     map[string]map[string]CommentReactionSummary{},
		attachments:          map[string]IssueAttachment{},
		issueLabels:          map[string][]IssueLabel{},
		workspaceSettings:    map[string]WorkspaceSettings{},
		agentProfiles:        map[string]AgentProfile{},
		clusters:             map[string]Cluster{},
		testEnvironments:     map[string]IssueTestEnvironment{},
		reviewEvidence:       map[string]SessionReviewEvidence{},
		sessionFailures:      map[string]SessionFailure{},
		handoffs:             map[string]IssueHandoff{},
		sessionHash:          map[string]memorySession{},
		runtimeTokens:        map[string]memoryRuntimeRegistrationToken{},
		runtimeWorkers:       map[string]RuntimeWorker{},
		runtimeTasks:         map[string]RuntimeTask{},
		runtimeTaskEvents:    map[string]RuntimeTaskEvent{},
		runtimeTaskLogs:      map[string]RuntimeTaskLog{},
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
		Kind:      "personal",
		Role:      "owner",
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.users[userID] = user
	s.identities[key] = userID
	s.workspaces[userID] = []Workspace{workspace}
	if s.workspaceMembers[workspace.ID] == nil {
		s.workspaceMembers[workspace.ID] = map[string]string{}
	}
	s.workspaceMembers[workspace.ID][userID] = "owner"
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

func (s *MemoryStore) CreateWorkspace(_ Context, userID string, input CreateWorkspaceInput) (Workspace, []Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	userID = strings.TrimSpace(userID)
	if userID == "" {
		return Workspace{}, nil, ErrNotFound
	}
	if _, ok := s.users[userID]; !ok {
		return Workspace{}, nil, ErrNotFound
	}
	normalized, err := normalizeCreateWorkspaceInput(input)
	if err != nil {
		return Workspace{}, nil, err
	}
	s.nextID++
	now := time.Now().UTC().Format(time.RFC3339)
	workspaceID := fmt.Sprintf("workspace-%04d", s.nextID)
	workspace := Workspace{
		ID:        workspaceID,
		Name:      normalized.Name,
		Slug:      workspaceSlug(normalized.Name, strings.TrimPrefix(workspaceID, "workspace-")),
		Kind:      normalized.Kind,
		Role:      "owner",
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.workspaces[userID] = append(s.workspaces[userID], workspace)
	if s.workspaceMembers[workspace.ID] == nil {
		s.workspaceMembers[workspace.ID] = map[string]string{}
	}
	s.workspaceMembers[workspace.ID][userID] = "owner"
	return workspace, append([]Workspace(nil), s.workspaces[userID]...), nil
}

func (s *MemoryStore) ListWorkspaceMembers(_ Context, userID, workspaceID string) ([]WorkspaceMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	members := []WorkspaceMember{}
	for memberUserID, role := range s.workspaceMembers[workspaceID] {
		user, ok := s.users[memberUserID]
		if !ok {
			continue
		}
		member := WorkspaceMember{
			ID:            workspaceID + ":" + memberUserID,
			WorkspaceID:   workspaceID,
			UserID:        memberUserID,
			Role:          role,
			Name:          user.Name,
			Email:         user.Email,
			AvatarURL:     user.AvatarURL,
			IdentityLogin: s.identityLoginForUser(memberUserID),
			CreatedAt:     user.CreatedAt,
			UpdatedAt:     user.UpdatedAt,
		}
		members = append(members, member)
	}
	sort.Slice(members, func(i, j int) bool {
		leftRank := workspaceRoleRank(members[i].Role)
		rightRank := workspaceRoleRank(members[j].Role)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return strings.ToLower(members[i].Name) < strings.ToLower(members[j].Name)
	})
	return members, nil
}

func (s *MemoryStore) CreateWorkspaceInvitation(_ Context, userID, workspaceID string, input CreateWorkspaceInvitationInput) (WorkspaceInvitationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isTeamWorkspace(workspaceID) {
		if !s.isWorkspaceMember(workspaceID, userID) {
			return WorkspaceInvitationResult{}, ErrNotFound
		}
		return WorkspaceInvitationResult{}, ErrForbidden
	}
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		if !s.isWorkspaceMember(workspaceID, userID) {
			return WorkspaceInvitationResult{}, ErrNotFound
		}
		return WorkspaceInvitationResult{}, ErrForbidden
	}
	normalized, err := normalizeCreateWorkspaceInvitationInput(input)
	if err != nil {
		return WorkspaceInvitationResult{}, err
	}
	token, err := newWorkspaceInvitationToken()
	if err != nil {
		return WorkspaceInvitationResult{}, err
	}
	s.nextID++
	now := time.Now().UTC()
	record := WorkspaceInvitation{
		ID:              fmt.Sprintf("workspace-invitation-%04d", s.nextID),
		WorkspaceID:     workspaceID,
		WorkspaceName:   s.workspaceName(workspaceID),
		Email:           normalized.Email,
		Role:            normalized.Role,
		TokenPrefix:     tokenPrefix(token),
		InvitedByUserID: strings.TrimSpace(userID),
		ExpiresAt:       now.Add(time.Duration(normalized.ExpiresInHours) * time.Hour).Format(time.RFC3339),
		Revoked:         false,
		CreatedAt:       now.Format(time.RFC3339),
		UpdatedAt:       now.Format(time.RFC3339),
	}
	s.workspaceInvitations[tokenHash(token)] = memoryWorkspaceInvitation{TokenHash: tokenHash(token), Record: record}
	return WorkspaceInvitationResult{Token: token, Invitation: record}, nil
}

func (s *MemoryStore) ListWorkspaceInvitations(_ Context, userID, workspaceID string) ([]WorkspaceInvitation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isTeamWorkspace(workspaceID) {
		if !s.isWorkspaceMember(workspaceID, userID) {
			return nil, ErrNotFound
		}
		return nil, ErrForbidden
	}
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		if !s.isWorkspaceMember(workspaceID, userID) {
			return nil, ErrNotFound
		}
		return nil, ErrForbidden
	}
	invitations := []WorkspaceInvitation{}
	for _, invitation := range s.workspaceInvitations {
		if invitation.Record.WorkspaceID == workspaceID {
			invitations = append(invitations, invitation.Record)
		}
	}
	sort.Slice(invitations, func(i, j int) bool {
		return invitations[i].CreatedAt > invitations[j].CreatedAt
	})
	return invitations, nil
}

func (s *MemoryStore) RevokeWorkspaceInvitation(_ Context, userID, workspaceID, invitationID string) (WorkspaceInvitation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	invitationID = strings.TrimSpace(invitationID)
	if !s.isTeamWorkspace(workspaceID) {
		if !s.isWorkspaceMember(workspaceID, userID) {
			return WorkspaceInvitation{}, ErrNotFound
		}
		return WorkspaceInvitation{}, ErrForbidden
	}
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		if !s.isWorkspaceMember(workspaceID, userID) {
			return WorkspaceInvitation{}, ErrNotFound
		}
		return WorkspaceInvitation{}, ErrForbidden
	}
	for key, invitation := range s.workspaceInvitations {
		if invitation.Record.ID != invitationID || invitation.Record.WorkspaceID != workspaceID {
			continue
		}
		invitation.Record.Revoked = true
		invitation.Record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		s.workspaceInvitations[key] = invitation
		return invitation.Record, nil
	}
	return WorkspaceInvitation{}, ErrNotFound
}

func (s *MemoryStore) AcceptWorkspaceInvitation(_ Context, userID string, input AcceptWorkspaceInvitationInput) (AcceptWorkspaceInvitationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	userID = strings.TrimSpace(userID)
	token := strings.TrimSpace(input.Token)
	if userID == "" || token == "" || !strings.HasPrefix(token, "msi_") {
		return AcceptWorkspaceInvitationResult{}, ErrNotFound
	}
	invitation, ok := s.workspaceInvitations[tokenHash(token)]
	if !ok {
		return AcceptWorkspaceInvitationResult{}, ErrNotFound
	}
	if invitation.Record.Revoked || invitation.Record.AcceptedAt != "" {
		return AcceptWorkspaceInvitationResult{}, ErrNotFound
	}
	expiresAt, err := time.Parse(time.RFC3339, invitation.Record.ExpiresAt)
	if err != nil || time.Now().After(expiresAt) {
		return AcceptWorkspaceInvitationResult{}, ErrExpired
	}
	if s.workspaceMembers[invitation.Record.WorkspaceID] == nil {
		s.workspaceMembers[invitation.Record.WorkspaceID] = map[string]string{}
	}
	currentRole := s.workspaceMembers[invitation.Record.WorkspaceID][userID]
	role := invitation.Record.Role
	if currentRole == "owner" || (currentRole == "admin" && role == "member") {
		role = currentRole
	}
	s.workspaceMembers[invitation.Record.WorkspaceID][userID] = role
	s.addWorkspaceForUser(userID, invitation.Record.WorkspaceID, role)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	invitation.Record.AcceptedByUserID = userID
	invitation.Record.AcceptedAt = now
	invitation.Record.UpdatedAt = now
	s.workspaceInvitations[tokenHash(token)] = invitation

	workspace := Workspace{}
	for _, item := range s.workspaces[userID] {
		if item.ID == invitation.Record.WorkspaceID {
			workspace = item
			break
		}
	}
	return AcceptWorkspaceInvitationResult{
		Workspace:  workspace,
		Invitation: invitation.Record,
		Workspaces: append([]Workspace(nil), s.workspaces[userID]...),
	}, nil
}

func (s *MemoryStore) ListInbox(_ Context, userID, workspaceID string) ([]InboxEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	if !s.isTeamWorkspace(workspaceID) {
		return nil, ErrForbidden
	}
	unreadByIssue := map[string]int{}
	for _, receipt := range s.receipts {
		if receipt.WorkspaceID == workspaceID && receipt.UserID == userID && receipt.State == "unread" {
			unreadByIssue[receipt.IssueID]++
		}
	}
	latestByIssue := map[string]InboxEntry{}
	for _, receipt := range s.receipts {
		if receipt.WorkspaceID != workspaceID || receipt.UserID != userID || receipt.State != "unread" {
			continue
		}
		event, ok := s.events[receipt.EventID]
		if !ok {
			continue
		}
		item := InboxEntry{
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
		}
		existing, exists := latestByIssue[event.IssueID]
		if !exists || item.CreatedAt > existing.CreatedAt || (item.CreatedAt == existing.CreatedAt && item.EventID > existing.EventID) {
			latestByIssue[event.IssueID] = item
		}
	}
	items := make([]InboxEntry, 0, len(latestByIssue))
	for _, item := range latestByIssue {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt == items[j].CreatedAt {
			return items[i].EventID > items[j].EventID
		}
		return items[i].CreatedAt > items[j].CreatedAt
	})
	if len(items) > 100 {
		items = items[:100]
	}
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

func (s *MemoryStore) CreateRuntimeRegistrationToken(_ Context, userID, workspaceID string, input CreateRuntimeRegistrationTokenInput) (RuntimeRegistrationTokenResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		if !s.isWorkspaceMember(workspaceID, userID) {
			return RuntimeRegistrationTokenResult{}, ErrNotFound
		}
		return RuntimeRegistrationTokenResult{}, ErrForbidden
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "Runtime worker token"
	}
	expiresInHours := input.ExpiresInHours
	if expiresInHours <= 0 {
		expiresInHours = 24
	}
	if expiresInHours > 24*90 {
		return RuntimeRegistrationTokenResult{}, fmt.Errorf("expiresInHours must be 2160 or less")
	}
	token, err := newRuntimeRegistrationToken()
	if err != nil {
		return RuntimeRegistrationTokenResult{}, err
	}
	s.nextID++
	now := time.Now().UTC()
	record := RuntimeRegistrationToken{
		ID:          fmt.Sprintf("runtime-token-%04d", s.nextID),
		WorkspaceID: strings.TrimSpace(workspaceID),
		Name:        name,
		TokenPrefix: tokenPrefix(token),
		ExpiresAt:   now.Add(time.Duration(expiresInHours) * time.Hour).Format(time.RFC3339),
		Revoked:     false,
		CreatedAt:   now.Format(time.RFC3339),
		UpdatedAt:   now.Format(time.RFC3339),
	}
	s.runtimeTokens[tokenHash(token)] = memoryRuntimeRegistrationToken{TokenHash: tokenHash(token), Record: record}
	return RuntimeRegistrationTokenResult{Token: token, RegistrationToken: record}, nil
}

func (s *MemoryStore) ListRuntimeRegistrationTokens(_ Context, userID, workspaceID string) ([]RuntimeRegistrationToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		if !s.isWorkspaceMember(workspaceID, userID) {
			return nil, ErrNotFound
		}
		return nil, ErrForbidden
	}
	tokens := []RuntimeRegistrationToken{}
	for _, token := range s.runtimeTokens {
		if token.Record.WorkspaceID == strings.TrimSpace(workspaceID) {
			tokens = append(tokens, token.Record)
		}
	}
	sort.Slice(tokens, func(i, j int) bool {
		return tokens[i].CreatedAt > tokens[j].CreatedAt
	})
	return tokens, nil
}

func (s *MemoryStore) RevokeRuntimeRegistrationToken(_ Context, userID, workspaceID, tokenID string) (RuntimeRegistrationToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		if !s.isWorkspaceMember(workspaceID, userID) {
			return RuntimeRegistrationToken{}, ErrNotFound
		}
		return RuntimeRegistrationToken{}, ErrForbidden
	}
	workspaceID = strings.TrimSpace(workspaceID)
	tokenID = strings.TrimSpace(tokenID)
	for key, token := range s.runtimeTokens {
		if token.Record.ID != tokenID || token.Record.WorkspaceID != workspaceID {
			continue
		}
		token.Record.Revoked = true
		token.Record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		s.runtimeTokens[key] = token
		return token.Record, nil
	}
	return RuntimeRegistrationToken{}, ErrNotFound
}

func (s *MemoryStore) AuthenticateRuntimeRegistrationToken(_ Context, token string) (RuntimeRegistration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, "msw_") {
		return RuntimeRegistration{}, ErrNotFound
	}
	record, ok := s.runtimeTokens[tokenHash(token)]
	if !ok || record.Record.Revoked {
		return RuntimeRegistration{}, ErrNotFound
	}
	expiresAt, err := time.Parse(time.RFC3339, record.Record.ExpiresAt)
	if err != nil || time.Now().After(expiresAt) {
		return RuntimeRegistration{}, ErrExpired
	}
	return RuntimeRegistration{TokenID: record.Record.ID, WorkspaceID: record.Record.WorkspaceID}, nil
}

func (s *MemoryStore) RegisterRuntimeWorker(_ Context, registration RuntimeRegistration, input RuntimeWorkerInput) (RuntimeWorker, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized, err := normalizeRuntimeWorkerInput(input)
	if err != nil {
		return RuntimeWorker{}, err
	}
	if registration.TokenID == "" || registration.WorkspaceID == "" {
		return RuntimeWorker{}, ErrNotFound
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for key, token := range s.runtimeTokens {
		if token.Record.ID == registration.TokenID && token.Record.WorkspaceID == registration.WorkspaceID {
			token.Record.LastUsedAt = now
			token.Record.UpdatedAt = now
			s.runtimeTokens[key] = token
			break
		}
	}

	key := registration.WorkspaceID + ":" + normalized.Name
	worker := s.runtimeWorkers[key]
	if worker.ID == "" {
		s.nextID++
		worker.ID = fmt.Sprintf("worker-%04d", s.nextID)
		worker.WorkspaceID = registration.WorkspaceID
		worker.Name = normalized.Name
		worker.CreatedAt = now
	}
	worker.Mode = normalized.Mode
	worker.Status = normalized.Status
	worker.Version = normalized.Version
	worker.CurrentLoad = normalized.CurrentLoad
	worker.Capabilities = copyRawMessage(normalized.Capabilities)
	worker.Labels = copyRawMessage(normalized.Labels)
	worker.LastSeenAt = now
	worker.UpdatedAt = now
	s.runtimeWorkers[key] = worker
	return worker, nil
}

func (s *MemoryStore) UpdateRuntimeWorkerHeartbeat(_ Context, registration RuntimeRegistration, workerID string, input RuntimeWorkerInput) (RuntimeWorker, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if registration.TokenID == "" || registration.WorkspaceID == "" || strings.TrimSpace(workerID) == "" {
		return RuntimeWorker{}, ErrNotFound
	}
	normalized, err := normalizeRuntimeHeartbeatInput(input)
	if err != nil {
		return RuntimeWorker{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for key, token := range s.runtimeTokens {
		if token.Record.ID == registration.TokenID && token.Record.WorkspaceID == registration.WorkspaceID {
			token.Record.LastUsedAt = now
			token.Record.UpdatedAt = now
			s.runtimeTokens[key] = token
			break
		}
	}
	for key, worker := range s.runtimeWorkers {
		if worker.ID != strings.TrimSpace(workerID) || worker.WorkspaceID != registration.WorkspaceID {
			continue
		}
		worker.Status = normalized.Status
		if normalized.Version != "" {
			worker.Version = normalized.Version
		}
		worker.CurrentLoad = normalized.CurrentLoad
		if string(normalized.Capabilities) != "{}" {
			worker.Capabilities = copyRawMessage(normalized.Capabilities)
		}
		if string(normalized.Labels) != "{}" {
			worker.Labels = copyRawMessage(normalized.Labels)
		}
		worker.LastSeenAt = now
		worker.UpdatedAt = now
		s.runtimeWorkers[key] = worker
		return worker, nil
	}
	return RuntimeWorker{}, ErrNotFound
}

func (s *MemoryStore) ListRuntimeWorkers(_ Context, userID, workspaceID string) ([]RuntimeWorker, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	workers := []RuntimeWorker{}
	for _, worker := range s.runtimeWorkers {
		if worker.WorkspaceID == strings.TrimSpace(workspaceID) {
			workers = append(workers, worker)
		}
	}
	sort.Slice(workers, func(i, j int) bool {
		if workers[i].LastSeenAt == workers[j].LastSeenAt {
			return workers[i].CreatedAt > workers[j].CreatedAt
		}
		return workers[i].LastSeenAt > workers[j].LastSeenAt
	})
	return workers, nil
}

func (s *MemoryStore) CreateRuntimeTask(_ Context, userID, workspaceID string, input CreateRuntimeTaskInput) (RuntimeTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return RuntimeTask{}, ErrNotFound
	}
	normalized, err := normalizeCreateRuntimeTaskInput(input)
	if err != nil {
		return RuntimeTask{}, err
	}
	s.nextID++
	now := time.Now().UTC().Format(time.RFC3339Nano)
	task := RuntimeTask{
		ID:                   fmt.Sprintf("runtime-task-%04d", s.nextID),
		WorkspaceID:          workspaceID,
		IssueID:              normalized.IssueID,
		SessionID:            normalized.SessionID,
		ProjectID:            normalized.ProjectID,
		Kind:                 normalized.Kind,
		Status:               "queued",
		Priority:             normalized.Priority,
		RuntimeMode:          normalized.RuntimeMode,
		RequiredCapabilities: copyRawMessage(normalized.RequiredCapabilities),
		Payload:              copyRawMessage(normalized.Payload),
		Result:               json.RawMessage(`{}`),
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	s.runtimeTasks[task.ID] = task
	s.appendRuntimeTaskEventLocked(task.WorkspaceID, task.ID, "", userID, "created", json.RawMessage(fmt.Sprintf(`{"kind":%q,"runtimeMode":%q,"status":%q}`, task.Kind, task.RuntimeMode, task.Status)))
	return task, nil
}

func (s *MemoryStore) ListProjects(_ Context, userID, workspaceID string) ([]Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	projects := []Project{}
	for _, project := range s.projects {
		if project.WorkspaceID != workspaceID {
			continue
		}
		item := project
		for _, issue := range s.issues {
			if issue.WorkspaceID != workspaceID || issue.ProjectID != item.ID || issue.ParentIssueID != "" {
				continue
			}
			item.IssueCount++
			if issue.UpdatedAt > item.LatestIssueUpdatedAt {
				item.LatestIssueUpdatedAt = issue.UpdatedAt
			}
		}
		projects = append(projects, item)
	}
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].UpdatedAt > projects[j].UpdatedAt
	})
	return projects, nil
}

func (s *MemoryStore) GetWorkspaceSettings(_ Context, userID, workspaceID string) (WorkspaceSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return WorkspaceSettings{}, ErrNotFound
	}
	return s.workspaceSettingsLocked(workspaceID), nil
}

func (s *MemoryStore) UpdateWorkspaceSettings(_ Context, userID, workspaceID string, input WorkspaceSettingsInput) (WorkspaceSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		return WorkspaceSettings{}, ErrForbidden
	}
	settings := s.workspaceSettingsLocked(workspaceID)
	settings.AutoCreateDraftPR = input.AutoCreateDraftPR
	settings.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.workspaceSettings[workspaceID] = settings
	return settings, nil
}

func (s *MemoryStore) ListAgentProfiles(_ Context, userID, workspaceID string) ([]AgentProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	s.seedDefaultAgentProfilesLocked(workspaceID)
	profiles := []AgentProfile{}
	for _, profile := range s.agentProfiles {
		if strings.HasPrefix(profile.ID, workspaceID+":") {
			item := profile
			item.ID = strings.TrimPrefix(item.ID, workspaceID+":")
			profiles = append(profiles, item)
		}
	}
	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].SortOrder == profiles[j].SortOrder {
			return profiles[i].Name < profiles[j].Name
		}
		return profiles[i].SortOrder < profiles[j].SortOrder
	})
	return profiles, nil
}

func (s *MemoryStore) CreateAgentProfile(_ Context, userID, workspaceID string, input AgentProfileInput) (AgentProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		return AgentProfile{}, ErrForbidden
	}
	profile, err := normalizeAgentProfileInput(AgentProfile{}, input, false)
	if err != nil {
		return AgentProfile{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	profile.CreatedAt = now
	profile.UpdatedAt = now
	key := workspaceID + ":" + profile.ID
	if _, ok := s.agentProfiles[key]; ok {
		return AgentProfile{}, errors.New("agent profile already exists")
	}
	stored := profile
	stored.ID = key
	s.agentProfiles[key] = stored
	return profile, nil
}

func (s *MemoryStore) UpdateAgentProfile(_ Context, userID, workspaceID, agentID string, input AgentProfileInput) (AgentProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		return AgentProfile{}, ErrForbidden
	}
	s.seedDefaultAgentProfilesLocked(workspaceID)
	existing, key, ok := s.agentProfileLocked(workspaceID, agentID)
	if !ok {
		return AgentProfile{}, ErrNotFound
	}
	updated, err := normalizeAgentProfileInput(existing, input, true)
	if err != nil {
		return AgentProfile{}, err
	}
	updated.CreatedAt = existing.CreatedAt
	updated.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	stored := updated
	stored.ID = key
	s.agentProfiles[key] = stored
	return updated, nil
}

func (s *MemoryStore) ListClusters(_ Context, userID, workspaceID string) ([]Cluster, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	clusters := []Cluster{}
	for _, cluster := range s.clusters {
		if cluster.WorkspaceID != workspaceID {
			continue
		}
		item := cluster
		item.ProjectCount = 0
		item.EnvironmentCount = 0
		for _, project := range s.projects {
			if project.WorkspaceID == workspaceID && project.DefaultClusterID == item.ID {
				item.ProjectCount++
			}
		}
		for _, environment := range s.testEnvironments {
			if environment.ClusterID == item.ID {
				item.EnvironmentCount++
			}
		}
		clusters = append(clusters, item)
	}
	sort.Slice(clusters, func(i, j int) bool { return clusters[i].UpdatedAt > clusters[j].UpdatedAt })
	return clusters, nil
}

func (s *MemoryStore) CreateCluster(_ Context, userID, workspaceID string, input ClusterInput) (Cluster, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		return Cluster{}, ErrForbidden
	}
	cluster, err := normalizeClusterInput(Cluster{}, input)
	if err != nil {
		return Cluster{}, err
	}
	s.nextID++
	now := time.Now().UTC().Format(time.RFC3339Nano)
	cluster.ID = fmt.Sprintf("cluster-%04d", s.nextID)
	cluster.WorkspaceID = workspaceID
	cluster.LastCheckedAt = now
	cluster.CreatedAt = now
	cluster.UpdatedAt = now
	s.clusters[cluster.ID] = cluster
	return cluster, nil
}

func (s *MemoryStore) UpdateCluster(_ Context, userID, workspaceID, clusterID string, input ClusterInput) (Cluster, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	clusterID = strings.TrimSpace(clusterID)
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		return Cluster{}, ErrForbidden
	}
	existing, ok := s.clusters[clusterID]
	if !ok || existing.WorkspaceID != workspaceID {
		return Cluster{}, ErrNotFound
	}
	updated, err := normalizeClusterInput(existing, input)
	if err != nil {
		return Cluster{}, err
	}
	updated.ID = existing.ID
	updated.WorkspaceID = existing.WorkspaceID
	updated.CreatedAt = existing.CreatedAt
	updated.LastCheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
	updated.UpdatedAt = updated.LastCheckedAt
	s.clusters[clusterID] = updated
	return updated, nil
}

func (s *MemoryStore) DeleteCluster(_ Context, userID, workspaceID, clusterID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	clusterID = strings.TrimSpace(clusterID)
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		return ErrForbidden
	}
	cluster, ok := s.clusters[clusterID]
	if !ok || cluster.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	for _, project := range s.projects {
		if project.WorkspaceID == workspaceID && project.DefaultClusterID == clusterID {
			return ErrForbidden
		}
	}
	for _, environment := range s.testEnvironments {
		if environment.ClusterID == clusterID {
			return ErrForbidden
		}
	}
	delete(s.clusters, clusterID)
	return nil
}

func (s *MemoryStore) DiscoverDefaultKubeconfigs(_ Context, userID, workspaceID string) (KubeconfigDiscoveryResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasWorkspaceRole(strings.TrimSpace(workspaceID), userID, "owner", "admin") {
		return KubeconfigDiscoveryResult{}, ErrForbidden
	}
	return KubeconfigDiscoveryResult{Candidates: []KubeconfigCandidate{}, Skipped: []KubeconfigImportSkip{}}, nil
}

func (s *MemoryStore) ImportKubeconfigs(_ Context, userID, workspaceID string, paths []string) (KubeconfigImportResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		return KubeconfigImportResult{}, ErrForbidden
	}
	result := KubeconfigImportResult{Imported: []Cluster{}, Skipped: []KubeconfigImportSkip{}}
	for _, path := range uniqueStrings(paths) {
		cluster := Cluster{
			ID:                  fmt.Sprintf("cluster-%04d", s.nextMemoryIDLocked()),
			WorkspaceID:         workspaceID,
			Name:                importedClusterName(path, ""),
			KubeconfigPath:      path,
			ImageRegistryPrefix: defaultImportedClusterImageRegistryPrefix,
			ExposureMode:        "nodeport",
			Status:              "configured",
			LastCheckedAt:       time.Now().UTC().Format(time.RFC3339Nano),
			CreatedAt:           time.Now().UTC().Format(time.RFC3339Nano),
			UpdatedAt:           time.Now().UTC().Format(time.RFC3339Nano),
		}
		s.clusters[cluster.ID] = cluster
		result.Imported = append(result.Imported, cluster)
	}
	return result, nil
}

func (s *MemoryStore) CreateProject(_ Context, userID, workspaceID string, input ProjectInput) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return Project{}, ErrNotFound
	}
	normalized, err := normalizeProjectInput(input)
	if err != nil {
		return Project{}, err
	}
	s.nextID++
	now := time.Now().UTC().Format(time.RFC3339Nano)
	project := Project{
		ID:                  fmt.Sprintf("project-%04d", s.nextID),
		WorkspaceID:         workspaceID,
		Name:                normalized.Name,
		RepoPath:            normalized.RepoPath,
		SourceType:          normalized.SourceType,
		RemoteURL:           normalized.RepoURL,
		GitProvider:         gitProviderFromURL(normalized.RepoURL),
		GitOwner:            gitOwnerRepoFromURL(normalized.RepoURL).owner,
		GitRepo:             gitOwnerRepoFromURL(normalized.RepoURL).repo,
		DefaultBranch:       normalized.DefaultBranch,
		KubeContext:         normalized.KubeContext,
		KubeconfigPath:      normalized.KubeconfigPath,
		Namespace:           normalized.Namespace,
		ImageRegistryPrefix: normalized.ImageRegistryPrefix,
		PreviewDomain:       normalized.PreviewDomain,
		IngressClass:        normalized.IngressClass,
		NodeHost:            normalized.NodeHost,
		DefaultClusterID:    normalized.DefaultClusterID,
		RunbookStatus:       "empty",
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if project.DefaultBranch == "" {
		project.DefaultBranch = "main"
	}
	s.projects[project.ID] = project
	s.projectRunbooks[project.ID] = ProjectRunbook{ProjectID: project.ID, Status: "empty", CreatedAt: now, UpdatedAt: now}
	return project, nil
}

func (s *MemoryStore) UpdateProject(_ Context, userID, workspaceID, projectID string, input ProjectInput) (Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return Project{}, ErrNotFound
	}
	project, ok := s.projects[projectID]
	if !ok || project.WorkspaceID != workspaceID {
		return Project{}, ErrNotFound
	}
	normalized, err := normalizeProjectInput(input)
	if err != nil {
		return Project{}, err
	}
	project.Name = normalized.Name
	project.DefaultBranch = normalized.DefaultBranch
	project.KubeContext = normalized.KubeContext
	project.KubeconfigPath = normalized.KubeconfigPath
	project.Namespace = normalized.Namespace
	project.ImageRegistryPrefix = normalized.ImageRegistryPrefix
	project.PreviewDomain = normalized.PreviewDomain
	project.IngressClass = normalized.IngressClass
	project.NodeHost = normalized.NodeHost
	project.DefaultClusterID = normalized.DefaultClusterID
	project.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.projects[project.ID] = project
	return project, nil
}

func (s *MemoryStore) DeleteProject(_ Context, userID, workspaceID, projectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return ErrNotFound
	}
	project, ok := s.projects[projectID]
	if !ok || project.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	for _, issue := range s.issues {
		if issue.ProjectID == projectID {
			return ErrForbidden
		}
	}
	delete(s.projects, projectID)
	delete(s.projectRunbooks, projectID)
	return nil
}

func (s *MemoryStore) GetProjectRunbook(_ Context, userID, workspaceID, projectID string) (ProjectRunbook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isWorkspaceMember(workspaceID, userID) {
		return ProjectRunbook{}, ErrNotFound
	}
	project, ok := s.projects[strings.TrimSpace(projectID)]
	if !ok || project.WorkspaceID != strings.TrimSpace(workspaceID) {
		return ProjectRunbook{}, ErrNotFound
	}
	runbook := s.projectRunbooks[project.ID]
	if runbook.ProjectID == "" {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		runbook = ProjectRunbook{ProjectID: project.ID, Status: "empty", CreatedAt: now, UpdatedAt: now}
	}
	return runbook, nil
}

func (s *MemoryStore) UpdateProjectRunbook(_ Context, userID, workspaceID, projectID string, input ProjectRunbookInput) (ProjectRunbook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isWorkspaceMember(workspaceID, userID) {
		return ProjectRunbook{}, ErrNotFound
	}
	project, ok := s.projects[strings.TrimSpace(projectID)]
	if !ok || project.WorkspaceID != strings.TrimSpace(workspaceID) {
		return ProjectRunbook{}, ErrNotFound
	}
	status := normalizeRunbookStatus(input.Status, input.Content)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	runbook := s.projectRunbooks[project.ID]
	if runbook.CreatedAt == "" {
		runbook.CreatedAt = now
	}
	runbook.ProjectID = project.ID
	runbook.Content = input.Content
	runbook.Status = status
	runbook.Source = "human"
	runbook.UpdatedAt = now
	s.projectRunbooks[project.ID] = runbook
	project.RunbookStatus = status
	project.RunbookUpdatedAt = now
	project.RunbookSource = "human"
	project.UpdatedAt = now
	s.projects[project.ID] = project
	return runbook, nil
}

func (s *MemoryStore) ListIssueLabelDefinitions(_ Context, userID, workspaceID string) ([]IssueLabelDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	return builtInIssueLabelDefinitions(), nil
}

func (s *MemoryStore) ListIssues(_ Context, userID, workspaceID string) ([]IssueListItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	items := []IssueListItem{}
	for _, issue := range s.issues {
		if issue.WorkspaceID != workspaceID || issue.ParentIssueID != "" {
			continue
		}
		items = append(items, s.issueListItemLocked(issue))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt > items[j].UpdatedAt
	})
	return items, nil
}

func (s *MemoryStore) CreateIssue(_ Context, user User, workspaceID string, input CreateIssueInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, user.ID) {
		return "", ErrNotFound
	}
	normalized, tasks, labels, err := normalizeCreateIssueInput(input, user)
	if err != nil {
		return "", err
	}
	projectID, err := s.resolveOptionalIssueProjectIDLocked(workspaceID, normalized.ProjectID, normalized.Title+"\n"+normalized.Body)
	if err != nil {
		return "", err
	}
	s.nextID++
	now := time.Now().UTC().Format(time.RFC3339Nano)
	issueID := fmt.Sprintf("issue-%04d", s.nextID)
	issue := Issue{
		ID:             issueID,
		WorkspaceID:    workspaceID,
		ProjectID:      projectID,
		Title:          normalized.Title,
		Body:           normalized.Body,
		Status:         "open",
		TriageStatus:   "pending",
		Assignee:       normalized.Assignee,
		AssigneeType:   normalized.AssigneeType,
		CreatorName:    normalized.CreatorName,
		CreatorAvatar:  normalized.CreatorAvatar,
		EnvironmentURL: "",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if issue.AssigneeType == "human" && issue.Assignee == "" {
		issue.Assignee = issue.CreatorName
	}
	if hasIssueLabelDimension(labels, "type") {
		issue.TriageStatus = "classified"
	}
	s.issues[issue.ID] = issue
	for index, task := range tasks {
		s.nextID++
		taskID := fmt.Sprintf("issue-%04d", s.nextID)
		s.issues[taskID] = Issue{
			ID:             taskID,
			WorkspaceID:    workspaceID,
			ProjectID:      projectID,
			ParentIssueID:  issue.ID,
			SortOrder:      index + 1,
			Title:          task.Title,
			Body:           task.Body,
			Status:         normalizeIssueStatus(task.Status),
			TriageStatus:   "none",
			Assignee:       issue.CreatorName,
			AssigneeType:   "human",
			CreatorName:    issue.CreatorName,
			CreatorAvatar:  issue.CreatorAvatar,
			EnvironmentURL: "",
			CreatedAt:      now,
			UpdatedAt:      now,
		}
	}
	s.issueLabels[issue.ID] = labels
	s.nextID++
	s.comments[fmt.Sprintf("comment-%04d", s.nextID)] = Comment{
		ID:         fmt.Sprintf("comment-%04d", s.nextID),
		IssueID:    issue.ID,
		AuthorType: "system",
		AuthorName: "mspace",
		Body:       "Issue created and ready for review.",
		CreatedAt:  now,
		UpdatedAt:  now,
		Reactions:  []CommentReactionSummary{},
	}
	return issue.ID, nil
}

func (s *MemoryStore) GetIssue(_ Context, userID, workspaceID, issueID string) (IssueDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.getIssueLocked(userID, workspaceID, issueID)
}

func (s *MemoryStore) getIssueLocked(userID, workspaceID, issueID string) (IssueDetail, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return IssueDetail{}, ErrNotFound
	}
	issue, ok := s.issues[issueID]
	if !ok || issue.WorkspaceID != workspaceID {
		return IssueDetail{}, ErrNotFound
	}
	project := s.projects[issue.ProjectID]
	comments := []Comment{}
	for _, comment := range s.comments {
		if comment.IssueID != issueID {
			continue
		}
		comment.Reactions = s.commentReactionSummariesLocked(comment.ID, userID)
		comments = append(comments, comment)
	}
	sort.Slice(comments, func(i, j int) bool {
		if comments[i].CreatedAt == comments[j].CreatedAt {
			return comments[i].ID > comments[j].ID
		}
		return comments[i].CreatedAt > comments[j].CreatedAt
	})
	children := []IssueListItem{}
	for _, child := range s.issues {
		if child.ParentIssueID == issueID {
			children = append(children, s.issueListItemLocked(child))
		}
	}
	sort.Slice(children, func(i, j int) bool {
		if children[i].SortOrder == children[j].SortOrder {
			return children[i].CreatedAt < children[j].CreatedAt
		}
		return children[i].SortOrder < children[j].SortOrder
	})
	return IssueDetail{
		Issue:           issue,
		Project:         project,
		TestEnvironment: s.testEnvironmentPointerLocked(issueID),
		ChildIssues:     children,
		Labels:          s.issueLabels[issueID],
		Comments:        comments,
		Sessions:        s.issueAgentSessionsLocked(workspaceID, issueID),
		Evidence:        []DeploymentEvidence{},
		Failures:        s.issueFailuresLocked(workspaceID, issueID),
		ChangeNodes:     s.issueChangeNodesLocked(workspaceID, issueID),
		ReviewEvidence:  s.issueReviewEvidenceLocked(workspaceID, issueID),
		Handoffs:        s.issueHandoffsLocked(workspaceID, issueID),
	}, nil
}

func (s *MemoryStore) CreateAgentSession(_ Context, userID, workspaceID, issueID string, input CreateAgentSessionInput) (AgentSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.createAgentSessionLocked(userID, workspaceID, issueID, input)
}

func (s *MemoryStore) createAgentSessionLocked(userID, workspaceID, issueID string, input CreateAgentSessionInput) (AgentSession, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return AgentSession{}, ErrNotFound
	}
	workspace, ok := s.workspaceLocked(workspaceID)
	if !ok {
		return AgentSession{}, ErrNotFound
	}
	issue, ok := s.issues[issueID]
	if !ok || issue.WorkspaceID != workspaceID {
		return AgentSession{}, ErrNotFound
	}
	if issue.ProjectID == "" {
		return AgentSession{}, errors.New("attach a project before starting an agent session")
	}
	project, ok := s.projects[issue.ProjectID]
	if !ok || project.WorkspaceID != workspaceID {
		return AgentSession{}, ErrNotFound
	}
	normalized := normalizeCreateAgentSessionInput(input)
	if normalized.Command == "" {
		return AgentSession{}, errors.New("command is required")
	}
	if normalized.Provider != "codex" {
		return AgentSession{}, errors.New("provider must be codex")
	}
	if normalized.RuntimeMode == "" {
		normalized.RuntimeMode = workspace.Kind
	}
	if normalized.RuntimeMode != "personal" && normalized.RuntimeMode != "team" {
		return AgentSession{}, errors.New("runtimeMode must be personal or team")
	}
	sessionID, err := newAgentSessionID()
	if err != nil {
		return AgentSession{}, err
	}
	if normalized.Branch == "" {
		normalized.Branch = defaultAgentSessionBranch(issueID, sessionID)
	}
	comments := []Comment{}
	for _, comment := range s.comments {
		if comment.IssueID == issueID {
			comments = append(comments, comment)
		}
	}
	sort.Slice(comments, func(i, j int) bool {
		return comments[i].CreatedAt > comments[j].CreatedAt
	})
	childIssues := []IssueListItem{}
	for _, child := range s.issues {
		if child.ParentIssueID == issueID {
			childIssues = append(childIssues, s.issueListItemLocked(child))
		}
	}
	runbook := s.projectRunbooks[project.ID]
	payload, err := json.Marshal(buildAgentSessionPayload(sessionID, issue, project, runbook, comments, s.issueLabels[issueID], childIssues, normalized))
	if err != nil {
		return AgentSession{}, err
	}
	capabilities, err := json.Marshal(map[string]bool{"codex": true})
	if err != nil {
		return AgentSession{}, err
	}
	s.nextID++
	now := time.Now().UTC().Format(time.RFC3339Nano)
	task := RuntimeTask{
		ID:                   fmt.Sprintf("runtime-task-%04d", s.nextID),
		WorkspaceID:          workspaceID,
		IssueID:              issue.ID,
		SessionID:            sessionID,
		ProjectID:            project.ID,
		Kind:                 "agent_session",
		Status:               "queued",
		Priority:             0,
		RuntimeMode:          normalized.RuntimeMode,
		RequiredCapabilities: json.RawMessage(capabilities),
		Payload:              json.RawMessage(payload),
		Result:               json.RawMessage(`{}`),
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	s.runtimeTasks[task.ID] = task
	s.appendRuntimeTaskEventLocked(task.WorkspaceID, task.ID, "", userID, "created", json.RawMessage(fmt.Sprintf(`{"kind":%q,"runtimeMode":%q,"status":%q}`, task.Kind, task.RuntimeMode, task.Status)))
	return runtimeTaskToAgentSession(task)
}

func (s *MemoryStore) GetSession(_ Context, userID, workspaceID, sessionID string) (SessionDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	sessionID = strings.TrimSpace(sessionID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return SessionDetail{}, ErrNotFound
	}
	var task RuntimeTask
	for _, candidate := range s.runtimeTasks {
		if candidate.WorkspaceID == workspaceID && candidate.Kind == "agent_session" && (candidate.SessionID == sessionID || candidate.ID == sessionID) {
			task = candidate
			break
		}
	}
	if task.ID == "" {
		return SessionDetail{}, ErrNotFound
	}
	session, err := runtimeTaskToAgentSession(task)
	if err != nil {
		return SessionDetail{}, err
	}
	issue, ok := s.issues[task.IssueID]
	if !ok || issue.WorkspaceID != workspaceID {
		return SessionDetail{}, ErrNotFound
	}
	project := s.projects[task.ProjectID]
	logs := []SessionLog{}
	for _, log := range s.runtimeTaskLogs {
		if log.WorkspaceID != workspaceID || log.TaskID != task.ID {
			continue
		}
		logs = append(logs, SessionLog{ID: log.ID, SessionID: session.ID, Stream: log.Stream, Message: log.Message, CreatedAt: log.CreatedAt})
	}
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].CreatedAt == logs[j].CreatedAt {
			return logs[i].ID < logs[j].ID
		}
		return logs[i].CreatedAt < logs[j].CreatedAt
	})
	return SessionDetail{
		Session:   session,
		Issue:     issue,
		Project:   project,
		Logs:      logs,
		Evidence:  []DeploymentEvidence{},
		Failures:  s.sessionFailuresLocked(workspaceID, task.IssueID, session.ID),
		Workspace: workspaceSnapshotFromRuntimeTask(task),
	}, nil
}

func (s *MemoryStore) StartIssueTestDeploy(_ Context, userID, workspaceID, issueID string, input StartTestDeployInput) (TestEnvironmentSessionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	detail, err := s.getIssueLocked(userID, workspaceID, issueID)
	if err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	if detail.Project.ID == "" {
		return TestEnvironmentSessionResult{}, errors.New("attach a project before starting a test deployment")
	}
	if hasActiveAgentSession(detail.Sessions) {
		return TestEnvironmentSessionResult{}, errors.New("issue already has an active session")
	}
	environment, err := s.buildIssueTestEnvironmentLocked(detail, input)
	if err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	sourceNode, err := selectIssueChangeNodeForDeploy(detail.ChangeNodes, input.SourceCommitSHA, input.SourceSessionID)
	if err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	environment.SourceSessionID = sourceNode.SessionID
	environment.SourceCommitSHA = sourceNode.CommitSHA
	session, err := s.createAgentSessionLocked(userID, workspaceID, issueID, CreateAgentSessionInput{
		Provider:        "codex",
		AgentProfile:    firstNonEmpty(input.AgentProfile, "codex"),
		Command:         buildIssueTestDeployPrompt(detail, environment, sourceNode),
		SourceSessionID: sourceNode.SessionID,
		SourceCommitSHA: sourceNode.CommitSHA,
	})
	if err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	environment.LastDeploySessionID = session.ID
	environment.NamespaceStatus = "deploying"
	s.saveTestEnvironmentLocked(environment)
	return TestEnvironmentSessionResult{SessionID: session.ID, TestEnvironment: environment}, nil
}

func (s *MemoryStore) RequestIssueTestEnvironmentCleanup(_ Context, userID, workspaceID, issueID string, input StartTestDeployInput) (TestEnvironmentSessionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	detail, err := s.getIssueLocked(userID, workspaceID, issueID)
	if err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	if detail.TestEnvironment == nil || strings.TrimSpace(detail.TestEnvironment.Namespace) == "" {
		return TestEnvironmentSessionResult{}, errors.New("issue has no test namespace to clean up")
	}
	if hasActiveAgentSession(detail.Sessions) {
		return TestEnvironmentSessionResult{}, errors.New("issue already has an active session")
	}
	environment := *detail.TestEnvironment
	environment.NamespaceStatus = "cleanup_requested"
	environment.CleanupStatus = "cleanup_requested"
	session, err := s.createAgentSessionLocked(userID, workspaceID, issueID, CreateAgentSessionInput{
		Provider:     "codex",
		AgentProfile: firstNonEmpty(input.AgentProfile, "codex"),
		Command:      buildIssueTestCleanupPrompt(detail, environment),
	})
	if err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	environment.LastCleanupSessionID = session.ID
	s.saveTestEnvironmentLocked(environment)
	return TestEnvironmentSessionResult{SessionID: session.ID, TestEnvironment: environment}, nil
}

func (s *MemoryStore) RetainIssueTestEnvironment(_ Context, userID, workspaceID, issueID string) (IssueTestEnvironment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isWorkspaceMember(strings.TrimSpace(workspaceID), userID) {
		return IssueTestEnvironment{}, ErrNotFound
	}
	environment, ok := s.testEnvironments[strings.TrimSpace(issueID)]
	if !ok {
		return IssueTestEnvironment{}, ErrNotFound
	}
	environment.CleanupStatus = "retained"
	s.saveTestEnvironmentLocked(environment)
	return environment, nil
}

func (s *MemoryStore) GetIssueTestEnvironmentResources(_ Context, userID, workspaceID, issueID string) (IssueTestEnvironmentResources, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isWorkspaceMember(strings.TrimSpace(workspaceID), userID) {
		return IssueTestEnvironmentResources{}, ErrNotFound
	}
	environment, ok := s.testEnvironments[strings.TrimSpace(issueID)]
	if !ok {
		return IssueTestEnvironmentResources{}, ErrNotFound
	}
	clusterName := ""
	if cluster, ok := s.clusters[environment.ClusterID]; ok {
		clusterName = cluster.Name
	}
	return IssueTestEnvironmentResources{
		IssueID:         environment.IssueID,
		ClusterID:       environment.ClusterID,
		ClusterName:     clusterName,
		Context:         environment.KubeContext,
		Namespace:       environment.Namespace,
		NamespaceStatus: environment.NamespaceStatus,
		CleanupStatus:   environment.CleanupStatus,
		ExposureMode:    environment.ExposureMode,
		PreviewURL:      environment.PreviewURL,
		NodeHost:        environment.NodeHost,
		RefreshedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		Pods:            []KubernetesPodResource{},
		Services:        []KubernetesServiceResource{},
		Deployments:     []KubernetesDeploymentResource{},
		Ingresses:       []KubernetesIngressResource{},
		Events:          []KubernetesEventResource{},
		Errors:          []KubernetesResourceFetchError{},
	}, nil
}

func (s *MemoryStore) ProbeIssueTestEnvironment(_ Context, userID, workspaceID, issueID string) (IssueTestEnvironment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isWorkspaceMember(strings.TrimSpace(workspaceID), userID) {
		return IssueTestEnvironment{}, ErrNotFound
	}
	environment, ok := s.testEnvironments[strings.TrimSpace(issueID)]
	if !ok {
		return IssueTestEnvironment{}, ErrNotFound
	}
	if environment.PreviewURL != "" {
		environment.NamespaceStatus = "preview_unverified"
		s.saveTestEnvironmentLocked(environment)
	}
	return environment, nil
}

func (s *MemoryStore) CreateIssuePullRequestHandoff(_ Context, userID, workspaceID, issueID string, input CreatePullRequestInput) (IssueHandoff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	detail, err := s.getIssueLocked(userID, workspaceID, issueID)
	if err != nil {
		return IssueHandoff{}, err
	}
	sourceNode, err := selectIssueChangeNodeForDeploy(detail.ChangeNodes, input.SourceCommitSHA, input.SourceSessionID)
	if err != nil {
		return IssueHandoff{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	handoff := IssueHandoff{
		ID:              fmt.Sprintf("handoff-%04d", s.nextMemoryIDLocked()),
		IssueID:         strings.TrimSpace(issueID),
		SourceSessionID: sourceNode.SessionID,
		SourceCommitSHA: sourceNode.CommitSHA,
		Branch:          sourceNode.Branch,
		HeadCommitSHA:   sourceNode.CommitSHA,
		Commits: []IssueHandoffCommit{{
			SHA:      sourceNode.CommitSHA,
			ShortSHA: shortCommitSHA(sourceNode.CommitSHA),
			Subject:  sourceNode.Subject,
		}},
		Kind:            "pr",
		PRTitle:         firstNonEmpty(input.Title, sourceNode.Subject, detail.Issue.Title),
		EvidenceSummary: issueHandoffEvidenceSummary(detail, sourceNode.SessionID, sourceNode.CommitSHA),
		CreatedVia:      "server",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	s.handoffs[handoff.ID] = handoff
	return handoff, nil
}

func (s *MemoryStore) RefreshIssueHandoff(_ Context, userID, workspaceID, issueID, handoffID string) (IssueHandoff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isWorkspaceMember(strings.TrimSpace(workspaceID), userID) {
		return IssueHandoff{}, ErrNotFound
	}
	handoff, ok := s.handoffs[strings.TrimSpace(handoffID)]
	if !ok || handoff.IssueID != strings.TrimSpace(issueID) {
		return IssueHandoff{}, ErrNotFound
	}
	handoff.LastCheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
	handoff.Error = "GitHub PR sync is server-owned but no GitHub App PR executor is configured yet."
	handoff.UpdatedAt = handoff.LastCheckedAt
	s.handoffs[handoff.ID] = handoff
	return handoff, nil
}

func (s *MemoryStore) issueAgentSessionsLocked(workspaceID, issueID string) []AgentSession {
	sessions := []AgentSession{}
	for _, task := range s.runtimeTasks {
		if task.WorkspaceID != workspaceID || task.IssueID != issueID || task.Kind != "agent_session" {
			continue
		}
		session, err := runtimeTaskToAgentSession(task)
		if err != nil {
			continue
		}
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].CreatedAt == sessions[j].CreatedAt {
			return sessions[i].ID > sessions[j].ID
		}
		return sessions[i].CreatedAt > sessions[j].CreatedAt
	})
	return sessions
}

func (s *MemoryStore) issueChangeNodesLocked(workspaceID, issueID string) []IssueChangeNode {
	nodes := []IssueChangeNode{}
	for _, task := range s.runtimeTasks {
		if task.WorkspaceID != workspaceID || task.IssueID != issueID || task.Kind != "agent_session" {
			continue
		}
		node := runtimeTaskChangeNode(task)
		if node.CommitSHA == "" && node.Error == "" {
			continue
		}
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].CreatedAt == nodes[j].CreatedAt {
			return nodes[i].ID > nodes[j].ID
		}
		return nodes[i].CreatedAt > nodes[j].CreatedAt
	})
	return nodes
}

func (s *MemoryStore) issueReviewEvidenceLocked(workspaceID, issueID string) []SessionReviewEvidence {
	reviews := []SessionReviewEvidence{}
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	for _, review := range s.reviewEvidence {
		issue := s.issues[review.IssueID]
		if issue.WorkspaceID == workspaceID && review.IssueID == issueID {
			reviews = append(reviews, review)
		}
	}
	sort.Slice(reviews, func(i, j int) bool {
		if reviews[i].UpdatedAt == reviews[j].UpdatedAt {
			return reviews[i].CreatedAt > reviews[j].CreatedAt
		}
		return reviews[i].UpdatedAt > reviews[j].UpdatedAt
	})
	return reviews
}

func (s *MemoryStore) issueFailuresLocked(workspaceID, issueID string) []SessionFailure {
	failures := []SessionFailure{}
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	for _, failure := range s.sessionFailures {
		issue := s.issues[failure.IssueID]
		if issue.WorkspaceID == workspaceID && failure.IssueID == issueID {
			failures = append(failures, failure)
		}
	}
	sort.Slice(failures, func(i, j int) bool {
		if failures[i].UpdatedAt == failures[j].UpdatedAt {
			return failures[i].CreatedAt > failures[j].CreatedAt
		}
		return failures[i].UpdatedAt > failures[j].UpdatedAt
	})
	return failures
}

func (s *MemoryStore) sessionFailuresLocked(workspaceID, issueID, sessionID string) []SessionFailure {
	failures := []SessionFailure{}
	for _, failure := range s.issueFailuresLocked(workspaceID, issueID) {
		if failure.SessionID == sessionID {
			failures = append(failures, failure)
		}
	}
	return failures
}

func (s *MemoryStore) workspaceSettingsLocked(workspaceID string) WorkspaceSettings {
	settings := s.workspaceSettings[workspaceID]
	if settings.CreatedAt == "" {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		settings = WorkspaceSettings{CreatedAt: now, UpdatedAt: now}
		s.workspaceSettings[workspaceID] = settings
	}
	return settings
}

func (s *MemoryStore) seedDefaultAgentProfilesLocked(workspaceID string) {
	for _, profile := range defaultAgentProfiles(time.Now().UTC()) {
		key := workspaceID + ":" + profile.ID
		if _, ok := s.agentProfiles[key]; ok {
			continue
		}
		stored := profile
		stored.ID = key
		s.agentProfiles[key] = stored
	}
}

func (s *MemoryStore) agentProfileLocked(workspaceID, value string) (AgentProfile, string, bool) {
	keyValue := agentProfileLookupKey(value)
	mention := "@" + keyValue
	for key, profile := range s.agentProfiles {
		if !strings.HasPrefix(key, workspaceID+":") {
			continue
		}
		id := strings.TrimPrefix(key, workspaceID+":")
		if strings.EqualFold(id, keyValue) || strings.EqualFold(profile.Mention, mention) {
			item := profile
			item.ID = id
			return item, key, true
		}
	}
	return AgentProfile{}, "", false
}

func (s *MemoryStore) testEnvironmentPointerLocked(issueID string) *IssueTestEnvironment {
	environment, ok := s.testEnvironments[strings.TrimSpace(issueID)]
	if !ok {
		return nil
	}
	return &environment
}

func (s *MemoryStore) issueHandoffsLocked(workspaceID, issueID string) []IssueHandoff {
	handoffs := []IssueHandoff{}
	for _, handoff := range s.handoffs {
		issue := s.issues[handoff.IssueID]
		if issue.WorkspaceID == strings.TrimSpace(workspaceID) && handoff.IssueID == strings.TrimSpace(issueID) {
			handoffs = append(handoffs, handoff)
		}
	}
	sort.Slice(handoffs, func(i, j int) bool {
		if handoffs[i].UpdatedAt == handoffs[j].UpdatedAt {
			return handoffs[i].CreatedAt > handoffs[j].CreatedAt
		}
		return handoffs[i].UpdatedAt > handoffs[j].UpdatedAt
	})
	return handoffs
}

func (s *MemoryStore) buildIssueTestEnvironmentLocked(detail IssueDetail, input StartTestDeployInput) (IssueTestEnvironment, error) {
	environment := IssueTestEnvironment{}
	if detail.TestEnvironment != nil {
		environment = *detail.TestEnvironment
	}
	environment.IssueID = detail.Issue.ID
	if environment.Namespace == "" {
		environment.Namespace = defaultIssueNamespace(detail)
	}
	clusterID := firstNonEmpty(input.ClusterID, environment.ClusterID, detail.Project.DefaultClusterID)
	if clusterID == "" {
		return environment, errors.New("cluster is required before starting a test deployment")
	}
	cluster, ok := s.clusters[clusterID]
	if !ok || cluster.WorkspaceID != detail.Issue.WorkspaceID {
		return environment, ErrNotFound
	}
	exposureMode, err := normalizeExposureMode(input.ExposureMode)
	if err != nil {
		return environment, err
	}
	if exposureMode == "" {
		exposureMode = firstNonEmpty(cluster.ExposureMode, "nodeport")
	}
	environment.NamespaceStatus = "planned"
	environment.CleanupStatus = "retained"
	environment.ClusterID = cluster.ID
	environment.KubeconfigPath = cluster.KubeconfigPath
	environment.KubeContext = cluster.KubeContext
	environment.ImageRegistryPrefix = cluster.ImageRegistryPrefix
	environment.ExposureMode = exposureMode
	environment.NodeHost = firstNonEmpty(input.NodeHost, cluster.NodeHost)
	if exposureMode == "ingress" {
		environment.PreviewDomain = firstNonEmpty(input.PreviewDomain, cluster.PreviewDomain)
		environment.IngressClass = firstNonEmpty(input.IngressClass, cluster.IngressClass)
	} else {
		environment.PreviewDomain = ""
		environment.IngressClass = ""
	}
	return environment, nil
}

func (s *MemoryStore) saveTestEnvironmentLocked(environment IssueTestEnvironment) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if environment.CreatedAt == "" {
		environment.CreatedAt = now
	}
	environment.UpdatedAt = now
	s.testEnvironments[environment.IssueID] = environment
}

func (s *MemoryStore) reconcileAgentSessionRuntimeResultLocked(task RuntimeTask) {
	var artifacts RuntimeTaskArtifactResult
	_ = json.Unmarshal(task.Result, &artifacts)
	session, err := runtimeTaskToAgentSession(task)
	if err != nil {
		return
	}
	switch task.Status {
	case "completed":
		s.reconcileSuccessfulIssueTestEnvironmentLocked(task, session, artifacts)
	case "failed":
		s.reconcileFailedIssueTestEnvironmentLocked(task)
	case "cancelled":
		s.markIssueTestEnvironmentInterruptedLocked(task)
	}
	if artifacts.ReviewEvidence != nil {
		s.storeRuntimeReviewEvidenceLocked(task, session, *artifacts.ReviewEvidence)
	}
	if task.Status == "failed" || task.Status == "cancelled" {
		s.storeRuntimeSessionFailureLocked(task, session)
	}
}

func (s *MemoryStore) reconcileSuccessfulIssueTestEnvironmentLocked(task RuntimeTask, session AgentSession, artifacts RuntimeTaskArtifactResult) {
	environment, ok := s.testEnvironments[strings.TrimSpace(task.IssueID)]
	if !ok {
		return
	}
	previewURL := ""
	if artifacts.TestEnvironment != nil {
		previewURL = firstNonEmpty(artifacts.TestEnvironment.PreviewURL, artifacts.TestEnvironment.PreviewURLSnake, artifacts.TestEnvironment.URL)
	}
	changed := false
	switch {
	case environment.LastDeploySessionID == session.ID:
		environment.NamespaceStatus = "active"
		environment.CleanupStatus = "retained"
		if previewURL != "" {
			environment.PreviewURL = previewURL
		}
		if issue, ok := s.issues[task.IssueID]; ok {
			issue.Status = "ready_for_test"
			issue.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			s.issues[issue.ID] = issue
		}
		changed = true
	case environment.LastCleanupSessionID == session.ID:
		environment.NamespaceStatus = "cleaned"
		environment.CleanupStatus = "cleaned"
		changed = true
	case previewURL != "" && environment.LastCleanupSessionID != session.ID:
		environment.PreviewURL = previewURL
		environment.NamespaceStatus = "active"
		environment.CleanupStatus = "retained"
		environment.LastDeploySessionID = session.ID
		environment.SourceSessionID = firstNonEmpty(session.SourceSessionID, environment.SourceSessionID)
		environment.SourceCommitSHA = firstNonEmpty(session.SourceCommitSHA, environment.SourceCommitSHA)
		if issue, ok := s.issues[task.IssueID]; ok {
			issue.Status = "ready_for_test"
			issue.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			s.issues[issue.ID] = issue
		}
		changed = true
	}
	if changed {
		s.saveTestEnvironmentLocked(environment)
	}
}

func (s *MemoryStore) reconcileFailedIssueTestEnvironmentLocked(task RuntimeTask) {
	environment, ok := s.testEnvironments[strings.TrimSpace(task.IssueID)]
	if !ok {
		return
	}
	sessionID := firstNonEmpty(task.SessionID, task.ID)
	changed := false
	switch {
	case environment.LastDeploySessionID == sessionID:
		environment.NamespaceStatus = "deploy_failed"
		changed = true
	case environment.LastCleanupSessionID == sessionID:
		environment.NamespaceStatus = "cleanup_failed"
		environment.CleanupStatus = "cleanup_failed"
		changed = true
	}
	if !changed {
		return
	}
	s.saveTestEnvironmentLocked(environment)
	if issue, ok := s.issues[task.IssueID]; ok {
		issue.Status = "blocked"
		issue.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		s.issues[issue.ID] = issue
	}
}

func (s *MemoryStore) markIssueTestEnvironmentInterruptedLocked(task RuntimeTask) {
	environment, ok := s.testEnvironments[strings.TrimSpace(task.IssueID)]
	if !ok {
		return
	}
	sessionID := firstNonEmpty(task.SessionID, task.ID)
	changed := false
	switch {
	case environment.LastDeploySessionID == sessionID:
		environment.NamespaceStatus = "deploy_interrupted"
		changed = true
	case environment.LastCleanupSessionID == sessionID:
		environment.NamespaceStatus = "cleanup_failed"
		environment.CleanupStatus = "cleanup_failed"
		changed = true
	}
	if !changed {
		return
	}
	s.saveTestEnvironmentLocked(environment)
	if issue, ok := s.issues[task.IssueID]; ok {
		issue.Status = "blocked"
		issue.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		s.issues[issue.ID] = issue
	}
}

func (s *MemoryStore) storeRuntimeReviewEvidenceLocked(task RuntimeTask, session AgentSession, artifact SessionReviewEvidenceArtifact) {
	if strings.TrimSpace(task.IssueID) == "" {
		return
	}
	review := buildRuntimeReviewEvidence(task, session, artifact)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	existing := s.reviewEvidence[session.ID]
	if existing.ID == "" {
		existing.ID = fmt.Sprintf("review-evidence-%04d", s.nextMemoryIDLocked())
		existing.CreatedAt = now
	}
	review.ID = existing.ID
	review.CreatedAt = existing.CreatedAt
	review.UpdatedAt = now
	s.reviewEvidence[session.ID] = review
}

func (s *MemoryStore) storeRuntimeSessionFailureLocked(task RuntimeTask, session AgentSession) {
	if strings.TrimSpace(task.IssueID) == "" {
		return
	}
	failure := buildRuntimeSessionFailure(task, session)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	existing := s.sessionFailures[session.ID]
	if existing.ID == "" {
		existing.ID = fmt.Sprintf("session-failure-%04d", s.nextMemoryIDLocked())
		existing.CreatedAt = now
	}
	failure.ID = existing.ID
	failure.CreatedAt = existing.CreatedAt
	failure.UpdatedAt = now
	s.sessionFailures[session.ID] = failure
}

func (s *MemoryStore) nextMemoryIDLocked() int {
	s.nextID++
	return s.nextID
}

func (s *MemoryStore) UpdateIssue(_ Context, userID, workspaceID, issueID string, input UpdateIssueInput) (Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isWorkspaceMember(workspaceID, userID) {
		return Issue{}, ErrNotFound
	}
	issue, ok := s.issues[strings.TrimSpace(issueID)]
	if !ok || issue.WorkspaceID != strings.TrimSpace(workspaceID) {
		return Issue{}, ErrNotFound
	}
	if input.ProjectID != nil {
		projectID := strings.TrimSpace(*input.ProjectID)
		if projectID != "" {
			project, err := s.resolveIssueProjectLocked(issue.WorkspaceID, projectID, "")
			if err != nil {
				return Issue{}, err
			}
			issue.ProjectID = project.ID
		} else {
			issue.ProjectID = ""
		}
	}
	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" {
			return Issue{}, errors.New("issue title is required")
		}
		issue.Title = title
	}
	if input.Body != nil {
		issue.Body = strings.TrimSpace(*input.Body)
	}
	if input.Status != nil {
		status := normalizeIssueStatus(*input.Status)
		if err := validateIssueStatus(status); err != nil {
			return Issue{}, err
		}
		issue.Status = status
	}
	issue.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.issues[issue.ID] = issue
	if input.ProjectID != nil && issue.ParentIssueID == "" {
		for id, child := range s.issues {
			if child.WorkspaceID == issue.WorkspaceID && child.ParentIssueID == issue.ID {
				child.ProjectID = issue.ProjectID
				child.UpdatedAt = issue.UpdatedAt
				s.issues[id] = child
			}
		}
	}
	return issue, nil
}

func (s *MemoryStore) CreateIssueTask(_ Context, userID, workspaceID, issueID string, input IssueTaskInput) (IssueListItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isWorkspaceMember(workspaceID, userID) {
		return IssueListItem{}, ErrNotFound
	}
	parent, ok := s.issues[strings.TrimSpace(issueID)]
	if !ok || parent.WorkspaceID != strings.TrimSpace(workspaceID) {
		return IssueListItem{}, ErrNotFound
	}
	task := normalizeIssueTaskInputs([]IssueTaskInput{input})
	if len(task) == 0 {
		return IssueListItem{}, errors.New("task title is required")
	}
	sortOrder := 1
	for _, issue := range s.issues {
		if issue.ParentIssueID == parent.ID && issue.SortOrder >= sortOrder {
			sortOrder = issue.SortOrder + 1
		}
	}
	s.nextID++
	now := time.Now().UTC().Format(time.RFC3339Nano)
	child := Issue{
		ID:             fmt.Sprintf("issue-%04d", s.nextID),
		WorkspaceID:    parent.WorkspaceID,
		ProjectID:      parent.ProjectID,
		ParentIssueID:  parent.ID,
		SortOrder:      sortOrder,
		Title:          task[0].Title,
		Body:           task[0].Body,
		Status:         normalizeIssueStatus(task[0].Status),
		TriageStatus:   "none",
		Assignee:       parent.CreatorName,
		AssigneeType:   "human",
		CreatorName:    parent.CreatorName,
		CreatorAvatar:  parent.CreatorAvatar,
		EnvironmentURL: "",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	s.issues[child.ID] = child
	return s.issueListItemLocked(child), nil
}

func (s *MemoryStore) DeleteIssueTask(_ Context, userID, workspaceID, issueID, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isWorkspaceMember(workspaceID, userID) {
		return ErrNotFound
	}
	task, ok := s.issues[strings.TrimSpace(taskID)]
	if !ok || task.WorkspaceID != strings.TrimSpace(workspaceID) || task.ParentIssueID != strings.TrimSpace(issueID) {
		return ErrNotFound
	}
	delete(s.issues, task.ID)
	return nil
}

func (s *MemoryStore) UpdateIssueLabels(_ Context, userID, workspaceID, issueID string, input UpdateIssueLabelsInput) ([]IssueLabel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	issue, ok := s.issues[strings.TrimSpace(issueID)]
	if !ok || issue.WorkspaceID != strings.TrimSpace(workspaceID) {
		return nil, ErrNotFound
	}
	labels, err := normalizeIssueLabelKeys(append(input.LabelKeys, input.Labels...))
	if err != nil {
		return nil, err
	}
	s.issueLabels[issue.ID] = labels
	if hasIssueLabelDimension(labels, issueLabelDimensionType) {
		issue.TriageStatus = "classified"
	} else {
		issue.TriageStatus = "none"
	}
	issue.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.issues[issue.ID] = issue
	return labels, nil
}

func (s *MemoryStore) ApplyIssueTypeClassification(_ Context, workspaceID, issueID string, labelKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	issue, ok := s.issues[strings.TrimSpace(issueID)]
	if !ok || issue.WorkspaceID != strings.TrimSpace(workspaceID) {
		return ErrNotFound
	}
	if issue.TriageStatus != "pending" {
		return nil
	}
	labels, err := normalizeIssueLabelKeys([]string{labelKey})
	if err != nil {
		return err
	}
	if !hasIssueLabelDimension(labels, issueLabelDimensionType) {
		return errors.New("issue type label is required")
	}
	s.issueLabels[issue.ID] = replaceIssueLabelDimension(s.issueLabels[issue.ID], issueLabelDimensionType, labels[0])
	issue.TriageStatus = "classified"
	issue.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.issues[issue.ID] = issue
	return nil
}

func (s *MemoryStore) MarkIssueTriageFailed(_ Context, workspaceID, issueID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	issue, ok := s.issues[strings.TrimSpace(issueID)]
	if !ok || issue.WorkspaceID != strings.TrimSpace(workspaceID) {
		return ErrNotFound
	}
	if issue.TriageStatus == "pending" {
		issue.TriageStatus = "failed"
		issue.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		s.issues[issue.ID] = issue
	}
	return nil
}

func (s *MemoryStore) AddComment(_ Context, user User, workspaceID, issueID string, input CreateCommentInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isWorkspaceMember(workspaceID, user.ID) {
		return "", ErrNotFound
	}
	issue, ok := s.issues[strings.TrimSpace(issueID)]
	if !ok || issue.WorkspaceID != strings.TrimSpace(workspaceID) {
		return "", ErrNotFound
	}
	body := strings.TrimSpace(input.Body)
	if body == "" {
		return "", errors.New("comment body is required")
	}
	s.nextID++
	now := time.Now().UTC().Format(time.RFC3339Nano)
	commentID := fmt.Sprintf("comment-%04d", s.nextID)
	s.comments[commentID] = Comment{
		ID:           commentID,
		IssueID:      issue.ID,
		AuthorType:   "human",
		AuthorUserID: user.ID,
		AuthorName:   firstNonEmpty(strings.TrimSpace(input.AuthorName), user.Name),
		AuthorAvatar: firstNonEmpty(strings.TrimSpace(input.AuthorAvatar), user.AvatarURL),
		Body:         body,
		CreatedAt:    now,
		UpdatedAt:    now,
		Reactions:    []CommentReactionSummary{},
	}
	return commentID, nil
}

func (s *MemoryStore) UpdateComment(_ Context, user User, workspaceID, issueID, commentID string, input UpdateCommentInput) (Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isWorkspaceMember(workspaceID, user.ID) {
		return Comment{}, ErrNotFound
	}
	comment, ok := s.comments[strings.TrimSpace(commentID)]
	if !ok || comment.IssueID != strings.TrimSpace(issueID) || s.issues[comment.IssueID].WorkspaceID != strings.TrimSpace(workspaceID) {
		return Comment{}, ErrNotFound
	}
	if comment.AuthorUserID != user.ID || comment.AuthorType != "human" {
		return Comment{}, ErrForbidden
	}
	body := strings.TrimSpace(input.Body)
	if body == "" {
		return Comment{}, errors.New("comment body is required")
	}
	comment.Body = body
	now := time.Now().UTC().Format(time.RFC3339Nano)
	comment.UpdatedAt = now
	comment.EditedAt = now
	comment.Reactions = s.commentReactionSummariesLocked(comment.ID, user.ID)
	s.comments[comment.ID] = comment
	return comment, nil
}

func (s *MemoryStore) CreateIssueAttachment(_ Context, userID, workspaceID, issueID string, input CreateIssueAttachmentInput) (IssueAttachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return IssueAttachment{}, ErrNotFound
	}
	issue, ok := s.issues[issueID]
	if !ok || issue.WorkspaceID != workspaceID {
		return IssueAttachment{}, ErrNotFound
	}
	filename := truncateString(strings.TrimSpace(input.Filename), 240)
	if filename == "" {
		filename = "image"
	}
	contentType := normalizeIssueAttachmentContentType(input.ContentType, input.Content)
	if !allowedIssueAttachmentContentType(contentType) {
		return IssueAttachment{}, errors.New("unsupported attachment content type")
	}
	if len(input.Content) == 0 {
		return IssueAttachment{}, errors.New("attachment content is required")
	}
	if len(input.Content) > maxIssueAttachmentBytes {
		return IssueAttachment{}, errors.New("attachment exceeds maximum size")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	attachment := IssueAttachment{
		ID:             fmt.Sprintf("attachment-%04d", s.nextMemoryIDLocked()),
		WorkspaceID:    workspaceID,
		IssueID:        issue.ID,
		Filename:       filename,
		ContentType:    contentType,
		SizeBytes:      int64(len(input.Content)),
		StorageBackend: "memory_blob",
		Content:        append([]byte(nil), input.Content...),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	s.attachments[attachment.ID] = attachment
	return attachment, nil
}

func (s *MemoryStore) GetIssueAttachment(_ Context, userID, attachmentID string) (IssueAttachment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	attachment, ok := s.attachments[strings.TrimSpace(attachmentID)]
	if !ok {
		return IssueAttachment{}, ErrNotFound
	}
	if !s.isWorkspaceMember(attachment.WorkspaceID, strings.TrimSpace(userID)) {
		return IssueAttachment{}, ErrNotFound
	}
	return attachment, nil
}

func (s *MemoryStore) SetCommentReaction(_ Context, user User, workspaceID, issueID, commentID, reaction string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isWorkspaceMember(workspaceID, user.ID) {
		return ErrNotFound
	}
	if err := validateCommentReaction(reaction); err != nil {
		return err
	}
	comment, ok := s.comments[strings.TrimSpace(commentID)]
	if !ok || comment.IssueID != strings.TrimSpace(issueID) || s.issues[comment.IssueID].WorkspaceID != strings.TrimSpace(workspaceID) {
		return ErrNotFound
	}
	key := comment.ID + ":" + strings.TrimSpace(reaction)
	if s.commentReactions[key] == nil {
		s.commentReactions[key] = map[string]CommentReactionSummary{}
	}
	s.commentReactions[key][user.ID] = CommentReactionSummary{Reaction: strings.TrimSpace(reaction), Count: 1, ReactedByMe: true}
	return nil
}

func (s *MemoryStore) DeleteCommentReaction(_ Context, userID, workspaceID, issueID, commentID, reaction string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isWorkspaceMember(workspaceID, userID) {
		return ErrNotFound
	}
	comment, ok := s.comments[strings.TrimSpace(commentID)]
	if !ok || comment.IssueID != strings.TrimSpace(issueID) || s.issues[comment.IssueID].WorkspaceID != strings.TrimSpace(workspaceID) {
		return ErrNotFound
	}
	delete(s.commentReactions[comment.ID+":"+strings.TrimSpace(reaction)], strings.TrimSpace(userID))
	return nil
}

func (s *MemoryStore) ListRuntimeTasks(_ Context, userID, workspaceID string) ([]RuntimeTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	tasks := []RuntimeTask{}
	for _, task := range s.runtimeTasks {
		if task.WorkspaceID == workspaceID {
			tasks = append(tasks, task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].CreatedAt == tasks[j].CreatedAt {
			return tasks[i].ID > tasks[j].ID
		}
		return tasks[i].CreatedAt > tasks[j].CreatedAt
	})
	if len(tasks) > 100 {
		tasks = tasks[:100]
	}
	return tasks, nil
}

func (s *MemoryStore) ListRuntimeTaskEvents(_ Context, userID, workspaceID, taskID string) ([]RuntimeTaskEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	taskID = strings.TrimSpace(taskID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	task, ok := s.runtimeTasks[taskID]
	if !ok || task.WorkspaceID != workspaceID {
		return nil, ErrNotFound
	}
	events := []RuntimeTaskEvent{}
	for _, event := range s.runtimeTaskEvents {
		if event.WorkspaceID == workspaceID && event.TaskID == taskID {
			events = append(events, event)
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].CreatedAt == events[j].CreatedAt {
			return events[i].ID < events[j].ID
		}
		return events[i].CreatedAt < events[j].CreatedAt
	})
	return events, nil
}

func (s *MemoryStore) ListRuntimeTaskLogs(_ Context, userID, workspaceID, taskID string) ([]RuntimeTaskLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	taskID = strings.TrimSpace(taskID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	task, ok := s.runtimeTasks[taskID]
	if !ok || task.WorkspaceID != workspaceID {
		return nil, ErrNotFound
	}
	logs := []RuntimeTaskLog{}
	for _, log := range s.runtimeTaskLogs {
		if log.WorkspaceID == workspaceID && log.TaskID == taskID {
			logs = append(logs, log)
		}
	}
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].CreatedAt == logs[j].CreatedAt {
			return logs[i].ID < logs[j].ID
		}
		return logs[i].CreatedAt < logs[j].CreatedAt
	})
	return logs, nil
}

func (s *MemoryStore) CancelRuntimeTask(_ Context, userID, workspaceID, taskID string, input CancelRuntimeTaskInput) (RuntimeTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	taskID = strings.TrimSpace(taskID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return RuntimeTask{}, ErrNotFound
	}
	task, ok := s.runtimeTasks[taskID]
	if !ok || task.WorkspaceID != workspaceID {
		return RuntimeTask{}, ErrNotFound
	}
	if task.Status != "queued" && task.Status != "claimed" && task.Status != "running" {
		return RuntimeTask{}, ErrNotFound
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	reason := normalizeRuntimeTaskCancelReason(input.Reason)
	task.Status = "cancelled"
	if task.StartedAt == "" && (task.ClaimedAt != "" || task.ClaimedByWorkerID != "") {
		task.StartedAt = now
	}
	task.FinishedAt = now
	task.Error = reason
	task.UpdatedAt = now
	s.runtimeTasks[task.ID] = task
	s.appendRuntimeTaskEventLocked(task.WorkspaceID, task.ID, "", userID, "cancel_requested", json.RawMessage(fmt.Sprintf(`{"status":%q,"reason":%q}`, task.Status, reason)))
	return task, nil
}

func (s *MemoryStore) ClaimRuntimeTask(_ Context, registration RuntimeRegistration, workerID string) (*RuntimeTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workerID = strings.TrimSpace(workerID)
	if registration.WorkspaceID == "" || workerID == "" {
		return nil, ErrNotFound
	}
	worker, ok := s.runtimeWorkerByID(registration.WorkspaceID, workerID)
	if !ok {
		return nil, ErrNotFound
	}
	if worker.Status != "online" {
		return nil, ErrForbidden
	}
	candidates := []RuntimeTask{}
	for _, task := range s.runtimeTasks {
		if task.WorkspaceID != registration.WorkspaceID ||
			task.Status != "queued" ||
			task.RuntimeMode != worker.Mode ||
			!jsonObjectContains(worker.Capabilities, task.RequiredCapabilities) {
			continue
		}
		candidates = append(candidates, task)
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority == candidates[j].Priority {
			if candidates[i].CreatedAt == candidates[j].CreatedAt {
				return candidates[i].ID < candidates[j].ID
			}
			return candidates[i].CreatedAt < candidates[j].CreatedAt
		}
		return candidates[i].Priority > candidates[j].Priority
	})
	task := s.runtimeTasks[candidates[0].ID]
	now := time.Now().UTC().Format(time.RFC3339Nano)
	task.Status = "claimed"
	task.ClaimedByWorkerID = worker.ID
	task.ClaimedAt = now
	task.UpdatedAt = now
	s.runtimeTasks[task.ID] = task
	s.appendRuntimeTaskEventLocked(task.WorkspaceID, task.ID, worker.ID, "", "claimed", json.RawMessage(fmt.Sprintf(`{"status":%q}`, task.Status)))
	return &task, nil
}

func (s *MemoryStore) GetRuntimeTaskForWorker(_ Context, registration RuntimeRegistration, workerID, taskID string) (RuntimeTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workerID = strings.TrimSpace(workerID)
	taskID = strings.TrimSpace(taskID)
	if registration.WorkspaceID == "" || workerID == "" || taskID == "" {
		return RuntimeTask{}, ErrNotFound
	}
	worker, ok := s.runtimeWorkerByID(registration.WorkspaceID, workerID)
	if !ok {
		return RuntimeTask{}, ErrNotFound
	}
	task, ok := s.runtimeTasks[taskID]
	if !ok || task.WorkspaceID != registration.WorkspaceID || task.ClaimedByWorkerID != worker.ID {
		return RuntimeTask{}, ErrNotFound
	}
	return task, nil
}

func (s *MemoryStore) AppendRuntimeTaskLog(_ Context, registration RuntimeRegistration, workerID, taskID string, input AppendRuntimeTaskLogInput) (RuntimeTaskLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workerID = strings.TrimSpace(workerID)
	taskID = strings.TrimSpace(taskID)
	if registration.WorkspaceID == "" || workerID == "" || taskID == "" {
		return RuntimeTaskLog{}, ErrNotFound
	}
	worker, ok := s.runtimeWorkerByID(registration.WorkspaceID, workerID)
	if !ok {
		return RuntimeTaskLog{}, ErrNotFound
	}
	task, ok := s.runtimeTasks[taskID]
	if !ok || task.WorkspaceID != registration.WorkspaceID || task.ClaimedByWorkerID != worker.ID {
		return RuntimeTaskLog{}, ErrNotFound
	}
	normalized, err := normalizeAppendRuntimeTaskLogInput(input)
	if err != nil {
		return RuntimeTaskLog{}, err
	}
	s.nextID++
	now := time.Now().UTC().Format(time.RFC3339Nano)
	log := RuntimeTaskLog{
		ID:          fmt.Sprintf("runtime-task-log-%04d", s.nextID),
		WorkspaceID: task.WorkspaceID,
		TaskID:      task.ID,
		WorkerID:    worker.ID,
		Stream:      normalized.Stream,
		Message:     normalized.Message,
		CreatedAt:   now,
	}
	s.runtimeTaskLogs[log.ID] = log
	return log, nil
}

func (s *MemoryStore) UpdateRuntimeTaskStatus(_ Context, registration RuntimeRegistration, workerID, taskID string, input UpdateRuntimeTaskStatusInput) (RuntimeTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workerID = strings.TrimSpace(workerID)
	taskID = strings.TrimSpace(taskID)
	if registration.WorkspaceID == "" || workerID == "" || taskID == "" {
		return RuntimeTask{}, ErrNotFound
	}
	worker, ok := s.runtimeWorkerByID(registration.WorkspaceID, workerID)
	if !ok {
		return RuntimeTask{}, ErrNotFound
	}
	normalized, resultProvided, err := normalizeUpdateRuntimeTaskStatusInput(input)
	if err != nil {
		return RuntimeTask{}, err
	}
	task, ok := s.runtimeTasks[taskID]
	if !ok ||
		task.WorkspaceID != registration.WorkspaceID ||
		task.ClaimedByWorkerID != worker.ID ||
		(task.Status != "claimed" && task.Status != "running") {
		return RuntimeTask{}, ErrNotFound
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	task.Status = normalized.Status
	if task.StartedAt == "" && (normalized.Status == "running" || isFinalRuntimeTaskStatus(normalized.Status)) {
		task.StartedAt = now
	}
	if isFinalRuntimeTaskStatus(normalized.Status) {
		task.FinishedAt = now
	}
	if resultProvided {
		task.Result = copyRawMessage(normalized.Result)
	}
	task.Error = normalized.Error
	task.UpdatedAt = now
	s.runtimeTasks[task.ID] = task
	s.appendRuntimeTaskEventLocked(task.WorkspaceID, task.ID, worker.ID, "", "status_changed", json.RawMessage(fmt.Sprintf(`{"status":%q,"error":%q}`, task.Status, task.Error)))
	if isFinalRuntimeTaskStatus(task.Status) && task.Kind == "agent_session" {
		s.reconcileAgentSessionRuntimeResultLocked(task)
	}
	return task, nil
}

func (s *MemoryStore) appendRuntimeTaskEventLocked(workspaceID, taskID, workerID, actorUserID, kind string, payload json.RawMessage) {
	s.nextID++
	now := time.Now().UTC().Format(time.RFC3339Nano)
	event := RuntimeTaskEvent{
		ID:          fmt.Sprintf("runtime-task-event-%04d", s.nextID),
		WorkspaceID: strings.TrimSpace(workspaceID),
		TaskID:      strings.TrimSpace(taskID),
		WorkerID:    strings.TrimSpace(workerID),
		ActorUserID: strings.TrimSpace(actorUserID),
		Kind:        strings.TrimSpace(kind),
		Payload:     copyRawMessage(payload),
		CreatedAt:   now,
	}
	s.runtimeTaskEvents[event.ID] = event
}

func (s *MemoryStore) isWorkspaceMember(workspaceID, userID string) bool {
	return s.workspaceMembers[strings.TrimSpace(workspaceID)][strings.TrimSpace(userID)] != ""
}

func (s *MemoryStore) resolveIssueProjectLocked(workspaceID, projectID, text string) (Project, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID != "" {
		project, ok := s.projects[projectID]
		if !ok || project.WorkspaceID != workspaceID {
			return Project{}, ErrNotFound
		}
		return project, nil
	}
	var best Project
	bestScore := -1
	for _, project := range s.projects {
		if project.WorkspaceID != workspaceID {
			continue
		}
		score := issueProjectScore(project, text)
		if best.ID == "" || score > bestScore {
			best = project
			bestScore = score
		}
	}
	if best.ID == "" {
		return Project{}, errors.New("create a project before creating issues")
	}
	return best, nil
}

func (s *MemoryStore) resolveOptionalIssueProjectIDLocked(workspaceID, projectID, text string) (string, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID != "" {
		project, err := s.resolveIssueProjectLocked(workspaceID, projectID, text)
		if err != nil {
			return "", err
		}
		return project.ID, nil
	}
	projectCount := 0
	for _, project := range s.projects {
		if project.WorkspaceID == workspaceID {
			projectCount++
		}
	}
	if projectCount == 0 || projectCount > 1 {
		return "", nil
	}
	project, err := s.resolveIssueProjectLocked(workspaceID, "", text)
	if err != nil {
		if strings.Contains(err.Error(), "create a project before creating issues") {
			return "", nil
		}
		return "", err
	}
	return project.ID, nil
}

func (s *MemoryStore) issueListItemLocked(issue Issue) IssueListItem {
	project := s.projects[issue.ProjectID]
	projectName := project.Name
	item := IssueListItem{
		ID:            issue.ID,
		WorkspaceID:   issue.WorkspaceID,
		ProjectID:     issue.ProjectID,
		ProjectName:   projectName,
		ParentIssueID: issue.ParentIssueID,
		SortOrder:     issue.SortOrder,
		Title:         issue.Title,
		Body:          issue.Body,
		Status:        issue.Status,
		CloseReason:   issue.CloseReason,
		TriageStatus:  issue.TriageStatus,
		Assignee:      issue.Assignee,
		AssigneeType:  issue.AssigneeType,
		Labels:        s.issueLabels[issue.ID],
		UpdatedAt:     issue.UpdatedAt,
		CreatedAt:     issue.CreatedAt,
	}
	for _, child := range s.issues {
		if child.ParentIssueID != issue.ID {
			continue
		}
		item.ChildIssueCount++
		if child.Status == "closed" {
			item.CompletedChildIssueCount++
		}
	}
	for _, task := range s.runtimeTasks {
		if task.IssueID == issue.ID {
			item.SessionCount++
		}
	}
	return item
}

func (s *MemoryStore) commentReactionSummariesLocked(commentID, viewerUserID string) []CommentReactionSummary {
	summaries := []CommentReactionSummary{}
	for key, users := range s.commentReactions {
		if !strings.HasPrefix(key, commentID+":") {
			continue
		}
		reaction := strings.TrimPrefix(key, commentID+":")
		summaries = append(summaries, CommentReactionSummary{
			Reaction:    reaction,
			Count:       len(users),
			ReactedByMe: users[strings.TrimSpace(viewerUserID)].Reaction != "",
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Reaction < summaries[j].Reaction
	})
	return summaries
}

func (s *MemoryStore) runtimeWorkerByID(workspaceID, workerID string) (RuntimeWorker, bool) {
	for _, worker := range s.runtimeWorkers {
		if worker.WorkspaceID == strings.TrimSpace(workspaceID) && worker.ID == strings.TrimSpace(workerID) {
			return worker, true
		}
	}
	return RuntimeWorker{}, false
}

func (s *MemoryStore) hasWorkspaceRole(workspaceID, userID string, roles ...string) bool {
	role := s.workspaceMembers[strings.TrimSpace(workspaceID)][strings.TrimSpace(userID)]
	if role == "" {
		return false
	}
	for _, allowed := range roles {
		if role == allowed {
			return true
		}
	}
	return false
}

func (s *MemoryStore) identityLoginForUser(userID string) string {
	for key, identityUserID := range s.identities {
		if identityUserID != userID {
			continue
		}
		parts := strings.SplitN(key, ":", 2)
		if len(parts) == 2 {
			return parts[1]
		}
	}
	return ""
}

func (s *MemoryStore) workspaceName(workspaceID string) string {
	for _, workspaces := range s.workspaces {
		for _, workspace := range workspaces {
			if workspace.ID == workspaceID {
				return workspace.Name
			}
		}
	}
	return "Workspace"
}

func (s *MemoryStore) workspaceLocked(workspaceID string) (Workspace, bool) {
	workspaceID = strings.TrimSpace(workspaceID)
	for _, workspaces := range s.workspaces {
		for _, workspace := range workspaces {
			if workspace.ID == workspaceID {
				return workspace, true
			}
		}
	}
	return Workspace{}, false
}

func (s *MemoryStore) isTeamWorkspace(workspaceID string) bool {
	workspaceID = strings.TrimSpace(workspaceID)
	for _, workspaces := range s.workspaces {
		for _, workspace := range workspaces {
			if workspace.ID == workspaceID {
				return workspace.Kind == "team"
			}
		}
	}
	return false
}

func (s *MemoryStore) addWorkspaceForUser(userID, workspaceID, role string) {
	for index, workspace := range s.workspaces[userID] {
		if workspace.ID != workspaceID {
			continue
		}
		workspace.Role = role
		s.workspaces[userID][index] = workspace
		return
	}
	for _, workspaces := range s.workspaces {
		for _, workspace := range workspaces {
			if workspace.ID != workspaceID {
				continue
			}
			workspace.Role = role
			s.workspaces[userID] = append(s.workspaces[userID], workspace)
			sort.Slice(s.workspaces[userID], func(i, j int) bool {
				return s.workspaces[userID][i].CreatedAt < s.workspaces[userID][j].CreatedAt
			})
			return
		}
	}
}

func workspaceRoleRank(role string) int {
	switch role {
	case "owner":
		return 0
	case "admin":
		return 1
	default:
		return 2
	}
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

func jsonObjectContains(container, subset json.RawMessage) bool {
	var containerMap map[string]any
	var subsetMap map[string]any
	if err := json.Unmarshal(container, &containerMap); err != nil {
		return false
	}
	if err := json.Unmarshal(subset, &subsetMap); err != nil {
		return false
	}
	return mapContains(containerMap, subsetMap)
}

func mapContains(container, subset map[string]any) bool {
	for key, subsetValue := range subset {
		containerValue, ok := container[key]
		if !ok {
			return false
		}
		nestedSubset, subsetIsMap := subsetValue.(map[string]any)
		nestedContainer, containerIsMap := containerValue.(map[string]any)
		if subsetIsMap {
			if !containerIsMap || !mapContains(nestedContainer, nestedSubset) {
				return false
			}
			continue
		}
		if !reflect.DeepEqual(containerValue, subsetValue) {
			return false
		}
	}
	return true
}
