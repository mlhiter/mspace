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

func TestFixedAgentEngineCatalogAndReadOnlyHTTP(t *testing.T) {
	catalog := fixedAgentEngineCatalog()
	want := []AgentEngineCatalogItem{
		{ID: "codex", Name: "Codex", Mention: "@codex", Capability: "codex"},
		{ID: "claude_code", Name: "Claude Code", Mention: "@claude", Capability: "claudeCode"},
		{ID: "pi", Name: "Pi", Mention: "@pi", Capability: "pi"},
	}
	if len(catalog) != len(want) {
		t.Fatalf("expected %d fixed agents, got %+v", len(want), catalog)
	}
	for index := range want {
		if catalog[index] != want[index] {
			t.Fatalf("unexpected catalog item %d: got %+v want %+v", index, catalog[index], want[index])
		}
	}

	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "agent-catalog-user",
		Login:          "agent-catalog-user",
		Name:           "Agent Catalog User",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, _, err := store.CreateAuthSession(context.Background(), user.ID, time.Hour)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	router := NewServer(Config{}, store, fakeGitHubClient{}).Routes()
	path := "/api/workspaces/" + workspaces[0].ID + "/agents"

	getRecorder := httptest.NewRecorder()
	getRequest := httptest.NewRequest(http.MethodGet, path, nil)
	getRequest.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(getRecorder, getRequest)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("list fixed agents status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}
	var response []AgentEngineCatalogItem
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode fixed agents: %v", err)
	}
	if len(response) != 3 || response[0].ID != "codex" || response[1].ID != "claude_code" || response[2].ID != "pi" {
		t.Fatalf("unexpected fixed agent response: %+v", response)
	}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(method, path, strings.NewReader(`{"name":"custom"}`))
		request.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s fixed agents status=%d body=%s", method, recorder.Code, recorder.Body.String())
		}
	}
}

func TestNormalizeAgentEngineInputCompatibilityAndFailClosed(t *testing.T) {
	for _, test := range []struct {
		name          string
		agentEngine   string
		provider      string
		agentProfile  string
		want          string
		wantErrorText string
	}{
		{name: "default", want: "codex"},
		{name: "explicit codex", agentEngine: "codex", want: "codex"},
		{name: "explicit claude wins over aliases", agentEngine: "claude_code", provider: "codex", agentProfile: "bugfix", want: "claude_code"},
		{name: "explicit pi", agentEngine: "pi", want: "pi"},
		{name: "legacy codex", provider: "codex", agentProfile: "codex", want: "codex"},
		{name: "legacy built in profile", agentProfile: "@design", want: "codex"},
		{name: "unknown engine", agentEngine: "custom", wantErrorText: "agentEngine"},
		{name: "unknown legacy provider", provider: "claude", wantErrorText: "legacy provider"},
		{name: "deleted custom profile", agentProfile: "custom", wantErrorText: "no longer supported"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeAgentEngineInput(test.agentEngine, test.provider, test.agentProfile)
			if test.wantErrorText != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErrorText) {
					t.Fatalf("expected error containing %q, got %v", test.wantErrorText, err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("got engine=%q err=%v want=%q", got, err, test.want)
			}
		})
	}

	if got, err := agentEngineFromHistoricalPayload("", "codex", "old-custom-profile"); err != nil || got != "codex" {
		t.Fatalf("historical custom profile should remain readable as codex, got engine=%q err=%v", got, err)
	}
	if _, err := requireCodexWorkflowAgentEngine("claude_code", "", ""); err == nil {
		t.Fatal("system workflows must reject non-Codex engines")
	}

	var clientInput CreateAgentSessionInput
	if err := json.Unmarshal([]byte(`{"agentEngine":"pi","requiredCapabilities":{"codex":true}}`), &clientInput); err != nil {
		t.Fatalf("decode client input: %v", err)
	}
	if len(clientInput.RequiredCapabilities) != 0 {
		t.Fatalf("client must not control required capabilities: %s", clientInput.RequiredCapabilities)
	}
}

func TestNormalizeAgentSessionRuntimeTaskRoutesExactEngine(t *testing.T) {
	required, payload, err := normalizeAgentSessionRuntimeTask(
		json.RawMessage(`{"browser":true}`),
		json.RawMessage(`{"agentEngine":"pi","prompt":"Fix it"}`),
	)
	if err != nil {
		t.Fatalf("normalize Pi task: %v", err)
	}
	if string(required) != `{"browser":true,"pi":true}` {
		t.Fatalf("unexpected Pi capabilities: %s", required)
	}
	if strings.Contains(string(payload), "provider") || strings.Contains(string(payload), "agentProfile") || !strings.Contains(string(payload), `"agentEngine":"pi"`) {
		t.Fatalf("unexpected normalized Pi payload: %s", payload)
	}

	required, payload, err = normalizeAgentSessionRuntimeTask(
		json.RawMessage(`{}`),
		json.RawMessage(`{"provider":"codex","agentProfile":"design","prompt":"Fix it"}`),
	)
	if err != nil {
		t.Fatalf("normalize legacy Codex task: %v", err)
	}
	if string(required) != `{"codex":true}` || !strings.Contains(string(payload), `"agentEngine":"codex"`) {
		t.Fatalf("unexpected normalized legacy task: capabilities=%s payload=%s", required, payload)
	}

	for _, test := range []struct {
		name         string
		capabilities string
		payload      string
	}{
		{name: "conflicting engine capability", capabilities: `{"codex":true}`, payload: `{"agentEngine":"pi"}`},
		{name: "unknown explicit engine", capabilities: `{}`, payload: `{"agentEngine":"custom"}`},
		{name: "malformed explicit engine", capabilities: `{}`, payload: `{"agentEngine":123}`},
		{name: "non boolean engine capability", capabilities: `{"pi":"yes"}`, payload: `{"agentEngine":"pi"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := normalizeAgentSessionRuntimeTask(json.RawMessage(test.capabilities), json.RawMessage(test.payload)); err == nil {
				t.Fatal("expected invalid raw agent_session task to fail closed")
			}
		})
	}
}

func TestAgentSessionCapabilitiesAndPayloadUseExactEngine(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.CreatePasswordIdentity(context.Background(), PasswordAuthInput{
		Login:    "engine-session-user",
		Password: "password-123456",
		Name:     "Engine Session User",
	})
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	workspaceID := workspaces[0].ID
	project, err := store.CreateProject(context.Background(), user.ID, workspaceID, ProjectInput{
		Name:       "engine-project",
		SourceType: "local",
		RepoPath:   "/tmp/engine-project",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	tokenResult, err := store.CreateRuntimeRegistrationToken(context.Background(), user.ID, workspaceID, CreateRuntimeRegistrationTokenInput{
		Name:           "all-engine-worker",
		ExpiresInHours: 1,
	})
	if err != nil {
		t.Fatalf("create runtime token: %v", err)
	}
	registration, err := store.AuthenticateRuntimeRegistrationToken(context.Background(), tokenResult.Token)
	if err != nil {
		t.Fatalf("authenticate runtime token: %v", err)
	}
	if _, err := store.RegisterRuntimeWorker(context.Background(), registration, RuntimeWorkerInput{
		Name:         "all-engine-worker",
		StorageID:    "msws_engine_test_storage",
		Mode:         "personal",
		Version:      "test",
		Capabilities: json.RawMessage(`{"codex":true,"claudeCode":true,"pi":true,"issueWorkingCopyV1":true}`),
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	for _, test := range []struct {
		engine     string
		capability string
	}{
		{engine: "codex", capability: "codex"},
		{engine: "claude_code", capability: "claudeCode"},
		{engine: "pi", capability: "pi"},
	} {
		t.Run(test.engine, func(t *testing.T) {
			issueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{
				ProjectID: project.ID,
				Title:     "Run " + test.engine,
				Body:      "Exercise exact engine routing.",
			})
			if err != nil {
				t.Fatalf("create issue: %v", err)
			}
			session, err := store.CreateAgentSession(context.Background(), user.ID, workspaceID, issueID, CreateAgentSessionInput{
				AgentEngine: test.engine,
				RuntimeMode: "personal",
				Command:     "Inspect the project.",
			})
			if err != nil {
				t.Fatalf("create session: %v", err)
			}
			if session.AgentEngine != test.engine {
				t.Fatalf("unexpected session engine: %+v", session)
			}
			task := store.runtimeTasks[session.RuntimeTaskID]
			var capabilities map[string]bool
			if err := json.Unmarshal(task.RequiredCapabilities, &capabilities); err != nil {
				t.Fatalf("decode capabilities: %v", err)
			}
			if len(capabilities) != 2 || !capabilities[test.capability] || !capabilities["issueWorkingCopyV1"] {
				t.Fatalf("engine %s should require %s and issueWorkingCopyV1, got %s", test.engine, test.capability, task.RequiredCapabilities)
			}
			var payload map[string]any
			if err := json.Unmarshal(task.Payload, &payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			if payload["agentEngine"] != test.engine {
				t.Fatalf("payload engine=%v want=%s", payload["agentEngine"], test.engine)
			}
			if _, ok := payload["provider"]; ok {
				t.Fatalf("new payload must not include provider: %s", task.Payload)
			}
			if _, ok := payload["agentProfile"]; ok {
				t.Fatalf("new payload must not include agentProfile: %s", task.Payload)
			}
		})
	}

	issueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{
		ProjectID: project.ID,
		Title:     "Reject unknown engine",
		Body:      "Unknown engines must fail closed.",
	})
	if err != nil {
		t.Fatalf("create unknown-engine issue: %v", err)
	}
	if _, err := store.CreateAgentSession(context.Background(), user.ID, workspaceID, issueID, CreateAgentSessionInput{
		AgentEngine: "unknown",
		RuntimeMode: "personal",
		Command:     "Do not run.",
	}); err == nil || !strings.Contains(err.Error(), "agentEngine") {
		t.Fatalf("expected unknown engine rejection, got %v", err)
	}
}

func TestRuntimeTaskToAgentSessionReadsGenericAndHistoricalReferences(t *testing.T) {
	current, err := runtimeTaskToAgentSession(RuntimeTask{
		ID:          "task-current",
		SessionID:   "session-current",
		Kind:        "agent_session",
		Status:      "completed",
		RuntimeMode: "personal",
		Payload:     json.RawMessage(`{"agentEngine":"claude_code","prompt":"Fix it"}`),
		Result:      json.RawMessage(`{"agentEngine":"claude_code","engineSessionRef":"claude-session","engineRunRef":"claude-run"}`),
	})
	if err != nil {
		t.Fatalf("read current session: %v", err)
	}
	if current.AgentEngine != "claude_code" || current.EngineSessionRef != "claude-session" || current.EngineRunRef != "claude-run" {
		t.Fatalf("unexpected current session: %+v", current)
	}

	historical, err := runtimeTaskToAgentSession(RuntimeTask{
		ID:          "task-old",
		SessionID:   "session-old",
		Kind:        "agent_session",
		Status:      "completed",
		RuntimeMode: "personal",
		Payload:     json.RawMessage(`{"provider":"codex","agentProfile":"bugfix","prompt":"Fix it"}`),
		Result:      json.RawMessage(`{"threadId":"thread-old","turnId":"turn-old"}`),
	})
	if err != nil {
		t.Fatalf("read historical session: %v", err)
	}
	if historical.AgentEngine != "codex" || historical.CodexThreadID != "thread-old" || historical.CodexTurnID != "turn-old" || historical.EngineSessionRef != "thread-old" || historical.EngineRunRef != "turn-old" {
		t.Fatalf("unexpected historical session: %+v", historical)
	}

	if _, err := runtimeTaskToAgentSession(RuntimeTask{
		ID:      "task-mismatch",
		Payload: json.RawMessage(`{"agentEngine":"pi"}`),
		Result:  json.RawMessage(`{"agentEngine":"codex"}`),
	}); err == nil {
		t.Fatal("expected mismatched result engine to fail closed")
	}
}

func TestAgentProfileMigrationAndSQLiteSnapshotRemoval(t *testing.T) {
	migration010, err := migrationFS.ReadFile("migrations/010_server_owned_runtime_surfaces.sql")
	if err != nil {
		t.Fatalf("read migration 010: %v", err)
	}
	if strings.Contains(string(migration010), "agent_profiles") {
		t.Fatalf("migration 010 must not recreate agent_profiles:\n%s", migration010)
	}
	migration030, err := migrationFS.ReadFile("migrations/030_fixed_agent_engines.sql")
	if err != nil {
		t.Fatalf("read migration 030: %v", err)
	}
	if !strings.Contains(string(migration030), "DROP TABLE IF EXISTS agent_profiles") {
		t.Fatalf("migration 030 must drop agent_profiles: %s", migration030)
	}

	var snapshot memoryStoreSnapshot
	if err := json.Unmarshal([]byte(`{"schemaVersion":1,"agentProfiles":{"workspace:custom":{"id":"custom"}}}`), &snapshot); err != nil {
		t.Fatalf("decode legacy sqlite snapshot: %v", err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("encode current sqlite snapshot: %v", err)
	}
	if strings.Contains(string(encoded), "agentProfiles") {
		t.Fatalf("current sqlite snapshot must not persist agent profiles: %s", encoded)
	}
}
