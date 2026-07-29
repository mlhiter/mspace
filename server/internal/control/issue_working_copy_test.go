package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type issueWorkingCopyFixture struct {
	store        *MemoryStore
	user         User
	workspaceID  string
	project      Project
	issueID      string
	registration RuntimeRegistration
	worker       RuntimeWorker
}

func newIssueWorkingCopyFixture(t *testing.T) issueWorkingCopyFixture {
	t.Helper()
	ctx := context.Background()
	store := NewMemoryStore()
	user, workspaces, err := store.CreatePasswordIdentity(ctx, PasswordAuthInput{
		Login: "working-copy-owner", Password: "password-123456", Name: "Working Copy Owner",
	})
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	workspaceID := workspaces[0].ID
	project, err := store.CreateProject(ctx, user.ID, workspaceID, ProjectInput{
		Name: "working-copy-project", SourceType: "local", RepoPath: "/tmp/working-copy-project",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	issueID, err := store.CreateIssue(ctx, user, workspaceID, CreateIssueInput{
		ProjectID: project.ID, Title: "Reuse one issue workspace", Body: "Keep source history linear.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	token, err := store.CreateRuntimeRegistrationToken(ctx, user.ID, workspaceID, CreateRuntimeRegistrationTokenInput{
		Name: "working-copy-token", ExpiresInHours: 1,
	})
	if err != nil {
		t.Fatalf("create runtime token: %v", err)
	}
	registration, err := store.AuthenticateRuntimeRegistrationToken(ctx, token.Token)
	if err != nil {
		t.Fatalf("authenticate runtime token: %v", err)
	}
	worker, err := store.RegisterRuntimeWorker(ctx, registration, RuntimeWorkerInput{
		Name: "working-copy-worker", StorageID: "msws_working_copy_primary", Mode: "personal",
		Capabilities: json.RawMessage(`{"codex":true,"claudeCode":true,"issueWorkingCopyV1":true}`),
	})
	if err != nil {
		t.Fatalf("register runtime worker: %v", err)
	}
	return issueWorkingCopyFixture{store: store, user: user, workspaceID: workspaceID, project: project, issueID: issueID, registration: registration, worker: worker}
}

func (f issueWorkingCopyFixture) createSession(t *testing.T, engine string) AgentSession {
	t.Helper()
	session, err := f.store.CreateAgentSession(context.Background(), f.user.ID, f.workspaceID, f.issueID, CreateAgentSessionInput{
		AgentEngine: engine, RuntimeMode: "personal", Command: "Update the issue source.",
	})
	if err != nil {
		t.Fatalf("create agent session: %v", err)
	}
	return session
}

func workingCopyResult(storageID, branch, baseSHA, headSHA, state, reason string, generation int64) json.RawMessage {
	body, _ := json.Marshal(map[string]any{
		"agentEngine": "codex",
		"workingCopy": map[string]any{
			"storageId": storageID, "branch": branch, "baseCommitSha": baseSHA,
			"headCommitSha": headSHA, "contentState": state, "recoveryReason": reason,
			"generation": generation,
		},
	})
	return body
}

func TestIssueWorkingCopyReusesBranchAndStorageAcrossSessions(t *testing.T) {
	f := newIssueWorkingCopyFixture(t)
	ctx := context.Background()
	first := f.createSession(t, "codex")
	if first.Branch != defaultIssueWorkingCopyBranch(f.issueID) {
		t.Fatalf("first branch=%q", first.Branch)
	}
	if _, err := f.store.CreateAgentSession(ctx, f.user.ID, f.workspaceID, f.issueID, CreateAgentSessionInput{
		AgentEngine: "claude_code", RuntimeMode: "personal", Command: "Race the active writer.",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("concurrent source session should conflict, got %v", err)
	}
	claimed, err := f.store.ClaimRuntimeTask(ctx, f.registration, f.worker.ID)
	if err != nil || claimed == nil || claimed.ID != first.RuntimeTaskID {
		t.Fatalf("claim first session: task=%+v err=%v", claimed, err)
	}
	if claimed.StorageAffinityID != f.worker.StorageID {
		t.Fatalf("first claim should bind storage, got %+v", claimed)
	}
	firstTask, err := f.store.UpdateRuntimeTaskStatus(ctx, f.registration, f.worker.ID, claimed.ID, UpdateRuntimeTaskStatusInput{
		Status: "completed", Result: workingCopyResult(f.worker.StorageID, first.Branch, "base-1", "head-1", "clean", "", 0),
	})
	if err != nil || firstTask.Status != "completed" {
		t.Fatalf("complete first session: task=%+v err=%v", firstTask, err)
	}
	detail, err := f.store.GetIssue(ctx, f.user.ID, f.workspaceID, f.issueID)
	if err != nil || detail.WorkingCopy == nil {
		t.Fatalf("read issue working copy: detail=%+v err=%v", detail.WorkingCopy, err)
	}
	if detail.WorkingCopy.HeadCommitSHA != "head-1" || detail.WorkingCopy.Generation != 1 || detail.WorkingCopy.ActiveSessionID != "" {
		t.Fatalf("unexpected first working copy: %+v", detail.WorkingCopy)
	}

	second := f.createSession(t, "claude_code")
	if second.Branch != first.Branch {
		t.Fatalf("second session branch=%q want=%q", second.Branch, first.Branch)
	}
	secondTask := f.store.runtimeTasks[second.RuntimeTaskID]
	if secondTask.StorageAffinityID != f.worker.StorageID {
		t.Fatalf("second session must retain affinity: %+v", secondTask)
	}
	var payload issueWorkingCopyTaskPayload
	if err := json.Unmarshal(secondTask.Payload, &payload); err != nil {
		t.Fatalf("decode second payload: %v", err)
	}
	if payload.ExpectedHeadSHA != "head-1" || payload.WorkingCopyGeneration != 1 || payload.Initialize {
		t.Fatalf("unexpected second payload: %+v", payload)
	}

	otherWorker, err := f.store.RegisterRuntimeWorker(ctx, f.registration, RuntimeWorkerInput{
		Name: "other-storage-worker", StorageID: "msws_working_copy_other", Mode: "personal",
		Capabilities: json.RawMessage(`{"claudeCode":true,"issueWorkingCopyV1":true}`),
	})
	if err != nil {
		t.Fatalf("register other worker: %v", err)
	}
	if task, err := f.store.ClaimRuntimeTask(ctx, f.registration, otherWorker.ID); err != nil || task != nil {
		t.Fatalf("other storage must not claim task: task=%+v err=%v", task, err)
	}
	claimed, err = f.store.ClaimRuntimeTask(ctx, f.registration, f.worker.ID)
	if err != nil || claimed == nil || claimed.ID != second.RuntimeTaskID {
		t.Fatalf("owner storage should claim second task: task=%+v err=%v", claimed, err)
	}
}

func TestIssueWorkingCopyCancellationHandshakeAndDirtyContinuation(t *testing.T) {
	f := newIssueWorkingCopyFixture(t)
	ctx := context.Background()
	session := f.createSession(t, "codex")
	claimed, err := f.store.ClaimRuntimeTask(ctx, f.registration, f.worker.ID)
	if err != nil || claimed == nil {
		t.Fatalf("claim task: %+v %v", claimed, err)
	}
	cancelled, err := f.store.CancelRuntimeTask(ctx, f.user.ID, f.workspaceID, claimed.ID, CancelRuntimeTaskInput{Reason: "Stop now"})
	if err != nil {
		t.Fatalf("request cancellation: %v", err)
	}
	if cancelled.Status != "claimed" || !cancelled.CancelRequested || cancelled.CancelRequestedAt == "" {
		t.Fatalf("running cancellation must wait for worker: %+v", cancelled)
	}
	if f.store.issueWorkingCopies[issueWorkingCopyKey(f.workspaceID, f.issueID)].ActiveSessionID != session.ID {
		t.Fatal("cancellation request must retain writer reservation")
	}
	terminal, err := f.store.UpdateRuntimeTaskStatus(ctx, f.registration, f.worker.ID, claimed.ID, UpdateRuntimeTaskStatusInput{
		Status: "cancelled", Error: "cancelled", Result: workingCopyResult(f.worker.StorageID, session.Branch, "base-1", "base-1", "dirty", "", 0),
	})
	if err != nil || terminal.Status != "cancelled" {
		t.Fatalf("worker cancellation outcome: %+v %v", terminal, err)
	}
	workingCopy := f.store.issueWorkingCopies[issueWorkingCopyKey(f.workspaceID, f.issueID)]
	if workingCopy.ContentState != "dirty" || workingCopy.ActiveSessionID != "" || workingCopy.Generation != 1 {
		t.Fatalf("dirty cancellation must release writer: %+v", workingCopy)
	}
	if next := f.createSession(t, "codex"); next.Branch != session.Branch {
		t.Fatalf("dirty continuation branch=%q want=%q", next.Branch, session.Branch)
	}
}

func TestIssueWorkingCopyLateCompletionAfterCancelIsTerminalCancelled(t *testing.T) {
	f := newIssueWorkingCopyFixture(t)
	ctx := context.Background()
	session := f.createSession(t, "codex")
	claimed, err := f.store.ClaimRuntimeTask(ctx, f.registration, f.worker.ID)
	if err != nil || claimed == nil {
		t.Fatalf("claim task: %+v %v", claimed, err)
	}
	if _, err := f.store.CancelRuntimeTask(ctx, f.user.ID, f.workspaceID, claimed.ID, CancelRuntimeTaskInput{Reason: "user stopped the session"}); err != nil {
		t.Fatalf("request cancellation: %v", err)
	}
	result, err := json.Marshal(map[string]any{
		"agentEngine": "codex",
		"source":      map[string]any{"commitSha": "head-after-race", "branch": session.Branch, "subject": "late successful result"},
		"workingCopy": map[string]any{
			"storageId": f.worker.StorageID, "branch": session.Branch, "baseCommitSha": "base-1", "headCommitSha": "head-after-race",
			"contentState": workingCopyStateClean, "recoveryReason": "", "generation": 0,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := f.store.UpdateRuntimeTaskStatus(ctx, f.registration, f.worker.ID, claimed.ID, UpdateRuntimeTaskStatusInput{
		Status: "completed", Result: result,
	})
	if err != nil {
		t.Fatalf("late completion should reconcile as cancellation: %v", err)
	}
	if terminal.Status != "cancelled" || terminal.FinishedAt == "" || terminal.Error != "user stopped the session" {
		t.Fatalf("unexpected coerced cancellation: %+v", terminal)
	}
	workingCopy := f.store.issueWorkingCopies[issueWorkingCopyKey(f.workspaceID, f.issueID)]
	if workingCopy.HeadCommitSHA != "head-after-race" || workingCopy.ContentState != workingCopyStateClean || workingCopy.ActiveSessionID != "" {
		t.Fatalf("trusted physical working-copy state was not reconciled: %+v", workingCopy)
	}
	if strings.Contains(string(terminal.Result), `"source"`) || runtimeTaskSource(terminal).CommitSHA != "" {
		t.Fatalf("cancelled completion must suppress successful source side effects: %s", terminal.Result)
	}
	if len(f.store.sessionFailures) != 1 {
		t.Fatalf("cancelled completion must retain a session failure record: %+v", f.store.sessionFailures)
	}
}

func TestQueuedIssueWorkingCopyCancellationReleasesReservation(t *testing.T) {
	f := newIssueWorkingCopyFixture(t)
	ctx := context.Background()
	session := f.createSession(t, "codex")
	task, err := f.store.CancelRuntimeTask(ctx, f.user.ID, f.workspaceID, session.RuntimeTaskID, CancelRuntimeTaskInput{})
	if err != nil || task.Status != "cancelled" {
		t.Fatalf("cancel queued task: %+v %v", task, err)
	}
	workingCopy := f.store.issueWorkingCopies[issueWorkingCopyKey(f.workspaceID, f.issueID)]
	if workingCopy.ActiveSessionID != "" || workingCopy.Generation != 1 || workingCopy.ContentState != "uninitialized" {
		t.Fatalf("queued cancellation should only release reservation: %+v", workingCopy)
	}
	_ = f.createSession(t, "codex")
}

func TestIssueWorkingCopyMismatchFailsClosedAndStaleResultCannotOverwrite(t *testing.T) {
	f := newIssueWorkingCopyFixture(t)
	ctx := context.Background()
	f.createSession(t, "codex")
	claimed, err := f.store.ClaimRuntimeTask(ctx, f.registration, f.worker.ID)
	if err != nil || claimed == nil {
		t.Fatalf("claim task: %+v %v", claimed, err)
	}
	if _, err := f.store.UpdateRuntimeTaskStatus(ctx, f.registration, f.worker.ID, claimed.ID, UpdateRuntimeTaskStatusInput{
		Status: "completed", Result: workingCopyResult(f.worker.StorageID, "mspace/wrong-branch", "base", "head", "clean", "", 0),
	}); err != nil {
		t.Fatalf("submit mismatched result: %v", err)
	}
	workingCopy := f.store.issueWorkingCopies[issueWorkingCopyKey(f.workspaceID, f.issueID)]
	if workingCopy.ContentState != workingCopyStateRecoveryRequired || workingCopy.RecoveryReason != "branch_mismatch" || workingCopy.HeadCommitSHA != "" {
		t.Fatalf("branch mismatch must fail closed: %+v", workingCopy)
	}
	if _, err := f.store.CreateAgentSession(ctx, f.user.ID, f.workspaceID, f.issueID, CreateAgentSessionInput{
		AgentEngine: "codex", Command: "Do not overwrite recovery state.",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("recovery-required working copy should reject writers, got %v", err)
	}

	second := newIssueWorkingCopyFixture(t)
	staleSession := second.createSession(t, "codex")
	staleTask, err := second.store.ClaimRuntimeTask(ctx, second.registration, second.worker.ID)
	if err != nil || staleTask == nil {
		t.Fatalf("claim stale task: %+v %v", staleTask, err)
	}
	key := issueWorkingCopyKey(second.workspaceID, second.issueID)
	mutated := second.store.issueWorkingCopies[key]
	mutated.Generation = 1
	mutated.ActiveSessionID = "newer-session"
	second.store.issueWorkingCopies[key] = mutated
	if _, err := second.store.UpdateRuntimeTaskStatus(ctx, second.registration, second.worker.ID, staleTask.ID, UpdateRuntimeTaskStatusInput{
		Status: "completed", Result: workingCopyResult(second.worker.StorageID, staleSession.Branch, "base", "stale-head", "clean", "", 0),
	}); err != nil {
		t.Fatalf("submit stale result: %v", err)
	}
	mutated = second.store.issueWorkingCopies[key]
	if mutated.HeadCommitSHA != "" || mutated.ActiveSessionID != "newer-session" || mutated.Generation != 1 {
		t.Fatalf("stale result changed canonical state: %+v", mutated)
	}
}

func TestIssueWorkingCopyCurrentGenerationMismatchRequiresRecoveryAndReleasesWriter(t *testing.T) {
	f := newIssueWorkingCopyFixture(t)
	ctx := context.Background()
	session := f.createSession(t, "codex")
	claimed, err := f.store.ClaimRuntimeTask(ctx, f.registration, f.worker.ID)
	if err != nil || claimed == nil {
		t.Fatalf("claim task: %+v %v", claimed, err)
	}
	key := issueWorkingCopyKey(f.workspaceID, f.issueID)
	workingCopy := f.store.issueWorkingCopies[key]
	workingCopy.Generation = 1
	f.store.issueWorkingCopies[key] = workingCopy
	if _, err := f.store.UpdateRuntimeTaskStatus(ctx, f.registration, f.worker.ID, claimed.ID, UpdateRuntimeTaskStatusInput{
		Status: "failed", Error: "worker state was inconsistent", Result: workingCopyResult(f.worker.StorageID, session.Branch, "base", "head", "clean", "", 0),
	}); err != nil {
		t.Fatalf("submit mismatched current result: %v", err)
	}
	workingCopy = f.store.issueWorkingCopies[key]
	if workingCopy.ContentState != workingCopyStateRecoveryRequired || workingCopy.RecoveryReason != "metadata_missing" || workingCopy.ActiveSessionID != "" || workingCopy.Generation != 2 {
		t.Fatalf("current generation mismatch must release into recovery: %+v", workingCopy)
	}
}

func TestIssueWorkingCopyRecoveryPreservesCanonicalSourceAndStoresEvidence(t *testing.T) {
	f := newIssueWorkingCopyFixture(t)
	ctx := context.Background()
	first := f.createSession(t, "codex")
	claimed, err := f.store.ClaimRuntimeTask(ctx, f.registration, f.worker.ID)
	if err != nil || claimed == nil {
		t.Fatalf("claim initial task: %+v %v", claimed, err)
	}
	if _, err := f.store.UpdateRuntimeTaskStatus(ctx, f.registration, f.worker.ID, claimed.ID, UpdateRuntimeTaskStatusInput{
		Status: "completed", Result: workingCopyResult(f.worker.StorageID, first.Branch, "base-1", "head-1", "clean", "", 0),
	}); err != nil {
		t.Fatalf("complete initial task: %v", err)
	}

	second := f.createSession(t, "codex")
	claimed, err = f.store.ClaimRuntimeTask(ctx, f.registration, f.worker.ID)
	if err != nil || claimed == nil {
		t.Fatalf("claim recovery task: %+v %v", claimed, err)
	}
	result, err := json.Marshal(map[string]any{
		"agentEngine": "codex",
		"source":      map[string]any{"commitSha": "untrusted-head", "branch": second.Branch},
		"workingCopy": map[string]any{
			"storageId": f.worker.StorageID, "branch": second.Branch, "baseCommitSha": "", "headCommitSha": "",
			"contentState": workingCopyStateRecoveryRequired, "recoveryReason": "worktree_missing", "generation": 1,
		},
		"reviewEvidence": map[string]any{"agentSummary": "The worker could not recover the source checkout."},
	})
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := f.store.UpdateRuntimeTaskStatus(ctx, f.registration, f.worker.ID, claimed.ID, UpdateRuntimeTaskStatusInput{
		Status: "failed", Error: "worktree disappeared", Result: result,
	})
	if err != nil {
		t.Fatalf("submit recovery result: %v", err)
	}
	workingCopy := f.store.issueWorkingCopies[issueWorkingCopyKey(f.workspaceID, f.issueID)]
	if workingCopy.BaseCommitSHA != "base-1" || workingCopy.HeadCommitSHA != "head-1" || workingCopy.StorageID != f.worker.StorageID {
		t.Fatalf("recovery envelope overwrote canonical source: %+v", workingCopy)
	}
	if workingCopy.ContentState != workingCopyStateRecoveryRequired || workingCopy.RecoveryReason != "worktree_missing" || workingCopy.ActiveSessionID != "" {
		t.Fatalf("unexpected recovery state: %+v", workingCopy)
	}
	if len(f.store.reviewEvidence) != 1 || len(f.store.sessionFailures) != 1 {
		t.Fatalf("terminal recovery must retain evidence and failure: reviews=%+v failures=%+v", f.store.reviewEvidence, f.store.sessionFailures)
	}
	if strings.Contains(string(terminal.Result), `"source"`) || runtimeTaskSource(terminal).CommitSHA != "" {
		t.Fatalf("unreconciled source must not be exposed: %s", terminal.Result)
	}
}

func TestIssueWorkingCopyReclaimsStaleClaimOnSameStorage(t *testing.T) {
	for _, staleStatus := range []string{"claimed", "running"} {
		t.Run(staleStatus, func(t *testing.T) {
			f := newIssueWorkingCopyFixture(t)
			ctx := context.Background()
			session := f.createSession(t, "codex")
			claimed, err := f.store.ClaimRuntimeTask(ctx, f.registration, f.worker.ID)
			if err != nil || claimed == nil {
				t.Fatalf("claim task: %+v %v", claimed, err)
			}
			if staleStatus == "running" {
				if _, err := f.store.UpdateRuntimeTaskStatus(ctx, f.registration, f.worker.ID, claimed.ID, UpdateRuntimeTaskStatusInput{Status: "running"}); err != nil {
					t.Fatalf("start task: %v", err)
				}
			}
			task := f.store.runtimeTasks[claimed.ID]
			task.UpdatedAt = time.Now().UTC().Add(-staleRunningTaskReclaimAge - time.Minute).Format(time.RFC3339Nano)
			f.store.runtimeTasks[task.ID] = task

			wrongStorage, err := f.store.RegisterRuntimeWorker(ctx, f.registration, RuntimeWorkerInput{
				Name: "wrong-storage-recovery", StorageID: "msws_wrong_recovery_storage", Mode: "personal",
				Capabilities: json.RawMessage(`{"codex":true,"issueWorkingCopyV1":true}`),
			})
			if err != nil {
				t.Fatal(err)
			}
			if got, err := f.store.ClaimRuntimeTask(ctx, f.registration, wrongStorage.ID); err != nil || got != nil {
				t.Fatalf("wrong storage reclaimed working copy: task=%+v err=%v", got, err)
			}
			replacement, err := f.store.RegisterRuntimeWorker(ctx, f.registration, RuntimeWorkerInput{
				Name: "same-storage-recovery", StorageID: f.worker.StorageID, Mode: "personal",
				Capabilities: json.RawMessage(`{"codex":true,"issueWorkingCopyV1":true}`),
			})
			if err != nil {
				t.Fatal(err)
			}
			reclaimed, err := f.store.ClaimRuntimeTask(ctx, f.registration, replacement.ID)
			if err != nil || reclaimed == nil || reclaimed.ID != session.RuntimeTaskID || reclaimed.Status != "claimed" || reclaimed.ClaimedByWorkerID != replacement.ID {
				t.Fatalf("same storage failed to reclaim stale %s task: task=%+v err=%v", staleStatus, reclaimed, err)
			}
		})
	}
}

func TestIssueWorkingCopyPreventsProjectReassignment(t *testing.T) {
	f := newIssueWorkingCopyFixture(t)
	ctx := context.Background()
	f.createSession(t, "codex")
	other, err := f.store.CreateProject(ctx, f.user.ID, f.workspaceID, ProjectInput{
		Name: "other-project", SourceType: "local", RepoPath: "/tmp/working-copy-other-project",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateIssue(ctx, f.user.ID, f.workspaceID, f.issueID, UpdateIssueInput{ProjectID: &other.ID}); !errors.Is(err, ErrConflict) {
		t.Fatalf("project reassignment should conflict, got %v", err)
	}
	emptyProjectID := ""
	if _, err := f.store.UpdateIssue(ctx, f.user.ID, f.workspaceID, f.issueID, UpdateIssueInput{ProjectID: &emptyProjectID}); !errors.Is(err, ErrConflict) {
		t.Fatalf("project removal should conflict, got %v", err)
	}
	if updated, err := f.store.UpdateIssue(ctx, f.user.ID, f.workspaceID, f.issueID, UpdateIssueInput{ProjectID: &f.project.ID}); err != nil || updated.ProjectID != f.project.ID {
		t.Fatalf("same project update should remain allowed: issue=%+v err=%v", updated, err)
	}
}

func TestLegacyWorkerClaimsDetachedButNotIssueWorkingCopyTask(t *testing.T) {
	f := newIssueWorkingCopyFixture(t)
	ctx := context.Background()
	source := f.createSession(t, "codex")
	legacy, err := f.store.RegisterRuntimeWorker(ctx, f.registration, RuntimeWorkerInput{
		Name: "legacy-worker", Mode: "personal", Capabilities: json.RawMessage(`{"codex":true}`),
	})
	if err != nil {
		t.Fatalf("register legacy worker: %v", err)
	}
	detached, err := f.store.CreateAgentSession(ctx, f.user.ID, f.workspaceID, f.issueID, CreateAgentSessionInput{
		AgentEngine: "codex", Command: "Clean test resources.", Automation: testEnvironmentCleanupAutomation,
	})
	if err != nil {
		t.Fatalf("create detached session: %v", err)
	}
	claimed, err := f.store.ClaimRuntimeTask(ctx, f.registration, legacy.ID)
	if err != nil || claimed == nil || claimed.ID != detached.RuntimeTaskID {
		t.Fatalf("legacy worker should claim detached task only: task=%+v err=%v", claimed, err)
	}
	workingCopy := f.store.issueWorkingCopies[issueWorkingCopyKey(f.workspaceID, f.issueID)]
	if workingCopy.ActiveSessionID != source.ID || workingCopy.StorageID != "" {
		t.Fatalf("detached claim must not mutate source working copy: %+v", workingCopy)
	}
}

func TestRuntimeAvailabilityRequiresV1AndHonorsWorkingCopyState(t *testing.T) {
	now := time.Now().UTC()
	legacy := RuntimeWorker{
		ID: "legacy", WorkspaceID: "workspace", Mode: "personal", Status: "online",
		Capabilities: json.RawMessage(`{"codex":true}`), LastSeenAt: now.Format(time.RFC3339Nano),
	}
	required, err := addIssueWorkingCopyCapability(json.RawMessage(`{"codex":true}`))
	if err != nil {
		t.Fatal(err)
	}
	availability := evaluateRuntimeAvailabilityForWorkingCopy("workspace", "personal", "personal", required, []RuntimeWorker{legacy}, nil, now)
	if availability.State == "ready" || availability.ClaimableWorkerCount != 0 {
		t.Fatalf("legacy worker must not satisfy source preflight: %+v", availability)
	}
	detached := evaluateRuntimeAvailability("workspace", "personal", "personal", json.RawMessage(`{"codex":true}`), []RuntimeWorker{legacy}, now)
	if detached.State != "ready" {
		t.Fatalf("legacy worker should remain valid for detached tasks: %+v", detached)
	}
	v1 := legacy
	v1.ID = "v1"
	v1.StorageID = "msws_working_copy_primary"
	v1.Capabilities = json.RawMessage(`{"codex":true,"issueWorkingCopyV1":true}`)
	busy := evaluateRuntimeAvailabilityForWorkingCopy("workspace", "personal", "personal", required, []RuntimeWorker{v1}, &IssueWorkingCopy{ActiveSessionID: "session"}, now)
	if busy.ReasonCode != "working_copy_busy" || busy.CanQueue || busy.ClaimableWorkerCount != 0 {
		t.Fatalf("busy working copy availability: %+v", busy)
	}
	unavailable := evaluateRuntimeAvailabilityForWorkingCopy("workspace", "personal", "personal", required, []RuntimeWorker{v1}, &IssueWorkingCopy{StorageID: "msws_other_storage", ContentState: "clean"}, now)
	if unavailable.ReasonCode != "working_copy_storage_unavailable" || unavailable.ClaimableWorkerCount != 0 {
		t.Fatalf("storage availability: %+v", unavailable)
	}
}

func TestIssueWorkingCopyMigrationContract(t *testing.T) {
	migration, err := migrationFS.ReadFile("migrations/032_issue_working_copies.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	body := string(migration)
	for _, required := range []string{"CREATE TABLE IF NOT EXISTS issue_working_copies", "storage_affinity_id", "cancel_requested_at", "recovery_required"} {
		if !strings.Contains(body, required) {
			t.Fatalf("migration missing %q", required)
		}
	}
}

func TestPostgresCancellationCompletionIsAtomicallyCoerced(t *testing.T) {
	body, err := os.ReadFile("postgres_store.go")
	if err != nil {
		t.Fatalf("read postgres store: %v", err)
	}
	source := string(body)
	for _, required := range []string{
		"WHEN cancel_requested_at IS NOT NULL AND $1 = 'completed' THEN 'cancelled'",
		"AND (cancel_requested_at IS NULL OR $1 IN ('completed', 'cancelled'))",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("postgres cancellation race contract missing %q", required)
		}
	}
}

func TestTestEnvironmentCleanupIsDetached(t *testing.T) {
	input := CreateAgentSessionInput{AgentEngine: agentEngineCodex, Automation: testEnvironmentCleanupAutomation}
	if got := agentSessionExecutionMode(input); got != agentSessionExecutionModeDetached {
		t.Fatalf("cleanup execution mode=%q", got)
	}
	required, err := agentSessionRequiredCapabilities(input)
	if err != nil {
		t.Fatal(err)
	}
	if jsonObjectContains(required, json.RawMessage(`{"issueWorkingCopyV1":true}`)) {
		t.Fatalf("cleanup must not reserve an Issue working copy: %s", required)
	}
}

func TestSQLitePersistsIssueWorkingCopyProtocolState(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "working-copy.db")
	store, err := NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	store.issueWorkingCopies["workspace:issue"] = IssueWorkingCopy{
		IssueID: "issue", ProjectID: "project", Branch: "mspace/issue", BaseCommitSHA: "base",
		HeadCommitSHA: "head", StorageID: "msws_sqlite_storage", ContentState: "clean", Generation: 3,
	}
	store.runtimeWorkers["workspace:worker"] = RuntimeWorker{ID: "worker", WorkspaceID: "workspace", StorageID: "msws_sqlite_storage"}
	store.runtimeTasks["task"] = RuntimeTask{ID: "task", WorkspaceID: "workspace", StorageAffinityID: "msws_sqlite_storage", CancelRequestedAt: "2026-07-29T00:00:00Z", CancelRequested: true}
	if err := store.Persist(); err != nil {
		t.Fatalf("persist sqlite store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}
	reopened, err := NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer reopened.Close()
	if got := reopened.issueWorkingCopies["workspace:issue"]; got.HeadCommitSHA != "head" || got.Generation != 3 {
		t.Fatalf("unexpected persisted working copy: %+v", got)
	}
	if got := reopened.runtimeWorkers["workspace:worker"].StorageID; got != "msws_sqlite_storage" {
		t.Fatalf("persisted worker storageId=%q", got)
	}
	if got := reopened.runtimeTasks["task"]; got.StorageAffinityID != "msws_sqlite_storage" || !got.CancelRequested {
		t.Fatalf("unexpected persisted runtime task: %+v", got)
	}
}
