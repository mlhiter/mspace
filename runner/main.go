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
	ID               string `json:"id"`
	Name             string `json:"name"`
	RepoPath         string `json:"repoPath"`
	DefaultBranch    string `json:"defaultBranch"`
	DeployCommand    string `json:"deployCommand"`
	ValidationCommand string `json:"validationCommand"`
	KubeContext      string `json:"kubeContext"`
	Namespace        string `json:"namespace"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
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
	ID         string `json:"id"`
	IssueID    string `json:"issueId"`
	Provider   string `json:"provider"`
	RuntimeMode string `json:"runtimeMode"`
	Command    string `json:"command"`
	Status     string `json:"status"`
	Branch     string `json:"branch"`
	Workdir    string `json:"workdir"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
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
	Session agentSession `json:"session"`
	Issue   issue        `json:"issue"`
	Project project      `json:"project"`
	Logs    []sessionLog `json:"logs"`
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
	db        *sql.DB
	logger    *slog.Logger
	workdir   string
	broker    *eventBroker
	mu        sync.Mutex
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
		SELECT id, name, repo_path, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at
		FROM projects
		ORDER BY updated_at DESC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var p project
		if err := rows.Scan(&p.ID, &p.Name, &p.RepoPath, &p.DefaultBranch, &p.DeployCommand, &p.ValidationCommand, &p.KubeContext, &p.Namespace, &p.CreatedAt, &p.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		projects = append(projects, p)
	}
	writeJSON(w, projects)
}

func (a *app) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var input project
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	now := nowString()
	input.ID = uuid.NewString()
	input.CreatedAt = now
	input.UpdatedAt = now

	_, err := a.db.Exec(`
		INSERT INTO projects (id, name, repo_path, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, input.ID, input.Name, input.RepoPath, input.DefaultBranch, input.DeployCommand, input.ValidationCommand, input.KubeContext, input.Namespace, input.CreatedAt, input.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, input)
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
	if input.Provider == "" {
		input.Provider = "codex"
	}

	detail, err := a.loadIssueDetail(issueID)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}

	command := strings.TrimSpace(input.Command)
	if command == "" {
		command = strings.TrimSpace(detail.Project.ValidationCommand)
	}
	if command == "" {
		command = fmt.Sprintf("printf 'Starting local session for issue %s\\n'; sleep 1; printf 'No validation command configured.\\n'", issueID)
	}
	branch := input.Branch
	if branch == "" {
		branch = fmt.Sprintf("mspace/%s", shortID(issueID))
	}

	session := agentSession{
		ID:          uuid.NewString(),
		IssueID:     issueID,
		Provider:    input.Provider,
		RuntimeMode: "local",
		Command:     command,
		Status:      "queued",
		Branch:      branch,
		Workdir:     detail.Project.RepoPath,
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

	a.updateSessionStatus(session.ID, "running")
	a.updateIssueStatus(session.IssueID, "running")
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Runner starting %s session in %s", session.Provider, session.Workdir))
	a.appendSessionLog(session.ID, "system", fmt.Sprintf("Command: %s", session.Command))

	command := exec.CommandContext(ctx, "/bin/zsh", "-lc", session.Command)
	command.Dir = project.RepoPath
	command.Env = append(os.Environ(), buildKubeEnv(project)...)

	stdout, err := command.StdoutPipe()
	if err != nil {
		a.failSession(session, fmt.Errorf("stdout pipe: %w", err))
		return
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		a.failSession(session, fmt.Errorf("stderr pipe: %w", err))
		return
	}

	if err := command.Start(); err != nil {
		a.failSession(session, fmt.Errorf("start command: %w", err))
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
		return
	}
	if err != nil {
		a.failSession(session, fmt.Errorf("command failed: %w", err))
		return
	}

	a.updateSessionStatus(session.ID, "completed")
	a.updateIssueStatus(session.IssueID, "completed")
	a.appendSessionLog(session.ID, "system", "Session completed. Collecting Kubernetes evidence.")
	a.collectEvidence(session, project)
}

func (a *app) captureStream(wg *sync.WaitGroup, sessionID, stream string, reader interface{ Read([]byte) (int, error) }) {
	defer wg.Done()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		a.appendSessionLog(sessionID, stream, scanner.Text())
	}
}

func (a *app) failSession(session agentSession, err error) {
	a.updateSessionStatus(session.ID, "failed")
	a.updateIssueStatus(session.IssueID, "failed")
	a.appendSessionLog(session.ID, "system", err.Error())
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

func (a *app) loadSessionDetail(sessionID string) (sessionDetail, error) {
	var detail sessionDetail
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
