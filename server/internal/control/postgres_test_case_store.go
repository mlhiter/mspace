package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) ListProjectTestCases(ctx Context, userID, workspaceID, projectID string, options TestCaseListOptions) (TestCaseListResult, error) {
	dbctx := asContext(ctx)
	options = normalizeTestCaseListOptions(options)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return TestCaseListResult{}, err
	}
	if err := ensureProjectInWorkspace(dbctx, s.pool, workspaceID, projectID); err != nil {
		return TestCaseListResult{}, err
	}
	status := strings.ToLower(strings.TrimSpace(options.Status))
	query := strings.ToLower(strings.TrimSpace(options.Query))
	var total int
	if err := s.pool.QueryRow(dbctx, `
		SELECT COUNT(*)::int
		FROM test_cases
		WHERE workspace_id = $1
			AND project_id = $2
			AND (($3 = '' AND status <> 'archived') OR ($3 <> '' AND status = $3))
			AND ($4 = '' OR lower(title || ' ' || type || ' ' || area || ' ' || tags::text) LIKE '%' || $4 || '%')
	`, workspaceID, projectID, status, query).Scan(&total); err != nil {
		return TestCaseListResult{}, err
	}
	rows, err := s.pool.Query(dbctx, `
		SELECT
			id::text,
			workspace_id::text,
			project_id::text,
			title,
			type,
			area,
			priority,
			status,
			source,
			preconditions,
			steps,
			expected_result,
			environment_requirements,
			dependencies,
			tags,
			quality_score,
			quality_findings,
			COALESCE(created_by_user_id::text, ''),
			created_at,
			updated_at
		FROM test_cases
		WHERE workspace_id = $1
			AND project_id = $2
			AND (($3 = '' AND status <> 'archived') OR ($3 <> '' AND status = $3))
			AND ($4 = '' OR lower(title || ' ' || type || ' ' || area || ' ' || tags::text) LIKE '%' || $4 || '%')
		ORDER BY updated_at DESC, created_at DESC
		LIMIT $5 OFFSET $6
	`, workspaceID, projectID, status, query, options.Limit, options.Offset)
	if err != nil {
		return TestCaseListResult{}, err
	}
	defer rows.Close()

	cases := []TestCase{}
	for rows.Next() {
		testCase, err := scanTestCase(rows)
		if err != nil {
			return TestCaseListResult{}, err
		}
		cases = append(cases, testCase)
	}
	if err := rows.Err(); err != nil {
		return TestCaseListResult{}, err
	}
	if err := attachLatestTestCaseResults(dbctx, s.pool, workspaceID, projectID, cases); err != nil {
		return TestCaseListResult{}, err
	}
	return TestCaseListResult{
		Cases:  cases,
		Total:  total,
		Limit:  options.Limit,
		Offset: options.Offset,
	}, nil
}

func (s *PostgresStore) CreateProjectTestCase(ctx Context, userID, workspaceID, projectID string, input TestCaseInput) (TestCase, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	normalized, score, findings, err := normalizeTestCaseInput(input, defaultTestCaseSource)
	if err != nil {
		return TestCase{}, err
	}
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return TestCase{}, err
	}
	if err := ensureProjectInWorkspace(dbctx, s.pool, workspaceID, projectID); err != nil {
		return TestCase{}, err
	}
	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return TestCase{}, err
	}
	defer tx.Rollback(dbctx)

	testCase, err := insertProjectTestCase(dbctx, tx, workspaceID, projectID, userID, normalized, score, findings)
	if err != nil {
		return TestCase{}, err
	}
	if err := insertProjectTestCaseRevision(dbctx, tx, testCase, userID); err != nil {
		return TestCase{}, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return TestCase{}, err
	}
	return testCase, nil
}

func (s *PostgresStore) ImportProjectTestCases(ctx Context, userID, workspaceID, projectID string, input ImportTestCasesInput) (ImportTestCasesResult, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	inputs, skipped, err := parseImportedTestCases(input)
	if err != nil {
		return ImportTestCasesResult{}, err
	}
	if len(inputs) == 0 {
		return ImportTestCasesResult{}, errors.New("content cannot be empty")
	}
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return ImportTestCasesResult{}, err
	}
	if err := ensureProjectInWorkspace(dbctx, s.pool, workspaceID, projectID); err != nil {
		return ImportTestCasesResult{}, err
	}
	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return ImportTestCasesResult{}, err
	}
	defer tx.Rollback(dbctx)

	result := ImportTestCasesResult{Created: []TestCase{}, Skipped: testCaseImportSkipsOrEmpty(skipped)}
	for _, imported := range inputs {
		normalized, score, findings, err := normalizeTestCaseInput(imported, defaultImportedCaseSource)
		if err != nil {
			result.Skipped = append(result.Skipped, TestCaseImportSkip{Reason: err.Error(), Content: imported.Title})
			continue
		}
		normalized.Source = defaultImportedCaseSource
		testCase, err := insertProjectTestCase(dbctx, tx, workspaceID, projectID, userID, normalized, score, findings)
		if err != nil {
			return ImportTestCasesResult{}, err
		}
		if err := insertProjectTestCaseRevision(dbctx, tx, testCase, userID); err != nil {
			return ImportTestCasesResult{}, err
		}
		result.Created = append(result.Created, testCase)
	}
	if len(result.Created) == 0 {
		return ImportTestCasesResult{}, errors.New("content cannot be empty")
	}
	if err := tx.Commit(dbctx); err != nil {
		return ImportTestCasesResult{}, err
	}
	return result, nil
}

func (s *PostgresStore) EnsureActiveCodexWorker(ctx Context, userID, workspaceID, runtimeMode string) (string, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	userID = strings.TrimSpace(userID)
	workspace, err := loadWorkspaceForUser(dbctx, s.pool, workspaceID, userID)
	if err != nil {
		return "", err
	}
	runtimeMode = strings.ToLower(strings.TrimSpace(runtimeMode))
	if runtimeMode == "" {
		runtimeMode = workspace.Kind
	}
	if runtimeMode != "personal" && runtimeMode != "team" {
		return "", errors.New("runtimeMode must be personal or team")
	}
	if runtimeMode != workspace.Kind {
		return "", ErrForbidden
	}
	hasActiveWorker, err := s.hasActiveCodexWorker(dbctx, workspaceID, runtimeMode)
	if err != nil {
		return "", err
	}
	if !hasActiveWorker {
		return "", ErrNoActiveCodexWorker
	}
	return runtimeMode, nil
}

func (s *PostgresStore) GetProjectTestCase(ctx Context, userID, workspaceID, projectID, caseID string) (TestCase, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	caseID = strings.TrimSpace(caseID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return TestCase{}, err
	}
	row := s.pool.QueryRow(dbctx, testCaseSelectQuery(`
		WHERE workspace_id = $1 AND project_id = $2 AND id = $3
	`), workspaceID, projectID, caseID)
	testCase, err := scanTestCase(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return TestCase{}, ErrNotFound
	}
	if err != nil {
		return TestCase{}, err
	}
	cases := []TestCase{testCase}
	if err := attachLatestTestCaseResults(dbctx, s.pool, workspaceID, projectID, cases); err != nil {
		return TestCase{}, err
	}
	return cases[0], nil
}

func (s *PostgresStore) UpdateProjectTestCase(ctx Context, userID, workspaceID, projectID, caseID string, input TestCaseInput) (TestCase, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	caseID = strings.TrimSpace(caseID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return TestCase{}, err
	}
	existing, err := loadProjectTestCase(dbctx, s.pool, workspaceID, projectID, caseID)
	if err != nil {
		return TestCase{}, err
	}
	if input.Source == "" {
		input.Source = existing.Source
	}
	normalized, score, findings, err := normalizeTestCaseInput(input, existing.Source)
	if err != nil {
		return TestCase{}, err
	}
	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return TestCase{}, err
	}
	defer tx.Rollback(dbctx)

	updated, err := updateProjectTestCase(dbctx, tx, workspaceID, projectID, caseID, normalized, score, findings)
	if err != nil {
		return TestCase{}, err
	}
	if err := insertProjectTestCaseRevision(dbctx, tx, updated, userID); err != nil {
		return TestCase{}, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return TestCase{}, err
	}
	return updated, nil
}

func (s *PostgresStore) DeleteProjectTestCase(ctx Context, userID, workspaceID, projectID, caseID string) (TestCase, error) {
	result, err := s.DeleteProjectTestCases(ctx, userID, workspaceID, projectID, DeleteProjectTestCasesInput{CaseIDs: []string{caseID}})
	if err != nil {
		return TestCase{}, err
	}
	if len(result) == 0 {
		return TestCase{}, ErrNotFound
	}
	return result[0], nil
}

func (s *PostgresStore) DeleteProjectTestCases(ctx Context, userID, workspaceID, projectID string, input DeleteProjectTestCasesInput) ([]TestCase, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	caseIDs := normalizeTestCaseIDList(input.CaseIDs)
	if len(caseIDs) == 0 {
		return nil, errors.New("caseIds is required")
	}
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return nil, err
	}
	if err := ensureProjectInWorkspace(dbctx, s.pool, workspaceID, projectID); err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(dbctx)

	for _, caseID := range caseIDs {
		if _, err := loadProjectTestCase(dbctx, tx, workspaceID, projectID, caseID); err != nil {
			return nil, err
		}
	}
	archived := make([]TestCase, 0, len(caseIDs))
	for _, caseID := range caseIDs {
		testCase, err := archiveProjectTestCase(dbctx, tx, workspaceID, projectID, caseID, userID)
		if err != nil {
			return nil, err
		}
		archived = append(archived, testCase)
	}
	if err := tx.Commit(dbctx); err != nil {
		return nil, err
	}
	return archived, nil
}

func (s *PostgresStore) ListProjectTestCaseRevisions(ctx Context, userID, workspaceID, projectID, caseID string) ([]TestCaseRevision, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	caseID = strings.TrimSpace(caseID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return nil, err
	}
	if _, err := loadProjectTestCase(dbctx, s.pool, workspaceID, projectID, caseID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(dbctx, `
		SELECT
			id::text,
			workspace_id::text,
			project_id::text,
			case_id::text,
			COALESCE(author_user_id::text, ''),
			revision_number,
			snapshot,
			created_at
		FROM test_case_revisions
		WHERE workspace_id = $1 AND project_id = $2 AND case_id = $3
		ORDER BY revision_number DESC
	`, workspaceID, projectID, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	revisions := []TestCaseRevision{}
	for rows.Next() {
		revision, err := scanTestCaseRevision(rows)
		if err != nil {
			return nil, err
		}
		revisions = append(revisions, revision)
	}
	return revisions, rows.Err()
}

func (s *PostgresStore) ListProjectTestCaseProposals(ctx Context, userID, workspaceID, projectID string, options TestCaseProposalListOptions) ([]TestCaseProposal, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return nil, err
	}
	if err := ensureProjectInWorkspace(dbctx, s.pool, workspaceID, projectID); err != nil {
		return nil, err
	}
	status := normalizeProposalStatus(options.Status)
	if strings.TrimSpace(options.Status) != "" && status == "" {
		return nil, errors.New("status must be pending, applied, rejected, or invalid")
	}
	rows, err := s.pool.Query(dbctx, testCaseProposalSelectQuery(`
		WHERE p.workspace_id = $1
			AND p.project_id = $2
			AND ($3 = '' OR p.status = $3)
		ORDER BY p.updated_at DESC, p.created_at DESC
	`), workspaceID, projectID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	proposals := []TestCaseProposal{}
	for rows.Next() {
		proposal, err := scanTestCaseProposal(rows)
		if err != nil {
			return nil, err
		}
		proposals = append(proposals, proposal)
	}
	return proposals, rows.Err()
}

func (s *PostgresStore) ApplyProjectTestCaseProposal(ctx Context, userID, workspaceID, projectID, proposalID string, input ReviewTestCaseProposalInput) (ApplyTestCaseProposalResult, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	proposalID = strings.TrimSpace(proposalID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return ApplyTestCaseProposalResult{}, err
	}
	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return ApplyTestCaseProposalResult{}, err
	}
	defer tx.Rollback(dbctx)

	proposal, err := loadTestCaseProposal(dbctx, tx, workspaceID, projectID, proposalID)
	if err != nil {
		return ApplyTestCaseProposalResult{}, err
	}
	if proposal.Status != "pending" {
		return ApplyTestCaseProposalResult{}, ErrConflict
	}
	if len(proposal.ValidationErrors) > 0 {
		return ApplyTestCaseProposalResult{}, errors.New("proposal has validation errors")
	}
	var applied *TestCase
	switch proposal.ProposalType {
	case "create":
		next := proposal.ProposedCase
		next.Source = "codex_generated"
		if next.Status == "" {
			next.Status = "needs_review"
		}
		normalized, score, findings, err := normalizeTestCaseInput(next, "codex_generated")
		if err != nil {
			return ApplyTestCaseProposalResult{}, err
		}
		testCase, err := insertProjectTestCase(dbctx, tx, workspaceID, projectID, userID, normalized, score, findings)
		if err != nil {
			return ApplyTestCaseProposalResult{}, err
		}
		if err := insertProjectTestCaseRevision(dbctx, tx, testCase, userID); err != nil {
			return ApplyTestCaseProposalResult{}, err
		}
		applied = cloneTestCasePointer(testCase)
		proposal.AppliedCaseID = testCase.ID
	case "update", "archive":
		existing, err := loadProjectTestCase(dbctx, tx, workspaceID, projectID, proposal.TargetCaseID)
		if err != nil {
			return ApplyTestCaseProposalResult{}, err
		}
		next := proposal.ProposedCase
		if proposal.ProposalType == "archive" {
			next = testCaseToInput(existing)
			next.Status = "archived"
		}
		next.Source = "codex_refined"
		normalized, score, findings, err := normalizeTestCaseInput(next, "codex_refined")
		if err != nil {
			return ApplyTestCaseProposalResult{}, err
		}
		updated, err := updateProjectTestCase(dbctx, tx, workspaceID, projectID, existing.ID, normalized, score, findings)
		if err != nil {
			return ApplyTestCaseProposalResult{}, err
		}
		if err := insertProjectTestCaseRevision(dbctx, tx, updated, userID); err != nil {
			return ApplyTestCaseProposalResult{}, err
		}
		applied = cloneTestCasePointer(updated)
		proposal.AppliedCaseID = updated.ID
	default:
		return ApplyTestCaseProposalResult{}, errors.New("proposal type must be create, update, or archive")
	}
	reviewed, err := reviewTestCaseProposalRecord(dbctx, tx, workspaceID, projectID, proposal.ID, "applied", userID, proposal.AppliedCaseID, input.Note)
	if err != nil {
		return ApplyTestCaseProposalResult{}, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return ApplyTestCaseProposalResult{}, err
	}
	return ApplyTestCaseProposalResult{Proposal: reviewed, TestCase: applied}, nil
}

func (s *PostgresStore) RejectProjectTestCaseProposal(ctx Context, userID, workspaceID, projectID, proposalID string, input ReviewTestCaseProposalInput) (TestCaseProposal, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	proposalID = strings.TrimSpace(proposalID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return TestCaseProposal{}, err
	}
	proposal, err := loadTestCaseProposal(dbctx, s.pool, workspaceID, projectID, proposalID)
	if err != nil {
		return TestCaseProposal{}, err
	}
	if proposal.Status != "pending" && proposal.Status != "invalid" {
		return TestCaseProposal{}, ErrConflict
	}
	return reviewTestCaseProposalRecord(dbctx, s.pool, workspaceID, projectID, proposalID, "rejected", userID, "", input.Note)
}

func (s *PostgresStore) ListProjectTestPlans(ctx Context, userID, workspaceID, projectID string, options TestPlanListOptions) ([]TestPlan, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return nil, err
	}
	if err := ensureProjectInWorkspace(dbctx, s.pool, workspaceID, projectID); err != nil {
		return nil, err
	}
	status := strings.ToLower(strings.TrimSpace(options.Status))
	if status != "" && status != "draft" && status != "ready" && status != "archived" {
		return nil, errors.New("status must be draft, ready, or archived")
	}
	rows, err := s.pool.Query(dbctx, testPlanSelectQuery(`
		WHERE p.workspace_id = $1 AND p.project_id = $2 AND ($3 = '' OR p.status = $3)
		ORDER BY p.updated_at DESC, p.created_at DESC
	`), workspaceID, projectID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	plans := []TestPlan{}
	for rows.Next() {
		plan, err := scanTestPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}

func (s *PostgresStore) CreateProjectTestPlan(ctx Context, userID, workspaceID, projectID string, input TestPlanInput) (TestPlanDetail, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return TestPlanDetail{}, err
	}
	if err := ensureProjectInWorkspace(dbctx, s.pool, workspaceID, projectID); err != nil {
		return TestPlanDetail{}, err
	}
	normalized, err := normalizeTestPlanInput(input)
	if err != nil {
		return TestPlanDetail{}, err
	}
	if err := s.resolveTestPlanEnvironment(dbctx, workspaceID, &normalized); err != nil {
		return TestPlanDetail{}, err
	}
	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return TestPlanDetail{}, err
	}
	defer tx.Rollback(dbctx)
	if err := ensureReadyTestCasesForPlan(dbctx, tx, workspaceID, projectID, normalized.CaseIDs); err != nil {
		return TestPlanDetail{}, err
	}
	plan, err := insertTestPlanRecord(dbctx, tx, workspaceID, projectID, userID, normalized)
	if err != nil {
		return TestPlanDetail{}, err
	}
	if err := replaceTestPlanCaseRecords(dbctx, tx, plan, normalized.CaseIDs); err != nil {
		return TestPlanDetail{}, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return TestPlanDetail{}, err
	}
	return s.GetProjectTestPlan(ctx, userID, workspaceID, projectID, plan.ID)
}

func (s *PostgresStore) GetProjectTestPlan(ctx Context, userID, workspaceID, projectID, planID string) (TestPlanDetail, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	planID = strings.TrimSpace(planID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return TestPlanDetail{}, err
	}
	plan, err := loadTestPlan(dbctx, s.pool, workspaceID, projectID, planID)
	if err != nil {
		return TestPlanDetail{}, err
	}
	cases, err := listTestPlanCases(dbctx, s.pool, workspaceID, projectID, planID)
	if err != nil {
		return TestPlanDetail{}, err
	}
	runs, err := listTestRunsForPlan(dbctx, s.pool, workspaceID, projectID, planID)
	if err != nil {
		return TestPlanDetail{}, err
	}
	return TestPlanDetail{Plan: plan, Cases: cases, Runs: runs}, nil
}

func (s *PostgresStore) UpdateProjectTestPlan(ctx Context, userID, workspaceID, projectID, planID string, input TestPlanInput) (TestPlanDetail, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	planID = strings.TrimSpace(planID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return TestPlanDetail{}, err
	}
	normalized, err := normalizeTestPlanInput(input)
	if err != nil {
		return TestPlanDetail{}, err
	}
	if err := s.resolveTestPlanEnvironment(dbctx, workspaceID, &normalized); err != nil {
		return TestPlanDetail{}, err
	}
	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return TestPlanDetail{}, err
	}
	defer tx.Rollback(dbctx)
	if _, err := loadTestPlan(dbctx, tx, workspaceID, projectID, planID); err != nil {
		return TestPlanDetail{}, err
	}
	if err := ensureReadyTestCasesForPlan(dbctx, tx, workspaceID, projectID, normalized.CaseIDs); err != nil {
		return TestPlanDetail{}, err
	}
	if _, err := updateTestPlanRecord(dbctx, tx, workspaceID, projectID, planID, normalized); err != nil {
		return TestPlanDetail{}, err
	}
	if err := replaceTestPlanCaseRecords(dbctx, tx, TestPlan{ID: planID, WorkspaceID: workspaceID, ProjectID: projectID}, normalized.CaseIDs); err != nil {
		return TestPlanDetail{}, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return TestPlanDetail{}, err
	}
	return s.GetProjectTestPlan(ctx, userID, workspaceID, projectID, planID)
}

func (s *PostgresStore) StartProjectTestRun(ctx Context, user User, workspaceID, projectID, planID string, input CreateTestRunInput) (TestRunDetail, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	planID = strings.TrimSpace(planID)
	workspace, err := loadWorkspaceForUser(dbctx, s.pool, workspaceID, user.ID)
	if err != nil {
		return TestRunDetail{}, err
	}
	plan, err := loadTestPlan(dbctx, s.pool, workspaceID, projectID, planID)
	if err != nil {
		return TestRunDetail{}, err
	}
	normalized, err := normalizeCreateTestRunInput(input, plan)
	if err != nil {
		return TestRunDetail{}, err
	}
	if normalized.RuntimeMode == "" {
		normalized.RuntimeMode = workspace.Kind
	}
	if normalized.RuntimeMode != workspace.Kind {
		return TestRunDetail{}, ErrForbidden
	}
	if err := s.resolveTestRunEnvironment(dbctx, workspaceID, &normalized); err != nil {
		return TestRunDetail{}, err
	}
	hasActiveWorker, err := s.hasActiveCodexWorker(dbctx, workspaceID, normalized.RuntimeMode)
	if err != nil {
		return TestRunDetail{}, err
	}
	if !hasActiveWorker {
		return TestRunDetail{}, ErrNoActiveCodexWorker
	}
	planCases, err := listTestPlanCaseSnapshots(dbctx, s.pool, workspaceID, projectID, planID)
	if err != nil {
		return TestRunDetail{}, err
	}
	if len(planCases) == 0 {
		return TestRunDetail{}, errors.New("plan has no test cases")
	}
	return s.startPostgresProjectTestRun(ctx, user, workspace, &plan, planCases, normalized)
}

func (s *PostgresStore) StartAdHocProjectTestRun(ctx Context, user User, workspaceID, projectID string, input CreateAdHocTestRunInput) (TestRunDetail, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	workspace, err := loadWorkspaceForUser(dbctx, s.pool, workspaceID, user.ID)
	if err != nil {
		return TestRunDetail{}, err
	}
	if err := ensureProjectInWorkspace(dbctx, s.pool, workspaceID, projectID); err != nil {
		return TestRunDetail{}, err
	}
	normalized, err := normalizeCreateAdHocTestRunInput(input)
	if err != nil {
		return TestRunDetail{}, err
	}
	if normalized.RuntimeMode == "" {
		normalized.RuntimeMode = workspace.Kind
	}
	if normalized.RuntimeMode != workspace.Kind {
		return TestRunDetail{}, ErrForbidden
	}
	runInput := CreateTestRunInput{
		TargetType:      normalized.TargetType,
		TargetValue:     normalized.TargetValue,
		Environment:     normalized.Environment,
		EnvironmentID:   normalized.EnvironmentID,
		EnvironmentKind: normalized.EnvironmentKind,
		AgentProfile:    normalized.AgentProfile,
		RuntimeMode:     normalized.RuntimeMode,
		BatchSize:       normalized.BatchSize,
	}
	if err := s.resolveTestRunEnvironment(dbctx, workspaceID, &runInput); err != nil {
		return TestRunDetail{}, err
	}
	hasActiveWorker, err := s.hasActiveCodexWorker(dbctx, workspaceID, normalized.RuntimeMode)
	if err != nil {
		return TestRunDetail{}, err
	}
	if !hasActiveWorker {
		return TestRunDetail{}, ErrNoActiveCodexWorker
	}
	cases, err := listReadyProjectTestCaseSnapshots(dbctx, s.pool, workspaceID, projectID, normalized.CaseIDs)
	if err != nil {
		return TestRunDetail{}, err
	}
	return s.startPostgresProjectTestRun(ctx, user, workspace, nil, cases, runInput)
}

func (s *PostgresStore) startPostgresProjectTestRun(ctx Context, user User, workspace Workspace, plan *TestPlan, planCases []TestCase, normalized CreateTestRunInput) (TestRunDetail, error) {
	dbctx := asContext(ctx)
	if len(planCases) == 0 {
		return TestRunDetail{}, errors.New("test run has no test cases")
	}
	runSource := "ad_hoc"
	planID := ""
	if plan != nil {
		runSource = "plan"
		planID = plan.ID
	}
	var runID string
	if err := s.pool.QueryRow(dbctx, `SELECT gen_random_uuid()::text`).Scan(&runID); err != nil {
		return TestRunDetail{}, err
	}
	run := TestRun{
		ID:                  runID,
		WorkspaceID:         workspace.ID,
		ProjectID:           planCases[0].ProjectID,
		PlanID:              planID,
		Source:              runSource,
		Status:              "running",
		SetupStatus:         "not_required",
		SetupResult:         json.RawMessage(`{}`),
		RunContext:          json.RawMessage(`{}`),
		TargetType:          normalized.TargetType,
		TargetValue:         normalized.TargetValue,
		Environment:         normalized.Environment,
		EnvironmentID:       normalized.EnvironmentID,
		EnvironmentKind:     normalized.EnvironmentKind,
		EnvironmentSnapshot: cloneRawJSONObject(normalized.EnvironmentSnapshot),
		TotalCount:          len(planCases),
		AcceptanceStatus:    "pending",
		CreatedByUserID:     user.ID,
	}
	if plan != nil {
		run.SetupSteps = normalizeTestPlanSetupSteps(plan.SetupSteps)
		if run.SetupSteps != "" {
			run.Status = "setup_running"
			run.SetupStatus = "running"
		}
	}
	parentIssueID, err := s.CreateIssue(ctx, user, workspace.ID, CreateIssueInput{
		ProjectID: run.ProjectID,
		Title:     testRunTitle(plan, run, planCases),
		Body:      buildTestRunParentIssueBody(plan, run, planCases),
		LabelKeys: []string{"type:test"},
	})
	if err != nil {
		return TestRunDetail{}, err
	}
	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return TestRunDetail{}, err
	}
	defer tx.Rollback(dbctx)
	run, err = insertTestRunRecord(dbctx, tx, run, parentIssueID)
	if err != nil {
		return TestRunDetail{}, err
	}
	if err := insertTestRunItems(dbctx, tx, run, planCases); err != nil {
		return TestRunDetail{}, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return TestRunDetail{}, err
	}
	if run.SetupSteps != "" {
		if err := s.startPostgresTestRunSetupSession(ctx, user.ID, run, normalized); err != nil {
			return TestRunDetail{}, err
		}
	} else {
		if err := s.startPostgresTestRunExecutionSessions(ctx, user.ID, run, normalized); err != nil {
			return TestRunDetail{}, err
		}
	}
	return s.GetProjectTestRun(ctx, user.ID, run.WorkspaceID, run.ProjectID, run.ID)
}

func (s *PostgresStore) GetProjectTestRun(ctx Context, userID, workspaceID, projectID, runID string) (TestRunDetail, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	runID = strings.TrimSpace(runID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return TestRunDetail{}, err
	}
	run, err := loadTestRun(dbctx, s.pool, workspaceID, projectID, runID)
	if err != nil {
		return TestRunDetail{}, err
	}
	var plan *TestPlan
	if run.PlanID != "" {
		loadedPlan, err := loadTestPlan(dbctx, s.pool, workspaceID, projectID, run.PlanID)
		if err != nil {
			return TestRunDetail{}, err
		}
		plan = &loadedPlan
	}
	items, err := listTestRunItems(dbctx, s.pool, workspaceID, projectID, runID)
	if err != nil {
		return TestRunDetail{}, err
	}
	run.TotalCount = len(items)
	run.PassedCount, run.FailedCount, run.BlockedCount, run.SkippedCount = testRunCounts(items)
	return TestRunDetail{Run: run, Plan: plan, Items: items}, nil
}

func (s *PostgresStore) ListProjectTestRuns(ctx Context, userID, workspaceID, projectID string, options TestRunListOptions) ([]TestRun, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return nil, err
	}
	if err := ensureProjectInWorkspace(dbctx, s.pool, workspaceID, projectID); err != nil {
		return nil, err
	}
	status := strings.ToLower(strings.TrimSpace(options.Status))
	source := normalizeTestRunSource(options.Source)
	if strings.TrimSpace(options.Source) != "" && source == "" {
		return nil, errors.New("source must be ad_hoc, plan, retry, or incremental")
	}
	rows, err := s.pool.Query(dbctx, testRunSelectQuery(`
		WHERE r.workspace_id = $1
			AND r.project_id = $2
			AND ($3 = '' OR r.status = $3)
			AND ($4 = '' OR r.source = $4)
		ORDER BY r.updated_at DESC, r.created_at DESC
	`), workspaceID, projectID, status, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []TestRun{}
	for rows.Next() {
		run, err := scanTestRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *PostgresStore) ListProjectTestCaseRunItems(ctx Context, userID, workspaceID, projectID, caseID string) ([]TestCaseRunItem, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	caseID = strings.TrimSpace(caseID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return nil, err
	}
	if _, err := loadProjectTestCase(dbctx, s.pool, workspaceID, projectID, caseID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(dbctx, `
		SELECT
			i.id::text,
			i.workspace_id::text,
			i.project_id::text,
			i.run_id::text,
			i.case_id::text,
			COALESCE(i.execution_issue_id::text, ''),
			i.agent_session_id,
			i.status,
			i.actual_result,
			i.failure_summary,
			i.evidence,
			i.created_at,
			i.updated_at,
			`+testCaseSelectColumnsForAlias("tc")+`,
			`+testRunSelectColumnsForAlias("r")+`
		FROM test_run_items i
		JOIN test_cases tc ON tc.id = i.case_id
		JOIN test_runs r ON r.id = i.run_id
		WHERE i.workspace_id = $1
			AND i.project_id = $2
			AND i.case_id = $3
		ORDER BY i.updated_at DESC, r.updated_at DESC, i.id DESC
	`, workspaceID, projectID, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []TestCaseRunItem{}
	for rows.Next() {
		item, err := scanTestCaseRunItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) RetryProjectTestRun(ctx Context, user User, workspaceID, projectID, runID string, input RetryTestRunInput) (TestRunDetail, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	runID = strings.TrimSpace(runID)
	workspace, err := loadWorkspaceForUser(dbctx, s.pool, workspaceID, user.ID)
	if err != nil {
		return TestRunDetail{}, err
	}
	run, err := loadTestRun(dbctx, s.pool, workspaceID, projectID, runID)
	if err != nil {
		return TestRunDetail{}, err
	}
	normalized := normalizeRetryTestRunInput(input)
	if normalized.RuntimeMode == "" {
		normalized.RuntimeMode = workspace.Kind
	}
	if normalized.RuntimeMode != workspace.Kind {
		return TestRunDetail{}, ErrForbidden
	}
	hasActiveWorker, err := s.hasActiveCodexWorker(dbctx, workspaceID, normalized.RuntimeMode)
	if err != nil {
		return TestRunDetail{}, err
	}
	if !hasActiveWorker {
		return TestRunDetail{}, ErrNoActiveCodexWorker
	}
	if err := resetTestRunItemsForRetry(dbctx, s.pool, workspaceID, projectID, runID, normalized.ItemIDs); err != nil {
		return TestRunDetail{}, err
	}
	if _, err := s.pool.Exec(dbctx, `
		UPDATE test_runs
		SET status = 'running', acceptance_status = 'pending', acceptance_note = '', updated_at = now()
		WHERE workspace_id = $1 AND project_id = $2 AND id = $3
	`, workspaceID, projectID, runID); err != nil {
		return TestRunDetail{}, err
	}
	if err := s.startPostgresTestRunExecutionSessions(ctx, user.ID, run, CreateTestRunInput{AgentProfile: normalized.AgentProfile, RuntimeMode: normalized.RuntimeMode, BatchSize: defaultTestRunBatchSize}); err != nil {
		return TestRunDetail{}, err
	}
	return s.GetProjectTestRun(ctx, user.ID, workspaceID, projectID, runID)
}

func (s *PostgresStore) AcceptProjectTestRun(ctx Context, userID, workspaceID, projectID, runID string, input ReviewTestRunInput) (TestRun, error) {
	return reviewTestRunRecord(asContext(ctx), s.pool, userID, workspaceID, projectID, runID, "accepted", input.Note)
}

func (s *PostgresStore) BlockProjectTestRun(ctx Context, userID, workspaceID, projectID, runID string, input ReviewTestRunInput) (TestRun, error) {
	return reviewTestRunRecord(asContext(ctx), s.pool, userID, workspaceID, projectID, runID, "blocked", input.Note)
}

func ensureProjectInWorkspace(ctx context.Context, q queryer, workspaceID, projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return errors.New("projectId is required")
	}
	var exists bool
	if err := q.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM projects WHERE workspace_id = $1 AND id = $2)
	`, strings.TrimSpace(workspaceID), projectID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func insertProjectTestCase(ctx context.Context, q queryer, workspaceID, projectID, userID string, input TestCaseInput, score int, findings []TestCaseQualityFinding) (TestCase, error) {
	steps, err := encodeJSON(input.Steps)
	if err != nil {
		return TestCase{}, err
	}
	dependencies, err := encodeJSON(input.Dependencies)
	if err != nil {
		return TestCase{}, err
	}
	tags, err := encodeJSON(input.Tags)
	if err != nil {
		return TestCase{}, err
	}
	qualityFindings, err := encodeJSON(findings)
	if err != nil {
		return TestCase{}, err
	}
	row := q.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO test_cases (
				workspace_id,
				project_id,
				title,
				type,
				area,
				priority,
				status,
				source,
				preconditions,
				steps,
				expected_result,
				environment_requirements,
				dependencies,
				tags,
				quality_score,
				quality_findings,
				created_by_user_id
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11, $12, $13::jsonb, $14::jsonb, $15, $16::jsonb, NULLIF($17, '')::uuid)
			RETURNING *
		)
		SELECT `+testCaseSelectColumns()+` FROM inserted
	`, workspaceID, projectID, input.Title, input.Type, input.Area, input.Priority, input.Status, input.Source, input.Preconditions, steps, input.ExpectedResult, input.EnvironmentRequirements, dependencies, tags, score, qualityFindings, userID)
	return scanTestCase(row)
}

func updateProjectTestCase(ctx context.Context, q queryer, workspaceID, projectID, caseID string, input TestCaseInput, score int, findings []TestCaseQualityFinding) (TestCase, error) {
	steps, err := encodeJSON(input.Steps)
	if err != nil {
		return TestCase{}, err
	}
	dependencies, err := encodeJSON(input.Dependencies)
	if err != nil {
		return TestCase{}, err
	}
	tags, err := encodeJSON(input.Tags)
	if err != nil {
		return TestCase{}, err
	}
	qualityFindings, err := encodeJSON(findings)
	if err != nil {
		return TestCase{}, err
	}
	row := q.QueryRow(ctx, `
		WITH updated AS (
			UPDATE test_cases
			SET title = $4,
				type = $5,
				area = $6,
				priority = $7,
				status = $8,
				source = $9,
				preconditions = $10,
				steps = $11::jsonb,
				expected_result = $12,
				environment_requirements = $13,
				dependencies = $14::jsonb,
				tags = $15::jsonb,
				quality_score = $16,
				quality_findings = $17::jsonb,
				updated_at = now()
			WHERE workspace_id = $1 AND project_id = $2 AND id = $3
			RETURNING *
		)
		SELECT `+testCaseSelectColumns()+` FROM updated
	`, workspaceID, projectID, caseID, input.Title, input.Type, input.Area, input.Priority, input.Status, input.Source, input.Preconditions, steps, input.ExpectedResult, input.EnvironmentRequirements, dependencies, tags, score, qualityFindings)
	testCase, err := scanTestCase(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return TestCase{}, ErrNotFound
	}
	return testCase, err
}

func archiveProjectTestCase(ctx context.Context, q queryer, workspaceID, projectID, caseID, userID string) (TestCase, error) {
	existing, err := loadProjectTestCase(ctx, q, workspaceID, projectID, caseID)
	if err != nil {
		return TestCase{}, err
	}
	if existing.Status == "archived" {
		return existing, nil
	}
	next := testCaseToInput(existing)
	next.Status = "archived"
	normalized, score, findings, err := normalizeTestCaseInput(next, existing.Source)
	if err != nil {
		return TestCase{}, err
	}
	updated, err := updateProjectTestCase(ctx, q, workspaceID, projectID, existing.ID, normalized, score, findings)
	if err != nil {
		return TestCase{}, err
	}
	if err := insertProjectTestCaseRevision(ctx, q, updated, userID); err != nil {
		return TestCase{}, err
	}
	return updated, nil
}

func loadProjectTestCase(ctx context.Context, q queryer, workspaceID, projectID, caseID string) (TestCase, error) {
	row := q.QueryRow(ctx, testCaseSelectQuery(`
		WHERE workspace_id = $1 AND project_id = $2 AND id = $3
	`), workspaceID, projectID, caseID)
	testCase, err := scanTestCase(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return TestCase{}, ErrNotFound
	}
	return testCase, err
}

func insertProjectTestCaseRevision(ctx context.Context, q queryer, testCase TestCase, userID string) error {
	snapshot, err := encodeJSON(testCaseSnapshot(testCase))
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		INSERT INTO test_case_revisions (
			workspace_id,
			project_id,
			case_id,
			author_user_id,
			revision_number,
			snapshot
		)
		SELECT $1, $2, $3, NULLIF($4, '')::uuid, COALESCE(MAX(revision_number), 0) + 1, $5::jsonb
		FROM test_case_revisions
		WHERE workspace_id = $1 AND project_id = $2 AND case_id = $3
	`, testCase.WorkspaceID, testCase.ProjectID, testCase.ID, userID, snapshot)
	return err
}

func testCaseSelectQuery(whereClause string) string {
	return `SELECT ` + testCaseSelectColumns() + `
		FROM test_cases
	` + whereClause
}

func testCaseSelectColumns() string {
	return `
			id::text,
			workspace_id::text,
			project_id::text,
			title,
			type,
			area,
			priority,
			status,
			source,
			preconditions,
			steps,
			expected_result,
			environment_requirements,
			dependencies,
			tags,
			quality_score,
			quality_findings,
			COALESCE(created_by_user_id::text, ''),
			created_at,
			updated_at
	`
}

func testCaseSelectColumnsForAlias(alias string) string {
	alias = strings.TrimSpace(alias)
	if alias == "" {
		return testCaseSelectColumns()
	}
	return fmt.Sprintf(`
			%s.id::text,
			%s.workspace_id::text,
			%s.project_id::text,
			%s.title,
			%s.type,
			%s.area,
			%s.priority,
			%s.status,
			%s.source,
			%s.preconditions,
			%s.steps,
			%s.expected_result,
			%s.environment_requirements,
			%s.dependencies,
			%s.tags,
			%s.quality_score,
			%s.quality_findings,
			COALESCE(%s.created_by_user_id::text, ''),
			%s.created_at,
			%s.updated_at
	`, alias, alias, alias, alias, alias, alias, alias, alias, alias, alias, alias, alias, alias, alias, alias, alias, alias, alias, alias, alias)
}

func scanTestCase(row scanner) (TestCase, error) {
	var testCase TestCase
	var stepsBytes, dependenciesBytes, tagsBytes, qualityFindingsBytes []byte
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&testCase.ID,
		&testCase.WorkspaceID,
		&testCase.ProjectID,
		&testCase.Title,
		&testCase.Type,
		&testCase.Area,
		&testCase.Priority,
		&testCase.Status,
		&testCase.Source,
		&testCase.Preconditions,
		&stepsBytes,
		&testCase.ExpectedResult,
		&testCase.EnvironmentRequirements,
		&dependenciesBytes,
		&tagsBytes,
		&testCase.QualityScore,
		&qualityFindingsBytes,
		&testCase.CreatedByUserID,
		&createdAt,
		&updatedAt,
	); err != nil {
		return TestCase{}, err
	}
	testCase.Steps = decodeTestCaseSteps(stepsBytes)
	testCase.Dependencies = decodeStringSlice(dependenciesBytes)
	testCase.Tags = decodeStringSlice(tagsBytes)
	testCase.QualityFindings = decodeQualityFindings(qualityFindingsBytes)
	testCase.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	testCase.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return testCase, nil
}

func attachLatestTestCaseResults(ctx context.Context, q queryer, workspaceID, projectID string, cases []TestCase) error {
	if len(cases) == 0 {
		return nil
	}
	caseIDs := make([]string, 0, len(cases))
	indexByCaseID := make(map[string]int, len(cases))
	for index, testCase := range cases {
		caseID := strings.TrimSpace(testCase.ID)
		if caseID == "" {
			continue
		}
		caseIDs = append(caseIDs, caseID)
		indexByCaseID[caseID] = index
	}
	if len(caseIDs) == 0 {
		return nil
	}
	rows, err := q.Query(ctx, `
		SELECT DISTINCT ON (i.case_id)
			i.case_id::text,
			i.id::text,
			i.run_id::text,
			r.status,
			r.source,
			i.status,
			i.actual_result,
			i.failure_summary,
			i.evidence,
			i.updated_at
		FROM test_run_items i
		JOIN test_runs r ON r.id = i.run_id
		WHERE i.workspace_id = $1
			AND i.project_id = $2
			AND i.case_id::text = ANY($3::text[])
			AND i.status IN ('passed', 'failed', 'blocked', 'skipped')
		ORDER BY i.case_id, i.updated_at DESC, r.updated_at DESC, i.id DESC
	`, workspaceID, projectID, caseIDs)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var caseID string
		var latest TestCaseLatestResult
		var evidenceBytes []byte
		var updatedAt time.Time
		if err := rows.Scan(
			&caseID,
			&latest.ItemID,
			&latest.RunID,
			&latest.RunStatus,
			&latest.RunSource,
			&latest.Status,
			&latest.ActualResult,
			&latest.FailureSummary,
			&evidenceBytes,
			&updatedAt,
		); err != nil {
			return err
		}
		latest.Evidence = copyRawMessage(json.RawMessage(evidenceBytes))
		latest.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		if index, ok := indexByCaseID[caseID]; ok {
			copyLatest := latest
			cases[index].LatestResult = &copyLatest
		}
	}
	return rows.Err()
}

func scanTestCaseRevision(row scanner) (TestCaseRevision, error) {
	var revision TestCaseRevision
	var snapshotBytes []byte
	var createdAt time.Time
	var authorUserID sql.NullString
	if err := row.Scan(
		&revision.ID,
		&revision.WorkspaceID,
		&revision.ProjectID,
		&revision.TestCaseID,
		&authorUserID,
		&revision.RevisionNumber,
		&snapshotBytes,
		&createdAt,
	); err != nil {
		return TestCaseRevision{}, err
	}
	if authorUserID.Valid {
		revision.AuthorUserID = authorUserID.String
	}
	revision.Snapshot = decodeTestCaseSnapshot(snapshotBytes)
	revision.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	return revision, nil
}

func testCaseProposalSelectQuery(whereClause string) string {
	return `SELECT
			p.id::text,
			p.workspace_id::text,
			p.project_id::text,
			COALESCE(p.source_issue_id::text, ''),
			p.source_session_id,
			COALESCE(p.target_case_id::text, ''),
			p.proposal_type,
			p.status,
			p.title,
			p.summary,
			p.rationale,
			COALESCE(p.current_case, 'null'::jsonb),
			p.proposed_case,
			p.quality_score,
			p.quality_findings,
			p.validation_errors,
			COALESCE(p.created_by_user_id::text, ''),
			COALESCE(p.reviewed_by_user_id::text, ''),
			COALESCE(p.applied_case_id::text, ''),
			p.review_note,
			p.reviewed_at,
			p.created_at,
			p.updated_at
		FROM test_case_proposals p
	` + whereClause
}

func scanTestCaseProposal(row scanner) (TestCaseProposal, error) {
	var proposal TestCaseProposal
	var currentCaseBytes, proposedCaseBytes, findingsBytes, validationErrorsBytes []byte
	var reviewedAt sql.NullTime
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&proposal.ID,
		&proposal.WorkspaceID,
		&proposal.ProjectID,
		&proposal.SourceIssueID,
		&proposal.SourceSessionID,
		&proposal.TargetCaseID,
		&proposal.ProposalType,
		&proposal.Status,
		&proposal.Title,
		&proposal.Summary,
		&proposal.Rationale,
		&currentCaseBytes,
		&proposedCaseBytes,
		&proposal.QualityScore,
		&findingsBytes,
		&validationErrorsBytes,
		&proposal.CreatedByUserID,
		&proposal.ReviewedByUserID,
		&proposal.AppliedCaseID,
		&proposal.ReviewNote,
		&reviewedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return TestCaseProposal{}, err
	}
	current := decodeTestCaseSnapshot(currentCaseBytes)
	if current.ID != "" {
		proposal.CurrentCase = &current
	}
	proposal.ProposedCase = decodeTestCaseInput(proposedCaseBytes)
	proposal.QualityFindings = decodeQualityFindings(findingsBytes)
	proposal.ValidationErrors = decodeStringSlice(validationErrorsBytes)
	if reviewedAt.Valid {
		proposal.ReviewedAt = reviewedAt.Time.UTC().Format(time.RFC3339)
	}
	proposal.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	proposal.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return proposal, nil
}

func loadTestCaseProposal(ctx context.Context, q queryer, workspaceID, projectID, proposalID string) (TestCaseProposal, error) {
	row := q.QueryRow(ctx, testCaseProposalSelectQuery(`
		WHERE p.workspace_id = $1 AND p.project_id = $2 AND p.id = $3
	`), strings.TrimSpace(workspaceID), strings.TrimSpace(projectID), strings.TrimSpace(proposalID))
	proposal, err := scanTestCaseProposal(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return TestCaseProposal{}, ErrNotFound
	}
	return proposal, err
}

func insertTestCaseProposalRecord(ctx context.Context, q queryer, proposal TestCaseProposal) (TestCaseProposal, error) {
	currentCase, err := encodeJSON(proposal.CurrentCase)
	if err != nil {
		return TestCaseProposal{}, err
	}
	proposedCase, err := encodeJSON(copyTestCaseInput(proposal.ProposedCase))
	if err != nil {
		return TestCaseProposal{}, err
	}
	findings, err := encodeJSON(proposal.QualityFindings)
	if err != nil {
		return TestCaseProposal{}, err
	}
	validationErrors, err := encodeJSON(proposal.ValidationErrors)
	if err != nil {
		return TestCaseProposal{}, err
	}
	var proposalID string
	err = q.QueryRow(ctx, `
		INSERT INTO test_case_proposals (
			workspace_id,
			project_id,
			source_issue_id,
			source_session_id,
			target_case_id,
			proposal_type,
			status,
			title,
			summary,
			rationale,
			current_case,
			proposed_case,
			quality_score,
			quality_findings,
			validation_errors,
			created_by_user_id
		)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, NULLIF($5, '')::uuid, $6, $7, $8, $9, $10, NULLIF($11::jsonb, 'null'::jsonb), $12::jsonb, $13, $14::jsonb, $15::jsonb, NULLIF($16, '')::uuid)
		RETURNING id::text
	`, proposal.WorkspaceID, proposal.ProjectID, proposal.SourceIssueID, proposal.SourceSessionID, proposal.TargetCaseID, proposal.ProposalType, proposal.Status, proposal.Title, proposal.Summary, proposal.Rationale, currentCase, proposedCase, proposal.QualityScore, findings, validationErrors, proposal.CreatedByUserID).Scan(&proposalID)
	if err != nil {
		return TestCaseProposal{}, err
	}
	return loadTestCaseProposal(ctx, q, proposal.WorkspaceID, proposal.ProjectID, proposalID)
}

func reviewTestCaseProposalRecord(ctx context.Context, q queryer, workspaceID, projectID, proposalID, status, userID, appliedCaseID, note string) (TestCaseProposal, error) {
	tag, err := q.Exec(ctx, `
		UPDATE test_case_proposals
		SET status = $4,
			reviewed_by_user_id = NULLIF($5, '')::uuid,
			applied_case_id = NULLIF($6, '')::uuid,
			review_note = $7,
			reviewed_at = now(),
			updated_at = now()
		WHERE workspace_id = $1 AND project_id = $2 AND id = $3
	`, strings.TrimSpace(workspaceID), strings.TrimSpace(projectID), strings.TrimSpace(proposalID), status, strings.TrimSpace(userID), strings.TrimSpace(appliedCaseID), normalizeReviewNote(note))
	if err != nil {
		return TestCaseProposal{}, err
	}
	if tag.RowsAffected() == 0 {
		return TestCaseProposal{}, ErrNotFound
	}
	return loadTestCaseProposal(ctx, q, workspaceID, projectID, proposalID)
}

func testPlanSelectQuery(whereClause string) string {
	return `SELECT
			p.id::text,
			p.workspace_id::text,
			p.project_id::text,
			p.title,
			p.description,
			p.setup_steps,
			p.status,
			p.target_type,
			p.target_value,
			p.environment,
			p.environment_id,
			p.environment_kind,
			p.environment_snapshot,
			(
				SELECT count(*)
				FROM test_plan_cases pc
				WHERE pc.plan_id = p.id
			)::int,
			COALESCE(p.created_by_user_id::text, ''),
			p.created_at,
			p.updated_at
		FROM test_plans p
	` + whereClause
}

func scanTestPlan(row scanner) (TestPlan, error) {
	var plan TestPlan
	var snapshotBytes []byte
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&plan.ID,
		&plan.WorkspaceID,
		&plan.ProjectID,
		&plan.Title,
		&plan.Description,
		&plan.SetupSteps,
		&plan.Status,
		&plan.TargetType,
		&plan.TargetValue,
		&plan.Environment,
		&plan.EnvironmentID,
		&plan.EnvironmentKind,
		&snapshotBytes,
		&plan.CaseCount,
		&plan.CreatedByUserID,
		&createdAt,
		&updatedAt,
	); err != nil {
		return TestPlan{}, err
	}
	plan.EnvironmentSnapshot = copyRawMessage(json.RawMessage(snapshotBytes))
	plan.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	plan.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return plan, nil
}

func loadTestPlan(ctx context.Context, q queryer, workspaceID, projectID, planID string) (TestPlan, error) {
	row := q.QueryRow(ctx, testPlanSelectQuery(`
		WHERE p.workspace_id = $1 AND p.project_id = $2 AND p.id = $3
	`), strings.TrimSpace(workspaceID), strings.TrimSpace(projectID), strings.TrimSpace(planID))
	plan, err := scanTestPlan(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return TestPlan{}, ErrNotFound
	}
	return plan, err
}

func (s *PostgresStore) resolveTestPlanEnvironment(ctx context.Context, workspaceID string, input *TestPlanInput) error {
	if input == nil || strings.TrimSpace(input.EnvironmentID) == "" {
		if input != nil {
			input.EnvironmentKind = ""
			input.EnvironmentSnapshot = json.RawMessage(`{}`)
		}
		return nil
	}
	environment, err := s.loadEnvironment(ctx, workspaceID, input.EnvironmentID)
	if err != nil {
		return err
	}
	if input.EnvironmentKind != "" && input.EnvironmentKind != environment.Kind {
		return errors.New("environmentKind does not match selected environment")
	}
	input.EnvironmentID = environment.ID
	input.EnvironmentKind = environment.Kind
	input.EnvironmentSnapshot = environmentSnapshot(environment)
	if strings.TrimSpace(input.Environment) == "" {
		input.Environment = environment.Name
	}
	return nil
}

func (s *PostgresStore) resolveTestRunEnvironment(ctx context.Context, workspaceID string, input *CreateTestRunInput) error {
	if input == nil || strings.TrimSpace(input.EnvironmentID) == "" {
		if input != nil {
			input.EnvironmentKind = ""
			input.EnvironmentSnapshot = json.RawMessage(`{}`)
		}
		return nil
	}
	environment, err := s.loadEnvironment(ctx, workspaceID, input.EnvironmentID)
	if err != nil {
		return err
	}
	if input.EnvironmentKind != "" && input.EnvironmentKind != environment.Kind {
		return errors.New("environmentKind does not match selected environment")
	}
	input.EnvironmentID = environment.ID
	input.EnvironmentKind = environment.Kind
	input.EnvironmentSnapshot = environmentSnapshot(environment)
	if strings.TrimSpace(input.Environment) == "" {
		input.Environment = environment.Name
	}
	return nil
}

func insertTestPlanRecord(ctx context.Context, q queryer, workspaceID, projectID, userID string, input TestPlanInput) (TestPlan, error) {
	var planID string
	if err := q.QueryRow(ctx, `
		INSERT INTO test_plans (workspace_id, project_id, title, description, setup_steps, status, target_type, target_value, environment, environment_id, environment_kind, environment_snapshot, created_by_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, NULLIF($13, '')::uuid)
		RETURNING id::text
	`, workspaceID, projectID, input.Title, input.Description, input.SetupSteps, input.Status, input.TargetType, input.TargetValue, input.Environment, input.EnvironmentID, input.EnvironmentKind, jsonOrObject(input.EnvironmentSnapshot), userID).Scan(&planID); err != nil {
		return TestPlan{}, err
	}
	return loadTestPlan(ctx, q, workspaceID, projectID, planID)
}

func updateTestPlanRecord(ctx context.Context, q queryer, workspaceID, projectID, planID string, input TestPlanInput) (TestPlan, error) {
	tag, err := q.Exec(ctx, `
		UPDATE test_plans
		SET title = $4,
			description = $5,
			setup_steps = $6,
			status = $7,
			target_type = $8,
			target_value = $9,
			environment = $10,
			environment_id = $11,
			environment_kind = $12,
			environment_snapshot = $13::jsonb,
			updated_at = now()
		WHERE workspace_id = $1 AND project_id = $2 AND id = $3
	`, workspaceID, projectID, planID, input.Title, input.Description, input.SetupSteps, input.Status, input.TargetType, input.TargetValue, input.Environment, input.EnvironmentID, input.EnvironmentKind, jsonOrObject(input.EnvironmentSnapshot))
	if err != nil {
		return TestPlan{}, err
	}
	if tag.RowsAffected() == 0 {
		return TestPlan{}, ErrNotFound
	}
	return loadTestPlan(ctx, q, workspaceID, projectID, planID)
}

func ensureReadyTestCasesForPlan(ctx context.Context, q queryer, workspaceID, projectID string, caseIDs []string) error {
	for _, caseID := range caseIDs {
		testCase, err := loadProjectTestCase(ctx, q, workspaceID, projectID, caseID)
		if err != nil {
			return err
		}
		if testCase.Status != "ready" {
			return errors.New("test plan can only include ready test cases")
		}
	}
	return nil
}

func replaceTestPlanCaseRecords(ctx context.Context, q queryer, plan TestPlan, caseIDs []string) error {
	if _, err := q.Exec(ctx, `DELETE FROM test_plan_cases WHERE workspace_id = $1 AND project_id = $2 AND plan_id = $3`, plan.WorkspaceID, plan.ProjectID, plan.ID); err != nil {
		return err
	}
	for index, caseID := range caseIDs {
		if _, err := q.Exec(ctx, `
			INSERT INTO test_plan_cases (workspace_id, project_id, plan_id, case_id, sort_order)
			VALUES ($1, $2, $3, $4, $5)
		`, plan.WorkspaceID, plan.ProjectID, plan.ID, strings.TrimSpace(caseID), index+1); err != nil {
			return err
		}
	}
	return nil
}

func listTestPlanCases(ctx context.Context, q queryer, workspaceID, projectID, planID string) ([]TestPlanCase, error) {
	rows, err := q.Query(ctx, `
		SELECT
			pc.id::text,
			pc.workspace_id::text,
			pc.project_id::text,
			pc.plan_id::text,
			pc.case_id::text,
			pc.sort_order,
			`+testCaseSelectColumnsForAlias("tc")+`
		FROM test_plan_cases pc
		JOIN test_cases tc ON tc.id = pc.case_id
		WHERE pc.workspace_id = $1 AND pc.project_id = $2 AND pc.plan_id = $3
		ORDER BY pc.sort_order ASC, pc.created_at ASC
	`, strings.TrimSpace(workspaceID), strings.TrimSpace(projectID), strings.TrimSpace(planID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cases := []TestPlanCase{}
	for rows.Next() {
		planCase, err := scanTestPlanCase(rows)
		if err != nil {
			return nil, err
		}
		cases = append(cases, planCase)
	}
	return cases, rows.Err()
}

func scanTestPlanCase(row scanner) (TestPlanCase, error) {
	var planCase TestPlanCase
	if err := scanTestPlanCaseInto(row, &planCase); err != nil {
		return TestPlanCase{}, err
	}
	return planCase, nil
}

func scanTestPlanCaseInto(row scanner, planCase *TestPlanCase) error {
	var stepsBytes, dependenciesBytes, tagsBytes, qualityFindingsBytes []byte
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&planCase.ID,
		&planCase.WorkspaceID,
		&planCase.ProjectID,
		&planCase.PlanID,
		&planCase.TestCaseID,
		&planCase.SortOrder,
		&planCase.TestCase.ID,
		&planCase.TestCase.WorkspaceID,
		&planCase.TestCase.ProjectID,
		&planCase.TestCase.Title,
		&planCase.TestCase.Type,
		&planCase.TestCase.Area,
		&planCase.TestCase.Priority,
		&planCase.TestCase.Status,
		&planCase.TestCase.Source,
		&planCase.TestCase.Preconditions,
		&stepsBytes,
		&planCase.TestCase.ExpectedResult,
		&planCase.TestCase.EnvironmentRequirements,
		&dependenciesBytes,
		&tagsBytes,
		&planCase.TestCase.QualityScore,
		&qualityFindingsBytes,
		&planCase.TestCase.CreatedByUserID,
		&createdAt,
		&updatedAt,
	); err != nil {
		return err
	}
	planCase.TestCase.Steps = decodeTestCaseSteps(stepsBytes)
	planCase.TestCase.Dependencies = decodeStringSlice(dependenciesBytes)
	planCase.TestCase.Tags = decodeStringSlice(tagsBytes)
	planCase.TestCase.QualityFindings = decodeQualityFindings(qualityFindingsBytes)
	planCase.TestCase.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	planCase.TestCase.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return nil
}

func listTestPlanCaseSnapshots(ctx context.Context, q queryer, workspaceID, projectID, planID string) ([]TestCase, error) {
	planCases, err := listTestPlanCases(ctx, q, workspaceID, projectID, planID)
	if err != nil {
		return nil, err
	}
	cases := make([]TestCase, 0, len(planCases))
	for _, planCase := range planCases {
		cases = append(cases, testCaseSnapshot(planCase.TestCase))
	}
	return cases, nil
}

func listReadyProjectTestCaseSnapshots(ctx context.Context, q queryer, workspaceID, projectID string, caseIDs []string) ([]TestCase, error) {
	cases := make([]TestCase, 0, len(caseIDs))
	for _, caseID := range caseIDs {
		testCase, err := loadProjectTestCase(ctx, q, workspaceID, projectID, caseID)
		if err != nil {
			return nil, err
		}
		if testCase.Status != "ready" {
			return nil, errors.New("test run can only include ready test cases")
		}
		cases = append(cases, testCaseSnapshot(testCase))
	}
	return cases, nil
}

func listTestRunsForPlan(ctx context.Context, q queryer, workspaceID, projectID, planID string) ([]TestRun, error) {
	rows, err := q.Query(ctx, testRunSelectQuery(`
		WHERE r.workspace_id = $1 AND r.project_id = $2 AND r.plan_id = $3
		ORDER BY r.updated_at DESC, r.created_at DESC
	`), strings.TrimSpace(workspaceID), strings.TrimSpace(projectID), strings.TrimSpace(planID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []TestRun{}
	for rows.Next() {
		run, err := scanTestRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func testRunSelectQuery(whereClause string) string {
	return `SELECT
			` + testRunSelectColumnsForAlias("r") + `
		FROM test_runs r
	` + whereClause
}

func testRunSelectColumnsForAlias(alias string) string {
	return alias + `.id::text,
			` + alias + `.workspace_id::text,
			` + alias + `.project_id::text,
			COALESCE(` + alias + `.plan_id::text, ''),
			COALESCE(` + alias + `.source, 'plan'),
			COALESCE(` + alias + `.parent_issue_id::text, ''),
			` + alias + `.status,
			` + alias + `.setup_steps,
			` + alias + `.setup_status,
			COALESCE(` + alias + `.setup_issue_id::text, ''),
			` + alias + `.setup_session_id,
			` + alias + `.setup_result,
			` + alias + `.run_context,
			` + alias + `.target_type,
			` + alias + `.target_value,
			` + alias + `.environment,
			` + alias + `.environment_id,
			` + alias + `.environment_kind,
			` + alias + `.environment_snapshot,
			` + alias + `.total_count,
			` + alias + `.passed_count,
			` + alias + `.failed_count,
			` + alias + `.blocked_count,
			` + alias + `.skipped_count,
			` + alias + `.acceptance_status,
			` + alias + `.acceptance_note,
			COALESCE(` + alias + `.created_by_user_id::text, ''),
			COALESCE(` + alias + `.accepted_by_user_id::text, ''),
			` + alias + `.completed_at,
			` + alias + `.accepted_at,
			` + alias + `.created_at,
			` + alias + `.updated_at`
}

func scanTestRun(row scanner) (TestRun, error) {
	var run TestRun
	var snapshotBytes, setupResultBytes, runContextBytes []byte
	var completedAt, acceptedAt sql.NullTime
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&run.ID,
		&run.WorkspaceID,
		&run.ProjectID,
		&run.PlanID,
		&run.Source,
		&run.ParentIssueID,
		&run.Status,
		&run.SetupSteps,
		&run.SetupStatus,
		&run.SetupIssueID,
		&run.SetupSessionID,
		&setupResultBytes,
		&runContextBytes,
		&run.TargetType,
		&run.TargetValue,
		&run.Environment,
		&run.EnvironmentID,
		&run.EnvironmentKind,
		&snapshotBytes,
		&run.TotalCount,
		&run.PassedCount,
		&run.FailedCount,
		&run.BlockedCount,
		&run.SkippedCount,
		&run.AcceptanceStatus,
		&run.AcceptanceNote,
		&run.CreatedByUserID,
		&run.AcceptedByUserID,
		&completedAt,
		&acceptedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return TestRun{}, err
	}
	run.SetupResult = copyRawMessage(json.RawMessage(setupResultBytes))
	if len(run.SetupResult) == 0 {
		run.SetupResult = json.RawMessage(`{}`)
	}
	run.RunContext = copyRawMessage(json.RawMessage(runContextBytes))
	if len(run.RunContext) == 0 {
		run.RunContext = json.RawMessage(`{}`)
	}
	run.EnvironmentSnapshot = copyRawMessage(json.RawMessage(snapshotBytes))
	if completedAt.Valid {
		run.CompletedAt = completedAt.Time.UTC().Format(time.RFC3339)
	}
	if acceptedAt.Valid {
		run.AcceptedAt = acceptedAt.Time.UTC().Format(time.RFC3339)
	}
	run.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	run.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return run, nil
}

func loadTestRun(ctx context.Context, q queryer, workspaceID, projectID, runID string) (TestRun, error) {
	row := q.QueryRow(ctx, testRunSelectQuery(`
		WHERE r.workspace_id = $1 AND r.project_id = $2 AND r.id = $3
	`), strings.TrimSpace(workspaceID), strings.TrimSpace(projectID), strings.TrimSpace(runID))
	run, err := scanTestRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return TestRun{}, ErrNotFound
	}
	return run, err
}

func insertTestRunRecord(ctx context.Context, q queryer, run TestRun, parentIssueID string) (TestRun, error) {
	var runID string
	if err := q.QueryRow(ctx, `
		INSERT INTO test_runs (
			id,
			workspace_id,
			project_id,
			plan_id,
			source,
			parent_issue_id,
			status,
			setup_steps,
			setup_status,
			setup_result,
			run_context,
			target_type,
			target_value,
			environment,
			environment_id,
			environment_kind,
			environment_snapshot,
			total_count,
			acceptance_status,
			created_by_user_id
		)
		VALUES (COALESCE(NULLIF($1, '')::uuid, gen_random_uuid()), $2, $3, NULLIF($4, '')::uuid, $5, NULLIF($6, '')::uuid, $7, $8, $9, $10::jsonb, $11::jsonb, $12, $13, $14, $15, $16::jsonb, $17, $18, NULLIF($19, '')::uuid)
		RETURNING id::text
	`, run.ID, run.WorkspaceID, run.ProjectID, run.PlanID, firstNonEmpty(run.Source, "ad_hoc"), parentIssueID, run.Status, run.SetupSteps, firstNonEmpty(run.SetupStatus, "not_required"), jsonOrObject(run.SetupResult), jsonOrObject(run.RunContext), run.TargetType, run.TargetValue, run.Environment, run.EnvironmentID, run.EnvironmentKind, jsonOrObject(run.EnvironmentSnapshot), run.TotalCount, run.AcceptanceStatus, run.CreatedByUserID).Scan(&runID); err != nil {
		return TestRun{}, err
	}
	return loadTestRun(ctx, q, run.WorkspaceID, run.ProjectID, runID)
}

func insertTestRunItems(ctx context.Context, q queryer, run TestRun, cases []TestCase) error {
	for _, testCase := range cases {
		if _, err := q.Exec(ctx, `
			INSERT INTO test_run_items (workspace_id, project_id, run_id, case_id, status, evidence)
			VALUES ($1, $2, $3, $4, 'queued', '{}'::jsonb)
			ON CONFLICT(run_id, case_id) DO NOTHING
		`, run.WorkspaceID, run.ProjectID, run.ID, testCase.ID); err != nil {
			return err
		}
	}
	return nil
}

func listTestRunItems(ctx context.Context, q queryer, workspaceID, projectID, runID string) ([]TestRunItem, error) {
	rows, err := q.Query(ctx, `
		SELECT
			i.id::text,
			i.workspace_id::text,
			i.project_id::text,
			i.run_id::text,
			i.case_id::text,
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
		WHERE i.workspace_id = $1 AND i.project_id = $2 AND i.run_id = $3
		ORDER BY i.created_at ASC, i.id ASC
	`, strings.TrimSpace(workspaceID), strings.TrimSpace(projectID), strings.TrimSpace(runID))
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

func scanTestRunItem(row scanner) (TestRunItem, error) {
	var item TestRunItem
	var evidenceBytes []byte
	var itemCreatedAt, itemUpdatedAt time.Time
	var stepsBytes, dependenciesBytes, tagsBytes, qualityFindingsBytes []byte
	var caseCreatedAt, caseUpdatedAt time.Time
	if err := row.Scan(
		&item.ID,
		&item.WorkspaceID,
		&item.ProjectID,
		&item.RunID,
		&item.TestCaseID,
		&item.ExecutionIssueID,
		&item.AgentSessionID,
		&item.Status,
		&item.ActualResult,
		&item.FailureSummary,
		&evidenceBytes,
		&itemCreatedAt,
		&itemUpdatedAt,
		&item.TestCase.ID,
		&item.TestCase.WorkspaceID,
		&item.TestCase.ProjectID,
		&item.TestCase.Title,
		&item.TestCase.Type,
		&item.TestCase.Area,
		&item.TestCase.Priority,
		&item.TestCase.Status,
		&item.TestCase.Source,
		&item.TestCase.Preconditions,
		&stepsBytes,
		&item.TestCase.ExpectedResult,
		&item.TestCase.EnvironmentRequirements,
		&dependenciesBytes,
		&tagsBytes,
		&item.TestCase.QualityScore,
		&qualityFindingsBytes,
		&item.TestCase.CreatedByUserID,
		&caseCreatedAt,
		&caseUpdatedAt,
	); err != nil {
		return TestRunItem{}, err
	}
	item.Evidence = copyRawMessage(json.RawMessage(evidenceBytes))
	if len(item.Evidence) == 0 {
		item.Evidence = json.RawMessage(`{}`)
	}
	item.CreatedAt = itemCreatedAt.UTC().Format(time.RFC3339)
	item.UpdatedAt = itemUpdatedAt.UTC().Format(time.RFC3339)
	item.TestCase.Steps = decodeTestCaseSteps(stepsBytes)
	item.TestCase.Dependencies = decodeStringSlice(dependenciesBytes)
	item.TestCase.Tags = decodeStringSlice(tagsBytes)
	item.TestCase.QualityFindings = decodeQualityFindings(qualityFindingsBytes)
	item.TestCase.CreatedAt = caseCreatedAt.UTC().Format(time.RFC3339)
	item.TestCase.UpdatedAt = caseUpdatedAt.UTC().Format(time.RFC3339)
	return item, nil
}

func scanTestCaseRunItem(row scanner) (TestCaseRunItem, error) {
	var item TestRunItem
	var run TestRun
	var evidenceBytes, runSetupResultBytes, runContextBytes, runEnvironmentSnapshotBytes []byte
	var itemCreatedAt, itemUpdatedAt time.Time
	var stepsBytes, dependenciesBytes, tagsBytes, qualityFindingsBytes []byte
	var caseCreatedAt, caseUpdatedAt time.Time
	var completedAt, acceptedAt sql.NullTime
	var runCreatedAt, runUpdatedAt time.Time
	if err := row.Scan(
		&item.ID,
		&item.WorkspaceID,
		&item.ProjectID,
		&item.RunID,
		&item.TestCaseID,
		&item.ExecutionIssueID,
		&item.AgentSessionID,
		&item.Status,
		&item.ActualResult,
		&item.FailureSummary,
		&evidenceBytes,
		&itemCreatedAt,
		&itemUpdatedAt,
		&item.TestCase.ID,
		&item.TestCase.WorkspaceID,
		&item.TestCase.ProjectID,
		&item.TestCase.Title,
		&item.TestCase.Type,
		&item.TestCase.Area,
		&item.TestCase.Priority,
		&item.TestCase.Status,
		&item.TestCase.Source,
		&item.TestCase.Preconditions,
		&stepsBytes,
		&item.TestCase.ExpectedResult,
		&item.TestCase.EnvironmentRequirements,
		&dependenciesBytes,
		&tagsBytes,
		&item.TestCase.QualityScore,
		&qualityFindingsBytes,
		&item.TestCase.CreatedByUserID,
		&caseCreatedAt,
		&caseUpdatedAt,
		&run.ID,
		&run.WorkspaceID,
		&run.ProjectID,
		&run.PlanID,
		&run.Source,
		&run.ParentIssueID,
		&run.Status,
		&run.SetupSteps,
		&run.SetupStatus,
		&run.SetupIssueID,
		&run.SetupSessionID,
		&runSetupResultBytes,
		&runContextBytes,
		&run.TargetType,
		&run.TargetValue,
		&run.Environment,
		&run.EnvironmentID,
		&run.EnvironmentKind,
		&runEnvironmentSnapshotBytes,
		&run.TotalCount,
		&run.PassedCount,
		&run.FailedCount,
		&run.BlockedCount,
		&run.SkippedCount,
		&run.AcceptanceStatus,
		&run.AcceptanceNote,
		&run.CreatedByUserID,
		&run.AcceptedByUserID,
		&completedAt,
		&acceptedAt,
		&runCreatedAt,
		&runUpdatedAt,
	); err != nil {
		return TestCaseRunItem{}, err
	}
	item.Evidence = copyRawMessage(json.RawMessage(evidenceBytes))
	item.CreatedAt = itemCreatedAt.UTC().Format(time.RFC3339)
	item.UpdatedAt = itemUpdatedAt.UTC().Format(time.RFC3339)
	item.TestCase.Steps = decodeTestCaseSteps(stepsBytes)
	item.TestCase.Dependencies = decodeStringSlice(dependenciesBytes)
	item.TestCase.Tags = decodeStringSlice(tagsBytes)
	item.TestCase.QualityFindings = decodeQualityFindings(qualityFindingsBytes)
	item.TestCase.CreatedAt = caseCreatedAt.UTC().Format(time.RFC3339)
	item.TestCase.UpdatedAt = caseUpdatedAt.UTC().Format(time.RFC3339)
	run.SetupResult = copyRawMessage(json.RawMessage(runSetupResultBytes))
	if len(run.SetupResult) == 0 {
		run.SetupResult = json.RawMessage(`{}`)
	}
	run.RunContext = copyRawMessage(json.RawMessage(runContextBytes))
	if len(run.RunContext) == 0 {
		run.RunContext = json.RawMessage(`{}`)
	}
	run.EnvironmentSnapshot = copyRawMessage(json.RawMessage(runEnvironmentSnapshotBytes))
	if completedAt.Valid {
		run.CompletedAt = completedAt.Time.UTC().Format(time.RFC3339)
	}
	if acceptedAt.Valid {
		run.AcceptedAt = acceptedAt.Time.UTC().Format(time.RFC3339)
	}
	run.CreatedAt = runCreatedAt.UTC().Format(time.RFC3339)
	run.UpdatedAt = runUpdatedAt.UTC().Format(time.RFC3339)
	return TestCaseRunItem{Item: item, Run: run}, nil
}

func resetTestRunItemsForRetry(ctx context.Context, q queryer, workspaceID, projectID, runID string, itemIDs []string) error {
	if len(itemIDs) == 0 {
		_, err := q.Exec(ctx, `
			UPDATE test_run_items
			SET status = 'queued',
				actual_result = '',
				failure_summary = '',
				evidence = '{}'::jsonb,
				agent_session_id = '',
				updated_at = now()
			WHERE workspace_id = $1
				AND project_id = $2
				AND run_id = $3
				AND status IN ('failed', 'blocked')
		`, workspaceID, projectID, runID)
		return err
	}
	for _, itemID := range itemIDs {
		tag, err := q.Exec(ctx, `
			UPDATE test_run_items
			SET status = 'queued',
				actual_result = '',
				failure_summary = '',
				evidence = '{}'::jsonb,
				agent_session_id = '',
				updated_at = now()
			WHERE workspace_id = $1
				AND project_id = $2
				AND run_id = $3
				AND (id::text = $4 OR case_id::text = $4)
		`, workspaceID, projectID, runID, strings.TrimSpace(itemID))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
	}
	return nil
}

func reviewTestRunRecord(ctx context.Context, q queryer, userID, workspaceID, projectID, runID, status, note string) (TestRun, error) {
	tag, err := q.Exec(ctx, `
		UPDATE test_runs
		SET status = $4,
			acceptance_status = $4,
			acceptance_note = $5,
			accepted_by_user_id = NULLIF($6, '')::uuid,
			accepted_at = now(),
			updated_at = now()
		WHERE workspace_id = $1 AND project_id = $2 AND id = $3
	`, strings.TrimSpace(workspaceID), strings.TrimSpace(projectID), strings.TrimSpace(runID), status, normalizeReviewNote(note), strings.TrimSpace(userID))
	if err != nil {
		return TestRun{}, err
	}
	if tag.RowsAffected() == 0 {
		return TestRun{}, ErrNotFound
	}
	return loadTestRun(ctx, q, workspaceID, projectID, runID)
}

func updateTestRunCounts(ctx context.Context, q queryer, workspaceID, projectID, runID string) error {
	items, err := listTestRunItems(ctx, q, workspaceID, projectID, runID)
	if err != nil {
		return err
	}
	passed, failed, blocked, skipped := testRunCounts(items)
	runStatus := "running"
	finalCount := passed + failed + blocked + skipped
	markCompleted := false
	if len(items) > 0 && finalCount >= len(items) {
		runStatus = "needs_acceptance"
		markCompleted = true
	}
	_, err = q.Exec(ctx, `
		UPDATE test_runs
		SET status = $4,
			total_count = $5,
			passed_count = $6,
			failed_count = $7,
			blocked_count = $8,
			skipped_count = $9,
			completed_at = CASE WHEN $10 THEN now() ELSE completed_at END,
			updated_at = now()
		WHERE workspace_id = $1 AND project_id = $2 AND id = $3
	`, workspaceID, projectID, runID, runStatus, len(items), passed, failed, blocked, skipped, markCompleted)
	return err
}

func (s *PostgresStore) startPostgresTestRunExecutionSessions(ctx Context, userID string, run TestRun, input CreateTestRunInput) error {
	return s.startPostgresTestRunExecutionSessionsWithQueryer(asContext(ctx), s.pool, userID, run, input)
}

func (s *PostgresStore) startPostgresTestRunExecutionSessionsWithQueryer(ctx context.Context, q queryer, userID string, run TestRun, input CreateTestRunInput) error {
	if input.BatchSize <= 0 {
		input.BatchSize = defaultTestRunBatchSize
	}
	if input.BatchSize > maxTestRunBatchSize {
		input.BatchSize = maxTestRunBatchSize
	}
	input.AgentProfile = normalizeAgentProfile(input.AgentProfile)
	if strings.TrimSpace(input.RuntimeMode) == "" {
		input.RuntimeMode = "team"
	}
	if err := s.ensureActiveCodexWorkerForQueryer(ctx, q, run.WorkspaceID, userID, input.RuntimeMode); err != nil {
		return err
	}
	items, err := listTestRunItems(ctx, q, run.WorkspaceID, run.ProjectID, run.ID)
	if err != nil {
		return err
	}
	var plan *TestPlan
	if run.PlanID != "" {
		loadedPlan, err := loadTestPlan(ctx, q, run.WorkspaceID, run.ProjectID, run.PlanID)
		if err != nil {
			return err
		}
		plan = &loadedPlan
	}
	queued := []TestRunItem{}
	for _, item := range items {
		if item.Status == "queued" {
			queued = append(queued, item)
		}
	}
	for start := 0; start < len(queued); start += input.BatchSize {
		end := start + input.BatchSize
		if end > len(queued) {
			end = len(queued)
		}
		batch := queued[start:end]
		cases := make([]TestCase, 0, len(batch))
		for _, item := range batch {
			cases = append(cases, item.TestCase)
		}
		body := buildTestRunExecutionIssueBody(run, cases)
		child, err := createTestRunChildIssue(ctx, q, userID, run, fmt.Sprintf("Execute %s batch %d", testRunExecutionScopeLabel(plan, cases), start/input.BatchSize+1), body)
		if err != nil {
			return err
		}
		session, err := createTestRunAgentSessionTask(ctx, q, userID, run.WorkspaceID, child.ID, CreateAgentSessionInput{
			Provider:     "codex",
			AgentProfile: input.AgentProfile,
			RuntimeMode:  input.RuntimeMode,
			Command:      body,
			Automation:   testRunExecutionAutomation,
			TestRunID:    run.ID,
		})
		if err != nil {
			return err
		}
		for _, item := range batch {
			if _, err := q.Exec(ctx, `
				UPDATE test_run_items
				SET execution_issue_id = $4,
					agent_session_id = $5,
					status = 'running',
					updated_at = now()
				WHERE workspace_id = $1 AND project_id = $2 AND id = $3
			`, run.WorkspaceID, run.ProjectID, item.ID, child.ID, session.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *PostgresStore) ensureActiveCodexWorkerForQueryer(ctx context.Context, q queryer, workspaceID, userID, runtimeMode string) error {
	workspace, err := loadWorkspaceForUser(ctx, q, workspaceID, userID)
	if err != nil {
		return err
	}
	runtimeMode = strings.ToLower(strings.TrimSpace(runtimeMode))
	if runtimeMode == "" {
		runtimeMode = workspace.Kind
	}
	if runtimeMode != "personal" && runtimeMode != "team" {
		return errors.New("runtimeMode must be personal or team")
	}
	if runtimeMode != workspace.Kind {
		return ErrForbidden
	}
	hasActiveWorker, err := s.hasActiveCodexWorker(ctx, workspaceID, runtimeMode)
	if err != nil {
		return err
	}
	if !hasActiveWorker {
		return ErrNoActiveCodexWorker
	}
	return nil
}

func (s *PostgresStore) startPostgresTestRunSetupSession(ctx Context, userID string, run TestRun, input CreateTestRunInput) error {
	dbctx := asContext(ctx)
	body := buildTestRunSetupIssueBody(run)
	child, err := s.CreateIssueTask(ctx, userID, run.WorkspaceID, run.ParentIssueID, IssueTaskInput{
		Title: "Prepare test run",
		Body:  body,
	})
	if err != nil {
		return err
	}
	session, err := s.CreateAgentSession(ctx, userID, run.WorkspaceID, child.ID, CreateAgentSessionInput{
		Provider:         "codex",
		AgentProfile:     input.AgentProfile,
		RuntimeMode:      input.RuntimeMode,
		Command:          body,
		Automation:       testRunSetupAutomation,
		TestRunID:        run.ID,
		TestRunBatchSize: input.BatchSize,
	})
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(dbctx, `
		UPDATE test_runs
		SET setup_issue_id = $4,
			setup_session_id = $5,
			setup_status = 'running',
			status = 'setup_running',
			updated_at = now()
		WHERE workspace_id = $1 AND project_id = $2 AND id = $3
	`, run.WorkspaceID, run.ProjectID, run.ID, child.ID, session.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func createTestRunChildIssue(ctx context.Context, q queryer, userID string, run TestRun, title, body string) (IssueListItem, error) {
	parent, err := loadIssue(ctx, q, run.WorkspaceID, run.ParentIssueID)
	if err != nil {
		return IssueListItem{}, err
	}
	var issueID string
	err = q.QueryRow(ctx, `
		WITH next_order AS (
			SELECT COALESCE(MAX(sort_order), 0) + 1 AS sort_order
			FROM issues
			WHERE workspace_id = $1 AND parent_issue_id = $2
		),
		inserted AS (
			INSERT INTO issues (workspace_id, project_id, parent_issue_id, sort_order, title, body, status, triage_status, assignee, assignee_type, creator_user_id, creator_name, creator_avatar_url)
			SELECT $1, NULLIF($3, '')::uuid, $2, sort_order, $4, $5, 'open', 'none', $6, 'human', NULLIF($7, '')::uuid, $6, $8
			FROM next_order
			RETURNING id
		)
		SELECT id::text FROM inserted
	`, run.WorkspaceID, run.ParentIssueID, parent.ProjectID, strings.TrimSpace(title), strings.TrimSpace(body), parent.CreatorName, strings.TrimSpace(userID), parent.CreatorAvatar).Scan(&issueID)
	if err != nil {
		return IssueListItem{}, err
	}
	items, err := listChildIssuesForQueryer(ctx, q, run.WorkspaceID, run.ParentIssueID)
	if err != nil {
		return IssueListItem{}, err
	}
	for _, item := range items {
		if item.ID == issueID {
			return item, nil
		}
	}
	return IssueListItem{}, ErrNotFound
}

func createTestRunAgentSessionTask(ctx context.Context, q queryer, userID, workspaceID, issueID string, input CreateAgentSessionInput) (AgentSession, error) {
	normalized := normalizeCreateAgentSessionInput(input)
	if normalized.Command == "" {
		return AgentSession{}, errors.New("command is required")
	}
	issue, err := loadIssue(ctx, q, workspaceID, issueID)
	if err != nil {
		return AgentSession{}, err
	}
	if issue.ProjectID == "" {
		return AgentSession{}, errors.New("attach a project before starting an agent session")
	}
	project, err := resolveIssueProject(ctx, q, workspaceID, issue.ProjectID, "")
	if err != nil {
		return AgentSession{}, err
	}
	sessionID, err := newAgentSessionID()
	if err != nil {
		return AgentSession{}, err
	}
	if normalized.Branch == "" {
		normalized.Branch = defaultAgentSessionBranch(issueID, sessionID)
	}
	runbook, _ := loadProjectRunbookSnapshot(ctx, q, workspaceID, project.ID)
	comments, err := listIssueCommentsForQueryer(ctx, q, workspaceID, issueID, userID)
	if err != nil {
		return AgentSession{}, err
	}
	labels, err := listIssueLabels(ctx, q, workspaceID, issueID)
	if err != nil {
		return AgentSession{}, err
	}
	childIssues, err := listChildIssuesForQueryer(ctx, q, workspaceID, issueID)
	if err != nil {
		return AgentSession{}, err
	}
	payload, err := json.Marshal(buildAgentSessionPayload(sessionID, issue, project, runbook, comments, labels, childIssues, normalized))
	if err != nil {
		return AgentSession{}, err
	}
	capabilities, err := json.Marshal(map[string]bool{"codex": true})
	if err != nil {
		return AgentSession{}, err
	}
	task, err := insertRuntimeTaskRecord(ctx, q, workspaceID, userID, CreateRuntimeTaskInput{
		IssueID:              issue.ID,
		SessionID:            sessionID,
		ProjectID:            project.ID,
		Kind:                 "agent_session",
		Priority:             0,
		RuntimeMode:          normalized.RuntimeMode,
		RequiredCapabilities: capabilities,
		Payload:              payload,
	})
	if err != nil {
		return AgentSession{}, err
	}
	return runtimeTaskToAgentSession(task)
}
