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

var errProjectNotFound = errors.New("project not found")

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
	ID           string `json:"id"`
	ProjectID    string `json:"projectId"`
	ProjectName  string `json:"projectName"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	Status       string `json:"status"`
	Assignee     string `json:"assignee"`
	AssigneeType string `json:"assigneeType"`
	Unread       bool   `json:"unread"`
	SessionCount int    `json:"sessionCount"`
	UpdatedAt    string `json:"updatedAt"`
	CreatedAt    string `json:"createdAt"`
}

type comment struct {
	ID         string `json:"id"`
	IssueID    string `json:"issueId"`
	AuthorType string `json:"authorType"`
	Body       string `json:"body"`
	CreatedAt  string `json:"createdAt"`
}

type agentSession struct {
	ID          string `json:"id"`
	IssueID     string `json:"issueId"`
	Provider    string `json:"provider"`
	RuntimeMode string `json:"runtimeMode"`
	Command     string `json:"command"`
	Status      string `json:"status"`
	Branch      string `json:"branch"`
	Workdir     string `json:"workdir"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
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
	Provider string `json:"provider"`
	Command  string `json:"command"`
	Branch   string `json:"branch"`
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
	router.Get("/api/projects", application.handleListProjects)
	router.Post("/api/projects", application.handleCreateProject)
	router.Put("/api/projects/{projectID}", application.handleUpdateProject)
	router.Delete("/api/projects/{projectID}", application.handleDeleteProject)
	router.Get("/api/issues", application.handleListIssues)
	router.Post("/api/issues", application.handleCreateIssue)
	router.Get("/api/issues/{issueID}", application.handleGetIssue)
	router.Post("/api/issues/{issueID}/comments", application.handleCreateComment)
	router.Post("/api/issues/{issueID}/assign-agent", application.handleAssignIssueToAgent)
	router.Post("/api/issues/{issueID}/sessions", application.handleCreateSession)
	router.Get("/api/sessions/{sessionID}", application.handleGetSession)
	router.Post("/api/sessions/{sessionID}/cancel", application.handleCancelSession)
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
		items = append(items, item)
	}
	writeJSON(w, items)
}

func (a *app) handleInboxStream(w http.ResponseWriter, r *http.Request) {
	a.streamEvents(w, r, "inbox")
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
		}
		writeError(w, status, err)
		return
	}

	writeJSON(w, map[string]string{"sessionId": session.ID})
}

func (a *app) queueAgentSession(issueID string, input sessionRequest) (agentSession, error) {
	input.Provider = strings.TrimSpace(input.Provider)
	input.Command = strings.TrimSpace(input.Command)
	input.Branch = strings.TrimSpace(input.Branch)
	if input.Provider == "" {
		input.Provider = "codex"
	}

	detail, err := a.loadIssueDetail(issueID)
	if err != nil {
		return agentSession{}, err
	}

	command := buildSessionCommand(issueID, detail.Project, input.Command)
	sessionID := uuid.NewString()
	branch := input.Branch
	if branch == "" {
		branch = fmt.Sprintf("mspace/%s/%s", shortID(issueID), shortID(sessionID))
	}
	workdir := plannedSessionWorkdir(a.workdir, detail.Project.ID, sessionID)

	session := agentSession{
		ID:          sessionID,
		IssueID:     issueID,
		Provider:    input.Provider,
		RuntimeMode: "local",
		Command:     command,
		Status:      "queued",
		Branch:      branch,
		Workdir:     workdir,
		CreatedAt:   nowString(),
		UpdatedAt:   nowString(),
	}

	if _, err := a.db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, runtime_mode, command, status, branch, workdir, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, session.ID, session.IssueID, session.Provider, session.RuntimeMode, session.Command, session.Status, session.Branch, session.Workdir, session.CreatedAt, session.UpdatedAt); err != nil {
		return agentSession{}, err
	}

	if err := a.updateIssueAssignment(issueID, input.Provider, "agent", "queued"); err != nil {
		return agentSession{}, err
	}

	a.addSystemComment(issueID, fmt.Sprintf(
		"Assigned to agent `%s` and queued local session `%s` on branch `%s`.\n\nPlanned workspace: `%s`",
		input.Provider,
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
		writeError(w, http.StatusNotFound, errors.New("session is not running"))
		return
	}
	cancel()
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
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Session context: %s", contextPath))
	a.addSystemComment(session.IssueID, fmt.Sprintf(
		"Started local session `%s` on branch `%s`.\n\nWorkspace: `%s`\nContext: `%s`",
		shortID(session.ID),
		session.Branch,
		session.Workdir,
		contextPath,
	))
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Command: %s", session.Command))

	command := exec.CommandContext(ctx, "/bin/zsh", "-lc", session.Command)
	command.Dir = session.Workdir
	command.Env = append(os.Environ(), buildSessionEnv(session, project, contextPath)...)

	stdout, err := command.StdoutPipe()
	if err != nil {
		a.failSession(session, &project, fmt.Errorf("stdout pipe: %w", err))
		return
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		a.failSession(session, &project, fmt.Errorf("stderr pipe: %w", err))
		return
	}

	if err := command.Start(); err != nil {
		a.failSession(session, &project, fmt.Errorf("start command: %w", err))
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go a.captureStream(&wg, session.ID, "stdout", stdout)
	go a.captureStream(&wg, session.ID, "stderr", stderr)

	err = command.Wait()
	wg.Wait()

	if ctx.Err() == context.Canceled {
		a.updateSessionStatus(session.ID, "cancelled")
		a.updateIssueStatus(session.IssueID, "cancelled")
		a.appendSessionLog(session.ID, "system", "Session cancelled.")
		a.addSystemComment(session.IssueID, fmt.Sprintf("Session `%s` was cancelled.", shortID(session.ID)))
		return
	}
	if err != nil {
		a.failSession(session, &project, fmt.Errorf("command failed: %w", err))
		return
	}

	a.updateSessionStatus(session.ID, "completed")
	a.updateIssueStatus(session.IssueID, "completed")
	a.appendSessionLog(session.ID, "system", "Session completed. Collecting Kubernetes evidence.")
	a.addSystemComment(session.IssueID, fmt.Sprintf("Session `%s` completed successfully.", shortID(session.ID)))
	a.collectEvidence(session, project)
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
		SELECT s.id, s.issue_id, s.provider, s.runtime_mode, s.command, s.status, s.branch, s.workdir, s.created_at, s.updated_at,
		       i.id, i.project_id, i.title, i.body, i.status, i.assignee, i.assignee_type, i.environment_url, i.created_at, i.updated_at,
		       p.id, p.name, p.repo_path, p.source_type, p.remote_url, p.git_provider, p.git_owner, p.git_repo, p.default_branch, p.deploy_command, p.validation_command, p.kube_context, p.namespace, p.created_at, p.updated_at
		FROM agent_sessions s
		JOIN issues i ON i.id = s.issue_id
		JOIN projects p ON p.id = i.project_id
		WHERE s.id = ?
	`, sessionID)
	if err := row.Scan(
		&detail.Session.ID, &detail.Session.IssueID, &detail.Session.Provider, &detail.Session.RuntimeMode, &detail.Session.Command, &detail.Session.Status, &detail.Session.Branch, &detail.Session.Workdir, &detail.Session.CreatedAt, &detail.Session.UpdatedAt,
		&detail.Issue.ID, &detail.Issue.ProjectID, &detail.Issue.Title, &detail.Issue.Body, &detail.Issue.Status, &detail.Issue.Assignee, &detail.Issue.AssigneeType, &detail.Issue.EnvironmentURL, &detail.Issue.CreatedAt, &detail.Issue.UpdatedAt,
		&detail.Project.ID, &detail.Project.Name, &detail.Project.RepoPath, &detail.Project.SourceType, &detail.Project.RemoteURL, &detail.Project.GitProvider, &detail.Project.GitOwner, &detail.Project.GitRepo, &detail.Project.DefaultBranch, &detail.Project.DeployCommand, &detail.Project.ValidationCommand, &detail.Project.KubeContext, &detail.Project.Namespace, &detail.Project.CreatedAt, &detail.Project.UpdatedAt,
	); err != nil {
		return detail, err
	}

	logRows, err := a.db.Query(`
		SELECT id, session_id, stream, message, created_at
		FROM session_logs
		WHERE session_id = ?
		ORDER BY id ASC
	`, sessionID)
	if err != nil {
		return detail, err
	}
	defer logRows.Close()

	for logRows.Next() {
		var log sessionLog
		if err := logRows.Scan(&log.ID, &log.SessionID, &log.Stream, &log.Message, &log.CreatedAt); err != nil {
			return detail, err
		}
		detail.Logs = append(detail.Logs, log)
	}

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

	return detail, nil
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

func (a *app) listSessions(issueID string) ([]agentSession, error) {
	rows, err := a.db.Query(`
		SELECT id, issue_id, provider, runtime_mode, command, status, branch, workdir, created_at, updated_at
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
		if err := rows.Scan(&s.ID, &s.IssueID, &s.Provider, &s.RuntimeMode, &s.Command, &s.Status, &s.Branch, &s.Workdir, &s.CreatedAt, &s.UpdatedAt); err != nil {
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

func plannedSessionWorkdir(root, projectID, sessionID string) string {
	return filepath.Join(root, projectID, sessionID)
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
		"MSPACE_SESSION_BRANCH="+session.Branch,
		"MSPACE_SESSION_WORKDIR="+session.Workdir,
	)
	if contextPath != "" {
		env = append(env, "MSPACE_SESSION_CONTEXT="+contextPath)
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
