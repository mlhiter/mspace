package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunOnceCompletesProtocolSmokeTask(t *testing.T) {
	var mu sync.Mutex
	statuses := []string{}
	offlineHeartbeat := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer msw_test" {
			t.Fatalf("missing worker token on %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/api/runtime/workers/register":
			assertMethod(t, r, http.MethodPost)
			var input runtimeWorkerInput
			decodeBody(t, r, &input)
			if input.Name != "worker-test" || input.Mode != "team" || !strings.Contains(string(input.Capabilities), "protocolSmoke") {
				t.Fatalf("unexpected register input: %+v capabilities=%s", input, input.Capabilities)
			}
			writeJSON(t, w, http.StatusCreated, runtimeWorker{ID: "worker-1", WorkspaceID: "workspace-1", Name: input.Name, Mode: input.Mode, Status: "online"})
		case "/api/runtime/workers/worker-1/heartbeat":
			assertMethod(t, r, http.MethodPost)
			var input runtimeWorkerInput
			decodeBody(t, r, &input)
			if input.Status == "offline" {
				offlineHeartbeat = true
			}
			writeJSON(t, w, http.StatusOK, runtimeWorker{ID: "worker-1", WorkspaceID: "workspace-1", Name: "worker-test", Mode: "team", Status: input.Status, CurrentLoad: input.CurrentLoad})
		case "/api/runtime/workers/worker-1/tasks/claim":
			assertMethod(t, r, http.MethodPost)
			writeJSON(t, w, http.StatusOK, runtimeTask{
				ID:                   "task-1",
				WorkspaceID:          "workspace-1",
				Kind:                 "protocol_smoke",
				RuntimeMode:          "team",
				RequiredCapabilities: json.RawMessage(`{"protocolSmoke":true}`),
				Payload:              json.RawMessage(`{"source":"test"}`),
			})
		case "/api/runtime/workers/worker-1/tasks/task-1/status":
			assertMethod(t, r, http.MethodPost)
			var input updateTaskStatusInput
			decodeBody(t, r, &input)
			mu.Lock()
			statuses = append(statuses, input.Status)
			mu.Unlock()
			if input.Status == "completed" {
				if !strings.Contains(string(input.Result), `"dryRun":true`) {
					t.Fatalf("expected dry-run result, got %s", input.Result)
				}
			}
			writeJSON(t, w, http.StatusOK, runtimeTask{ID: "task-1", WorkspaceID: "workspace-1", Kind: "protocol_smoke", Status: input.Status, Result: input.Result})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	if err := run(context.Background(), cfg, discardLogger()); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if got := strings.Join(statuses, ","); got != "running,completed" {
		t.Fatalf("unexpected statuses: %s", got)
	}
	if !offlineHeartbeat {
		t.Fatalf("expected offline heartbeat before exit")
	}
}

func TestReadTestResultArtifactAcceptsArrayShape(t *testing.T) {
	artifactDir := t.TempDir()
	data := `[
		{
			"runId": "test-run-1",
			"caseId": "test-case-1",
			"status": "passed",
			"actualResult": "Passed through CDP.",
			"evidence": {"screenshot": "homepage.png"}
		}
	]`
	if err := os.WriteFile(filepath.Join(artifactDir, testResultArtifactName), []byte(data), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	artifact, ok := readTestResultArtifact(agentSessionPayload{ArtifactDir: artifactDir})
	if !ok {
		t.Fatal("expected array-shaped test-result artifact to be accepted")
	}
	if artifact.RunID != "test-run-1" || len(artifact.Items) != 1 {
		t.Fatalf("unexpected artifact: %+v", artifact)
	}
	item := artifact.Items[0]
	if item.RunID != "test-run-1" || item.CaseID != "test-case-1" || item.Status != "passed" {
		t.Fatalf("unexpected item: %+v", item)
	}
	if !strings.Contains(string(item.Evidence), "homepage.png") {
		t.Fatalf("expected evidence to be preserved, got %s", item.Evidence)
	}
}

func TestReadTestSetupResultArtifact(t *testing.T) {
	artifactDir := t.TempDir()
	data := `{
		"runId": "test-run-setup",
		"status": "passed",
		"summary": "Preview is ready.",
		"outputs": {"previewUrl": "https://setup.example.test"},
		"steps": [{"title": "Update image", "status": "passed", "command": "kubectl set image deployment/app app=image:rc"}]
	}`
	if err := os.WriteFile(filepath.Join(artifactDir, testSetupResultArtifactName), []byte(data), 0o644); err != nil {
		t.Fatalf("write setup artifact: %v", err)
	}

	artifact, ok := readTestSetupResultArtifact(agentSessionPayload{ArtifactDir: artifactDir})
	if !ok {
		t.Fatal("expected test setup artifact to be accepted")
	}
	if artifact.RunID != "test-run-setup" || artifact.Status != "passed" || len(artifact.Steps) != 1 {
		t.Fatalf("unexpected setup artifact: %+v", artifact)
	}
	if !strings.Contains(string(artifact.Outputs), "setup.example.test") {
		t.Fatalf("expected setup outputs to be preserved, got %s", artifact.Outputs)
	}
}

func TestReadTestResultArtifactEmbedsScreenshotImages(t *testing.T) {
	artifactDir := t.TempDir()
	pngBytes := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(filepath.Join(artifactDir, "homepage.png"), pngBytes, 0o644); err != nil {
		t.Fatalf("write screenshot: %v", err)
	}
	data := `{
		"runId": "test-run-1",
		"items": [
			{
				"caseId": "test-case-1",
				"status": "passed",
				"actualResult": "Passed through CDP.",
				"evidence": {"screenshot": "homepage.png"}
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(artifactDir, testResultArtifactName), []byte(data), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	artifact, ok := readTestResultArtifact(agentSessionPayload{ArtifactDir: artifactDir})
	if !ok || len(artifact.Items) != 1 {
		t.Fatalf("expected test result artifact, got %+v", artifact)
	}
	evidence := string(artifact.Items[0].Evidence)
	if !strings.Contains(evidence, `"screenshotImages"`) || !strings.Contains(evidence, `data:image/png;base64`) {
		t.Fatalf("expected embedded screenshot image, got %s", evidence)
	}
}

func TestRunOnceFailsInvalidAgentSessionPayload(t *testing.T) {
	var completedStatus string
	var completedError string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/runtime/workers/register":
			writeJSON(t, w, http.StatusCreated, runtimeWorker{ID: "worker-1", WorkspaceID: "workspace-1", Name: "worker-test", Mode: "team", Status: "online"})
		case "/api/runtime/workers/worker-1/heartbeat":
			writeJSON(t, w, http.StatusOK, runtimeWorker{ID: "worker-1", WorkspaceID: "workspace-1", Name: "worker-test", Mode: "team", Status: "online"})
		case "/api/runtime/workers/worker-1/tasks/claim":
			writeJSON(t, w, http.StatusOK, runtimeTask{ID: "task-2", WorkspaceID: "workspace-1", Kind: "agent_session", RuntimeMode: "team"})
		case "/api/runtime/workers/worker-1/tasks/task-2/status":
			var input updateTaskStatusInput
			decodeBody(t, r, &input)
			if input.Status != "running" {
				completedStatus = input.Status
				completedError = input.Error
			}
			writeJSON(t, w, http.StatusOK, runtimeTask{ID: "task-2", WorkspaceID: "workspace-1", Kind: "agent_session", Status: input.Status, Error: input.Error})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	if err := run(context.Background(), testConfig(server.URL), discardLogger()); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if completedStatus != "failed" || !strings.Contains(completedError, "requires prompt") {
		t.Fatalf("expected invalid agent session payload to fail honestly, status=%q error=%q", completedStatus, completedError)
	}
}

func TestRunOnceCompletesAgentSessionWithCodexAppServer(t *testing.T) {
	installFakeCodex(t)

	repoDir := createTestGitRepo(t)
	workRoot := filepath.Join(t.TempDir(), "worker-root")
	var mu sync.Mutex
	statuses := []string{}
	logs := []appendTaskLogInput{}
	var completedResult agentSessionResult
	var completedError string

	payload, err := json.Marshal(agentSessionPayload{
		Prompt:    "write a concise completion message",
		IssueID:   "issue-1",
		SessionID: "session-1",
		ProjectID: "project-1",
		Branch:    "mspace/issue/session-1",
		Repository: repositorySpec{
			URL:           repoDir,
			DefaultBranch: "main",
			Provider:      "local",
			Owner:         "test",
			Repo:          "demo",
		},
		ContextMarkdown: "# context\n",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer msw_test" {
			t.Fatalf("missing worker token on %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/api/runtime/workers/register":
			writeJSON(t, w, http.StatusCreated, runtimeWorker{ID: "worker-1", WorkspaceID: "workspace-1", Name: "worker-test", Mode: "team", Status: "online"})
		case "/api/runtime/workers/worker-1/heartbeat":
			writeJSON(t, w, http.StatusOK, runtimeWorker{ID: "worker-1", WorkspaceID: "workspace-1", Name: "worker-test", Mode: "team", Status: "online"})
		case "/api/runtime/workers/worker-1/tasks/claim":
			writeJSON(t, w, http.StatusOK, runtimeTask{
				ID:                   "task-3",
				WorkspaceID:          "workspace-1",
				Kind:                 "agent_session",
				RuntimeMode:          "team",
				RequiredCapabilities: json.RawMessage(`{"codex":true}`),
				Payload:              payload,
			})
		case "/api/runtime/workers/worker-1/tasks/task-3/logs":
			assertMethod(t, r, http.MethodPost)
			var input appendTaskLogInput
			decodeBody(t, r, &input)
			mu.Lock()
			logs = append(logs, input)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		case "/api/runtime/workers/worker-1/tasks/task-3/status":
			assertMethod(t, r, http.MethodPost)
			var input updateTaskStatusInput
			decodeBody(t, r, &input)
			mu.Lock()
			statuses = append(statuses, input.Status)
			completedError = input.Error
			if input.Status == "completed" {
				if err := json.Unmarshal(input.Result, &completedResult); err != nil {
					t.Fatalf("decode completed result: %v", err)
				}
			}
			mu.Unlock()
			writeJSON(t, w, http.StatusOK, runtimeTask{ID: "task-3", WorkspaceID: "workspace-1", Kind: "agent_session", Status: input.Status, Result: input.Result})
		case "/api/runtime/workers/worker-1/tasks/task-3":
			assertMethod(t, r, http.MethodGet)
			writeJSON(t, w, http.StatusOK, runtimeTask{ID: "task-3", WorkspaceID: "workspace-1", Kind: "agent_session", Status: "running"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	cfg.Capabilities = json.RawMessage(`{"protocolSmoke":true,"codex":true,"dryRun":false}`)
	cfg.WorkRoot = workRoot
	if err := run(context.Background(), cfg, discardLogger()); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if got := strings.Join(statuses, ","); got != "running,completed" {
		t.Fatalf("unexpected statuses: %s error=%q logs=%s", got, completedError, taskLogMessages(logs))
	}
	if completedResult.ThreadID != "thread-test" || completedResult.TurnID != "turn-test" || completedResult.Status != "completed" || completedResult.DryRun {
		t.Fatalf("unexpected completed result: %+v", completedResult)
	}
	if completedResult.Workdir == "" || !strings.HasPrefix(completedResult.Workdir, workRoot) {
		t.Fatalf("expected worker-managed workdir under %s, got %+v", workRoot, completedResult)
	}
	if completedResult.ArtifactDir == "" || !strings.HasPrefix(completedResult.ArtifactDir, completedResult.Workdir) {
		t.Fatalf("expected worker-managed artifact dir, got %+v", completedResult)
	}
	if completedResult.Source.CommitSHA == "" || completedResult.Source.FilesChanged == 0 {
		t.Fatalf("expected worker source commit metadata, got %+v", completedResult.Source)
	}
	if completedResult.Source.Changes[0].Path != "worker-output.txt" {
		t.Fatalf("expected fake codex file change to be captured, got %+v", completedResult.Source.Changes)
	}
	if completedResult.TestCaseProposals == nil || len(completedResult.TestCaseProposals.Proposals) != 1 {
		t.Fatalf("expected test case proposals artifact, got %+v", completedResult.TestCaseProposals)
	}
	if completedResult.TestResult == nil || len(completedResult.TestResult.Items) != 1 {
		t.Fatalf("expected test result artifact, got %+v", completedResult.TestResult)
	}
	joinedLogs := taskLogMessages(logs)
	if !strings.Contains(joinedLogs, "Prepared worker workspace: "+completedResult.Workdir) {
		t.Fatalf("expected worker workspace log, got %s", joinedLogs)
	}
	if !strings.Contains(joinedLogs, "Codex app-server ready: fake-codex") {
		t.Fatalf("expected app-server ready log, got %s", joinedLogs)
	}
	if !strings.Contains(joinedLogs, "fake agent completed") {
		t.Fatalf("expected agent message log, got %s", joinedLogs)
	}
	if !strings.Contains(joinedLogs, "turn-completed") {
		t.Fatalf("expected turn completion log, got %s", joinedLogs)
	}
}

func TestRunOnceCompletesDryRunAgentSessionInWorkerWorkspace(t *testing.T) {
	repoDir := createTestGitRepo(t)
	workRoot := filepath.Join(t.TempDir(), "worker-root")
	var mu sync.Mutex
	statuses := []string{}
	logs := []appendTaskLogInput{}
	var completedResult agentSessionResult
	var completedError string

	payload, err := json.Marshal(agentSessionPayload{
		Prompt:    "exercise the docker server worker path",
		IssueID:   "issue-dry",
		SessionID: "session-dry",
		ProjectID: "project-dry",
		Branch:    "mspace/issue/session-dry",
		Repository: repositorySpec{
			URL:           repoDir,
			DefaultBranch: "main",
			Provider:      "local",
			Owner:         "test",
			Repo:          "demo-dry",
		},
		ContextMarkdown: "# dry-run context\n",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer msw_test" {
			t.Fatalf("missing worker token on %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/api/runtime/workers/register":
			writeJSON(t, w, http.StatusCreated, runtimeWorker{ID: "worker-1", WorkspaceID: "workspace-1", Name: "worker-test", Mode: "team", Status: "online"})
		case "/api/runtime/workers/worker-1/heartbeat":
			writeJSON(t, w, http.StatusOK, runtimeWorker{ID: "worker-1", WorkspaceID: "workspace-1", Name: "worker-test", Mode: "team", Status: "online"})
		case "/api/runtime/workers/worker-1/tasks/claim":
			writeJSON(t, w, http.StatusOK, runtimeTask{
				ID:                   "task-dry-run",
				WorkspaceID:          "workspace-1",
				Kind:                 "agent_session",
				RuntimeMode:          "team",
				RequiredCapabilities: json.RawMessage(`{"codex":true}`),
				Payload:              payload,
			})
		case "/api/runtime/workers/worker-1/tasks/task-dry-run/logs":
			assertMethod(t, r, http.MethodPost)
			var input appendTaskLogInput
			decodeBody(t, r, &input)
			mu.Lock()
			logs = append(logs, input)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		case "/api/runtime/workers/worker-1/tasks/task-dry-run/status":
			assertMethod(t, r, http.MethodPost)
			var input updateTaskStatusInput
			decodeBody(t, r, &input)
			mu.Lock()
			statuses = append(statuses, input.Status)
			completedError = input.Error
			if input.Status == "completed" {
				if err := json.Unmarshal(input.Result, &completedResult); err != nil {
					t.Fatalf("decode completed result: %v", err)
				}
			}
			mu.Unlock()
			writeJSON(t, w, http.StatusOK, runtimeTask{ID: "task-dry-run", WorkspaceID: "workspace-1", Kind: "agent_session", Status: input.Status, Result: input.Result})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	cfg.Capabilities = json.RawMessage(`{"protocolSmoke":true,"codex":true,"dryRun":true}`)
	cfg.WorkRoot = workRoot
	if err := run(context.Background(), cfg, discardLogger()); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if got := strings.Join(statuses, ","); got != "running,completed" {
		t.Fatalf("unexpected statuses: %s error=%q logs=%s", got, completedError, taskLogMessages(logs))
	}
	if !completedResult.DryRun || completedResult.ThreadID == "" || completedResult.TurnID == "" || completedResult.Status != "completed" {
		t.Fatalf("unexpected dry-run result: %+v", completedResult)
	}
	if completedResult.Workdir == "" || !strings.HasPrefix(completedResult.Workdir, workRoot) {
		t.Fatalf("expected worker-managed workdir under %s, got %+v", workRoot, completedResult)
	}
	if completedResult.Source.CommitSHA == "" || completedResult.Source.FilesChanged == 0 {
		t.Fatalf("expected dry-run source commit metadata, got %+v", completedResult.Source)
	}
	foundDryRunFile := false
	for _, change := range completedResult.Source.Changes {
		if change.Path == "TEAM_RUNTIME_DRY_RUN.md" {
			foundDryRunFile = true
		}
	}
	if !foundDryRunFile {
		t.Fatalf("expected dry-run file to be captured, got %+v", completedResult.Source.Changes)
	}
	if !strings.Contains(taskLogMessages(logs), "Running dry-run agent session") {
		t.Fatalf("expected dry-run log, got %s", taskLogMessages(logs))
	}
}

func TestRunOnceStopsAgentSessionWhenTaskCancelled(t *testing.T) {
	installSlowFakeCodex(t)

	workdir := t.TempDir()
	var mu sync.Mutex
	statuses := []string{}
	logs := []appendTaskLogInput{}
	taskPolls := 0

	payload, err := json.Marshal(agentSessionPayload{
		Workdir: workdir,
		Prompt:  "wait for cancellation",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer msw_test" {
			t.Fatalf("missing worker token on %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/api/runtime/workers/register":
			writeJSON(t, w, http.StatusCreated, runtimeWorker{ID: "worker-1", WorkspaceID: "workspace-1", Name: "worker-test", Mode: "team", Status: "online"})
		case "/api/runtime/workers/worker-1/heartbeat":
			writeJSON(t, w, http.StatusOK, runtimeWorker{ID: "worker-1", WorkspaceID: "workspace-1", Name: "worker-test", Mode: "team", Status: "online"})
		case "/api/runtime/workers/worker-1/tasks/claim":
			writeJSON(t, w, http.StatusOK, runtimeTask{
				ID:                   "task-cancel",
				WorkspaceID:          "workspace-1",
				Kind:                 "agent_session",
				RuntimeMode:          "team",
				RequiredCapabilities: json.RawMessage(`{"codex":true}`),
				Payload:              payload,
			})
		case "/api/runtime/workers/worker-1/tasks/task-cancel":
			assertMethod(t, r, http.MethodGet)
			mu.Lock()
			taskPolls++
			mu.Unlock()
			writeJSON(t, w, http.StatusOK, runtimeTask{ID: "task-cancel", WorkspaceID: "workspace-1", Kind: "agent_session", Status: "cancelled", Error: "user stopped session"})
		case "/api/runtime/workers/worker-1/tasks/task-cancel/logs":
			assertMethod(t, r, http.MethodPost)
			var input appendTaskLogInput
			decodeBody(t, r, &input)
			mu.Lock()
			logs = append(logs, input)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		case "/api/runtime/workers/worker-1/tasks/task-cancel/status":
			assertMethod(t, r, http.MethodPost)
			var input updateTaskStatusInput
			decodeBody(t, r, &input)
			mu.Lock()
			statuses = append(statuses, input.Status)
			mu.Unlock()
			if input.Status == "cancelled" {
				writeJSON(t, w, http.StatusOK, runtimeTask{ID: "task-cancel", WorkspaceID: "workspace-1", Kind: "agent_session", Status: "cancelled", Error: input.Error})
				return
			}
			writeJSON(t, w, http.StatusOK, runtimeTask{ID: "task-cancel", WorkspaceID: "workspace-1", Kind: "agent_session", Status: input.Status})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := testConfig(server.URL)
	cfg.Capabilities = json.RawMessage(`{"protocolSmoke":true,"codex":true,"dryRun":false}`)
	if err := run(context.Background(), cfg, discardLogger()); err != nil {
		t.Fatalf("run once: %v", err)
	}
	if got := strings.Join(statuses, ","); got != "running,cancelled" {
		t.Fatalf("unexpected statuses: %s logs=%s", got, taskLogMessages(logs))
	}
	if taskPolls == 0 {
		t.Fatalf("expected worker to poll claimed task for cancellation")
	}
	if !strings.Contains(taskLogMessages(logs), "Cancellation requested by control plane.") {
		t.Fatalf("expected cancellation log, got %s", taskLogMessages(logs))
	}
}

func TestConfigRequiresRuntimeToken(t *testing.T) {
	_, err := normalizeConfig(config{
		ServerURL:         "http://127.0.0.1:8787",
		Token:             "msp_wrong",
		Name:              "worker",
		Mode:              "team",
		Version:           workerVersion,
		Capabilities:      json.RawMessage(`{}`),
		Labels:            json.RawMessage(`{}`),
		PollInterval:      time.Second,
		HeartbeatInterval: time.Second,
	})
	if err == nil || !strings.Contains(err.Error(), "msw_") {
		t.Fatalf("expected msw token validation error, got %v", err)
	}
}

func TestRuntimeClientReadsUpdatedTokenFile(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "runtime.token")
	if err := os.WriteFile(tokenPath, []byte("msw_first\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	cfg, err := normalizeConfig(config{
		ServerURL:         "http://127.0.0.1:8787",
		TokenFile:         tokenPath,
		Name:              "worker",
		Mode:              "personal",
		Version:           workerVersion,
		Capabilities:      json.RawMessage(`{}`),
		Labels:            json.RawMessage(`{}`),
		WorkRoot:          t.TempDir(),
		PollInterval:      time.Second,
		HeartbeatInterval: time.Second,
	})
	if err != nil {
		t.Fatalf("normalize config: %v", err)
	}
	client := runtimeClient{token: cfg.Token, tokenFile: cfg.TokenFile}
	token, err := client.runtimeToken()
	if err != nil || token != "msw_first" {
		t.Fatalf("expected first token, got token=%q err=%v", token, err)
	}
	if err := os.WriteFile(tokenPath, []byte("msw_second\n"), 0o600); err != nil {
		t.Fatalf("rotate token: %v", err)
	}
	token, err = client.runtimeToken()
	if err != nil || token != "msw_second" {
		t.Fatalf("expected rotated token, got token=%q err=%v", token, err)
	}
}

func TestDefaultAgentSessionDeveloperInstructionsAvoidDevServerPreviewURLs(t *testing.T) {
	instructions := defaultAgentSessionDeveloperInstructions()
	required := []string{
		"team runtime worker",
		"Do not start or keep a development server running unless the user explicitly asks",
		"prefer non-interactive checks",
		"If a temporary server is required for validation, stop it before finishing",
		"Do not present container-local localhost or 127.0.0.1 URLs as user-accessible preview URLs",
		"branch-name.json",
		"fix/short-semantic-name",
	}
	for _, text := range required {
		if !strings.Contains(instructions, text) {
			t.Fatalf("expected default instructions to contain %q, got:\n%s", text, instructions)
		}
	}
}

func testConfig(serverURL string) config {
	return config{
		ServerURL:         serverURL,
		Token:             "msw_test",
		Name:              "worker-test",
		Mode:              "team",
		Version:           workerVersion,
		Capabilities:      json.RawMessage(`{"protocolSmoke":true,"codex":false,"dryRun":true}`),
		Labels:            json.RawMessage(`{"test":true}`),
		WorkRoot:          filepath.Join(os.TempDir(), "mspace-worker-test"),
		PollInterval:      time.Millisecond,
		HeartbeatInterval: time.Second,
		Once:              true,
	}
}

func createTestGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	repoDir := t.TempDir()
	runGit(t, repoDir, "init", "-b", "main")
	runGit(t, repoDir, "config", "user.name", "mspace test")
	runGit(t, repoDir, "config", "user.email", "mspace@example.com")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "base")
	return repoDir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func installFakeCodex(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	script := `#!/bin/sh
if [ "$1" != "app-server" ] || [ "$2" != "--listen" ] || [ "$3" != "stdio://" ]; then
  echo "unexpected fake codex args: $*" >&2
  exit 2
fi
python3 -u -c '
import json
import os
import sys

thread_id = "thread-test"
turn_id = "turn-test"

def emit(payload):
    print(json.dumps(payload, separators=(",", ":")), flush=True)

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    request = json.loads(line)
    request_id = request.get("id")
    method = request.get("method")
    if method == "initialize":
        emit({"id": request_id, "result": {"userAgent": "fake-codex", "codexHome": "/tmp/fake-codex"}})
    elif method == "thread/start":
        emit({"id": request_id, "result": {"thread": {"id": thread_id}, "model": "fake-model", "modelProvider": "fake-provider"}})
    elif method == "turn/start":
        with open("worker-output.txt", "w", encoding="utf-8") as handle:
            handle.write("created by fake codex in " + os.getcwd() + "\n")
        artifact_dir = os.environ.get("MSPACE_SESSION_ARTIFACT_DIR")
        if artifact_dir:
            os.makedirs(artifact_dir, exist_ok=True)
            with open(os.path.join(artifact_dir, "test-case-proposals.json"), "w", encoding="utf-8") as handle:
                json.dump({"proposals":[{"type":"create","title":"Generated login smoke","summary":"Generated by fake Codex","proposedCase":{"title":"Generated login smoke","type":"functional","steps":[{"action":"Open login"}],"expectedResult":"Login opens."}}]}, handle)
            with open(os.path.join(artifact_dir, "test-result.json"), "w", encoding="utf-8") as handle:
                json.dump({"runId":"test-run-1","items":[{"caseId":"test-case-1","status":"passed","actualResult":"Passed in fake Codex.","evidence":{"log":"ok"}}]}, handle)
        emit({"id": request_id, "result": {"turn": {"id": turn_id, "status": "running", "items": []}}})
        emit({"method": "turn/started", "params": {"threadId": thread_id, "turn": {"id": turn_id, "status": "running", "items": []}}})
        emit({"method": "item/started", "params": {"threadId": thread_id, "turnId": turn_id, "item": {"id": "item-1", "type": "agentMessage"}}})
        emit({"method": "item/completed", "params": {"threadId": thread_id, "turnId": turn_id, "item": {"id": "item-1", "type": "agentMessage", "text": "fake agent completed"}}})
        emit({"method": "turn/completed", "params": {"threadId": thread_id, "turn": {"id": turn_id, "status": "completed", "items": []}}})
    elif method == "turn/interrupt":
        emit({"id": request_id, "result": {}})
    else:
        emit({"id": request_id, "error": {"code": -32601, "message": "method not found"}})
'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func installSlowFakeCodex(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	script := `#!/bin/sh
if [ "$1" != "app-server" ] || [ "$2" != "--listen" ] || [ "$3" != "stdio://" ]; then
  echo "unexpected fake codex args: $*" >&2
  exit 2
fi
python3 -u -c '
import json
import sys
import time

thread_id = "thread-cancel"
turn_id = "turn-cancel"

def emit(payload):
    print(json.dumps(payload, separators=(",", ":")), flush=True)

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    request = json.loads(line)
    request_id = request.get("id")
    method = request.get("method")
    if method == "initialize":
        emit({"id": request_id, "result": {"userAgent": "fake-codex", "codexHome": "/tmp/fake-codex"}})
    elif method == "thread/start":
        emit({"id": request_id, "result": {"thread": {"id": thread_id}, "model": "fake-model", "modelProvider": "fake-provider"}})
    elif method == "turn/start":
        emit({"id": request_id, "result": {"turn": {"id": turn_id, "status": "running", "items": []}}})
        emit({"method": "turn/started", "params": {"threadId": thread_id, "turn": {"id": turn_id, "status": "running", "items": []}}})
    elif method == "turn/interrupt":
        emit({"id": request_id, "result": {}})
        emit({"method": "turn/completed", "params": {"threadId": thread_id, "turn": {"id": turn_id, "status": "interrupted", "items": []}}})
    else:
        emit({"id": request_id, "error": {"code": -32601, "message": "method not found"}})
'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write slow fake codex: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func taskLogMessages(logs []appendTaskLogInput) string {
	messages := make([]string, 0, len(logs))
	for _, log := range logs {
		messages = append(messages, log.Stream+": "+log.Message)
	}
	return strings.Join(messages, "\n")
}

func assertMethod(t *testing.T, r *http.Request, method string) {
	t.Helper()
	if r.Method != method {
		t.Fatalf("expected method %s, got %s", method, r.Method)
	}
}

func decodeBody(t *testing.T, r *http.Request, target any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("write response: %v", err)
	}
}
