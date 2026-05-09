package main

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

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
	if tasks[1].Title != "Keep completed tasks checked" || tasks[1].Status != "completed" {
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
	router.ServeHTTP(wrongParent, httptest.NewRequest(http.MethodDelete, "/api/issues/issue-2/tasks/task-1", nil))
	if wrongParent.Code != http.StatusNotFound {
		t.Fatalf("expected wrong parent delete to return 404, got %d body=%s", wrongParent.Code, wrongParent.Body.String())
	}

	correctParent := httptest.NewRecorder()
	router.ServeHTTP(correctParent, httptest.NewRequest(http.MethodDelete, "/api/issues/issue-1/tasks/task-1", nil))
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

	for _, column := range []string{"codex_thread_id", "codex_turn_id", "agent_status", "artifact_dir", "agent_profile", "cleanup_status", "cleaned_at"} {
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
	for _, column := range []string{"issue_id", "cluster_id", "namespace", "namespace_status", "cleanup_status", "preview_url", "image_registry_prefix", "kubeconfig_path", "kube_context", "exposure_mode", "last_deploy_session_id", "last_cleanup_session_id"} {
		if !tableColumnExists(t, db, "issue_test_environments", column) {
			t.Fatalf("expected issue_test_environments.%s to exist", column)
		}
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
		VALUES ('issue-1', 'project-1', 'Cleanup issue', 'Clean this session.', 'completed', 'codex', 'agent', '', ?, ?)
	`, now, now); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO inbox_items (id, issue_id, project_id, title, status, unread, created_at, updated_at)
		VALUES ('inbox-1', 'issue-1', 'project-1', 'Cleanup issue', 'completed', 0, ?, ?)
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
		"Project fallback namespace: demo-ns",
		"Deploy command: pnpm deploy",
		"Validation command: pnpm test",
		"Session context file: /tmp/context.md",
		"Artifact directory: /tmp/artifacts",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", expected, prompt)
		}
	}
	if strings.Index(prompt, "你好，codex。你之前做过这个任务了吗？") > strings.Index(prompt, "## Issue") {
		t.Fatalf("expected current turn request to appear before original issue context, got:\n%s", prompt)
	}
	if strings.Contains(prompt, "Implement the issue as far as practical") {
		t.Fatalf("expected prompt to avoid unconditional fresh implementation wording, got:\n%s", prompt)
	}
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}
