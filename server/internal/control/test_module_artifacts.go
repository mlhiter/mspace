package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	run, err := loadTestRun(ctx, q, task.WorkspaceID, runID)
	if err != nil {
		return err
	}
	for _, item := range artifact.Items {
		if isBatchTestResultArtifactItem(item) {
			if err := s.updateTestRunBatchItemsFromArtifact(ctx, q, task, run, item); err != nil {
				return err
			}
			continue
		}
		if err := s.updateTestRunItemFromArtifact(ctx, q, task, run, item); err != nil {
			return err
		}
	}
	return updateTestRunCounts(ctx, q, run.WorkspaceID, run.ID)
}

func (s *PostgresStore) reconcileTestSetupArtifact(ctx context.Context, q queryer, task RuntimeTask, artifact *TestSetupResultArtifact) error {
	runID := ""
	if artifact != nil {
		runID = strings.TrimSpace(artifact.RunID)
	}
	if runID == "" {
		runID = testRunIDFromTaskPayload(task)
	}
	if runID == "" {
		return errors.New("test-setup-result.json runId is required")
	}
	run, err := loadTestRun(ctx, q, task.WorkspaceID, runID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(run.SetupSteps) == "" {
		return nil
	}
	setupResult, status, runContext := buildTestSetupReconciliation(task, artifact)
	if status != "passed" {
		_, err := q.Exec(ctx, `
			UPDATE test_runs
			SET status = 'setup_failed',
				setup_status = 'failed',
				setup_result = $3::jsonb,
				run_context = $4::jsonb,
				completed_at = now(),
				updated_at = now()
			WHERE workspace_id = $1 AND id = $2
		`, run.WorkspaceID, run.ID, setupResult, runContext)
		return err
	}
	_, err = q.Exec(ctx, `
		UPDATE test_runs
		SET status = 'running',
			setup_status = 'passed',
			setup_result = $3::jsonb,
			run_context = $4::jsonb,
			updated_at = now()
		WHERE workspace_id = $1 AND id = $2
	`, run.WorkspaceID, run.ID, setupResult, runContext)
	if err != nil {
		return err
	}
	run.SetupStatus = "passed"
	run.Status = "running"
	run.SetupResult = setupResult
	run.RunContext = runContext
	userID, _ := runtimeTaskCreator(ctx, q, task.WorkspaceID, task.ID)
	return s.startPostgresTestRunExecutionSessionsWithQueryer(ctx, q, userID, run, CreateTestRunInput{
		AgentProfile: runtimeTaskAgentProfile(task),
		RuntimeMode:  task.RuntimeMode,
		BatchSize:    runtimeTaskTestRunBatchSize(task),
		ResultLocale: run.ResultLocale,
	})
}

func (s *PostgresStore) updateTestRunItemFromArtifact(ctx context.Context, q queryer, task RuntimeTask, run TestRun, item TestResultArtifactItem) error {
	caseID := strings.TrimSpace(item.CaseID)
	status := normalizeTestRunItemStatus(item.Status)
	if caseID == "" {
		return errors.New("test-result.json caseId is required")
	}
	if status == "" || !isFinalTestRunItemStatus(status) {
		return errors.New("test-result.json status must be passed, failed, blocked, or skipped")
	}
	runItem, err := loadTestRunItemByRunAndCase(ctx, q, run.WorkspaceID, run.ID, caseID)
	if err != nil {
		return err
	}
	evidence := cloneRawJSONObject(item.Evidence)
	artifacts, err := s.storeTestResultEvidenceArtifacts(ctx, q, task, run, runItem, evidence)
	if err != nil {
		return err
	}
	evidence = rewriteTestResultEvidenceWithArtifacts(evidence, artifacts)
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
	`, run.WorkspaceID, runItem.ProjectID, run.ID, caseID, status, strings.TrimSpace(item.ActualResult), strings.TrimSpace(item.FailureSummary), evidence)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) updateTestRunBatchItemsFromArtifact(ctx context.Context, q queryer, task RuntimeTask, run TestRun, item TestResultArtifactItem) error {
	status := normalizeTestRunItemStatus(item.Status)
	if status == "" || !isFinalTestRunItemStatus(status) {
		return errors.New("test-result.json status must be passed, failed, blocked, or skipped")
	}
	sessionID := strings.TrimSpace(task.SessionID)
	if sessionID == "" {
		return nil
	}
	runItems, err := loadActiveTestRunItemsBySession(ctx, q, run.WorkspaceID, run.ID, sessionID)
	if err != nil {
		return err
	}
	for _, runItem := range runItems {
		evidence := cloneRawJSONObject(item.Evidence)
		artifacts, err := s.storeTestResultEvidenceArtifacts(ctx, q, task, run, runItem, evidence)
		if err != nil {
			return err
		}
		evidence = rewriteTestResultEvidenceWithArtifacts(evidence, artifacts)
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
				AND id = $4
				AND status IN ('queued', 'running')
		`, run.WorkspaceID, runItem.ProjectID, run.ID, runItem.ID, status, strings.TrimSpace(item.ActualResult), strings.TrimSpace(item.FailureSummary), evidence)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
	}
	return nil
}

func isBatchTestResultArtifactItem(item TestResultArtifactItem) bool {
	switch strings.ToLower(strings.TrimSpace(item.CaseID)) {
	case "batch", "__batch__", "all", "*":
		return true
	default:
		return false
	}
}

func buildTestSetupReconciliation(task RuntimeTask, artifact *TestSetupResultArtifact) (json.RawMessage, string, json.RawMessage) {
	if task.Status != "completed" {
		summary := strings.TrimSpace(task.Error)
		if summary == "" {
			summary = fmt.Sprintf("Setup task ended with status %s.", firstNonEmpty(task.Status, "unknown"))
		}
		result, _ := json.Marshal(map[string]any{
			"runId":          testRunIDFromTaskPayload(task),
			"status":         "failed",
			"summary":        summary,
			"failureSummary": summary,
		})
		return cloneRawJSONObject(result), "failed", json.RawMessage(`{}`)
	}
	if artifact == nil {
		result, _ := json.Marshal(map[string]any{
			"runId":   testRunIDFromTaskPayload(task),
			"status":  "failed",
			"summary": "Setup task completed without test-setup-result.json.",
		})
		return cloneRawJSONObject(result), "failed", json.RawMessage(`{}`)
	}
	status := normalizeTestSetupStatus(artifact.Status)
	if status == "" {
		status = "passed"
	}
	if status != "passed" {
		status = "failed"
	}
	outputs := cloneRawJSONObject(artifact.Outputs)
	evidence := cloneRawJSONObject(artifact.Evidence)
	steps := make([]map[string]any, 0, len(artifact.Steps))
	for _, step := range artifact.Steps {
		stepStatus := normalizeTestSetupStatus(step.Status)
		if stepStatus == "" {
			stepStatus = "passed"
		}
		steps = append(steps, map[string]any{
			"title":          strings.TrimSpace(step.Title),
			"status":         stepStatus,
			"command":        strings.TrimSpace(step.Command),
			"summary":        strings.TrimSpace(step.Summary),
			"failureSummary": strings.TrimSpace(step.FailureSummary),
			"evidence":       cloneRawJSONObject(step.Evidence),
		})
		if stepStatus == "failed" && status == "passed" {
			status = "failed"
		}
	}
	result, _ := json.Marshal(map[string]any{
		"runId":          strings.TrimSpace(artifact.RunID),
		"status":         status,
		"summary":        strings.TrimSpace(artifact.Summary),
		"failureSummary": strings.TrimSpace(artifact.FailureSummary),
		"outputs":        outputs,
		"evidence":       evidence,
		"steps":          steps,
	})
	return cloneRawJSONObject(result), status, outputs
}

func normalizeTestSetupStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "passed", "pass", "success", "succeeded", "ok":
		return "passed"
	case "failed", "fail", "error", "blocked":
		return "failed"
	case "skipped", "skip":
		return "skipped"
	default:
		return ""
	}
}

func runtimeTaskAgentProfile(task RuntimeTask) string {
	var payload struct {
		AgentProfile string `json:"agentProfile"`
	}
	_ = json.Unmarshal(task.Payload, &payload)
	return normalizeAgentProfile(payload.AgentProfile)
}

func runtimeTaskTestRunBatchSize(task RuntimeTask) int {
	var payload struct {
		BatchSize int `json:"testRunBatchSize"`
	}
	_ = json.Unmarshal(task.Payload, &payload)
	if payload.BatchSize <= 0 {
		return defaultTestRunBatchSize
	}
	if payload.BatchSize > maxTestRunBatchSize {
		return maxTestRunBatchSize
	}
	return payload.BatchSize
}

func loadTestRunItemByRunAndCase(ctx context.Context, q queryer, workspaceID, runID, caseID string) (TestRunItem, error) {
	rows, err := q.Query(ctx, `
		SELECT
			i.id::text,
			i.workspace_id::text,
			i.project_id::text,
			i.run_id::text,
			i.case_id::text,
			i.sort_order,
			COALESCE(i.execution_issue_id::text, ''),
			i.agent_session_id,
			i.status,
			i.actual_result,
			i.failure_summary,
			i.evidence,
			i.created_at,
			i.updated_at,
			`+testCaseSelectColumnsForAlias("tc")+`
		FROM test_run_items i
		JOIN test_cases tc ON tc.id = i.case_id
		WHERE i.workspace_id = $1
			AND i.run_id = $2
			AND i.case_id::text = $3
		LIMIT 1
	`, workspaceID, runID, caseID)
	if err != nil {
		return TestRunItem{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return TestRunItem{}, ErrNotFound
	}
	item, err := scanTestRunItem(rows)
	if err != nil {
		return TestRunItem{}, err
	}
	if err := rows.Err(); err != nil {
		return TestRunItem{}, err
	}
	return item, nil
}

func loadActiveTestRunItemsBySession(ctx context.Context, q queryer, workspaceID, runID, sessionID string) ([]TestRunItem, error) {
	rows, err := q.Query(ctx, `
		SELECT
			i.id::text,
			i.workspace_id::text,
			i.project_id::text,
			i.run_id::text,
			i.case_id::text,
			i.sort_order,
			COALESCE(i.execution_issue_id::text, ''),
			i.agent_session_id,
			i.status,
			i.actual_result,
			i.failure_summary,
			i.evidence,
			i.created_at,
			i.updated_at,
			`+testCaseSelectColumnsForAlias("tc")+`
		FROM test_run_items i
		JOIN test_cases tc ON tc.id = i.case_id
		WHERE i.workspace_id = $1
			AND i.run_id = $2
			AND i.agent_session_id = $3
			AND i.status IN ('queued', 'running')
		ORDER BY i.sort_order ASC, i.created_at ASC
	`, workspaceID, runID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []TestRunItem{}
	for rows.Next() {
		item, err := scanTestRunItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) storeTestResultEvidenceArtifacts(ctx context.Context, q queryer, task RuntimeTask, run TestRun, item TestRunItem, evidence json.RawMessage) ([]TestArtifact, error) {
	candidates := testEvidenceScreenshotCandidates(evidence)
	if len(candidates) == 0 {
		return nil, nil
	}
	artifacts := []TestArtifact{}
	for _, candidate := range candidates {
		metadata, _ := json.Marshal(candidate.Metadata)
		artifact, err := s.createTestArtifact(ctx, q, CreateTestArtifactInput{
			WorkspaceID:     run.WorkspaceID,
			ProjectID:       item.ProjectID,
			RunID:           run.ID,
			RunItemID:       item.ID,
			CaseID:          item.TestCaseID,
			SourceIssueID:   firstNonEmpty(item.ExecutionIssueID, task.IssueID),
			SourceTaskID:    task.ID,
			SourceSessionID: firstNonEmpty(item.AgentSessionID, task.SessionID),
			Kind:            "screenshot",
			Role:            "evidence",
			Filename:        candidate.Filename,
			ContentType:     candidate.ContentType,
			Content:         candidate.Data,
			Metadata:        metadata,
		})
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
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
	if !ok || run.WorkspaceID != task.WorkspaceID {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, artifactItem := range artifact.Items {
		status := normalizeTestRunItemStatus(artifactItem.Status)
		if isBatchTestResultArtifactItem(artifactItem) {
			if status == "" || !isFinalTestRunItemStatus(status) {
				continue
			}
			sessionID := strings.TrimSpace(task.SessionID)
			if sessionID == "" {
				continue
			}
			for id, item := range s.testRunItems {
				if item.RunID != run.ID || item.AgentSessionID != sessionID || isFinalTestRunItemStatus(item.Status) {
					continue
				}
				item.Status = status
				item.ActualResult = strings.TrimSpace(artifactItem.ActualResult)
				item.FailureSummary = strings.TrimSpace(artifactItem.FailureSummary)
				artifacts := s.storeMemoryTestResultEvidenceArtifactsLocked(task, run, item, artifactItem.Evidence)
				item.Evidence = rewriteTestResultEvidenceWithArtifacts(cloneRawJSONObject(artifactItem.Evidence), artifacts)
				item.UpdatedAt = now
				s.testRunItems[id] = item
			}
			continue
		}
		if strings.TrimSpace(artifactItem.CaseID) == "" || status == "" || !isFinalTestRunItemStatus(status) {
			continue
		}
		for id, item := range s.testRunItems {
			if item.RunID == run.ID && item.TestCaseID == strings.TrimSpace(artifactItem.CaseID) {
				item.Status = status
				item.ActualResult = strings.TrimSpace(artifactItem.ActualResult)
				item.FailureSummary = strings.TrimSpace(artifactItem.FailureSummary)
				artifacts := s.storeMemoryTestResultEvidenceArtifactsLocked(task, run, item, artifactItem.Evidence)
				item.Evidence = rewriteTestResultEvidenceWithArtifacts(cloneRawJSONObject(artifactItem.Evidence), artifacts)
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

func (s *MemoryStore) reconcileTestSetupArtifactLocked(task RuntimeTask, artifact *TestSetupResultArtifact) {
	runID := ""
	if artifact != nil {
		runID = strings.TrimSpace(artifact.RunID)
	}
	if runID == "" {
		runID = testRunIDFromTaskPayload(task)
	}
	run, ok := s.testRuns[runID]
	if !ok || run.WorkspaceID != task.WorkspaceID || run.ProjectID != task.ProjectID || strings.TrimSpace(run.SetupSteps) == "" {
		return
	}
	setupResult, status, runContext := buildTestSetupReconciliation(task, artifact)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	run.SetupResult = setupResult
	run.RunContext = runContext
	run.UpdatedAt = now
	if status != "passed" {
		run.Status = "setup_failed"
		run.SetupStatus = "failed"
		run.CompletedAt = now
		s.testRuns[run.ID] = run
		return
	}
	run.Status = "running"
	run.SetupStatus = "passed"
	s.testRuns[run.ID] = run
	input := CreateTestRunInput{
		AgentProfile: runtimeTaskAgentProfile(task),
		RuntimeMode:  task.RuntimeMode,
		BatchSize:    runtimeTaskTestRunBatchSize(task),
		ResultLocale: run.ResultLocale,
	}
	if err := s.startTestRunExecutionSessionsLocked(s.runtimeTaskCreatedByLocked(task.ID), run.ID, input); err != nil {
		run.Status = "setup_failed"
		run.SetupStatus = "failed"
		run.SetupResult = cloneRawJSONObject(json.RawMessage(fmt.Sprintf(`{"status":"failed","summary":"%s"}`, err.Error())))
		run.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		s.testRuns[run.ID] = run
	}
}

func (s *MemoryStore) storeMemoryTestResultEvidenceArtifactsLocked(task RuntimeTask, run TestRun, item TestRunItem, evidence json.RawMessage) []TestArtifact {
	candidates := testEvidenceScreenshotCandidates(evidence)
	if len(candidates) == 0 {
		return nil
	}
	artifacts := []TestArtifact{}
	for _, candidate := range candidates {
		metadata, _ := json.Marshal(candidate.Metadata)
		artifact, err := s.createMemoryTestArtifactLocked(CreateTestArtifactInput{
			WorkspaceID:     run.WorkspaceID,
			ProjectID:       item.ProjectID,
			RunID:           run.ID,
			RunItemID:       item.ID,
			CaseID:          item.TestCaseID,
			SourceIssueID:   firstNonEmpty(item.ExecutionIssueID, task.IssueID),
			SourceTaskID:    task.ID,
			SourceSessionID: firstNonEmpty(item.AgentSessionID, task.SessionID),
			Kind:            "screenshot",
			Role:            "evidence",
			Filename:        candidate.Filename,
			ContentType:     candidate.ContentType,
			Content:         candidate.Data,
			Metadata:        metadata,
		})
		if err != nil {
			continue
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts
}

func testRunIDFromTaskPayload(task RuntimeTask) string {
	var payload struct {
		RunID string `json:"testRunId"`
	}
	_ = json.Unmarshal(task.Payload, &payload)
	return strings.TrimSpace(payload.RunID)
}
