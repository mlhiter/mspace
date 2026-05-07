package main

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

	for _, column := range []string{"source_type", "remote_url", "git_provider", "git_owner", "git_repo"} {
		if !projectColumnExists(t, db, column) {
			t.Fatalf("expected projects.%s to exist", column)
		}
	}
}

func TestEnsureIssueColumnsAddsAssigneeType(t *testing.T) {
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
