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

type project struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	RepoPath             string `json:"repoPath"`
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
	EnvironmentURL string `json:"environmentUrl"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

type inboxItem struct {
	ID        string `json:"id"`
	IssueID   string `json:"issueId"`
	ProjectID string `json:"projectId"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Unread    bool   `json:"unread"`
	UpdatedAt string `json:"updatedAt"`
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
	Session   agentSession      `json:"session"`
	Issue     issue             `json:"issue"`
	Project   project           `json:"project"`
	Logs      []sessionLog      `json:"logs"`
	Evidence  []deploymentEvidence `json:"evidence"`
	Workspace workspaceSnapshot `json:"workspace"`
}

type workspaceSnapshot struct {
	Exists          bool              `json:"exists"`
	IsGitRepository bool              `json:"isGitRepository"`
	HasChanges      bool              `json:"hasChanges"`
	ChangedFiles    int               `json:"changedFiles"`
	UntrackedFiles  int               `json:"untrackedFiles"`
	Head            string            `json:"head"`
	ShortHead       string            `json:"shortHead"`
	Branch          string            `json:"branch"`
	StatusLines     []string          `json:"statusLines"`
	Changes         []workspaceChange `json:"changes"`
	DiffPreview     string            `json:"diffPreview"`
	DiffTruncated   bool              `json:"diffTruncated"`
	Comparison      workspaceComparison `json:"comparison"`
	Error           string            `json:"error"`
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
	broker     *eventBroker
	mu         sync.Mutex
	cancellers map[string]context.CancelFunc
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
		broker:     newEventBroker(),
		cancellers: map[string]context.CancelFunc{},
	}

	if err := os.MkdirAll(application.workdir, 0o755); err != nil {
		logger.Error("failed to create workdir root", "error", err)
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
	router.Get("/api/projects", application.handleListProjects)
	router.Post("/api/projects", application.handleCreateProject)
	router.Put("/api/projects/{projectID}", application.handleUpdateProject)
	router.Delete("/api/projects/{projectID}", application.handleDeleteProject)
	router.Post("/api/issues", application.handleCreateIssue)
	router.Get("/api/issues/{issueID}", application.handleGetIssue)
	router.Post("/api/issues/{issueID}/comments", application.handleCreateComment)
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
	return nil
}

func (a *app) handleListInbox(w http.ResponseWriter, _ *http.Request) {
	items := make([]inboxItem, 0)
	rows, err := a.db.Query(`
		SELECT id, issue_id, project_id, title, status, unread, updated_at
		FROM inbox_items
		ORDER BY updated_at DESC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var item inboxItem
		var unread int
		if err := rows.Scan(&item.ID, &item.IssueID, &item.ProjectID, &item.Title, &item.Status, &unread, &item.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		item.Unread = unread == 1
		items = append(items, item)
	}
	writeJSON(w, items)
}

func (a *app) handleListProjects(w http.ResponseWriter, _ *http.Request) {
	projects := make([]project, 0)
	rows, err := a.db.Query(`
		SELECT
			p.id,
			p.name,
			p.repo_path,
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
		if err := rows.Scan(&p.ID, &p.Name, &p.RepoPath, &p.DefaultBranch, &p.DeployCommand, &p.ValidationCommand, &p.KubeContext, &p.Namespace, &p.IssueCount, &p.SessionCount, &latestIssueUpdatedAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
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
	var input struct {
		Name              string `json:"name"`
		RepoPath          string `json:"repoPath"`
		DefaultBranch     string `json:"defaultBranch"`
		DeployCommand     string `json:"deployCommand"`
		ValidationCommand string `json:"validationCommand"`
		KubeContext       string `json:"kubeContext"`
		Namespace         string `json:"namespace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	normalizedProject, err := normalizeProjectInput(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	now := nowString()
	normalizedProject.ID = uuid.NewString()
	normalizedProject.CreatedAt = now
	normalizedProject.UpdatedAt = now

	_, err = a.db.Exec(`
		INSERT INTO projects (id, name, repo_path, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, normalizedProject.ID, normalizedProject.Name, normalizedProject.RepoPath, normalizedProject.DefaultBranch, normalizedProject.DeployCommand, normalizedProject.ValidationCommand, normalizedProject.KubeContext, normalizedProject.Namespace, normalizedProject.CreatedAt, normalizedProject.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, normalizedProject)
}

func (a *app) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

	var input struct {
		Name              string `json:"name"`
		RepoPath          string `json:"repoPath"`
		DefaultBranch     string `json:"defaultBranch"`
		DeployCommand     string `json:"deployCommand"`
		ValidationCommand string `json:"validationCommand"`
		KubeContext       string `json:"kubeContext"`
		Namespace         string `json:"namespace"`
	}
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

	normalizedProject, err := normalizeProjectInput(input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	normalizedProject.ID = existingProject.ID
	normalizedProject.CreatedAt = existingProject.CreatedAt
	normalizedProject.UpdatedAt = nowString()

	_, err = a.db.Exec(`
		UPDATE projects
		SET name = ?, repo_path = ?, default_branch = ?, deploy_command = ?, validation_command = ?, kube_context = ?, namespace = ?, updated_at = ?
		WHERE id = ?
	`, normalizedProject.Name, normalizedProject.RepoPath, normalizedProject.DefaultBranch, normalizedProject.DeployCommand, normalizedProject.ValidationCommand, normalizedProject.KubeContext, normalizedProject.Namespace, normalizedProject.UpdatedAt, normalizedProject.ID)
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
		ProjectID string `json:"projectId"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		Assignee  string `json:"assignee"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.Title = strings.TrimSpace(input.Title)
	input.Body = strings.TrimSpace(input.Body)
	input.Assignee = strings.TrimSpace(input.Assignee)
	if input.ProjectID == "" {
		writeError(w, http.StatusBadRequest, errors.New("project is required"))
		return
	}
	if input.Title == "" {
		writeError(w, http.StatusBadRequest, errors.New("issue title cannot be empty"))
		return
	}
	if _, err := a.loadProject(input.ProjectID); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
			err = errors.New("project not found")
		}
		writeError(w, status, err)
		return
	}

	now := nowString()
	issueID := uuid.NewString()
	inboxID := uuid.NewString()
	status := "open"
	assignee := input.Assignee
	if assignee == "" {
		assignee = "me"
	}

	tx, err := a.db.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO issues (id, project_id, title, body, status, assignee, environment_url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, issueID, input.ProjectID, input.Title, input.Body, status, assignee, "", now, now); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if _, err := tx.Exec(`
		INSERT INTO inbox_items (id, issue_id, project_id, title, status, unread, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)
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
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *app) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueID")
	var input struct {
		Provider string `json:"provider"`
		Command  string `json:"command"`
		Branch   string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input.Provider = strings.TrimSpace(input.Provider)
	input.Command = strings.TrimSpace(input.Command)
	input.Branch = strings.TrimSpace(input.Branch)
	if input.Provider == "" {
		input.Provider = "codex"
	}

	detail, err := a.loadIssueDetail(issueID)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
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
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	a.addSystemComment(issueID, fmt.Sprintf(
		"Queued local session `%s` on branch `%s`.\n\nPlanned workspace: `%s`",
		shortID(session.ID),
		session.Branch,
		session.Workdir,
	))

	go a.runSession(session, detail.Project)
	writeJSON(w, map[string]string{"sessionId": session.ID})
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
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsubscribe := a.broker.subscribe(sessionID)
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

	a.updateSessionStatus(session.ID, "running")
	a.updateIssueStatus(session.IssueID, "running")
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Runner starting %s session in %s", session.Provider, session.Workdir))
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Source repository: %s", project.RepoPath))
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Session branch: %s", session.Branch))
	a.addSystemComment(session.IssueID, fmt.Sprintf(
		"Started local session `%s` on branch `%s`.\n\nWorkspace: `%s`",
		shortID(session.ID),
		session.Branch,
		session.Workdir,
	))
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Command: %s", session.Command))

	command := exec.CommandContext(ctx, "/bin/zsh", "-lc", session.Command)
	command.Dir = session.Workdir
	command.Env = append(os.Environ(), buildKubeEnv(project)...)

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
		SELECT i.id, i.project_id, i.title, i.body, i.status, i.assignee, i.environment_url, i.created_at, i.updated_at,
		       p.id, p.name, p.repo_path, p.default_branch, p.deploy_command, p.validation_command, p.kube_context, p.namespace, p.created_at, p.updated_at
		FROM issues i
		JOIN projects p ON p.id = i.project_id
		WHERE i.id = ?
	`, issueID)
	if err := row.Scan(
		&detail.Issue.ID, &detail.Issue.ProjectID, &detail.Issue.Title, &detail.Issue.Body, &detail.Issue.Status, &detail.Issue.Assignee, &detail.Issue.EnvironmentURL, &detail.Issue.CreatedAt, &detail.Issue.UpdatedAt,
		&detail.Project.ID, &detail.Project.Name, &detail.Project.RepoPath, &detail.Project.DefaultBranch, &detail.Project.DeployCommand, &detail.Project.ValidationCommand, &detail.Project.KubeContext, &detail.Project.Namespace, &detail.Project.CreatedAt, &detail.Project.UpdatedAt,
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
		       i.id, i.project_id, i.title, i.body, i.status, i.assignee, i.environment_url, i.created_at, i.updated_at,
		       p.id, p.name, p.repo_path, p.default_branch, p.deploy_command, p.validation_command, p.kube_context, p.namespace, p.created_at, p.updated_at
		FROM agent_sessions s
		JOIN issues i ON i.id = s.issue_id
		JOIN projects p ON p.id = i.project_id
		WHERE s.id = ?
	`, sessionID)
	if err := row.Scan(
		&detail.Session.ID, &detail.Session.IssueID, &detail.Session.Provider, &detail.Session.RuntimeMode, &detail.Session.Command, &detail.Session.Status, &detail.Session.Branch, &detail.Session.Workdir, &detail.Session.CreatedAt, &detail.Session.UpdatedAt,
		&detail.Issue.ID, &detail.Issue.ProjectID, &detail.Issue.Title, &detail.Issue.Body, &detail.Issue.Status, &detail.Issue.Assignee, &detail.Issue.EnvironmentURL, &detail.Issue.CreatedAt, &detail.Issue.UpdatedAt,
		&detail.Project.ID, &detail.Project.Name, &detail.Project.RepoPath, &detail.Project.DefaultBranch, &detail.Project.DeployCommand, &detail.Project.ValidationCommand, &detail.Project.KubeContext, &detail.Project.Namespace, &detail.Project.CreatedAt, &detail.Project.UpdatedAt,
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
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
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

func normalizeProjectInput(input struct {
	Name              string `json:"name"`
	RepoPath          string `json:"repoPath"`
	DefaultBranch     string `json:"defaultBranch"`
	DeployCommand     string `json:"deployCommand"`
	ValidationCommand string `json:"validationCommand"`
	KubeContext       string `json:"kubeContext"`
	Namespace         string `json:"namespace"`
}) (project, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return project{}, errors.New("project name cannot be empty")
	}

	repoPath := strings.TrimSpace(input.RepoPath)
	if repoPath == "" {
		return project{}, errors.New("repo path cannot be empty")
	}
	if !filepath.IsAbs(repoPath) {
		return project{}, errors.New("repo path must be an absolute path")
	}
	repoInfo, err := os.Stat(repoPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return project{}, errors.New("repo path does not exist")
		}
		return project{}, fmt.Errorf("repo path validation failed: %w", err)
	}
	if !repoInfo.IsDir() {
		return project{}, errors.New("repo path must point to a directory")
	}

	defaultBranch := strings.TrimSpace(input.DefaultBranch)
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	return project{
		Name:              name,
		RepoPath:          repoPath,
		DefaultBranch:     defaultBranch,
		DeployCommand:     strings.TrimSpace(input.DeployCommand),
		ValidationCommand: strings.TrimSpace(input.ValidationCommand),
		KubeContext:       strings.TrimSpace(input.KubeContext),
		Namespace:         strings.TrimSpace(input.Namespace),
	}, nil
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
