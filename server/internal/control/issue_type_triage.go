package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type issueTypeTriageDetail struct {
	Issue     Issue
	Project   Project
	Labels    []IssueLabel
	Workspace Workspace
}

type issueTypeTriageResult struct {
	Title      string  `json:"title"`
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

var errIssueTypeTriageNotNeeded = errors.New("issue does not need type triage")

func (s *Server) enqueueIssueTypeTriageNow(ctx context.Context, userID, workspaceID, issueID, expectedTitle string) {
	if err := s.enqueueIssueTypeTriage(ctx, userID, workspaceID, issueID, expectedTitle); err != nil {
		if !errors.Is(err, errIssueTypeTriageNotNeeded) {
			slog.Warn("failed to enqueue issue type triage", slog.String("workspace_id", workspaceID), slog.String("issue_id", issueID), slog.String("error", err.Error()))
		}
		return
	}
	if persistent, ok := s.store.(interface{ Persist() error }); ok {
		if err := persistent.Persist(); err != nil {
			slog.Warn("failed to persist issue type triage state", slog.String("workspace_id", workspaceID), slog.String("issue_id", issueID), slog.String("error", err.Error()))
		}
	}
}

func (s *Server) enqueueIssueTypeTriage(ctx context.Context, userID, workspaceID, issueID, expectedTitle string) error {
	detail, err := s.loadIssueForTypeTriage(ctx, workspaceID, issueID)
	if err != nil {
		return err
	}
	if detail.Issue.TriageStatus != "pending" || hasIssueLabelDimension(detail.Labels, issueLabelDimensionType) {
		return errIssueTypeTriageNotNeeded
	}
	capabilities, err := json.Marshal(map[string]bool{"codex": true})
	if err != nil {
		return err
	}
	payload, err := json.Marshal(buildIssueTypeTriagePayload(detail, plainIssueTitleFromText(expectedTitle)))
	if err != nil {
		return err
	}
	workspace := workspaceForTriage(detail, workspaceID)
	return s.ensureIssueTypeTriageTask(ctx, userID, workspaceID, issueID, plainIssueTitleFromText(expectedTitle), CreateRuntimeTaskInput{
		IssueID:              detail.Issue.ID,
		ProjectID:            detail.Issue.ProjectID,
		Kind:                 "issue_type_triage",
		Priority:             0,
		RuntimeMode:          workspace.Kind,
		RequiredCapabilities: capabilities,
		Payload:              payload,
		ServerManaged:        true,
	})
}

func (s *Server) ensureIssueTypeTriageTask(ctx context.Context, userID, workspaceID, issueID, expectedTitle string, input CreateRuntimeTaskInput) error {
	switch store := s.store.(type) {
	case *PostgresStore:
		return ensurePostgresIssueTypeTriageTask(ctx, store, userID, workspaceID, issueID, expectedTitle, input)
	case *SQLiteStore:
		store.MemoryStore.mu.Lock()
		defer store.MemoryStore.mu.Unlock()
		return ensureMemoryIssueTypeTriageTask(store.MemoryStore, userID, workspaceID, issueID, expectedTitle, input)
	case *MemoryStore:
		store.mu.Lock()
		defer store.mu.Unlock()
		return ensureMemoryIssueTypeTriageTask(store, userID, workspaceID, issueID, expectedTitle, input)
	default:
		active, err := s.hasActiveIssueTypeTriageTask(ctx, workspaceID, issueID)
		if err != nil {
			return err
		}
		if active {
			return errIssueTypeTriageNotNeeded
		}
		_, err = s.store.CreateRuntimeTask(ctx, userID, workspaceID, input)
		return err
	}
}

func ensurePostgresIssueTypeTriageTask(ctx context.Context, store *PostgresStore, userID, workspaceID, issueID, expectedTitle string, input CreateRuntimeTaskInput) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`, workspaceID, issueID); err != nil {
		return err
	}
	var taskID string
	var payload json.RawMessage
	err = tx.QueryRow(ctx, `
		SELECT id::text, payload
		FROM runtime_tasks
		WHERE workspace_id = $1
			AND issue_id = $2
			AND kind = 'issue_type_triage'
			AND status IN ('queued', 'claimed', 'running')
		ORDER BY created_at ASC, id ASC
		LIMIT 1
		FOR UPDATE
	`, workspaceID, issueID).Scan(&taskID, &payload)
	switch {
	case err == nil:
		nextPayload, changed, err := issueTypeTriagePayloadWithExpectedTitle(payload, expectedTitle)
		if err != nil {
			return err
		}
		if !changed {
			return errIssueTypeTriageNotNeeded
		}
		if _, err := tx.Exec(ctx, `UPDATE runtime_tasks SET payload = $2, updated_at = now() WHERE id = $1`, taskID, nextPayload); err != nil {
			return err
		}
	case errors.Is(err, pgx.ErrNoRows):
		var triageStatus string
		if err := tx.QueryRow(ctx, `
			SELECT triage_status
			FROM issues
			WHERE workspace_id = $1 AND id = $2
			FOR UPDATE
		`, workspaceID, issueID).Scan(&triageStatus); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		labels, err := listIssueLabels(ctx, tx, workspaceID, issueID)
		if err != nil {
			return err
		}
		if triageStatus != "pending" || hasIssueLabelDimension(labels, issueLabelDimensionType) {
			return errIssueTypeTriageNotNeeded
		}
		if _, err := insertRuntimeTaskRecord(ctx, tx, workspaceID, userID, input); err != nil {
			return err
		}
	case err != nil:
		return err
	}
	return tx.Commit(ctx)
}

func ensureMemoryIssueTypeTriageTask(store *MemoryStore, userID, workspaceID, issueID, expectedTitle string, input CreateRuntimeTaskInput) error {
	for id, task := range store.runtimeTasks {
		if task.WorkspaceID != workspaceID || task.IssueID != issueID || task.Kind != "issue_type_triage" ||
			(task.Status != "queued" && task.Status != "claimed" && task.Status != "running") {
			continue
		}
		nextPayload, changed, err := issueTypeTriagePayloadWithExpectedTitle(task.Payload, expectedTitle)
		if err != nil {
			return err
		}
		if !changed {
			return errIssueTypeTriageNotNeeded
		}
		task.Payload = nextPayload
		task.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		store.runtimeTasks[id] = task
		return nil
	}
	issue, ok := store.issues[issueID]
	if !ok || issue.WorkspaceID != workspaceID {
		return ErrNotFound
	}
	if issue.TriageStatus != "pending" || hasIssueLabelDimension(store.issueLabels[issueID], issueLabelDimensionType) {
		return errIssueTypeTriageNotNeeded
	}
	_, err := store.createRuntimeTaskLocked(userID, workspaceID, input)
	return err
}

func issueTypeTriagePayloadWithExpectedTitle(payload json.RawMessage, expectedTitle string) (json.RawMessage, bool, error) {
	expectedTitle = plainIssueTitleFromText(expectedTitle)
	if expectedTitle == "" {
		return payload, false, nil
	}
	value := map[string]any{}
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, false, err
	}
	if current, _ := value["expectedTitle"].(string); plainIssueTitleFromText(current) != "" {
		return payload, false, nil
	}
	value["expectedTitle"] = expectedTitle
	nextPayload, err := json.Marshal(value)
	return nextPayload, err == nil, err
}

func (s *Server) hasActiveIssueTypeTriageTask(ctx context.Context, workspaceID, issueID string) (bool, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	switch store := s.store.(type) {
	case *PostgresStore:
		var exists bool
		if err := store.pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM runtime_tasks
				WHERE workspace_id = $1
					AND issue_id = $2
					AND kind = 'issue_type_triage'
					AND status IN ('queued', 'claimed', 'running')
			)
		`, workspaceID, issueID).Scan(&exists); err != nil {
			return false, err
		}
		return exists, nil
	case *SQLiteStore:
		store.MemoryStore.mu.Lock()
		defer store.MemoryStore.mu.Unlock()
		for _, task := range store.MemoryStore.runtimeTasks {
			if task.WorkspaceID == workspaceID &&
				task.IssueID == issueID &&
				task.Kind == "issue_type_triage" &&
				(task.Status == "queued" || task.Status == "claimed" || task.Status == "running") {
				return true, nil
			}
		}
		return false, nil
	case *MemoryStore:
		store.mu.Lock()
		defer store.mu.Unlock()
		for _, task := range store.runtimeTasks {
			if task.WorkspaceID == workspaceID &&
				task.IssueID == issueID &&
				task.Kind == "issue_type_triage" &&
				(task.Status == "queued" || task.Status == "claimed" || task.Status == "running") {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, nil
	}
}

func buildIssueTypeTriagePayload(detail issueTypeTriageDetail, expectedTitle string) map[string]any {
	return map[string]any{
		"issueId":               detail.Issue.ID,
		"workspaceId":           detail.Issue.WorkspaceID,
		"projectId":             detail.Issue.ProjectID,
		"expectedTitle":         expectedTitle,
		"developerInstructions": buildIssueTypeTriageDeveloperInstructions(),
		"prompt":                buildIssueTypeTriagePrompt(detail),
		"issue": map[string]any{
			"title":  detail.Issue.Title,
			"body":   detail.Issue.Body,
			"status": detail.Issue.Status,
		},
		"project": map[string]any{
			"name":          detail.Project.Name,
			"repoPath":      detail.Project.RepoPath,
			"remoteUrl":     detail.Project.RemoteURL,
			"gitOwner":      detail.Project.GitOwner,
			"gitRepo":       detail.Project.GitRepo,
			"defaultBranch": detail.Project.DefaultBranch,
		},
	}
}

func (s *Server) loadIssueForTypeTriage(ctx context.Context, workspaceID, issueID string) (issueTypeTriageDetail, error) {
	switch store := s.store.(type) {
	case *PostgresStore:
		issue, err := loadIssue(ctx, store.pool, workspaceID, issueID)
		if err != nil {
			return issueTypeTriageDetail{}, err
		}
		project := Project{}
		if issue.ProjectID != "" {
			project, err = resolveIssueProject(ctx, store.pool, workspaceID, issue.ProjectID, "")
			if err != nil {
				return issueTypeTriageDetail{}, err
			}
		}
		labels, err := listIssueLabels(ctx, store.pool, workspaceID, issueID)
		if err != nil {
			return issueTypeTriageDetail{}, err
		}
		workspace, err := loadWorkspaceForIssueTriage(ctx, store.pool, workspaceID)
		if err != nil {
			return issueTypeTriageDetail{}, err
		}
		return issueTypeTriageDetail{Issue: issue, Project: project, Labels: labels, Workspace: workspace}, nil
	case *SQLiteStore:
		store.MemoryStore.mu.Lock()
		defer store.MemoryStore.mu.Unlock()
		issue, ok := store.MemoryStore.issues[strings.TrimSpace(issueID)]
		if !ok || issue.WorkspaceID != strings.TrimSpace(workspaceID) {
			return issueTypeTriageDetail{}, ErrNotFound
		}
		workspace, _ := store.MemoryStore.workspaceLocked(workspaceID)
		return issueTypeTriageDetail{
			Issue:     issue,
			Project:   store.MemoryStore.projects[issue.ProjectID],
			Labels:    store.MemoryStore.issueLabels[issue.ID],
			Workspace: workspace,
		}, nil
	case *MemoryStore:
		store.mu.Lock()
		defer store.mu.Unlock()
		issue, ok := store.issues[strings.TrimSpace(issueID)]
		if !ok || issue.WorkspaceID != strings.TrimSpace(workspaceID) {
			return issueTypeTriageDetail{}, ErrNotFound
		}
		workspace, _ := store.workspaceLocked(workspaceID)
		return issueTypeTriageDetail{
			Issue:     issue,
			Project:   store.projects[issue.ProjectID],
			Labels:    store.issueLabels[issue.ID],
			Workspace: workspace,
		}, nil
	default:
		detail, err := store.GetIssue(ctx, "", workspaceID, issueID)
		if err != nil {
			return issueTypeTriageDetail{}, err
		}
		return issueTypeTriageDetail{
			Issue:     detail.Issue,
			Project:   detail.Project,
			Labels:    detail.Labels,
			Workspace: Workspace{ID: strings.TrimSpace(workspaceID), Kind: "team"},
		}, nil
	}
}

func loadWorkspaceForIssueTriage(ctx context.Context, q queryer, workspaceID string) (Workspace, error) {
	row := q.QueryRow(ctx, `
		SELECT id::text, name, slug, kind, '', icon, description, created_at, updated_at
		FROM workspaces
		WHERE id = $1
	`, strings.TrimSpace(workspaceID))
	var workspace Workspace
	var createdAt, updatedAt time.Time
	if err := row.Scan(&workspace.ID, &workspace.Name, &workspace.Slug, &workspace.Kind, &workspace.Role, &workspace.Icon, &workspace.Description, &createdAt, &updatedAt); err != nil {
		return Workspace{}, err
	}
	workspace.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	workspace.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return workspace, nil
}

func workspaceForTriage(detail issueTypeTriageDetail, workspaceID string) Workspace {
	workspace := detail.Workspace
	if strings.TrimSpace(workspace.ID) == "" {
		workspace.ID = strings.TrimSpace(workspaceID)
	}
	if strings.TrimSpace(workspace.Kind) == "" {
		workspace.Kind = "team"
	}
	return workspace
}

func (s *PostgresStore) reconcileIssueTypeTriageRuntimeResult(ctx context.Context, q queryer, task RuntimeTask) error {
	if task.Status != "completed" {
		return markIssueTriageFailed(ctx, q, task.WorkspaceID, task.IssueID)
	}
	result, err := parseIssueTypeTriageResult(string(task.Result))
	if err != nil {
		return markIssueTriageFailed(ctx, q, task.WorkspaceID, task.IssueID)
	}
	return applyIssueTypeTriageResult(ctx, q, task.WorkspaceID, task.IssueID, "type:"+result.Type, issueTypeTriageExpectedTitle(task), result.Title)
}

func applyIssueTypeClassification(ctx context.Context, q queryer, workspaceID, issueID string, labelKey string) error {
	return applyIssueTypeTriageResult(ctx, q, workspaceID, issueID, labelKey, "", "")
}

func applyIssueTypeTriageResult(ctx context.Context, q queryer, workspaceID, issueID, labelKey, expectedTitle, suggestedTitle string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	expectedTitle = plainIssueTitleFromText(expectedTitle)
	suggestedTitle = normalizeSuggestedIssueTitle(suggestedTitle)
	labels, err := normalizeIssueLabelKeys([]string{labelKey})
	if err != nil {
		return err
	}
	if !hasIssueLabelDimension(labels, issueLabelDimensionType) {
		return errors.New("issue type label is required")
	}
	var triageStatus string
	if err := q.QueryRow(ctx, `
		SELECT triage_status
		FROM issues
		WHERE workspace_id = $1 AND id = $2
		FOR UPDATE
	`, workspaceID, issueID).Scan(&triageStatus); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if expectedTitle != "" && suggestedTitle != "" && expectedTitle != suggestedTitle {
		if _, err := q.Exec(ctx, `
			UPDATE issues
			SET title = $4, updated_at = now()
			WHERE workspace_id = $1 AND id = $2 AND title = $3
		`, workspaceID, issueID, expectedTitle, suggestedTitle); err != nil {
			return err
		}
	}
	if triageStatus != "pending" {
		return nil
	}
	existingLabels, err := listIssueLabels(ctx, q, workspaceID, issueID)
	if err != nil {
		return err
	}
	nextLabels := replaceIssueLabelDimension(existingLabels, issueLabelDimensionType, labels[0])
	if err := replaceIssueLabels(ctx, q, workspaceID, issueID, nextLabels); err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		UPDATE issues
		SET triage_status = 'classified', updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND triage_status = 'pending'
	`, workspaceID, issueID)
	return err
}

func issueTypeTriageExpectedTitle(task RuntimeTask) string {
	var payload struct {
		ExpectedTitle string `json:"expectedTitle"`
	}
	if err := json.Unmarshal(task.Payload, &payload); err != nil {
		return ""
	}
	return plainIssueTitleFromText(payload.ExpectedTitle)
}

func markIssueTriageFailed(ctx context.Context, q queryer, workspaceID, issueID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	tag, err := q.Exec(ctx, `
		UPDATE issues
		SET triage_status = 'failed', updated_at = now()
		WHERE workspace_id = $1 AND id = $2 AND triage_status = 'pending'
	`, workspaceID, issueID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, err := loadIssue(ctx, q, workspaceID, issueID); err != nil {
			return err
		}
	}
	return nil
}

func buildIssueTypeTriageDeveloperInstructions() string {
	return strings.TrimSpace(`
You are an mspace issue triage assistant.

Write a concise issue title and classify the issue into exactly one Conventional Commit type.
Allowed types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert.

Rules:
- Return only one compact JSON object.
- Do not wrap the JSON in Markdown.
- Write the title in the same language as the issue note when clear.
- Keep the title specific, plain text, and under 72 characters.
- Treat the existing title as a temporary draft. Always rewrite it from the full body instead of copying it verbatim.
- Do not include Markdown, URLs, labels, quotes, or trailing punctuation in the title.
- Do not assign priority.
- Do not change issue status.
- Do not edit files or run commands.
- If the issue is ambiguous, choose chore with lower confidence.
`)
}

func buildIssueTypeTriagePrompt(detail issueTypeTriageDetail) string {
	var builder strings.Builder
	builder.WriteString("# Issue Type Triage\n\n")
	builder.WriteString("Return exactly this JSON shape:\n")
	builder.WriteString(`{"title":"Fix stale image pull secret after visibility change","type":"fix","confidence":0.86,"reason":"short reason"}`)
	builder.WriteString("\n\n")
	builder.WriteString("Allowed type values: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert.\n\n")
	builder.WriteString("## Issue\n\n")
	builder.WriteString(fmt.Sprintf("Title: %s\n", detail.Issue.Title))
	builder.WriteString(fmt.Sprintf("Status: %s\n", detail.Issue.Status))
	builder.WriteString("Body:\n")
	builder.WriteString(strings.TrimSpace(detail.Issue.Body))
	builder.WriteString("\n\n")
	builder.WriteString("## Project\n\n")
	builder.WriteString(fmt.Sprintf("Name: %s\n", valueOrUnset(detail.Project.Name)))
	builder.WriteString(fmt.Sprintf("Repository path: %s\n", valueOrUnset(detail.Project.RepoPath)))
	builder.WriteString(fmt.Sprintf("Remote: %s\n", valueOrUnset(detail.Project.RemoteURL)))
	builder.WriteString(fmt.Sprintf("GitHub: %s/%s\n", valueOrUnset(detail.Project.GitOwner), valueOrUnset(detail.Project.GitRepo)))
	return builder.String()
}

func parseIssueTypeTriageResult(value string) (issueTypeTriageResult, error) {
	raw := strings.TrimSpace(value)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return issueTypeTriageResult{}, errors.New("triage response did not contain a JSON object")
	}
	raw = raw[start : end+1]
	var result issueTypeTriageResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return issueTypeTriageResult{}, fmt.Errorf("parse triage JSON: %w", err)
	}
	result.Type = strings.TrimSpace(strings.ToLower(result.Type))
	result.Title = normalizeSuggestedIssueTitle(result.Title)
	result.Reason = strings.Join(strings.Fields(result.Reason), " ")
	if !isAllowedIssueTypeLabel(result.Type) {
		return issueTypeTriageResult{}, fmt.Errorf("triage returned unsupported issue type %q", result.Type)
	}
	if result.Confidence < 0 {
		result.Confidence = 0
	}
	if result.Confidence > 1 {
		result.Confidence = 1
	}
	return result, nil
}

func isAllowedIssueTypeLabel(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "feat", "fix", "docs", "style", "refactor", "perf", "test", "build", "ci", "chore", "revert":
		return true
	default:
		return false
	}
}

func valueOrUnset(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "(unset)"
	}
	return value
}
