package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerStorageIDPersistsAndCapabilityIsForced(t *testing.T) {
	workRoot := t.TempDir()
	first, err := ensureWorkerStorageID(workRoot)
	if err != nil {
		t.Fatalf("create worker storage id: %v", err)
	}
	second, err := ensureWorkerStorageID(workRoot)
	if err != nil {
		t.Fatalf("reload worker storage id: %v", err)
	}
	if first != second || !validWorkerStorageID(first) {
		t.Fatalf("expected one persistent valid storage id, first=%q second=%q", first, second)
	}
	other, err := ensureWorkerStorageID(t.TempDir())
	if err != nil {
		t.Fatalf("create storage id for another root: %v", err)
	}
	if other == first {
		t.Fatalf("different storage roots must not share identity: %q", first)
	}
	capabilities, err := requireIssueWorkingCopyCapability(json.RawMessage(`{"issueWorkingCopyV1":false,"codex":true}`))
	if err != nil {
		t.Fatalf("force working-copy capability: %v", err)
	}
	if !capabilityEnabled(capabilities, issueWorkingCopyCapability) || !capabilityEnabled(capabilities, "codex") {
		t.Fatalf("expected forced working-copy capability without losing existing capabilities: %s", capabilities)
	}
}

func TestParseIssueWorkingCopyPayloadContract(t *testing.T) {
	raw := json.RawMessage(`{
		"agentEngine":"claude_code",
		"prompt":"continue the issue",
		"issueId":"issue-1",
		"sessionId":"session-2",
		"projectId":"project-1",
		"branch":"mspace/issue-1",
		"executionMode":"issue_working_copy",
		"workingCopyGeneration":4,
		"expectedHeadSha":"0123456789012345678901234567890123456789",
		"initialize":false,
		"repository":{"url":"https://example.invalid/repo.git","defaultBranch":"main"}
	}`)
	payload, err := parseAgentSessionPayload(raw)
	if err != nil {
		t.Fatalf("parse issue working-copy payload: %v", err)
	}
	if !payload.usesIssueWorkingCopy() || payload.WorkingCopyGeneration != 4 || payload.ExpectedHeadSHA != "0123456789012345678901234567890123456789" || payload.Initialize {
		t.Fatalf("unexpected parsed working-copy fields: %+v", payload)
	}

	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("decode payload fixture: %v", err)
	}
	delete(record, "expectedHeadSha")
	missingHead, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("encode invalid payload: %v", err)
	}
	if _, err := parseAgentSessionPayload(missingHead); err == nil || !strings.Contains(err.Error(), "expectedHeadSha") {
		t.Fatalf("expected existing working copy without expected HEAD to fail closed, got %v", err)
	}
}

func TestIssueWorkingCopyReusesPathAndAdvancesLinearHead(t *testing.T) {
	ctx := context.Background()
	gitPath, repoDir, cfg, payload, runtimeClient, closeServer := issueWorkingCopyFixture(t)
	defer closeServer()

	first, err := prepareIssueWorkingCopy(ctx, gitPath, repoDir, cfg, payload)
	if err != nil {
		t.Fatalf("initialize issue working copy: %v", err)
	}
	if first.WorkingCopyRoot != filepath.Join(cfg.WorkRoot, "workdirs", payload.ProjectID, payload.IssueID) {
		t.Fatalf("unexpected issue worktree path: %q", first.WorkingCopyRoot)
	}
	if first.ArtifactDir != filepath.Join(cfg.WorkRoot, "artifacts", payload.ProjectID, payload.IssueID, payload.SessionID) {
		t.Fatalf("unexpected first artifact path: %q", first.ArtifactDir)
	}
	if err := os.MkdirAll(first.ArtifactDir, 0o755); err != nil {
		t.Fatalf("create first artifact directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(first.ArtifactDir, branchNameArtifactName), []byte(`{"branch":"fix/must-not-rename"}`), 0o600); err != nil {
		t.Fatalf("write semantic branch artifact: %v", err)
	}
	if err := os.WriteFile(filepath.Join(first.Workdir, "first.txt"), []byte("first\n"), 0o644); err != nil {
		t.Fatalf("write first source change: %v", err)
	}
	firstSource, err := captureAgentSessionSource(ctx, runtimeClient, "worker-1", "task-1", first)
	if err != nil {
		t.Fatalf("capture first source change: %v", err)
	}
	if firstSource.CommitSHA == "" || firstSource.Branch != payload.Branch {
		t.Fatalf("unexpected first source result: %+v", firstSource)
	}
	branch, err := runGitOutput(ctx, gitPath, first.WorkingCopyRoot, "symbolic-ref", "--short", "HEAD")
	if err != nil || strings.TrimSpace(branch) != payload.Branch {
		t.Fatalf("mutable session must retain stable branch, branch=%q err=%v", branch, err)
	}

	secondPayload := payload
	secondPayload.SessionID = "session-2"
	secondPayload.Initialize = false
	secondPayload.ExpectedHeadSHA = firstSource.CommitSHA
	second, err := prepareIssueWorkingCopy(ctx, gitPath, repoDir, cfg, secondPayload)
	if err != nil {
		t.Fatalf("reuse issue working copy: %v", err)
	}
	if second.WorkingCopyRoot != first.WorkingCopyRoot || second.Workdir != first.Workdir {
		t.Fatalf("two turns must reuse one worktree: first=%q second=%q", first.WorkingCopyRoot, second.WorkingCopyRoot)
	}
	if second.ArtifactDir == first.ArtifactDir || second.ArtifactDir != filepath.Join(cfg.WorkRoot, "artifacts", payload.ProjectID, payload.IssueID, secondPayload.SessionID) {
		t.Fatalf("session artifacts must remain isolated: first=%q second=%q", first.ArtifactDir, second.ArtifactDir)
	}
	if err := os.WriteFile(filepath.Join(second.Workdir, "second.txt"), []byte("second\n"), 0o644); err != nil {
		t.Fatalf("write second source change: %v", err)
	}
	secondSource, err := captureAgentSessionSource(ctx, runtimeClient, "worker-1", "task-2", second)
	if err != nil {
		t.Fatalf("capture second source change: %v", err)
	}
	if secondSource.CommitSHA == "" || secondSource.CommitSHA == firstSource.CommitSHA {
		t.Fatalf("expected second turn to advance HEAD: first=%q second=%q", firstSource.CommitSHA, secondSource.CommitSHA)
	}
	if err := runGitCommand(ctx, gitPath, second.WorkingCopyRoot, "merge-base", "--is-ancestor", firstSource.CommitSHA, secondSource.CommitSHA); err != nil {
		t.Fatalf("expected linear source history: %v", err)
	}
	envelope := inspectIssueWorkingCopy(ctx, second, "")
	if envelope.ContentState != "clean" || envelope.HeadCommitSHA != secondSource.CommitSHA || envelope.Generation != secondPayload.WorkingCopyGeneration {
		t.Fatalf("unexpected terminal working-copy envelope: %+v", envelope)
	}
}

func TestIssueWorkingCopyMaterializesSkillsOutsideGitWorktree(t *testing.T) {
	installFakeEngine(t, "claude", "fake_claude.py")
	ctx := context.Background()
	gitPath, _, cfg, payload, runtimeClient, closeServer := issueWorkingCopyFixture(t)
	defer closeServer()
	payload.AgentEngine = agentEngineClaudeCode
	payload.Env = map[string]string{
		"MSPACE_FAKE_CLAUDE_MODE":         "success",
		"MSPACE_FAKE_CLAUDE_EXPECT_SKILL": "think/SKILL.md",
	}
	skillContent := "# Think\n\nUse the issue context.\n"
	payload.RequiredSkills = []skillBundle{{
		Slug:     "think",
		Name:     "Think",
		Revision: "rev-1",
		Files: []skillBundleFile{{
			Path:    "SKILL.md",
			Content: stringRef(skillContent),
			SHA256:  sha256Hex([]byte(skillContent)),
		}},
	}}

	result, err := runAgentSession(ctx, runtimeClient, cfg, "worker-1", "task-skills", payload)
	if err != nil {
		t.Fatalf("run issue working-copy session with required skill: %v", err)
	}
	if result.Status != "completed" || result.EngineSessionRef != "claude-session-opaque" || result.EngineRunRef != "claude-run-opaque" {
		t.Fatalf("expected required skill session to reach Agent launch, got %+v", result)
	}
	expectedSkillPath := filepath.Join(cfg.WorkRoot, "artifacts", payload.ProjectID, payload.IssueID, payload.SessionID, "skills", "think", "SKILL.md")
	data, err := os.ReadFile(expectedSkillPath)
	if err != nil || string(data) != skillContent {
		t.Fatalf("expected materialized external skill, data=%q err=%v", data, err)
	}
	if result.ArtifactDir != filepath.Join(cfg.WorkRoot, "artifacts", payload.ProjectID, payload.IssueID, payload.SessionID) {
		t.Fatalf("unexpected external artifact dir: %q", result.ArtifactDir)
	}
	status, err := runGitOutput(ctx, gitPath, filepath.Join(cfg.WorkRoot, "workdirs", payload.ProjectID, payload.IssueID), "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		t.Fatalf("inspect issue working-copy status: %v", err)
	}
	if strings.TrimSpace(status) != "" {
		t.Fatalf("external skill materialization must not dirty the source worktree: %q", status)
	}
}

func TestIssueWorkingCopyRejectsSymlinkedArtifactDirectoryWithoutSkills(t *testing.T) {
	ctx := context.Background()
	_, _, cfg, payload, runtimeClient, closeServer := issueWorkingCopyFixture(t)
	defer closeServer()
	payload.ContextMarkdown = "must remain inside the Worker artifact root"
	outside := t.TempDir()
	expectedArtifactDir := filepath.Join(cfg.WorkRoot, "artifacts", payload.ProjectID, payload.IssueID, payload.SessionID)
	if err := os.MkdirAll(filepath.Dir(expectedArtifactDir), 0o755); err != nil {
		t.Fatalf("create artifact parent: %v", err)
	}
	if err := os.Symlink(outside, expectedArtifactDir); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}

	if _, err := prepareAgentSessionWorkspace(ctx, runtimeClient, cfg, "worker-1", "task-no-skills-symlink", payload); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("expected no-skill symlinked artifact directory to fail closed, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "context.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("context must not escape through artifact symlink, stat err=%v", err)
	}
}

func TestIssueWorkingCopyRejectsSymlinkedExternalArtifactDirectory(t *testing.T) {
	ctx := context.Background()
	_, _, cfg, payload, runtimeClient, closeServer := issueWorkingCopyFixture(t)
	defer closeServer()
	outside := t.TempDir()
	expectedArtifactDir := filepath.Join(cfg.WorkRoot, "artifacts", payload.ProjectID, payload.IssueID, payload.SessionID)
	if err := os.MkdirAll(filepath.Dir(expectedArtifactDir), 0o755); err != nil {
		t.Fatalf("create artifact parent: %v", err)
	}
	if err := os.Symlink(outside, expectedArtifactDir); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	skillContent := "# Unsafe target\n"
	payload.RequiredSkills = []skillBundle{{
		Name: "think",
		Files: []skillBundleFile{{
			Path:    "SKILL.md",
			Content: stringRef(skillContent),
		}},
	}}

	if _, err := prepareAgentSessionWorkspace(ctx, runtimeClient, cfg, "worker-1", "task-symlink", payload); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("expected symlinked external artifact directory to fail closed, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "skills", "think", "SKILL.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("skill materialization must not escape through artifact symlink, stat err=%v", err)
	}
}

func TestIssueWorkingCopyDirtyContinuationDoesNotResetChanges(t *testing.T) {
	ctx := context.Background()
	gitPath, repoDir, cfg, payload, _, closeServer := issueWorkingCopyFixture(t)
	defer closeServer()

	first, err := prepareIssueWorkingCopy(ctx, gitPath, repoDir, cfg, payload)
	if err != nil {
		t.Fatalf("initialize issue working copy: %v", err)
	}
	dirtyPath := filepath.Join(first.Workdir, "unfinished.txt")
	if err := os.WriteFile(dirtyPath, []byte("keep me\n"), 0o644); err != nil {
		t.Fatalf("write dirty source change: %v", err)
	}
	secondPayload := payload
	secondPayload.SessionID = "session-continue"
	secondPayload.Initialize = false
	secondPayload.ExpectedHeadSHA = first.WorkingCopyBaseCommitSHA
	second, err := prepareIssueWorkingCopy(ctx, gitPath, repoDir, cfg, secondPayload)
	if err != nil {
		t.Fatalf("continue dirty issue working copy: %v", err)
	}
	data, err := os.ReadFile(dirtyPath)
	if err != nil || string(data) != "keep me\n" {
		t.Fatalf("dirty continuation must preserve source changes, data=%q err=%v", data, err)
	}
	envelope := inspectIssueWorkingCopy(ctx, second, "")
	if envelope.ContentState != "dirty" || envelope.RecoveryReason != "" {
		t.Fatalf("expected an honest dirty envelope: %+v", envelope)
	}
	body, taskErr := marshalAgentSessionFailureResult(second, errors.New("agent failed"))
	if taskErr == nil {
		t.Fatal("expected original agent failure to be preserved")
	}
	var result agentSessionResult
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode failed task result: %v", err)
	}
	if result.WorkingCopy == nil || result.WorkingCopy.ContentState != "dirty" {
		t.Fatalf("failed task must return the dirty working-copy envelope: %+v", result.WorkingCopy)
	}
	cancelledBody, taskErr := marshalAgentSessionFailureResult(second, context.Canceled)
	if !errors.Is(taskErr, context.Canceled) {
		t.Fatalf("expected cancellation error to be preserved, got %v", taskErr)
	}
	if err := json.Unmarshal(cancelledBody, &result); err != nil {
		t.Fatalf("decode cancelled task result: %v", err)
	}
	if result.Status != "cancelled" || result.WorkingCopy == nil || result.WorkingCopy.ContentState != "dirty" {
		t.Fatalf("cancelled task must return the dirty working-copy envelope: %+v", result)
	}
}

func TestIssueWorkingCopyFailsClosedOnHeadAndBranchMismatch(t *testing.T) {
	ctx := context.Background()
	gitPath, repoDir, cfg, payload, _, closeServer := issueWorkingCopyFixture(t)
	defer closeServer()

	first, err := prepareIssueWorkingCopy(ctx, gitPath, repoDir, cfg, payload)
	if err != nil {
		t.Fatalf("initialize issue working copy: %v", err)
	}
	headMismatch := payload
	headMismatch.Initialize = false
	headMismatch.ExpectedHeadSHA = strings.Repeat("0", 40)
	if _, err := prepareIssueWorkingCopy(ctx, gitPath, repoDir, cfg, headMismatch); issueWorkingCopyRecoveryReason(err) != "head_mismatch" {
		t.Fatalf("expected head mismatch recovery, got %v", err)
	}
	if err := runGitCommand(ctx, gitPath, first.WorkingCopyRoot, "checkout", "-b", "other-branch"); err != nil {
		t.Fatalf("change working-copy branch: %v", err)
	}
	branchMismatch := payload
	branchMismatch.Initialize = false
	branchMismatch.ExpectedHeadSHA = first.WorkingCopyBaseCommitSHA
	if _, err := prepareIssueWorkingCopy(ctx, gitPath, repoDir, cfg, branchMismatch); issueWorkingCopyRecoveryReason(err) != "branch_mismatch" {
		t.Fatalf("expected branch mismatch recovery, got %v", err)
	}
}

func TestIssueWorkingCopyFailsClosedWhenSessionRewritesHistory(t *testing.T) {
	ctx := context.Background()
	gitPath, repoDir, cfg, payload, _, closeServer := issueWorkingCopyFixture(t)
	defer closeServer()

	prepared, err := prepareIssueWorkingCopy(ctx, gitPath, repoDir, cfg, payload)
	if err != nil {
		t.Fatalf("initialize issue working copy: %v", err)
	}
	if err := runGitCommand(ctx, gitPath, prepared.WorkingCopyRoot, "checkout", "--orphan", "rewritten"); err != nil {
		t.Fatalf("create unrelated history: %v", err)
	}
	if err := os.WriteFile(filepath.Join(prepared.WorkingCopyRoot, "rewritten.txt"), []byte("rewritten\n"), 0o644); err != nil {
		t.Fatalf("write rewritten source: %v", err)
	}
	if err := runGitCommand(ctx, gitPath, prepared.WorkingCopyRoot, "add", "-A"); err != nil {
		t.Fatalf("stage rewritten history: %v", err)
	}
	if err := runGitCommandWithEnv(ctx, gitPath, prepared.WorkingCopyRoot, []string{
		"GIT_AUTHOR_NAME=mspace",
		"GIT_AUTHOR_EMAIL=mspace@example.local",
		"GIT_COMMITTER_NAME=mspace",
		"GIT_COMMITTER_EMAIL=mspace@example.local",
	}, "commit", "-m", "rewrite history"); err != nil {
		t.Fatalf("commit rewritten history: %v", err)
	}
	if err := runGitCommand(ctx, gitPath, prepared.WorkingCopyRoot, "branch", "-M", payload.Branch); err != nil {
		t.Fatalf("restore assigned branch name: %v", err)
	}

	envelope := inspectIssueWorkingCopy(ctx, prepared, "")
	if envelope.ContentState != "recovery_required" || envelope.RecoveryReason != "head_mismatch" {
		t.Fatalf("rewritten history must fail closed: %+v", envelope)
	}
}

func issueWorkingCopyFixture(t *testing.T) (string, string, config, agentSessionPayload, *runtimeClient, func()) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is required")
	}
	sourceRepo := createTestGitRepo(t)
	workRoot := t.TempDir()
	storageID, err := ensureWorkerStorageID(workRoot)
	if err != nil {
		t.Fatalf("create worker storage id: %v", err)
	}
	cfg := config{WorkRoot: workRoot, StorageID: storageID}
	repository := repositorySpec{URL: sourceRepo, DefaultBranch: "main", SourceType: "local", Provider: "local", Owner: "test", Repo: "working-copy"}
	repoDir := filepath.Join(workRoot, "repos", repositoryCacheKey(repository))
	if err := ensureWorkerRepository(context.Background(), gitPath, sourceRepo, repoDir); err != nil {
		t.Fatalf("prepare worker repository cache: %v", err)
	}
	executionContext, _, closeServer := newAgentEngineTestContext(t)
	payload := agentSessionPayload{
		AgentEngine:           agentEngineCodex,
		Prompt:                "change source",
		IssueID:               "issue-1",
		SessionID:             "session-1",
		ProjectID:             "project-1",
		Branch:                "mspace/issue-1",
		ExecutionMode:         agentSessionExecutionIssueWorkingCopy,
		WorkingCopyGeneration: 3,
		Initialize:            true,
		Repository:            repository,
	}
	return gitPath, repoDir, cfg, payload, executionContext.RuntimeClient, closeServer
}
