package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestRunnerHealthPayloadAdvertisesRequiredCapabilities(t *testing.T) {
	payload := runnerHealthPayload()
	if payload["ok"] != true {
		t.Fatalf("expected ok=true, got %#v", payload["ok"])
	}
	if payload["runnerProtocol"] != runnerProtocolVersion {
		t.Fatalf("expected runner protocol %d, got %#v", runnerProtocolVersion, payload["runnerProtocol"])
	}
	capabilities, ok := payload["capabilities"].(map[string]bool)
	if !ok {
		t.Fatalf("expected boolean capabilities map, got %#v", payload["capabilities"])
	}
	if !maps.Equal(capabilities, map[string]bool{"issueAttachments": true, "issueHandoffs": true}) {
		t.Fatalf("unexpected capabilities: %#v", capabilities)
	}
}

func TestInspectWorkspaceComparison(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for workspace comparison test")
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init", "-b", "main")
	runGit(t, repoDir, "config", "user.name", "mspace test")
	runGit(t, repoDir, "config", "user.email", "mspace@example.com")

	writeFile(t, filepath.Join(repoDir, "note.txt"), "base\n")
	runGit(t, repoDir, "add", "note.txt")
	runGit(t, repoDir, "commit", "-m", "base commit")

	runGit(t, repoDir, "checkout", "-b", "feature/session")
	writeFile(t, filepath.Join(repoDir, "note.txt"), "base\nfeature\n")
	writeFile(t, filepath.Join(repoDir, "new.txt"), "hello\n")
	runGit(t, repoDir, "add", "note.txt", "new.txt")
	runGit(t, repoDir, "commit", "-m", "feature commit")

	writeFile(t, filepath.Join(repoDir, "draft.txt"), "draft\n")

	snapshot := inspectWorkspace(repoDir, "main")

	if !snapshot.Exists || !snapshot.IsGitRepository {
		t.Fatalf("expected git workspace snapshot, got %+v", snapshot)
	}
	if snapshot.Comparison.BaseRef != "main" {
		t.Fatalf("expected base ref main, got %q", snapshot.Comparison.BaseRef)
	}
	if snapshot.Comparison.AheadCount != 1 {
		t.Fatalf("expected ahead count 1, got %d", snapshot.Comparison.AheadCount)
	}
	if snapshot.Comparison.BehindCount != 0 {
		t.Fatalf("expected behind count 0, got %d", snapshot.Comparison.BehindCount)
	}
	if len(snapshot.Comparison.CommitLines) == 0 || !strings.Contains(snapshot.Comparison.CommitLines[0], "feature commit") {
		t.Fatalf("expected commit lines to mention feature commit, got %#v", snapshot.Comparison.CommitLines)
	}
	if len(snapshot.Comparison.Changes) < 2 {
		t.Fatalf("expected comparison changes, got %#v", snapshot.Comparison.Changes)
	}
	if !strings.Contains(snapshot.Comparison.DiffPreview, "feature commit") && !strings.Contains(snapshot.Comparison.DiffPreview, "new.txt") {
		t.Fatalf("expected diff preview content, got %q", snapshot.Comparison.DiffPreview)
	}
	if snapshot.UntrackedFiles != 1 {
		t.Fatalf("expected one untracked file, got %d", snapshot.UntrackedFiles)
	}
	if len(snapshot.Changes) == 0 || snapshot.Changes[0].Path == "" {
		t.Fatalf("expected working tree changes, got %#v", snapshot.Changes)
	}
}

func TestParseWorkspaceChangePreservesShortStatusSpacing(t *testing.T) {
	change := parseWorkspaceChange(" M packages/core/src/api.ts")
	if change.StatusCode != " M" {
		t.Fatalf("expected status code ' M', got %q", change.StatusCode)
	}
	if change.Path != "packages/core/src/api.ts" {
		t.Fatalf("expected path packages/core/src/api.ts, got %q", change.Path)
	}
}

func TestParseGitRemoteInfo(t *testing.T) {
	cases := []struct {
		name   string
		remote string
	}{
		{name: "https", remote: "https://github.com/mlhiter/mspace.git"},
		{name: "ssh shorthand", remote: "git@github.com:mlhiter/mspace.git"},
		{name: "ssh url", remote: "ssh://git@github.com/mlhiter/mspace.git"},
		{name: "host path", remote: "github.com/mlhiter/mspace"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, ok := parseGitRemoteInfo(tc.remote)
			if !ok {
				t.Fatalf("expected %q to parse", tc.remote)
			}
			if info.Provider != "github" || info.Owner != "mlhiter" || info.Repo != "mspace" {
				t.Fatalf("unexpected remote info: %+v", info)
			}
		})
	}
}

func TestNormalizeProjectInputDetectsLocalGitHubMetadata(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for project normalization test")
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init", "-b", "main")
	runGit(t, repoDir, "remote", "add", "origin", "git@github.com:mlhiter/mspace.git")

	application := &app{repoRoot: t.TempDir()}
	project, err := application.normalizeProjectInput(projectInput{
		RepoPath: repoDir,
	})
	if err != nil {
		t.Fatalf("normalize project input failed: %v", err)
	}
	if project.SourceType != "local" {
		t.Fatalf("expected local source, got %q", project.SourceType)
	}
	if project.RemoteURL != "git@github.com:mlhiter/mspace.git" {
		t.Fatalf("expected remote url to be detected, got %q", project.RemoteURL)
	}
	if project.GitProvider != "github" || project.GitOwner != "mlhiter" || project.GitRepo != "mspace" {
		t.Fatalf("unexpected git metadata: %+v", project)
	}
	if project.DefaultBranch != "main" {
		t.Fatalf("expected default branch main, got %q", project.DefaultBranch)
	}
}

func TestNormalizeProjectInputUsesExistingGitHubClone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for project normalization test")
	}

	repoRoot := t.TempDir()
	clonePath := filepath.Join(repoRoot, "mlhiter", "mspace")
	if err := os.MkdirAll(clonePath, 0o755); err != nil {
		t.Fatalf("create clone path: %v", err)
	}
	runGit(t, clonePath, "init", "-b", "main")

	application := &app{repoRoot: repoRoot}
	project, err := application.normalizeProjectInput(projectInput{
		SourceType: "github",
		RepoURL:    "https://github.com/mlhiter/mspace.git",
	})
	if err != nil {
		t.Fatalf("normalize github project input failed: %v", err)
	}
	if project.Name != "mspace" {
		t.Fatalf("expected name mspace, got %q", project.Name)
	}
	if project.RepoPath != clonePath {
		t.Fatalf("expected repo path %q, got %q", clonePath, project.RepoPath)
	}
	if project.SourceType != "github" || project.GitOwner != "mlhiter" || project.GitRepo != "mspace" {
		t.Fatalf("unexpected project metadata: %+v", project)
	}
}

func TestEnsureProjectColumnsAddsMetadataFields(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			repo_path TEXT NOT NULL,
			default_branch TEXT NOT NULL,
			deploy_command TEXT NOT NULL,
			validation_command TEXT NOT NULL,
			kube_context TEXT NOT NULL,
			namespace TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`); err != nil {
		t.Fatalf("create old projects table: %v", err)
	}

	application := &app{db: db}
	if err := application.ensureProjectColumns(); err != nil {
		t.Fatalf("ensure project columns failed: %v", err)
	}
	if err := application.ensureProjectColumns(); err != nil {
		t.Fatalf("ensure project columns should be idempotent: %v", err)
	}

	for _, column := range []string{"source_type", "remote_url", "git_provider", "git_owner", "git_repo", "default_cluster_id"} {
		if !projectColumnExists(t, db, column) {
			t.Fatalf("expected projects.%s to exist", column)
		}
	}
}

func TestProjectRunbookArtifactUpdatesCurrentRunbook(t *testing.T) {
	application, db := newAuthTestApp(t)
	insertAuthTestIssue(t, db, "issue-1", "", "Learn project", "open")

	artifactDir := filepath.Join(t.TempDir(), ".mspace", "session")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("create artifact dir: %v", err)
	}
	runbookContent := strings.Join([]string{
		"# Runbook",
		"",
		"## Dependencies",
		"pnpm install",
		"",
		"## Tests",
		"pnpm typecheck",
	}, "\n")
	writeFile(t, filepath.Join(artifactDir, projectRunbookArtifactName), runbookContent)

	application.importProjectRunbookArtifact(agentSession{
		ID:           "session-1",
		IssueID:      "issue-1",
		AgentProfile: "codex",
		ArtifactDir:  artifactDir,
	}, project{ID: "project-1"})

	runbook, err := application.loadProjectRunbook("project-1")
	if err != nil {
		t.Fatalf("load project runbook: %v", err)
	}
	if runbook.Content != runbookContent {
		t.Fatalf("expected runbook content to be stored, got %q", runbook.Content)
	}
	if runbook.Status != "learned" || runbook.Source != "agent" || runbook.SourceSessionID != "session-1" {
		t.Fatalf("unexpected runbook metadata: %+v", runbook)
	}
	var revisionCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM project_runbook_revisions WHERE project_id = 'project-1'`).Scan(&revisionCount); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if revisionCount != 1 {
		t.Fatalf("expected one revision, got %d", revisionCount)
	}
}

func TestEnsureClusterTablesCreatesClusterTable(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db}
	if err := application.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := application.ensureClusterTables(); err != nil {
		t.Fatalf("ensure cluster tables should be idempotent: %v", err)
	}
	for _, column := range []string{"id", "name", "kubeconfig_path", "kube_context", "image_registry_prefix", "exposure_mode", "node_host", "preview_domain", "ingress_class", "status", "last_checked_at"} {
		if !tableColumnExists(t, db, "clusters", column) {
			t.Fatalf("expected clusters.%s to exist", column)
		}
	}
}

func TestEnsureIssueColumnsAddsTriageStatus(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			title TEXT NOT NULL,
			body TEXT NOT NULL,
			status TEXT NOT NULL,
			assignee TEXT NOT NULL,
			environment_url TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`); err != nil {
		t.Fatalf("create old issues table: %v", err)
	}

	application := &app{db: db}
	if err := application.ensureIssueColumns(); err != nil {
		t.Fatalf("ensure issue columns failed: %v", err)
	}
	if err := application.ensureIssueColumns(); err != nil {
		t.Fatalf("ensure issue columns should be idempotent: %v", err)
	}
	if !tableColumnExists(t, db, "issues", "assignee_type") {
		t.Fatal("expected issues.assignee_type to exist")
	}
	if !tableColumnExists(t, db, "issues", "triage_status") {
		t.Fatal("expected issues.triage_status to exist")
	}
	if !tableColumnExists(t, db, "issues", "parent_issue_id") {
		t.Fatal("expected issues.parent_issue_id to exist")
	}
	if !tableColumnExists(t, db, "issues", "sort_order") {
		t.Fatal("expected issues.sort_order to exist")
	}
	if !tableIndexExists(t, db, "issues", "idx_issues_parent_issue_id") {
		t.Fatal("expected idx_issues_parent_issue_id to exist")
	}
	if !tableIndexExists(t, db, "issues", "idx_issues_project_parent_updated") {
		t.Fatal("expected idx_issues_project_parent_updated to exist")
	}
}

func TestEnsureCommentReactionTablesCreatesTable(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db}
	if err := application.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := application.ensureCommentReactionTables(); err != nil {
		t.Fatalf("ensure comment reaction tables should be idempotent: %v", err)
	}
	for _, column := range []string{"id", "issue_id", "comment_id", "reaction", "user_id", "actor_name", "actor_avatar_url", "created_at"} {
		if !tableColumnExists(t, db, "comment_reactions", column) {
			t.Fatalf("expected comment_reactions.%s to exist", column)
		}
	}
	if !tableIndexExists(t, db, "comment_reactions", "idx_comment_reactions_issue_comment") {
		t.Fatal("expected idx_comment_reactions_issue_comment to exist")
	}
	if !tableIndexExists(t, db, "comment_reactions", "idx_comment_reactions_user") {
		t.Fatal("expected idx_comment_reactions_user to exist")
	}
}

func TestExtractIssueTaskDraftsRemovesChecklistLines(t *testing.T) {
	body, tasks := extractIssueTaskDrafts(strings.Join([]string{
		"Implement rich issue editing",
		"",
		"- [ ] Add inline child issue rows",
		"- [x] Keep completed tasks checked",
		"Keep the parent issue body as the durable brief.",
	}, "\n"))

	if !strings.Contains(body, "Implement rich issue editing") || strings.Contains(body, "[ ]") || strings.Contains(body, "[x]") {
		t.Fatalf("expected parent body without checklist lines, got %q", body)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected two tasks, got %#v", tasks)
	}
	if tasks[0].Title != "Add inline child issue rows" || tasks[0].Status != "open" {
		t.Fatalf("unexpected first task: %#v", tasks[0])
	}
	if tasks[1].Title != "Keep completed tasks checked" || tasks[1].Status != "closed" {
		t.Fatalf("unexpected second task: %#v", tasks[1])
	}
}

func TestDeleteIssueTaskRemovesChildIssueOnly(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	application := &app{db: db, broker: newEventBroker()}
	if err := application.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	humanToken := configureTestHumanAuth(t, application)

	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES ('project-1', 'Demo', '/tmp/demo', 'local', '', '', '', '', 'main', '', '', '', '', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issues (id, project_id, parent_issue_id, sort_order, title, body, status, triage_status, assignee, assignee_type, environment_url, created_at, updated_at)
		VALUES
			('issue-1', 'project-1', NULL, 0, 'Parent issue', 'Parent body', 'open', 'pending', 'me', 'human', '', ?, ?),
			('issue-2', 'project-1', NULL, 0, 'Other issue', 'Other body', 'open', 'pending', 'me', 'human', '', ?, ?),
			('task-1', 'project-1', 'issue-1', 1, 'Delete me', '', 'open', 'none', 'me', 'human', '', ?, ?),
			('task-2', 'project-1', 'issue-2', 1, 'Keep me', '', 'open', 'none', 'me', 'human', '', ?, ?)
	`, now, now, now, now, now, now, now, now); err != nil {
		t.Fatalf("insert issues: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO inbox_items (id, issue_id, project_id, title, status, unread, created_at, updated_at)
		VALUES ('inbox-1', 'issue-1', 'project-1', 'Parent issue', 'open', 0, ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert inbox item: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO comments (id, issue_id, author_type, body, created_at)
		VALUES ('comment-1', 'task-1', 'human', 'Task note', ?)
	`, now); err != nil {
		t.Fatalf("insert task comment: %v", err)
	}

	router := chi.NewRouter()
	router.Delete("/api/issues/{issueID}/tasks/{taskID}", application.handleDeleteIssueTask)

	wrongParent := httptest.NewRecorder()
	router.ServeHTTP(wrongParent, authRequest(http.MethodDelete, "/api/issues/issue-2/tasks/task-1", "", humanToken))
	if wrongParent.Code != http.StatusNotFound {
		t.Fatalf("expected wrong parent delete to return 404, got %d body=%s", wrongParent.Code, wrongParent.Body.String())
	}

	correctParent := httptest.NewRecorder()
	router.ServeHTTP(correctParent, authRequest(http.MethodDelete, "/api/issues/issue-1/tasks/task-1", "", humanToken))
	if correctParent.Code != http.StatusOK {
		t.Fatalf("expected delete to return 200, got %d body=%s", correctParent.Code, correctParent.Body.String())
	}
	if !strings.Contains(correctParent.Body.String(), `"ok":true`) {
		t.Fatalf("expected ok response, got %s", correctParent.Body.String())
	}

	var taskCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM issues WHERE id = 'task-1'`).Scan(&taskCount); err != nil {
		t.Fatalf("count deleted task: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected task-1 to be deleted, count=%d", taskCount)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM issues WHERE id IN ('issue-1', 'task-2')`).Scan(&taskCount); err != nil {
		t.Fatalf("count remaining issues: %v", err)
	}
	if taskCount != 2 {
		t.Fatalf("expected parent issue and unrelated task to remain, count=%d", taskCount)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM comments WHERE issue_id = 'task-1'`).Scan(&taskCount); err != nil {
		t.Fatalf("count cascaded comments: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected deleted task comments to cascade, count=%d", taskCount)
	}
	var unread int
	if err := db.QueryRow(`SELECT unread FROM inbox_items WHERE issue_id = 'issue-1'`).Scan(&unread); err != nil {
		t.Fatalf("query inbox unread: %v", err)
	}
	if unread != 1 {
		t.Fatalf("expected parent inbox item to become unread, got %d", unread)
	}
}

func TestGetIssueDoesNotClearInboxUnread(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	application := &app{db: db, broker: newEventBroker()}
	if err := application.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES ('project-1', 'Demo', '/tmp/demo', 'local', '', '', '', '', 'main', '', '', '', '', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issues (id, project_id, parent_issue_id, sort_order, title, body, status, triage_status, assignee, assignee_type, environment_url, created_at, updated_at)
		VALUES ('issue-1', 'project-1', NULL, 0, 'Parent issue', 'Parent body', 'open', 'pending', 'me', 'human', '', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO inbox_items (id, issue_id, project_id, title, status, unread, created_at, updated_at)
		VALUES ('inbox-1', 'issue-1', 'project-1', 'Parent issue', 'open', 1, ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert inbox item: %v", err)
	}

	router := chi.NewRouter()
	router.Get("/api/issues/{issueID}", application.handleGetIssue)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/issues/issue-1", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected get issue to return 200, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var unread int
	if err := db.QueryRow(`SELECT unread FROM inbox_items WHERE issue_id = 'issue-1'`).Scan(&unread); err != nil {
		t.Fatalf("query inbox unread: %v", err)
	}
	if unread != 1 {
		t.Fatalf("expected GET issue to leave inbox unread, got %d", unread)
	}
}

func TestIssueAttachmentUploadServeAndCommentBinding(t *testing.T) {
	application, db := newAuthTestApp(t)
	humanToken := configureTestHumanAuth(t, application)
	insertAuthTestIssue(t, db, "issue-1", "", "Image issue", "open")

	router := chi.NewRouter()
	router.Post("/api/attachments", application.handleUploadAttachment)
	router.Get("/api/attachments/{attachmentID}", application.handleGetAttachment)
	router.Post("/api/issues/{issueID}/comments", application.handleCreateComment)

	imageBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	upload := httptest.NewRecorder()
	router.ServeHTTP(upload, multipartFileRequest(t, http.MethodPost, "/api/attachments", "screenshot.png", imageBytes, humanToken))
	if upload.Code != http.StatusOK {
		t.Fatalf("expected image upload to return 200, got %d body=%s", upload.Code, upload.Body.String())
	}
	var attachment issueAttachment
	if err := json.NewDecoder(upload.Body).Decode(&attachment); err != nil {
		t.Fatalf("decode attachment: %v", err)
	}
	if attachment.ID == "" || attachment.URL != "/api/attachments/"+attachment.ID {
		t.Fatalf("unexpected attachment response: %+v", attachment)
	}
	if attachment.StorageBackend != "sqlite_blob" || attachment.ContentType != "image/png" {
		t.Fatalf("unexpected attachment storage metadata: %+v", attachment)
	}

	served := httptest.NewRecorder()
	router.ServeHTTP(served, httptest.NewRequest(http.MethodGet, attachment.URL, nil))
	if served.Code != http.StatusOK {
		t.Fatalf("expected attachment GET to return 200, got %d body=%s", served.Code, served.Body.String())
	}
	if served.Header().Get("Content-Type") != "image/png" || !bytes.Equal(served.Body.Bytes(), imageBytes) {
		t.Fatalf("unexpected served attachment content type=%q body=%v", served.Header().Get("Content-Type"), served.Body.Bytes())
	}

	payload, err := json.Marshal(map[string]any{
		"body":          "Screenshot attached.\n\n![screenshot](" + attachment.URL + ")",
		"attachmentIds": []string{attachment.ID},
	})
	if err != nil {
		t.Fatalf("marshal comment payload: %v", err)
	}
	comment := httptest.NewRecorder()
	router.ServeHTTP(comment, authRequest(http.MethodPost, "/api/issues/issue-1/comments", string(payload), humanToken))
	if comment.Code != http.StatusOK {
		t.Fatalf("expected comment to return 200, got %d body=%s", comment.Code, comment.Body.String())
	}
	var commentResponse struct {
		CommentID string `json:"commentId"`
	}
	if err := json.NewDecoder(comment.Body).Decode(&commentResponse); err != nil {
		t.Fatalf("decode comment response: %v", err)
	}
	if commentResponse.CommentID == "" {
		t.Fatalf("expected comment id response, got %#v", commentResponse)
	}

	var issueID, commentID, boundAt string
	if err := db.QueryRow(`
		SELECT issue_id, comment_id, bound_at
		FROM issue_attachments
		WHERE id = ?
	`, attachment.ID).Scan(&issueID, &commentID, &boundAt); err != nil {
		t.Fatalf("query bound attachment: %v", err)
	}
	if issueID != "issue-1" || commentID != commentResponse.CommentID || boundAt == "" {
		t.Fatalf("unexpected bound attachment issue=%q comment=%q boundAt=%q", issueID, commentID, boundAt)
	}
}

func TestUpdateLatestHumanComment(t *testing.T) {
	application, db := newAuthTestApp(t)
	humanToken := configureTestHumanAuth(t, application)
	insertAuthTestIssue(t, db, "issue-1", "", "Editable issue", "open")

	router := chi.NewRouter()
	router.Post("/api/issues/{issueID}/comments", application.handleCreateComment)
	router.Put("/api/issues/{issueID}/comments/{commentID}", application.handleUpdateComment)

	create := httptest.NewRecorder()
	router.ServeHTTP(create, authRequest(http.MethodPost, "/api/issues/issue-1/comments", `{"body":"@codex please do the wrong thing"}`, humanToken))
	if create.Code != http.StatusOK {
		t.Fatalf("expected comment create to return 200, got %d body=%s", create.Code, create.Body.String())
	}
	var createResponse struct {
		CommentID string `json:"commentId"`
	}
	if err := json.NewDecoder(create.Body).Decode(&createResponse); err != nil {
		t.Fatalf("decode create comment response: %v", err)
	}

	update := httptest.NewRecorder()
	router.ServeHTTP(update, authRequest(http.MethodPut, "/api/issues/issue-1/comments/"+createResponse.CommentID, `{"body":"@codex please do the corrected thing"}`, humanToken))
	if update.Code != http.StatusOK {
		t.Fatalf("expected comment update to return 200, got %d body=%s", update.Code, update.Body.String())
	}

	var body, authorUserID, editedAt string
	if err := db.QueryRow(`SELECT body, author_user_id, edited_at FROM comments WHERE id = ?`, createResponse.CommentID).Scan(&body, &authorUserID, &editedAt); err != nil {
		t.Fatalf("query updated comment: %v", err)
	}
	if body != "@codex please do the corrected thing" {
		t.Fatalf("expected updated body, got %q", body)
	}
	if authorUserID != "user-1" {
		t.Fatalf("expected comment author user id to be stored, got %q", authorUserID)
	}
	if editedAt == "" {
		t.Fatalf("expected edited_at to be set")
	}
}

func TestUpdateCommentRejectsSessionTrigger(t *testing.T) {
	application, db := newAuthTestApp(t)
	humanToken := configureTestHumanAuth(t, application)
	insertAuthTestIssue(t, db, "issue-1", "", "Consumed comment issue", "open")

	router := chi.NewRouter()
	router.Post("/api/issues/{issueID}/comments", application.handleCreateComment)
	router.Put("/api/issues/{issueID}/comments/{commentID}", application.handleUpdateComment)

	create := httptest.NewRecorder()
	router.ServeHTTP(create, authRequest(http.MethodPost, "/api/issues/issue-1/comments", `{"body":"@codex original turn"}`, humanToken))
	if create.Code != http.StatusOK {
		t.Fatalf("expected comment create to return 200, got %d body=%s", create.Code, create.Body.String())
	}
	var createResponse struct {
		CommentID string `json:"commentId"`
	}
	if err := json.NewDecoder(create.Body).Decode(&createResponse); err != nil {
		t.Fatalf("decode create comment response: %v", err)
	}
	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, trigger_comment_id, created_at, updated_at)
		VALUES ('session-1', 'issue-1', 'codex', 'codex', 'local', 'original turn', 'completed', 'mspace/issue/session', '/tmp/workdir', ?, ?, ?)
	`, createResponse.CommentID, now, now); err != nil {
		t.Fatalf("insert consumed session: %v", err)
	}

	update := httptest.NewRecorder()
	router.ServeHTTP(update, authRequest(http.MethodPut, "/api/issues/issue-1/comments/"+createResponse.CommentID, `{"body":"@codex edited turn"}`, humanToken))
	if update.Code != http.StatusConflict {
		t.Fatalf("expected consumed comment update to return 409, got %d body=%s", update.Code, update.Body.String())
	}
}

func TestCommentReactionsToggleAndAggregateByViewer(t *testing.T) {
	application, db := newAuthTestApp(t)
	humanToken := configureTestHumanAuth(t, application)
	insertAuthTestIssue(t, db, "issue-1", "", "Reactable issue", "open")

	router := chi.NewRouter()
	router.Get("/api/issues/{issueID}", application.handleGetIssue)
	router.Post("/api/issues/{issueID}/comments", application.handleCreateComment)
	router.Put("/api/issues/{issueID}/comments/{commentID}/reactions/{reaction}", application.handleSetCommentReaction)
	router.Delete("/api/issues/{issueID}/comments/{commentID}/reactions/{reaction}", application.handleDeleteCommentReaction)

	create := httptest.NewRecorder()
	router.ServeHTTP(create, authRequest(http.MethodPost, "/api/issues/issue-1/comments", `{"body":"Looks good"}`, humanToken))
	if create.Code != http.StatusOK {
		t.Fatalf("expected comment create to return 200, got %d body=%s", create.Code, create.Body.String())
	}
	var createResponse struct {
		CommentID string `json:"commentId"`
	}
	if err := json.NewDecoder(create.Body).Decode(&createResponse); err != nil {
		t.Fatalf("decode create comment response: %v", err)
	}

	react := httptest.NewRecorder()
	router.ServeHTTP(react, authRequest(http.MethodPut, "/api/issues/issue-1/comments/"+createResponse.CommentID+"/reactions/thumbs_up", "", humanToken))
	if react.Code != http.StatusOK {
		t.Fatalf("expected reaction create to return 200, got %d body=%s", react.Code, react.Body.String())
	}
	again := httptest.NewRecorder()
	router.ServeHTTP(again, authRequest(http.MethodPut, "/api/issues/issue-1/comments/"+createResponse.CommentID+"/reactions/thumbs_up", "", humanToken))
	if again.Code != http.StatusOK {
		t.Fatalf("expected duplicate reaction create to return 200, got %d body=%s", again.Code, again.Body.String())
	}

	detailRecorder := httptest.NewRecorder()
	router.ServeHTTP(detailRecorder, authRequest(http.MethodGet, "/api/issues/issue-1", "", humanToken))
	if detailRecorder.Code != http.StatusOK {
		t.Fatalf("expected issue detail to return 200, got %d body=%s", detailRecorder.Code, detailRecorder.Body.String())
	}
	var detail issueDetail
	if err := json.NewDecoder(detailRecorder.Body).Decode(&detail); err != nil {
		t.Fatalf("decode issue detail: %v", err)
	}
	if len(detail.Comments) != 1 || len(detail.Comments[0].Reactions) != 1 {
		t.Fatalf("expected one comment reaction summary, got %+v", detail.Comments)
	}
	reaction := detail.Comments[0].Reactions[0]
	if reaction.Reaction != "thumbs_up" || reaction.Count != 1 || !reaction.ReactedByMe {
		t.Fatalf("unexpected reaction summary: %+v", reaction)
	}

	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, authRequest(http.MethodPut, "/api/issues/issue-1/comments/"+createResponse.CommentID+"/reactions/not_real", "", humanToken))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid reaction to return 400, got %d body=%s", invalid.Code, invalid.Body.String())
	}

	remove := httptest.NewRecorder()
	router.ServeHTTP(remove, authRequest(http.MethodDelete, "/api/issues/issue-1/comments/"+createResponse.CommentID+"/reactions/thumbs_up", "", humanToken))
	if remove.Code != http.StatusOK {
		t.Fatalf("expected reaction delete to return 200, got %d body=%s", remove.Code, remove.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM comment_reactions WHERE comment_id = ?`, createResponse.CommentID).Scan(&count); err != nil {
		t.Fatalf("count comment reactions: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected deleted reaction count 0, got %d", count)
	}
}

func TestCreateIssueBindsUploadedAttachments(t *testing.T) {
	application, db := newAuthTestApp(t)
	humanToken := configureTestHumanAuth(t, application)
	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES ('project-1', 'Demo', '/tmp/demo', 'local', '', '', '', '', 'main', '', '', '', '', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	router := chi.NewRouter()
	router.Post("/api/attachments", application.handleUploadAttachment)
	router.Post("/api/issues", application.handleCreateIssue)

	upload := httptest.NewRecorder()
	router.ServeHTTP(upload, multipartFileRequest(t, http.MethodPost, "/api/attachments", "issue.png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, humanToken))
	if upload.Code != http.StatusOK {
		t.Fatalf("expected image upload to return 200, got %d body=%s", upload.Code, upload.Body.String())
	}
	var attachment issueAttachment
	if err := json.NewDecoder(upload.Body).Decode(&attachment); err != nil {
		t.Fatalf("decode attachment: %v", err)
	}

	payload, err := json.Marshal(map[string]any{
		"prompt":        "Investigate screenshot.\n\n![issue](" + attachment.URL + ")",
		"attachmentIds": []string{attachment.ID},
		"labelKeys":     []string{"type:fix"},
	})
	if err != nil {
		t.Fatalf("marshal issue payload: %v", err)
	}
	create := httptest.NewRecorder()
	router.ServeHTTP(create, authRequest(http.MethodPost, "/api/issues", string(payload), humanToken))
	if create.Code != http.StatusOK {
		t.Fatalf("expected issue create to return 200, got %d body=%s", create.Code, create.Body.String())
	}
	var createResponse struct {
		IssueID string `json:"issueId"`
	}
	if err := json.NewDecoder(create.Body).Decode(&createResponse); err != nil {
		t.Fatalf("decode issue response: %v", err)
	}
	if createResponse.IssueID == "" {
		t.Fatalf("expected issue id response, got %#v", createResponse)
	}

	var issueID string
	var commentID sql.NullString
	if err := db.QueryRow(`
		SELECT issue_id, comment_id
		FROM issue_attachments
		WHERE id = ?
	`, attachment.ID).Scan(&issueID, &commentID); err != nil {
		t.Fatalf("query issue attachment: %v", err)
	}
	if issueID != createResponse.IssueID || commentID.Valid {
		t.Fatalf("unexpected issue attachment binding issue=%q comment=%v", issueID, commentID)
	}
}

func TestIssueAttachmentUploadRejectsNonImages(t *testing.T) {
	application, _ := newAuthTestApp(t)
	humanToken := configureTestHumanAuth(t, application)
	router := chi.NewRouter()
	router.Post("/api/attachments", application.handleUploadAttachment)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, multipartFileRequest(t, http.MethodPost, "/api/attachments", "note.txt", []byte("hello"), humanToken))
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected non-image upload to return 415, got %d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestTopLevelIssueCloseRequiresHumanAuth(t *testing.T) {
	application, db := newAuthTestApp(t)
	humanToken := configureTestHumanAuth(t, application)
	insertAuthTestIssue(t, db, "issue-1", "", "Top issue", "open")
	agentToken := insertAuthTestAgentSession(t, db, "session-1", "issue-1")

	router := chi.NewRouter()
	router.Put("/api/issues/{issueID}", application.handleUpdateIssue)
	body := `{"status":"closed"}`

	unauthenticated := httptest.NewRecorder()
	router.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodPut, "/api/issues/issue-1", strings.NewReader(body)))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated close to return 401, got %d body=%s", unauthenticated.Code, unauthenticated.Body.String())
	}

	agent := httptest.NewRecorder()
	router.ServeHTTP(agent, authRequest(http.MethodPut, "/api/issues/issue-1", body, agentToken))
	if agent.Code != http.StatusForbidden {
		t.Fatalf("expected agent top-level close to return 403, got %d body=%s", agent.Code, agent.Body.String())
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM issues WHERE id = 'issue-1'`).Scan(&status); err != nil {
		t.Fatalf("query issue status: %v", err)
	}
	if status != "open" {
		t.Fatalf("expected unauthorized close attempts to leave issue open, got %q", status)
	}

	human := httptest.NewRecorder()
	router.ServeHTTP(human, authRequest(http.MethodPut, "/api/issues/issue-1", body, humanToken))
	if human.Code != http.StatusOK {
		t.Fatalf("expected human close to return 200, got %d body=%s", human.Code, human.Body.String())
	}
	if err := db.QueryRow(`SELECT status FROM issues WHERE id = 'issue-1'`).Scan(&status); err != nil {
		t.Fatalf("query issue status after human close: %v", err)
	}
	if status != "closed" {
		t.Fatalf("expected human close to set closed, got %q", status)
	}
	assertCommentContains(t, db, "issue-1", "Issue status changed from `open` to `closed` by Test Human.")
	assertCommentAuthorContains(t, db, "issue-1", "Issue status changed from `open` to `closed` by Test Human.", "human", "Test Human")

	reopen := httptest.NewRecorder()
	router.ServeHTTP(reopen, authRequest(http.MethodPut, "/api/issues/issue-1", `{"status":"changes_requested"}`, humanToken))
	if reopen.Code != http.StatusOK {
		t.Fatalf("expected human reopen for changes to return 200, got %d body=%s", reopen.Code, reopen.Body.String())
	}
	if err := db.QueryRow(`SELECT status FROM issues WHERE id = 'issue-1'`).Scan(&status); err != nil {
		t.Fatalf("query issue status after reopen: %v", err)
	}
	if status != "changes_requested" {
		t.Fatalf("expected reopen to set changes_requested, got %q", status)
	}

	reopenToOpen := httptest.NewRecorder()
	router.ServeHTTP(reopenToOpen, authRequest(http.MethodPut, "/api/issues/issue-1", `{"status":"open"}`, humanToken))
	if reopenToOpen.Code != http.StatusForbidden {
		t.Fatalf("expected human reopen to open to return 403, got %d body=%s", reopenToOpen.Code, reopenToOpen.Body.String())
	}
}

func TestAgentStatusChangeIsScopedAndRecorded(t *testing.T) {
	application, db := newAuthTestApp(t)
	humanToken := configureTestHumanAuth(t, application)
	insertAuthTestIssue(t, db, "issue-1", "", "Top issue", "open")
	insertAuthTestIssue(t, db, "task-1", "issue-1", "Task issue", "open")
	agentToken := insertAuthTestAgentSession(t, db, "session-1", "issue-1")

	router := chi.NewRouter()
	router.Put("/api/issues/{issueID}", application.handleUpdateIssue)

	humanReview := httptest.NewRecorder()
	router.ServeHTTP(humanReview, authRequest(http.MethodPut, "/api/issues/issue-1", `{"status":"needs_review"}`, humanToken))
	if humanReview.Code != http.StatusForbidden {
		t.Fatalf("expected human review handoff transition to return 403, got %d body=%s", humanReview.Code, humanReview.Body.String())
	}

	review := httptest.NewRecorder()
	router.ServeHTTP(review, authRequest(http.MethodPut, "/api/issues/issue-1", `{"status":"needs_review"}`, agentToken))
	if review.Code != http.StatusOK {
		t.Fatalf("expected scoped agent review transition to return 200, got %d body=%s", review.Code, review.Body.String())
	}
	var issueStatus string
	if err := db.QueryRow(`SELECT status FROM issues WHERE id = 'issue-1'`).Scan(&issueStatus); err != nil {
		t.Fatalf("query issue status: %v", err)
	}
	if issueStatus != "needs_review" {
		t.Fatalf("expected issue status needs_review, got %q", issueStatus)
	}
	assertCommentContains(t, db, "issue-1", "Issue status changed from `open` to `needs_review` by Codex.")
	assertCommentAuthorContains(t, db, "issue-1", "Issue status changed from `open` to `needs_review` by Codex.", "agent", "Codex")

	humanRequestChanges := httptest.NewRecorder()
	router.ServeHTTP(humanRequestChanges, authRequest(http.MethodPut, "/api/issues/issue-1", `{"status":"changes_requested"}`, humanToken))
	if humanRequestChanges.Code != http.StatusForbidden {
		t.Fatalf("expected human request-changes status edit to return 403, got %d body=%s", humanRequestChanges.Code, humanRequestChanges.Body.String())
	}

	closeTask := httptest.NewRecorder()
	router.ServeHTTP(closeTask, authRequest(http.MethodPut, "/api/issues/task-1", `{"status":"closed"}`, agentToken))
	if closeTask.Code != http.StatusOK {
		t.Fatalf("expected scoped agent child-task close to return 200, got %d body=%s", closeTask.Code, closeTask.Body.String())
	}
	var taskStatus string
	if err := db.QueryRow(`SELECT status FROM issues WHERE id = 'task-1'`).Scan(&taskStatus); err != nil {
		t.Fatalf("query task status: %v", err)
	}
	if taskStatus != "closed" {
		t.Fatalf("expected child task to close, got %q", taskStatus)
	}
	assertCommentContains(t, db, "issue-1", "Task `Task issue` status changed from `open` to `closed` by Codex.")
	assertCommentAuthorContains(t, db, "issue-1", "Task `Task issue` status changed from `open` to `closed` by Codex.", "agent", "Codex")
}

func TestCancelQueuedSessionKeepsIssueStatusForRetry(t *testing.T) {
	application, db := newAuthTestApp(t)
	humanToken := configureTestHumanAuth(t, application)
	insertAuthTestIssue(t, db, "issue-1", "", "Retryable issue", "open")
	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, created_at, updated_at)
		VALUES ('session-1', 'issue-1', 'codex', 'codex', 'local', 'bad prompt', 'queued', 'mspace/issue/session', '/tmp/workdir', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert queued session: %v", err)
	}

	router := chi.NewRouter()
	router.Post("/api/sessions/{sessionID}/cancel", application.handleCancelSession)

	cancel := httptest.NewRecorder()
	router.ServeHTTP(cancel, authRequest(http.MethodPost, "/api/sessions/session-1/cancel", "", humanToken))
	if cancel.Code != http.StatusOK {
		t.Fatalf("expected cancel to return 200, got %d body=%s", cancel.Code, cancel.Body.String())
	}

	var sessionStatus, issueStatus string
	if err := db.QueryRow(`SELECT status FROM agent_sessions WHERE id = 'session-1'`).Scan(&sessionStatus); err != nil {
		t.Fatalf("query session status: %v", err)
	}
	if sessionStatus != "cancelled" {
		t.Fatalf("expected session to be cancelled, got %q", sessionStatus)
	}
	if err := db.QueryRow(`SELECT status FROM issues WHERE id = 'issue-1'`).Scan(&issueStatus); err != nil {
		t.Fatalf("query issue status: %v", err)
	}
	if issueStatus != "open" {
		t.Fatalf("expected issue status to stay open after session cancel, got %q", issueStatus)
	}
	assertCommentAuthorContains(t, db, "issue-1", "Stopped session `session-` by Test Human.", "system", "mspace")
}

func TestRetainIssueTestEnvironmentKeepsNamespaceLifecycle(t *testing.T) {
	application, db := newAuthTestApp(t)
	humanToken := configureTestHumanAuth(t, application)
	insertAuthTestIssue(t, db, "issue-1", "", "Retain namespace", "ready_for_test")
	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO issue_test_environments (issue_id, namespace, namespace_status, cleanup_status, preview_url, created_at, updated_at)
		VALUES ('issue-1', 'ns-demo', 'active', 'cleanup_failed', 'http://192.0.2.10:30080/', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert test environment: %v", err)
	}

	router := chi.NewRouter()
	router.Post("/api/issues/{issueID}/test-environment/retain", application.handleRetainIssueTestEnvironment)

	retain := httptest.NewRecorder()
	router.ServeHTTP(retain, authRequest(http.MethodPost, "/api/issues/issue-1/test-environment/retain", "", humanToken))
	if retain.Code != http.StatusOK {
		t.Fatalf("expected retain to return 200, got %d body=%s", retain.Code, retain.Body.String())
	}

	var namespaceStatus, cleanupStatus string
	if err := db.QueryRow(`SELECT namespace_status, cleanup_status FROM issue_test_environments WHERE issue_id = 'issue-1'`).Scan(&namespaceStatus, &cleanupStatus); err != nil {
		t.Fatalf("query test environment: %v", err)
	}
	if namespaceStatus != "active" || cleanupStatus != "retained" {
		t.Fatalf("expected retain to preserve namespace lifecycle and update decision, got namespace=%q cleanup=%q", namespaceStatus, cleanupStatus)
	}
	assertCommentAuthorContains(t, db, "issue-1", "Retained test namespace `ns-demo` for later inspection.", "system", "mspace")
}

func TestRetainIssueTestEnvironmentRejectsActiveSession(t *testing.T) {
	application, db := newAuthTestApp(t)
	humanToken := configureTestHumanAuth(t, application)
	insertAuthTestIssue(t, db, "issue-1", "", "Retain with active session", "ready_for_test")
	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, agent_status, created_at, updated_at)
		VALUES ('session-1', 'issue-1', 'codex', 'codex', 'local', 'deploy', 'running', 'mspace/issue/session', '/tmp/workdir', 'reasoning', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert running session: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issue_test_environments (issue_id, namespace, namespace_status, cleanup_status, preview_url, created_at, updated_at)
		VALUES ('issue-1', 'ns-demo', 'active', 'cleanup_failed', 'http://192.0.2.10:30080/', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert test environment: %v", err)
	}

	router := chi.NewRouter()
	router.Post("/api/issues/{issueID}/test-environment/retain", application.handleRetainIssueTestEnvironment)

	retain := httptest.NewRecorder()
	router.ServeHTTP(retain, authRequest(http.MethodPost, "/api/issues/issue-1/test-environment/retain", "", humanToken))
	if retain.Code != http.StatusConflict {
		t.Fatalf("expected retain to return 409, got %d body=%s", retain.Code, retain.Body.String())
	}

	var namespaceStatus, cleanupStatus string
	if err := db.QueryRow(`SELECT namespace_status, cleanup_status FROM issue_test_environments WHERE issue_id = 'issue-1'`).Scan(&namespaceStatus, &cleanupStatus); err != nil {
		t.Fatalf("query test environment: %v", err)
	}
	if namespaceStatus != "active" || cleanupStatus != "cleanup_failed" {
		t.Fatalf("expected failed retain to leave environment unchanged, got namespace=%q cleanup=%q", namespaceStatus, cleanupStatus)
	}
}

func TestListIssueTestEnvironmentResourcesRejectsNamespaceOverride(t *testing.T) {
	application, db := newAuthTestApp(t)
	humanToken := configureTestHumanAuth(t, application)
	insertAuthTestIssue(t, db, "issue-1", "", "Inspect namespace", "ready_for_test")

	router := chi.NewRouter()
	router.Get("/api/issues/{issueID}/test-environment/resources", application.handleListIssueTestEnvironmentResources)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, authRequest(http.MethodGet, "/api/issues/issue-1/test-environment/resources?namespace=other-ns", "", humanToken))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected namespace override to return 400, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "namespace is fixed by the issue test environment") {
		t.Fatalf("expected fixed namespace error, got %s", recorder.Body.String())
	}
}

func TestCancelOrphanedRunningSessionAllowsStop(t *testing.T) {
	application, db := newAuthTestApp(t)
	humanToken := configureTestHumanAuth(t, application)
	insertAuthTestIssue(t, db, "issue-1", "", "Deploy issue", "open")
	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, agent_status, created_at, updated_at)
		VALUES ('session-1', 'issue-1', 'codex', 'codex', 'local', 'deploy', 'running', 'mspace/issue/session', '/tmp/workdir', 'reasoning', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert running session: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issue_test_environments (issue_id, namespace, namespace_status, cleanup_status, last_deploy_session_id, created_at, updated_at)
		VALUES ('issue-1', 'ns-demo', 'deploying', 'retained', 'session-1', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert test environment: %v", err)
	}

	router := chi.NewRouter()
	router.Post("/api/sessions/{sessionID}/cancel", application.handleCancelSession)

	cancel := httptest.NewRecorder()
	router.ServeHTTP(cancel, authRequest(http.MethodPost, "/api/sessions/session-1/cancel", "", humanToken))
	if cancel.Code != http.StatusOK {
		t.Fatalf("expected cancel to return 200, got %d body=%s", cancel.Code, cancel.Body.String())
	}

	var sessionStatus, agentStatus, issueStatus, namespaceStatus string
	if err := db.QueryRow(`SELECT status, agent_status FROM agent_sessions WHERE id = 'session-1'`).Scan(&sessionStatus, &agentStatus); err != nil {
		t.Fatalf("query session status: %v", err)
	}
	if sessionStatus != "cancelled" || agentStatus != "cancelled" {
		t.Fatalf("expected cancelled session, got status=%q agent_status=%q", sessionStatus, agentStatus)
	}
	if err := db.QueryRow(`SELECT status FROM issues WHERE id = 'issue-1'`).Scan(&issueStatus); err != nil {
		t.Fatalf("query issue status: %v", err)
	}
	if issueStatus != "open" {
		t.Fatalf("expected stop to leave issue open, got %q", issueStatus)
	}
	if err := db.QueryRow(`SELECT namespace_status FROM issue_test_environments WHERE issue_id = 'issue-1'`).Scan(&namespaceStatus); err != nil {
		t.Fatalf("query namespace status: %v", err)
	}
	if namespaceStatus != "deploy_interrupted" {
		t.Fatalf("expected interrupted deploy environment to be marked interrupted, got %q", namespaceStatus)
	}
	assertCommentAuthorContains(t, db, "issue-1", "Stopped session `session-` by Test Human.", "system", "mspace")
	assertSessionLogContains(t, db, "session-1", "lost its in-memory handle")
}

func TestStartupReconcilesInterruptedActiveSessions(t *testing.T) {
	application, db := newAuthTestApp(t)
	insertAuthTestIssue(t, db, "issue-1", "", "Restarted runner issue", "open")
	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, agent_status, created_at, updated_at)
		VALUES ('session-1', 'issue-1', 'codex', 'codex', 'local', 'deploy', 'running', 'mspace/issue/session', '/tmp/workdir', 'reasoning', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert running session: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issue_test_environments (issue_id, namespace, namespace_status, cleanup_status, last_deploy_session_id, created_at, updated_at)
		VALUES ('issue-1', 'ns-demo', 'deploying', 'retained', 'session-1', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert test environment: %v", err)
	}

	if err := application.reconcileInterruptedSessionsOnStartup(); err != nil {
		t.Fatalf("reconcile interrupted sessions: %v", err)
	}

	var sessionStatus, agentStatus, issueStatus, namespaceStatus string
	if err := db.QueryRow(`SELECT status, agent_status FROM agent_sessions WHERE id = 'session-1'`).Scan(&sessionStatus, &agentStatus); err != nil {
		t.Fatalf("query session status: %v", err)
	}
	if sessionStatus != "failed" || agentStatus != "interrupted" {
		t.Fatalf("expected interrupted session to fail, got status=%q agent_status=%q", sessionStatus, agentStatus)
	}
	if err := db.QueryRow(`SELECT status FROM issues WHERE id = 'issue-1'`).Scan(&issueStatus); err != nil {
		t.Fatalf("query issue status: %v", err)
	}
	if issueStatus != "blocked" {
		t.Fatalf("expected interrupted session to block issue, got %q", issueStatus)
	}
	if err := db.QueryRow(`SELECT namespace_status FROM issue_test_environments WHERE issue_id = 'issue-1'`).Scan(&namespaceStatus); err != nil {
		t.Fatalf("query namespace status: %v", err)
	}
	if namespaceStatus != "deploy_interrupted" {
		t.Fatalf("expected interrupted deploy environment to be marked interrupted, got %q", namespaceStatus)
	}
	if application.issueHasActiveSession("issue-1") {
		t.Fatal("expected startup reconciliation to clear active session state")
	}
	assertCommentAuthorContains(t, db, "issue-1", "Session `session-` was interrupted.", "system", "mspace")
	assertSessionLogContains(t, db, "session-1", "runner restarted before this session completed")
}

func TestProbePreviewCandidatesTreatsClientErrorsAsOpen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "topic not found", http.StatusNotFound)
	}))
	defer server.Close()

	result := probePreviewCandidates([]previewCandidate{{URL: server.URL, Source: "nodeport"}})
	if !result.OK || result.URL != server.URL || result.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 preview to count as open, got %+v", result)
	}
}

func TestProbePreviewCandidatesRejectsServerErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "booting", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	result := probePreviewCandidates([]previewCandidate{{URL: server.URL, Source: "nodeport"}})
	if result.OK || !strings.Contains(result.Error, "HTTP 503") {
		t.Fatalf("expected 503 preview to fail, got %+v", result)
	}
}

func TestProbeIssueTestEnvironmentOnlyUpdatesEnvironmentState(t *testing.T) {
	application, db := newAuthTestApp(t)
	humanToken := configureTestHumanAuth(t, application)
	insertAuthTestIssue(t, db, "issue-1", "", "Probe namespace", "blocked")
	preview := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer preview.Close()

	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, agent_status, created_at, updated_at)
		VALUES ('session-1', 'issue-1', 'codex', 'codex', 'local', 'deploy', 'completed', 'mspace/issue/session', '/tmp/workdir', 'completed', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert deploy session: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issue_test_environments (issue_id, namespace, namespace_status, cleanup_status, preview_url, last_deploy_session_id, created_at, updated_at)
		VALUES ('issue-1', 'ns-demo', 'preview_unverified', 'retained', ?, 'session-1', ?, ?)
	`, preview.URL, now, now); err != nil {
		t.Fatalf("insert test environment: %v", err)
	}

	router := chi.NewRouter()
	router.Post("/api/issues/{issueID}/test-environment/probe", application.handleProbeIssueTestEnvironment)

	probe := httptest.NewRecorder()
	router.ServeHTTP(probe, authRequest(http.MethodPost, "/api/issues/issue-1/test-environment/probe", "", humanToken))
	if probe.Code != http.StatusOK {
		t.Fatalf("expected probe to return 200, got %d body=%s", probe.Code, probe.Body.String())
	}

	var namespaceStatus, cleanupStatus, issueStatus string
	if err := db.QueryRow(`SELECT namespace_status, cleanup_status FROM issue_test_environments WHERE issue_id = 'issue-1'`).Scan(&namespaceStatus, &cleanupStatus); err != nil {
		t.Fatalf("query test environment: %v", err)
	}
	if namespaceStatus != "active" || cleanupStatus != "retained" {
		t.Fatalf("expected probe to update only environment status, got namespace=%q cleanup=%q", namespaceStatus, cleanupStatus)
	}
	if err := db.QueryRow(`SELECT status FROM issues WHERE id = 'issue-1'`).Scan(&issueStatus); err != nil {
		t.Fatalf("query issue status: %v", err)
	}
	if issueStatus != "blocked" {
		t.Fatalf("expected probe to leave issue status unchanged, got %q", issueStatus)
	}
	for table, query := range map[string]string{
		"deployment_evidence":     `SELECT COUNT(*) FROM deployment_evidence WHERE issue_id = 'issue-1'`,
		"session_review_evidence": `SELECT COUNT(*) FROM session_review_evidence WHERE issue_id = 'issue-1'`,
		"session_failures":        `SELECT COUNT(*) FROM session_failures WHERE issue_id = 'issue-1'`,
	} {
		var count int
		if err := db.QueryRow(query).Scan(&count); err != nil {
			t.Fatalf("query %s count: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("expected probe not to create %s rows, got %d", table, count)
		}
	}
}

func TestUpdateIssueTestEnvironmentAdoptsPreviewArtifactFromContinuationSession(t *testing.T) {
	application, db := newAuthTestApp(t)
	insertAuthTestIssue(t, db, "issue-1", "", "Continue deployment", "blocked")
	now := nowString()
	artifactDir := filepath.Join(t.TempDir(), "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("create artifact dir: %v", err)
	}
	writeFile(t, filepath.Join(artifactDir, "test-environment.json"), `{"previewUrl":"http://192.0.2.10:30080/"}`)

	for _, session := range []struct {
		id        string
		status    string
		artifact  string
		agentStat string
	}{
		{id: "old-deploy-session", status: "failed", agentStat: "failed"},
		{id: "continuation-session", status: "failed", artifact: artifactDir, agentStat: "failed"},
	} {
		if _, err := db.Exec(`
			INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, cleanup_status, cleaned_at, created_at, updated_at)
			VALUES (?, 'issue-1', 'codex', 'codex', 'local', 'continue deployment', ?, ?, ?, '', '', ?, ?, 'retained', '', ?, ?)
		`, session.id, session.status, "mspace/issue/"+session.id, t.TempDir(), session.agentStat, session.artifact, now, now); err != nil {
			t.Fatalf("insert session %s: %v", session.id, err)
		}
	}
	if _, err := db.Exec(`
		INSERT INTO issue_test_environments (issue_id, namespace, namespace_status, cleanup_status, preview_url, last_deploy_session_id, source_session_id, source_commit_sha, created_at, updated_at)
		VALUES ('issue-1', 'ns-demo', 'deploy_failed', 'retained', '', 'old-deploy-session', 'source-session', 'abcdef1234567890', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert test environment: %v", err)
	}

	application.updateIssueTestEnvironmentForSession(agentSession{
		ID:          "continuation-session",
		IssueID:     "issue-1",
		Status:      "failed",
		ArtifactDir: artifactDir,
	}, false)

	var namespaceStatus, cleanupStatus, previewURL, lastDeploySessionID, sourceSessionID, sourceCommitSHA, issueStatus string
	if err := db.QueryRow(`
		SELECT namespace_status, cleanup_status, preview_url, last_deploy_session_id, source_session_id, source_commit_sha
		FROM issue_test_environments
		WHERE issue_id = 'issue-1'
	`).Scan(&namespaceStatus, &cleanupStatus, &previewURL, &lastDeploySessionID, &sourceSessionID, &sourceCommitSHA); err != nil {
		t.Fatalf("query test environment: %v", err)
	}
	if namespaceStatus != "active" || cleanupStatus != "retained" || previewURL != "http://192.0.2.10:30080/" || lastDeploySessionID != "continuation-session" {
		t.Fatalf("expected continuation session preview to activate environment, got status=%q cleanup=%q preview=%q last=%q", namespaceStatus, cleanupStatus, previewURL, lastDeploySessionID)
	}
	if sourceSessionID != "source-session" || sourceCommitSHA != "abcdef1234567890" {
		t.Fatalf("expected existing source selection to be preserved, got source_session=%q source_commit=%q", sourceSessionID, sourceCommitSHA)
	}
	if err := db.QueryRow(`SELECT status FROM issues WHERE id = 'issue-1'`).Scan(&issueStatus); err != nil {
		t.Fatalf("query issue status: %v", err)
	}
	if issueStatus != "ready_for_test" {
		t.Fatalf("expected issue to return to ready_for_test, got %q", issueStatus)
	}
	assertSessionLogContains(t, db, "continuation-session", "Updated issue test environment from session preview artifact.")
}

func TestMigrateClosedIssueStatusesKeepsSessionCompletion(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	application := &app{db: db, broker: newEventBroker()}
	if err := application.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES ('project-1', 'Demo', '/tmp/demo', 'local', '', '', '', '', 'main', '', '', '', '', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issues (id, project_id, parent_issue_id, sort_order, title, body, status, triage_status, assignee, assignee_type, environment_url, created_at, updated_at)
		VALUES ('issue-1', 'project-1', NULL, 0, 'Parent issue', 'Parent body', 'completed', 'pending', 'me', 'human', '', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO inbox_items (id, issue_id, project_id, title, status, unread, created_at, updated_at)
		VALUES ('inbox-1', 'issue-1', 'project-1', 'Parent issue', 'completed', 1, ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert inbox item: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issues (id, project_id, parent_issue_id, sort_order, title, body, status, triage_status, assignee, assignee_type, environment_url, created_at, updated_at)
		VALUES
			('issue-progress', 'project-1', NULL, 0, 'Progress issue', '', 'in_progress', 'pending', 'me', 'human', '', ?, ?),
			('issue-testing', 'project-1', NULL, 0, 'Testing issue', '', 'test_in_progress', 'pending', 'me', 'human', '', ?, ?)
	`, now, now, now, now); err != nil {
		t.Fatalf("insert transient status issues: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO inbox_items (id, issue_id, project_id, title, status, unread, created_at, updated_at)
		VALUES
			('inbox-progress', 'issue-progress', 'project-1', 'Progress issue', 'in_progress', 1, ?, ?),
			('inbox-testing', 'issue-testing', 'project-1', 'Testing issue', 'test_in_progress', 1, ?, ?)
	`, now, now, now, now); err != nil {
		t.Fatalf("insert transient status inbox items: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, cleanup_status, cleaned_at, created_at, updated_at)
		VALUES ('session-1', 'issue-1', 'codex', 'codex', 'local', 'done', 'completed', 'mspace/issue/session', '/tmp/workdir', '', '', 'completed', '', 'retained', '', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	if err := application.migrateClosedIssueStatuses(); err != nil {
		t.Fatalf("migrate closed issue statuses: %v", err)
	}

	var issueStatus, inboxStatus, sessionStatus, progressIssueStatus, testingIssueStatus, progressInboxStatus, testingInboxStatus string
	if err := db.QueryRow(`SELECT status FROM issues WHERE id = 'issue-1'`).Scan(&issueStatus); err != nil {
		t.Fatalf("query issue status: %v", err)
	}
	if err := db.QueryRow(`SELECT status FROM inbox_items WHERE id = 'inbox-1'`).Scan(&inboxStatus); err != nil {
		t.Fatalf("query inbox status: %v", err)
	}
	if err := db.QueryRow(`SELECT status FROM agent_sessions WHERE id = 'session-1'`).Scan(&sessionStatus); err != nil {
		t.Fatalf("query session status: %v", err)
	}
	if err := db.QueryRow(`SELECT status FROM issues WHERE id = 'issue-progress'`).Scan(&progressIssueStatus); err != nil {
		t.Fatalf("query progress issue status: %v", err)
	}
	if err := db.QueryRow(`SELECT status FROM issues WHERE id = 'issue-testing'`).Scan(&testingIssueStatus); err != nil {
		t.Fatalf("query testing issue status: %v", err)
	}
	if err := db.QueryRow(`SELECT status FROM inbox_items WHERE id = 'inbox-progress'`).Scan(&progressInboxStatus); err != nil {
		t.Fatalf("query progress inbox status: %v", err)
	}
	if err := db.QueryRow(`SELECT status FROM inbox_items WHERE id = 'inbox-testing'`).Scan(&testingInboxStatus); err != nil {
		t.Fatalf("query testing inbox status: %v", err)
	}
	if issueStatus != "closed" || inboxStatus != "closed" || sessionStatus != "completed" {
		t.Fatalf("unexpected statuses issue=%q inbox=%q session=%q", issueStatus, inboxStatus, sessionStatus)
	}
	if progressIssueStatus != "open" || progressInboxStatus != "open" {
		t.Fatalf("expected progress statuses to migrate to open, got issue=%q inbox=%q", progressIssueStatus, progressInboxStatus)
	}
	if testingIssueStatus != "needs_review" || testingInboxStatus != "needs_review" {
		t.Fatalf("expected test progress statuses to migrate to needs_review, got issue=%q inbox=%q", testingIssueStatus, testingInboxStatus)
	}
}

func TestEnsureSessionColumnsAddsCodexFields(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE agent_sessions (
			id TEXT PRIMARY KEY,
			issue_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			runtime_mode TEXT NOT NULL,
			command TEXT NOT NULL,
			status TEXT NOT NULL,
			branch TEXT NOT NULL,
			workdir TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`); err != nil {
		t.Fatalf("create old agent_sessions table: %v", err)
	}

	application := &app{db: db}
	if err := application.ensureSessionColumns(); err != nil {
		t.Fatalf("ensure session columns failed: %v", err)
	}
	if err := application.ensureSessionColumns(); err != nil {
		t.Fatalf("ensure session columns should be idempotent: %v", err)
	}

	for _, column := range []string{"codex_thread_id", "codex_turn_id", "agent_status", "artifact_dir", "agent_profile", "source_session_id", "source_commit_sha", "agent_token", "cleanup_status", "cleaned_at"} {
		if !tableColumnExists(t, db, "agent_sessions", column) {
			t.Fatalf("expected agent_sessions.%s to exist", column)
		}
	}
}

func TestEnsureIssueLabelTablesCreatesLabelTable(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			title TEXT NOT NULL,
			body TEXT NOT NULL,
			status TEXT NOT NULL,
			assignee TEXT NOT NULL,
			assignee_type TEXT NOT NULL,
			environment_url TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`); err != nil {
		t.Fatalf("create issues table: %v", err)
	}

	application := &app{db: db}
	if err := application.ensureIssueLabelTables(); err != nil {
		t.Fatalf("ensure issue label tables failed: %v", err)
	}
	if err := application.ensureIssueLabelTables(); err != nil {
		t.Fatalf("ensure issue label tables should be idempotent: %v", err)
	}
	if !tableColumnExists(t, db, "issue_labels", "issue_id") {
		t.Fatal("expected issue_labels.issue_id to exist")
	}
	if !tableColumnExists(t, db, "issue_labels", "name") {
		t.Fatal("expected issue_labels.name to exist")
	}
	if !tableColumnExists(t, db, "issue_labels", "label_id") {
		t.Fatal("expected issue_labels.label_id to exist")
	}
	if !tableColumnExists(t, db, "issue_label_definitions", "key") {
		t.Fatal("expected issue_label_definitions.key to exist")
	}
	definitions, err := application.listIssueLabelDefinitions()
	if err != nil {
		t.Fatalf("list issue label definitions: %v", err)
	}
	if len(definitions) != 15 {
		t.Fatalf("expected 15 built-in label definitions, got %#v", definitions)
	}
}

func TestEnsureIssueTestEnvironmentTablesCreatesEnvironmentTable(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
		CREATE TABLE issues (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			title TEXT NOT NULL,
			body TEXT NOT NULL,
			status TEXT NOT NULL,
			assignee TEXT NOT NULL,
			assignee_type TEXT NOT NULL,
			environment_url TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`); err != nil {
		t.Fatalf("create issues table: %v", err)
	}

	application := &app{db: db}
	if err := application.ensureIssueTestEnvironmentTables(); err != nil {
		t.Fatalf("ensure issue test environment tables failed: %v", err)
	}
	if err := application.ensureIssueTestEnvironmentTables(); err != nil {
		t.Fatalf("ensure issue test environment tables should be idempotent: %v", err)
	}
	for _, column := range []string{"issue_id", "cluster_id", "namespace", "namespace_status", "cleanup_status", "preview_url", "image_registry_prefix", "kubeconfig_path", "kube_context", "exposure_mode", "last_deploy_session_id", "last_cleanup_session_id", "source_session_id", "source_commit_sha"} {
		if !tableColumnExists(t, db, "issue_test_environments", column) {
			t.Fatalf("expected issue_test_environments.%s to exist", column)
		}
	}
}

func TestEnsureIssueChangeNodeTablesCreatesTable(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db}
	if err := application.ensureIssueChangeNodeTables(); err != nil {
		t.Fatalf("ensure issue change node tables failed: %v", err)
	}
	if err := application.ensureIssueChangeNodeTables(); err != nil {
		t.Fatalf("ensure issue change node tables should be idempotent: %v", err)
	}
	for _, column := range []string{"id", "issue_id", "session_id", "commit_sha", "branch", "subject", "files_changed", "created_at"} {
		if !tableColumnExists(t, db, "issue_change_nodes", column) {
			t.Fatalf("expected issue_change_nodes.%s to exist", column)
		}
	}
	if !tableIndexExists(t, db, "issue_change_nodes", "idx_issue_change_nodes_issue_created") {
		t.Fatal("expected issue change node issue index to exist")
	}
}

func TestEnsureSessionReviewEvidenceTablesCreatesTable(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db}
	if err := application.ensureSessionReviewEvidenceTables(); err != nil {
		t.Fatalf("ensure session review evidence tables failed: %v", err)
	}
	if err := application.ensureSessionReviewEvidenceTables(); err != nil {
		t.Fatalf("ensure session review evidence tables should be idempotent: %v", err)
	}
	for _, column := range []string{"id", "issue_id", "session_id", "source_session_id", "source_commit_sha", "branch", "agent_summary", "commands_json", "tests_json", "build_result_json", "deployment_result_json", "risks_json", "follow_ups_json", "preview_url", "cluster", "namespace", "namespace_status", "cleanup_status", "created_at", "updated_at"} {
		if !tableColumnExists(t, db, "session_review_evidence", column) {
			t.Fatalf("expected session_review_evidence.%s to exist", column)
		}
	}
	if !tableIndexExists(t, db, "session_review_evidence", "idx_session_review_evidence_issue_created") {
		t.Fatal("expected session review evidence issue index to exist")
	}
}

func TestEnsureIssueHandoffTablesCreatesTable(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db}
	if err := application.ensureIssueHandoffTables(); err != nil {
		t.Fatalf("ensure issue handoff tables failed: %v", err)
	}
	if err := application.ensureIssueHandoffTables(); err != nil {
		t.Fatalf("ensure issue handoff tables should be idempotent: %v", err)
	}
	for _, column := range []string{"id", "issue_id", "source_session_id", "source_commit_sha", "branch", "head_commit_sha", "commits_json", "kind", "pr_url", "pr_number", "pr_state", "pr_title", "preview_url", "evidence_summary", "created_via", "last_checked_at", "error", "created_at", "updated_at"} {
		if !tableColumnExists(t, db, "issue_handoffs", column) {
			t.Fatalf("expected issue_handoffs.%s to exist", column)
		}
	}
	if !tableIndexExists(t, db, "issue_handoffs", "idx_issue_handoffs_issue_updated") {
		t.Fatal("expected issue handoff issue index to exist")
	}
}

func TestEnsureWorkspaceSettingsTablesCreatesDefault(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db}
	if err := application.ensureWorkspaceSettingsTables(); err != nil {
		t.Fatalf("ensure workspace settings tables failed: %v", err)
	}
	if err := application.ensureWorkspaceSettingsTables(); err != nil {
		t.Fatalf("ensure workspace settings tables should be idempotent: %v", err)
	}
	for _, column := range []string{"id", "auto_create_draft_pr", "created_at", "updated_at"} {
		if !tableColumnExists(t, db, "workspace_settings", column) {
			t.Fatalf("expected workspace_settings.%s to exist", column)
		}
	}
	settings, err := application.loadWorkspaceSettings()
	if err != nil {
		t.Fatalf("load workspace settings: %v", err)
	}
	if settings.AutoCreateDraftPR {
		t.Fatal("expected automatic draft PRs to be disabled by default")
	}
}

func TestUpdateWorkspaceSettingsRoundTrip(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db}
	if err := application.ensureWorkspaceSettingsTables(); err != nil {
		t.Fatalf("ensure workspace settings tables failed: %v", err)
	}
	settings, err := application.updateWorkspaceSettings(workspaceSettingsInput{AutoCreateDraftPR: true})
	if err != nil {
		t.Fatalf("update workspace settings: %v", err)
	}
	if !settings.AutoCreateDraftPR {
		t.Fatal("expected automatic draft PRs to be enabled")
	}
	loaded, err := application.loadWorkspaceSettings()
	if err != nil {
		t.Fatalf("load workspace settings: %v", err)
	}
	if !loaded.AutoCreateDraftPR {
		t.Fatal("expected loaded workspace settings to preserve automatic draft PRs")
	}
}

func TestBuildIssueHandoffUsesBranchCommitsAndReviewEvidence(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for handoff test")
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init", "-b", "main")
	runGit(t, repoDir, "config", "user.name", "mspace test")
	runGit(t, repoDir, "config", "user.email", "mspace@example.com")
	writeFile(t, filepath.Join(repoDir, "README.md"), "# demo\n")
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "base")
	runGit(t, repoDir, "checkout", "-b", "mspace/issue/session-1")
	writeFile(t, filepath.Join(repoDir, "app.txt"), "hello\n")
	runGit(t, repoDir, "add", "app.txt")
	runGit(t, repoDir, "commit", "-m", "feat: add app")
	sourceCommit := strings.TrimSpace(gitOutput(t, repoDir, "rev-parse", "HEAD"))

	application, db := newAuthTestApp(t)
	insertAuthTestIssue(t, db, "issue-1", "", "Ship branch handoff", "needs_review")
	insertAuthTestAgentSession(t, db, "session-1", "issue-1")
	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO issue_change_nodes (id, issue_id, session_id, commit_sha, branch, subject, files_changed, created_at)
		VALUES ('node-1', 'issue-1', 'session-1', ?, 'mspace/issue/session-1', 'feat: add app', 1, ?)
	`, sourceCommit, now); err != nil {
		t.Fatalf("insert issue change node: %v", err)
	}

	detail := issueDetail{
		Issue: issue{
			ID:    "issue-1",
			Title: "Ship branch handoff",
		},
		Project: project{
			ID:            "project-1",
			Name:          "Demo",
			RepoPath:      repoDir,
			DefaultBranch: "main",
		},
		ReviewEvidence: []sessionReviewEvidence{{
			ID:               "review-1",
			IssueID:          "issue-1",
			SessionID:        "deploy-session",
			SourceSessionID:  "session-1",
			SourceCommitSHA:  sourceCommit,
			Branch:           "mspace/issue/session-1",
			AgentSummary:     "Implemented the source change.",
			Tests:            []reviewEvidenceCheck{{Name: "go test ./...", Status: "passed", Summary: "ok"}},
			BuildResult:      reviewEvidenceResult{Status: "passed", Summary: "build succeeded"},
			DeploymentResult: reviewEvidenceResult{Status: "completed", Summary: "preview deployed"},
			PreviewURL:       "https://preview.example.com/issue-1",
			Risks:            []string{"manual QA still recommended"},
		}},
	}

	handoff, err := application.buildIssueHandoff(detail, issueHandoffRequest{
		SourceSessionID: "session-1",
		SourceCommitSHA: sourceCommit,
		Kind:            "pr",
	}, "gh")
	if err != nil {
		t.Fatalf("build issue handoff failed: %v", err)
	}
	if handoff.Branch != "mspace/issue/session-1" {
		t.Fatalf("expected handoff branch from source node, got %q", handoff.Branch)
	}
	if handoff.HeadCommitSHA != sourceCommit {
		t.Fatalf("expected handoff head commit %q, got %q", sourceCommit, handoff.HeadCommitSHA)
	}
	if len(handoff.Commits) != 1 || handoff.Commits[0].SHA != sourceCommit || handoff.Commits[0].Subject != "feat: add app" {
		t.Fatalf("unexpected handoff commits: %+v", handoff.Commits)
	}
	if handoff.PreviewURL != "https://preview.example.com/issue-1" {
		t.Fatalf("expected preview URL from review evidence, got %q", handoff.PreviewURL)
	}
	for _, expected := range []string{"Agent summary: Implemented the source change.", "Tests: go test ./... passed - ok", "Preview URL: https://preview.example.com/issue-1"} {
		if !strings.Contains(handoff.EvidenceSummary, expected) {
			t.Fatalf("expected evidence summary to contain %q, got:\n%s", expected, handoff.EvidenceSummary)
		}
	}

	body := buildIssuePullRequestBody(detail, handoff)
	for _, expected := range []string{"Issue: `issue-1`", "Title: Ship branch handoff", "Branch: `mspace/issue/session-1`", "https://preview.example.com/issue-1", "Agent summary: Implemented the source change."} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected PR body to contain %q, got:\n%s", expected, body)
		}
	}
}

func TestStoreIssueHandoffKeepsOnePullRequestPerIssue(t *testing.T) {
	application, db := newAuthTestApp(t)
	insertAuthTestIssue(t, db, "issue-1", "", "One issue PR", "needs_review")

	first, err := application.storeIssueHandoff(issueHandoff{
		IssueID:         "issue-1",
		SourceSessionID: "session-1",
		SourceCommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Branch:          "mspace/issue/session-1",
		Kind:            "pr",
		PRURL:           "https://github.com/mlhiter/mspace/pull/1",
		PRNumber:        1,
		PRState:         "open",
		CreatedVia:      "manual",
	})
	if err != nil {
		t.Fatalf("store first handoff: %v", err)
	}
	second, err := application.storeIssueHandoff(issueHandoff{
		IssueID:         "issue-1",
		SourceSessionID: "session-2",
		SourceCommitSHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Branch:          "mspace/issue/session-2",
		Kind:            "pr",
		PRURL:           "https://github.com/mlhiter/mspace/pull/2",
		PRNumber:        2,
		PRState:         "open",
		CreatedVia:      "manual",
	})
	if err != nil {
		t.Fatalf("store second handoff: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected second PR handoff to update first row, got first=%q second=%q", first.ID, second.ID)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM issue_handoffs WHERE issue_id = 'issue-1' AND kind = 'pr'`).Scan(&count); err != nil {
		t.Fatalf("query handoff count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one PR handoff row, got %d", count)
	}
	var prURL string
	if err := db.QueryRow(`SELECT pr_url FROM issue_handoffs WHERE issue_id = 'issue-1'`).Scan(&prURL); err != nil {
		t.Fatalf("query handoff URL: %v", err)
	}
	if prURL != "https://github.com/mlhiter/mspace/pull/2" {
		t.Fatalf("expected current PR URL to update, got %q", prURL)
	}
}

func TestIssueHandoffCommentLinksPullRequest(t *testing.T) {
	comment := issueHandoffComment(issueHandoff{
		PRURL:           "https://github.com/mlhiter/mspace/pull/12",
		PRNumber:        12,
		PRState:         "open",
		Branch:          "mspace/issue/session-1",
		SourceCommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	})
	if !strings.Contains(comment, "[PR #12](https://github.com/mlhiter/mspace/pull/12)") {
		t.Fatalf("expected PR handoff comment to contain a markdown PR link, got %q", comment)
	}
	if strings.Contains(comment, "`https://github.com") {
		t.Fatalf("expected PR URL not to be rendered as inline code, got %q", comment)
	}
}

func TestEnsureSessionFailureTablesCreatesTable(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db}
	if err := application.ensureSessionFailureTables(); err != nil {
		t.Fatalf("ensure session failure tables failed: %v", err)
	}
	if err := application.ensureSessionFailureTables(); err != nil {
		t.Fatalf("ensure session failure tables should be idempotent: %v", err)
	}
	for _, column := range []string{"id", "issue_id", "session_id", "phase", "status", "failed_command", "error_summary", "error_excerpt", "cluster", "namespace", "resource_kind", "resource_name", "evidence_id", "review_evidence_id", "retry_session_id", "continued_session_id", "created_at", "updated_at"} {
		if !tableColumnExists(t, db, "session_failures", column) {
			t.Fatalf("expected session_failures.%s to exist", column)
		}
	}
	if !tableIndexExists(t, db, "session_failures", "idx_session_failures_issue_created") {
		t.Fatal("expected session failures issue index to exist")
	}
}

func TestBuildIssueTestEnvironmentUsesSelectedCluster(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db}
	if err := application.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := nowString()
	if err := application.insertCluster(cluster{
		ID:                  "cluster-1",
		Name:                "Test",
		KubeconfigPath:      "/tmp/test.kubeconfig",
		KubeContext:         "test-context",
		ImageRegistryPrefix: "registry.example.com/mspace",
		ExposureMode:        "ingress",
		NodeHost:            "1.2.3.4",
		PreviewDomain:       "preview.example.com",
		IngressClass:        "nginx",
		Status:              "configured",
		CreatedAt:           now,
		UpdatedAt:           now,
	}); err != nil {
		t.Fatalf("insert cluster: %v", err)
	}

	environment, err := application.buildIssueTestEnvironment(issueDetail{
		Issue: issue{
			ID: "issue-1",
		},
		Project: project{
			ID:               "project-1",
			Name:             "Demo",
			DefaultClusterID: "cluster-1",
		},
	}, testDeployRequest{ExposureMode: "nodeport"})
	if err != nil {
		t.Fatalf("build issue test environment failed: %v", err)
	}
	if environment.ClusterID != "cluster-1" {
		t.Fatalf("expected cluster id cluster-1, got %q", environment.ClusterID)
	}
	if environment.KubeconfigPath != "/tmp/test.kubeconfig" || environment.KubeContext != "test-context" {
		t.Fatalf("unexpected kube settings: %+v", environment)
	}
	if environment.ImageRegistryPrefix != "registry.example.com/mspace" {
		t.Fatalf("unexpected registry prefix: %q", environment.ImageRegistryPrefix)
	}
	if environment.ExposureMode != "nodeport" || environment.PreviewDomain != "" || environment.IngressClass != "" || environment.NodeHost != "1.2.3.4" {
		t.Fatalf("expected nodeport override to clear ingress fields, got %+v", environment)
	}
}

func TestDiscoverDefaultKubeconfigPathsListsRegularFiles(t *testing.T) {
	kubeDir := t.TempDir()
	configPath := filepath.Join(kubeDir, "config")
	prodPath := filepath.Join(kubeDir, "prod.yaml")
	writeFile(t, configPath, "apiVersion: v1\n")
	writeFile(t, prodPath, "apiVersion: v1\n")
	if err := os.Mkdir(filepath.Join(kubeDir, "cache"), 0o755); err != nil {
		t.Fatalf("create cache dir: %v", err)
	}

	paths, err := discoverDefaultKubeconfigPaths(kubeDir)
	if err != nil {
		t.Fatalf("discover default kubeconfig paths: %v", err)
	}
	expected := []string{configPath, prodPath}
	if strings.Join(paths, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("expected paths %#v, got %#v", expected, paths)
	}
}

func TestDiscoverKubeconfigsReturnsSelectableCandidates(t *testing.T) {
	binDir := t.TempDir()
	kubectlPath := filepath.Join(binDir, "kubectl")
	writeFile(t, kubectlPath, `#!/bin/sh
cat <<'JSON'
{"current-context":"beta","contexts":[{"name":"alpha"},{"name":"beta"},{"name":"alpha"}]}
JSON
`)
	if err := os.Chmod(kubectlPath, 0o755); err != nil {
		t.Fatalf("chmod kubectl: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	kubeDir := t.TempDir()
	firstPath := filepath.Join(kubeDir, "first")
	secondPath := filepath.Join(kubeDir, "second")
	missingPath := filepath.Join(kubeDir, "missing")
	writeFile(t, firstPath, "apiVersion: v1\n")
	writeFile(t, secondPath, "apiVersion: v1\n")

	result := discoverKubeconfigs([]string{firstPath, secondPath, firstPath, missingPath})
	if len(result.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %#v", result.Candidates)
	}
	if result.Candidates[0].Path != firstPath || result.Candidates[1].Path != secondPath {
		t.Fatalf("unexpected candidate paths: %#v", result.Candidates)
	}
	for _, candidate := range result.Candidates {
		if strings.Join(candidate.Contexts, ",") != "beta,alpha" {
			t.Fatalf("expected current context first and deduped contexts, got %#v", candidate.Contexts)
		}
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Path != missingPath || !strings.Contains(result.Skipped[0].Reason, "does not exist") {
		t.Fatalf("expected missing kubeconfig skip, got %#v", result.Skipped)
	}
}

func TestEnsureAgentProfileTablesSeedsDefaults(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db}
	if err := application.ensureAgentProfileTables(); err != nil {
		t.Fatalf("ensure agent profile tables failed: %v", err)
	}
	if err := application.ensureAgentProfileTables(); err != nil {
		t.Fatalf("ensure agent profile tables should be idempotent: %v", err)
	}

	for _, column := range []string{"id", "name", "mention", "provider", "description", "instructions", "enabled", "built_in", "sort_order"} {
		if !tableColumnExists(t, db, "agent_profiles", column) {
			t.Fatalf("expected agent_profiles.%s to exist", column)
		}
	}

	profiles, err := application.listAgentProfiles()
	if err != nil {
		t.Fatalf("list agent profiles: %v", err)
	}
	if len(profiles) != 4 {
		t.Fatalf("expected four default profiles, got %#v", profiles)
	}
	if profiles[0].ID != "triage" || profiles[1].ID != "codex" || profiles[2].ID != "bugfix" || profiles[3].ID != "design" {
		t.Fatalf("unexpected default profile order: %#v", profiles)
	}

	design, err := application.resolveEnabledAgentProfile("@design")
	if err != nil {
		t.Fatalf("resolve design profile: %v", err)
	}
	if design.Name != "Design" || !strings.Contains(design.Instructions, "design profile") {
		t.Fatalf("unexpected design profile: %#v", design)
	}
}

func TestResolveAgentProfileRejectsDisabledProfiles(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db}
	if err := application.ensureAgentProfileTables(); err != nil {
		t.Fatalf("ensure agent profile tables failed: %v", err)
	}
	enabled := false
	profile, err := normalizeAgentProfileInput(agentProfile{}, agentProfileInput{
		Name:         "Reviewer",
		Mention:      "@review",
		Provider:     "codex",
		Description:  "Review changes",
		Instructions: "Review the change for correctness.",
		Enabled:      &enabled,
	}, false)
	if err != nil {
		t.Fatalf("normalize profile: %v", err)
	}
	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO agent_profiles (id, name, mention, provider, description, instructions, enabled, built_in, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, profile.ID, profile.Name, profile.Mention, profile.Provider, profile.Description, profile.Instructions, boolToInt(profile.Enabled), boolToInt(profile.BuiltIn), profile.SortOrder, now, now); err != nil {
		t.Fatalf("insert profile: %v", err)
	}

	if loaded, err := application.loadAgentProfile("review"); err != nil || loaded.ID != "review" {
		t.Fatalf("expected disabled profile to remain loadable, profile=%#v err=%v", loaded, err)
	}
	if _, err := application.resolveEnabledAgentProfile("@review"); !errors.Is(err, errUnknownAgentProfile) {
		t.Fatalf("expected disabled profile to be rejected for new sessions, got %v", err)
	}
}

func TestRecordSourceChangeNodeCommitsWorkspace(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for source change node test")
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init", "-b", "main")
	runGit(t, repoDir, "config", "user.name", "mspace test")
	runGit(t, repoDir, "config", "user.email", "mspace@example.com")
	writeFile(t, filepath.Join(repoDir, "README.md"), "# demo\n")
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "base")

	workdirRoot := filepath.Join(t.TempDir(), "workdirs")
	sessionWorkdir := filepath.Join(workdirRoot, "project-1", "session-1")
	if err := os.MkdirAll(filepath.Dir(sessionWorkdir), 0o755); err != nil {
		t.Fatalf("create worktree parent: %v", err)
	}
	runGit(t, repoDir, "worktree", "add", "-b", "mspace/issue/session-1", sessionWorkdir, "HEAD")
	writeFile(t, filepath.Join(sessionWorkdir, "app.txt"), "hello\n")
	if err := os.MkdirAll(filepath.Join(sessionWorkdir, ".mspace", "session"), 0o755); err != nil {
		t.Fatalf("create artifact dir: %v", err)
	}
	writeFile(t, filepath.Join(sessionWorkdir, ".mspace", "session", "test-environment.json"), `{"previewUrl":"http://example.test"}`)

	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db, broker: newEventBroker(), workdir: workdirRoot, repoRoot: t.TempDir()}
	if err := application.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := nowString()
	project := project{
		ID:            "project-1",
		Name:          "Demo",
		RepoPath:      repoDir,
		SourceType:    "local",
		DefaultBranch: "main",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', '', '', '', ?, '', '', '', '', ?, ?)
	`, project.ID, project.Name, project.RepoPath, project.SourceType, project.DefaultBranch, project.CreatedAt, project.UpdatedAt); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issues (id, project_id, title, body, status, assignee, assignee_type, environment_url, created_at, updated_at)
		VALUES ('issue-1', ?, 'Add app file', '', 'running', 'codex', 'agent', '', ?, ?)
	`, project.ID, now, now); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, cleanup_status, cleaned_at, created_at, updated_at)
		VALUES ('session-1', 'issue-1', 'codex', 'codex', 'local', 'Implement the issue.', 'running', 'mspace/issue/session-1', ?, '', '', 'running', '', 'retained', '', ?, ?)
	`, sessionWorkdir, now, now); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	worktreeGitDir := strings.TrimSpace(gitOutput(t, sessionWorkdir, "rev-parse", "--git-dir"))
	if !filepath.IsAbs(worktreeGitDir) {
		worktreeGitDir = filepath.Join(sessionWorkdir, worktreeGitDir)
	}
	indexLockPath := filepath.Join(worktreeGitDir, "index.lock")
	writeFile(t, indexLockPath, "locked\n")
	time.AfterFunc(300*time.Millisecond, func() {
		_ = os.Remove(indexLockPath)
	})
	t.Cleanup(func() {
		_ = os.Remove(indexLockPath)
	})

	node, err := application.recordSourceChangeNode(agentSession{
		ID:           "session-1",
		IssueID:      "issue-1",
		Provider:     "codex",
		AgentProfile: "codex",
		Command:      "Implement the issue.",
		Status:       "running",
		Branch:       "mspace/issue/session-1",
		Workdir:      sessionWorkdir,
	}, project)
	if err != nil {
		t.Fatalf("record source change node failed: %v", err)
	}
	if node == nil {
		t.Fatal("expected source change node")
	}
	if node.FilesChanged != 1 || len(node.Changes) != 1 || node.Changes[0].Path != "app.txt" {
		t.Fatalf("expected only app.txt to be committed, got %+v", node)
	}
	if strings.Contains(node.DiffPreview, ".mspace") {
		t.Fatalf("expected .mspace artifacts to be excluded, diff=%s", node.DiffPreview)
	}

	loaded, err := application.loadIssueChangeNodeForDeploy("issue-1", node.CommitSHA, node.SessionID, project)
	if err != nil {
		t.Fatalf("load issue change node for deploy failed: %v", err)
	}
	if loaded.CommitSHA != node.CommitSHA || loaded.SessionID != "session-1" {
		t.Fatalf("unexpected deploy source node: %+v", loaded)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM issue_change_nodes WHERE issue_id = 'issue-1' AND session_id = 'session-1'`).Scan(&count); err != nil {
		t.Fatalf("query issue change node count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one issue change node row, got %d", count)
	}
}

func TestRecordSourceChangeNodeCapturesExistingSessionCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for source change node test")
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init", "-b", "main")
	runGit(t, repoDir, "config", "user.name", "mspace test")
	runGit(t, repoDir, "config", "user.email", "mspace@example.com")
	writeFile(t, filepath.Join(repoDir, "README.md"), "# demo\n")
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "base")

	workdirRoot := filepath.Join(t.TempDir(), "workdirs")
	sessionWorkdir := filepath.Join(workdirRoot, "project-1", "session-1")
	if err := os.MkdirAll(filepath.Dir(sessionWorkdir), 0o755); err != nil {
		t.Fatalf("create worktree parent: %v", err)
	}
	runGit(t, repoDir, "worktree", "add", "-b", "mspace/issue/session-1", sessionWorkdir, "HEAD")
	writeFile(t, filepath.Join(sessionWorkdir, "app.txt"), "hello\n")
	runGit(t, sessionWorkdir, "add", "app.txt")
	runGit(t, sessionWorkdir, "commit", "-m", "agent commit")
	existingCommit := strings.TrimSpace(gitOutput(t, sessionWorkdir, "rev-parse", "HEAD"))
	if err := os.MkdirAll(filepath.Join(sessionWorkdir, ".mspace", "session"), 0o755); err != nil {
		t.Fatalf("create artifact dir: %v", err)
	}
	writeFile(t, filepath.Join(sessionWorkdir, ".mspace", "session", "review-evidence.json"), `{"agentSummary":"done"}`)

	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db, broker: newEventBroker(), workdir: workdirRoot, repoRoot: t.TempDir()}
	if err := application.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := nowString()
	project := project{
		ID:            "project-1",
		Name:          "Demo",
		RepoPath:      repoDir,
		SourceType:    "local",
		DefaultBranch: "main",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', '', '', '', ?, '', '', '', '', ?, ?)
	`, project.ID, project.Name, project.RepoPath, project.SourceType, project.DefaultBranch, project.CreatedAt, project.UpdatedAt); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issues (id, project_id, title, body, status, assignee, assignee_type, environment_url, created_at, updated_at)
		VALUES ('issue-1', ?, 'Polish UI', '', 'running', 'codex', 'agent', '', ?, ?)
	`, project.ID, now, now); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, cleanup_status, cleaned_at, created_at, updated_at)
		VALUES ('session-1', 'issue-1', 'codex', 'codex', 'local', 'Implement the issue.', 'running', 'mspace/issue/session-1', ?, '', '', 'running', '', 'retained', '', ?, ?)
	`, sessionWorkdir, now, now); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	node, err := application.recordSourceChangeNode(agentSession{
		ID:           "session-1",
		IssueID:      "issue-1",
		Provider:     "codex",
		AgentProfile: "codex",
		Command:      "Implement the issue.",
		Status:       "running",
		Branch:       "mspace/issue/session-1",
		Workdir:      sessionWorkdir,
	}, project)
	if err != nil {
		t.Fatalf("record source change node failed: %v", err)
	}
	if node == nil {
		t.Fatal("expected source change node for existing commit")
	}
	if node.CommitSHA != existingCommit {
		t.Fatalf("expected existing commit %s, got %s", existingCommit, node.CommitSHA)
	}
	if node.Subject != "agent commit" {
		t.Fatalf("expected existing commit subject, got %q", node.Subject)
	}
	if node.FilesChanged != 1 || len(node.Changes) != 1 || node.Changes[0].Path != "app.txt" {
		t.Fatalf("expected only app.txt to be captured, got %+v", node)
	}
	if strings.TrimSpace(gitOutput(t, sessionWorkdir, "rev-parse", "HEAD")) != existingCommit {
		t.Fatalf("expected recordSourceChangeNode not to create an extra commit")
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM issue_change_nodes WHERE issue_id = 'issue-1' AND session_id = 'session-1' AND commit_sha = ?`, existingCommit).Scan(&count); err != nil {
		t.Fatalf("query issue change node count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one issue change node row for existing commit, got %d", count)
	}
}

func TestRecordSourceChangeNodeSkipsDuplicateExistingIssueCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for source change node test")
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init", "-b", "main")
	runGit(t, repoDir, "config", "user.name", "mspace test")
	runGit(t, repoDir, "config", "user.email", "mspace@example.com")
	writeFile(t, filepath.Join(repoDir, "README.md"), "# demo\n")
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "base")

	workdirRoot := filepath.Join(t.TempDir(), "workdirs")
	firstWorkdir := filepath.Join(workdirRoot, "project-1", "session-1")
	secondWorkdir := filepath.Join(workdirRoot, "project-1", "session-2")
	if err := os.MkdirAll(filepath.Dir(firstWorkdir), 0o755); err != nil {
		t.Fatalf("create worktree parent: %v", err)
	}
	runGit(t, repoDir, "worktree", "add", "-b", "mspace/issue/session-1", firstWorkdir, "HEAD")
	writeFile(t, filepath.Join(firstWorkdir, "app.txt"), "hello\n")
	runGit(t, firstWorkdir, "add", "app.txt")
	runGit(t, firstWorkdir, "commit", "-m", "agent commit")
	existingCommit := strings.TrimSpace(gitOutput(t, firstWorkdir, "rev-parse", "HEAD"))
	runGit(t, repoDir, "worktree", "add", "--detach", secondWorkdir, existingCommit)

	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db, broker: newEventBroker(), workdir: workdirRoot, repoRoot: t.TempDir()}
	if err := application.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := nowString()
	project := project{
		ID:            "project-1",
		Name:          "Demo",
		RepoPath:      repoDir,
		SourceType:    "local",
		DefaultBranch: "main",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', '', '', '', ?, '', '', '', '', ?, ?)
	`, project.ID, project.Name, project.RepoPath, project.SourceType, project.DefaultBranch, project.CreatedAt, project.UpdatedAt); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issues (id, project_id, title, body, status, assignee, assignee_type, environment_url, created_at, updated_at)
		VALUES ('issue-1', ?, 'Polish UI', '', 'running', 'codex', 'agent', '', ?, ?)
	`, project.ID, now, now); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	for _, session := range []struct {
		id      string
		command string
		workdir string
	}{
		{id: "session-1", command: "Implement the issue.", workdir: firstWorkdir},
		{id: "session-2", command: "Continue deployment.", workdir: secondWorkdir},
	} {
		if _, err := db.Exec(`
			INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, cleanup_status, cleaned_at, created_at, updated_at)
			VALUES (?, 'issue-1', 'codex', 'codex', 'local', ?, 'running', ?, ?, '', '', 'running', '', 'retained', '', ?, ?)
		`, session.id, session.command, "mspace/issue/"+session.id, session.workdir, now, now); err != nil {
			t.Fatalf("insert session %s: %v", session.id, err)
		}
	}

	firstNode, err := application.recordSourceChangeNode(agentSession{
		ID:           "session-1",
		IssueID:      "issue-1",
		Provider:     "codex",
		AgentProfile: "codex",
		Command:      "Implement the issue.",
		Status:       "running",
		Branch:       "mspace/issue/session-1",
		Workdir:      firstWorkdir,
	}, project)
	if err != nil {
		t.Fatalf("record first source change node failed: %v", err)
	}
	if firstNode == nil || firstNode.CommitSHA != existingCommit {
		t.Fatalf("expected first session to record %s, got %+v", existingCommit, firstNode)
	}

	duplicateNode, err := application.recordSourceChangeNode(agentSession{
		ID:           "session-2",
		IssueID:      "issue-1",
		Provider:     "codex",
		AgentProfile: "codex",
		Command:      "Continue deployment.",
		Status:       "running",
		Branch:       "mspace/issue/session-2",
		Workdir:      secondWorkdir,
	}, project)
	if err != nil {
		t.Fatalf("record duplicate source change node should not fail: %v", err)
	}
	if duplicateNode != nil {
		t.Fatalf("expected duplicate commit to be skipped, got %+v", duplicateNode)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM issue_change_nodes WHERE issue_id = 'issue-1' AND commit_sha = ?`, existingCommit).Scan(&count); err != nil {
		t.Fatalf("query issue change node count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one issue change node row for duplicate commit, got %d", count)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM issue_change_nodes WHERE session_id = 'session-2'`).Scan(&count); err != nil {
		t.Fatalf("query duplicate session change node count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected duplicate session not to create a change node, got %d", count)
	}
}

func TestBuildSessionReviewEvidenceUsesArtifactAndEnvironment(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db, broker: newEventBroker(), workdir: t.TempDir(), repoRoot: t.TempDir()}
	if err := application.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES ('project-1', 'Demo', ?, 'local', '', '', '', '', 'main', '', '', 'ctx', 'ns-demo', ?, ?)
	`, t.TempDir(), now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issues (id, project_id, title, body, status, assignee, assignee_type, environment_url, created_at, updated_at)
			VALUES ('issue-1', 'project-1', 'Evidence issue', '', 'needs_review', 'codex', 'agent', '', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	artifactDir := filepath.Join(t.TempDir(), "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("create artifact dir: %v", err)
	}
	writeFile(t, filepath.Join(artifactDir, "review-evidence.json"), `{
		"agentSummary":"Implemented and deployed the change.",
		"commandsRun":[{"command":"pnpm build","status":"passed","category":"build","summary":"build ok"}],
		"tests":[{"name":"pnpm test","status":"passed","summary":"all tests passed"}],
		"buildResult":{"status":"passed","summary":"build passed"},
		"deploymentResult":{"status":"passed","summary":"deployment passed"},
		"risks":["manual cache refresh may still be needed"],
		"followUps":["add e2e coverage"]
	}`)
	session := agentSession{
		ID:              "session-1",
		IssueID:         "issue-1",
		RuntimeMode:     "codex-app-server",
		Status:          "completed",
		Branch:          "mspace/issue/session-1",
		ArtifactDir:     artifactDir,
		SourceSessionID: "source-session",
		SourceCommitSHA: "abcdef1234567890",
	}
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, source_session_id, source_commit_sha, cleanup_status, cleaned_at, created_at, updated_at)
		VALUES (?, ?, 'codex', 'codex', ?, '', ?, ?, '', '', '', 'completed', ?, ?, ?, 'retained', '', ?, ?)
	`, session.ID, session.IssueID, session.RuntimeMode, session.Status, session.Branch, session.ArtifactDir, session.SourceSessionID, session.SourceCommitSHA, now, now); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issue_test_environments (issue_id, cluster_id, namespace, namespace_status, cleanup_status, preview_url, kube_context, last_deploy_session_id, source_session_id, source_commit_sha, created_at, updated_at)
		VALUES ('issue-1', 'cluster-1', 'ns-demo', 'active', 'retained', 'http://preview.example', 'ctx', 'session-1', 'source-session', 'abcdef1234567890', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert test environment: %v", err)
	}

	review, err := application.buildSessionReviewEvidence(session, project{ID: "project-1", Name: "Demo"}, nil, true)
	if err != nil {
		t.Fatalf("build session review evidence failed: %v", err)
	}
	if review.AgentSummary != "Implemented and deployed the change." {
		t.Fatalf("expected artifact summary, got %q", review.AgentSummary)
	}
	if review.PreviewURL != "http://preview.example" || review.Namespace != "ns-demo" || review.Cluster != "ctx" {
		t.Fatalf("expected environment metadata, got %+v", review)
	}
	if len(review.CommandsRun) != 1 || review.BuildResult.Status != "passed" || len(review.Tests) != 1 {
		t.Fatalf("expected artifact evidence sections, got %+v", review)
	}
	if err := application.storeSessionReviewEvidence(review); err != nil {
		t.Fatalf("store session review evidence failed: %v", err)
	}
	loaded, err := application.listSessionReviewEvidence("issue-1")
	if err != nil {
		t.Fatalf("list session review evidence failed: %v", err)
	}
	if len(loaded) != 1 || loaded[0].SourceCommitSHA != "abcdef1234567890" || loaded[0].Risks[0] == "" {
		t.Fatalf("unexpected loaded review evidence: %+v", loaded)
	}
}

func TestBuildSessionReviewEvidenceCompactsDerivedCommands(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db, broker: newEventBroker(), workdir: t.TempDir(), repoRoot: t.TempDir()}
	if err := application.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES ('project-1', 'Demo', ?, 'local', '', '', '', '', 'main', '', '', '', '', ?, ?)
	`, t.TempDir(), now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issues (id, project_id, title, body, status, assignee, assignee_type, environment_url, created_at, updated_at)
			VALUES ('issue-1', 'project-1', 'Evidence issue', '', 'needs_review', 'codex', 'agent', '', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	session := agentSession{
		ID:          "session-1",
		IssueID:     "issue-1",
		RuntimeMode: "codex-app-server",
		Status:      "completed",
		Branch:      "mspace/issue/session-1",
		Workdir:     t.TempDir(),
	}
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, cleanup_status, cleaned_at, created_at, updated_at)
		VALUES (?, ?, 'codex', 'codex', ?, '', ?, ?, ?, '', '', 'completed', '', 'retained', '', ?, ?)
	`, session.ID, session.IssueID, session.RuntimeMode, session.Status, session.Branch, session.Workdir, now, now); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	logMessages := []string{
		`$ /bin/zsh -lc "sed -n '1,220p' package.json"`,
		`Command exit 0.`,
		`$ /bin/zsh -lc 'rg -n "Evidence" runner/main.go'`,
		`Command exit 0.`,
		`$ /bin/zsh -lc 'find . -maxdepth 2 -type f'`,
		`Command exit 0.`,
		`$ /bin/zsh -lc 'npm run lint'`,
		"Command failed with exit 127.\nsh: eslint: command not found",
		`$ /bin/zsh -lc 'npm ci'`,
		`Command exit 0.`,
		`$ /bin/zsh -lc 'npm run lint'`,
		`Command exit 0.`,
		`$ /bin/zsh -lc 'npm test'`,
		`Command exit 0.`,
		`$ /bin/zsh -lc 'NODE_ENV=production npm run build'`,
		`Command exit 0.`,
		`$ /bin/zsh -lc 'git diff --check'`,
		`Command exit 0.`,
		`$ /bin/zsh -lc 'git commit -m "fix: demo"'`,
		`Command exit 0.`,
	}
	for _, message := range logMessages {
		application.appendSessionLog(session.ID, "command", message)
	}

	review, err := application.buildSessionReviewEvidence(session, project{ID: "project-1", Name: "Demo"}, nil, true)
	if err != nil {
		t.Fatalf("build session review evidence failed: %v", err)
	}

	commands := make([]string, 0, len(review.CommandsRun))
	for _, command := range review.CommandsRun {
		commands = append(commands, command.Command)
		if strings.Contains(command.Command, "sed -n") || strings.Contains(command.Command, "rg -n") || strings.Contains(command.Command, "find .") {
			t.Fatalf("exploratory command should not be persisted in review evidence: %+v", review.CommandsRun)
		}
		if strings.Contains(command.Summary, "eslint: command not found") {
			t.Fatalf("superseded failed lint output should stay in session logs: %+v", review.CommandsRun)
		}
	}
	joined := strings.Join(commands, "\n")
	for _, want := range []string{"npm ci", "npm run lint", "npm test", "npm run build", "git diff --check", "git commit"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected compact review commands to include %q, got %v", want, commands)
		}
	}
	if review.BuildResult.Status != "passed" {
		t.Fatalf("expected latest build result to pass, got %+v", review.BuildResult)
	}
	if len(review.Tests) != 2 {
		t.Fatalf("expected lint and test checks, got %+v", review.Tests)
	}
	for _, check := range review.Tests {
		if check.Status != "passed" {
			t.Fatalf("expected superseded test failures to be hidden by latest pass, got %+v", review.Tests)
		}
	}
}

func TestBuildSessionFailureUsesFailedCommandAndEvidence(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db, broker: newEventBroker(), workdir: t.TempDir(), repoRoot: t.TempDir()}
	if err := application.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES ('project-1', 'Demo', ?, 'local', '', '', '', '', 'main', '', '', 'ctx-a', 'issue-ns', ?, ?)
	`, t.TempDir(), now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issues (id, project_id, title, body, status, assignee, assignee_type, environment_url, created_at, updated_at)
			VALUES ('issue-1', 'project-1', 'Failure issue', '', 'blocked', 'codex', 'agent', '', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	session := agentSession{
		ID:          "session-1",
		IssueID:     "issue-1",
		RuntimeMode: "codex-app-server",
		Status:      "failed",
		Branch:      "mspace/issue/session-1",
		Workdir:     t.TempDir(),
	}
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, cleanup_status, cleaned_at, created_at, updated_at)
		VALUES (?, ?, 'codex', 'codex', ?, '', ?, ?, ?, '', '', 'failed', '', 'retained', '', ?, ?)
	`, session.ID, session.IssueID, session.RuntimeMode, session.Status, session.Branch, session.Workdir, now, now); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	application.appendSessionLog(session.ID, "command", `$ /bin/zsh -lc 'docker push registry.example/mspace:demo'`)
	application.appendSessionLog(session.ID, "command", "Command failed with exit 1.\ndenied: requested access to the resource is denied")
	evidence := deploymentEvidence{
		ID:        "evidence-1",
		IssueID:   session.IssueID,
		SessionID: session.ID,
		Cluster:   "ctx-a",
		Namespace: "issue-ns",
		Summary:   "Deployment failed.",
		Details:   "LAST SEEN TYPE REASON OBJECT MESSAGE\n1m Warning Failed pod/demo denied: requested access",
		CreatedAt: now,
	}

	failure := application.buildSessionFailure(session, project{ID: "project-1", KubeContext: "ctx-a", Namespace: "issue-ns"}, errors.New("command failed"), &evidence, "", "")
	if failure.Phase != "image_push" {
		t.Fatalf("expected image_push phase, got %+v", failure)
	}
	if !strings.Contains(failure.FailedCommand, "docker push") || !strings.Contains(failure.ErrorSummary, "denied") {
		t.Fatalf("expected failed command and summary, got %+v", failure)
	}
	if failure.ResourceKind != "pod" || failure.ResourceName != "demo" || failure.EvidenceID != "evidence-1" {
		t.Fatalf("expected Kubernetes evidence linkage, got %+v", failure)
	}

	stored, err := application.storeSessionFailure(failure)
	if err != nil {
		t.Fatalf("store session failure: %v", err)
	}
	loaded, err := application.listSessionFailures("issue-1")
	if err != nil {
		t.Fatalf("list session failures: %v", err)
	}
	if len(loaded) != 1 || loaded[0].ID != stored.ID || loaded[0].Phase != "image_push" {
		t.Fatalf("unexpected loaded failures: %+v", loaded)
	}
}

func TestBackfillSessionFailuresCreatesRowsForExistingFailedSessions(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db, broker: newEventBroker(), workdir: t.TempDir(), repoRoot: t.TempDir()}
	if err := application.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES ('project-1', 'Demo', ?, 'local', '', '', '', '', 'main', '', '', 'ctx-a', 'issue-ns', ?, ?)
	`, t.TempDir(), now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issues (id, project_id, title, body, status, assignee, assignee_type, environment_url, created_at, updated_at)
			VALUES ('issue-1', 'project-1', 'Failure issue', '', 'blocked', 'codex', 'agent', '', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, cleanup_status, cleaned_at, created_at, updated_at)
		VALUES ('session-1', 'issue-1', 'codex', 'codex', 'codex-app-server', '', 'failed', 'mspace/issue/session-1', ?, '', '', 'failed', '', 'retained', '', ?, ?)
	`, t.TempDir(), now, now); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	application.appendSessionLog("session-1", "command", `$ /bin/zsh -lc 'pnpm test'`)
	application.appendSessionLog("session-1", "command", "Command failed with exit 1.\nTest failed.")

	if err := application.backfillSessionFailures(); err != nil {
		t.Fatalf("backfill session failures: %v", err)
	}
	if err := application.backfillSessionFailures(); err != nil {
		t.Fatalf("backfill session failures should be idempotent: %v", err)
	}
	loaded, err := application.listSessionFailures("issue-1")
	if err != nil {
		t.Fatalf("list session failures: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Phase != "test" || !strings.Contains(loaded[0].FailedCommand, "pnpm test") {
		t.Fatalf("unexpected backfilled failure: %+v", loaded)
	}
}

func TestReadReviewEvidenceArtifactAcceptsCompactShapes(t *testing.T) {
	artifactDir := filepath.Join(t.TempDir(), "artifacts")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("create artifact dir: %v", err)
	}
	writeFile(t, filepath.Join(artifactDir, "review-evidence.json"), `{
		"agentSummary":"Artifact summary.",
		"commandsRun":["pnpm test","NODE_ENV=production pnpm build"],
		"tests":{"pnpm test":"passed: all tests"},
		"buildResult":"success: build passed",
		"deploymentResult":{"status":"ok","summary":"preview updated"},
		"risks":"manual verification still recommended",
		"followUps":{"e2e":"add browser coverage"}
	}`)

	artifact := readReviewEvidenceArtifact(agentSession{ArtifactDir: artifactDir})
	if artifact.AgentSummary != "Artifact summary." {
		t.Fatalf("expected artifact summary, got %q", artifact.AgentSummary)
	}
	if len(artifact.CommandsRun) != 2 || artifact.CommandsRun[0].Command != "pnpm test" {
		t.Fatalf("expected string command list to parse, got %+v", artifact.CommandsRun)
	}
	if len(artifact.Tests) != 1 || artifact.Tests[0].Status != "passed" {
		t.Fatalf("expected test map to parse, got %+v", artifact.Tests)
	}
	if artifact.BuildResult.Status != "passed" || artifact.DeploymentResult.Status != "passed" {
		t.Fatalf("expected compact result shapes to normalize, got build=%+v deploy=%+v", artifact.BuildResult, artifact.DeploymentResult)
	}
	if len(artifact.Risks) != 1 || len(artifact.FollowUps) != 1 {
		t.Fatalf("expected risk/follow-up compact shapes to parse, got risks=%+v followUps=%+v", artifact.Risks, artifact.FollowUps)
	}
}

func TestPrepareSessionWorkspaceChecksOutSourceCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for source checkout test")
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init", "-b", "main")
	runGit(t, repoDir, "config", "user.name", "mspace test")
	runGit(t, repoDir, "config", "user.email", "mspace@example.com")
	writeFile(t, filepath.Join(repoDir, "README.md"), "# demo\n")
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "base")
	runGit(t, repoDir, "checkout", "-b", "mspace/issue/source")
	writeFile(t, filepath.Join(repoDir, "app.txt"), "source\n")
	runGit(t, repoDir, "add", "app.txt")
	runGit(t, repoDir, "commit", "-m", "source commit")
	sourceCommit := strings.TrimSpace(gitOutput(t, repoDir, "rev-parse", "HEAD"))
	runGit(t, repoDir, "checkout", "main")

	application := &app{workdir: filepath.Join(t.TempDir(), "workdirs")}
	session, err := application.prepareSessionWorkspace(agentSession{
		ID:              "deploy-session",
		IssueID:         "issue-1",
		Branch:          "mspace/issue/deploy-session",
		SourceSessionID: "source-session",
		SourceCommitSHA: sourceCommit,
	}, project{
		ID:            "project-1",
		RepoPath:      repoDir,
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("prepare session workspace failed: %v", err)
	}
	head := strings.TrimSpace(gitOutput(t, session.Workdir, "rev-parse", "HEAD"))
	if head != sourceCommit {
		t.Fatalf("expected worktree head %s, got %s", sourceCommit, head)
	}
}

func TestQueueAgentSessionRoutesTeamRuntimeThroughControlPlane(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for team runtime session test")
	}

	application, db := newAuthTestApp(t)
	humanToken := configureTestHumanAuth(t, application)
	repoDir := t.TempDir()
	runGit(t, repoDir, "init", "-b", "main")
	runGit(t, repoDir, "config", "user.name", "mspace test")
	runGit(t, repoDir, "config", "user.email", "mspace@example.com")
	writeFile(t, filepath.Join(repoDir, "README.md"), "# demo\n")
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "base")
	insertAuthTestIssue(t, db, "issue-1", "", "Team runtime issue", "open")
	if _, err := db.Exec(`UPDATE projects SET repo_path = ?, default_branch = 'main' WHERE id = 'project-1'`, repoDir); err != nil {
		t.Fatalf("update project repo: %v", err)
	}

	controlPlaneToken := "human-token"
	workspaceID := "workspace-1"
	taskCreated := false
	taskPollCount := 0
	var taskPayload struct {
		IssueID              string          `json:"issueId"`
		SessionID            string          `json:"sessionId"`
		ProjectID            string          `json:"projectId"`
		Kind                 string          `json:"kind"`
		RuntimeMode          string          `json:"runtimeMode"`
		RequiredCapabilities map[string]bool `json:"requiredCapabilities"`
		Payload              struct {
			Workdir         string            `json:"workdir"`
			Prompt          string            `json:"prompt"`
			Env             map[string]string `json:"env"`
			Branch          string            `json:"branch"`
			ContextMarkdown string            `json:"contextMarkdown"`
			Repository      struct {
				URL           string `json:"url"`
				DefaultBranch string `json:"defaultBranch"`
			} `json:"repository"`
		} `json:"payload"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+controlPlaneToken {
			writeError(w, http.StatusUnauthorized, errUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/auth/me":
			writeJSON(w, map[string]any{
				"user":       map[string]string{"id": "user-1", "name": "Test Human"},
				"workspaces": []map[string]string{{"id": workspaceID, "kind": "team"}},
			})
		case r.URL.Path == "/api/workspaces/"+workspaceID+"/runtime-tasks" && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&taskPayload); err != nil {
				t.Fatalf("decode runtime task: %v", err)
			}
			taskCreated = true
			writeJSONStatus(t, w, http.StatusCreated, runtimeTask{ID: "runtime-task-1", Status: "queued"})
		case r.URL.Path == "/api/workspaces/"+workspaceID+"/runtime-tasks" && r.Method == http.MethodGet:
			taskPollCount++
			result := json.RawMessage(`{
					"threadId":"thread-team",
					"turnId":"turn-team",
					"status":"completed",
					"workdir":"/worker/workdirs/project-1/session-1",
					"artifactDir":"/worker/workdirs/project-1/session-1/.mspace/session",
					"source":{
						"commitSha":"1111111111111111111111111111111111111111",
						"shortCommitSha":"111111111111",
						"branch":"mspace/issue/session",
						"subject":"mspace: Team runtime issue",
						"filesChanged":1,
						"changes":[{"statusCode":"A","path":"worker-output.txt"}],
						"diffPreview":"commit 1111111111111111111111111111111111111111\nAuthor: mspace\n\n    mspace: Team runtime issue\n\n diff --git a/worker-output.txt b/worker-output.txt\n new file mode 100644\n",
						"diffTruncated":false
					}
				}`)
			writeJSON(w, []runtimeTask{{ID: "runtime-task-1", Status: "completed", Result: result}})
		case r.URL.Path == "/api/workspaces/"+workspaceID+"/runtime-tasks/runtime-task-1/logs":
			writeJSON(w, []runtimeTaskLog{
				{ID: "log-1", Stream: "agent", Message: "Team runtime completed.", CreatedAt: nowString()},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	application.controlPlaneBaseURL = server.URL
	application.controlPlaneToken = controlPlaneToken
	application.controlPlaneWorkspaceID = workspaceID

	router := chi.NewRouter()
	router.Post("/api/issues/{issueID}/assign-agent", application.handleAssignIssueToAgent)
	create := httptest.NewRecorder()
	router.ServeHTTP(create, authRequest(http.MethodPost, "/api/issues/issue-1/assign-agent", `{"provider":"codex","agentProfile":"codex","runtimeMode":"team","command":"make a small change"}`, humanToken))
	if create.Code != http.StatusOK {
		t.Fatalf("expected team session create to return 200, got %d body=%s", create.Code, create.Body.String())
	}
	var response struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(create.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	waitForCondition(t, 5*time.Second, func() bool {
		var status, agentStatus string
		_ = db.QueryRow(`SELECT status, agent_status FROM agent_sessions WHERE id = ?`, response.SessionID).Scan(&status, &agentStatus)
		return status == "completed" && agentStatus == "completed"
	}, func() string {
		var status, agentStatus string
		_ = db.QueryRow(`SELECT status, agent_status FROM agent_sessions WHERE id = ?`, response.SessionID).Scan(&status, &agentStatus)
		rows, _ := db.Query(`SELECT stream, message FROM session_logs WHERE session_id = ? ORDER BY id`, response.SessionID)
		defer func() {
			if rows != nil {
				_ = rows.Close()
			}
		}()
		var logs []string
		if rows != nil {
			for rows.Next() {
				var stream, message string
				_ = rows.Scan(&stream, &message)
				logs = append(logs, stream+": "+message)
			}
		}
		return "status=" + status + " agentStatus=" + agentStatus + " logs=" + strings.Join(logs, " | ")
	})
	if !taskCreated || taskPollCount == 0 {
		t.Fatalf("expected control-plane task creation and polling, created=%v polls=%d", taskCreated, taskPollCount)
	}
	if taskPayload.Kind != "agent_session" || taskPayload.RuntimeMode != "team" || !taskPayload.RequiredCapabilities["codex"] {
		t.Fatalf("unexpected runtime task payload: %+v", taskPayload)
	}
	if taskPayload.IssueID != "issue-1" || taskPayload.SessionID != response.SessionID || taskPayload.ProjectID != "project-1" {
		t.Fatalf("runtime task lost issue/session/project identity: %+v", taskPayload)
	}
	if !strings.Contains(taskPayload.Payload.Prompt, "make a small change") || taskPayload.Payload.Workdir != "" || taskPayload.Payload.Env["MSPACE_SESSION_ID"] != response.SessionID {
		t.Fatalf("unexpected agent payload: %+v", taskPayload.Payload)
	}
	if taskPayload.Payload.Repository.URL == "" || taskPayload.Payload.Repository.DefaultBranch != "main" || taskPayload.Payload.Branch == "" || !strings.Contains(taskPayload.Payload.ContextMarkdown, "Team runtime issue") {
		t.Fatalf("team runtime payload did not include repo/session bootstrap spec: %+v", taskPayload.Payload)
	}
	var runtimeMode, threadID, turnID string
	if err := db.QueryRow(`SELECT runtime_mode, codex_thread_id, codex_turn_id FROM agent_sessions WHERE id = ?`, response.SessionID).Scan(&runtimeMode, &threadID, &turnID); err != nil {
		t.Fatalf("query session: %v", err)
	}
	if runtimeMode != "team" || threadID != "thread-team" || turnID != "turn-team" {
		t.Fatalf("unexpected session runtime metadata mode=%q thread=%q turn=%q", runtimeMode, threadID, turnID)
	}
	assertSessionLogContains(t, db, response.SessionID, "Team runtime completed.")
	var commitSHA, changesJSON, diffPreview, source, remoteWorkdir string
	if err := db.QueryRow(`SELECT commit_sha, changes_json, diff_preview, source, remote_workdir FROM issue_change_nodes WHERE session_id = ?`, response.SessionID).Scan(&commitSHA, &changesJSON, &diffPreview, &source, &remoteWorkdir); err != nil {
		t.Fatalf("query remote change node: %v", err)
	}
	if commitSHA != "1111111111111111111111111111111111111111" || source != "team-runtime" || remoteWorkdir == "" || !strings.Contains(changesJSON, "worker-output.txt") || !strings.Contains(diffPreview, "worker-output.txt") {
		t.Fatalf("unexpected remote change node commit=%q source=%q remoteWorkdir=%q changes=%s diff=%s", commitSHA, source, remoteWorkdir, changesJSON, diffPreview)
	}
}

func TestCreateServerIssueTeamSessionQueuesRuntimeTask(t *testing.T) {
	application, db := newAuthTestApp(t)
	controlPlaneToken := configureTestHumanAuth(t, application)
	workspaceID := "workspace-1"
	repoURL := "https://github.com/mlhiter/mspace.git"
	taskCreated := false
	var taskPayload struct {
		IssueID              string          `json:"issueId"`
		SessionID            string          `json:"sessionId"`
		ProjectID            string          `json:"projectId"`
		Kind                 string          `json:"kind"`
		RuntimeMode          string          `json:"runtimeMode"`
		RequiredCapabilities map[string]bool `json:"requiredCapabilities"`
		Payload              struct {
			Prompt          string `json:"prompt"`
			AgentProfile    string `json:"agentProfile"`
			Branch          string `json:"branch"`
			ContextMarkdown string `json:"contextMarkdown"`
			Repository      struct {
				URL           string `json:"url"`
				DefaultBranch string `json:"defaultBranch"`
			} `json:"repository"`
		} `json:"payload"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+controlPlaneToken {
			writeError(w, http.StatusUnauthorized, errUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/auth/me":
			writeJSON(w, map[string]any{
				"user":       map[string]string{"id": "user-1", "name": "Test Human"},
				"workspaces": []map[string]string{{"id": workspaceID, "kind": "team"}},
			})
		case r.URL.Path == "/api/workspaces/"+workspaceID+"/runtime-tasks" && r.Method == http.MethodPost:
			if err := json.NewDecoder(r.Body).Decode(&taskPayload); err != nil {
				t.Fatalf("decode runtime task: %v", err)
			}
			taskCreated = true
			writeJSONStatus(t, w, http.StatusCreated, runtimeTask{ID: "runtime-task-server-issue", Status: "queued"})
		case r.URL.Path == "/api/workspaces/"+workspaceID+"/runtime-tasks" && r.Method == http.MethodGet:
			writeJSON(w, []runtimeTask{{ID: "runtime-task-server-issue", Status: "running"}})
		case r.URL.Path == "/api/workspaces/"+workspaceID+"/runtime-tasks/runtime-task-server-issue/logs":
			writeJSON(w, []runtimeTaskLog{{ID: "log-1", Stream: "agent", Message: "Server issue task started.", CreatedAt: nowString()}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	application.controlPlaneBaseURL = server.URL
	application.controlPlaneToken = controlPlaneToken
	application.controlPlaneWorkspaceID = workspaceID

	router := chi.NewRouter()
	router.Post("/api/server-issues/{issueID}/team-session", application.handleCreateServerIssueTeamSession)
	body := `{
		"workspaceId":"workspace-1",
		"issueId":"server-issue-1",
		"commentId":"server-comment-1",
		"provider":"codex",
		"agentProfile":"codex",
		"runtimeMode":"team",
		"command":"@codex wire the bridge",
		"issue":{
			"id":"server-issue-1",
			"projectId":"server-project-1",
			"title":"PG issue bridge",
			"body":"Connect the shared workspace issue to Team worker.",
			"status":"open",
			"triageStatus":"pending",
			"creatorName":"Test Human",
			"creatorAvatarUrl":"https://example.com/avatar.png"
		},
		"project":{
			"id":"server-project-1",
			"name":"mspace",
			"repoPath":"",
			"sourceType":"github",
			"remoteUrl":"https://github.com/mlhiter/mspace.git",
			"gitProvider":"github",
			"gitOwner":"mlhiter",
			"gitRepo":"mspace",
			"defaultBranch":"main"
		},
		"comments":[{"id":"server-comment-1","issueId":"server-issue-1","authorType":"human","authorUserId":"user-1","authorName":"Test Human","body":"@codex wire the bridge","createdAt":"2026-05-14T00:00:00Z","updatedAt":"2026-05-14T00:00:00Z"}]
	}`
	create := httptest.NewRecorder()
	router.ServeHTTP(create, authRequest(http.MethodPost, "/api/server-issues/server-issue-1/team-session", body, controlPlaneToken))
	if create.Code != http.StatusOK {
		t.Fatalf("expected server issue team session create to return 200, got %d body=%s", create.Code, create.Body.String())
	}
	var response struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(create.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	waitForCondition(t, 5*time.Second, func() bool {
		var runtimeTaskID string
		_ = db.QueryRow(`SELECT runtime_task_id FROM agent_sessions WHERE id = ?`, response.SessionID).Scan(&runtimeTaskID)
		return runtimeTaskID == "runtime-task-server-issue"
	}, func() string {
		var status, agentStatus, runtimeTaskID string
		_ = db.QueryRow(`SELECT status, agent_status, runtime_task_id FROM agent_sessions WHERE id = ?`, response.SessionID).Scan(&status, &agentStatus, &runtimeTaskID)
		return "status=" + status + " agentStatus=" + agentStatus + " runtimeTaskID=" + runtimeTaskID + " logs=" + strings.Join(sessionLogMessages(db, response.SessionID), " | ")
	})
	if !taskCreated {
		t.Fatalf("expected runtime task to be created")
	}
	if taskPayload.IssueID != "server-issue-1" || taskPayload.SessionID != response.SessionID || taskPayload.ProjectID != "server-project-1" {
		t.Fatalf("runtime task lost server issue identity: %+v", taskPayload)
	}
	if taskPayload.Kind != "agent_session" || taskPayload.RuntimeMode != "team" || !taskPayload.RequiredCapabilities["codex"] {
		t.Fatalf("unexpected runtime task routing payload: %+v", taskPayload)
	}
	if taskPayload.Payload.Repository.URL != repoURL || taskPayload.Payload.Repository.DefaultBranch != "main" || taskPayload.Payload.AgentProfile != "codex" {
		t.Fatalf("unexpected team runtime payload: %+v", taskPayload.Payload)
	}
	if !strings.Contains(taskPayload.Payload.ContextMarkdown, "PG issue bridge") || !strings.Contains(taskPayload.Payload.ContextMarkdown, "@codex wire the bridge") {
		t.Fatalf("context did not include server issue snapshot: %s", taskPayload.Payload.ContextMarkdown)
	}
	var issueTitle, commentBody, triggerCommentID string
	if err := db.QueryRow(`SELECT title FROM issues WHERE id = 'server-issue-1'`).Scan(&issueTitle); err != nil {
		t.Fatalf("query bridged issue: %v", err)
	}
	if err := db.QueryRow(`SELECT body FROM comments WHERE id = 'server-comment-1'`).Scan(&commentBody); err != nil {
		t.Fatalf("query bridged comment: %v", err)
	}
	if err := db.QueryRow(`SELECT trigger_comment_id FROM agent_sessions WHERE id = ?`, response.SessionID).Scan(&triggerCommentID); err != nil {
		t.Fatalf("query trigger comment: %v", err)
	}
	if issueTitle != "PG issue bridge" || commentBody != "@codex wire the bridge" || triggerCommentID != "server-comment-1" {
		t.Fatalf("unexpected bridged snapshot issue=%q comment=%q trigger=%q", issueTitle, commentBody, triggerCommentID)
	}
}

func TestQueueAgentSessionRejectsTeamRuntimeFromPersonalWorkspace(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for team runtime session test")
	}

	application, db := newAuthTestApp(t)
	humanToken := configureTestHumanAuth(t, application)
	repoDir := t.TempDir()
	runGit(t, repoDir, "init", "-b", "main")
	runGit(t, repoDir, "config", "user.name", "mspace test")
	runGit(t, repoDir, "config", "user.email", "mspace@example.com")
	writeFile(t, filepath.Join(repoDir, "README.md"), "# demo\n")
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "base")
	insertAuthTestIssue(t, db, "issue-1", "", "Personal runtime issue", "open")
	if _, err := db.Exec(`UPDATE projects SET repo_path = ?, default_branch = 'main' WHERE id = 'project-1'`, repoDir); err != nil {
		t.Fatalf("update project repo: %v", err)
	}

	controlPlaneToken := "human-token"
	workspaceID := "workspace-1"
	taskCreated := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+controlPlaneToken {
			writeError(w, http.StatusUnauthorized, errUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/auth/me":
			writeJSON(w, map[string]any{
				"user":       map[string]string{"id": "user-1", "name": "Test Human"},
				"workspaces": []map[string]string{{"id": workspaceID, "kind": "personal"}},
			})
		case r.URL.Path == "/api/workspaces/"+workspaceID+"/runtime-tasks":
			taskCreated = true
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	application.controlPlaneBaseURL = server.URL
	application.controlPlaneToken = controlPlaneToken
	application.controlPlaneWorkspaceID = workspaceID

	router := chi.NewRouter()
	router.Post("/api/issues/{issueID}/assign-agent", application.handleAssignIssueToAgent)
	create := httptest.NewRecorder()
	router.ServeHTTP(create, authRequest(http.MethodPost, "/api/issues/issue-1/assign-agent", `{"provider":"codex","agentProfile":"codex","runtimeMode":"team","command":"make a small change"}`, humanToken))
	if create.Code != http.StatusOK {
		t.Fatalf("expected session create to return 200 before async runtime validation, got %d body=%s", create.Code, create.Body.String())
	}
	var response struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(create.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	waitForCondition(t, 5*time.Second, func() bool {
		var status, agentStatus string
		_ = db.QueryRow(`SELECT status, agent_status FROM agent_sessions WHERE id = ?`, response.SessionID).Scan(&status, &agentStatus)
		return status == "failed" && agentStatus == "failed" && sessionLogContains(db, response.SessionID, "team runtime requires a team workspace")
	}, func() string {
		var status, agentStatus string
		_ = db.QueryRow(`SELECT status, agent_status FROM agent_sessions WHERE id = ?`, response.SessionID).Scan(&status, &agentStatus)
		return "status=" + status + " agentStatus=" + agentStatus + " logs=" + strings.Join(sessionLogMessages(db, response.SessionID), " | ")
	})
	if taskCreated {
		t.Fatalf("personal workspace should not create a team runtime task")
	}
	assertSessionLogContains(t, db, response.SessionID, "team runtime requires a team workspace")
}

func TestCancelTeamRuntimeSessionRequestsControlPlaneCancellation(t *testing.T) {
	application, db := newAuthTestApp(t)
	_ = configureTestHumanAuth(t, application)
	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES ('project-1', 'Demo', '/tmp/demo', 'local', '', '', '', '', 'main', '', '', '', '', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issues (id, project_id, title, body, status, assignee, assignee_type, environment_url, created_at, updated_at)
			VALUES ('issue-1', 'project-1', 'Team cancel issue', '', 'open', 'codex', 'agent', '', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, runtime_task_id, command, status, branch, workdir, agent_status, cleanup_status, cleaned_at, created_at, updated_at)
		VALUES ('session-team', 'issue-1', 'codex', 'codex', 'team', 'runtime-task-1', 'run it', 'running', 'mspace/issue/session-team', ?, 'team-runtime-running', 'retained', '', ?, ?)
	`, t.TempDir(), now, now); err != nil {
		t.Fatalf("insert team session: %v", err)
	}

	controlPlaneToken := "human-token"
	workspaceID := "workspace-1"
	var cancelPayload cancelRuntimeTaskInput
	cancelRequested := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+controlPlaneToken {
			writeError(w, http.StatusUnauthorized, errUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/auth/me":
			writeJSON(w, map[string]any{
				"user":       map[string]string{"id": "user-1", "name": "Test Human"},
				"workspaces": []map[string]string{{"id": workspaceID, "kind": "team"}},
			})
		case r.URL.Path == "/api/workspaces/"+workspaceID+"/runtime-tasks/runtime-task-1/cancel" && r.Method == http.MethodPost:
			cancelRequested = true
			if err := json.NewDecoder(r.Body).Decode(&cancelPayload); err != nil {
				t.Fatalf("decode cancel task payload: %v", err)
			}
			writeJSONStatus(t, w, http.StatusOK, runtimeTask{ID: "runtime-task-1", Status: "cancelled", Error: cancelPayload.Reason})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	application.controlPlaneBaseURL = server.URL
	application.controlPlaneToken = controlPlaneToken
	application.controlPlaneWorkspaceID = workspaceID

	router := chi.NewRouter()
	router.Post("/api/sessions/{sessionID}/cancel", application.handleCancelSession)
	cancel := httptest.NewRecorder()
	router.ServeHTTP(cancel, authRequest(http.MethodPost, "/api/sessions/session-team/cancel", `{}`, controlPlaneToken))
	if cancel.Code != http.StatusOK {
		t.Fatalf("expected cancel to return 200, got %d body=%s", cancel.Code, cancel.Body.String())
	}
	if !cancelRequested || !strings.Contains(cancelPayload.Reason, "Stopped session") {
		t.Fatalf("expected control-plane cancellation request, requested=%v payload=%+v", cancelRequested, cancelPayload)
	}
	var status, agentStatus string
	if err := db.QueryRow(`SELECT status, agent_status FROM agent_sessions WHERE id = 'session-team'`).Scan(&status, &agentStatus); err != nil {
		t.Fatalf("query cancelled session: %v", err)
	}
	if status != "cancelled" || agentStatus != "cancelled" {
		t.Fatalf("expected local team session cancelled, got status=%q agentStatus=%q", status, agentStatus)
	}
	assertSessionLogContains(t, db, "session-team", "Requested cancellation for team runtime task runtime-task-1.")
}

func TestNormalizeIssueLabelKeys(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db}
	if err := application.ensureIssueLabelTables(); err != nil {
		t.Fatalf("ensure issue label tables failed: %v", err)
	}

	labels, err := application.normalizeIssueLabelKeys([]string{" fix ", "#priority:p1", "Fix", "", "P1"})
	if err != nil {
		t.Fatalf("normalize issue labels failed: %v", err)
	}
	if len(labels) != 2 {
		t.Fatalf("expected two labels, got %#v", labels)
	}
	if labels[0].Key != "type:fix" || labels[1].Key != "priority:p1" {
		t.Fatalf("unexpected labels %#v", labels)
	}
}

func TestParseIssueTriageResultValidatesType(t *testing.T) {
	result, err := parseIssueTriageResult(`{"type":"fix","confidence":0.86,"reason":"failure path"}`)
	if err != nil {
		t.Fatalf("parse triage result failed: %v", err)
	}
	if result.Type != "fix" || result.Confidence != 0.86 {
		t.Fatalf("unexpected triage result %#v", result)
	}
	if _, err := parseIssueTriageResult(`{"type":"p0","confidence":1}`); err == nil {
		t.Fatal("expected priority-like triage type to be rejected")
	}
}

func TestCleanupSessionWorktreeRemovesWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for session cleanup test")
	}

	repoDir := t.TempDir()
	runGit(t, repoDir, "init", "-b", "main")
	runGit(t, repoDir, "config", "user.name", "mspace test")
	runGit(t, repoDir, "config", "user.email", "mspace@example.com")
	writeFile(t, filepath.Join(repoDir, "README.md"), "# demo\n")
	runGit(t, repoDir, "add", "README.md")
	runGit(t, repoDir, "commit", "-m", "base")

	workdirRoot := filepath.Join(t.TempDir(), "workdirs")
	sessionWorkdir := filepath.Join(workdirRoot, "project-1", "session-1")
	if err := os.MkdirAll(filepath.Dir(sessionWorkdir), 0o755); err != nil {
		t.Fatalf("create worktree parent: %v", err)
	}
	runGit(t, repoDir, "worktree", "add", "--detach", sessionWorkdir, "HEAD")

	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db, broker: newEventBroker(), workdir: workdirRoot, repoRoot: t.TempDir()}
	if err := application.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := nowString()
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES ('project-1', 'Demo', ?, 'local', '', '', '', '', 'main', '', '', '', '', ?, ?)
	`, repoDir, now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issues (id, project_id, title, body, status, assignee, assignee_type, environment_url, created_at, updated_at)
		VALUES ('issue-1', 'project-1', 'Cleanup issue', 'Clean this session.', 'closed', 'codex', 'agent', '', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO inbox_items (id, issue_id, project_id, title, status, unread, created_at, updated_at)
		VALUES ('inbox-1', 'issue-1', 'project-1', 'Cleanup issue', 'closed', 0, ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert inbox item: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, cleanup_status, cleaned_at, created_at, updated_at)
		VALUES ('session-1', 'issue-1', 'codex', 'codex', 'local', 'done', 'completed', 'mspace/issue/session', ?, '', '', 'completed', '', 'retained', '', ?, ?)
	`, sessionWorkdir, now, now); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	if err := application.cleanupSessionWorktree("session-1"); err != nil {
		t.Fatalf("cleanup session worktree failed: %v", err)
	}
	if _, err := os.Stat(sessionWorkdir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected worktree to be removed, stat err=%v", err)
	}

	var cleanupStatus string
	var cleanedAt string
	if err := db.QueryRow(`SELECT cleanup_status, cleaned_at FROM agent_sessions WHERE id = 'session-1'`).Scan(&cleanupStatus, &cleanedAt); err != nil {
		t.Fatalf("query cleanup status: %v", err)
	}
	if cleanupStatus != "cleaned" || cleanedAt == "" {
		t.Fatalf("expected cleaned status with timestamp, got status=%q cleaned_at=%q", cleanupStatus, cleanedAt)
	}

	detail, err := application.loadSessionDetail("session-1")
	if err != nil {
		t.Fatalf("load cleaned session detail: %v", err)
	}
	if detail.Workspace.Error != "Session worktree has been cleaned up." {
		t.Fatalf("expected cleaned workspace message, got %q", detail.Workspace.Error)
	}
}

func TestValidateSessionWorkdirRejectsOutsideRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workdirs")
	if _, err := validateSessionWorkdir(root, filepath.Join(root, "project", "session")); err != nil {
		t.Fatalf("expected workdir inside root to pass: %v", err)
	}
	if _, err := validateSessionWorkdir(root, filepath.Join(filepath.Dir(root), "other", "session")); !errors.Is(err, errUnsafeSessionWorkdir) {
		t.Fatalf("expected unsafe workdir error, got %v", err)
	}
}

func TestBuildCodexSessionPromptIncludesRuntimeContext(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	application := &app{db: db, broker: newEventBroker(), workdir: t.TempDir(), repoRoot: t.TempDir()}
	if err := application.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := nowString()
	project := project{
		ID:                "project-1",
		Name:              "Demo",
		RepoPath:          "/tmp/demo",
		SourceType:        "local",
		DefaultBranch:     "main",
		DeployCommand:     "pnpm deploy",
		ValidationCommand: "pnpm test",
		KubeContext:       "test-cluster",
		Namespace:         "demo-ns",
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if _, err := db.Exec(`
		INSERT INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', '', '', '', ?, ?, ?, ?, ?, ?, ?)
	`, project.ID, project.Name, project.RepoPath, project.SourceType, project.DefaultBranch, project.DeployCommand, project.ValidationCommand, project.KubeContext, project.Namespace, project.CreatedAt, project.UpdatedAt); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issues (id, project_id, title, body, status, assignee, assignee_type, environment_url, created_at, updated_at)
		VALUES ('issue-1', ?, 'Fix login', 'The login flow hangs.', 'open', 'codex', 'agent', '', ?, ?)
	`, project.ID, now, now); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO issues (id, project_id, parent_issue_id, sort_order, title, body, status, triage_status, assignee, assignee_type, environment_url, created_at, updated_at)
		VALUES ('task-1', ?, 'issue-1', 1, 'Add a regression test', '', 'open', 'none', 'me', 'human', '', ?, ?)
	`, project.ID, now, now); err != nil {
		t.Fatalf("insert child issue task: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO comments (id, issue_id, author_type, body, created_at)
		VALUES ('comment-1', 'issue-1', 'human', 'Please add a regression test.', ?)
	`, now); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, created_at, updated_at)
		VALUES ('prior-session', 'issue-1', 'codex', 'bugfix', 'local', 'Fix the original issue.', 'completed', 'mspace/issue/prior', '/tmp/prior', 'thread-1', 'turn-1', 'completed', '/tmp/prior/.mspace/session', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert prior session: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO session_logs (session_id, stream, message, created_at)
		VALUES ('prior-session', 'agent', 'Created login fix and verified the smoke test.', ?)
	`, now); err != nil {
		t.Fatalf("insert prior session log: %v", err)
	}
	if _, err := application.saveProjectRunbook(project.ID, strings.Join([]string{
		"# Runbook",
		"",
		"## Dependencies",
		"pnpm install",
		"",
		"## Tests",
		"pnpm test",
	}, "\n"), "learned", "human", "mlhiter", "", true); err != nil {
		t.Fatalf("save project runbook: %v", err)
	}

	prompt, err := application.buildCodexSessionPrompt(agentSession{
		ID:           "session-1",
		IssueID:      "issue-1",
		Provider:     "codex",
		AgentProfile: "design",
		Command:      "你好，codex。你之前做过这个任务了吗？",
		Branch:       "mspace/issue/session",
		Workdir:      "/tmp/worktree",
	}, project, "/tmp/context.md", "/tmp/artifacts")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}

	for _, expected := range []string{
		"Fix login",
		"The login flow hangs.",
		"Task List",
		"Add a regression test (`task-1`, status: open)",
		"MSPACE_API_BASE_URL",
		"Please add a regression test.",
		"Agent Profile",
		"Profile: Design (@design)",
		"Use the design profile",
		"Current Turn Request",
		"highest-priority request",
		"newly added `@design` issue comment",
		"你好，codex。你之前做过这个任务了吗？",
		"Prior Sessions",
		"Bugfix session prior-se",
		"Agent profile: bugfix",
		"Fix the original issue.",
		"Created login fix and verified the smoke test.",
		"Do not introduce yourself",
		"Do not say you saw the current comment, Issue history, or prior sessions",
		"Project Runbook",
		"pnpm install",
		"Use this as advisory project memory",
		"Project fallback namespace: demo-ns",
		"Session context file: /tmp/context.md",
		"Artifact directory: /tmp/artifacts",
		"project-runbook.md",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", expected, prompt)
		}
	}
	for _, unexpected := range []string{
		"Deploy command: pnpm deploy",
		"Validation command: pnpm test",
	} {
		if strings.Contains(prompt, unexpected) {
			t.Fatalf("expected prompt not to contain %q, got:\n%s", unexpected, prompt)
		}
	}
	if strings.Index(prompt, "你好，codex。你之前做过这个任务了吗？") > strings.Index(prompt, "## Issue") {
		t.Fatalf("expected current turn request to appear before original issue context, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "Implement the issue as far as practical") {
		t.Fatalf("expected prompt to avoid unconditional fresh implementation wording, got:\n%s", prompt)
	}
}

func newAuthTestApp(t *testing.T) (*app, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "mspace.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	application := &app{
		db:         db,
		broker:     newEventBroker(),
		workdir:    t.TempDir(),
		repoRoot:   t.TempDir(),
		cancellers: map[string]sessionCanceller{},
	}
	if err := application.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return application, db
}

func configureTestHumanAuth(t *testing.T, application *app) string {
	t.Helper()
	token := "human-token"
	workspaceID := "workspace-1"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/me" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			writeError(w, http.StatusUnauthorized, errUnauthorized)
			return
		}
		writeJSON(w, map[string]any{
			"user": map[string]string{
				"id":        "user-1",
				"name":      "Test Human",
				"email":     "test@example.com",
				"avatarUrl": "https://example.com/avatar.png",
			},
			"workspaces": []map[string]string{{"id": workspaceID, "kind": "team"}},
		})
	}))
	t.Cleanup(server.Close)
	application.controlPlaneBaseURL = server.URL
	application.controlPlaneWorkspaceID = workspaceID
	return token
}

func authRequest(method, target, body, token string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if strings.TrimSpace(body) != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func multipartFileRequest(t *testing.T, method, target, filename string, content []byte, token string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(method, target, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func insertAuthTestIssue(t *testing.T, db *sql.DB, issueID, parentIssueID, title, status string) {
	t.Helper()
	now := nowString()
	if _, err := db.Exec(`
		INSERT OR IGNORE INTO projects (id, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, namespace, created_at, updated_at)
		VALUES ('project-1', 'Demo', '/tmp/demo', 'local', '', '', '', '', 'main', '', '', '', '', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert auth test project: %v", err)
	}
	var parent any
	if parentIssueID != "" {
		parent = parentIssueID
	}
	if _, err := db.Exec(`
		INSERT INTO issues (id, project_id, parent_issue_id, sort_order, title, body, status, triage_status, assignee, assignee_type, environment_url, created_at, updated_at)
		VALUES (?, 'project-1', ?, 0, ?, '', ?, 'pending', 'me', 'human', '', ?, ?)
	`, issueID, parent, title, status, now, now); err != nil {
		t.Fatalf("insert auth test issue: %v", err)
	}
	if parentIssueID == "" {
		if _, err := db.Exec(`
			INSERT INTO inbox_items (id, issue_id, project_id, title, status, unread, created_at, updated_at)
			VALUES (?, ?, 'project-1', ?, ?, 0, ?, ?)
		`, "inbox-"+issueID, issueID, title, status, now, now); err != nil {
			t.Fatalf("insert auth test inbox item: %v", err)
		}
	}
}

func insertAuthTestAgentSession(t *testing.T, db *sql.DB, sessionID, issueID string) string {
	t.Helper()
	now := nowString()
	token := agentTokenPrefix + strings.ReplaceAll(sessionID, "-", "") + "token"
	if _, err := db.Exec(`
		INSERT INTO agent_sessions (id, issue_id, provider, agent_profile, runtime_mode, command, status, branch, workdir, codex_thread_id, codex_turn_id, agent_status, artifact_dir, source_session_id, source_commit_sha, agent_token, cleanup_status, cleaned_at, created_at, updated_at)
		VALUES (?, ?, 'codex', 'codex', 'local', 'Implement the issue.', 'running', 'mspace/issue/session', '/tmp/workdir', '', '', 'running', '', '', '', ?, 'retained', '', ?, ?)
	`, sessionID, issueID, token, now, now); err != nil {
		t.Fatalf("insert auth test agent session: %v", err)
	}
	return token
}

func assertCommentContains(t *testing.T, db *sql.DB, issueID, expected string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM comments WHERE issue_id = ? AND body LIKE ?`, issueID, "%"+expected+"%").Scan(&count); err != nil {
		t.Fatalf("query comments: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected comment on %s containing %q", issueID, expected)
	}
}

func assertCommentAuthorContains(t *testing.T, db *sql.DB, issueID, expectedBody, authorType, authorName string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM comments
		WHERE issue_id = ? AND body LIKE ? AND author_type = ? AND author_name = ?
	`, issueID, "%"+expectedBody+"%", authorType, authorName).Scan(&count); err != nil {
		t.Fatalf("query comments by author: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected comment on %s containing %q from %s/%s", issueID, expectedBody, authorType, authorName)
	}
}

func assertSessionLogContains(t *testing.T, db *sql.DB, sessionID, expected string) {
	t.Helper()
	if sessionLogContains(db, sessionID, expected) {
		return
	}
	t.Fatalf("expected session log on %s containing %q", sessionID, expected)
}

func sessionLogContains(db *sql.DB, sessionID, expected string) bool {
	var count int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM session_logs
		WHERE session_id = ? AND message LIKE ?
	`, sessionID, "%"+expected+"%").Scan(&count); err != nil {
		return false
	}
	return count > 0
}

func sessionLogMessages(db *sql.DB, sessionID string) []string {
	rows, err := db.Query(`
		SELECT stream, message
		FROM session_logs
		WHERE session_id = ?
		ORDER BY id
	`, sessionID)
	if err != nil {
		return []string{"query session logs: " + err.Error()}
	}
	defer rows.Close()

	var logs []string
	for rows.Next() {
		var stream, message string
		if err := rows.Scan(&stream, &message); err != nil {
			return append(logs, "scan session log: "+err.Error())
		}
		logs = append(logs, stream+": "+message)
	}
	return logs
}

func writeJSONStatus(t *testing.T, w http.ResponseWriter, status int, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("write json response: %v", err)
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool, details ...func() string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(details) > 0 && details[0] != nil {
		t.Fatalf("condition was not met within %s: %s", timeout, details[0]())
	}
	t.Fatalf("condition was not met within %s", timeout)
}

func projectColumnExists(t *testing.T, db *sql.DB, column string) bool {
	t.Helper()
	return tableColumnExists(t, db, "projects", column)
}

func tableColumnExists(t *testing.T, db *sql.DB, tableName, column string) bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + tableName + `)`)
	if err != nil {
		t.Fatalf("query table info: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table info: %v", err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table info: %v", err)
	}
	return false
}

func tableIndexExists(t *testing.T, db *sql.DB, tableName, indexName string) bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA index_list(` + tableName + `)`)
	if err != nil {
		t.Fatalf("query index list: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			t.Fatalf("scan index list: %v", err)
		}
		if name == indexName {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate index list: %v", err)
	}
	return false
}

func runGit(t *testing.T, repoDir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func gitOutput(t *testing.T, repoDir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}
