package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	identityLogins       map[string]string
	passwordCredentials  map[string]memoryPasswordCredential
	workspaces           map[string][]Workspace
	workspaceMembers     map[string]map[string]string
	workspaceInvitations map[string]memoryWorkspaceInvitation
	events               map[string]IssueEvent
	receipts             map[string]memoryReceipt
	watchers             map[string]map[string]bool
	projects             map[string]Project
	projectRunbooks      map[string]ProjectRunbook
	testCases            map[string]TestCase
	testCaseRevisions    map[string]TestCaseRevision
	testCaseProposals    map[string]TestCaseProposal
	testPlans            map[string]TestPlan
	testPlanCases        map[string]TestPlanCase
	testRuns             map[string]TestRun
	testRunItems         map[string]TestRunItem
	testArtifacts        map[string]TestArtifact
	issues               map[string]Issue
	comments             map[string]Comment
	commentReactions     map[string]map[string]CommentReactionSummary
	attachments          map[string]IssueAttachment
	issueLabels          map[string][]IssueLabel
	workspaceSettings    map[string]WorkspaceSettings
	githubAppInstalls    map[string]WorkspaceGitHubAppInstallation
	workspaceSkills      map[string]WorkspaceSkill
	skillRevisions       map[string]WorkspaceSkillRevision
	builtinSkillSettings map[string]WorkspaceBuiltinSkillSetting
	clusters             map[string]Cluster
	environments         map[string]Environment
	environmentSSHAuth   map[string]virtualMachineStoredSSHAuth
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

type memoryPasswordCredential struct {
	UserID       string
	PasswordHash string
	Disabled     bool
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
		identityLogins:       map[string]string{},
		passwordCredentials:  map[string]memoryPasswordCredential{},
		workspaces:           map[string][]Workspace{},
		workspaceMembers:     map[string]map[string]string{},
		workspaceInvitations: map[string]memoryWorkspaceInvitation{},
		events:               map[string]IssueEvent{},
		receipts:             map[string]memoryReceipt{},
		watchers:             map[string]map[string]bool{},
		projects:             map[string]Project{},
		projectRunbooks:      map[string]ProjectRunbook{},
		testCases:            map[string]TestCase{},
		testCaseRevisions:    map[string]TestCaseRevision{},
		testCaseProposals:    map[string]TestCaseProposal{},
		testPlans:            map[string]TestPlan{},
		testPlanCases:        map[string]TestPlanCase{},
		testRuns:             map[string]TestRun{},
		testRunItems:         map[string]TestRunItem{},
		testArtifacts:        map[string]TestArtifact{},
		issues:               map[string]Issue{},
		comments:             map[string]Comment{},
		commentReactions:     map[string]map[string]CommentReactionSummary{},
		attachments:          map[string]IssueAttachment{},
		issueLabels:          map[string][]IssueLabel{},
		workspaceSettings:    map[string]WorkspaceSettings{},
		githubAppInstalls:    map[string]WorkspaceGitHubAppInstallation{},
		workspaceSkills:      map[string]WorkspaceSkill{},
		skillRevisions:       map[string]WorkspaceSkillRevision{},
		builtinSkillSettings: map[string]WorkspaceBuiltinSkillSetting{},
		clusters:             map[string]Cluster{},
		environments:         map[string]Environment{},
		environmentSSHAuth:   map[string]virtualMachineStoredSSHAuth{},
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

func (s *MemoryStore) EnsureBootstrapAdmin(_ Context, input PasswordAuthInput) (User, []Workspace, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized, err := normalizePasswordAuthInput(input, true)
	if err != nil {
		return User{}, nil, false, err
	}
	if credential, ok := s.passwordCredentials[normalized.Login]; ok {
		user, ok := s.users[credential.UserID]
		if !ok {
			return User{}, nil, false, ErrNotFound
		}
		return user, s.workspaces[credential.UserID], false, nil
	}
	if s.identities["password:"+normalized.Login] != "" {
		return User{}, nil, false, ErrConflict
	}
	passwordHash, err := hashPassword(normalized.Password)
	if err != nil {
		return User{}, nil, false, err
	}

	s.nextID++
	userID := fmt.Sprintf("user-%04d", s.nextID)
	now := time.Now().UTC().Format(time.RFC3339)
	name := strings.TrimSpace(normalized.Name)
	if name == "" {
		name = normalized.Login
	}
	user := User{
		ID:        userID,
		Name:      name,
		Email:     "",
		AvatarURL: "",
		CreatedAt: now,
		UpdatedAt: now,
	}
	workspace := Workspace{
		ID:        "workspace-" + userID,
		Name:      defaultPersonalWorkspaceName(name),
		Slug:      defaultWorkspaceSlug(normalized.Login, userID),
		Kind:      "personal",
		Role:      "owner",
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.users[userID] = user
	identityKey := "password:" + normalized.Login
	s.identities[identityKey] = userID
	s.identityLogins[identityKey] = normalized.Login
	s.passwordCredentials[normalized.Login] = memoryPasswordCredential{UserID: userID, PasswordHash: passwordHash}
	s.workspaces[userID] = []Workspace{workspace}
	if s.workspaceMembers[workspace.ID] == nil {
		s.workspaceMembers[workspace.ID] = map[string]string{}
	}
	s.workspaceMembers[workspace.ID][userID] = "owner"
	return user, s.workspaces[userID], true, nil
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
		Name:      defaultPersonalWorkspaceName(name),
		Slug:      defaultWorkspaceSlug(profile.Login, userID),
		Kind:      "personal",
		Role:      "owner",
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.users[userID] = user
	s.identities[key] = userID
	s.identityLogins[key] = profile.Login
	s.workspaces[userID] = []Workspace{workspace}
	if s.workspaceMembers[workspace.ID] == nil {
		s.workspaceMembers[workspace.ID] = map[string]string{}
	}
	s.workspaceMembers[workspace.ID][userID] = "owner"
	return user, s.workspaces[userID], nil
}

func (s *MemoryStore) CreatePasswordIdentity(_ Context, input PasswordAuthInput) (User, []Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized, err := normalizePasswordAuthInput(input, true)
	if err != nil {
		return User{}, nil, err
	}
	if _, exists := s.passwordCredentials[normalized.Login]; exists {
		return User{}, nil, ErrConflict
	}
	key := "password:" + normalized.Login
	if s.identities[key] != "" {
		return User{}, nil, ErrConflict
	}
	passwordHash, err := hashPassword(normalized.Password)
	if err != nil {
		return User{}, nil, err
	}

	s.nextID++
	userID := fmt.Sprintf("user-%04d", s.nextID)
	now := time.Now().UTC().Format(time.RFC3339)
	name := strings.TrimSpace(normalized.Name)
	if name == "" {
		name = normalized.Login
	}
	user := User{
		ID:        userID,
		Name:      name,
		Email:     "",
		AvatarURL: "",
		CreatedAt: now,
		UpdatedAt: now,
	}
	workspace := Workspace{
		ID:        "workspace-" + userID,
		Name:      defaultPersonalWorkspaceName(name),
		Slug:      defaultWorkspaceSlug(normalized.Login, userID),
		Kind:      "personal",
		Role:      "owner",
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.users[userID] = user
	s.identities[key] = userID
	s.identityLogins[key] = normalized.Login
	s.passwordCredentials[normalized.Login] = memoryPasswordCredential{UserID: userID, PasswordHash: passwordHash}
	s.workspaces[userID] = []Workspace{workspace}
	if s.workspaceMembers[workspace.ID] == nil {
		s.workspaceMembers[workspace.ID] = map[string]string{}
	}
	s.workspaceMembers[workspace.ID][userID] = "owner"
	return user, s.workspaces[userID], nil
}

func (s *MemoryStore) AuthenticatePassword(_ Context, input PasswordAuthInput) (User, []Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized, err := normalizePasswordAuthInput(input, false)
	if err != nil {
		return User{}, nil, err
	}
	credential, ok := s.passwordCredentials[normalized.Login]
	if !ok {
		verifyPasswordHash(dummyPasswordHash, normalized.Password)
		return User{}, nil, ErrNotFound
	}
	if credential.Disabled {
		verifyPasswordHash(credential.PasswordHash, normalized.Password)
		return User{}, nil, ErrForbidden
	}
	if !verifyPasswordHash(credential.PasswordHash, normalized.Password) {
		return User{}, nil, ErrNotFound
	}
	user, ok := s.users[credential.UserID]
	if !ok {
		return User{}, nil, ErrNotFound
	}
	return user, s.workspaces[credential.UserID], nil
}

func (s *MemoryStore) GetUserAuthIdentity(_ Context, userID string) (AuthIdentityInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	userID = strings.TrimSpace(userID)
	if _, ok := s.users[userID]; !ok {
		return AuthIdentityInfo{}, ErrNotFound
	}
	identity := s.authIdentityForUser(userID)
	if identity.Login == "" {
		return AuthIdentityInfo{}, ErrNotFound
	}
	return identity, nil
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

func (s *MemoryStore) UpdateCurrentUserProfile(_ Context, userID string, input UpdateCurrentUserProfileInput) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	userID = strings.TrimSpace(userID)
	if userID == "" {
		return User{}, ErrNotFound
	}
	normalized, err := normalizeUpdateCurrentUserProfileInput(input)
	if err != nil {
		return User{}, err
	}
	user, ok := s.users[userID]
	if !ok {
		return User{}, ErrNotFound
	}
	user.Name = normalized.Name
	user.AvatarURL = normalized.AvatarURL
	user.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	s.users[userID] = user
	return user, nil
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
		ID:          workspaceID,
		Name:        normalized.Name,
		Slug:        workspaceSlug(normalized.Name, strings.TrimPrefix(workspaceID, "workspace-")),
		Kind:        normalized.Kind,
		Role:        "owner",
		Icon:        "",
		Description: "",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	s.workspaces[userID] = append(s.workspaces[userID], workspace)
	if s.workspaceMembers[workspace.ID] == nil {
		s.workspaceMembers[workspace.ID] = map[string]string{}
	}
	s.workspaceMembers[workspace.ID][userID] = "owner"
	return workspace, append([]Workspace(nil), s.workspaces[userID]...), nil
}

func (s *MemoryStore) UpdateWorkspace(_ Context, userID, workspaceID string, input UpdateWorkspaceInput) (Workspace, []Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	userID = strings.TrimSpace(userID)
	workspaceID = strings.TrimSpace(workspaceID)
	normalized, err := normalizeUpdateWorkspaceInput(input)
	if err != nil {
		return Workspace{}, nil, err
	}
	if !s.isTeamWorkspace(workspaceID) {
		if !s.isWorkspaceMember(workspaceID, userID) {
			return Workspace{}, nil, ErrNotFound
		}
		return Workspace{}, nil, ErrForbidden
	}
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		if !s.isWorkspaceMember(workspaceID, userID) {
			return Workspace{}, nil, ErrNotFound
		}
		return Workspace{}, nil, ErrForbidden
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	var updated Workspace
	for memberUserID, workspaces := range s.workspaces {
		for index, workspace := range workspaces {
			if workspace.ID != workspaceID {
				continue
			}
			workspace.Name = normalized.Name
			workspace.Icon = normalized.Icon
			workspace.Description = normalized.Description
			workspace.UpdatedAt = now
			if role := s.workspaceMembers[workspaceID][memberUserID]; role != "" {
				workspace.Role = role
			}
			s.workspaces[memberUserID][index] = workspace
			if memberUserID == userID {
				updated = workspace
			}
		}
	}
	if updated.ID == "" {
		return Workspace{}, nil, ErrNotFound
	}
	return updated, append([]Workspace(nil), s.workspaces[userID]...), nil
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

func (s *MemoryStore) PreviewWorkspaceInvitation(_ Context, token string) (WorkspaceInvitationPreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	token = strings.TrimSpace(token)
	if token == "" || !strings.HasPrefix(token, "msi_") {
		return WorkspaceInvitationPreview{}, ErrNotFound
	}
	invitation, ok := s.workspaceInvitations[tokenHash(token)]
	if !ok {
		return WorkspaceInvitationPreview{}, ErrNotFound
	}
	expiresAt, err := time.Parse(time.RFC3339, invitation.Record.ExpiresAt)
	if err != nil {
		return WorkspaceInvitationPreview{}, err
	}
	inviter := s.users[strings.TrimSpace(invitation.Record.InvitedByUserID)]
	return WorkspaceInvitationPreview{
		WorkspaceName:      invitation.Record.WorkspaceName,
		Role:               invitation.Record.Role,
		InvitedByName:      firstNonEmpty(inviter.Name, s.identityLoginForUser(invitation.Record.InvitedByUserID)),
		InvitedByAvatarURL: inviter.AvatarURL,
		InvitedByLogin:     s.identityLoginForUser(invitation.Record.InvitedByUserID),
		ExpiresAt:          invitation.Record.ExpiresAt,
		Status:             workspaceInvitationPreviewStatus(invitation.Record.Revoked, invitation.Record.AcceptedAt != "", expiresAt),
	}, nil
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

func (s *MemoryStore) EnsureRuntimeRegistrationToken(_ Context, userID, workspaceID string, input EnsureRuntimeRegistrationTokenInput) (RuntimeRegistrationToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		if !s.isWorkspaceMember(workspaceID, userID) {
			return RuntimeRegistrationToken{}, ErrNotFound
		}
		return RuntimeRegistrationToken{}, ErrForbidden
	}
	token := strings.TrimSpace(input.Token)
	if err := validateRuntimeRegistrationToken(token); err != nil {
		return RuntimeRegistrationToken{}, err
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
		return RuntimeRegistrationToken{}, fmt.Errorf("expiresInHours must be 2160 or less")
	}
	now := time.Now().UTC()
	hash := tokenHash(token)
	if existing, ok := s.runtimeTokens[hash]; ok {
		if existing.Record.WorkspaceID != strings.TrimSpace(workspaceID) {
			return RuntimeRegistrationToken{}, ErrConflict
		}
		existing.Record.Name = name
		existing.Record.TokenPrefix = tokenPrefix(token)
		existing.Record.ExpiresAt = now.Add(time.Duration(expiresInHours) * time.Hour).Format(time.RFC3339)
		existing.Record.UpdatedAt = now.Format(time.RFC3339)
		s.runtimeTokens[hash] = existing
		return existing.Record, nil
	}
	s.nextID++
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
	s.runtimeTokens[hash] = memoryRuntimeRegistrationToken{TokenHash: hash, Record: record}
	return record, nil
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
	if err := s.ensureRuntimeModeAllowedForWorkspaceLocked(registration.WorkspaceID, normalized.Mode); err != nil {
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
	worker.AgentEngineDiagnostics = copyRawMessage(normalized.AgentEngineDiagnostics)
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
	diagnosticsProvided := strings.TrimSpace(string(input.AgentEngineDiagnostics)) != ""
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
		capabilities := worker.Capabilities
		if string(normalized.Capabilities) != "{}" {
			capabilities = normalized.Capabilities
		}
		diagnostics := worker.AgentEngineDiagnostics
		if diagnosticsProvided {
			diagnostics = normalized.AgentEngineDiagnostics
		}
		worker.Capabilities = downgradeUnavailableAgentEngineCapabilities(capabilities, diagnostics)
		if string(normalized.Labels) != "{}" {
			worker.Labels = copyRawMessage(normalized.Labels)
		}
		if diagnosticsProvided {
			worker.AgentEngineDiagnostics = copyRawMessage(diagnostics)
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
			worker.AgentEngineDiagnostics = normalizedRuntimeWorkerDiagnostics(worker.AgentEngineDiagnostics)
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
	return s.createRuntimeTaskLocked(userID, workspaceID, input)
}

func (s *MemoryStore) createRuntimeTaskLocked(userID, workspaceID string, input CreateRuntimeTaskInput) (RuntimeTask, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return RuntimeTask{}, ErrNotFound
	}
	if !input.ServerManaged && !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		return RuntimeTask{}, ErrForbidden
	}
	normalized, err := normalizeCreateRuntimeTaskInput(input)
	if err != nil {
		return RuntimeTask{}, err
	}
	if err := s.ensureRuntimeModeAllowedForWorkspaceLocked(workspaceID, normalized.RuntimeMode); err != nil {
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
	settings.AutoDeployTestEnvironment = input.AutoDeployTestEnvironment
	settings.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.workspaceSettings[workspaceID] = settings
	return settings, nil
}

func (s *MemoryStore) GetWorkspaceGitHubAppInstallation(_ Context, userID, workspaceID string) (WorkspaceGitHubAppInstallation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return WorkspaceGitHubAppInstallation{}, ErrNotFound
	}
	installation, ok := s.githubAppInstalls[workspaceID]
	if !ok {
		return defaultWorkspaceGitHubAppInstallation(), nil
	}
	return normalizeStoredWorkspaceGitHubAppInstallation(installation), nil
}

func (s *MemoryStore) UpsertWorkspaceGitHubAppInstallation(_ Context, workspaceID string, input WorkspaceGitHubAppInstallation) (WorkspaceGitHubAppInstallation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if _, ok := s.workspaceLocked(workspaceID); !ok {
		return WorkspaceGitHubAppInstallation{}, ErrNotFound
	}
	installation := normalizeStoredWorkspaceGitHubAppInstallation(input)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if existing, ok := s.githubAppInstalls[workspaceID]; ok && existing.CreatedAt != "" {
		installation.CreatedAt = existing.CreatedAt
	} else if installation.CreatedAt == "" {
		installation.CreatedAt = now
	}
	installation.UpdatedAt = now
	s.githubAppInstalls[workspaceID] = installation
	return normalizeStoredWorkspaceGitHubAppInstallation(installation), nil
}

func (s *MemoryStore) ListSkills(_ Context, userID, workspaceID string) ([]SkillCatalogItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	return s.listSkillsLocked(workspaceID)
}

func (s *MemoryStore) GetSkill(_ Context, userID, workspaceID, skillID string) (SkillDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		if !s.isWorkspaceMember(workspaceID, userID) {
			return SkillDetail{}, ErrNotFound
		}
		return SkillDetail{}, ErrForbidden
	}
	return s.skillDetailLocked(workspaceID, skillID)
}

func (s *MemoryStore) CreateSkill(_ Context, userID, workspaceID string, input SkillInput) (SkillDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		return SkillDetail{}, ErrForbidden
	}
	normalized, files, err := normalizeWorkspaceSkillCreateInput(input)
	if err != nil {
		return SkillDetail{}, err
	}
	if _, err := builtinSkillBundle(normalized.Slug, "latest"); err == nil {
		return SkillDetail{}, fmt.Errorf("skill slug conflicts with built-in skill: %s", normalized.Slug)
	} else if !errors.Is(err, ErrNotFound) {
		return SkillDetail{}, err
	}
	if s.workspaceSkillIdentifierExistsLocked(workspaceID, normalized.Slug) {
		return SkillDetail{}, ErrConflict
	}
	return s.createWorkspaceSkillLocked(workspaceID, userID, normalized, files)
}

func (s *MemoryStore) UpdateSkill(_ Context, userID, workspaceID, skillID string, input SkillInput) (SkillDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		return SkillDetail{}, ErrForbidden
	}
	if slug, ok := slugFromBuiltinSkillID(skillID); ok || s.isBuiltinSkillSlug(strings.TrimSpace(skillID)) {
		if !ok {
			slug = normalizeSkillSlug(skillID)
		}
		return s.updateBuiltinSkillSettingLocked(workspaceID, slug, input)
	}
	existing, err := s.skillDetailLocked(workspaceID, skillID)
	if err != nil {
		return SkillDetail{}, err
	}
	if existing.BuiltIn {
		return s.updateBuiltinSkillSettingLocked(workspaceID, existing.Slug, input)
	}
	normalized, files, hasFiles, err := normalizeWorkspaceSkillUpdateInput(existing, input)
	if err != nil {
		return SkillDetail{}, err
	}
	skill, ok := s.workspaceSkills[existing.ID]
	if !ok || skill.DeletedAt != "" || skill.WorkspaceID != workspaceID {
		return SkillDetail{}, ErrNotFound
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	skill.Name = normalized.Name
	skill.Description = normalized.Description
	skill.Enabled = skillBoolValue(normalized.Enabled, skill.Enabled)
	skill.Invocable = skillBoolValue(normalized.Invocable, skill.Invocable)
	skill.UpdatedAt = now
	if hasFiles {
		revision := s.createWorkspaceSkillRevisionLocked(workspaceID, skill.ID, userID, files, now)
		skill.CurrentRevisionID = revision.ID
	}
	s.workspaceSkills[skill.ID] = skill
	return s.skillDetailLocked(workspaceID, skill.ID)
}

func (s *MemoryStore) DeleteSkill(_ Context, userID, workspaceID, skillID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		return ErrForbidden
	}
	if _, ok := slugFromBuiltinSkillID(skillID); ok || s.isBuiltinSkillSlug(strings.TrimSpace(skillID)) {
		return ErrForbidden
	}
	skill, _, ok := s.workspaceSkillByIdentifierLocked(workspaceID, skillID)
	if !ok {
		return ErrNotFound
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	skill.DeletedAt = now
	skill.Enabled = false
	skill.Invocable = false
	skill.UpdatedAt = now
	s.workspaceSkills[skill.ID] = skill
	return nil
}

func (s *MemoryStore) DuplicateSkill(_ Context, userID, workspaceID, skillID string, input DuplicateSkillInput) (SkillDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		return SkillDetail{}, ErrForbidden
	}
	source, err := s.skillDetailLocked(workspaceID, skillID)
	if err != nil {
		return SkillDetail{}, err
	}
	slug := normalizeSkillSlug(input.Slug)
	if slug == "" {
		slug = s.nextWorkspaceSkillCopySlugLocked(workspaceID, source.Slug)
	}
	if _, err := builtinSkillBundle(slug, "latest"); err == nil {
		return SkillDetail{}, fmt.Errorf("skill slug conflicts with built-in skill: %s", slug)
	} else if !errors.Is(err, ErrNotFound) {
		return SkillDetail{}, err
	}
	if s.workspaceSkillIdentifierExistsLocked(workspaceID, slug) {
		return SkillDetail{}, ErrConflict
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = strings.TrimSpace(source.Name)
		if name == "" {
			name = source.Slug
		}
		name += " copy"
	}
	description := strings.TrimSpace(input.Description)
	if description == "" {
		description = source.Description
	}
	createInput := SkillInput{
		Slug:        slug,
		Name:        name,
		Description: description,
		Enabled:     boolPointer(skillBoolValue(input.Enabled, true)),
		Invocable:   boolPointer(skillBoolValue(input.Invocable, true)),
		Files:       source.Files,
	}
	normalized, files, err := normalizeWorkspaceSkillCreateInput(createInput)
	if err != nil {
		return SkillDetail{}, err
	}
	return s.createWorkspaceSkillLocked(workspaceID, userID, normalized, files)
}

func (s *MemoryStore) listSkillsLocked(workspaceID string) ([]SkillCatalogItem, error) {
	builtins, err := listBuiltinSkills()
	if err != nil {
		return nil, err
	}
	items := make([]SkillCatalogItem, 0, len(builtins)+len(s.workspaceSkills))
	for _, builtin := range builtins {
		bundle, err := builtinSkillBundle(builtin.Slug, builtin.Revision)
		if err != nil {
			return nil, err
		}
		items = append(items, skillCatalogItemFromBundle(bundle, s.builtinSkillSettingLocked(workspaceID, builtin.Slug)))
	}
	for _, skill := range s.workspaceSkills {
		if skill.WorkspaceID != workspaceID || skill.DeletedAt != "" {
			continue
		}
		revision, ok := s.skillRevisions[skill.CurrentRevisionID]
		if !ok {
			continue
		}
		items = append(items, skillCatalogItemFromWorkspaceSkill(skill, revision))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].BuiltIn != items[j].BuiltIn {
			return items[i].BuiltIn
		}
		return items[i].Slug < items[j].Slug
	})
	return items, nil
}

func (s *MemoryStore) skillDetailLocked(workspaceID, identifier string) (SkillDetail, error) {
	identifier = strings.TrimSpace(identifier)
	if slug, ok := slugFromBuiltinSkillID(identifier); ok || s.isBuiltinSkillSlug(identifier) {
		if !ok {
			slug = normalizeSkillSlug(identifier)
		}
		bundle, err := builtinSkillBundle(slug, "latest")
		if err != nil {
			return SkillDetail{}, err
		}
		return skillDetailFromBundle(bundle, s.builtinSkillSettingLocked(workspaceID, slug)), nil
	}
	skill, revision, ok := s.workspaceSkillByIdentifierLocked(workspaceID, identifier)
	if !ok {
		return SkillDetail{}, ErrNotFound
	}
	return skillDetailFromWorkspaceSkill(skill, revision), nil
}

func (s *MemoryStore) resolveAgentSessionSkillBundleLocked(workspaceID, slug string) (AgentSessionSkillReference, RuntimeSkillBundle, error) {
	slug = normalizeSkillSlug(slug)
	if slug == "" {
		return AgentSessionSkillReference{}, RuntimeSkillBundle{}, errors.New("skill slug is required")
	}
	if skill, revision, ok := s.workspaceSkillByIdentifierLocked(workspaceID, slug); ok {
		if !skill.Enabled {
			return AgentSessionSkillReference{}, RuntimeSkillBundle{}, fmt.Errorf("skill slug is disabled: %s", slug)
		}
		bundle := runtimeSkillBundleFromWorkspaceSkill(skill, revision)
		return skillReferenceFromBundle(bundle), bundle, nil
	}
	bundle, err := builtinSkillBundle(slug, "latest")
	if err != nil {
		return AgentSessionSkillReference{}, RuntimeSkillBundle{}, err
	}
	setting := s.builtinSkillSettingLocked(workspaceID, slug)
	if !setting.Enabled {
		return AgentSessionSkillReference{}, RuntimeSkillBundle{}, fmt.Errorf("skill slug is disabled: %s", slug)
	}
	return skillReferenceFromBundle(bundle), bundle, nil
}

func (s *MemoryStore) createWorkspaceSkillLocked(workspaceID, userID string, input SkillInput, files []RuntimeSkillFile) (SkillDetail, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	skill := WorkspaceSkill{
		ID:          fmt.Sprintf("skill-%04d", s.nextMemoryIDLocked()),
		WorkspaceID: workspaceID,
		Slug:        input.Slug,
		Name:        input.Name,
		Description: input.Description,
		SourceType:  skillSourceTypeCustom,
		Enabled:     skillBoolValue(input.Enabled, true),
		Invocable:   skillBoolValue(input.Invocable, true),
		CreatedBy:   strings.TrimSpace(userID),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	revision := s.createWorkspaceSkillRevisionLocked(workspaceID, skill.ID, userID, files, now)
	skill.CurrentRevisionID = revision.ID
	s.workspaceSkills[skill.ID] = skill
	return skillDetailFromWorkspaceSkill(skill, revision), nil
}

func (s *MemoryStore) createWorkspaceSkillRevisionLocked(workspaceID, skillID, userID string, files []RuntimeSkillFile, now string) WorkspaceSkillRevision {
	revisionValue, contentHash := workspaceSkillRevisionFromFiles(files)
	revision := WorkspaceSkillRevision{
		ID:          fmt.Sprintf("skill-revision-%04d", s.nextMemoryIDLocked()),
		WorkspaceID: workspaceID,
		SkillID:     skillID,
		Revision:    revisionValue,
		ContentHash: contentHash,
		Files:       append([]RuntimeSkillFile(nil), files...),
		CreatedBy:   strings.TrimSpace(userID),
		CreatedAt:   now,
	}
	s.skillRevisions[revision.ID] = revision
	return revision
}

func (s *MemoryStore) updateBuiltinSkillSettingLocked(workspaceID, slug string, input SkillInput) (SkillDetail, error) {
	bundle, err := builtinSkillBundle(slug, "latest")
	if err != nil {
		return SkillDetail{}, err
	}
	if input.Files != nil || strings.TrimSpace(input.Name) != "" || strings.TrimSpace(input.Description) != "" {
		return SkillDetail{}, errors.New("built-in skill content cannot be edited")
	}
	existing := s.builtinSkillSettingLocked(workspaceID, slug)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if existing.CreatedAt == "" {
		existing.CreatedAt = now
	}
	existing.WorkspaceID = workspaceID
	existing.Slug = slug
	existing.Enabled = skillBoolValue(input.Enabled, existing.Enabled)
	existing.Invocable = skillBoolValue(input.Invocable, existing.Invocable)
	existing.UpdatedAt = now
	s.builtinSkillSettings[builtinSkillSettingKey(workspaceID, slug)] = existing
	return skillDetailFromBundle(bundle, existing), nil
}

func (s *MemoryStore) workspaceSkillByIdentifierLocked(workspaceID, identifier string) (WorkspaceSkill, WorkspaceSkillRevision, bool) {
	identifier = strings.TrimSpace(identifier)
	for _, skill := range s.workspaceSkills {
		if skill.WorkspaceID != workspaceID || skill.DeletedAt != "" {
			continue
		}
		if skill.ID != identifier {
			continue
		}
		revision, ok := s.skillRevisions[skill.CurrentRevisionID]
		if !ok {
			return WorkspaceSkill{}, WorkspaceSkillRevision{}, false
		}
		return skill, revision, true
	}
	slug := normalizeSkillSlug(identifier)
	for _, skill := range s.workspaceSkills {
		if skill.WorkspaceID != workspaceID || skill.DeletedAt != "" {
			continue
		}
		if skill.Slug != slug {
			continue
		}
		revision, ok := s.skillRevisions[skill.CurrentRevisionID]
		if !ok {
			return WorkspaceSkill{}, WorkspaceSkillRevision{}, false
		}
		return skill, revision, true
	}
	return WorkspaceSkill{}, WorkspaceSkillRevision{}, false
}

func (s *MemoryStore) workspaceSkillSlugExistsLocked(workspaceID, slug string) bool {
	slug = normalizeSkillSlug(slug)
	for _, skill := range s.workspaceSkills {
		if skill.WorkspaceID == workspaceID && skill.DeletedAt == "" && skill.Slug == slug {
			return true
		}
	}
	return false
}

func (s *MemoryStore) workspaceSkillIdentifierExistsLocked(workspaceID, identifier string) bool {
	identifier = normalizeSkillSlug(identifier)
	for _, skill := range s.workspaceSkills {
		if skill.WorkspaceID != workspaceID || skill.DeletedAt != "" {
			continue
		}
		if skill.Slug == identifier || skill.ID == identifier {
			return true
		}
	}
	return false
}

func (s *MemoryStore) builtinSkillSettingLocked(workspaceID, slug string) WorkspaceBuiltinSkillSetting {
	slug = normalizeSkillSlug(slug)
	if setting, ok := s.builtinSkillSettings[builtinSkillSettingKey(workspaceID, slug)]; ok {
		return setting
	}
	return WorkspaceBuiltinSkillSetting{
		WorkspaceID: workspaceID,
		Slug:        slug,
		Enabled:     true,
		Invocable:   true,
	}
}

func builtinSkillSettingKey(workspaceID, slug string) string {
	return strings.TrimSpace(workspaceID) + ":" + normalizeSkillSlug(slug)
}

func (s *MemoryStore) isBuiltinSkillSlug(value string) bool {
	slug := normalizeSkillSlug(value)
	if slug == "" {
		return false
	}
	_, err := builtinSkillBundle(slug, "latest")
	return err == nil
}

func (s *MemoryStore) nextWorkspaceSkillCopySlugLocked(workspaceID, base string) string {
	base = normalizeSkillSlug(base)
	if base == "" {
		base = "skill"
	}
	candidate := base + "-copy"
	if !s.workspaceSkillSlugExistsLocked(workspaceID, candidate) {
		if _, err := builtinSkillBundle(candidate, "latest"); errors.Is(err, ErrNotFound) {
			return candidate
		}
	}
	for index := 2; ; index++ {
		candidate = fmt.Sprintf("%s-copy-%d", base, index)
		if s.workspaceSkillSlugExistsLocked(workspaceID, candidate) {
			continue
		}
		if _, err := builtinSkillBundle(candidate, "latest"); errors.Is(err, ErrNotFound) {
			return candidate
		}
	}
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

func (s *MemoryStore) ListEnvironments(_ Context, userID, workspaceID string) ([]Environment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	environments := []Environment{}
	for _, cluster := range s.clusters {
		if cluster.WorkspaceID != workspaceID {
			continue
		}
		item := environmentFromCluster(cluster)
		for _, project := range s.projects {
			if project.WorkspaceID == workspaceID && project.DefaultClusterID == item.ID {
				item.ProjectCount++
			}
		}
		for _, issueEnvironment := range s.testEnvironments {
			if s.issueEnvironmentWorkspaceMatchesLocked(issueEnvironment, workspaceID) && firstNonEmpty(issueEnvironment.EnvironmentID, issueEnvironment.ClusterID) == item.ID {
				item.IssueEnvironmentCount++
			}
		}
		for _, plan := range s.testPlans {
			if plan.WorkspaceID == workspaceID && plan.EnvironmentID == item.ID {
				item.TestPlanCount++
			}
		}
		for _, run := range s.testRuns {
			if run.WorkspaceID == workspaceID && run.EnvironmentID == item.ID {
				item.TestRunCount++
			}
		}
		environments = append(environments, item)
	}
	for _, environment := range s.environments {
		if environment.WorkspaceID != workspaceID {
			continue
		}
		item := environment
		item.ProjectCount = 0
		item.IssueEnvironmentCount = 0
		item.TestPlanCount = 0
		item.TestRunCount = 0
		for _, issueEnvironment := range s.testEnvironments {
			if s.issueEnvironmentWorkspaceMatchesLocked(issueEnvironment, workspaceID) && issueEnvironment.EnvironmentID == item.ID {
				item.IssueEnvironmentCount++
			}
		}
		for _, plan := range s.testPlans {
			if plan.WorkspaceID == workspaceID && plan.EnvironmentID == item.ID {
				item.TestPlanCount++
			}
		}
		for _, run := range s.testRuns {
			if run.WorkspaceID == workspaceID && run.EnvironmentID == item.ID {
				item.TestRunCount++
			}
		}
		environments = append(environments, item)
	}
	sort.Slice(environments, func(i, j int) bool {
		if environments[i].UpdatedAt == environments[j].UpdatedAt {
			return environments[i].CreatedAt > environments[j].CreatedAt
		}
		return environments[i].UpdatedAt > environments[j].UpdatedAt
	})
	return environments, nil
}

func (s *MemoryStore) CreateEnvironment(_ Context, userID, workspaceID string, input EnvironmentInput) (Environment, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	kind := normalizeEnvironmentKind(input.Kind)
	if kind != environmentKindKubernetes {
		s.mu.Lock()
		allowed := s.hasWorkspaceRole(workspaceID, userID, "owner", "admin")
		s.mu.Unlock()
		if !allowed {
			return Environment{}, ErrForbidden
		}
		normalized, err := normalizeEnvironmentInput(Environment{}, input)
		if err != nil {
			return Environment{}, err
		}
		storedAuth, err := normalizeVirtualMachineStoredSSHAuth(input.SSHAuth)
		if err != nil {
			return Environment{}, err
		}
		applyVirtualMachineStoredSSHAuth(&normalized, storedAuth)
		status, err := virtualMachineSSHStatus(context.Background(), normalized, storedAuth)
		if err != nil {
			return Environment{}, err
		}
		normalized.Status = status
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
			return Environment{}, ErrForbidden
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		normalized.ID = fmt.Sprintf("environment-%04d", s.nextMemoryIDLocked())
		normalized.WorkspaceID = workspaceID
		normalized.LastCheckedAt = now
		normalized.CreatedAt = now
		normalized.UpdatedAt = now
		s.environments[normalized.ID] = normalized
		s.environmentSSHAuth[normalized.ID] = storedAuth
		return normalized, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		return Environment{}, ErrForbidden
	}
	if kind == environmentKindKubernetes {
		cluster, err := normalizeClusterInput(Cluster{}, clusterInputFromEnvironmentInput(input))
		if err != nil {
			return Environment{}, err
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		cluster.Status = kubeconfigStatus(context.Background(), cluster.KubeconfigPath, cluster.KubeContext)
		cluster.ID = fmt.Sprintf("cluster-%04d", s.nextMemoryIDLocked())
		cluster.WorkspaceID = workspaceID
		cluster.LastCheckedAt = now
		cluster.CreatedAt = now
		cluster.UpdatedAt = now
		s.clusters[cluster.ID] = cluster
		return environmentFromCluster(cluster), nil
	}
	return Environment{}, errors.New("environment kind must be kubernetes or virtual_machine")
}

func (s *MemoryStore) UpdateEnvironment(_ Context, userID, workspaceID, environmentID string, input EnvironmentInput) (Environment, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	environmentID = strings.TrimSpace(environmentID)
	s.mu.Lock()
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		s.mu.Unlock()
		return Environment{}, ErrForbidden
	}
	if cluster, ok := s.clusters[environmentID]; ok && cluster.WorkspaceID == workspaceID {
		defer s.mu.Unlock()
		updated, err := normalizeClusterInput(cluster, clusterInputFromEnvironmentInput(input))
		if err != nil {
			return Environment{}, err
		}
		updated.ID = cluster.ID
		updated.WorkspaceID = cluster.WorkspaceID
		updated.CreatedAt = cluster.CreatedAt
		updated.Status = kubeconfigStatus(context.Background(), updated.KubeconfigPath, updated.KubeContext)
		updated.LastCheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
		updated.UpdatedAt = updated.LastCheckedAt
		s.clusters[environmentID] = updated
		return environmentFromCluster(updated), nil
	}
	existing, ok := s.environments[environmentID]
	if !ok || existing.WorkspaceID != workspaceID {
		s.mu.Unlock()
		return Environment{}, ErrNotFound
	}
	s.mu.Unlock()
	updated, err := normalizeEnvironmentInput(existing, input)
	if err != nil {
		return Environment{}, err
	}
	if updated.Kind != existing.Kind {
		return Environment{}, errors.New("environment kind cannot be changed")
	}
	s.mu.Lock()
	storedAuth := s.environmentSSHAuth[environmentID]
	s.mu.Unlock()
	if input.SSHAuth != nil {
		var err error
		storedAuth, err = normalizeVirtualMachineStoredSSHAuth(input.SSHAuth)
		if err != nil {
			return Environment{}, err
		}
	}
	applyVirtualMachineStoredSSHAuth(&updated, storedAuth)
	status, err := virtualMachineSSHStatus(context.Background(), updated, storedAuth)
	if err != nil {
		return Environment{}, err
	}
	updated.Status = status
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		return Environment{}, ErrForbidden
	}
	existing, ok = s.environments[environmentID]
	if !ok || existing.WorkspaceID != workspaceID {
		return Environment{}, ErrNotFound
	}
	if updated.Kind != existing.Kind {
		return Environment{}, errors.New("environment kind cannot be changed")
	}
	updated.ID = existing.ID
	updated.WorkspaceID = existing.WorkspaceID
	updated.CreatedAt = existing.CreatedAt
	updated.LastCheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
	updated.UpdatedAt = updated.LastCheckedAt
	s.environments[environmentID] = updated
	s.environmentSSHAuth[environmentID] = storedAuth
	return updated, nil
}

func (s *MemoryStore) CheckEnvironment(_ Context, userID, workspaceID, environmentID string, input EnvironmentCheckInput) (Environment, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	environmentID = strings.TrimSpace(environmentID)
	s.mu.Lock()
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		s.mu.Unlock()
		return Environment{}, ErrForbidden
	}
	if cluster, ok := s.clusters[environmentID]; ok && cluster.WorkspaceID == workspaceID {
		cluster.Status = kubeconfigStatus(context.Background(), cluster.KubeconfigPath, cluster.KubeContext)
		cluster.LastCheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
		cluster.UpdatedAt = cluster.LastCheckedAt
		s.clusters[environmentID] = cluster
		s.mu.Unlock()
		return environmentFromCluster(cluster), nil
	}
	existing, ok := s.environments[environmentID]
	if !ok || existing.WorkspaceID != workspaceID {
		s.mu.Unlock()
		return Environment{}, ErrNotFound
	}
	s.mu.Unlock()

	s.mu.Lock()
	storedAuth := s.environmentSSHAuth[environmentID]
	s.mu.Unlock()
	if input.SSHAuth != nil {
		var err error
		storedAuth, err = normalizeVirtualMachineStoredSSHAuth(input.SSHAuth)
		if err != nil {
			return Environment{}, err
		}
	}
	applyVirtualMachineStoredSSHAuth(&existing, storedAuth)
	status, err := virtualMachineSSHStatus(context.Background(), existing, storedAuth)
	if err != nil {
		return Environment{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		return Environment{}, ErrForbidden
	}
	existing, ok = s.environments[environmentID]
	if !ok || existing.WorkspaceID != workspaceID {
		return Environment{}, ErrNotFound
	}
	existing.Status = status
	existing.LastCheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
	existing.UpdatedAt = existing.LastCheckedAt
	s.environments[environmentID] = existing
	s.environmentSSHAuth[environmentID] = storedAuth
	return existing, nil
}

func (s *MemoryStore) DeleteEnvironment(_ Context, userID, workspaceID, environmentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	environmentID = strings.TrimSpace(environmentID)
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		return ErrForbidden
	}
	if cluster, ok := s.clusters[environmentID]; ok && cluster.WorkspaceID == workspaceID {
		for _, project := range s.projects {
			if project.WorkspaceID == workspaceID && project.DefaultClusterID == environmentID {
				return ErrForbidden
			}
		}
		for _, issueEnvironment := range s.testEnvironments {
			if s.issueEnvironmentWorkspaceMatchesLocked(issueEnvironment, workspaceID) && firstNonEmpty(issueEnvironment.EnvironmentID, issueEnvironment.ClusterID) == environmentID {
				return ErrForbidden
			}
		}
		for _, plan := range s.testPlans {
			if plan.WorkspaceID == workspaceID && plan.EnvironmentID == environmentID {
				return ErrForbidden
			}
		}
		for _, run := range s.testRuns {
			if run.WorkspaceID == workspaceID && run.EnvironmentID == environmentID {
				return ErrForbidden
			}
		}
		delete(s.clusters, environmentID)
		return nil
	}
	environment, ok := s.environments[environmentID]
	if !ok || environment.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	for _, issueEnvironment := range s.testEnvironments {
		if s.issueEnvironmentWorkspaceMatchesLocked(issueEnvironment, workspaceID) && issueEnvironment.EnvironmentID == environmentID {
			return ErrForbidden
		}
	}
	for _, plan := range s.testPlans {
		if plan.WorkspaceID == workspaceID && plan.EnvironmentID == environmentID {
			return ErrForbidden
		}
	}
	for _, run := range s.testRuns {
		if run.WorkspaceID == workspaceID && run.EnvironmentID == environmentID {
			return ErrForbidden
		}
	}
	delete(s.environments, environmentID)
	delete(s.environmentSSHAuth, environmentID)
	return nil
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
	cluster.Status = kubeconfigStatus(context.Background(), cluster.KubeconfigPath, cluster.KubeContext)
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
	updated.Status = kubeconfigStatus(context.Background(), updated.KubeconfigPath, updated.KubeContext)
	updated.ID = existing.ID
	updated.WorkspaceID = existing.WorkspaceID
	updated.CreatedAt = existing.CreatedAt
	updated.LastCheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
	updated.UpdatedAt = updated.LastCheckedAt
	s.clusters[clusterID] = updated
	return updated, nil
}

func (s *MemoryStore) CheckCluster(_ Context, userID, workspaceID, clusterID string) (Cluster, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspaceID = strings.TrimSpace(workspaceID)
	clusterID = strings.TrimSpace(clusterID)
	if !s.hasWorkspaceRole(workspaceID, userID, "owner", "admin") {
		return Cluster{}, ErrForbidden
	}
	cluster, ok := s.clusters[clusterID]
	if !ok || cluster.WorkspaceID != workspaceID {
		return Cluster{}, ErrNotFound
	}
	cluster.Status = kubeconfigStatus(context.Background(), cluster.KubeconfigPath, cluster.KubeContext)
	cluster.LastCheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
	cluster.UpdatedAt = cluster.LastCheckedAt
	s.clusters[clusterID] = cluster
	return cluster, nil
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
	for _, plan := range s.testPlans {
		if plan.WorkspaceID == workspaceID && plan.EnvironmentID == clusterID {
			return ErrForbidden
		}
	}
	for _, run := range s.testRuns {
		if run.WorkspaceID == workspaceID && run.EnvironmentID == clusterID {
			return ErrForbidden
		}
	}
	delete(s.clusters, clusterID)
	return nil
}

func (s *MemoryStore) issueEnvironmentWorkspaceMatchesLocked(environment IssueTestEnvironment, workspaceID string) bool {
	issue, ok := s.issues[strings.TrimSpace(environment.IssueID)]
	return ok && issue.WorkspaceID == strings.TrimSpace(workspaceID)
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
			Status:              kubeconfigStatus(context.Background(), path, ""),
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
	workspace, ok := s.workspaceLocked(workspaceID)
	if !ok {
		return Project{}, ErrNotFound
	}
	if err := ensureProjectSourceAllowedForWorkspace(workspace, normalized); err != nil {
		return Project{}, err
	}
	s.nextID++
	now := time.Now().UTC().Format(time.RFC3339Nano)
	project := Project{
		ID:                   fmt.Sprintf("project-%04d", s.nextID),
		WorkspaceID:          workspaceID,
		Name:                 normalized.Name,
		RepoPath:             normalized.RepoPath,
		SourceType:           normalized.SourceType,
		RemoteURL:            normalized.RepoURL,
		GitProvider:          gitProviderFromURL(normalized.RepoURL),
		GitOwner:             gitOwnerRepoFromURL(normalized.RepoURL).owner,
		GitRepo:              gitOwnerRepoFromURL(normalized.RepoURL).repo,
		DefaultBranch:        normalized.DefaultBranch,
		KubeContext:          normalized.KubeContext,
		KubeconfigPath:       normalized.KubeconfigPath,
		Namespace:            normalized.Namespace,
		ImageRegistryPrefix:  normalized.ImageRegistryPrefix,
		PreviewDomain:        normalized.PreviewDomain,
		IngressClass:         normalized.IngressClass,
		NodeHost:             normalized.NodeHost,
		DefaultClusterID:     normalized.DefaultClusterID,
		DefaultEnvironmentID: normalized.DefaultEnvironmentID,
		RunbookStatus:        "empty",
		CreatedAt:            now,
		UpdatedAt:            now,
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
	project.DefaultEnvironmentID = normalized.DefaultEnvironmentID
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
	for caseID, testCase := range s.testCases {
		if testCase.ProjectID == projectID {
			delete(s.testCases, caseID)
		}
	}
	for revisionID, revision := range s.testCaseRevisions {
		if revision.ProjectID == projectID {
			delete(s.testCaseRevisions, revisionID)
		}
	}
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

func (s *MemoryStore) ListProjectTestCases(_ Context, userID, workspaceID, projectID string, options TestCaseListOptions) (TestCaseListResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	options = normalizeTestCaseListOptions(options)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return TestCaseListResult{}, ErrNotFound
	}
	if _, err := s.projectForTestCasesLocked(workspaceID, projectID); err != nil {
		return TestCaseListResult{}, err
	}
	status := strings.ToLower(strings.TrimSpace(options.Status))
	query := strings.ToLower(strings.TrimSpace(options.Query))
	cases := []TestCase{}
	for _, testCase := range s.testCases {
		if testCase.WorkspaceID != workspaceID || testCase.ProjectID != projectID {
			continue
		}
		if status == "" && testCase.Status == "archived" {
			continue
		}
		if status != "" && testCase.Status != status {
			continue
		}
		if query != "" {
			searchText := strings.ToLower(strings.Join([]string{testCase.Title, testCase.Type, testCase.Area, strings.Join(stringsOrEmpty(testCase.Tags), " ")}, " "))
			if !strings.Contains(searchText, query) {
				continue
			}
		}
		cases = append(cases, testCaseSnapshot(testCase))
	}
	s.attachLatestTestCaseResultsLocked(cases)
	sort.Slice(cases, func(i, j int) bool {
		return cases[i].UpdatedAt > cases[j].UpdatedAt
	})
	total := len(cases)
	start := options.Offset
	if start > total {
		start = total
	}
	end := start + options.Limit
	if end > total {
		end = total
	}
	return TestCaseListResult{
		Cases:  cases[start:end],
		Total:  total,
		Limit:  options.Limit,
		Offset: options.Offset,
	}, nil
}

func (s *MemoryStore) CreateProjectTestCase(_ Context, userID, workspaceID, projectID string, input TestCaseInput) (TestCase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return TestCase{}, ErrNotFound
	}
	if _, err := s.projectForTestCasesLocked(workspaceID, projectID); err != nil {
		return TestCase{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return s.createProjectTestCaseLocked(userID, workspaceID, projectID, input, now)
}

func (s *MemoryStore) ImportProjectTestCases(_ Context, userID, workspaceID, projectID string, input ImportTestCasesInput) (ImportTestCasesResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return ImportTestCasesResult{}, ErrNotFound
	}
	if _, err := s.projectForTestCasesLocked(workspaceID, projectID); err != nil {
		return ImportTestCasesResult{}, err
	}
	inputs, skipped, err := parseImportedTestCases(input)
	if err != nil {
		return ImportTestCasesResult{}, err
	}
	if len(inputs) == 0 {
		return ImportTestCasesResult{}, errors.New("content cannot be empty")
	}
	result := ImportTestCasesResult{Created: []TestCase{}, Skipped: testCaseImportSkipsOrEmpty(skipped)}
	for _, imported := range inputs {
		normalized, score, findings, err := normalizeTestCaseInput(imported, defaultImportedCaseSource)
		if err != nil {
			result.Skipped = append(result.Skipped, TestCaseImportSkip{Reason: err.Error(), Content: imported.Title})
			continue
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		testCase := TestCase{
			ID:                      fmt.Sprintf("test-case-%04d", s.nextMemoryIDLocked()),
			WorkspaceID:             workspaceID,
			ProjectID:               projectID,
			Title:                   normalized.Title,
			Type:                    normalized.Type,
			Area:                    normalized.Area,
			Priority:                normalized.Priority,
			Status:                  normalized.Status,
			Source:                  defaultImportedCaseSource,
			Preconditions:           normalized.Preconditions,
			Steps:                   normalized.Steps,
			ExpectedResult:          normalized.ExpectedResult,
			EnvironmentRequirements: normalized.EnvironmentRequirements,
			Dependencies:            normalized.Dependencies,
			Tags:                    normalized.Tags,
			QualityScore:            score,
			QualityFindings:         findings,
			CreatedByUserID:         strings.TrimSpace(userID),
			CreatedAt:               now,
			UpdatedAt:               now,
		}
		s.testCases[testCase.ID] = testCase
		s.appendTestCaseRevisionLocked(testCase, userID, now)
		result.Created = append(result.Created, testCaseSnapshot(testCase))
	}
	if len(result.Created) == 0 {
		return ImportTestCasesResult{}, errors.New("content cannot be empty")
	}
	return result, nil
}

func (s *MemoryStore) EnsureActiveCodexWorker(_ Context, userID, workspaceID, runtimeMode string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	userID = strings.TrimSpace(userID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return "", ErrNotFound
	}
	workspace, ok := s.workspaceLocked(workspaceID)
	if !ok {
		return "", ErrNotFound
	}
	runtimeMode = strings.ToLower(strings.TrimSpace(runtimeMode))
	if runtimeMode == "" {
		runtimeMode = workspace.Kind
	}
	if runtimeMode != "personal" && runtimeMode != "team" {
		return "", errors.New("runtimeMode must be personal or team")
	}
	if runtimeMode != workspace.Kind {
		return "", ErrForbidden
	}
	if !s.hasActiveCodexWorkerLocked(workspaceID, runtimeMode, time.Now().UTC()) {
		return "", ErrNoActiveCodexWorker
	}
	return runtimeMode, nil
}

func (s *MemoryStore) GetProjectTestCase(_ Context, userID, workspaceID, projectID, caseID string) (TestCase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	testCase, err := s.testCaseForProjectLocked(userID, workspaceID, projectID, caseID)
	if err != nil {
		return TestCase{}, err
	}
	cases := []TestCase{testCaseSnapshot(testCase)}
	s.attachLatestTestCaseResultsLocked(cases)
	return cases[0], nil
}

func (s *MemoryStore) UpdateProjectTestCase(_ Context, userID, workspaceID, projectID, caseID string, input TestCaseInput) (TestCase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := s.testCaseForProjectLocked(userID, workspaceID, projectID, caseID)
	if err != nil {
		return TestCase{}, err
	}
	_ = existing
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return s.updateProjectTestCaseLocked(userID, workspaceID, projectID, caseID, input, now)
}

func (s *MemoryStore) DeleteProjectTestCase(_ Context, userID, workspaceID, projectID, caseID string) (TestCase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	caseID = strings.TrimSpace(caseID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return TestCase{}, ErrNotFound
	}
	existing, err := s.testCaseForProjectLocked(userID, workspaceID, projectID, caseID)
	if err != nil {
		return TestCase{}, err
	}
	return s.archiveProjectTestCaseLocked(userID, workspaceID, projectID, existing.ID, time.Now().UTC().Format(time.RFC3339Nano))
}

func (s *MemoryStore) DeleteProjectTestCases(_ Context, userID, workspaceID, projectID string, input DeleteProjectTestCasesInput) ([]TestCase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	if _, err := s.projectForTestCasesLocked(workspaceID, projectID); err != nil {
		return nil, err
	}
	caseIDs := normalizeTestCaseIDList(input.CaseIDs)
	if len(caseIDs) == 0 {
		return nil, errors.New("caseIds is required")
	}
	for _, caseID := range caseIDs {
		if _, err := s.testCaseForProjectLocked(userID, workspaceID, projectID, caseID); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	archived := make([]TestCase, 0, len(caseIDs))
	for _, caseID := range caseIDs {
		testCase, err := s.archiveProjectTestCaseLocked(userID, workspaceID, projectID, caseID, now)
		if err != nil {
			return nil, err
		}
		archived = append(archived, testCase)
	}
	return archived, nil
}

func (s *MemoryStore) ListProjectTestCaseRevisions(_ Context, userID, workspaceID, projectID, caseID string) ([]TestCaseRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.testCaseForProjectLocked(userID, workspaceID, projectID, caseID); err != nil {
		return nil, err
	}
	revisions := []TestCaseRevision{}
	for _, revision := range s.testCaseRevisions {
		if revision.WorkspaceID == strings.TrimSpace(workspaceID) && revision.ProjectID == strings.TrimSpace(projectID) && revision.TestCaseID == strings.TrimSpace(caseID) {
			item := revision
			item.Snapshot = testCaseSnapshot(item.Snapshot)
			revisions = append(revisions, item)
		}
	}
	sort.Slice(revisions, func(i, j int) bool {
		return revisions[i].RevisionNumber > revisions[j].RevisionNumber
	})
	return revisions, nil
}

func (s *MemoryStore) ListProjectTestCaseProposals(_ Context, userID, workspaceID, projectID string, options TestCaseProposalListOptions) ([]TestCaseProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	if _, err := s.projectForTestCasesLocked(workspaceID, projectID); err != nil {
		return nil, err
	}
	status := normalizeProposalStatus(options.Status)
	if strings.TrimSpace(options.Status) != "" && status == "" {
		return nil, errors.New("status must be pending, applied, rejected, or invalid")
	}
	proposals := []TestCaseProposal{}
	for _, proposal := range s.testCaseProposals {
		if proposal.WorkspaceID != workspaceID || proposal.ProjectID != projectID {
			continue
		}
		if status != "" && proposal.Status != status {
			continue
		}
		proposals = append(proposals, s.testCaseProposalSnapshotLocked(proposal))
	}
	sort.Slice(proposals, func(i, j int) bool {
		return proposals[i].UpdatedAt > proposals[j].UpdatedAt
	})
	return proposals, nil
}

func (s *MemoryStore) ApplyProjectTestCaseProposal(_ Context, userID, workspaceID, projectID, proposalID string, input ReviewTestCaseProposalInput) (ApplyTestCaseProposalResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	proposal, err := s.testCaseProposalForProjectLocked(userID, workspaceID, projectID, proposalID)
	if err != nil {
		return ApplyTestCaseProposalResult{}, err
	}
	if proposal.Status != "pending" {
		return ApplyTestCaseProposalResult{}, ErrConflict
	}
	if len(proposal.ValidationErrors) > 0 {
		return ApplyTestCaseProposalResult{}, errors.New("proposal has validation errors")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var applied *TestCase
	switch proposal.ProposalType {
	case "create":
		normalized := proposal.ProposedCase
		normalized.Source = "codex_generated"
		if normalized.Status == "" {
			normalized.Status = "needs_review"
		}
		testCase, err := s.createProjectTestCaseLocked(userID, proposal.WorkspaceID, proposal.ProjectID, normalized, now)
		if err != nil {
			return ApplyTestCaseProposalResult{}, err
		}
		applied = cloneTestCasePointer(testCase)
		proposal.AppliedCaseID = testCase.ID
	case "update", "archive":
		existing, ok := s.testCases[proposal.TargetCaseID]
		if !ok || existing.WorkspaceID != proposal.WorkspaceID || existing.ProjectID != proposal.ProjectID {
			return ApplyTestCaseProposalResult{}, ErrNotFound
		}
		next := proposal.ProposedCase
		if next.Source == "" {
			next.Source = "codex_refined"
		}
		if proposal.ProposalType == "archive" {
			next = testCaseToInput(existing)
			next.Status = "archived"
			next.Source = "codex_refined"
		}
		updated, err := s.updateProjectTestCaseLocked(userID, proposal.WorkspaceID, proposal.ProjectID, proposal.TargetCaseID, next, now)
		if err != nil {
			return ApplyTestCaseProposalResult{}, err
		}
		applied = cloneTestCasePointer(updated)
		proposal.AppliedCaseID = updated.ID
	default:
		return ApplyTestCaseProposalResult{}, errors.New("proposal type must be create, update, or archive")
	}
	proposal.Status = "applied"
	proposal.ReviewedByUserID = strings.TrimSpace(userID)
	proposal.ReviewNote = normalizeReviewNote(input.Note)
	proposal.ReviewedAt = now
	proposal.UpdatedAt = now
	s.testCaseProposals[proposal.ID] = proposal
	syncTestRunCaseSnapshotLocked(s.testRunItems, applied)
	snapshot := s.testCaseProposalSnapshotLocked(proposal)
	return ApplyTestCaseProposalResult{Proposal: snapshot, TestCase: applied}, nil
}

func (s *MemoryStore) RejectProjectTestCaseProposal(_ Context, userID, workspaceID, projectID, proposalID string, input ReviewTestCaseProposalInput) (TestCaseProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	proposal, err := s.testCaseProposalForProjectLocked(userID, workspaceID, projectID, proposalID)
	if err != nil {
		return TestCaseProposal{}, err
	}
	if proposal.Status != "pending" && proposal.Status != "invalid" {
		return TestCaseProposal{}, ErrConflict
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	proposal.Status = "rejected"
	proposal.ReviewedByUserID = strings.TrimSpace(userID)
	proposal.ReviewNote = normalizeReviewNote(input.Note)
	proposal.ReviewedAt = now
	proposal.UpdatedAt = now
	s.testCaseProposals[proposal.ID] = proposal
	return s.testCaseProposalSnapshotLocked(proposal), nil
}

func (s *MemoryStore) ListWorkspaceTestPlans(_ Context, userID, workspaceID string, options TestPlanListOptions) ([]TestPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	status := strings.ToLower(strings.TrimSpace(options.Status))
	if status != "" && status != "draft" && status != "ready" && status != "archived" {
		return nil, errors.New("status must be draft, ready, or archived")
	}
	plans := []TestPlan{}
	for _, plan := range s.testPlans {
		if plan.WorkspaceID != workspaceID {
			continue
		}
		if status != "" && plan.Status != status {
			continue
		}
		item := plan
		item.CaseCount = s.testPlanCaseCountLocked(plan.ID)
		plans = append(plans, item)
	}
	sort.Slice(plans, func(i, j int) bool {
		return plans[i].UpdatedAt > plans[j].UpdatedAt
	})
	return plans, nil
}

func (s *MemoryStore) ListProjectTestPlans(ctx Context, userID, workspaceID, projectID string, options TestPlanListOptions) ([]TestPlan, error) {
	plans, err := s.ListWorkspaceTestPlans(ctx, userID, workspaceID, options)
	if err != nil {
		return nil, err
	}
	projectID = strings.TrimSpace(projectID)
	filtered := []TestPlan{}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.projectForTestCasesLocked(strings.TrimSpace(workspaceID), projectID); err != nil {
		return nil, err
	}
	for _, plan := range plans {
		detail, err := s.testPlanDetailLocked(plan.ID)
		if err != nil {
			return nil, err
		}
		if testPlanDetailIncludesProject(detail, projectID) {
			filtered = append(filtered, plan)
		}
	}
	return filtered, nil
}

func (s *MemoryStore) CreateWorkspaceTestPlan(_ Context, userID, workspaceID string, input TestPlanInput) (TestPlanDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return TestPlanDetail{}, ErrNotFound
	}
	normalized, err := normalizeTestPlanInput(input)
	if err != nil {
		return TestPlanDetail{}, err
	}
	if err := s.resolveTestPlanEnvironmentLocked(workspaceID, &normalized); err != nil {
		return TestPlanDetail{}, err
	}
	caseInputs, err := normalizedPlanCaseInputs(normalized, "")
	if err != nil {
		return TestPlanDetail{}, err
	}
	cases, err := s.testCasesForPlanLocked(workspaceID, caseInputs, true, "test plan can only include ready test cases")
	if err != nil {
		return TestPlanDetail{}, err
	}
	projectID := primaryProjectIDFromCases(cases)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	plan := TestPlan{
		ID:                  fmt.Sprintf("test-plan-%04d", s.nextMemoryIDLocked()),
		WorkspaceID:         workspaceID,
		ProjectID:           projectID,
		Title:               normalized.Title,
		Description:         normalized.Description,
		SetupSteps:          normalized.SetupSteps,
		Status:              normalized.Status,
		TargetType:          normalized.TargetType,
		TargetValue:         normalized.TargetValue,
		Environment:         normalized.Environment,
		EnvironmentID:       normalized.EnvironmentID,
		EnvironmentKind:     normalized.EnvironmentKind,
		EnvironmentSnapshot: cloneRawJSONObject(normalized.EnvironmentSnapshot),
		CaseCount:           len(cases),
		CreatedByUserID:     strings.TrimSpace(userID),
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	s.testPlans[plan.ID] = plan
	s.replaceTestPlanCasesLocked(plan, cases, now)
	return s.testPlanDetailLocked(plan.ID)
}

func (s *MemoryStore) CreateProjectTestPlan(ctx Context, userID, workspaceID, projectID string, input TestPlanInput) (TestPlanDetail, error) {
	s.mu.Lock()
	if _, err := s.projectForTestCasesLocked(strings.TrimSpace(workspaceID), strings.TrimSpace(projectID)); err != nil {
		s.mu.Unlock()
		return TestPlanDetail{}, err
	}
	s.mu.Unlock()
	caseInputs, err := normalizedPlanCaseInputs(input, projectID)
	if err != nil {
		return TestPlanDetail{}, err
	}
	input.CaseIDs = nil
	input.Cases = caseInputs
	return s.CreateWorkspaceTestPlan(ctx, userID, workspaceID, input)
}

func (s *MemoryStore) GetWorkspaceTestPlan(_ Context, userID, workspaceID, planID string) (TestPlanDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.testPlanForWorkspaceLocked(userID, workspaceID, planID); err != nil {
		return TestPlanDetail{}, err
	}
	return s.testPlanDetailLocked(strings.TrimSpace(planID))
}

func (s *MemoryStore) GetProjectTestPlan(ctx Context, userID, workspaceID, projectID, planID string) (TestPlanDetail, error) {
	detail, err := s.GetWorkspaceTestPlan(ctx, userID, workspaceID, planID)
	if err != nil {
		return TestPlanDetail{}, err
	}
	if !testPlanDetailIncludesProject(detail, strings.TrimSpace(projectID)) {
		return TestPlanDetail{}, ErrNotFound
	}
	return detail, nil
}

func (s *MemoryStore) UpdateWorkspaceTestPlan(_ Context, userID, workspaceID, planID string, input TestPlanInput) (TestPlanDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	plan, err := s.testPlanForWorkspaceLocked(userID, workspaceID, planID)
	if err != nil {
		return TestPlanDetail{}, err
	}
	normalized, err := normalizeTestPlanInput(input)
	if err != nil {
		return TestPlanDetail{}, err
	}
	if err := s.resolveTestPlanEnvironmentLocked(plan.WorkspaceID, &normalized); err != nil {
		return TestPlanDetail{}, err
	}
	caseInputs, err := normalizedPlanCaseInputs(normalized, "")
	if err != nil {
		return TestPlanDetail{}, err
	}
	cases, err := s.testCasesForPlanLocked(plan.WorkspaceID, caseInputs, true, "test plan can only include ready test cases")
	if err != nil {
		return TestPlanDetail{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	plan.Title = normalized.Title
	plan.Description = normalized.Description
	plan.SetupSteps = normalized.SetupSteps
	plan.Status = normalized.Status
	plan.TargetType = normalized.TargetType
	plan.TargetValue = normalized.TargetValue
	if projectID := primaryProjectIDFromCases(cases); projectID != "" {
		plan.ProjectID = projectID
	}
	plan.Environment = normalized.Environment
	plan.EnvironmentID = normalized.EnvironmentID
	plan.EnvironmentKind = normalized.EnvironmentKind
	plan.EnvironmentSnapshot = cloneRawJSONObject(normalized.EnvironmentSnapshot)
	plan.CaseCount = len(cases)
	plan.UpdatedAt = now
	s.testPlans[plan.ID] = plan
	s.replaceTestPlanCasesLocked(plan, cases, now)
	return s.testPlanDetailLocked(plan.ID)
}

func (s *MemoryStore) UpdateProjectTestPlan(ctx Context, userID, workspaceID, projectID, planID string, input TestPlanInput) (TestPlanDetail, error) {
	caseInputs, err := normalizedPlanCaseInputs(input, projectID)
	if err != nil {
		return TestPlanDetail{}, err
	}
	input.CaseIDs = nil
	input.Cases = caseInputs
	detail, err := s.UpdateWorkspaceTestPlan(ctx, userID, workspaceID, planID, input)
	if err != nil {
		return TestPlanDetail{}, err
	}
	if !testPlanDetailIncludesProject(detail, strings.TrimSpace(projectID)) {
		return TestPlanDetail{}, ErrNotFound
	}
	return detail, nil
}

func (s *MemoryStore) resolveTestPlanEnvironmentLocked(workspaceID string, input *TestPlanInput) error {
	if input == nil || strings.TrimSpace(input.EnvironmentID) == "" {
		if input != nil {
			input.EnvironmentKind = ""
			input.EnvironmentSnapshot = json.RawMessage(`{}`)
		}
		return nil
	}
	environment, err := s.environmentLocked(workspaceID, input.EnvironmentID)
	if err != nil {
		return err
	}
	if input.EnvironmentKind != "" && input.EnvironmentKind != environment.Kind {
		return errors.New("environmentKind does not match selected environment")
	}
	input.EnvironmentID = environment.ID
	input.EnvironmentKind = environment.Kind
	input.EnvironmentSnapshot = environmentSnapshot(environment)
	if strings.TrimSpace(input.Environment) == "" {
		input.Environment = environment.Name
	}
	return nil
}

func (s *MemoryStore) resolveTestRunEnvironmentLocked(workspaceID string, input *CreateTestRunInput) error {
	if input == nil || strings.TrimSpace(input.EnvironmentID) == "" {
		if input != nil {
			input.EnvironmentKind = ""
			input.EnvironmentSnapshot = json.RawMessage(`{}`)
		}
		return nil
	}
	environment, err := s.environmentLocked(workspaceID, input.EnvironmentID)
	if err != nil {
		return err
	}
	if input.EnvironmentKind != "" && input.EnvironmentKind != environment.Kind {
		return errors.New("environmentKind does not match selected environment")
	}
	input.EnvironmentID = environment.ID
	input.EnvironmentKind = environment.Kind
	input.EnvironmentSnapshot = environmentSnapshot(environment)
	if strings.TrimSpace(input.Environment) == "" {
		input.Environment = environment.Name
	}
	return nil
}

func (s *MemoryStore) environmentLocked(workspaceID, environmentID string) (Environment, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	environmentID = strings.TrimSpace(environmentID)
	if cluster, ok := s.clusters[environmentID]; ok && cluster.WorkspaceID == workspaceID {
		return environmentFromCluster(cluster), nil
	}
	environment, ok := s.environments[environmentID]
	if !ok || environment.WorkspaceID != workspaceID {
		return Environment{}, ErrNotFound
	}
	return environment, nil
}

func (s *MemoryStore) StartWorkspaceTestRun(_ Context, user User, workspaceID, planID string, input CreateTestRunInput) (TestRunDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	plan, err := s.testPlanForWorkspaceLocked(user.ID, workspaceID, planID)
	if err != nil {
		return TestRunDetail{}, err
	}
	cases := s.testCasesForPlanIDLocked(plan.ID)
	if len(cases) == 0 {
		return TestRunDetail{}, errors.New("plan has no test cases")
	}
	normalized, err := normalizeCreateTestRunInput(input, plan)
	if err != nil {
		return TestRunDetail{}, err
	}
	if err := s.resolveTestRunEnvironmentLocked(plan.WorkspaceID, &normalized); err != nil {
		return TestRunDetail{}, err
	}
	return s.startProjectTestRunLocked(user, &plan, cases, normalized)
}

func (s *MemoryStore) StartProjectTestRun(ctx Context, user User, workspaceID, projectID, planID string, input CreateTestRunInput) (TestRunDetail, error) {
	detail, err := s.StartWorkspaceTestRun(ctx, user, workspaceID, planID, input)
	if err != nil {
		return TestRunDetail{}, err
	}
	if !testRunDetailIncludesProject(detail, strings.TrimSpace(projectID)) {
		return TestRunDetail{}, ErrNotFound
	}
	return detail, nil
}

func (s *MemoryStore) StartAdHocWorkspaceTestRun(_ Context, user User, workspaceID string, input CreateAdHocTestRunInput) (TestRunDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, user.ID) {
		return TestRunDetail{}, ErrNotFound
	}
	normalized, err := normalizeCreateAdHocTestRunInput(input)
	if err != nil {
		return TestRunDetail{}, err
	}
	caseInputs, err := normalizedAdHocRunCaseInputs(normalized, "")
	if err != nil {
		return TestRunDetail{}, err
	}
	cases, err := s.testCasesForPlanLocked(workspaceID, caseInputs, true, "test run can only include ready test cases")
	if err != nil {
		return TestRunDetail{}, err
	}
	runInput := CreateTestRunInput{
		TargetType:      normalized.TargetType,
		TargetValue:     normalized.TargetValue,
		Environment:     normalized.Environment,
		EnvironmentID:   normalized.EnvironmentID,
		EnvironmentKind: normalized.EnvironmentKind,
		AgentEngine:     normalized.AgentEngine,
		RuntimeMode:     normalized.RuntimeMode,
		BatchSize:       normalized.BatchSize,
		ResultLocale:    normalized.ResultLocale,
	}
	if err := s.resolveTestRunEnvironmentLocked(workspaceID, &runInput); err != nil {
		return TestRunDetail{}, err
	}
	return s.startProjectTestRunLocked(user, nil, cases, runInput)
}

func (s *MemoryStore) StartAdHocProjectTestRun(ctx Context, user User, workspaceID, projectID string, input CreateAdHocTestRunInput) (TestRunDetail, error) {
	caseInputs, err := normalizedAdHocRunCaseInputs(input, projectID)
	if err != nil {
		return TestRunDetail{}, err
	}
	input.CaseIDs = nil
	input.Cases = caseInputs
	detail, err := s.StartAdHocWorkspaceTestRun(ctx, user, workspaceID, input)
	if err != nil {
		return TestRunDetail{}, err
	}
	if !testRunDetailIncludesProject(detail, strings.TrimSpace(projectID)) {
		return TestRunDetail{}, ErrNotFound
	}
	return detail, nil
}

func (s *MemoryStore) startProjectTestRunLocked(user User, plan *TestPlan, cases []TestCase, input CreateTestRunInput) (TestRunDetail, error) {
	if len(cases) == 0 {
		return TestRunDetail{}, errors.New("test run has no test cases")
	}
	workspaceID := cases[0].WorkspaceID
	projectID := cases[0].ProjectID
	if input.RuntimeMode == "" {
		workspace, ok := s.workspaceLocked(workspaceID)
		if !ok {
			return TestRunDetail{}, ErrNotFound
		}
		input.RuntimeMode = workspace.Kind
	}
	if !s.hasActiveCodexWorkerLocked(workspaceID, input.RuntimeMode, time.Now().UTC()) {
		return TestRunDetail{}, ErrNoActiveCodexWorker
	}
	setupSteps := ""
	if plan != nil {
		setupSteps = normalizeTestPlanSetupSteps(plan.SetupSteps)
	}
	if err := s.ensureActiveWorkersForTestRunExecutionLocked(workspaceID, input.RuntimeMode, input.BatchSize, cases); err != nil {
		return TestRunDetail{}, err
	}
	runSource := "ad_hoc"
	planID := ""
	if plan != nil {
		runSource = "plan"
		planID = plan.ID
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	run := TestRun{
		ID:                  fmt.Sprintf("test-run-%04d", s.nextMemoryIDLocked()),
		WorkspaceID:         workspaceID,
		ProjectID:           projectID,
		PlanID:              planID,
		Source:              runSource,
		Status:              "running",
		SetupStatus:         "not_required",
		SetupResult:         json.RawMessage(`{}`),
		RunContext:          json.RawMessage(`{}`),
		TargetType:          input.TargetType,
		TargetValue:         input.TargetValue,
		Environment:         input.Environment,
		EnvironmentID:       input.EnvironmentID,
		EnvironmentKind:     input.EnvironmentKind,
		EnvironmentSnapshot: cloneRawJSONObject(input.EnvironmentSnapshot),
		ResultLocale:        input.ResultLocale,
		TotalCount:          len(cases),
		AcceptanceStatus:    "pending",
		CreatedByUserID:     user.ID,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if plan != nil {
		run.SetupSteps = setupSteps
		if run.SetupSteps != "" {
			run.Status = "setup_running"
			run.SetupStatus = "running"
		}
	}
	parentIssueID, err := s.createIssueLocked(user, run.WorkspaceID, CreateIssueInput{
		ProjectID:   run.ProjectID,
		Title:       testRunTitle(plan, run, cases),
		TitleSource: issueTitleSourcePlainText,
		Body:        buildTestRunParentIssueBody(plan, run, cases),
		LabelKeys:   []string{"type:test"},
	})
	if err != nil {
		return TestRunDetail{}, err
	}
	run.ParentIssueID = parentIssueID
	s.testRuns[run.ID] = run
	for index, testCase := range cases {
		item := TestRunItem{
			ID:          fmt.Sprintf("test-run-item-%04d", s.nextMemoryIDLocked()),
			WorkspaceID: run.WorkspaceID,
			ProjectID:   testCase.ProjectID,
			RunID:       run.ID,
			TestCaseID:  testCase.ID,
			SortOrder:   index + 1,
			Status:      "queued",
			Evidence:    json.RawMessage(`{}`),
			TestCase:    testCaseSnapshot(testCase),
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		s.testRunItems[item.ID] = item
	}
	if run.SetupSteps != "" {
		if err := s.startTestRunSetupSessionLocked(user.ID, run.ID, input); err != nil {
			return TestRunDetail{}, err
		}
	} else {
		if err := s.startTestRunExecutionSessionsLocked(user.ID, run.ID, input); err != nil {
			return TestRunDetail{}, err
		}
	}
	return s.testRunDetailLocked(run.ID)
}

func (s *MemoryStore) ensureActiveWorkersForTestRunExecutionLocked(workspaceID, runtimeMode string, batchSize int, cases []TestCase) error {
	requiredSets, err := testRunExecutionCapabilitySets(cases, batchSize)
	if err != nil {
		return err
	}
	for _, requiredCapabilities := range requiredSets {
		if !s.hasActiveWorkerWithCapabilitiesLocked(workspaceID, runtimeMode, requiredCapabilities, time.Now().UTC()) {
			return ErrNoActiveCodexWorker
		}
	}
	return nil
}

func (s *MemoryStore) ListWorkspaceTestRuns(_ Context, userID, workspaceID string, options TestRunListOptions) ([]TestRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	status := strings.ToLower(strings.TrimSpace(options.Status))
	source := normalizeTestRunSource(options.Source)
	if strings.TrimSpace(options.Source) != "" && source == "" {
		return nil, errors.New("source must be ad_hoc, plan, retry, or incremental")
	}
	runs := []TestRun{}
	for _, run := range s.testRuns {
		if run.WorkspaceID != workspaceID {
			continue
		}
		if status != "" && run.Status != status {
			continue
		}
		if source != "" && run.Source != source {
			continue
		}
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].UpdatedAt > runs[j].UpdatedAt
	})
	return runs, nil
}

func (s *MemoryStore) ListProjectTestRuns(ctx Context, userID, workspaceID, projectID string, options TestRunListOptions) ([]TestRun, error) {
	runs, err := s.ListWorkspaceTestRuns(ctx, userID, workspaceID, options)
	if err != nil {
		return nil, err
	}
	projectID = strings.TrimSpace(projectID)
	filtered := []TestRun{}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.projectForTestCasesLocked(strings.TrimSpace(workspaceID), projectID); err != nil {
		return nil, err
	}
	for _, run := range runs {
		detail, err := s.testRunDetailLocked(run.ID)
		if err != nil {
			return nil, err
		}
		if testRunDetailIncludesProject(detail, projectID) {
			filtered = append(filtered, run)
		}
	}
	return filtered, nil
}

func (s *MemoryStore) GetWorkspaceTestRun(_ Context, userID, workspaceID, runID string) (TestRunDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.testRunForWorkspaceLocked(userID, workspaceID, runID); err != nil {
		return TestRunDetail{}, err
	}
	return s.testRunDetailLocked(strings.TrimSpace(runID))
}

func (s *MemoryStore) GetProjectTestRun(ctx Context, userID, workspaceID, projectID, runID string) (TestRunDetail, error) {
	detail, err := s.GetWorkspaceTestRun(ctx, userID, workspaceID, runID)
	if err != nil {
		return TestRunDetail{}, err
	}
	if !testRunDetailIncludesProject(detail, strings.TrimSpace(projectID)) {
		return TestRunDetail{}, ErrNotFound
	}
	return detail, nil
}

func (s *MemoryStore) ListProjectTestCaseRunItems(_ Context, userID, workspaceID, projectID, caseID string) ([]TestCaseRunItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	testCase, err := s.testCaseForProjectLocked(userID, workspaceID, projectID, caseID)
	if err != nil {
		return nil, err
	}
	items := []TestCaseRunItem{}
	for _, item := range s.testRunItems {
		if item.WorkspaceID != testCase.WorkspaceID || item.ProjectID != testCase.ProjectID || item.TestCaseID != testCase.ID {
			continue
		}
		run, ok := s.testRuns[item.RunID]
		if !ok {
			continue
		}
		copyItem := item
		copyItem.TestCase = testCaseSnapshot(testCase)
		items = append(items, TestCaseRunItem{Item: copyItem, Run: run})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Item.UpdatedAt != items[j].Item.UpdatedAt {
			return items[i].Item.UpdatedAt > items[j].Item.UpdatedAt
		}
		if items[i].Run.UpdatedAt != items[j].Run.UpdatedAt {
			return items[i].Run.UpdatedAt > items[j].Run.UpdatedAt
		}
		return items[i].Item.ID > items[j].Item.ID
	})
	return items, nil
}

func (s *MemoryStore) RetryWorkspaceTestRun(_ Context, user User, workspaceID, runID string, input RetryTestRunInput) (TestRunDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	run, err := s.testRunForWorkspaceLocked(user.ID, workspaceID, runID)
	if err != nil {
		return TestRunDetail{}, err
	}
	normalized, err := normalizeRetryTestRunInput(input)
	if err != nil {
		return TestRunDetail{}, err
	}
	if normalized.RuntimeMode == "" {
		workspace, ok := s.workspaceLocked(run.WorkspaceID)
		if !ok {
			return TestRunDetail{}, ErrNotFound
		}
		normalized.RuntimeMode = workspace.Kind
	}
	if !s.hasActiveCodexWorkerLocked(run.WorkspaceID, normalized.RuntimeMode, time.Now().UTC()) {
		return TestRunDetail{}, ErrNoActiveCodexWorker
	}
	retryCases, err := s.testRunCasesForRetryLocked(run.ID, normalized.ItemIDs)
	if err != nil {
		return TestRunDetail{}, err
	}
	if err := s.ensureActiveWorkersForTestRunExecutionLocked(run.WorkspaceID, normalized.RuntimeMode, defaultTestRunBatchSize, retryCases); err != nil {
		return TestRunDetail{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for id, item := range s.testRunItems {
		if item.RunID != run.ID {
			continue
		}
		if len(normalized.ItemIDs) > 0 && !containsString(normalized.ItemIDs, item.ID) && !containsString(normalized.ItemIDs, item.TestCaseID) {
			continue
		}
		if len(normalized.ItemIDs) == 0 && item.Status != "failed" && item.Status != "blocked" {
			continue
		}
		item.Status = "queued"
		item.ActualResult = ""
		item.FailureSummary = ""
		item.Evidence = json.RawMessage(`{}`)
		item.AgentSessionID = ""
		item.UpdatedAt = now
		s.testRunItems[id] = item
	}
	run.Status = "running"
	run.AcceptanceStatus = "pending"
	run.AcceptanceNote = ""
	run.ResultLocale = normalized.ResultLocale
	run.UpdatedAt = now
	s.testRuns[run.ID] = run
	createRunInput := CreateTestRunInput{AgentEngine: normalized.AgentEngine, RuntimeMode: normalized.RuntimeMode, BatchSize: defaultTestRunBatchSize, ResultLocale: normalized.ResultLocale}
	if err := s.startTestRunExecutionSessionsLocked(user.ID, run.ID, createRunInput); err != nil {
		return TestRunDetail{}, err
	}
	return s.testRunDetailLocked(run.ID)
}

func (s *MemoryStore) testRunCasesForRetryLocked(runID string, itemIDs []string) ([]TestCase, error) {
	cases := []TestCase{}
	for _, item := range s.testRunItems {
		if item.RunID != strings.TrimSpace(runID) {
			continue
		}
		if len(itemIDs) > 0 && !containsString(itemIDs, item.ID) && !containsString(itemIDs, item.TestCaseID) {
			continue
		}
		if len(itemIDs) == 0 && item.Status != "failed" && item.Status != "blocked" {
			continue
		}
		testCase, ok := s.testCases[item.TestCaseID]
		if !ok {
			continue
		}
		cases = append(cases, testCaseSnapshot(testCase))
	}
	if len(cases) == 0 {
		return nil, ErrNotFound
	}
	return cases, nil
}

func (s *MemoryStore) RetryProjectTestRun(ctx Context, user User, workspaceID, projectID, runID string, input RetryTestRunInput) (TestRunDetail, error) {
	detail, err := s.RetryWorkspaceTestRun(ctx, user, workspaceID, runID, input)
	if err != nil {
		return TestRunDetail{}, err
	}
	if !testRunDetailIncludesProject(detail, strings.TrimSpace(projectID)) {
		return TestRunDetail{}, ErrNotFound
	}
	return detail, nil
}

func (s *MemoryStore) CancelWorkspaceTestRun(_ Context, user User, workspaceID, runID string, input CancelRuntimeTaskInput) (TestRunDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	run, err := s.testRunForWorkspaceLocked(user.ID, workspaceID, runID)
	if err != nil {
		return TestRunDetail{}, err
	}
	if !isCancellableTestRunStatus(run.Status) {
		return TestRunDetail{}, ErrNotFound
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	reason := normalizeRuntimeTaskCancelReason(input.Reason)
	sessionIDs := []string{}
	if strings.TrimSpace(run.SetupSessionID) != "" {
		sessionIDs = append(sessionIDs, strings.TrimSpace(run.SetupSessionID))
	}
	for id, item := range s.testRunItems {
		if item.WorkspaceID != run.WorkspaceID || item.RunID != run.ID {
			continue
		}
		if item.Status != "queued" && item.Status != "running" {
			continue
		}
		if strings.TrimSpace(item.AgentSessionID) != "" {
			sessionIDs = append(sessionIDs, strings.TrimSpace(item.AgentSessionID))
		}
		item.Status = "cancelled"
		if item.ActualResult == "" {
			item.ActualResult = reason
		}
		if item.FailureSummary == "" {
			item.FailureSummary = reason
		}
		item.UpdatedAt = now
		s.testRunItems[id] = item
	}
	s.cancelRuntimeTasksBySessionIDsLocked(run.WorkspaceID, sessionIDs, user.ID, reason, run.ID)
	if run.SetupStatus == "queued" || run.SetupStatus == "running" {
		run.SetupStatus = "cancelled"
	}
	run.Status = "cancelled"
	run.AcceptanceStatus = "pending"
	run.AcceptanceNote = ""
	run.CompletedAt = now
	run.UpdatedAt = now
	s.testRuns[run.ID] = run
	return s.testRunDetailLocked(run.ID)
}

func (s *MemoryStore) CancelProjectTestRun(ctx Context, user User, workspaceID, projectID, runID string, input CancelRuntimeTaskInput) (TestRunDetail, error) {
	if _, err := s.GetProjectTestRun(ctx, user.ID, workspaceID, projectID, runID); err != nil {
		return TestRunDetail{}, err
	}
	return s.CancelWorkspaceTestRun(ctx, user, workspaceID, runID, input)
}

func (s *MemoryStore) AcceptWorkspaceTestRun(_ Context, userID, workspaceID, runID string, input ReviewTestRunInput) (TestRun, error) {
	return s.reviewTestRun(userID, workspaceID, runID, input, "accepted")
}

func (s *MemoryStore) BlockWorkspaceTestRun(_ Context, userID, workspaceID, runID string, input ReviewTestRunInput) (TestRun, error) {
	return s.reviewTestRun(userID, workspaceID, runID, input, "blocked")
}

func (s *MemoryStore) AcceptProjectTestRun(ctx Context, userID, workspaceID, projectID, runID string, input ReviewTestRunInput) (TestRun, error) {
	if _, err := s.GetProjectTestRun(ctx, userID, workspaceID, projectID, runID); err != nil {
		return TestRun{}, err
	}
	return s.AcceptWorkspaceTestRun(ctx, userID, workspaceID, runID, input)
}

func (s *MemoryStore) BlockProjectTestRun(ctx Context, userID, workspaceID, projectID, runID string, input ReviewTestRunInput) (TestRun, error) {
	if _, err := s.GetProjectTestRun(ctx, userID, workspaceID, projectID, runID); err != nil {
		return TestRun{}, err
	}
	return s.BlockWorkspaceTestRun(ctx, userID, workspaceID, runID, input)
}

func (s *MemoryStore) ListIssueLabelDefinitions(_ Context, userID, workspaceID string) ([]IssueLabelDefinition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isWorkspaceMember(workspaceID, userID) {
		return nil, ErrNotFound
	}
	return builtInIssueLabelDefinitions(), nil
}

func (s *MemoryStore) ListIssues(_ Context, userID, workspaceID string, options IssueListOptions) ([]IssueListItem, error) {
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
		if !options.IncludeTestAutomation && s.isTestAutomationIssueLocked(issue) {
			continue
		}
		items = append(items, s.issueListItemLocked(issue))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt > items[j].UpdatedAt
	})
	return items, nil
}

func (s *MemoryStore) isTestAutomationIssueLocked(issue Issue) bool {
	issueID := strings.TrimSpace(issue.ID)
	if issueID == "" {
		return false
	}
	if isLegacyTestAutomationIssue(issue.Title, issue.Body) {
		return true
	}
	for _, run := range s.testRuns {
		if run.ParentIssueID == issueID {
			return true
		}
	}
	for _, task := range s.runtimeTasks {
		if task.IssueID != issueID {
			continue
		}
		var payload struct {
			Automation string `json:"automation"`
		}
		_ = json.Unmarshal(task.Payload, &payload)
		switch payload.Automation {
		case testCaseOptimizationAutomation, testCaseGenerationAutomation:
			return true
		}
	}
	return false
}

func (s *MemoryStore) CreateIssue(_ Context, user User, workspaceID string, input CreateIssueInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.createIssueLocked(user, workspaceID, input)
}

func (s *MemoryStore) createIssueLocked(user User, workspaceID string, input CreateIssueInput) (string, error) {
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
	normalized, err := normalizeCreateAgentSessionInput(input)
	if err != nil {
		return AgentSession{}, err
	}
	if normalized.Command == "" {
		return AgentSession{}, errors.New("command is required")
	}
	if err := resolveAgentSessionSkillBundles(&normalized, func(slug string) (AgentSessionSkillReference, RuntimeSkillBundle, error) {
		return s.resolveAgentSessionSkillBundleLocked(workspaceID, slug)
	}); err != nil {
		return AgentSession{}, err
	}
	if normalized.RuntimeMode == "" {
		normalized.RuntimeMode = workspace.Kind
	}
	if normalized.RuntimeMode != "personal" && normalized.RuntimeMode != "team" {
		return AgentSession{}, errors.New("runtimeMode must be personal or team")
	}
	if normalized.RuntimeMode != workspace.Kind {
		return AgentSession{}, ErrForbidden
	}
	requiredCapabilities, err := agentSessionRequiredCapabilities(normalized)
	if err != nil {
		return AgentSession{}, err
	}
	if !s.hasActiveWorkerWithCapabilitiesLocked(workspaceID, normalized.RuntimeMode, requiredCapabilities, time.Now().UTC()) {
		return AgentSession{}, ErrNoActiveAgentWorker
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
		Priority:             agentSessionPriority(normalized),
		RuntimeMode:          normalized.RuntimeMode,
		RequiredCapabilities: requiredCapabilities,
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
	return s.queueIssueTestDeployLocked(userID, workspaceID, issueID, detail, input, false)
}

func (s *MemoryStore) queueIssueTestDeployLocked(userID, workspaceID, issueID string, detail IssueDetail, input StartTestDeployInput, automated bool) (TestEnvironmentSessionResult, error) {
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
	engine, err := requireCodexWorkflowAgentEngine(input.AgentEngine, input.LegacyProvider, input.LegacyAgentProfile)
	if err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	session, err := s.createAgentSessionLocked(userID, workspaceID, issueID, CreateAgentSessionInput{
		AgentEngine:     engine,
		Command:         buildIssueTestDeployPrompt(detail, environment, sourceNode, automated),
		SourceSessionID: sourceNode.SessionID,
		SourceCommitSHA: sourceNode.CommitSHA,
		Automation:      testDeployAutomationMarker(automated),
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
	engine, err := requireCodexWorkflowAgentEngine(input.AgentEngine, input.LegacyProvider, input.LegacyAgentProfile)
	if err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	session, err := s.createAgentSessionLocked(userID, workspaceID, issueID, CreateAgentSessionInput{
		AgentEngine: engine,
		Command:     buildIssueTestCleanupPrompt(detail, environment),
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
	environmentID := firstNonEmpty(input.EnvironmentID, input.ClusterID, environment.EnvironmentID, environment.ClusterID, detail.Project.DefaultEnvironmentID, detail.Project.DefaultClusterID)
	if environmentID == "" {
		return environment, errors.New("environment is required before starting a test deployment")
	}
	selectedEnvironment, err := s.environmentLocked(detail.Issue.WorkspaceID, environmentID)
	if err != nil {
		return environment, err
	}
	if selectedEnvironment.Kind != environmentKindKubernetes || selectedEnvironment.Kubernetes == nil {
		return environment, errors.New("test deployment currently requires a Kubernetes environment")
	}
	cluster, ok := s.clusters[selectedEnvironment.Kubernetes.ClusterID]
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
	environment.EnvironmentID = selectedEnvironment.ID
	environment.EnvironmentKind = selectedEnvironment.Kind
	environment.EnvironmentSnapshot = environmentSnapshot(selectedEnvironment)
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
	environment.EnvironmentID = firstNonEmpty(environment.EnvironmentID, environment.ClusterID)
	environment.EnvironmentKind = firstNonEmpty(environment.EnvironmentKind, environmentKindKubernetes)
	if len(environment.EnvironmentSnapshot) == 0 {
		environment.EnvironmentSnapshot = json.RawMessage(`{}`)
	}
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
	if runtimeTaskAutomation(task) == issueAnalysisAutomation {
		if task.Status == "failed" || task.Status == "cancelled" {
			s.storeRuntimeSessionFailureLocked(task, session)
		}
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
	if task.Status == "completed" && artifacts.TestCaseProposals != nil {
		s.storeTestCaseProposalArtifactsLocked(task, *artifacts.TestCaseProposals)
	}
	if runtimeTaskAutomation(task) == testRunSetupAutomation && isFinalRuntimeTaskStatus(task.Status) {
		s.reconcileTestSetupArtifactLocked(task, artifacts.TestSetup)
	}
	if task.Status == "completed" && artifacts.TestResult != nil {
		s.reconcileTestResultArtifactLocked(task, *artifacts.TestResult)
	}
	if task.Status == "failed" || task.Status == "cancelled" {
		s.storeRuntimeSessionFailureLocked(task, session)
	}
	if task.Status == "completed" {
		s.queueAutomaticTestDeployIfEnabledLocked(task)
	}
}

func (s *MemoryStore) queueAutomaticTestDeployIfEnabledLocked(task RuntimeTask) {
	if strings.TrimSpace(task.IssueID) == "" || isIssueTestDeployTask(task) || runtimeTaskIsDryRun(task) {
		return
	}
	source := runtimeTaskSource(task)
	if strings.TrimSpace(source.CommitSHA) == "" || strings.TrimSpace(source.Error) != "" {
		return
	}
	settings := s.workspaceSettingsLocked(task.WorkspaceID)
	if !settings.AutoDeployTestEnvironment {
		return
	}
	var userID string
	for _, event := range s.runtimeTaskEvents {
		if event.TaskID == task.ID && event.Kind == "created" {
			userID = event.ActorUserID
			break
		}
	}
	if userID == "" {
		return
	}
	issue, ok := s.issues[task.IssueID]
	if !ok || issue.ProjectID == "" {
		return
	}
	project, ok := s.projects[issue.ProjectID]
	if !ok {
		return
	}
	workspace, ok := s.workspaceLocked(task.WorkspaceID)
	if !ok {
		return
	}
	runtimeMode := firstNonEmpty(task.RuntimeMode, workspace.Kind)
	if !s.hasActiveCodexWorkerLocked(task.WorkspaceID, runtimeMode, time.Now().UTC()) {
		return
	}
	for _, candidate := range s.runtimeTasks {
		if candidate.WorkspaceID == task.WorkspaceID &&
			candidate.IssueID == task.IssueID &&
			candidate.ID != task.ID &&
			candidate.Kind == "agent_session" &&
			(candidate.Status == "queued" || candidate.Status == "claimed" || candidate.Status == "running") {
			return
		}
	}
	var existingEnvironment *IssueTestEnvironment
	if environment, ok := s.testEnvironments[task.IssueID]; ok {
		copy := environment
		existingEnvironment = &copy
	}
	detail := IssueDetail{
		Issue:           issue,
		Project:         project,
		TestEnvironment: existingEnvironment,
		ChildIssues:     []IssueListItem{},
		Labels:          s.issueLabels[task.IssueID],
		Comments:        []Comment{},
		Sessions:        []AgentSession{},
		ChangeNodes:     []IssueChangeNode{runtimeTaskChangeNode(task)},
	}
	_, _ = s.queueIssueTestDeployLocked(userID, task.WorkspaceID, task.IssueID, detail, StartTestDeployInput{}, true)
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
	expectedTitle, conditionalTitle, conditional, err := conditionalIssueTitleUpdate(input)
	if err != nil {
		return Issue{}, err
	}
	if conditional {
		if issue.Title != expectedTitle {
			return issue, nil
		}
		issue.Title = conditionalTitle
		issue.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		s.issues[issue.ID] = issue
		return issue, nil
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
		title := plainIssueTitleFromText(*input.Title)
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

func (s *MemoryStore) reconcileIssueTypeTriageRuntimeResultLocked(task RuntimeTask) {
	issue, ok := s.issues[strings.TrimSpace(task.IssueID)]
	if !ok || issue.WorkspaceID != strings.TrimSpace(task.WorkspaceID) {
		return
	}
	if task.Status != "completed" {
		if issue.TriageStatus == "pending" {
			issue.TriageStatus = "failed"
			issue.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			s.issues[issue.ID] = issue
		}
		return
	}
	result, err := parseIssueTypeTriageResult(string(task.Result))
	if err != nil {
		if issue.TriageStatus == "pending" {
			issue.TriageStatus = "failed"
			issue.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			s.issues[issue.ID] = issue
		}
		return
	}
	expectedTitle := issueTypeTriageExpectedTitle(task)
	titleChanged := false
	if expectedTitle != "" && result.Title != "" && expectedTitle != result.Title && issue.Title == expectedTitle {
		issue.Title = result.Title
		titleChanged = true
	}
	if issue.TriageStatus != "pending" {
		if titleChanged {
			issue.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			s.issues[issue.ID] = issue
		}
		return
	}
	labels, err := normalizeIssueLabelKeys([]string{"type:" + result.Type})
	if err != nil || !hasIssueLabelDimension(labels, issueLabelDimensionType) {
		issue.TriageStatus = "failed"
		issue.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		s.issues[issue.ID] = issue
		return
	}
	s.issueLabels[issue.ID] = replaceIssueLabelDimension(s.issueLabels[issue.ID], issueLabelDimensionType, labels[0])
	issue.TriageStatus = "classified"
	issue.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.issues[issue.ID] = issue
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

func (s *MemoryStore) createMemoryTestArtifactLocked(input CreateTestArtifactInput) (TestArtifact, error) {
	normalized, err := normalizeCreateTestArtifactInput(input)
	if err != nil {
		return TestArtifact{}, err
	}
	for _, artifact := range s.testArtifacts {
		if artifact.RunItemID == normalized.RunItemID {
			sum := sha256.Sum256(normalized.Content)
			if artifact.SHA256 == hex.EncodeToString(sum[:]) {
				return artifact, nil
			}
		}
	}
	sum := sha256.Sum256(normalized.Content)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	artifact := TestArtifact{
		ID:              fmt.Sprintf("test-artifact-%04d", s.nextMemoryIDLocked()),
		WorkspaceID:     normalized.WorkspaceID,
		ProjectID:       normalized.ProjectID,
		RunID:           normalized.RunID,
		RunItemID:       normalized.RunItemID,
		CaseID:          normalized.CaseID,
		SourceIssueID:   normalized.SourceIssueID,
		SourceTaskID:    normalized.SourceTaskID,
		SourceSessionID: normalized.SourceSessionID,
		Kind:            normalized.Kind,
		Role:            normalized.Role,
		Filename:        normalized.Filename,
		ContentType:     normalized.ContentType,
		SizeBytes:       int64(len(normalized.Content)),
		SHA256:          hex.EncodeToString(sum[:]),
		StorageBackend:  "memory_blob",
		Content:         append([]byte(nil), normalized.Content...),
		Metadata:        copyRawMessage(normalized.Metadata),
		CreatedAt:       now,
	}
	s.testArtifacts[artifact.ID] = artifact
	return artifact, nil
}

func (s *MemoryStore) ListWorkspaceTestRunArtifacts(_ Context, userID, workspaceID, runID string) ([]TestArtifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.testRunForWorkspaceLocked(userID, workspaceID, runID); err != nil {
		return nil, err
	}
	artifacts := []TestArtifact{}
	for _, artifact := range s.testArtifacts {
		if artifact.WorkspaceID == strings.TrimSpace(workspaceID) && artifact.RunID == strings.TrimSpace(runID) {
			copyArtifact := artifact
			copyArtifact.Content = append([]byte(nil), artifact.Content...)
			copyArtifact.Metadata = copyRawMessage(artifact.Metadata)
			artifacts = append(artifacts, copyArtifact)
		}
	}
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].CreatedAt != artifacts[j].CreatedAt {
			return artifacts[i].CreatedAt > artifacts[j].CreatedAt
		}
		return artifacts[i].ID > artifacts[j].ID
	})
	return artifacts, nil
}

func (s *MemoryStore) ListProjectTestRunArtifacts(ctx Context, userID, workspaceID, projectID, runID string) ([]TestArtifact, error) {
	if _, err := s.GetProjectTestRun(ctx, userID, workspaceID, projectID, runID); err != nil {
		return nil, err
	}
	artifacts, err := s.ListWorkspaceTestRunArtifacts(ctx, userID, workspaceID, runID)
	if err != nil {
		return nil, err
	}
	filtered := []TestArtifact{}
	projectID = strings.TrimSpace(projectID)
	for _, artifact := range artifacts {
		if artifact.ProjectID == projectID {
			filtered = append(filtered, artifact)
		}
	}
	return filtered, nil
}

func (s *MemoryStore) GetTestArtifact(_ Context, userID, artifactID string) (TestArtifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	artifact, ok := s.testArtifacts[strings.TrimSpace(artifactID)]
	if !ok {
		return TestArtifact{}, ErrNotFound
	}
	if !s.isWorkspaceMember(artifact.WorkspaceID, strings.TrimSpace(userID)) {
		return TestArtifact{}, ErrNotFound
	}
	copyArtifact := artifact
	copyArtifact.Content = append([]byte(nil), artifact.Content...)
	copyArtifact.Metadata = copyRawMessage(artifact.Metadata)
	return copyArtifact, nil
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
	result, err := s.ListRuntimeTasksPage(nil, userID, workspaceID, RuntimeTaskListOptions{Limit: maxRuntimeTaskListLimit})
	if err != nil {
		return nil, err
	}
	return result.Tasks, nil
}

func (s *MemoryStore) ListRuntimeTasksPage(_ Context, userID, workspaceID string, options RuntimeTaskListOptions) (RuntimeTaskListResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	options = normalizeRuntimeTaskListOptions(options)
	workspaceID = strings.TrimSpace(workspaceID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return RuntimeTaskListResult{}, ErrNotFound
	}
	tasks := []RuntimeTask{}
	statusCounts := map[string]int{}
	for _, task := range s.runtimeTasks {
		if task.WorkspaceID == workspaceID {
			tasks = append(tasks, task)
			statusCounts[task.Status]++
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		if compareTimestampStrings(tasks[i].CreatedAt, tasks[j].CreatedAt) == 0 {
			return tasks[i].ID > tasks[j].ID
		}
		return compareTimestampStrings(tasks[i].CreatedAt, tasks[j].CreatedAt) > 0
	})
	total := len(tasks)
	start := options.Offset
	if start > total {
		start = total
	}
	end := start + options.Limit
	if end > total {
		end = total
	}
	return RuntimeTaskListResult{
		Tasks:        tasks[start:end],
		Total:        total,
		Limit:        options.Limit,
		Offset:       options.Offset,
		StatusCounts: statusCounts,
	}, nil
}

func normalizeRuntimeTaskListOptions(options RuntimeTaskListOptions) RuntimeTaskListOptions {
	if options.Limit <= 0 {
		options.Limit = defaultRuntimeTaskListLimit
	}
	if options.Limit > maxRuntimeTaskListLimit {
		options.Limit = maxRuntimeTaskListLimit
	}
	if options.Offset < 0 {
		options.Offset = 0
	}
	return options
}

func compareTimestampStrings(left, right string) int {
	leftTime, leftErr := time.Parse(time.RFC3339Nano, left)
	rightTime, rightErr := time.Parse(time.RFC3339Nano, right)
	if leftErr == nil && rightErr == nil {
		if leftTime.Before(rightTime) {
			return -1
		}
		if leftTime.After(rightTime) {
			return 1
		}
		return 0
	}
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
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
		if compareTimestampStrings(events[i].CreatedAt, events[j].CreatedAt) == 0 {
			return events[i].ID < events[j].ID
		}
		return compareTimestampStrings(events[i].CreatedAt, events[j].CreatedAt) < 0
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
		if compareTimestampStrings(logs[i].CreatedAt, logs[j].CreatedAt) == 0 {
			return logs[i].ID < logs[j].ID
		}
		return compareTimestampStrings(logs[i].CreatedAt, logs[j].CreatedAt) < 0
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

func (s *MemoryStore) cancelRuntimeTasksBySessionIDsLocked(workspaceID string, sessionIDs []string, userID, reason, testRunID string) []string {
	seenSessions := map[string]struct{}{}
	for _, sessionID := range sessionIDs {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID != "" {
			seenSessions[sessionID] = struct{}{}
		}
	}
	if len(seenSessions) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	cancelledTaskIDs := []string{}
	for id, task := range s.runtimeTasks {
		if task.WorkspaceID != strings.TrimSpace(workspaceID) {
			continue
		}
		if _, ok := seenSessions[strings.TrimSpace(task.SessionID)]; !ok {
			continue
		}
		if task.Status != "queued" && task.Status != "claimed" && task.Status != "running" {
			continue
		}
		task.Status = "cancelled"
		if task.StartedAt == "" && (task.ClaimedAt != "" || task.ClaimedByWorkerID != "") {
			task.StartedAt = now
		}
		task.FinishedAt = now
		task.Error = reason
		task.UpdatedAt = now
		s.runtimeTasks[id] = task
		cancelledTaskIDs = append(cancelledTaskIDs, task.ID)
		s.appendRuntimeTaskEventLocked(task.WorkspaceID, task.ID, "", userID, "cancel_requested", json.RawMessage(fmt.Sprintf(`{"status":%q,"reason":%q,"source":"test_run","testRunId":%q}`, task.Status, reason, testRunID)))
	}
	return cancelledTaskIDs
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
	nowTime := time.Now().UTC()
	for _, task := range s.runtimeTasks {
		if task.WorkspaceID == registration.WorkspaceID &&
			task.ClaimedByWorkerID == worker.ID &&
			task.Status == "running" &&
			task.RuntimeMode == worker.Mode &&
			jsonObjectContains(worker.Capabilities, task.RequiredCapabilities) &&
			runtimeTaskUpdatedBefore(task, nowTime.Add(-staleRunningTaskReclaimAge)) {
			candidates = append(candidates, task)
		}
	}
	if len(candidates) == 0 {
		for _, task := range s.runtimeTasks {
			if task.WorkspaceID != registration.WorkspaceID ||
				task.Status != "queued" ||
				task.RuntimeMode != worker.Mode ||
				!jsonObjectContains(worker.Capabilities, task.RequiredCapabilities) {
				continue
			}
			candidates = append(candidates, task)
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
	} else {
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].UpdatedAt == candidates[j].UpdatedAt {
				if candidates[i].CreatedAt == candidates[j].CreatedAt {
					return candidates[i].ID < candidates[j].ID
				}
				return candidates[i].CreatedAt < candidates[j].CreatedAt
			}
			return candidates[i].UpdatedAt < candidates[j].UpdatedAt
		})
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	task := s.runtimeTasks[candidates[0].ID]
	now := nowTime.Format(time.RFC3339Nano)
	task.Status = "claimed"
	task.ClaimedByWorkerID = worker.ID
	if task.ClaimedAt == "" {
		task.ClaimedAt = now
	}
	task.UpdatedAt = now
	s.runtimeTasks[task.ID] = task
	s.appendRuntimeTaskEventLocked(task.WorkspaceID, task.ID, worker.ID, "", "claimed", json.RawMessage(fmt.Sprintf(`{"status":%q}`, task.Status)))
	return &task, nil
}

func runtimeTaskUpdatedBefore(task RuntimeTask, cutoff time.Time) bool {
	updatedAt, ok := parseRuntimeTimestamp(task.UpdatedAt)
	if !ok {
		return false
	}
	return updatedAt.Before(cutoff)
}

func parseRuntimeTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, value)
	}
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
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
	if isFinalRuntimeTaskStatus(task.Status) && task.Kind == "issue_type_triage" {
		s.reconcileIssueTypeTriageRuntimeResultLocked(task)
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

func (s *MemoryStore) runtimeTaskCreatedByLocked(taskID string) string {
	for _, event := range s.runtimeTaskEvents {
		if event.TaskID == strings.TrimSpace(taskID) && event.Kind == "created" {
			return event.ActorUserID
		}
	}
	return ""
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

func (s *MemoryStore) projectForTestCasesLocked(workspaceID, projectID string) (Project, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return Project{}, errors.New("projectId is required")
	}
	project, ok := s.projects[projectID]
	if !ok || project.WorkspaceID != strings.TrimSpace(workspaceID) {
		return Project{}, ErrNotFound
	}
	return project, nil
}

func (s *MemoryStore) createProjectTestCaseLocked(userID, workspaceID, projectID string, input TestCaseInput, now string) (TestCase, error) {
	normalized, score, findings, err := normalizeTestCaseInput(input, defaultTestCaseSource)
	if err != nil {
		return TestCase{}, err
	}
	testCase := TestCase{
		ID:                      fmt.Sprintf("test-case-%04d", s.nextMemoryIDLocked()),
		WorkspaceID:             strings.TrimSpace(workspaceID),
		ProjectID:               strings.TrimSpace(projectID),
		Title:                   normalized.Title,
		Type:                    normalized.Type,
		Area:                    normalized.Area,
		Priority:                normalized.Priority,
		Status:                  normalized.Status,
		Source:                  normalized.Source,
		Preconditions:           normalized.Preconditions,
		Steps:                   normalized.Steps,
		ExpectedResult:          normalized.ExpectedResult,
		EnvironmentRequirements: normalized.EnvironmentRequirements,
		Dependencies:            normalized.Dependencies,
		Tags:                    normalized.Tags,
		QualityScore:            score,
		QualityFindings:         findings,
		CreatedByUserID:         strings.TrimSpace(userID),
		CreatedAt:               now,
		UpdatedAt:               now,
	}
	s.testCases[testCase.ID] = testCase
	s.appendTestCaseRevisionLocked(testCase, userID, now)
	return testCaseSnapshot(testCase), nil
}

func (s *MemoryStore) updateProjectTestCaseLocked(userID, workspaceID, projectID, caseID string, input TestCaseInput, now string) (TestCase, error) {
	existing, ok := s.testCases[strings.TrimSpace(caseID)]
	if !ok || existing.WorkspaceID != strings.TrimSpace(workspaceID) || existing.ProjectID != strings.TrimSpace(projectID) {
		return TestCase{}, ErrNotFound
	}
	if input.Source == "" {
		input.Source = existing.Source
	}
	normalized, score, findings, err := normalizeTestCaseInput(input, existing.Source)
	if err != nil {
		return TestCase{}, err
	}
	existing.Title = normalized.Title
	existing.Type = normalized.Type
	existing.Area = normalized.Area
	existing.Priority = normalized.Priority
	existing.Status = normalized.Status
	existing.Source = normalized.Source
	existing.Preconditions = normalized.Preconditions
	existing.Steps = normalized.Steps
	existing.ExpectedResult = normalized.ExpectedResult
	existing.EnvironmentRequirements = normalized.EnvironmentRequirements
	existing.Dependencies = normalized.Dependencies
	existing.Tags = normalized.Tags
	existing.QualityScore = score
	existing.QualityFindings = findings
	existing.UpdatedAt = now
	s.testCases[existing.ID] = existing
	s.appendTestCaseRevisionLocked(existing, userID, now)
	return testCaseSnapshot(existing), nil
}

func (s *MemoryStore) archiveProjectTestCaseLocked(userID, workspaceID, projectID, caseID, now string) (TestCase, error) {
	existing, err := s.testCaseForProjectLocked(userID, workspaceID, projectID, caseID)
	if err != nil {
		return TestCase{}, err
	}
	if existing.Status == "archived" {
		return existing, nil
	}
	input := TestCaseInput{
		Title:                   existing.Title,
		Type:                    existing.Type,
		Area:                    existing.Area,
		Priority:                existing.Priority,
		Status:                  "archived",
		Source:                  existing.Source,
		Preconditions:           existing.Preconditions,
		Steps:                   existing.Steps,
		ExpectedResult:          existing.ExpectedResult,
		EnvironmentRequirements: existing.EnvironmentRequirements,
		Dependencies:            existing.Dependencies,
		Tags:                    existing.Tags,
	}
	return s.updateProjectTestCaseLocked(userID, workspaceID, projectID, caseID, input, now)
}

func normalizeTestCaseIDList(values []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (s *MemoryStore) testCaseForProjectLocked(userID, workspaceID, projectID, caseID string) (TestCase, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	caseID = strings.TrimSpace(caseID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return TestCase{}, ErrNotFound
	}
	if _, err := s.projectForTestCasesLocked(workspaceID, projectID); err != nil {
		return TestCase{}, err
	}
	testCase, ok := s.testCases[caseID]
	if !ok || testCase.WorkspaceID != workspaceID || testCase.ProjectID != projectID {
		return TestCase{}, ErrNotFound
	}
	return testCase, nil
}

func (s *MemoryStore) testCaseProposalForProjectLocked(userID, workspaceID, projectID, proposalID string) (TestCaseProposal, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	proposalID = strings.TrimSpace(proposalID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return TestCaseProposal{}, ErrNotFound
	}
	if _, err := s.projectForTestCasesLocked(workspaceID, projectID); err != nil {
		return TestCaseProposal{}, err
	}
	proposal, ok := s.testCaseProposals[proposalID]
	if !ok || proposal.WorkspaceID != workspaceID || proposal.ProjectID != projectID {
		return TestCaseProposal{}, ErrNotFound
	}
	return proposal, nil
}

func (s *MemoryStore) testCaseProposalSnapshotLocked(proposal TestCaseProposal) TestCaseProposal {
	proposal.ProposedCase = copyTestCaseInput(proposal.ProposedCase)
	proposal.ValidationErrors = stringsOrEmpty(proposal.ValidationErrors)
	proposal.QualityFindings = qualityFindingsOrEmpty(proposal.QualityFindings)
	if proposal.CurrentCase != nil {
		proposal.CurrentCase = cloneTestCasePointer(*proposal.CurrentCase)
	} else if proposal.TargetCaseID != "" {
		if current, ok := s.testCases[proposal.TargetCaseID]; ok {
			proposal.CurrentCase = cloneTestCasePointer(current)
		}
	}
	return proposal
}

func (s *MemoryStore) insertTestCaseProposalLocked(proposal TestCaseProposal) TestCaseProposal {
	if proposal.ID == "" {
		proposal.ID = fmt.Sprintf("test-case-proposal-%04d", s.nextMemoryIDLocked())
	}
	proposal.ProposedCase = copyTestCaseInput(proposal.ProposedCase)
	if proposal.QualityFindings == nil {
		proposal.QualityFindings = []TestCaseQualityFinding{}
	}
	if proposal.ValidationErrors == nil {
		proposal.ValidationErrors = []string{}
	}
	s.testCaseProposals[proposal.ID] = proposal
	return proposal
}

func (s *MemoryStore) testPlanForWorkspaceLocked(userID, workspaceID, planID string) (TestPlan, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	planID = strings.TrimSpace(planID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return TestPlan{}, ErrNotFound
	}
	plan, ok := s.testPlans[planID]
	if !ok || plan.WorkspaceID != workspaceID {
		return TestPlan{}, ErrNotFound
	}
	plan.CaseCount = s.testPlanCaseCountLocked(plan.ID)
	return plan, nil
}

func (s *MemoryStore) testPlanForProjectLocked(userID, workspaceID, projectID, planID string) (TestPlan, error) {
	plan, err := s.testPlanForWorkspaceLocked(userID, workspaceID, planID)
	if err != nil {
		return TestPlan{}, err
	}
	if _, err := s.projectForTestCasesLocked(strings.TrimSpace(workspaceID), strings.TrimSpace(projectID)); err != nil {
		return TestPlan{}, err
	}
	detail, err := s.testPlanDetailLocked(plan.ID)
	if err != nil {
		return TestPlan{}, err
	}
	if !testPlanDetailIncludesProject(detail, strings.TrimSpace(projectID)) {
		return TestPlan{}, ErrNotFound
	}
	return plan, nil
}

func (s *MemoryStore) testRunForWorkspaceLocked(userID, workspaceID, runID string) (TestRun, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	runID = strings.TrimSpace(runID)
	if !s.isWorkspaceMember(workspaceID, userID) {
		return TestRun{}, ErrNotFound
	}
	run, ok := s.testRuns[runID]
	if !ok || run.WorkspaceID != workspaceID {
		return TestRun{}, ErrNotFound
	}
	return run, nil
}

func (s *MemoryStore) testRunForProjectLocked(userID, workspaceID, projectID, runID string) (TestRun, error) {
	run, err := s.testRunForWorkspaceLocked(userID, workspaceID, runID)
	if err != nil {
		return TestRun{}, err
	}
	if _, err := s.projectForTestCasesLocked(strings.TrimSpace(workspaceID), strings.TrimSpace(projectID)); err != nil {
		return TestRun{}, err
	}
	detail, err := s.testRunDetailLocked(run.ID)
	if err != nil {
		return TestRun{}, err
	}
	if !testRunDetailIncludesProject(detail, strings.TrimSpace(projectID)) {
		return TestRun{}, ErrNotFound
	}
	return run, nil
}

func (s *MemoryStore) testCasesForPlanLocked(workspaceID string, inputs []TestPlanCaseInput, requireReady bool, statusError string) ([]TestCase, error) {
	cases := make([]TestCase, 0, len(inputs))
	for _, input := range inputs {
		testCase, ok := s.testCases[strings.TrimSpace(input.CaseID)]
		if !ok || testCase.WorkspaceID != strings.TrimSpace(workspaceID) || testCase.ProjectID != strings.TrimSpace(input.ProjectID) {
			return nil, ErrNotFound
		}
		if requireReady && testCase.Status != "ready" {
			return nil, errors.New(statusError)
		}
		cases = append(cases, testCaseSnapshot(testCase))
	}
	return cases, nil
}

func (s *MemoryStore) testPlanCaseCountLocked(planID string) int {
	count := 0
	for _, planCase := range s.testPlanCases {
		if planCase.PlanID == strings.TrimSpace(planID) {
			count++
		}
	}
	return count
}

func (s *MemoryStore) replaceTestPlanCasesLocked(plan TestPlan, cases []TestCase, now string) {
	for id, planCase := range s.testPlanCases {
		if planCase.PlanID == plan.ID {
			delete(s.testPlanCases, id)
		}
	}
	for index, testCase := range cases {
		planCase := TestPlanCase{
			ID:          fmt.Sprintf("test-plan-case-%04d", s.nextMemoryIDLocked()),
			WorkspaceID: plan.WorkspaceID,
			ProjectID:   testCase.ProjectID,
			PlanID:      plan.ID,
			TestCaseID:  testCase.ID,
			SortOrder:   index + 1,
			TestCase:    testCaseSnapshot(testCase),
		}
		_ = now
		s.testPlanCases[planCase.ID] = planCase
	}
}

func (s *MemoryStore) testCasesForPlanIDLocked(planID string) []TestCase {
	planCases := []TestPlanCase{}
	for _, planCase := range s.testPlanCases {
		if planCase.PlanID == strings.TrimSpace(planID) {
			planCases = append(planCases, planCase)
		}
	}
	sort.Slice(planCases, func(i, j int) bool {
		return planCases[i].SortOrder < planCases[j].SortOrder
	})
	cases := make([]TestCase, 0, len(planCases))
	for _, planCase := range planCases {
		if testCase, ok := s.testCases[planCase.TestCaseID]; ok {
			cases = append(cases, testCaseSnapshot(testCase))
		}
	}
	return cases
}

func (s *MemoryStore) attachLatestTestCaseResultsLocked(cases []TestCase) {
	if len(cases) == 0 {
		return
	}
	indexByCaseID := make(map[string]int, len(cases))
	for index, testCase := range cases {
		if testCase.ID != "" {
			indexByCaseID[testCase.ID] = index
		}
	}
	for _, item := range s.testRunItems {
		index, ok := indexByCaseID[item.TestCaseID]
		if !ok || !isFinalTestRunItemStatus(item.Status) {
			continue
		}
		run := s.testRuns[item.RunID]
		latest := TestCaseLatestResult{
			ItemID:         item.ID,
			RunID:          item.RunID,
			RunStatus:      run.Status,
			RunSource:      run.Source,
			Status:         item.Status,
			ActualResult:   item.ActualResult,
			FailureSummary: item.FailureSummary,
			Evidence:       copyRawMessage(item.Evidence),
			UpdatedAt:      item.UpdatedAt,
		}
		current := cases[index].LatestResult
		if current == nil || s.isNewerTestCaseLatestResultLocked(latest, *current) {
			copyLatest := latest
			cases[index].LatestResult = &copyLatest
		}
	}
}

func (s *MemoryStore) isNewerTestCaseLatestResultLocked(candidate, current TestCaseLatestResult) bool {
	if candidate.UpdatedAt != current.UpdatedAt {
		return candidate.UpdatedAt > current.UpdatedAt
	}
	candidateRunUpdatedAt := s.testRuns[candidate.RunID].UpdatedAt
	currentRunUpdatedAt := s.testRuns[current.RunID].UpdatedAt
	if candidateRunUpdatedAt != currentRunUpdatedAt {
		return candidateRunUpdatedAt > currentRunUpdatedAt
	}
	return candidate.ItemID > current.ItemID
}

func (s *MemoryStore) testPlanDetailLocked(planID string) (TestPlanDetail, error) {
	plan, ok := s.testPlans[strings.TrimSpace(planID)]
	if !ok {
		return TestPlanDetail{}, ErrNotFound
	}
	plan.CaseCount = s.testPlanCaseCountLocked(plan.ID)
	planCases := []TestPlanCase{}
	for _, planCase := range s.testPlanCases {
		if planCase.PlanID != plan.ID {
			continue
		}
		item := planCase
		if testCase, ok := s.testCases[item.TestCaseID]; ok {
			item.TestCase = testCaseSnapshot(testCase)
		}
		planCases = append(planCases, item)
	}
	sort.Slice(planCases, func(i, j int) bool {
		return planCases[i].SortOrder < planCases[j].SortOrder
	})
	runs := []TestRun{}
	for _, run := range s.testRuns {
		if run.PlanID == plan.ID {
			runs = append(runs, run)
		}
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].UpdatedAt > runs[j].UpdatedAt
	})
	return TestPlanDetail{Plan: plan, Cases: planCases, Runs: runs}, nil
}

func (s *MemoryStore) testRunDetailLocked(runID string) (TestRunDetail, error) {
	run, ok := s.testRuns[strings.TrimSpace(runID)]
	if !ok {
		return TestRunDetail{}, ErrNotFound
	}
	var plan *TestPlan
	if run.PlanID != "" {
		if existingPlan, ok := s.testPlans[run.PlanID]; ok {
			snapshot := existingPlan
			plan = &snapshot
		}
	}
	items := []TestRunItem{}
	for _, item := range s.testRunItems {
		if item.RunID != run.ID {
			continue
		}
		copyItem := item
		if testCase, ok := s.testCases[item.TestCaseID]; ok {
			copyItem.TestCase = testCaseSnapshot(testCase)
		}
		items = append(items, copyItem)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].SortOrder != items[j].SortOrder {
			return items[i].SortOrder < items[j].SortOrder
		}
		return items[i].CreatedAt < items[j].CreatedAt
	})
	run.TotalCount = len(items)
	run.PassedCount, run.FailedCount, run.BlockedCount, run.SkippedCount = testRunCounts(items)
	return TestRunDetail{Run: run, Plan: plan, Items: items}, nil
}

func (s *MemoryStore) startTestRunExecutionSessionsLocked(userID, runID string, input CreateTestRunInput) error {
	run, ok := s.testRuns[strings.TrimSpace(runID)]
	if !ok {
		return ErrNotFound
	}
	run.ResultLocale = normalizeTestResultLocale(firstNonEmpty(input.ResultLocale, run.ResultLocale))
	var plan *TestPlan
	if run.PlanID != "" {
		if existingPlan, ok := s.testPlans[run.PlanID]; ok {
			snapshot := existingPlan
			plan = &snapshot
		}
	}
	queued := []TestRunItem{}
	for _, item := range s.testRunItems {
		if item.RunID == run.ID && item.Status == "queued" {
			queued = append(queued, item)
		}
	}
	sort.Slice(queued, func(i, j int) bool {
		if queued[i].SortOrder != queued[j].SortOrder {
			return queued[i].SortOrder < queued[j].SortOrder
		}
		return queued[i].CreatedAt < queued[j].CreatedAt
	})
	for _, projectItems := range testRunItemsGroupedByProject(queued) {
		projectID := ""
		if len(projectItems) > 0 {
			projectID = projectItems[0].ProjectID
		}
		for start := 0; start < len(projectItems); start += input.BatchSize {
			end := start + input.BatchSize
			if end > len(projectItems) {
				end = len(projectItems)
			}
			batch := projectItems[start:end]
			cases := make([]TestCase, 0, len(batch))
			for _, item := range batch {
				if testCase, ok := s.testCases[item.TestCaseID]; ok {
					cases = append(cases, testCaseSnapshot(testCase))
				}
			}
			requiredCapabilities, err := testRunExecutionRequiredCapabilities(cases)
			if err != nil {
				return err
			}
			if !s.hasActiveWorkerWithCapabilitiesLocked(run.WorkspaceID, input.RuntimeMode, requiredCapabilities, time.Now().UTC()) {
				return ErrNoActiveCodexWorker
			}
			parent, ok := s.issues[run.ParentIssueID]
			if !ok {
				return ErrNotFound
			}
			sortOrder := 1
			for _, issue := range s.issues {
				if issue.ParentIssueID == parent.ID && issue.SortOrder >= sortOrder {
					sortOrder = issue.SortOrder + 1
				}
			}
			now := time.Now().UTC().Format(time.RFC3339Nano)
			body := buildTestRunExecutionIssueBody(run, cases)
			child := Issue{
				ID:            fmt.Sprintf("issue-%04d", s.nextMemoryIDLocked()),
				WorkspaceID:   run.WorkspaceID,
				ProjectID:     firstNonEmpty(projectID, parent.ProjectID, run.ProjectID),
				ParentIssueID: parent.ID,
				SortOrder:     sortOrder,
				Title:         fmt.Sprintf("Execute %s batch %d", testRunExecutionScopeLabel(plan, cases), start/input.BatchSize+1),
				Body:          body,
				Status:        "open",
				TriageStatus:  "none",
				Assignee:      parent.CreatorName,
				AssigneeType:  "human",
				CreatorName:   parent.CreatorName,
				CreatorAvatar: parent.CreatorAvatar,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			s.issues[child.ID] = child
			session, err := s.createAgentSessionLocked(userID, run.WorkspaceID, child.ID, CreateAgentSessionInput{
				AgentEngine:          input.AgentEngine,
				RuntimeMode:          input.RuntimeMode,
				Command:              body,
				Automation:           testRunExecutionAutomation,
				TestRunID:            run.ID,
				RequiredCapabilities: requiredCapabilities,
			})
			if err != nil {
				return err
			}
			for _, item := range batch {
				item.ExecutionIssueID = child.ID
				item.AgentSessionID = session.ID
				item.Status = "running"
				item.UpdatedAt = now
				s.testRunItems[item.ID] = item
			}
		}
	}
	run.Status = "running"
	run.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.testRuns[run.ID] = run
	return nil
}

func (s *MemoryStore) startTestRunSetupSessionLocked(userID, runID string, input CreateTestRunInput) error {
	run, ok := s.testRuns[strings.TrimSpace(runID)]
	if !ok {
		return ErrNotFound
	}
	run.ResultLocale = normalizeTestResultLocale(firstNonEmpty(input.ResultLocale, run.ResultLocale))
	parent, ok := s.issues[run.ParentIssueID]
	if !ok {
		return ErrNotFound
	}
	sortOrder := 1
	for _, issue := range s.issues {
		if issue.ParentIssueID == parent.ID && issue.SortOrder >= sortOrder {
			sortOrder = issue.SortOrder + 1
		}
	}
	body := buildTestRunSetupIssueBody(run)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	child := Issue{
		ID:            fmt.Sprintf("issue-%04d", s.nextMemoryIDLocked()),
		WorkspaceID:   run.WorkspaceID,
		ProjectID:     run.ProjectID,
		ParentIssueID: parent.ID,
		SortOrder:     sortOrder,
		Title:         "Prepare test run",
		Body:          body,
		Status:        "open",
		TriageStatus:  "none",
		Assignee:      parent.CreatorName,
		AssigneeType:  "human",
		CreatorName:   parent.CreatorName,
		CreatorAvatar: parent.CreatorAvatar,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	s.issues[child.ID] = child
	session, err := s.createAgentSessionLocked(userID, run.WorkspaceID, child.ID, CreateAgentSessionInput{
		AgentEngine:      input.AgentEngine,
		RuntimeMode:      input.RuntimeMode,
		Command:          body,
		Automation:       testRunSetupAutomation,
		TestRunID:        run.ID,
		TestRunBatchSize: input.BatchSize,
	})
	if err != nil {
		return err
	}
	run.SetupIssueID = child.ID
	run.SetupSessionID = session.ID
	run.SetupStatus = "running"
	run.Status = "setup_running"
	run.UpdatedAt = now
	s.testRuns[run.ID] = run
	return nil
}

func (s *MemoryStore) reviewTestRun(userID, workspaceID, runID string, input ReviewTestRunInput, status string) (TestRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	run, err := s.testRunForWorkspaceLocked(userID, workspaceID, runID)
	if err != nil {
		return TestRun{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	run.AcceptanceStatus = status
	run.Status = status
	run.AcceptanceNote = normalizeReviewNote(input.Note)
	run.AcceptedByUserID = strings.TrimSpace(userID)
	run.AcceptedAt = now
	run.UpdatedAt = now
	s.testRuns[run.ID] = run
	return run, nil
}

func (s *MemoryStore) appendTestCaseRevisionLocked(testCase TestCase, authorUserID, now string) TestCaseRevision {
	revisionNumber := 1
	for _, revision := range s.testCaseRevisions {
		if revision.TestCaseID == testCase.ID && revision.RevisionNumber >= revisionNumber {
			revisionNumber = revision.RevisionNumber + 1
		}
	}
	revision := TestCaseRevision{
		ID:             fmt.Sprintf("test-case-revision-%04d", s.nextMemoryIDLocked()),
		WorkspaceID:    testCase.WorkspaceID,
		ProjectID:      testCase.ProjectID,
		TestCaseID:     testCase.ID,
		AuthorUserID:   strings.TrimSpace(authorUserID),
		RevisionNumber: revisionNumber,
		Snapshot:       testCaseSnapshot(testCase),
		CreatedAt:      now,
	}
	s.testCaseRevisions[revision.ID] = revision
	return revision
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

func (s *MemoryStore) hasActiveCodexWorkerLocked(workspaceID, runtimeMode string, now time.Time) bool {
	return s.hasActiveWorkerWithCapabilitiesLocked(workspaceID, runtimeMode, json.RawMessage(`{"codex":true}`), now)
}

func (s *MemoryStore) hasActiveWorkerWithCapabilitiesLocked(workspaceID, runtimeMode string, requiredCapabilities json.RawMessage, now time.Time) bool {
	for _, worker := range s.runtimeWorkers {
		if isActiveWorkerWithCapabilities(worker, workspaceID, runtimeMode, requiredCapabilities, now) {
			return true
		}
	}
	return false
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

func (s *MemoryStore) ensureRuntimeModeAllowedForWorkspaceLocked(workspaceID, runtimeMode string) error {
	workspace, ok := s.workspaceLocked(workspaceID)
	if !ok {
		return ErrNotFound
	}
	if workspace.Kind != strings.TrimSpace(runtimeMode) {
		return ErrForbidden
	}
	return nil
}

func (s *MemoryStore) identityLoginForUser(userID string) string {
	return s.authIdentityForUser(userID).Login
}

func (s *MemoryStore) authIdentityForUser(userID string) AuthIdentityInfo {
	var githubIdentity AuthIdentityInfo
	var fallbackIdentity AuthIdentityInfo
	for key, identityUserID := range s.identities {
		if identityUserID != userID {
			continue
		}
		provider, fallbackLogin, _ := strings.Cut(key, ":")
		identity := AuthIdentityInfo{Provider: provider, Login: fallbackLogin}
		if login := strings.TrimSpace(s.identityLogins[key]); login != "" {
			identity.Login = login
		}
		if provider == "password" {
			return identity
		}
		if provider == "github" {
			githubIdentity = identity
			continue
		}
		if fallbackIdentity.Login == "" {
			fallbackIdentity = identity
		}
	}
	if githubIdentity.Login != "" {
		return githubIdentity
	}
	return fallbackIdentity
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
