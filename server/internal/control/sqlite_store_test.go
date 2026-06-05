package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSQLiteStorePersistsSnapshot(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/mspace.db"

	store, err := NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	auth, workspaces, err := store.CreatePasswordIdentity(ctx, PasswordAuthInput{
		Login:    "local-user",
		Password: "password-123456",
		Name:     "Local User",
	})
	if err != nil {
		t.Fatalf("create local identity: %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("expected default workspace, got %d", len(workspaces))
	}
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

	loadedAuth, loadedWorkspaces, err := reopened.AuthenticatePassword(ctx, PasswordAuthInput{
		Login:    "local-user",
		Password: "password-123456",
	})
	if err != nil {
		t.Fatalf("authenticate persisted identity: %v", err)
	}
	if loadedAuth.ID != auth.ID {
		t.Fatalf("expected user %q, got %q", auth.ID, loadedAuth.ID)
	}
	if len(loadedWorkspaces) != 1 || loadedWorkspaces[0].ID != workspaces[0].ID {
		t.Fatalf("expected persisted workspace %+v, got %+v", workspaces, loadedWorkspaces)
	}
}

func TestSQLiteStorePersistsVirtualMachineSSHAuth(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/mspace.db"

	store, err := NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	user, workspaces, err := store.CreatePasswordIdentity(ctx, PasswordAuthInput{
		Login:    "vm-user",
		Password: "password-123456",
		Name:     "VM User",
	})
	if err != nil {
		t.Fatalf("create local identity: %v", err)
	}
	workspaceID := workspaces[0].ID
	environment, err := store.CreateEnvironment(ctx, user.ID, workspaceID, EnvironmentInput{
		Name:       "persisted-vm",
		Kind:       environmentKindVirtualMachine,
		SSHHost:    "127.0.0.1",
		SSHPort:    1,
		SSHUser:    "ubuntu",
		SSHAuthRef: "secret://mspace/persisted-vm",
		SSHAuth:    &VirtualMachineSSHAuthInput{Method: "password", Password: "persisted-password"},
	})
	if err != nil {
		t.Fatalf("create vm environment: %v", err)
	}
	if environment.VirtualMachine == nil || !environment.VirtualMachine.SSHAuthConfigured {
		t.Fatalf("expected vm auth configured before persist, got %+v", environment)
	}
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

	checked, err := reopened.CheckEnvironment(ctx, user.ID, workspaceID, environment.ID, EnvironmentCheckInput{})
	if err != nil {
		t.Fatalf("check persisted vm without sshAuth: %v", err)
	}
	if checked.VirtualMachine == nil || !checked.VirtualMachine.SSHAuthConfigured {
		t.Fatalf("expected persisted vm auth to remain configured, got %+v", checked)
	}
	if checked.Status != "unreachable" || checked.LastCheckedAt == "" {
		t.Fatalf("expected persisted vm check to refresh status, got %+v", checked)
	}
}

func TestSQLiteStorePersistsProjectTestCases(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/mspace.db"

	store, err := NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	user, workspaces, err := store.CreatePasswordIdentity(ctx, PasswordAuthInput{
		Login:    "case-user",
		Password: "password-123456",
		Name:     "Case User",
	})
	if err != nil {
		t.Fatalf("create local identity: %v", err)
	}
	project, err := store.CreateProject(ctx, user.ID, workspaces[0].ID, ProjectInput{
		Name:       "mspace",
		SourceType: "local",
		RepoPath:   "/tmp/mspace",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	testCase, err := store.CreateProjectTestCase(ctx, user.ID, workspaces[0].ID, project.ID, TestCaseInput{
		Title:                   "Persisted test case",
		Preconditions:           "The server is running.",
		Steps:                   []TestCaseStep{{Action: "Open the workspace"}},
		ExpectedResult:          "The workspace opens.",
		EnvironmentRequirements: "SQLite personal store.",
	})
	if err != nil {
		t.Fatalf("create test case: %v", err)
	}
	if _, err := store.UpdateProjectTestCase(ctx, user.ID, workspaces[0].ID, project.ID, testCase.ID, TestCaseInput{
		Title:                   "Persisted test case after edit",
		Preconditions:           "The server is running.",
		Steps:                   []TestCaseStep{{Action: "Open the workspace"}},
		ExpectedResult:          "The workspace opens.",
		EnvironmentRequirements: "SQLite personal store.",
	}); err != nil {
		t.Fatalf("update test case: %v", err)
	}
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

	loaded, err := reopened.GetProjectTestCase(ctx, user.ID, workspaces[0].ID, project.ID, testCase.ID)
	if err != nil {
		t.Fatalf("load persisted test case: %v", err)
	}
	if loaded.Title != "Persisted test case after edit" {
		t.Fatalf("unexpected persisted test case: %+v", loaded)
	}
	revisions, err := reopened.ListProjectTestCaseRevisions(ctx, user.ID, workspaces[0].ID, project.ID, testCase.ID)
	if err != nil {
		t.Fatalf("load persisted revisions: %v", err)
	}
	if len(revisions) != 2 {
		t.Fatalf("expected persisted revisions, got %+v", revisions)
	}
}

func TestSQLiteStorePersistsTestModuleWorkflowSnapshot(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/mspace.db"

	store, err := NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	user, workspaces, err := store.CreatePasswordIdentity(ctx, PasswordAuthInput{
		Login:    "workflow-user",
		Password: "password-123456",
		Name:     "Workflow User",
	})
	if err != nil {
		t.Fatalf("create local identity: %v", err)
	}
	workspaceID := workspaces[0].ID
	project, err := store.CreateProject(ctx, user.ID, workspaceID, ProjectInput{
		Name:       "workflow",
		SourceType: "local",
		RepoPath:   "/tmp/workflow",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	sourceCase, err := store.CreateProjectTestCase(ctx, user.ID, workspaceID, project.ID, TestCaseInput{
		Title:                   "Password login opens workspace",
		Status:                  "ready",
		Preconditions:           "A local account exists.",
		Steps:                   []TestCaseStep{{Action: "Submit valid credentials", Expected: "The workspace opens."}},
		ExpectedResult:          "The selected workspace opens.",
		EnvironmentRequirements: "Personal desktop server is running.",
	})
	if err != nil {
		t.Fatalf("create source test case: %v", err)
	}
	issueID, err := store.CreateIssue(ctx, user, workspaceID, CreateIssueInput{
		ProjectID: project.ID,
		Title:     "Generate persisted test proposals",
		Body:      "Generate proposal artifacts.",
	})
	if err != nil {
		t.Fatalf("create proposal issue: %v", err)
	}
	task, err := store.CreateRuntimeTask(ctx, user.ID, workspaceID, CreateRuntimeTaskInput{
		IssueID:              issueID,
		ProjectID:            project.ID,
		Kind:                 "agent_session",
		RuntimeMode:          "personal",
		RequiredCapabilities: json.RawMessage(`{"codex":true}`),
		Payload:              json.RawMessage(`{"sessionId":"sqlite-proposal-session"}`),
	})
	if err != nil {
		t.Fatalf("create proposal task: %v", err)
	}
	tokenResult, err := store.CreateRuntimeRegistrationToken(ctx, user.ID, workspaceID, CreateRuntimeRegistrationTokenInput{
		Name:           "sqlite worker",
		ExpiresInHours: 1,
	})
	if err != nil {
		t.Fatalf("create runtime token: %v", err)
	}
	registration, err := store.AuthenticateRuntimeRegistrationToken(ctx, tokenResult.Token)
	if err != nil {
		t.Fatalf("authenticate runtime token: %v", err)
	}
	worker, err := store.RegisterRuntimeWorker(ctx, registration, RuntimeWorkerInput{
		Name:         "sqlite-worker",
		Mode:         "personal",
		Version:      "0.1.0",
		Capabilities: json.RawMessage(`{"codex":true}`),
	})
	if err != nil {
		t.Fatalf("register runtime worker: %v", err)
	}
	claimed, err := store.ClaimRuntimeTask(ctx, registration, worker.ID)
	if err != nil {
		t.Fatalf("claim proposal task: %v", err)
	}
	if claimed == nil || claimed.ID != task.ID {
		t.Fatalf("expected claimed proposal task %s, got %+v", task.ID, claimed)
	}
	if _, err := store.UpdateRuntimeTaskStatus(ctx, registration, worker.ID, task.ID, UpdateRuntimeTaskStatusInput{
		Status: "completed",
		Result: json.RawMessage(`{"exitCode":0,"testCaseProposals":{"proposals":[
			{"type":"create","title":"Persist generated logout case","proposedCase":{"title":"Logout returns to sign-in","status":"ready","preconditions":"A signed-in user is in the workspace.","steps":[{"action":"Click logout","expected":"The sign-in form appears."}],"expectedResult":"The signed-out user sees the sign-in form.","environmentRequirements":"Personal desktop server is running."}},
			{"type":"update","caseId":"missing-case","title":"Invalid missing target","proposedCase":{"title":"Missing target","steps":[{"action":"Try update"}],"expectedResult":"It fails validation."}}
		]}}`),
	}); err != nil {
		t.Fatalf("complete proposal task: %v", err)
	}
	proposals, err := store.ListProjectTestCaseProposals(ctx, user.ID, workspaceID, project.ID, TestCaseProposalListOptions{})
	if err != nil {
		t.Fatalf("list proposals: %v", err)
	}
	if len(proposals) != 2 {
		t.Fatalf("expected two proposals, got %+v", proposals)
	}
	var appliedCaseID string
	for _, proposal := range proposals {
		switch proposal.Status {
		case "pending":
			applied, err := store.ApplyProjectTestCaseProposal(ctx, user.ID, workspaceID, project.ID, proposal.ID, ReviewTestCaseProposalInput{Note: "accept persisted proposal"})
			if err != nil {
				t.Fatalf("apply persisted proposal: %v", err)
			}
			if applied.TestCase == nil {
				t.Fatalf("expected applied test case, got %+v", applied)
			}
			appliedCaseID = applied.TestCase.ID
		case "invalid":
			if _, err := store.ApplyProjectTestCaseProposal(ctx, user.ID, workspaceID, project.ID, proposal.ID, ReviewTestCaseProposalInput{}); err == nil {
				t.Fatalf("expected invalid proposal apply to fail")
			}
		default:
			t.Fatalf("unexpected proposal status before review: %+v", proposal)
		}
	}
	if appliedCaseID == "" {
		t.Fatalf("expected one applied proposal, got %+v", proposals)
	}
	plan, err := store.CreateProjectTestPlan(ctx, user.ID, workspaceID, project.ID, TestPlanInput{
		Title:       "persisted rc plan",
		Status:      "ready",
		TargetType:  "branch",
		TargetValue: "release/rc",
		Environment: "sqlite personal",
		CaseIDs:     []string{sourceCase.ID, appliedCaseID},
	})
	if err != nil {
		t.Fatalf("create test plan: %v", err)
	}
	run, err := store.StartProjectTestRun(ctx, user, workspaceID, project.ID, plan.Plan.ID, CreateTestRunInput{
		RuntimeMode:  "personal",
		AgentProfile: "codex",
		BatchSize:    2,
	})
	if err != nil {
		t.Fatalf("start test run: %v", err)
	}
	if len(run.Items) != 2 || run.Run.ParentIssueID == "" {
		t.Fatalf("unexpected started run: %+v", run)
	}
	adHocRun, err := store.StartAdHocProjectTestRun(ctx, user, workspaceID, project.ID, CreateAdHocTestRunInput{
		CaseIDs:      []string{sourceCase.ID},
		RuntimeMode:  "personal",
		AgentProfile: "codex",
		BatchSize:    1,
	})
	if err != nil {
		t.Fatalf("start direct test run: %v", err)
	}
	if adHocRun.Run.PlanID != "" || adHocRun.Run.Source != "ad_hoc" || adHocRun.Plan != nil || len(adHocRun.Items) != 1 {
		t.Fatalf("unexpected direct started run: %+v", adHocRun)
	}
	resultTask, err := store.ClaimRuntimeTask(ctx, registration, worker.ID)
	if err != nil {
		t.Fatalf("claim test run task: %v", err)
	}
	if resultTask == nil || resultTask.SessionID == "" {
		t.Fatalf("expected test run task with session id, got %+v", resultTask)
	}
	const screenshotDataURL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII="
	if _, err := store.UpdateRuntimeTaskStatus(ctx, registration, worker.ID, resultTask.ID, UpdateRuntimeTaskStatusInput{
		Status: "completed",
		Result: json.RawMessage(`{"exitCode":0,"testResult":{"runId":"` + run.Run.ID + `","items":[{"caseId":"` + sourceCase.ID + `","status":"passed","actualResult":"Workspace opened from persisted run.","evidence":{"screenshotImages":[{"path":"homepage.png","dataUrl":"` + screenshotDataURL + `"}],"assertions":[{"name":"workspace visible","passed":true}]}}]}}`),
	}); err != nil {
		t.Fatalf("complete test run task: %v", err)
	}
	artifactRefs, err := store.ListProjectTestRunArtifacts(ctx, user.ID, workspaceID, project.ID, run.Run.ID)
	if err != nil {
		t.Fatalf("list test artifacts: %v", err)
	}
	if len(artifactRefs) != 1 || artifactRefs[0].Kind != "screenshot" || artifactRefs[0].ContentType != "image/png" || len(artifactRefs[0].Content) == 0 {
		t.Fatalf("expected one persisted screenshot artifact, got %+v", artifactRefs)
	}
	reconciledRun, err := store.GetProjectTestRun(ctx, user.ID, workspaceID, project.ID, run.Run.ID)
	if err != nil {
		t.Fatalf("load reconciled run: %v", err)
	}
	var reconciledItem TestRunItem
	for _, item := range reconciledRun.Items {
		if item.TestCaseID == sourceCase.ID {
			reconciledItem = item
			break
		}
	}
	if reconciledItem.Status != "passed" {
		t.Fatalf("expected source case to pass, got %+v", reconciledItem)
	}
	evidence := string(reconciledItem.Evidence)
	if strings.Contains(evidence, "data:image") || strings.Contains(evidence, "base64") {
		t.Fatalf("expected embedded image payload to be stripped, got %s", evidence)
	}
	if !strings.Contains(evidence, `"/api/test-artifacts/`+artifactRefs[0].ID+`"`) {
		t.Fatalf("expected evidence to reference persisted artifact, got %s", evidence)
	}
	caseRuns, err := store.ListProjectTestCaseRunItems(ctx, user.ID, workspaceID, project.ID, sourceCase.ID)
	if err != nil {
		t.Fatalf("list case run history: %v", err)
	}
	if len(caseRuns) < 2 || caseRuns[0].Item.Status != "passed" || caseRuns[0].Run.ID != run.Run.ID {
		t.Fatalf("expected latest case run history entry to be passed run item, got %+v", caseRuns)
	}

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

	loadedProposals, err := reopened.ListProjectTestCaseProposals(ctx, user.ID, workspaceID, project.ID, TestCaseProposalListOptions{})
	if err != nil {
		t.Fatalf("load persisted proposals: %v", err)
	}
	if len(loadedProposals) != 2 {
		t.Fatalf("expected persisted proposals, got %+v", loadedProposals)
	}
	appliedCount := 0
	invalidCount := 0
	for _, proposal := range loadedProposals {
		if proposal.Status == "applied" {
			appliedCount++
		}
		if proposal.Status == "invalid" {
			invalidCount++
		}
	}
	if appliedCount != 1 || invalidCount != 1 {
		t.Fatalf("expected applied and invalid proposals to persist, got %+v", loadedProposals)
	}
	loadedPlan, err := reopened.GetProjectTestPlan(ctx, user.ID, workspaceID, project.ID, plan.Plan.ID)
	if err != nil {
		t.Fatalf("load persisted plan: %v", err)
	}
	if loadedPlan.Plan.CaseCount != 2 || len(loadedPlan.Cases) != 2 {
		t.Fatalf("unexpected persisted plan: %+v", loadedPlan)
	}
	loadedRun, err := reopened.GetProjectTestRun(ctx, user.ID, workspaceID, project.ID, run.Run.ID)
	if err != nil {
		t.Fatalf("load persisted run: %v", err)
	}
	if loadedRun.Run.Status != "running" || loadedRun.Run.ParentIssueID == "" || len(loadedRun.Items) != 2 {
		t.Fatalf("unexpected persisted run: %+v", loadedRun)
	}
	var loadedPassedItem TestRunItem
	for _, item := range loadedRun.Items {
		if item.ExecutionIssueID == "" || item.AgentSessionID == "" {
			t.Fatalf("expected persisted run item with issue/session, got %+v", item)
		}
		if item.TestCaseID == sourceCase.ID {
			loadedPassedItem = item
		} else if item.Status != "running" {
			t.Fatalf("expected untouched persisted item to remain running, got %+v", item)
		}
	}
	if loadedPassedItem.Status != "passed" || strings.Contains(string(loadedPassedItem.Evidence), "data:image") || !strings.Contains(string(loadedPassedItem.Evidence), "/api/test-artifacts/") {
		t.Fatalf("expected persisted passed item with artifact ref evidence, got %+v", loadedPassedItem)
	}
	loadedArtifacts, err := reopened.ListProjectTestRunArtifacts(ctx, user.ID, workspaceID, project.ID, run.Run.ID)
	if err != nil {
		t.Fatalf("load persisted test artifacts: %v", err)
	}
	if len(loadedArtifacts) != 1 || loadedArtifacts[0].ID != artifactRefs[0].ID || len(loadedArtifacts[0].Content) == 0 {
		t.Fatalf("expected persisted screenshot artifact, got %+v", loadedArtifacts)
	}
	loadedAdHocRun, err := reopened.GetProjectTestRun(ctx, user.ID, workspaceID, project.ID, adHocRun.Run.ID)
	if err != nil {
		t.Fatalf("load persisted direct run: %v", err)
	}
	if loadedAdHocRun.Run.PlanID != "" || loadedAdHocRun.Run.Source != "ad_hoc" || loadedAdHocRun.Plan != nil || len(loadedAdHocRun.Items) != 1 {
		t.Fatalf("unexpected persisted direct run: %+v", loadedAdHocRun)
	}
}
