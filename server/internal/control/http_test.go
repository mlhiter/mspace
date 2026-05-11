package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeGitHubClient struct{}

func (fakeGitHubClient) ExchangeCode(_ context.Context, code, _ string) (string, error) {
	if code != "ok-code" {
		return "", ErrNotFound
	}
	return "github-token", nil
}

func (fakeGitHubClient) FetchUser(_ context.Context, accessToken string) (IdentityProfile, error) {
	if accessToken != "github-token" {
		return IdentityProfile{}, ErrNotFound
	}
	return IdentityProfile{
		Provider:       "github",
		ProviderUserID: "123",
		Login:          "mlhiter",
		Name:           "ML Hiter",
		Email:          "mlhiter@example.com",
		AvatarURL:      "https://avatars.githubusercontent.com/u/123?v=4",
		RawProfile:     json.RawMessage(`{"id":123,"login":"mlhiter"}`),
	}, nil
}

func TestHealthAdvertisesServerProtocol(t *testing.T) {
	server := NewServer(Config{}, NewMemoryStore(), fakeGitHubClient{})
	router := server.Routes()

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		OK             bool            `json:"ok"`
		Service        string          `json:"service"`
		ServerProtocol int             `json:"serverProtocol"`
		Capabilities   map[string]bool `json:"capabilities"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("parse health response: %v", err)
	}
	if !payload.OK || payload.Service != "mspace-server" || payload.ServerProtocol != serverProtocolVersion {
		t.Fatalf("unexpected health payload: %+v", payload)
	}
	if payload.Capabilities["teamInboxIssueGrouping"] != true {
		t.Fatalf("expected team inbox grouping capability, got %+v", payload.Capabilities)
	}
}

func TestGitHubLoginIssuesMspaceSession(t *testing.T) {
	store := NewMemoryStore()
	server := NewServer(Config{
		GitHubClientID:     "client-id",
		GitHubClientSecret: "client-secret",
		GitHubRedirectURI:  "http://127.0.0.1:8787/api/auth/github/callback",
	}, store, fakeGitHubClient{})
	router := server.Routes()

	startRecorder := httptest.NewRecorder()
	router.ServeHTTP(startRecorder, httptest.NewRequest(http.MethodGet, "/api/auth/github/start", nil))
	if startRecorder.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", startRecorder.Code, startRecorder.Body.String())
	}
	var start struct {
		AuthorizeURL string `json:"authorizeUrl"`
		State        string `json:"state"`
		PollURL      string `json:"pollUrl"`
	}
	if err := json.Unmarshal(startRecorder.Body.Bytes(), &start); err != nil {
		t.Fatalf("parse start response: %v", err)
	}
	if start.State == "" ||
		!strings.Contains(start.AuthorizeURL, "github.com/login/oauth/authorize") ||
		!strings.Contains(start.PollURL, "/api/auth/github/result?state=") {
		t.Fatalf("unexpected start response: %+v", start)
	}

	pendingRecorder := httptest.NewRecorder()
	router.ServeHTTP(pendingRecorder, httptest.NewRequest(http.MethodGet, "/api/auth/github/result?state="+start.State, nil))
	if pendingRecorder.Code != http.StatusAccepted {
		t.Fatalf("pending status=%d body=%s", pendingRecorder.Code, pendingRecorder.Body.String())
	}

	callbackRecorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/github/callback?code=ok-code&state="+start.State, nil)
	router.ServeHTTP(callbackRecorder, req)
	if callbackRecorder.Code != http.StatusOK {
		t.Fatalf("callback status=%d body=%s", callbackRecorder.Code, callbackRecorder.Body.String())
	}
	if !strings.Contains(callbackRecorder.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("expected html callback, got content-type=%q", callbackRecorder.Header().Get("Content-Type"))
	}

	resultRecorder := httptest.NewRecorder()
	router.ServeHTTP(resultRecorder, httptest.NewRequest(http.MethodGet, "/api/auth/github/result?state="+start.State, nil))
	if resultRecorder.Code != http.StatusOK {
		t.Fatalf("result status=%d body=%s", resultRecorder.Code, resultRecorder.Body.String())
	}
	var auth AuthResult
	if err := json.Unmarshal(resultRecorder.Body.Bytes(), &auth); err != nil {
		t.Fatalf("parse result response: %v", err)
	}
	if !strings.HasPrefix(auth.Token, "msp_") {
		t.Fatalf("expected mspace token, got %q", auth.Token)
	}
	if auth.User.Email != "mlhiter@example.com" || len(auth.Workspaces) != 1 || auth.Workspaces[0].Role != "owner" {
		t.Fatalf("unexpected auth result: %+v", auth)
	}

	meRecorder := httptest.NewRecorder()
	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+auth.Token)
	router.ServeHTTP(meRecorder, meReq)
	if meRecorder.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", meRecorder.Code, meRecorder.Body.String())
	}

	workspaceID := auth.Workspaces[0].ID
	eventBody := `{"issueId":"issue-1","kind":"agent_completed","summary":"Agent completed issue work.","payload":{"title":"Fix inbox","status":"closed","projectName":"mspace"}}`
	createEventRecorder := httptest.NewRecorder()
	createEventReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/issue-events", strings.NewReader(eventBody))
	createEventReq.Header.Set("Authorization", "Bearer "+auth.Token)
	router.ServeHTTP(createEventRecorder, createEventReq)
	if createEventRecorder.Code != http.StatusCreated {
		t.Fatalf("create event status=%d body=%s", createEventRecorder.Code, createEventRecorder.Body.String())
	}
	var event IssueEvent
	if err := json.Unmarshal(createEventRecorder.Body.Bytes(), &event); err != nil {
		t.Fatalf("parse event response: %v", err)
	}
	if event.ID == "" || event.WorkspaceID != workspaceID || event.IssueID != "issue-1" {
		t.Fatalf("unexpected event: %+v", event)
	}

	inboxRecorder := httptest.NewRecorder()
	inboxReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/inbox", nil)
	inboxReq.Header.Set("Authorization", "Bearer "+auth.Token)
	router.ServeHTTP(inboxRecorder, inboxReq)
	if inboxRecorder.Code != http.StatusOK {
		t.Fatalf("inbox status=%d body=%s", inboxRecorder.Code, inboxRecorder.Body.String())
	}
	var inbox []InboxEntry
	if err := json.Unmarshal(inboxRecorder.Body.Bytes(), &inbox); err != nil {
		t.Fatalf("parse inbox response: %v", err)
	}
	if len(inbox) != 1 || inbox[0].EventID != event.ID || inbox[0].UnreadCount != 1 {
		t.Fatalf("unexpected inbox: %+v", inbox)
	}

	time.Sleep(time.Millisecond)
	secondEventBody := `{"issueId":"issue-1","kind":"status_changed","summary":"Issue moved to review.","payload":{"title":"Fix inbox","status":"needs_review","projectName":"mspace"}}`
	secondEventRecorder := httptest.NewRecorder()
	secondEventReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/issue-events", strings.NewReader(secondEventBody))
	secondEventReq.Header.Set("Authorization", "Bearer "+auth.Token)
	router.ServeHTTP(secondEventRecorder, secondEventReq)
	if secondEventRecorder.Code != http.StatusCreated {
		t.Fatalf("create second event status=%d body=%s", secondEventRecorder.Code, secondEventRecorder.Body.String())
	}
	var secondEvent IssueEvent
	if err := json.Unmarshal(secondEventRecorder.Body.Bytes(), &secondEvent); err != nil {
		t.Fatalf("parse second event response: %v", err)
	}

	groupedInboxRecorder := httptest.NewRecorder()
	groupedInboxReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/inbox", nil)
	groupedInboxReq.Header.Set("Authorization", "Bearer "+auth.Token)
	router.ServeHTTP(groupedInboxRecorder, groupedInboxReq)
	if groupedInboxRecorder.Code != http.StatusOK {
		t.Fatalf("grouped inbox status=%d body=%s", groupedInboxRecorder.Code, groupedInboxRecorder.Body.String())
	}
	if err := json.Unmarshal(groupedInboxRecorder.Body.Bytes(), &inbox); err != nil {
		t.Fatalf("parse grouped inbox response: %v", err)
	}
	if len(inbox) != 1 || inbox[0].EventID != secondEvent.ID || inbox[0].UnreadCount != 2 {
		t.Fatalf("expected latest event grouped by issue with unread count 2, got %+v", inbox)
	}

	readRecorder := httptest.NewRecorder()
	readReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/issues/issue-1/read-through", strings.NewReader(`{"throughEventId":"`+secondEvent.ID+`"}`))
	readReq.Header.Set("Authorization", "Bearer "+auth.Token)
	router.ServeHTTP(readRecorder, readReq)
	if readRecorder.Code != http.StatusOK {
		t.Fatalf("read-through status=%d body=%s", readRecorder.Code, readRecorder.Body.String())
	}

	emptyInboxRecorder := httptest.NewRecorder()
	emptyInboxReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/inbox", nil)
	emptyInboxReq.Header.Set("Authorization", "Bearer "+auth.Token)
	router.ServeHTTP(emptyInboxRecorder, emptyInboxReq)
	if emptyInboxRecorder.Code != http.StatusOK {
		t.Fatalf("empty inbox status=%d body=%s", emptyInboxRecorder.Code, emptyInboxRecorder.Body.String())
	}
	if err := json.Unmarshal(emptyInboxRecorder.Body.Bytes(), &inbox); err != nil {
		t.Fatalf("parse empty inbox response: %v", err)
	}
	if len(inbox) != 0 {
		t.Fatalf("expected empty inbox after read-through, got %+v", inbox)
	}

	reusedResultRecorder := httptest.NewRecorder()
	router.ServeHTTP(reusedResultRecorder, httptest.NewRequest(http.MethodGet, "/api/auth/github/result?state="+start.State, nil))
	if reusedResultRecorder.Code != http.StatusBadRequest {
		t.Fatalf("reused result should be consumed, status=%d body=%s", reusedResultRecorder.Code, reusedResultRecorder.Body.String())
	}
}

func TestGitHubCallbackRejectsReusedState(t *testing.T) {
	store := NewMemoryStore()
	server := NewServer(Config{
		GitHubClientID:     "client-id",
		GitHubClientSecret: "client-secret",
		GitHubRedirectURI:  "http://127.0.0.1:8787/api/auth/github/callback",
	}, store, fakeGitHubClient{})
	router := server.Routes()

	startRecorder := httptest.NewRecorder()
	router.ServeHTTP(startRecorder, httptest.NewRequest(http.MethodGet, "/api/auth/github/start", nil))
	var start struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(startRecorder.Body.Bytes(), &start); err != nil {
		t.Fatalf("parse start response: %v", err)
	}

	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/auth/github/callback?code=ok-code&state="+start.State, nil))
		if i == 0 && recorder.Code != http.StatusOK {
			t.Fatalf("first callback status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		if i == 1 && recorder.Code != http.StatusBadRequest {
			t.Fatalf("second callback should reject reused state, status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}
