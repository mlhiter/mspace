package control

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func (s *PostgresStore) storeTestCaseProposalArtifacts(ctx context.Context, q queryer, task RuntimeTask, artifact TestCaseProposalArtifact) error {
	if len(artifact.Proposals) > maxArtifactTestCaseProposals {
		return errors.New("test-case-proposals.json contains too many proposals")
	}
	userID, _ := runtimeTaskCreator(ctx, q, task.WorkspaceID, task.ID)
	for _, item := range artifact.Proposals {
		proposal := buildTestCaseProposalFromArtifact(ctx, q, task, userID, item)
		if _, err := insertTestCaseProposalRecord(ctx, q, proposal); err != nil {
			return err
		}
	}
	return nil
}

func buildTestCaseProposalFromArtifact(ctx context.Context, q queryer, task RuntimeTask, userID string, item TestCaseProposalArtifactItem) TestCaseProposal {
	now := time.Now().UTC().Format(time.RFC3339)
	proposalType := normalizeProposalType(item.Type)
	errorsList := []string{}
	if proposalType == "" {
		proposalType = "update"
		errorsList = append(errorsList, "proposal type must be create, update, or archive")
	}
	targetCaseID := strings.TrimSpace(item.CaseID)
	var currentCase *TestCase
	if proposalType != "create" {
		if targetCaseID == "" {
			errorsList = append(errorsList, "caseId is required for update or archive proposals")
		} else if testCase, err := loadProjectTestCase(ctx, q, task.WorkspaceID, task.ProjectID, targetCaseID); err == nil {
			currentCase = cloneTestCasePointer(testCase)
		} else {
			errorsList = append(errorsList, "caseId must belong to this project")
		}
	}
	proposed := copyTestCaseInput(item.ProposedCase)
	if proposalType == "archive" && currentCase != nil {
		proposed = testCaseToInput(*currentCase)
		proposed.Status = "archived"
	}
	if proposed.Source == "" {
		if proposalType == "create" {
			proposed.Source = "codex_generated"
		} else {
			proposed.Source = "codex_refined"
		}
	}
	if proposed.Status == "" && proposalType == "create" {
		proposed.Status = "needs_review"
	}
	score := 0
	findings := []TestCaseQualityFinding{}
	if normalized, qualityScore, qualityFindings, err := normalizeTestCaseInput(proposed, proposed.Source); err == nil {
		proposed = normalized
		score = qualityScore
		findings = qualityFindings
	} else {
		errorsList = append(errorsList, err.Error())
	}
	status := "pending"
	if len(errorsList) > 0 {
		status = "invalid"
	}
	title := collapseWhitespace(item.Title)
	if title == "" {
		title = proposed.Title
	}
	return TestCaseProposal{
		WorkspaceID:      task.WorkspaceID,
		ProjectID:        task.ProjectID,
		SourceIssueID:    task.IssueID,
		SourceSessionID:  firstNonEmpty(task.SessionID, task.ID),
		TargetCaseID:     targetCaseID,
		ProposalType:     proposalType,
		Status:           status,
		Title:            title,
		Summary:          strings.TrimSpace(item.Summary),
		Rationale:        strings.TrimSpace(item.Rationale),
		CurrentCase:      currentCase,
		ProposedCase:     proposed,
		QualityScore:     score,
		QualityFindings:  findings,
		ValidationErrors: errorsList,
		CreatedByUserID:  userID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

func (s *MemoryStore) storeTestCaseProposalArtifactsLocked(task RuntimeTask, artifact TestCaseProposalArtifact) {
	if len(artifact.Proposals) > maxArtifactTestCaseProposals {
		return
	}
	userID := s.runtimeTaskCreatedByLocked(task.ID)
	for _, item := range artifact.Proposals {
		proposal := s.buildMemoryTestCaseProposalFromArtifact(task, userID, item)
		s.insertTestCaseProposalLocked(proposal)
	}
}

func (s *MemoryStore) buildMemoryTestCaseProposalFromArtifact(task RuntimeTask, userID string, item TestCaseProposalArtifactItem) TestCaseProposal {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	proposalType := normalizeProposalType(item.Type)
	errorsList := []string{}
	if proposalType == "" {
		proposalType = "update"
		errorsList = append(errorsList, "proposal type must be create, update, or archive")
	}
	targetCaseID := strings.TrimSpace(item.CaseID)
	var currentCase *TestCase
	if proposalType != "create" {
		if targetCaseID == "" {
			errorsList = append(errorsList, "caseId is required for update or archive proposals")
		} else if testCase, ok := s.testCases[targetCaseID]; ok && testCase.WorkspaceID == task.WorkspaceID && testCase.ProjectID == task.ProjectID {
			currentCase = cloneTestCasePointer(testCase)
		} else {
			errorsList = append(errorsList, "caseId must belong to this project")
		}
	}
	proposed := copyTestCaseInput(item.ProposedCase)
	if proposalType == "archive" && currentCase != nil {
		proposed = testCaseToInput(*currentCase)
		proposed.Status = "archived"
	}
	if proposed.Source == "" {
		if proposalType == "create" {
			proposed.Source = "codex_generated"
		} else {
			proposed.Source = "codex_refined"
		}
	}
	if proposed.Status == "" && proposalType == "create" {
		proposed.Status = "needs_review"
	}
	score := 0
	findings := []TestCaseQualityFinding{}
	if normalized, qualityScore, qualityFindings, err := normalizeTestCaseInput(proposed, proposed.Source); err == nil {
		proposed = normalized
		score = qualityScore
		findings = qualityFindings
	} else {
		errorsList = append(errorsList, err.Error())
	}
	status := "pending"
	if len(errorsList) > 0 {
		status = "invalid"
	}
	title := collapseWhitespace(item.Title)
	if title == "" {
		title = proposed.Title
	}
	return TestCaseProposal{
		WorkspaceID:      task.WorkspaceID,
		ProjectID:        task.ProjectID,
		SourceIssueID:    task.IssueID,
		SourceSessionID:  firstNonEmpty(task.SessionID, task.ID),
		TargetCaseID:     targetCaseID,
		ProposalType:     proposalType,
		Status:           status,
		Title:            title,
		Summary:          strings.TrimSpace(item.Summary),
		Rationale:        strings.TrimSpace(item.Rationale),
		CurrentCase:      currentCase,
		ProposedCase:     proposed,
		QualityScore:     score,
		QualityFindings:  findings,
		ValidationErrors: errorsList,
		CreatedByUserID:  userID,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

func (s *PostgresStore) reconcileTestResultArtifact(ctx context.Context, q queryer, task RuntimeTask, artifact TestResultArtifact) error {
	runID := strings.TrimSpace(artifact.RunID)
	if runID == "" {
		runID = testRunIDFromTaskPayload(task)
	}
	if runID == "" {
		return errors.New("test-result.json runId is required")
	}
	if len(artifact.Items) > maxArtifactTestResultItems {
		return errors.New("test-result.json contains too many items")
	}
	run, err := loadTestRun(ctx, q, task.WorkspaceID, task.ProjectID, runID)
	if err != nil {
		return err
	}
	for _, item := range artifact.Items {
		if err := updateTestRunItemFromArtifact(ctx, q, run, item); err != nil {
			return err
		}
	}
	return updateTestRunCounts(ctx, q, run.WorkspaceID, run.ProjectID, run.ID)
}

func updateTestRunItemFromArtifact(ctx context.Context, q queryer, run TestRun, item TestResultArtifactItem) error {
	caseID := strings.TrimSpace(item.CaseID)
	status := normalizeTestRunItemStatus(item.Status)
	if caseID == "" {
		return errors.New("test-result.json caseId is required")
	}
	if status == "" || !isFinalTestRunItemStatus(status) {
		return errors.New("test-result.json status must be passed, failed, blocked, or skipped")
	}
	evidence := cloneRawJSONObject(item.Evidence)
	tag, err := q.Exec(ctx, `
		UPDATE test_run_items
		SET status = $5,
			actual_result = $6,
			failure_summary = $7,
			evidence = $8::jsonb,
			updated_at = now()
		WHERE workspace_id = $1
			AND project_id = $2
			AND run_id = $3
			AND case_id::text = $4
	`, run.WorkspaceID, run.ProjectID, run.ID, caseID, status, strings.TrimSpace(item.ActualResult), strings.TrimSpace(item.FailureSummary), evidence)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *MemoryStore) reconcileTestResultArtifactLocked(task RuntimeTask, artifact TestResultArtifact) {
	runID := strings.TrimSpace(artifact.RunID)
	if runID == "" {
		runID = testRunIDFromTaskPayload(task)
	}
	if runID == "" || len(artifact.Items) > maxArtifactTestResultItems {
		return
	}
	run, ok := s.testRuns[runID]
	if !ok || run.WorkspaceID != task.WorkspaceID || run.ProjectID != task.ProjectID {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, artifactItem := range artifact.Items {
		status := normalizeTestRunItemStatus(artifactItem.Status)
		if strings.TrimSpace(artifactItem.CaseID) == "" || status == "" || !isFinalTestRunItemStatus(status) {
			continue
		}
		for id, item := range s.testRunItems {
			if item.RunID == run.ID && item.TestCaseID == strings.TrimSpace(artifactItem.CaseID) {
				item.Status = status
				item.ActualResult = strings.TrimSpace(artifactItem.ActualResult)
				item.FailureSummary = strings.TrimSpace(artifactItem.FailureSummary)
				item.Evidence = cloneRawJSONObject(artifactItem.Evidence)
				item.UpdatedAt = now
				s.testRunItems[id] = item
			}
		}
	}
	items := []TestRunItem{}
	for _, item := range s.testRunItems {
		if item.RunID == run.ID {
			items = append(items, item)
		}
	}
	run.TotalCount = len(items)
	run.PassedCount, run.FailedCount, run.BlockedCount, run.SkippedCount = testRunCounts(items)
	if run.TotalCount > 0 && run.PassedCount+run.FailedCount+run.BlockedCount+run.SkippedCount >= run.TotalCount {
		run.Status = "needs_acceptance"
		run.CompletedAt = now
	} else {
		run.Status = "running"
	}
	run.UpdatedAt = now
	s.testRuns[run.ID] = run
}

func testRunIDFromTaskPayload(task RuntimeTask) string {
	var payload struct {
		RunID string `json:"testRunId"`
	}
	_ = json.Unmarshal(task.Payload, &payload)
	return strings.TrimSpace(payload.RunID)
}
