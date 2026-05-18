package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const workerVersion = "0.1.0"
const codexProtocolLineLimit = 16 * 1024 * 1024
const branchNameArtifactName = "branch-name.json"

var branchSlugUnsafePattern = regexp.MustCompile(`[^a-z0-9]+`)

type config struct {
	ServerURL         string
	Token             string
	Name              string
	Mode              string
	Version           string
	Capabilities      json.RawMessage
	Labels            json.RawMessage
	WorkRoot          string
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	Once              bool
}

type runtimeWorkerInput struct {
	Name         string          `json:"name,omitempty"`
	Mode         string          `json:"mode,omitempty"`
	Status       string          `json:"status,omitempty"`
	Version      string          `json:"version,omitempty"`
	CurrentLoad  int             `json:"currentLoad"`
	Capabilities json.RawMessage `json:"capabilities,omitempty"`
	Labels       json.RawMessage `json:"labels,omitempty"`
}

type runtimeWorker struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspaceId"`
	Name         string          `json:"name"`
	Mode         string          `json:"mode"`
	Status       string          `json:"status"`
	Version      string          `json:"version"`
	CurrentLoad  int             `json:"currentLoad"`
	Capabilities json.RawMessage `json:"capabilities"`
	Labels       json.RawMessage `json:"labels"`
	LastSeenAt   string          `json:"lastSeenAt"`
	CreatedAt    string          `json:"createdAt"`
	UpdatedAt    string          `json:"updatedAt"`
}

type runtimeTask struct {
	ID                   string          `json:"id"`
	WorkspaceID          string          `json:"workspaceId"`
	IssueID              string          `json:"issueId"`
	SessionID            string          `json:"sessionId"`
	ProjectID            string          `json:"projectId"`
	Kind                 string          `json:"kind"`
	Status               string          `json:"status"`
	Priority             int             `json:"priority"`
	RuntimeMode          string          `json:"runtimeMode"`
	RequiredCapabilities json.RawMessage `json:"requiredCapabilities"`
	Payload              json.RawMessage `json:"payload"`
	Result               json.RawMessage `json:"result"`
	ClaimedByWorkerID    string          `json:"claimedByWorkerId"`
	ClaimedAt            string          `json:"claimedAt"`
	StartedAt            string          `json:"startedAt"`
	FinishedAt           string          `json:"finishedAt"`
	Error                string          `json:"error"`
	CreatedAt            string          `json:"createdAt"`
	UpdatedAt            string          `json:"updatedAt"`
}

type appendTaskLogInput struct {
	Stream  string `json:"stream"`
	Message string `json:"message"`
}

type updateTaskStatusInput struct {
	Status string          `json:"status"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type runtimeClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type cancellationWatcher struct {
	done chan struct{}
	once sync.Once
}

type apiError struct {
	StatusCode int
	Body       string
}

type agentSessionPayload struct {
	Workdir               string            `json:"workdir"`
	Prompt                string            `json:"prompt"`
	DeveloperInstructions string            `json:"developerInstructions"`
	ApprovalPolicy        string            `json:"approvalPolicy"`
	Sandbox               string            `json:"sandbox"`
	Env                   map[string]string `json:"env"`
	IssueID               string            `json:"issueId"`
	SessionID             string            `json:"sessionId"`
	ProjectID             string            `json:"projectId"`
	Branch                string            `json:"branch"`
	SourceCommitSHA       string            `json:"sourceCommitSha"`
	ContextMarkdown       string            `json:"contextMarkdown"`
	ArtifactDir           string            `json:"artifactDir"`
	Repository            repositorySpec    `json:"repository"`
}

type repositorySpec struct {
	URL           string `json:"url"`
	DefaultBranch string `json:"defaultBranch"`
	SourceType    string `json:"sourceType"`
	Provider      string `json:"provider"`
	Owner         string `json:"owner"`
	Repo          string `json:"repo"`
}

type agentSessionResult struct {
	ThreadID    string             `json:"threadId"`
	TurnID      string             `json:"turnId"`
	Status      string             `json:"status"`
	CompletedAt string             `json:"completedAt"`
	DryRun      bool               `json:"dryRun"`
	Workdir     string             `json:"workdir"`
	ArtifactDir string             `json:"artifactDir"`
	Source      agentSessionSource `json:"source"`
}

type agentSessionSource struct {
	CommitSHA      string            `json:"commitSha"`
	ShortCommitSHA string            `json:"shortCommitSha"`
	Branch         string            `json:"branch"`
	Subject        string            `json:"subject"`
	FilesChanged   int               `json:"filesChanged"`
	Changes        []workspaceChange `json:"changes"`
	DiffPreview    string            `json:"diffPreview"`
	DiffTruncated  bool              `json:"diffTruncated"`
}

type branchNameArtifact struct {
	Branch string `json:"branch"`
	Type   string `json:"type"`
	Slug   string `json:"slug"`
}

type workspaceChange struct {
	StatusCode   string `json:"statusCode"`
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath"`
}

type codexAppServerClient struct {
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	stderr        io.Reader
	encoder       *json.Encoder
	pending       map[int64]chan codexRPCResponse
	notifications chan codexRPCNotification
	waitDone      chan error
	mu            sync.Mutex
	nextID        int64
}

type codexRPCNotification struct {
	Method string
	Params json.RawMessage
}

type codexRPCResponse struct {
	ID     int64
	Result json.RawMessage
	Error  *codexRPCError
	Err    error
}

type codexRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *codexRPCError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == 0 {
		return e.Message
	}
	return fmt.Sprintf("%s (code %d)", e.Message, e.Code)
}

type codexInitializeResponse struct {
	UserAgent string `json:"userAgent"`
	CodexHome string `json:"codexHome"`
}

type codexThreadStartResponse struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
	Model         string `json:"model"`
	ModelProvider string `json:"modelProvider"`
}

type codexTurnStartResponse struct {
	Turn codexTurn `json:"turn"`
}

type codexTurn struct {
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Error  *codexTurnError   `json:"error"`
	Items  []codexThreadItem `json:"items"`
}

type codexTurnError struct {
	Message           string  `json:"message"`
	AdditionalDetails *string `json:"additionalDetails"`
}

func (e *codexTurnError) Error() string {
	if e == nil {
		return ""
	}
	if e.AdditionalDetails != nil && strings.TrimSpace(*e.AdditionalDetails) != "" {
		return strings.TrimSpace(e.Message + "\n" + *e.AdditionalDetails)
	}
	return strings.TrimSpace(e.Message)
}

type codexThreadItem struct {
	Type             string            `json:"type"`
	ID               string            `json:"id"`
	Text             string            `json:"text"`
	Command          string            `json:"command"`
	AggregatedOutput *string           `json:"aggregatedOutput"`
	ExitCode         *int              `json:"exitCode"`
	Changes          []json.RawMessage `json:"changes"`
	Summary          []string          `json:"summary"`
	Server           string            `json:"server"`
	Tool             string            `json:"tool"`
	Namespace        *string           `json:"namespace"`
}

type codexTurnNotification struct {
	ThreadID string    `json:"threadId"`
	Turn     codexTurn `json:"turn"`
}

type codexThreadStatusNotification struct {
	ThreadID string `json:"threadId"`
	Status   struct {
		Type string `json:"type"`
	} `json:"status"`
}

type codexItemNotification struct {
	Item     codexThreadItem `json:"item"`
	ThreadID string          `json:"threadId"`
	TurnID   string          `json:"turnId"`
}

type codexDeltaNotification struct {
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
}

type codexErrorNotification struct {
	Error     codexTurnError `json:"error"`
	WillRetry bool           `json:"willRetry"`
	ThreadID  string         `json:"threadId"`
	TurnID    string         `json:"turnId"`
}

func (e apiError) Error() string {
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return fmt.Sprintf("server returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("server returned HTTP %d: %s", e.StatusCode, body)
}

func main() {
	cfg, err := configFromArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(ctx, cfg, logger); err != nil {
		logger.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func configFromArgs(args []string) (config, error) {
	host, _ := os.Hostname()
	defaultName := strings.TrimSpace(envFirst("MSPACE_WORKER_NAME"))
	if defaultName == "" {
		defaultName = "mspace-worker"
		if host != "" {
			defaultName += "-" + host
		}
	}
	defaultServer := envFirst("MSPACE_SERVER_URL", "MSPACE_CONTROL_PLANE_URL")
	if defaultServer == "" {
		defaultServer = "http://127.0.0.1:8787"
	}
	defaultCapabilities := envFirst("MSPACE_WORKER_CAPABILITIES")
	if defaultCapabilities == "" {
		defaultCapabilities = `{"protocolSmoke":true,"codex":false,"dryRun":true}`
	}
	defaultLabels := envFirst("MSPACE_WORKER_LABELS")
	if defaultLabels == "" {
		defaultLabels = `{}`
	}
	defaultWorkRoot := envFirst("MSPACE_WORKER_WORK_ROOT")
	if defaultWorkRoot == "" {
		defaultWorkRoot = filepath.Join(os.TempDir(), "mspace-worker")
	}

	fs := flag.NewFlagSet("mspace-worker", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg := config{}
	capabilitiesText := defaultCapabilities
	labelsText := defaultLabels
	fs.StringVar(&cfg.ServerURL, "server", defaultServer, "mspace server base URL")
	fs.StringVar(&cfg.Token, "token", envFirst("MSPACE_RUNTIME_TOKEN"), "runtime registration token with msw_ prefix")
	fs.StringVar(&cfg.Name, "name", defaultName, "worker name")
	fs.StringVar(&cfg.Mode, "mode", envDefault("MSPACE_WORKER_MODE", "team"), "worker runtime mode: personal or team")
	fs.StringVar(&cfg.Version, "version", envDefault("MSPACE_WORKER_VERSION", workerVersion), "worker version")
	fs.StringVar(&cfg.WorkRoot, "work-root", defaultWorkRoot, "root directory for worker-managed repos, workdirs, and artifacts")
	fs.StringVar(&capabilitiesText, "capabilities", defaultCapabilities, "worker capability JSON object")
	fs.StringVar(&labelsText, "labels", defaultLabels, "worker label JSON object")
	fs.DurationVar(&cfg.PollInterval, "poll-interval", envDuration("MSPACE_WORKER_POLL_INTERVAL", 5*time.Second), "task poll interval")
	fs.DurationVar(&cfg.HeartbeatInterval, "heartbeat-interval", envDuration("MSPACE_WORKER_HEARTBEAT_INTERVAL", 10*time.Second), "heartbeat interval")
	fs.BoolVar(&cfg.Once, "once", envBool("MSPACE_WORKER_ONCE", false), "claim at most one task then exit")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	capabilities, err := normalizeJSONObject(capabilitiesText, "capabilities")
	if err != nil {
		return config{}, err
	}
	labels, err := normalizeJSONObject(labelsText, "labels")
	if err != nil {
		return config{}, err
	}
	cfg.Capabilities = capabilities
	cfg.Labels = labels
	return normalizeConfig(cfg)
}

func normalizeConfig(cfg config) (config, error) {
	cfg.ServerURL = strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")
	cfg.Token = strings.TrimSpace(cfg.Token)
	cfg.Name = strings.TrimSpace(cfg.Name)
	cfg.Mode = strings.ToLower(strings.TrimSpace(cfg.Mode))
	cfg.Version = strings.TrimSpace(cfg.Version)
	cfg.WorkRoot = strings.TrimSpace(cfg.WorkRoot)
	if cfg.ServerURL == "" {
		return config{}, errors.New("server URL is required")
	}
	if _, err := url.ParseRequestURI(cfg.ServerURL); err != nil {
		return config{}, fmt.Errorf("server URL is invalid: %w", err)
	}
	if !strings.HasPrefix(cfg.Token, "msw_") {
		return config{}, errors.New("runtime token with msw_ prefix is required")
	}
	if cfg.Name == "" {
		return config{}, errors.New("worker name is required")
	}
	if cfg.Mode == "" {
		cfg.Mode = "team"
	}
	if cfg.Mode != "personal" && cfg.Mode != "team" {
		return config{}, errors.New("worker mode must be personal or team")
	}
	if cfg.Version == "" {
		cfg.Version = workerVersion
	}
	if cfg.WorkRoot == "" {
		return config{}, errors.New("worker work root is required")
	}
	if abs, err := filepath.Abs(cfg.WorkRoot); err == nil {
		cfg.WorkRoot = abs
	}
	if cfg.PollInterval <= 0 {
		return config{}, errors.New("poll interval must be positive")
	}
	if cfg.HeartbeatInterval <= 0 {
		return config{}, errors.New("heartbeat interval must be positive")
	}
	return cfg, nil
}

func run(ctx context.Context, cfg config, logger *slog.Logger) error {
	client := &runtimeClient{
		baseURL:    cfg.ServerURL,
		token:      cfg.Token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	worker, err := client.register(ctx, cfg.workerInput("online", 0, true))
	if err != nil {
		return fmt.Errorf("register worker: %w", err)
	}
	logger.Info("registered runtime worker", "workerID", worker.ID, "workspaceID", worker.WorkspaceID, "mode", worker.Mode)
	if cfg.Once {
		return runOneClaim(ctx, client, cfg, logger, worker.ID)
	}
	return runLoop(ctx, client, cfg, logger, worker.ID)
}

func runLoop(ctx context.Context, client *runtimeClient, cfg config, logger *slog.Logger, workerID string) error {
	var currentLoad int64
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(cfg.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				load := int(atomic.LoadInt64(&currentLoad))
				if _, err := client.heartbeat(ctx, workerID, cfg.workerInput("online", load, load == 0)); err != nil {
					logger.Warn("heartbeat failed", "error", err)
				}
			}
		}
	}()

	poll := time.NewTicker(cfg.PollInterval)
	defer poll.Stop()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := client.heartbeat(shutdownCtx, workerID, cfg.workerInput("offline", int(atomic.LoadInt64(&currentLoad)), false)); err != nil {
			logger.Warn("offline heartbeat failed", "error", err)
		}
	}()

	for {
		if err := claimAndHandle(ctx, client, cfg, logger, workerID, &currentLoad); err != nil {
			logger.Warn("claim cycle failed", "error", err)
		}
		select {
		case <-ctx.Done():
			<-heartbeatDone
			return nil
		case <-poll.C:
		}
	}
}

func runOneClaim(ctx context.Context, client *runtimeClient, cfg config, logger *slog.Logger, workerID string) error {
	var currentLoad int64
	if err := claimAndHandle(ctx, client, cfg, logger, workerID, &currentLoad); err != nil {
		return err
	}
	_, err := client.heartbeat(ctx, workerID, cfg.workerInput("offline", 0, false))
	return err
}

func claimAndHandle(ctx context.Context, client *runtimeClient, cfg config, logger *slog.Logger, workerID string, currentLoad *int64) error {
	task, err := client.claim(ctx, workerID)
	if err != nil {
		return fmt.Errorf("claim task: %w", err)
	}
	if task == nil {
		logger.Debug("no runtime task available")
		return nil
	}
	logger.Info("claimed runtime task", "taskID", task.ID, "kind", task.Kind, "priority", task.Priority)
	atomic.StoreInt64(currentLoad, 1)
	_, _ = client.heartbeat(ctx, workerID, cfg.workerInput("online", 1, false))
	defer func() {
		atomic.StoreInt64(currentLoad, 0)
		_, _ = client.heartbeat(context.WithoutCancel(ctx), workerID, cfg.workerInput("online", 0, true))
	}()
	return handleTask(ctx, client, cfg, logger, workerID, *task)
}

func handleTask(ctx context.Context, client *runtimeClient, cfg config, logger *slog.Logger, workerID string, task runtimeTask) error {
	if _, err := client.updateTaskStatus(ctx, workerID, task.ID, updateTaskStatusInput{Status: "running"}); err != nil {
		return fmt.Errorf("mark task running: %w", err)
	}

	var result json.RawMessage
	var taskErr error
	switch strings.ToLower(strings.TrimSpace(task.Kind)) {
	case "agent_session":
		result, taskErr = executeAgentSessionTask(ctx, client, cfg, workerID, task)
	default:
		result, taskErr = executeProtocolTask(cfg, task)
	}
	update := updateTaskStatusInput{Result: result}
	if taskErr != nil {
		update.Status = "failed"
		update.Error = taskErr.Error()
		if errors.Is(taskErr, context.Canceled) {
			update.Status = "cancelled"
			update.Error = "Task cancellation requested by control plane."
		}
	} else {
		update.Status = "completed"
	}
	if _, err := client.updateTaskStatus(ctx, workerID, task.ID, update); err != nil {
		if errors.Is(taskErr, context.Canceled) {
			logger.Info("runtime task cancelled", "taskID", task.ID)
			return nil
		}
		return fmt.Errorf("mark task %s: %w", update.Status, err)
	}
	if errors.Is(taskErr, context.Canceled) {
		logger.Info("runtime task cancelled", "taskID", task.ID)
		return nil
	}
	if taskErr != nil {
		logger.Info("runtime task failed by design", "taskID", task.ID, "error", taskErr)
		return nil
	}
	logger.Info("runtime task completed", "taskID", task.ID)
	return nil
}

func executeProtocolTask(cfg config, task runtimeTask) (json.RawMessage, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	result := map[string]any{
		"workerName":    cfg.Name,
		"workerMode":    cfg.Mode,
		"workerVersion": cfg.Version,
		"taskID":        task.ID,
		"kind":          task.Kind,
		"completedAt":   now,
		"dryRun":        true,
	}
	switch strings.ToLower(strings.TrimSpace(task.Kind)) {
	case "noop", "protocol_smoke":
		result["message"] = "mspace worker protocol task completed."
		body, err := json.Marshal(result)
		return body, err
	default:
		result["message"] = "This worker does not implement the requested task kind."
		body, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		return body, fmt.Errorf("task kind %q is not implemented by this worker", task.Kind)
	}
}

func executeAgentSessionTask(ctx context.Context, client *runtimeClient, cfg config, workerID string, task runtimeTask) (json.RawMessage, error) {
	payload, err := parseAgentSessionPayload(task.Payload)
	if err != nil {
		return nil, err
	}
	if capabilityEnabled(cfg.Capabilities, "dryRun") {
		if err := client.appendTaskLog(ctx, workerID, task.ID, appendTaskLogInput{Stream: "system", Message: "Running dry-run agent session in worker-managed workspace."}); err != nil {
			return nil, err
		}
		result, err := runDryRunAgentSession(ctx, client, cfg, workerID, task.ID, payload)
		if err != nil {
			_ = client.appendTaskLog(context.WithoutCancel(ctx), workerID, task.ID, appendTaskLogInput{Stream: "dry-run-error", Message: err.Error()})
			return nil, err
		}
		body, err := json.Marshal(result)
		return body, err
	}
	runCtx, stopCancelWatcher := client.watchTaskCancellation(ctx, workerID, task.ID, cfg.PollInterval)
	defer stopCancelWatcher()
	if err := client.appendTaskLog(ctx, workerID, task.ID, appendTaskLogInput{Stream: "system", Message: "Starting Codex app-server with stdio transport."}); err != nil {
		return nil, err
	}
	result, err := runCodexAgentSession(runCtx, client, cfg, workerID, task.ID, payload)
	if err != nil {
		_ = client.appendTaskLog(context.WithoutCancel(ctx), workerID, task.ID, appendTaskLogInput{Stream: "codex-error", Message: err.Error()})
		return nil, err
	}
	body, err := json.Marshal(result)
	return body, err
}

func runDryRunAgentSession(ctx context.Context, runtimeClient *runtimeClient, cfg config, workerID, taskID string, payload agentSessionPayload) (agentSessionResult, error) {
	prepared, err := prepareAgentSessionWorkspace(ctx, runtimeClient, cfg, workerID, taskID, payload)
	if err != nil {
		return agentSessionResult{}, err
	}
	payload = prepared
	if err := writeDryRunAgentSessionFiles(payload, taskID); err != nil {
		return agentSessionResult{}, err
	}
	source, err := captureAgentSessionSource(ctx, runtimeClient, workerID, taskID, payload)
	if err != nil {
		return agentSessionResult{}, err
	}
	return agentSessionResult{
		ThreadID:    "dry-run-thread-" + shortTaskID(taskID),
		TurnID:      "dry-run-turn-" + shortTaskID(taskID),
		Status:      "completed",
		CompletedAt: time.Now().UTC().Format(time.RFC3339),
		DryRun:      true,
		Workdir:     payload.Workdir,
		ArtifactDir: payload.ArtifactDir,
		Source:      source,
	}, nil
}

func writeDryRunAgentSessionFiles(payload agentSessionPayload, taskID string) error {
	if payload.Workdir == "" {
		return errors.New("dry-run agent session requires a prepared workdir")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	summary := strings.TrimSpace(payload.Prompt)
	if summary == "" {
		summary = "No prompt provided."
	}
	summary = truncate(summary, 800)
	sourceContent := strings.Join([]string{
		"# mspace team runtime dry run",
		"",
		"This file was written by the mspace Server Worker dry-run executor.",
		"It proves the worker prepared an isolated repository worktree, changed source, committed it, and returned commit metadata to the control plane.",
		"",
		"- Task: " + firstNonEmpty(taskID, "unknown"),
		"- Issue: " + firstNonEmpty(payload.IssueID, "unknown"),
		"- Session: " + firstNonEmpty(payload.SessionID, "unknown"),
		"- Project: " + firstNonEmpty(payload.ProjectID, "unknown"),
		"- Branch: " + firstNonEmpty(payload.Branch, "unknown"),
		"- Completed at: " + now,
		"",
		"Prompt excerpt:",
		"",
		summary,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(payload.Workdir, "TEAM_RUNTIME_DRY_RUN.md"), []byte(sourceContent), 0o644); err != nil {
		return fmt.Errorf("write dry-run source file: %w", err)
	}
	if payload.ArtifactDir == "" {
		return nil
	}
	if err := os.MkdirAll(payload.ArtifactDir, 0o755); err != nil {
		return fmt.Errorf("create dry-run artifact dir: %w", err)
	}
	artifact := map[string]any{
		"dryRun":      true,
		"taskId":      taskID,
		"issueId":     payload.IssueID,
		"sessionId":   payload.SessionID,
		"projectId":   payload.ProjectID,
		"workdir":     payload.Workdir,
		"completedAt": now,
		"message":     "Server Worker dry-run agent session completed in an isolated worker worktree.",
	}
	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal dry-run artifact: %w", err)
	}
	if err := os.WriteFile(filepath.Join(payload.ArtifactDir, "team-runtime-dry-run.json"), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write dry-run artifact: %w", err)
	}
	return nil
}

func parseAgentSessionPayload(raw json.RawMessage) (agentSessionPayload, error) {
	var payload agentSessionPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return agentSessionPayload{}, fmt.Errorf("agent_session payload must be a JSON object: %w", err)
	}
	payload.Workdir = strings.TrimSpace(payload.Workdir)
	payload.Prompt = strings.TrimSpace(payload.Prompt)
	payload.DeveloperInstructions = strings.TrimSpace(payload.DeveloperInstructions)
	payload.ApprovalPolicy = strings.TrimSpace(payload.ApprovalPolicy)
	payload.Sandbox = strings.TrimSpace(payload.Sandbox)
	payload.IssueID = strings.TrimSpace(payload.IssueID)
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	payload.ProjectID = strings.TrimSpace(payload.ProjectID)
	payload.Branch = strings.TrimSpace(payload.Branch)
	payload.SourceCommitSHA = strings.TrimSpace(payload.SourceCommitSHA)
	payload.ArtifactDir = strings.TrimSpace(payload.ArtifactDir)
	payload.Repository.URL = strings.TrimSpace(payload.Repository.URL)
	payload.Repository.DefaultBranch = strings.TrimSpace(payload.Repository.DefaultBranch)
	payload.Repository.SourceType = strings.TrimSpace(payload.Repository.SourceType)
	payload.Repository.Provider = strings.TrimSpace(payload.Repository.Provider)
	payload.Repository.Owner = strings.TrimSpace(payload.Repository.Owner)
	payload.Repository.Repo = strings.TrimSpace(payload.Repository.Repo)
	if payload.Prompt == "" {
		return agentSessionPayload{}, errors.New("agent_session payload requires prompt")
	}
	if payload.Workdir != "" {
		info, err := os.Stat(payload.Workdir)
		if err != nil {
			return agentSessionPayload{}, fmt.Errorf("stat workdir: %w", err)
		}
		if !info.IsDir() {
			return agentSessionPayload{}, errors.New("agent_session workdir must be a directory")
		}
	} else if payload.Repository.URL == "" {
		return agentSessionPayload{}, errors.New("agent_session payload requires repository.url or workdir")
	}
	if payload.ApprovalPolicy == "" {
		payload.ApprovalPolicy = "never"
	}
	if payload.Sandbox == "" {
		payload.Sandbox = "danger-full-access"
	}
	if payload.DeveloperInstructions == "" {
		payload.DeveloperInstructions = defaultAgentSessionDeveloperInstructions()
	}
	return payload, nil
}

func runCodexAgentSession(ctx context.Context, runtimeClient *runtimeClient, cfg config, workerID, taskID string, payload agentSessionPayload) (agentSessionResult, error) {
	prepared, err := prepareAgentSessionWorkspace(ctx, runtimeClient, cfg, workerID, taskID, payload)
	if err != nil {
		return agentSessionResult{}, err
	}
	payload = prepared
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return agentSessionResult{}, errors.New("codex CLI is not available on PATH")
	}
	appClient, err := startCodexAppServer(codexPath, payload)
	if err != nil {
		return agentSessionResult{}, err
	}
	defer appClient.close()

	go captureCodexDiagnosticStream(ctx, runtimeClient, workerID, taskID, appClient.stderr)

	var initResp codexInitializeResponse
	if err := appClient.request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "mspace-worker",
			"title":   "mspace worker",
			"version": cfg.Version,
		},
		"capabilities": map[string]any{
			"experimentalApi": true,
		},
	}, &initResp); err != nil {
		return agentSessionResult{}, fmt.Errorf("initialize codex app-server: %w", err)
	}
	_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Codex app-server ready: " + initResp.UserAgent})

	var threadResp codexThreadStartResponse
	if err := appClient.request(ctx, "thread/start", map[string]any{
		"cwd":                    payload.Workdir,
		"approvalPolicy":         payload.ApprovalPolicy,
		"approvalsReviewer":      "user",
		"sandbox":                payload.Sandbox,
		"developerInstructions":  payload.DeveloperInstructions,
		"personality":            "pragmatic",
		"ephemeral":              false,
		"sessionStartSource":     "startup",
		"serviceName":            "mspace-worker",
		"experimentalRawEvents":  false,
		"persistExtendedHistory": true,
	}, &threadResp); err != nil {
		return agentSessionResult{}, fmt.Errorf("start codex thread: %w", err)
	}
	if strings.TrimSpace(threadResp.Thread.ID) == "" {
		return agentSessionResult{}, errors.New("codex app-server returned an empty thread id")
	}
	_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Codex thread: " + threadResp.Thread.ID})

	var turnResp codexTurnStartResponse
	if err := appClient.request(ctx, "turn/start", map[string]any{
		"threadId": threadResp.Thread.ID,
		"input": []map[string]any{
			{
				"type":          "text",
				"text":          payload.Prompt,
				"text_elements": []map[string]any{},
			},
		},
		"cwd":            payload.Workdir,
		"approvalPolicy": payload.ApprovalPolicy,
		"sandboxPolicy": map[string]any{
			"type": sandboxPolicyType(payload.Sandbox),
		},
		"responsesapiClientMetadata": map[string]string{
			"mspace.runtime_task_id": taskID,
			"mspace.worker_name":     cfg.Name,
		},
	}, &turnResp); err != nil {
		return agentSessionResult{}, fmt.Errorf("start codex turn: %w", err)
	}
	if strings.TrimSpace(turnResp.Turn.ID) == "" {
		return agentSessionResult{}, errors.New("codex app-server returned an empty turn id")
	}
	_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Codex turn: " + turnResp.Turn.ID})

	if err := waitCodexTurn(ctx, runtimeClient, appClient, workerID, taskID, threadResp.Thread.ID, turnResp.Turn.ID); err != nil {
		return agentSessionResult{}, err
	}
	result := agentSessionResult{
		ThreadID:    threadResp.Thread.ID,
		TurnID:      turnResp.Turn.ID,
		Status:      "completed",
		CompletedAt: time.Now().UTC().Format(time.RFC3339),
		DryRun:      false,
		Workdir:     payload.Workdir,
		ArtifactDir: payload.ArtifactDir,
	}
	source, err := captureAgentSessionSource(ctx, runtimeClient, workerID, taskID, payload)
	if err != nil {
		return agentSessionResult{}, err
	}
	result.Source = source
	return result, nil
}

func startCodexAppServer(codexPath string, payload agentSessionPayload) (*codexAppServerClient, error) {
	cmd := exec.Command(codexPath, "app-server", "--listen", "stdio://")
	cmd.Dir = payload.Workdir
	cmd.Env = append(os.Environ(), payloadEnv(payload.Env)...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}

	client := &codexAppServerClient{
		cmd:           cmd,
		stdin:         stdin,
		stderr:        stderr,
		encoder:       json.NewEncoder(stdin),
		pending:       map[int64]chan codexRPCResponse{},
		notifications: make(chan codexRPCNotification, 128),
		waitDone:      make(chan error, 1),
	}
	go client.readLoop(stdout)
	go func() {
		client.waitDone <- cmd.Wait()
	}()
	return client, nil
}

func (c *codexAppServerClient) request(ctx context.Context, method string, params any, result any) error {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	responseCh := make(chan codexRPCResponse, 1)
	c.pending[id] = responseCh
	request := map[string]any{
		"id":     id,
		"method": method,
	}
	if params != nil {
		request["params"] = params
	}
	err := c.encoder.Encode(request)
	c.mu.Unlock()
	if err != nil {
		c.removePending(id)
		return err
	}

	select {
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	case response := <-responseCh:
		if response.Err != nil {
			return response.Err
		}
		if response.Error != nil {
			return response.Error
		}
		if result != nil && len(response.Result) > 0 {
			if err := json.Unmarshal(response.Result, result); err != nil {
				return err
			}
		}
		return nil
	}
}

func (c *codexAppServerClient) readLoop(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), codexProtocolLineLimit)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			continue
		}
		if rawMethod, ok := envelope["method"]; ok {
			var method string
			if err := json.Unmarshal(rawMethod, &method); err != nil {
				continue
			}
			c.notifications <- codexRPCNotification{
				Method: method,
				Params: envelope["params"],
			}
			continue
		}
		rawID, ok := envelope["id"]
		if !ok {
			continue
		}
		var id int64
		if err := json.Unmarshal(rawID, &id); err != nil {
			continue
		}
		response := codexRPCResponse{ID: id}
		if rawResult, ok := envelope["result"]; ok {
			response.Result = rawResult
		}
		if rawError, ok := envelope["error"]; ok {
			var rpcErr codexRPCError
			if err := json.Unmarshal(rawError, &rpcErr); err == nil {
				response.Error = &rpcErr
			} else {
				response.Err = err
			}
		}
		c.resolveResponse(response)
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	c.failPending(err)
	close(c.notifications)
}

func (c *codexAppServerClient) resolveResponse(response codexRPCResponse) {
	c.mu.Lock()
	responseCh := c.pending[response.ID]
	delete(c.pending, response.ID)
	c.mu.Unlock()
	if responseCh != nil {
		responseCh <- response
	}
}

func (c *codexAppServerClient) removePending(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *codexAppServerClient) failPending(err error) {
	c.mu.Lock()
	pending := c.pending
	c.pending = map[int64]chan codexRPCResponse{}
	c.mu.Unlock()
	for id, responseCh := range pending {
		responseCh <- codexRPCResponse{ID: id, Err: err}
	}
}

func (c *codexAppServerClient) close() {
	if c == nil || c.cmd == nil {
		return
	}
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	select {
	case <-c.waitDone:
		return
	case <-time.After(500 * time.Millisecond):
	}
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Signal(os.Interrupt)
	}
	select {
	case <-c.waitDone:
		return
	case <-time.After(2 * time.Second):
	}
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	<-c.waitDone
}

func waitCodexTurn(ctx context.Context, runtimeClient *runtimeClient, appClient *codexAppServerClient, workerID, taskID, threadID, turnID string) error {
	outputBuffers := map[string]*strings.Builder{}
	for {
		select {
		case <-ctx.Done():
			interruptCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = appClient.request(interruptCtx, "turn/interrupt", map[string]any{
				"threadId": threadID,
				"turnId":   turnID,
			}, nil)
			cancel()
			return context.Canceled
		case notification, ok := <-appClient.notifications:
			if !ok {
				return errors.New("codex app-server exited before the turn completed")
			}
			done, err := handleCodexNotification(ctx, runtimeClient, workerID, taskID, threadID, turnID, notification, outputBuffers)
			if done {
				return err
			}
		}
	}
}

func handleCodexNotification(ctx context.Context, runtimeClient *runtimeClient, workerID, taskID, threadID, turnID string, notification codexRPCNotification, outputBuffers map[string]*strings.Builder) (bool, error) {
	switch notification.Method {
	case "thread/status/changed":
		var payload codexThreadStatusNotification
		if decodeCodexParams(notification.Params, &payload) == nil && payload.ThreadID == threadID {
			_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "codex-status", Message: "thread-" + payload.Status.Type})
		}
	case "turn/started":
		var payload codexTurnNotification
		if decodeCodexParams(notification.Params, &payload) == nil && payload.ThreadID == threadID {
			_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "codex-status", Message: "turn-" + payload.Turn.Status})
		}
	case "item/started":
		var payload codexItemNotification
		if decodeCodexParams(notification.Params, &payload) == nil && sameCodexTurn(threadID, turnID, payload.ThreadID, payload.TurnID) {
			logCodexItemStarted(ctx, runtimeClient, workerID, taskID, payload.Item)
		}
	case "item/commandExecution/outputDelta":
		var payload codexDeltaNotification
		if decodeCodexParams(notification.Params, &payload) == nil && sameCodexTurn(threadID, turnID, payload.ThreadID, payload.TurnID) {
			builder := outputBuffers[payload.ItemID]
			if builder == nil {
				builder = &strings.Builder{}
				outputBuffers[payload.ItemID] = builder
			}
			builder.WriteString(payload.Delta)
		}
	case "item/completed":
		var payload codexItemNotification
		if decodeCodexParams(notification.Params, &payload) == nil && sameCodexTurn(threadID, turnID, payload.ThreadID, payload.TurnID) {
			logCodexItemCompleted(ctx, runtimeClient, workerID, taskID, payload.Item, outputBuffers[payload.Item.ID])
			delete(outputBuffers, payload.Item.ID)
		}
	case "error":
		var payload codexErrorNotification
		if decodeCodexParams(notification.Params, &payload) == nil && sameCodexTurn(threadID, turnID, payload.ThreadID, payload.TurnID) {
			message := payload.Error.Error()
			if message == "" {
				message = "Codex app-server reported an unknown error."
			}
			_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "codex-error", Message: message})
			if !payload.WillRetry {
				return true, errors.New(message)
			}
		}
	case "turn/completed":
		var payload codexTurnNotification
		if decodeCodexParams(notification.Params, &payload) == nil && sameCodexTurn(threadID, turnID, payload.ThreadID, payload.Turn.ID) {
			_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "codex-status", Message: "turn-" + payload.Turn.Status})
			switch payload.Turn.Status {
			case "completed":
				return true, nil
			case "interrupted":
				return true, context.Canceled
			case "failed":
				if payload.Turn.Error != nil && payload.Turn.Error.Error() != "" {
					return true, errors.New(payload.Turn.Error.Error())
				}
				return true, errors.New("codex turn failed")
			default:
				return true, fmt.Errorf("codex turn ended with status %s", payload.Turn.Status)
			}
		}
	}
	return false, nil
}

func logCodexItemStarted(ctx context.Context, runtimeClient *runtimeClient, workerID, taskID string, item codexThreadItem) {
	switch item.Type {
	case "commandExecution":
		if strings.TrimSpace(item.Command) != "" {
			_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "command", Message: "$ " + strings.TrimSpace(item.Command)})
		}
	case "agentMessage", "reasoning", "fileChange":
		_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "codex-status", Message: item.Type})
	}
}

func logCodexItemCompleted(ctx context.Context, runtimeClient *runtimeClient, workerID, taskID string, item codexThreadItem, bufferedOutput *strings.Builder) {
	switch item.Type {
	case "agentMessage":
		if strings.TrimSpace(item.Text) != "" {
			_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "agent", Message: item.Text})
		}
	case "plan":
		if strings.TrimSpace(item.Text) != "" {
			_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "plan", Message: item.Text})
		}
	case "reasoning":
		summary := strings.TrimSpace(strings.Join(item.Summary, "\n"))
		if summary != "" {
			_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "reasoning", Message: summary})
		}
	case "commandExecution":
		output := ""
		if item.AggregatedOutput != nil {
			output = *item.AggregatedOutput
		} else if bufferedOutput != nil {
			output = bufferedOutput.String()
		}
		status := "completed"
		if item.ExitCode != nil {
			status = fmt.Sprintf("exit %d", *item.ExitCode)
		}
		message := "Command " + status + "."
		if strings.TrimSpace(output) != "" {
			message = truncate(fmt.Sprintf("Command %s:\n%s", status, output), 12000)
		}
		_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "command", Message: message})
	case "fileChange":
		if len(item.Changes) > 0 {
			_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "file", Message: fmt.Sprintf("Applied %d file change(s).", len(item.Changes))})
		}
	case "mcpToolCall":
		tool := strings.TrimSpace(item.Server + "." + item.Tool)
		_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "tool", Message: strings.TrimSpace("MCP tool completed: " + tool)})
	case "dynamicToolCall":
		namespace := ""
		if item.Namespace != nil {
			namespace = *item.Namespace
		}
		tool := strings.TrimSpace(namespace + "." + item.Tool)
		_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "tool", Message: strings.TrimSpace("Tool completed: " + tool)})
	}
}

func captureCodexDiagnosticStream(ctx context.Context, runtimeClient *runtimeClient, workerID, taskID string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 16*1024), 1024*1024)
	for scanner.Scan() {
		_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "codex-stderr", Message: truncate(scanner.Text(), 4000)})
	}
}

func prepareAgentSessionWorkspace(ctx context.Context, runtimeClient *runtimeClient, cfg config, workerID, taskID string, payload agentSessionPayload) (agentSessionPayload, error) {
	if payload.Workdir != "" {
		if payload.ArtifactDir == "" {
			payload.ArtifactDir = filepath.Join(payload.Workdir, ".mspace", "session")
		}
		if err := os.MkdirAll(payload.ArtifactDir, 0o755); err != nil {
			return payload, fmt.Errorf("create artifact dir: %w", err)
		}
		payload.Env = withPayloadRuntimeEnv(payload)
		return payload, nil
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return payload, errors.New("git is not available on PATH")
	}
	if err := os.MkdirAll(cfg.WorkRoot, 0o755); err != nil {
		return payload, fmt.Errorf("create worker work root: %w", err)
	}
	repoDir := filepath.Join(cfg.WorkRoot, "repos", repositoryCacheKey(payload.Repository))
	if err := ensureWorkerRepository(ctx, gitPath, payload.Repository.URL, repoDir); err != nil {
		return payload, err
	}
	workdir := filepath.Join(cfg.WorkRoot, "workdirs", safePathPart(firstNonEmpty(payload.ProjectID, "project")), safePathPart(firstNonEmpty(payload.SessionID, taskID)))
	if err := os.MkdirAll(filepath.Dir(workdir), 0o755); err != nil {
		return payload, fmt.Errorf("create worker workdir parent: %w", err)
	}
	if _, err := os.Stat(workdir); err == nil {
		return payload, fmt.Errorf("worker session workdir already exists: %s", workdir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return payload, fmt.Errorf("inspect worker session workdir: %w", err)
	}
	baseRef := strings.TrimSpace(payload.SourceCommitSHA)
	if baseRef == "" {
		resolved, err := resolveWorkerBaseRef(ctx, gitPath, repoDir, payload.Repository.DefaultBranch)
		if err != nil {
			return payload, err
		}
		baseRef = resolved
	}
	if err := runGitCommand(ctx, gitPath, repoDir, "worktree", "add", "--detach", workdir, baseRef); err != nil {
		return payload, fmt.Errorf("create worker worktree from %q: %w", baseRef, err)
	}
	if branch := strings.TrimSpace(payload.Branch); branch != "" && payload.SourceCommitSHA == "" {
		if err := runGitCommand(ctx, gitPath, workdir, "checkout", "-B", branch); err != nil {
			_ = runGitCommand(context.Background(), gitPath, repoDir, "worktree", "remove", "--force", workdir)
			return payload, fmt.Errorf("create worker branch %q: %w", branch, err)
		}
	}
	payload.Workdir = workdir
	payload.ArtifactDir = filepath.Join(workdir, ".mspace", "session")
	if err := os.MkdirAll(payload.ArtifactDir, 0o755); err != nil {
		return payload, fmt.Errorf("create artifact dir: %w", err)
	}
	contextPath := filepath.Join(payload.ArtifactDir, "context.md")
	if strings.TrimSpace(payload.ContextMarkdown) != "" {
		if err := os.WriteFile(contextPath, []byte(payload.ContextMarkdown), 0o600); err != nil {
			return payload, fmt.Errorf("write session context: %w", err)
		}
	}
	payload.Env = withPayloadRuntimeEnv(payload)
	_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Prepared worker workspace: " + payload.Workdir})
	return payload, nil
}

func ensureWorkerRepository(ctx context.Context, gitPath, repoURL, repoDir string) error {
	if info, err := os.Stat(repoDir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("repo cache path exists and is not a directory: %s", repoDir)
		}
		if err := runGitCommand(ctx, gitPath, repoDir, "rev-parse", "--is-inside-work-tree"); err != nil {
			return fmt.Errorf("repo cache is not a git work tree: %w", err)
		}
		if err := runGitCommand(ctx, gitPath, repoDir, "fetch", "--all", "--prune"); err != nil {
			return fmt.Errorf("refresh repo cache: %w", err)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect repo cache: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(repoDir), 0o755); err != nil {
		return fmt.Errorf("create repo cache parent: %w", err)
	}
	if err := runGitCommand(ctx, gitPath, "", "clone", repoURL, repoDir); err != nil {
		return fmt.Errorf("clone worker repo: %w", err)
	}
	return nil
}

func resolveWorkerBaseRef(ctx context.Context, gitPath, repoDir, defaultBranch string) (string, error) {
	defaultBranch = strings.TrimSpace(defaultBranch)
	candidates := []string{}
	if defaultBranch != "" {
		candidates = append(candidates, "refs/remotes/origin/"+defaultBranch, "refs/heads/"+defaultBranch, defaultBranch)
	}
	candidates = append(candidates, "refs/remotes/origin/HEAD", "HEAD")
	for _, candidate := range candidates {
		if err := runGitCommand(ctx, gitPath, repoDir, "rev-parse", "--verify", "--quiet", candidate); err == nil {
			out, err := runGitOutput(ctx, gitPath, repoDir, "rev-parse", "--verify", candidate)
			if err == nil && strings.TrimSpace(out) != "" {
				return strings.TrimSpace(out), nil
			}
		}
	}
	return "", errors.New("unable to resolve worker repository base ref")
}

func captureAgentSessionSource(ctx context.Context, runtimeClient *runtimeClient, workerID, taskID string, payload agentSessionPayload) (agentSessionSource, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return agentSessionSource{}, errors.New("git is not available on PATH")
	}
	if err := runGitCommand(ctx, gitPath, payload.Workdir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return agentSessionSource{}, err
	}
	payload = applySemanticWorkerBranchName(ctx, runtimeClient, workerID, taskID, gitPath, payload)
	if err := runGitCommand(ctx, gitPath, payload.Workdir, "add", "-A", "--", "."); err != nil {
		return agentSessionSource{}, fmt.Errorf("stage source changes: %w", err)
	}
	_ = runGitCommand(ctx, gitPath, payload.Workdir, "reset", "-q", "--", ".mspace")
	if err := runGitCommand(ctx, gitPath, payload.Workdir, "diff", "--cached", "--quiet"); err == nil {
		source, err := existingWorkerHeadSource(ctx, gitPath, payload)
		if err != nil || source.CommitSHA != "" {
			return source, err
		}
		_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Team runtime completed with no source changes."})
		return agentSessionSource{}, nil
	}
	subject := sourceCommitSubject(firstNonEmpty(payload.IssueID, payload.SessionID))
	if err := runGitCommandWithEnv(ctx, gitPath, payload.Workdir, []string{
		"GIT_AUTHOR_NAME=mspace",
		"GIT_AUTHOR_EMAIL=mspace@example.local",
		"GIT_COMMITTER_NAME=mspace",
		"GIT_COMMITTER_EMAIL=mspace@example.local",
	}, "commit", "-m", subject); err != nil {
		return agentSessionSource{}, fmt.Errorf("commit source changes: %w", err)
	}
	head, err := runGitOutput(ctx, gitPath, payload.Workdir, "rev-parse", "HEAD")
	if err != nil {
		return agentSessionSource{}, err
	}
	source, err := workerSourceForCommit(ctx, gitPath, payload.Workdir, strings.TrimSpace(head), payload.Branch)
	if err != nil {
		return agentSessionSource{}, err
	}
	_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: fmt.Sprintf("Captured worker source commit %s with %d changed files.", source.ShortCommitSHA, source.FilesChanged)})
	return source, nil
}

func applySemanticWorkerBranchName(ctx context.Context, runtimeClient *runtimeClient, workerID, taskID, gitPath string, payload agentSessionPayload) agentSessionPayload {
	currentBranch := strings.TrimSpace(payload.Branch)
	targetBranch, ok, err := readSemanticWorkerBranchName(payload)
	if err != nil {
		_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Semantic branch name ignored: " + err.Error()})
		return payload
	}
	if !ok || targetBranch == "" || targetBranch == currentBranch {
		return payload
	}
	if err := validateWorkerBranchName(ctx, gitPath, payload.Workdir, targetBranch); err != nil {
		_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Semantic branch name ignored: " + err.Error()})
		return payload
	}
	if exists, err := workerBranchExists(ctx, gitPath, payload.Workdir, targetBranch); err == nil && exists {
		targetBranch = addBranchSessionSuffix(targetBranch, payload.SessionID)
		if err := validateWorkerBranchName(ctx, gitPath, payload.Workdir, targetBranch); err != nil {
			_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Semantic branch fallback ignored: " + err.Error()})
			return payload
		}
	} else if err != nil {
		_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Semantic branch name ignored: " + err.Error()})
		return payload
	}
	if currentBranch == "" {
		payload.Branch = targetBranch
		return payload
	}
	if err := runGitCommand(ctx, gitPath, payload.Workdir, "branch", "-m", targetBranch); err != nil {
		_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Semantic branch rename failed: " + err.Error()})
		return payload
	}
	payload.Branch = targetBranch
	_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: fmt.Sprintf("Renamed source branch from %s to %s.", currentBranch, targetBranch)})
	return payload
}

func readSemanticWorkerBranchName(payload agentSessionPayload) (string, bool, error) {
	artifactPath := branchNameArtifactPath(payload)
	if artifactPath == "" {
		return "", false, nil
	}
	data, err := os.ReadFile(artifactPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var artifact branchNameArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return "", false, fmt.Errorf("parse %s: %w", branchNameArtifactName, err)
	}
	branch, err := normalizeSemanticBranchArtifact(artifact)
	if err != nil {
		return "", true, err
	}
	return branch, true, nil
}

func branchNameArtifactPath(payload agentSessionPayload) string {
	if strings.TrimSpace(payload.ArtifactDir) != "" {
		return filepath.Join(payload.ArtifactDir, branchNameArtifactName)
	}
	if strings.TrimSpace(payload.Workdir) != "" {
		return filepath.Join(payload.Workdir, ".mspace", "session", branchNameArtifactName)
	}
	return ""
}

func normalizeSemanticBranchArtifact(artifact branchNameArtifact) (string, error) {
	branch := strings.ToLower(strings.TrimSpace(artifact.Branch))
	if branch == "" {
		branchType := strings.ToLower(strings.TrimSpace(artifact.Type))
		slug := strings.ToLower(strings.TrimSpace(artifact.Slug))
		if branchType != "" && slug != "" {
			branch = branchType + "/" + slug
		}
	}
	if branch == "" {
		return "", errors.New("branch name artifact did not include branch or type/slug")
	}
	parts := strings.SplitN(branch, "/", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("branch %q must use <type>/<slug>", branch)
	}
	branchType := strings.TrimSpace(parts[0])
	slug := normalizeBranchSlug(parts[1])
	if !allowedSemanticBranchType(branchType) {
		return "", fmt.Errorf("branch type %q is not supported", branchType)
	}
	if slug == "" {
		return "", fmt.Errorf("branch %q has an empty slug", branch)
	}
	return branchType + "/" + slug, nil
}

func normalizeBranchSlug(value string) string {
	slug := strings.ToLower(strings.TrimSpace(value))
	slug = strings.Trim(slug, "/")
	slug = branchSlugUnsafePattern.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	const maxSlugLength = 48
	if len(slug) > maxSlugLength {
		slug = strings.Trim(slug[:maxSlugLength], "-")
	}
	return slug
}

func allowedSemanticBranchType(value string) bool {
	switch value {
	case "feat", "fix", "chore", "docs", "refactor", "test", "perf", "build", "ci", "style", "revert":
		return true
	default:
		return false
	}
}

func validateWorkerBranchName(ctx context.Context, gitPath, workdir, branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return errors.New("branch is required")
	}
	if strings.HasPrefix(branch, "-") {
		return fmt.Errorf("invalid branch name %q", branch)
	}
	if err := runGitCommand(ctx, gitPath, workdir, "check-ref-format", "--branch", branch); err != nil {
		return fmt.Errorf("invalid branch name %q: %w", branch, err)
	}
	return nil
}

func workerBranchExists(ctx context.Context, gitPath, workdir, branch string) (bool, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return false, nil
	}
	cmd := exec.CommandContext(ctx, gitPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if workdir != "" {
		cmd.Dir = workdir
	}
	output, err := cmd.CombinedOutput()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, errors.New(formatCommandFailure(err, output))
}

func addBranchSessionSuffix(branch, sessionID string) string {
	branchType, slug, ok := strings.Cut(branch, "/")
	if !ok {
		return branch + "-" + shortStableID(sessionID)
	}
	suffix := shortStableID(sessionID)
	maxSlugLength := 48 - len(suffix) - 1
	if maxSlugLength < 12 {
		maxSlugLength = 12
	}
	if len(slug) > maxSlugLength {
		slug = strings.Trim(slug[:maxSlugLength], "-")
	}
	return branchType + "/" + strings.Trim(slug+"-"+suffix, "-")
}

func shortStableID(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "-")
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '-' || r == '_' || r == '/'
	})
	for index := len(parts) - 1; index >= 0; index-- {
		part := strings.TrimSpace(parts[index])
		if len(part) >= 8 {
			return part[:8]
		}
	}
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

func existingWorkerHeadSource(ctx context.Context, gitPath string, payload agentSessionPayload) (agentSessionSource, error) {
	head, err := runGitOutput(ctx, gitPath, payload.Workdir, "rev-parse", "HEAD")
	if err != nil {
		return agentSessionSource{}, err
	}
	head = strings.TrimSpace(head)
	baseRef := strings.TrimSpace(payload.SourceCommitSHA)
	if baseRef == "" {
		resolved, err := resolveWorkerBaseRef(ctx, gitPath, payload.Workdir, payload.Repository.DefaultBranch)
		if err != nil {
			return agentSessionSource{}, nil
		}
		baseRef = resolved
	}
	mergeBase, err := runGitOutput(ctx, gitPath, payload.Workdir, "merge-base", "HEAD", baseRef)
	if err != nil {
		return agentSessionSource{}, nil
	}
	if head == strings.TrimSpace(mergeBase) {
		return agentSessionSource{}, nil
	}
	count, err := runGitOutput(ctx, gitPath, payload.Workdir, "rev-list", "--count", strings.TrimSpace(mergeBase)+"..HEAD")
	if err != nil || strings.TrimSpace(count) == "0" {
		return agentSessionSource{}, nil
	}
	return workerSourceForCommit(ctx, gitPath, payload.Workdir, head, payload.Branch)
}

func workerSourceForCommit(ctx context.Context, gitPath, workdir, commitSHA, branch string) (agentSessionSource, error) {
	subject, err := runGitOutput(ctx, gitPath, workdir, "log", "-1", "--pretty=%s", commitSHA)
	if err != nil {
		return agentSessionSource{}, err
	}
	nameStatusOutput, err := runGitOutput(ctx, gitPath, workdir, "diff-tree", "--root", "--no-commit-id", "--name-status", "-r", "--find-renames", commitSHA)
	if err != nil {
		return agentSessionSource{}, err
	}
	changes := []workspaceChange{}
	for _, line := range splitNonEmptyLines(nameStatusOutput) {
		changes = append(changes, parseNameStatusChange(line))
	}
	diffPreview, err := runGitOutput(ctx, gitPath, workdir, "show", "--stat", "--patch", "--find-renames", "--no-ext-diff", "--format=medium", "--no-color", commitSHA)
	if err != nil {
		return agentSessionSource{}, err
	}
	preview, truncated := truncateWithFlag(diffPreview, 20000)
	return agentSessionSource{
		CommitSHA:      strings.TrimSpace(commitSHA),
		ShortCommitSHA: shortCommitSHA(commitSHA),
		Branch:         strings.TrimSpace(branch),
		Subject:        strings.TrimSpace(subject),
		FilesChanged:   len(changes),
		Changes:        changes,
		DiffPreview:    preview,
		DiffTruncated:  truncated,
	}, nil
}

func withPayloadRuntimeEnv(payload agentSessionPayload) map[string]string {
	env := map[string]string{}
	for key, value := range payload.Env {
		env[key] = value
	}
	if payload.Workdir != "" {
		env["MSPACE_SESSION_WORKDIR"] = payload.Workdir
	}
	if payload.ArtifactDir != "" {
		env["MSPACE_SESSION_ARTIFACT_DIR"] = payload.ArtifactDir
	}
	if strings.TrimSpace(payload.ContextMarkdown) != "" {
		env["MSPACE_SESSION_CONTEXT"] = filepath.Join(payload.ArtifactDir, "context.md")
	}
	return env
}

func runGitCommand(ctx context.Context, gitPath, dir string, args ...string) error {
	return runGitCommandWithEnv(ctx, gitPath, dir, nil, args...)
}

func runGitCommandWithEnv(ctx context.Context, gitPath, dir string, extraEnv []string, args ...string) error {
	cmd := exec.CommandContext(ctx, gitPath, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(formatCommandFailure(err, output))
	}
	return nil
}

func runGitOutput(ctx context.Context, gitPath, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, gitPath, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", errors.New(formatCommandFailure(err, output))
	}
	return strings.TrimSpace(string(output)), nil
}

func sameCodexTurn(expectedThreadID, expectedTurnID, threadID, turnID string) bool {
	if expectedThreadID != "" && threadID != expectedThreadID {
		return false
	}
	return expectedTurnID == "" || turnID == expectedTurnID
}

func decodeCodexParams(params json.RawMessage, target any) error {
	if len(params) == 0 {
		return errors.New("empty params")
	}
	return json.Unmarshal(params, target)
}

func sandboxPolicyType(sandbox string) string {
	switch strings.ToLower(strings.TrimSpace(sandbox)) {
	case "danger-full-access", "dangerfullaccess":
		return "dangerFullAccess"
	default:
		return sandbox
	}
}

func payloadEnv(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	values := make([]string, 0, len(env))
	for key, value := range env {
		key = strings.TrimSpace(key)
		if key == "" || strings.Contains(key, "=") {
			continue
		}
		values = append(values, key+"="+value)
	}
	return values
}

func capabilityEnabled(capabilities json.RawMessage, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	var values map[string]any
	if err := json.Unmarshal(capabilities, &values); err != nil {
		return false
	}
	value, ok := values[name]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func defaultAgentSessionDeveloperInstructions() string {
	return strings.TrimSpace(`
	You are running as a Codex coding agent inside an mspace team runtime worker.

Follow these mspace rules:
- Work in the provided workdir for this task.
- Inspect the repository before changing code.
- Keep changes focused on the task and avoid unrelated refactors.
- Do not commit, push, create a pull request, or delete workdirs unless the task prompt explicitly asks for it.
- Run relevant validation when practical, and report exactly what passed or failed.
- Do not start or keep a development server running unless the user explicitly asks for a preview or a live server.
- For ordinary validation, prefer non-interactive checks such as lint, tests, typecheck, build, or short one-shot HTTP probes.
- If a temporary server is required for validation, stop it before finishing and report it only as an internal validation step.
	- Do not present container-local localhost or 127.0.0.1 URLs as user-accessible preview URLs. Only report a URL when mspace provides an explicit preview/test-environment URL or the user asked for a local preview and the host mapping is known.
	- Answer directly. Do not introduce yourself.
	- If you make source-code changes, write ${MSPACE_SESSION_ARTIFACT_DIR}/branch-name.json before finishing. Use JSON like { "branch": "fix/short-semantic-name" }. The branch must use a Conventional Commit type prefix such as feat/, fix/, chore/, docs/, refactor/, test/, perf/, build/, or ci/, and the slug should summarize the actual diff in lowercase words separated by hyphens.
	- Finish with a concise summary of changes, validation, and remaining risks.
	`)
}

func repositoryCacheKey(repository repositorySpec) string {
	parts := []string{repository.Provider, repository.Owner, repository.Repo}
	joined := strings.Trim(strings.Join(parts, "-"), "-")
	if joined == "" {
		joined = repository.URL
	}
	return safePathPart(joined)
}

func safePathPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	re := regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	value = re.ReplaceAllString(value, "-")
	value = strings.Trim(value, ".-")
	if value == "" {
		return "unknown"
	}
	if len(value) > 80 {
		value = value[:80]
		value = strings.Trim(value, ".-")
	}
	if value == "" {
		return "unknown"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func sourceCommitSubject(identifier string) string {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		identifier = "team runtime session"
	}
	subject := "mspace: " + identifier
	const maxSubjectRunes = 72
	runes := []rune(subject)
	if len(runes) <= maxSubjectRunes {
		return subject
	}
	return string(runes[:maxSubjectRunes-3]) + "..."
}

func shortTaskID(taskID string) string {
	taskID = safePathPart(taskID)
	if len(taskID) <= 12 {
		return taskID
	}
	return taskID[:12]
}

func parseNameStatusChange(line string) workspaceChange {
	fields := strings.Split(strings.TrimSpace(line), "\t")
	if len(fields) == 0 {
		return workspaceChange{}
	}
	change := workspaceChange{StatusCode: fields[0]}
	if len(fields) >= 2 {
		change.Path = fields[len(fields)-1]
	}
	if len(fields) >= 3 {
		change.PreviousPath = fields[1]
	}
	return change
}

func splitNonEmptyLines(value string) []string {
	lines := strings.Split(value, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func shortCommitSHA(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func truncateWithFlag(value string, max int) (string, bool) {
	runes := []rune(value)
	if len(runes) <= max {
		return value, false
	}
	if max <= 1 {
		return string(runes[:max]), true
	}
	return string(runes[:max-1]) + "…", true
}

func formatCommandFailure(err error, output []byte) string {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return err.Error()
	}
	return strings.TrimSpace(err.Error() + ": " + text)
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}

func (cfg config) workerInput(status string, currentLoad int, includeCapabilities bool) runtimeWorkerInput {
	input := runtimeWorkerInput{
		Name:        cfg.Name,
		Mode:        cfg.Mode,
		Status:      status,
		Version:     cfg.Version,
		CurrentLoad: currentLoad,
	}
	if includeCapabilities {
		input.Capabilities = cfg.Capabilities
		input.Labels = cfg.Labels
	}
	return input
}

func (c *runtimeClient) register(ctx context.Context, input runtimeWorkerInput) (runtimeWorker, error) {
	var worker runtimeWorker
	err := c.doJSON(ctx, http.MethodPost, "/api/runtime/workers/register", input, http.StatusCreated, &worker)
	return worker, err
}

func (c *runtimeClient) heartbeat(ctx context.Context, workerID string, input runtimeWorkerInput) (runtimeWorker, error) {
	var worker runtimeWorker
	err := c.doJSON(ctx, http.MethodPost, "/api/runtime/workers/"+url.PathEscape(workerID)+"/heartbeat", input, http.StatusOK, &worker)
	return worker, err
}

func (c *runtimeClient) claim(ctx context.Context, workerID string) (*runtimeTask, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/runtime/workers/"+url.PathEscape(workerID)+"/tasks/claim", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, apiError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	var task runtimeTask
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (c *runtimeClient) loadClaimedTask(ctx context.Context, workerID, taskID string) (runtimeTask, error) {
	var task runtimeTask
	err := c.doJSON(ctx, http.MethodGet, "/api/runtime/workers/"+url.PathEscape(workerID)+"/tasks/"+url.PathEscape(taskID), nil, http.StatusOK, &task)
	return task, err
}

func (c *runtimeClient) updateTaskStatus(ctx context.Context, workerID, taskID string, input updateTaskStatusInput) (runtimeTask, error) {
	var task runtimeTask
	err := c.doJSON(ctx, http.MethodPost, "/api/runtime/workers/"+url.PathEscape(workerID)+"/tasks/"+url.PathEscape(taskID)+"/status", input, http.StatusOK, &task)
	return task, err
}

func (c *runtimeClient) watchTaskCancellation(parent context.Context, workerID, taskID string, pollInterval time.Duration) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	watcher := &cancellationWatcher{done: make(chan struct{})}
	if pollInterval < 250*time.Millisecond || pollInterval > 5*time.Second {
		pollInterval = 2 * time.Second
	}
	go func() {
		defer close(watcher.done)
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkCtx, checkCancel := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
				task, err := c.loadClaimedTask(checkCtx, workerID, taskID)
				checkCancel()
				if err != nil {
					continue
				}
				if task.Status == "cancelled" {
					_ = c.appendTaskLog(context.WithoutCancel(parent), workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Cancellation requested by control plane."})
					cancel()
					return
				}
			}
		}
	}()
	stop := func() {
		watcher.once.Do(func() {
			cancel()
			<-watcher.done
		})
	}
	return ctx, stop
}

func (c *runtimeClient) appendTaskLog(ctx context.Context, workerID, taskID string, input appendTaskLogInput) error {
	return c.doJSON(ctx, http.MethodPost, "/api/runtime/workers/"+url.PathEscape(workerID)+"/tasks/"+url.PathEscape(taskID)+"/logs", input, http.StatusCreated, nil)
}

func (c *runtimeClient) doJSON(ctx context.Context, method, path string, input any, expectedStatus int, output any) error {
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != expectedStatus {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return apiError{StatusCode: resp.StatusCode, Body: string(body)}
	}
	if output == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(output)
}

func normalizeJSONObject(value string, label string) (json.RawMessage, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "{}"
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(value), &object); err != nil {
		return nil, fmt.Errorf("%s must be valid JSON object: %w", label, err)
	}
	body, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}

func envFirst(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func envDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fallback
	}
	return duration
}

func envBool(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}
