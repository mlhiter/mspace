package control

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type Server struct {
	config Config
	store  Store
	github GitHubClient
}

const serverProtocolVersion = 1

func NewServer(config Config, store Store, github GitHubClient) *Server {
	config = config.withDefaults()
	return &Server{config: config, store: store, github: github}
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
	r.Get("/api/workspaces/{workspaceID}/inbox", s.handleWorkspaceInbox)
	r.Post("/api/workspaces/{workspaceID}/issue-events", s.handleCreateIssueEvent)
	r.Post("/api/workspaces/{workspaceID}/issue-events/{eventID}/read", s.handleMarkIssueEventRead)
	r.Post("/api/workspaces/{workspaceID}/issues/{issueID}/read-through", s.handleMarkIssueReadThrough)
	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"service":        "mspace-server",
		"version":        "0.1.0",
		"serverProtocol": serverProtocolVersion,
		"capabilities": map[string]bool{
			"teamInboxIssueGrouping": true,
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
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
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
	} else if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "valid JSON") {
		status = http.StatusBadRequest
	}
	writeError(w, status, err)
}
