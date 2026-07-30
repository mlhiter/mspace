package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

type fakeGitHubClient struct {
	pullRequests   map[string]GitHubPullRequest
	pullRequestErr error
}

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

func (c fakeGitHubClient) FetchPullRequest(_ context.Context, ref gitPullRequestRef, _ string) (GitHubPullRequest, error) {
	if c.pullRequestErr != nil {
		return GitHubPullRequest{}, c.pullRequestErr
	}
	if c.pullRequests == nil {
		return GitHubPullRequest{}, GitHubAPIError{Operation: "fetch github pull request", StatusCode: http.StatusNotFound}
	}
	pr, ok := c.pullRequests[fakeGitHubPullRequestKey(ref)]
	if !ok {
		return GitHubPullRequest{}, GitHubAPIError{Operation: "fetch github pull request", StatusCode: http.StatusNotFound}
	}
	return pr, nil
}

func fakeGitHubPullRequestKey(ref gitPullRequestRef) string {
	return strings.ToLower(strings.TrimSpace(ref.owner)) + "/" + strings.ToLower(strings.TrimSpace(ref.repo)) + "#" + fmt.Sprint(ref.number)
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
		Version        string          `json:"version"`
		CommitSHA      string          `json:"commitSha"`
		BuildTime      string          `json:"buildTime"`
		ServerProtocol int             `json:"serverProtocol"`
		Capabilities   map[string]bool `json:"capabilities"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("parse health response: %v", err)
	}
	if !payload.OK || payload.Service != "mspace-server" || payload.ServerProtocol != serverProtocolVersion {
		t.Fatalf("unexpected health payload: %+v", payload)
	}
	if payload.Version != "dev" || payload.CommitSHA != "unknown" || payload.BuildTime != "unknown" {
		t.Fatalf("unexpected default build identity: %+v", payload)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected Cache-Control no-store, got %q", got)
	}
	if payload.Capabilities["workspaceInboxIssueGrouping"] != true {
		t.Fatalf("expected workspace inbox grouping capability, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["teamWorkspaceCreation"] != true {
		t.Fatalf("expected team workspace creation capability, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["workspaceInvitations"] != true {
		t.Fatalf("expected workspace invitations capability, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["workspaceInvitationPreview"] != true {
		t.Fatalf("expected workspace invitation preview capability, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["workspaceKinds"] != true {
		t.Fatalf("expected workspace kinds capability, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["githubAuth"] != false {
		t.Fatalf("expected disabled github auth capability without OAuth config, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["githubApp"] != false {
		t.Fatalf("expected disabled github app capability without app config, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["passwordAuth"] != true {
		t.Fatalf("expected password auth capability, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["runtimeWorkerRegistration"] != true {
		t.Fatalf("expected runtime worker registration capability, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["runtimeAvailability"] != true {
		t.Fatalf("expected runtime availability capability, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["runtimeTaskQueue"] != true {
		t.Fatalf("expected runtime task queue capability, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["testCaseLibrary"] != true {
		t.Fatalf("expected test case library capability, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["testCaseWorkflow"] != true {
		t.Fatalf("expected test case workflow capability, got %+v", payload.Capabilities)
	}
}

func TestHealthReportsCompileTimeBuildIdentity(t *testing.T) {
	originalVersion, originalCommitSHA, originalBuildTime := version, commitSHA, buildTime
	version = "1.2.3"
	commitSHA = "0123456789abcdef"
	buildTime = "2026-07-27T09:30:00Z"
	defer func() {
		version, commitSHA, buildTime = originalVersion, originalCommitSHA, originalBuildTime
	}()

	server := NewServer(Config{}, NewMemoryStore(), fakeGitHubClient{})
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

	var payload struct {
		Version   string `json:"version"`
		CommitSHA string `json:"commitSha"`
		BuildTime string `json:"buildTime"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("parse health response: %v", err)
	}
	if payload.Version != version || payload.CommitSHA != commitSHA || payload.BuildTime != buildTime {
		t.Fatalf("unexpected injected build identity: %+v", payload)
	}
}

func TestHealthReportsGitHubAuthCapability(t *testing.T) {
	server := NewServer(Config{
		GitHubClientID:     "client-id",
		GitHubClientSecret: "client-secret",
		GitHubRedirectURI:  "http://127.0.0.1:8787/api/auth/github/callback",
	}, NewMemoryStore(), fakeGitHubClient{})
	router := server.Routes()

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Capabilities map[string]bool `json:"capabilities"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("parse health response: %v", err)
	}
	if payload.Capabilities["githubAuth"] != true {
		t.Fatalf("expected enabled github auth capability with OAuth config, got %+v", payload.Capabilities)
	}
}

func TestHealthReportsGitHubAppCapability(t *testing.T) {
	server := NewServer(Config{
		GitHubAppID:         "12345",
		GitHubAppClientID:   "Iv1.example",
		GitHubAppPrivateKey: "-----BEGIN PRIVATE KEY-----\nredacted\n-----END PRIVATE KEY-----",
	}, NewMemoryStore(), fakeGitHubClient{})
	router := server.Routes()

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Capabilities map[string]bool `json:"capabilities"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("parse health response: %v", err)
	}
	if payload.Capabilities["githubApp"] != true {
		t.Fatalf("expected enabled github app capability with app config, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["githubAuth"] != false {
		t.Fatalf("github app config should not imply github auth capability, got %+v", payload.Capabilities)
	}
}

func TestWorkspaceGitHubAppInstallationStatus(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "github-app-owner",
		Login:          "github-app-owner",
		Name:           "GitHub App Owner",
		Email:          "github-app-owner@example.com",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID

	getInstallation := func(t *testing.T, server *Server) WorkspaceGitHubAppInstallation {
		t.Helper()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/github-app", nil)
		req.Header.Set("Authorization", "Bearer "+sessionToken)
		server.Routes().ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("github app status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "PRIVATE KEY") {
			t.Fatalf("github app response leaked private key: %s", recorder.Body.String())
		}
		var installation WorkspaceGitHubAppInstallation
		if err := json.Unmarshal(recorder.Body.Bytes(), &installation); err != nil {
			t.Fatalf("parse github app installation: %v", err)
		}
		return installation
	}

	unavailable := getInstallation(t, NewServer(Config{}, store, fakeGitHubClient{}))
	if unavailable.Available || unavailable.Status != WorkspaceGitHubAppStatusUnavailable {
		t.Fatalf("expected unavailable github app status without server config, got %+v", unavailable)
	}
	if unavailable.RequiredPermissions["contents"] != "write" || unavailable.Permissions == nil {
		t.Fatalf("expected required permissions in unavailable status, got %+v", unavailable)
	}

	configuredServer := NewServer(Config{
		GitHubAppID:         "12345",
		GitHubAppClientID:   "Iv1.example",
		GitHubAppPrivateKey: "-----BEGIN PRIVATE KEY-----\nredacted\n-----END PRIVATE KEY-----",
	}, store, fakeGitHubClient{})
	notConnected := getInstallation(t, configuredServer)
	if !notConnected.Available || notConnected.Status != WorkspaceGitHubAppStatusNotConnected {
		t.Fatalf("expected configured server to report not connected by default, got %+v", notConnected)
	}

	if _, err := store.UpsertWorkspaceGitHubAppInstallation(context.Background(), workspaceID, WorkspaceGitHubAppInstallation{
		Status:              WorkspaceGitHubAppStatusConnected,
		InstallationID:      "98765",
		AccountLogin:        "mlhiter",
		AccountType:         "User",
		RepositorySelection: "selected",
		Permissions: map[string]string{
			"contents":      "write",
			"metadata":      "read",
			"pull_requests": "read",
		},
		HTMLURL:      "https://github.com/settings/installations/98765",
		LastSyncedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("seed github app installation: %v", err)
	}
	needsAttention := getInstallation(t, configuredServer)
	if needsAttention.Status != WorkspaceGitHubAppStatusNeedsAction || len(needsAttention.MissingPermissions) != 1 || needsAttention.MissingPermissions[0] != "pull_requests:write" {
		t.Fatalf("expected missing pull request write permission, got %+v", needsAttention)
	}
}

func TestRefreshIssueHandoffReadsGitHubPullRequestState(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	user, workspaces, err := store.CreatePasswordIdentity(ctx, PasswordAuthInput{
		Login:    "pr-refresh-user",
		Password: "password-123456",
		Name:     "PR Refresh User",
	})
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	workspaceID := workspaces[0].ID
	token, _, err := store.CreateAuthSession(ctx, user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	project, issueID := seedPullRequestIssue(t, ctx, store, user, workspaceID, "Refresh merged PR")
	seedIssueSourceTask(t, store, workspaceID, issueID, project.ID, "session-source-refresh", "personal", pullRequestHandoffSourceCommitSHA, "mspace/pr-refresh")
	handoff, err := store.CreateIssuePullRequestHandoff(ctx, user.ID, workspaceID, issueID, CreatePullRequestInput{
		SourceSessionID: "session-source-refresh",
		SourceCommitSHA: pullRequestHandoffSourceCommitSHA,
		HeadCommitSHA:   pullRequestHandoffSourceCommitSHA,
		PRURL:           "https://github.com/mlhiter/mspace/pull/42",
		PRNumber:        42,
		PRState:         "open",
		CreatedVia:      "codex",
	})
	if err != nil {
		t.Fatalf("create PR handoff: %v", err)
	}

	server := NewServer(Config{}, store, fakeGitHubClient{
		pullRequests: map[string]GitHubPullRequest{
			"mlhiter/mspace#42": {
				URL:           "https://github.com/mlhiter/mspace/pull/42",
				Number:        42,
				State:         "closed",
				Merged:        true,
				MergedAt:      "2026-07-30T03:00:00Z",
				Title:         "fix(issue): refresh merged PR state",
				HeadCommitSHA: "2222222222222222222222222222222222222222",
			},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/issues/"+issueID+"/handoffs/"+handoff.ID+"/refresh", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("refresh handoff status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var refreshed IssueHandoff
	if err := json.Unmarshal(recorder.Body.Bytes(), &refreshed); err != nil {
		t.Fatalf("parse refreshed handoff: %v", err)
	}
	if refreshed.PRState != "merged" || refreshed.Error != "" {
		t.Fatalf("expected merged PR state without error, got %+v", refreshed)
	}
	if refreshed.PRTitle != "fix(issue): refresh merged PR state" || refreshed.HeadCommitSHA != "2222222222222222222222222222222222222222" {
		t.Fatalf("expected refreshed PR metadata, got %+v", refreshed)
	}
	if refreshed.LastCheckedAt == "" {
		t.Fatalf("expected refresh to update last checked timestamp")
	}
}

func TestFallbackIssueTitleSkipsGitHubURLLine(t *testing.T) {
	title := fallbackIssueTitle("https://github.com/mlhiter/orcai.git\n这个项目，我想优化一下他的页面")
	if title == "" {
		t.Fatal("expected fallback title")
	}
	if strings.Contains(title, "github.com") || strings.Contains(title, "[") || strings.Contains(title, "]") {
		t.Fatalf("expected readable title, got %q", title)
	}
	if !strings.Contains(strings.ToLower(title), "orcai") {
		t.Fatalf("expected repository name in title, got %q", title)
	}
}

func TestGitHubLoginIssuesMspaceSession(t *testing.T) {
	store := NewMemoryStore()
	server := NewServer(Config{
		GitHubClientID:     "client-id",
		GitHubClientSecret: "client-secret",
		GitHubRedirectURI:  "http://127.0.0.1:8787/api/auth/github/callback",
		ServerAdminLogins:  []string{"mlhiter"},
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
	if auth.Identity.Provider != "github" || auth.Identity.Login != "mlhiter" {
		t.Fatalf("unexpected auth identity: %+v", auth.Identity)
	}
	if auth.Workspaces[0].Kind != "personal" {
		t.Fatalf("default workspace should be personal, got %+v", auth.Workspaces[0])
	}

	createWorkspaceRecorder := httptest.NewRecorder()
	createWorkspaceReq := httptest.NewRequest(http.MethodPost, "/api/workspaces", strings.NewReader(`{"name":"ML team","kind":"team"}`))
	createWorkspaceReq.Header.Set("Authorization", "Bearer "+auth.Token)
	router.ServeHTTP(createWorkspaceRecorder, createWorkspaceReq)
	if createWorkspaceRecorder.Code != http.StatusCreated {
		t.Fatalf("create team workspace status=%d body=%s", createWorkspaceRecorder.Code, createWorkspaceRecorder.Body.String())
	}
	var workspaceResult CreateWorkspaceResult
	if err := json.Unmarshal(createWorkspaceRecorder.Body.Bytes(), &workspaceResult); err != nil {
		t.Fatalf("parse create workspace result: %v", err)
	}
	if workspaceResult.Workspace.Kind != "team" || workspaceResult.Workspace.Role != "owner" || workspaceResult.Workspace.Name != "ML team" {
		t.Fatalf("unexpected created workspace: %+v", workspaceResult.Workspace)
	}
	if len(workspaceResult.Workspaces) != 2 {
		t.Fatalf("expected personal and team workspace, got %+v", workspaceResult.Workspaces)
	}

	meRecorder := httptest.NewRecorder()
	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+auth.Token)
	router.ServeHTTP(meRecorder, meReq)
	if meRecorder.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", meRecorder.Code, meRecorder.Body.String())
	}

	workspaceID := workspaceResult.Workspace.ID
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

func TestPasswordAuthIssuesMspaceSession(t *testing.T) {
	store := NewMemoryStore()
	server := NewServer(Config{ServerAdminLogins: []string{"local-admin"}}, store, fakeGitHubClient{})
	router := server.Routes()

	registerBody := `{"login":"local-admin","name":"Local Admin","email":"admin@example.test","password":"correct-password"}`
	registerRecorder := httptest.NewRecorder()
	router.ServeHTTP(registerRecorder, httptest.NewRequest(http.MethodPost, "/api/auth/password/register", strings.NewReader(registerBody)))
	if registerRecorder.Code != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", registerRecorder.Code, registerRecorder.Body.String())
	}
	var register AuthResult
	if err := json.Unmarshal(registerRecorder.Body.Bytes(), &register); err != nil {
		t.Fatalf("parse register response: %v", err)
	}
	if !strings.HasPrefix(register.Token, "msp_") {
		t.Fatalf("expected mspace token, got %q", register.Token)
	}
	if register.Identity.Provider != "password" || register.Identity.Login != "local-admin" {
		t.Fatalf("unexpected registered identity: %+v", register.Identity)
	}
	if register.User.Name != "Local Admin" || register.User.Email != "" {
		t.Fatalf("unexpected registered user: %+v", register.User)
	}
	if len(register.Workspaces) != 1 || register.Workspaces[0].Kind != "personal" || register.Workspaces[0].Role != "owner" {
		t.Fatalf("unexpected default workspace: %+v", register.Workspaces)
	}
	if !register.IsServerAdmin {
		t.Fatalf("expected bootstrap login to be server admin")
	}

	meRecorder := httptest.NewRecorder()
	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+register.Token)
	router.ServeHTTP(meRecorder, meReq)
	if meRecorder.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", meRecorder.Code, meRecorder.Body.String())
	}

	updateRecorder := httptest.NewRecorder()
	updateReq := httptest.NewRequest(http.MethodPut, "/api/auth/me", strings.NewReader(`{"name":"Updated Admin","avatarUrl":"https://avatars.example.test/local-admin.png","login":"should-not-change"}`))
	updateReq.Header.Set("Authorization", "Bearer "+register.Token)
	router.ServeHTTP(updateRecorder, updateReq)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update profile status=%d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	var updated AuthResult
	if err := json.Unmarshal(updateRecorder.Body.Bytes(), &updated); err != nil {
		t.Fatalf("parse update profile response: %v", err)
	}
	if updated.User.Name != "Updated Admin" || updated.User.AvatarURL != "https://avatars.example.test/local-admin.png" {
		t.Fatalf("unexpected updated user: %+v", updated.User)
	}
	if updated.Identity.Provider != "password" || updated.Identity.Login != "local-admin" {
		t.Fatalf("profile update should not change identity, got %+v", updated.Identity)
	}
	if !updated.IsServerAdmin {
		t.Fatalf("profile update should preserve server admin status")
	}

	updatedMeRecorder := httptest.NewRecorder()
	updatedMeReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	updatedMeReq.Header.Set("Authorization", "Bearer "+register.Token)
	router.ServeHTTP(updatedMeRecorder, updatedMeReq)
	if updatedMeRecorder.Code != http.StatusOK {
		t.Fatalf("updated me status=%d body=%s", updatedMeRecorder.Code, updatedMeRecorder.Body.String())
	}
	var updatedMe AuthResult
	if err := json.Unmarshal(updatedMeRecorder.Body.Bytes(), &updatedMe); err != nil {
		t.Fatalf("parse updated me response: %v", err)
	}
	if updatedMe.User.Name != "Updated Admin" || updatedMe.User.AvatarURL != "https://avatars.example.test/local-admin.png" {
		t.Fatalf("expected persisted profile in me response, got %+v", updatedMe.User)
	}
	if updatedMe.Identity.Login != "local-admin" {
		t.Fatalf("expected identity login to stay local-admin, got %+v", updatedMe.Identity)
	}

	unauthorizedUpdateRecorder := httptest.NewRecorder()
	router.ServeHTTP(unauthorizedUpdateRecorder, httptest.NewRequest(http.MethodPut, "/api/auth/me", strings.NewReader(`{"name":"Nope","avatarUrl":""}`)))
	if unauthorizedUpdateRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized update status=%d body=%s", unauthorizedUpdateRecorder.Code, unauthorizedUpdateRecorder.Body.String())
	}

	blankNameRecorder := httptest.NewRecorder()
	blankNameReq := httptest.NewRequest(http.MethodPut, "/api/auth/me", strings.NewReader(`{"name":"   ","avatarUrl":""}`))
	blankNameReq.Header.Set("Authorization", "Bearer "+register.Token)
	router.ServeHTTP(blankNameRecorder, blankNameReq)
	if blankNameRecorder.Code != http.StatusBadRequest {
		t.Fatalf("blank profile name status=%d body=%s", blankNameRecorder.Code, blankNameRecorder.Body.String())
	}

	invalidAvatarRecorder := httptest.NewRecorder()
	invalidAvatarReq := httptest.NewRequest(http.MethodPut, "/api/auth/me", strings.NewReader(`{"name":"Updated Admin","avatarUrl":"javascript:alert(1)"}`))
	invalidAvatarReq.Header.Set("Authorization", "Bearer "+register.Token)
	router.ServeHTTP(invalidAvatarRecorder, invalidAvatarReq)
	if invalidAvatarRecorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid avatar URL status=%d body=%s", invalidAvatarRecorder.Code, invalidAvatarRecorder.Body.String())
	}

	loginRecorder := httptest.NewRecorder()
	router.ServeHTTP(loginRecorder, httptest.NewRequest(http.MethodPost, "/api/auth/password/login", strings.NewReader(`{"login":"local-admin","password":"correct-password"}`)))
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginRecorder.Code, loginRecorder.Body.String())
	}
	var login AuthResult
	if err := json.Unmarshal(loginRecorder.Body.Bytes(), &login); err != nil {
		t.Fatalf("parse login response: %v", err)
	}
	if login.User.ID != register.User.ID || login.Workspaces[0].ID != register.Workspaces[0].ID {
		t.Fatalf("login should return existing user/workspace, got user=%+v workspaces=%+v", login.User, login.Workspaces)
	}

	duplicateRecorder := httptest.NewRecorder()
	router.ServeHTTP(duplicateRecorder, httptest.NewRequest(http.MethodPost, "/api/auth/password/register", strings.NewReader(registerBody)))
	if duplicateRecorder.Code != http.StatusConflict {
		t.Fatalf("duplicate register status=%d body=%s", duplicateRecorder.Code, duplicateRecorder.Body.String())
	}

	badLoginRecorder := httptest.NewRecorder()
	router.ServeHTTP(badLoginRecorder, httptest.NewRequest(http.MethodPost, "/api/auth/password/login", strings.NewReader(`{"login":"local-admin","password":"wrong-password"}`)))
	if badLoginRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("bad password status=%d body=%s", badLoginRecorder.Code, badLoginRecorder.Body.String())
	}

	missingLoginRecorder := httptest.NewRecorder()
	router.ServeHTTP(missingLoginRecorder, httptest.NewRequest(http.MethodPost, "/api/auth/password/login", strings.NewReader(`{"login":"missing-admin","password":"correct-password"}`)))
	if missingLoginRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("missing login status=%d body=%s", missingLoginRecorder.Code, missingLoginRecorder.Body.String())
	}
}

func TestBootstrapAdminCreatesDefaultAdminAccount(t *testing.T) {
	store := NewMemoryStore()
	bootstrapUser, workspaces, created, err := store.EnsureBootstrapAdmin(context.Background(), PasswordAuthInput{
		Login:    "platform-admin",
		Name:     "Platform Admin",
		Password: "correct-password",
	})
	if err != nil {
		t.Fatalf("ensure bootstrap admin: %v", err)
	}
	if !created {
		t.Fatalf("expected bootstrap admin to be created")
	}
	if bootstrapUser.ID == "" || len(workspaces) != 1 || workspaces[0].Kind != "personal" {
		t.Fatalf("unexpected bootstrap admin result: user=%+v workspaces=%+v", bootstrapUser, workspaces)
	}
	if _, _, createdAgain, err := store.EnsureBootstrapAdmin(context.Background(), PasswordAuthInput{
		Login:    "platform-admin",
		Name:     "Platform Admin",
		Password: "different-password",
	}); err != nil {
		t.Fatalf("ensure existing bootstrap admin: %v", err)
	} else if createdAgain {
		t.Fatalf("existing bootstrap admin should not be recreated")
	}

	server := NewServer(Config{BootstrapAdminLogin: "platform-admin"}, store, fakeGitHubClient{})
	router := server.Routes()

	adminRecorder := httptest.NewRecorder()
	router.ServeHTTP(adminRecorder, httptest.NewRequest(http.MethodPost, "/api/auth/password/login", strings.NewReader(`{"login":"platform-admin","password":"correct-password"}`)))
	if adminRecorder.Code != http.StatusOK {
		t.Fatalf("admin login status=%d body=%s", adminRecorder.Code, adminRecorder.Body.String())
	}
	var admin AuthResult
	if err := json.Unmarshal(adminRecorder.Body.Bytes(), &admin); err != nil {
		t.Fatalf("parse admin response: %v", err)
	}
	adminTeamRecorder := httptest.NewRecorder()
	adminTeamReq := httptest.NewRequest(http.MethodPost, "/api/workspaces", strings.NewReader(`{"name":"Admin Team","kind":"team"}`))
	adminTeamReq.Header.Set("Authorization", "Bearer "+admin.Token)
	router.ServeHTTP(adminTeamRecorder, adminTeamReq)
	if adminTeamRecorder.Code != http.StatusCreated {
		t.Fatalf("admin create team status=%d body=%s", adminTeamRecorder.Code, adminTeamRecorder.Body.String())
	}
}

func TestOpenRegistrationDoesNotGrantTeamWorkspaceCreation(t *testing.T) {
	store := NewMemoryStore()
	server := NewServer(Config{ServerAdminLogins: []string{"platform-admin"}}, store, fakeGitHubClient{})
	router := server.Routes()

	adminRecorder := httptest.NewRecorder()
	router.ServeHTTP(adminRecorder, httptest.NewRequest(http.MethodPost, "/api/auth/password/register", strings.NewReader(`{"login":"platform-admin","name":"Platform Admin","password":"correct-password"}`)))
	if adminRecorder.Code != http.StatusCreated {
		t.Fatalf("admin register status=%d body=%s", adminRecorder.Code, adminRecorder.Body.String())
	}
	var admin AuthResult
	if err := json.Unmarshal(adminRecorder.Body.Bytes(), &admin); err != nil {
		t.Fatalf("parse admin response: %v", err)
	}
	if !admin.IsServerAdmin {
		t.Fatalf("expected configured admin login to be server admin")
	}
	emailSpoofRecorder := httptest.NewRecorder()
	router.ServeHTTP(emailSpoofRecorder, httptest.NewRequest(http.MethodPost, "/api/auth/password/register", strings.NewReader(`{"login":"email-spoofer","name":"Email Spoofer","email":"platform-admin@example.com","password":"correct-password"}`)))
	if emailSpoofRecorder.Code != http.StatusCreated {
		t.Fatalf("email spoof register status=%d body=%s", emailSpoofRecorder.Code, emailSpoofRecorder.Body.String())
	}
	var emailSpoof AuthResult
	if err := json.Unmarshal(emailSpoofRecorder.Body.Bytes(), &emailSpoof); err != nil {
		t.Fatalf("parse email spoof response: %v", err)
	}
	if emailSpoof.IsServerAdmin {
		t.Fatalf("unverified email must not grant server admin")
	}

	userRecorder := httptest.NewRecorder()
	router.ServeHTTP(userRecorder, httptest.NewRequest(http.MethodPost, "/api/auth/password/register", strings.NewReader(`{"login":"tester","name":"platform-admin","password":"correct-password"}`)))
	if userRecorder.Code != http.StatusCreated {
		t.Fatalf("user register status=%d body=%s", userRecorder.Code, userRecorder.Body.String())
	}
	var user AuthResult
	if err := json.Unmarshal(userRecorder.Body.Bytes(), &user); err != nil {
		t.Fatalf("parse user response: %v", err)
	}
	if user.IsServerAdmin {
		t.Fatalf("ordinary registered user should not be server admin")
	}
	if len(user.Workspaces) != 1 || user.Workspaces[0].Kind != "personal" {
		t.Fatalf("ordinary user should only get a personal workspace, got %+v", user.Workspaces)
	}
	userTeamRecorder := httptest.NewRecorder()
	userTeamReq := httptest.NewRequest(http.MethodPost, "/api/workspaces", strings.NewReader(`{"name":"Tester Team","kind":"team"}`))
	userTeamReq.Header.Set("Authorization", "Bearer "+user.Token)
	router.ServeHTTP(userTeamRecorder, userTeamReq)
	if userTeamRecorder.Code != http.StatusForbidden {
		t.Fatalf("ordinary user team creation should be forbidden, status=%d body=%s", userTeamRecorder.Code, userTeamRecorder.Body.String())
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

func TestIssueAttachmentRequiresWorkspaceAuthorization(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "attachment-owner",
		Login:          "attachment-owner",
		Name:           "Attachment Owner",
		Email:          "attachment-owner@example.com",
	})
	if err != nil {
		t.Fatalf("upsert owner: %v", err)
	}
	token, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	store.attachments["attachment-1"] = IssueAttachment{
		ID:          "attachment-1",
		WorkspaceID: workspaceID,
		Filename:    "diagram.png",
		ContentType: "image/png",
		SizeBytes:   4,
		Content:     []byte{0x89, 0x50, 0x4e, 0x47},
	}

	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	unauthorizedRecorder := httptest.NewRecorder()
	router.ServeHTTP(unauthorizedRecorder, httptest.NewRequest(http.MethodGet, "/api/attachments/attachment-1", nil))
	if unauthorizedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized attachment status=%d body=%s", unauthorizedRecorder.Code, unauthorizedRecorder.Body.String())
	}

	authorizedRecorder := httptest.NewRecorder()
	authorizedReq := httptest.NewRequest(http.MethodGet, "/api/attachments/attachment-1", nil)
	authorizedReq.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(authorizedRecorder, authorizedReq)
	if authorizedRecorder.Code != http.StatusOK {
		t.Fatalf("authorized attachment status=%d body=%s", authorizedRecorder.Code, authorizedRecorder.Body.String())
	}
	if contentType := authorizedRecorder.Header().Get("Content-Type"); contentType != "image/png" {
		t.Fatalf("expected image/png content-type, got %q", contentType)
	}
	if body := authorizedRecorder.Body.Bytes(); string(body) != string([]byte{0x89, 0x50, 0x4e, 0x47}) {
		t.Fatalf("unexpected attachment body: %v", body)
	}
}

func TestIssueAttachmentUploadStoresServerOwnedBlob(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "attachment-uploader",
		Login:          "attachment-uploader",
		Name:           "Attachment Uploader",
		Email:          "attachment-uploader@example.com",
	})
	if err != nil {
		t.Fatalf("upsert uploader: %v", err)
	}
	token, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	issueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{Body: "Attach an image"})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "diagram.png")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	pngBytes := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if _, err := part.Write(pngBytes); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()
	uploadRecorder := httptest.NewRecorder()
	uploadReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/issues/"+issueID+"/attachments", body)
	uploadReq.Header.Set("Authorization", "Bearer "+token)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(uploadRecorder, uploadReq)
	if uploadRecorder.Code != http.StatusCreated {
		t.Fatalf("upload attachment status=%d body=%s", uploadRecorder.Code, uploadRecorder.Body.String())
	}
	var attachment IssueAttachment
	if err := json.Unmarshal(uploadRecorder.Body.Bytes(), &attachment); err != nil {
		t.Fatalf("parse upload response: %v", err)
	}
	if attachment.ID == "" || attachment.WorkspaceID != workspaceID || attachment.IssueID != issueID || attachment.StorageKey != "/api/attachments/"+attachment.ID || attachment.ContentType != "image/png" {
		t.Fatalf("unexpected upload response: %+v", attachment)
	}

	getRecorder := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, "/api/attachments/"+attachment.ID, nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(getRecorder, getReq)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get uploaded attachment status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}
	if string(getRecorder.Body.Bytes()) != string(pngBytes) {
		t.Fatalf("unexpected uploaded attachment bytes: %v", getRecorder.Body.Bytes())
	}
}

func TestWorkspaceInvitationFlow(t *testing.T) {
	store := NewMemoryStore()
	owner, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "owner-1",
		Login:          "owner",
		Name:           "Owner User",
		Email:          "owner@example.com",
	})
	if err != nil {
		t.Fatalf("upsert owner: %v", err)
	}
	ownerToken, _, err := store.CreateAuthSession(context.Background(), owner.ID, time.Hour)
	if err != nil {
		t.Fatalf("create owner session: %v", err)
	}
	personalWorkspaceID := workspaces[0].ID
	teamWorkspace, workspaces, err := store.CreateWorkspace(context.Background(), owner.ID, CreateWorkspaceInput{Name: "Owner Team", Kind: "team"})
	if err != nil {
		t.Fatalf("create team workspace: %v", err)
	}
	workspaceID := teamWorkspace.ID

	member, _, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "member-1",
		Login:          "member",
		Name:           "Member User",
		Email:          "member@example.com",
	})
	if err != nil {
		t.Fatalf("upsert member: %v", err)
	}
	memberToken, _, err := store.CreateAuthSession(context.Background(), member.ID, time.Hour)
	if err != nil {
		t.Fatalf("create member session: %v", err)
	}

	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	personalInviteRecorder := httptest.NewRecorder()
	personalInviteReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+personalWorkspaceID+"/invitations", strings.NewReader(`{"email":"member@example.com","role":"member","expiresInHours":24}`))
	personalInviteReq.Header.Set("Authorization", "Bearer "+ownerToken)
	router.ServeHTTP(personalInviteRecorder, personalInviteReq)
	if personalInviteRecorder.Code != http.StatusForbidden {
		t.Fatalf("personal workspace invitation should be forbidden, status=%d body=%s", personalInviteRecorder.Code, personalInviteRecorder.Body.String())
	}

	createRecorder := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/invitations", strings.NewReader(`{"email":"member@example.com","role":"member","expiresInHours":24}`))
	createReq.Header.Set("Authorization", "Bearer "+ownerToken)
	router.ServeHTTP(createRecorder, createReq)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create invitation status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	var inviteResult WorkspaceInvitationResult
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &inviteResult); err != nil {
		t.Fatalf("parse invitation: %v", err)
	}
	if !strings.HasPrefix(inviteResult.Token, "msi_") || inviteResult.Invitation.WorkspaceID != workspaceID || inviteResult.Invitation.Role != "member" {
		t.Fatalf("unexpected invite result: %+v", inviteResult)
	}

	listRecorder := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/invitations", nil)
	listReq.Header.Set("Authorization", "Bearer "+ownerToken)
	router.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list invitation status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var invitations []WorkspaceInvitation
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &invitations); err != nil {
		t.Fatalf("parse invitations: %v", err)
	}
	if len(invitations) != 1 || invitations[0].ID != inviteResult.Invitation.ID || invitations[0].AcceptedAt != "" {
		t.Fatalf("unexpected invitations: %+v", invitations)
	}

	previewRecorder := httptest.NewRecorder()
	previewReq := httptest.NewRequest(http.MethodGet, "/api/workspace-invitations/preview?token="+inviteResult.Token, nil)
	router.ServeHTTP(previewRecorder, previewReq)
	if previewRecorder.Code != http.StatusOK {
		t.Fatalf("preview invitation status=%d body=%s", previewRecorder.Code, previewRecorder.Body.String())
	}
	var preview WorkspaceInvitationPreview
	if err := json.Unmarshal(previewRecorder.Body.Bytes(), &preview); err != nil {
		t.Fatalf("parse invitation preview: %v", err)
	}
	if preview.WorkspaceName != "Owner Team" || preview.Role != "member" || preview.Status != "pending" || preview.InvitedByName != owner.Name {
		t.Fatalf("unexpected invitation preview: %+v", preview)
	}

	acceptRecorder := httptest.NewRecorder()
	acceptReq := httptest.NewRequest(http.MethodPost, "/api/workspace-invitations/accept", strings.NewReader(`{"token":"`+inviteResult.Token+`"}`))
	acceptReq.Header.Set("Authorization", "Bearer "+memberToken)
	router.ServeHTTP(acceptRecorder, acceptReq)
	if acceptRecorder.Code != http.StatusOK {
		t.Fatalf("accept invitation status=%d body=%s", acceptRecorder.Code, acceptRecorder.Body.String())
	}
	var acceptResult AcceptWorkspaceInvitationResult
	if err := json.Unmarshal(acceptRecorder.Body.Bytes(), &acceptResult); err != nil {
		t.Fatalf("parse accept result: %v", err)
	}
	if acceptResult.Workspace.ID != workspaceID || acceptResult.Workspace.Role != "member" || len(acceptResult.Workspaces) != 2 {
		t.Fatalf("unexpected accept result: %+v", acceptResult)
	}

	membersRecorder := httptest.NewRecorder()
	membersReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/members", nil)
	membersReq.Header.Set("Authorization", "Bearer "+ownerToken)
	router.ServeHTTP(membersRecorder, membersReq)
	if membersRecorder.Code != http.StatusOK {
		t.Fatalf("list members status=%d body=%s", membersRecorder.Code, membersRecorder.Body.String())
	}
	var members []WorkspaceMember
	if err := json.Unmarshal(membersRecorder.Body.Bytes(), &members); err != nil {
		t.Fatalf("parse members: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected owner and invited member, got %+v", members)
	}
	if members[0].Role != "owner" || members[1].UserID != member.ID || members[1].Role != "member" {
		t.Fatalf("unexpected members: %+v", members)
	}

	workspaceListRecorder := httptest.NewRecorder()
	workspaceListReq := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	workspaceListReq.Header.Set("Authorization", "Bearer "+memberToken)
	router.ServeHTTP(workspaceListRecorder, workspaceListReq)
	if workspaceListRecorder.Code != http.StatusOK {
		t.Fatalf("member workspaces status=%d body=%s", workspaceListRecorder.Code, workspaceListRecorder.Body.String())
	}
	var memberWorkspaces []Workspace
	if err := json.Unmarshal(workspaceListRecorder.Body.Bytes(), &memberWorkspaces); err != nil {
		t.Fatalf("parse member workspaces: %v", err)
	}
	foundShared := false
	for _, workspace := range memberWorkspaces {
		if workspace.ID == workspaceID && workspace.Role == "member" {
			foundShared = true
			break
		}
	}
	if !foundShared {
		t.Fatalf("expected member to see accepted workspace, got %+v", memberWorkspaces)
	}

	reuseRecorder := httptest.NewRecorder()
	reuseReq := httptest.NewRequest(http.MethodPost, "/api/workspace-invitations/accept", strings.NewReader(`{"token":"`+inviteResult.Token+`"}`))
	reuseReq.Header.Set("Authorization", "Bearer "+memberToken)
	router.ServeHTTP(reuseRecorder, reuseReq)
	if reuseRecorder.Code != http.StatusNotFound {
		t.Fatalf("accepted invitation should not be reusable, status=%d body=%s", reuseRecorder.Code, reuseRecorder.Body.String())
	}

	acceptedPreviewRecorder := httptest.NewRecorder()
	acceptedPreviewReq := httptest.NewRequest(http.MethodGet, "/api/workspace-invitations/preview?token="+inviteResult.Token, nil)
	router.ServeHTTP(acceptedPreviewRecorder, acceptedPreviewReq)
	if acceptedPreviewRecorder.Code != http.StatusOK {
		t.Fatalf("accepted preview status=%d body=%s", acceptedPreviewRecorder.Code, acceptedPreviewRecorder.Body.String())
	}
	if err := json.Unmarshal(acceptedPreviewRecorder.Body.Bytes(), &preview); err != nil {
		t.Fatalf("parse accepted invitation preview: %v", err)
	}
	if preview.Status != "accepted" {
		t.Fatalf("expected accepted preview, got %+v", preview)
	}
}

func TestTeamWorkspaceIdentityFlow(t *testing.T) {
	store := NewMemoryStore()
	owner, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "rename-owner",
		Login:          "rename-owner",
		Name:           "Rename Owner",
		Email:          "rename-owner@example.com",
	})
	if err != nil {
		t.Fatalf("upsert owner: %v", err)
	}
	ownerToken, _, err := store.CreateAuthSession(context.Background(), owner.ID, time.Hour)
	if err != nil {
		t.Fatalf("create owner session: %v", err)
	}
	personalWorkspaceID := workspaces[0].ID
	teamWorkspace, _, err := store.CreateWorkspace(context.Background(), owner.ID, CreateWorkspaceInput{Name: "Old Team", Kind: "team"})
	if err != nil {
		t.Fatalf("create team workspace: %v", err)
	}
	workspaceID := teamWorkspace.ID

	member, _, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "rename-member",
		Login:          "rename-member",
		Name:           "Rename Member",
		Email:          "rename-member@example.com",
	})
	if err != nil {
		t.Fatalf("upsert member: %v", err)
	}
	memberToken, _, err := store.CreateAuthSession(context.Background(), member.ID, time.Hour)
	if err != nil {
		t.Fatalf("create member session: %v", err)
	}

	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	inviteRecorder := httptest.NewRecorder()
	inviteReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/invitations", strings.NewReader(`{"email":"rename-member@example.com","role":"member","expiresInHours":24}`))
	inviteReq.Header.Set("Authorization", "Bearer "+ownerToken)
	router.ServeHTTP(inviteRecorder, inviteReq)
	if inviteRecorder.Code != http.StatusCreated {
		t.Fatalf("create invitation status=%d body=%s", inviteRecorder.Code, inviteRecorder.Body.String())
	}
	var invite WorkspaceInvitationResult
	if err := json.Unmarshal(inviteRecorder.Body.Bytes(), &invite); err != nil {
		t.Fatalf("parse invitation: %v", err)
	}
	acceptRecorder := httptest.NewRecorder()
	acceptReq := httptest.NewRequest(http.MethodPost, "/api/workspace-invitations/accept", strings.NewReader(`{"token":"`+invite.Token+`"}`))
	acceptReq.Header.Set("Authorization", "Bearer "+memberToken)
	router.ServeHTTP(acceptRecorder, acceptReq)
	if acceptRecorder.Code != http.StatusOK {
		t.Fatalf("accept invitation status=%d body=%s", acceptRecorder.Code, acceptRecorder.Body.String())
	}

	memberUpdateRecorder := httptest.NewRecorder()
	memberUpdateReq := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+workspaceID, strings.NewReader(`{"name":"Member Rename","icon":"MR","description":"Member edit."}`))
	memberUpdateReq.Header.Set("Authorization", "Bearer "+memberToken)
	router.ServeHTTP(memberUpdateRecorder, memberUpdateReq)
	if memberUpdateRecorder.Code != http.StatusForbidden {
		t.Fatalf("workspace member identity update should be forbidden, status=%d body=%s", memberUpdateRecorder.Code, memberUpdateRecorder.Body.String())
	}

	personalUpdateRecorder := httptest.NewRecorder()
	personalUpdateReq := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+personalWorkspaceID, strings.NewReader(`{"name":"Personal Rename","icon":"P","description":"Personal edit."}`))
	personalUpdateReq.Header.Set("Authorization", "Bearer "+ownerToken)
	router.ServeHTTP(personalUpdateRecorder, personalUpdateReq)
	if personalUpdateRecorder.Code != http.StatusForbidden {
		t.Fatalf("personal workspace identity update through team endpoint should be forbidden, status=%d body=%s", personalUpdateRecorder.Code, personalUpdateRecorder.Body.String())
	}

	invalidIconRecorder := httptest.NewRecorder()
	invalidIconReq := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+workspaceID, strings.NewReader(`{"name":"Renamed Team","icon":"12345678901234567","description":"Engineering workspace."}`))
	invalidIconReq.Header.Set("Authorization", "Bearer "+ownerToken)
	router.ServeHTTP(invalidIconRecorder, invalidIconReq)
	if invalidIconRecorder.Code != http.StatusBadRequest {
		t.Fatalf("long workspace icon should be rejected, status=%d body=%s", invalidIconRecorder.Code, invalidIconRecorder.Body.String())
	}

	longDescription := strings.Repeat("d", 281)
	invalidDescriptionRecorder := httptest.NewRecorder()
	invalidDescriptionReq := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+workspaceID, strings.NewReader(`{"name":"Renamed Team","icon":"RT","description":"`+longDescription+`"}`))
	invalidDescriptionReq.Header.Set("Authorization", "Bearer "+ownerToken)
	router.ServeHTTP(invalidDescriptionRecorder, invalidDescriptionReq)
	if invalidDescriptionRecorder.Code != http.StatusBadRequest {
		t.Fatalf("long workspace description should be rejected, status=%d body=%s", invalidDescriptionRecorder.Code, invalidDescriptionRecorder.Body.String())
	}

	updateRecorder := httptest.NewRecorder()
	updateReq := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+workspaceID, strings.NewReader(`{"name":"Renamed Team","icon":"RT","description":"Engineering workspace for customer-facing agent work."}`))
	updateReq.Header.Set("Authorization", "Bearer "+ownerToken)
	router.ServeHTTP(updateRecorder, updateReq)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update team workspace identity status=%d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	var updateResult UpdateWorkspaceResult
	if err := json.Unmarshal(updateRecorder.Body.Bytes(), &updateResult); err != nil {
		t.Fatalf("parse update result: %v", err)
	}
	if updateResult.Workspace.ID != workspaceID ||
		updateResult.Workspace.Name != "Renamed Team" ||
		updateResult.Workspace.Icon != "RT" ||
		updateResult.Workspace.Description != "Engineering workspace for customer-facing agent work." ||
		updateResult.Workspace.Role != "owner" {
		t.Fatalf("unexpected workspace identity result: %+v", updateResult.Workspace)
	}
	foundUpdated := false
	for _, workspace := range updateResult.Workspaces {
		if workspace.ID == workspaceID &&
			workspace.Name == "Renamed Team" &&
			workspace.Icon == "RT" &&
			workspace.Description == "Engineering workspace for customer-facing agent work." &&
			workspace.Role == "owner" {
			foundUpdated = true
			break
		}
	}
	if !foundUpdated {
		t.Fatalf("expected updated workspace in owner list, got %+v", updateResult.Workspaces)
	}

	memberListRecorder := httptest.NewRecorder()
	memberListReq := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	memberListReq.Header.Set("Authorization", "Bearer "+memberToken)
	router.ServeHTTP(memberListRecorder, memberListReq)
	if memberListRecorder.Code != http.StatusOK {
		t.Fatalf("member workspace list status=%d body=%s", memberListRecorder.Code, memberListRecorder.Body.String())
	}
	var memberWorkspaces []Workspace
	if err := json.Unmarshal(memberListRecorder.Body.Bytes(), &memberWorkspaces); err != nil {
		t.Fatalf("parse member workspaces: %v", err)
	}
	foundMemberRenamed := false
	for _, workspace := range memberWorkspaces {
		if workspace.ID == workspaceID &&
			workspace.Name == "Renamed Team" &&
			workspace.Icon == "RT" &&
			workspace.Description == "Engineering workspace for customer-facing agent work." &&
			workspace.Role == "member" {
			foundMemberRenamed = true
			break
		}
	}
	if !foundMemberRenamed {
		t.Fatalf("expected updated workspace in member list, got %+v", memberWorkspaces)
	}
}

func TestWorkspaceMembersCannotMutateRuntimeConfiguration(t *testing.T) {
	store := NewMemoryStore()
	owner, _, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "runtime-config-owner",
		Login:          "runtime-config-owner",
		Name:           "Runtime Config Owner",
		Email:          "runtime-config-owner@example.com",
	})
	if err != nil {
		t.Fatalf("upsert owner: %v", err)
	}
	ownerToken, _, err := store.CreateAuthSession(context.Background(), owner.ID, time.Hour)
	if err != nil {
		t.Fatalf("create owner session: %v", err)
	}
	teamWorkspace, _, err := store.CreateWorkspace(context.Background(), owner.ID, CreateWorkspaceInput{Name: "Runtime Config Team", Kind: "team"})
	if err != nil {
		t.Fatalf("create team workspace: %v", err)
	}
	workspaceID := teamWorkspace.ID
	member, _, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "runtime-config-member",
		Login:          "runtime-config-member",
		Name:           "Runtime Config Member",
		Email:          "runtime-config-member@example.com",
	})
	if err != nil {
		t.Fatalf("upsert member: %v", err)
	}
	memberToken, _, err := store.CreateAuthSession(context.Background(), member.ID, time.Hour)
	if err != nil {
		t.Fatalf("create member session: %v", err)
	}

	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()
	inviteRecorder := httptest.NewRecorder()
	inviteReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/invitations", strings.NewReader(`{"email":"runtime-config-member@example.com","role":"member","expiresInHours":24}`))
	inviteReq.Header.Set("Authorization", "Bearer "+ownerToken)
	router.ServeHTTP(inviteRecorder, inviteReq)
	if inviteRecorder.Code != http.StatusCreated {
		t.Fatalf("create invitation status=%d body=%s", inviteRecorder.Code, inviteRecorder.Body.String())
	}
	var invite WorkspaceInvitationResult
	if err := json.Unmarshal(inviteRecorder.Body.Bytes(), &invite); err != nil {
		t.Fatalf("parse invitation: %v", err)
	}
	acceptRecorder := httptest.NewRecorder()
	acceptReq := httptest.NewRequest(http.MethodPost, "/api/workspace-invitations/accept", strings.NewReader(`{"token":"`+invite.Token+`"}`))
	acceptReq.Header.Set("Authorization", "Bearer "+memberToken)
	router.ServeHTTP(acceptRecorder, acceptReq)
	if acceptRecorder.Code != http.StatusOK {
		t.Fatalf("accept invitation status=%d body=%s", acceptRecorder.Code, acceptRecorder.Body.String())
	}

	clusterRecorder := httptest.NewRecorder()
	clusterReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/clusters", strings.NewReader(`{"name":"owner cluster","kubeconfigPath":"/tmp/kubeconfig","imageRegistryPrefix":"registry.example.com/team","exposureMode":"nodeport"}`))
	clusterReq.Header.Set("Authorization", "Bearer "+ownerToken)
	router.ServeHTTP(clusterRecorder, clusterReq)
	if clusterRecorder.Code != http.StatusCreated {
		t.Fatalf("owner create cluster status=%d body=%s", clusterRecorder.Code, clusterRecorder.Body.String())
	}
	var cluster Cluster
	if err := json.Unmarshal(clusterRecorder.Body.Bytes(), &cluster); err != nil {
		t.Fatalf("parse cluster: %v", err)
	}

	for _, item := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "workspace settings",
			method: http.MethodPut,
			path:   "/api/workspaces/" + workspaceID + "/workspace/settings",
			body:   `{"autoCreateDraftPr":true}`,
		},
		{
			name:   "create raw runtime task",
			method: http.MethodPost,
			path:   "/api/workspaces/" + workspaceID + "/runtime-tasks",
			body:   `{"kind":"noop","runtimeMode":"team","requiredCapabilities":{"protocolSmoke":true},"payload":{"prompt":"smoke"}}`,
		},
		{
			name:   "create cluster",
			method: http.MethodPost,
			path:   "/api/workspaces/" + workspaceID + "/clusters",
			body:   `{"name":"member cluster","kubeconfigPath":"/tmp/member","imageRegistryPrefix":"registry.example.com/member","exposureMode":"nodeport"}`,
		},
		{
			name:   "update cluster",
			method: http.MethodPut,
			path:   "/api/workspaces/" + workspaceID + "/clusters/" + cluster.ID,
			body:   `{"name":"mutated cluster","kubeconfigPath":"/tmp/kubeconfig","imageRegistryPrefix":"registry.example.com/member","exposureMode":"nodeport"}`,
		},
		{
			name:   "check cluster",
			method: http.MethodPost,
			path:   "/api/workspaces/" + workspaceID + "/clusters/" + cluster.ID + "/check",
		},
		{
			name:   "delete cluster",
			method: http.MethodDelete,
			path:   "/api/workspaces/" + workspaceID + "/clusters/" + cluster.ID,
		},
		{
			name:   "discover default kubeconfigs",
			method: http.MethodGet,
			path:   "/api/workspaces/" + workspaceID + "/clusters/discover-defaults",
		},
		{
			name:   "import kubeconfigs",
			method: http.MethodPost,
			path:   "/api/workspaces/" + workspaceID + "/clusters/import",
			body:   `{"paths":["/tmp/kubeconfig"]}`,
		},
	} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(item.method, item.path, strings.NewReader(item.body))
		req.Header.Set("Authorization", "Bearer "+memberToken)
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s should be forbidden for workspace member, status=%d body=%s", item.name, recorder.Code, recorder.Body.String())
		}
	}
}

func TestClusterCheckRefreshesKubeconfigStatus(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "cluster-check-owner",
		Login:          "cluster-check-owner",
		Name:           "Cluster Check Owner",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	createRecorder := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/clusters", strings.NewReader(`{
		"name":"unreachable-k8s",
		"kubeconfigPath":"/tmp/mspace-missing-kubeconfig",
		"imageRegistryPrefix":"registry.example.com/team",
		"exposureMode":"nodeport"
	}`))
	createReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createRecorder, createReq)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create cluster status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	var created Cluster
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse cluster: %v", err)
	}
	if created.Status != "unreachable" || created.LastCheckedAt == "" {
		t.Fatalf("expected created cluster to be checked as unreachable, got %+v", created)
	}

	checkRecorder := httptest.NewRecorder()
	checkReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/clusters/"+created.ID+"/check", nil)
	checkReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(checkRecorder, checkReq)
	if checkRecorder.Code != http.StatusOK {
		t.Fatalf("check cluster status=%d body=%s", checkRecorder.Code, checkRecorder.Body.String())
	}
	var checked Cluster
	if err := json.Unmarshal(checkRecorder.Body.Bytes(), &checked); err != nil {
		t.Fatalf("parse checked cluster: %v", err)
	}
	if checked.Status != "unreachable" || checked.LastCheckedAt == "" {
		t.Fatalf("expected checked cluster to stay unreachable with checked time, got %+v", checked)
	}
}

func TestWorkspaceCollaborationIssueIsolation(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "collab-owner",
		Login:          "collab-owner",
		Name:           "Collab Owner",
		Email:          "collab-owner@example.com",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	personalWorkspaceID := workspaces[0].ID
	teamWorkspace, _, err := store.CreateWorkspace(context.Background(), user.ID, CreateWorkspaceInput{Name: "Collab Team", Kind: "team"})
	if err != nil {
		t.Fatalf("create team workspace: %v", err)
	}

	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	createPersonalProjectRecorder := httptest.NewRecorder()
	createPersonalProjectReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+personalWorkspaceID+"/projects", strings.NewReader(`{"name":"mspace","sourceType":"local","repoPath":"/Users/mlhiter/personal-projects/mspace","defaultBranch":"main"}`))
	createPersonalProjectReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createPersonalProjectRecorder, createPersonalProjectReq)
	if createPersonalProjectRecorder.Code != http.StatusCreated {
		t.Fatalf("create personal project status=%d body=%s", createPersonalProjectRecorder.Code, createPersonalProjectRecorder.Body.String())
	}
	var personalProject Project
	if err := json.Unmarshal(createPersonalProjectRecorder.Body.Bytes(), &personalProject); err != nil {
		t.Fatalf("parse personal project: %v", err)
	}
	if personalProject.WorkspaceID != personalWorkspaceID || personalProject.Name != "mspace" {
		t.Fatalf("unexpected personal project: %+v", personalProject)
	}

	createTeamLocalProjectRecorder := httptest.NewRecorder()
	createTeamLocalProjectReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+teamWorkspace.ID+"/projects", strings.NewReader(`{"name":"local-team","sourceType":"local","repoPath":"/Users/mlhiter/personal-projects/mspace","defaultBranch":"main"}`))
	createTeamLocalProjectReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createTeamLocalProjectRecorder, createTeamLocalProjectReq)
	if createTeamLocalProjectRecorder.Code != http.StatusBadRequest {
		t.Fatalf("team local project should be rejected, status=%d body=%s", createTeamLocalProjectRecorder.Code, createTeamLocalProjectRecorder.Body.String())
	}

	createTeamGitHubProjectRecorder := httptest.NewRecorder()
	createTeamGitHubProjectReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+teamWorkspace.ID+"/projects", strings.NewReader(`{"name":"team-repo","sourceType":"github","repoUrl":"https://github.com/mlhiter/team-repo.git","defaultBranch":"main"}`))
	createTeamGitHubProjectReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createTeamGitHubProjectRecorder, createTeamGitHubProjectReq)
	if createTeamGitHubProjectRecorder.Code != http.StatusCreated {
		t.Fatalf("team github project should be accepted, status=%d body=%s", createTeamGitHubProjectRecorder.Code, createTeamGitHubProjectRecorder.Body.String())
	}

	createIssueRecorder := httptest.NewRecorder()
	createIssueReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+personalWorkspaceID+"/issues", strings.NewReader(`{"projectId":"`+personalProject.ID+`","body":"Move issues to server PG\n\n- [ ] Keep worker workdirs isolated","labelKeys":["type:feat","priority:p1"]}`))
	createIssueReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createIssueRecorder, createIssueReq)
	if createIssueRecorder.Code != http.StatusCreated {
		t.Fatalf("create issue status=%d body=%s", createIssueRecorder.Code, createIssueRecorder.Body.String())
	}
	var createIssueResult struct {
		IssueID string `json:"issueId"`
	}
	if err := json.Unmarshal(createIssueRecorder.Body.Bytes(), &createIssueResult); err != nil {
		t.Fatalf("parse create issue: %v", err)
	}
	if createIssueResult.IssueID == "" {
		t.Fatalf("expected issue id, got %+v", createIssueResult)
	}

	listPersonalRecorder := httptest.NewRecorder()
	listPersonalReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+personalWorkspaceID+"/issues", nil)
	listPersonalReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(listPersonalRecorder, listPersonalReq)
	if listPersonalRecorder.Code != http.StatusOK {
		t.Fatalf("list personal issues status=%d body=%s", listPersonalRecorder.Code, listPersonalRecorder.Body.String())
	}
	var personalIssues []IssueListItem
	if err := json.Unmarshal(listPersonalRecorder.Body.Bytes(), &personalIssues); err != nil {
		t.Fatalf("parse personal issues: %v", err)
	}
	if len(personalIssues) != 1 || personalIssues[0].ID != createIssueResult.IssueID || personalIssues[0].ChildIssueCount != 1 {
		t.Fatalf("unexpected personal issues: %+v", personalIssues)
	}

	listTeamRecorder := httptest.NewRecorder()
	listTeamReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+teamWorkspace.ID+"/issues", nil)
	listTeamReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(listTeamRecorder, listTeamReq)
	if listTeamRecorder.Code != http.StatusOK {
		t.Fatalf("list team issues status=%d body=%s", listTeamRecorder.Code, listTeamRecorder.Body.String())
	}
	var teamIssues []IssueListItem
	if err := json.Unmarshal(listTeamRecorder.Body.Bytes(), &teamIssues); err != nil {
		t.Fatalf("parse team issues: %v", err)
	}
	if len(teamIssues) != 0 {
		t.Fatalf("expected team workspace isolation, got %+v", teamIssues)
	}

	addCommentRecorder := httptest.NewRecorder()
	addCommentReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+personalWorkspaceID+"/issues/"+createIssueResult.IssueID+"/comments", strings.NewReader(`{"body":"Server comment"}`))
	addCommentReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(addCommentRecorder, addCommentReq)
	if addCommentRecorder.Code != http.StatusCreated {
		t.Fatalf("add comment status=%d body=%s", addCommentRecorder.Code, addCommentRecorder.Body.String())
	}
	var commentResult struct {
		CommentID string `json:"commentId"`
	}
	if err := json.Unmarshal(addCommentRecorder.Body.Bytes(), &commentResult); err != nil {
		t.Fatalf("parse comment result: %v", err)
	}
	if commentResult.CommentID == "" {
		t.Fatalf("expected comment id, got %+v", commentResult)
	}

	reactionRecorder := httptest.NewRecorder()
	reactionReq := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+personalWorkspaceID+"/issues/"+createIssueResult.IssueID+"/comments/"+commentResult.CommentID+"/reactions/rocket", nil)
	reactionReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(reactionRecorder, reactionReq)
	if reactionRecorder.Code != http.StatusOK {
		t.Fatalf("set reaction status=%d body=%s", reactionRecorder.Code, reactionRecorder.Body.String())
	}

	detailRecorder := httptest.NewRecorder()
	detailReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+personalWorkspaceID+"/issues/"+createIssueResult.IssueID, nil)
	detailReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(detailRecorder, detailReq)
	if detailRecorder.Code != http.StatusOK {
		t.Fatalf("get issue status=%d body=%s", detailRecorder.Code, detailRecorder.Body.String())
	}
	var detail IssueDetail
	if err := json.Unmarshal(detailRecorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("parse issue detail: %v", err)
	}
	if detail.Issue.WorkspaceID != personalWorkspaceID || detail.Issue.Body != "Move issues to server PG" {
		t.Fatalf("unexpected issue detail: %+v", detail.Issue)
	}
	if len(detail.ChildIssues) != 1 || detail.ChildIssues[0].Title != "Keep worker workdirs isolated" {
		t.Fatalf("unexpected child issues: %+v", detail.ChildIssues)
	}
	if len(detail.Labels) != 2 {
		t.Fatalf("expected type and priority labels, got %+v", detail.Labels)
	}
	serverComment := findCommentByID(detail.Comments, commentResult.CommentID)
	if serverComment.ID == "" || serverComment.Body != "Server comment" || len(serverComment.Reactions) != 1 || serverComment.Reactions[0].Reaction != "rocket" {
		t.Fatalf("unexpected comments: %+v", detail.Comments)
	}

	noWorkerSessionRecorder := httptest.NewRecorder()
	noWorkerSessionReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+personalWorkspaceID+"/issues/"+createIssueResult.IssueID+"/sessions", strings.NewReader(`{"agentEngine":"codex","command":"@codex update the docs","triggerCommentId":"`+commentResult.CommentID+`"}`))
	noWorkerSessionReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(noWorkerSessionRecorder, noWorkerSessionReq)
	if noWorkerSessionRecorder.Code != http.StatusConflict || !strings.Contains(noWorkerSessionRecorder.Body.String(), "no active agent worker") {
		t.Fatalf("create agent session without worker status=%d body=%s", noWorkerSessionRecorder.Code, noWorkerSessionRecorder.Body.String())
	}

	personalTokenRecorder := httptest.NewRecorder()
	personalTokenReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+personalWorkspaceID+"/runtime-registration-tokens", strings.NewReader(`{"name":"personal worker","expiresInHours":12}`))
	personalTokenReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(personalTokenRecorder, personalTokenReq)
	if personalTokenRecorder.Code != http.StatusCreated {
		t.Fatalf("create personal runtime token status=%d body=%s", personalTokenRecorder.Code, personalTokenRecorder.Body.String())
	}
	var personalTokenResult RuntimeRegistrationTokenResult
	if err := json.Unmarshal(personalTokenRecorder.Body.Bytes(), &personalTokenResult); err != nil {
		t.Fatalf("parse personal runtime token: %v", err)
	}
	personalWorkerRecorder := httptest.NewRecorder()
	personalWorkerReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/register", strings.NewReader(`{"name":"personal-worker-1","storageId":"msws_personal_worker_1","mode":"personal","version":"0.1.0","capabilities":{"codex":true,"issueWorkingCopyV1":true},"labels":{"host":"local"}}`))
	personalWorkerReq.Header.Set("Authorization", "Bearer "+personalTokenResult.Token)
	router.ServeHTTP(personalWorkerRecorder, personalWorkerReq)
	if personalWorkerRecorder.Code != http.StatusCreated {
		t.Fatalf("register personal worker status=%d body=%s", personalWorkerRecorder.Code, personalWorkerRecorder.Body.String())
	}

	createSessionRecorder := httptest.NewRecorder()
	createSessionReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+personalWorkspaceID+"/issues/"+createIssueResult.IssueID+"/sessions", strings.NewReader(`{"agentEngine":"codex","command":"@codex update the docs","triggerCommentId":"`+commentResult.CommentID+`"}`))
	createSessionReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createSessionRecorder, createSessionReq)
	if createSessionRecorder.Code != http.StatusCreated {
		t.Fatalf("create agent session status=%d body=%s", createSessionRecorder.Code, createSessionRecorder.Body.String())
	}
	var session AgentSession
	if err := json.Unmarshal(createSessionRecorder.Body.Bytes(), &session); err != nil {
		t.Fatalf("parse agent session: %v", err)
	}
	if session.ID == "" || session.RuntimeTaskID == "" || session.RuntimeMode != "personal" || session.Status != "queued" || session.TriggerCommentID != commentResult.CommentID {
		t.Fatalf("unexpected agent session: %+v", session)
	}

	getSessionRecorder := httptest.NewRecorder()
	getSessionReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+personalWorkspaceID+"/sessions/"+session.ID, nil)
	getSessionReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(getSessionRecorder, getSessionReq)
	if getSessionRecorder.Code != http.StatusOK {
		t.Fatalf("get agent session status=%d body=%s", getSessionRecorder.Code, getSessionRecorder.Body.String())
	}
	var sessionDetail SessionDetail
	if err := json.Unmarshal(getSessionRecorder.Body.Bytes(), &sessionDetail); err != nil {
		t.Fatalf("parse session detail: %v", err)
	}
	if sessionDetail.Session.ID != session.ID || sessionDetail.Issue.ID != createIssueResult.IssueID || sessionDetail.Project.ID != personalProject.ID {
		t.Fatalf("unexpected session detail: %+v", sessionDetail)
	}

	cancelSessionRecorder := httptest.NewRecorder()
	cancelSessionReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+personalWorkspaceID+"/sessions/"+session.ID+"/cancel", strings.NewReader(`{"reason":"user stopped session"}`))
	cancelSessionReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(cancelSessionRecorder, cancelSessionReq)
	if cancelSessionRecorder.Code != http.StatusOK {
		t.Fatalf("cancel agent session status=%d body=%s", cancelSessionRecorder.Code, cancelSessionRecorder.Body.String())
	}
	var cancelledTask RuntimeTask
	if err := json.Unmarshal(cancelSessionRecorder.Body.Bytes(), &cancelledTask); err != nil {
		t.Fatalf("parse cancelled session task: %v", err)
	}
	if cancelledTask.ID != session.RuntimeTaskID || cancelledTask.Status != "cancelled" {
		t.Fatalf("unexpected cancelled session task: %+v", cancelledTask)
	}
}

func TestCreateWorkspaceIssueWithoutProject(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "issue-user-2",
		Login:          "issue-user-2",
		Name:           "Issue User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()
	registerTestRuntimeWorkerWithCapabilities(t, router, sessionToken, workspaceID, `{"codex":true,"issueWorkingCopyV1":true}`)

	createRecorder := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/issues", strings.NewReader(`{"body":"Capture a workspace-level issue before the repo is known"}`))
	createReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createRecorder, createReq)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create issue without project status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	var createResult struct {
		IssueID string `json:"issueId"`
	}
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &createResult); err != nil {
		t.Fatalf("parse create result: %v", err)
	}

	listRecorder := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/issues", nil)
	listReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list issues status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var issues []IssueListItem
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &issues); err != nil {
		t.Fatalf("parse issues: %v", err)
	}
	if len(issues) != 1 || issues[0].ProjectID != "" || issues[0].ProjectName != "" {
		t.Fatalf("expected workspace-level issue without project, got %+v", issues)
	}

	detailRecorder := httptest.NewRecorder()
	detailReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/issues/"+createResult.IssueID, nil)
	detailReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(detailRecorder, detailReq)
	if detailRecorder.Code != http.StatusOK {
		t.Fatalf("get issue status=%d body=%s", detailRecorder.Code, detailRecorder.Body.String())
	}
	var detail IssueDetail
	if err := json.Unmarshal(detailRecorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("parse issue detail: %v", err)
	}
	if detail.Issue.ProjectID != "" || detail.Project.ID != "" {
		t.Fatalf("expected detail without project, got issue=%+v project=%+v", detail.Issue, detail.Project)
	}
	tasksBeforeAttach, err := store.ListRuntimeTasks(context.Background(), user.ID, workspaceID)
	if err != nil {
		t.Fatalf("list tasks before attach: %v", err)
	}
	for _, task := range tasksBeforeAttach {
		if task.IssueID == createResult.IssueID && runtimeTaskAutomation(task) == issueAnalysisAutomation {
			t.Fatalf("issue without project should not queue analysis before attach, got %+v", task)
		}
	}

	createProjectRecorder := httptest.NewRecorder()
	createProjectReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects", strings.NewReader(`{"name":"mspace","sourceType":"local","repoPath":"/Users/mlhiter/personal-projects/mspace","defaultBranch":"main"}`))
	createProjectReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createProjectRecorder, createProjectReq)
	if createProjectRecorder.Code != http.StatusCreated {
		t.Fatalf("create project status=%d body=%s", createProjectRecorder.Code, createProjectRecorder.Body.String())
	}
	var project Project
	if err := json.Unmarshal(createProjectRecorder.Body.Bytes(), &project); err != nil {
		t.Fatalf("parse project: %v", err)
	}

	updateRecorder := httptest.NewRecorder()
	updateReq := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+workspaceID+"/issues/"+createResult.IssueID, strings.NewReader(`{"projectId":"`+project.ID+`"}`))
	updateReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(updateRecorder, updateReq)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("attach project status=%d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	var updated Issue
	if err := json.Unmarshal(updateRecorder.Body.Bytes(), &updated); err != nil {
		t.Fatalf("parse updated issue: %v", err)
	}
	if updated.ProjectID != project.ID {
		t.Fatalf("expected issue attached to project %q, got %+v", project.ID, updated)
	}
	analysisTask := waitForIssueAnalysisTask(t, store, user.ID, workspaceID, createResult.IssueID)
	if analysisTask.ProjectID != project.ID || analysisTask.Priority != 10 {
		t.Fatalf("expected project attach to queue high-priority analysis for %q, got %+v", project.ID, analysisTask)
	}

	attachedDetailRecorder := httptest.NewRecorder()
	attachedDetailReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/issues/"+createResult.IssueID, nil)
	attachedDetailReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(attachedDetailRecorder, attachedDetailReq)
	if attachedDetailRecorder.Code != http.StatusOK {
		t.Fatalf("get attached issue status=%d body=%s", attachedDetailRecorder.Code, attachedDetailRecorder.Body.String())
	}
	var attachedDetail IssueDetail
	if err := json.Unmarshal(attachedDetailRecorder.Body.Bytes(), &attachedDetail); err != nil {
		t.Fatalf("parse attached detail: %v", err)
	}
	if attachedDetail.Issue.ProjectID != project.ID || attachedDetail.Project.ID != project.ID || attachedDetail.Project.Name != "mspace" {
		t.Fatalf("expected attached project detail, got issue=%+v project=%+v", attachedDetail.Issue, attachedDetail.Project)
	}
}

func TestIssueTypeTriageStoreTransitions(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "triage-user",
		Login:          "triage-user",
		Name:           "Triage User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	workspaceID := workspaces[0].ID
	issueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{Body: "Fix the broken issue type classifier"})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	detail, err := store.GetIssue(context.Background(), user.ID, workspaceID, issueID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if detail.Issue.TriageStatus != "pending" {
		t.Fatalf("expected pending triage, got %q", detail.Issue.TriageStatus)
	}

	if err := store.ApplyIssueTypeClassification(context.Background(), workspaceID, issueID, "type:fix"); err != nil {
		t.Fatalf("apply issue type classification: %v", err)
	}
	detail, err = store.GetIssue(context.Background(), user.ID, workspaceID, issueID)
	if err != nil {
		t.Fatalf("get classified issue: %v", err)
	}
	if detail.Issue.TriageStatus != "classified" {
		t.Fatalf("expected classified triage, got %q", detail.Issue.TriageStatus)
	}
	if len(detail.Labels) != 1 || detail.Labels[0].Key != "type:fix" {
		t.Fatalf("expected type:fix label, got %+v", detail.Labels)
	}

	failedIssueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{Body: "Ambiguous issue"})
	if err != nil {
		t.Fatalf("create issue for failed triage: %v", err)
	}
	if err := store.MarkIssueTriageFailed(context.Background(), workspaceID, failedIssueID); err != nil {
		t.Fatalf("mark triage failed: %v", err)
	}
	failedDetail, err := store.GetIssue(context.Background(), user.ID, workspaceID, failedIssueID)
	if err != nil {
		t.Fatalf("get failed issue: %v", err)
	}
	if failedDetail.Issue.TriageStatus != "failed" {
		t.Fatalf("expected failed triage, got %q", failedDetail.Issue.TriageStatus)
	}
}

func TestProjectTestCasesHTTPFlow(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "test-case-user",
		Login:          "test-case-user",
		Name:           "Test Case User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	project := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "mspace")

	createRecorder := httptest.NewRecorder()
	createBody := `{
		"title":"Local password login succeeds",
		"type":"ui",
		"area":"auth",
		"priority":"p1",
		"status":"ready",
		"preconditions":"A local account exists.",
		"steps":[{"action":"Open the sign-in form"},{"action":"Submit a valid username and password","expected":"The workspace opens."}],
		"expectedResult":"The user lands in the selected workspace.",
		"environmentRequirements":"Personal desktop server is running.",
		"tags":["auth","smoke"]
	}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-cases", strings.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createRecorder, createReq)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create test case status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	var created TestCase
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse created test case: %v", err)
	}
	if created.ProjectID != project.ID || created.Type != "ui" || created.Status != "ready" || created.QualityScore < 80 {
		t.Fatalf("unexpected created test case: %+v", created)
	}

	importRecorder := httptest.NewRecorder()
	importReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-cases/import", strings.NewReader(`{"format":"markdown","content":"- Invalid password shows an error\n- User can reset password from the login page"}`))
	importReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(importRecorder, importReq)
	if importRecorder.Code != http.StatusCreated {
		t.Fatalf("import test cases status=%d body=%s", importRecorder.Code, importRecorder.Body.String())
	}
	var imported ImportTestCasesResult
	if err := json.Unmarshal(importRecorder.Body.Bytes(), &imported); err != nil {
		t.Fatalf("parse imported test cases: %v", err)
	}
	if len(imported.Created) != 2 || imported.Created[0].Source != "import" {
		t.Fatalf("unexpected import result: %+v", imported)
	}
	if imported.Created[0].Type != "functional" {
		t.Fatalf("expected markdown import to default to functional type, got %+v", imported.Created[0])
	}
	if imported.Created[0].Tags == nil || imported.Created[0].Dependencies == nil {
		t.Fatalf("expected imported case array fields to be non-nil, got %+v", imported.Created[0])
	}
	var rawImported map[string]json.RawMessage
	if err := json.Unmarshal(importRecorder.Body.Bytes(), &rawImported); err != nil {
		t.Fatalf("parse raw imported test cases: %v", err)
	}
	var rawCreated []map[string]json.RawMessage
	if err := json.Unmarshal(rawImported["created"], &rawCreated); err != nil {
		t.Fatalf("parse raw imported created cases: %v", err)
	}
	for _, field := range []string{"tags", "dependencies", "qualityFindings"} {
		if string(rawCreated[0][field]) == "null" {
			t.Fatalf("expected created[0].%s to be an empty array, got null in %s", field, importRecorder.Body.String())
		}
	}
	if string(rawImported["skipped"]) == "null" {
		t.Fatalf("expected skipped to be an empty array, got null in %s", importRecorder.Body.String())
	}
	if len(imported.Skipped) != 0 {
		t.Fatalf("expected no skipped markdown imports, got %+v", imported.Skipped)
	}

	previewRecorder := httptest.NewRecorder()
	workbookContent := testCaseWorkbookBase64(t)
	workbookBytes, err := base64.StdEncoding.DecodeString(workbookContent)
	if err != nil {
		t.Fatalf("decode workbook fixture: %v", err)
	}
	previewBody, err := json.Marshal(ImportTestCasesInput{
		Format:   "xlsx",
		FileName: "cases.xlsx",
		Content:  workbookContent,
	})
	if err != nil {
		t.Fatalf("marshal preview import body: %v", err)
	}
	previewReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-cases/import/preview", bytes.NewReader(previewBody))
	previewReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(previewRecorder, previewReq)
	if previewRecorder.Code != http.StatusOK {
		t.Fatalf("preview import test cases status=%d body=%s", previewRecorder.Code, previewRecorder.Body.String())
	}
	var preview ImportTestCasesPreview
	if err := json.Unmarshal(previewRecorder.Body.Bytes(), &preview); err != nil {
		t.Fatalf("parse import preview: %v", err)
	}
	if preview.ImportableCount != 2 || preview.SkippedCount != 2 || preview.ParsedCount != 4 {
		t.Fatalf("unexpected import preview counts: %+v", preview)
	}
	if preview.MissingFieldCounts["preconditions"] != 0 || len(preview.ImportableCaseSamples) == 0 || preview.ImportableCaseSamples[0].Title != "Invite link opens workspace" {
		t.Fatalf("unexpected import preview details: %+v", preview)
	}
	if preview.ContentBytes != len(workbookBytes) || preview.MaxImportableCases != maxImportedTestCases {
		t.Fatalf("unexpected import preview limits: %+v", preview)
	}
	if preview.ReachedImportCaseLimit {
		t.Fatalf("did not expect small preview to hit import limit: %+v", preview)
	}
	emptyPreviewRecorder := httptest.NewRecorder()
	emptyPreviewReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-cases/import/preview", strings.NewReader(`{"format":"csv","content":""}`))
	emptyPreviewReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(emptyPreviewRecorder, emptyPreviewReq)
	if emptyPreviewRecorder.Code != http.StatusBadRequest {
		t.Fatalf("empty preview status=%d body=%s", emptyPreviewRecorder.Code, emptyPreviewRecorder.Body.String())
	}

	listRecorder := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-cases?status=ready", nil)
	listReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list test cases status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var listed []TestCase
	var listedPage TestCaseListResult
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listedPage); err != nil {
		t.Fatalf("parse listed test cases: %v", err)
	}
	listed = listedPage.Cases
	if len(listed) == 0 {
		t.Fatalf("expected ready cases, got %+v", listed)
	}
	if findTestCase(t, listed, created.ID).ID != created.ID {
		t.Fatalf("expected ready list to include created case, got %+v", listed)
	}
	if listedPage.Total < 1 || listedPage.Limit != defaultTestCaseListLimit || listedPage.Offset != 0 {
		t.Fatalf("unexpected ready list pagination: %+v", listedPage)
	}

	excelRecorder := httptest.NewRecorder()
	excelBody, err := json.Marshal(ImportTestCasesInput{
		Format:   "xlsx",
		FileName: "cases.xlsx",
		Content:  testCaseWorkbookBase64(t),
	})
	if err != nil {
		t.Fatalf("marshal excel import body: %v", err)
	}
	excelReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-cases/import", bytes.NewReader(excelBody))
	excelReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(excelRecorder, excelReq)
	if excelRecorder.Code != http.StatusCreated {
		t.Fatalf("excel import test cases status=%d body=%s", excelRecorder.Code, excelRecorder.Body.String())
	}
	var excelImported ImportTestCasesResult
	if err := json.Unmarshal(excelRecorder.Body.Bytes(), &excelImported); err != nil {
		t.Fatalf("parse excel imported test cases: %v", err)
	}
	if len(excelImported.Created) != 2 || len(excelImported.Skipped) != 2 {
		t.Fatalf("unexpected excel import result: %+v", excelImported)
	}
	if excelImported.Created[0].Title != "Invite link opens workspace" || excelImported.Created[0].Type != "deployment" || excelImported.Created[0].Priority != "p1" || excelImported.Created[0].Source != "import" {
		t.Fatalf("unexpected first excel import case: %+v", excelImported.Created[0])
	}
	if len(excelImported.Created[0].Steps) != 2 || excelImported.Created[0].Steps[0].Action != "Open invite link" {
		t.Fatalf("unexpected excel import steps: %+v", excelImported.Created[0].Steps)
	}
	if excelImported.Skipped[0].Line != 3 || excelImported.Skipped[0].Reason != "missing title" {
		t.Fatalf("unexpected excel import skip: %+v", excelImported.Skipped)
	}
	if excelImported.Skipped[1].Reason != "type must be functional, ui, api, or deployment" {
		t.Fatalf("unexpected invalid type skip: %+v", excelImported.Skipped)
	}

	updateRecorder := httptest.NewRecorder()
	updateBody := `{
		"title":"Local password login succeeds with saved workspace",
		"type":"api",
		"area":"auth",
		"priority":"p0",
		"status":"ready",
		"preconditions":"A local account exists and the personal server is reachable.",
		"steps":[{"action":"Open the sign-in form"},{"action":"Submit a valid username and password","expected":"The workspace opens."}],
		"expectedResult":"The selected workspace opens without showing the account creation screen.",
		"environmentRequirements":"Personal desktop server is running.",
		"tags":["auth","smoke"]
	}`
	updateReq := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-cases/"+created.ID, strings.NewReader(updateBody))
	updateReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(updateRecorder, updateReq)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update test case status=%d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	var updated TestCase
	if err := json.Unmarshal(updateRecorder.Body.Bytes(), &updated); err != nil {
		t.Fatalf("parse updated test case: %v", err)
	}
	if updated.Type != "api" || updated.Priority != "p0" || updated.Title == created.Title {
		t.Fatalf("unexpected updated test case: %+v", updated)
	}

	revisionsRecorder := httptest.NewRecorder()
	revisionsReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-cases/"+created.ID+"/revisions", nil)
	revisionsReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(revisionsRecorder, revisionsReq)
	if revisionsRecorder.Code != http.StatusOK {
		t.Fatalf("list revisions status=%d body=%s", revisionsRecorder.Code, revisionsRecorder.Body.String())
	}
	var revisions []TestCaseRevision
	if err := json.Unmarshal(revisionsRecorder.Body.Bytes(), &revisions); err != nil {
		t.Fatalf("parse revisions: %v", err)
	}
	if len(revisions) != 2 || revisions[0].RevisionNumber != 2 || revisions[1].RevisionNumber != 1 {
		t.Fatalf("expected two descending revisions, got %+v", revisions)
	}

	pagedCases := listProjectTestCasesPageViaHTTP(t, router, sessionToken, workspaceID, project.ID, "limit=2&offset=1")
	if pagedCases.Total < pagedCases.Offset+len(pagedCases.Cases) || pagedCases.Limit != 2 || pagedCases.Offset != 1 || len(pagedCases.Cases) != 2 {
		t.Fatalf("unexpected paged cases: %+v", pagedCases)
	}

	deleteRecorder := httptest.NewRecorder()
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-cases/"+created.ID, nil)
	deleteReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(deleteRecorder, deleteReq)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete test case status=%d body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	var archived TestCase
	if err := json.Unmarshal(deleteRecorder.Body.Bytes(), &archived); err != nil {
		t.Fatalf("parse archived test case: %v", err)
	}
	if archived.ID != created.ID || archived.Status != "archived" {
		t.Fatalf("expected delete to archive the case, got %+v", archived)
	}
	defaultAfterArchive := listProjectTestCasesPageViaHTTP(t, router, sessionToken, workspaceID, project.ID, "")
	for _, testCase := range defaultAfterArchive.Cases {
		if testCase.ID == created.ID {
			t.Fatalf("default list should hide archived case, got %+v", defaultAfterArchive)
		}
	}
	archivedPage := listProjectTestCasesPageViaHTTP(t, router, sessionToken, workspaceID, project.ID, "status=archived")
	if archivedPage.Total != 1 || len(archivedPage.Cases) != 1 || archivedPage.Cases[0].ID != created.ID {
		t.Fatalf("expected archived list to contain deleted case, got %+v", archivedPage)
	}
	archivedDetail := getProjectTestCaseViaHTTP(t, router, sessionToken, workspaceID, project.ID, created.ID)
	if archivedDetail.Status != "archived" {
		t.Fatalf("expected archived detail to remain readable, got %+v", archivedDetail)
	}
	archivedRevisions := listProjectTestCaseRevisionsViaHTTP(t, router, sessionToken, workspaceID, project.ID, created.ID)
	if len(archivedRevisions) != 3 || archivedRevisions[0].Snapshot.Status != "archived" {
		t.Fatalf("expected archive revision to be retained, got %+v", archivedRevisions)
	}

	bulkBody, err := json.Marshal(DeleteProjectTestCasesInput{CaseIDs: []string{imported.Created[0].ID, imported.Created[0].ID, imported.Created[1].ID}})
	if err != nil {
		t.Fatalf("marshal bulk delete body: %v", err)
	}
	bulkRecorder := httptest.NewRecorder()
	bulkReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-cases/delete", bytes.NewReader(bulkBody))
	bulkReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(bulkRecorder, bulkReq)
	if bulkRecorder.Code != http.StatusOK {
		t.Fatalf("bulk delete test cases status=%d body=%s", bulkRecorder.Code, bulkRecorder.Body.String())
	}
	var bulkArchived []TestCase
	if err := json.Unmarshal(bulkRecorder.Body.Bytes(), &bulkArchived); err != nil {
		t.Fatalf("parse bulk archived test cases: %v", err)
	}
	if len(bulkArchived) != 2 || bulkArchived[0].Status != "archived" || bulkArchived[1].Status != "archived" {
		t.Fatalf("expected bulk delete to archive unique cases, got %+v", bulkArchived)
	}
	emptyBulkRecorder := httptest.NewRecorder()
	emptyBulkReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-cases/delete", strings.NewReader(`{"caseIds":[]}`))
	emptyBulkReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(emptyBulkRecorder, emptyBulkReq)
	if emptyBulkRecorder.Code != http.StatusBadRequest {
		t.Fatalf("empty bulk delete status=%d body=%s", emptyBulkRecorder.Code, emptyBulkRecorder.Body.String())
	}
}

func testCaseWorkbookBase64(t *testing.T) string {
	t.Helper()
	file := excelize.NewFile()
	sheet := "Cases"
	index, err := file.NewSheet(sheet)
	if err != nil {
		t.Fatalf("create worksheet: %v", err)
	}
	file.SetActiveSheet(index)
	if err := file.DeleteSheet("Sheet1"); err != nil {
		t.Fatalf("delete default worksheet: %v", err)
	}
	rows := [][]string{
		{"title", "type", "area", "priority", "preconditions", "steps", "expected_result", "environment_requirements", "tags"},
		{"Invite link opens workspace", "deployment", "team access", "p1", "A valid invite link exists.", "Open invite link\nSign in with local account", "The app accepts the invitation and opens the team workspace.", "Desktop app connected to the team server.", "invite,smoke"},
		{"", "ui", "team access", "p2", "No title", "Do something", "Skipped", "Any environment", "skip"},
		{"CSV-compatible columns import from Excel", "api", "tests", "p2", "A project test library exists.", "Import the workbook", "Two valid cases are created from the workbook.", "Personal desktop server is running.", "tests,import"},
		{"Unsupported mobile type is skipped", "mobile", "tests", "p3", "A project test library exists.", "Import the workbook", "This row is skipped.", "Personal desktop server is running.", "tests,import"},
	}
	for rowIndex, row := range rows {
		for columnIndex, value := range row {
			cell, err := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+1)
			if err != nil {
				t.Fatalf("cell name: %v", err)
			}
			if err := file.SetCellValue(sheet, cell, value); err != nil {
				t.Fatalf("set cell %s: %v", cell, err)
			}
		}
	}
	var buffer bytes.Buffer
	if err := file.Write(&buffer); err != nil {
		t.Fatalf("write workbook: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close workbook: %v", err)
	}
	return base64.StdEncoding.EncodeToString(buffer.Bytes())
}

func TestProjectTestCasesAreProjectScoped(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "test-case-scope-user",
		Login:          "test-case-scope-user",
		Name:           "Test Case Scope User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()
	firstProject := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "first")
	secondProject := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "second")

	createRecorder := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+firstProject.ID+"/test-cases", strings.NewReader(`{"title":"Scoped case","steps":[{"action":"Run scoped case"}],"expectedResult":"It stays scoped."}`))
	createReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createRecorder, createReq)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create test case status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	var created TestCase
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse created test case: %v", err)
	}

	wrongProjectRecorder := httptest.NewRecorder()
	wrongProjectReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/projects/"+secondProject.ID+"/test-cases/"+created.ID, nil)
	wrongProjectReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(wrongProjectRecorder, wrongProjectReq)
	if wrongProjectRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected not found across projects, status=%d body=%s", wrongProjectRecorder.Code, wrongProjectRecorder.Body.String())
	}
}

func TestEnvironmentAPISupportsVirtualMachines(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "environment-vm-user",
		Login:          "environment-vm-user",
		Name:           "Environment VM User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	createRecorder := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/environments", strings.NewReader(`{
		"name":"staging-vm",
		"kind":"virtual_machine",
		"status":"ready",
		"sshHost":"127.0.0.1",
		"sshPort":1,
		"sshUser":"ubuntu",
		"sshAuthRef":"secret://mspace/staging-vm",
		"sshAuth":{"method":"password","password":"wrong-password"},
		"workdir":"/srv/app",
		"serviceHint":"systemd:mspace"
	}`))
	createReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createRecorder, createReq)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create vm environment status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	var created Environment
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse vm environment: %v", err)
	}
	if created.Kind != environmentKindVirtualMachine || created.VirtualMachine == nil || created.VirtualMachine.SSHHost != "127.0.0.1" || created.VirtualMachine.SSHPort != 1 {
		t.Fatalf("unexpected vm environment: %+v", created)
	}
	if created.Status != "unreachable" || created.LastCheckedAt == "" {
		t.Fatalf("expected failed ssh check to save vm as unreachable, got %+v", created)
	}
	if created.VirtualMachine.SSHAuthRef != "secret://mspace/staging-vm" || !created.VirtualMachine.SSHAuthConfigured {
		t.Fatalf("expected created vm to report configured ssh auth without raw material, got %+v", created.VirtualMachine)
	}
	assertNoSSHAuthMaterialInBody(t, createRecorder.Body.String(), "wrong-password")

	updateRecorder := httptest.NewRecorder()
	updateReq := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+workspaceID+"/environments/"+created.ID, strings.NewReader(`{
		"name":"staging-vm-renamed",
		"kind":"virtual_machine",
		"status":"ready",
		"sshHost":"127.0.0.1",
		"sshPort":1,
		"sshUser":"ubuntu"
	}`))
	updateReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(updateRecorder, updateReq)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update vm environment status=%d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	var updated Environment
	if err := json.Unmarshal(updateRecorder.Body.Bytes(), &updated); err != nil {
		t.Fatalf("parse updated vm environment: %v", err)
	}
	if updated.Name != "staging-vm-renamed" || updated.VirtualMachine == nil || updated.VirtualMachine.SSHHost != "127.0.0.1" || updated.VirtualMachine.SSHPort != 1 {
		t.Fatalf("unexpected updated vm environment: %+v", updated)
	}
	if updated.Status != "unreachable" || updated.LastCheckedAt == "" {
		t.Fatalf("expected updated vm to remain unreachable after failed ssh check, got %+v", updated)
	}
	if !updated.VirtualMachine.SSHAuthConfigured || updated.VirtualMachine.SSHAuthRef != "secret://mspace/staging-vm" {
		t.Fatalf("expected update without sshAuth to preserve stored auth, got %+v", updated.VirtualMachine)
	}
	assertNoSSHAuthMaterialInBody(t, updateRecorder.Body.String(), "wrong-password")

	checkRecorder := httptest.NewRecorder()
	checkReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/environments/"+created.ID+"/check", strings.NewReader(`{}`))
	checkReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(checkRecorder, checkReq)
	if checkRecorder.Code != http.StatusOK {
		t.Fatalf("check vm environment status=%d body=%s", checkRecorder.Code, checkRecorder.Body.String())
	}
	var checked Environment
	if err := json.Unmarshal(checkRecorder.Body.Bytes(), &checked); err != nil {
		t.Fatalf("parse checked vm environment: %v", err)
	}
	if checked.ID != created.ID || checked.Kind != environmentKindVirtualMachine || checked.Status != "unreachable" || checked.LastCheckedAt == "" {
		t.Fatalf("expected checked vm to remain unreachable with checked time, got %+v", checked)
	}
	if !checked.VirtualMachine.SSHAuthConfigured {
		t.Fatalf("expected recheck without sshAuth to use saved auth, got %+v", checked.VirtualMachine)
	}
	assertNoSSHAuthMaterialInBody(t, checkRecorder.Body.String(), "wrong-password")

	replaceAuthRecorder := httptest.NewRecorder()
	replaceAuthReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/environments/"+created.ID+"/check", strings.NewReader(`{
		"sshAuth":{"method":"password","password":"replacement-password"}
	}`))
	replaceAuthReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(replaceAuthRecorder, replaceAuthReq)
	if replaceAuthRecorder.Code != http.StatusOK {
		t.Fatalf("replace vm auth status=%d body=%s", replaceAuthRecorder.Code, replaceAuthRecorder.Body.String())
	}
	assertNoSSHAuthMaterialInBody(t, replaceAuthRecorder.Body.String(), "replacement-password")

	listRecorder := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/environments", nil)
	listReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list environments status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var environments []Environment
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &environments); err != nil {
		t.Fatalf("parse environments: %v", err)
	}
	if len(environments) != 1 || environments[0].ID != created.ID || environments[0].Kind != environmentKindVirtualMachine {
		t.Fatalf("expected vm environment in list, got %+v", environments)
	}

	deleteRecorder := httptest.NewRecorder()
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/workspaces/"+workspaceID+"/environments/"+created.ID, nil)
	deleteReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(deleteRecorder, deleteReq)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete vm environment status=%d body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
}

func assertNoSSHAuthMaterialInBody(t *testing.T, body string, values ...string) {
	t.Helper()
	for _, value := range values {
		if value != "" && strings.Contains(body, value) {
			t.Fatalf("response leaked ssh auth material %q in body=%s", value, body)
		}
	}
	if strings.Contains(body, "sshAuth\"") || strings.Contains(body, "sshPassword") || strings.Contains(body, "privateKey") || strings.Contains(body, "passphrase") {
		t.Fatalf("response leaked ssh auth fields in body=%s", body)
	}
}

func TestEnvironmentAPIRejectsVirtualMachineWithoutUsableSSHAuth(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "environment-vm-auth-user",
		Login:          "environment-vm-auth-user",
		Name:           "Environment VM Auth User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	for _, item := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing auth",
			body: `{
				"name":"missing-auth-vm",
				"kind":"virtual_machine",
				"sshHost":"127.0.0.1",
				"sshPort":1,
				"sshUser":"ubuntu"
			}`,
			want: "sshAuth is required",
		},
		{
			name: "missing password",
			body: `{
				"name":"missing-password-vm",
				"kind":"virtual_machine",
				"sshHost":"127.0.0.1",
				"sshPort":1,
				"sshUser":"ubuntu",
				"sshAuth":{"method":"password"}
			}`,
			want: "ssh password is required",
		},
		{
			name: "invalid private key",
			body: `{
				"name":"bad-key-vm",
				"kind":"virtual_machine",
				"sshHost":"127.0.0.1",
				"sshPort":1,
				"sshUser":"ubuntu",
				"sshAuth":{"method":"private_key","privateKey":"not a private key"}
			}`,
			want: "ssh private key must be parseable",
		},
	} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/environments", strings.NewReader(item.body))
		req.Header.Set("Authorization", "Bearer "+sessionToken)
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), item.want) {
			t.Fatalf("%s status=%d body=%s", item.name, recorder.Code, recorder.Body.String())
		}
	}
}

func TestEnvironmentAPIChecksKubernetesReachability(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "environment-kubernetes-user",
		Login:          "environment-kubernetes-user",
		Name:           "Environment Kubernetes User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	createRecorder := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/environments", strings.NewReader(`{
		"name":"staging-k8s",
		"kind":"kubernetes",
		"kubernetes":{
			"kubeconfigPath":"/tmp/mspace-missing-kubeconfig",
			"imageRegistryPrefix":"registry.example.com/team",
			"exposureMode":"nodeport"
		}
	}`))
	createReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createRecorder, createReq)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create kubernetes environment status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	var created Environment
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse kubernetes environment: %v", err)
	}
	if created.Kind != environmentKindKubernetes || created.Status != "unreachable" || created.LastCheckedAt == "" {
		t.Fatalf("expected unreachable checked kubernetes environment, got %+v", created)
	}

	checkRecorder := httptest.NewRecorder()
	checkReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/environments/"+created.ID+"/check", nil)
	checkReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(checkRecorder, checkReq)
	if checkRecorder.Code != http.StatusOK {
		t.Fatalf("check kubernetes environment status=%d body=%s", checkRecorder.Code, checkRecorder.Body.String())
	}
	var checked Environment
	if err := json.Unmarshal(checkRecorder.Body.Bytes(), &checked); err != nil {
		t.Fatalf("parse checked kubernetes environment: %v", err)
	}
	if checked.ID != created.ID || checked.Kind != environmentKindKubernetes || checked.Status != "unreachable" || checked.LastCheckedAt == "" {
		t.Fatalf("expected environment-level check to return unreachable kubernetes environment, got %+v", checked)
	}
}

func TestTestPlansFreezeEnvironmentSnapshotsAndProtectReferences(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "environment-plan-user",
		Login:          "environment-plan-user",
		Name:           "Environment Plan User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()
	project := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "environment-plan-project")
	testCase := createProjectTestCaseViaHTTP(t, router, sessionToken, workspaceID, project.ID, `{
		"title":"Environment-bound smoke",
		"status":"ready",
		"steps":[{"action":"Run smoke"}],
		"expectedResult":"Smoke passes."
	}`)
	environment, err := store.CreateEnvironment(context.Background(), user.ID, workspaceID, EnvironmentInput{
		Name:       "plan-vm",
		Kind:       environmentKindVirtualMachine,
		SSHHost:    "127.0.0.1",
		SSHPort:    1,
		SSHUser:    "ubuntu",
		SSHAuthRef: "secret://mspace/plan-vm",
		SSHAuth:    &VirtualMachineSSHAuthInput{Method: "password", Password: "wrong-password"},
	})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}
	plan, err := store.CreateProjectTestPlan(context.Background(), user.ID, workspaceID, project.ID, TestPlanInput{
		Title:         "VM plan",
		Status:        "ready",
		TargetType:    "branch",
		TargetValue:   "main",
		EnvironmentID: environment.ID,
		CaseIDs:       []string{testCase.ID},
	})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if plan.Plan.EnvironmentID != environment.ID || plan.Plan.EnvironmentKind != environmentKindVirtualMachine || !strings.Contains(string(plan.Plan.EnvironmentSnapshot), "plan-vm") {
		t.Fatalf("expected plan environment snapshot, got %+v snapshot=%s", plan.Plan, string(plan.Plan.EnvironmentSnapshot))
	}
	if err := store.DeleteEnvironment(context.Background(), user.ID, workspaceID, environment.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("referenced environment should be protected, got %v", err)
	}
}

func TestIssueTestDeployRejectsVirtualMachineEnvironment(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "environment-vm-deploy-user",
		Login:          "environment-vm-deploy-user",
		Name:           "Environment VM Deploy User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()
	project := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "vm-deploy-project")
	issueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{
		ProjectID: project.ID,
		Body:      "Try to deploy against a VM environment.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	environment, err := store.CreateEnvironment(context.Background(), user.ID, workspaceID, EnvironmentInput{
		Name:    "deploy-vm",
		Kind:    environmentKindVirtualMachine,
		SSHHost: "127.0.0.1",
		SSHPort: 1,
		SSHUser: "ubuntu",
		SSHAuth: &VirtualMachineSSHAuthInput{Method: "password", Password: "wrong-password"},
	})
	if err != nil {
		t.Fatalf("create vm environment: %v", err)
	}
	registerTestRuntimeWorker(t, router, sessionToken, workspaceID)

	deployRecorder := httptest.NewRecorder()
	deployReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/issues/"+issueID+"/test-deploy", strings.NewReader(`{
		"environmentId":"`+environment.ID+`",
		"sourceCommitSha":"5555555555555555555555555555555555555555"
	}`))
	deployReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(deployRecorder, deployReq)
	if deployRecorder.Code != http.StatusBadRequest || !strings.Contains(deployRecorder.Body.String(), "requires a Kubernetes environment") {
		t.Fatalf("expected vm deploy bad request, status=%d body=%s", deployRecorder.Code, deployRecorder.Body.String())
	}
}

func TestProjectTestCaseAgentActionsRequireWorkerBeforeCreatingIssues(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "test-module-no-worker-user",
		Login:          "test-module-no-worker-user",
		Name:           "Test Module No Worker User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()
	project := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "test-module-no-worker")
	testCase := createProjectTestCaseViaHTTP(t, router, sessionToken, workspaceID, project.ID, `{
		"title":"No worker optimization stays visible",
		"status":"ready",
		"steps":[{"action":"Click optimize"}],
		"expectedResult":"The UI shows a worker error."
	}`)

	optimizeRecorder := httptest.NewRecorder()
	optimizeReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-cases/optimize", strings.NewReader(`{"caseIds":["`+testCase.ID+`"],"runtimeMode":"personal"}`))
	optimizeReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(optimizeRecorder, optimizeReq)
	if optimizeRecorder.Code != http.StatusConflict || !strings.Contains(optimizeRecorder.Body.String(), "no active codex worker") {
		t.Fatalf("expected optimize no-worker conflict, status=%d body=%s", optimizeRecorder.Code, optimizeRecorder.Body.String())
	}
	if issues := listIssuesViaHTTP(t, router, sessionToken, workspaceID); len(issues) != 0 {
		t.Fatalf("optimize without a worker should not create orphan issues, got %+v", issues)
	}

	generateRecorder := httptest.NewRecorder()
	generateReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-cases/generate", strings.NewReader(`{"area":"auth","runtimeMode":"personal"}`))
	generateReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(generateRecorder, generateReq)
	if generateRecorder.Code != http.StatusConflict || !strings.Contains(generateRecorder.Body.String(), "no active codex worker") {
		t.Fatalf("expected generate no-worker conflict, status=%d body=%s", generateRecorder.Code, generateRecorder.Body.String())
	}
	if issues := listIssuesViaHTTP(t, router, sessionToken, workspaceID); len(issues) != 0 {
		t.Fatalf("generate without a worker should not create orphan issues, got %+v", issues)
	}

	runRecorder := httptest.NewRecorder()
	runReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-runs", strings.NewReader(`{"caseIds":["`+testCase.ID+`"],"runtimeMode":"personal"}`))
	runReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(runRecorder, runReq)
	if runRecorder.Code != http.StatusConflict || !strings.Contains(runRecorder.Body.String(), "no active codex worker") {
		t.Fatalf("expected direct run no-worker conflict, status=%d body=%s", runRecorder.Code, runRecorder.Body.String())
	}
	if issues := listIssuesViaHTTP(t, router, sessionToken, workspaceID); len(issues) != 0 {
		t.Fatalf("direct run without a worker should not create orphan issues, got %+v", issues)
	}
}

func TestUITestRunRequiresBrowserCapableWorker(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "test-module-ui-worker-user",
		Login:          "test-module-ui-worker-user",
		Name:           "Test Module UI Worker User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()
	project := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "test-module-ui-worker")
	uiCase := createProjectTestCaseViaHTTP(t, router, sessionToken, workspaceID, project.ID, `{
		"title":"Login form renders in browser",
		"type":"ui",
		"status":"ready",
		"steps":[{"action":"Open the login page","expected":"The login form is visible."},{"action":"Capture the page","expected":"A screenshot is saved."}],
		"expectedResult":"The browser evidence includes a screenshot.",
		"environmentRequirements":"Preview URL and browser/CDP worker are available."
	}`)

	registerTestRuntimeWorker(t, router, sessionToken, workspaceID)
	blockedRecorder := httptest.NewRecorder()
	blockedReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-runs", strings.NewReader(`{"caseIds":["`+uiCase.ID+`"],"runtimeMode":"personal","batchSize":1}`))
	blockedReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(blockedRecorder, blockedReq)
	if blockedRecorder.Code != http.StatusConflict || !strings.Contains(blockedRecorder.Body.String(), "no active codex worker") {
		t.Fatalf("expected UI run without browser worker to conflict, status=%d body=%s", blockedRecorder.Code, blockedRecorder.Body.String())
	}
	if issues := listIssuesViaHTTPWithQuery(t, router, sessionToken, workspaceID, "includeTestAutomation=1"); len(issues) != 0 {
		t.Fatalf("UI run without a browser worker should not create orphan issues, got %+v", issues)
	}

	tokenResult, worker := registerTestRuntimeWorkerWithCapabilities(t, router, sessionToken, workspaceID, `{"codex":true,"browser":true,"chrome_cdp":true}`)
	run := startAdHocProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, `{"caseIds":["`+uiCase.ID+`"],"runtimeMode":"personal","batchSize":1}`)
	if run.Run.Status != "running" || len(run.Items) != 1 || run.Items[0].AgentSessionID == "" {
		t.Fatalf("expected UI run to start with browser worker, got %+v", run)
	}
	task, err := store.ClaimRuntimeTask(context.Background(), RuntimeRegistration{WorkspaceID: workspaceID, TokenID: tokenResult.RegistrationToken.ID}, worker.ID)
	if err != nil {
		t.Fatalf("claim UI task: %v", err)
	}
	if task == nil || task.SessionID != run.Items[0].AgentSessionID {
		t.Fatalf("expected UI execution task, got %+v", task)
	}
	capabilitiesText := string(task.RequiredCapabilities)
	if !strings.Contains(capabilitiesText, `"browser":true`) || !strings.Contains(capabilitiesText, `"chrome_cdp":true`) {
		t.Fatalf("expected UI task to require browser and chrome_cdp, got %s", capabilitiesText)
	}
	sessionDetail, err := store.GetSession(context.Background(), user.ID, workspaceID, run.Items[0].AgentSessionID)
	if err != nil {
		t.Fatalf("get UI session: %v", err)
	}
	if !strings.Contains(sessionDetail.Session.Command, "screenshotPaths") || !strings.Contains(sessionDetail.Session.Command, "MSPACE_CHROME_CDP_URL") {
		t.Fatalf("expected UI session command to require screenshot evidence, got %q", sessionDetail.Session.Command)
	}
}

func TestProjectTestCaseImportMappingTaskRequiresWorker(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "test-module-import-mapping-user",
		Login:          "test-module-import-mapping-user",
		Name:           "Test Module Import Mapping User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()
	project := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "test-module-import-mapping")
	body := `{"format":"csv","fileName":"cases.csv","content":"用例ID,用例名称,步骤描述\nOSV2-001,登录页检查,打开登录页","runtimeMode":"personal"}`

	noWorkerRecorder := httptest.NewRecorder()
	noWorkerReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-cases/import/mapping-task", strings.NewReader(body))
	noWorkerReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(noWorkerRecorder, noWorkerReq)
	if noWorkerRecorder.Code != http.StatusConflict || !strings.Contains(noWorkerRecorder.Body.String(), "no active codex worker") {
		t.Fatalf("expected mapping no-worker conflict, status=%d body=%s", noWorkerRecorder.Code, noWorkerRecorder.Body.String())
	}

	registerTestRuntimeWorker(t, router, sessionToken, workspaceID)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-cases/import/mapping-task", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create import mapping task status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var task RuntimeTask
	if err := json.Unmarshal(recorder.Body.Bytes(), &task); err != nil {
		t.Fatalf("parse runtime task: %v", err)
	}
	if task.Kind != "test_case_import_mapping" || task.ProjectID != project.ID || task.RuntimeMode != "personal" {
		t.Fatalf("unexpected mapping task: %+v", task)
	}
	if !strings.Contains(string(task.Payload), `"workspaceId":"`+workspaceID+`"`) || !strings.Contains(string(task.Payload), `"用例名称"`) {
		t.Fatalf("expected mapping payload to include workspace and headers, got %s", task.Payload)
	}
}

func TestIssueListHidesLegacyTestAutomationIssues(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "test-module-legacy-automation-user",
		Login:          "test-module-legacy-automation-user",
		Name:           "Test Module Legacy Automation User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()
	project := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "test-module-legacy-automation")
	testCase := createProjectTestCaseViaHTTP(t, router, sessionToken, workspaceID, project.ID, `{
		"title":"Password login opens the workspace",
		"status":"ready",
		"steps":[{"action":"Open the sign-in form","expected":"The password form is visible."}],
		"expectedResult":"The selected workspace opens."
	}`)
	legacyOptimizeIssueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{
		ProjectID: project.ID,
		Title:     "Optimize test cases",
		Body:      buildTestCaseOptimizationIssueBody(project, []TestCase{testCase}, ""),
		LabelKeys: []string{"type:test"},
	})
	if err != nil {
		t.Fatalf("create legacy optimization issue: %v", err)
	}
	manualTestIssueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{
		ProjectID: project.ID,
		Title:     "Manual test investigation",
		Body:      "This is a human-owned testing issue and should remain in Issues.",
		LabelKeys: []string{"type:test"},
	})
	if err != nil {
		t.Fatalf("create manual test issue: %v", err)
	}

	defaultIssues := listIssuesViaHTTP(t, router, sessionToken, workspaceID)
	if issueListContains(defaultIssues, legacyOptimizeIssueID) {
		t.Fatalf("default issue list should hide legacy test automation issue %s, got %+v", legacyOptimizeIssueID, defaultIssues)
	}
	if !issueListContains(defaultIssues, manualTestIssueID) {
		t.Fatalf("default issue list should keep manual type:test issue %s, got %+v", manualTestIssueID, defaultIssues)
	}
	allTopLevelIssues := listIssuesViaHTTPWithQuery(t, router, sessionToken, workspaceID, "includeTestAutomation=1")
	if !issueListContains(allTopLevelIssues, legacyOptimizeIssueID) {
		t.Fatalf("includeTestAutomation should expose legacy optimization issue %s, got %+v", legacyOptimizeIssueID, allTopLevelIssues)
	}
}

func TestProjectTestModuleWorkflowHTTPFlow(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "test-module-user",
		Login:          "test-module-user",
		Name:           "Test Module User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()
	project := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "test-module")
	tokenResult, worker := registerTestRuntimeWorker(t, router, sessionToken, workspaceID)

	existingCase := createProjectTestCaseViaHTTP(t, router, sessionToken, workspaceID, project.ID, `{
		"title":"Password login opens the workspace",
		"area":"auth",
		"priority":"p1",
		"status":"ready",
		"preconditions":"A local account exists.",
		"steps":[{"action":"Open the sign-in form","expected":"The password form is visible."},{"action":"Submit valid credentials","expected":"The workspace opens."}],
		"expectedResult":"The selected workspace opens.",
		"environmentRequirements":"Personal desktop server is running.",
		"tags":["auth"]
	}`)
	issueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{
		ProjectID: project.ID,
		Title:     "Optimize test module cases",
		Body:      "Ask Codex to refine and generate test cases.",
	})
	if err != nil {
		t.Fatalf("create optimization issue: %v", err)
	}
	session, err := store.CreateAgentSession(context.Background(), user.ID, workspaceID, issueID, CreateAgentSessionInput{
		AgentEngine: agentEngineCodex,
		RuntimeMode: "personal",
		Command:     "Write test-case-proposals.json.",
		Automation:  testCaseOptimizationAutomation,
	})
	if err != nil {
		t.Fatalf("create optimization session: %v", err)
	}
	proposalResult := fmt.Sprintf(`{"status":"completed","result":{"exitCode":0,"testCaseProposals":{"summary":"proposal batch","proposals":[
		{"type":"create","title":"Invalid generated proposal","proposedCase":{"title":"","steps":[],"expectedResult":""}},
		{"type":"create","title":"Valid generated logout case","proposedCase":{"title":"Logout returns to sign-in","area":"auth","priority":"p1","status":"ready","preconditions":"A signed-in user is in the workspace.","steps":[{"action":"Open the account menu","expected":"Logout is available."},{"action":"Click logout","expected":"The sign-in form appears."}],"expectedResult":"The signed-out user sees the sign-in form.","environmentRequirements":"Personal desktop server is running.","tags":["auth","regression"]}},
		{"type":"update","caseId":%q,"title":"Refine password login case","proposedCase":{"title":"Password login opens the last selected workspace","area":"auth","priority":"p0","status":"ready","preconditions":"A local account exists and a workspace was previously selected.","steps":[{"action":"Open the sign-in form","expected":"The password form is visible."},{"action":"Submit valid credentials","expected":"The previous workspace opens."}],"expectedResult":"The selected workspace opens without returning to account creation.","environmentRequirements":"Personal desktop server is running.","tags":["auth","smoke"]}}
	]}}}`, existingCase.ID)
	completedOptimizationTask, err := claimAndCompleteTask(t, router, tokenResult.Token, worker.ID, proposalResult)
	if err != nil {
		t.Fatalf("complete proposal task: %v", err)
	}
	if completedOptimizationTask.SessionID != session.ID {
		t.Fatalf("expected optimization session %s, got task %+v", session.ID, completedOptimizationTask)
	}
	manualTestIssueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{
		ProjectID: project.ID,
		Title:     "Manual test investigation",
		Body:      "This is a human-owned testing issue and should remain in Issues.",
		LabelKeys: []string{"type:test"},
	})
	if err != nil {
		t.Fatalf("create manual test issue: %v", err)
	}
	defaultIssues := listIssuesViaHTTP(t, router, sessionToken, workspaceID)
	if issueListContains(defaultIssues, issueID) {
		t.Fatalf("default issue list should hide test-case optimization issue %s, got %+v", issueID, defaultIssues)
	}
	if !issueListContains(defaultIssues, manualTestIssueID) {
		t.Fatalf("default issue list should keep manual type:test issue %s, got %+v", manualTestIssueID, defaultIssues)
	}
	allTopLevelIssues := listIssuesViaHTTPWithQuery(t, router, sessionToken, workspaceID, "includeTestAutomation=1")
	if !issueListContains(allTopLevelIssues, issueID) {
		t.Fatalf("includeTestAutomation should expose optimization issue %s, got %+v", issueID, allTopLevelIssues)
	}

	proposals := listProjectTestCaseProposalsViaHTTP(t, router, sessionToken, workspaceID, project.ID)
	if len(proposals) != 3 {
		t.Fatalf("expected three proposals, got %+v", proposals)
	}
	invalidProposal := findTestCaseProposal(t, proposals, "Invalid generated proposal")
	if invalidProposal.Status != "invalid" || len(invalidProposal.ValidationErrors) == 0 {
		t.Fatalf("expected invalid proposal with validation errors, got %+v", invalidProposal)
	}
	applyInvalidRecorder := httptest.NewRecorder()
	applyInvalidReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+project.ID+"/test-case-proposals/"+invalidProposal.ID+"/apply", strings.NewReader(`{"note":"try invalid"}`))
	applyInvalidReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(applyInvalidRecorder, applyInvalidReq)
	if applyInvalidRecorder.Code != http.StatusConflict {
		t.Fatalf("expected invalid proposal apply conflict, status=%d body=%s", applyInvalidRecorder.Code, applyInvalidRecorder.Body.String())
	}

	createProposal := findTestCaseProposal(t, proposals, "Valid generated logout case")
	createdFromProposal := applyProjectTestCaseProposalViaHTTP(t, router, sessionToken, workspaceID, project.ID, createProposal.ID, "accept generated case")
	if createdFromProposal.Proposal.Status != "applied" || createdFromProposal.TestCase == nil || createdFromProposal.TestCase.Status != "ready" {
		t.Fatalf("unexpected applied create proposal result: %+v", createdFromProposal)
	}
	updateProposal := findTestCaseProposal(t, proposals, "Refine password login case")
	updatedFromProposal := applyProjectTestCaseProposalViaHTTP(t, router, sessionToken, workspaceID, project.ID, updateProposal.ID, "accept refinement")
	if updatedFromProposal.Proposal.Status != "applied" || updatedFromProposal.TestCase == nil || updatedFromProposal.TestCase.ID != existingCase.ID || updatedFromProposal.TestCase.Priority != "p0" {
		t.Fatalf("unexpected applied update proposal result: %+v", updatedFromProposal)
	}
	revisions := listProjectTestCaseRevisionsViaHTTP(t, router, sessionToken, workspaceID, project.ID, existingCase.ID)
	if len(revisions) != 2 || revisions[0].RevisionNumber != 2 {
		t.Fatalf("expected update proposal to create a second revision, got %+v", revisions)
	}

	plan := createProjectTestPlanViaHTTP(t, router, sessionToken, workspaceID, project.ID, fmt.Sprintf(`{
			"title":"rc4 functional test plan",
			"description":"Functional regression for auth.",
			"setupSteps":"1. Confirm the release namespace.\n2. Update the target deployment image.\n3. Verify the preview URL is reachable.",
			"status":"ready",
			"targetType":"branch",
			"targetValue":"release/rc4",
		"environment":"personal desktop",
		"caseIds":[%q,%q]
	}`, existingCase.ID, createdFromProposal.TestCase.ID))
	if plan.Plan.Status != "ready" || plan.Plan.CaseCount != 2 || len(plan.Cases) != 2 || !strings.Contains(plan.Plan.SetupSteps, "Update the target deployment image") {
		t.Fatalf("unexpected created plan: %+v", plan)
	}
	if plan.Cases[0].SortOrder != 1 || plan.Cases[1].SortOrder != 2 || plan.Cases[0].TestCaseID != existingCase.ID || plan.Cases[1].TestCaseID != createdFromProposal.TestCase.ID {
		t.Fatalf("expected plan case order to match selection order, got %+v", plan.Cases)
	}

	run := startProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, plan.Plan.ID, `{"runtimeMode":"personal","agentEngine":"codex","batchSize":1,"resultLocale":"zh-CN"}`)
	if run.Run.Status != "setup_running" || run.Run.SetupStatus != "running" || run.Run.ResultLocale != "zh-CN" || run.Run.SetupIssueID == "" || run.Run.SetupSessionID == "" || run.Run.TotalCount != 2 || run.Run.ParentIssueID == "" || len(run.Items) != 2 {
		t.Fatalf("unexpected started run: %+v", run)
	}
	if run.Items[0].SortOrder != 1 || run.Items[1].SortOrder != 2 || run.Items[0].TestCaseID != existingCase.ID || run.Items[1].TestCaseID != createdFromProposal.TestCase.ID {
		t.Fatalf("expected run items to freeze plan order, got %+v", run.Items)
	}
	parentIssue, err := store.GetIssue(context.Background(), user.ID, workspaceID, run.Run.ParentIssueID)
	if err != nil {
		t.Fatalf("get run parent issue: %v", err)
	}
	if parentIssue.Issue.Title != "Test run: rc4 functional test plan" {
		t.Fatalf("unexpected parent issue: %+v", parentIssue.Issue)
	}
	defaultIssues = listIssuesViaHTTP(t, router, sessionToken, workspaceID)
	if issueListContains(defaultIssues, run.Run.ParentIssueID) {
		t.Fatalf("default issue list should hide test run parent issue %s, got %+v", run.Run.ParentIssueID, defaultIssues)
	}
	allTopLevelIssues = listIssuesViaHTTPWithQuery(t, router, sessionToken, workspaceID, "includeTestAutomation=1")
	if !issueListContains(allTopLevelIssues, run.Run.ParentIssueID) {
		t.Fatalf("includeTestAutomation should expose test run parent issue %s, got %+v", run.Run.ParentIssueID, allTopLevelIssues)
	}
	setupIssue, err := store.GetIssue(context.Background(), user.ID, workspaceID, run.Run.SetupIssueID)
	if err != nil {
		t.Fatalf("get setup issue: %v", err)
	}
	if setupIssue.Issue.ParentIssueID != run.Run.ParentIssueID || !strings.Contains(setupIssue.Issue.Body, "test-setup-result.json") || !strings.Contains(setupIssue.Issue.Body, "Use Simplified Chinese") {
		t.Fatalf("unexpected setup issue: %+v", setupIssue.Issue)
	}
	for _, item := range run.Items {
		if item.Status != "queued" || item.ExecutionIssueID != "" || item.AgentSessionID != "" {
			t.Fatalf("expected queued item before setup passes, got %+v", item)
		}
	}
	setupResult := fmt.Sprintf(`{"status":"completed","result":{"exitCode":0,"testSetup":{"runId":%q,"status":"passed","summary":"Preview is ready.","outputs":{"previewUrl":"https://rc4.example.test","image":"registry.example/mspace:rc4"},"steps":[{"title":"Update deployment image","status":"passed","command":"kubectl set image deployment/mspace app=registry.example/mspace:rc4","summary":"Deployment updated."}]}}}`, run.Run.ID)
	if _, err := completeSessionTaskInMemoryStore(t, store, worker.ID, run.Run.SetupSessionID, setupResult); err != nil {
		t.Fatalf("complete setup task: %v", err)
	}
	run = getProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, run.Run.ID)
	if run.Run.Status != "running" || run.Run.SetupStatus != "passed" || !strings.Contains(string(run.Run.RunContext), "rc4.example.test") {
		t.Fatalf("expected setup to pass and start case execution, got %+v", run.Run)
	}
	for _, item := range run.Items {
		if item.Status != "running" || item.ExecutionIssueID == "" || item.AgentSessionID == "" {
			t.Fatalf("expected running item with execution issue and session after setup, got %+v", item)
		}
		childIssue, err := store.GetIssue(context.Background(), user.ID, workspaceID, item.ExecutionIssueID)
		if err != nil {
			t.Fatalf("get child issue: %v", err)
		}
		if childIssue.Issue.ParentIssueID != run.Run.ParentIssueID {
			t.Fatalf("expected child issue under parent %s, got %+v", run.Run.ParentIssueID, childIssue.Issue)
		}
		if !strings.Contains(childIssue.Issue.Body, "Use Simplified Chinese") {
			t.Fatalf("execution issue should preserve result locale instruction, got body %q", childIssue.Issue.Body)
		}
		if issueListContains(allTopLevelIssues, item.ExecutionIssueID) {
			t.Fatalf("top-level issue list should still hide execution child issue %s, got %+v", item.ExecutionIssueID, allTopLevelIssues)
		}
		sessionDetail, err := store.GetSession(context.Background(), user.ID, workspaceID, item.AgentSessionID)
		if err != nil {
			t.Fatalf("get item agent session: %v", err)
		}
		if !strings.Contains(sessionDetail.Session.Command, "Use Simplified Chinese") {
			t.Fatalf("execution session command should preserve result locale instruction, got %q", sessionDetail.Session.Command)
		}
	}

	firstItem := run.Items[0]
	secondItem := run.Items[1]
	firstResult := fmt.Sprintf(`{"status":"completed","result":{"exitCode":0,"testResult":{"runId":%q,"items":[{"caseId":%q,"status":"passed","actualResult":"Passed in Codex.","evidence":{"commands":["pnpm test"]}}]}}}`, run.Run.ID, firstItem.TestCaseID)
	if _, err := completeSessionTaskInMemoryStore(t, store, worker.ID, firstItem.AgentSessionID, firstResult); err != nil {
		t.Fatalf("complete first result task: %v", err)
	}
	partialRun := getProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, run.Run.ID)
	if partialRun.Run.Status != "running" || partialRun.Run.PassedCount != 1 || partialRun.Run.FailedCount != 0 {
		t.Fatalf("unexpected partial run counts: %+v", partialRun.Run)
	}
	secondResult := fmt.Sprintf(`{"status":"completed","result":{"exitCode":0,"testResult":{"runId":%q,"items":[{"caseId":%q,"status":"failed","actualResult":"The logout button did not return to sign-in.","failureSummary":"Logout stayed on workspace.","evidence":{"screenshot":"logout-failed.png"}}]}}}`, run.Run.ID, secondItem.TestCaseID)
	if _, err := completeSessionTaskInMemoryStore(t, store, worker.ID, secondItem.AgentSessionID, secondResult); err != nil {
		t.Fatalf("complete second result task: %v", err)
	}
	completedRun := getProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, run.Run.ID)
	if completedRun.Run.Status != "needs_acceptance" || completedRun.Run.PassedCount != 1 || completedRun.Run.FailedCount != 1 || completedRun.Run.CompletedAt == "" {
		t.Fatalf("expected completed run to need acceptance, got %+v", completedRun.Run)
	}
	failedItem := findTestRunItem(t, completedRun.Items, secondItem.TestCaseID)
	if failedItem.Status != "failed" || failedItem.FailureSummary != "Logout stayed on workspace." || !strings.Contains(string(failedItem.Evidence), "logout-failed.png") {
		t.Fatalf("unexpected failed item: %+v", failedItem)
	}

	batchFallbackRun := startProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, plan.Plan.ID, `{"runtimeMode":"personal","agentEngine":"codex","batchSize":2,"resultLocale":"zh-CN"}`)
	setupBatchFallbackResult := fmt.Sprintf(`{"status":"completed","result":{"exitCode":0,"testSetup":{"runId":%q,"status":"passed","summary":"Preview is ready.","outputs":{"previewUrl":"https://rc5.example.test"}}}}`, batchFallbackRun.Run.ID)
	if _, err := completeSessionTaskInMemoryStore(t, store, worker.ID, batchFallbackRun.Run.SetupSessionID, setupBatchFallbackResult); err != nil {
		t.Fatalf("complete batch fallback setup task: %v", err)
	}
	batchFallbackRun = getProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, batchFallbackRun.Run.ID)
	if batchFallbackRun.Run.Status != "running" || len(batchFallbackRun.Items) != 2 {
		t.Fatalf("expected batch fallback run to start execution, got %+v", batchFallbackRun)
	}
	firstBatchItem := batchFallbackRun.Items[0]
	secondBatchItem := batchFallbackRun.Items[1]
	if firstBatchItem.AgentSessionID == "" || firstBatchItem.AgentSessionID != secondBatchItem.AgentSessionID {
		t.Fatalf("expected both batch items to share one execution session, got %+v", batchFallbackRun.Items)
	}
	batchBlockedResult := fmt.Sprintf(`{"status":"completed","result":{"exitCode":0,"testResult":{"runId":%q,"summary":"Batch execution stopped.","items":[{"caseId":%q,"status":"passed","actualResult":"First case passed before the batch blocker.","evidence":{"commands":["pnpm test -- first"]}},{"caseId":"batch","status":"blocked","actualResult":"Batch execution script aborted.","failureSummary":"TypeError: fetch failed","evidence":{"networkStatuses":[{"method":"GET","url":"https://192.168.0.62.nip.io/","status":503}]}}]}}}`, batchFallbackRun.Run.ID, firstBatchItem.TestCaseID)
	if _, err := completeSessionTaskInMemoryStore(t, store, worker.ID, secondBatchItem.AgentSessionID, batchBlockedResult); err != nil {
		t.Fatalf("complete batch blocker result: %v", err)
	}
	batchCompletedRun := getProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, batchFallbackRun.Run.ID)
	if batchCompletedRun.Run.Status != "needs_acceptance" || batchCompletedRun.Run.PassedCount != 1 || batchCompletedRun.Run.BlockedCount != 1 || batchCompletedRun.Run.CompletedAt == "" {
		t.Fatalf("expected batch fallback run to finish with one blocked item, got %+v", batchCompletedRun.Run)
	}
	preservedBatchItem := findTestRunItem(t, batchCompletedRun.Items, firstBatchItem.TestCaseID)
	if preservedBatchItem.Status != "passed" || preservedBatchItem.ActualResult != "First case passed before the batch blocker." {
		t.Fatalf("batch fallback should not overwrite already-final items, got %+v", preservedBatchItem)
	}
	blockedBatchItem := findTestRunItem(t, batchCompletedRun.Items, secondBatchItem.TestCaseID)
	if blockedBatchItem.Status != "blocked" || blockedBatchItem.FailureSummary != "TypeError: fetch failed" || !strings.Contains(string(blockedBatchItem.Evidence), "192.168.0.62.nip.io") {
		t.Fatalf("expected running batch item to be blocked from batch artifact, got %+v", blockedBatchItem)
	}

	adHocRun := startAdHocProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, fmt.Sprintf(`{"caseIds":[%q],"runtimeMode":"personal","agentEngine":"codex","batchSize":1}`, existingCase.ID))
	if adHocRun.Run.Status != "running" || adHocRun.Run.Source != "ad_hoc" || adHocRun.Run.PlanID != "" || adHocRun.Plan != nil || adHocRun.Run.TotalCount != 1 || len(adHocRun.Items) != 1 {
		t.Fatalf("unexpected direct started run: %+v", adHocRun)
	}
	adHocIssue, err := store.GetIssue(context.Background(), user.ID, workspaceID, adHocRun.Run.ParentIssueID)
	if err != nil {
		t.Fatalf("get direct run parent issue: %v", err)
	}
	if adHocIssue.Issue.Title != "Test run: "+adHocRun.Items[0].TestCase.Title {
		t.Fatalf("unexpected direct run parent issue: %+v", adHocIssue.Issue)
	}
	if issueListContains(listIssuesViaHTTP(t, router, sessionToken, workspaceID), adHocRun.Run.ParentIssueID) {
		t.Fatalf("default issue list should hide direct test run parent issue %s", adHocRun.Run.ParentIssueID)
	}
	adHocRuns := listProjectTestRunsViaHTTP(t, router, sessionToken, workspaceID, project.ID)
	if len(adHocRuns) < 2 {
		t.Fatalf("expected project run list to include plan and direct runs, got %+v", adHocRuns)
	}
	adHocResult := fmt.Sprintf(`{"status":"completed","result":{"exitCode":0,"testResult":{"runId":%q,"items":[{"caseId":%q,"status":"passed","actualResult":"Direct case passed.","evidence":{"screenshot":"homepage.png","commands":["pnpm test -- login"]}}]}}}`, adHocRun.Run.ID, existingCase.ID)
	if _, err := completeSessionTaskInMemoryStore(t, store, worker.ID, adHocRun.Items[0].AgentSessionID, adHocResult); err != nil {
		t.Fatalf("complete direct run result task: %v", err)
	}
	completedAdHocRun := getProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, adHocRun.Run.ID)
	if completedAdHocRun.Run.Status != "needs_acceptance" || completedAdHocRun.Run.PassedCount != 1 || completedAdHocRun.Plan != nil {
		t.Fatalf("unexpected completed direct run: %+v", completedAdHocRun)
	}
	casesAfterAdHocRun := listProjectTestCasesViaHTTP(t, router, sessionToken, workspaceID, project.ID)
	caseAfterAdHocRun := findTestCase(t, casesAfterAdHocRun, existingCase.ID)
	if caseAfterAdHocRun.LatestResult == nil || caseAfterAdHocRun.LatestResult.RunID != adHocRun.Run.ID || caseAfterAdHocRun.LatestResult.Status != "passed" || caseAfterAdHocRun.LatestResult.ActualResult != "Direct case passed." {
		t.Fatalf("expected case list to expose latest run result, got %+v", caseAfterAdHocRun.LatestResult)
	}
	if !strings.Contains(string(caseAfterAdHocRun.LatestResult.Evidence), "homepage.png") {
		t.Fatalf("expected case list latest result to expose evidence, got %s", caseAfterAdHocRun.LatestResult.Evidence)
	}
	detailAfterAdHocRun := getProjectTestCaseViaHTTP(t, router, sessionToken, workspaceID, project.ID, existingCase.ID)
	if detailAfterAdHocRun.LatestResult == nil || detailAfterAdHocRun.LatestResult.RunID != adHocRun.Run.ID || detailAfterAdHocRun.LatestResult.Status != "passed" {
		t.Fatalf("expected case detail to expose latest run result, got %+v", detailAfterAdHocRun.LatestResult)
	}
	if !strings.Contains(string(detailAfterAdHocRun.LatestResult.Evidence), "homepage.png") {
		t.Fatalf("expected case detail latest result to expose evidence, got %s", detailAfterAdHocRun.LatestResult.Evidence)
	}

	acceptedRun := reviewProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, run.Run.ID, "accept", "Accepted with known follow-up.")
	if acceptedRun.Status != "accepted" || acceptedRun.AcceptanceStatus != "accepted" || acceptedRun.AcceptanceNote != "Accepted with known follow-up." {
		t.Fatalf("unexpected accepted run: %+v", acceptedRun)
	}
	failingSetupRun := startProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, plan.Plan.ID, `{"runtimeMode":"personal","agentEngine":"codex","batchSize":2}`)
	if failingSetupRun.Run.Status != "setup_running" || failingSetupRun.Run.SetupSessionID == "" {
		t.Fatalf("expected second plan run to wait on setup, got %+v", failingSetupRun.Run)
	}
	failingSetupResult := fmt.Sprintf(`{"status":"completed","result":{"exitCode":1,"testSetup":{"runId":%q,"status":"failed","summary":"Deployment image update failed.","failureSummary":"Image tag rc4 was not found.","outputs":{},"steps":[{"title":"Update deployment image","status":"failed","command":"kubectl set image deployment/mspace app=missing","failureSummary":"Image tag rc4 was not found."}]}}}`, failingSetupRun.Run.ID)
	if _, err := completeSessionTaskInMemoryStore(t, store, worker.ID, failingSetupRun.Run.SetupSessionID, failingSetupResult); err != nil {
		t.Fatalf("complete failing setup task: %v", err)
	}
	failedSetupRun := getProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, failingSetupRun.Run.ID)
	if failedSetupRun.Run.Status != "setup_failed" || failedSetupRun.Run.SetupStatus != "failed" || !strings.Contains(string(failedSetupRun.Run.SetupResult), "Image tag rc4 was not found") {
		t.Fatalf("expected setup failure to stop run, got %+v", failedSetupRun.Run)
	}
	for _, item := range failedSetupRun.Items {
		if item.Status != "queued" || item.AgentSessionID != "" {
			t.Fatalf("setup failure should not start case execution, got %+v", item)
		}
	}
	failedTaskRun := startProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, plan.Plan.ID, `{"runtimeMode":"personal","agentEngine":"codex","batchSize":2}`)
	failedTaskResult := fmt.Sprintf(`{"status":"failed","error":"SSH command failed","result":{"exitCode":1,"testSetup":{"runId":%q,"status":"passed","summary":"Stale artifact should not pass."}}}`, failedTaskRun.Run.ID)
	if _, err := completeSessionTaskInMemoryStore(t, store, worker.ID, failedTaskRun.Run.SetupSessionID, failedTaskResult); err != nil {
		t.Fatalf("fail setup task: %v", err)
	}
	failedTaskRun = getProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, failedTaskRun.Run.ID)
	if failedTaskRun.Run.Status != "setup_failed" || failedTaskRun.Run.SetupStatus != "failed" || !strings.Contains(string(failedTaskRun.Run.SetupResult), "SSH command failed") {
		t.Fatalf("failed setup task should stop run even if artifact says passed, got %+v", failedTaskRun.Run)
	}
	for _, item := range failedTaskRun.Items {
		if item.Status != "queued" || item.AgentSessionID != "" {
			t.Fatalf("failed setup task should not start case execution, got %+v", item)
		}
	}
	cancelSetupRun := startProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, plan.Plan.ID, `{"runtimeMode":"personal","agentEngine":"codex","batchSize":2}`)
	cancelledSetupRun := cancelProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, cancelSetupRun.Run.ID, "User stopped setup.")
	if cancelledSetupRun.Run.Status != "cancelled" || cancelledSetupRun.Run.SetupStatus != "cancelled" || cancelledSetupRun.Run.CompletedAt == "" {
		t.Fatalf("expected setup run cancellation, got %+v", cancelledSetupRun.Run)
	}
	setupSession, err := store.GetSession(context.Background(), user.ID, workspaceID, cancelSetupRun.Run.SetupSessionID)
	if err != nil {
		t.Fatalf("get cancelled setup session: %v", err)
	}
	setupTask := mustRuntimeTaskBySession(t, store, workspaceID, cancelSetupRun.Run.SetupSessionID)
	if setupSession.Session.Status != "cancelled" || setupTask.Status != "cancelled" || !strings.Contains(setupTask.Error, "User stopped setup.") {
		t.Fatalf("expected cancelled setup session, got %+v", setupSession.Session)
	}
	for _, item := range cancelledSetupRun.Items {
		if item.Status != "cancelled" || !strings.Contains(item.FailureSummary, "User stopped setup.") {
			t.Fatalf("setup cancellation should cancel queued items, got %+v", item)
		}
	}
	lateSetupResult := fmt.Sprintf(`{"status":"completed","result":{"exitCode":0,"testSetup":{"runId":%q,"status":"passed","summary":"Too late."}}}`, cancelSetupRun.Run.ID)
	if _, err := completeSessionTaskInMemoryStore(t, store, worker.ID, cancelSetupRun.Run.SetupSessionID, lateSetupResult); err != nil {
		t.Fatalf("complete late setup task: %v", err)
	}
	cancelledSetupRun = getProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, cancelSetupRun.Run.ID)
	if cancelledSetupRun.Run.Status != "cancelled" || cancelledSetupRun.Run.SetupStatus != "cancelled" {
		t.Fatalf("late setup artifact should not revive cancelled run, got %+v", cancelledSetupRun.Run)
	}

	cancelExecutionRun := startProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, plan.Plan.ID, `{"runtimeMode":"personal","agentEngine":"codex","batchSize":2}`)
	cancelExecutionSetupResult := fmt.Sprintf(`{"status":"completed","result":{"exitCode":0,"testSetup":{"runId":%q,"status":"passed","summary":"Ready before cancellation.","outputs":{}}}}`, cancelExecutionRun.Run.ID)
	if _, err := completeSessionTaskInMemoryStore(t, store, worker.ID, cancelExecutionRun.Run.SetupSessionID, cancelExecutionSetupResult); err != nil {
		t.Fatalf("complete cancel-execution setup task: %v", err)
	}
	cancelExecutionRun = getProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, cancelExecutionRun.Run.ID)
	cancelledExecutionRun := cancelProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, cancelExecutionRun.Run.ID, "User stopped execution.")
	if cancelledExecutionRun.Run.Status != "cancelled" || cancelledExecutionRun.Run.SetupStatus != "passed" {
		t.Fatalf("expected execution run cancellation, got %+v", cancelledExecutionRun.Run)
	}
	for _, item := range cancelledExecutionRun.Items {
		if item.Status != "cancelled" || item.AgentSessionID == "" {
			t.Fatalf("execution cancellation should cancel running items while preserving session links, got %+v", item)
		}
		sessionDetail, err := store.GetSession(context.Background(), user.ID, workspaceID, item.AgentSessionID)
		if err != nil {
			t.Fatalf("get cancelled execution session: %v", err)
		}
		task := mustRuntimeTaskBySession(t, store, workspaceID, item.AgentSessionID)
		if sessionDetail.Session.Status != "cancelled" || task.Status != "cancelled" || !strings.Contains(task.Error, "User stopped execution.") {
			t.Fatalf("expected cancelled execution session, got %+v", sessionDetail.Session)
		}
	}
	lateExecutionResult := fmt.Sprintf(`{"status":"completed","result":{"exitCode":0,"testResult":{"runId":%q,"items":[{"caseId":%q,"status":"passed","actualResult":"Too late."}]}}}`, cancelExecutionRun.Run.ID, cancelExecutionRun.Items[0].TestCaseID)
	if _, err := completeSessionTaskInMemoryStore(t, store, worker.ID, cancelExecutionRun.Items[0].AgentSessionID, lateExecutionResult); err != nil {
		t.Fatalf("complete late execution task: %v", err)
	}
	cancelledExecutionRun = getProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, cancelExecutionRun.Run.ID)
	if cancelledExecutionRun.Run.Status != "cancelled" {
		t.Fatalf("late execution artifact should not revive cancelled run, got %+v", cancelledExecutionRun.Run)
	}
	for _, item := range cancelledExecutionRun.Items {
		if item.Status != "cancelled" {
			t.Fatalf("late execution artifact should not overwrite cancelled item, got %+v", item)
		}
	}
	blockedRun := startProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, plan.Plan.ID, `{"runtimeMode":"personal","agentEngine":"codex","batchSize":2}`)
	blockedSetupResult := fmt.Sprintf(`{"status":"completed","result":{"exitCode":0,"testSetup":{"runId":%q,"status":"passed","summary":"Ready for blocked review.","outputs":{}}}}`, blockedRun.Run.ID)
	if _, err := completeSessionTaskInMemoryStore(t, store, worker.ID, blockedRun.Run.SetupSessionID, blockedSetupResult); err != nil {
		t.Fatalf("complete blocked-run setup task: %v", err)
	}
	blockedReview := reviewProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, project.ID, blockedRun.Run.ID, "block", "Blocked by release environment.")
	if blockedReview.Status != "blocked" || blockedReview.AcceptanceStatus != "blocked" || blockedReview.AcceptanceNote != "Blocked by release environment." {
		t.Fatalf("unexpected blocked run: %+v", blockedReview)
	}
}

func TestWorkspaceTestPlanCanSpanProjects(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "workspace-test-plan-user",
		Login:          "workspace-test-plan-user",
		Name:           "Workspace Test Plan User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()
	firstProject := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "workspace-plan-first")
	secondProject := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "workspace-plan-second")
	_, _ = registerTestRuntimeWorker(t, router, sessionToken, workspaceID)

	firstCase := createProjectTestCaseViaHTTP(t, router, sessionToken, workspaceID, firstProject.ID, `{
		"title":"First project smoke",
		"area":"first",
		"status":"ready",
		"steps":[{"action":"Run first smoke","expected":"First project passes."}],
		"expectedResult":"First project remains healthy."
	}`)
	secondCase := createProjectTestCaseViaHTTP(t, router, sessionToken, workspaceID, secondProject.ID, `{
		"title":"Second project smoke",
		"area":"second",
		"status":"ready",
		"steps":[{"action":"Run second smoke","expected":"Second project passes."}],
		"expectedResult":"Second project remains healthy."
	}`)

	plan := createWorkspaceTestPlanViaHTTP(t, router, sessionToken, workspaceID, fmt.Sprintf(`{
		"title":"Cross-project smoke plan",
		"status":"ready",
		"targetType":"branch",
		"targetValue":"main",
		"cases":[
			{"projectId":%q,"caseId":%q},
			{"projectId":%q,"caseId":%q}
		]
	}`, firstProject.ID, firstCase.ID, secondProject.ID, secondCase.ID))
	if plan.Plan.CaseCount != 2 || len(plan.Cases) != 2 {
		t.Fatalf("expected two cases in workspace plan, got %+v", plan)
	}
	if plan.Cases[0].ProjectID != firstProject.ID || plan.Cases[1].ProjectID != secondProject.ID || plan.Cases[0].SortOrder != 1 || plan.Cases[1].SortOrder != 2 {
		t.Fatalf("expected plan cases to preserve project ids, got %+v", plan.Cases)
	}

	workspacePlans := listWorkspaceTestPlansViaHTTP(t, router, sessionToken, workspaceID)
	if len(workspacePlans) != 1 || workspacePlans[0].ID != plan.Plan.ID {
		t.Fatalf("workspace plan list should include cross-project plan, got %+v", workspacePlans)
	}
	firstProjectPlans := listProjectTestPlansViaHTTP(t, router, sessionToken, workspaceID, firstProject.ID)
	secondProjectPlans := listProjectTestPlansViaHTTP(t, router, sessionToken, workspaceID, secondProject.ID)
	if len(firstProjectPlans) != 1 || len(secondProjectPlans) != 1 || firstProjectPlans[0].ID != plan.Plan.ID || secondProjectPlans[0].ID != plan.Plan.ID {
		t.Fatalf("project compatibility plan lists should include shared plan, first=%+v second=%+v", firstProjectPlans, secondProjectPlans)
	}

	run := startWorkspaceTestRunViaHTTP(t, router, sessionToken, workspaceID, plan.Plan.ID, `{"runtimeMode":"personal","agentEngine":"codex","batchSize":1}`)
	if run.Run.TotalCount != 2 || len(run.Items) != 2 || run.Run.ProjectID != firstProject.ID {
		t.Fatalf("unexpected workspace run: %+v", run)
	}
	if run.Items[0].SortOrder != 1 || run.Items[1].SortOrder != 2 || run.Items[0].TestCaseID != firstCase.ID || run.Items[1].TestCaseID != secondCase.ID {
		t.Fatalf("expected workspace run items to preserve plan order, got %+v", run.Items)
	}
	firstItem := findTestRunItem(t, run.Items, firstCase.ID)
	secondItem := findTestRunItem(t, run.Items, secondCase.ID)
	if firstItem.ProjectID != firstProject.ID || secondItem.ProjectID != secondProject.ID {
		t.Fatalf("run items should preserve each case project, first=%+v second=%+v", firstItem, secondItem)
	}
	if firstItem.ExecutionIssueID == "" || secondItem.ExecutionIssueID == "" || firstItem.AgentSessionID == "" || secondItem.AgentSessionID == "" {
		t.Fatalf("expected execution sessions for both project batches, first=%+v second=%+v", firstItem, secondItem)
	}
	firstIssue, err := store.GetIssue(context.Background(), user.ID, workspaceID, firstItem.ExecutionIssueID)
	if err != nil {
		t.Fatalf("get first execution issue: %v", err)
	}
	secondIssue, err := store.GetIssue(context.Background(), user.ID, workspaceID, secondItem.ExecutionIssueID)
	if err != nil {
		t.Fatalf("get second execution issue: %v", err)
	}
	if firstIssue.Issue.ProjectID != firstProject.ID || secondIssue.Issue.ProjectID != secondProject.ID {
		t.Fatalf("execution child issues should use batch project ids, first=%+v second=%+v", firstIssue.Issue, secondIssue.Issue)
	}

	workspaceRun := getWorkspaceTestRunViaHTTP(t, router, sessionToken, workspaceID, run.Run.ID)
	if len(workspaceRun.Items) != 2 {
		t.Fatalf("workspace run detail should include both projects, got %+v", workspaceRun)
	}
	firstProjectRun := getProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, firstProject.ID, run.Run.ID)
	secondProjectRun := getProjectTestRunViaHTTP(t, router, sessionToken, workspaceID, secondProject.ID, run.Run.ID)
	if len(firstProjectRun.Items) != 2 || len(secondProjectRun.Items) != 2 {
		t.Fatalf("project compatibility detail should return full shared run, first=%+v second=%+v", firstProjectRun, secondProjectRun)
	}
	workspaceRuns := listWorkspaceTestRunsViaHTTP(t, router, sessionToken, workspaceID)
	if len(workspaceRuns) != 1 || workspaceRuns[0].ID != run.Run.ID {
		t.Fatalf("workspace run list should include shared run, got %+v", workspaceRuns)
	}
	firstProjectRuns := listProjectTestRunsViaHTTP(t, router, sessionToken, workspaceID, firstProject.ID)
	secondProjectRuns := listProjectTestRunsViaHTTP(t, router, sessionToken, workspaceID, secondProject.ID)
	if len(firstProjectRuns) != 1 || len(secondProjectRuns) != 1 || firstProjectRuns[0].ID != run.Run.ID || secondProjectRuns[0].ID != run.Run.ID {
		t.Fatalf("project compatibility run lists should include shared run, first=%+v second=%+v", firstProjectRuns, secondProjectRuns)
	}
}

func TestIssueTypeTriageRuntimeTaskResultAppliesGeneratedTitleAndLabel(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "triage-worker-user",
		Login:          "triage-worker-user",
		Name:           "Triage Worker User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	issueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{Body: "Fix worker-backed issue type triage"})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	createTokenRecorder := httptest.NewRecorder()
	createTokenReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/runtime-registration-tokens", strings.NewReader(`{"name":"triage worker","expiresInHours":12}`))
	createTokenReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createTokenRecorder, createTokenReq)
	if createTokenRecorder.Code != http.StatusCreated {
		t.Fatalf("create runtime token status=%d body=%s", createTokenRecorder.Code, createTokenRecorder.Body.String())
	}
	var tokenResult RuntimeRegistrationTokenResult
	if err := json.Unmarshal(createTokenRecorder.Body.Bytes(), &tokenResult); err != nil {
		t.Fatalf("parse runtime token: %v", err)
	}

	registerRecorder := httptest.NewRecorder()
	registerReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/register", strings.NewReader(`{"name":"triage-worker","mode":"personal","version":"0.1.0","capabilities":{"codex":true},"labels":{"host":"test"}}`))
	registerReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(registerRecorder, registerReq)
	if registerRecorder.Code != http.StatusCreated {
		t.Fatalf("register worker status=%d body=%s", registerRecorder.Code, registerRecorder.Body.String())
	}
	var worker RuntimeWorker
	if err := json.Unmarshal(registerRecorder.Body.Bytes(), &worker); err != nil {
		t.Fatalf("parse worker: %v", err)
	}

	if err := server.enqueueIssueTypeTriage(context.Background(), user.ID, workspaceID, issueID, "Fix worker-backed issue type triage"); err != nil {
		t.Fatalf("enqueue issue type triage: %v", err)
	}

	claimRecorder := httptest.NewRecorder()
	claimReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/claim", nil)
	claimReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(claimRecorder, claimReq)
	if claimRecorder.Code != http.StatusOK {
		t.Fatalf("claim triage task status=%d body=%s", claimRecorder.Code, claimRecorder.Body.String())
	}
	var task RuntimeTask
	if err := json.Unmarshal(claimRecorder.Body.Bytes(), &task); err != nil {
		t.Fatalf("parse claimed triage task: %v", err)
	}
	if task.Kind != "issue_type_triage" || task.IssueID != issueID || task.RuntimeMode != "personal" || !strings.Contains(string(task.RequiredCapabilities), `"codex":true`) {
		t.Fatalf("unexpected triage task: %+v", task)
	}
	if !strings.Contains(string(task.Payload), `"expectedTitle":"Fix worker-backed issue type triage"`) {
		t.Fatalf("expected triage task to capture the draft title, got %s", task.Payload)
	}

	runningRecorder := httptest.NewRecorder()
	runningReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/"+task.ID+"/status", strings.NewReader(`{"status":"running"}`))
	runningReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(runningRecorder, runningReq)
	if runningRecorder.Code != http.StatusOK {
		t.Fatalf("running update status=%d body=%s", runningRecorder.Code, runningRecorder.Body.String())
	}

	completedRecorder := httptest.NewRecorder()
	completedReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/"+task.ID+"/status", strings.NewReader(`{"status":"completed","result":{"title":"**Classify issue types through runtime workers**","type":"fix","confidence":0.91,"reason":"bug fix"}}`))
	completedReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(completedRecorder, completedReq)
	if completedRecorder.Code != http.StatusOK {
		t.Fatalf("completed update status=%d body=%s", completedRecorder.Code, completedRecorder.Body.String())
	}

	detail, err := store.GetIssue(context.Background(), user.ID, workspaceID, issueID)
	if err != nil {
		t.Fatalf("get triaged issue: %v", err)
	}
	if detail.Issue.TriageStatus != "classified" {
		t.Fatalf("expected classified triage, got %q", detail.Issue.TriageStatus)
	}
	if len(detail.Labels) != 1 || detail.Labels[0].Key != "type:fix" {
		t.Fatalf("expected type:fix label, got %+v", detail.Labels)
	}
	if detail.Issue.Title != "Classify issue types through runtime workers" {
		t.Fatalf("expected generated plain title to replace the unchanged draft, got %q", detail.Issue.Title)
	}
}

func TestListSkillsReturnsExpectedBuiltinCatalog(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "skills-user",
		Login:          "skills-user",
		Name:           "Skills User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/skills", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list skills status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var skills []SkillCatalogItem
	if err := json.Unmarshal(recorder.Body.Bytes(), &skills); err != nil {
		t.Fatalf("parse skills: %v", err)
	}
	bySlug := make(map[string]SkillCatalogItem, len(skills))
	for _, skill := range skills {
		bySlug[skill.Slug] = skill
	}
	for _, slug := range []string{issueAnalysisSkillSlug, "codex-dynamic-workflows"} {
		skill, ok := bySlug[slug]
		if !ok || !skill.BuiltIn || skill.Source != builtinSkillSource || skill.SourceType != skillSourceTypeBuiltin || !skill.Enabled || !skill.Invocable || skill.FileCount == 0 || !strings.HasPrefix(skill.ContentHash, "sha256:") || skill.Revision == "" {
			t.Fatalf("expected built-in %s skill, got %+v from %+v", slug, skill, skills)
		}
	}
}

func TestSkillManagementCRUDAndBuiltinControls(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "skill-manager",
		Login:          "skill-manager",
		Name:           "Skill Manager",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	createBody := `{
		"slug":"review-plan",
		"name":"Review Plan",
		"description":"Review implementation plans.",
		"enabled":true,
		"invocable":true,
		"files":[{"path":"SKILL.md","content":"---\nname: Review Plan\ndescription: Review implementation plans.\n---\n# Review Plan\n"}]
	}`
	createRecorder := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/skills", strings.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createRecorder, createReq)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create skill status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	var created SkillDetail
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse created skill: %v", err)
	}
	if created.ID == "" || created.Slug != "review-plan" || created.BuiltIn || !created.Editable || !created.Deletable || len(created.Files) != 1 || created.Revision == "" {
		t.Fatalf("unexpected created skill: %+v", created)
	}
	firstRevision := created.Revision

	conflictRecorder := httptest.NewRecorder()
	conflictReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/skills", strings.NewReader(`{
		"slug":"`+created.ID+`",
		"name":"Conflicting Skill",
		"files":[{"path":"SKILL.md","content":"# conflict"}]
	}`))
	conflictReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(conflictRecorder, conflictReq)
	if conflictRecorder.Code != http.StatusConflict {
		t.Fatalf("id-like slug conflict status=%d body=%s existing=%+v", conflictRecorder.Code, conflictRecorder.Body.String(), created)
	}

	updateBody := `{
		"name":"Review Plan v2",
		"description":"Updated plan review skill.",
		"enabled":false,
		"invocable":false,
		"files":[{"path":"SKILL.md","content":"---\nname: Review Plan v2\ndescription: Updated plan review skill.\n---\n# Review Plan v2\n"}]
	}`
	updateRecorder := httptest.NewRecorder()
	updateReq := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+workspaceID+"/skills/"+created.ID, strings.NewReader(updateBody))
	updateReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(updateRecorder, updateReq)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update skill status=%d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	var updated SkillDetail
	if err := json.Unmarshal(updateRecorder.Body.Bytes(), &updated); err != nil {
		t.Fatalf("parse updated skill: %v", err)
	}
	if updated.Name != "Review Plan v2" || updated.Enabled || updated.Invocable || updated.Revision == firstRevision {
		t.Fatalf("unexpected updated skill: %+v firstRevision=%s", updated, firstRevision)
	}

	builtinRecorder := httptest.NewRecorder()
	builtinReq := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+workspaceID+"/skills/"+issueAnalysisSkillSlug, strings.NewReader(`{"enabled":true,"invocable":false}`))
	builtinReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(builtinRecorder, builtinReq)
	if builtinRecorder.Code != http.StatusOK {
		t.Fatalf("update built-in skill status=%d body=%s", builtinRecorder.Code, builtinRecorder.Body.String())
	}
	var builtin SkillDetail
	if err := json.Unmarshal(builtinRecorder.Body.Bytes(), &builtin); err != nil {
		t.Fatalf("parse built-in skill: %v", err)
	}
	if !builtin.BuiltIn || !builtin.Enabled || builtin.Invocable || builtin.Editable || builtin.Deletable || len(builtin.Files) == 0 {
		t.Fatalf("unexpected built-in skill setting: %+v", builtin)
	}

	builtinEnabledRecorder := httptest.NewRecorder()
	builtinEnabledReq := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+workspaceID+"/skills/"+issueAnalysisSkillSlug, strings.NewReader(`{"enabled":false}`))
	builtinEnabledReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(builtinEnabledRecorder, builtinEnabledReq)
	if builtinEnabledRecorder.Code != http.StatusOK {
		t.Fatalf("partial built-in skill update status=%d body=%s", builtinEnabledRecorder.Code, builtinEnabledRecorder.Body.String())
	}
	var builtinEnabled SkillDetail
	if err := json.Unmarshal(builtinEnabledRecorder.Body.Bytes(), &builtinEnabled); err != nil {
		t.Fatalf("parse partial built-in skill: %v", err)
	}
	if builtinEnabled.Enabled || builtinEnabled.Invocable {
		t.Fatalf("expected partial update to preserve invocable=false, got %+v", builtinEnabled)
	}

	duplicateRecorder := httptest.NewRecorder()
	duplicateReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/skills/"+issueAnalysisSkillSlug+"/duplicate", strings.NewReader(`{"slug":"think-copy","name":"Think Copy"}`))
	duplicateReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(duplicateRecorder, duplicateReq)
	if duplicateRecorder.Code != http.StatusCreated {
		t.Fatalf("duplicate skill status=%d body=%s", duplicateRecorder.Code, duplicateRecorder.Body.String())
	}
	var duplicated SkillDetail
	if err := json.Unmarshal(duplicateRecorder.Body.Bytes(), &duplicated); err != nil {
		t.Fatalf("parse duplicated skill: %v", err)
	}
	if duplicated.Slug != "think-copy" || duplicated.BuiltIn || len(duplicated.Files) == 0 {
		t.Fatalf("unexpected duplicated skill: %+v", duplicated)
	}

	deleteRecorder := httptest.NewRecorder()
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/workspaces/"+workspaceID+"/skills/"+created.ID, nil)
	deleteReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(deleteRecorder, deleteReq)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete skill status=%d body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}

	listRecorder := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/skills", nil)
	listReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list skills status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var skills []SkillCatalogItem
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &skills); err != nil {
		t.Fatalf("parse skills: %v", err)
	}
	bySlug := map[string]SkillCatalogItem{}
	for _, skill := range skills {
		bySlug[skill.Slug] = skill
	}
	if _, ok := bySlug["review-plan"]; ok {
		t.Fatalf("deleted custom skill should not be listed: %+v", bySlug["review-plan"])
	}
	if bySlug[issueAnalysisSkillSlug].Invocable {
		t.Fatalf("built-in think should be hidden from invocation: %+v", bySlug[issueAnalysisSkillSlug])
	}
	if !bySlug["think-copy"].Editable || bySlug["think-copy"].BuiltIn {
		t.Fatalf("duplicated skill should be editable custom skill: %+v", bySlug["think-copy"])
	}

	member, _, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "skill-member",
		Login:          "skill-member",
		Name:           "Skill Member",
	})
	if err != nil {
		t.Fatalf("upsert member identity: %v", err)
	}
	memberToken, _, err := store.CreateAuthSession(context.Background(), member.ID, time.Hour)
	if err != nil {
		t.Fatalf("create member auth session: %v", err)
	}
	store.mu.Lock()
	if store.workspaceMembers[workspaceID] == nil {
		store.workspaceMembers[workspaceID] = map[string]string{}
	}
	store.workspaceMembers[workspaceID][member.ID] = "member"
	store.addWorkspaceForUser(member.ID, workspaceID, "member")
	store.mu.Unlock()

	memberListRecorder := httptest.NewRecorder()
	memberListReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/skills", nil)
	memberListReq.Header.Set("Authorization", "Bearer "+memberToken)
	router.ServeHTTP(memberListRecorder, memberListReq)
	if memberListRecorder.Code != http.StatusOK {
		t.Fatalf("member list skills status=%d body=%s", memberListRecorder.Code, memberListRecorder.Body.String())
	}
	memberGetRecorder := httptest.NewRecorder()
	memberGetReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/skills/think-copy", nil)
	memberGetReq.Header.Set("Authorization", "Bearer "+memberToken)
	router.ServeHTTP(memberGetRecorder, memberGetReq)
	if memberGetRecorder.Code != http.StatusForbidden {
		t.Fatalf("member get skill detail status=%d body=%s", memberGetRecorder.Code, memberGetRecorder.Body.String())
	}
}

func TestCreateSkillRejectsUnsafeFilePath(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "unsafe-skill-path-manager",
		Login:          "unsafe-skill-path-manager",
		Name:           "Unsafe Skill Path Manager",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/skills", strings.NewReader("{\"slug\":\"bad-path\",\"files\":[{\"path\":\"SKILL.md\",\"content\":\"# ok\"},{\"path\":\"bad\\u0000name.md\",\"content\":\"bad\"}]}"))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unsafe path status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestBuiltinDynamicWorkflowSkillPreservesUpstreamAttribution(t *testing.T) {
	bundle, err := builtinSkillBundle("codex-dynamic-workflows", "latest")
	if err != nil {
		t.Fatalf("load codex dynamic workflows skill: %v", err)
	}
	files := make(map[string]string, len(bundle.Files))
	for _, file := range bundle.Files {
		files[file.Path] = file.Content
	}
	if !strings.Contains(files["SKILL.md"], "External skill from") || !strings.Contains(files["SKILL.md"], "github.com/DannyMac180/skills") {
		t.Fatalf("expected external upstream attribution in SKILL.md")
	}
	if !strings.Contains(files["LICENSE"], "Copyright (c) 2026 Dan McAteer") {
		t.Fatalf("expected upstream copyright in bundled LICENSE")
	}
}

func TestCreateAgentSessionAcceptsBuiltInSkillSlugs(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "session-skill-user",
		Login:          "session-skill-user",
		Name:           "Session Skill User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()
	project := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "session-skill-project")
	issueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{
		ProjectID: project.ID,
		Body:      "Use a skill from the issue composer.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	registerTestRuntimeWorkerWithCapabilities(t, router, sessionToken, workspaceID, `{"codex":true,"issueWorkingCopyV1":true}`)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/issues/"+issueID+"/sessions", strings.NewReader(`{
		"agentEngine":"codex",
		"command":"@codex #think produce a plan",
		"skillSlugs":["think","think"]
	}`))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create agent session status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var session AgentSession
	if err := json.Unmarshal(recorder.Body.Bytes(), &session); err != nil {
		t.Fatalf("parse session: %v", err)
	}
	if len(session.RequiredSkills) != 1 || session.RequiredSkills[0].Slug != issueAnalysisSkillSlug {
		t.Fatalf("expected compact think skill reference, got %+v", session.RequiredSkills)
	}
	if strings.Contains(string(session.Payload), "SKILL.md") || strings.Contains(string(session.Payload), `"files"`) {
		t.Fatalf("session response should not expose full skill bundle: %s", session.Payload)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	var task RuntimeTask
	for _, candidate := range store.runtimeTasks {
		if candidate.SessionID == session.ID {
			task = candidate
			break
		}
	}
	if task.ID == "" {
		t.Fatalf("expected runtime task for session %s", session.ID)
	}
	var payload struct {
		RequiredSkills []RuntimeSkillBundle `json:"requiredSkills"`
	}
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		t.Fatalf("parse runtime payload: %v", err)
	}
	if len(payload.RequiredSkills) != 1 || payload.RequiredSkills[0].Slug != issueAnalysisSkillSlug || len(payload.RequiredSkills[0].Files) == 0 {
		t.Fatalf("expected full think skill bundle in runtime payload, got %+v", payload.RequiredSkills)
	}
}

func TestCreateAgentSessionAcceptsWorkspaceCustomSkillSlugs(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "custom-session-skill-user",
		Login:          "custom-session-skill-user",
		Name:           "Custom Session Skill User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()
	custom, err := store.CreateSkill(context.Background(), user.ID, workspaceID, SkillInput{
		Slug:        "repo-map",
		Name:        "Repo Map",
		Description: "Map repository structure before implementation.",
		Enabled:     boolPointer(true),
		Invocable:   boolPointer(true),
		Files: []RuntimeSkillFile{{
			Path:    "SKILL.md",
			Content: "---\nname: Repo Map\ndescription: Map repository structure before implementation.\n---\n# Repo Map\n",
		}},
	})
	if err != nil {
		t.Fatalf("create custom skill: %v", err)
	}
	project := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "custom-session-skill-project")
	issueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{
		ProjectID: project.ID,
		Body:      "Use a workspace custom skill.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	registerTestRuntimeWorkerWithCapabilities(t, router, sessionToken, workspaceID, `{"codex":true,"issueWorkingCopyV1":true}`)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/issues/"+issueID+"/sessions", strings.NewReader(`{
		"agentEngine":"codex",
			"command":"@codex #repo-map inspect first",
		"skillSlugs":["repo-map"]
	}`))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create agent session status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var session AgentSession
	if err := json.Unmarshal(recorder.Body.Bytes(), &session); err != nil {
		t.Fatalf("parse session: %v", err)
	}
	if len(session.RequiredSkills) != 1 || session.RequiredSkills[0].Slug != custom.Slug || session.RequiredSkills[0].BuiltIn {
		t.Fatalf("expected compact custom skill reference, got %+v", session.RequiredSkills)
	}

	store.mu.Lock()
	var task RuntimeTask
	for _, candidate := range store.runtimeTasks {
		if candidate.SessionID == session.ID {
			task = candidate
			break
		}
	}
	store.mu.Unlock()
	if task.ID == "" {
		t.Fatalf("expected runtime task for session %s", session.ID)
	}
	var payload struct {
		RequiredSkills []RuntimeSkillBundle `json:"requiredSkills"`
	}
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		t.Fatalf("parse runtime payload: %v", err)
	}
	if len(payload.RequiredSkills) != 1 || payload.RequiredSkills[0].Slug != "repo-map" || payload.RequiredSkills[0].BuiltIn || len(payload.RequiredSkills[0].Files) != 1 {
		t.Fatalf("expected full custom skill bundle in runtime payload, got %+v", payload.RequiredSkills)
	}
}

func TestCreateAgentSessionRejectsUnknownSkillSlug(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "unknown-session-skill-user",
		Login:          "unknown-session-skill-user",
		Name:           "Unknown Session Skill User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()
	project := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "unknown-session-skill-project")
	issueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{
		ProjectID: project.ID,
		Body:      "Try an unknown skill.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/issues/"+issueID+"/sessions", strings.NewReader(`{
		"agentEngine":"codex",
			"command":"@codex #missing-skill try this",
		"skillSlugs":["missing-skill"]
	}`))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown skill slug status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "enabled workspace skill") {
		t.Fatalf("expected enabled workspace skill error, got %s", recorder.Body.String())
	}
}

func TestCreateAgentSessionRejectsDisabledWorkspaceSkillSlug(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "disabled-session-skill-user",
		Login:          "disabled-session-skill-user",
		Name:           "Disabled Session Skill User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()
	custom, err := store.CreateSkill(context.Background(), user.ID, workspaceID, SkillInput{
		Slug:    "disabled-skill",
		Name:    "Disabled Skill",
		Enabled: boolPointer(false),
		Files: []RuntimeSkillFile{{
			Path:    "SKILL.md",
			Content: "---\nname: Disabled Skill\n---\n# Disabled Skill\n",
		}},
	})
	if err != nil {
		t.Fatalf("create custom skill: %v", err)
	}
	project := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "disabled-session-skill-project")
	issueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{
		ProjectID: project.ID,
		Body:      "Try a disabled skill.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/issues/"+issueID+"/sessions", strings.NewReader(`{
		"agentEngine":"codex",
			"command":"@codex #disabled-skill try this",
		"skillSlugs":["disabled-skill"]
	}`))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("disabled skill status=%d body=%s custom=%+v", recorder.Code, recorder.Body.String(), custom)
	}
	if !strings.Contains(recorder.Body.String(), "disabled") {
		t.Fatalf("expected disabled skill error, got %s", recorder.Body.String())
	}
}

func TestCreateAgentSessionRejectsClientSkillBundles(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "client-session-skill-user",
		Login:          "client-session-skill-user",
		Name:           "Client Session Skill User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/issues/issue-0001/sessions", strings.NewReader(`{
		"provider":"codex",
		"command":"@codex use unsafe skill",
		"requiredSkills":[{"slug":"think","files":[{"path":"SKILL.md","content":"unsafe"}]}]
	}`))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("client skill bundle status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "server-managed") {
		t.Fatalf("expected server-managed error, got %s", recorder.Body.String())
	}

	mixedCaseRecorder := httptest.NewRecorder()
	mixedCaseReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/issues/issue-0001/sessions", strings.NewReader(`{
		"agentEngine":"codex",
		"command":"@codex use unsafe skill",
		"RequiredSkills":[{"slug":"think","files":[{"path":"SKILL.md","content":"unsafe"}]}]
	}`))
	mixedCaseReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(mixedCaseRecorder, mixedCaseReq)
	if mixedCaseRecorder.Code != http.StatusBadRequest || !strings.Contains(mixedCaseRecorder.Body.String(), "server-managed") {
		t.Fatalf("mixed-case client skill bundle status=%d body=%s", mixedCaseRecorder.Code, mixedCaseRecorder.Body.String())
	}
}

func TestCreateIssueQueuesAutomaticThinkAnalysis(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "issue-analysis-user",
		Login:          "issue-analysis-user",
		Name:           "Issue Analysis User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()
	tokenResult, worker := registerTestRuntimeWorkerWithCapabilities(t, router, sessionToken, workspaceID, `{"codex":true,"docker":true,"kubectl":true}`)
	project := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "issue-analysis")
	settings, err := store.UpdateWorkspaceSettings(context.Background(), user.ID, workspaceID, WorkspaceSettingsInput{AutoDeployTestEnvironment: true})
	if err != nil {
		t.Fatalf("enable auto deploy: %v", err)
	}
	if !settings.AutoDeployTestEnvironment {
		t.Fatalf("expected auto deploy setting enabled")
	}
	clusterResult, err := store.ImportKubeconfigs(context.Background(), user.ID, workspaceID, []string{"/tmp/mspace-issue-analysis-kubeconfig"})
	if err != nil {
		t.Fatalf("import kubeconfig: %v", err)
	}
	if len(clusterResult.Imported) != 1 {
		t.Fatalf("expected one imported cluster, got %+v", clusterResult)
	}
	project, err = store.UpdateProject(context.Background(), user.ID, workspaceID, project.ID, ProjectInput{
		Name:                 project.Name,
		SourceType:           "local",
		RepoPath:             project.RepoPath,
		DefaultBranch:        project.DefaultBranch,
		DefaultClusterID:     clusterResult.Imported[0].ID,
		DefaultEnvironmentID: clusterResult.Imported[0].ID,
	})
	if err != nil {
		t.Fatalf("attach default cluster: %v", err)
	}

	createRecorder := httptest.NewRecorder()
	createReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/issues", strings.NewReader(`{"projectId":"`+project.ID+`","body":"After creating an issue, analyze what Codex should do next."}`))
	createReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createRecorder, createReq)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create issue status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	var created struct {
		IssueID string `json:"issueId"`
	}
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse created issue: %v", err)
	}
	analysisTask := waitForIssueAnalysisTask(t, store, user.ID, workspaceID, created.IssueID)
	if analysisTask.Priority != 10 || analysisTask.Kind != "agent_session" || analysisTask.SessionID == "" {
		t.Fatalf("unexpected analysis task: %+v", analysisTask)
	}
	if !strings.Contains(string(analysisTask.RequiredCapabilities), `"codex":true`) {
		t.Fatalf("analysis task should require codex, got %s", analysisTask.RequiredCapabilities)
	}
	var payload struct {
		Automation     string               `json:"automation"`
		Prompt         string               `json:"prompt"`
		Sandbox        string               `json:"sandbox"`
		SourceCapture  bool                 `json:"sourceCapture"`
		RequiredSkills []RuntimeSkillBundle `json:"requiredSkills"`
	}
	if err := json.Unmarshal(analysisTask.Payload, &payload); err != nil {
		t.Fatalf("parse analysis payload: %v", err)
	}
	if payload.Automation != issueAnalysisAutomation || !strings.Contains(payload.Prompt, "read-only planning pass") {
		t.Fatalf("unexpected analysis payload: %+v", payload)
	}
	if payload.SourceCapture {
		t.Fatalf("issue analysis must disable source capture, got payload %+v", payload)
	}
	if payload.Sandbox != "read-only" {
		t.Fatalf("issue analysis must run with read-only sandbox, got %q", payload.Sandbox)
	}
	if len(payload.RequiredSkills) != 1 || payload.RequiredSkills[0].Slug != issueAnalysisSkillSlug || len(payload.RequiredSkills[0].Files) == 0 {
		t.Fatalf("expected think skill bundle, got %+v", payload.RequiredSkills)
	}
	if payload.RequiredSkills[0].ContentHash == "" || !strings.HasPrefix(payload.RequiredSkills[0].ContentHash, "sha256:") {
		t.Fatalf("expected bundle content hash, got %+v", payload.RequiredSkills[0])
	}
	if got, want := payload.RequiredSkills[0].ContentHash, workerCompatibleSkillBundleContentHash(payload.RequiredSkills[0].Files); got != want {
		t.Fatalf("bundle content hash must match worker digest: got %s want %s", got, want)
	}

	detail, err := store.GetIssue(context.Background(), user.ID, workspaceID, created.IssueID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if len(detail.Sessions) == 0 || detail.Sessions[0].Automation != issueAnalysisAutomation || len(detail.Sessions[0].RequiredSkills) != 1 || detail.Sessions[0].RequiredSkills[0].Slug != issueAnalysisSkillSlug {
		t.Fatalf("expected analysis session metadata, got %+v", detail.Sessions)
	}
	if strings.Contains(string(detail.Sessions[0].Payload), "SKILL.md") {
		t.Fatalf("session payload response should not expose full skill bundle: %s", detail.Sessions[0].Payload)
	}
	listRecorder := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/runtime-tasks?limit=20", nil)
	listReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list runtime tasks status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	if strings.Contains(listRecorder.Body.String(), `"files":[`) || strings.Contains(listRecorder.Body.String(), `"content":`) {
		t.Fatalf("runtime task list should redact full skill bundle: %s", listRecorder.Body.String())
	}
	var taskList RuntimeTaskListResult
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &taskList); err != nil {
		t.Fatalf("parse runtime task list: %v", err)
	}
	foundRedactedAnalysis := false
	for _, task := range taskList.Tasks {
		if task.IssueID != created.IssueID || task.Kind != "agent_session" || runtimeTaskAutomation(task) != issueAnalysisAutomation {
			continue
		}
		var publicPayload struct {
			RequiredSkills []AgentSessionSkillReference `json:"requiredSkills"`
		}
		if err := json.Unmarshal(task.Payload, &publicPayload); err != nil {
			t.Fatalf("parse redacted task payload: %v", err)
		}
		if len(publicPayload.RequiredSkills) != 1 || publicPayload.RequiredSkills[0].Slug != issueAnalysisSkillSlug {
			t.Fatalf("expected compact skill reference, got %s", task.Payload)
		}
		foundRedactedAnalysis = true
	}
	if !foundRedactedAnalysis {
		t.Fatalf("expected redacted analysis task in runtime task list, got %+v", taskList.Tasks)
	}
	if err := server.enqueueIssueAnalysis(context.Background(), user.ID, workspaceID, created.IssueID); !errors.Is(err, errIssueAnalysisNotNeeded) {
		t.Fatalf("duplicate issue analysis should be skipped, got %v", err)
	}
	titleTriageTask := waitForIssueTypeTriageTask(t, store, user.ID, workspaceID, created.IssueID)
	if !strings.Contains(string(titleTriageTask.Payload), `"expectedTitle":"After creating an issue, analyze what Codex should do next."`) {
		t.Fatalf("expected create path to capture the draft title before responding, got %s", titleTriageTask.Payload)
	}
	claimRecorder := httptest.NewRecorder()
	claimReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/claim", nil)
	claimReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(claimRecorder, claimReq)
	if claimRecorder.Code != http.StatusOK {
		t.Fatalf("claim runtime task status=%d body=%s", claimRecorder.Code, claimRecorder.Body.String())
	}
	var claimed RuntimeTask
	if err := json.Unmarshal(claimRecorder.Body.Bytes(), &claimed); err != nil {
		t.Fatalf("parse claimed task: %v", err)
	}
	if claimed.ID != analysisTask.ID || runtimeTaskAutomation(claimed) != issueAnalysisAutomation {
		t.Fatalf("expected worker to claim issue analysis first, got %+v", claimed)
	}
	maliciousResult := `{"status":"completed","result":{"exitCode":0,"source":{"commitSha":"9999999999999999999999999999999999999999","shortCommitSha":"999999999999","branch":"fix/should-not-capture","subject":"fix injected change","filesChanged":1,"changes":[],"diffPreview":"diff --git a/app.go b/app.go"},"testCaseProposals":{"summary":"should be ignored","proposals":[{"type":"create","title":"Injected case","proposedCase":{"title":"Injected case","area":"security","priority":"p1","status":"ready","preconditions":"none","steps":[{"action":"do it","expected":"done"}],"expectedResult":"done","tags":["injected"]}}]}}}`
	if _, err := completeSessionTaskInMemoryStore(t, store, worker.ID, analysisTask.SessionID, maliciousResult); err != nil {
		t.Fatalf("complete issue analysis with injected artifacts: %v", err)
	}
	proposals, err := store.ListProjectTestCaseProposals(context.Background(), user.ID, workspaceID, project.ID, TestCaseProposalListOptions{})
	if err != nil {
		t.Fatalf("list proposals: %v", err)
	}
	if len(proposals) != 0 {
		t.Fatalf("issue analysis must not store test-case proposals, got %+v", proposals)
	}
	tasksAfterCompletion, err := store.ListRuntimeTasks(context.Background(), user.ID, workspaceID)
	if err != nil {
		t.Fatalf("list tasks after completion: %v", err)
	}
	for _, task := range tasksAfterCompletion {
		if task.ID != analysisTask.ID && isAutoDeployTestEnvironmentTask(task) {
			t.Fatalf("issue analysis must not queue auto deploy, got %+v", task)
		}
	}
}

func TestCreateRuntimeTaskRejectsClientSkillBundles(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "client-skill-user",
		Login:          "client-skill-user",
		Name:           "Client Skill User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/runtime-tasks", strings.NewReader(`{
		"kind":"noop",
		"runtimeMode":"personal",
		"requiredCapabilities":{},
		"payload":{"requiredSkills":[{"slug":"think","files":[{"path":"SKILL.md","content":"unsafe"}]}]}
	}`))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("create runtime task status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "skill bundles are server-managed") {
		t.Fatalf("expected server-managed skill bundle error, got %s", recorder.Body.String())
	}
}

func TestCreateRuntimeTaskRejectsRawAgentSession(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "raw-agent-session-user",
		Login:          "raw-agent-session-user",
		Name:           "Raw Agent Session User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	server := NewServer(Config{}, store, fakeGitHubClient{})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaces[0].ID+"/runtime-tasks", strings.NewReader(`{
		"kind":"agent_session",
		"runtimeMode":"personal",
		"requiredCapabilities":{"pi":true},
		"payload":{"agentEngine":"pi","automation":"test_run_execution","workdir":"/tmp/attacker-controlled","env":{"DATABASE_URL":"exfiltrate"}}
	}`))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	server.Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("create raw agent session status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "agent_session") || !strings.Contains(recorder.Body.String(), "server-managed") {
		t.Fatalf("expected server-managed agent session error, got %s", recorder.Body.String())
	}
}

func TestCreateRuntimeTaskRejectsSystemKindsAndControlFields(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "raw-system-task-user",
		Login:          "raw-system-task-user",
		Name:           "Raw System Task User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	router := NewServer(Config{}, store, fakeGitHubClient{}).Routes()
	path := "/api/workspaces/" + workspaces[0].ID + "/runtime-tasks"

	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{name: "triage workflow", body: `{"kind":"issue_type_triage","runtimeMode":"personal","requiredCapabilities":{"codex":true},"payload":{}}`, want: "server-managed"},
		{name: "import mapping workflow", body: `{"kind":"test_case_import_mapping","runtimeMode":"personal","requiredCapabilities":{"codex":true},"payload":{}}`, want: "server-managed"},
		{name: "unknown executable kind", body: `{"kind":"custom_exec","runtimeMode":"personal","requiredCapabilities":{},"payload":{}}`, want: "server-managed"},
		{name: "internal flag injection", body: `{"kind":"issue_type_triage","runtimeMode":"personal","serverManaged":true,"requiredCapabilities":{"codex":true},"payload":{}}`, want: "server-managed"},
		{name: "issue binding", body: `{"issueId":"issue-1","kind":"noop","runtimeMode":"personal","requiredCapabilities":{},"payload":{}}`, want: "cannot bind"},
		{name: "automation payload", body: `{"kind":"noop","runtimeMode":"personal","requiredCapabilities":{},"payload":{"automation":"test_run_execution"}}`, want: "automation is server-managed"},
		{name: "mixed case automation payload", body: `{"kind":"noop","runtimeMode":"personal","requiredCapabilities":{},"payload":{"Automation":"test_run_execution"}}`, want: "automation is server-managed"},
		{name: "engine payload", body: `{"kind":"noop","runtimeMode":"personal","requiredCapabilities":{},"payload":{"agentEngine":"pi"}}`, want: "agentEngine is server-managed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer "+sessionToken)
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), test.want) {
				t.Fatalf("status=%d body=%s want=%q", recorder.Code, recorder.Body.String(), test.want)
			}
		})
	}
}

func TestCreateAgentSessionRejectsServerOwnedControlFields(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "session-control-field-user",
		Login:          "session-control-field-user",
		Name:           "Session Control Field User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	server := NewServer(Config{}, store, fakeGitHubClient{})
	for _, field := range []string{
		"runtimeMode", "provider", "agentProfile", "branch", "automation", "Automation",
		"testRunId", "sourceSessionId", "SourceSessionID", "sourceCommitSha", "executionMode",
		"workingCopy", "workingCopyGeneration", "expectedHeadSha", "initialize", "storageId",
		"storageAffinityId", "env", "workdir", "repository", "sourceCapture", "sandbox",
	} {
		t.Run(field, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			body := fmt.Sprintf(`{"agentEngine":"pi","command":"@pi inspect","%s":"attacker-controlled"}`, field)
			req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaces[0].ID+"/issues/issue-does-not-matter/sessions", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+sessionToken)
			server.Routes().ServeHTTP(recorder, req)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("create session with %s status=%d body=%s", field, recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), "is server-managed") {
				t.Fatalf("expected server-managed field error for %s, got %s", field, recorder.Body.String())
			}
		})
	}
}

func TestIssueAnalysisSkipsIssuesWithoutProject(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "issue-analysis-no-project-user",
		Login:          "issue-analysis-no-project-user",
		Name:           "Issue Analysis No Project User",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	issueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{Body: "No project yet"})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := server.enqueueIssueAnalysis(context.Background(), user.ID, workspaceID, issueID); !errors.Is(err, errIssueAnalysisNotNeeded) {
		t.Fatalf("analysis without a project should be skipped, got %v", err)
	}
}

func TestRuntimeWorkerRegistrationFlow(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "worker-owner",
		Login:          "worker-owner",
		Name:           "Worker Owner",
		Email:          "worker-owner@example.com",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	personalWorkspaceID := workspaces[0].ID
	teamWorkspace, _, err := store.CreateWorkspace(context.Background(), user.ID, CreateWorkspaceInput{Name: "Worker Team", Kind: "team"})
	if err != nil {
		t.Fatalf("create team workspace: %v", err)
	}
	workspaceID := teamWorkspace.ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	personalTokenRecorder := httptest.NewRecorder()
	personalTokenReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+personalWorkspaceID+"/runtime-registration-tokens", strings.NewReader(`{"name":"personal worker","expiresInHours":12}`))
	personalTokenReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(personalTokenRecorder, personalTokenReq)
	if personalTokenRecorder.Code != http.StatusCreated {
		t.Fatalf("create personal runtime token status=%d body=%s", personalTokenRecorder.Code, personalTokenRecorder.Body.String())
	}
	var personalTokenResult RuntimeRegistrationTokenResult
	if err := json.Unmarshal(personalTokenRecorder.Body.Bytes(), &personalTokenResult); err != nil {
		t.Fatalf("parse personal runtime token: %v", err)
	}
	if !strings.HasPrefix(personalTokenResult.Token, "msw_") || personalTokenResult.RegistrationToken.WorkspaceID != personalWorkspaceID {
		t.Fatalf("unexpected personal runtime token result: %+v", personalTokenResult)
	}

	personalTeamRegisterRecorder := httptest.NewRecorder()
	personalTeamRegisterReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/register", strings.NewReader(`{"name":"wrong-personal-worker","mode":"team","version":"0.1.0","capabilities":{"codex":true},"labels":{"host":"local"}}`))
	personalTeamRegisterReq.Header.Set("Authorization", "Bearer "+personalTokenResult.Token)
	router.ServeHTTP(personalTeamRegisterRecorder, personalTeamRegisterReq)
	if personalTeamRegisterRecorder.Code != http.StatusForbidden {
		t.Fatalf("personal workspace team worker should be forbidden, status=%d body=%s", personalTeamRegisterRecorder.Code, personalTeamRegisterRecorder.Body.String())
	}

	personalRegisterRecorder := httptest.NewRecorder()
	personalRegisterReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/register", strings.NewReader(`{"name":"personal-worker-1","mode":"personal","version":"0.1.0","capabilities":{"codex":true},"labels":{"host":"local"}}`))
	personalRegisterReq.Header.Set("Authorization", "Bearer "+personalTokenResult.Token)
	router.ServeHTTP(personalRegisterRecorder, personalRegisterReq)
	if personalRegisterRecorder.Code != http.StatusCreated {
		t.Fatalf("register personal worker status=%d body=%s", personalRegisterRecorder.Code, personalRegisterRecorder.Body.String())
	}
	var personalWorker RuntimeWorker
	if err := json.Unmarshal(personalRegisterRecorder.Body.Bytes(), &personalWorker); err != nil {
		t.Fatalf("parse personal worker: %v", err)
	}
	if personalWorker.ID == "" || personalWorker.WorkspaceID != personalWorkspaceID || personalWorker.Mode != "personal" {
		t.Fatalf("unexpected personal worker: %+v", personalWorker)
	}
	if string(personalWorker.AgentEngineDiagnostics) != "{}" {
		t.Fatalf("worker without diagnostics should return an empty object, got %s", personalWorker.AgentEngineDiagnostics)
	}

	createTokenRecorder := httptest.NewRecorder()
	createTokenReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/runtime-registration-tokens", strings.NewReader(`{"name":"team worker","expiresInHours":12}`))
	createTokenReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createTokenRecorder, createTokenReq)
	if createTokenRecorder.Code != http.StatusCreated {
		t.Fatalf("create runtime token status=%d body=%s", createTokenRecorder.Code, createTokenRecorder.Body.String())
	}
	var tokenResult RuntimeRegistrationTokenResult
	if err := json.Unmarshal(createTokenRecorder.Body.Bytes(), &tokenResult); err != nil {
		t.Fatalf("parse runtime token: %v", err)
	}
	if !strings.HasPrefix(tokenResult.Token, "msw_") || tokenResult.RegistrationToken.WorkspaceID != workspaceID {
		t.Fatalf("unexpected runtime token result: %+v", tokenResult)
	}

	listTokensRecorder := httptest.NewRecorder()
	listTokensReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/runtime-registration-tokens", nil)
	listTokensReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(listTokensRecorder, listTokensReq)
	if listTokensRecorder.Code != http.StatusOK {
		t.Fatalf("list runtime tokens status=%d body=%s", listTokensRecorder.Code, listTokensRecorder.Body.String())
	}
	var tokens []RuntimeRegistrationToken
	if err := json.Unmarshal(listTokensRecorder.Body.Bytes(), &tokens); err != nil {
		t.Fatalf("parse runtime tokens: %v", err)
	}
	if len(tokens) != 1 || tokens[0].ID != tokenResult.RegistrationToken.ID || tokens[0].TokenPrefix == "" {
		t.Fatalf("unexpected runtime tokens: %+v", tokens)
	}

	registerRecorder := httptest.NewRecorder()
	registerReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/register", strings.NewReader(`{"name":"team-worker-1","mode":"team","version":"0.1.0","capabilities":{"codex":true,"docker":true},"labels":{"region":"local"},"agentEngineDiagnostics":{"codex":{"status":"ready","reasonCode":"auth_ok","version":"0.1.0","checkedAt":"2026-07-17T08:00:00Z","secret":"must-not-persist"},"pi":{"status":"unverified","reasonCode":"unknown_reason","version":"/Users/example/.pi/session.jsonl","checkedAt":"not-a-time","rawOutput":"must-not-persist"},"unknown":{"status":"ready"}}}`))
	registerReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(registerRecorder, registerReq)
	if registerRecorder.Code != http.StatusCreated {
		t.Fatalf("register worker status=%d body=%s", registerRecorder.Code, registerRecorder.Body.String())
	}
	var worker RuntimeWorker
	if err := json.Unmarshal(registerRecorder.Body.Bytes(), &worker); err != nil {
		t.Fatalf("parse worker: %v", err)
	}
	if worker.ID == "" || worker.WorkspaceID != workspaceID || worker.Name != "team-worker-1" || worker.Status != "online" {
		t.Fatalf("unexpected worker: %+v", worker)
	}
	if !strings.Contains(string(worker.Capabilities), `"codex":true`) {
		t.Fatalf("expected codex capability, got %s", worker.Capabilities)
	}
	if string(worker.AgentEngineDiagnostics) != `{"codex":{"status":"ready","reasonCode":"auth_ok","version":"0.1.0","checkedAt":"2026-07-17T08:00:00Z"},"pi":{"status":"unverified"}}` {
		t.Fatalf("expected sanitized engine diagnostics, got %s", worker.AgentEngineDiagnostics)
	}

	heartbeatRecorder := httptest.NewRecorder()
	heartbeatReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/heartbeat", strings.NewReader(`{"status":"draining","currentLoad":2,"agentEngineDiagnostics":{"claude_code":{"status":"needs_setup","reasonCode":"auth_required","internalError":"must-not-persist"}}}`))
	heartbeatReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(heartbeatRecorder, heartbeatReq)
	if heartbeatRecorder.Code != http.StatusOK {
		t.Fatalf("heartbeat status=%d body=%s", heartbeatRecorder.Code, heartbeatRecorder.Body.String())
	}
	if err := json.Unmarshal(heartbeatRecorder.Body.Bytes(), &worker); err != nil {
		t.Fatalf("parse heartbeat worker: %v", err)
	}
	if worker.Status != "draining" || worker.CurrentLoad != 2 {
		t.Fatalf("unexpected heartbeat worker: %+v", worker)
	}
	if string(worker.AgentEngineDiagnostics) != `{"claude_code":{"status":"needs_setup","reasonCode":"auth_required"}}` {
		t.Fatalf("heartbeat should replace diagnostics with a sanitized snapshot, got %s", worker.AgentEngineDiagnostics)
	}
	var heartbeatCapabilities map[string]bool
	if err := json.Unmarshal(worker.Capabilities, &heartbeatCapabilities); err != nil {
		t.Fatalf("parse heartbeat capabilities: %v", err)
	}
	if heartbeatCapabilities["claudeCode"] || !heartbeatCapabilities["codex"] || !heartbeatCapabilities["docker"] {
		t.Fatalf("unavailable diagnostics should only downgrade the matching engine capability, got %s", worker.Capabilities)
	}

	omittedDiagnosticsRecorder := httptest.NewRecorder()
	omittedDiagnosticsReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/heartbeat", strings.NewReader(`{"status":"draining","currentLoad":2,"capabilities":{"codex":true,"docker":true,"claudeCode":true}}`))
	omittedDiagnosticsReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(omittedDiagnosticsRecorder, omittedDiagnosticsReq)
	if omittedDiagnosticsRecorder.Code != http.StatusOK {
		t.Fatalf("omitted diagnostics heartbeat status=%d body=%s", omittedDiagnosticsRecorder.Code, omittedDiagnosticsRecorder.Body.String())
	}
	if err := json.Unmarshal(omittedDiagnosticsRecorder.Body.Bytes(), &worker); err != nil {
		t.Fatalf("parse omitted diagnostics heartbeat worker: %v", err)
	}
	if string(worker.AgentEngineDiagnostics) != `{"claude_code":{"status":"needs_setup","reasonCode":"auth_required"}}` {
		t.Fatalf("omitted diagnostics should preserve the prior snapshot, got %s", worker.AgentEngineDiagnostics)
	}
	if err := json.Unmarshal(worker.Capabilities, &heartbeatCapabilities); err != nil {
		t.Fatalf("parse omitted diagnostics capabilities: %v", err)
	}
	if heartbeatCapabilities["claudeCode"] {
		t.Fatalf("omitting diagnostics must not let a heartbeat re-upgrade an unavailable engine, got %s", worker.Capabilities)
	}

	clearDiagnosticsRecorder := httptest.NewRecorder()
	clearDiagnosticsReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/heartbeat", strings.NewReader(`{"status":"draining","currentLoad":2,"agentEngineDiagnostics":{}}`))
	clearDiagnosticsReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(clearDiagnosticsRecorder, clearDiagnosticsReq)
	if clearDiagnosticsRecorder.Code != http.StatusOK {
		t.Fatalf("clear diagnostics heartbeat status=%d body=%s", clearDiagnosticsRecorder.Code, clearDiagnosticsRecorder.Body.String())
	}
	if err := json.Unmarshal(clearDiagnosticsRecorder.Body.Bytes(), &worker); err != nil {
		t.Fatalf("parse clear diagnostics heartbeat worker: %v", err)
	}
	if string(worker.AgentEngineDiagnostics) != "{}" {
		t.Fatalf("explicit empty diagnostics should clear the prior snapshot, got %s", worker.AgentEngineDiagnostics)
	}
	if err := json.Unmarshal(worker.Capabilities, &heartbeatCapabilities); err != nil {
		t.Fatalf("parse cleared diagnostics capabilities: %v", err)
	}
	if heartbeatCapabilities["claudeCode"] {
		t.Fatalf("clearing diagnostics alone must not alter existing capabilities, got %s", worker.Capabilities)
	}

	listRecorder := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/runtime-workers", nil)
	listReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list workers status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var workers []RuntimeWorker
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &workers); err != nil {
		t.Fatalf("parse workers: %v", err)
	}
	if len(workers) != 1 || workers[0].ID != worker.ID || workers[0].Status != "draining" {
		t.Fatalf("unexpected workers: %+v", workers)
	}
	if string(workers[0].AgentEngineDiagnostics) != string(worker.AgentEngineDiagnostics) {
		t.Fatalf("listed worker should preserve heartbeat diagnostics, got %s", workers[0].AgentEngineDiagnostics)
	}

	revokeRecorder := httptest.NewRecorder()
	revokeReq := httptest.NewRequest(http.MethodDelete, "/api/workspaces/"+workspaceID+"/runtime-registration-tokens/"+tokenResult.RegistrationToken.ID, nil)
	revokeReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(revokeRecorder, revokeReq)
	if revokeRecorder.Code != http.StatusOK {
		t.Fatalf("revoke token status=%d body=%s", revokeRecorder.Code, revokeRecorder.Body.String())
	}

	rejectedHeartbeatRecorder := httptest.NewRecorder()
	rejectedHeartbeatReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/heartbeat", strings.NewReader(`{"status":"online"}`))
	rejectedHeartbeatReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(rejectedHeartbeatRecorder, rejectedHeartbeatReq)
	if rejectedHeartbeatRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token heartbeat should be unauthorized, status=%d body=%s", rejectedHeartbeatRecorder.Code, rejectedHeartbeatRecorder.Body.String())
	}
}

func TestRuntimeAvailabilityFlow(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "runtime-availability-owner",
		Login:          "runtime-availability-owner",
		Name:           "Runtime Availability Owner",
		Email:          "runtime-availability-owner@example.com",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	outsider, outsiderWorkspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "runtime-availability-outsider",
		Login:          "runtime-availability-outsider",
		Name:           "Runtime Availability Outsider",
		Email:          "runtime-availability-outsider@example.com",
	})
	if err != nil {
		t.Fatalf("upsert outsider identity: %v", err)
	}
	outsiderToken, _, err := store.CreateAuthSession(context.Background(), outsider.ID, time.Hour)
	if err != nil {
		t.Fatalf("create outsider auth session: %v", err)
	}
	outsiderWorkspaceID := outsiderWorkspaces[0].ID
	teamWorkspace, _, err := store.CreateWorkspace(context.Background(), user.ID, CreateWorkspaceInput{Name: "Availability Team", Kind: "team"})
	if err != nil {
		t.Fatalf("create team workspace: %v", err)
	}
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	requestAvailability := func(path, token string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		router.ServeHTTP(recorder, req)
		return recorder
	}
	getAvailability := func(path string) RuntimeAvailability {
		t.Helper()
		recorder := requestAvailability(path, sessionToken)
		if recorder.Code != http.StatusOK {
			t.Fatalf("availability status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var availability RuntimeAvailability
		if err := json.Unmarshal(recorder.Body.Bytes(), &availability); err != nil {
			t.Fatalf("parse availability: %v", err)
		}
		return availability
	}

	unauthorized := requestAvailability("/api/workspaces/"+workspaceID+"/runtime/availability?runtimeMode=personal", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("availability should require auth, status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	forbiddenWorkspace := requestAvailability("/api/workspaces/"+workspaceID+"/runtime/availability?runtimeMode=personal", outsiderToken)
	if forbiddenWorkspace.Code != http.StatusNotFound {
		t.Fatalf("availability should hide non-member workspaces, status=%d body=%s", forbiddenWorkspace.Code, forbiddenWorkspace.Body.String())
	}
	_, outsiderWorker := registerTestRuntimeWorkerWithCapabilities(t, router, outsiderToken, outsiderWorkspaceID, `{"codex":true}`)
	if outsiderWorker.WorkspaceID != outsiderWorkspaceID {
		t.Fatalf("unexpected outsider worker workspace: %+v", outsiderWorker)
	}
	availability := getAvailability("/api/workspaces/" + workspaceID + "/runtime/availability?runtimeMode=personal")
	if availability.State != "unavailable" || availability.ReasonCode != "no_worker" || availability.CanQueue || !availability.CanAutoStart {
		t.Fatalf("unexpected no-worker availability with only other workspace workers online: %+v", availability)
	}
	if availability.ClaimableWorkerCount != 0 {
		t.Fatalf("other workspaces must not contribute claimable workers: %+v", availability)
	}
	if !strings.Contains(string(availability.RequiredCapabilities), `"codex":true`) {
		t.Fatalf("default availability should require codex, got %s", availability.RequiredCapabilities)
	}

	tokenResult, worker := registerTestRuntimeWorkerWithCapabilities(t, router, sessionToken, workspaceID, `{"codex":true}`)
	availability = getAvailability("/api/workspaces/" + workspaceID + "/runtime/availability?runtimeMode=personal")
	if availability.State != "ready" || availability.ReasonCode != "ready" || !availability.CanQueue || availability.CanAutoStart {
		t.Fatalf("unexpected ready availability: %+v", availability)
	}
	if availability.MatchedWorker == nil || availability.MatchedWorker.ID != worker.ID || availability.LastSeenAt == "" {
		t.Fatalf("expected matched worker, got %+v", availability)
	}
	if availability.ClaimableWorkerCount != 1 {
		t.Fatalf("expected one claimable worker, got %+v", availability)
	}
	project := createTestProjectViaHTTP(t, router, sessionToken, workspaceID, "availability-source-project")
	issueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{
		ProjectID: project.ID, Title: "Check source availability", Body: "A source session needs V1.",
	})
	if err != nil {
		t.Fatalf("create availability issue: %v", err)
	}
	sourceAvailability := getAvailability("/api/workspaces/" + workspaceID + "/runtime/availability?runtimeMode=personal&capabilities=codex&issueId=" + issueID)
	if sourceAvailability.State == "ready" || sourceAvailability.ClaimableWorkerCount != 0 || !strings.Contains(string(sourceAvailability.RequiredCapabilities), `"issueWorkingCopyV1":true`) {
		t.Fatalf("legacy worker must not satisfy issue source availability: %+v", sourceAvailability)
	}
	if !strings.Contains(string(availability.MatchedWorker.AgentEngineDiagnostics), `"codex":{"status":"ready"`) {
		t.Fatalf("matched worker should include engine diagnostics, got %+v", availability.MatchedWorker)
	}

	downgradeRecorder := httptest.NewRecorder()
	downgradeReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/heartbeat", strings.NewReader(`{"status":"online","capabilities":{"codex":true},"agentEngineDiagnostics":{"codex":{"status":"needs_setup","reasonCode":"auth_required"}}}`))
	downgradeReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(downgradeRecorder, downgradeReq)
	if downgradeRecorder.Code != http.StatusOK {
		t.Fatalf("downgrade heartbeat status=%d body=%s", downgradeRecorder.Code, downgradeRecorder.Body.String())
	}
	availability = getAvailability("/api/workspaces/" + workspaceID + "/runtime/availability?runtimeMode=personal")
	if availability.State != "unavailable" || availability.ReasonCode != "missing_capability" || availability.CanQueue || availability.ClaimableWorkerCount != 0 {
		t.Fatalf("unavailable diagnostics must prevent claimability, got %+v", availability)
	}

	restoreRecorder := httptest.NewRecorder()
	restoreReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/heartbeat", strings.NewReader(`{"status":"online","capabilities":{"codex":true},"agentEngineDiagnostics":{"codex":{"status":"ready"}}}`))
	restoreReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(restoreRecorder, restoreReq)
	if restoreRecorder.Code != http.StatusOK {
		t.Fatalf("restore heartbeat status=%d body=%s", restoreRecorder.Code, restoreRecorder.Body.String())
	}

	availability = getAvailability("/api/workspaces/" + workspaceID + "/runtime/availability?runtimeMode=personal&capabilities=codex,browser")
	if availability.State != "unavailable" || availability.ReasonCode != "missing_capability" || availability.CanQueue || !availability.CanAutoStart {
		t.Fatalf("unexpected missing-capability availability: %+v", availability)
	}
	if len(availability.MissingCapabilities) != 1 || availability.MissingCapabilities[0] != "browser" {
		t.Fatalf("expected missing browser capability, got %+v", availability.MissingCapabilities)
	}
	if availability.ClaimableWorkerCount != 0 {
		t.Fatalf("missing capability should have no claimable workers: %+v", availability)
	}

	drainingRecorder := httptest.NewRecorder()
	drainingReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/heartbeat", strings.NewReader(`{"status":"draining","currentLoad":1}`))
	drainingReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(drainingRecorder, drainingReq)
	if drainingRecorder.Code != http.StatusOK {
		t.Fatalf("draining heartbeat status=%d body=%s", drainingRecorder.Code, drainingRecorder.Body.String())
	}
	availability = getAvailability("/api/workspaces/" + workspaceID + "/runtime/availability?runtimeMode=personal")
	if availability.State != "unavailable" || availability.ReasonCode != "worker_draining" || availability.CanQueue || !availability.CanAutoStart {
		t.Fatalf("unexpected draining availability: %+v", availability)
	}
	if availability.ClaimableWorkerCount != 0 {
		t.Fatalf("draining worker must not be claimable: %+v", availability)
	}

	store.mu.Lock()
	for key, existing := range store.runtimeWorkers {
		if existing.ID == worker.ID {
			existing.Status = "online"
			existing.LastSeenAt = time.Now().UTC().Add(-activeWorkerMaxAge - time.Minute).Format(time.RFC3339Nano)
			store.runtimeWorkers[key] = existing
			break
		}
	}
	store.mu.Unlock()
	availability = getAvailability("/api/workspaces/" + workspaceID + "/runtime/availability?runtimeMode=personal")
	if availability.State != "unavailable" || availability.ReasonCode != "stale_heartbeat" || availability.CanQueue || !availability.CanAutoStart {
		t.Fatalf("unexpected stale availability: %+v", availability)
	}
	if availability.ClaimableWorkerCount != 0 {
		t.Fatalf("stale worker must not be claimable: %+v", availability)
	}

	availability = getAvailability("/api/workspaces/" + teamWorkspace.ID + "/runtime/availability?runtimeMode=personal")
	if availability.State != "unavailable" || availability.ReasonCode != "wrong_runtime_mode" || availability.CanAutoStart || availability.CanQueue {
		t.Fatalf("unexpected wrong-mode availability: %+v", availability)
	}
	availability = getAvailability("/api/workspaces/" + teamWorkspace.ID + "/runtime/availability?runtimeMode=team")
	if availability.State != "unavailable" || availability.ReasonCode != "no_worker" || availability.CanAutoStart || availability.CanQueue {
		t.Fatalf("unexpected team no-worker availability: %+v", availability)
	}
}

func TestEnsureRuntimeRegistrationTokenDoesNotReviveRevokedToken(t *testing.T) {
	store := NewMemoryStore()
	user, _, _, err := store.EnsureBootstrapAdmin(context.Background(), PasswordAuthInput{
		Login:    "runtime-owner",
		Name:     "Runtime Owner",
		Password: "correct-password",
	})
	if err != nil {
		t.Fatalf("ensure bootstrap admin: %v", err)
	}
	workspace, workspaces, err := store.CreateWorkspace(context.Background(), user.ID, CreateWorkspaceInput{Name: "Runtime Team", Kind: "team"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if len(workspaces) != 2 || workspace.ID == "" {
		t.Fatalf("unexpected workspaces: workspace=%+v workspaces=%+v", workspace, workspaces)
	}
	runtimeToken := "msw_testrevokedruntime0000000000000000000000000000000000000000000000000000"
	record, err := store.EnsureRuntimeRegistrationToken(context.Background(), user.ID, workspace.ID, EnsureRuntimeRegistrationTokenInput{
		Token:          runtimeToken,
		Name:           "Helm fixed worker",
		ExpiresInHours: 24,
	})
	if err != nil {
		t.Fatalf("ensure runtime token: %v", err)
	}
	if _, err := store.RevokeRuntimeRegistrationToken(context.Background(), user.ID, workspace.ID, record.ID); err != nil {
		t.Fatalf("revoke runtime token: %v", err)
	}
	record, err = store.EnsureRuntimeRegistrationToken(context.Background(), user.ID, workspace.ID, EnsureRuntimeRegistrationTokenInput{
		Token:          runtimeToken,
		Name:           "Helm fixed worker",
		ExpiresInHours: 48,
	})
	if err != nil {
		t.Fatalf("ensure revoked runtime token: %v", err)
	}
	if !record.Revoked {
		t.Fatalf("expected ensure to preserve revoked state, got %+v", record)
	}
	if _, err := store.AuthenticateRuntimeRegistrationToken(context.Background(), runtimeToken); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked runtime token should not authenticate, got %v", err)
	}
}

func TestCreateWorkerInstallationReturnsInstallCommand(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "worker-install-owner",
		Login:          "worker-install-owner",
		Name:           "Worker Install Owner",
		Email:          "worker-install-owner@example.com",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	teamWorkspace, _, err := store.CreateWorkspace(context.Background(), user.ID, CreateWorkspaceInput{Name: "Install Team", Kind: "team"})
	if err != nil {
		t.Fatalf("create team workspace: %v", err)
	}
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+teamWorkspace.ID+"/worker-installations", strings.NewReader(`{"name":"edge-worker","expiresInHours":1}`))
	req.Host = "mspace.example.com"
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	req.Header.Set("X-Forwarded-Proto", "https")
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create worker installation status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var result WorkerInstallationResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse worker installation: %v", err)
	}
	if result.RuntimeMode != "team" || result.WorkerName != "edge-worker" {
		t.Fatalf("unexpected install metadata: %+v", result)
	}
	if result.ServerURL != "https://mspace.example.com" || result.InstallScriptURL != "https://mspace.example.com/install/worker" {
		t.Fatalf("unexpected install URLs: %+v", result)
	}
	for _, fragment := range []string{
		"MSPACE_SERVER_URL='https://mspace.example.com'",
		"MSPACE_RUNTIME_TOKEN='msw_",
		"MSPACE_WORKER_MODE='team'",
		"MSPACE_WORKER_NAME='edge-worker'",
		"curl -fsSL 'https://mspace.example.com/install/worker'",
	} {
		if !strings.Contains(result.InstallCommand, fragment) {
			t.Fatalf("install command missing %q: %s", fragment, result.InstallCommand)
		}
	}
	if result.CredentialPrefix == "" || result.ExpiresAt == "" {
		t.Fatalf("expected credential metadata: %+v", result)
	}

	tokens, err := store.ListRuntimeRegistrationTokens(context.Background(), user.ID, teamWorkspace.ID)
	if err != nil {
		t.Fatalf("list runtime tokens: %v", err)
	}
	if len(tokens) != 1 || tokens[0].Name != "edge-worker" || tokens[0].TokenPrefix != result.CredentialPrefix {
		t.Fatalf("unexpected installation credential: %+v", tokens)
	}

	personalRecorder := httptest.NewRecorder()
	personalReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaces[0].ID+"/worker-installations", strings.NewReader(`{}`))
	personalReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(personalRecorder, personalReq)
	if personalRecorder.Code != http.StatusCreated {
		t.Fatalf("create personal worker installation status=%d body=%s", personalRecorder.Code, personalRecorder.Body.String())
	}
	var personalResult WorkerInstallationResult
	if err := json.Unmarshal(personalRecorder.Body.Bytes(), &personalResult); err != nil {
		t.Fatalf("parse personal worker installation: %v", err)
	}
	if personalResult.RuntimeMode != "personal" || !strings.Contains(personalResult.InstallCommand, "MSPACE_WORKER_MODE='personal'") {
		t.Fatalf("unexpected personal installation result: %+v", personalResult)
	}
}

func TestWorkerInstallScriptRoute(t *testing.T) {
	server := NewServer(Config{}, NewMemoryStore(), fakeGitHubClient{})
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/install/worker", nil)
	server.Routes().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("install script status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "docker run -d") || !strings.Contains(recorder.Body.String(), "MSPACE_RUNTIME_TOKEN") {
		t.Fatalf("unexpected install script: %s", recorder.Body.String())
	}
}

func TestRuntimeTaskQueueClaimFlow(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "task-owner",
		Login:          "task-owner",
		Name:           "Task Owner",
		Email:          "task-owner@example.com",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	personalWorkspaceID := workspaces[0].ID
	teamWorkspace, _, err := store.CreateWorkspace(context.Background(), user.ID, CreateWorkspaceInput{Name: "Task Team", Kind: "team"})
	if err != nil {
		t.Fatalf("create team workspace: %v", err)
	}
	workspaceID := teamWorkspace.ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	personalTaskRecorder := httptest.NewRecorder()
	personalTaskReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+personalWorkspaceID+"/runtime-tasks", strings.NewReader(`{"kind":"noop","runtimeMode":"team","requiredCapabilities":{"codex":true},"payload":{"prompt":"fix it"}}`))
	personalTaskReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(personalTaskRecorder, personalTaskReq)
	if personalTaskRecorder.Code != http.StatusForbidden {
		t.Fatalf("personal workspace team task should be forbidden, status=%d body=%s", personalTaskRecorder.Code, personalTaskRecorder.Body.String())
	}

	personalModeTaskRecorder := httptest.NewRecorder()
	personalModeTaskReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+personalWorkspaceID+"/runtime-tasks", strings.NewReader(`{"kind":"noop","runtimeMode":"personal","requiredCapabilities":{"codex":true},"payload":{"prompt":"fix it"}}`))
	personalModeTaskReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(personalModeTaskRecorder, personalModeTaskReq)
	if personalModeTaskRecorder.Code != http.StatusCreated {
		t.Fatalf("create personal runtime task status=%d body=%s", personalModeTaskRecorder.Code, personalModeTaskRecorder.Body.String())
	}

	createTokenRecorder := httptest.NewRecorder()
	createTokenReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/runtime-registration-tokens", strings.NewReader(`{"name":"task worker","expiresInHours":12}`))
	createTokenReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createTokenRecorder, createTokenReq)
	if createTokenRecorder.Code != http.StatusCreated {
		t.Fatalf("create runtime token status=%d body=%s", createTokenRecorder.Code, createTokenRecorder.Body.String())
	}
	var tokenResult RuntimeRegistrationTokenResult
	if err := json.Unmarshal(createTokenRecorder.Body.Bytes(), &tokenResult); err != nil {
		t.Fatalf("parse runtime token: %v", err)
	}

	registerRecorder := httptest.NewRecorder()
	registerReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/register", strings.NewReader(`{"name":"task-worker-1","mode":"team","version":"0.1.0","capabilities":{"codex":true,"docker":true},"labels":{"host":"local"}}`))
	registerReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(registerRecorder, registerReq)
	if registerRecorder.Code != http.StatusCreated {
		t.Fatalf("register worker status=%d body=%s", registerRecorder.Code, registerRecorder.Body.String())
	}
	var worker RuntimeWorker
	if err := json.Unmarshal(registerRecorder.Body.Bytes(), &worker); err != nil {
		t.Fatalf("parse worker: %v", err)
	}

	rejectPersonalWorkerRecorder := httptest.NewRecorder()
	rejectPersonalWorkerReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/register", strings.NewReader(`{"name":"wrong-mode-worker","mode":"personal","version":"0.1.0","capabilities":{"codex":true},"labels":{"host":"local"}}`))
	rejectPersonalWorkerReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(rejectPersonalWorkerRecorder, rejectPersonalWorkerReq)
	if rejectPersonalWorkerRecorder.Code != http.StatusForbidden {
		t.Fatalf("team workspace personal worker should be forbidden, status=%d body=%s", rejectPersonalWorkerRecorder.Code, rejectPersonalWorkerRecorder.Body.String())
	}

	rejectPersonalTaskRecorder := httptest.NewRecorder()
	rejectPersonalTaskReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/runtime-tasks", strings.NewReader(`{"kind":"noop","runtimeMode":"personal","requiredCapabilities":{"codex":true},"payload":{"prompt":"fix it"}}`))
	rejectPersonalTaskReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(rejectPersonalTaskRecorder, rejectPersonalTaskReq)
	if rejectPersonalTaskRecorder.Code != http.StatusForbidden {
		t.Fatalf("team workspace personal task should be forbidden, status=%d body=%s", rejectPersonalTaskRecorder.Code, rejectPersonalTaskRecorder.Body.String())
	}

	createTaskRecorder := httptest.NewRecorder()
	createTaskReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/runtime-tasks", strings.NewReader(`{"kind":"noop","priority":5,"runtimeMode":"team","requiredCapabilities":{"codex":true},"payload":{"prompt":"fix it"}}`))
	createTaskReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createTaskRecorder, createTaskReq)
	if createTaskRecorder.Code != http.StatusCreated {
		t.Fatalf("create runtime task status=%d body=%s", createTaskRecorder.Code, createTaskRecorder.Body.String())
	}
	var task RuntimeTask
	if err := json.Unmarshal(createTaskRecorder.Body.Bytes(), &task); err != nil {
		t.Fatalf("parse runtime task: %v", err)
	}
	if task.ID == "" || task.WorkspaceID != workspaceID || task.Status != "queued" || task.Kind != "noop" || task.RuntimeMode != "team" {
		t.Fatalf("unexpected created task: %+v", task)
	}

	listTasksRecorder := httptest.NewRecorder()
	listTasksReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/runtime-tasks", nil)
	listTasksReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(listTasksRecorder, listTasksReq)
	if listTasksRecorder.Code != http.StatusOK {
		t.Fatalf("list runtime tasks status=%d body=%s", listTasksRecorder.Code, listTasksRecorder.Body.String())
	}
	var tasks []RuntimeTask
	if err := json.Unmarshal(listTasksRecorder.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("parse runtime tasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != task.ID {
		t.Fatalf("unexpected runtime tasks: %+v", tasks)
	}

	claimRecorder := httptest.NewRecorder()
	claimReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/claim", nil)
	claimReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(claimRecorder, claimReq)
	if claimRecorder.Code != http.StatusOK {
		t.Fatalf("claim task status=%d body=%s", claimRecorder.Code, claimRecorder.Body.String())
	}
	if err := json.Unmarshal(claimRecorder.Body.Bytes(), &task); err != nil {
		t.Fatalf("parse claimed task: %v", err)
	}
	if task.Status != "claimed" || task.ClaimedByWorkerID != worker.ID || task.ClaimedAt == "" {
		t.Fatalf("unexpected claimed task: %+v", task)
	}

	secondClaimRecorder := httptest.NewRecorder()
	secondClaimReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/claim", nil)
	secondClaimReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(secondClaimRecorder, secondClaimReq)
	if secondClaimRecorder.Code != http.StatusNoContent {
		t.Fatalf("second claim should find no task, status=%d body=%s", secondClaimRecorder.Code, secondClaimRecorder.Body.String())
	}

	runningRecorder := httptest.NewRecorder()
	runningReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/"+task.ID+"/status", strings.NewReader(`{"status":"running"}`))
	runningReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(runningRecorder, runningReq)
	if runningRecorder.Code != http.StatusOK {
		t.Fatalf("running update status=%d body=%s", runningRecorder.Code, runningRecorder.Body.String())
	}
	if err := json.Unmarshal(runningRecorder.Body.Bytes(), &task); err != nil {
		t.Fatalf("parse running task: %v", err)
	}
	if task.Status != "running" || task.StartedAt == "" {
		t.Fatalf("unexpected running task: %+v", task)
	}

	freshResumeRecorder := httptest.NewRecorder()
	freshResumeReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/claim", nil)
	freshResumeReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(freshResumeRecorder, freshResumeReq)
	if freshResumeRecorder.Code != http.StatusNoContent {
		t.Fatalf("fresh running task should not be reclaimed, status=%d body=%s", freshResumeRecorder.Code, freshResumeRecorder.Body.String())
	}

	store.mu.Lock()
	staleTask := store.runtimeTasks[task.ID]
	staleTask.UpdatedAt = time.Now().UTC().Add(-staleRunningTaskReclaimAge - time.Minute).Format(time.RFC3339Nano)
	store.runtimeTasks[task.ID] = staleTask
	store.mu.Unlock()

	staleResumeRecorder := httptest.NewRecorder()
	staleResumeReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/claim", nil)
	staleResumeReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(staleResumeRecorder, staleResumeReq)
	if staleResumeRecorder.Code != http.StatusOK {
		t.Fatalf("resume stale running task claim status=%d body=%s", staleResumeRecorder.Code, staleResumeRecorder.Body.String())
	}
	var resumedTask RuntimeTask
	if err := json.Unmarshal(staleResumeRecorder.Body.Bytes(), &resumedTask); err != nil {
		t.Fatalf("parse resumed task: %v", err)
	}
	if resumedTask.ID != task.ID || resumedTask.Status != "claimed" || resumedTask.ClaimedByWorkerID != worker.ID || resumedTask.ClaimedAt != task.ClaimedAt {
		t.Fatalf("expected same worker to resume running task before new work, got %+v original=%+v", resumedTask, task)
	}
	task = resumedTask

	getWorkerTaskRecorder := httptest.NewRecorder()
	getWorkerTaskReq := httptest.NewRequest(http.MethodGet, "/api/runtime/workers/"+worker.ID+"/tasks/"+task.ID, nil)
	getWorkerTaskReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(getWorkerTaskRecorder, getWorkerTaskReq)
	if getWorkerTaskRecorder.Code != http.StatusOK {
		t.Fatalf("get claimed task status=%d body=%s", getWorkerTaskRecorder.Code, getWorkerTaskRecorder.Body.String())
	}
	var workerVisibleTask RuntimeTask
	if err := json.Unmarshal(getWorkerTaskRecorder.Body.Bytes(), &workerVisibleTask); err != nil {
		t.Fatalf("parse worker-visible task: %v", err)
	}
	if workerVisibleTask.ID != task.ID || workerVisibleTask.Status != "claimed" {
		t.Fatalf("unexpected worker-visible task: %+v", workerVisibleTask)
	}

	logRecorder := httptest.NewRecorder()
	logReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/"+task.ID+"/logs", strings.NewReader(`{"stream":"agent","message":"Task is running on the worker."}`))
	logReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(logRecorder, logReq)
	if logRecorder.Code != http.StatusCreated {
		t.Fatalf("append runtime task log status=%d body=%s", logRecorder.Code, logRecorder.Body.String())
	}
	var taskLog RuntimeTaskLog
	if err := json.Unmarshal(logRecorder.Body.Bytes(), &taskLog); err != nil {
		t.Fatalf("parse runtime task log: %v", err)
	}
	if taskLog.TaskID != task.ID || taskLog.WorkerID != worker.ID || taskLog.Stream != "agent" {
		t.Fatalf("unexpected runtime task log: %+v", taskLog)
	}

	logsRecorder := httptest.NewRecorder()
	logsReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/runtime-tasks/"+task.ID+"/logs", nil)
	logsReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(logsRecorder, logsReq)
	if logsRecorder.Code != http.StatusOK {
		t.Fatalf("list runtime task logs status=%d body=%s", logsRecorder.Code, logsRecorder.Body.String())
	}
	var taskLogs []RuntimeTaskLog
	if err := json.Unmarshal(logsRecorder.Body.Bytes(), &taskLogs); err != nil {
		t.Fatalf("parse runtime task logs: %v", err)
	}
	if len(taskLogs) != 1 || taskLogs[0].Message != "Task is running on the worker." {
		t.Fatalf("unexpected runtime task logs: %+v", taskLogs)
	}

	cancelRecorder := httptest.NewRecorder()
	cancelReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/runtime-tasks/"+task.ID+"/cancel", strings.NewReader(`{"reason":"user stopped session"}`))
	cancelReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(cancelRecorder, cancelReq)
	if cancelRecorder.Code != http.StatusOK {
		t.Fatalf("cancel runtime task status=%d body=%s", cancelRecorder.Code, cancelRecorder.Body.String())
	}
	var cancelledTask RuntimeTask
	if err := json.Unmarshal(cancelRecorder.Body.Bytes(), &cancelledTask); err != nil {
		t.Fatalf("parse cancelled task: %v", err)
	}
	if cancelledTask.Status != "cancelled" || cancelledTask.FinishedAt == "" || !strings.Contains(cancelledTask.Error, "user stopped") {
		t.Fatalf("unexpected cancelled task: %+v", cancelledTask)
	}

	cancelledCompleteRecorder := httptest.NewRecorder()
	cancelledCompleteReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/"+task.ID+"/status", strings.NewReader(`{"status":"completed","result":{"exitCode":0}}`))
	cancelledCompleteReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(cancelledCompleteRecorder, cancelledCompleteReq)
	if cancelledCompleteRecorder.Code != http.StatusNotFound {
		t.Fatalf("cancelled task should reject worker completion, status=%d body=%s", cancelledCompleteRecorder.Code, cancelledCompleteRecorder.Body.String())
	}

	createTask2Recorder := httptest.NewRecorder()
	createTask2Req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/runtime-tasks", strings.NewReader(`{"kind":"noop","priority":5,"runtimeMode":"team","requiredCapabilities":{"codex":true},"payload":{"prompt":"finish it"}}`))
	createTask2Req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createTask2Recorder, createTask2Req)
	if createTask2Recorder.Code != http.StatusCreated {
		t.Fatalf("create second runtime task status=%d body=%s", createTask2Recorder.Code, createTask2Recorder.Body.String())
	}
	if err := json.Unmarshal(createTask2Recorder.Body.Bytes(), &task); err != nil {
		t.Fatalf("parse second runtime task: %v", err)
	}

	claim2Recorder := httptest.NewRecorder()
	claim2Req := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/claim", nil)
	claim2Req.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(claim2Recorder, claim2Req)
	if claim2Recorder.Code != http.StatusOK {
		t.Fatalf("claim second task status=%d body=%s", claim2Recorder.Code, claim2Recorder.Body.String())
	}
	if err := json.Unmarshal(claim2Recorder.Body.Bytes(), &task); err != nil {
		t.Fatalf("parse second claimed task: %v", err)
	}

	running2Recorder := httptest.NewRecorder()
	running2Req := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/"+task.ID+"/status", strings.NewReader(`{"status":"running"}`))
	running2Req.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(running2Recorder, running2Req)
	if running2Recorder.Code != http.StatusOK {
		t.Fatalf("running second update status=%d body=%s", running2Recorder.Code, running2Recorder.Body.String())
	}

	completedRecorder := httptest.NewRecorder()
	completedReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/"+task.ID+"/status", strings.NewReader(`{"status":"completed","result":{"exitCode":0}}`))
	completedReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(completedRecorder, completedReq)
	if completedRecorder.Code != http.StatusOK {
		t.Fatalf("completed update status=%d body=%s", completedRecorder.Code, completedRecorder.Body.String())
	}
	if err := json.Unmarshal(completedRecorder.Body.Bytes(), &task); err != nil {
		t.Fatalf("parse completed task: %v", err)
	}
	if task.Status != "completed" || task.FinishedAt == "" || !strings.Contains(string(task.Result), `"exitCode":0`) {
		t.Fatalf("unexpected completed task: %+v", task)
	}

	eventsRecorder := httptest.NewRecorder()
	eventsReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/runtime-tasks/"+task.ID+"/events", nil)
	eventsReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(eventsRecorder, eventsReq)
	if eventsRecorder.Code != http.StatusOK {
		t.Fatalf("list runtime task events status=%d body=%s", eventsRecorder.Code, eventsRecorder.Body.String())
	}
	var events []RuntimeTaskEvent
	if err := json.Unmarshal(eventsRecorder.Body.Bytes(), &events); err != nil {
		t.Fatalf("parse runtime task events: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("expected 4 runtime task events, got %+v", events)
	}
	counts := map[string]int{}
	var createdEvent RuntimeTaskEvent
	var claimedEvent RuntimeTaskEvent
	for _, event := range events {
		counts[event.Kind]++
		if event.Kind == "created" {
			createdEvent = event
		}
		if event.Kind == "claimed" {
			claimedEvent = event
		}
	}
	if counts["created"] != 1 || counts["claimed"] != 1 || counts["status_changed"] != 2 {
		t.Fatalf("unexpected task event kinds: %+v", events)
	}
	if createdEvent.ActorUserID != user.ID || claimedEvent.WorkerID != worker.ID {
		t.Fatalf("unexpected task event actors: %+v", events)
	}

	repeatUpdateRecorder := httptest.NewRecorder()
	repeatUpdateReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/"+task.ID+"/status", strings.NewReader(`{"status":"failed","error":"too late"}`))
	repeatUpdateReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(repeatUpdateRecorder, repeatUpdateReq)
	if repeatUpdateRecorder.Code != http.StatusNotFound {
		t.Fatalf("completed task should reject further updates, status=%d body=%s", repeatUpdateRecorder.Code, repeatUpdateRecorder.Body.String())
	}

	unmatchedTaskRecorder := httptest.NewRecorder()
	unmatchedTaskReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/runtime-tasks", strings.NewReader(`{"kind":"noop","runtimeMode":"team","requiredCapabilities":{"kubectl":true},"payload":{"prompt":"deploy it"}}`))
	unmatchedTaskReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(unmatchedTaskRecorder, unmatchedTaskReq)
	if unmatchedTaskRecorder.Code != http.StatusCreated {
		t.Fatalf("create unmatched task status=%d body=%s", unmatchedTaskRecorder.Code, unmatchedTaskRecorder.Body.String())
	}

	unmatchedClaimRecorder := httptest.NewRecorder()
	unmatchedClaimReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/claim", nil)
	unmatchedClaimReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(unmatchedClaimRecorder, unmatchedClaimReq)
	if unmatchedClaimRecorder.Code != http.StatusNoContent {
		t.Fatalf("worker without required capability should not claim task, status=%d body=%s", unmatchedClaimRecorder.Code, unmatchedClaimRecorder.Body.String())
	}

	invalidTokenClaimRecorder := httptest.NewRecorder()
	invalidTokenClaimReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/claim", nil)
	invalidTokenClaimReq.Header.Set("Authorization", "Bearer msw_invalid")
	router.ServeHTTP(invalidTokenClaimRecorder, invalidTokenClaimReq)
	if invalidTokenClaimRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("invalid worker token should be unauthorized, status=%d body=%s", invalidTokenClaimRecorder.Code, invalidTokenClaimRecorder.Body.String())
	}
}

func TestRuntimeTaskListPagination(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "task-pagination-owner",
		Login:          "task-pagination-owner",
		Name:           "Task Pagination Owner",
		Email:          "task-pagination-owner@example.com",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	createdIDs := make([]string, 0, 12)
	for index := 0; index < 12; index++ {
		createRecorder := httptest.NewRecorder()
		body := fmt.Sprintf(`{"kind":"noop","priority":%d,"runtimeMode":"personal","requiredCapabilities":{"protocolSmoke":true},"payload":{"prompt":"task %02d"}}`, index, index)
		createReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/runtime-tasks", strings.NewReader(body))
		createReq.Header.Set("Authorization", "Bearer "+sessionToken)
		router.ServeHTTP(createRecorder, createReq)
		if createRecorder.Code != http.StatusCreated {
			t.Fatalf("create runtime task %d status=%d body=%s", index, createRecorder.Code, createRecorder.Body.String())
		}
		var task RuntimeTask
		if err := json.Unmarshal(createRecorder.Body.Bytes(), &task); err != nil {
			t.Fatalf("parse runtime task %d: %v", index, err)
		}
		createdIDs = append(createdIDs, task.ID)
	}

	pageRecorder := httptest.NewRecorder()
	pageReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/runtime-tasks?limit=5&offset=5", nil)
	pageReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(pageRecorder, pageReq)
	if pageRecorder.Code != http.StatusOK {
		t.Fatalf("list paged runtime tasks status=%d body=%s", pageRecorder.Code, pageRecorder.Body.String())
	}
	var page RuntimeTaskListResult
	if err := json.Unmarshal(pageRecorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("parse paged runtime tasks: %v", err)
	}
	if page.Total != 12 || page.Limit != 5 || page.Offset != 5 || len(page.Tasks) != 5 || page.StatusCounts["queued"] != 12 {
		t.Fatalf("unexpected page metadata: %+v", page)
	}
	for index, task := range page.Tasks {
		expectedID := createdIDs[len(createdIDs)-1-5-index]
		if task.ID != expectedID {
			t.Fatalf("unexpected task at page index %d: got %s want %s page=%+v created=%+v", index, task.ID, expectedID, page.Tasks, createdIDs)
		}
	}

	legacyRecorder := httptest.NewRecorder()
	legacyReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/runtime-tasks", nil)
	legacyReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(legacyRecorder, legacyReq)
	if legacyRecorder.Code != http.StatusOK {
		t.Fatalf("list legacy runtime tasks status=%d body=%s", legacyRecorder.Code, legacyRecorder.Body.String())
	}
	var legacyTasks []RuntimeTask
	if err := json.Unmarshal(legacyRecorder.Body.Bytes(), &legacyTasks); err != nil {
		t.Fatalf("parse legacy runtime tasks: %v", err)
	}
	if len(legacyTasks) != 10 || legacyTasks[0].ID != createdIDs[len(createdIDs)-1] {
		t.Fatalf("unexpected legacy runtime tasks: %+v", legacyTasks)
	}
}

func TestAutoDeployTestEnvironmentQueuesAfterSourceSession(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "auto-deploy-owner",
		Login:          "auto-deploy-owner",
		Name:           "Auto Deploy Owner",
		Email:          "auto-deploy-owner@example.com",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	settingsRecorder := httptest.NewRecorder()
	settingsReq := httptest.NewRequest(http.MethodPut, "/api/workspaces/"+workspaceID+"/workspace/settings", strings.NewReader(`{"autoCreateDraftPr":false,"autoDeployTestEnvironment":true}`))
	settingsReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(settingsRecorder, settingsReq)
	if settingsRecorder.Code != http.StatusOK {
		t.Fatalf("update workspace settings status=%d body=%s", settingsRecorder.Code, settingsRecorder.Body.String())
	}
	var settings WorkspaceSettings
	if err := json.Unmarshal(settingsRecorder.Body.Bytes(), &settings); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	if !settings.AutoDeployTestEnvironment {
		t.Fatalf("expected auto deploy setting to be enabled: %+v", settings)
	}

	clusterResult, err := store.ImportKubeconfigs(context.Background(), user.ID, workspaceID, []string{"/tmp/mspace-auto-deploy-kubeconfig"})
	if err != nil {
		t.Fatalf("import kubeconfig: %v", err)
	}
	if len(clusterResult.Imported) != 1 {
		t.Fatalf("expected one imported cluster, got %+v", clusterResult)
	}
	cluster := clusterResult.Imported[0]
	project, err := store.CreateProject(context.Background(), user.ID, workspaceID, ProjectInput{
		Name:             "Auto Deploy Project",
		SourceType:       "github",
		RepoURL:          "https://github.com/mlhiter/auto-deploy-project.git",
		DefaultBranch:    "main",
		DefaultClusterID: cluster.ID,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	issueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{
		ProjectID: project.ID,
		Body:      "Fix a source bug and deploy the result automatically.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	createTokenRecorder := httptest.NewRecorder()
	createTokenReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/runtime-registration-tokens", strings.NewReader(`{"name":"auto deploy worker","expiresInHours":12}`))
	createTokenReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createTokenRecorder, createTokenReq)
	if createTokenRecorder.Code != http.StatusCreated {
		t.Fatalf("create runtime token status=%d body=%s", createTokenRecorder.Code, createTokenRecorder.Body.String())
	}
	var tokenResult RuntimeRegistrationTokenResult
	if err := json.Unmarshal(createTokenRecorder.Body.Bytes(), &tokenResult); err != nil {
		t.Fatalf("parse runtime token: %v", err)
	}

	registerRecorder := httptest.NewRecorder()
	registerReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/register", strings.NewReader(`{"name":"auto-deploy-worker","storageId":"msws_auto_deploy_worker","mode":"personal","version":"0.1.0","capabilities":{"codex":true,"docker":true,"kubectl":true,"issueWorkingCopyV1":true},"labels":{"host":"local"}}`))
	registerReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(registerRecorder, registerReq)
	if registerRecorder.Code != http.StatusCreated {
		t.Fatalf("register worker status=%d body=%s", registerRecorder.Code, registerRecorder.Body.String())
	}
	var worker RuntimeWorker
	if err := json.Unmarshal(registerRecorder.Body.Bytes(), &worker); err != nil {
		t.Fatalf("parse worker: %v", err)
	}

	createSessionRecorder := httptest.NewRecorder()
	createSessionReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/issues/"+issueID+"/sessions", strings.NewReader(`{"agentEngine":"codex","command":"Fix the source bug."}`))
	createSessionReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createSessionRecorder, createSessionReq)
	if createSessionRecorder.Code != http.StatusCreated {
		t.Fatalf("create agent session status=%d body=%s", createSessionRecorder.Code, createSessionRecorder.Body.String())
	}
	var sourceSession AgentSession
	if err := json.Unmarshal(createSessionRecorder.Body.Bytes(), &sourceSession); err != nil {
		t.Fatalf("parse source session: %v", err)
	}

	claimSourceRecorder := httptest.NewRecorder()
	claimSourceReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/claim", nil)
	claimSourceReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(claimSourceRecorder, claimSourceReq)
	if claimSourceRecorder.Code != http.StatusOK {
		t.Fatalf("claim source task status=%d body=%s", claimSourceRecorder.Code, claimSourceRecorder.Body.String())
	}
	var sourceTask RuntimeTask
	if err := json.Unmarshal(claimSourceRecorder.Body.Bytes(), &sourceTask); err != nil {
		t.Fatalf("parse claimed source task: %v", err)
	}
	if sourceTask.ID != sourceSession.RuntimeTaskID {
		t.Fatalf("expected source task %s, got %+v", sourceSession.RuntimeTaskID, sourceTask)
	}

	completeSourceRecorder := httptest.NewRecorder()
	completeSourceBody := fmt.Sprintf(`{"status":"completed","result":{"exitCode":0,"source":{"commitSha":"1111111111111111111111111111111111111111","shortCommitSha":"111111111111","branch":%q,"subject":"fix auto deploy","filesChanged":1,"changes":[],"diffPreview":"diff --git a/app.go b/app.go"},"workingCopy":{"storageId":"msws_auto_deploy_worker","branch":%q,"baseCommitSha":"0000000000000000000000000000000000000000","headCommitSha":"1111111111111111111111111111111111111111","contentState":"clean","recoveryReason":"","generation":0}}}`, sourceSession.Branch, sourceSession.Branch)
	completeSourceReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/"+sourceTask.ID+"/status", strings.NewReader(completeSourceBody))
	completeSourceReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(completeSourceRecorder, completeSourceReq)
	if completeSourceRecorder.Code != http.StatusOK {
		t.Fatalf("complete source task status=%d body=%s", completeSourceRecorder.Code, completeSourceRecorder.Body.String())
	}

	tasksRecorder := httptest.NewRecorder()
	tasksReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/runtime-tasks?limit=10&offset=0", nil)
	tasksReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(tasksRecorder, tasksReq)
	if tasksRecorder.Code != http.StatusOK {
		t.Fatalf("list runtime tasks status=%d body=%s", tasksRecorder.Code, tasksRecorder.Body.String())
	}
	var tasksPage RuntimeTaskListResult
	if err := json.Unmarshal(tasksRecorder.Body.Bytes(), &tasksPage); err != nil {
		t.Fatalf("parse runtime tasks: %v", err)
	}
	var deployTask RuntimeTask
	for _, task := range tasksPage.Tasks {
		if task.ID != sourceTask.ID && task.Kind == "agent_session" {
			var payload struct {
				Automation      string `json:"automation"`
				SourceCommitSHA string `json:"sourceCommitSha"`
			}
			_ = json.Unmarshal(task.Payload, &payload)
			if payload.Automation == autoDeployTestEnvironmentAutomation {
				deployTask = task
				if payload.SourceCommitSHA != "1111111111111111111111111111111111111111" {
					t.Fatalf("auto deploy task used wrong source commit: %+v", payload)
				}
			}
		}
	}
	if deployTask.ID == "" {
		t.Fatalf("expected an auto deploy task, got %+v", tasksPage)
	}
	if deployTask.Status != "queued" || deployTask.RuntimeMode != "personal" || deployTask.IssueID != issueID || deployTask.ProjectID != project.ID {
		t.Fatalf("unexpected auto deploy task: %+v", deployTask)
	}

	detail, err := store.GetIssue(context.Background(), user.ID, workspaceID, issueID)
	if err != nil {
		t.Fatalf("get issue detail: %v", err)
	}
	if detail.TestEnvironment == nil {
		t.Fatalf("expected test environment to be created")
	}
	if detail.TestEnvironment.NamespaceStatus != "deploying" ||
		detail.TestEnvironment.LastDeploySessionID != deployTask.SessionID ||
		detail.TestEnvironment.SourceCommitSHA != "1111111111111111111111111111111111111111" {
		t.Fatalf("unexpected test environment: %+v", detail.TestEnvironment)
	}

	claimDeployRecorder := httptest.NewRecorder()
	claimDeployReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/claim", nil)
	claimDeployReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(claimDeployRecorder, claimDeployReq)
	if claimDeployRecorder.Code != http.StatusOK {
		t.Fatalf("claim auto deploy task status=%d body=%s", claimDeployRecorder.Code, claimDeployRecorder.Body.String())
	}
	var claimedDeployTask RuntimeTask
	if err := json.Unmarshal(claimDeployRecorder.Body.Bytes(), &claimedDeployTask); err != nil {
		t.Fatalf("parse claimed auto deploy task: %v", err)
	}
	if claimedDeployTask.ID != deployTask.ID {
		t.Fatalf("expected deploy task %s, got %+v", deployTask.ID, claimedDeployTask)
	}

	completeDeployRecorder := httptest.NewRecorder()
	completeDeployReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/"+claimedDeployTask.ID+"/status", strings.NewReader(`{"status":"completed","result":{"exitCode":0,"source":{"commitSha":"2222222222222222222222222222222222222222","shortCommitSha":"222222222222","branch":"deploy/auto","subject":"deploy auto","filesChanged":1},"testEnvironment":{"previewUrl":"http://127.0.0.1:31000"}}}`))
	completeDeployReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(completeDeployRecorder, completeDeployReq)
	if completeDeployRecorder.Code != http.StatusOK {
		t.Fatalf("complete auto deploy task status=%d body=%s", completeDeployRecorder.Code, completeDeployRecorder.Body.String())
	}

	finalTasksRecorder := httptest.NewRecorder()
	finalTasksReq := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/runtime-tasks?limit=10&offset=0", nil)
	finalTasksReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(finalTasksRecorder, finalTasksReq)
	if finalTasksRecorder.Code != http.StatusOK {
		t.Fatalf("list final runtime tasks status=%d body=%s", finalTasksRecorder.Code, finalTasksRecorder.Body.String())
	}
	var finalTasksPage RuntimeTaskListResult
	if err := json.Unmarshal(finalTasksRecorder.Body.Bytes(), &finalTasksPage); err != nil {
		t.Fatalf("parse final runtime tasks: %v", err)
	}
	autoDeployCount := 0
	for _, task := range finalTasksPage.Tasks {
		if isAutoDeployTestEnvironmentTask(task) {
			autoDeployCount++
		}
	}
	if autoDeployCount != 1 {
		t.Fatalf("auto deploy task should not recursively queue another deploy, count=%d tasks=%+v", autoDeployCount, finalTasksPage)
	}
}

func TestManualTestDeploySessionDoesNotTriggerAutomaticDeploy(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "manual-deploy-owner",
		Login:          "manual-deploy-owner",
		Name:           "Manual Deploy Owner",
		Email:          "manual-deploy-owner@example.com",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	sessionToken, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	workspaceID := workspaces[0].ID
	server := NewServer(Config{}, store, fakeGitHubClient{})
	router := server.Routes()

	if _, err := store.UpdateWorkspaceSettings(context.Background(), user.ID, workspaceID, WorkspaceSettingsInput{AutoDeployTestEnvironment: true}); err != nil {
		t.Fatalf("enable auto deploy setting: %v", err)
	}
	clusterResult, err := store.ImportKubeconfigs(context.Background(), user.ID, workspaceID, []string{"/tmp/mspace-manual-deploy-kubeconfig"})
	if err != nil {
		t.Fatalf("import kubeconfig: %v", err)
	}
	cluster := clusterResult.Imported[0]
	project, err := store.CreateProject(context.Background(), user.ID, workspaceID, ProjectInput{
		Name:             "Manual Deploy Project",
		SourceType:       "github",
		RepoURL:          "https://github.com/mlhiter/manual-deploy-project.git",
		DefaultBranch:    "main",
		DefaultClusterID: cluster.ID,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	issueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{
		ProjectID: project.ID,
		Body:      "Deploy an already reviewed commit manually.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	tokenResult, worker := registerTestRuntimeWorker(t, router, sessionToken, workspaceID)

	sourceSession, err := store.CreateAgentSession(context.Background(), user.ID, workspaceID, issueID, CreateAgentSessionInput{
		AgentEngine: agentEngineCodex,
		Command:     "Produce a source commit.",
	})
	if err != nil {
		t.Fatalf("create source session: %v", err)
	}
	sourceResult := fmt.Sprintf(`{"status":"completed","result":{"exitCode":0,"source":{"commitSha":"3333333333333333333333333333333333333333","shortCommitSha":"333333333333","branch":%q,"subject":"fix manual source","filesChanged":1},"workingCopy":{"storageId":%q,"branch":%q,"baseCommitSha":"0000000000000000000000000000000000000000","headCommitSha":"3333333333333333333333333333333333333333","contentState":"clean","recoveryReason":"","generation":0}}}`, sourceSession.Branch, worker.StorageID, sourceSession.Branch)
	sourceTask, err := claimAndCompleteTask(t, router, tokenResult.Token, worker.ID, sourceResult)
	if err != nil {
		t.Fatalf("complete source task: %v", err)
	}
	if sourceTask.ID != sourceSession.RuntimeTaskID {
		t.Fatalf("expected source task %s, got %+v", sourceSession.RuntimeTaskID, sourceTask)
	}
	autoDeployTask := findAutoDeployTask(t, store, user.ID, workspaceID, issueID, sourceTask.ID)

	_, err = claimAndCompleteTask(t, router, tokenResult.Token, worker.ID, `{"status":"completed","result":{"exitCode":0,"testEnvironment":{"previewUrl":"http://127.0.0.1:31001"}}}`)
	if err != nil {
		t.Fatalf("complete first auto deploy task: %v", err)
	}

	result, err := store.StartIssueTestDeploy(context.Background(), user.ID, workspaceID, issueID, StartTestDeployInput{
		SourceCommitSHA: "3333333333333333333333333333333333333333",
	})
	if err != nil {
		t.Fatalf("start manual test deploy: %v", err)
	}
	manualDeployTask, err := runtimeTaskBySession(store, workspaceID, result.SessionID)
	if err != nil {
		t.Fatalf("lookup manual deploy task: %v", err)
	}
	if runtimeTaskAutomation(manualDeployTask) != testDeployAutomation {
		t.Fatalf("expected manual deploy task marker %q, got payload %s", testDeployAutomation, manualDeployTask.Payload)
	}

	claimedManualDeploy, err := claimAndCompleteTask(t, router, tokenResult.Token, worker.ID, `{"status":"completed","result":{"exitCode":0,"source":{"commitSha":"4444444444444444444444444444444444444444","shortCommitSha":"444444444444","branch":"deploy/manual","subject":"deploy manual","filesChanged":1},"testEnvironment":{"previewUrl":"http://127.0.0.1:31002"}}}`)
	if err != nil {
		t.Fatalf("complete manual deploy task: %v", err)
	}
	if claimedManualDeploy.ID != manualDeployTask.ID {
		t.Fatalf("expected manual deploy task %s, got %+v", manualDeployTask.ID, claimedManualDeploy)
	}

	tasks, err := store.ListRuntimeTasks(context.Background(), user.ID, workspaceID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	autoDeployCount := 0
	for _, task := range tasks {
		if isAutoDeployTestEnvironmentTask(task) {
			autoDeployCount++
		}
	}
	if autoDeployCount != 1 || autoDeployTask.ID == "" {
		t.Fatalf("manual test deploy should not queue another auto deploy, count=%d tasks=%+v", autoDeployCount, tasks)
	}
}

func registerTestRuntimeWorker(t *testing.T, router http.Handler, sessionToken, workspaceID string) (RuntimeRegistrationTokenResult, RuntimeWorker) {
	return registerTestRuntimeWorkerWithCapabilities(t, router, sessionToken, workspaceID, `{"codex":true,"docker":true,"kubectl":true,"issueWorkingCopyV1":true}`)
}

func registerTestRuntimeWorkerWithCapabilities(t *testing.T, router http.Handler, sessionToken, workspaceID, capabilities string) (RuntimeRegistrationTokenResult, RuntimeWorker) {
	t.Helper()
	createTokenRecorder := httptest.NewRecorder()
	createTokenReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/runtime-registration-tokens", strings.NewReader(`{"name":"test worker","expiresInHours":12}`))
	createTokenReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createTokenRecorder, createTokenReq)
	if createTokenRecorder.Code != http.StatusCreated {
		t.Fatalf("create runtime token status=%d body=%s", createTokenRecorder.Code, createTokenRecorder.Body.String())
	}
	var tokenResult RuntimeRegistrationTokenResult
	if err := json.Unmarshal(createTokenRecorder.Body.Bytes(), &tokenResult); err != nil {
		t.Fatalf("parse runtime token: %v", err)
	}

	registerRecorder := httptest.NewRecorder()
	registerReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/register", strings.NewReader(`{"name":"test-worker","storageId":"msws_test_worker_storage","mode":"personal","version":"0.1.0","capabilities":`+capabilities+`,"labels":{"host":"local"},"agentEngineDiagnostics":{"codex":{"status":"ready"}}}`))
	registerReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(registerRecorder, registerReq)
	if registerRecorder.Code != http.StatusCreated {
		t.Fatalf("register worker status=%d body=%s", registerRecorder.Code, registerRecorder.Body.String())
	}
	var worker RuntimeWorker
	if err := json.Unmarshal(registerRecorder.Body.Bytes(), &worker); err != nil {
		t.Fatalf("parse worker: %v", err)
	}
	return tokenResult, worker
}

func createTestProjectViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, name string) Project {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"sourceType":"local","repoPath":%q,"defaultBranch":"main"}`, name, "/tmp/"+name)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create project status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var project Project
	if err := json.Unmarshal(recorder.Body.Bytes(), &project); err != nil {
		t.Fatalf("parse project: %v", err)
	}
	return project
}

func listIssuesViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID string) []IssueListItem {
	t.Helper()
	return listIssuesViaHTTPWithQuery(t, router, sessionToken, workspaceID, "")
}

func listIssuesViaHTTPWithQuery(t *testing.T, router http.Handler, sessionToken, workspaceID, query string) []IssueListItem {
	t.Helper()
	recorder := httptest.NewRecorder()
	path := "/api/workspaces/" + workspaceID + "/issues"
	if strings.TrimSpace(query) != "" {
		path += "?" + strings.TrimSpace(query)
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list issues status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var issues []IssueListItem
	if err := json.Unmarshal(recorder.Body.Bytes(), &issues); err != nil {
		t.Fatalf("parse issues: %v", err)
	}
	return issues
}

func issueListContains(issues []IssueListItem, issueID string) bool {
	for _, issue := range issues {
		if issue.ID == issueID {
			return true
		}
	}
	return false
}

func findCommentByID(comments []Comment, commentID string) Comment {
	for _, comment := range comments {
		if comment.ID == commentID {
			return comment
		}
	}
	return Comment{}
}

func createProjectTestCaseViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, projectID, body string) TestCase {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+projectID+"/test-cases", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create test case status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var testCase TestCase
	if err := json.Unmarshal(recorder.Body.Bytes(), &testCase); err != nil {
		t.Fatalf("parse test case: %v", err)
	}
	return testCase
}

func listProjectTestCasesViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, projectID string) []TestCase {
	t.Helper()
	return listProjectTestCasesPageViaHTTP(t, router, sessionToken, workspaceID, projectID, "").Cases
}

func listProjectTestCasesPageViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, projectID, query string) TestCaseListResult {
	t.Helper()
	recorder := httptest.NewRecorder()
	path := "/api/workspaces/" + workspaceID + "/projects/" + projectID + "/test-cases"
	if strings.TrimSpace(query) != "" {
		path += "?" + strings.TrimSpace(query)
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list test cases status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var result TestCaseListResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse test cases: %v", err)
	}
	return result
}

func getProjectTestCaseViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, projectID, caseID string) TestCase {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/projects/"+projectID+"/test-cases/"+caseID, nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("get test case status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var testCase TestCase
	if err := json.Unmarshal(recorder.Body.Bytes(), &testCase); err != nil {
		t.Fatalf("parse test case detail: %v", err)
	}
	return testCase
}

func findTestCase(t *testing.T, testCases []TestCase, caseID string) TestCase {
	t.Helper()
	for _, testCase := range testCases {
		if testCase.ID == caseID {
			return testCase
		}
	}
	t.Fatalf("test case %s not found in %+v", caseID, testCases)
	return TestCase{}
}

func listProjectTestCaseProposalsViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, projectID string) []TestCaseProposal {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/projects/"+projectID+"/test-case-proposals", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list proposals status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var proposals []TestCaseProposal
	if err := json.Unmarshal(recorder.Body.Bytes(), &proposals); err != nil {
		t.Fatalf("parse proposals: %v", err)
	}
	return proposals
}

func findTestCaseProposal(t *testing.T, proposals []TestCaseProposal, title string) TestCaseProposal {
	t.Helper()
	for _, proposal := range proposals {
		if proposal.Title == title {
			return proposal
		}
	}
	t.Fatalf("proposal %q not found in %+v", title, proposals)
	return TestCaseProposal{}
}

func applyProjectTestCaseProposalViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, projectID, proposalID, note string) ApplyTestCaseProposalResult {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+projectID+"/test-case-proposals/"+proposalID+"/apply", strings.NewReader(fmt.Sprintf(`{"note":%q}`, note)))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("apply proposal status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var result ApplyTestCaseProposalResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse proposal apply result: %v", err)
	}
	return result
}

func listProjectTestCaseRevisionsViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, projectID, caseID string) []TestCaseRevision {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/projects/"+projectID+"/test-cases/"+caseID+"/revisions", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list revisions status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var revisions []TestCaseRevision
	if err := json.Unmarshal(recorder.Body.Bytes(), &revisions); err != nil {
		t.Fatalf("parse revisions: %v", err)
	}
	return revisions
}

func createProjectTestPlanViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, projectID, body string) TestPlanDetail {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+projectID+"/test-plans", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create test plan status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var detail TestPlanDetail
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("parse test plan: %v", err)
	}
	return detail
}

func createWorkspaceTestPlanViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, body string) TestPlanDetail {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/test-plans", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create workspace test plan status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var detail TestPlanDetail
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("parse workspace test plan: %v", err)
	}
	return detail
}

func listWorkspaceTestPlansViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID string) []TestPlan {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/test-plans", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list workspace test plans status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var plans []TestPlan
	if err := json.Unmarshal(recorder.Body.Bytes(), &plans); err != nil {
		t.Fatalf("parse workspace test plans: %v", err)
	}
	return plans
}

func listProjectTestPlansViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, projectID string) []TestPlan {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/projects/"+projectID+"/test-plans", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list project test plans status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var plans []TestPlan
	if err := json.Unmarshal(recorder.Body.Bytes(), &plans); err != nil {
		t.Fatalf("parse project test plans: %v", err)
	}
	return plans
}

func startProjectTestRunViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, projectID, planID, body string) TestRunDetail {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+projectID+"/test-plans/"+planID+"/runs", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("start test run status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var detail TestRunDetail
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("parse test run: %v", err)
	}
	return detail
}

func startWorkspaceTestRunViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, planID, body string) TestRunDetail {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/test-plans/"+planID+"/runs", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("start workspace test run status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var detail TestRunDetail
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("parse workspace test run: %v", err)
	}
	return detail
}

func startAdHocProjectTestRunViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, projectID, body string) TestRunDetail {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+projectID+"/test-runs", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("start direct test run status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var detail TestRunDetail
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("parse direct test run: %v", err)
	}
	return detail
}

func listProjectTestRunsViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, projectID string) []TestRun {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/projects/"+projectID+"/test-runs", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list test runs status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var runs []TestRun
	if err := json.Unmarshal(recorder.Body.Bytes(), &runs); err != nil {
		t.Fatalf("parse test runs: %v", err)
	}
	return runs
}

func listWorkspaceTestRunsViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID string) []TestRun {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/test-runs", nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("list workspace test runs status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var runs []TestRun
	if err := json.Unmarshal(recorder.Body.Bytes(), &runs); err != nil {
		t.Fatalf("parse workspace test runs: %v", err)
	}
	return runs
}

func getProjectTestRunViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, projectID, runID string) TestRunDetail {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/projects/"+projectID+"/test-runs/"+runID, nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("get test run status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var detail TestRunDetail
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("parse test run detail: %v", err)
	}
	return detail
}

func getWorkspaceTestRunViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, runID string) TestRunDetail {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+workspaceID+"/test-runs/"+runID, nil)
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("get workspace test run status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var detail TestRunDetail
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("parse workspace test run detail: %v", err)
	}
	return detail
}

func cancelProjectTestRunViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, projectID, runID, reason string) TestRunDetail {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+projectID+"/test-runs/"+runID+"/cancel", strings.NewReader(fmt.Sprintf(`{"reason":%q}`, reason)))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("cancel test run status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var detail TestRunDetail
	if err := json.Unmarshal(recorder.Body.Bytes(), &detail); err != nil {
		t.Fatalf("parse cancelled test run: %v", err)
	}
	return detail
}

func reviewProjectTestRunViaHTTP(t *testing.T, router http.Handler, sessionToken, workspaceID, projectID, runID, action, note string) TestRun {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/projects/"+projectID+"/test-runs/"+runID+"/"+action, strings.NewReader(fmt.Sprintf(`{"note":%q}`, note)))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s test run status=%d body=%s", action, recorder.Code, recorder.Body.String())
	}
	var run TestRun
	if err := json.Unmarshal(recorder.Body.Bytes(), &run); err != nil {
		t.Fatalf("parse reviewed test run: %v", err)
	}
	return run
}

func findTestRunItem(t *testing.T, items []TestRunItem, caseID string) TestRunItem {
	t.Helper()
	for _, item := range items {
		if item.TestCaseID == caseID {
			return item
		}
	}
	t.Fatalf("test run item for case %s not found in %+v", caseID, items)
	return TestRunItem{}
}

func mustRuntimeTaskBySession(t *testing.T, store *MemoryStore, workspaceID, sessionID string) RuntimeTask {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, task := range store.runtimeTasks {
		if task.WorkspaceID == workspaceID && task.SessionID == sessionID {
			return task
		}
	}
	t.Fatalf("runtime task for session %s not found", sessionID)
	return RuntimeTask{}
}

func completeSessionTaskInMemoryStore(t *testing.T, store *MemoryStore, workerID, sessionID, completionPayload string) (RuntimeTask, error) {
	t.Helper()
	var input UpdateRuntimeTaskStatusInput
	if err := json.Unmarshal([]byte(completionPayload), &input); err != nil {
		return RuntimeTask{}, err
	}
	store.mu.Lock()
	var task RuntimeTask
	for _, candidate := range store.runtimeTasks {
		if candidate.SessionID == sessionID {
			task = candidate
			break
		}
	}
	if task.ID == "" {
		store.mu.Unlock()
		return RuntimeTask{}, fmt.Errorf("runtime task for session %s not found", sessionID)
	}
	task.ClaimedByWorkerID = workerID
	task.Status = "claimed"
	store.runtimeTasks[task.ID] = task
	registration := RuntimeRegistration{WorkspaceID: task.WorkspaceID}
	store.mu.Unlock()
	return store.UpdateRuntimeTaskStatus(context.Background(), registration, workerID, task.ID, input)
}

func claimAndCompleteTask(t *testing.T, router http.Handler, token, workerID, completionPayload string) (RuntimeTask, error) {
	t.Helper()
	claimRecorder := httptest.NewRecorder()
	claimReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+workerID+"/tasks/claim", nil)
	claimReq.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(claimRecorder, claimReq)
	if claimRecorder.Code != http.StatusOK {
		return RuntimeTask{}, fmt.Errorf("claim task status=%d body=%s", claimRecorder.Code, claimRecorder.Body.String())
	}
	var task RuntimeTask
	if err := json.Unmarshal(claimRecorder.Body.Bytes(), &task); err != nil {
		return RuntimeTask{}, err
	}

	completeRecorder := httptest.NewRecorder()
	completeReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+workerID+"/tasks/"+task.ID+"/status", strings.NewReader(completionPayload))
	completeReq.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(completeRecorder, completeReq)
	if completeRecorder.Code != http.StatusOK {
		return RuntimeTask{}, fmt.Errorf("complete task status=%d body=%s", completeRecorder.Code, completeRecorder.Body.String())
	}
	if err := json.Unmarshal(completeRecorder.Body.Bytes(), &task); err != nil {
		return RuntimeTask{}, err
	}
	return task, nil
}

func findAutoDeployTask(t *testing.T, store *MemoryStore, userID, workspaceID, issueID, sourceTaskID string) RuntimeTask {
	t.Helper()
	tasks, err := store.ListRuntimeTasks(context.Background(), userID, workspaceID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.ID != sourceTaskID && task.IssueID == issueID && isAutoDeployTestEnvironmentTask(task) {
			return task
		}
	}
	t.Fatalf("expected auto deploy task, got %+v", tasks)
	return RuntimeTask{}
}

func waitForIssueAnalysisTask(t *testing.T, store *MemoryStore, userID, workspaceID, issueID string) RuntimeTask {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		tasks, err := store.ListRuntimeTasks(context.Background(), userID, workspaceID)
		if err != nil {
			t.Fatalf("list tasks: %v", err)
		}
		for _, task := range tasks {
			if task.IssueID == issueID && task.Kind == "agent_session" && runtimeTaskAutomation(task) == issueAnalysisAutomation {
				return task
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected issue analysis task, got %+v", tasks)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForIssueTypeTriageTask(t *testing.T, store *MemoryStore, userID, workspaceID, issueID string) RuntimeTask {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		tasks, err := store.ListRuntimeTasks(context.Background(), userID, workspaceID)
		if err != nil {
			t.Fatalf("list tasks: %v", err)
		}
		for _, task := range tasks {
			if task.IssueID == issueID && task.Kind == "issue_type_triage" {
				return task
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected issue type triage task, got %+v", tasks)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func workerCompatibleSkillBundleContentHash(files []RuntimeSkillFile) string {
	hash := sha256.New()
	for _, file := range files {
		_, _ = hash.Write([]byte(file.Path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(file.SHA256)), "sha256:")))
		_, _ = hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func runtimeTaskBySession(store *MemoryStore, workspaceID, sessionID string) (RuntimeTask, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, task := range store.runtimeTasks {
		if task.WorkspaceID == workspaceID && task.SessionID == sessionID {
			return task, nil
		}
	}
	return RuntimeTask{}, ErrNotFound
}
