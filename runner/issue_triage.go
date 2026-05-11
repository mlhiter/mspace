package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
)

const issueTriageTimeout = 2 * time.Minute

type issueTriageResult struct {
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

func (a *app) triageIssueType(issueID string) {
	ctx, cancel := context.WithTimeout(context.Background(), issueTriageTimeout)
	defer cancel()

	result, err := a.classifyIssueType(ctx, issueID)
	if err != nil {
		if a.logger != nil {
			a.logger.Warn("issue triage failed", slog.String("issue_id", issueID), slog.String("error", err.Error()))
		}
		if markErr := a.markIssueTriageFailed(issueID); markErr != nil && a.logger != nil {
			a.logger.Warn("failed to mark issue triage failed", slog.String("issue_id", issueID), slog.String("error", markErr.Error()))
		}
		return
	}
	if err := a.applyIssueTypeClassification(issueID, result); err != nil {
		if a.logger != nil {
			a.logger.Warn("failed to apply issue triage result", slog.String("issue_id", issueID), slog.String("error", err.Error()))
		}
		if markErr := a.markIssueTriageFailed(issueID); markErr != nil && a.logger != nil {
			a.logger.Warn("failed to mark issue triage failed", slog.String("issue_id", issueID), slog.String("error", markErr.Error()))
		}
	}
}

func (a *app) classifyIssueType(ctx context.Context, issueID string) (issueTriageResult, error) {
	detail, err := a.loadIssueDetail(issueID)
	if err != nil {
		return issueTriageResult{}, err
	}
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return issueTriageResult{}, errors.New("codex CLI is not available on PATH")
	}

	cwd := strings.TrimSpace(detail.Project.RepoPath)
	if cwd == "" {
		cwd = os.TempDir()
	}

	client, err := startIssueTriageCodexAppServer(codexPath, cwd)
	if err != nil {
		return issueTriageResult{}, err
	}
	defer client.close()

	var initResp codexInitializeResponse
	if err := client.request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "mspace-triage",
			"title":   "mspace triage",
			"version": "0.1.0",
		},
		"capabilities": map[string]any{
			"experimentalApi": true,
		},
	}, &initResp); err != nil {
		return issueTriageResult{}, fmt.Errorf("initialize codex app-server: %w", err)
	}

	var threadResp codexThreadStartResponse
	if err := client.request(ctx, "thread/start", map[string]any{
		"cwd":                    cwd,
		"approvalPolicy":         "never",
		"approvalsReviewer":      "user",
		"sandbox":                "danger-full-access",
		"developerInstructions":  buildIssueTriageDeveloperInstructions(),
		"personality":            "pragmatic",
		"ephemeral":              true,
		"sessionStartSource":     "startup",
		"serviceName":            "mspace-triage",
		"experimentalRawEvents":  false,
		"persistExtendedHistory": false,
	}, &threadResp); err != nil {
		return issueTriageResult{}, fmt.Errorf("start codex triage thread: %w", err)
	}
	if strings.TrimSpace(threadResp.Thread.ID) == "" {
		return issueTriageResult{}, errors.New("codex app-server returned an empty triage thread id")
	}

	var turnResp codexTurnStartResponse
	if err := client.request(ctx, "turn/start", map[string]any{
		"threadId": threadResp.Thread.ID,
		"input": []map[string]any{
			{
				"type":          "text",
				"text":          buildIssueTriagePrompt(detail),
				"text_elements": []map[string]any{},
			},
		},
		"cwd":            cwd,
		"approvalPolicy": "never",
		"sandboxPolicy": map[string]any{
			"type": "dangerFullAccess",
		},
		"responsesapiClientMetadata": map[string]string{
			"mspace.issue_id": issueID,
			"mspace.task":     "issue_type_triage",
		},
	}, &turnResp); err != nil {
		return issueTriageResult{}, fmt.Errorf("start codex triage turn: %w", err)
	}
	if strings.TrimSpace(turnResp.Turn.ID) == "" {
		return issueTriageResult{}, errors.New("codex app-server returned an empty triage turn id")
	}

	message, err := waitCodexTriageTurn(ctx, client, threadResp.Thread.ID, turnResp.Turn.ID)
	if err != nil {
		return issueTriageResult{}, err
	}
	return parseIssueTriageResult(message)
}

func startIssueTriageCodexAppServer(codexPath, cwd string) (*codexAppServerClient, error) {
	cmd := exec.Command(codexPath, "app-server", "--listen", "stdio://")
	cmd.Dir = cwd
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}

	client := &codexAppServerClient{
		cmd:           cmd,
		stdin:         stdin,
		encoder:       json.NewEncoder(stdin),
		pending:       map[int64]chan codexRPCResponse{},
		notifications: make(chan codexRPCNotification, 128),
		waitDone:      make(chan error, 1),
	}
	go client.readLoop(stdout)
	go discardIssueTriageDiagnostics(stderr)
	go func() {
		client.waitDone <- cmd.Wait()
	}()
	return client, nil
}

func discardIssueTriageDiagnostics(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 16*1024), 1024*1024)
	for scanner.Scan() {
	}
}

func waitCodexTriageTurn(ctx context.Context, client *codexAppServerClient, threadID, turnID string) (string, error) {
	var lastAgentMessage string
	for {
		select {
		case <-ctx.Done():
			interruptCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = client.request(interruptCtx, "turn/interrupt", map[string]any{
				"threadId": threadID,
				"turnId":   turnID,
			}, nil)
			cancel()
			return "", ctx.Err()
		case notification, ok := <-client.notifications:
			if !ok {
				return "", errors.New("codex app-server exited before triage completed")
			}
			switch notification.Method {
			case "item/completed":
				var payload codexItemNotification
				if decodeCodexParams(notification.Params, &payload) == nil && payload.ThreadID == threadID && payload.TurnID == turnID && payload.Item.Type == "agentMessage" {
					if strings.TrimSpace(payload.Item.Text) != "" {
						lastAgentMessage = payload.Item.Text
					}
				}
			case "error":
				var payload codexErrorNotification
				if decodeCodexParams(notification.Params, &payload) == nil && payload.ThreadID == threadID && payload.TurnID == turnID && !payload.WillRetry {
					message := payload.Error.Error()
					if message == "" {
						message = "Codex app-server reported an unknown triage error."
					}
					return "", errors.New(message)
				}
			case "turn/completed":
				var payload codexTurnNotification
				if decodeCodexParams(notification.Params, &payload) == nil && payload.ThreadID == threadID && payload.Turn.ID == turnID {
					if payload.Turn.Status != "completed" {
						if payload.Turn.Error != nil && payload.Turn.Error.Error() != "" {
							return "", errors.New(payload.Turn.Error.Error())
						}
						return "", fmt.Errorf("codex triage turn ended with status %s", payload.Turn.Status)
					}
					if strings.TrimSpace(lastAgentMessage) == "" {
						return "", errors.New("codex triage returned an empty response")
					}
					return lastAgentMessage, nil
				}
			}
		}
	}
}

func buildIssueTriageDeveloperInstructions() string {
	return strings.TrimSpace(`
You are an mspace issue triage classifier.

Classify the issue into exactly one Conventional Commit type.
Allowed types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert.

Rules:
- Return only one compact JSON object.
- Do not wrap the JSON in Markdown.
- Do not assign priority.
- Do not change issue status.
- Do not edit files or run commands unless absolutely necessary to understand the issue.
- If the issue is ambiguous, choose chore with lower confidence.
`)
}

func buildIssueTriagePrompt(detail issueDetail) string {
	var builder strings.Builder
	builder.WriteString("# Issue Type Triage\n\n")
	builder.WriteString("Return exactly this JSON shape:\n")
	builder.WriteString(`{"type":"fix","confidence":0.86,"reason":"short reason"}`)
	builder.WriteString("\n\n")
	builder.WriteString("Allowed type values: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert.\n\n")
	builder.WriteString("## Issue\n\n")
	builder.WriteString(fmt.Sprintf("Title: %s\n", detail.Issue.Title))
	builder.WriteString(fmt.Sprintf("Status: %s\n", detail.Issue.Status))
	builder.WriteString("Body:\n")
	builder.WriteString(strings.TrimSpace(detail.Issue.Body))
	builder.WriteString("\n\n")
	builder.WriteString("## Project\n\n")
	builder.WriteString(fmt.Sprintf("Name: %s\n", detail.Project.Name))
	builder.WriteString(fmt.Sprintf("Repository path: %s\n", detail.Project.RepoPath))
	builder.WriteString(fmt.Sprintf("Remote: %s\n", valueOrUnset(detail.Project.RemoteURL)))
	builder.WriteString(fmt.Sprintf("GitHub: %s/%s\n", valueOrUnset(detail.Project.GitOwner), valueOrUnset(detail.Project.GitRepo)))
	return builder.String()
}

func parseIssueTriageResult(value string) (issueTriageResult, error) {
	raw := strings.TrimSpace(value)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return issueTriageResult{}, errors.New("triage response did not contain a JSON object")
	}
	raw = raw[start : end+1]
	var result issueTriageResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return issueTriageResult{}, fmt.Errorf("parse triage JSON: %w", err)
	}
	result.Type = strings.TrimSpace(strings.ToLower(result.Type))
	result.Reason = strings.Join(strings.Fields(result.Reason), " ")
	if !isAllowedIssueTypeLabel(result.Type) {
		return issueTriageResult{}, fmt.Errorf("triage returned unsupported issue type %q", result.Type)
	}
	if result.Confidence < 0 {
		result.Confidence = 0
	}
	if result.Confidence > 1 {
		result.Confidence = 1
	}
	return result, nil
}

func (a *app) applyIssueTypeClassification(issueID string, result issueTriageResult) error {
	definition, err := a.loadIssueLabelDefinitionByKey("type:" + result.Type)
	if err != nil {
		return err
	}
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var triageStatus string
	if err := tx.QueryRow(`SELECT triage_status FROM issues WHERE id = ?`, issueID).Scan(&triageStatus); err != nil {
		return err
	}
	if triageStatus != "pending" {
		return nil
	}

	now := nowString()
	if err := deleteIssueLabelsByDimension(tx, issueID, issueLabelDimensionType); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO issue_labels (id, issue_id, label_id, name, color, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, uuid.NewString(), issueID, definition.ID, definition.Name, definition.Color, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE issues SET triage_status = 'classified', updated_at = ? WHERE id = ?`, now, issueID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE inbox_items SET updated_at = ? WHERE issue_id = ?`, now, issueID); err != nil {
		return err
	}
	comment := fmt.Sprintf("Triage classified type as `%s`.", definition.Name)
	if result.Reason != "" {
		comment = fmt.Sprintf("Triage classified type as `%s`: %s", definition.Name, truncate(result.Reason, 220))
	}
	if _, err := tx.Exec(`
		INSERT INTO comments (id, issue_id, author_type, author_name, author_avatar_url, body, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, uuid.NewString(), issueID, "system", systemActorName, "", comment, now, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	a.publishInboxEvent(issueID, "triaged")
	return nil
}

func (a *app) markIssueTriageFailed(issueID string) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var triageStatus string
	if err := tx.QueryRow(`SELECT triage_status FROM issues WHERE id = ?`, issueID).Scan(&triageStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if triageStatus != "pending" {
		return nil
	}
	now := nowString()
	if _, err := tx.Exec(`UPDATE issues SET triage_status = 'failed', updated_at = ? WHERE id = ?`, now, issueID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE inbox_items SET updated_at = ? WHERE issue_id = ?`, now, issueID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO comments (id, issue_id, author_type, author_name, author_avatar_url, body, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, uuid.NewString(), issueID, "system", systemActorName, "", "Triage could not classify the issue type. Set a type manually.", now, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	a.publishInboxEvent(issueID, "triage-failed")
	return nil
}

func deleteIssueLabelsByDimension(tx *sql.Tx, issueID, dimension string) error {
	_, err := tx.Exec(`
		DELETE FROM issue_labels
		WHERE issue_id = ?
			AND (
				label_id IN (
					SELECT id
					FROM issue_label_definitions
					WHERE dimension = ?
				)
				OR (
					label_id = ''
					AND ? = 'type'
					AND lower(name) IN ('feat', 'fix', 'docs', 'style', 'refactor', 'perf', 'test', 'build', 'ci', 'chore', 'revert')
				)
				OR (
					label_id = ''
					AND ? = 'priority'
					AND lower(name) IN ('p0', 'p1', 'p2', 'p3')
				)
			)
	`, issueID, dimension, dimension, dimension)
	return err
}
