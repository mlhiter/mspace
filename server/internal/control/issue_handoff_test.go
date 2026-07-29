package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const pullRequestHandoffSourceCommitSHA = "1111111111111111111111111111111111111111"

func TestMemoryStoreCreatePullRequestHandoffAcceptsDesktopGitHubMetadataForPersonalWorkspace(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	user, workspaces, err := store.CreatePasswordIdentity(ctx, PasswordAuthInput{
		Login:    "pr-personal-user",
		Password: "password-123456",
		Name:     "PR Personal User",
	})
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	workspaceID := workspaces[0].ID
	project, err := store.CreateProject(ctx, user.ID, workspaceID, ProjectInput{
		SourceType:    "github",
		RepoURL:       "https://github.com/mlhiter/mspace.git",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	issueID, err := store.CreateIssue(ctx, user, workspaceID, CreateIssueInput{
		ProjectID: project.ID,
		Title:     "Fix PR handoff",
		Body:      "Create the pull request from the desktop.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	seedIssueSourceTask(t, store, workspaceID, issueID, project.ID, "session-source-personal", "personal", pullRequestHandoffSourceCommitSHA, "mspace/issue-pr")

	handoff, err := store.CreateIssuePullRequestHandoff(ctx, user.ID, workspaceID, issueID, CreatePullRequestInput{
		SourceCommitSHA: pullRequestHandoffSourceCommitSHA[:12],
		HeadCommitSHA:   pullRequestHandoffSourceCommitSHA,
		Title:           "fix: make pull requests work",
		PRURL:           "https://github.com/mlhiter/mspace/pull/42",
		PRNumber:        42,
		PRState:         "OPEN",
		CreatedVia:      "desktop-gh",
	})
	if err != nil {
		t.Fatalf("create PR handoff: %v", err)
	}
	if handoff.PRURL != "https://github.com/mlhiter/mspace/pull/42" || handoff.PRNumber != 42 || handoff.PRState != "open" {
		t.Fatalf("unexpected PR metadata: %+v", handoff)
	}
	if handoff.CreatedVia != "desktop-gh" || handoff.HeadCommitSHA != pullRequestHandoffSourceCommitSHA || handoff.LastCheckedAt == "" {
		t.Fatalf("unexpected handoff provenance: %+v", handoff)
	}

	refreshed, err := store.RefreshIssueHandoff(ctx, user.ID, workspaceID, issueID, handoff.ID)
	if err != nil {
		t.Fatalf("refresh handoff: %v", err)
	}
	if !strings.Contains(refreshed.Error, "server-owned GitHub App PR executor") {
		t.Fatalf("refresh should report missing server executor, got %+v", refreshed)
	}
}

func TestMemoryStoreCreatePullRequestHandoffRejectsDesktopGitHubMetadataForTeamWorkspace(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	user, _, err := store.CreatePasswordIdentity(ctx, PasswordAuthInput{
		Login:    "pr-team-user",
		Password: "password-123456",
		Name:     "PR Team User",
	})
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	workspace, _, err := store.CreateWorkspace(ctx, user.ID, CreateWorkspaceInput{
		Name: "Team PR Workspace",
		Kind: "team",
	})
	if err != nil {
		t.Fatalf("create team workspace: %v", err)
	}
	project, err := store.CreateProject(ctx, user.ID, workspace.ID, ProjectInput{
		SourceType:    "github",
		RepoURL:       "https://github.com/mlhiter/mspace.git",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	issueID, err := store.CreateIssue(ctx, user, workspace.ID, CreateIssueInput{
		ProjectID: project.ID,
		Title:     "Team PR handoff",
		Body:      "Team workspace should wait for the server-owned executor.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	seedIssueSourceTask(t, store, workspace.ID, issueID, project.ID, "session-source-team", "team", pullRequestHandoffSourceCommitSHA, "mspace/team-pr")

	_, err = store.CreateIssuePullRequestHandoff(ctx, user.ID, workspace.ID, issueID, CreatePullRequestInput{
		SourceCommitSHA: pullRequestHandoffSourceCommitSHA,
		HeadCommitSHA:   pullRequestHandoffSourceCommitSHA,
		PRURL:           "https://github.com/mlhiter/mspace/pull/43",
		PRNumber:        43,
		PRState:         "open",
		CreatedVia:      "desktop-gh",
	})
	if err == nil || !strings.Contains(err.Error(), "personal workspaces") {
		t.Fatalf("expected team workspace to reject desktop PR metadata, got %v", err)
	}

	handoff, err := store.CreateIssuePullRequestHandoff(ctx, user.ID, workspace.ID, issueID, CreatePullRequestInput{
		SourceCommitSHA: pullRequestHandoffSourceCommitSHA,
		Title:           "fix: server-owned placeholder",
	})
	if err != nil {
		t.Fatalf("server-owned handoff placeholder should still be allowed: %v", err)
	}
	if handoff.PRURL != "" || handoff.CreatedVia != "server" {
		t.Fatalf("unexpected server-owned handoff: %+v", handoff)
	}
}

func TestGitOwnerRepoFromPullRequestURLRequiresCanonicalPullRequestPath(t *testing.T) {
	if _, err := gitOwnerRepoFromPullRequestURL("https://github.com/mlhiter/mspace/pull/42/files"); err == nil {
		t.Fatalf("expected extra PR URL path segments to be rejected")
	}
	if _, err := gitOwnerRepoFromPullRequestURL("https://github.com/mlhiter/mspace/pulls/42"); err == nil {
		t.Fatalf("expected non-canonical PR URL path to be rejected")
	}
}

func seedIssueSourceTask(t *testing.T, store *MemoryStore, workspaceID, issueID, projectID, sessionID, runtimeMode, commitSHA, branch string) {
	t.Helper()
	result, err := json.Marshal(struct {
		Source runtimeTaskSourceSnapshot `json:"source"`
	}{
		Source: runtimeTaskSourceSnapshot{
			CommitSHA:      commitSHA,
			ShortCommitSHA: shortCommitSHA(commitSHA),
			Branch:         branch,
			Subject:        "fix source",
			FilesChanged:   1,
			Changes:        []WorkspaceChange{},
			DiffPreview:    "diff --git a/app.go b/app.go",
		},
	})
	if err != nil {
		t.Fatalf("marshal runtime result: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	store.mu.Lock()
	defer store.mu.Unlock()
	store.runtimeTasks["task-"+sessionID] = RuntimeTask{
		ID:          "task-" + sessionID,
		WorkspaceID: workspaceID,
		IssueID:     issueID,
		SessionID:   sessionID,
		ProjectID:   projectID,
		Kind:        "agent_session",
		Status:      "completed",
		RuntimeMode: runtimeMode,
		Result:      result,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}
