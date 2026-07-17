package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const workerVersion = "0.1.0"
const codexProtocolLineLimit = 16 * 1024 * 1024
const branchNameArtifactName = "branch-name.json"
const testEnvironmentArtifactName = "test-environment.json"
const reviewEvidenceArtifactName = "review-evidence.json"
const testCaseProposalsArtifactName = "test-case-proposals.json"
const testSetupResultArtifactName = "test-setup-result.json"
const testResultArtifactName = "test-result.json"
const maxTestResultScreenshotBytes = 2 * 1024 * 1024
const testArtifactCompletionSettleTimeout = 10 * time.Second

type testArtifactReadiness int

const (
	testArtifactMissing testArtifactReadiness = iota
	testArtifactPending
	testArtifactReady
)

var branchSlugUnsafePattern = regexp.MustCompile(`[^a-z0-9]+`)

type config struct {
	ServerURL         string
	Token             string
	TokenFile         string
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
	tokenFile  string
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
	AgentEngine           string            `json:"agentEngine"`
	LegacyProvider        string            `json:"provider"`
	LegacyAgentProfile    string            `json:"agentProfile"`
	Workdir               string            `json:"workdir"`
	Prompt                string            `json:"prompt"`
	DeveloperInstructions string            `json:"developerInstructions"`
	ApprovalPolicy        string            `json:"approvalPolicy"`
	Sandbox               string            `json:"sandbox"`
	Env                   map[string]string `json:"env"`
	IssueID               string            `json:"issueId"`
	SessionID             string            `json:"sessionId"`
	ProjectID             string            `json:"projectId"`
	Automation            string            `json:"automation"`
	SourceCapture         *bool             `json:"sourceCapture,omitempty"`
	TestRunID             string            `json:"testRunId"`
	Branch                string            `json:"branch"`
	SourceCommitSHA       string            `json:"sourceCommitSha"`
	ContextMarkdown       string            `json:"contextMarkdown"`
	ArtifactDir           string            `json:"artifactDir"`
	Repository            repositorySpec    `json:"repository"`
	RequiredSkills        []skillBundle     `json:"requiredSkills"`
	Skills                []skillBundle     `json:"skills"`
}

type skillBundle struct {
	Slug        string            `json:"slug,omitempty"`
	Name        string            `json:"name"`
	Revision    string            `json:"revision"`
	Hash        string            `json:"hash,omitempty"`
	SHA256      string            `json:"sha256,omitempty"`
	ContentHash string            `json:"contentHash,omitempty"`
	Files       []skillBundleFile `json:"files"`
}

type skillBundleFile struct {
	Path          string  `json:"path"`
	Content       *string `json:"content,omitempty"`
	ContentBase64 *string `json:"contentBase64,omitempty"`
	SHA256        string  `json:"sha256,omitempty"`
	Executable    bool    `json:"executable,omitempty"`
}

type issueTypeTriagePayload struct {
	WorkspaceID           string            `json:"workspaceId"`
	IssueID               string            `json:"issueId"`
	ProjectID             string            `json:"projectId"`
	Prompt                string            `json:"prompt"`
	DeveloperInstructions string            `json:"developerInstructions"`
	Env                   map[string]string `json:"env"`
	Project               struct {
		Name     string `json:"name"`
		RepoPath string `json:"repoPath"`
	} `json:"project"`
}

type issueTypeTriageResult struct {
	Title      string  `json:"title,omitempty"`
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
	ThreadID   string  `json:"threadId,omitempty"`
	TurnID     string  `json:"turnId,omitempty"`
}

type importMappingPayload struct {
	WorkspaceID           string                   `json:"workspaceId"`
	ProjectID             string                   `json:"projectId"`
	Format                string                   `json:"format"`
	FileName              string                   `json:"fileName"`
	Headers               []string                 `json:"headers"`
	SampleRows            [][]string               `json:"sampleRows"`
	SystemFields          []map[string]string      `json:"systemFields"`
	Prompt                string                   `json:"prompt"`
	DeveloperInstructions string                   `json:"developerInstructions"`
	Env                   map[string]string        `json:"env"`
	Project               importMappingProjectSpec `json:"project"`
}

type importMappingProjectSpec struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type importMappingResult struct {
	Format      string                    `json:"format"`
	FileName    string                    `json:"fileName"`
	Suggestions []importMappingSuggestion `json:"suggestions"`
	Warnings    []string                  `json:"warnings"`
	ThreadID    string                    `json:"threadId,omitempty"`
	TurnID      string                    `json:"turnId,omitempty"`
}

type importMappingSuggestion struct {
	Source     string  `json:"source"`
	Field      string  `json:"field"`
	Index      int     `json:"index"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

type repositorySpec struct {
	URL           string `json:"url"`
	DefaultBranch string `json:"defaultBranch"`
	SourceType    string `json:"sourceType"`
	Provider      string `json:"provider"`
	Owner         string `json:"owner"`
	Repo          string `json:"repo"`
	OriginalURL   string `json:"originalUrl,omitempty"`
	Subdir        string `json:"subdir,omitempty"`
}

type agentSessionResult struct {
	AgentEngine       string                       `json:"agentEngine,omitempty"`
	EngineSessionRef  string                       `json:"engineSessionRef,omitempty"`
	EngineRunRef      string                       `json:"engineRunRef,omitempty"`
	ThreadID          string                       `json:"threadId,omitempty"`
	TurnID            string                       `json:"turnId,omitempty"`
	Status            string                       `json:"status"`
	CompletedAt       string                       `json:"completedAt"`
	DryRun            bool                         `json:"dryRun"`
	Workdir           string                       `json:"workdir"`
	ArtifactDir       string                       `json:"artifactDir"`
	Source            agentSessionSource           `json:"source"`
	TestEnvironment   *agentSessionTestEnvironment `json:"testEnvironment,omitempty"`
	ReviewEvidence    *reviewEvidenceArtifact      `json:"reviewEvidence,omitempty"`
	TestCaseProposals *testCaseProposalsArtifact   `json:"testCaseProposals,omitempty"`
	TestSetup         *testSetupResultArtifact     `json:"testSetup,omitempty"`
	TestResult        *testResultArtifact          `json:"testResult,omitempty"`
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

type agentSessionTestEnvironment struct {
	PreviewURL      string `json:"previewUrl"`
	PreviewURLSnake string `json:"preview_url,omitempty"`
	URL             string `json:"url,omitempty"`
}

type reviewEvidenceArtifact struct {
	AgentSummary     string                  `json:"agentSummary"`
	CommandsRun      []reviewEvidenceCommand `json:"commandsRun"`
	Tests            []reviewEvidenceCheck   `json:"tests"`
	BuildResult      reviewEvidenceResult    `json:"buildResult"`
	DeploymentResult reviewEvidenceResult    `json:"deploymentResult"`
	Risks            []string                `json:"risks"`
	FollowUps        []string                `json:"followUps"`
}

type reviewEvidenceCommand struct {
	Command   string `json:"command"`
	Status    string `json:"status"`
	Category  string `json:"category"`
	Summary   string `json:"summary"`
	CreatedAt string `json:"createdAt"`
}

type reviewEvidenceCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type reviewEvidenceResult struct {
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Details string `json:"details"`
}

type testCaseProposalsArtifact struct {
	Proposals []testCaseProposalArtifactItem `json:"proposals"`
	Summary   string                         `json:"summary"`
}

type testCaseProposalArtifactItem struct {
	Type         string          `json:"type"`
	CaseID       string          `json:"caseId"`
	Title        string          `json:"title"`
	Summary      string          `json:"summary"`
	Rationale    string          `json:"rationale"`
	ProposedCase json.RawMessage `json:"proposedCase"`
}

type testSetupResultArtifact struct {
	RunID          string                `json:"runId"`
	Status         string                `json:"status"`
	Summary        string                `json:"summary"`
	FailureSummary string                `json:"failureSummary"`
	Outputs        json.RawMessage       `json:"outputs"`
	Evidence       json.RawMessage       `json:"evidence"`
	Steps          []testSetupResultStep `json:"steps"`
}

type testSetupResultStep struct {
	Title          string          `json:"title"`
	Status         string          `json:"status"`
	Command        string          `json:"command"`
	Summary        string          `json:"summary"`
	FailureSummary string          `json:"failureSummary"`
	Evidence       json.RawMessage `json:"evidence"`
}

type testResultArtifact struct {
	RunID   string                   `json:"runId"`
	Items   []testResultArtifactItem `json:"items"`
	Summary string                   `json:"summary"`
}

type testResultArtifactItem struct {
	RunID          string          `json:"runId,omitempty"`
	CaseID         string          `json:"caseId"`
	Status         string          `json:"status"`
	ActualResult   string          `json:"actualResult"`
	FailureSummary string          `json:"failureSummary"`
	Evidence       json.RawMessage `json:"evidence"`
}

type codexAppServerClient struct {
	process       *agentEngineProcess
	stdin         io.WriteCloser
	stderr        io.Reader
	encoder       *json.Encoder
	pending       map[int64]chan codexRPCResponse
	notifications chan codexRPCNotification
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
	fs.StringVar(&cfg.TokenFile, "token-file", envFirst("MSPACE_RUNTIME_TOKEN_FILE"), "path to a file containing the runtime registration token")
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
	cfg.TokenFile = strings.TrimSpace(cfg.TokenFile)
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
	if cfg.TokenFile != "" {
		if abs, err := filepath.Abs(cfg.TokenFile); err == nil {
			cfg.TokenFile = abs
		}
		token, err := readRuntimeToken(cfg.Token, cfg.TokenFile)
		if err != nil {
			return config{}, err
		}
		cfg.Token = token
	} else if !strings.HasPrefix(cfg.Token, "msw_") {
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
		tokenFile:  cfg.TokenFile,
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
	case "issue_type_triage":
		result, taskErr = executeIssueTypeTriageTask(ctx, client, cfg, workerID, task)
	case "test_case_import_mapping":
		result, taskErr = executeImportMappingTask(ctx, client, cfg, workerID, task)
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

func executeIssueTypeTriageTask(ctx context.Context, client *runtimeClient, cfg config, workerID string, task runtimeTask) (json.RawMessage, error) {
	payload, err := parseIssueTypeTriagePayload(task.Payload)
	if err != nil {
		return nil, err
	}
	runCtx, stopCancelWatcher := client.watchTaskCancellation(ctx, workerID, task.ID, cfg.PollInterval)
	defer stopCancelWatcher()
	if err := client.appendTaskLog(ctx, workerID, task.ID, appendTaskLogInput{Stream: "system", Message: "Starting Codex issue type triage."}); err != nil {
		return nil, err
	}
	result, err := runCodexIssueTypeTriage(runCtx, client, cfg, workerID, task.ID, payload)
	if err != nil {
		_ = client.appendTaskLog(context.WithoutCancel(ctx), workerID, task.ID, appendTaskLogInput{Stream: "codex-error", Message: err.Error()})
		return nil, err
	}
	body, err := json.Marshal(result)
	return body, err
}

func parseIssueTypeTriagePayload(raw json.RawMessage) (issueTypeTriagePayload, error) {
	var payload issueTypeTriagePayload
	if len(raw) == 0 {
		return payload, errors.New("issue type triage payload is required")
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, fmt.Errorf("parse issue type triage payload: %w", err)
	}
	payload.WorkspaceID = strings.TrimSpace(payload.WorkspaceID)
	payload.IssueID = strings.TrimSpace(payload.IssueID)
	payload.ProjectID = strings.TrimSpace(payload.ProjectID)
	payload.Prompt = strings.TrimSpace(payload.Prompt)
	payload.DeveloperInstructions = strings.TrimSpace(payload.DeveloperInstructions)
	payload.Project.Name = strings.TrimSpace(payload.Project.Name)
	payload.Project.RepoPath = strings.TrimSpace(payload.Project.RepoPath)
	if payload.IssueID == "" {
		return payload, errors.New("issue type triage payload issueId is required")
	}
	if payload.Prompt == "" {
		return payload, errors.New("issue type triage payload prompt is required")
	}
	if payload.DeveloperInstructions == "" {
		payload.DeveloperInstructions = defaultIssueTypeTriageDeveloperInstructions()
	}
	return payload, nil
}

func runCodexIssueTypeTriage(ctx context.Context, runtimeClient *runtimeClient, cfg config, workerID, taskID string, payload issueTypeTriagePayload) (issueTypeTriageResult, error) {
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return issueTypeTriageResult{}, errors.New("codex CLI is not available on PATH")
	}
	workdir := os.TempDir()
	codexPayload := agentSessionPayload{
		Workdir:               workdir,
		Prompt:                payload.Prompt,
		DeveloperInstructions: payload.DeveloperInstructions,
		ApprovalPolicy:        "never",
		Sandbox:               "danger-full-access",
		Env:                   payload.Env,
	}
	appClient, err := startCodexAppServer(codexPath, codexPayload)
	if err != nil {
		return issueTypeTriageResult{}, err
	}
	defer appClient.close()

	go captureCodexDiagnosticStream(ctx, runtimeClient, workerID, taskID, appClient.stderr)

	var initResp codexInitializeResponse
	if err := appClient.request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "mspace-worker-triage",
			"title":   "mspace worker triage",
			"version": cfg.Version,
		},
		"capabilities": map[string]any{
			"experimentalApi": true,
		},
	}, &initResp); err != nil {
		return issueTypeTriageResult{}, fmt.Errorf("initialize codex app-server: %w", err)
	}
	_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Codex app-server ready: " + initResp.UserAgent})

	var threadResp codexThreadStartResponse
	if err := appClient.request(ctx, "thread/start", map[string]any{
		"cwd":                    workdir,
		"approvalPolicy":         "never",
		"approvalsReviewer":      "user",
		"sandbox":                "danger-full-access",
		"developerInstructions":  payload.DeveloperInstructions,
		"personality":            "pragmatic",
		"ephemeral":              true,
		"sessionStartSource":     "startup",
		"serviceName":            "mspace-worker-triage",
		"experimentalRawEvents":  false,
		"persistExtendedHistory": false,
	}, &threadResp); err != nil {
		return issueTypeTriageResult{}, fmt.Errorf("start codex triage thread: %w", err)
	}
	if strings.TrimSpace(threadResp.Thread.ID) == "" {
		return issueTypeTriageResult{}, errors.New("codex app-server returned an empty triage thread id")
	}
	_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Codex triage thread: " + threadResp.Thread.ID})

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
		"cwd":            workdir,
		"approvalPolicy": "never",
		"sandboxPolicy": map[string]any{
			"type": "dangerFullAccess",
		},
		"responsesapiClientMetadata": map[string]string{
			"mspace.runtime_task_id": taskID,
			"mspace.issue_id":        payload.IssueID,
			"mspace.task":            "issue_type_triage",
			"mspace.worker_name":     cfg.Name,
		},
	}, &turnResp); err != nil {
		return issueTypeTriageResult{}, fmt.Errorf("start codex triage turn: %w", err)
	}
	if strings.TrimSpace(turnResp.Turn.ID) == "" {
		return issueTypeTriageResult{}, errors.New("codex app-server returned an empty triage turn id")
	}
	_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Codex triage turn: " + turnResp.Turn.ID})

	message, err := waitCodexTurnMessage(ctx, runtimeClient, appClient, workerID, taskID, threadResp.Thread.ID, turnResp.Turn.ID)
	if err != nil {
		return issueTypeTriageResult{}, err
	}
	result, err := parseIssueTypeTriageResult(message)
	if err != nil {
		return issueTypeTriageResult{}, err
	}
	result.ThreadID = threadResp.Thread.ID
	result.TurnID = turnResp.Turn.ID
	_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: fmt.Sprintf("Issue type triage classified as %s.", result.Type)})
	return result, nil
}

func executeImportMappingTask(ctx context.Context, client *runtimeClient, cfg config, workerID string, task runtimeTask) (json.RawMessage, error) {
	payload, err := parseImportMappingPayload(task.Payload)
	if err != nil {
		return nil, err
	}
	runCtx, stopCancelWatcher := client.watchTaskCancellation(ctx, workerID, task.ID, cfg.PollInterval)
	defer stopCancelWatcher()
	if err := client.appendTaskLog(ctx, workerID, task.ID, appendTaskLogInput{Stream: "system", Message: "Starting Codex import column mapping."}); err != nil {
		return nil, err
	}
	result, err := runCodexImportMapping(runCtx, client, cfg, workerID, task.ID, payload)
	if err != nil {
		_ = client.appendTaskLog(context.WithoutCancel(ctx), workerID, task.ID, appendTaskLogInput{Stream: "codex-error", Message: err.Error()})
		return nil, err
	}
	body, err := json.Marshal(result)
	return body, err
}

func parseImportMappingPayload(raw json.RawMessage) (importMappingPayload, error) {
	var payload importMappingPayload
	if len(raw) == 0 {
		return payload, errors.New("import mapping payload is required")
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, fmt.Errorf("parse import mapping payload: %w", err)
	}
	payload.WorkspaceID = strings.TrimSpace(payload.WorkspaceID)
	payload.ProjectID = strings.TrimSpace(payload.ProjectID)
	payload.Format = strings.ToLower(strings.TrimSpace(payload.Format))
	payload.FileName = strings.TrimSpace(payload.FileName)
	payload.Prompt = strings.TrimSpace(payload.Prompt)
	payload.DeveloperInstructions = strings.TrimSpace(payload.DeveloperInstructions)
	payload.Project.ID = strings.TrimSpace(payload.Project.ID)
	payload.Project.Name = strings.TrimSpace(payload.Project.Name)
	for index, header := range payload.Headers {
		payload.Headers[index] = strings.TrimSpace(header)
	}
	if payload.Format != "csv" && payload.Format != "xlsx" {
		return payload, errors.New("import mapping payload format must be csv or xlsx")
	}
	if len(payload.Headers) == 0 {
		return payload, errors.New("import mapping payload requires headers")
	}
	if payload.Prompt == "" {
		return payload, errors.New("import mapping payload prompt is required")
	}
	if payload.DeveloperInstructions == "" {
		payload.DeveloperInstructions = defaultImportMappingDeveloperInstructions()
	}
	return payload, nil
}

func runCodexImportMapping(ctx context.Context, runtimeClient *runtimeClient, cfg config, workerID, taskID string, payload importMappingPayload) (importMappingResult, error) {
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return importMappingResult{}, errors.New("codex CLI is not available on PATH")
	}
	workdir := os.TempDir()
	codexPayload := agentSessionPayload{
		Workdir:               workdir,
		Prompt:                payload.Prompt,
		DeveloperInstructions: payload.DeveloperInstructions,
		ApprovalPolicy:        "never",
		Sandbox:               "danger-full-access",
		Env:                   payload.Env,
	}
	appClient, err := startCodexAppServer(codexPath, codexPayload)
	if err != nil {
		return importMappingResult{}, err
	}
	defer appClient.close()

	go captureCodexDiagnosticStream(ctx, runtimeClient, workerID, taskID, appClient.stderr)

	var initResp codexInitializeResponse
	if err := appClient.request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "mspace-worker-import-mapping",
			"title":   "mspace worker import mapping",
			"version": cfg.Version,
		},
		"capabilities": map[string]any{
			"experimentalApi": true,
		},
	}, &initResp); err != nil {
		return importMappingResult{}, fmt.Errorf("initialize codex app-server: %w", err)
	}
	_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Codex app-server ready: " + initResp.UserAgent})

	var threadResp codexThreadStartResponse
	if err := appClient.request(ctx, "thread/start", map[string]any{
		"cwd":                    workdir,
		"approvalPolicy":         "never",
		"approvalsReviewer":      "user",
		"sandbox":                "danger-full-access",
		"developerInstructions":  payload.DeveloperInstructions,
		"personality":            "pragmatic",
		"ephemeral":              true,
		"sessionStartSource":     "startup",
		"serviceName":            "mspace-worker-import-mapping",
		"experimentalRawEvents":  false,
		"persistExtendedHistory": false,
	}, &threadResp); err != nil {
		return importMappingResult{}, fmt.Errorf("start codex import mapping thread: %w", err)
	}
	if strings.TrimSpace(threadResp.Thread.ID) == "" {
		return importMappingResult{}, errors.New("codex app-server returned an empty import mapping thread id")
	}
	_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Codex import mapping thread: " + threadResp.Thread.ID})

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
		"cwd":            workdir,
		"approvalPolicy": "never",
		"sandboxPolicy": map[string]any{
			"type": "dangerFullAccess",
		},
		"responsesapiClientMetadata": map[string]string{
			"mspace.runtime_task_id": taskID,
			"mspace.task":            "test_case_import_mapping",
			"mspace.worker_name":     cfg.Name,
		},
	}, &turnResp); err != nil {
		return importMappingResult{}, fmt.Errorf("start codex import mapping turn: %w", err)
	}
	if strings.TrimSpace(turnResp.Turn.ID) == "" {
		return importMappingResult{}, errors.New("codex app-server returned an empty import mapping turn id")
	}
	_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Codex import mapping turn: " + turnResp.Turn.ID})

	message, err := waitCodexTurnMessage(ctx, runtimeClient, appClient, workerID, taskID, threadResp.Thread.ID, turnResp.Turn.ID)
	if err != nil {
		return importMappingResult{}, err
	}
	result, err := parseImportMappingResult(message, payload)
	if err != nil {
		return importMappingResult{}, err
	}
	result.ThreadID = threadResp.Thread.ID
	result.TurnID = turnResp.Turn.ID
	_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: fmt.Sprintf("Import mapping produced %d suggestions.", len(result.Suggestions))})
	return result, nil
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
	capabilityKey, err := agentEngineCapabilityKey(payload.AgentEngine)
	if err != nil {
		return nil, err
	}
	if !capabilityEnabled(cfg.Capabilities, capabilityKey) {
		return nil, fmt.Errorf("worker does not advertise required %s capability for agentEngine %s", capabilityKey, payload.AgentEngine)
	}
	runCtx, stopCancelWatcher := client.watchTaskCancellation(ctx, workerID, task.ID, cfg.PollInterval)
	defer stopCancelWatcher()
	if err := client.appendTaskLog(ctx, workerID, task.ID, appendTaskLogInput{Stream: "system", Message: "Starting " + payload.AgentEngine + " agent engine."}); err != nil {
		return nil, err
	}
	result, err := runAgentSession(runCtx, client, cfg, workerID, task.ID, payload)
	if err != nil {
		_ = client.appendTaskLog(context.WithoutCancel(ctx), workerID, task.ID, appendTaskLogInput{Stream: "agent-error", Message: err.Error()})
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
	source := agentSessionSource{}
	if sourceCaptureEnabled(payload) {
		var err error
		source, err = captureAgentSessionSource(ctx, runtimeClient, workerID, taskID, payload)
		if err != nil {
			return agentSessionResult{}, err
		}
	} else {
		_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Source capture disabled for this agent session."})
	}
	result := agentSessionResult{
		AgentEngine:      payload.AgentEngine,
		EngineSessionRef: "dry-run-session-" + shortTaskID(taskID),
		EngineRunRef:     "dry-run-run-" + shortTaskID(taskID),
		Status:           "completed",
		CompletedAt:      time.Now().UTC().Format(time.RFC3339),
		DryRun:           true,
		Workdir:          payload.Workdir,
		ArtifactDir:      payload.ArtifactDir,
		Source:           source,
	}
	if payload.AgentEngine == agentEngineCodex {
		result.ThreadID = "dry-run-thread-" + shortTaskID(taskID)
		result.TurnID = "dry-run-turn-" + shortTaskID(taskID)
	}
	result.attachArtifacts(payload)
	return result, nil
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
	if sourceCaptureEnabled(payload) {
		if err := os.WriteFile(filepath.Join(payload.Workdir, "TEAM_RUNTIME_DRY_RUN.md"), []byte(sourceContent), 0o644); err != nil {
			return fmt.Errorf("write dry-run source file: %w", err)
		}
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
	payload.AgentEngine = strings.TrimSpace(payload.AgentEngine)
	if payload.AgentEngine == "" {
		legacyProvider := strings.ToLower(strings.TrimSpace(payload.LegacyProvider))
		if legacyProvider != "" && legacyProvider != agentEngineCodex {
			return agentSessionPayload{}, fmt.Errorf("unsupported legacy agent provider %q", payload.LegacyProvider)
		}
		payload.AgentEngine = agentEngineCodex
	} else {
		engine, err := normalizeAgentEngine(payload.AgentEngine)
		if err != nil {
			return agentSessionPayload{}, err
		}
		payload.AgentEngine = engine
	}
	payload.Workdir = strings.TrimSpace(payload.Workdir)
	payload.Prompt = strings.TrimSpace(payload.Prompt)
	payload.DeveloperInstructions = strings.TrimSpace(payload.DeveloperInstructions)
	payload.ApprovalPolicy = strings.TrimSpace(payload.ApprovalPolicy)
	payload.Sandbox = strings.TrimSpace(payload.Sandbox)
	payload.IssueID = strings.TrimSpace(payload.IssueID)
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	payload.ProjectID = strings.TrimSpace(payload.ProjectID)
	payload.Automation = strings.TrimSpace(payload.Automation)
	payload.TestRunID = strings.TrimSpace(payload.TestRunID)
	payload.Branch = strings.TrimSpace(payload.Branch)
	payload.SourceCommitSHA = strings.TrimSpace(payload.SourceCommitSHA)
	payload.ArtifactDir = strings.TrimSpace(payload.ArtifactDir)
	payload.Repository.URL = strings.TrimSpace(payload.Repository.URL)
	payload.Repository.DefaultBranch = strings.TrimSpace(payload.Repository.DefaultBranch)
	payload.Repository.SourceType = strings.TrimSpace(payload.Repository.SourceType)
	payload.Repository.Provider = strings.TrimSpace(payload.Repository.Provider)
	payload.Repository.Owner = strings.TrimSpace(payload.Repository.Owner)
	payload.Repository.Repo = strings.TrimSpace(payload.Repository.Repo)
	payload.Repository.OriginalURL = strings.TrimSpace(payload.Repository.OriginalURL)
	payload.Repository.Subdir = normalizeRepositorySubdir(payload.Repository.Subdir)
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
	if _, err := normalizePayloadSkillBundles(payload); err != nil {
		return agentSessionPayload{}, err
	}
	return payload, nil
}

func runCodexEngineProtocol(ctx context.Context, runtimeClient *runtimeClient, cfg config, workerID, taskID string, payload agentSessionPayload, updateRefs func(agentEngineExecution)) (agentEngineExecution, error) {
	execution := agentEngineExecution{AgentEngine: agentEngineCodex}
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return execution, errors.New("codex CLI is not available on PATH")
	}
	appClient, err := startCodexAppServer(codexPath, payload)
	if err != nil {
		return execution, err
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
		return execution, fmt.Errorf("initialize codex app-server: %w", err)
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
		return execution, fmt.Errorf("start codex thread: %w", err)
	}
	if strings.TrimSpace(threadResp.Thread.ID) == "" {
		return execution, errors.New("codex app-server returned an empty thread id")
	}
	execution.EngineSessionRef = opaqueEngineRef(threadResp.Thread.ID)
	execution.LegacyThreadID = threadResp.Thread.ID
	updateRefs(execution)
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
		return execution, fmt.Errorf("start codex turn: %w", err)
	}
	if strings.TrimSpace(turnResp.Turn.ID) == "" {
		return execution, errors.New("codex app-server returned an empty turn id")
	}
	execution.EngineRunRef = opaqueEngineRef(turnResp.Turn.ID)
	execution.LegacyTurnID = turnResp.Turn.ID
	updateRefs(execution)
	_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Codex turn: " + turnResp.Turn.ID})

	if err := waitCodexTurn(ctx, runtimeClient, appClient, workerID, taskID, threadResp.Thread.ID, turnResp.Turn.ID); err != nil {
		return execution, err
	}
	return execution, nil
}

func canCompleteFromTestArtifacts(payload agentSessionPayload) bool {
	switch strings.TrimSpace(payload.Automation) {
	case "test_run_execution", "test_run_setup":
		return strings.TrimSpace(payload.TestRunID) != ""
	default:
		return false
	}
}

func testArtifactsReady(payload agentSessionPayload) bool {
	return testArtifactsReadiness(payload) == testArtifactReady
}

func testArtifactsReadiness(payload agentSessionPayload) testArtifactReadiness {
	switch strings.TrimSpace(payload.Automation) {
	case "test_run_execution":
		artifact, ok := readTestResultArtifact(payload)
		if !ok || !artifactMatchesRun(payload.TestRunID, artifact.RunID) {
			return testArtifactMissing
		}
		if !testResultArtifactScreenshotsReady(payload.ArtifactDir, artifact) {
			return testArtifactPending
		}
		return testArtifactReady
	case "test_run_setup":
		artifact, ok := readTestSetupResultArtifact(payload)
		if ok && artifactMatchesRun(payload.TestRunID, artifact.RunID) {
			return testArtifactReady
		}
		return testArtifactMissing
	default:
		return testArtifactMissing
	}
}

func waitForTestArtifactsReady(ctx context.Context, payload agentSessionPayload, timeout time.Duration) (bool, error) {
	if timeout <= 0 {
		return testArtifactsReady(payload), nil
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		switch testArtifactsReadiness(payload) {
		case testArtifactReady:
			return true, nil
		case testArtifactMissing:
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-deadline.C:
			return false, nil
		case <-ticker.C:
		}
	}
}

func missingTestCompletionArtifactError(payload agentSessionPayload) error {
	name := expectedTestCompletionArtifactName(payload)
	if name == "" {
		return errors.New("test automation completed without a required result artifact")
	}
	path := artifactPath(payload, name)
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("test automation completed without matching %s for run %s", name, payload.TestRunID)
	}
	return fmt.Errorf("test automation completed without matching %s for run %s at %s", name, payload.TestRunID, path)
}

func expectedTestCompletionArtifactName(payload agentSessionPayload) string {
	switch strings.TrimSpace(payload.Automation) {
	case "test_run_execution":
		return testResultArtifactName
	case "test_run_setup":
		return testSetupResultArtifactName
	default:
		return ""
	}
}

func artifactMatchesRun(expectedRunID, artifactRunID string) bool {
	expectedRunID = strings.TrimSpace(expectedRunID)
	if expectedRunID == "" {
		return false
	}
	return strings.TrimSpace(artifactRunID) == expectedRunID
}

func sourceCaptureEnabled(payload agentSessionPayload) bool {
	return payload.SourceCapture == nil || *payload.SourceCapture
}

func (result *agentSessionResult) attachMatchingTestCompletionArtifact(payload agentSessionPayload) bool {
	switch strings.TrimSpace(payload.Automation) {
	case "test_run_execution":
		artifact, ok := readTestResultArtifact(payload)
		if !ok || !artifactMatchesRun(payload.TestRunID, artifact.RunID) {
			return false
		}
		if !testResultArtifactScreenshotsReady(payload.ArtifactDir, artifact) {
			return false
		}
		result.TestResult = &artifact
		return true
	case "test_run_setup":
		artifact, ok := readTestSetupResultArtifact(payload)
		if !ok || !artifactMatchesRun(payload.TestRunID, artifact.RunID) {
			return false
		}
		result.TestSetup = &artifact
		return true
	default:
		return false
	}
}

func (result *agentSessionResult) attachArtifacts(payload agentSessionPayload) {
	if artifact, ok := readTestEnvironmentArtifact(payload); ok {
		result.TestEnvironment = &artifact
	}
	if artifact, ok := readReviewEvidenceArtifact(payload); ok {
		result.ReviewEvidence = &artifact
	}
	if artifact, ok := readTestCaseProposalsArtifact(payload); ok {
		result.TestCaseProposals = &artifact
	}
	if artifact, ok := readTestSetupResultArtifact(payload); ok {
		result.TestSetup = &artifact
	}
	if artifact, ok := readTestResultArtifact(payload); ok {
		result.TestResult = &artifact
	}
}

func startCodexAppServer(codexPath string, payload agentSessionPayload) (*codexAppServerClient, error) {
	cmd, err := newAgentEngineCommand(codexPath, "app-server", "--listen", "stdio://")
	if err != nil {
		return nil, err
	}
	cmd.Dir = payload.Workdir
	cmd.Env = defaultAgentEngineEnv(payload.Env)
	configureAgentEngineProcess(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stdin pipe: %w", err)
	}
	process, stdout, stderr, err := startAgentEngineProcess(cmd, stdin)
	if err != nil {
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}

	client := &codexAppServerClient{
		process:       process,
		stdin:         stdin,
		stderr:        stderr,
		encoder:       json.NewEncoder(stdin),
		pending:       map[int64]chan codexRPCResponse{},
		notifications: make(chan codexRPCNotification, 128),
	}
	go client.readLoop(stdout)
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
	if c == nil || c.process == nil {
		return
	}
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	select {
	case <-c.process.done:
		return
	case <-time.After(500 * time.Millisecond):
	}
	c.process.stop(2 * time.Second)
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

func waitCodexTurnMessage(ctx context.Context, runtimeClient *runtimeClient, appClient *codexAppServerClient, workerID, taskID, threadID, turnID string) (string, error) {
	outputBuffers := map[string]*strings.Builder{}
	lastAgentMessage := ""
	for {
		select {
		case <-ctx.Done():
			interruptCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = appClient.request(interruptCtx, "turn/interrupt", map[string]any{
				"threadId": threadID,
				"turnId":   turnID,
			}, nil)
			cancel()
			return "", context.Canceled
		case notification, ok := <-appClient.notifications:
			if !ok {
				return "", errors.New("codex app-server exited before the turn completed")
			}
			done, err := handleCodexNotification(ctx, runtimeClient, workerID, taskID, threadID, turnID, notification, outputBuffers)
			if notification.Method == "item/completed" {
				var payload codexItemNotification
				if decodeCodexParams(notification.Params, &payload) == nil && sameCodexTurn(threadID, turnID, payload.ThreadID, payload.TurnID) && payload.Item.Type == "agentMessage" && strings.TrimSpace(payload.Item.Text) != "" {
					lastAgentMessage = strings.TrimSpace(payload.Item.Text)
				}
			}
			if done {
				if err != nil {
					return "", err
				}
				if lastAgentMessage == "" {
					return "", errors.New("codex turn completed without an agent message")
				}
				return lastAgentMessage, nil
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
		var skillRefs []string
		var err error
		payload, skillRefs, err = materializePayloadSkillBundles(payload)
		if err != nil {
			return payload, err
		}
		payload.Env = withPayloadRuntimeEnv(payload)
		appendMaterializedSkillLog(ctx, runtimeClient, workerID, taskID, skillRefs)
		return payload, nil
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return payload, errors.New("git is not available on PATH")
	}
	if err := os.MkdirAll(cfg.WorkRoot, 0o755); err != nil {
		return payload, fmt.Errorf("create worker work root: %w", err)
	}
	payload.Repository, err = resolveWorkerRepository(ctx, gitPath, payload.Repository)
	if err != nil {
		return payload, err
	}
	if payload.Repository.OriginalURL != "" && payload.Repository.OriginalURL != payload.Repository.URL {
		message := "Resolved local project path to repository root: " + payload.Repository.URL
		if payload.Repository.Subdir != "" {
			message += " (project subdirectory: " + payload.Repository.Subdir + ")"
		}
		_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: message})
	}
	repoDir := filepath.Join(cfg.WorkRoot, "repos", repositoryCacheKey(payload.Repository))
	if err := ensureWorkerRepository(ctx, gitPath, payload.Repository.URL, repoDir); err != nil {
		return payload, err
	}
	worktreeDir := filepath.Join(cfg.WorkRoot, "workdirs", safePathPart(firstNonEmpty(payload.ProjectID, "project")), safePathPart(firstNonEmpty(payload.SessionID, taskID)))
	agentWorkdir := worktreeDir
	if subdir := normalizeRepositorySubdir(payload.Repository.Subdir); subdir != "" {
		agentWorkdir = filepath.Join(worktreeDir, filepath.FromSlash(subdir))
	}
	if err := os.MkdirAll(filepath.Dir(worktreeDir), 0o755); err != nil {
		return payload, fmt.Errorf("create worker workdir parent: %w", err)
	}
	payload.Workdir = agentWorkdir
	payload.ArtifactDir = filepath.Join(agentWorkdir, ".mspace", "session")
	if _, err := os.Stat(worktreeDir); err == nil {
		readiness := testArtifactsReadiness(payload)
		if readiness == testArtifactPending {
			ready, waitErr := waitForTestArtifactsReady(ctx, payload, testArtifactCompletionSettleTimeout)
			if waitErr != nil {
				return payload, waitErr
			}
			if ready {
				readiness = testArtifactReady
			}
		}
		if readiness == testArtifactReady {
			var skillRefs []string
			var err error
			payload, skillRefs, err = materializePayloadSkillBundles(payload)
			if err != nil {
				return payload, err
			}
			payload.Env = withPayloadRuntimeEnv(payload)
			appendMaterializedSkillLog(ctx, runtimeClient, workerID, taskID, skillRefs)
			_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{Stream: "system", Message: "Recovered existing worker workspace from completed test artifacts: " + payload.Workdir})
			return payload, nil
		}
		return payload, fmt.Errorf("worker session workdir already exists: %s", worktreeDir)
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
	if err := runGitCommand(ctx, gitPath, repoDir, "worktree", "add", "--detach", worktreeDir, baseRef); err != nil {
		return payload, fmt.Errorf("create worker worktree from %q: %w", baseRef, err)
	}
	if branch := strings.TrimSpace(payload.Branch); branch != "" && payload.SourceCommitSHA == "" {
		if err := runGitCommand(ctx, gitPath, worktreeDir, "checkout", "-B", branch); err != nil {
			_ = runGitCommand(context.Background(), gitPath, repoDir, "worktree", "remove", "--force", worktreeDir)
			return payload, fmt.Errorf("create worker branch %q: %w", branch, err)
		}
	}
	if info, err := os.Stat(agentWorkdir); err != nil {
		_ = runGitCommand(context.Background(), gitPath, repoDir, "worktree", "remove", "--force", worktreeDir)
		return payload, fmt.Errorf("project subdirectory is not present in worker worktree: %s: %w", agentWorkdir, err)
	} else if !info.IsDir() {
		_ = runGitCommand(context.Background(), gitPath, repoDir, "worktree", "remove", "--force", worktreeDir)
		return payload, fmt.Errorf("project subdirectory is not a directory in worker worktree: %s", agentWorkdir)
	}
	if err := os.MkdirAll(payload.ArtifactDir, 0o755); err != nil {
		return payload, fmt.Errorf("create artifact dir: %w", err)
	}
	var skillRefs []string
	payload, skillRefs, err = materializePayloadSkillBundles(payload)
	if err != nil {
		return payload, err
	}
	payload.ContextMarkdown = appendWorkerRepositoryContext(payload.ContextMarkdown, payload.Repository)
	contextPath := filepath.Join(payload.ArtifactDir, "context.md")
	if strings.TrimSpace(payload.ContextMarkdown) != "" {
		if err := os.WriteFile(contextPath, []byte(payload.ContextMarkdown), 0o600); err != nil {
			return payload, fmt.Errorf("write session context: %w", err)
		}
	}
	payload.Env = withPayloadRuntimeEnv(payload)
	appendMaterializedSkillLog(ctx, runtimeClient, workerID, taskID, skillRefs)
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

func resolveWorkerRepository(ctx context.Context, gitPath string, repository repositorySpec) (repositorySpec, error) {
	repository.URL = strings.TrimSpace(repository.URL)
	if repository.URL == "" || !isLocalRepository(repository) {
		return repository, nil
	}
	originalURL := repository.URL
	info, err := os.Stat(originalURL)
	if err != nil {
		return repository, fmt.Errorf("local project path is not visible to this worker: %s: %w", originalURL, err)
	}
	if !info.IsDir() {
		return repository, fmt.Errorf("local project path is not a directory: %s", originalURL)
	}
	root, err := runGitOutput(ctx, gitPath, originalURL, "rev-parse", "--show-toplevel")
	if err != nil {
		return repository, fmt.Errorf("local project path is not inside a git work tree visible to this worker: %s: %w", originalURL, err)
	}
	root = filepath.Clean(root)
	selectedPath := filepath.Clean(originalURL)
	if abs, err := filepath.Abs(selectedPath); err == nil {
		selectedPath = filepath.Clean(abs)
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = filepath.Clean(abs)
	}
	if evaluated, err := filepath.EvalSymlinks(selectedPath); err == nil {
		selectedPath = filepath.Clean(evaluated)
	}
	if evaluated, err := filepath.EvalSymlinks(root); err == nil {
		root = filepath.Clean(evaluated)
	}
	subdir, err := filepath.Rel(root, selectedPath)
	if err != nil {
		return repository, fmt.Errorf("resolve project subdirectory relative to git root: %w", err)
	}
	if subdir == "." {
		subdir = ""
	}
	if subdir != "" && (subdir == ".." || strings.HasPrefix(subdir, ".."+string(os.PathSeparator)) || filepath.IsAbs(subdir)) {
		return repository, fmt.Errorf("local project path %s is outside resolved git root %s", originalURL, root)
	}
	repository.OriginalURL = originalURL
	repository.URL = root
	repository.Subdir = normalizeRepositorySubdir(subdir)
	return repository, nil
}

func appendWorkerRepositoryContext(markdown string, repository repositorySpec) string {
	root := strings.TrimSpace(repository.URL)
	original := strings.TrimSpace(repository.OriginalURL)
	subdir := normalizeRepositorySubdir(repository.Subdir)
	if original == "" && subdir == "" {
		return markdown
	}
	var builder strings.Builder
	if trimmed := strings.TrimRight(markdown, "\n"); trimmed != "" {
		builder.WriteString(trimmed)
		builder.WriteString("\n\n")
	}
	builder.WriteString("## Worker Repository\n")
	if root != "" {
		builder.WriteString("Root: " + root + "\n")
	}
	if original != "" && original != root {
		builder.WriteString("Configured path: " + original + "\n")
	}
	if subdir != "" {
		builder.WriteString("Project subdirectory: " + subdir + "\n")
	}
	return builder.String()
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

func artifactPath(payload agentSessionPayload, name string) string {
	if strings.TrimSpace(payload.ArtifactDir) != "" {
		return filepath.Join(payload.ArtifactDir, name)
	}
	if strings.TrimSpace(payload.Workdir) != "" {
		return filepath.Join(payload.Workdir, ".mspace", "session", name)
	}
	return ""
}

func readTestEnvironmentArtifact(payload agentSessionPayload) (agentSessionTestEnvironment, bool) {
	path := artifactPath(payload, testEnvironmentArtifactName)
	if path == "" {
		return agentSessionTestEnvironment{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return agentSessionTestEnvironment{}, false
	}
	var artifact agentSessionTestEnvironment
	if err := json.Unmarshal(data, &artifact); err != nil {
		return agentSessionTestEnvironment{}, false
	}
	if previewURL := firstNonEmpty(artifact.PreviewURL, artifact.PreviewURLSnake, artifact.URL); previewURL != "" {
		artifact.PreviewURL = previewURL
	}
	return artifact, artifact.PreviewURL != ""
}

func readReviewEvidenceArtifact(payload agentSessionPayload) (reviewEvidenceArtifact, bool) {
	path := artifactPath(payload, reviewEvidenceArtifactName)
	if path == "" {
		return reviewEvidenceArtifact{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return reviewEvidenceArtifact{}, false
	}
	var raw struct {
		AgentSummary     string          `json:"agentSummary"`
		CommandsRun      json.RawMessage `json:"commandsRun"`
		Tests            json.RawMessage `json:"tests"`
		BuildResult      json.RawMessage `json:"buildResult"`
		DeploymentResult json.RawMessage `json:"deploymentResult"`
		Risks            json.RawMessage `json:"risks"`
		FollowUps        json.RawMessage `json:"followUps"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return reviewEvidenceArtifact{}, false
	}
	return reviewEvidenceArtifact{
		AgentSummary:     strings.TrimSpace(raw.AgentSummary),
		CommandsRun:      parseReviewCommandsValue(raw.CommandsRun),
		Tests:            parseReviewChecksValue(raw.Tests),
		BuildResult:      parseReviewResultValue(raw.BuildResult),
		DeploymentResult: parseReviewResultValue(raw.DeploymentResult),
		Risks:            parseReviewStringListValue(raw.Risks),
		FollowUps:        parseReviewStringListValue(raw.FollowUps),
	}, true
}

func readTestCaseProposalsArtifact(payload agentSessionPayload) (testCaseProposalsArtifact, bool) {
	path := artifactPath(payload, testCaseProposalsArtifactName)
	if path == "" {
		return testCaseProposalsArtifact{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return testCaseProposalsArtifact{}, false
	}
	var artifact testCaseProposalsArtifact
	if err := json.Unmarshal(data, &artifact); err != nil || len(artifact.Proposals) == 0 {
		return testCaseProposalsArtifact{}, false
	}
	return artifact, true
}

func readTestResultArtifact(payload agentSessionPayload) (testResultArtifact, bool) {
	path := artifactPath(payload, testResultArtifactName)
	if path == "" {
		return testResultArtifact{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return testResultArtifact{}, false
	}
	var artifact testResultArtifact
	if err := json.Unmarshal(data, &artifact); err != nil || len(artifact.Items) == 0 {
		var items []testResultArtifactItem
		if err := json.Unmarshal(data, &items); err != nil || len(items) == 0 {
			return testResultArtifact{}, false
		}
		artifact.Items = items
		artifact.RunID = strings.TrimSpace(items[0].RunID)
	}
	artifact = enrichTestResultArtifactEvidence(payload, artifact)
	return artifact, true
}

func readTestSetupResultArtifact(payload agentSessionPayload) (testSetupResultArtifact, bool) {
	path := artifactPath(payload, testSetupResultArtifactName)
	if path == "" {
		return testSetupResultArtifact{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return testSetupResultArtifact{}, false
	}
	var artifact testSetupResultArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		return testSetupResultArtifact{}, false
	}
	if strings.TrimSpace(artifact.Status) == "" && strings.TrimSpace(artifact.Summary) == "" && len(artifact.Outputs) == 0 && len(artifact.Evidence) == 0 && len(artifact.Steps) == 0 {
		return testSetupResultArtifact{}, false
	}
	if len(artifact.Outputs) == 0 {
		artifact.Outputs = json.RawMessage(`{}`)
	}
	if len(artifact.Evidence) == 0 {
		artifact.Evidence = json.RawMessage(`{}`)
	}
	for index, step := range artifact.Steps {
		if len(step.Evidence) == 0 {
			step.Evidence = json.RawMessage(`{}`)
		}
		artifact.Steps[index] = step
	}
	return artifact, true
}

func enrichTestResultArtifactEvidence(payload agentSessionPayload, artifact testResultArtifact) testResultArtifact {
	for index, item := range artifact.Items {
		item.Evidence = enrichTestResultEvidence(payload.ArtifactDir, item.Evidence)
		artifact.Items[index] = item
	}
	return artifact
}

func enrichTestResultEvidence(artifactDir string, evidence json.RawMessage) json.RawMessage {
	if len(evidence) == 0 {
		return evidence
	}
	var record map[string]any
	if err := json.Unmarshal(evidence, &record); err != nil || len(record) == 0 {
		return evidence
	}
	paths := localEvidenceScreenshotPaths(record)
	if len(paths) == 0 {
		return evidence
	}
	images := existingEvidenceScreenshotImages(record["screenshotImages"])
	for _, path := range paths {
		image, ok := readTestResultScreenshotDataURL(artifactDir, path)
		if !ok {
			continue
		}
		images = append(images, image)
	}
	if len(images) == 0 {
		return evidence
	}
	record["screenshotImages"] = images
	data, err := json.Marshal(record)
	if err != nil {
		return evidence
	}
	return data
}

func existingEvidenceScreenshotImages(value any) []any {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	images := []any{}
	for _, item := range values {
		record, ok := item.(map[string]any)
		if !ok || !hasEmbeddedOrRemoteScreenshotSource(record) {
			continue
		}
		images = append(images, record)
	}
	return images
}

func testResultArtifactScreenshotsReady(artifactDir string, artifact testResultArtifact) bool {
	return len(testResultArtifactPendingScreenshotPathsFromArtifact(artifactDir, artifact)) == 0
}

func testResultArtifactPendingScreenshotPaths(payload agentSessionPayload) []string {
	artifact, ok := readTestResultArtifact(payload)
	if !ok || !artifactMatchesRun(payload.TestRunID, artifact.RunID) {
		return nil
	}
	return testResultArtifactPendingScreenshotPathsFromArtifact(payload.ArtifactDir, artifact)
}

func testResultArtifactPendingScreenshotPathsFromArtifact(artifactDir string, artifact testResultArtifact) []string {
	pending := []string{}
	for _, item := range artifact.Items {
		record := map[string]any{}
		if len(item.Evidence) == 0 || json.Unmarshal(item.Evidence, &record) != nil || len(record) == 0 {
			continue
		}
		for _, path := range localEvidenceScreenshotPaths(record) {
			if _, ok := readTestResultScreenshotDataURL(artifactDir, path); !ok {
				pending = append(pending, path)
			}
		}
	}
	return dedupeStrings(pending)
}

func localEvidenceScreenshotPaths(record map[string]any) []string {
	paths := screenshotPathReferences(record["screenshot"])
	for _, key := range []string{"screenshots", "screenshotPaths"} {
		paths = append(paths, screenshotPathReferences(record[key])...)
	}
	paths = append(paths, screenshotImagePathReferences(record["screenshotImages"])...)
	return dedupeStrings(paths)
}

func screenshotPathReferences(value any) []string {
	switch typed := value.(type) {
	case string:
		if isLocalScreenshotPathReference(typed) {
			return []string{strings.TrimSpace(typed)}
		}
	case []any:
		paths := []string{}
		for _, item := range typed {
			paths = append(paths, screenshotPathReferences(item)...)
		}
		return paths
	}
	return nil
}

func screenshotImagePathReferences(value any) []string {
	switch typed := value.(type) {
	case []any:
		paths := []string{}
		for _, item := range typed {
			paths = append(paths, screenshotImagePathReferences(item)...)
		}
		return paths
	case map[string]any:
		if hasEmbeddedOrRemoteScreenshotSource(typed) {
			return nil
		}
		if path, ok := typed["path"].(string); ok && isLocalScreenshotPathReference(path) {
			return []string{strings.TrimSpace(path)}
		}
	}
	return nil
}

func hasEmbeddedOrRemoteScreenshotSource(record map[string]any) bool {
	for _, key := range []string{"dataUrl", "dataURL", "data_url", "base64", "url", "artifactUrl", "artifactURL", "artifact_url", "thumbnailUrl", "thumbnailURL", "thumbnail_url"} {
		if value, ok := record[key].(string); ok && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func isLocalScreenshotPathReference(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "data:") || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "/api/") || strings.HasPrefix(value, "/artifacts/") {
		return false
	}
	return true
}

func readTestResultScreenshotDataURL(artifactDir, screenshotPath string) (map[string]string, bool) {
	artifactDir = strings.TrimSpace(artifactDir)
	screenshotPath = strings.TrimSpace(screenshotPath)
	if artifactDir == "" || screenshotPath == "" {
		return nil, false
	}
	path := screenshotPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(artifactDir, path)
	}
	artifactRoot, err := filepath.Abs(artifactDir)
	if err != nil {
		return nil, false
	}
	absPath, err := filepath.Abs(path)
	if err != nil || !strings.HasPrefix(absPath, artifactRoot+string(os.PathSeparator)) {
		return nil, false
	}
	content, err := os.ReadFile(absPath)
	if err != nil || len(content) == 0 || len(content) > maxTestResultScreenshotBytes {
		return nil, false
	}
	mimeType := http.DetectContentType(content)
	if mimeType != "image/png" && mimeType != "image/jpeg" && mimeType != "image/webp" && mimeType != "image/gif" {
		return nil, false
	}
	return map[string]string{
		"path":    screenshotPath,
		"mime":    mimeType,
		"dataUrl": "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(content),
	}, true
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	deduped := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		deduped = append(deduped, value)
	}
	return deduped
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

func parseReviewCommandsValue(data json.RawMessage) []reviewEvidenceCommand {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var commands []reviewEvidenceCommand
	if err := json.Unmarshal(data, &commands); err == nil {
		return normalizeReviewCommands(commands)
	}
	var commandStrings []string
	if err := json.Unmarshal(data, &commandStrings); err == nil {
		commands = make([]reviewEvidenceCommand, 0, len(commandStrings))
		for _, command := range commandStrings {
			command = strings.TrimSpace(command)
			if command == "" {
				continue
			}
			commands = append(commands, reviewEvidenceCommand{Command: command})
		}
		return normalizeReviewCommands(commands)
	}
	return nil
}

func parseReviewChecksValue(data json.RawMessage) []reviewEvidenceCheck {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var checks []reviewEvidenceCheck
	if err := json.Unmarshal(data, &checks); err == nil {
		return normalizeReviewChecks(checks)
	}
	var checkMap map[string]any
	if err := json.Unmarshal(data, &checkMap); err == nil {
		checks = make([]reviewEvidenceCheck, 0, len(checkMap))
		for name, value := range checkMap {
			summary := reviewEvidenceValueText(value)
			checks = append(checks, reviewEvidenceCheck{
				Name:    strings.TrimSpace(name),
				Status:  inferReviewStatus(summary),
				Summary: truncate(summary, 600),
			})
		}
		return normalizeReviewChecks(checks)
	}
	return nil
}

func parseReviewResultValue(data json.RawMessage) reviewEvidenceResult {
	if len(data) == 0 || string(data) == "null" {
		return reviewEvidenceResult{}
	}
	var result reviewEvidenceResult
	if err := json.Unmarshal(data, &result); err == nil {
		result.Status = normalizeReviewStatus(result.Status)
		result.Summary = strings.TrimSpace(result.Summary)
		result.Details = truncate(strings.TrimSpace(result.Details), 1200)
		return result
	}
	var summary string
	if err := json.Unmarshal(data, &summary); err == nil {
		summary = strings.TrimSpace(summary)
		return reviewEvidenceResult{Status: inferReviewStatus(summary), Summary: summary}
	}
	summary = reviewEvidenceValueText(data)
	return reviewEvidenceResult{Status: inferReviewStatus(summary), Summary: truncate(summary, 600)}
}

func parseReviewStringListValue(data json.RawMessage) []string {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var values []string
	if err := json.Unmarshal(data, &values); err == nil {
		return normalizeStringList(values)
	}
	var value string
	if err := json.Unmarshal(data, &value); err == nil {
		return normalizeStringList([]string{value})
	}
	var valueMap map[string]any
	if err := json.Unmarshal(data, &valueMap); err == nil {
		values = make([]string, 0, len(valueMap))
		for key, value := range valueMap {
			text := strings.TrimSpace(reviewEvidenceValueText(value))
			if text == "" {
				continue
			}
			values = append(values, fmt.Sprintf("%s: %s", key, text))
		}
		return normalizeStringList(values)
	}
	return nil
}

func reviewEvidenceValueText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.RawMessage:
		var decoded any
		if err := json.Unmarshal(typed, &decoded); err == nil {
			return reviewEvidenceValueText(decoded)
		}
		return strings.TrimSpace(string(typed))
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	}
}

func normalizeReviewCommands(commands []reviewEvidenceCommand) []reviewEvidenceCommand {
	result := make([]reviewEvidenceCommand, 0, len(commands))
	for _, command := range commands {
		command.Command = strings.TrimSpace(command.Command)
		if command.Command == "" {
			continue
		}
		command.Status = normalizeReviewStatus(command.Status)
		command.Category = strings.TrimSpace(command.Category)
		command.Summary = truncate(strings.TrimSpace(command.Summary), 600)
		result = append(result, command)
	}
	return result
}

func normalizeReviewChecks(checks []reviewEvidenceCheck) []reviewEvidenceCheck {
	result := make([]reviewEvidenceCheck, 0, len(checks))
	for _, check := range checks {
		check.Name = strings.TrimSpace(check.Name)
		if check.Name == "" {
			continue
		}
		check.Status = normalizeReviewStatus(check.Status)
		check.Summary = truncate(strings.TrimSpace(check.Summary), 600)
		result = append(result, check)
	}
	return result
}

func normalizeReviewStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "success", "succeeded", "ok", "pass":
		return "passed"
	case "failure", "error", "fail":
		return "failed"
	default:
		return status
	}
}

func inferReviewStatus(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "failed") || strings.Contains(lower, " failure") || strings.Contains(lower, "error"):
		return "failed"
	case strings.Contains(lower, "passed") || strings.Contains(lower, "success") || strings.Contains(lower, " ok"):
		return "passed"
	default:
		return ""
	}
}

func normalizeStringList(values []string) []string {
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
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
	if strings.TrimSpace(payload.Repository.URL) != "" {
		env["MSPACE_REPOSITORY_URL"] = payload.Repository.URL
	}
	if strings.TrimSpace(payload.Repository.OriginalURL) != "" {
		env["MSPACE_PROJECT_REPOSITORY_PATH"] = payload.Repository.OriginalURL
	}
	if subdir := normalizeRepositorySubdir(payload.Repository.Subdir); subdir != "" {
		env["MSPACE_PROJECT_SUBDIR"] = subdir
	}
	return env
}

type normalizedSkillBundle struct {
	Name          string
	Revision      string
	DirectoryName string
	Digest        string
	Files         []normalizedSkillBundleFile
}

type normalizedSkillBundleFile struct {
	Path       string
	Content    []byte
	SHA256     string
	Executable bool
}

type materializedSkillsManifest struct {
	Root   string                           `json:"root"`
	Skills []materializedSkillManifestEntry `json:"skills"`
}

type materializedSkillManifestEntry struct {
	Name      string                          `json:"name"`
	Revision  string                          `json:"revision,omitempty"`
	Directory string                          `json:"directory"`
	SHA256    string                          `json:"sha256"`
	Files     []materializedSkillManifestFile `json:"files"`
}

type materializedSkillManifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

func materializePayloadSkillBundles(payload agentSessionPayload) (agentSessionPayload, []string, error) {
	bundles, err := normalizePayloadSkillBundles(payload)
	if err != nil {
		return payload, nil, err
	}
	if len(bundles) == 0 {
		return payload, nil, nil
	}
	if strings.TrimSpace(payload.Workdir) == "" {
		return payload, nil, errors.New("agent_session skill bundles require workdir")
	}
	if strings.TrimSpace(payload.ArtifactDir) == "" {
		return payload, nil, errors.New("agent_session skill bundles require artifactDir")
	}
	if err := ensureSessionArtifactPath(payload.Workdir, payload.ArtifactDir); err != nil {
		return payload, nil, err
	}

	skillsRoot := filepath.Join(payload.ArtifactDir, "skills")
	if err := os.RemoveAll(skillsRoot); err != nil {
		return payload, nil, fmt.Errorf("reset session skills dir: %w", err)
	}
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		return payload, nil, fmt.Errorf("create session skills dir: %w", err)
	}
	if err := ensureSessionArtifactPath(payload.Workdir, skillsRoot); err != nil {
		return payload, nil, err
	}

	manifest := materializedSkillsManifest{Root: skillsRoot}
	refs := make([]string, 0, len(bundles))
	for _, bundle := range bundles {
		skillDir := filepath.Join(skillsRoot, bundle.DirectoryName)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return payload, nil, fmt.Errorf("create skill bundle dir %q: %w", bundle.Name, err)
		}
		if err := ensurePathWithin(skillsRoot, skillDir); err != nil {
			return payload, nil, err
		}

		manifestSkill := materializedSkillManifestEntry{
			Name:      bundle.Name,
			Revision:  bundle.Revision,
			Directory: skillDir,
			SHA256:    bundle.Digest,
			Files:     make([]materializedSkillManifestFile, 0, len(bundle.Files)),
		}
		for _, file := range bundle.Files {
			target := filepath.Join(skillDir, filepath.FromSlash(file.Path))
			if err := ensurePathWithin(skillDir, target); err != nil {
				return payload, nil, err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return payload, nil, fmt.Errorf("create skill file parent %q: %w", file.Path, err)
			}
			mode := os.FileMode(0o600)
			if file.Executable {
				mode = 0o700
			}
			if err := os.WriteFile(target, file.Content, mode); err != nil {
				return payload, nil, fmt.Errorf("write skill file %q: %w", file.Path, err)
			}
			manifestSkill.Files = append(manifestSkill.Files, materializedSkillManifestFile{
				Path:   file.Path,
				SHA256: file.SHA256,
				Bytes:  len(file.Content),
			})
		}
		manifest.Skills = append(manifest.Skills, manifestSkill)
		refs = append(refs, skillBundleRef(bundle.Name, bundle.Revision))
	}

	manifestPath := filepath.Join(skillsRoot, "manifest.json")
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return payload, nil, fmt.Errorf("encode session skill manifest: %w", err)
	}
	if err := os.WriteFile(manifestPath, append(data, '\n'), 0o600); err != nil {
		return payload, nil, fmt.Errorf("write session skill manifest: %w", err)
	}

	env := map[string]string{}
	for key, value := range payload.Env {
		env[key] = value
	}
	env["MSPACE_SESSION_SKILLS_DIR"] = skillsRoot
	env["MSPACE_SESSION_SKILL_MANIFEST"] = manifestPath
	env["MSPACE_REQUIRED_SKILLS"] = strings.Join(refs, ",")
	payload.Env = env
	payload.DeveloperInstructions = withSessionSkillDeveloperInstructions(payload.DeveloperInstructions)
	return payload, refs, nil
}

func normalizePayloadSkillBundles(payload agentSessionPayload) ([]normalizedSkillBundle, error) {
	input := append([]skillBundle{}, payload.RequiredSkills...)
	input = append(input, payload.Skills...)
	if len(input) == 0 {
		return nil, nil
	}

	bundles := make([]normalizedSkillBundle, 0, len(input))
	directories := map[string]string{}
	for index, bundle := range input {
		name := strings.TrimSpace(firstNonEmpty(bundle.Slug, bundle.Name))
		if name == "" {
			return nil, fmt.Errorf("skill bundle %d requires name", index)
		}
		if len(bundle.Files) == 0 {
			return nil, fmt.Errorf("skill bundle %q requires files", name)
		}
		directoryName := safePathPart(name)
		if directoryName == "unknown" {
			return nil, fmt.Errorf("skill bundle %q has no safe directory name", name)
		}
		if existing, ok := directories[directoryName]; ok {
			return nil, fmt.Errorf("skill bundle %q collides with %q after path normalization", name, existing)
		}
		directories[directoryName] = name

		normalized := normalizedSkillBundle{
			Name:          name,
			Revision:      strings.TrimSpace(bundle.Revision),
			DirectoryName: directoryName,
			Files:         make([]normalizedSkillBundleFile, 0, len(bundle.Files)),
		}
		paths := map[string]bool{}
		for _, file := range bundle.Files {
			normalizedPath, err := normalizeSkillBundlePath(file.Path)
			if err != nil {
				return nil, fmt.Errorf("skill bundle %q file path: %w", name, err)
			}
			if paths[normalizedPath] {
				return nil, fmt.Errorf("skill bundle %q contains duplicate file path %q", name, normalizedPath)
			}
			paths[normalizedPath] = true

			content, err := skillBundleFileContent(file)
			if err != nil {
				return nil, fmt.Errorf("skill bundle %q file %q: %w", name, normalizedPath, err)
			}
			digest := sha256Hex(content)
			expected, err := normalizeOptionalSHA256(file.SHA256)
			if err != nil {
				return nil, fmt.Errorf("skill bundle %q file %q: %w", name, normalizedPath, err)
			}
			if expected != "" && expected != digest {
				return nil, fmt.Errorf("skill bundle %q file %q sha256 mismatch", name, normalizedPath)
			}
			normalized.Files = append(normalized.Files, normalizedSkillBundleFile{
				Path:       normalizedPath,
				Content:    content,
				SHA256:     digest,
				Executable: file.Executable,
			})
		}
		sort.Slice(normalized.Files, func(i, j int) bool {
			return normalized.Files[i].Path < normalized.Files[j].Path
		})
		normalized.Digest = skillBundleDigest(normalized.Files)
		expectedBundleDigest, err := normalizeOptionalSHA256(firstNonEmpty(bundle.SHA256, bundle.Hash, bundle.ContentHash))
		if err != nil {
			return nil, fmt.Errorf("skill bundle %q: %w", name, err)
		}
		if expectedBundleDigest != "" && expectedBundleDigest != normalized.Digest {
			return nil, fmt.Errorf("skill bundle %q sha256 mismatch", name)
		}
		bundles = append(bundles, normalized)
	}
	return bundles, nil
}

func normalizeSkillBundlePath(value string) (string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return "", errors.New("path is empty")
	}
	if strings.Contains(raw, "\x00") {
		return "", errors.New("path contains NUL byte")
	}
	if strings.Contains(raw, "\\") {
		return "", fmt.Errorf("path %q must use slash separators", raw)
	}
	if strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("path %q must be relative", raw)
	}
	cleaned := path.Clean(raw)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path %q escapes skill directory", raw)
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("path %q contains unsafe segment", raw)
		}
	}
	return cleaned, nil
}

func skillBundleFileContent(file skillBundleFile) ([]byte, error) {
	if file.Content != nil && file.ContentBase64 != nil {
		return nil, errors.New("specify content or contentBase64, not both")
	}
	if file.ContentBase64 != nil {
		content, err := base64.StdEncoding.DecodeString(*file.ContentBase64)
		if err != nil {
			return nil, fmt.Errorf("decode contentBase64: %w", err)
		}
		return content, nil
	}
	if file.Content == nil {
		return nil, errors.New("requires content or contentBase64")
	}
	return []byte(*file.Content), nil
}

func normalizeOptionalSHA256(value string) (string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "sha256:")
	if value == "" {
		return "", nil
	}
	if len(value) != sha256.Size*2 {
		return "", fmt.Errorf("sha256 must be %d hex characters", sha256.Size*2)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("sha256 must be hex: %w", err)
	}
	return value, nil
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func skillBundleDigest(files []normalizedSkillBundleFile) string {
	hash := sha256.New()
	for _, file := range files {
		_, _ = hash.Write([]byte(file.Path))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(file.SHA256))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func skillBundleRef(name, revision string) string {
	name = strings.TrimSpace(name)
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return name
	}
	return name + "@" + revision
}

func withSessionSkillDeveloperInstructions(instructions string) string {
	instructions = strings.TrimSpace(instructions)
	sessionSkillInstructions := strings.TrimSpace(`
Server-provided skills for this task are materialized in ${MSPACE_SESSION_SKILLS_DIR}; the manifest is ${MSPACE_SESSION_SKILL_MANIFEST}.
When the task names one of these skills, read that session-scoped SKILL.md and use it instead of any globally installed skill copy.
`)
	if instructions == "" {
		return sessionSkillInstructions
	}
	if strings.Contains(instructions, "MSPACE_SESSION_SKILLS_DIR") {
		return instructions
	}
	return instructions + "\n\n" + sessionSkillInstructions
}

func ensureSessionArtifactPath(workdir, target string) error {
	workdir = strings.TrimSpace(workdir)
	target = strings.TrimSpace(target)
	if workdir == "" || target == "" {
		return errors.New("session path check requires workdir and target")
	}
	realWorkdir, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		return fmt.Errorf("resolve workdir path: %w", err)
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return fmt.Errorf("resolve session path %q: %w", target, err)
	}
	if err := ensurePathWithin(realWorkdir, realTarget); err != nil {
		return fmt.Errorf("session path %q is outside workdir: %w", target, err)
	}
	return nil
}

func ensurePathWithin(root, target string) error {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return fmt.Errorf("compare paths: %w", err)
	}
	if relative == "." || relative == "" {
		return nil
	}
	if strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." || filepath.IsAbs(relative) {
		return fmt.Errorf("%s is not within %s", target, root)
	}
	return nil
}

func appendMaterializedSkillLog(ctx context.Context, runtimeClient *runtimeClient, workerID, taskID string, refs []string) {
	if runtimeClient == nil || len(refs) == 0 {
		return
	}
	_ = runtimeClient.appendTaskLog(ctx, workerID, taskID, appendTaskLogInput{
		Stream:  "system",
		Message: "Materialized server skill bundles: " + strings.Join(refs, ", "),
	})
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
	case "read-only", "readonly":
		return "readOnly"
	case "workspace-write", "workspacewrite":
		return "workspaceWrite"
	case "external-sandbox", "externalsandbox":
		return "externalSandbox"
	default:
		return sandbox
	}
}

func parseIssueTypeTriageResult(value string) (issueTypeTriageResult, error) {
	raw := strings.TrimSpace(value)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return issueTypeTriageResult{}, errors.New("triage response did not contain a JSON object")
	}
	raw = raw[start : end+1]
	var result issueTypeTriageResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return issueTypeTriageResult{}, fmt.Errorf("parse triage JSON: %w", err)
	}
	result.Type = strings.TrimSpace(strings.ToLower(result.Type))
	result.Title = strings.Join(strings.Fields(result.Title), " ")
	result.Reason = strings.Join(strings.Fields(result.Reason), " ")
	if !isAllowedIssueTypeLabel(result.Type) {
		return issueTypeTriageResult{}, fmt.Errorf("triage returned unsupported issue type %q", result.Type)
	}
	if result.Confidence < 0 {
		result.Confidence = 0
	}
	if result.Confidence > 1 {
		result.Confidence = 1
	}
	return result, nil
}

func parseImportMappingResult(value string, payload importMappingPayload) (importMappingResult, error) {
	raw := strings.TrimSpace(value)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return importMappingResult{}, errors.New("import mapping response did not contain a JSON object")
	}
	raw = raw[start : end+1]
	var result importMappingResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return importMappingResult{}, fmt.Errorf("parse import mapping JSON: %w", err)
	}
	result.Format = strings.ToLower(strings.TrimSpace(firstNonEmpty(result.Format, payload.Format)))
	result.FileName = strings.TrimSpace(firstNonEmpty(result.FileName, payload.FileName))
	seenIndexes := map[int]bool{}
	seenFields := map[string]bool{}
	suggestions := make([]importMappingSuggestion, 0, len(result.Suggestions))
	for _, suggestion := range result.Suggestions {
		suggestion.Source = strings.TrimSpace(suggestion.Source)
		suggestion.Field = normalizeImportMappingField(suggestion.Field)
		suggestion.Reason = truncate(strings.Join(strings.Fields(suggestion.Reason), " "), 240)
		if suggestion.Field == "" {
			continue
		}
		if suggestion.Index < 0 {
			suggestion.Index = importMappingHeaderIndex(payload.Headers, suggestion.Source)
		}
		if suggestion.Index < 0 || suggestion.Index >= len(payload.Headers) || seenIndexes[suggestion.Index] || seenFields[suggestion.Field] {
			continue
		}
		if suggestion.Source == "" {
			suggestion.Source = strings.TrimSpace(payload.Headers[suggestion.Index])
		}
		if suggestion.Confidence < 0 {
			suggestion.Confidence = 0
		}
		if suggestion.Confidence > 1 {
			suggestion.Confidence = 1
		}
		if suggestion.Confidence == 0 {
			suggestion.Confidence = 0.5
		}
		seenIndexes[suggestion.Index] = true
		seenFields[suggestion.Field] = true
		suggestions = append(suggestions, suggestion)
	}
	result.Suggestions = suggestions
	result.Warnings = normalizeStringList(result.Warnings)
	return result, nil
}

func importMappingHeaderIndex(headers []string, source string) int {
	sourceKey := normalizeImportMappingKey(source)
	if sourceKey == "" {
		return -1
	}
	for index, header := range headers {
		if normalizeImportMappingKey(header) == sourceKey {
			return index
		}
	}
	return -1
}

func normalizeImportMappingField(value string) string {
	field := strings.TrimSpace(value)
	switch field {
	case "expectedResult":
		field = "expected_result"
	case "environmentRequirements":
		field = "environment_requirements"
	case "externalId":
		field = "external_id"
	case "latestResult":
		field = "latest_result"
	default:
		field = strings.ToLower(field)
	}
	if !isAllowedImportMappingField(field) {
		return ""
	}
	return field
}

func isAllowedImportMappingField(field string) bool {
	switch strings.TrimSpace(field) {
	case "title", "type", "area", "priority", "preconditions", "steps", "expected_result", "environment_requirements", "tags", "external_id", "latest_result":
		return true
	default:
		return false
	}
}

func normalizeImportMappingKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "\ufeff")
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, "（", "(")
	value = strings.ReplaceAll(value, "）", ")")
	return value
}

func defaultImportMappingDeveloperInstructions() string {
	return strings.TrimSpace(`
You are helping mspace preview a test-case import. You only produce column mapping suggestions.
Return exactly one JSON object and no markdown fences or prose.
Allowed fields are: title, type, area, priority, preconditions, steps, expected_result, environment_requirements, tags, external_id, latest_result.
Never claim data was imported or modified. The user must confirm mappings before the server writes anything.
`)
}

func isAllowedIssueTypeLabel(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "feat", "fix", "docs", "style", "refactor", "perf", "test", "build", "ci", "chore", "revert":
		return true
	default:
		return false
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
- If ${MSPACE_SESSION_CONTEXT} is set, read that file before acting; it contains issue, project, runbook, and worker repository context.
- If ${MSPACE_PROJECT_SUBDIR} is set, treat that path inside the workdir as the project focus unless the task explicitly says otherwise.
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

func defaultIssueTypeTriageDeveloperInstructions() string {
	return strings.TrimSpace(`
You are an mspace issue triage assistant.

Write a concise issue title and classify the issue into exactly one Conventional Commit type.
Allowed types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert.

Rules:
- Return only one compact JSON object.
- Do not wrap the JSON in Markdown.
- Write the title in the same language as the issue note when clear.
- Keep the title specific, plain text, and under 72 characters.
- Treat the existing title as a temporary draft. Always rewrite it from the full body instead of copying it verbatim.
- Do not include Markdown, URLs, labels, quotes, or trailing punctuation in the title.
- Do not assign priority.
- Do not change issue status.
- Do not edit files or run commands.
- If the issue is ambiguous, choose chore with lower confidence.
`)
}

func repositoryCacheKey(repository repositorySpec) string {
	if isLocalRepository(repository) {
		return safePathPart(repository.URL)
	}
	parts := []string{repository.Provider, repository.Owner, repository.Repo}
	joined := strings.Trim(strings.Join(parts, "-"), "-")
	if joined == "" {
		joined = repository.URL
	}
	return safePathPart(joined)
}

func isLocalRepository(repository repositorySpec) bool {
	return strings.EqualFold(strings.TrimSpace(repository.SourceType), "local") || strings.EqualFold(strings.TrimSpace(repository.Provider), "local")
}

func normalizeRepositorySubdir(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = filepath.Clean(value)
	if value == "." {
		return ""
	}
	return strings.Trim(filepath.ToSlash(value), "/")
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

func readRuntimeToken(fallbackToken string, tokenFile string) (string, error) {
	if strings.TrimSpace(tokenFile) == "" {
		token := strings.TrimSpace(fallbackToken)
		if !strings.HasPrefix(token, "msw_") {
			return "", errors.New("runtime token with msw_ prefix is required")
		}
		return token, nil
	}
	content, err := os.ReadFile(tokenFile)
	if err != nil {
		return "", fmt.Errorf("read runtime token file: %w", err)
	}
	token := strings.TrimSpace(string(content))
	if !strings.HasPrefix(token, "msw_") {
		return "", errors.New("runtime token file must contain a token with msw_ prefix")
	}
	return token, nil
}

func (c *runtimeClient) runtimeToken() (string, error) {
	return readRuntimeToken(c.token, c.tokenFile)
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
	token, err := c.runtimeToken()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
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
	token, err := c.runtimeToken()
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
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
