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
	errUnknownAgentProfile  = errors.New("unknown agent profile")
	errSessionActive        = errors.New("session is still active")
	errUnsafeSessionWorkdir = errors.New("session workdir is outside the mspace workdir root")
)

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
	Namespace            string `json:"namespace"`
	IssueCount           int    `json:"issueCount"`
	SessionCount         int    `json:"sessionCount"`
	LatestIssueUpdatedAt string `json:"latestIssueUpdatedAt"`
	CreatedAt            string `json:"createdAt"`
	UpdatedAt            string `json:"updatedAt"`
}

type issue struct {
	ID             string `json:"id"`
	ProjectID      string `json:"projectId"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	Status         string `json:"status"`
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
	ID           string       `json:"id"`
	ProjectID    string       `json:"projectId"`
	ProjectName  string       `json:"projectName"`
	Title        string       `json:"title"`
	Body         string       `json:"body"`
	Status       string       `json:"status"`
	Assignee     string       `json:"assignee"`
	AssigneeType string       `json:"assigneeType"`
	Labels       []issueLabel `json:"labels"`
	Unread       bool         `json:"unread"`
	SessionCount int          `json:"sessionCount"`
	UpdatedAt    string       `json:"updatedAt"`
	CreatedAt    string       `json:"createdAt"`
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
	Name      string `json:"name"`
	Color     string `json:"color"`
	CreatedAt string `json:"createdAt"`
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

type issueDetail struct {
	Issue    issue                `json:"issue"`
	Project  project              `json:"project"`
	Labels   []issueLabel         `json:"labels"`
	Comments []comment            `json:"comments"`
	Sessions []agentSession       `json:"sessions"`
	Evidence []deploymentEvidence `json:"evidence"`
}

type sessionDetail struct {
	Session   agentSession         `json:"session"`
	Issue     issue                `json:"issue"`
	Project   project              `json:"project"`
	Logs      []sessionLog         `json:"logs"`
	Evidence  []deploymentEvidence `json:"evidence"`
	Workspace workspaceSnapshot    `json:"workspace"`
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
	Name              string `json:"name"`
	SourceType        string `json:"sourceType"`
	RepoPath          string `json:"repoPath"`
	RepoURL           string `json:"repoUrl"`
	DefaultBranch     string `json:"defaultBranch"`
	DeployCommand     string `json:"deployCommand"`
	ValidationCommand string `json:"validationCommand"`
	KubeContext       string `json:"kubeContext"`
	Namespace         string `json:"namespace"`
}

type sessionRequest struct {
	Provider     string `json:"provider"`
	AgentProfile string `json:"agentProfile"`
	Command      string `json:"command"`
	Branch       string `json:"branch"`
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
	router.Get("/api/agents", application.handleListAgentProfiles)
	router.Post("/api/agents", application.handleCreateAgentProfile)
	router.Put("/api/agents/{agentID}", application.handleUpdateAgentProfile)
	router.Get("/api/projects", application.handleListProjects)
	router.Post("/api/projects", application.handleCreateProject)
	router.Put("/api/projects/{projectID}", application.handleUpdateProject)
	router.Delete("/api/projects/{projectID}", application.handleDeleteProject)
	router.Get("/api/issues", application.handleListIssues)
	router.Post("/api/issues", application.handleCreateIssue)
	router.Get("/api/issues/{issueID}", application.handleGetIssue)
	router.Put("/api/issues/{issueID}/labels", application.handleUpdateIssueLabels)
	router.Post("/api/issues/{issueID}/comments", application.handleCreateComment)
	router.Post("/api/issues/{issueID}/assign-agent", application.handleAssignIssueToAgent)
	router.Post("/api/issues/{issueID}/sessions", application.handleCreateSession)
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
	if err := a.ensureProjectColumns(); err != nil {
		return err
	}
	if err := a.ensureIssueColumns(); err != nil {
		return err
	}
	if err := a.ensureIssueLabelTables(); err != nil {
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
		"source_type":  "TEXT NOT NULL DEFAULT 'local'",
		"remote_url":   "TEXT NOT NULL DEFAULT ''",
		"git_provider": "TEXT NOT NULL DEFAULT ''",
		"git_owner":    "TEXT NOT NULL DEFAULT ''",
		"git_repo":     "TEXT NOT NULL DEFAULT ''",
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

	if !existing["assignee_type"] {
		if _, err := a.db.Exec(`ALTER TABLE issues ADD COLUMN assignee_type TEXT NOT NULL DEFAULT 'human'`); err != nil {
			return fmt.Errorf("add issues.assignee_type: %w", err)
		}
	}
	return nil
}

func (a *app) ensureIssueLabelTables() error {
	if _, err := a.db.Exec(`
		CREATE TABLE IF NOT EXISTS issue_labels (
			id TEXT PRIMARY KEY,
			issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			color TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			UNIQUE(issue_id, name)
		)
	`); err != nil {
		return fmt.Errorf("create issue_labels: %w", err)
	}
	if _, err := a.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_issue_labels_issue_id ON issue_labels(issue_id)
	`); err != nil {
		return fmt.Errorf("create issue_labels issue index: %w", err)
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

func (a *app) handleListIssues(w http.ResponseWriter, _ *http.Request) {
	items := make([]issueListItem, 0)
	rows, err := a.db.Query(`
		SELECT
			i.id,
			i.project_id,
			p.name,
			i.title,
			i.body,
			i.status,
			i.assignee,
			i.assignee_type,
			COALESCE(ii.unread, 0) AS unread,
			COUNT(DISTINCT s.id) AS session_count,
			i.updated_at,
			i.created_at
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		LEFT JOIN inbox_items ii ON ii.issue_id = i.id
		LEFT JOIN agent_sessions s ON s.issue_id = i.id
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
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.ProjectName, &item.Title, &item.Body, &item.Status, &item.Assignee, &item.AssigneeType, &unread, &item.SessionCount, &item.UpdatedAt, &item.CreatedAt); err != nil {
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
			p.namespace,
			COUNT(DISTINCT i.id) AS issue_count,
			COUNT(DISTINCT s.id) AS session_count,
			MAX(i.updated_at) AS latest_issue_updated_at,
			p.created_at,
			p.updated_at
		FROM projects p
		LEFT JOIN issues i ON i.project_id = p.id
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
		if err := rows.Scan(&p.ID, &p.Name, &p.RepoPath, &p.SourceType, &p.RemoteURL, &p.GitProvider, &p.GitOwner, &p.GitRepo, &p.DefaultBranch, &p.DeployCommand, &p.ValidationCommand, &p.KubeContext, &p.Namespace, &p.IssueCount, &p.SessionCount, &latestIssueUpdatedAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
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
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, normalizedProject.ID, normalizedProject.Name, normalizedProject.RepoPath, normalizedProject.SourceType, normalizedProject.RemoteURL, normalizedProject.GitProvider, normalizedProject.GitOwner, normalizedProject.GitRepo, normalizedProject.DefaultBranch, normalizedProject.DeployCommand, normalizedProject.ValidationCommand, normalizedProject.KubeContext, normalizedProject.Namespace, normalizedProject.CreatedAt, normalizedProject.UpdatedAt)
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
		SET name = ?, repo_path = ?, source_type = ?, remote_url = ?, git_provider = ?, git_owner = ?, git_repo = ?, default_branch = ?, deploy_command = ?, validation_command = ?, kube_context = ?, namespace = ?, updated_at = ?
		WHERE id = ?
	`, normalizedProject.Name, normalizedProject.RepoPath, normalizedProject.SourceType, normalizedProject.RemoteURL, normalizedProject.GitProvider, normalizedProject.GitOwner, normalizedProject.GitRepo, normalizedProject.DefaultBranch, normalizedProject.DeployCommand, normalizedProject.ValidationCommand, normalizedProject.KubeContext, normalizedProject.Namespace, normalizedProject.UpdatedAt, normalizedProject.ID)
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
		ProjectID    string `json:"projectId"`
		Title        string `json:"title"`
		Body         string `json:"body"`
		Prompt       string `json:"prompt"`
		Assignee     string `json:"assignee"`
		AssigneeType string `json:"assigneeType"`
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
	if input.Body == "" {
		input.Body = input.Title
	}
	if input.Title == "" {
		input.Title = deriveIssueTitle(input.Body)
	}
	if input.Body == "" {
		input.Body = input.Title
	}
	if input.Title == "" {
		writeError(w, http.StatusBadRequest, errors.New("issue cannot be empty"))
		return
	}
	resolvedProject, err := a.resolveIssueProject(input.ProjectID, input.Title+"\n"+input.Body)
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

	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO issues (id, project_id, title, body, status, assignee, assignee_type, environment_url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, issueID, input.ProjectID, input.Title, input.Body, status, assignee, input.AssigneeType, "", now, now); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
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

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
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
		SELECT id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at
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
		if err := rows.Scan(&p.ID, &p.Name, &p.RepoPath, &p.SourceType, &p.RemoteURL, &p.GitProvider, &p.GitOwner, &p.GitRepo, &p.DefaultBranch, &p.DeployCommand, &p.ValidationCommand, &p.KubeContext, &p.Namespace, &p.CreatedAt, &p.UpdatedAt); err != nil {
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

func (a *app) handleUpdateIssueLabels(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	var input struct {
		Labels []string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	labels, err := normalizeIssueLabelNames(input.Labels)
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
	if err := tx.QueryRow(`SELECT project_id FROM issues WHERE id = ?`, issueID).Scan(&projectID); err != nil {
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
			INSERT INTO issue_labels (id, issue_id, name, color, created_at)
			VALUES (?, ?, ?, ?, ?)
		`, uuid.NewString(), issueID, label, "", now); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	if _, err := tx.Exec(`UPDATE issues SET updated_at = ? WHERE id = ?`, now, issueID); err != nil {
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
	a.appendSessionLog(session.ID, "system", "Session completed. Collecting Kubernetes evidence.")
	a.addSystemComment(session.IssueID, fmt.Sprintf("Session `%s` completed successfully.", shortID(session.ID)))
	a.collectEvidence(session, project)
}

func (a *app) runShellSession(ctx context.Context, session agentSession, project project, contextPath string) error {
	command := exec.CommandContext(ctx, "/bin/zsh", "-lc", session.Command)
	command.Dir = session.Workdir
	command.Env = append(os.Environ(), buildSessionEnv(session, project, contextPath)...)

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
	if project != nil && project.Namespace != "" {
		a.appendSessionLog(session.ID, "system", "Collecting Kubernetes evidence after failure.")
		a.collectEvidence(session, *project)
	}
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

	builder.WriteString("## Project\n\n")
	builder.WriteString(fmt.Sprintf("- Name: %s\n", project.Name))
	builder.WriteString(fmt.Sprintf("- Repository: %s\n", project.RepoPath))
	builder.WriteString(fmt.Sprintf("- Remote: %s\n", project.RemoteURL))
	builder.WriteString(fmt.Sprintf("- GitHub: %s/%s\n", project.GitOwner, project.GitRepo))
	builder.WriteString(fmt.Sprintf("- Default branch: %s\n", project.DefaultBranch))
	builder.WriteString(fmt.Sprintf("- Kube context: %s\n", project.KubeContext))
	builder.WriteString(fmt.Sprintf("- Namespace: %s\n", project.Namespace))
	builder.WriteString("\n")

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

func (a *app) loadIssueDetail(issueID string) (issueDetail, error) {
	var detail issueDetail
	row := a.db.QueryRow(`
		SELECT i.id, i.project_id, i.title, i.body, i.status, i.assignee, i.assignee_type, i.environment_url, i.created_at, i.updated_at,
		       p.id, p.name, p.repo_path, p.source_type, p.remote_url, p.git_provider, p.git_owner, p.git_repo, p.default_branch, p.deploy_command, p.validation_command, p.kube_context, p.namespace, p.created_at, p.updated_at
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		WHERE i.id = ?
	`, issueID)
	if err := row.Scan(
		&detail.Issue.ID, &detail.Issue.ProjectID, &detail.Issue.Title, &detail.Issue.Body, &detail.Issue.Status, &detail.Issue.Assignee, &detail.Issue.AssigneeType, &detail.Issue.EnvironmentURL, &detail.Issue.CreatedAt, &detail.Issue.UpdatedAt,
		&detail.Project.ID, &detail.Project.Name, &detail.Project.RepoPath, &detail.Project.SourceType, &detail.Project.RemoteURL, &detail.Project.GitProvider, &detail.Project.GitOwner, &detail.Project.GitRepo, &detail.Project.DefaultBranch, &detail.Project.DeployCommand, &detail.Project.ValidationCommand, &detail.Project.KubeContext, &detail.Project.Namespace, &detail.Project.CreatedAt, &detail.Project.UpdatedAt,
	); err != nil {
		return detail, err
	}

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
			p.namespace,
			COUNT(DISTINCT i.id) AS issue_count,
			COUNT(DISTINCT s.id) AS session_count,
			MAX(i.updated_at) AS latest_issue_updated_at,
			p.created_at,
			p.updated_at
		FROM projects p
		LEFT JOIN issues i ON i.project_id = p.id
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
		&project.Namespace,
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
		       i.id, i.project_id, i.title, i.body, i.status, i.assignee, i.assignee_type, i.environment_url, i.created_at, i.updated_at,
		       p.id, p.name, p.repo_path, p.source_type, p.remote_url, p.git_provider, p.git_owner, p.git_repo, p.default_branch, p.deploy_command, p.validation_command, p.kube_context, p.namespace, p.created_at, p.updated_at
		FROM agent_sessions s
		JOIN issues i ON i.id = s.issue_id
		JOIN projects p ON p.id = i.project_id
		WHERE s.id = ?
	`, sessionID)
	if err := row.Scan(
		&detail.Session.ID, &detail.Session.IssueID, &detail.Session.Provider, &detail.Session.AgentProfile, &detail.Session.RuntimeMode, &detail.Session.Command, &detail.Session.Status, &detail.Session.Branch, &detail.Session.Workdir, &detail.Session.CodexThreadID, &detail.Session.CodexTurnID, &detail.Session.AgentStatus, &detail.Session.ArtifactDir, &detail.Session.CleanupStatus, &detail.Session.CleanedAt, &detail.Session.CreatedAt, &detail.Session.UpdatedAt,
		&detail.Issue.ID, &detail.Issue.ProjectID, &detail.Issue.Title, &detail.Issue.Body, &detail.Issue.Status, &detail.Issue.Assignee, &detail.Issue.AssigneeType, &detail.Issue.EnvironmentURL, &detail.Issue.CreatedAt, &detail.Issue.UpdatedAt,
		&detail.Project.ID, &detail.Project.Name, &detail.Project.RepoPath, &detail.Project.SourceType, &detail.Project.RemoteURL, &detail.Project.GitProvider, &detail.Project.GitOwner, &detail.Project.GitRepo, &detail.Project.DefaultBranch, &detail.Project.DeployCommand, &detail.Project.ValidationCommand, &detail.Project.KubeContext, &detail.Project.Namespace, &detail.Project.CreatedAt, &detail.Project.UpdatedAt,
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
		SELECT id, issue_id, name, color, created_at
		FROM issue_labels
		WHERE issue_id = ?
		ORDER BY created_at ASC, name ASC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	labels := make([]issueLabel, 0)
	for rows.Next() {
		var label issueLabel
		if err := rows.Scan(&label.ID, &label.IssueID, &label.Name, &label.Color, &label.CreatedAt); err != nil {
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

	return project{
		Name:              name,
		RepoPath:          repoPath,
		SourceType:        sourceType,
		RemoteURL:         remoteURL,
		GitProvider:       gitInfo.Provider,
		GitOwner:          gitInfo.Owner,
		GitRepo:           gitInfo.Repo,
		DefaultBranch:     defaultBranch,
		DeployCommand:     strings.TrimSpace(input.DeployCommand),
		ValidationCommand: strings.TrimSpace(input.ValidationCommand),
		KubeContext:       strings.TrimSpace(input.KubeContext),
		Namespace:         strings.TrimSpace(input.Namespace),
	}, nil
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

func normalizeIssueLabelNames(values []string) ([]string, error) {
	labels := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, raw := range values {
		label := strings.TrimSpace(raw)
		label = strings.TrimSpace(strings.TrimPrefix(label, "#"))
		label = strings.Join(strings.Fields(label), " ")
		if label == "" {
			continue
		}
		if strings.ContainsAny(label, "\r\n\t") {
			return nil, fmt.Errorf("label %q contains unsupported whitespace", raw)
		}
		if len([]rune(label)) > 32 {
			return nil, fmt.Errorf("label %q is longer than 32 characters", label)
		}
		key := strings.ToLower(label)
		if seen[key] {
			continue
		}
		seen[key] = true
		labels = append(labels, label)
		if len(labels) > 12 {
			return nil, errors.New("an issue can have at most 12 labels")
		}
	}
	return labels, nil
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
	if project.Namespace != "" {
		env = append(env, "MSPACE_KUBE_NAMESPACE="+project.Namespace)
	}
	return env
}

func buildSessionEnv(session agentSession, project project, contextPath string) []string {
	env := buildKubeEnv(project)
	env = append(env,
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

func buildKubectlArgs(project project, args ...string) []string {
	kubectlArgs := []string{}
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
