package main

import (
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
