package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
