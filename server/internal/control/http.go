package control

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

type Server struct {
	config         Config
	store          Store
	github         GitHubClient
	triageMu       sync.Mutex
	triageInFlight map[string]struct{}
}

const serverProtocolVersion = 1

func NewServer(config Config, store Store, github GitHubClient) *Server {
	config = config.withDefaults()
	return &Server{
		config:         config,
		store:          store,
		github:         github,
		triageInFlight: map[string]struct{}{},
	}
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(jsonMiddleware)
	r.Get("/health", s.handleHealth)
	r.Get("/api/auth/github/start", s.handleGitHubStart)
	r.Get("/api/auth/github/callback", s.handleGitHubCallback)
	r.Get("/api/auth/github/result", s.handleGitHubResult)
	r.Get("/api/auth/me", s.handleMe)
	r.Get("/api/workspaces", s.handleWorkspaces)
	r.Post("/api/workspaces", s.handleCreateWorkspace)
	r.Post("/api/workspace-invitations/accept", s.handleAcceptWorkspaceInvitation)
	r.Get("/api/workspaces/{workspaceID}/members", s.handleListWorkspaceMembers)
	r.Post("/api/workspaces/{workspaceID}/invitations", s.handleCreateWorkspaceInvitation)
	r.Get("/api/workspaces/{workspaceID}/invitations", s.handleListWorkspaceInvitations)
	r.Delete("/api/workspaces/{workspaceID}/invitations/{invitationID}", s.handleRevokeWorkspaceInvitation)
	r.Get("/api/workspaces/{workspaceID}/inbox", s.handleWorkspaceInbox)
	r.Post("/api/workspaces/{workspaceID}/issue-events", s.handleCreateIssueEvent)
	r.Post("/api/workspaces/{workspaceID}/issue-events/{eventID}/read", s.handleMarkIssueEventRead)
	r.Post("/api/workspaces/{workspaceID}/issues/{issueID}/read-through", s.handleMarkIssueReadThrough)
	r.Get("/api/workspaces/{workspaceID}/projects", s.handleListProjects)
	r.Post("/api/workspaces/{workspaceID}/projects", s.handleCreateProject)
	r.Put("/api/workspaces/{workspaceID}/projects/{projectID}", s.handleUpdateProject)
	r.Delete("/api/workspaces/{workspaceID}/projects/{projectID}", s.handleDeleteProject)
	r.Get("/api/workspaces/{workspaceID}/projects/{projectID}/runbook", s.handleGetProjectRunbook)
	r.Put("/api/workspaces/{workspaceID}/projects/{projectID}/runbook", s.handleUpdateProjectRunbook)
	r.Get("/api/workspaces/{workspaceID}/issue-label-definitions", s.handleListIssueLabelDefinitions)
	r.Get("/api/workspaces/{workspaceID}/issues", s.handleListIssues)
	r.Post("/api/workspaces/{workspaceID}/issues/suggest-title", s.handleSuggestIssueTitle)
	r.Post("/api/workspaces/{workspaceID}/issues", s.handleCreateIssue)
	r.Get("/api/workspaces/{workspaceID}/issues/{issueID}", s.handleGetIssue)
	r.Post("/api/workspaces/{workspaceID}/issues/{issueID}/sessions", s.handleCreateAgentSession)
	r.Get("/api/workspaces/{workspaceID}/sessions/{sessionID}", s.handleGetSession)
	r.Post("/api/workspaces/{workspaceID}/sessions/{sessionID}/cancel", s.handleCancelAgentSession)
	r.Put("/api/workspaces/{workspaceID}/issues/{issueID}", s.handleUpdateIssue)
	r.Post("/api/workspaces/{workspaceID}/issues/{issueID}/tasks", s.handleCreateIssueTask)
	r.Delete("/api/workspaces/{workspaceID}/issues/{issueID}/tasks/{taskID}", s.handleDeleteIssueTask)
	r.Put("/api/workspaces/{workspaceID}/issues/{issueID}/labels", s.handleUpdateIssueLabels)
	r.Post("/api/workspaces/{workspaceID}/issues/{issueID}/comments", s.handleAddComment)
	r.Put("/api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}", s.handleUpdateComment)
	r.Put("/api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}/reactions/{reaction}", s.handleSetCommentReaction)
	r.Delete("/api/workspaces/{workspaceID}/issues/{issueID}/comments/{commentID}/reactions/{reaction}", s.handleDeleteCommentReaction)
	r.Post("/api/workspaces/{workspaceID}/runtime-registration-tokens", s.handleCreateRuntimeRegistrationToken)
	r.Get("/api/workspaces/{workspaceID}/runtime-registration-tokens", s.handleListRuntimeRegistrationTokens)
	r.Delete("/api/workspaces/{workspaceID}/runtime-registration-tokens/{tokenID}", s.handleRevokeRuntimeRegistrationToken)
	r.Get("/api/workspaces/{workspaceID}/runtime-workers", s.handleListRuntimeWorkers)
	r.Post("/api/workspaces/{workspaceID}/runtime-tasks", s.handleCreateRuntimeTask)
	r.Get("/api/workspaces/{workspaceID}/runtime-tasks", s.handleListRuntimeTasks)
	r.Get("/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/events", s.handleListRuntimeTaskEvents)
	r.Get("/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/logs", s.handleListRuntimeTaskLogs)
	r.Post("/api/workspaces/{workspaceID}/runtime-tasks/{taskID}/cancel", s.handleCancelRuntimeTask)
	r.Post("/api/runtime/workers/register", s.handleRegisterRuntimeWorker)
	r.Post("/api/runtime/workers/{workerID}/heartbeat", s.handleRuntimeWorkerHeartbeat)
	r.Post("/api/runtime/workers/{workerID}/tasks/claim", s.handleClaimRuntimeTask)
	r.Get("/api/runtime/workers/{workerID}/tasks/{taskID}", s.handleGetRuntimeTaskForWorker)
	r.Post("/api/runtime/workers/{workerID}/tasks/{taskID}/logs", s.handleAppendRuntimeTaskLog)
	r.Post("/api/runtime/workers/{workerID}/tasks/{taskID}/status", s.handleUpdateRuntimeTaskStatus)
	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"service":        "mspace-server",
		"version":        "0.1.0",
		"serverProtocol": serverProtocolVersion,
		"capabilities": map[string]bool{
			"workspaceInboxIssueGrouping": true,
			"teamWorkspaceCreation":       true,
			"workspaceInvitations":        true,
			"workspaceKinds":              true,
			"runtimeWorkerRegistration":   true,
			"runtimeTaskQueue":            true,
			"workspaceCollaboration":      true,
		},
	})
}

func (s *Server) handleGitHubStart(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(s.config.GitHubClientID) == "" ||
		strings.TrimSpace(s.config.GitHubClientSecret) == "" ||
		strings.TrimSpace(s.config.GitHubRedirectURI) == "" {
		writeError(w, http.StatusServiceUnavailable, errors.New("github login is not configured"))
		return
	}
	state, err := newState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.store.SaveOAuthState(r.Context(), OAuthState{
		State:       state,
		Provider:    "github",
		RedirectURI: s.config.GitHubRedirectURI,
		ExpiresAt:   time.Now().UTC().Add(s.config.OAuthStateTTL),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	params := url.Values{
		"client_id":    {s.config.GitHubClientID},
		"redirect_uri": {s.config.GitHubRedirectURI},
		"scope":        {"read:user user:email"},
		"state":        {state},
		"allow_signup": {"true"},
		"prompt":       {"select_account"},
	}
	writeJSON(w, http.StatusOK, AuthStartResult{
		AuthorizeURL: "https://github.com/login/oauth/authorize?" + params.Encode(),
		State:        state,
		PollURL:      "/api/auth/github/result?state=" + url.QueryEscape(state),
	})
}

func (s *Server) handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || state == "" {
		writeError(w, http.StatusBadRequest, errors.New("code and state are required"))
		return
	}
	if errText := strings.TrimSpace(r.URL.Query().Get("error")); errText != "" {
		writeError(w, http.StatusBadRequest, errors.New(errText))
		return
	}

	record, err := s.store.ConsumeOAuthState(r.Context(), "github", state)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrExpired) {
			status = http.StatusUnauthorized
		}
		writeError(w, status, err)
		return
	}

	accessToken, err := s.github.ExchangeCode(r.Context(), code, record.RedirectURI)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	profile, err := s.github.FetchUser(r.Context(), accessToken)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	user, workspaces, err := s.store.UpsertIdentity(r.Context(), profile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	token, expiresAt, err := s.store.CreateAuthSession(r.Context(), user.ID, s.config.SessionTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	result := AuthResult{
		Token:      token,
		ExpiresAt:  expiresAt.UTC().Format(time.RFC3339),
		User:       user,
		Workspaces: workspaces,
	}
	if err := s.store.SaveOAuthResult(r.Context(), "github", state, result, time.Now().UTC().Add(s.config.OAuthStateTTL)); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><title>mspace sign in</title></head><body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;margin:48px;color:#202020"><h1 style="font-size:20px;margin:0 0 12px">Signed in to mspace</h1><p style="margin:0;color:#5f5f5f">You can return to the mspace desktop app.</p><script>window.close()</script></body></html>`))
}

func (s *Server) handleGitHubResult(w http.ResponseWriter, r *http.Request) {
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		writeError(w, http.StatusBadRequest, errors.New("state is required"))
		return
	}
	result, ready, err := s.store.ConsumeOAuthResult(r.Context(), "github", state)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrExpired) {
			status = http.StatusUnauthorized
		}
		writeError(w, status, err)
		return
	}
	if !ready {
		writeJSON(w, http.StatusAccepted, AuthPollResult{Pending: true})
		return
	}
	writeJSON(w, http.StatusOK, AuthPollResult{
		Pending:    false,
		AuthResult: result,
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, workspaces, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":       user,
		"workspaces": workspaces,
	})
}

func (s *Server) handleWorkspaces(w http.ResponseWriter, r *http.Request) {
	_, workspaces, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, workspaces)
}

func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CreateWorkspaceInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workspace, workspaces, err := s.store.CreateWorkspace(r.Context(), user.ID, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, CreateWorkspaceResult{
		Workspace:  workspace,
		Workspaces: workspaces,
	})
}

func (s *Server) handleListWorkspaceMembers(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	members, err := s.store.ListWorkspaceMembers(r.Context(), user.ID, workspaceID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, members)
}

func (s *Server) handleCreateWorkspaceInvitation(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CreateWorkspaceInvitationInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	result, err := s.store.CreateWorkspaceInvitation(r.Context(), user.ID, workspaceID, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleListWorkspaceInvitations(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	invitations, err := s.store.ListWorkspaceInvitations(r.Context(), user.ID, workspaceID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, invitations)
}

func (s *Server) handleRevokeWorkspaceInvitation(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	invitationID := strings.TrimSpace(chi.URLParam(r, "invitationID"))
	invitation, err := s.store.RevokeWorkspaceInvitation(r.Context(), user.ID, workspaceID, invitationID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, invitation)
}

func (s *Server) handleAcceptWorkspaceInvitation(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := AcceptWorkspaceInvitationInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.store.AcceptWorkspaceInvitation(r.Context(), user.ID, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleWorkspaceInbox(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	items, err := s.store.ListInbox(r.Context(), user.ID, workspaceID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleCreateIssueEvent(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var input CreateIssueEventInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.WorkspaceID = strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	event, err := s.store.CreateIssueEvent(r.Context(), user.ID, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, event)
}

func (s *Server) handleMarkIssueEventRead(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	eventID := strings.TrimSpace(chi.URLParam(r, "eventID"))
	if eventID == "" {
		writeError(w, http.StatusBadRequest, errors.New("eventID is required"))
		return
	}
	if err := s.store.MarkIssueEventRead(r.Context(), user.ID, workspaceID, eventID); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMarkIssueReadThrough(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := ReadThroughInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	issueID := strings.TrimSpace(chi.URLParam(r, "issueID"))
	count, err := s.store.MarkIssueReadThrough(r.Context(), user.ID, workspaceID, issueID, input.ThroughEventID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "readCount": count})
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	projects, err := s.store.ListProjects(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := ProjectInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	project, err := s.store.CreateProject(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := ProjectInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	project, err := s.store.UpdateProject(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteProject(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID"))); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleGetProjectRunbook(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	runbook, err := s.store.GetProjectRunbook(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runbook)
}

func (s *Server) handleUpdateProjectRunbook(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := ProjectRunbookInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	runbook, err := s.store.UpdateProjectRunbook(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runbook)
}

func (s *Server) handleListIssueLabelDefinitions(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	definitions, err := s.store.ListIssueLabelDefinitions(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, definitions)
}

func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	issues, err := s.store.ListIssues(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	for _, issue := range issues {
		if issue.ParentIssueID == "" && issue.TriageStatus == "pending" && !hasIssueLabelDimension(issue.Labels, issueLabelDimensionType) {
			s.startIssueTypeTriage(workspaceID, issue.ID)
		}
	}
	writeJSON(w, http.StatusOK, issues)
}

func (s *Server) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CreateIssueInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	issueID, err := s.store.CreateIssue(r.Context(), user, strings.TrimSpace(chi.URLParam(r, "workspaceID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.startIssueTypeTriage(strings.TrimSpace(chi.URLParam(r, "workspaceID")), issueID)
	writeJSON(w, http.StatusCreated, map[string]string{"issueId": issueID})
}

func (s *Server) handleSuggestIssueTitle(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, errors.New("workspaceID is required"))
		return
	}
	if _, err := s.store.ListProjects(r.Context(), user.ID, workspaceID); err != nil {
		writeStoreError(w, err)
		return
	}
	input := SuggestIssueTitleInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result := suggestIssueTitle(r.Context(), input)
	if strings.TrimSpace(result.Title) == "" {
		writeError(w, http.StatusBadRequest, errors.New("issue cannot be empty"))
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	detail, err := s.store.GetIssue(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if detail.Issue.ParentIssueID == "" && detail.Issue.TriageStatus == "pending" && !hasIssueLabelDimension(detail.Labels, issueLabelDimensionType) {
		s.startIssueTypeTriage(strings.TrimSpace(chi.URLParam(r, "workspaceID")), detail.Issue.ID)
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleCreateAgentSession(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CreateAgentSessionInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	session, err := s.store.CreateAgentSession(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	detail, err := s.store.GetSession(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "sessionID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleCancelAgentSession(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CancelRuntimeTaskInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	sessionID := strings.TrimSpace(chi.URLParam(r, "sessionID"))
	session, err := s.store.GetSession(r.Context(), user.ID, workspaceID, sessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	task, err := s.store.CancelRuntimeTask(r.Context(), user.ID, workspaceID, session.Session.RuntimeTaskID, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleUpdateIssue(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := UpdateIssueInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	issue, err := s.store.UpdateIssue(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, issue)
}

func (s *Server) handleCreateIssueTask(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := IssueTaskInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	task, err := s.store.CreateIssueTask(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) handleDeleteIssueTask(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	err := s.store.DeleteIssueTask(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")), strings.TrimSpace(chi.URLParam(r, "taskID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleUpdateIssueLabels(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := UpdateIssueLabelsInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	labels, err := s.store.UpdateIssueLabels(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, labels)
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CreateCommentInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	commentID, err := s.store.AddComment(r.Context(), user, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "commentId": commentID})
}

func (s *Server) handleUpdateComment(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := UpdateCommentInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	comment, err := s.store.UpdateComment(r.Context(), user, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")), strings.TrimSpace(chi.URLParam(r, "commentID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "comment": comment})
}

func (s *Server) handleSetCommentReaction(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	err := s.store.SetCommentReaction(r.Context(), user, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")), strings.TrimSpace(chi.URLParam(r, "commentID")), strings.TrimSpace(chi.URLParam(r, "reaction")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteCommentReaction(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	err := s.store.DeleteCommentReaction(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")), strings.TrimSpace(chi.URLParam(r, "commentID")), strings.TrimSpace(chi.URLParam(r, "reaction")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleCreateRuntimeRegistrationToken(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CreateRuntimeRegistrationTokenInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	result, err := s.store.CreateRuntimeRegistrationToken(r.Context(), user.ID, workspaceID, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleListRuntimeRegistrationTokens(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	tokens, err := s.store.ListRuntimeRegistrationTokens(r.Context(), user.ID, workspaceID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

func (s *Server) handleRevokeRuntimeRegistrationToken(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	tokenID := strings.TrimSpace(chi.URLParam(r, "tokenID"))
	token, err := s.store.RevokeRuntimeRegistrationToken(r.Context(), user.ID, workspaceID, tokenID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, token)
}

func (s *Server) handleListRuntimeWorkers(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	workers, err := s.store.ListRuntimeWorkers(r.Context(), user.ID, workspaceID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workers)
}

func (s *Server) handleCreateRuntimeTask(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CreateRuntimeTaskInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	task, err := s.store.CreateRuntimeTask(r.Context(), user.ID, workspaceID, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) handleListRuntimeTasks(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	tasks, err := s.store.ListRuntimeTasks(r.Context(), user.ID, workspaceID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (s *Server) handleListRuntimeTaskEvents(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	taskID := strings.TrimSpace(chi.URLParam(r, "taskID"))
	events, err := s.store.ListRuntimeTaskEvents(r.Context(), user.ID, workspaceID, taskID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleListRuntimeTaskLogs(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	taskID := strings.TrimSpace(chi.URLParam(r, "taskID"))
	logs, err := s.store.ListRuntimeTaskLogs(r.Context(), user.ID, workspaceID, taskID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

func (s *Server) handleCancelRuntimeTask(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CancelRuntimeTaskInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	taskID := strings.TrimSpace(chi.URLParam(r, "taskID"))
	task, err := s.store.CancelRuntimeTask(r.Context(), user.ID, workspaceID, taskID, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleRegisterRuntimeWorker(w http.ResponseWriter, r *http.Request) {
	registration, ok := s.authenticateRuntimeRegistration(w, r)
	if !ok {
		return
	}
	input := RuntimeWorkerInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	worker, err := s.store.RegisterRuntimeWorker(r.Context(), registration, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, worker)
}

func (s *Server) handleRuntimeWorkerHeartbeat(w http.ResponseWriter, r *http.Request) {
	registration, ok := s.authenticateRuntimeRegistration(w, r)
	if !ok {
		return
	}
	input := RuntimeWorkerInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workerID := strings.TrimSpace(chi.URLParam(r, "workerID"))
	worker, err := s.store.UpdateRuntimeWorkerHeartbeat(r.Context(), registration, workerID, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, worker)
}

func (s *Server) handleClaimRuntimeTask(w http.ResponseWriter, r *http.Request) {
	registration, ok := s.authenticateRuntimeRegistration(w, r)
	if !ok {
		return
	}
	workerID := strings.TrimSpace(chi.URLParam(r, "workerID"))
	task, err := s.store.ClaimRuntimeTask(r.Context(), registration, workerID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if task == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleGetRuntimeTaskForWorker(w http.ResponseWriter, r *http.Request) {
	registration, ok := s.authenticateRuntimeRegistration(w, r)
	if !ok {
		return
	}
	workerID := strings.TrimSpace(chi.URLParam(r, "workerID"))
	taskID := strings.TrimSpace(chi.URLParam(r, "taskID"))
	task, err := s.store.GetRuntimeTaskForWorker(r.Context(), registration, workerID, taskID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) handleAppendRuntimeTaskLog(w http.ResponseWriter, r *http.Request) {
	registration, ok := s.authenticateRuntimeRegistration(w, r)
	if !ok {
		return
	}
	input := AppendRuntimeTaskLogInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workerID := strings.TrimSpace(chi.URLParam(r, "workerID"))
	taskID := strings.TrimSpace(chi.URLParam(r, "taskID"))
	log, err := s.store.AppendRuntimeTaskLog(r.Context(), registration, workerID, taskID, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, log)
}

func (s *Server) handleUpdateRuntimeTaskStatus(w http.ResponseWriter, r *http.Request) {
	registration, ok := s.authenticateRuntimeRegistration(w, r)
	if !ok {
		return
	}
	input := UpdateRuntimeTaskStatusInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workerID := strings.TrimSpace(chi.URLParam(r, "workerID"))
	taskID := strings.TrimSpace(chi.URLParam(r, "taskID"))
	task, err := s.store.UpdateRuntimeTaskStatus(r.Context(), registration, workerID, taskID, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (User, []Workspace, bool) {
	token := bearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, errors.New("missing authorization"))
		return User{}, nil, false
	}
	user, workspaces, err := s.store.GetUserBySessionToken(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, errors.New("invalid authorization"))
		return User{}, nil, false
	}
	return user, workspaces, true
}

func (s *Server) authenticateRuntimeRegistration(w http.ResponseWriter, r *http.Request) (RuntimeRegistration, bool) {
	token := bearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, errors.New("missing runtime registration token"))
		return RuntimeRegistration{}, false
	}
	registration, err := s.store.AuthenticateRuntimeRegistrationToken(r.Context(), token)
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, ErrNotFound) {
			status = http.StatusUnauthorized
		}
		writeError(w, status, errors.New("invalid runtime registration token"))
		return RuntimeRegistration{}, false
	}
	return registration, true
}

func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
}

func jsonMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeStoreError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, ErrNotFound) {
		status = http.StatusNotFound
	} else if errors.Is(err, ErrExpired) {
		status = http.StatusUnauthorized
	} else if errors.Is(err, ErrForbidden) {
		status = http.StatusForbidden
	} else if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "must be") || strings.Contains(err.Error(), "greater than") || strings.Contains(err.Error(), "valid JSON") || strings.Contains(err.Error(), "unsupported") || strings.Contains(err.Error(), "cannot be empty") {
		status = http.StatusBadRequest
	}
	writeError(w, status, err)
}
