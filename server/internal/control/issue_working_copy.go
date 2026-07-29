package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	agentSessionExecutionModeWorkingCopy = "issue_working_copy"
	agentSessionExecutionModeDetached    = "detached"

	workingCopyStateUninitialized    = "uninitialized"
	workingCopyStateClean            = "clean"
	workingCopyStateDirty            = "dirty"
	workingCopyStateRecoveryRequired = "recovery_required"
)

type issueWorkingCopyTaskPayload struct {
	ExecutionMode         string `json:"executionMode"`
	WorkingCopyGeneration int64  `json:"workingCopyGeneration"`
	ExpectedHeadSHA       string `json:"expectedHeadSha"`
	Branch                string `json:"branch"`
	Initialize            bool   `json:"initialize"`
}

type issueWorkingCopyResultEnvelope struct {
	StorageID      string `json:"storageId"`
	Branch         string `json:"branch"`
	BaseCommitSHA  string `json:"baseCommitSha"`
	HeadCommitSHA  string `json:"headCommitSha"`
	ContentState   string `json:"contentState"`
	RecoveryReason string `json:"recoveryReason"`
	Generation     int64  `json:"generation"`
}

func agentSessionExecutionMode(input CreateAgentSessionInput) string {
	if strings.TrimSpace(input.Automation) == "" && strings.TrimSpace(input.SourceCommitSHA) == "" {
		return agentSessionExecutionModeWorkingCopy
	}
	return agentSessionExecutionModeDetached
}

func runtimeTaskExecutionMode(task RuntimeTask) string {
	var payload issueWorkingCopyTaskPayload
	_ = json.Unmarshal(task.Payload, &payload)
	if payload.ExecutionMode == agentSessionExecutionModeWorkingCopy {
		return agentSessionExecutionModeWorkingCopy
	}
	return agentSessionExecutionModeDetached
}

func issueWorkingCopyPayload(task RuntimeTask) (issueWorkingCopyTaskPayload, error) {
	var payload issueWorkingCopyTaskPayload
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return issueWorkingCopyTaskPayload{}, err
	}
	if payload.ExecutionMode != agentSessionExecutionModeWorkingCopy {
		return issueWorkingCopyTaskPayload{}, errors.New("runtime task is not an issue working-copy task")
	}
	if payload.WorkingCopyGeneration < 0 || strings.TrimSpace(payload.Branch) == "" {
		return issueWorkingCopyTaskPayload{}, errors.New("invalid issue working-copy task metadata")
	}
	return payload, nil
}

func issueWorkingCopyResult(task RuntimeTask) (issueWorkingCopyResultEnvelope, error) {
	var result struct {
		WorkingCopy *issueWorkingCopyResultEnvelope `json:"workingCopy"`
	}
	if err := json.Unmarshal(task.Result, &result); err != nil {
		return issueWorkingCopyResultEnvelope{}, err
	}
	if result.WorkingCopy == nil {
		return issueWorkingCopyResultEnvelope{}, errors.New("workingCopy result is required")
	}
	envelope := *result.WorkingCopy
	envelope.StorageID = strings.TrimSpace(envelope.StorageID)
	envelope.Branch = strings.TrimSpace(envelope.Branch)
	envelope.BaseCommitSHA = strings.TrimSpace(envelope.BaseCommitSHA)
	envelope.HeadCommitSHA = strings.TrimSpace(envelope.HeadCommitSHA)
	envelope.ContentState = strings.TrimSpace(envelope.ContentState)
	envelope.RecoveryReason = strings.TrimSpace(envelope.RecoveryReason)
	if err := validateWorkingCopyResultEnvelope(envelope); err != nil {
		return issueWorkingCopyResultEnvelope{}, err
	}
	return envelope, nil
}

func runtimeTaskResultWithoutSource(raw json.RawMessage) json.RawMessage {
	var result map[string]json.RawMessage
	if err := json.Unmarshal(raw, &result); err != nil {
		return copyRawMessage(raw)
	}
	delete(result, "source")
	updated, err := json.Marshal(result)
	if err != nil {
		return copyRawMessage(raw)
	}
	return updated
}

func validateWorkingCopyResultEnvelope(envelope issueWorkingCopyResultEnvelope) error {
	if err := validateRuntimeStorageID(envelope.StorageID, true); err != nil {
		return err
	}
	if envelope.Branch == "" {
		return errors.New("workingCopy branch is required")
	}
	if envelope.Generation < 0 {
		return errors.New("workingCopy generation must be greater than or equal to zero")
	}
	switch envelope.ContentState {
	case workingCopyStateClean:
		if envelope.HeadCommitSHA == "" {
			return errors.New("clean workingCopy headCommitSha is required")
		}
		if envelope.RecoveryReason != "" {
			return errors.New("clean workingCopy recoveryReason must be empty")
		}
	case workingCopyStateDirty:
		if envelope.RecoveryReason != "" {
			return errors.New("dirty workingCopy recoveryReason must be empty")
		}
	case workingCopyStateRecoveryRequired:
		if !validWorkingCopyRecoveryReason(envelope.RecoveryReason) {
			return errors.New("invalid workingCopy recoveryReason")
		}
	default:
		return errors.New("workingCopy contentState must be clean, dirty, or recovery_required")
	}
	return nil
}

func validWorkingCopyRecoveryReason(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "worktree_missing", "branch_mismatch", "head_mismatch", "metadata_missing", "workspace_probe_failed":
		return true
	default:
		return false
	}
}

func validateRuntimeStorageID(value string, required bool) error {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return errors.New("storageId is required")
		}
		return nil
	}
	if !strings.HasPrefix(value, "msws_") || len(value) > 128 {
		return errors.New("storageId must be an msws_ identifier")
	}
	for _, r := range strings.TrimPrefix(value, "msws_") {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return errors.New("storageId contains invalid characters")
		}
	}
	if len(strings.TrimPrefix(value, "msws_")) < 8 {
		return errors.New("storageId is too short")
	}
	return nil
}

func defaultIssueWorkingCopyBranch(issueID string) string {
	issueID = strings.ToLower(strings.Trim(strings.TrimSpace(issueID), "-"))
	var builder strings.Builder
	for _, r := range issueID {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	safe := strings.Trim(builder.String(), "-_")
	if safe == "" {
		safe = "issue"
	}
	return "mspace/" + safe
}

func addIssueWorkingCopyCapability(raw json.RawMessage) (json.RawMessage, error) {
	var capabilities map[string]bool
	if err := json.Unmarshal(raw, &capabilities); err != nil {
		return nil, fmt.Errorf("requiredCapabilities must be a JSON object")
	}
	capabilities["issueWorkingCopyV1"] = true
	return json.Marshal(capabilities)
}

func issueWorkingCopyKey(workspaceID, issueID string) string {
	return strings.TrimSpace(workspaceID) + ":" + strings.TrimSpace(issueID)
}

func applyIssueWorkingCopyTaskPayload(payload map[string]any, workingCopy IssueWorkingCopy) {
	payload["executionMode"] = agentSessionExecutionModeWorkingCopy
	payload["workingCopyGeneration"] = workingCopy.Generation
	payload["expectedHeadSha"] = workingCopy.HeadCommitSHA
	payload["branch"] = workingCopy.Branch
	payload["initialize"] = workingCopy.ContentState == workingCopyStateUninitialized
	if env, ok := payload["env"].(map[string]string); ok {
		env["MSPACE_SESSION_BRANCH"] = workingCopy.Branch
	}
}

func (s *MemoryStore) issueWorkingCopyPointerLocked(workspaceID, issueID string) *IssueWorkingCopy {
	workingCopy, ok := s.issueWorkingCopies[issueWorkingCopyKey(workspaceID, issueID)]
	if !ok {
		return nil
	}
	copy := workingCopy
	return &copy
}

func (s *MemoryStore) hasActiveWorkerForWorkingCopyLocked(workspaceID, runtimeMode string, requiredCapabilities json.RawMessage, storageID string, now time.Time) bool {
	storageID = strings.TrimSpace(storageID)
	for _, worker := range s.runtimeWorkers {
		if storageID != "" && worker.StorageID != storageID {
			continue
		}
		if worker.StorageID == "" {
			continue
		}
		if isActiveWorkerWithCapabilities(worker, workspaceID, runtimeMode, requiredCapabilities, now) {
			return true
		}
	}
	return false
}

func (s *MemoryStore) runtimeTaskMatchesWorkingCopyStorageLocked(task RuntimeTask, worker RuntimeWorker) bool {
	if task.StorageAffinityID != "" && task.StorageAffinityID != worker.StorageID {
		return false
	}
	if runtimeTaskExecutionMode(task) != agentSessionExecutionModeWorkingCopy {
		return true
	}
	if worker.StorageID == "" {
		return false
	}
	workingCopy, ok := s.issueWorkingCopies[issueWorkingCopyKey(task.WorkspaceID, task.IssueID)]
	if !ok || workingCopy.ActiveSessionID != task.SessionID || workingCopy.ProjectID != task.ProjectID {
		return false
	}
	return workingCopy.StorageID == "" || workingCopy.StorageID == worker.StorageID
}

func (s *MemoryStore) releaseQueuedIssueWorkingCopyLocked(task RuntimeTask) {
	payload, err := issueWorkingCopyPayload(task)
	if err != nil {
		return
	}
	key := issueWorkingCopyKey(task.WorkspaceID, task.IssueID)
	workingCopy, ok := s.issueWorkingCopies[key]
	if !ok || workingCopy.ActiveSessionID != task.SessionID || workingCopy.Generation != payload.WorkingCopyGeneration {
		return
	}
	workingCopy.ActiveSessionID = ""
	workingCopy.Generation++
	workingCopy.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.issueWorkingCopies[key] = workingCopy
}

func (s *MemoryStore) markIssueWorkingCopyRecoveryRequiredLocked(key string, workingCopy IssueWorkingCopy, task RuntimeTask, reason string) {
	workingCopy.ContentState = workingCopyStateRecoveryRequired
	workingCopy.RecoveryReason = reason
	workingCopy.LastWorkerID = task.ClaimedByWorkerID
	workingCopy.ActiveSessionID = ""
	workingCopy.Generation++
	workingCopy.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.issueWorkingCopies[key] = workingCopy
}

func (s *MemoryStore) reconcileIssueWorkingCopyTerminalLocked(task RuntimeTask) bool {
	if runtimeTaskExecutionMode(task) != agentSessionExecutionModeWorkingCopy {
		return true
	}
	key := issueWorkingCopyKey(task.WorkspaceID, task.IssueID)
	workingCopy, ok := s.issueWorkingCopies[key]
	if !ok || workingCopy.ActiveSessionID != task.SessionID {
		s.appendRuntimeTaskEventLocked(task.WorkspaceID, task.ID, task.ClaimedByWorkerID, "", "working_copy_reconcile_rejected", json.RawMessage(`{"reason":"stale_generation"}`))
		return false
	}
	payload, err := issueWorkingCopyPayload(task)
	if err != nil || workingCopy.Generation != payload.WorkingCopyGeneration {
		s.markIssueWorkingCopyRecoveryRequiredLocked(key, workingCopy, task, "metadata_missing")
		return false
	}
	envelope, envelopeErr := issueWorkingCopyResult(task)
	recoveryReason := ""
	if envelopeErr != nil {
		recoveryReason = "metadata_missing"
	} else {
		worker, workerOK := s.runtimeWorkerByID(task.WorkspaceID, task.ClaimedByWorkerID)
		switch {
		case envelope.Generation != payload.WorkingCopyGeneration:
			recoveryReason = "metadata_missing"
		case envelope.Branch != workingCopy.Branch || envelope.Branch != payload.Branch:
			recoveryReason = "branch_mismatch"
		case !workerOK || worker.StorageID == "" || envelope.StorageID != worker.StorageID || (workingCopy.StorageID != "" && envelope.StorageID != workingCopy.StorageID):
			recoveryReason = "workspace_probe_failed"
		case workingCopy.HeadCommitSHA != payload.ExpectedHeadSHA:
			recoveryReason = "head_mismatch"
		case envelope.ContentState == workingCopyStateRecoveryRequired:
			recoveryReason = envelope.RecoveryReason
		case workingCopy.ContentState == workingCopyStateUninitialized && (envelope.BaseCommitSHA == "" || envelope.HeadCommitSHA == ""):
			recoveryReason = "metadata_missing"
		case task.Status == "completed" && envelope.ContentState != workingCopyStateClean:
			recoveryReason = "workspace_probe_failed"
		}
	}
	if recoveryReason != "" {
		s.markIssueWorkingCopyRecoveryRequiredLocked(key, workingCopy, task, recoveryReason)
		return false
	}
	if workingCopy.BaseCommitSHA == "" {
		workingCopy.BaseCommitSHA = envelope.BaseCommitSHA
	}
	workingCopy.HeadCommitSHA = envelope.HeadCommitSHA
	workingCopy.StorageID = envelope.StorageID
	workingCopy.ContentState = envelope.ContentState
	workingCopy.RecoveryReason = envelope.RecoveryReason
	workingCopy.LastWorkerID = task.ClaimedByWorkerID
	workingCopy.ActiveSessionID = ""
	workingCopy.Generation++
	workingCopy.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	s.issueWorkingCopies[key] = workingCopy
	return true
}

func lockOrCreateIssueWorkingCopy(ctx context.Context, q queryer, workspaceID, issueID, projectID string) (IssueWorkingCopy, error) {
	var currentProjectID string
	if err := q.QueryRow(ctx, `
		SELECT COALESCE(project_id::text, '')
		FROM issues
		WHERE workspace_id = $1 AND id = $2
		FOR UPDATE
	`, workspaceID, issueID).Scan(&currentProjectID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IssueWorkingCopy{}, ErrNotFound
		}
		return IssueWorkingCopy{}, err
	}
	if currentProjectID != strings.TrimSpace(projectID) {
		return IssueWorkingCopy{}, ErrConflict
	}
	branch := defaultIssueWorkingCopyBranch(issueID)
	if _, err := q.Exec(ctx, `
		INSERT INTO issue_working_copies (workspace_id, issue_id, project_id, branch)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (workspace_id, issue_id) DO NOTHING
	`, workspaceID, issueID, projectID, branch); err != nil {
		return IssueWorkingCopy{}, err
	}
	return scanIssueWorkingCopy(q.QueryRow(ctx, `
		SELECT issue_id::text, project_id::text, branch, base_commit_sha, head_commit_sha,
			storage_id, COALESCE(last_worker_id::text, ''), content_state, recovery_reason,
			active_session_id, generation, created_at, updated_at
		FROM issue_working_copies
		WHERE workspace_id = $1 AND issue_id = $2
		FOR UPDATE
	`, workspaceID, issueID))
}

func lockIssueWorkingCopyProjectChange(ctx context.Context, q queryer, workspaceID, issueID, nextProjectID string) error {
	var currentProjectID string
	if err := q.QueryRow(ctx, `
		SELECT COALESCE(project_id::text, '')
		FROM issues
		WHERE workspace_id = $1 AND id = $2
		FOR UPDATE
	`, workspaceID, issueID).Scan(&currentProjectID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	var workingCopyProjectID string
	err := q.QueryRow(ctx, `
		SELECT project_id::text
		FROM issue_working_copies
		WHERE workspace_id = $1 AND issue_id = $2
		FOR UPDATE
	`, workspaceID, issueID).Scan(&workingCopyProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if workingCopyProjectID != strings.TrimSpace(nextProjectID) {
		return ErrConflict
	}
	return nil
}

func (s *PostgresStore) loadIssueWorkingCopyOptional(ctx context.Context, workspaceID, issueID string) (*IssueWorkingCopy, error) {
	workingCopy, err := scanIssueWorkingCopy(s.pool.QueryRow(ctx, `
		SELECT issue_id::text, project_id::text, branch, base_commit_sha, head_commit_sha,
			storage_id, COALESCE(last_worker_id::text, ''), content_state, recovery_reason,
			active_session_id, generation, created_at, updated_at
		FROM issue_working_copies
		WHERE workspace_id = $1 AND issue_id = $2
	`, workspaceID, issueID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &workingCopy, nil
}

func scanIssueWorkingCopy(row scanner) (IssueWorkingCopy, error) {
	var workingCopy IssueWorkingCopy
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&workingCopy.IssueID, &workingCopy.ProjectID, &workingCopy.Branch,
		&workingCopy.BaseCommitSHA, &workingCopy.HeadCommitSHA, &workingCopy.StorageID,
		&workingCopy.LastWorkerID, &workingCopy.ContentState, &workingCopy.RecoveryReason,
		&workingCopy.ActiveSessionID, &workingCopy.Generation, &createdAt, &updatedAt,
	); err != nil {
		return IssueWorkingCopy{}, err
	}
	workingCopy.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	workingCopy.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return workingCopy, nil
}

func hasActiveWorkerForWorkingCopyRecord(ctx context.Context, q queryer, workspaceID, runtimeMode string, requiredCapabilities json.RawMessage, storageID string) (bool, error) {
	requiredCapabilities, err := normalizeJSONObjectPayload(requiredCapabilities)
	if err != nil {
		return false, err
	}
	var exists bool
	err = q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM runtime_workers
			WHERE workspace_id = $1 AND mode = $2 AND status = 'online'
				AND capabilities @> $3::jsonb
				AND storage_id <> ''
				AND ($4 = '' OR storage_id = $4)
				AND last_seen_at >= now() - interval '45 seconds'
		)
	`, workspaceID, runtimeMode, requiredCapabilities, strings.TrimSpace(storageID)).Scan(&exists)
	return exists, err
}

func bindClaimedIssueWorkingCopy(ctx context.Context, q queryer, task RuntimeTask, workerID string) (RuntimeTask, error) {
	var storageID string
	if err := q.QueryRow(ctx, `
		SELECT storage_id FROM runtime_workers
		WHERE workspace_id = $1 AND id = $2
	`, task.WorkspaceID, workerID).Scan(&storageID); err != nil {
		return RuntimeTask{}, err
	}
	if err := validateRuntimeStorageID(storageID, true); err != nil {
		return RuntimeTask{}, err
	}
	commandTag, err := q.Exec(ctx, `
		UPDATE issue_working_copies
		SET storage_id = CASE WHEN storage_id = '' THEN $1 ELSE storage_id END,
			last_worker_id = $2, updated_at = now()
		WHERE workspace_id = $3 AND issue_id::text = $4 AND project_id::text = $5
			AND active_session_id = $6 AND (storage_id = '' OR storage_id = $1)
	`, storageID, workerID, task.WorkspaceID, task.IssueID, task.ProjectID, task.SessionID)
	if err != nil {
		return RuntimeTask{}, err
	}
	if commandTag.RowsAffected() != 1 {
		return RuntimeTask{}, ErrConflict
	}
	if _, err := q.Exec(ctx, `
		UPDATE runtime_tasks SET storage_affinity_id = $1, updated_at = now()
		WHERE workspace_id = $2 AND id = $3 AND claimed_by_worker_id = $4
	`, storageID, task.WorkspaceID, task.ID, workerID); err != nil {
		return RuntimeTask{}, err
	}
	task.StorageAffinityID = storageID
	return task, nil
}

func issueWorkingCopyRecoveryUpdate(ctx context.Context, q queryer, workingCopy IssueWorkingCopy, task RuntimeTask, reason string) error {
	commandTag, err := q.Exec(ctx, `
		UPDATE issue_working_copies
		SET content_state = 'recovery_required', recovery_reason = $1,
			last_worker_id = NULLIF($2, '')::uuid, active_session_id = '',
			generation = generation + 1, updated_at = now()
		WHERE workspace_id = $3 AND issue_id = $4
			AND active_session_id = $5 AND generation = $6
	`, reason, task.ClaimedByWorkerID, task.WorkspaceID, task.IssueID, task.SessionID, workingCopy.Generation)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func releaseQueuedIssueWorkingCopyPostgres(ctx context.Context, q queryer, task RuntimeTask) error {
	payload, err := issueWorkingCopyPayload(task)
	if err != nil {
		return err
	}
	commandTag, err := q.Exec(ctx, `
		UPDATE issue_working_copies
		SET active_session_id = '', generation = generation + 1, updated_at = now()
		WHERE workspace_id = $1 AND issue_id::text = $2
			AND active_session_id = $3 AND generation = $4
	`, task.WorkspaceID, task.IssueID, task.SessionID, payload.WorkingCopyGeneration)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func (s *PostgresStore) reconcileIssueWorkingCopyTerminal(ctx context.Context, q queryer, task RuntimeTask) (bool, error) {
	if runtimeTaskExecutionMode(task) != agentSessionExecutionModeWorkingCopy {
		return true, nil
	}
	workingCopy, err := scanIssueWorkingCopy(q.QueryRow(ctx, `
		SELECT issue_id::text, project_id::text, branch, base_commit_sha, head_commit_sha,
			storage_id, COALESCE(last_worker_id::text, ''), content_state, recovery_reason,
			active_session_id, generation, created_at, updated_at
		FROM issue_working_copies
		WHERE workspace_id = $1 AND issue_id = $2
		FOR UPDATE
	`, task.WorkspaceID, task.IssueID))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if workingCopy.ActiveSessionID != task.SessionID {
		if err := insertRuntimeTaskEvent(ctx, q, task.WorkspaceID, task.ID, task.ClaimedByWorkerID, "", "working_copy_reconcile_rejected", map[string]any{"reason": "stale_generation"}); err != nil {
			return false, err
		}
		return false, nil
	}
	payload, payloadErr := issueWorkingCopyPayload(task)
	if payloadErr != nil || workingCopy.Generation != payload.WorkingCopyGeneration {
		if err := issueWorkingCopyRecoveryUpdate(ctx, q, workingCopy, task, "metadata_missing"); err != nil {
			return false, err
		}
		return false, nil
	}
	envelope, envelopeErr := issueWorkingCopyResult(task)
	recoveryReason := ""
	if envelopeErr != nil {
		recoveryReason = "metadata_missing"
	} else {
		var workerStorageID string
		workerErr := q.QueryRow(ctx, `SELECT storage_id FROM runtime_workers WHERE workspace_id = $1 AND id = $2`, task.WorkspaceID, task.ClaimedByWorkerID).Scan(&workerStorageID)
		switch {
		case envelope.Generation != payload.WorkingCopyGeneration:
			recoveryReason = "metadata_missing"
		case envelope.Branch != workingCopy.Branch || envelope.Branch != payload.Branch:
			recoveryReason = "branch_mismatch"
		case workerErr != nil || workerStorageID == "" || envelope.StorageID != workerStorageID || (workingCopy.StorageID != "" && envelope.StorageID != workingCopy.StorageID):
			recoveryReason = "workspace_probe_failed"
		case workingCopy.HeadCommitSHA != payload.ExpectedHeadSHA:
			recoveryReason = "head_mismatch"
		case envelope.ContentState == workingCopyStateRecoveryRequired:
			recoveryReason = envelope.RecoveryReason
		case workingCopy.ContentState == workingCopyStateUninitialized && (envelope.BaseCommitSHA == "" || envelope.HeadCommitSHA == ""):
			recoveryReason = "metadata_missing"
		case task.Status == "completed" && envelope.ContentState != workingCopyStateClean:
			recoveryReason = "workspace_probe_failed"
		}
	}
	if recoveryReason != "" {
		if err := issueWorkingCopyRecoveryUpdate(ctx, q, workingCopy, task, recoveryReason); err != nil {
			return false, err
		}
		return false, nil
	}
	baseCommitSHA := workingCopy.BaseCommitSHA
	if baseCommitSHA == "" {
		baseCommitSHA = envelope.BaseCommitSHA
	}
	commandTag, err := q.Exec(ctx, `
		UPDATE issue_working_copies
		SET base_commit_sha = $1, head_commit_sha = $2, storage_id = $3,
			last_worker_id = NULLIF($4, '')::uuid, content_state = $5,
			recovery_reason = $6, active_session_id = '', generation = generation + 1,
			updated_at = now()
		WHERE workspace_id = $7 AND issue_id = $8
			AND active_session_id = $9 AND generation = $10
	`, baseCommitSHA, envelope.HeadCommitSHA, envelope.StorageID, task.ClaimedByWorkerID,
		envelope.ContentState, envelope.RecoveryReason, task.WorkspaceID, task.IssueID,
		task.SessionID, payload.WorkingCopyGeneration)
	if err != nil {
		return false, err
	}
	return commandTag.RowsAffected() == 1, nil
}
