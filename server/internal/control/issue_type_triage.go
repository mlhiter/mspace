package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

const issueTypeTriageTimeout = 2 * time.Minute

type issueTypeTriageResult struct {
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

var errIssueTypeTriageNotNeeded = errors.New("issue does not need type triage")

func (s *Server) startIssueTypeTriage(workspaceID, issueID string) {
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	if workspaceID == "" || issueID == "" {
		return
	}
	go s.triageIssueType(workspaceID, issueID)
}

func (s *Server) triageIssueType(workspaceID, issueID string) {
	ctx, cancel := context.WithTimeout(context.Background(), issueTypeTriageTimeout)
	defer cancel()

	result, err := s.classifyIssueType(ctx, workspaceID, issueID)
	if err != nil {
		if errors.Is(err, errIssueTypeTriageNotNeeded) {
			return
		}
		slog.Warn("issue type triage failed", slog.String("workspace_id", workspaceID), slog.String("issue_id", issueID), slog.String("error", err.Error()))
		if markErr := s.store.MarkIssueTriageFailed(context.Background(), workspaceID, issueID); markErr != nil {
			slog.Warn("failed to mark issue triage failed", slog.String("workspace_id", workspaceID), slog.String("issue_id", issueID), slog.String("error", markErr.Error()))
		}
		return
	}
	if err := s.store.ApplyIssueTypeClassification(context.Background(), workspaceID, issueID, "type:"+result.Type); err != nil {
		slog.Warn("failed to apply issue type triage", slog.String("workspace_id", workspaceID), slog.String("issue_id", issueID), slog.String("error", err.Error()))
		if markErr := s.store.MarkIssueTriageFailed(context.Background(), workspaceID, issueID); markErr != nil {
			slog.Warn("failed to mark issue triage failed", slog.String("workspace_id", workspaceID), slog.String("issue_id", issueID), slog.String("error", markErr.Error()))
		}
	}
}

func (s *Server) classifyIssueType(ctx context.Context, workspaceID, issueID string) (issueTypeTriageResult, error) {
	detail, err := s.loadIssueForTypeTriage(ctx, workspaceID, issueID)
	if err != nil {
		return issueTypeTriageResult{}, err
	}
	if detail.Issue.TriageStatus != "pending" || hasIssueLabelDimension(detail.Labels, issueLabelDimensionType) {
		return issueTypeTriageResult{}, errIssueTypeTriageNotNeeded
	}
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return issueTypeTriageResult{}, errors.New("codex CLI is not available on PATH")
	}
	cwd := strings.TrimSpace(detail.Project.RepoPath)
	if cwd == "" {
		cwd = os.TempDir()
	}
	client, err := startIssueTypeTriageCodexAppServer(codexPath, cwd)
	if err != nil {
		return issueTypeTriageResult{}, err
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
		return issueTypeTriageResult{}, fmt.Errorf("initialize codex app-server: %w", err)
	}

	var threadResp codexThreadStartResponse
	if err := client.request(ctx, "thread/start", map[string]any{
		"cwd":                    cwd,
		"approvalPolicy":         "never",
		"approvalsReviewer":      "user",
		"sandbox":                "danger-full-access",
		"developerInstructions":  buildIssueTypeTriageDeveloperInstructions(),
		"personality":            "pragmatic",
		"ephemeral":              true,
		"sessionStartSource":     "startup",
		"serviceName":            "mspace-triage",
		"experimentalRawEvents":  false,
		"persistExtendedHistory": false,
	}, &threadResp); err != nil {
		return issueTypeTriageResult{}, fmt.Errorf("start codex triage thread: %w", err)
	}
	if strings.TrimSpace(threadResp.Thread.ID) == "" {
		return issueTypeTriageResult{}, errors.New("codex app-server returned an empty triage thread id")
	}

	var turnResp codexTurnStartResponse
	if err := client.request(ctx, "turn/start", map[string]any{
		"threadId": threadResp.Thread.ID,
		"input": []map[string]any{
			{
				"type":          "text",
				"text":          buildIssueTypeTriagePrompt(detail),
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
		return issueTypeTriageResult{}, fmt.Errorf("start codex triage turn: %w", err)
	}
	if strings.TrimSpace(turnResp.Turn.ID) == "" {
		return issueTypeTriageResult{}, errors.New("codex app-server returned an empty triage turn id")
	}

	message, err := waitCodexTitleTurn(ctx, client, threadResp.Thread.ID, turnResp.Turn.ID)
	if err != nil {
		return issueTypeTriageResult{}, err
	}
	return parseIssueTypeTriageResult(message)
}

func (s *Server) loadIssueForTypeTriage(ctx context.Context, workspaceID, issueID string) (IssueDetail, error) {
	switch store := s.store.(type) {
	case *PostgresStore:
		issue, err := loadIssue(ctx, store.pool, workspaceID, issueID)
		if err != nil {
			return IssueDetail{}, err
		}
		project := Project{}
		if issue.ProjectID != "" {
			project, err = resolveIssueProject(ctx, store.pool, workspaceID, issue.ProjectID, "")
			if err != nil {
				return IssueDetail{}, err
			}
		}
		labels, err := listIssueLabels(ctx, store.pool, workspaceID, issueID)
		if err != nil {
			return IssueDetail{}, err
		}
		return IssueDetail{Issue: issue, Project: project, Labels: labels}, nil
	case *MemoryStore:
		store.mu.Lock()
		defer store.mu.Unlock()
		issue, ok := store.issues[strings.TrimSpace(issueID)]
		if !ok || issue.WorkspaceID != strings.TrimSpace(workspaceID) {
			return IssueDetail{}, ErrNotFound
		}
		return IssueDetail{
			Issue:   issue,
			Project: store.projects[issue.ProjectID],
			Labels:  store.issueLabels[issue.ID],
		}, nil
	default:
		return store.GetIssue(ctx, "", workspaceID, issueID)
	}
}

func startIssueTypeTriageCodexAppServer(codexPath, cwd string) (*codexAppServerClient, error) {
	return startCodexAppServer(codexPath, cwd)
}

func buildIssueTypeTriageDeveloperInstructions() string {
	return strings.TrimSpace(`
You are an mspace issue triage classifier.

Classify the issue into exactly one Conventional Commit type.
Allowed types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert.

Rules:
- Return only one compact JSON object.
- Do not wrap the JSON in Markdown.
- Do not assign priority.
- Do not change issue status.
- Do not edit files or run commands.
- If the issue is ambiguous, choose chore with lower confidence.
`)
}

func buildIssueTypeTriagePrompt(detail IssueDetail) string {
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
