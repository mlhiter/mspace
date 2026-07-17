package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

const (
	issueAnalysisAutomation = "issue_analysis"
	issueAnalysisSkillSlug  = "think"
)

var errIssueAnalysisNotNeeded = errors.New("issue analysis does not need to be queued")

func (s *Server) tryEnqueueIssueAnalysis(ctx context.Context, userID, workspaceID, issueID string) {
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	if err := s.enqueueIssueAnalysis(ctx, userID, workspaceID, issueID); err != nil {
		if !errors.Is(err, errIssueAnalysisNotNeeded) {
			slog.Warn("failed to enqueue issue analysis", slog.String("workspace_id", workspaceID), slog.String("issue_id", issueID), slog.String("error", err.Error()))
		}
		return
	}
	if persistent, ok := s.store.(interface{ Persist() error }); ok {
		if err := persistent.Persist(); err != nil {
			slog.Warn("failed to persist issue analysis state", slog.String("workspace_id", workspaceID), slog.String("issue_id", issueID), slog.String("error", err.Error()))
		}
	}
}

func (s *Server) enqueueIssueAnalysis(ctx context.Context, userID, workspaceID, issueID string) error {
	detail, err := s.loadIssueForTypeTriage(ctx, workspaceID, issueID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(detail.Issue.ParentIssueID) != "" || strings.TrimSpace(detail.Issue.ProjectID) == "" {
		return errIssueAnalysisNotNeeded
	}
	exists, err := s.hasIssueAnalysisTask(ctx, workspaceID, issueID)
	if err != nil {
		return err
	}
	if exists {
		return errIssueAnalysisNotNeeded
	}
	_, bundle, err := builtinSkillReference(issueAnalysisSkillSlug)
	if err != nil {
		return err
	}
	workspace := workspaceForTriage(detail, workspaceID)
	_, err = s.store.CreateAgentSession(ctx, userID, workspaceID, issueID, CreateAgentSessionInput{
		AgentEngine:  agentEngineCodex,
		RuntimeMode:  workspace.Kind,
		Command:      buildIssueAnalysisPrompt(detail),
		Automation:   issueAnalysisAutomation,
		SkillBundles: []RuntimeSkillBundle{bundle},
	})
	if errors.Is(err, ErrNoActiveAgentWorker) {
		return errIssueAnalysisNotNeeded
	}
	return err
}

func (s *Server) hasIssueAnalysisTask(ctx context.Context, workspaceID, issueID string) (bool, error) {
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
					AND kind = 'agent_session'
					AND payload->>'automation' = $3
			)
		`, workspaceID, issueID, issueAnalysisAutomation).Scan(&exists); err != nil {
			return false, err
		}
		return exists, nil
	case *SQLiteStore:
		store.MemoryStore.mu.Lock()
		defer store.MemoryStore.mu.Unlock()
		return memoryStoreHasIssueAnalysisTask(store.MemoryStore, workspaceID, issueID), nil
	case *MemoryStore:
		store.mu.Lock()
		defer store.mu.Unlock()
		return memoryStoreHasIssueAnalysisTask(store, workspaceID, issueID), nil
	default:
		return false, nil
	}
}

func memoryStoreHasIssueAnalysisTask(store *MemoryStore, workspaceID, issueID string) bool {
	for _, task := range store.runtimeTasks {
		if task.WorkspaceID == workspaceID &&
			task.IssueID == issueID &&
			task.Kind == "agent_session" &&
			runtimeTaskAutomation(task) == issueAnalysisAutomation {
			return true
		}
	}
	return false
}

func agentSessionPriority(input CreateAgentSessionInput) int {
	if strings.TrimSpace(input.Automation) == issueAnalysisAutomation {
		return 10
	}
	return 0
}

func buildIssueAnalysisPrompt(detail issueTypeTriageDetail) string {
	var builder strings.Builder
	builder.WriteString("# Issue Analysis\n\n")
	builder.WriteString("Use the server-provided `think` skill for this turn. Read `${MSPACE_SESSION_SKILLS_DIR}/think/SKILL.md` before producing the analysis.\n\n")
	builder.WriteString("This is a read-only planning pass. Do not edit files, do not commit, do not create a PR, and do not start implementation work. Inspect the repository only when it materially improves the analysis.\n\n")
	builder.WriteString("Produce a concise issue analysis with:\n")
	builder.WriteString("- problem framing\n")
	builder.WriteString("- likely affected product/code areas\n")
	builder.WriteString("- unknowns or risks\n")
	builder.WriteString("- suggested next action for a future implementation turn\n\n")
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
