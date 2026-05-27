package control

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
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
	if payload.Capabilities["workspaceInboxIssueGrouping"] != true {
		t.Fatalf("expected workspace inbox grouping capability, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["teamWorkspaceCreation"] != true {
		t.Fatalf("expected team workspace creation capability, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["workspaceInvitations"] != true {
		t.Fatalf("expected workspace invitations capability, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["workspaceKinds"] != true {
		t.Fatalf("expected workspace kinds capability, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["githubAuth"] != false {
		t.Fatalf("expected disabled github auth capability without OAuth config, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["passwordAuth"] != true {
		t.Fatalf("expected password auth capability, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["runtimeWorkerRegistration"] != true {
		t.Fatalf("expected runtime worker registration capability, got %+v", payload.Capabilities)
	}
	if payload.Capabilities["runtimeTaskQueue"] != true {
		t.Fatalf("expected runtime task queue capability, got %+v", payload.Capabilities)
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
			name:   "create agent profile",
			method: http.MethodPost,
			path:   "/api/workspaces/" + workspaceID + "/agents",
			body:   `{"name":"Ops","mention":"@ops","provider":"codex","instructions":"Handle operational follow-up."}`,
		},
		{
			name:   "update agent profile",
			method: http.MethodPut,
			path:   "/api/workspaces/" + workspaceID + "/agents/codex",
			body:   `{"name":"Codex","mention":"@codex","provider":"codex","instructions":"Changed by member."}`,
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
	if len(detail.Comments) != 2 || detail.Comments[0].Body != "Server comment" || len(detail.Comments[0].Reactions) != 1 || detail.Comments[0].Reactions[0].Reaction != "rocket" {
		t.Fatalf("unexpected comments: %+v", detail.Comments)
	}

	noWorkerSessionRecorder := httptest.NewRecorder()
	noWorkerSessionReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+personalWorkspaceID+"/issues/"+createIssueResult.IssueID+"/sessions", strings.NewReader(`{"provider":"codex","agentProfile":"codex","runtimeMode":"personal","command":"@codex update the docs","triggerCommentId":"`+commentResult.CommentID+`"}`))
	noWorkerSessionReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(noWorkerSessionRecorder, noWorkerSessionReq)
	if noWorkerSessionRecorder.Code != http.StatusConflict || !strings.Contains(noWorkerSessionRecorder.Body.String(), "no active codex worker") {
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
	personalWorkerReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/register", strings.NewReader(`{"name":"personal-worker-1","mode":"personal","version":"0.1.0","capabilities":{"codex":true},"labels":{"host":"local"}}`))
	personalWorkerReq.Header.Set("Authorization", "Bearer "+personalTokenResult.Token)
	router.ServeHTTP(personalWorkerRecorder, personalWorkerReq)
	if personalWorkerRecorder.Code != http.StatusCreated {
		t.Fatalf("register personal worker status=%d body=%s", personalWorkerRecorder.Code, personalWorkerRecorder.Body.String())
	}

	createSessionRecorder := httptest.NewRecorder()
	createSessionReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+personalWorkspaceID+"/issues/"+createIssueResult.IssueID+"/sessions", strings.NewReader(`{"provider":"codex","agentProfile":"codex","runtimeMode":"personal","command":"@codex update the docs","triggerCommentId":"`+commentResult.CommentID+`"}`))
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

func TestIssueTypeTriageRuntimeTaskResultAppliesLabel(t *testing.T) {
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

	if err := server.enqueueIssueTypeTriage(context.Background(), user.ID, workspaceID, issueID); err != nil {
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

	runningRecorder := httptest.NewRecorder()
	runningReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/"+task.ID+"/status", strings.NewReader(`{"status":"running"}`))
	runningReq.Header.Set("Authorization", "Bearer "+tokenResult.Token)
	router.ServeHTTP(runningRecorder, runningReq)
	if runningRecorder.Code != http.StatusOK {
		t.Fatalf("running update status=%d body=%s", runningRecorder.Code, runningRecorder.Body.String())
	}

	completedRecorder := httptest.NewRecorder()
	completedReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/tasks/"+task.ID+"/status", strings.NewReader(`{"status":"completed","result":{"type":"fix","confidence":0.91,"reason":"bug fix"}}`))
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
	registerReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/register", strings.NewReader(`{"name":"team-worker-1","mode":"team","version":"0.1.0","capabilities":{"codex":true,"docker":true},"labels":{"region":"local"}}`))
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

	heartbeatRecorder := httptest.NewRecorder()
	heartbeatReq := httptest.NewRequest(http.MethodPost, "/api/runtime/workers/"+worker.ID+"/heartbeat", strings.NewReader(`{"status":"draining","currentLoad":2}`))
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
	personalTaskReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+personalWorkspaceID+"/runtime-tasks", strings.NewReader(`{"kind":"agent_session","runtimeMode":"team","requiredCapabilities":{"codex":true},"payload":{"prompt":"fix it"}}`))
	personalTaskReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(personalTaskRecorder, personalTaskReq)
	if personalTaskRecorder.Code != http.StatusForbidden {
		t.Fatalf("personal workspace team task should be forbidden, status=%d body=%s", personalTaskRecorder.Code, personalTaskRecorder.Body.String())
	}

	personalModeTaskRecorder := httptest.NewRecorder()
	personalModeTaskReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+personalWorkspaceID+"/runtime-tasks", strings.NewReader(`{"kind":"agent_session","runtimeMode":"personal","requiredCapabilities":{"codex":true},"payload":{"prompt":"fix it"}}`))
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
	rejectPersonalTaskReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/runtime-tasks", strings.NewReader(`{"kind":"agent_session","runtimeMode":"personal","requiredCapabilities":{"codex":true},"payload":{"prompt":"fix it"}}`))
	rejectPersonalTaskReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(rejectPersonalTaskRecorder, rejectPersonalTaskReq)
	if rejectPersonalTaskRecorder.Code != http.StatusForbidden {
		t.Fatalf("team workspace personal task should be forbidden, status=%d body=%s", rejectPersonalTaskRecorder.Code, rejectPersonalTaskRecorder.Body.String())
	}

	createTaskRecorder := httptest.NewRecorder()
	createTaskReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/runtime-tasks", strings.NewReader(`{"issueId":"issue-1","sessionId":"session-1","projectId":"project-1","kind":"agent_session","priority":5,"runtimeMode":"team","requiredCapabilities":{"codex":true},"payload":{"prompt":"fix it"}}`))
	createTaskReq.Header.Set("Authorization", "Bearer "+sessionToken)
	router.ServeHTTP(createTaskRecorder, createTaskReq)
	if createTaskRecorder.Code != http.StatusCreated {
		t.Fatalf("create runtime task status=%d body=%s", createTaskRecorder.Code, createTaskRecorder.Body.String())
	}
	var task RuntimeTask
	if err := json.Unmarshal(createTaskRecorder.Body.Bytes(), &task); err != nil {
		t.Fatalf("parse runtime task: %v", err)
	}
	if task.ID == "" || task.WorkspaceID != workspaceID || task.Status != "queued" || task.Kind != "agent_session" || task.RuntimeMode != "team" {
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
	if workerVisibleTask.ID != task.ID || workerVisibleTask.Status != "running" {
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
	createTask2Req := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/runtime-tasks", strings.NewReader(`{"issueId":"issue-2","sessionId":"session-2","projectId":"project-1","kind":"agent_session","priority":5,"runtimeMode":"team","requiredCapabilities":{"codex":true},"payload":{"prompt":"finish it"}}`))
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
	unmatchedTaskReq := httptest.NewRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/runtime-tasks", strings.NewReader(`{"issueId":"issue-2","kind":"agent_session","runtimeMode":"team","requiredCapabilities":{"kubectl":true},"payload":{"prompt":"deploy it"}}`))
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
