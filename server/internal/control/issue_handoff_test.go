package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const pullRequestHandoffSourceCommitSHA = "1111111111111111111111111111111111111111"

func TestMemoryStoreCreatePullRequestSessionQueuesCodexAutomation(t *testing.T) {
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
	seedActiveCodexWorker(t, store, workspaceID, "personal")
	project, issueID := seedPullRequestIssue(t, ctx, store, user, workspaceID, "Fix PR handoff")
	seedIssueSourceTask(t, store, workspaceID, issueID, project.ID, "session-source-personal", "personal", pullRequestHandoffSourceCommitSHA, "mspace/issue-pr")

	session, err := store.CreateIssuePullRequestSession(ctx, user.ID, workspaceID, issueID, CreatePullRequestInput{
		SourceCommitSHA: pullRequestHandoffSourceCommitSHA[:12],
	})
	if err != nil {
		t.Fatalf("create PR session: %v", err)
	}
	if session.AgentEngine != "codex" || session.Automation != pullRequestHandoffAutomation || session.Status != "queued" {
		t.Fatalf("unexpected PR session: %+v", session)
	}
	if session.SourceSessionID != "session-source-personal" || session.SourceCommitSHA != pullRequestHandoffSourceCommitSHA || session.Branch != "mspace/issue-pr" {
		t.Fatalf("unexpected source binding: %+v", session)
	}
	if !strings.Contains(session.Command, "pull-request.json") || !strings.Contains(session.Command, "pr-creator") {
		t.Fatalf("PR prompt should instruct Codex to create the PR and write the artifact, got:\n%s", session.Command)
	}
	if len(session.RequiredSkills) != 1 || session.RequiredSkills[0].Slug != pullRequestCreatorSkillSlug {
		t.Fatalf("PR session should require the PR creator skill, got %+v", session.RequiredSkills)
	}
	store.mu.Lock()
	var taskPayload json.RawMessage
	for _, candidate := range store.runtimeTasks {
		if candidate.SessionID == session.ID {
			taskPayload = append(json.RawMessage(nil), candidate.Payload...)
			break
		}
	}
	store.mu.Unlock()
	if len(taskPayload) == 0 {
		t.Fatalf("expected runtime task payload for session %s", session.ID)
	}
	var payload struct {
		RequiredSkills []RuntimeSkillBundle `json:"requiredSkills"`
	}
	if err := json.Unmarshal(taskPayload, &payload); err != nil {
		t.Fatalf("parse runtime payload: %v", err)
	}
	if len(payload.RequiredSkills) != 1 || payload.RequiredSkills[0].Slug != pullRequestCreatorSkillSlug || len(payload.RequiredSkills[0].Files) == 0 {
		t.Fatalf("expected full PR creator skill bundle in runtime payload, got %+v", payload.RequiredSkills)
	}

	detail, err := store.GetIssue(ctx, user.ID, workspaceID, issueID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if len(detail.Handoffs) != 1 {
		t.Fatalf("expected one placeholder handoff, got %+v", detail.Handoffs)
	}
	handoff := detail.Handoffs[0]
	if handoff.PRURL != "" || handoff.CreatedVia != "codex" || handoff.Branch != "mspace/issue-pr" {
		t.Fatalf("unexpected placeholder handoff: %+v", handoff)
	}

	seedIssueSourceTask(t, store, workspaceID, issueID, project.ID, "session-source-second", "personal", "2222222222222222222222222222222222222222", "mspace/issue-pr-second")
	_, err = store.CreateIssuePullRequestSession(ctx, user.ID, workspaceID, issueID, CreatePullRequestInput{
		SourceSessionID: "session-source-second",
	})
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected active issue PR session to reject another source, got %v", err)
	}
}

func TestMemoryStoreReconcilesCodexPullRequestArtifact(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	user, workspaces, err := store.CreatePasswordIdentity(ctx, PasswordAuthInput{
		Login:    "pr-artifact-user",
		Password: "password-123456",
		Name:     "PR Artifact User",
	})
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	workspaceID := workspaces[0].ID
	project, issueID := seedPullRequestIssue(t, ctx, store, user, workspaceID, "Record PR artifact")
	seedIssueSourceTask(t, store, workspaceID, issueID, project.ID, "session-source-artifact", "personal", pullRequestHandoffSourceCommitSHA, "mspace/artifact-pr")

	payload, err := json.Marshal(map[string]any{
		"prompt":          "Create the pull request.",
		"agentEngine":     "codex",
		"automation":      pullRequestHandoffAutomation,
		"branch":          "mspace/artifact-pr",
		"sourceSessionId": "session-source-artifact",
		"sourceCommitSha": pullRequestHandoffSourceCommitSHA,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	result, err := json.Marshal(map[string]any{
		"agentEngine": "codex",
		"status":      "completed",
		"pullRequest": map[string]any{
			"url":           "https://github.com/mlhiter/mspace/pull/42",
			"number":        42,
			"state":         "OPEN",
			"title":         "fix: create PRs through Codex",
			"headCommitSha": pullRequestHandoffSourceCommitSHA,
			"repository":    "mlhiter/mspace",
			"branch":        "mspace/artifact-pr",
		},
	})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	task := RuntimeTask{
		ID:          "task-pr-artifact",
		WorkspaceID: workspaceID,
		IssueID:     issueID,
		SessionID:   "session-pr-artifact",
		ProjectID:   project.ID,
		Kind:        "agent_session",
		Status:      "completed",
		RuntimeMode: "personal",
		Payload:     payload,
		Result:      result,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	store.mu.Lock()
	store.runtimeTasks[task.ID] = task
	store.reconcileAgentSessionRuntimeResultLocked(task)
	store.mu.Unlock()

	detail, err := store.GetIssue(ctx, user.ID, workspaceID, issueID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if len(detail.Handoffs) != 1 {
		t.Fatalf("expected one PR handoff, got %+v", detail.Handoffs)
	}
	handoff := detail.Handoffs[0]
	if handoff.PRURL != "https://github.com/mlhiter/mspace/pull/42" || handoff.PRNumber != 42 || handoff.PRState != "open" {
		t.Fatalf("unexpected PR metadata: %+v", handoff)
	}
	if handoff.CreatedVia != "codex" || handoff.HeadCommitSHA != pullRequestHandoffSourceCommitSHA || handoff.LastCheckedAt == "" {
		t.Fatalf("unexpected handoff provenance: %+v", handoff)
	}

	refreshed, err := store.RefreshIssueHandoff(ctx, user.ID, workspaceID, issueID, handoff.ID, IssueHandoffRefreshInput{})
	if err != nil {
		t.Fatalf("refresh PR handoff: %v", err)
	}
	if refreshed.Error != "" || refreshed.PRURL != handoff.PRURL || refreshed.PRNumber != handoff.PRNumber {
		t.Fatalf("personal Codex PR refresh should keep the recorded PR without GitHub App errors, got %+v", refreshed)
	}
	if refreshed.LastCheckedAt == "" {
		t.Fatalf("refresh should update last checked time: %+v", refreshed)
	}
}

func TestNormalizeGitHubPullRequestState(t *testing.T) {
	tests := []struct {
		name string
		pr   GitHubPullRequest
		want string
	}{
		{
			name: "merged flag wins over closed state",
			pr:   GitHubPullRequest{State: "closed", Merged: true},
			want: "merged",
		},
		{
			name: "merged timestamp wins over closed state",
			pr:   GitHubPullRequest{State: "closed", MergedAt: "2026-07-30T03:00:00Z"},
			want: "merged",
		},
		{
			name: "closed without merged stays closed",
			pr:   GitHubPullRequest{State: "closed"},
			want: "closed",
		},
		{
			name: "open draft stays draft",
			pr:   GitHubPullRequest{State: "open", Draft: true},
			want: "draft",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeGitHubPullRequestState(tt.pr); got != tt.want {
				t.Fatalf("normalizeGitHubPullRequestState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMemoryStoreRecordsPullRequestHandoffErrorWhenArtifactMissing(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	user, workspaces, err := store.CreatePasswordIdentity(ctx, PasswordAuthInput{
		Login:    "pr-missing-artifact-user",
		Password: "password-123456",
		Name:     "PR Missing Artifact User",
	})
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	workspaceID := workspaces[0].ID
	project, issueID := seedPullRequestIssue(t, ctx, store, user, workspaceID, "Missing PR artifact")
	seedIssueSourceTask(t, store, workspaceID, issueID, project.ID, "session-source-missing-artifact", "personal", pullRequestHandoffSourceCommitSHA, "mspace/missing-artifact-pr")

	payload, err := json.Marshal(map[string]any{
		"prompt":          "Create the pull request.",
		"agentEngine":     "codex",
		"automation":      pullRequestHandoffAutomation,
		"branch":          "mspace/missing-artifact-pr",
		"sourceSessionId": "session-source-missing-artifact",
		"sourceCommitSha": pullRequestHandoffSourceCommitSHA,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	result, err := json.Marshal(map[string]any{
		"agentEngine": "codex",
		"status":      "completed",
	})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	task := RuntimeTask{
		ID:          "task-pr-missing-artifact",
		WorkspaceID: workspaceID,
		IssueID:     issueID,
		SessionID:   "session-pr-missing-artifact",
		ProjectID:   project.ID,
		Kind:        "agent_session",
		Status:      "completed",
		RuntimeMode: "personal",
		Payload:     payload,
		Result:      result,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	store.mu.Lock()
	store.runtimeTasks[task.ID] = task
	store.reconcileAgentSessionRuntimeResultLocked(task)
	store.mu.Unlock()

	detail, err := store.GetIssue(ctx, user.ID, workspaceID, issueID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if len(detail.Handoffs) != 1 || !strings.Contains(detail.Handoffs[0].Error, "pull-request.json") {
		t.Fatalf("expected handoff error for missing artifact, got %+v", detail.Handoffs)
	}
}

func TestMemoryStorePullRequestSessionRejectsTeamWorkspace(t *testing.T) {
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
	project, issueID := seedPullRequestIssue(t, ctx, store, user, workspace.ID, "Team PR handoff")
	seedIssueSourceTask(t, store, workspace.ID, issueID, project.ID, "session-source-team", "team", pullRequestHandoffSourceCommitSHA, "mspace/team-pr")

	_, err = store.CreateIssuePullRequestSession(ctx, user.ID, workspace.ID, issueID, CreatePullRequestInput{
		SourceCommitSHA: pullRequestHandoffSourceCommitSHA,
	})
	if err == nil || !strings.Contains(err.Error(), "personal workspaces") {
		t.Fatalf("expected team workspace to reject Codex PR sessions, got %v", err)
	}

	_, err = store.CreateIssuePullRequestHandoff(ctx, user.ID, workspace.ID, issueID, CreatePullRequestInput{
		SourceCommitSHA: pullRequestHandoffSourceCommitSHA,
		HeadCommitSHA:   pullRequestHandoffSourceCommitSHA,
		PRURL:           "https://github.com/mlhiter/mspace/pull/43",
		PRNumber:        43,
		PRState:         "open",
		CreatedVia:      "codex",
	})
	if err == nil || !strings.Contains(err.Error(), "personal workspaces") {
		t.Fatalf("expected team workspace to reject Codex PR metadata, got %v", err)
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

func TestNormalizeCreatePullRequestInputRequiresHeadCommitForPullRequestURL(t *testing.T) {
	_, err := normalizeCreatePullRequestInput(CreatePullRequestInput{
		SourceCommitSHA: pullRequestHandoffSourceCommitSHA,
		PRURL:           "https://github.com/mlhiter/mspace/pull/42",
		PRNumber:        42,
		CreatedVia:      "codex",
	}, Workspace{Kind: "personal"}, Project{
		GitOwner: "mlhiter",
		GitRepo:  "mspace",
	}, IssueChangeNode{
		CommitSHA: pullRequestHandoffSourceCommitSHA,
	})
	if err == nil || !strings.Contains(err.Error(), "head commit") {
		t.Fatalf("expected PR URL metadata to require head commit, got %v", err)
	}
}

func seedPullRequestIssue(t *testing.T, ctx context.Context, store *MemoryStore, user User, workspaceID, title string) (Project, string) {
	t.Helper()
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
		Title:     title,
		Body:      "Create the pull request through Codex.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return project, issueID
}

func seedActiveCodexWorker(t *testing.T, store *MemoryStore, workspaceID, mode string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	store.mu.Lock()
	defer store.mu.Unlock()
	store.runtimeWorkers[workspaceID+":worker-pr"] = RuntimeWorker{
		ID:           "worker-pr",
		WorkspaceID:  workspaceID,
		Name:         "PR Worker",
		Mode:         mode,
		Status:       "online",
		Capabilities: json.RawMessage(`{"codex":true}`),
		Labels:       json.RawMessage(`{}`),
		LastSeenAt:   now,
		CreatedAt:    now,
		UpdatedAt:    now,
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
