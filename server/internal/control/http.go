package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type Server struct {
	config      Config
	store       Store
	github      GitHubClient
	adminLogins map[string]struct{}
}

const serverProtocolVersion = 2
const maxIssueAttachmentBytes = 10 << 20
const maxPasswordAuthBodyBytes = 4 << 10
const maxProfileBodyBytes = 4 << 10
const maxTestCaseImportBodyBytes = 5 << 20
const defaultTestCaseListLimit = 50
const maxTestCaseListLimit = 200
const defaultRuntimeTaskListLimit = 10
const maxRuntimeTaskListLimit = 100

var (
	version   = "dev"
	commitSHA = "unknown"
	buildTime = "unknown"
)

func NewServer(config Config, store Store, github GitHubClient) *Server {
	config = config.withDefaults()
	return &Server{
		config:      config,
		store:       store,
		github:      github,
		adminLogins: normalizeAdminLogins(config),
	}
}

func normalizeAdminLogins(config Config) map[string]struct{} {
	admins := map[string]struct{}{}
	values := append([]string{}, config.ServerAdminLogins...)
	values = append(values, config.BootstrapAdminLogin)
	for _, value := range values {
		login := strings.ToLower(strings.TrimSpace(value))
		if login != "" {
			admins[login] = struct{}{}
		}
	}
	return admins
}

func (s *Server) isServerAdmin(user User) bool {
	identity, err := s.store.GetUserAuthIdentity(context.Background(), user.ID)
	if err != nil {
		return false
	}
	_, ok := s.adminLogins[strings.ToLower(strings.TrimSpace(identity.Login))]
	return ok
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(jsonMiddleware)
	if persistent, ok := s.store.(interface{ Persist() error }); ok {
		r.Use(persistStoreMiddleware(persistent))
	}
	r.Get("/health", s.handleHealth)
	r.Get("/install/worker", s.handleWorkerInstallScript)
	r.Get("/api/auth/github/start", s.handleGitHubStart)
	r.Get("/api/auth/github/callback", s.handleGitHubCallback)
	r.Get("/api/auth/github/result", s.handleGitHubResult)
	r.Post("/api/auth/password/register", s.handlePasswordRegister)
	r.Post("/api/auth/password/login", s.handlePasswordLogin)
	r.Get("/api/auth/me", s.handleMe)
	r.Put("/api/auth/me", s.handleUpdateMe)
	r.Get("/api/workspaces", s.handleWorkspaces)
	r.Post("/api/workspaces", s.handleCreateWorkspace)
	r.Put("/api/workspaces/{workspaceID}", s.handleUpdateWorkspace)
	r.Get("/api/workspace-invitations/preview", s.handlePreviewWorkspaceInvitation)
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
	r.Get("/api/workspaces/{workspaceID}/test-plans", s.handleListWorkspaceTestPlans)
	r.Post("/api/workspaces/{workspaceID}/test-plans", s.handleCreateWorkspaceTestPlan)
	r.Get("/api/workspaces/{workspaceID}/test-plans/{planID}", s.handleGetWorkspaceTestPlan)
	r.Put("/api/workspaces/{workspaceID}/test-plans/{planID}", s.handleUpdateWorkspaceTestPlan)
	r.Get("/api/workspaces/{workspaceID}/test-runs", s.handleListWorkspaceTestRuns)
	r.Post("/api/workspaces/{workspaceID}/test-runs", s.handleStartAdHocWorkspaceTestRun)
	r.Post("/api/workspaces/{workspaceID}/test-plans/{planID}/runs", s.handleStartWorkspaceTestRun)
	r.Get("/api/workspaces/{workspaceID}/test-runs/{runID}", s.handleGetWorkspaceTestRun)
	r.Get("/api/workspaces/{workspaceID}/test-runs/{runID}/artifacts", s.handleListWorkspaceTestRunArtifacts)
	r.Post("/api/workspaces/{workspaceID}/test-runs/{runID}/retry", s.handleRetryWorkspaceTestRun)
	r.Post("/api/workspaces/{workspaceID}/test-runs/{runID}/cancel", s.handleCancelWorkspaceTestRun)
	r.Post("/api/workspaces/{workspaceID}/test-runs/{runID}/accept", s.handleAcceptWorkspaceTestRun)
	r.Post("/api/workspaces/{workspaceID}/test-runs/{runID}/block", s.handleBlockWorkspaceTestRun)
	r.Get("/api/workspaces/{workspaceID}/projects/{projectID}/runbook", s.handleGetProjectRunbook)
	r.Put("/api/workspaces/{workspaceID}/projects/{projectID}/runbook", s.handleUpdateProjectRunbook)
	r.Get("/api/workspaces/{workspaceID}/projects/{projectID}/test-cases", s.handleListProjectTestCases)
	r.Post("/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/import/preview", s.handlePreviewImportProjectTestCases)
	r.Post("/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/import/mapping-task", s.handleCreateImportMappingTask)
	r.Post("/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/import", s.handleImportProjectTestCases)
	r.Post("/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/optimize", s.handleOptimizeProjectTestCases)
	r.Post("/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/generate", s.handleGenerateProjectTestCases)
	r.Post("/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/delete", s.handleDeleteProjectTestCases)
	r.Post("/api/workspaces/{workspaceID}/projects/{projectID}/test-cases", s.handleCreateProjectTestCase)
	r.Get("/api/workspaces/{workspaceID}/projects/{projectID}/test-case-proposals", s.handleListProjectTestCaseProposals)
	r.Post("/api/workspaces/{workspaceID}/projects/{projectID}/test-case-proposals/{proposalID}/apply", s.handleApplyProjectTestCaseProposal)
	r.Post("/api/workspaces/{workspaceID}/projects/{projectID}/test-case-proposals/{proposalID}/reject", s.handleRejectProjectTestCaseProposal)
	r.Get("/api/workspaces/{workspaceID}/projects/{projectID}/test-plans", s.handleListProjectTestPlans)
	r.Post("/api/workspaces/{workspaceID}/projects/{projectID}/test-plans", s.handleCreateProjectTestPlan)
	r.Get("/api/workspaces/{workspaceID}/projects/{projectID}/test-plans/{planID}", s.handleGetProjectTestPlan)
	r.Put("/api/workspaces/{workspaceID}/projects/{projectID}/test-plans/{planID}", s.handleUpdateProjectTestPlan)
	r.Get("/api/workspaces/{workspaceID}/projects/{projectID}/test-runs", s.handleListProjectTestRuns)
	r.Post("/api/workspaces/{workspaceID}/projects/{projectID}/test-runs", s.handleStartAdHocProjectTestRun)
	r.Post("/api/workspaces/{workspaceID}/projects/{projectID}/test-plans/{planID}/runs", s.handleStartProjectTestRun)
	r.Get("/api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}", s.handleGetProjectTestRun)
	r.Get("/api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}/artifacts", s.handleListProjectTestRunArtifacts)
	r.Post("/api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}/retry", s.handleRetryProjectTestRun)
	r.Post("/api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}/cancel", s.handleCancelProjectTestRun)
	r.Post("/api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}/accept", s.handleAcceptProjectTestRun)
	r.Post("/api/workspaces/{workspaceID}/projects/{projectID}/test-runs/{runID}/block", s.handleBlockProjectTestRun)
	r.Get("/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}", s.handleGetProjectTestCase)
	r.Put("/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}", s.handleUpdateProjectTestCase)
	r.Delete("/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}", s.handleDeleteProjectTestCase)
	r.Get("/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}/runs", s.handleListProjectTestCaseRunItems)
	r.Get("/api/workspaces/{workspaceID}/projects/{projectID}/test-cases/{caseID}/revisions", s.handleListProjectTestCaseRevisions)
	r.Get("/api/test-artifacts/{artifactID}", s.handleGetTestArtifact)
	r.Get("/api/workspaces/{workspaceID}/issue-label-definitions", s.handleListIssueLabelDefinitions)
	r.Get("/api/workspaces/{workspaceID}/workspace/settings", s.handleGetWorkspaceSettings)
	r.Put("/api/workspaces/{workspaceID}/workspace/settings", s.handleUpdateWorkspaceSettings)
	r.Get("/api/workspaces/{workspaceID}/github-app", s.handleGetWorkspaceGitHubAppInstallation)
	r.Get("/api/workspaces/{workspaceID}/skills", s.handleListSkills)
	r.Post("/api/workspaces/{workspaceID}/skills", s.handleCreateSkill)
	r.Get("/api/workspaces/{workspaceID}/skills/{skillID}", s.handleGetSkill)
	r.Put("/api/workspaces/{workspaceID}/skills/{skillID}", s.handleUpdateSkill)
	r.Delete("/api/workspaces/{workspaceID}/skills/{skillID}", s.handleDeleteSkill)
	r.Post("/api/workspaces/{workspaceID}/skills/{skillID}/duplicate", s.handleDuplicateSkill)
	r.Get("/api/workspaces/{workspaceID}/agents", s.handleListAgentEngines)
	r.Get("/api/workspaces/{workspaceID}/environments", s.handleListEnvironments)
	r.Post("/api/workspaces/{workspaceID}/environments", s.handleCreateEnvironment)
	r.Put("/api/workspaces/{workspaceID}/environments/{environmentID}", s.handleUpdateEnvironment)
	r.Post("/api/workspaces/{workspaceID}/environments/{environmentID}/check", s.handleCheckEnvironment)
	r.Delete("/api/workspaces/{workspaceID}/environments/{environmentID}", s.handleDeleteEnvironment)
	r.Get("/api/workspaces/{workspaceID}/clusters", s.handleListClusters)
	r.Post("/api/workspaces/{workspaceID}/clusters", s.handleCreateCluster)
	r.Put("/api/workspaces/{workspaceID}/clusters/{clusterID}", s.handleUpdateCluster)
	r.Post("/api/workspaces/{workspaceID}/clusters/{clusterID}/check", s.handleCheckCluster)
	r.Delete("/api/workspaces/{workspaceID}/clusters/{clusterID}", s.handleDeleteCluster)
	r.Get("/api/workspaces/{workspaceID}/clusters/discover-defaults", s.handleDiscoverDefaultKubeconfigs)
	r.Post("/api/workspaces/{workspaceID}/clusters/import", s.handleImportKubeconfigs)
	r.Get("/api/workspaces/{workspaceID}/issues", s.handleListIssues)
	r.Post("/api/workspaces/{workspaceID}/issues/suggest-title", s.handleSuggestIssueTitle)
	r.Post("/api/workspaces/{workspaceID}/issues", s.handleCreateIssue)
	r.Get("/api/workspaces/{workspaceID}/issues/{issueID}", s.handleGetIssue)
	r.Post("/api/workspaces/{workspaceID}/issues/{issueID}/attachments", s.handleCreateIssueAttachment)
	r.Get("/api/attachments/{attachmentID}", s.handleGetIssueAttachment)
	r.Post("/api/workspaces/{workspaceID}/issues/{issueID}/sessions", s.handleCreateAgentSession)
	r.Get("/api/workspaces/{workspaceID}/sessions/{sessionID}", s.handleGetSession)
	r.Post("/api/workspaces/{workspaceID}/sessions/{sessionID}/cancel", s.handleCancelAgentSession)
	r.Post("/api/workspaces/{workspaceID}/issues/{issueID}/test-deploy", s.handleStartIssueTestDeploy)
	r.Post("/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/cleanup", s.handleRequestIssueTestEnvironmentCleanup)
	r.Post("/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/retain", s.handleRetainIssueTestEnvironment)
	r.Get("/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/resources", s.handleListIssueTestEnvironmentResources)
	r.Post("/api/workspaces/{workspaceID}/issues/{issueID}/test-environment/probe", s.handleProbeIssueTestEnvironment)
	r.Post("/api/workspaces/{workspaceID}/issues/{issueID}/handoffs/create-pr", s.handleCreateIssuePullRequestHandoff)
	r.Post("/api/workspaces/{workspaceID}/issues/{issueID}/handoffs/{handoffID}/refresh", s.handleRefreshIssueHandoff)
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
	r.Post("/api/workspaces/{workspaceID}/worker-installations", s.handleCreateWorkerInstallation)
	r.Get("/api/workspaces/{workspaceID}/runtime-workers", s.handleListRuntimeWorkers)
	r.Get("/api/workspaces/{workspaceID}/runtime/availability", s.handleRuntimeAvailability)
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

type responseStatusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseStatusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseStatusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func persistStoreMiddleware(store interface{ Persist() error }) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorder := &responseStatusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(recorder, r)
			if recorder.status >= 500 || !shouldPersistAfterRequest(r) {
				return
			}
			if err := store.Persist(); err != nil {
				slog.Error("persist local store", slog.String("error", err.Error()))
			}
		})
	}
}

func shouldPersistAfterRequest(r *http.Request) bool {
	if r.Method == http.MethodHead {
		return false
	}
	if r.Method != http.MethodGet {
		return true
	}
	path := r.URL.Path
	return path == "/api/auth/github/start" ||
		path == "/api/auth/github/callback" ||
		path == "/api/auth/github/result"
}

func (s *Server) githubAuthConfigured() bool {
	return strings.TrimSpace(s.config.GitHubClientID) != "" &&
		strings.TrimSpace(s.config.GitHubClientSecret) != "" &&
		strings.TrimSpace(s.config.GitHubRedirectURI) != ""
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"service":        "mspace-server",
		"version":        version,
		"commitSha":      commitSHA,
		"buildTime":      buildTime,
		"serverProtocol": serverProtocolVersion,
		"capabilities": map[string]bool{
			"workspaceInboxIssueGrouping": true,
			"teamWorkspaceCreation":       true,
			"workspaceInvitations":        true,
			"workspaceInvitationPreview":  true,
			"workspaceKinds":              true,
			"githubAuth":                  s.githubAuthConfigured(),
			"githubApp":                   s.githubAppConfigured(),
			"passwordAuth":                true,
			"testCaseLibrary":             true,
			"testCaseWorkflow":            true,
			"runtimeWorkerRegistration":   true,
			"runtimeAvailability":         true,
			"runtimeTaskQueue":            true,
			"workspaceCollaboration":      true,
		},
	})
}

func (s *Server) handleWorkerInstallScript(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, workerInstallScript)
}

func (s *Server) handleGitHubStart(w http.ResponseWriter, r *http.Request) {
	if !s.githubAuthConfigured() {
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
	result, err := s.authResultForUser(r, user, workspaces)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
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

func (s *Server) handlePasswordRegister(w http.ResponseWriter, r *http.Request) {
	input := PasswordAuthInput{}
	r.Body = http.MaxBytesReader(w, r.Body, maxPasswordAuthBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	user, workspaces, err := s.store.CreatePasswordIdentity(r.Context(), input)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			writeError(w, http.StatusConflict, errors.New("login already exists"))
			return
		}
		writeStoreError(w, err)
		return
	}
	result, err := s.authResultForUser(r, user, workspaces)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	input := PasswordAuthInput{}
	r.Body = http.MaxBytesReader(w, r.Body, maxPasswordAuthBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	user, workspaces, err := s.store.AuthenticatePassword(r.Context(), input)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrForbidden) {
			writeError(w, http.StatusUnauthorized, errors.New("invalid login or password"))
			return
		}
		writeStoreError(w, err)
		return
	}
	result, err := s.authResultForUser(r, user, workspaces)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, workspaces, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	identity, err := s.store.GetUserAuthIdentity(r.Context(), user.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":          user,
		"workspaces":    workspaces,
		"isServerAdmin": s.isServerAdmin(user),
		"identity":      identity,
	})
}

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	user, workspaces, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := UpdateCurrentUserProfileInput{}
	r.Body = http.MaxBytesReader(w, r.Body, maxProfileBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	updatedUser, err := s.store.UpdateCurrentUserProfile(r.Context(), user.ID, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	identity, err := s.store.GetUserAuthIdentity(r.Context(), updatedUser.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":          updatedUser,
		"workspaces":    workspaces,
		"isServerAdmin": s.isServerAdmin(updatedUser),
		"identity":      identity,
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
	if !s.isServerAdmin(user) {
		writeError(w, http.StatusForbidden, errors.New("only server admins can create team workspaces"))
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

func (s *Server) handleUpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := UpdateWorkspaceInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workspace, workspaces, err := s.store.UpdateWorkspace(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, UpdateWorkspaceResult{
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

func (s *Server) handlePreviewWorkspaceInvitation(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	preview, err := s.store.PreviewWorkspaceInvitation(r.Context(), token)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
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

func (s *Server) handleListProjectTestCases(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	options := TestCaseListOptions{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Query:  strings.TrimSpace(r.URL.Query().Get("q")),
	}
	options = parseTestCaseListOptions(r.URL.Query(), options)
	result, err := s.store.ListProjectTestCases(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), options)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleCreateProjectTestCase(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := TestCaseInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	testCase, err := s.store.CreateProjectTestCase(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, testCase)
}

func (s *Server) handleDeleteProjectTestCase(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	testCase, err := s.store.DeleteProjectTestCase(
		r.Context(),
		user.ID,
		strings.TrimSpace(chi.URLParam(r, "workspaceID")),
		strings.TrimSpace(chi.URLParam(r, "projectID")),
		strings.TrimSpace(chi.URLParam(r, "caseID")),
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, testCase)
}

func (s *Server) handleDeleteProjectTestCases(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := DeleteProjectTestCasesInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	testCases, err := s.store.DeleteProjectTestCases(
		r.Context(),
		user.ID,
		strings.TrimSpace(chi.URLParam(r, "workspaceID")),
		strings.TrimSpace(chi.URLParam(r, "projectID")),
		input,
	)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, testCases)
}

func (s *Server) handlePreviewImportProjectTestCases(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	projectID := strings.TrimSpace(chi.URLParam(r, "projectID"))
	projects, err := s.store.ListProjects(r.Context(), user.ID, workspaceID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	foundProject := false
	for _, project := range projects {
		if project.ID == projectID {
			foundProject = true
			break
		}
	}
	if !foundProject {
		writeStoreError(w, ErrNotFound)
		return
	}
	input := ImportTestCasesInput{}
	body := http.MaxBytesReader(w, r.Body, maxTestCaseImportBodyBytes)
	if err := json.NewDecoder(body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	preview, err := previewImportedTestCases(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, normalizeImportTestCasesPreview(preview))
}

func (s *Server) handleCreateImportMappingTask(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	projectID := strings.TrimSpace(chi.URLParam(r, "projectID"))
	input := TestCaseImportMappingTaskInput{}
	body := http.MaxBytesReader(w, r.Body, maxTestCaseImportBodyBytes)
	if err := json.NewDecoder(body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	runtimeMode, err := s.store.EnsureActiveCodexWorker(r.Context(), user.ID, workspaceID, input.RuntimeMode)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	projects, err := s.store.ListProjects(r.Context(), user.ID, workspaceID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	project, ok := projectByID(projects, projectID)
	if !ok {
		writeStoreError(w, ErrNotFound)
		return
	}
	taskInput, err := buildImportMappingRuntimeTaskInput(project, runtimeMode, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	task, err := s.store.CreateRuntimeTask(r.Context(), user.ID, workspaceID, taskInput)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, publicRuntimeTask(task))
}

func (s *Server) handleImportProjectTestCases(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := ImportTestCasesInput{}
	body := http.MaxBytesReader(w, r.Body, maxTestCaseImportBodyBytes)
	if err := json.NewDecoder(body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.store.ImportProjectTestCases(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, normalizeImportTestCasesResult(result))
}

func (s *Server) handleOptimizeProjectTestCases(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := OptimizeTestCasesInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	engine, err := requireCodexWorkflowAgentEngine(input.AgentEngine, input.LegacyProvider, input.LegacyAgentProfile)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.AgentEngine = engine
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	projectID := strings.TrimSpace(chi.URLParam(r, "projectID"))
	cases := []TestCase{}
	for _, caseID := range uniqueStrings(input.CaseIDs) {
		testCase, err := s.store.GetProjectTestCase(r.Context(), user.ID, workspaceID, projectID, caseID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		cases = append(cases, testCase)
	}
	if len(cases) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("caseIds are required"))
		return
	}
	runtimeMode, err := s.store.EnsureActiveCodexWorker(r.Context(), user.ID, workspaceID, input.RuntimeMode)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	projects, err := s.store.ListProjects(r.Context(), user.ID, workspaceID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	project, ok := projectByID(projects, projectID)
	if !ok {
		writeStoreError(w, ErrNotFound)
		return
	}
	issueID, err := s.store.CreateIssue(r.Context(), user, workspaceID, CreateIssueInput{
		ProjectID:   projectID,
		Title:       "Optimize test cases",
		TitleSource: issueTitleSourcePlainText,
		Body:        buildTestCaseOptimizationIssueBody(project, cases, input.Prompt),
		LabelKeys:   []string{"type:test"},
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	session, err := s.store.CreateAgentSession(r.Context(), user.ID, workspaceID, issueID, CreateAgentSessionInput{
		AgentEngine: agentEngineCodex,
		RuntimeMode: runtimeMode,
		Command:     buildTestCaseOptimizationIssueBody(project, cases, input.Prompt),
		Automation:  testCaseOptimizationAutomation,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, TestCaseAgentSessionResult{IssueID: issueID, Session: session})
}

func (s *Server) handleGenerateProjectTestCases(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := GenerateTestCasesInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	engine, err := requireCodexWorkflowAgentEngine(input.AgentEngine, input.LegacyProvider, input.LegacyAgentProfile)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.AgentEngine = engine
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	projectID := strings.TrimSpace(chi.URLParam(r, "projectID"))
	projects, err := s.store.ListProjects(r.Context(), user.ID, workspaceID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	project, ok := projectByID(projects, projectID)
	if !ok {
		writeStoreError(w, ErrNotFound)
		return
	}
	runtimeMode, err := s.store.EnsureActiveCodexWorker(r.Context(), user.ID, workspaceID, input.RuntimeMode)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	body := buildTestCaseGenerationIssueBody(project, input)
	issueID, err := s.store.CreateIssue(r.Context(), user, workspaceID, CreateIssueInput{
		ProjectID:   projectID,
		Title:       "Generate test cases",
		TitleSource: issueTitleSourcePlainText,
		Body:        body,
		LabelKeys:   []string{"type:test"},
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	session, err := s.store.CreateAgentSession(r.Context(), user.ID, workspaceID, issueID, CreateAgentSessionInput{
		AgentEngine: agentEngineCodex,
		RuntimeMode: runtimeMode,
		Command:     body,
		Automation:  testCaseGenerationAutomation,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, TestCaseAgentSessionResult{IssueID: issueID, Session: session})
}

func (s *Server) handleListProjectTestCaseProposals(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	proposals, err := s.store.ListProjectTestCaseProposals(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), TestCaseProposalListOptions{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, proposals)
}

func (s *Server) handleApplyProjectTestCaseProposal(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := ReviewTestCaseProposalInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.store.ApplyProjectTestCaseProposal(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), strings.TrimSpace(chi.URLParam(r, "proposalID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleRejectProjectTestCaseProposal(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := ReviewTestCaseProposalInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	proposal, err := s.store.RejectProjectTestCaseProposal(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), strings.TrimSpace(chi.URLParam(r, "proposalID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, proposal)
}

func (s *Server) handleListWorkspaceTestPlans(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	plans, err := s.store.ListWorkspaceTestPlans(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), TestPlanListOptions{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, plans)
}

func (s *Server) handleCreateWorkspaceTestPlan(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := TestPlanInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	detail, err := s.store.CreateWorkspaceTestPlan(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, testPlanDetailForResponse(detail))
}

func (s *Server) handleGetWorkspaceTestPlan(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	detail, err := s.store.GetWorkspaceTestPlan(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "planID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, testPlanDetailForResponse(detail))
}

func (s *Server) handleUpdateWorkspaceTestPlan(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := TestPlanInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	detail, err := s.store.UpdateWorkspaceTestPlan(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "planID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, testPlanDetailForResponse(detail))
}

func (s *Server) handleStartWorkspaceTestRun(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CreateTestRunInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	detail, err := s.store.StartWorkspaceTestRun(r.Context(), user, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "planID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, testRunDetailForResponse(detail))
}

func (s *Server) handleListWorkspaceTestRuns(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	options := TestRunListOptions{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Source: strings.TrimSpace(r.URL.Query().Get("source")),
	}
	runs, err := s.store.ListWorkspaceTestRuns(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), options)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) handleStartAdHocWorkspaceTestRun(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CreateAdHocTestRunInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	detail, err := s.store.StartAdHocWorkspaceTestRun(r.Context(), user, strings.TrimSpace(chi.URLParam(r, "workspaceID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, testRunDetailForResponse(detail))
}

func (s *Server) handleGetWorkspaceTestRun(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	detail, err := s.store.GetWorkspaceTestRun(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "runID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, testRunDetailForResponse(detail))
}

func (s *Server) handleListWorkspaceTestRunArtifacts(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	artifacts, err := s.store.ListWorkspaceTestRunArtifacts(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "runID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, testArtifactRefs(artifacts))
}

func (s *Server) handleRetryWorkspaceTestRun(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := RetryTestRunInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	detail, err := s.store.RetryWorkspaceTestRun(r.Context(), user, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "runID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, testRunDetailForResponse(detail))
}

func (s *Server) handleCancelWorkspaceTestRun(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CancelRuntimeTaskInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	detail, err := s.store.CancelWorkspaceTestRun(r.Context(), user, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "runID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, testRunDetailForResponse(detail))
}

func (s *Server) handleAcceptWorkspaceTestRun(w http.ResponseWriter, r *http.Request) {
	s.handleReviewWorkspaceTestRun(w, r, true)
}

func (s *Server) handleBlockWorkspaceTestRun(w http.ResponseWriter, r *http.Request) {
	s.handleReviewWorkspaceTestRun(w, r, false)
}

func (s *Server) handleReviewWorkspaceTestRun(w http.ResponseWriter, r *http.Request, accepted bool) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := ReviewTestRunInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	runID := strings.TrimSpace(chi.URLParam(r, "runID"))
	var (
		run TestRun
		err error
	)
	if accepted {
		run, err = s.store.AcceptWorkspaceTestRun(r.Context(), user.ID, workspaceID, runID, input)
	} else {
		run, err = s.store.BlockWorkspaceTestRun(r.Context(), user.ID, workspaceID, runID, input)
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleListProjectTestPlans(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	plans, err := s.store.ListProjectTestPlans(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), TestPlanListOptions{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, plans)
}

func (s *Server) handleCreateProjectTestPlan(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := TestPlanInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	detail, err := s.store.CreateProjectTestPlan(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, testPlanDetailForResponse(detail))
}

func (s *Server) handleGetProjectTestPlan(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	detail, err := s.store.GetProjectTestPlan(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), strings.TrimSpace(chi.URLParam(r, "planID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, testPlanDetailForResponse(detail))
}

func (s *Server) handleUpdateProjectTestPlan(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := TestPlanInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	detail, err := s.store.UpdateProjectTestPlan(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), strings.TrimSpace(chi.URLParam(r, "planID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, testPlanDetailForResponse(detail))
}

func (s *Server) handleStartProjectTestRun(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CreateTestRunInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	detail, err := s.store.StartProjectTestRun(r.Context(), user, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), strings.TrimSpace(chi.URLParam(r, "planID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, testRunDetailForResponse(detail))
}

func (s *Server) handleListProjectTestRuns(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	options := TestRunListOptions{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Source: strings.TrimSpace(r.URL.Query().Get("source")),
	}
	runs, err := s.store.ListProjectTestRuns(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), options)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) handleStartAdHocProjectTestRun(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CreateAdHocTestRunInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	detail, err := s.store.StartAdHocProjectTestRun(r.Context(), user, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, testRunDetailForResponse(detail))
}

func (s *Server) handleGetProjectTestRun(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	detail, err := s.store.GetProjectTestRun(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), strings.TrimSpace(chi.URLParam(r, "runID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, testRunDetailForResponse(detail))
}

func (s *Server) handleListProjectTestRunArtifacts(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	artifacts, err := s.store.ListProjectTestRunArtifacts(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), strings.TrimSpace(chi.URLParam(r, "runID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, testArtifactRefs(artifacts))
}

func (s *Server) handleListProjectTestCaseRunItems(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	items, err := s.store.ListProjectTestCaseRunItems(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), strings.TrimSpace(chi.URLParam(r, "caseID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleGetTestArtifact(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	artifact, err := s.store.GetTestArtifact(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "artifactID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Content-Type", artifact.ContentType)
	w.Header().Set("Content-Disposition", contentDispositionInline(artifact.Filename))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(artifact.Content)
}

func (s *Server) handleRetryProjectTestRun(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := RetryTestRunInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	detail, err := s.store.RetryProjectTestRun(r.Context(), user, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), strings.TrimSpace(chi.URLParam(r, "runID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, testRunDetailForResponse(detail))
}

func (s *Server) handleCancelProjectTestRun(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CancelRuntimeTaskInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	detail, err := s.store.CancelProjectTestRun(r.Context(), user, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), strings.TrimSpace(chi.URLParam(r, "runID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, testRunDetailForResponse(detail))
}

func (s *Server) handleAcceptProjectTestRun(w http.ResponseWriter, r *http.Request) {
	s.handleReviewProjectTestRun(w, r, true)
}

func (s *Server) handleBlockProjectTestRun(w http.ResponseWriter, r *http.Request) {
	s.handleReviewProjectTestRun(w, r, false)
}

func (s *Server) handleReviewProjectTestRun(w http.ResponseWriter, r *http.Request, accepted bool) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := ReviewTestRunInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	projectID := strings.TrimSpace(chi.URLParam(r, "projectID"))
	runID := strings.TrimSpace(chi.URLParam(r, "runID"))
	var (
		run TestRun
		err error
	)
	if accepted {
		run, err = s.store.AcceptProjectTestRun(r.Context(), user.ID, workspaceID, projectID, runID, input)
	} else {
		run, err = s.store.BlockProjectTestRun(r.Context(), user.ID, workspaceID, projectID, runID, input)
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func projectByID(projects []Project, projectID string) (Project, bool) {
	projectID = strings.TrimSpace(projectID)
	for _, project := range projects {
		if project.ID == projectID {
			return project, true
		}
	}
	return Project{}, false
}

func (s *Server) handleGetProjectTestCase(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	testCase, err := s.store.GetProjectTestCase(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), strings.TrimSpace(chi.URLParam(r, "caseID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, testCase)
}

func (s *Server) handleUpdateProjectTestCase(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := TestCaseInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	testCase, err := s.store.UpdateProjectTestCase(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), strings.TrimSpace(chi.URLParam(r, "caseID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, testCase)
}

func (s *Server) handleListProjectTestCaseRevisions(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	revisions, err := s.store.ListProjectTestCaseRevisions(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "projectID")), strings.TrimSpace(chi.URLParam(r, "caseID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, revisions)
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

func (s *Server) handleGetWorkspaceSettings(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	settings, err := s.store.GetWorkspaceSettings(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleUpdateWorkspaceSettings(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := WorkspaceSettingsInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	settings, err := s.store.UpdateWorkspaceSettings(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleGetWorkspaceGitHubAppInstallation(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	installation, err := s.store.GetWorkspaceGitHubAppInstallation(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workspaceGitHubAppInstallationForServer(installation, s.githubAppConfigured()))
}

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	skills, err := s.store.ListSkills(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, skills)
}

func (s *Server) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	skill, err := s.store.GetSkill(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "skillID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, skill)
}

func (s *Server) handleCreateSkill(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := SkillInput{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxWorkspaceSkillRequestBytes)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	skill, err := s.store.CreateSkill(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, skill)
}

func (s *Server) handleUpdateSkill(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := SkillInput{}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxWorkspaceSkillRequestBytes)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	skill, err := s.store.UpdateSkill(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "skillID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, skill)
}

func (s *Server) handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteSkill(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "skillID"))); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDuplicateSkill(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := DuplicateSkillInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	skill, err := s.store.DuplicateSkill(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "skillID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, skill)
}

func (s *Server) handleListAgentEngines(w http.ResponseWriter, r *http.Request) {
	_, workspaces, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if _, found := workspaceByID(workspaces, strings.TrimSpace(chi.URLParam(r, "workspaceID"))); !found {
		writeStoreError(w, ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, fixedAgentEngineCatalog())
}

func (s *Server) handleListEnvironments(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	environments, err := s.store.ListEnvironments(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, environments)
}

func (s *Server) handleCreateEnvironment(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := EnvironmentInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	environment, err := s.store.CreateEnvironment(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, environment)
}

func (s *Server) handleUpdateEnvironment(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := EnvironmentInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	environment, err := s.store.UpdateEnvironment(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "environmentID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, environment)
}

func (s *Server) handleCheckEnvironment(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := EnvironmentCheckInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	environment, err := s.store.CheckEnvironment(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "environmentID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, environment)
}

func (s *Server) handleDeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	err := s.store.DeleteEnvironment(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "environmentID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleListClusters(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	clusters, err := s.store.ListClusters(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, clusters)
}

func (s *Server) handleCreateCluster(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := ClusterInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cluster, err := s.store.CreateCluster(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, cluster)
}

func (s *Server) handleUpdateCluster(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := ClusterInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cluster, err := s.store.UpdateCluster(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "clusterID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cluster)
}

func (s *Server) handleCheckCluster(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	cluster, err := s.store.CheckCluster(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "clusterID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cluster)
}

func (s *Server) handleDeleteCluster(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	err := s.store.DeleteCluster(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "clusterID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDiscoverDefaultKubeconfigs(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	result, err := s.store.DiscoverDefaultKubeconfigs(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleImportKubeconfigs(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var input struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(input.Paths) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("at least one kubeconfig path is required"))
		return
	}
	result, err := s.store.ImportKubeconfigs(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), input.Paths)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	issues, err := s.store.ListIssues(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), IssueListOptions{
		IncludeTestAutomation: queryBool(r, "includeTestAutomation"),
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, issues)
}

func queryBool(r *http.Request, key string) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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
	normalizedInput, _, _, err := normalizeCreateIssueInput(input, user)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	issueID, err := s.store.CreateIssue(r.Context(), user, strings.TrimSpace(chi.URLParam(r, "workspaceID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	s.tryEnqueueIssueAnalysis(r.Context(), user.ID, workspaceID, issueID)
	s.enqueueIssueTypeTriageNow(r.Context(), user.ID, workspaceID, issueID, normalizedInput.Title)
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
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleCreateAgentSession(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CreateAgentSessionInput{}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := rejectClientAgentSessionSkillBundles(body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := rejectClientAgentSessionControlFields(body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &input); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
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
	writeJSON(w, http.StatusOK, publicRuntimeTask(task))
}

func (s *Server) handleStartIssueTestDeploy(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := StartTestDeployInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.store.StartIssueTestDeploy(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleRequestIssueTestEnvironmentCleanup(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := StartTestDeployInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.store.RequestIssueTestEnvironmentCleanup(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleRetainIssueTestEnvironment(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	environment, err := s.store.RetainIssueTestEnvironment(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, environment)
}

func (s *Server) handleListIssueTestEnvironmentResources(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if _, ok := r.URL.Query()["namespace"]; ok {
		writeError(w, http.StatusBadRequest, errors.New("namespace is fixed by the issue test environment"))
		return
	}
	resources, err := s.store.GetIssueTestEnvironmentResources(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resources)
}

func (s *Server) handleProbeIssueTestEnvironment(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	environment, err := s.store.ProbeIssueTestEnvironment(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, environment)
}

func (s *Server) handleCreateIssuePullRequestHandoff(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CreatePullRequestInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	session, err := s.store.CreateIssuePullRequestSession(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

func (s *Server) handleRefreshIssueHandoff(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	handoff, err := s.store.RefreshIssueHandoff(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")), strings.TrimSpace(chi.URLParam(r, "handoffID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, handoff)
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
	if input.ProjectID != nil && strings.TrimSpace(*input.ProjectID) != "" && issue.ParentIssueID == "" {
		s.tryEnqueueIssueAnalysis(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), issue.ID)
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

func (s *Server) handleCreateIssueAttachment(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxIssueAttachmentBytes+1024)
	if err := r.ParseMultipartForm(maxIssueAttachmentBytes); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("file is required"))
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxIssueAttachmentBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(content) > maxIssueAttachmentBytes {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("attachment exceeds %d bytes", maxIssueAttachmentBytes))
		return
	}
	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = http.DetectContentType(content)
	}
	attachment, err := s.store.CreateIssueAttachment(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "workspaceID")), strings.TrimSpace(chi.URLParam(r, "issueID")), CreateIssueAttachmentInput{
		Filename:    filepath.Base(header.Filename),
		ContentType: contentType,
		Content:     content,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, issueAttachmentResponse(attachment))
}

func (s *Server) handleGetIssueAttachment(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	attachment, err := s.store.GetIssueAttachment(r.Context(), user.ID, strings.TrimSpace(chi.URLParam(r, "attachmentID")))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Content-Type", attachment.ContentType)
	w.Header().Set("Content-Disposition", contentDispositionInline(attachment.Filename))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(attachment.Content)
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

func (s *Server) handleCreateWorkerInstallation(w http.ResponseWriter, r *http.Request) {
	user, workspaces, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	input := CreateWorkerInstallationInput{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	workspace, found := workspaceByID(workspaces, workspaceID)
	if !found {
		writeStoreError(w, ErrNotFound)
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = defaultWorkerInstallationName(workspace)
	}
	expiresInHours := input.ExpiresInHours
	if expiresInHours <= 0 {
		expiresInHours = 1
	}
	tokenResult, err := s.store.CreateRuntimeRegistrationToken(r.Context(), user.ID, workspaceID, CreateRuntimeRegistrationTokenInput{
		Name:           name,
		ExpiresInHours: expiresInHours,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	serverURL := publicServerURL(r)
	mode := runtimeModeForWorkspace(workspace)
	installScriptURL := serverURL + "/install/worker"
	result := WorkerInstallationResult{
		InstallCommand:   buildWorkerInstallCommand(installScriptURL, serverURL, tokenResult.Token, mode, name),
		InstallScriptURL: installScriptURL,
		ServerURL:        serverURL,
		RuntimeMode:      mode,
		WorkerName:       name,
		CredentialPrefix: tokenResult.RegistrationToken.TokenPrefix,
		ExpiresAt:        tokenResult.RegistrationToken.ExpiresAt,
	}
	writeJSON(w, http.StatusCreated, result)
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

func (s *Server) handleRuntimeAvailability(w http.ResponseWriter, r *http.Request) {
	user, workspaces, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	workspace, found := workspaceByID(workspaces, workspaceID)
	if !found {
		writeStoreError(w, ErrNotFound)
		return
	}
	requiredCapabilities, err := runtimeAvailabilityRequiredCapabilities(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workers, err := s.store.ListRuntimeWorkers(r.Context(), user.ID, workspaceID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var workingCopy *IssueWorkingCopy
	if issueID := strings.TrimSpace(r.URL.Query().Get("issueId")); issueID != "" {
		detail, err := s.store.GetIssue(r.Context(), user.ID, workspaceID, issueID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		workingCopy = detail.WorkingCopy
		requiredCapabilities, err = addIssueWorkingCopyCapability(requiredCapabilities)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	availability := evaluateRuntimeAvailabilityForWorkingCopy(
		workspaceID,
		workspace.Kind,
		r.URL.Query().Get("runtimeMode"),
		requiredCapabilities,
		workers,
		workingCopy,
		time.Now().UTC(),
	)
	writeJSON(w, http.StatusOK, availability)
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
	if err := rejectClientRuntimeTaskSkillBundles(input.Payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	normalized, err := normalizeCreateRuntimeTaskInput(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	task, err := s.store.CreateRuntimeTask(r.Context(), user.ID, workspaceID, normalized)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, publicRuntimeTask(task))
}

func (s *Server) handleListRuntimeTasks(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceID"))
	query := r.URL.Query()
	tasks, err := s.store.ListRuntimeTasksPage(r.Context(), user.ID, workspaceID, parseRuntimeTaskListOptions(query))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if _, hasLimit := query["limit"]; !hasLimit {
		if _, hasOffset := query["offset"]; !hasOffset {
			writeJSON(w, http.StatusOK, publicRuntimeTasks(tasks.Tasks))
			return
		}
	}
	writeJSON(w, http.StatusOK, publicRuntimeTaskListResult(tasks))
}

func parseRuntimeTaskListOptions(values url.Values) RuntimeTaskListOptions {
	options := RuntimeTaskListOptions{
		Limit:  defaultRuntimeTaskListLimit,
		Offset: 0,
	}
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			options.Limit = parsed
		}
	}
	if raw := strings.TrimSpace(values.Get("offset")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			options.Offset = parsed
		}
	}
	return normalizeRuntimeTaskListOptions(options)
}

func parseTestCaseListOptions(values url.Values, options TestCaseListOptions) TestCaseListOptions {
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			options.Limit = parsed
		}
	}
	if raw := strings.TrimSpace(values.Get("offset")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			options.Offset = parsed
		}
	}
	return normalizeTestCaseListOptions(options)
}

func normalizeTestCaseListOptions(options TestCaseListOptions) TestCaseListOptions {
	if options.Limit <= 0 {
		options.Limit = defaultTestCaseListLimit
	}
	if options.Limit > maxTestCaseListLimit {
		options.Limit = maxTestCaseListLimit
	}
	if options.Offset < 0 {
		options.Offset = 0
	}
	return options
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
	writeJSON(w, http.StatusOK, publicRuntimeTask(task))
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

func (s *Server) authResultForUser(r *http.Request, user User, workspaces []Workspace) (AuthResult, error) {
	token, expiresAt, err := s.store.CreateAuthSession(r.Context(), user.ID, s.config.SessionTTL)
	if err != nil {
		return AuthResult{}, err
	}
	identity, err := s.store.GetUserAuthIdentity(r.Context(), user.ID)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{
		Token:         token,
		ExpiresAt:     expiresAt.UTC().Format(time.RFC3339),
		User:          user,
		Workspaces:    workspaces,
		IsServerAdmin: s.isServerAdmin(user),
		Identity:      identity,
	}, nil
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

func workspaceByID(workspaces []Workspace, workspaceID string) (Workspace, bool) {
	workspaceID = strings.TrimSpace(workspaceID)
	for _, workspace := range workspaces {
		if workspace.ID == workspaceID {
			return workspace, true
		}
	}
	return Workspace{}, false
}

func runtimeAvailabilityRequiredCapabilities(values url.Values) (json.RawMessage, error) {
	if raw := strings.TrimSpace(values.Get("requiredCapabilities")); raw != "" {
		return normalizeJSONObjectPayload(json.RawMessage(raw))
	}
	capabilities := map[string]bool{}
	for _, value := range values["capability"] {
		for _, capability := range strings.Split(value, ",") {
			capability = strings.TrimSpace(capability)
			if capability != "" {
				capabilities[capability] = true
			}
		}
	}
	for _, value := range values["capabilities"] {
		for _, capability := range strings.Split(value, ",") {
			capability = strings.TrimSpace(capability)
			if capability != "" {
				capabilities[capability] = true
			}
		}
	}
	if len(capabilities) == 0 {
		capabilities["codex"] = true
	}
	payload, err := json.Marshal(capabilities)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(payload), nil
}

func runtimeModeForWorkspace(workspace Workspace) string {
	if workspace.Kind == "personal" {
		return "personal"
	}
	return "team"
}

func defaultWorkerInstallationName(workspace Workspace) string {
	slug := strings.TrimSpace(workspace.Slug)
	if slug == "" {
		slug = workspaceSlug(workspace.Name, workspace.ID)
	}
	return "worker-" + slug
}

func publicServerURL(r *http.Request) string {
	proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		host = "127.0.0.1:8787"
	}
	return strings.TrimRight(proto+"://"+host, "/")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func buildWorkerInstallCommand(installScriptURL, serverURL, token, mode, workerName string) string {
	env := []string{
		"MSPACE_SERVER_URL=" + shellQuote(serverURL),
		"MSPACE_RUNTIME_TOKEN=" + shellQuote(token),
		"MSPACE_WORKER_MODE=" + shellQuote(mode),
		"MSPACE_WORKER_NAME=" + shellQuote(workerName),
	}
	return strings.Join(env, " ") + " bash -c \"$(curl -fsSL " + shellQuote(installScriptURL) + ")\""
}

func contentDispositionInline(filename string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return "inline"
	}
	filename = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '\\' || r == '"' {
			return '_'
		}
		return r
	}, filename)
	if strings.TrimSpace(filename) == "" {
		return "inline"
	}
	if value := mime.FormatMediaType("inline", map[string]string{"filename": filename}); value != "" {
		return value
	}
	return "inline"
}

func allowedIssueAttachmentContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch contentType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func normalizeIssueAttachmentContentType(contentType string, content []byte) string {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if contentType == "image/jpg" {
		contentType = "image/jpeg"
	}
	if contentType == "" || !allowedIssueAttachmentContentType(contentType) {
		detected := strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(content), ";")[0]))
		if allowedIssueAttachmentContentType(detected) {
			return detected
		}
	}
	return contentType
}

func issueAttachmentResponse(attachment IssueAttachment) IssueAttachment {
	attachment.Content = nil
	attachment.StorageKey = ""
	if attachment.ID != "" {
		attachment.StorageKey = "/api/attachments/" + attachment.ID
	}
	return attachment
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

func publicRuntimeTaskListResult(result RuntimeTaskListResult) RuntimeTaskListResult {
	result.Tasks = publicRuntimeTasks(result.Tasks)
	return result
}

func publicRuntimeTasks(tasks []RuntimeTask) []RuntimeTask {
	if len(tasks) == 0 {
		return tasks
	}
	redacted := make([]RuntimeTask, 0, len(tasks))
	for _, task := range tasks {
		redacted = append(redacted, publicRuntimeTask(task))
	}
	return redacted
}

func publicRuntimeTask(task RuntimeTask) RuntimeTask {
	task.Payload = redactRuntimeTaskSkillPayload(task.Payload)
	return task
}

func rejectClientRuntimeTaskSkillBundles(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	for _, key := range []string{"requiredSkills", "skills"} {
		if jsonObjectHasFieldFold(payload, key) {
			return errors.New("runtime task skill bundles are server-managed")
		}
	}
	return nil
}

func rejectClientAgentSessionSkillBundles(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	for _, key := range []string{"requiredSkills", "skills", "skillBundles"} {
		if jsonObjectHasFieldFold(payload, key) {
			return errors.New("agent session skill bundles are server-managed; use skillSlugs")
		}
	}
	return nil
}

func rejectClientAgentSessionControlFields(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	for _, key := range []string{
		"runtimeMode",
		"provider",
		"agentProfile",
		"branch",
		"automation",
		"testRunId",
		"testRunBatchSize",
		"sourceSessionId",
		"sourceCommitSha",
		"requiredCapabilities",
		"env",
		"workdir",
		"issueId",
		"sessionId",
		"projectId",
		"artifactDir",
		"repository",
		"sourceCapture",
		"executionMode",
		"workingCopy",
		"workingCopyGeneration",
		"expectedHeadSha",
		"initialize",
		"storageId",
		"storageAffinityId",
		"developerInstructions",
		"approvalPolicy",
		"sandbox",
	} {
		if jsonObjectHasFieldFold(payload, key) {
			return fmt.Errorf("agent session field %s is server-managed", key)
		}
	}
	return nil
}

func redactRuntimeTaskSkillPayload(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return raw
	}
	changed := false
	for actualKey, value := range payload {
		if strings.EqualFold(actualKey, "requiredSkills") || strings.EqualFold(actualKey, "skills") {
			payload[actualKey] = redactRuntimeTaskSkillReferences(value)
			changed = true
		}
	}
	if !changed {
		return raw
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return raw
	}
	return json.RawMessage(body)
}

func jsonObjectHasFieldFold(payload map[string]json.RawMessage, field string) bool {
	for key := range payload {
		if strings.EqualFold(key, field) {
			return true
		}
	}
	return false
}

func redactRuntimeTaskSkillReferences(value any) any {
	switch typed := value.(type) {
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, redactRuntimeTaskSkillReference(item))
		}
		return result
	default:
		return redactRuntimeTaskSkillReference(value)
	}
}

func redactRuntimeTaskSkillReference(value any) any {
	record, ok := value.(map[string]any)
	if !ok {
		return value
	}
	reference := map[string]any{}
	for _, key := range []string{"slug", "name", "source", "revision", "contentHash", "builtIn"} {
		if field, ok := record[key]; ok {
			reference[key] = field
		}
	}
	if len(reference) == 0 {
		return map[string]any{"redacted": true}
	}
	return reference
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
	} else if errors.Is(err, ErrNoActiveCodexWorker) || errors.Is(err, ErrNoActiveAgentWorker) {
		status = http.StatusConflict
	} else if errors.Is(err, ErrConflict) {
		status = http.StatusConflict
	} else if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "requires") || strings.Contains(err.Error(), "must be") || strings.Contains(err.Error(), "skill slug") || strings.Contains(err.Error(), "greater than") || strings.Contains(err.Error(), "valid JSON") || strings.Contains(err.Error(), "unsupported") || strings.Contains(err.Error(), "cannot be empty") || strings.Contains(err.Error(), "not safe") || strings.Contains(err.Error(), "server-managed") {
		status = http.StatusBadRequest
	}
	writeError(w, status, err)
}
