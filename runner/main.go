package main

import (
	"bufio"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var (
	errProjectNotFound      = errors.New("project not found")
	errClusterNotFound      = errors.New("cluster not found")
	errUnknownAgentProfile  = errors.New("unknown agent profile")
	errActiveIssueSession   = errors.New("issue already has an active session")
	errSessionActive        = errors.New("session is still active")
	errUnsafeSessionWorkdir = errors.New("session workdir is outside the mspace workdir root")
)

const defaultImportedClusterImageRegistryPrefix = "crpi-7jr40k6elhldekqp.cn-hangzhou.personal.cr.aliyuncs.com/mlhiter"

var checklistItemPattern = regexp.MustCompile(`^\s*(?:[-*+]|\d+\.)\s+\[([ xX])\]\s+(.+?)\s*$`)

type cluster struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	KubeconfigPath      string `json:"kubeconfigPath"`
	KubeContext         string `json:"kubeContext"`
	ImageRegistryPrefix string `json:"imageRegistryPrefix"`
	ExposureMode        string `json:"exposureMode"`
	NodeHost            string `json:"nodeHost"`
	PreviewDomain       string `json:"previewDomain"`
	IngressClass        string `json:"ingressClass"`
	Status              string `json:"status"`
	LastCheckedAt       string `json:"lastCheckedAt"`
	ProjectCount        int    `json:"projectCount"`
	EnvironmentCount    int    `json:"environmentCount"`
	CreatedAt           string `json:"createdAt"`
	UpdatedAt           string `json:"updatedAt"`
}

type project struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	RepoPath             string `json:"repoPath"`
	SourceType           string `json:"sourceType"`
	RemoteURL            string `json:"remoteUrl"`
	GitProvider          string `json:"gitProvider"`
	GitOwner             string `json:"gitOwner"`
	GitRepo              string `json:"gitRepo"`
	DefaultBranch        string `json:"defaultBranch"`
	DeployCommand        string `json:"deployCommand"`
	ValidationCommand    string `json:"validationCommand"`
	KubeContext          string `json:"kubeContext"`
	KubeconfigPath       string `json:"kubeconfigPath"`
	Namespace            string `json:"namespace"`
	ImageRegistryPrefix  string `json:"imageRegistryPrefix"`
	PreviewDomain        string `json:"previewDomain"`
	IngressClass         string `json:"ingressClass"`
	NodeHost             string `json:"nodeHost"`
	DefaultClusterID     string `json:"defaultClusterId"`
	IssueCount           int    `json:"issueCount"`
	SessionCount         int    `json:"sessionCount"`
	LatestIssueUpdatedAt string `json:"latestIssueUpdatedAt"`
	CreatedAt            string `json:"createdAt"`
	UpdatedAt            string `json:"updatedAt"`
}

type issue struct {
	ID             string `json:"id"`
	ProjectID      string `json:"projectId"`
	ParentIssueID  string `json:"parentIssueId"`
	SortOrder      int    `json:"sortOrder"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	Status         string `json:"status"`
	TriageStatus   string `json:"triageStatus"`
	Assignee       string `json:"assignee"`
	AssigneeType   string `json:"assigneeType"`
	EnvironmentURL string `json:"environmentUrl"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

type inboxItem struct {
	ID           string `json:"id"`
	IssueID      string `json:"issueId"`
	ProjectID    string `json:"projectId"`
	ProjectName  string `json:"projectName"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	Assignee     string `json:"assignee"`
	AssigneeType string `json:"assigneeType"`
	Unread       bool   `json:"unread"`
	UpdatedAt    string `json:"updatedAt"`
}

type issueListItem struct {
	ID                       string       `json:"id"`
	ProjectID                string       `json:"projectId"`
	ProjectName              string       `json:"projectName"`
	ParentIssueID            string       `json:"parentIssueId"`
	SortOrder                int          `json:"sortOrder"`
	Title                    string       `json:"title"`
	Body                     string       `json:"body"`
	Status                   string       `json:"status"`
	TriageStatus             string       `json:"triageStatus"`
	Assignee                 string       `json:"assignee"`
	AssigneeType             string       `json:"assigneeType"`
	Labels                   []issueLabel `json:"labels"`
	Unread                   bool         `json:"unread"`
	SessionCount             int          `json:"sessionCount"`
	ChildIssueCount          int          `json:"childIssueCount"`
	CompletedChildIssueCount int          `json:"completedChildIssueCount"`
	UpdatedAt                string       `json:"updatedAt"`
	CreatedAt                string       `json:"createdAt"`
}

type comment struct {
	ID         string `json:"id"`
	IssueID    string `json:"issueId"`
	AuthorType string `json:"authorType"`
	Body       string `json:"body"`
	CreatedAt  string `json:"createdAt"`
}

type issueLabel struct {
	ID        string `json:"id"`
	IssueID   string `json:"issueId"`
	LabelID   string `json:"labelId"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	Dimension string `json:"dimension"`
	Color     string `json:"color"`
	SortOrder int    `json:"sortOrder"`
	CreatedAt string `json:"createdAt"`
}

type issueLabelDefinition struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	Dimension string `json:"dimension"`
	Color     string `json:"color"`
	SortOrder int    `json:"sortOrder"`
	BuiltIn   bool   `json:"builtIn"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type agentProfile struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Mention      string `json:"mention"`
	Provider     string `json:"provider"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	Enabled      bool   `json:"enabled"`
	BuiltIn      bool   `json:"builtIn"`
	SortOrder    int    `json:"sortOrder"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

type agentSession struct {
	ID            string `json:"id"`
	IssueID       string `json:"issueId"`
	Provider      string `json:"provider"`
	AgentProfile  string `json:"agentProfile"`
	RuntimeMode   string `json:"runtimeMode"`
	Command       string `json:"command"`
	Status        string `json:"status"`
	Branch        string `json:"branch"`
	Workdir       string `json:"workdir"`
	CodexThreadID string `json:"codexThreadId"`
	CodexTurnID   string `json:"codexTurnId"`
	AgentStatus   string `json:"agentStatus"`
	ArtifactDir   string `json:"artifactDir"`
	CleanupStatus string `json:"cleanupStatus"`
	CleanedAt     string `json:"cleanedAt"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

type sessionLog struct {
	ID        int64  `json:"id"`
	SessionID string `json:"sessionId"`
	Stream    string `json:"stream"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
}

type deploymentEvidence struct {
	ID        string `json:"id"`
	IssueID   string `json:"issueId"`
	SessionID string `json:"sessionId"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Summary   string `json:"summary"`
	Details   string `json:"details"`
	CreatedAt string `json:"createdAt"`
}

type issueTestEnvironment struct {
	IssueID              string `json:"issueId"`
	ClusterID            string `json:"clusterId"`
	Namespace            string `json:"namespace"`
	NamespaceStatus      string `json:"namespaceStatus"`
	CleanupStatus        string `json:"cleanupStatus"`
	PreviewURL           string `json:"previewUrl"`
	ImageRegistryPrefix  string `json:"imageRegistryPrefix"`
	KubeconfigPath       string `json:"kubeconfigPath"`
	KubeContext          string `json:"kubeContext"`
	ExposureMode         string `json:"exposureMode"`
	PreviewDomain        string `json:"previewDomain"`
	IngressClass         string `json:"ingressClass"`
	NodeHost             string `json:"nodeHost"`
	LastDeploySessionID  string `json:"lastDeploySessionId"`
	LastCleanupSessionID string `json:"lastCleanupSessionId"`
	CreatedAt            string `json:"createdAt"`
	UpdatedAt            string `json:"updatedAt"`
}

type issueDetail struct {
	Issue           issue                 `json:"issue"`
	Project         project               `json:"project"`
	TestEnvironment *issueTestEnvironment `json:"testEnvironment"`
	ChildIssues     []issueListItem       `json:"childIssues"`
	Labels          []issueLabel          `json:"labels"`
	Comments        []comment             `json:"comments"`
	Sessions        []agentSession        `json:"sessions"`
	Evidence        []deploymentEvidence  `json:"evidence"`
}

type sessionDetail struct {
	Session   agentSession         `json:"session"`
	Issue     issue                `json:"issue"`
	Project   project              `json:"project"`
	Logs      []sessionLog         `json:"logs"`
	Evidence  []deploymentEvidence `json:"evidence"`
	Workspace workspaceSnapshot    `json:"workspace"`
}

type activeWorkItem struct {
	IssueID         string `json:"issueId"`
	ProjectID       string `json:"projectId"`
	ProjectName     string `json:"projectName"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	Namespace       string `json:"namespace"`
	NamespaceStatus string `json:"namespaceStatus"`
	CleanupStatus   string `json:"cleanupStatus"`
	SessionStatus   string `json:"sessionStatus"`
	UpdatedAt       string `json:"updatedAt"`
}

type workspaceSnapshot struct {
	Exists          bool                `json:"exists"`
	IsGitRepository bool                `json:"isGitRepository"`
	HasChanges      bool                `json:"hasChanges"`
	ChangedFiles    int                 `json:"changedFiles"`
	UntrackedFiles  int                 `json:"untrackedFiles"`
	Head            string              `json:"head"`
	ShortHead       string              `json:"shortHead"`
	Branch          string              `json:"branch"`
	StatusLines     []string            `json:"statusLines"`
	Changes         []workspaceChange   `json:"changes"`
	DiffPreview     string              `json:"diffPreview"`
	DiffTruncated   bool                `json:"diffTruncated"`
	Comparison      workspaceComparison `json:"comparison"`
	Error           string              `json:"error"`
}

type workspaceChange struct {
	StatusCode   string `json:"statusCode"`
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath"`
}

type workspaceComparison struct {
	BaseRef        string            `json:"baseRef"`
	MergeBase      string            `json:"mergeBase"`
	MergeBaseShort string            `json:"mergeBaseShort"`
	AheadCount     int               `json:"aheadCount"`
	BehindCount    int               `json:"behindCount"`
	CommitLines    []string          `json:"commitLines"`
	Changes        []workspaceChange `json:"changes"`
	DiffPreview    string            `json:"diffPreview"`
	DiffTruncated  bool              `json:"diffTruncated"`
	Error          string            `json:"error"`
}

type eventBroker struct {
	mu          sync.Mutex
	subscribers map[string]map[chan []byte]struct{}
}

func newEventBroker() *eventBroker {
	return &eventBroker{
		subscribers: map[string]map[chan []byte]struct{}{},
	}
}

func (b *eventBroker) subscribe(sessionID string) (chan []byte, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan []byte, 32)
	if _, ok := b.subscribers[sessionID]; !ok {
		b.subscribers[sessionID] = map[chan []byte]struct{}{}
	}
	b.subscribers[sessionID][ch] = struct{}{}
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subscribers[sessionID], ch)
		close(ch)
		if len(b.subscribers[sessionID]) == 0 {
			delete(b.subscribers, sessionID)
		}
	}
}

func (b *eventBroker) publish(sessionID string, payload any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	for ch := range b.subscribers[sessionID] {
		select {
		case ch <- data:
		default:
		}
	}
}

type app struct {
	db         *sql.DB
	logger     *slog.Logger
	workdir    string
	repoRoot   string
	broker     *eventBroker
	mu         sync.Mutex
	cancellers map[string]context.CancelFunc
}

type projectInput struct {
	Name                string `json:"name"`
	SourceType          string `json:"sourceType"`
	RepoPath            string `json:"repoPath"`
	RepoURL             string `json:"repoUrl"`
	DefaultBranch       string `json:"defaultBranch"`
	DeployCommand       string `json:"deployCommand"`
	ValidationCommand   string `json:"validationCommand"`
	KubeContext         string `json:"kubeContext"`
	KubeconfigPath      string `json:"kubeconfigPath"`
	Namespace           string `json:"namespace"`
	ImageRegistryPrefix string `json:"imageRegistryPrefix"`
	PreviewDomain       string `json:"previewDomain"`
	IngressClass        string `json:"ingressClass"`
	NodeHost            string `json:"nodeHost"`
	DefaultClusterID    string `json:"defaultClusterId"`
}

type clusterInput struct {
	Name                string `json:"name"`
	KubeconfigPath      string `json:"kubeconfigPath"`
	KubeContext         string `json:"kubeContext"`
	ImageRegistryPrefix string `json:"imageRegistryPrefix"`
	ExposureMode        string `json:"exposureMode"`
	NodeHost            string `json:"nodeHost"`
	PreviewDomain       string `json:"previewDomain"`
	IngressClass        string `json:"ingressClass"`
	Status              string `json:"status"`
}

type kubeconfigImportRequest struct {
	Path  string   `json:"path"`
	Paths []string `json:"paths"`
}

type kubeconfigImportResult struct {
	Imported []cluster              `json:"imported"`
	Skipped  []kubeconfigImportSkip `json:"skipped"`
}

type kubeconfigDiscoveryResult struct {
	Candidates []kubeconfigCandidate  `json:"candidates"`
	Skipped    []kubeconfigImportSkip `json:"skipped"`
}

type kubeconfigCandidate struct {
	Path     string   `json:"path"`
	Contexts []string `json:"contexts"`
}

type kubeconfigImportSkip struct {
	Path    string `json:"path"`
	Context string `json:"context"`
	Reason  string `json:"reason"`
}

type sessionRequest struct {
	Provider     string `json:"provider"`
	AgentProfile string `json:"agentProfile"`
	Command      string `json:"command"`
	Branch       string `json:"branch"`
}

type issueTaskInput struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	Status    string `json:"status"`
	Completed bool   `json:"completed"`
}

type issueTaskDraft struct {
	Title  string
	Body   string
	Status string
}

type testDeployRequest struct {
	AgentProfile  string `json:"agentProfile"`
	ClusterID     string `json:"clusterId"`
	ExposureMode  string `json:"exposureMode"`
	PreviewDomain string `json:"previewDomain"`
	IngressClass  string `json:"ingressClass"`
	NodeHost      string `json:"nodeHost"`
}

type agentProfileInput struct {
	Name         string `json:"name"`
	Mention      string `json:"mention"`
	Provider     string `json:"provider"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	Enabled      *bool  `json:"enabled"`
}

type gitRemoteInfo struct {
	Provider string
	Owner    string
	Repo     string
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	port := os.Getenv("MSPACE_PORT")
	if port == "" {
		port = "7788"
	}

	rootDir := filepath.Join(userHomeDir(), ".mspace")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		logger.Error("failed to create app dir", "error", err)
		os.Exit(1)
	}
	dbPath := filepath.Join(rootDir, "mspace.db")
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", dbPath))
	if err != nil {
		logger.Error("failed to open sqlite", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		logger.Error("failed to enable foreign keys", "error", err)
		os.Exit(1)
	}

	application := &app{
		db:         db,
		logger:     logger,
		workdir:    filepath.Join(rootDir, "workdirs"),
		repoRoot:   filepath.Join(rootDir, "repos"),
		broker:     newEventBroker(),
		cancellers: map[string]context.CancelFunc{},
	}

	if err := os.MkdirAll(application.workdir, 0o755); err != nil {
		logger.Error("failed to create workdir root", "error", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(application.repoRoot, 0o755); err != nil {
		logger.Error("failed to create repo root", "error", err)
		os.Exit(1)
	}

	if err := application.migrate(); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	router := chi.NewRouter()
	router.Use(jsonMiddleware)
	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "version": "0.1.0"})
	})
	router.Get("/api/inbox", application.handleListInbox)
	router.Get("/api/inbox/stream", application.handleInboxStream)
	router.Get("/api/active-work", application.handleListActiveWork)
	router.Get("/api/agents", application.handleListAgentProfiles)
	router.Post("/api/agents", application.handleCreateAgentProfile)
	router.Put("/api/agents/{agentID}", application.handleUpdateAgentProfile)
	router.Get("/api/clusters", application.handleListClusters)
	router.Post("/api/clusters", application.handleCreateCluster)
	router.Get("/api/clusters/discover-defaults", application.handleDiscoverDefaultClusters)
	router.Post("/api/clusters/import", application.handleImportClusters)
	router.Post("/api/clusters/import-defaults", application.handleImportDefaultClusters)
	router.Put("/api/clusters/{clusterID}", application.handleUpdateCluster)
	router.Delete("/api/clusters/{clusterID}", application.handleDeleteCluster)
	router.Get("/api/projects", application.handleListProjects)
	router.Post("/api/projects", application.handleCreateProject)
	router.Put("/api/projects/{projectID}", application.handleUpdateProject)
	router.Delete("/api/projects/{projectID}", application.handleDeleteProject)
	router.Get("/api/issue-label-definitions", application.handleListIssueLabelDefinitions)
	router.Get("/api/issues", application.handleListIssues)
	router.Post("/api/issues", application.handleCreateIssue)
	router.Get("/api/issues/{issueID}", application.handleGetIssue)
	router.Put("/api/issues/{issueID}", application.handleUpdateIssue)
	router.Post("/api/issues/{issueID}/tasks", application.handleCreateIssueTask)
	router.Delete("/api/issues/{issueID}/tasks/{taskID}", application.handleDeleteIssueTask)
	router.Put("/api/issues/{issueID}/labels", application.handleUpdateIssueLabels)
	router.Post("/api/issues/{issueID}/comments", application.handleCreateComment)
	router.Post("/api/issues/{issueID}/assign-agent", application.handleAssignIssueToAgent)
	router.Post("/api/issues/{issueID}/sessions", application.handleCreateSession)
	router.Post("/api/issues/{issueID}/test-deploy", application.handleStartIssueTestDeploy)
	router.Post("/api/issues/{issueID}/test-environment/cleanup", application.handleRequestIssueTestEnvironmentCleanup)
	router.Post("/api/issues/{issueID}/test-environment/retain", application.handleRetainIssueTestEnvironment)
	router.Get("/api/sessions/{sessionID}", application.handleGetSession)
	router.Post("/api/sessions/{sessionID}/cancel", application.handleCancelSession)
	router.Post("/api/sessions/{sessionID}/cleanup", application.handleCleanupSessionWorktree)
	router.Get("/api/sessions/{sessionID}/stream", application.handleSessionStream)

	logger.Info("starting mspace local runner", "port", port, "db", dbPath)
	if err := http.ListenAndServe("127.0.0.1:"+port, router); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func (a *app) migrate() error {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		sqlBytes, err := migrationFS.ReadFile(filepath.Join("migrations", entry.Name()))
		if err != nil {
			return err
		}
		if _, err := a.db.Exec(string(sqlBytes)); err != nil {
			return fmt.Errorf("apply %s: %w", entry.Name(), err)
		}
	}
	if err := a.ensureClusterTables(); err != nil {
		return err
	}
	if err := a.ensureProjectColumns(); err != nil {
		return err
	}
	if err := a.ensureIssueColumns(); err != nil {
		return err
	}
	if err := a.ensureIssueLabelTables(); err != nil {
		return err
	}
	if err := a.ensureIssueTestEnvironmentTables(); err != nil {
		return err
	}
	if err := a.ensureAgentProfileTables(); err != nil {
		return err
	}
	if err := a.ensureSessionColumns(); err != nil {
		return err
	}
	return nil
}

func (a *app) ensureClusterTables() error {
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS clusters (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			kubeconfig_path TEXT NOT NULL,
			kube_context TEXT NOT NULL DEFAULT '',
			image_registry_prefix TEXT NOT NULL DEFAULT '',
			exposure_mode TEXT NOT NULL DEFAULT 'nodeport',
			node_host TEXT NOT NULL DEFAULT '',
			preview_domain TEXT NOT NULL DEFAULT '',
			ingress_class TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'configured',
			last_checked_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create clusters: %w", err)
	}
	if err := a.ensureTableColumns("clusters", map[string]string{
		"kube_context":          "TEXT NOT NULL DEFAULT ''",
		"image_registry_prefix": "TEXT NOT NULL DEFAULT ''",
		"exposure_mode":         "TEXT NOT NULL DEFAULT 'nodeport'",
		"node_host":             "TEXT NOT NULL DEFAULT ''",
		"preview_domain":        "TEXT NOT NULL DEFAULT ''",
		"ingress_class":         "TEXT NOT NULL DEFAULT ''",
		"status":                "TEXT NOT NULL DEFAULT 'configured'",
		"last_checked_at":       "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_clusters_updated_at ON clusters(updated_at)
	`); err != nil {
		return fmt.Errorf("create clusters updated index: %w", err)
	}
	return nil
}

func (a *app) ensureProjectColumns() error {
	rows, err := a.db.Query(`PRAGMA table_info(projects)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	requiredColumns := map[string]string{
		"source_type":           "TEXT NOT NULL DEFAULT 'local'",
		"remote_url":            "TEXT NOT NULL DEFAULT ''",
		"git_provider":          "TEXT NOT NULL DEFAULT ''",
		"git_owner":             "TEXT NOT NULL DEFAULT ''",
		"git_repo":              "TEXT NOT NULL DEFAULT ''",
		"kubeconfig_path":       "TEXT NOT NULL DEFAULT ''",
		"image_registry_prefix": "TEXT NOT NULL DEFAULT ''",
		"preview_domain":        "TEXT NOT NULL DEFAULT ''",
		"ingress_class":         "TEXT NOT NULL DEFAULT ''",
		"node_host":             "TEXT NOT NULL DEFAULT ''",
		"default_cluster_id":    "TEXT NOT NULL DEFAULT ''",
	}
	for name, definition := range requiredColumns {
		if existing[name] {
			continue
		}
		if _, err := a.db.Exec(fmt.Sprintf("ALTER TABLE projects ADD COLUMN %s %s", name, definition)); err != nil {
			return fmt.Errorf("add projects.%s: %w", name, err)
		}
	}
	return nil
}

func (a *app) ensureIssueColumns() error {
	rows, err := a.db.Query(`PRAGMA table_info(issues)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	requiredColumns := map[string]string{
		"parent_issue_id": "TEXT REFERENCES issues(id) ON DELETE CASCADE",
		"sort_order":      "INTEGER NOT NULL DEFAULT 0",
		"assignee_type":   "TEXT NOT NULL DEFAULT 'human'",
		"triage_status":   "TEXT NOT NULL DEFAULT 'none'",
	}
	for name, definition := range requiredColumns {
		if existing[name] {
			continue
		}
		if _, err := a.db.Exec(fmt.Sprintf("ALTER TABLE issues ADD COLUMN %s %s", name, definition)); err != nil {
			return fmt.Errorf("add issues.%s: %w", name, err)
		}
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issues_parent_issue_id ON issues(parent_issue_id)
	`); err != nil {
		return fmt.Errorf("create issues parent index: %w", err)
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issues_project_parent_updated ON issues(project_id, parent_issue_id, updated_at)
	`); err != nil {
		return fmt.Errorf("create issues project parent updated index: %w", err)
	}
	return nil
}

func (a *app) ensureIssueLabelTables() error {
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS issue_label_definitions (
			id TEXT PRIMARY KEY,
			key TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			dimension TEXT NOT NULL,
			color TEXT NOT NULL DEFAULT '',
			sort_order INTEGER NOT NULL DEFAULT 0,
			built_in INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create issue_label_definitions: %w", err)
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issue_label_definitions_dimension ON issue_label_definitions(dimension, sort_order)
	`); err != nil {
		return fmt.Errorf("create issue_label_definitions dimension index: %w", err)
	}
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS issue_labels (
			id TEXT PRIMARY KEY,
			issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			label_id TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL,
			color TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			UNIQUE(issue_id, name),
			UNIQUE(issue_id, label_id)
		)
	`); err != nil {
		return fmt.Errorf("create issue_labels: %w", err)
	}
	if err := a.ensureTableColumns("issue_labels", map[string]string{
		"label_id": "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issue_labels_issue_id ON issue_labels(issue_id)
	`); err != nil {
		return fmt.Errorf("create issue_labels issue index: %w", err)
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issue_labels_label_id ON issue_labels(label_id)
	`); err != nil {
		return fmt.Errorf("create issue_labels label index: %w", err)
	}
	if err := a.seedIssueLabelDefinitions(); err != nil {
		return err
	}
	return a.backfillIssueLabelIDs()
}

func (a *app) ensureIssueTestEnvironmentTables() error {
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS issue_test_environments (
			issue_id TEXT PRIMARY KEY REFERENCES issues(id) ON DELETE CASCADE,
			namespace TEXT NOT NULL,
			namespace_status TEXT NOT NULL DEFAULT 'planned',
			cleanup_status TEXT NOT NULL DEFAULT 'retained',
			preview_url TEXT NOT NULL DEFAULT '',
			cluster_id TEXT NOT NULL DEFAULT '',
			image_registry_prefix TEXT NOT NULL DEFAULT '',
			kubeconfig_path TEXT NOT NULL DEFAULT '',
			kube_context TEXT NOT NULL DEFAULT '',
			exposure_mode TEXT NOT NULL DEFAULT 'nodeport',
			preview_domain TEXT NOT NULL DEFAULT '',
			ingress_class TEXT NOT NULL DEFAULT '',
			node_host TEXT NOT NULL DEFAULT '',
			last_deploy_session_id TEXT NOT NULL DEFAULT '',
			last_cleanup_session_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create issue_test_environments: %w", err)
	}
	if err := a.ensureTableColumns("issue_test_environments", map[string]string{
		"namespace_status":        "TEXT NOT NULL DEFAULT 'planned'",
		"cleanup_status":          "TEXT NOT NULL DEFAULT 'retained'",
		"preview_url":             "TEXT NOT NULL DEFAULT ''",
		"cluster_id":              "TEXT NOT NULL DEFAULT ''",
		"image_registry_prefix":   "TEXT NOT NULL DEFAULT ''",
		"kubeconfig_path":         "TEXT NOT NULL DEFAULT ''",
		"kube_context":            "TEXT NOT NULL DEFAULT ''",
		"exposure_mode":           "TEXT NOT NULL DEFAULT 'nodeport'",
		"preview_domain":          "TEXT NOT NULL DEFAULT ''",
		"ingress_class":           "TEXT NOT NULL DEFAULT ''",
		"node_host":               "TEXT NOT NULL DEFAULT ''",
		"last_deploy_session_id":  "TEXT NOT NULL DEFAULT ''",
		"last_cleanup_session_id": "TEXT NOT NULL DEFAULT ''",
	}); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issue_test_environments_namespace ON issue_test_environments(namespace)
	`); err != nil {
		return fmt.Errorf("create issue_test_environments namespace index: %w", err)
	}
	return nil
}

func (a *app) ensureAgentProfileTables() error {
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS agent_profiles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			mention TEXT NOT NULL UNIQUE,
			provider TEXT NOT NULL DEFAULT 'codex',
			description TEXT NOT NULL DEFAULT '',
			instructions TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			built_in INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create agent_profiles: %w", err)
	}
	if err := a.ensureTableColumns("agent_profiles", map[string]string{
		"sort_order": "INTEGER NOT NULL DEFAULT 0",
	}); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_agent_profiles_enabled ON agent_profiles(enabled)
	`); err != nil {
		return fmt.Errorf("create agent_profiles enabled index: %w", err)
	}
	return a.seedDefaultAgentProfiles()
}

func (a *app) ensureTableColumns(table string, requiredColumns map[string]string) error {
	rows, err := a.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()

	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for name, definition := range requiredColumns {
		if existing[name] {
			continue
		}
		if _, err := a.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, name, definition)); err != nil {
			return fmt.Errorf("add %s.%s: %w", table, name, err)
		}
	}
	return nil
}

func (a *app) ensureSessionColumns() error {
	rows, err := a.db.Query(`PRAGMA table_info(agent_sessions)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	requiredColumns := map[string]string{
		"codex_thread_id": "TEXT NOT NULL DEFAULT ''",
		"codex_turn_id":   "TEXT NOT NULL DEFAULT ''",
		"agent_status":    "TEXT NOT NULL DEFAULT ''",
		"artifact_dir":    "TEXT NOT NULL DEFAULT ''",
		"agent_profile":   "TEXT NOT NULL DEFAULT ''",
		"cleanup_status":  "TEXT NOT NULL DEFAULT 'retained'",
		"cleaned_at":      "TEXT NOT NULL DEFAULT ''",
	}
	for name, definition := range requiredColumns {
		if existing[name] {
			continue
		}
		if _, err := a.db.Exec(fmt.Sprintf("ALTER TABLE agent_sessions ADD COLUMN %s %s", name, definition)); err != nil {
			return fmt.Errorf("add agent_sessions.%s: %w", name, err)
		}
	}
	return nil
}

func (a *app) handleListInbox(w http.ResponseWriter, _ *http.Request) {
	items := make([]inboxItem, 0)
	rows, err := a.db.Query(`
		SELECT ii.id, ii.issue_id, ii.project_id, p.name, ii.title, ii.status, i.assignee, i.assignee_type, ii.unread, ii.updated_at
		FROM inbox_items ii
		JOIN issues i ON i.id = ii.issue_id
		JOIN projects p ON p.id = ii.project_id
		WHERE ii.unread = 1
		ORDER BY ii.updated_at DESC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var item inboxItem
		var unread int
		if err := rows.Scan(&item.ID, &item.IssueID, &item.ProjectID, &item.ProjectName, &item.Title, &item.Status, &item.Assignee, &item.AssigneeType, &unread, &item.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		item.Unread = unread == 1
		items = append(items, item)
	}
	writeJSON(w, items)
}

func (a *app) handleListActiveWork(w http.ResponseWriter, _ *http.Request) {
	items := make([]activeWorkItem, 0)
	rows, err := a.db.Query(`
		SELECT
			i.id,
			i.project_id,
			p.name,
			i.title,
			i.status,
			COALESCE(e.namespace, '') AS namespace,
			COALESCE(e.namespace_status, '') AS namespace_status,
			COALESCE(e.cleanup_status, '') AS cleanup_status,
			COALESCE(s.status, '') AS session_status,
			CASE
				WHEN s.updated_at IS NOT NULL AND s.updated_at > i.updated_at THEN s.updated_at
				ELSE i.updated_at
			END AS work_updated_at
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		LEFT JOIN issue_test_environments e ON e.issue_id = i.id
		LEFT JOIN agent_sessions s ON s.id = (
			SELECT id
			FROM agent_sessions latest
			WHERE latest.issue_id = i.id
			ORDER BY latest.updated_at DESC
			LIMIT 1
		)
		WHERE COALESCE(i.parent_issue_id, '') = ''
		ORDER BY
			CASE
				WHEN s.status IN ('queued', 'running') THEN 0
				WHEN e.namespace_status IN ('deploying', 'cleanup_requested') THEN 1
				WHEN e.cleanup_status = 'retained' AND e.namespace != '' THEN 2
				ELSE 3
			END,
			work_updated_at DESC
		LIMIT 6
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var item activeWorkItem
		if err := rows.Scan(&item.IssueID, &item.ProjectID, &item.ProjectName, &item.Title, &item.Status, &item.Namespace, &item.NamespaceStatus, &item.CleanupStatus, &item.SessionStatus, &item.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		items = append(items, item)
	}
	writeJSON(w, items)
}

func (a *app) handleListIssues(w http.ResponseWriter, _ *http.Request) {
	items := make([]issueListItem, 0)
	rows, err := a.db.Query(`
		SELECT
			i.id,
			i.project_id,
			p.name,
			COALESCE(i.parent_issue_id, '') AS parent_issue_id,
			i.sort_order,
			i.title,
			i.body,
			i.status,
			i.triage_status,
			i.assignee,
			i.assignee_type,
			COALESCE(ii.unread, 0) AS unread,
			COUNT(DISTINCT s.id) AS session_count,
			COUNT(DISTINCT child.id) AS child_issue_count,
			COUNT(DISTINCT CASE WHEN child.status = 'completed' THEN child.id END) AS completed_child_issue_count,
			i.updated_at,
			i.created_at
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		LEFT JOIN inbox_items ii ON ii.issue_id = i.id
		LEFT JOIN agent_sessions s ON s.issue_id = i.id
		LEFT JOIN issues child ON child.parent_issue_id = i.id
		WHERE COALESCE(i.parent_issue_id, '') = ''
		GROUP BY i.id
		ORDER BY i.updated_at DESC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var item issueListItem
		var unread int
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.ProjectName, &item.ParentIssueID, &item.SortOrder, &item.Title, &item.Body, &item.Status, &item.TriageStatus, &item.Assignee, &item.AssigneeType, &unread, &item.SessionCount, &item.ChildIssueCount, &item.CompletedChildIssueCount, &item.UpdatedAt, &item.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		item.Unread = unread == 1
		labels, err := a.listIssueLabels(item.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		item.Labels = labels
		items = append(items, item)
	}
	writeJSON(w, items)
}

func (a *app) handleListIssueLabelDefinitions(w http.ResponseWriter, _ *http.Request) {
	definitions, err := a.listIssueLabelDefinitions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, definitions)
}

func (a *app) handleInboxStream(w http.ResponseWriter, r *http.Request) {
	a.streamEvents(w, r, "inbox")
}

func (a *app) handleListAgentProfiles(w http.ResponseWriter, _ *http.Request) {
	profiles, err := a.listAgentProfiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, profiles)
}

func (a *app) handleCreateAgentProfile(w http.ResponseWriter, r *http.Request) {
	var input agentProfileInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	profile, err := normalizeAgentProfileInput(agentProfile{}, input, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	now := nowString()
	profile.CreatedAt = now
	profile.UpdatedAt = now

	_, err = a.db.Exec(`
		INSERT INTO agent_profiles (id, name, mention, provider, description, instructions, enabled, built_in, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, profile.ID, profile.Name, profile.Mention, profile.Provider, profile.Description, profile.Instructions, boolToInt(profile.Enabled), boolToInt(profile.BuiltIn), profile.SortOrder, profile.CreatedAt, profile.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("create agent profile: %w", err))
		return
	}
	writeJSON(w, profile)
}

func (a *app) handleUpdateAgentProfile(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "agentID")
	existing, err := a.loadAgentProfile(agentID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("agent profile not found")
		}
		writeError(w, status, err)
		return
	}

	var input agentProfileInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	updated, err := normalizeAgentProfileInput(existing, input, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	updated.UpdatedAt = nowString()

	_, err = a.db.Exec(`
		UPDATE agent_profiles
		SET name = ?, mention = ?, provider = ?, description = ?, instructions = ?, enabled = ?, sort_order = ?, updated_at = ?
		WHERE id = ?
	`, updated.Name, updated.Mention, updated.Provider, updated.Description, updated.Instructions, boolToInt(updated.Enabled), updated.SortOrder, updated.UpdatedAt, updated.ID)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("update agent profile: %w", err))
		return
	}
	writeJSON(w, updated)
}

func (a *app) handleListClusters(w http.ResponseWriter, _ *http.Request) {
	clusters, err := a.listClusters()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, clusters)
}

func (a *app) handleCreateCluster(w http.ResponseWriter, r *http.Request) {
	var input clusterInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	cluster, err := normalizeClusterInput(cluster{}, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	now := nowString()
	cluster.ID = uuid.NewString()
	cluster.CreatedAt = now
	cluster.UpdatedAt = now

	if err := a.insertCluster(cluster); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, cluster)
}

func (a *app) handleImportClusters(w http.ResponseWriter, r *http.Request) {
	var input kubeconfigImportRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	paths := normalizeKubeconfigImportPaths(input)
	if len(paths) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("at least one kubeconfig path is required"))
		return
	}
	result, err := a.importKubeconfigs(paths)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, result)
}

func (a *app) handleDiscoverDefaultClusters(w http.ResponseWriter, _ *http.Request) {
	paths, err := discoverDefaultKubeconfigPaths(filepath.Join(userHomeDir(), ".kube"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result := discoverKubeconfigs(paths)
	writeJSON(w, result)
}

func (a *app) handleImportDefaultClusters(w http.ResponseWriter, _ *http.Request) {
	paths, err := discoverDefaultKubeconfigPaths(filepath.Join(userHomeDir(), ".kube"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := a.importKubeconfigs(paths)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, result)
}

func (a *app) handleUpdateCluster(w http.ResponseWriter, r *http.Request) {
	clusterID := chi.URLParam(r, "clusterID")
	existing, err := a.loadCluster(clusterID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errClusterNotFound
		}
		writeError(w, status, err)
		return
	}

	var input clusterInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	updated, err := normalizeClusterInput(existing, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	updated.ID = existing.ID
	updated.CreatedAt = existing.CreatedAt
	updated.UpdatedAt = nowString()

	if err := a.updateCluster(updated); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	reloaded, err := a.loadCluster(clusterID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, reloaded)
}

func (a *app) handleDeleteCluster(w http.ResponseWriter, r *http.Request) {
	clusterID := chi.URLParam(r, "clusterID")
	existing, err := a.loadCluster(clusterID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errClusterNotFound
		}
		writeError(w, status, err)
		return
	}
	if existing.ProjectCount > 0 || existing.EnvironmentCount > 0 {
		writeError(w, http.StatusConflict, errors.New("cluster cannot be deleted while projects or test environments reference it"))
		return
	}
	if _, err := a.db.Exec(`DELETE FROM clusters WHERE id = ?`, clusterID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *app) handleListProjects(w http.ResponseWriter, _ *http.Request) {
	projects := make([]project, 0)
	rows, err := a.db.Query(`
		SELECT
			p.id,
			p.name,
			p.repo_path,
			p.source_type,
			p.remote_url,
			p.git_provider,
			p.git_owner,
			p.git_repo,
			p.default_branch,
			p.deploy_command,
			p.validation_command,
			p.kube_context,
			p.kubeconfig_path,
			p.namespace,
			p.image_registry_prefix,
			p.preview_domain,
			p.ingress_class,
			p.node_host,
			p.default_cluster_id,
			COUNT(DISTINCT i.id) AS issue_count,
			COUNT(DISTINCT s.id) AS session_count,
			MAX(i.updated_at) AS latest_issue_updated_at,
			p.created_at,
			p.updated_at
		FROM projects p
		LEFT JOIN issues i ON i.project_id = p.id AND COALESCE(i.parent_issue_id, '') = ''
		LEFT JOIN agent_sessions s ON s.issue_id = i.id
		GROUP BY p.id
		ORDER BY p.updated_at DESC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var p project
		var latestIssueUpdatedAt sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &p.RepoPath, &p.SourceType, &p.RemoteURL, &p.GitProvider, &p.GitOwner, &p.GitRepo, &p.DefaultBranch, &p.DeployCommand, &p.ValidationCommand, &p.KubeContext, &p.KubeconfigPath, &p.Namespace, &p.ImageRegistryPrefix, &p.PreviewDomain, &p.IngressClass, &p.NodeHost, &p.DefaultClusterID, &p.IssueCount, &p.SessionCount, &latestIssueUpdatedAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if latestIssueUpdatedAt.Valid {
			p.LatestIssueUpdatedAt = latestIssueUpdatedAt.String
		}
		projects = append(projects, p)
	}
	writeJSON(w, projects)
}

func (a *app) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var input projectInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	normalizedProject, err := a.normalizeProjectInput(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	now := nowString()
	normalizedProject.ID = uuid.NewString()
	normalizedProject.CreatedAt = now
	normalizedProject.UpdatedAt = now

	_, err = a.db.Exec(`
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, kubeconfig_path, namespace, image_registry_prefix, preview_domain, ingress_class, node_host, default_cluster_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, normalizedProject.ID, normalizedProject.Name, normalizedProject.RepoPath, normalizedProject.SourceType, normalizedProject.RemoteURL, normalizedProject.GitProvider, normalizedProject.GitOwner, normalizedProject.GitRepo, normalizedProject.DefaultBranch, normalizedProject.DeployCommand, normalizedProject.ValidationCommand, normalizedProject.KubeContext, normalizedProject.KubeconfigPath, normalizedProject.Namespace, normalizedProject.ImageRegistryPrefix, normalizedProject.PreviewDomain, normalizedProject.IngressClass, normalizedProject.NodeHost, normalizedProject.DefaultClusterID, normalizedProject.CreatedAt, normalizedProject.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, normalizedProject)
}

func (a *app) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

	var input projectInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	existingProject, err := a.loadProject(projectID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("project not found")
		}
		writeError(w, status, err)
		return
	}

	normalizedProject, err := a.normalizeProjectInput(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	normalizedProject.ID = existingProject.ID
	normalizedProject.CreatedAt = existingProject.CreatedAt
	normalizedProject.UpdatedAt = nowString()

	_, err = a.db.Exec(`
		UPDATE projects
		SET name = ?, repo_path = ?, source_type = ?, remote_url = ?, git_provider = ?, git_owner = ?, git_repo = ?, default_branch = ?, deploy_command = ?, validation_command = ?, kube_context = ?, kubeconfig_path = ?, namespace = ?, image_registry_prefix = ?, preview_domain = ?, ingress_class = ?, node_host = ?, default_cluster_id = ?, updated_at = ?
		WHERE id = ?
	`, normalizedProject.Name, normalizedProject.RepoPath, normalizedProject.SourceType, normalizedProject.RemoteURL, normalizedProject.GitProvider, normalizedProject.GitOwner, normalizedProject.GitRepo, normalizedProject.DefaultBranch, normalizedProject.DeployCommand, normalizedProject.ValidationCommand, normalizedProject.KubeContext, normalizedProject.KubeconfigPath, normalizedProject.Namespace, normalizedProject.ImageRegistryPrefix, normalizedProject.PreviewDomain, normalizedProject.IngressClass, normalizedProject.NodeHost, normalizedProject.DefaultClusterID, normalizedProject.UpdatedAt, normalizedProject.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	updatedProject, err := a.loadProject(projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, updatedProject)
}

func (a *app) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

	existingProject, err := a.loadProject(projectID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("project not found")
		}
		writeError(w, status, err)
		return
	}

	if existingProject.IssueCount > 0 || existingProject.SessionCount > 0 {
		writeError(w, http.StatusConflict, errors.New("project cannot be deleted after issues or sessions have been created"))
		return
	}

	_, err = a.db.Exec(`DELETE FROM projects WHERE id = ?`, projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, map[string]bool{"ok": true})
}

func (a *app) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProjectID    string           `json:"projectId"`
		Title        string           `json:"title"`
		Body         string           `json:"body"`
		Prompt       string           `json:"prompt"`
		Tasks        []string         `json:"tasks"`
		ChildIssues  []issueTaskInput `json:"childIssues"`
		Labels       []string         `json:"labels"`
		LabelKeys    []string         `json:"labelKeys"`
		Assignee     string           `json:"assignee"`
		AssigneeType string           `json:"assigneeType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.Title = strings.TrimSpace(input.Title)
	input.Body = strings.TrimSpace(input.Body)
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.Assignee = strings.TrimSpace(input.Assignee)
	input.AssigneeType = normalizeAssigneeType(input.AssigneeType)
	if input.Body == "" {
		input.Body = input.Prompt
	}
	parentBody, taskDrafts := extractIssueTaskDrafts(input.Body)
	taskDrafts = append(taskDrafts, normalizeIssueTaskStrings(input.Tasks)...)
	taskDrafts = append(taskDrafts, normalizeIssueTaskInputs(input.ChildIssues)...)
	if input.Title == "" {
		if parentBody != "" {
			input.Title = deriveIssueTitle(parentBody)
		} else if len(taskDrafts) > 0 {
			input.Title = deriveIssueTitle(taskDrafts[0].Title)
		} else {
			input.Title = deriveIssueTitle(input.Body)
		}
	}
	if input.Title == "" {
		writeError(w, http.StatusBadRequest, errors.New("issue cannot be empty"))
		return
	}
	input.Body = parentBody
	labelValues := append([]string{}, input.LabelKeys...)
	labelValues = append(labelValues, input.Labels...)
	initialLabels, err := a.normalizeIssueLabelKeys(labelValues)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	resolvedProject, err := a.resolveIssueProject(input.ProjectID, input.Title+"\n"+input.Body+"\n"+formatIssueTaskDraftTitles(taskDrafts))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusBadRequest
			err = errors.New("create a project before creating issues")
		} else if errors.Is(err, errProjectNotFound) {
			status = http.StatusNotFound
			err = errors.New("project not found")
		}
		writeError(w, status, err)
		return
	}
	input.ProjectID = resolvedProject.ID

	now := nowString()
	issueID := uuid.NewString()
	inboxID := uuid.NewString()
	status := "open"
	triageStatus := "pending"
	if hasIssueLabelDimension(initialLabels, issueLabelDimensionType) {
		triageStatus = "classified"
	}
	assignee := input.Assignee
	if assignee == "" {
		assignee = "me"
	}
	if input.AssigneeType == "" {
		input.AssigneeType = "human"
	}
	if input.AssigneeType != "human" && input.AssigneeType != "agent" {
		writeError(w, http.StatusBadRequest, errors.New("assignee type must be human or agent"))
		return
	}
	for _, task := range taskDrafts {
		if err := validateIssueStatus(task.Status); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}

	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO issues (id, project_id, parent_issue_id, sort_order, title, body, status, triage_status, assignee, assignee_type, environment_url, created_at, updated_at)
		VALUES (?, ?, NULL, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, issueID, input.ProjectID, input.Title, input.Body, status, triageStatus, assignee, input.AssigneeType, "", now, now); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	for index, task := range taskDrafts {
		taskID := uuid.NewString()
		taskStatus := normalizeIssueStatus(task.Status)
		if taskStatus == "" {
			taskStatus = "open"
		}
		if _, err := tx.Exec(`
			INSERT INTO issues (id, project_id, parent_issue_id, sort_order, title, body, status, triage_status, assignee, assignee_type, environment_url, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'none', ?, 'human', '', ?, ?)
		`, taskID, input.ProjectID, issueID, index+1, task.Title, task.Body, taskStatus, "me", now, now); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	if _, err := tx.Exec(`
		INSERT INTO inbox_items (id, issue_id, project_id, title, status, unread, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)
	`, inboxID, issueID, input.ProjectID, input.Title, status, now, now); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if _, err := tx.Exec(`
		INSERT INTO comments (id, issue_id, author_type, body, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, uuid.NewString(), issueID, "system", "Issue created and ready for a local-first agent session.", now); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	for _, label := range initialLabels {
		if _, err := tx.Exec(`
			INSERT INTO issue_labels (id, issue_id, label_id, name, color, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, uuid.NewString(), issueID, label.ID, label.Name, label.Color, now); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if triageStatus == "pending" {
		go a.triageIssueType(issueID)
	}

	writeJSON(w, map[string]string{"issueId": issueID})
}

func deriveIssueTitle(body string) string {
	title := strings.TrimSpace(body)
	if title == "" {
		return ""
	}
	if idx := strings.IndexByte(title, '\n'); idx >= 0 {
		title = strings.TrimSpace(title[:idx])
	}
	title = strings.Join(strings.Fields(title), " ")
	runes := []rune(title)
	if len(runes) > 64 {
		return string(runes[:64]) + "..."
	}
	return title
}

func extractIssueTaskDrafts(body string) (string, []issueTaskDraft) {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	parentLines := make([]string, 0, len(lines))
	tasks := make([]issueTaskDraft, 0)
	for _, line := range lines {
		matches := checklistItemPattern.FindStringSubmatch(line)
		if len(matches) != 3 {
			parentLines = append(parentLines, line)
			continue
		}
		title := strings.TrimSpace(matches[2])
		if title == "" {
			parentLines = append(parentLines, line)
			continue
		}
		status := "open"
		if strings.EqualFold(matches[1], "x") {
			status = "completed"
		}
		tasks = append(tasks, issueTaskDraft{Title: title, Status: status})
	}
	return strings.TrimSpace(strings.Join(parentLines, "\n")), tasks
}

func normalizeIssueTaskStrings(values []string) []issueTaskDraft {
	tasks := make([]issueTaskDraft, 0, len(values))
	for _, value := range values {
		title := strings.TrimSpace(value)
		if title == "" {
			continue
		}
		tasks = append(tasks, issueTaskDraft{Title: title, Status: "open"})
	}
	return tasks
}

func normalizeIssueTaskInputs(values []issueTaskInput) []issueTaskDraft {
	tasks := make([]issueTaskDraft, 0, len(values))
	for _, value := range values {
		title := strings.TrimSpace(value.Title)
		body := strings.TrimSpace(value.Body)
		if title == "" {
			title = deriveIssueTitle(body)
		}
		if title == "" {
			continue
		}
		status := normalizeIssueStatus(value.Status)
		if status == "" && value.Completed {
			status = "completed"
		}
		if status == "" {
			status = "open"
		}
		tasks = append(tasks, issueTaskDraft{Title: title, Body: body, Status: status})
	}
	return tasks
}

func formatIssueTaskDraftTitles(tasks []issueTaskDraft) string {
	if len(tasks) == 0 {
		return ""
	}
	titles := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if task.Title != "" {
			titles = append(titles, task.Title)
		}
	}
	return strings.Join(titles, "\n")
}

func normalizeIssueStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "open", "in_progress", "blocked", "review", "completed", "cancelled", "failed", "running", "queued":
		return value
	case "todo":
		return "open"
	case "done", "closed":
		return "completed"
	default:
		return value
	}
}

func validateIssueStatus(value string) error {
	switch normalizeIssueStatus(value) {
	case "open", "in_progress", "blocked", "review", "completed", "cancelled", "failed", "running", "queued":
		return nil
	default:
		return fmt.Errorf("unsupported issue status %q", value)
	}
}

func (a *app) resolveIssueProject(projectID, text string) (project, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID != "" {
		p, err := a.loadProject(projectID)
		if errors.Is(err, sql.ErrNoRows) {
			return project{}, errProjectNotFound
		}
		return p, err
	}

	projects, err := a.listProjectsForIssueInference()
	if err != nil {
		return project{}, err
	}
	if len(projects) == 0 {
		return project{}, sql.ErrNoRows
	}

	bestProject := projects[0]
	bestScore := issueProjectScore(bestProject, text)
	for _, candidate := range projects[1:] {
		score := issueProjectScore(candidate, text)
		if score > bestScore {
			bestProject = candidate
			bestScore = score
		}
	}
	return bestProject, nil
}

func (a *app) listProjectsForIssueInference() ([]project, error) {
	rows, err := a.db.Query(`
		SELECT id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, kubeconfig_path, namespace, image_registry_prefix, preview_domain, ingress_class, node_host, default_cluster_id, created_at, updated_at
		FROM projects
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := make([]project, 0)
	for rows.Next() {
		var p project
		if err := rows.Scan(&p.ID, &p.Name, &p.RepoPath, &p.SourceType, &p.RemoteURL, &p.GitProvider, &p.GitOwner, &p.GitRepo, &p.DefaultBranch, &p.DeployCommand, &p.ValidationCommand, &p.KubeContext, &p.KubeconfigPath, &p.Namespace, &p.ImageRegistryPrefix, &p.PreviewDomain, &p.IngressClass, &p.NodeHost, &p.DefaultClusterID, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func issueProjectScore(project project, text string) int {
	normalizedText := strings.ToLower(text)
	score := 0
	score += projectTokenScore(normalizedText, project.Name, 6)
	score += projectTokenScore(normalizedText, project.GitRepo, 5)
	score += projectTokenScore(normalizedText, project.GitOwner+"/"+project.GitRepo, 7)
	score += projectTokenScore(normalizedText, filepath.Base(project.RepoPath), 4)
	score += projectTokenScore(normalizedText, project.RemoteURL, 2)
	return score
}

func projectTokenScore(text, token string, weight int) int {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" || !strings.Contains(text, token) {
		return 0
	}
	return weight
}

func (a *app) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	detail, err := a.loadIssueDetail(issueID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}
	_, _ = a.db.Exec(`UPDATE inbox_items SET unread = 0 WHERE issue_id = ?`, issueID)
	writeJSON(w, detail)
}

func (a *app) handleUpdateIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	var input struct {
		Title  *string `json:"title"`
		Body   *string `json:"body"`
		Status *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	existing, err := a.loadIssue(issueID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		}
		writeError(w, status, err)
		return
	}

	updated := existing
	if input.Title != nil {
		updated.Title = strings.TrimSpace(*input.Title)
		if updated.Title == "" {
			writeError(w, http.StatusBadRequest, errors.New("issue title cannot be empty"))
			return
		}
	}
	if input.Body != nil {
		updated.Body = strings.TrimSpace(*input.Body)
	}
	if input.Status != nil {
		updated.Status = normalizeIssueStatus(*input.Status)
		if err := validateIssueStatus(updated.Status); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	updated.UpdatedAt = nowString()

	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		UPDATE issues
		SET title = ?, body = ?, status = ?, updated_at = ?
		WHERE id = ?
	`, updated.Title, updated.Body, updated.Status, updated.UpdatedAt, issueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	topIssueID := issueID
	if existing.ParentIssueID == "" {
		if _, err := tx.Exec(`
			UPDATE inbox_items SET title = ?, status = ?, updated_at = ?, unread = 1 WHERE issue_id = ?
		`, updated.Title, updated.Status, updated.UpdatedAt, issueID); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	} else {
		topIssueID = existing.ParentIssueID
		if _, err := tx.Exec(`UPDATE issues SET updated_at = ? WHERE id = ?`, updated.UpdatedAt, existing.ParentIssueID); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if _, err := tx.Exec(`
			UPDATE inbox_items SET updated_at = ?, unread = 1 WHERE issue_id = ?
		`, updated.UpdatedAt, existing.ParentIssueID); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	a.publishInboxEvent(topIssueID, "updated")
	reloaded, err := a.loadIssue(issueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, reloaded)
}

func (a *app) handleCreateIssueTask(w http.ResponseWriter, r *http.Request) {
	parentIssueID := chi.URLParam(r, "issueID")
	var input issueTaskInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	parent, err := a.loadIssue(parentIssueID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		}
		writeError(w, status, err)
		return
	}
	if parent.ParentIssueID != "" {
		writeError(w, http.StatusBadRequest, errors.New("nested issue tasks are not supported yet"))
		return
	}

	taskDrafts := normalizeIssueTaskInputs([]issueTaskInput{input})
	if len(taskDrafts) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("task title cannot be empty"))
		return
	}
	task := taskDrafts[0]
	if err := validateIssueStatus(task.Status); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	now := nowString()
	taskID := uuid.NewString()
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	var sortOrder int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(sort_order), 0) + 1 FROM issues WHERE parent_issue_id = ?`, parentIssueID).Scan(&sortOrder); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := tx.Exec(`
		INSERT INTO issues (id, project_id, parent_issue_id, sort_order, title, body, status, triage_status, assignee, assignee_type, environment_url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'none', 'me', 'human', '', ?, ?)
	`, taskID, parent.ProjectID, parentIssueID, sortOrder, task.Title, task.Body, task.Status, now, now); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := tx.Exec(`UPDATE issues SET updated_at = ? WHERE id = ?`, now, parentIssueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := tx.Exec(`UPDATE inbox_items SET updated_at = ?, unread = 1 WHERE issue_id = ?`, now, parentIssueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	a.publishInboxEvent(parentIssueID, "updated")
	item, err := a.loadIssueListItem(taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, item)
}

func (a *app) handleDeleteIssueTask(w http.ResponseWriter, r *http.Request) {
	parentIssueID := chi.URLParam(r, "issueID")
	taskID := chi.URLParam(r, "taskID")

	parent, err := a.loadIssue(parentIssueID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		}
		writeError(w, status, err)
		return
	}
	if parent.ParentIssueID != "" {
		writeError(w, http.StatusBadRequest, errors.New("nested issue tasks are not supported yet"))
		return
	}

	task, err := a.loadIssue(taskID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("task not found")
		}
		writeError(w, status, err)
		return
	}
	if task.ParentIssueID != parentIssueID {
		writeError(w, http.StatusNotFound, errors.New("task not found"))
		return
	}

	now := nowString()
	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	result, err := tx.Exec(`DELETE FROM issues WHERE id = ? AND parent_issue_id = ?`, taskID, parentIssueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if rowsAffected == 0 {
		writeError(w, http.StatusNotFound, errors.New("task not found"))
		return
	}
	if _, err := tx.Exec(`UPDATE issues SET updated_at = ? WHERE id = ?`, now, parentIssueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := tx.Exec(`UPDATE inbox_items SET updated_at = ?, unread = 1 WHERE issue_id = ?`, now, parentIssueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	a.publishInboxEvent(parentIssueID, "updated")
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *app) handleUpdateIssueLabels(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	var input struct {
		Labels    []string `json:"labels"`
		LabelKeys []string `json:"labelKeys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	labelValues := append([]string{}, input.LabelKeys...)
	labelValues = append(labelValues, input.Labels...)
	labels, err := a.normalizeIssueLabelKeys(labelValues)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	var projectID string
	var currentTriageStatus string
	if err := tx.QueryRow(`SELECT project_id, triage_status FROM issues WHERE id = ?`, issueID).Scan(&projectID, &currentTriageStatus); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		}
		writeError(w, status, err)
		return
	}

	if _, err := tx.Exec(`DELETE FROM issue_labels WHERE issue_id = ?`, issueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	now := nowString()
	for _, label := range labels {
		if _, err := tx.Exec(`
			INSERT INTO issue_labels (id, issue_id, label_id, name, color, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, uuid.NewString(), issueID, label.ID, label.Name, label.Color, now); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	nextTriageStatus := currentTriageStatus
	if hasIssueLabelDimension(labels, issueLabelDimensionType) {
		nextTriageStatus = "classified"
	} else if currentTriageStatus == "classified" {
		nextTriageStatus = "none"
	}
	if _, err := tx.Exec(`UPDATE issues SET triage_status = ?, updated_at = ? WHERE id = ?`, nextTriageStatus, now, issueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if _, err := tx.Exec(`UPDATE inbox_items SET updated_at = ?, unread = 1 WHERE issue_id = ?`, now, issueID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	a.publishInboxEvent(issueID, "labels")
	updatedLabels, err := a.listIssueLabels(issueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, updatedLabels)
}

func (a *app) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	var input struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(input.Body) == "" {
		writeError(w, http.StatusBadRequest, errors.New("comment body cannot be empty"))
		return
	}
	now := nowString()
	_, err := a.db.Exec(`
		INSERT INTO comments (id, issue_id, author_type, body, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, uuid.NewString(), issueID, "human", input.Body, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	_, _ = a.db.Exec(`UPDATE issues SET updated_at = ? WHERE id = ?`, now, issueID)
	_, _ = a.db.Exec(`UPDATE inbox_items SET updated_at = ?, unread = 1 WHERE issue_id = ?`, now, issueID)
	a.publishInboxEvent(issueID, "updated")
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *app) handleAssignIssueToAgent(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	var input sessionRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	session, err := a.queueAgentSession(issueID, input)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		} else if errors.Is(err, errUnknownAgentProfile) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}

	writeJSON(w, map[string]string{"sessionId": session.ID})
}

func (a *app) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	var input sessionRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	session, err := a.queueAgentSession(issueID, input)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		} else if errors.Is(err, errUnknownAgentProfile) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}

	writeJSON(w, map[string]string{"sessionId": session.ID})
}

func (a *app) handleStartIssueTestDeploy(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	var input testDeployRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	detail, err := a.loadIssueDetail(issueID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		}
		writeError(w, status, err)
		return
	}
	if a.issueHasActiveSession(issueID) {
		writeError(w, http.StatusConflict, errActiveIssueSession)
		return
	}

	environment, err := a.buildIssueTestEnvironment(detail, input)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errClusterNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}
	command := buildIssueTestDeployPrompt(detail, environment)
	if err := a.saveIssueTestEnvironment(environment); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	session, err := a.queueAgentSession(issueID, sessionRequest{
		Provider:     "codex",
		AgentProfile: strings.TrimSpace(input.AgentProfile),
		Command:      command,
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errUnknownAgentProfile) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}

	environment.LastDeploySessionID = session.ID
	environment.NamespaceStatus = "deploying"
	if err := a.saveIssueTestEnvironment(environment); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.addSystemComment(issueID, fmt.Sprintf(
		"Queued test deployment session `%s` for namespace `%s`.\n\nCluster: `%s`\nRegistry: `%s`\nExposure: %s",
		shortID(session.ID),
		environment.Namespace,
		environment.ClusterID,
		environment.ImageRegistryPrefix,
		previewStrategyLabel(environment),
	))
	a.publishInboxEvent(issueID, "test-deploy")

	writeJSON(w, map[string]any{
		"sessionId":       session.ID,
		"testEnvironment": environment,
	})
}

func (a *app) handleRequestIssueTestEnvironmentCleanup(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	var input testDeployRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	detail, err := a.loadIssueDetail(issueID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		}
		writeError(w, status, err)
		return
	}
	if detail.TestEnvironment == nil || strings.TrimSpace(detail.TestEnvironment.Namespace) == "" {
		writeError(w, http.StatusBadRequest, errors.New("issue has no test namespace to clean up"))
		return
	}
	if a.issueHasActiveSession(issueID) {
		writeError(w, http.StatusConflict, errActiveIssueSession)
		return
	}

	environment := *detail.TestEnvironment
	environment.NamespaceStatus = "cleanup_requested"
	environment.CleanupStatus = "cleanup_requested"
	command := buildIssueTestCleanupPrompt(detail, environment)
	if err := a.saveIssueTestEnvironment(environment); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	session, err := a.queueAgentSession(issueID, sessionRequest{
		Provider:     "codex",
		AgentProfile: strings.TrimSpace(input.AgentProfile),
		Command:      command,
	})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errUnknownAgentProfile) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}
	environment.LastCleanupSessionID = session.ID
	if err := a.saveIssueTestEnvironment(environment); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.addSystemComment(issueID, fmt.Sprintf("Queued namespace cleanup session `%s` for `%s`.", shortID(session.ID), environment.Namespace))
	a.publishInboxEvent(issueID, "test-cleanup")
	writeJSON(w, map[string]any{
		"sessionId":       session.ID,
		"testEnvironment": environment,
	})
}

func (a *app) handleRetainIssueTestEnvironment(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	detail, err := a.loadIssueDetail(issueID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("issue not found")
		}
		writeError(w, status, err)
		return
	}
	if detail.TestEnvironment == nil || strings.TrimSpace(detail.TestEnvironment.Namespace) == "" {
		writeError(w, http.StatusBadRequest, errors.New("issue has no test namespace to retain"))
		return
	}
	environment := *detail.TestEnvironment
	environment.NamespaceStatus = "retained"
	environment.CleanupStatus = "retained"
	if err := a.saveIssueTestEnvironment(environment); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.addSystemComment(issueID, fmt.Sprintf("Retained test namespace `%s` for later inspection.", environment.Namespace))
	a.publishInboxEvent(issueID, "test-retain")
	writeJSON(w, environment)
}

func (a *app) queueAgentSession(issueID string, input sessionRequest) (agentSession, error) {
	input.Provider = strings.TrimSpace(input.Provider)
	input.AgentProfile = strings.TrimSpace(input.AgentProfile)
	input.Command = strings.TrimSpace(input.Command)
	input.Branch = strings.TrimSpace(input.Branch)
	if input.Provider != "" && !strings.EqualFold(input.Provider, "codex") && input.AgentProfile == "" {
		if profile, err := a.resolveEnabledAgentProfile(input.Provider); err == nil {
			input.AgentProfile = profile.ID
			input.Provider = "codex"
		}
	}
	if input.Provider == "" {
		input.Provider = "codex"
	}
	profile := agentProfile{}
	if isCodexProvider(input.Provider) {
		var err error
		profile, err = a.resolveEnabledAgentProfile(input.AgentProfile)
		if err != nil {
			return agentSession{}, err
		}
		input.Provider = "codex"
		input.AgentProfile = profile.ID
	}

	detail, err := a.loadIssueDetail(issueID)
	if err != nil {
		return agentSession{}, err
	}

	command := input.Command
	if !isCodexProvider(input.Provider) {
		command = buildSessionCommand(issueID, detail.Project, input.Command)
	}
	sessionID := uuid.NewString()
	branch := input.Branch
	if branch == "" {
		branch = fmt.Sprintf("mspace/%s/%s", shortID(issueID), shortID(sessionID))
	}
	workdir := plannedSessionWorkdir(a.workdir, detail.Project.ID, sessionID)

	session := agentSession{
		ID:            sessionID,
		IssueID:       issueID,
		Provider:      input.Provider,
		AgentProfile:  input.AgentProfile,
		RuntimeMode:   "local",
		Command:       command,
		Status:        "queued",
		Branch:        branch,
		Workdir:       workdir,
		AgentStatus:   "queued",
		CleanupStatus: "retained",
		CreatedAt:     nowString(),
		UpdatedAt:     nowString(),
	}

	if _, err := a.db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, cleanup_status, cleaned_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, session.ID, session.IssueID, session.Provider, session.AgentProfile, session.RuntimeMode, session.Command, session.Status, session.Branch, session.Workdir, session.CodexThreadID, session.CodexTurnID, session.AgentStatus, session.ArtifactDir, session.CleanupStatus, session.CleanedAt, session.CreatedAt, session.UpdatedAt); err != nil {
		return agentSession{}, err
	}

	assignee := input.Provider
	if profile.ID != "" {
		assignee = profile.ID
	}
	if err := a.updateIssueAssignment(issueID, assignee, "agent", "queued"); err != nil {
		return agentSession{}, err
	}

	displayAgent := input.Provider
	if profile.Name != "" {
		displayAgent = profile.Name
	}
	a.addSystemComment(issueID, fmt.Sprintf(
		"Assigned to agent `%s` and queued local session `%s` on branch `%s`.\n\nPlanned workspace: `%s`",
		displayAgent,
		shortID(session.ID),
		session.Branch,
		session.Workdir,
	))

	go a.runSession(session, detail.Project)
	return session, nil
}

func (a *app) issueHasActiveSession(issueID string) bool {
	var count int
	if err := a.db.QueryRow(`
		SELECT COUNT(*)
		FROM agent_sessions
		WHERE issue_id = ? AND status IN ('queued', 'running')
	`, issueID).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

func (a *app) buildIssueTestEnvironment(detail issueDetail, input testDeployRequest) (issueTestEnvironment, error) {
	environment := issueTestEnvironment{}
	if detail.TestEnvironment != nil {
		environment = *detail.TestEnvironment
	}
	environment.IssueID = detail.Issue.ID
	if strings.TrimSpace(environment.Namespace) == "" {
		environment.Namespace = defaultIssueNamespace(detail)
	}
	clusterID := firstNonEmpty(input.ClusterID, environment.ClusterID, detail.Project.DefaultClusterID)
	if clusterID == "" {
		return environment, errors.New("cluster is required before starting a test deployment")
	}
	selectedCluster, err := a.loadCluster(clusterID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return environment, errClusterNotFound
		}
		return environment, err
	}
	exposureMode, err := normalizeExposureMode(input.ExposureMode)
	if err != nil {
		return environment, err
	}
	if exposureMode == "" {
		exposureMode = selectedCluster.ExposureMode
	}
	if exposureMode == "" {
		exposureMode = "nodeport"
	}

	environment.NamespaceStatus = "planned"
	environment.CleanupStatus = "retained"
	environment.ClusterID = selectedCluster.ID
	environment.KubeconfigPath = selectedCluster.KubeconfigPath
	environment.KubeContext = selectedCluster.KubeContext
	environment.ImageRegistryPrefix = selectedCluster.ImageRegistryPrefix
	environment.ExposureMode = exposureMode
	environment.NodeHost = firstNonEmpty(input.NodeHost, selectedCluster.NodeHost)
	if exposureMode == "ingress" {
		environment.PreviewDomain = firstNonEmpty(input.PreviewDomain, selectedCluster.PreviewDomain)
		environment.IngressClass = firstNonEmpty(input.IngressClass, selectedCluster.IngressClass)
	} else {
		environment.PreviewDomain = ""
		environment.IngressClass = ""
	}

	if environment.KubeconfigPath == "" {
		return environment, errors.New("selected cluster needs a kubeconfig path before starting a test deployment")
	}
	if environment.ImageRegistryPrefix == "" {
		return environment, errors.New("selected cluster needs an image registry prefix before starting a test deployment")
	}
	if environment.ExposureMode == "ingress" && environment.PreviewDomain == "" {
		return environment, errors.New("preview domain is required when ingress exposure is selected")
	}
	return environment, nil
}

func defaultIssueNamespace(detail issueDetail) string {
	return dnsLabel("mspace-" + detail.Project.Name + "-" + shortID(detail.Issue.ID))
}

func dnsLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastHyphen := false
	for _, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			builder.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen && builder.Len() > 0 {
			builder.WriteByte('-')
			lastHyphen = true
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		result = "mspace-issue"
	}
	if len(result) > 63 {
		result = strings.TrimRight(result[:63], "-")
	}
	if result == "" {
		return "mspace-issue"
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func buildIssueTestDeployPrompt(detail issueDetail, environment issueTestEnvironment) string {
	var builder strings.Builder
	builder.WriteString("Deploy a test environment for this issue.\n\n")
	builder.WriteString("The user manually triggered this deployment after agent work, so do not create a PR unless explicitly asked in a separate turn.\n\n")
	if sourceSession := latestSourceSession(detail); sourceSession != nil {
		builder.WriteString("Source code to deploy:\n")
		builder.WriteString(fmt.Sprintf("- Source session: %s\n", shortID(sourceSession.ID)))
		builder.WriteString(fmt.Sprintf("- Source branch: %s\n", valueOrUnset(sourceSession.Branch)))
		builder.WriteString(fmt.Sprintf("- Source worktree: %s\n", valueOrUnset(sourceSession.Workdir)))
		builder.WriteString("- If the source worktree exists, build and deploy from that path. Use the current session worktree only for orchestration when needed.\n\n")
	} else {
		builder.WriteString("No previous completed source session was found. Use the current prepared worktree as the source.\n\n")
	}
	builder.WriteString("Deployment contract:\n")
	builder.WriteString(fmt.Sprintf("- Cluster ID: %s\n", environment.ClusterID))
	builder.WriteString(fmt.Sprintf("- Kubeconfig path: %s\n", environment.KubeconfigPath))
	if environment.KubeContext != "" {
		builder.WriteString(fmt.Sprintf("- Kube context: %s\n", environment.KubeContext))
	}
	builder.WriteString(fmt.Sprintf("- Issue namespace to create and manage: %s\n", environment.Namespace))
	builder.WriteString(fmt.Sprintf("- Image registry prefix: %s\n", environment.ImageRegistryPrefix))
	builder.WriteString(fmt.Sprintf("- Project deploy command hint: %s\n", valueOrUnset(detail.Project.DeployCommand)))
	builder.WriteString(fmt.Sprintf("- Project validation command hint: %s\n", valueOrUnset(detail.Project.ValidationCommand)))
	builder.WriteString("- Build and push linux/amd64 images unless the issue explicitly requires another platform.\n")
	builder.WriteString("- Create the namespace yourself if it does not exist.\n")
	builder.WriteString("- Deploy or update only namespaced resources inside the issue namespace.\n")
	builder.WriteString("- Do not read Kubernetes Secrets.\n")
	builder.WriteString("- Return a team-accessible preview URL after deployment.\n")
	if environment.ExposureMode == "ingress" {
		builder.WriteString(fmt.Sprintf("- Preferred preview exposure: Ingress or HTTPRoute under domain %s.\n", environment.PreviewDomain))
		if environment.IngressClass != "" {
			builder.WriteString(fmt.Sprintf("- Preferred ingress class: %s.\n", environment.IngressClass))
		}
	} else {
		builder.WriteString("- Preferred preview exposure: Service type=NodePort.\n")
		if environment.NodeHost != "" {
			builder.WriteString(fmt.Sprintf("- Use this node host when forming the preview URL: %s.\n", environment.NodeHost))
		} else {
			builder.WriteString("- Discover a usable node ExternalIP, node hostname, or fallback node address for the NodePort URL.\n")
		}
	}
	builder.WriteString("\nRequired completion evidence:\n")
	builder.WriteString("- Image reference(s) pushed.\n")
	builder.WriteString("- Kubernetes resources applied.\n")
	builder.WriteString("- Pods, Services, Ingress/HTTPRoute if any, and recent Events inspected.\n")
	builder.WriteString("- Preview URL probed with curl or an equivalent HTTP check.\n")
	builder.WriteString("- Final answer includes the preview URL, namespace, image reference, validation result, and any blocker.\n")
	builder.WriteString("- Also write a JSON file to `${MSPACE_SESSION_ARTIFACT_DIR}/test-environment.json` with at least `previewUrl` when a URL is available.\n")
	return builder.String()
}

func latestSourceSession(detail issueDetail) *agentSession {
	for _, session := range detail.Sessions {
		if session.Status == "queued" || session.Status == "running" {
			continue
		}
		if detail.TestEnvironment != nil {
			if session.ID == detail.TestEnvironment.LastDeploySessionID || session.ID == detail.TestEnvironment.LastCleanupSessionID {
				continue
			}
		}
		if isSystemTestEnvironmentCommand(session.Command) {
			continue
		}
		sessionCopy := session
		return &sessionCopy
	}
	return nil
}

func isSystemTestEnvironmentCommand(command string) bool {
	command = strings.TrimSpace(command)
	return strings.HasPrefix(command, "Deploy a test environment for this issue.") ||
		strings.HasPrefix(command, "Clean up the test environment for this issue.")
}

func buildIssueTestCleanupPrompt(detail issueDetail, environment issueTestEnvironment) string {
	var builder strings.Builder
	builder.WriteString("Clean up the test environment for this issue.\n\n")
	builder.WriteString("The user manually chose to remove this issue's test namespace. Use the provided kubeconfig and delete only this namespace and resources inside it.\n\n")
	builder.WriteString(fmt.Sprintf("- Kubeconfig path: %s\n", environment.KubeconfigPath))
	if environment.KubeContext != "" {
		builder.WriteString(fmt.Sprintf("- Kube context: %s\n", environment.KubeContext))
	}
	builder.WriteString(fmt.Sprintf("- Namespace to delete: %s\n", environment.Namespace))
	builder.WriteString(fmt.Sprintf("- Issue: %s\n", detail.Issue.Title))
	builder.WriteString("\nAfter cleanup, verify whether the namespace still exists and report the result. Do not delete any other namespace or cluster-scoped resource.\n")
	return builder.String()
}

func previewStrategyLabel(environment issueTestEnvironment) string {
	if environment.ExposureMode == "ingress" || environment.PreviewDomain != "" {
		if environment.IngressClass != "" {
			return fmt.Sprintf("Ingress `%s` on `%s`", environment.IngressClass, environment.PreviewDomain)
		}
		return fmt.Sprintf("Ingress on `%s`", environment.PreviewDomain)
	}
	if environment.NodeHost != "" {
		return fmt.Sprintf("NodePort via `%s`", environment.NodeHost)
	}
	return "NodePort with discovered node address"
}

func (a *app) handleGetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	detail, err := a.loadSessionDetail(sessionID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, detail)
}

func (a *app) handleCancelSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	a.mu.Lock()
	cancel := a.cancellers[sessionID]
	a.mu.Unlock()
	if cancel == nil {
		detail, err := a.loadSessionDetail(sessionID)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, sql.ErrNoRows) {
				status = http.StatusNotFound
				err = errors.New("session not found")
			}
			writeError(w, status, err)
			return
		}
		if detail.Session.Status == "queued" {
			a.updateSessionStatus(sessionID, "cancelled")
			a.updateSessionAgentStatus(sessionID, "cancelled")
			a.updateIssueStatus(detail.Session.IssueID, "cancelled")
			a.addSystemComment(detail.Session.IssueID, fmt.Sprintf("Session `%s` was cancelled before it started.", shortID(sessionID)))
			writeJSON(w, map[string]bool{"ok": true})
			return
		}
		writeError(w, http.StatusConflict, errors.New("session is not running"))
		return
	}
	cancel()
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *app) handleCleanupSessionWorktree(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	err := a.cleanupSessionWorktree(sessionID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("session not found")
		} else if errors.Is(err, errSessionActive) {
			status = http.StatusConflict
		} else if errors.Is(err, errUnsafeSessionWorkdir) {
			status = http.StatusBadRequest
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *app) handleSessionStream(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	a.streamEvents(w, r, sessionID)
}

func (a *app) streamEvents(w http.ResponseWriter, r *http.Request, channelID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsubscribe := a.broker.subscribe(channelID)
	defer unsubscribe()

	for {
		select {
		case payload, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (a *app) runSession(session agentSession, project project) {
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.cancellers[session.ID] = cancel
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.cancellers, session.ID)
		a.mu.Unlock()
	}()
	if a.sessionWasCancelled(session.ID) {
		return
	}

	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Preparing workspace for branch %s", session.Branch))
	preparedSession, err := a.prepareSessionWorkspace(session, project)
	if err != nil {
		a.failSession(session, &project, fmt.Errorf("prepare workspace: %w", err))
		return
	}
	session = preparedSession
	contextPath, err := a.writeSessionContext(session, project)
	if err != nil {
		a.failSession(session, &project, fmt.Errorf("write session context: %w", err))
		return
	}

	a.updateSessionStatus(session.ID, "running")
	a.updateIssueStatus(session.IssueID, "running")
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Runner starting %s session in %s", session.Provider, session.Workdir))
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Source repository: %s", project.RepoPath))
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Session branch: %s", session.Branch))
	if strings.TrimSpace(session.AgentProfile) != "" {
		a.appendSessionLog(session.ID, "system", fmt.Sprintf("Agent profile: %s", session.AgentProfile))
	}
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Session context: %s", contextPath))
	a.addSystemComment(session.IssueID, fmt.Sprintf(
		"Started local session `%s` on branch `%s`.\n\nWorkspace: `%s`\nContext: `%s`",
		shortID(session.ID),
		session.Branch,
		session.Workdir,
		contextPath,
	))

	if isCodexProvider(session.Provider) {
		if strings.TrimSpace(session.Command) != "" {
			a.appendSessionLog(session.ID, "system", fmt.Sprintf("Agent instructions: %s", session.Command))
		}
		err = a.runCodexAppServerSession(ctx, session, project, contextPath)
	} else {
		a.appendSessionLog(session.ID, "system", fmt.Sprintf("Command: %s", session.Command))
		err = a.runShellSession(ctx, session, project, contextPath)
	}

	if ctx.Err() == context.Canceled || errors.Is(err, context.Canceled) {
		a.updateSessionStatus(session.ID, "cancelled")
		a.updateSessionAgentStatus(session.ID, "cancelled")
		a.updateIssueStatus(session.IssueID, "cancelled")
		a.appendSessionLog(session.ID, "system", "Session cancelled.")
		a.addSystemComment(session.IssueID, fmt.Sprintf("Session `%s` was cancelled.", shortID(session.ID)))
		return
	}
	if err != nil {
		a.failSession(session, &project, err)
		return
	}

	a.updateSessionStatus(session.ID, "completed")
	a.updateSessionAgentStatus(session.ID, "completed")
	a.updateIssueStatus(session.IssueID, "completed")
	a.addSystemComment(session.IssueID, fmt.Sprintf("Session `%s` completed successfully.", shortID(session.ID)))
	if a.isIssueTestCleanupSession(session) {
		a.appendSessionLog(session.ID, "system", "Session completed. Updating test namespace cleanup state.")
		a.updateIssueTestEnvironmentForSession(session, true)
		return
	}
	a.appendSessionLog(session.ID, "system", "Session completed. Collecting Kubernetes evidence.")
	a.collectEvidence(session, project)
	a.updateIssueTestEnvironmentForSession(session, true)
}

func (a *app) runShellSession(ctx context.Context, session agentSession, project project, contextPath string) error {
	command := exec.CommandContext(ctx, "/bin/zsh", "-lc", session.Command)
	command.Dir = session.Workdir
	command.Env = append(os.Environ(), a.buildSessionEnv(session, project, contextPath)...)

	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := command.Start(); err != nil {
		return fmt.Errorf("start command: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go a.captureStream(&wg, session.ID, "stdout", stdout)
	go a.captureStream(&wg, session.ID, "stderr", stderr)

	err = command.Wait()
	wg.Wait()
	if ctx.Err() == context.Canceled {
		return context.Canceled
	}
	if err != nil {
		return fmt.Errorf("command failed: %w", err)
	}
	return nil
}

func (a *app) captureStream(wg *sync.WaitGroup, sessionID, stream string, reader interface{ Read([]byte) (int, error) }) {
	defer wg.Done()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		a.appendSessionLog(sessionID, stream, scanner.Text())
	}
}

func (a *app) failSession(session agentSession, project *project, err error) {
	a.updateSessionStatus(session.ID, "failed")
	a.updateSessionAgentStatus(session.ID, "failed")
	a.updateIssueStatus(session.IssueID, "failed")
	a.appendSessionLog(session.ID, "system", err.Error())
	a.addSystemComment(session.IssueID, fmt.Sprintf("Session `%s` failed.\n\n%s", shortID(session.ID), err.Error()))
	if project != nil && a.evidenceTargetProject(session, *project).Namespace != "" {
		a.appendSessionLog(session.ID, "system", "Collecting Kubernetes evidence after failure.")
		a.collectEvidence(session, *project)
	}
	a.updateIssueTestEnvironmentForSession(session, false)
}

func (a *app) writeSessionContext(session agentSession, project project) (string, error) {
	detail, err := a.loadIssueDetail(session.IssueID)
	if err != nil {
		return "", err
	}
	contextDir := filepath.Join(a.workdir, "_contexts")
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		return "", err
	}
	contextPath := filepath.Join(contextDir, session.ID+".md")

	var builder strings.Builder
	builder.WriteString("# mspace Session Context\n\n")
	builder.WriteString("## Issue\n\n")
	builder.WriteString(fmt.Sprintf("- ID: %s\n", detail.Issue.ID))
	builder.WriteString(fmt.Sprintf("- Title: %s\n", detail.Issue.Title))
	builder.WriteString(fmt.Sprintf("- Status: %s\n", detail.Issue.Status))
	builder.WriteString(fmt.Sprintf("- Assignee: %s (%s)\n", detail.Issue.Assignee, detail.Issue.AssigneeType))
	builder.WriteString(fmt.Sprintf("- Labels: %s\n", formatIssueLabels(detail.Labels)))
	builder.WriteString("\n")
	builder.WriteString(strings.TrimSpace(detail.Issue.Body))
	if strings.TrimSpace(detail.Issue.Body) == "" {
		builder.WriteString("(no issue body)")
	}
	builder.WriteString("\n\n")

	writeIssueTaskList(&builder, detail.ChildIssues)

	builder.WriteString("## Project\n\n")
	builder.WriteString(fmt.Sprintf("- Name: %s\n", project.Name))
	builder.WriteString(fmt.Sprintf("- Repository: %s\n", project.RepoPath))
	builder.WriteString(fmt.Sprintf("- Remote: %s\n", project.RemoteURL))
	builder.WriteString(fmt.Sprintf("- GitHub: %s/%s\n", project.GitOwner, project.GitRepo))
	builder.WriteString(fmt.Sprintf("- Default branch: %s\n", project.DefaultBranch))
	builder.WriteString(fmt.Sprintf("- Kube context: %s\n", project.KubeContext))
	builder.WriteString(fmt.Sprintf("- Kubeconfig path: %s\n", valueOrUnset(project.KubeconfigPath)))
	builder.WriteString(fmt.Sprintf("- Namespace: %s\n", project.Namespace))
	builder.WriteString(fmt.Sprintf("- Image registry prefix: %s\n", valueOrUnset(project.ImageRegistryPrefix)))
	builder.WriteString(fmt.Sprintf("- Preview domain: %s\n", valueOrUnset(project.PreviewDomain)))
	builder.WriteString(fmt.Sprintf("- Ingress class: %s\n", valueOrUnset(project.IngressClass)))
	builder.WriteString(fmt.Sprintf("- Node host: %s\n", valueOrUnset(project.NodeHost)))
	builder.WriteString(fmt.Sprintf("- Default cluster ID: %s\n", valueOrUnset(project.DefaultClusterID)))
	builder.WriteString("\n")

	if detail.TestEnvironment != nil {
		builder.WriteString("## Issue Test Environment\n\n")
		builder.WriteString(fmt.Sprintf("- Cluster ID: %s\n", valueOrUnset(detail.TestEnvironment.ClusterID)))
		builder.WriteString(fmt.Sprintf("- Namespace: %s\n", detail.TestEnvironment.Namespace))
		builder.WriteString(fmt.Sprintf("- Namespace status: %s\n", detail.TestEnvironment.NamespaceStatus))
		builder.WriteString(fmt.Sprintf("- Cleanup status: %s\n", detail.TestEnvironment.CleanupStatus))
		builder.WriteString(fmt.Sprintf("- Preview URL: %s\n", valueOrUnset(detail.TestEnvironment.PreviewURL)))
		builder.WriteString(fmt.Sprintf("- Image registry prefix: %s\n", valueOrUnset(detail.TestEnvironment.ImageRegistryPrefix)))
		builder.WriteString(fmt.Sprintf("- Kubeconfig path: %s\n", valueOrUnset(detail.TestEnvironment.KubeconfigPath)))
		builder.WriteString(fmt.Sprintf("- Kube context: %s\n", valueOrUnset(detail.TestEnvironment.KubeContext)))
		builder.WriteString(fmt.Sprintf("- Exposure mode: %s\n", valueOrUnset(detail.TestEnvironment.ExposureMode)))
		builder.WriteString(fmt.Sprintf("- Preview strategy: %s\n", previewStrategyLabel(*detail.TestEnvironment)))
		builder.WriteString("\n")
	}

	builder.WriteString("## Session\n\n")
	builder.WriteString(fmt.Sprintf("- ID: %s\n", session.ID))
	builder.WriteString(fmt.Sprintf("- Provider: %s\n", session.Provider))
	builder.WriteString(fmt.Sprintf("- Agent profile: %s\n", valueOrUnset(session.AgentProfile)))
	builder.WriteString(fmt.Sprintf("- Branch: %s\n", session.Branch))
	builder.WriteString(fmt.Sprintf("- Workdir: %s\n", session.Workdir))
	builder.WriteString("\n")

	builder.WriteString("## Comments\n\n")
	if len(detail.Comments) == 0 {
		builder.WriteString("(no comments)\n")
	} else {
		for i := len(detail.Comments) - 1; i >= 0; i-- {
			comment := detail.Comments[i]
			builder.WriteString(fmt.Sprintf("### %s at %s\n\n", comment.AuthorType, comment.CreatedAt))
			builder.WriteString(strings.TrimSpace(comment.Body))
			builder.WriteString("\n\n")
		}
	}

	if err := os.WriteFile(contextPath, []byte(builder.String()), 0o600); err != nil {
		return "", err
	}
	return contextPath, nil
}

func (a *app) collectEvidence(session agentSession, project project) {
	project = a.evidenceTargetProject(session, project)
	if project.Namespace == "" {
		a.appendSessionLog(session.ID, "system", "Skipping Kubernetes evidence collection because no namespace is configured for this project.")
		return
	}

	kubectlPath, err := exec.LookPath("kubectl")
	if err != nil {
		a.appendSessionLog(session.ID, "system", "kubectl is not available on PATH, so Kubernetes evidence could not be collected.")
		a.storeEvidence(deploymentEvidence{
			ID:        uuid.NewString(),
			IssueID:   session.IssueID,
			SessionID: session.ID,
			Cluster:   project.KubeContext,
			Namespace: project.Namespace,
			Summary:   "Kubernetes evidence collection failed after session completion.",
			Details:   "kubectl was not found on PATH.",
			CreatedAt: nowString(),
		})
		return
	}

	summaryOutput, summaryErr := exec.Command(kubectlPath, buildKubectlArgs(project, "get", "pods,deploy,svc,ingress")...).CombinedOutput()
	eventsOutput, eventsErr := exec.Command(kubectlPath, buildKubectlArgs(project, "get", "events", "--sort-by=.lastTimestamp")...).CombinedOutput()

	summary := "Captured Kubernetes resources after session completion."
	if summaryErr != nil {
		summary = "Kubernetes evidence collection failed after session completion."
	}
	details := strings.TrimSpace(string(summaryOutput))
	if details == "" {
		if summaryErr != nil {
			details = summaryErr.Error()
		} else {
			details = "(no kubectl summary output)"
		}
	}
	if strings.TrimSpace(string(eventsOutput)) != "" {
		details += "\n\n--- events ---\n" + strings.TrimSpace(string(eventsOutput))
	} else if eventsErr != nil {
		details += "\n\n--- events ---\n" + eventsErr.Error()
	}

	a.storeEvidence(deploymentEvidence{
		ID:        uuid.NewString(),
		IssueID:   session.IssueID,
		SessionID: session.ID,
		Cluster:   project.KubeContext,
		Namespace: project.Namespace,
		Summary:   summary,
		Details:   truncate(details, 12000),
		CreatedAt: nowString(),
	})
}

func (a *app) evidenceTargetProject(session agentSession, project project) project {
	environment, err := a.loadIssueTestEnvironment(session.IssueID)
	if err != nil || environment == nil {
		return project
	}
	if strings.TrimSpace(environment.Namespace) != "" {
		project.Namespace = strings.TrimSpace(environment.Namespace)
	}
	if strings.TrimSpace(environment.KubeconfigPath) != "" {
		project.KubeconfigPath = strings.TrimSpace(environment.KubeconfigPath)
	}
	if strings.TrimSpace(environment.KubeContext) != "" {
		project.KubeContext = strings.TrimSpace(environment.KubeContext)
	} else if strings.TrimSpace(environment.ClusterID) != "" {
		project.KubeContext = strings.TrimSpace(environment.ClusterID)
	}
	return project
}

func (a *app) updateIssueTestEnvironmentForSession(session agentSession, success bool) {
	environment, err := a.loadIssueTestEnvironment(session.IssueID)
	if err != nil || environment == nil {
		return
	}
	changed := false
	switch {
	case environment.LastDeploySessionID == session.ID:
		changed = true
		if success {
			if previewURL := readTestEnvironmentPreviewURL(session); previewURL != "" {
				environment.PreviewURL = previewURL
			}
			environment.NamespaceStatus = "active"
			environment.CleanupStatus = "retained"
		} else {
			environment.NamespaceStatus = "deploy_failed"
		}
	case environment.LastCleanupSessionID == session.ID:
		changed = true
		if success {
			environment.NamespaceStatus = "cleaned"
			environment.CleanupStatus = "cleaned"
		} else {
			environment.NamespaceStatus = "cleanup_failed"
			environment.CleanupStatus = "cleanup_failed"
		}
	}
	if !changed {
		return
	}
	if err := a.saveIssueTestEnvironment(*environment); err == nil {
		a.publishInboxEvent(session.IssueID, "test-environment")
	}
}

func (a *app) isIssueTestCleanupSession(session agentSession) bool {
	environment, err := a.loadIssueTestEnvironment(session.IssueID)
	if err != nil || environment == nil {
		return false
	}
	return environment.LastCleanupSessionID == session.ID
}

func readTestEnvironmentPreviewURL(session agentSession) string {
	resultPath := filepath.Join(session.Workdir, ".mspace", "session", "test-environment.json")
	data, err := os.ReadFile(resultPath)
	if err != nil {
		return ""
	}
	var result struct {
		PreviewURL string `json:"previewUrl"`
		PreviewUrl string `json:"preview_url"`
		URL        string `json:"url"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return ""
	}
	return firstNonEmpty(result.PreviewURL, result.PreviewUrl, result.URL)
}

func (a *app) loadIssue(issueID string) (issue, error) {
	var item issue
	row := a.db.QueryRow(`
		SELECT id, project_id, COALESCE(parent_issue_id, ''), sort_order, title, body, status, triage_status, assignee, assignee_type, environment_url, created_at, updated_at
		FROM issues
		WHERE id = ?
	`, issueID)
	err := row.Scan(&item.ID, &item.ProjectID, &item.ParentIssueID, &item.SortOrder, &item.Title, &item.Body, &item.Status, &item.TriageStatus, &item.Assignee, &item.AssigneeType, &item.EnvironmentURL, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (a *app) loadIssueListItem(issueID string) (issueListItem, error) {
	var item issueListItem
	row := a.db.QueryRow(`
		SELECT
			i.id,
			i.project_id,
			p.name,
			COALESCE(i.parent_issue_id, '') AS parent_issue_id,
			i.sort_order,
			i.title,
			i.body,
			i.status,
			i.triage_status,
			i.assignee,
			i.assignee_type,
			COALESCE(ii.unread, 0) AS unread,
			COUNT(DISTINCT s.id) AS session_count,
			COUNT(DISTINCT child.id) AS child_issue_count,
			COUNT(DISTINCT CASE WHEN child.status = 'completed' THEN child.id END) AS completed_child_issue_count,
			i.updated_at,
			i.created_at
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		LEFT JOIN inbox_items ii ON ii.issue_id = i.id
		LEFT JOIN agent_sessions s ON s.issue_id = i.id
		LEFT JOIN issues child ON child.parent_issue_id = i.id
		WHERE i.id = ?
		GROUP BY i.id
	`, issueID)
	var unread int
	if err := row.Scan(&item.ID, &item.ProjectID, &item.ProjectName, &item.ParentIssueID, &item.SortOrder, &item.Title, &item.Body, &item.Status, &item.TriageStatus, &item.Assignee, &item.AssigneeType, &unread, &item.SessionCount, &item.ChildIssueCount, &item.CompletedChildIssueCount, &item.UpdatedAt, &item.CreatedAt); err != nil {
		return item, err
	}
	item.Unread = unread == 1
	labels, err := a.listIssueLabels(item.ID)
	if err != nil {
		return item, err
	}
	item.Labels = labels
	return item, nil
}

func (a *app) listChildIssues(parentIssueID string) ([]issueListItem, error) {
	rows, err := a.db.Query(`
		SELECT
			i.id,
			i.project_id,
			p.name,
			COALESCE(i.parent_issue_id, '') AS parent_issue_id,
			i.sort_order,
			i.title,
			i.body,
			i.status,
			i.triage_status,
			i.assignee,
			i.assignee_type,
			COALESCE(ii.unread, 0) AS unread,
			COUNT(DISTINCT s.id) AS session_count,
			COUNT(DISTINCT child.id) AS child_issue_count,
			COUNT(DISTINCT CASE WHEN child.status = 'completed' THEN child.id END) AS completed_child_issue_count,
			i.updated_at,
			i.created_at
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		LEFT JOIN inbox_items ii ON ii.issue_id = i.id
		LEFT JOIN agent_sessions s ON s.issue_id = i.id
		LEFT JOIN issues child ON child.parent_issue_id = i.id
		WHERE i.parent_issue_id = ?
		GROUP BY i.id
		ORDER BY i.sort_order ASC, i.created_at ASC
	`, parentIssueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]issueListItem, 0)
	for rows.Next() {
		var item issueListItem
		var unread int
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.ProjectName, &item.ParentIssueID, &item.SortOrder, &item.Title, &item.Body, &item.Status, &item.TriageStatus, &item.Assignee, &item.AssigneeType, &unread, &item.SessionCount, &item.ChildIssueCount, &item.CompletedChildIssueCount, &item.UpdatedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Unread = unread == 1
		labels, err := a.listIssueLabels(item.ID)
		if err != nil {
			return nil, err
		}
		item.Labels = labels
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *app) loadIssueDetail(issueID string) (issueDetail, error) {
	var detail issueDetail
	row := a.db.QueryRow(`
		SELECT i.id, i.project_id, COALESCE(i.parent_issue_id, ''), i.sort_order, i.title, i.body, i.status, i.triage_status, i.assignee, i.assignee_type, i.environment_url, i.created_at, i.updated_at,
		       p.id, p.name, p.repo_path, p.source_type, p.remote_url, p.git_provider, p.git_owner, p.git_repo, p.default_branch, p.deploy_command, p.validation_command, p.kube_context, p.kubeconfig_path, p.namespace, p.image_registry_prefix, p.preview_domain, p.ingress_class, p.node_host, p.default_cluster_id, p.created_at, p.updated_at
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		WHERE i.id = ?
	`, issueID)
	if err := row.Scan(
		&detail.Issue.ID, &detail.Issue.ProjectID, &detail.Issue.ParentIssueID, &detail.Issue.SortOrder, &detail.Issue.Title, &detail.Issue.Body, &detail.Issue.Status, &detail.Issue.TriageStatus, &detail.Issue.Assignee, &detail.Issue.AssigneeType, &detail.Issue.EnvironmentURL, &detail.Issue.CreatedAt, &detail.Issue.UpdatedAt,
		&detail.Project.ID, &detail.Project.Name, &detail.Project.RepoPath, &detail.Project.SourceType, &detail.Project.RemoteURL, &detail.Project.GitProvider, &detail.Project.GitOwner, &detail.Project.GitRepo, &detail.Project.DefaultBranch, &detail.Project.DeployCommand, &detail.Project.ValidationCommand, &detail.Project.KubeContext, &detail.Project.KubeconfigPath, &detail.Project.Namespace, &detail.Project.ImageRegistryPrefix, &detail.Project.PreviewDomain, &detail.Project.IngressClass, &detail.Project.NodeHost, &detail.Project.DefaultClusterID, &detail.Project.CreatedAt, &detail.Project.UpdatedAt,
	); err != nil {
		return detail, err
	}

	children, err := a.listChildIssues(issueID)
	if err != nil {
		return detail, err
	}
	detail.ChildIssues = children

	testEnvironment, err := a.loadIssueTestEnvironment(issueID)
	if err != nil {
		return detail, err
	}
	detail.TestEnvironment = testEnvironment

	labels, err := a.listIssueLabels(issueID)
	if err != nil {
		return detail, err
	}
	detail.Labels = labels

	comments, err := a.listComments(issueID)
	if err != nil {
		return detail, err
	}
	detail.Comments = comments

	sessions, err := a.listSessions(issueID)
	if err != nil {
		return detail, err
	}
	detail.Sessions = sessions

	evidence, err := a.listEvidence(issueID)
	if err != nil {
		return detail, err
	}
	detail.Evidence = evidence

	return detail, nil
}

func (a *app) loadProject(projectID string) (project, error) {
	var project project
	row := a.db.QueryRow(`
		SELECT
			p.id,
			p.name,
			p.repo_path,
			p.source_type,
			p.remote_url,
			p.git_provider,
			p.git_owner,
			p.git_repo,
			p.default_branch,
			p.deploy_command,
			p.validation_command,
			p.kube_context,
			p.kubeconfig_path,
			p.namespace,
			p.image_registry_prefix,
			p.preview_domain,
			p.ingress_class,
			p.node_host,
			p.default_cluster_id,
			COUNT(DISTINCT i.id) AS issue_count,
			COUNT(DISTINCT s.id) AS session_count,
			MAX(i.updated_at) AS latest_issue_updated_at,
			p.created_at,
			p.updated_at
		FROM projects p
		LEFT JOIN issues i ON i.project_id = p.id AND COALESCE(i.parent_issue_id, '') = ''
		LEFT JOIN agent_sessions s ON s.issue_id = i.id
		WHERE p.id = ?
		GROUP BY p.id
	`, projectID)
	var latestIssueUpdatedAt sql.NullString
	err := row.Scan(
		&project.ID,
		&project.Name,
		&project.RepoPath,
		&project.SourceType,
		&project.RemoteURL,
		&project.GitProvider,
		&project.GitOwner,
		&project.GitRepo,
		&project.DefaultBranch,
		&project.DeployCommand,
		&project.ValidationCommand,
		&project.KubeContext,
		&project.KubeconfigPath,
		&project.Namespace,
		&project.ImageRegistryPrefix,
		&project.PreviewDomain,
		&project.IngressClass,
		&project.NodeHost,
		&project.DefaultClusterID,
		&project.IssueCount,
		&project.SessionCount,
		&latestIssueUpdatedAt,
		&project.CreatedAt,
		&project.UpdatedAt,
	)
	if latestIssueUpdatedAt.Valid {
		project.LatestIssueUpdatedAt = latestIssueUpdatedAt.String
	}
	return project, err
}

func (a *app) listClusters() ([]cluster, error) {
	rows, err := a.db.Query(`
		SELECT
			c.id,
			c.name,
			c.kubeconfig_path,
			c.kube_context,
			c.image_registry_prefix,
			c.exposure_mode,
			c.node_host,
			c.preview_domain,
			c.ingress_class,
			c.status,
			c.last_checked_at,
			COUNT(DISTINCT p.id) AS project_count,
			COUNT(DISTINCT e.issue_id) AS environment_count,
			c.created_at,
			c.updated_at
		FROM clusters c
		LEFT JOIN projects p ON p.default_cluster_id = c.id
		LEFT JOIN issue_test_environments e ON e.cluster_id = c.id
		GROUP BY c.id
		ORDER BY c.updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	clusters := make([]cluster, 0)
	for rows.Next() {
		var cluster cluster
		if err := rows.Scan(&cluster.ID, &cluster.Name, &cluster.KubeconfigPath, &cluster.KubeContext, &cluster.ImageRegistryPrefix, &cluster.ExposureMode, &cluster.NodeHost, &cluster.PreviewDomain, &cluster.IngressClass, &cluster.Status, &cluster.LastCheckedAt, &cluster.ProjectCount, &cluster.EnvironmentCount, &cluster.CreatedAt, &cluster.UpdatedAt); err != nil {
			return nil, err
		}
		clusters = append(clusters, cluster)
	}
	return clusters, rows.Err()
}

func (a *app) loadCluster(clusterID string) (cluster, error) {
	var cluster cluster
	row := a.db.QueryRow(`
		SELECT
			c.id,
			c.name,
			c.kubeconfig_path,
			c.kube_context,
			c.image_registry_prefix,
			c.exposure_mode,
			c.node_host,
			c.preview_domain,
			c.ingress_class,
			c.status,
			c.last_checked_at,
			COUNT(DISTINCT p.id) AS project_count,
			COUNT(DISTINCT e.issue_id) AS environment_count,
			c.created_at,
			c.updated_at
		FROM clusters c
		LEFT JOIN projects p ON p.default_cluster_id = c.id
		LEFT JOIN issue_test_environments e ON e.cluster_id = c.id
		WHERE c.id = ?
		GROUP BY c.id
	`, clusterID)
	err := row.Scan(&cluster.ID, &cluster.Name, &cluster.KubeconfigPath, &cluster.KubeContext, &cluster.ImageRegistryPrefix, &cluster.ExposureMode, &cluster.NodeHost, &cluster.PreviewDomain, &cluster.IngressClass, &cluster.Status, &cluster.LastCheckedAt, &cluster.ProjectCount, &cluster.EnvironmentCount, &cluster.CreatedAt, &cluster.UpdatedAt)
	return cluster, err
}

func (a *app) insertCluster(cluster cluster) error {
	_, err := a.db.Exec(`
		INSERT INTO clusters (id, name, kubeconfig_path, kube_context, image_registry_prefix, exposure_mode, node_host, preview_domain, ingress_class, status, last_checked_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, cluster.ID, cluster.Name, cluster.KubeconfigPath, cluster.KubeContext, cluster.ImageRegistryPrefix, cluster.ExposureMode, cluster.NodeHost, cluster.PreviewDomain, cluster.IngressClass, cluster.Status, cluster.LastCheckedAt, cluster.CreatedAt, cluster.UpdatedAt)
	return err
}

func (a *app) updateCluster(cluster cluster) error {
	_, err := a.db.Exec(`
		UPDATE clusters
		SET name = ?, kubeconfig_path = ?, kube_context = ?, image_registry_prefix = ?, exposure_mode = ?, node_host = ?, preview_domain = ?, ingress_class = ?, status = ?, last_checked_at = ?, updated_at = ?
		WHERE id = ?
	`, cluster.Name, cluster.KubeconfigPath, cluster.KubeContext, cluster.ImageRegistryPrefix, cluster.ExposureMode, cluster.NodeHost, cluster.PreviewDomain, cluster.IngressClass, cluster.Status, cluster.LastCheckedAt, cluster.UpdatedAt, cluster.ID)
	return err
}

func (a *app) loadClusterByKubeconfig(kubeconfigPath, kubeContext string) (cluster, error) {
	var cluster cluster
	row := a.db.QueryRow(`
		SELECT id, name, kubeconfig_path, kube_context, image_registry_prefix, exposure_mode, node_host, preview_domain, ingress_class, status, last_checked_at, created_at, updated_at
		FROM clusters
		WHERE kubeconfig_path = ? AND kube_context = ?
		ORDER BY updated_at DESC
		LIMIT 1
	`, kubeconfigPath, kubeContext)
	err := row.Scan(&cluster.ID, &cluster.Name, &cluster.KubeconfigPath, &cluster.KubeContext, &cluster.ImageRegistryPrefix, &cluster.ExposureMode, &cluster.NodeHost, &cluster.PreviewDomain, &cluster.IngressClass, &cluster.Status, &cluster.LastCheckedAt, &cluster.CreatedAt, &cluster.UpdatedAt)
	return cluster, err
}

func (a *app) importKubeconfigs(paths []string) (kubeconfigImportResult, error) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return kubeconfigImportResult{}, errors.New("kubectl is not available on PATH")
	}

	result := kubeconfigImportResult{
		Imported: []cluster{},
		Skipped:  []kubeconfigImportSkip{},
	}
	for _, kubeconfigPath := range uniqueStrings(paths) {
		normalizedPath, err := normalizeKubeconfigPath(kubeconfigPath)
		if err != nil {
			result.Skipped = append(result.Skipped, kubeconfigImportSkip{Path: kubeconfigPath, Reason: err.Error()})
			continue
		}
		contexts, err := kubeconfigContexts(normalizedPath)
		if err != nil {
			result.Skipped = append(result.Skipped, kubeconfigImportSkip{Path: normalizedPath, Reason: err.Error()})
			continue
		}
		if len(contexts) == 0 {
			result.Skipped = append(result.Skipped, kubeconfigImportSkip{Path: normalizedPath, Reason: "no kube contexts found"})
			continue
		}
		for _, kubeContext := range contexts {
			cluster, err := a.importKubeconfigContext(normalizedPath, kubeContext)
			if err != nil {
				result.Skipped = append(result.Skipped, kubeconfigImportSkip{Path: normalizedPath, Context: kubeContext, Reason: err.Error()})
				continue
			}
			result.Imported = append(result.Imported, cluster)
		}
	}
	return result, nil
}

func (a *app) importKubeconfigContext(kubeconfigPath, kubeContext string) (cluster, error) {
	status := "ready"
	if err := validateKubeconfigContext(kubeconfigPath, kubeContext); err != nil {
		status = "unreachable"
	}
	now := nowString()
	existing, err := a.loadClusterByKubeconfig(kubeconfigPath, kubeContext)
	if err == nil {
		existing.Status = status
		existing.LastCheckedAt = now
		existing.UpdatedAt = now
		if existing.ImageRegistryPrefix == "" {
			existing.ImageRegistryPrefix = defaultImportedClusterImageRegistryPrefix
		}
		if existing.ExposureMode == "" {
			existing.ExposureMode = "nodeport"
		}
		if err := a.updateCluster(existing); err != nil {
			return cluster{}, err
		}
		return a.loadCluster(existing.ID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return cluster{}, err
	}

	importedCluster := cluster{
		ID:                  uuid.NewString(),
		Name:                importedClusterName(kubeconfigPath, kubeContext),
		KubeconfigPath:      kubeconfigPath,
		KubeContext:         kubeContext,
		ImageRegistryPrefix: defaultImportedClusterImageRegistryPrefix,
		ExposureMode:        "nodeport",
		Status:              status,
		LastCheckedAt:       now,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := a.insertCluster(importedCluster); err != nil {
		return cluster{}, err
	}
	return a.loadCluster(importedCluster.ID)
}

func (a *app) prepareSessionWorkspace(session agentSession, project project) (agentSession, error) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return session, errors.New("git is not available on PATH")
	}
	if err := ensureGitRepository(gitPath, project.RepoPath); err != nil {
		return session, err
	}

	workdir := plannedSessionWorkdir(a.workdir, project.ID, session.ID)
	if err := os.MkdirAll(filepath.Dir(workdir), 0o755); err != nil {
		return session, fmt.Errorf("create workdir parent: %w", err)
	}
	if _, err := os.Stat(workdir); err == nil {
		return session, fmt.Errorf("session workdir already exists: %s", workdir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return session, fmt.Errorf("inspect session workdir: %w", err)
	}

	branchExists, err := gitRefExists(gitPath, project.RepoPath, "refs/heads/"+session.Branch)
	if err != nil {
		return session, err
	}
	if branchExists {
		output, err := exec.Command(gitPath, "-C", project.RepoPath, "worktree", "add", workdir, session.Branch).CombinedOutput()
		if err != nil {
			return session, fmt.Errorf("attach existing branch %q: %s", session.Branch, formatCommandFailure(err, output))
		}
	} else {
		baseRef, err := resolveBaseRef(gitPath, project.RepoPath, project.DefaultBranch)
		if err != nil {
			return session, err
		}
		output, err := exec.Command(gitPath, "-C", project.RepoPath, "worktree", "add", "--detach", workdir, baseRef).CombinedOutput()
		if err != nil {
			return session, fmt.Errorf("create worktree from %q: %s", baseRef, formatCommandFailure(err, output))
		}
		output, err = exec.Command(gitPath, "-C", workdir, "checkout", "-b", session.Branch).CombinedOutput()
		if err != nil {
			_ = removeWorktree(gitPath, project.RepoPath, workdir)
			return session, fmt.Errorf("create branch %q: %s", session.Branch, formatCommandFailure(err, output))
		}
	}

	session.Workdir = workdir
	return session, nil
}

func (a *app) loadSessionDetail(sessionID string) (sessionDetail, error) {
	detail := sessionDetail{
		Logs:     []sessionLog{},
		Evidence: []deploymentEvidence{},
		Workspace: workspaceSnapshot{
			StatusLines: []string{},
			Changes:     []workspaceChange{},
			Comparison: workspaceComparison{
				CommitLines: []string{},
				Changes:     []workspaceChange{},
			},
		},
	}
	row := a.db.QueryRow(`
		SELECT s.id, s.issue_id, s.provider, s.agent_profile, s.runtime_mode, s.command, s.status, s.branch, s.workdir, s.codex_thread_id, s.codex_turn_id, s.agent_status, s.artifact_dir, s.cleanup_status, s.cleaned_at, s.created_at, s.updated_at,
		       i.id, i.project_id, COALESCE(i.parent_issue_id, ''), i.sort_order, i.title, i.body, i.status, i.triage_status, i.assignee, i.assignee_type, i.environment_url, i.created_at, i.updated_at,
		       p.id, p.name, p.repo_path, p.source_type, p.remote_url, p.git_provider, p.git_owner, p.git_repo, p.default_branch, p.deploy_command, p.validation_command, p.kube_context, p.kubeconfig_path, p.namespace, p.image_registry_prefix, p.preview_domain, p.ingress_class, p.node_host, p.default_cluster_id, p.created_at, p.updated_at
		FROM agent_sessions s
		JOIN issues i ON i.id = s.issue_id
		JOIN projects p ON p.id = i.project_id
		WHERE s.id = ?
	`, sessionID)
	if err := row.Scan(
		&detail.Session.ID, &detail.Session.IssueID, &detail.Session.Provider, &detail.Session.AgentProfile, &detail.Session.RuntimeMode, &detail.Session.Command, &detail.Session.Status, &detail.Session.Branch, &detail.Session.Workdir, &detail.Session.CodexThreadID, &detail.Session.CodexTurnID, &detail.Session.AgentStatus, &detail.Session.ArtifactDir, &detail.Session.CleanupStatus, &detail.Session.CleanedAt, &detail.Session.CreatedAt, &detail.Session.UpdatedAt,
		&detail.Issue.ID, &detail.Issue.ProjectID, &detail.Issue.ParentIssueID, &detail.Issue.SortOrder, &detail.Issue.Title, &detail.Issue.Body, &detail.Issue.Status, &detail.Issue.TriageStatus, &detail.Issue.Assignee, &detail.Issue.AssigneeType, &detail.Issue.EnvironmentURL, &detail.Issue.CreatedAt, &detail.Issue.UpdatedAt,
		&detail.Project.ID, &detail.Project.Name, &detail.Project.RepoPath, &detail.Project.SourceType, &detail.Project.RemoteURL, &detail.Project.GitProvider, &detail.Project.GitOwner, &detail.Project.GitRepo, &detail.Project.DefaultBranch, &detail.Project.DeployCommand, &detail.Project.ValidationCommand, &detail.Project.KubeContext, &detail.Project.KubeconfigPath, &detail.Project.Namespace, &detail.Project.ImageRegistryPrefix, &detail.Project.PreviewDomain, &detail.Project.IngressClass, &detail.Project.NodeHost, &detail.Project.DefaultClusterID, &detail.Project.CreatedAt, &detail.Project.UpdatedAt,
	); err != nil {
		return detail, err
	}

	logs, err := a.listSessionLogs(sessionID)
	if err != nil {
		return detail, err
	}
	detail.Logs = logs

	evidenceRows, err := a.db.Query(`
		SELECT id, issue_id, session_id, cluster, namespace, summary, details, created_at
		FROM deployment_evidence
		WHERE session_id = ?
		ORDER BY created_at DESC
	`, sessionID)
	if err != nil {
		return detail, err
	}
	defer evidenceRows.Close()

	for evidenceRows.Next() {
		var evidence deploymentEvidence
		if err := evidenceRows.Scan(&evidence.ID, &evidence.IssueID, &evidence.SessionID, &evidence.Cluster, &evidence.Namespace, &evidence.Summary, &evidence.Details, &evidence.CreatedAt); err != nil {
			return detail, err
		}
		detail.Evidence = append(detail.Evidence, evidence)
	}

	detail.Workspace = inspectWorkspace(detail.Session.Workdir, detail.Project.DefaultBranch)
	if detail.Session.CleanupStatus == "cleaned" && !detail.Workspace.Exists {
		detail.Workspace.Error = "Session worktree has been cleaned up."
	}

	return detail, nil
}

func (a *app) cleanupSessionWorktree(sessionID string) error {
	detail, err := a.loadSessionDetail(sessionID)
	if err != nil {
		return err
	}
	session := detail.Session
	if session.Status == "queued" || session.Status == "running" {
		return errSessionActive
	}
	if session.CleanupStatus == "cleaned" {
		return nil
	}

	workdir, err := validateSessionWorkdir(a.workdir, session.Workdir)
	if err != nil {
		return err
	}

	cleanupMessage := fmt.Sprintf("Session worktree cleaned up: %s", workdir)
	if _, err := os.Stat(workdir); err == nil {
		gitPath, err := exec.LookPath("git")
		if err != nil {
			return errors.New("git is not available on PATH")
		}
		if err := removeWorktree(gitPath, detail.Project.RepoPath, workdir); err != nil {
			return err
		}
	} else if errors.Is(err, os.ErrNotExist) {
		cleanupMessage = fmt.Sprintf("Session worktree was already missing; marked cleaned: %s", workdir)
	} else {
		return fmt.Errorf("inspect session workdir: %w", err)
	}

	cleanedAt := nowString()
	if _, err := a.db.Exec(`
		UPDATE agent_sessions SET cleanup_status = 'cleaned', cleaned_at = ?, updated_at = ? WHERE id = ?
	`, cleanedAt, cleanedAt, session.ID); err != nil {
		return err
	}
	a.appendSessionLog(session.ID, "system", cleanupMessage)
	a.addSystemComment(session.IssueID, fmt.Sprintf("Session `%s` worktree was cleaned up.", shortID(session.ID)))
	a.broker.publish(session.ID, sessionEvent{Type: "status", Payload: "cleaned"})
	return nil
}

func (a *app) listSessionLogs(sessionID string) ([]sessionLog, error) {
	logRows, err := a.db.Query(`
		SELECT id, session_id, stream, message, created_at
		FROM session_logs
		WHERE session_id = ?
		ORDER BY id ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer logRows.Close()

	logs := []sessionLog{}
	for logRows.Next() {
		var log sessionLog
		if err := logRows.Scan(&log.ID, &log.SessionID, &log.Stream, &log.Message, &log.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, logRows.Err()
}

func (a *app) listComments(issueID string) ([]comment, error) {
	rows, err := a.db.Query(`
		SELECT id, issue_id, author_type, body, created_at
		FROM comments
		WHERE issue_id = ?
		ORDER BY created_at DESC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	comments := make([]comment, 0)
	for rows.Next() {
		var c comment
		if err := rows.Scan(&c.ID, &c.IssueID, &c.AuthorType, &c.Body, &c.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, nil
}

func (a *app) listIssueLabels(issueID string) ([]issueLabel, error) {
	rows, err := a.db.Query(`
		SELECT
			il.id,
			il.issue_id,
			il.label_id,
			COALESCE(ld.key, '') AS key,
			COALESCE(ld.name, il.name) AS name,
			COALESCE(ld.dimension, '') AS dimension,
			COALESCE(ld.color, il.color) AS color,
			COALESCE(ld.sort_order, 999) AS sort_order,
			il.created_at
		FROM issue_labels il
		LEFT JOIN issue_label_definitions ld ON ld.id = il.label_id
		WHERE il.issue_id = ?
		ORDER BY
			CASE COALESCE(ld.dimension, '')
				WHEN 'type' THEN 0
				WHEN 'priority' THEN 1
				ELSE 2
			END,
			COALESCE(ld.sort_order, 999) ASC,
			il.created_at ASC,
			name COLLATE NOCASE ASC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	labels := make([]issueLabel, 0)
	for rows.Next() {
		var label issueLabel
		if err := rows.Scan(&label.ID, &label.IssueID, &label.LabelID, &label.Key, &label.Name, &label.Dimension, &label.Color, &label.SortOrder, &label.CreatedAt); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}

func (a *app) listSessions(issueID string) ([]agentSession, error) {
	rows, err := a.db.Query(`
		SELECT id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, cleanup_status, cleaned_at, created_at, updated_at
		FROM agent_sessions
		WHERE issue_id = ?
		ORDER BY created_at DESC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := make([]agentSession, 0)
	for rows.Next() {
		var s agentSession
		if err := rows.Scan(&s.ID, &s.IssueID, &s.Provider, &s.AgentProfile, &s.RuntimeMode, &s.Command, &s.Status, &s.Branch, &s.Workdir, &s.CodexThreadID, &s.CodexTurnID, &s.AgentStatus, &s.ArtifactDir, &s.CleanupStatus, &s.CleanedAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

func (a *app) listEvidence(issueID string) ([]deploymentEvidence, error) {
	rows, err := a.db.Query(`
		SELECT id, issue_id, session_id, cluster, namespace, summary, details, created_at
		FROM deployment_evidence
		WHERE issue_id = ?
		ORDER BY created_at DESC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	evidence := make([]deploymentEvidence, 0)
	for rows.Next() {
		var e deploymentEvidence
		if err := rows.Scan(&e.ID, &e.IssueID, &e.SessionID, &e.Cluster, &e.Namespace, &e.Summary, &e.Details, &e.CreatedAt); err != nil {
			return nil, err
		}
		evidence = append(evidence, e)
	}
	return evidence, nil
}

func (a *app) loadIssueTestEnvironment(issueID string) (*issueTestEnvironment, error) {
	row := a.db.QueryRow(`
		SELECT issue_id, cluster_id, namespace, namespace_status, cleanup_status, preview_url, image_registry_prefix, kubeconfig_path, kube_context, exposure_mode, preview_domain, ingress_class, node_host, last_deploy_session_id, last_cleanup_session_id, created_at, updated_at
		FROM issue_test_environments
		WHERE issue_id = ?
	`, issueID)
	var environment issueTestEnvironment
	if err := row.Scan(
		&environment.IssueID,
		&environment.ClusterID,
		&environment.Namespace,
		&environment.NamespaceStatus,
		&environment.CleanupStatus,
		&environment.PreviewURL,
		&environment.ImageRegistryPrefix,
		&environment.KubeconfigPath,
		&environment.KubeContext,
		&environment.ExposureMode,
		&environment.PreviewDomain,
		&environment.IngressClass,
		&environment.NodeHost,
		&environment.LastDeploySessionID,
		&environment.LastCleanupSessionID,
		&environment.CreatedAt,
		&environment.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &environment, nil
}

func (a *app) saveIssueTestEnvironment(environment issueTestEnvironment) error {
	now := nowString()
	if environment.CreatedAt == "" {
		environment.CreatedAt = now
	}
	environment.UpdatedAt = now
	_, err := a.db.Exec(`
		INSERT INTO issue_test_environments (issue_id, cluster_id, namespace, namespace_status, cleanup_status, preview_url, image_registry_prefix, kubeconfig_path, kube_context, exposure_mode, preview_domain, ingress_class, node_host, last_deploy_session_id, last_cleanup_session_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(issue_id) DO UPDATE SET
			cluster_id = excluded.cluster_id,
			namespace = excluded.namespace,
			namespace_status = excluded.namespace_status,
			cleanup_status = excluded.cleanup_status,
			preview_url = excluded.preview_url,
			image_registry_prefix = excluded.image_registry_prefix,
			kubeconfig_path = excluded.kubeconfig_path,
			kube_context = excluded.kube_context,
			exposure_mode = excluded.exposure_mode,
			preview_domain = excluded.preview_domain,
			ingress_class = excluded.ingress_class,
			node_host = excluded.node_host,
			last_deploy_session_id = excluded.last_deploy_session_id,
			last_cleanup_session_id = excluded.last_cleanup_session_id,
			updated_at = excluded.updated_at
	`, environment.IssueID, environment.ClusterID, environment.Namespace, environment.NamespaceStatus, environment.CleanupStatus, environment.PreviewURL, environment.ImageRegistryPrefix, environment.KubeconfigPath, environment.KubeContext, environment.ExposureMode, environment.PreviewDomain, environment.IngressClass, environment.NodeHost, environment.LastDeploySessionID, environment.LastCleanupSessionID, environment.CreatedAt, environment.UpdatedAt)
	return err
}

func (a *app) appendSessionLog(sessionID, stream, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	createdAt := nowString()
	_, _ = a.db.Exec(`
		INSERT INTO session_logs (session_id, stream, message, created_at)
		VALUES (?, ?, ?, ?)
	`, sessionID, stream, message, createdAt)
	a.broker.publish(sessionID, sessionEvent{Type: "log", Payload: message})
}

func (a *app) addSystemComment(issueID, body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}
	createdAt := nowString()
	_, _ = a.db.Exec(`
		INSERT INTO comments (id, issue_id, author_type, body, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, uuid.NewString(), issueID, "system", body, createdAt)
	_, _ = a.db.Exec(`
		UPDATE issues SET updated_at = ? WHERE id = ?
	`, createdAt, issueID)
	_, _ = a.db.Exec(`
		UPDATE inbox_items SET updated_at = ?, unread = 1 WHERE issue_id = ?
	`, createdAt, issueID)
	a.publishInboxEvent(issueID, "updated")
}

func (a *app) updateSessionStatus(sessionID, status string) {
	_, _ = a.db.Exec(`
		UPDATE agent_sessions SET status = ?, updated_at = ? WHERE id = ?
	`, status, nowString(), sessionID)
	a.broker.publish(sessionID, sessionEvent{Type: "status", Payload: status})
}

func (a *app) sessionWasCancelled(sessionID string) bool {
	var status string
	if err := a.db.QueryRow(`SELECT status FROM agent_sessions WHERE id = ?`, sessionID).Scan(&status); err != nil {
		return false
	}
	return status == "cancelled"
}

func (a *app) updateSessionAgentStatus(sessionID, agentStatus string) {
	agentStatus = strings.TrimSpace(agentStatus)
	if agentStatus == "" {
		return
	}
	_, _ = a.db.Exec(`
		UPDATE agent_sessions SET agent_status = ?, updated_at = ? WHERE id = ?
	`, agentStatus, nowString(), sessionID)
	a.broker.publish(sessionID, sessionEvent{Type: "status", Payload: agentStatus})
}

func (a *app) updateSessionCodexThread(sessionID, threadID string) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}
	_, _ = a.db.Exec(`
		UPDATE agent_sessions SET codex_thread_id = ?, updated_at = ? WHERE id = ?
	`, threadID, nowString(), sessionID)
	a.broker.publish(sessionID, sessionEvent{Type: "status", Payload: "codex-thread"})
}

func (a *app) updateSessionCodexTurn(sessionID, turnID string) {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return
	}
	_, _ = a.db.Exec(`
		UPDATE agent_sessions SET codex_turn_id = ?, updated_at = ? WHERE id = ?
	`, turnID, nowString(), sessionID)
	a.broker.publish(sessionID, sessionEvent{Type: "status", Payload: "codex-turn"})
}

func (a *app) updateSessionArtifactDir(sessionID, artifactDir string) {
	artifactDir = strings.TrimSpace(artifactDir)
	if artifactDir == "" {
		return
	}
	_, _ = a.db.Exec(`
		UPDATE agent_sessions SET artifact_dir = ?, updated_at = ? WHERE id = ?
	`, artifactDir, nowString(), sessionID)
	a.broker.publish(sessionID, sessionEvent{Type: "status", Payload: "artifacts"})
}

func (a *app) updateIssueStatus(issueID, status string) {
	updatedAt := nowString()
	_, _ = a.db.Exec(`
		UPDATE issues SET status = ?, updated_at = ? WHERE id = ?
	`, status, updatedAt, issueID)
	_, _ = a.db.Exec(`
		UPDATE inbox_items SET status = ?, unread = 1, updated_at = ? WHERE issue_id = ?
	`, status, updatedAt, issueID)
	a.publishInboxEvent(issueID, status)
}

func (a *app) updateIssueAssignment(issueID, assignee, assigneeType, status string) error {
	assignee = strings.TrimSpace(assignee)
	assigneeType = normalizeAssigneeType(assigneeType)
	status = strings.TrimSpace(status)
	if assignee == "" {
		return errors.New("assignee cannot be empty")
	}
	if assigneeType != "human" && assigneeType != "agent" {
		return errors.New("assignee type must be human or agent")
	}
	if status == "" {
		return errors.New("issue status cannot be empty")
	}

	updatedAt := nowString()
	if _, err := a.db.Exec(`
		UPDATE issues SET assignee = ?, assignee_type = ?, status = ?, updated_at = ? WHERE id = ?
	`, assignee, assigneeType, status, updatedAt, issueID); err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		UPDATE inbox_items SET status = ?, unread = 1, updated_at = ? WHERE issue_id = ?
	`, status, updatedAt, issueID); err != nil {
		return err
	}
	a.publishInboxEvent(issueID, status)
	return nil
}

func (a *app) publishInboxEvent(issueID, status string) {
	a.broker.publish("inbox", sessionEvent{Type: "inbox", Payload: strings.TrimSpace(issueID + " " + status)})
}

func (a *app) storeEvidence(evidence deploymentEvidence) {
	_, _ = a.db.Exec(`
		INSERT INTO deployment_evidence (id, issue_id, session_id, cluster, namespace, summary, details, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, evidence.ID, evidence.IssueID, evidence.SessionID, evidence.Cluster, evidence.Namespace, evidence.Summary, evidence.Details, evidence.CreatedAt)
	a.addSystemComment(evidence.IssueID, fmt.Sprintf(
		"Kubernetes evidence captured for session `%s` in `%s/%s`.\n\n%s",
		shortID(evidence.SessionID),
		clusterLabel(evidence.Cluster),
		evidence.Namespace,
		evidence.Summary,
	))
	a.broker.publish(evidence.SessionID, sessionEvent{Type: "status", Payload: "evidence"})
}

type sessionEvent struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

func jsonMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func shortID(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

func formatIssueLabels(labels []issueLabel) string {
	if len(labels) == 0 {
		return "none"
	}
	names := make([]string, 0, len(labels))
	for _, label := range labels {
		if strings.TrimSpace(label.Name) == "" {
			continue
		}
		names = append(names, label.Name)
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

func writeIssueTaskList(builder *strings.Builder, childIssues []issueListItem) {
	builder.WriteString("## Task List\n\n")
	builder.WriteString("Task list items are child issues. Treat these rows as the source of truth instead of Markdown checkbox text in the issue body.\n")
	builder.WriteString("When this session needs to create, check, or delete tasks, use the mspace API if `MSPACE_API_BASE_URL` is available: `POST /api/issues/${MSPACE_ISSUE_ID}/tasks` to add a task, `PUT /api/issues/<task-id>` with `{\"status\":\"completed\"}` to check one off, and `DELETE /api/issues/${MSPACE_ISSUE_ID}/tasks/<task-id>` to remove an obsolete task.\n\n")
	if len(childIssues) == 0 {
		builder.WriteString("(no child issue tasks yet)\n\n")
		return
	}
	for _, task := range childIssues {
		marker := "[ ]"
		if task.Status == "completed" {
			marker = "[x]"
		}
		builder.WriteString(fmt.Sprintf("- %s %s (`%s`, status: %s)\n", marker, task.Title, task.ID, task.Status))
	}
	builder.WriteString("\n")
}

func plannedSessionWorkdir(root, projectID, sessionID string) string {
	return filepath.Join(root, projectID, sessionID)
}

func validateSessionWorkdir(root, workdir string) (string, error) {
	root = strings.TrimSpace(root)
	workdir = strings.TrimSpace(workdir)
	if root == "" || workdir == "" {
		return "", errUnsafeSessionWorkdir
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absWorkdir, err := filepath.Abs(workdir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, absWorkdir)
	if err != nil {
		return "", err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errUnsafeSessionWorkdir
	}
	return absWorkdir, nil
}

func (a *app) normalizeProjectInput(input projectInput) (project, error) {
	name := strings.TrimSpace(input.Name)
	sourceType := normalizeSourceType(input.SourceType)
	repoPath := strings.TrimSpace(input.RepoPath)
	repoURL := strings.TrimSpace(input.RepoURL)
	if repoURL == "" && looksLikeGitRemoteURL(repoPath) {
		repoURL = repoPath
		repoPath = ""
	}
	if sourceType == "" {
		if repoURL != "" {
			sourceType = "github"
		} else {
			sourceType = "local"
		}
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return project{}, errors.New("git is not available on PATH")
	}

	remoteURL := ""
	gitInfo := gitRemoteInfo{}
	nameCandidate := ""
	switch sourceType {
	case "local":
		var err error
		repoPath, remoteURL, gitInfo, nameCandidate, err = normalizeLocalProjectRepository(gitPath, repoPath)
		if err != nil {
			return project{}, err
		}
	case "github":
		var err error
		repoPath, remoteURL, gitInfo, nameCandidate, err = a.normalizeGitHubProjectRepository(gitPath, repoURL)
		if err != nil {
			return project{}, err
		}
	default:
		return project{}, errors.New("project source must be local or github")
	}

	if name == "" {
		name = nameCandidate
	}
	if name == "" {
		name = filepath.Base(repoPath)
	}

	defaultBranch := strings.TrimSpace(input.DefaultBranch)
	if defaultBranch == "" {
		defaultBranch = detectDefaultBranch(gitPath, repoPath)
	}
	defaultClusterID := strings.TrimSpace(input.DefaultClusterID)
	if defaultClusterID != "" {
		if _, err := a.loadCluster(defaultClusterID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return project{}, errClusterNotFound
			}
			return project{}, err
		}
	}

	return project{
		Name:                name,
		RepoPath:            repoPath,
		SourceType:          sourceType,
		RemoteURL:           remoteURL,
		GitProvider:         gitInfo.Provider,
		GitOwner:            gitInfo.Owner,
		GitRepo:             gitInfo.Repo,
		DefaultBranch:       defaultBranch,
		DeployCommand:       strings.TrimSpace(input.DeployCommand),
		ValidationCommand:   strings.TrimSpace(input.ValidationCommand),
		KubeContext:         strings.TrimSpace(input.KubeContext),
		KubeconfigPath:      strings.TrimSpace(input.KubeconfigPath),
		Namespace:           strings.TrimSpace(input.Namespace),
		ImageRegistryPrefix: strings.TrimSpace(input.ImageRegistryPrefix),
		PreviewDomain:       strings.TrimSpace(input.PreviewDomain),
		IngressClass:        strings.TrimSpace(input.IngressClass),
		NodeHost:            strings.TrimSpace(input.NodeHost),
		DefaultClusterID:    defaultClusterID,
	}, nil
}

func normalizeClusterInput(existing cluster, input clusterInput) (cluster, error) {
	exposureMode, err := normalizeExposureMode(input.ExposureMode)
	if err != nil {
		return cluster{}, err
	}
	if exposureMode == "" {
		exposureMode = "nodeport"
	}

	name := strings.TrimSpace(input.Name)
	kubeconfigPath := strings.TrimSpace(input.KubeconfigPath)
	imageRegistryPrefix := strings.TrimSpace(input.ImageRegistryPrefix)
	previewDomain := strings.TrimSpace(input.PreviewDomain)
	if name == "" {
		return cluster{}, errors.New("cluster name is required")
	}
	if kubeconfigPath == "" {
		return cluster{}, errors.New("kubeconfig path is required")
	}
	if imageRegistryPrefix == "" {
		return cluster{}, errors.New("image registry prefix is required")
	}
	if exposureMode == "ingress" && previewDomain == "" {
		return cluster{}, errors.New("preview domain is required when ingress is the default exposure mode")
	}

	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = existing.Status
	}
	if status == "" {
		status = "configured"
	}

	return cluster{
		ID:                  existing.ID,
		Name:                name,
		KubeconfigPath:      kubeconfigPath,
		KubeContext:         strings.TrimSpace(input.KubeContext),
		ImageRegistryPrefix: imageRegistryPrefix,
		ExposureMode:        exposureMode,
		NodeHost:            strings.TrimSpace(input.NodeHost),
		PreviewDomain:       previewDomain,
		IngressClass:        strings.TrimSpace(input.IngressClass),
		Status:              status,
		LastCheckedAt:       existing.LastCheckedAt,
		CreatedAt:           existing.CreatedAt,
		UpdatedAt:           existing.UpdatedAt,
	}, nil
}

func normalizeKubeconfigImportPaths(input kubeconfigImportRequest) []string {
	paths := append([]string{}, input.Paths...)
	if strings.TrimSpace(input.Path) != "" {
		paths = append(paths, input.Path)
	}
	return uniqueStrings(paths)
}

func discoverKubeconfigs(paths []string) kubeconfigDiscoveryResult {
	result := kubeconfigDiscoveryResult{
		Candidates: []kubeconfigCandidate{},
		Skipped:    []kubeconfigImportSkip{},
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		result.Skipped = append(result.Skipped, kubeconfigImportSkip{Reason: "kubectl is not available on PATH"})
		return result
	}
	for _, kubeconfigPath := range uniqueStrings(paths) {
		normalizedPath, err := normalizeKubeconfigPath(kubeconfigPath)
		if err != nil {
			result.Skipped = append(result.Skipped, kubeconfigImportSkip{Path: kubeconfigPath, Reason: err.Error()})
			continue
		}
		contexts, err := kubeconfigContexts(normalizedPath)
		if err != nil {
			result.Skipped = append(result.Skipped, kubeconfigImportSkip{Path: normalizedPath, Reason: err.Error()})
			continue
		}
		if len(contexts) == 0 {
			result.Skipped = append(result.Skipped, kubeconfigImportSkip{Path: normalizedPath, Reason: "no kube contexts found"})
			continue
		}
		result.Candidates = append(result.Candidates, kubeconfigCandidate{
			Path:     normalizedPath,
			Contexts: contexts,
		})
	}
	return result
}

func discoverDefaultKubeconfigPaths(kubeDir string) ([]string, error) {
	entries, err := os.ReadDir(kubeDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read default kube directory: %w", err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(kubeDir, entry.Name())
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func normalizeKubeconfigPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("kubeconfig path is empty")
	}
	if strings.HasPrefix(path, "~/") {
		path = filepath.Join(userHomeDir(), strings.TrimPrefix(path, "~/"))
	}
	if !filepath.IsAbs(path) {
		absolutePath, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve kubeconfig path: %w", err)
		}
		path = absolutePath
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("kubeconfig file does not exist")
		}
		return "", fmt.Errorf("read kubeconfig file: %w", err)
	}
	if info.IsDir() {
		return "", errors.New("kubeconfig path points to a directory")
	}
	return path, nil
}

type kubeconfigView struct {
	CurrentContext string `json:"current-context"`
	Contexts       []struct {
		Name string `json:"name"`
	} `json:"contexts"`
}

func kubeconfigContexts(kubeconfigPath string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "kubectl", "--kubeconfig", kubeconfigPath, "config", "view", "-o", "json").CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, errors.New("kubectl config view timed out")
	}
	if err != nil {
		return nil, fmt.Errorf("kubectl config view failed: %s", truncate(strings.TrimSpace(string(output)), 240))
	}

	var view kubeconfigView
	if err := json.Unmarshal(output, &view); err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}

	contexts := make([]string, 0, len(view.Contexts)+1)
	for _, item := range view.Contexts {
		if name := strings.TrimSpace(item.Name); name != "" {
			contexts = append(contexts, name)
		}
	}
	contexts = uniqueStrings(contexts)
	currentContext := strings.TrimSpace(view.CurrentContext)
	if currentContext == "" {
		return contexts, nil
	}
	filtered := make([]string, 0, len(contexts))
	for _, name := range contexts {
		if name != currentContext {
			filtered = append(filtered, name)
		}
	}
	sort.Strings(filtered)
	return append([]string{currentContext}, filtered...), nil
}

func validateKubeconfigContext(kubeconfigPath, kubeContext string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	args := []string{"--kubeconfig", kubeconfigPath}
	if strings.TrimSpace(kubeContext) != "" {
		args = append(args, "--context", kubeContext)
	}
	args = append(args, "--request-timeout=5s", "get", "--raw=/version")
	output, err := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return errors.New("cluster version check timed out")
	}
	if err != nil {
		return fmt.Errorf("cluster version check failed: %s", truncate(strings.TrimSpace(string(output)), 240))
	}
	return nil
}

func importedClusterName(kubeconfigPath, kubeContext string) string {
	if strings.TrimSpace(kubeContext) != "" {
		return strings.Join(strings.Fields(kubeContext), " ")
	}
	base := strings.TrimSuffix(filepath.Base(kubeconfigPath), filepath.Ext(kubeconfigPath))
	if base == "" {
		return filepath.Base(kubeconfigPath)
	}
	return base
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func normalizeExposureMode(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "default":
		return "", nil
	case "nodeport", "node-port", "node_port":
		return "nodeport", nil
	case "ingress":
		return "ingress", nil
	default:
		return "", fmt.Errorf("unsupported exposure mode %q", value)
	}
}

func normalizeSourceType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "local", "github":
		return value
	case "github-url", "github_repo", "github-repo":
		return "github"
	default:
		return value
	}
}

func normalizeAssigneeType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "human", "agent":
		return value
	default:
		return value
	}
}

func defaultAgentProfiles(now string) []agentProfile {
	return []agentProfile{
		{
			ID:          "triage",
			Name:        "Triage",
			Mention:     "@triage",
			Provider:    "codex",
			Description: "Automatic issue type classification for new issues.",
			Instructions: strings.TrimSpace(`
Use the triage profile only for classification. Read the issue title, body, and project context, then choose exactly one Conventional Commit type: feat, fix, docs, style, refactor, perf, test, build, ci, chore, or revert. Do not assign priority, change status, or perform implementation work.
`),
			Enabled:   false,
			BuiltIn:   true,
			SortOrder: 5,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:          "codex",
			Name:        "Codex",
			Mention:     "@codex",
			Provider:    "codex",
			Description: "General implementation, explanation, and follow-up work.",
			Instructions: strings.TrimSpace(`
Use the general Codex profile. Answer questions directly, make focused code changes when requested, and choose the smallest practical path through implementation and validation.
`),
			Enabled:   true,
			BuiltIn:   true,
			SortOrder: 10,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:          "bugfix",
			Name:        "Bugfix",
			Mention:     "@bugfix",
			Provider:    "codex",
			Description: "Debugging, reproduction, root cause analysis, and narrow fixes.",
			Instructions: strings.TrimSpace(`
Use the bugfix profile. Start by identifying the failing behavior and the most likely root cause. Reproduce or inspect the real path before editing when practical. Keep the fix narrow, add or update regression coverage when the codebase supports it, and report the exact validation result.
`),
			Enabled:   true,
			BuiltIn:   true,
			SortOrder: 20,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:          "design",
			Name:        "Design",
			Mention:     "@design",
			Provider:    "codex",
			Description: "UI, UX, interaction polish, and product surface decisions.",
			Instructions: strings.TrimSpace(`
Use the design profile. Improve product UI with the existing design system and the Issue/document-first mspace direction. Keep the interface quiet, text-led, keyboard-accessible, and avoid dashboard/card-heavy patterns unless they are already the local convention for that surface.
`),
			Enabled:   true,
			BuiltIn:   true,
			SortOrder: 30,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
}

func (a *app) seedDefaultAgentProfiles() error {
	now := nowString()
	for _, profile := range defaultAgentProfiles(now) {
		if _, err := a.db.Exec(`
			INSERT OR IGNORE INTO agent_profiles (id, name, mention, provider, description, instructions, enabled, built_in, sort_order, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, profile.ID, profile.Name, profile.Mention, profile.Provider, profile.Description, profile.Instructions, boolToInt(profile.Enabled), boolToInt(profile.BuiltIn), profile.SortOrder, profile.CreatedAt, profile.UpdatedAt); err != nil {
			return fmt.Errorf("seed agent profile %s: %w", profile.ID, err)
		}
	}
	return nil
}

func (a *app) listAgentProfiles() ([]agentProfile, error) {
	rows, err := a.db.Query(`
		SELECT id, name, mention, provider, description, instructions, enabled, built_in, sort_order, created_at, updated_at
		FROM agent_profiles
		ORDER BY sort_order ASC, created_at ASC, name COLLATE NOCASE ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	profiles := []agentProfile{}
	for rows.Next() {
		profile, err := scanAgentProfile(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

func (a *app) loadAgentProfile(value string) (agentProfile, error) {
	key := agentProfileLookupKey(value)
	if key == "" {
		key = "codex"
	}
	mention := "@" + key
	row := a.db.QueryRow(`
		SELECT id, name, mention, provider, description, instructions, enabled, built_in, sort_order, created_at, updated_at
		FROM agent_profiles
		WHERE lower(id) = ? OR lower(mention) = ?
	`, key, mention)
	return scanAgentProfile(row)
}

func (a *app) resolveEnabledAgentProfile(value string) (agentProfile, error) {
	profile, err := a.loadAgentProfile(value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return agentProfile{}, fmt.Errorf("%w %q", errUnknownAgentProfile, value)
		}
		return agentProfile{}, err
	}
	if !profile.Enabled {
		return agentProfile{}, fmt.Errorf("%w %q is disabled", errUnknownAgentProfile, value)
	}
	return profile, nil
}

type agentProfileScanner interface {
	Scan(dest ...any) error
}

func scanAgentProfile(scanner agentProfileScanner) (agentProfile, error) {
	var profile agentProfile
	var enabled int
	var builtIn int
	if err := scanner.Scan(&profile.ID, &profile.Name, &profile.Mention, &profile.Provider, &profile.Description, &profile.Instructions, &enabled, &builtIn, &profile.SortOrder, &profile.CreatedAt, &profile.UpdatedAt); err != nil {
		return agentProfile{}, err
	}
	profile.Enabled = enabled == 1
	profile.BuiltIn = builtIn == 1
	return profile, nil
}

func normalizeAgentProfileInput(existing agentProfile, input agentProfileInput, isUpdate bool) (agentProfile, error) {
	profile := existing
	profile.Name = strings.Join(strings.Fields(strings.TrimSpace(input.Name)), " ")
	if profile.Name == "" {
		return agentProfile{}, errors.New("agent name is required")
	}

	mentionInput := input.Mention
	if strings.TrimSpace(mentionInput) == "" {
		mentionInput = profile.Name
	}
	mention, err := normalizeAgentMention(mentionInput)
	if err != nil {
		return agentProfile{}, err
	}
	profile.Mention = mention
	if !isUpdate {
		profile.ID = strings.TrimPrefix(mention, "@")
		profile.BuiltIn = false
		profile.SortOrder = 100
	}

	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	if provider == "" && isUpdate {
		provider = existing.Provider
	}
	if provider == "" {
		provider = "codex"
	}
	if provider != "codex" {
		return agentProfile{}, errors.New("only the codex provider is supported right now")
	}
	profile.Provider = provider

	profile.Description = strings.TrimSpace(input.Description)
	profile.Instructions = strings.TrimSpace(input.Instructions)
	if profile.Instructions == "" {
		return agentProfile{}, errors.New("agent instructions are required")
	}

	if input.Enabled == nil {
		profile.Enabled = !isUpdate || existing.Enabled
	} else {
		profile.Enabled = *input.Enabled
	}
	return profile, nil
}

func normalizeAgentMention(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "@")))
	value = strings.ReplaceAll(value, " ", "-")
	if value == "" {
		return "", errors.New("agent mention is required")
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return "", fmt.Errorf("agent mention %q can only use letters, numbers, hyphen, or underscore", value)
	}
	return "@" + value, nil
}

func agentProfileLookupKey(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "@")))
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func isCodexProvider(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "codex")
}

func normalizeLocalProjectRepository(gitPath, repoPath string) (string, string, gitRemoteInfo, string, error) {
	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		return "", "", gitRemoteInfo{}, "", errors.New("repo path cannot be empty")
	}
	if !filepath.IsAbs(repoPath) {
		return "", "", gitRemoteInfo{}, "", errors.New("repo path must be an absolute path")
	}
	repoInfo, err := os.Stat(repoPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", gitRemoteInfo{}, "", errors.New("repo path does not exist")
		}
		return "", "", gitRemoteInfo{}, "", fmt.Errorf("repo path validation failed: %w", err)
	}
	if !repoInfo.IsDir() {
		return "", "", gitRemoteInfo{}, "", errors.New("repo path must point to a directory")
	}
	if err := ensureGitRepository(gitPath, repoPath); err != nil {
		return "", "", gitRemoteInfo{}, "", err
	}

	remoteURL := detectRemoteURL(gitPath, repoPath)
	gitInfo, _ := parseGitRemoteInfo(remoteURL)
	return repoPath, remoteURL, gitInfo, filepath.Base(repoPath), nil
}

func (a *app) normalizeGitHubProjectRepository(gitPath, repoURL string) (string, string, gitRemoteInfo, string, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return "", "", gitRemoteInfo{}, "", errors.New("github repo url cannot be empty")
	}

	gitInfo, ok := parseGitRemoteInfo(repoURL)
	if !ok || gitInfo.Provider != "github" {
		return "", "", gitRemoteInfo{}, "", errors.New("github repo url must point to a GitHub repository")
	}

	repoPath := filepath.Join(a.repoRootPath(), safePathPart(gitInfo.Owner), safePathPart(gitInfo.Repo))
	if err := ensureClonedRepository(gitPath, repoURL, repoPath); err != nil {
		return "", "", gitRemoteInfo{}, "", err
	}
	return repoPath, repoURL, gitInfo, gitInfo.Repo, nil
}

func (a *app) repoRootPath() string {
	if a != nil && a.repoRoot != "" {
		return a.repoRoot
	}
	return filepath.Join(userHomeDir(), ".mspace", "repos")
}

func ensureClonedRepository(gitPath, repoURL, repoPath string) error {
	if repoInfo, err := os.Stat(repoPath); err == nil {
		if !repoInfo.IsDir() {
			return fmt.Errorf("clone path exists and is not a directory: %s", repoPath)
		}
		if err := ensureGitRepository(gitPath, repoPath); err != nil {
			return fmt.Errorf("clone path exists but is not a git work tree: %w", err)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect clone path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(repoPath), 0o755); err != nil {
		return fmt.Errorf("create clone parent: %w", err)
	}
	output, err := exec.Command(gitPath, "clone", repoURL, repoPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("clone github repo: %s", formatCommandFailure(err, output))
	}
	return nil
}

func detectRemoteURL(gitPath, repoPath string) string {
	if output, err := exec.Command(gitPath, "-C", repoPath, "remote", "get-url", "origin").CombinedOutput(); err == nil {
		return strings.TrimSpace(string(output))
	}
	output, err := exec.Command(gitPath, "-C", repoPath, "remote").CombinedOutput()
	if err != nil {
		return ""
	}
	remotes := strings.Fields(string(output))
	if len(remotes) == 0 {
		return ""
	}
	output, err = exec.Command(gitPath, "-C", repoPath, "remote", "get-url", remotes[0]).CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func detectDefaultBranch(gitPath, repoPath string) string {
	if output, err := exec.Command(gitPath, "-C", repoPath, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD").CombinedOutput(); err == nil {
		branch := strings.TrimSpace(string(output))
		if branch != "" {
			return strings.TrimPrefix(branch, "origin/")
		}
	}
	if output, err := exec.Command(gitPath, "-C", repoPath, "branch", "--show-current").CombinedOutput(); err == nil {
		branch := strings.TrimSpace(string(output))
		if branch != "" {
			return branch
		}
	}
	return "main"
}

func looksLikeGitRemoteURL(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "https://") ||
		strings.HasPrefix(value, "http://") ||
		strings.HasPrefix(value, "ssh://") ||
		strings.HasPrefix(value, "git@") ||
		strings.HasPrefix(value, "github.com/")
}

func parseGitRemoteInfo(remoteURL string) (gitRemoteInfo, bool) {
	value := strings.TrimSpace(strings.TrimPrefix(remoteURL, "git+"))
	if value == "" {
		return gitRemoteInfo{}, false
	}

	host := ""
	repoPath := ""
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		host = parsed.Host
		repoPath = parsed.Path
	} else if strings.Contains(value, "@") && strings.Contains(value, ":") {
		afterAt := value[strings.LastIndex(value, "@")+1:]
		parts := strings.SplitN(afterAt, ":", 2)
		if len(parts) == 2 {
			host = parts[0]
			repoPath = parts[1]
		}
	} else if strings.HasPrefix(value, "github.com/") {
		host = "github.com"
		repoPath = strings.TrimPrefix(value, "github.com/")
	}
	host = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(host)), "www.")
	repoPath = strings.Trim(strings.TrimSpace(repoPath), "/")
	repoPath = strings.TrimSuffix(repoPath, ".git")
	parts := strings.Split(repoPath, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return gitRemoteInfo{}, false
	}

	provider := host
	if host == "github.com" {
		provider = "github"
	}
	return gitRemoteInfo{
		Provider: provider,
		Owner:    parts[0],
		Repo:     parts[1],
	}, true
}

func safePathPart(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "..", "")
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, "\\", "-")
	if value == "" {
		return "repo"
	}
	return value
}

func buildSessionCommand(issueID string, project project, override string) string {
	override = strings.TrimSpace(override)
	if override != "" {
		return override
	}

	steps := []string{
		"set -e",
		"set -o pipefail",
		fmt.Sprintf("printf '%%s\\n' %s", shellQuote(fmt.Sprintf("Issue: %s", issueID))),
		fmt.Sprintf("printf '%%s\\n' %s", shellQuote(fmt.Sprintf("Project: %s", project.Name))),
		fmt.Sprintf("printf '%%s\\n' %s", shellQuote(fmt.Sprintf("Repo: %s", project.RepoPath))),
		"printf '%s\n' \"Context: ${MSPACE_SESSION_CONTEXT:-not available}\"",
	}

	hasWorkflowStep := false
	if project.DeployCommand != "" {
		hasWorkflowStep = true
		steps = append(
			steps,
			"",
			fmt.Sprintf("printf '%%s\\n' %s", shellQuote("==> Deploy")),
			project.DeployCommand,
		)
	}
	if project.ValidationCommand != "" {
		hasWorkflowStep = true
		steps = append(
			steps,
			"",
			fmt.Sprintf("printf '%%s\\n' %s", shellQuote("==> Validate")),
			project.ValidationCommand,
		)
	} else if project.Namespace != "" {
		hasWorkflowStep = true
		steps = append(
			steps,
			"",
			fmt.Sprintf("printf '%%s\\n' %s", shellQuote("==> Cluster snapshot")),
			buildKubectlCommand(project, "get", "pods,deploy,svc,ingress"),
		)
	}
	if !hasWorkflowStep {
		steps = append(
			steps,
			"",
			fmt.Sprintf("printf '%%s\\n' %s", shellQuote("No deploy or validation command configured for this project.")),
		)
	}

	return strings.Join(steps, "\n")
}

func buildKubeEnv(project project) []string {
	env := []string{}
	if project.KubeContext != "" {
		env = append(env, "MSPACE_KUBE_CONTEXT="+project.KubeContext)
	}
	if project.KubeconfigPath != "" {
		env = append(env, "KUBECONFIG="+project.KubeconfigPath)
		env = append(env, "MSPACE_KUBECONFIG="+project.KubeconfigPath)
	}
	if project.Namespace != "" {
		env = append(env, "MSPACE_KUBE_NAMESPACE="+project.Namespace)
	}
	if project.ImageRegistryPrefix != "" {
		env = append(env, "MSPACE_IMAGE_REGISTRY_PREFIX="+project.ImageRegistryPrefix)
	}
	if project.PreviewDomain != "" {
		env = append(env, "MSPACE_PREVIEW_DOMAIN="+project.PreviewDomain)
	}
	if project.IngressClass != "" {
		env = append(env, "MSPACE_INGRESS_CLASS="+project.IngressClass)
	}
	if project.NodeHost != "" {
		env = append(env, "MSPACE_NODE_HOST="+project.NodeHost)
	}
	return env
}

func (a *app) buildSessionEnv(session agentSession, project project, contextPath string) []string {
	env := buildKubeEnv(project)
	if environment, err := a.loadIssueTestEnvironment(session.IssueID); err == nil && environment != nil {
		if environment.KubeconfigPath != "" {
			env = append(env, "KUBECONFIG="+environment.KubeconfigPath)
			env = append(env, "MSPACE_KUBECONFIG="+environment.KubeconfigPath)
		}
		if environment.KubeContext != "" {
			env = append(env, "MSPACE_KUBE_CONTEXT="+environment.KubeContext)
		}
		if environment.Namespace != "" {
			env = append(env, "MSPACE_TEST_NAMESPACE="+environment.Namespace)
			env = append(env, "MSPACE_KUBE_NAMESPACE="+environment.Namespace)
		}
		if environment.ClusterID != "" {
			env = append(env, "MSPACE_CLUSTER_ID="+environment.ClusterID)
		}
		if environment.ExposureMode != "" {
			env = append(env, "MSPACE_EXPOSURE_MODE="+environment.ExposureMode)
		}
		if environment.ImageRegistryPrefix != "" {
			env = append(env, "MSPACE_IMAGE_REGISTRY_PREFIX="+environment.ImageRegistryPrefix)
		}
		if environment.PreviewDomain != "" {
			env = append(env, "MSPACE_PREVIEW_DOMAIN="+environment.PreviewDomain)
		}
		if environment.IngressClass != "" {
			env = append(env, "MSPACE_INGRESS_CLASS="+environment.IngressClass)
		}
		if environment.NodeHost != "" {
			env = append(env, "MSPACE_NODE_HOST="+environment.NodeHost)
		}
	}
	env = append(env,
		"MSPACE_API_BASE_URL="+mspaceAPIBaseURL(),
		"MSPACE_ISSUE_ID="+session.IssueID,
		"MSPACE_SESSION_ID="+session.ID,
		"MSPACE_AGENT_PROFILE="+session.AgentProfile,
		"MSPACE_SESSION_BRANCH="+session.Branch,
		"MSPACE_SESSION_WORKDIR="+session.Workdir,
	)
	if contextPath != "" {
		env = append(env, "MSPACE_SESSION_CONTEXT="+contextPath)
	}
	if session.ArtifactDir != "" {
		env = append(env, "MSPACE_SESSION_ARTIFACT_DIR="+session.ArtifactDir)
	}
	return env
}

func mspaceAPIBaseURL() string {
	port := strings.TrimSpace(os.Getenv("MSPACE_PORT"))
	if port == "" {
		port = "7788"
	}
	return "http://127.0.0.1:" + port
}

func buildKubectlArgs(project project, args ...string) []string {
	kubectlArgs := []string{}
	if project.KubeconfigPath != "" {
		kubectlArgs = append(kubectlArgs, "--kubeconfig", project.KubeconfigPath)
	}
	if project.KubeContext != "" {
		kubectlArgs = append(kubectlArgs, "--context", project.KubeContext)
	}
	if project.Namespace != "" {
		kubectlArgs = append(kubectlArgs, "-n", project.Namespace)
	}
	kubectlArgs = append(kubectlArgs, args...)
	return kubectlArgs
}

func buildKubectlCommand(project project, args ...string) string {
	commandArgs := append([]string{"kubectl"}, buildKubectlArgs(project, args...)...)
	return shellJoin(commandArgs)
}

func ensureGitRepository(gitPath, repoPath string) error {
	output, err := exec.Command(gitPath, "-C", repoPath, "rev-parse", "--is-inside-work-tree").CombinedOutput()
	if err != nil {
		return fmt.Errorf("repo path is not a git work tree: %s", formatCommandFailure(err, output))
	}
	if strings.TrimSpace(string(output)) != "true" {
		return errors.New("repo path is not a git work tree")
	}
	return nil
}

func gitRefExists(gitPath, repoPath, ref string) (bool, error) {
	cmd := exec.Command(gitPath, "-C", repoPath, "rev-parse", "--verify", "--quiet", ref)
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("check git ref %q: %w", ref, err)
	}
	return true, nil
}

func resolveBaseRef(gitPath, repoPath, defaultBranch string) (string, error) {
	defaultBranch = strings.TrimSpace(defaultBranch)
	if defaultBranch != "" {
		exists, err := gitRefExists(gitPath, repoPath, "refs/heads/"+defaultBranch)
		if err != nil {
			return "", err
		}
		if exists {
			return defaultBranch, nil
		}

		remoteRef := "refs/remotes/origin/" + defaultBranch
		exists, err = gitRefExists(gitPath, repoPath, remoteRef)
		if err != nil {
			return "", err
		}
		if exists {
			return "origin/" + defaultBranch, nil
		}
	}

	output, err := exec.Command(gitPath, "-C", repoPath, "rev-parse", "--verify", "HEAD").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve base ref: %s", formatCommandFailure(err, output))
	}
	return "HEAD", nil
}

func removeWorktree(gitPath, repoPath, workdir string) error {
	output, err := exec.Command(gitPath, "-C", repoPath, "worktree", "remove", "--force", workdir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove worktree %s: %s", workdir, formatCommandFailure(err, output))
	}
	return nil
}

func inspectWorkspace(workdir, defaultBranch string) workspaceSnapshot {
	snapshot := workspaceSnapshot{
		StatusLines: []string{},
		Changes:     []workspaceChange{},
		Comparison: workspaceComparison{
			CommitLines: []string{},
			Changes:     []workspaceChange{},
		},
	}

	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		snapshot.Error = "Workspace path has not been recorded for this session yet."
		return snapshot
	}

	info, err := os.Stat(workdir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			snapshot.Error = "Workspace has not been prepared yet."
			return snapshot
		}
		snapshot.Error = fmt.Sprintf("Inspect workspace: %v", err)
		return snapshot
	}
	if !info.IsDir() {
		snapshot.Error = "Workspace path is not a directory."
		return snapshot
	}
	snapshot.Exists = true

	gitPath, err := exec.LookPath("git")
	if err != nil {
		snapshot.Error = "git is not available on PATH."
		return snapshot
	}
	if err := ensureGitRepository(gitPath, workdir); err != nil {
		snapshot.Error = err.Error()
		return snapshot
	}
	snapshot.IsGitRepository = true

	if branch, err := runGitReadOnly(gitPath, workdir, "branch", "--show-current"); err == nil {
		snapshot.Branch = branch
	} else {
		snapshot.Error = err.Error()
	}

	if head, err := runGitReadOnly(gitPath, workdir, "rev-parse", "HEAD"); err == nil {
		snapshot.Head = head
		snapshot.ShortHead = shortCommitSHA(head)
	} else if snapshot.Error == "" {
		snapshot.Error = err.Error()
	}

	statusOutput, err := runGitReadOnly(gitPath, workdir, "status", "--short")
	if err != nil {
		if snapshot.Error == "" {
			snapshot.Error = err.Error()
		}
		return snapshot
	}

	for _, line := range strings.Split(statusOutput, "\n") {
		line = strings.TrimRight(line, " ")
		if line == "" {
			continue
		}
		snapshot.StatusLines = append(snapshot.StatusLines, line)
		snapshot.Changes = append(snapshot.Changes, parseWorkspaceChange(line))
		if strings.HasPrefix(line, "??") {
			snapshot.UntrackedFiles++
			continue
		}
		snapshot.ChangedFiles++
	}
	snapshot.HasChanges = snapshot.ChangedFiles > 0 || snapshot.UntrackedFiles > 0

	diffPreview, err := runGitReadOnly(gitPath, workdir, "diff", "--stat", "--patch", "--find-renames", "HEAD")
	if err != nil {
		if snapshot.Error == "" {
			snapshot.Error = err.Error()
		}
		return snapshot
	}
	snapshot.DiffPreview, snapshot.DiffTruncated = truncateWithFlag(diffPreview, 20000)
	inspectWorkspaceComparison(&snapshot, gitPath, workdir, defaultBranch)

	return snapshot
}

func inspectWorkspaceComparison(snapshot *workspaceSnapshot, gitPath, workdir, defaultBranch string) {
	baseRef, err := resolveBaseRef(gitPath, workdir, defaultBranch)
	if err != nil {
		snapshot.Comparison.Error = err.Error()
		return
	}
	snapshot.Comparison.BaseRef = baseRef

	mergeBase, err := runGitReadOnly(gitPath, workdir, "merge-base", "HEAD", baseRef)
	if err != nil {
		snapshot.Comparison.Error = err.Error()
		return
	}
	snapshot.Comparison.MergeBase = strings.TrimSpace(mergeBase)
	snapshot.Comparison.MergeBaseShort = shortCommitSHA(snapshot.Comparison.MergeBase)

	aheadBehind, err := runGitReadOnly(gitPath, workdir, "rev-list", "--left-right", "--count", baseRef+"...HEAD")
	if err != nil {
		snapshot.Comparison.Error = err.Error()
		return
	}
	fields := strings.Fields(aheadBehind)
	if len(fields) == 2 {
		snapshot.Comparison.BehindCount = parseIntOrZero(fields[0])
		snapshot.Comparison.AheadCount = parseIntOrZero(fields[1])
	}

	commitLines, err := runGitReadOnly(gitPath, workdir, "log", "--oneline", "--decorate", "--no-color", snapshot.Comparison.MergeBase+"..HEAD")
	if err != nil {
		snapshot.Comparison.Error = err.Error()
		return
	}
	snapshot.Comparison.CommitLines = splitNonEmptyLines(commitLines)

	nameStatusOutput, err := runGitReadOnly(gitPath, workdir, "diff", "--name-status", "--find-renames", snapshot.Comparison.MergeBase)
	if err != nil {
		snapshot.Comparison.Error = err.Error()
		return
	}
	for _, line := range splitNonEmptyLines(nameStatusOutput) {
		snapshot.Comparison.Changes = append(snapshot.Comparison.Changes, parseNameStatusChange(line))
	}

	diffPreview, err := runGitReadOnly(gitPath, workdir, "diff", "--stat", "--patch", "--find-renames", snapshot.Comparison.MergeBase)
	if err != nil {
		snapshot.Comparison.Error = err.Error()
		return
	}
	snapshot.Comparison.DiffPreview, snapshot.Comparison.DiffTruncated = truncateWithFlag(diffPreview, 20000)
}

func parseWorkspaceChange(line string) workspaceChange {
	change := workspaceChange{}
	if len(line) < 3 {
		change.Path = strings.TrimSpace(line)
		return change
	}

	change.StatusCode = line[:2]
	pathPart := strings.TrimSpace(line[3:])
	parts := strings.SplitN(pathPart, " -> ", 2)
	if len(parts) == 2 {
		change.PreviousPath = parts[0]
		change.Path = parts[1]
		return change
	}
	change.Path = pathPart
	return change
}

func parseNameStatusChange(line string) workspaceChange {
	change := workspaceChange{}
	fields := strings.Split(line, "\t")
	if len(fields) == 0 {
		return change
	}
	change.StatusCode = strings.TrimSpace(fields[0])
	if len(fields) >= 3 {
		change.PreviousPath = strings.TrimSpace(fields[1])
		change.Path = strings.TrimSpace(fields[2])
		return change
	}
	if len(fields) >= 2 {
		change.Path = strings.TrimSpace(fields[1])
	}
	return change
}

func runGitReadOnly(gitPath, repoPath string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", repoPath}, args...)
	output, err := exec.Command(gitPath, commandArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), formatCommandFailure(err, output))
	}
	return strings.TrimRight(string(output), "\r\n"), nil
}

func formatCommandFailure(err error, output []byte) string {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return err.Error()
	}
	return message
}

func shellJoin(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, shellQuote(value))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func clusterLabel(cluster string) string {
	cluster = strings.TrimSpace(cluster)
	if cluster == "" {
		return "current-context"
	}
	return cluster
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max] + "\n\n...truncated..."
}

func truncateWithFlag(value string, max int) (string, bool) {
	if len(value) <= max {
		return value, false
	}
	return value[:max] + "\n\n...truncated...", true
}

func splitNonEmptyLines(value string) []string {
	lines := strings.Split(value, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		result = append(result, line)
	}
	return result
}

func parseIntOrZero(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed
}

func shortCommitSHA(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}
