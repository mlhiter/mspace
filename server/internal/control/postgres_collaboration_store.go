package control

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) ListProjects(ctx Context, userID, workspaceID string) ([]Project, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(dbctx, `
		SELECT
			p.id::text,
			p.workspace_id::text,
			p.name,
			p.repo_path,
			p.source_type,
			p.remote_url,
			p.git_provider,
			p.git_owner,
			p.git_repo,
			p.default_branch,
			p.deploy_command,
			p.validation_command,
			p.kube_context,
			p.kubeconfig_path,
			p.namespace,
			p.image_registry_prefix,
			p.preview_domain,
			p.ingress_class,
			p.node_host,
			p.default_cluster_id,
			COALESCE(r.status, 'empty'),
			COALESCE(r.updated_at, NULL),
			COALESCE(r.source, ''),
			COALESCE(r.source_session_id, ''),
			COUNT(DISTINCT i.id) FILTER (WHERE i.parent_issue_id IS NULL),
			COUNT(DISTINCT t.id),
			MAX(i.updated_at),
			p.created_at,
			p.updated_at
		FROM projects p
		LEFT JOIN project_runbooks r ON r.project_id = p.id
		LEFT JOIN issues i ON i.project_id = p.id
		LEFT JOIN runtime_tasks t ON t.project_id = p.id::text
		WHERE p.workspace_id = $1
		GROUP BY p.id, r.status, r.updated_at, r.source, r.source_session_id
		ORDER BY p.updated_at DESC, p.created_at DESC
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := []Project{}
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

func (s *PostgresStore) CreateProject(ctx Context, userID, workspaceID string, input ProjectInput) (Project, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	normalized, err := normalizeProjectInput(input)
	if err != nil {
		return Project{}, err
	}
	workspace, err := loadWorkspaceForUser(dbctx, s.pool, workspaceID, userID)
	if err != nil {
		return Project{}, err
	}
	if err := ensureProjectSourceAllowedForWorkspace(workspace, normalized); err != nil {
		return Project{}, err
	}
	gitInfo := gitOwnerRepoFromURL(normalized.RepoURL)
	row := s.pool.QueryRow(dbctx, `
		WITH inserted AS (
			INSERT INTO projects (
				workspace_id,
				name,
				repo_path,
				source_type,
				remote_url,
				git_provider,
				git_owner,
				git_repo,
				default_branch,
				kube_context,
				kubeconfig_path,
				namespace,
				image_registry_prefix,
				preview_domain,
				ingress_class,
				node_host,
				default_cluster_id,
				created_by_user_id
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
			RETURNING *
		),
		runbook AS (
			INSERT INTO project_runbooks (project_id, workspace_id)
			SELECT id, workspace_id FROM inserted
			ON CONFLICT (project_id) DO NOTHING
		)
		SELECT
			i.id::text,
			i.workspace_id::text,
			i.name,
			i.repo_path,
			i.source_type,
			i.remote_url,
			i.git_provider,
			i.git_owner,
			i.git_repo,
			i.default_branch,
			i.deploy_command,
			i.validation_command,
			i.kube_context,
			i.kubeconfig_path,
			i.namespace,
			i.image_registry_prefix,
			i.preview_domain,
			i.ingress_class,
			i.node_host,
			i.default_cluster_id,
			'empty',
			NULL::timestamptz,
			'',
			'',
			0,
			0,
			NULL::timestamptz,
			i.created_at,
			i.updated_at
		FROM inserted i
	`, workspaceID, normalized.Name, normalized.RepoPath, normalized.SourceType, normalized.RepoURL, gitProviderFromURL(normalized.RepoURL), gitInfo.owner, gitInfo.repo, normalized.DefaultBranch, normalized.KubeContext, normalized.KubeconfigPath, normalized.Namespace, normalized.ImageRegistryPrefix, normalized.PreviewDomain, normalized.IngressClass, normalized.NodeHost, normalized.DefaultClusterID, userID)
	return scanProject(row)
}

func (s *PostgresStore) UpdateProject(ctx Context, userID, workspaceID, projectID string, input ProjectInput) (Project, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	normalized, err := normalizeProjectInput(input)
	if err != nil {
		return Project{}, err
	}
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return Project{}, err
	}
	row := s.pool.QueryRow(dbctx, `
		WITH updated AS (
			UPDATE projects
			SET name = $3,
				default_branch = $4,
				kube_context = $5,
				kubeconfig_path = $6,
				namespace = $7,
				image_registry_prefix = $8,
				preview_domain = $9,
				ingress_class = $10,
				node_host = $11,
				default_cluster_id = $12,
				updated_at = now()
			WHERE workspace_id = $1 AND id = $2
			RETURNING *
		)
		SELECT
			u.id::text,
			u.workspace_id::text,
			u.name,
			u.repo_path,
			u.source_type,
			u.remote_url,
			u.git_provider,
			u.git_owner,
			u.git_repo,
			u.default_branch,
			u.deploy_command,
			u.validation_command,
			u.kube_context,
			u.kubeconfig_path,
			u.namespace,
			u.image_registry_prefix,
			u.preview_domain,
			u.ingress_class,
			u.node_host,
			u.default_cluster_id,
			COALESCE(r.status, 'empty'),
			r.updated_at,
			COALESCE(r.source, ''),
			COALESCE(r.source_session_id, ''),
			COUNT(DISTINCT i.id) FILTER (WHERE i.parent_issue_id IS NULL),
			COUNT(DISTINCT t.id),
			MAX(i.updated_at),
			u.created_at,
			u.updated_at
		FROM updated u
		LEFT JOIN project_runbooks r ON r.project_id = u.id
		LEFT JOIN issues i ON i.project_id = u.id
		LEFT JOIN runtime_tasks t ON t.project_id = u.id::text
		GROUP BY u.id, u.workspace_id, u.name, u.repo_path, u.source_type, u.remote_url, u.git_provider, u.git_owner, u.git_repo, u.default_branch, u.deploy_command, u.validation_command, u.kube_context, u.kubeconfig_path, u.namespace, u.image_registry_prefix, u.preview_domain, u.ingress_class, u.node_host, u.default_cluster_id, u.created_at, u.updated_at, r.status, r.updated_at, r.source, r.source_session_id
	`, workspaceID, projectID, normalized.Name, normalized.DefaultBranch, normalized.KubeContext, normalized.KubeconfigPath, normalized.Namespace, normalized.ImageRegistryPrefix, normalized.PreviewDomain, normalized.IngressClass, normalized.NodeHost, normalized.DefaultClusterID)
	project, err := scanProject(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	return project, err
}

func (s *PostgresStore) DeleteProject(ctx Context, userID, workspaceID, projectID string) error {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return err
	}
	var issueCount int
	if err := s.pool.QueryRow(dbctx, `
		SELECT COUNT(*)
		FROM issues
		WHERE workspace_id = $1 AND project_id = $2
	`, workspaceID, projectID).Scan(&issueCount); err != nil {
		return err
	}
	if issueCount > 0 {
		return ErrForbidden
	}
	tag, err := s.pool.Exec(dbctx, `
		DELETE FROM projects
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, projectID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) GetProjectRunbook(ctx Context, userID, workspaceID, projectID string) (ProjectRunbook, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return ProjectRunbook{}, err
	}
	row := s.pool.QueryRow(dbctx, `
		WITH project AS (
			SELECT id, workspace_id FROM projects WHERE workspace_id = $1 AND id = $2
		),
		inserted AS (
			INSERT INTO project_runbooks (project_id, workspace_id)
			SELECT id, workspace_id FROM project
			ON CONFLICT (project_id) DO NOTHING
		)
		SELECT r.project_id::text, r.content, r.status, r.source, r.source_session_id, r.content_hash, r.created_at, r.updated_at
		FROM project_runbooks r
		JOIN project p ON p.id = r.project_id
	`, workspaceID, projectID)
	runbook, err := scanProjectRunbook(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectRunbook{}, ErrNotFound
	}
	return runbook, err
}

func (s *PostgresStore) UpdateProjectRunbook(ctx Context, userID, workspaceID, projectID string, input ProjectRunbookInput) (ProjectRunbook, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return ProjectRunbook{}, err
	}
	status := normalizeRunbookStatus(input.Status, input.Content)
	hash := sha256.Sum256([]byte(input.Content))
	row := s.pool.QueryRow(dbctx, `
		WITH project AS (
			SELECT id, workspace_id FROM projects WHERE workspace_id = $1 AND id = $2
		),
		upserted AS (
			INSERT INTO project_runbooks (project_id, workspace_id, content, status, source, content_hash, updated_at)
			SELECT id, workspace_id, $3, $4, 'human', $5, now() FROM project
			ON CONFLICT (project_id) DO UPDATE
			SET content = EXCLUDED.content,
				status = EXCLUDED.status,
				source = EXCLUDED.source,
				content_hash = EXCLUDED.content_hash,
				updated_at = now()
			RETURNING *
		),
		revision AS (
			INSERT INTO project_runbook_revisions (workspace_id, project_id, author_type, author_name, content, content_hash, status)
			SELECT workspace_id, project_id, 'human', '', content, content_hash, status FROM upserted
		)
		SELECT project_id::text, content, status, source, source_session_id, content_hash, created_at, updated_at
		FROM upserted
	`, workspaceID, projectID, input.Content, status, hex.EncodeToString(hash[:]))
	runbook, err := scanProjectRunbook(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectRunbook{}, ErrNotFound
	}
	return runbook, err
}

func (s *PostgresStore) ListIssueLabelDefinitions(ctx Context, userID, workspaceID string) ([]IssueLabelDefinition, error) {
	dbctx := asContext(ctx)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(dbctx, `
		SELECT id::text, key, name, dimension, color, sort_order, built_in, created_at, updated_at
		FROM issue_label_definitions
		ORDER BY
			CASE dimension WHEN 'type' THEN 0 WHEN 'priority' THEN 1 ELSE 2 END,
			sort_order ASC,
			name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	definitions := []IssueLabelDefinition{}
	for rows.Next() {
		definition, err := scanIssueLabelDefinition(rows)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	return definitions, rows.Err()
}

func (s *PostgresStore) ListIssues(ctx Context, userID, workspaceID string) ([]IssueListItem, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(dbctx, issueListQuery(`
		WHERE i.workspace_id = $1 AND i.parent_issue_id IS NULL
	`, `
		ORDER BY i.updated_at DESC
	`), workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []IssueListItem{}
	for rows.Next() {
		item, err := scanIssueListItem(rows)
		if err != nil {
			return nil, err
		}
		labels, err := s.listIssueLabels(dbctx, item.WorkspaceID, item.ID)
		if err != nil {
			return nil, err
		}
		item.Labels = labels
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) CreateIssue(ctx Context, user User, workspaceID string, input CreateIssueInput) (string, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	normalized, tasks, labels, err := normalizeCreateIssueInput(input, user)
	if err != nil {
		return "", err
	}
	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(dbctx)
	if err := ensureWorkspaceMember(dbctx, tx, workspaceID, user.ID); err != nil {
		return "", err
	}
	projectID, err := resolveOptionalIssueProjectID(dbctx, tx, workspaceID, normalized.ProjectID, normalized.Title+"\n"+normalized.Body)
	if err != nil {
		return "", err
	}
	triageStatus := "pending"
	if hasIssueLabelDimension(labels, issueLabelDimensionType) {
		triageStatus = "classified"
	}
	var issueID string
	err = tx.QueryRow(dbctx, `
		INSERT INTO issues (workspace_id, project_id, title, body, status, triage_status, assignee, assignee_type, creator_user_id, creator_name, creator_avatar_url)
		VALUES ($1, $2, $3, $4, 'open', $5, $6, $7, $8, $9, $10)
		RETURNING id::text
	`, workspaceID, nullableText(projectID), normalized.Title, normalized.Body, triageStatus, normalized.Assignee, normalized.AssigneeType, user.ID, normalized.CreatorName, normalized.CreatorAvatar).Scan(&issueID)
	if err != nil {
		return "", err
	}
	for index, task := range tasks {
		if _, err := tx.Exec(dbctx, `
			INSERT INTO issues (workspace_id, project_id, parent_issue_id, sort_order, title, body, status, triage_status, assignee, assignee_type, creator_user_id, creator_name, creator_avatar_url)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'none', $8, 'human', $9, $10, $11)
			`, workspaceID, nullableText(projectID), issueID, index+1, task.Title, task.Body, normalizeIssueStatus(task.Status), normalized.CreatorName, user.ID, normalized.CreatorName, normalized.CreatorAvatar); err != nil {
			return "", err
		}
	}
	if err := replaceIssueLabels(dbctx, tx, workspaceID, issueID, labels); err != nil {
		return "", err
	}
	if _, err := tx.Exec(dbctx, `
		INSERT INTO comments (workspace_id, issue_id, author_type, author_name, body)
		VALUES ($1, $2, 'system', 'mspace', 'Issue created and ready for review.')
	`, workspaceID, issueID); err != nil {
		return "", err
	}
	if err := tx.Commit(dbctx); err != nil {
		return "", err
	}
	return issueID, nil
}

func (s *PostgresStore) GetIssue(ctx Context, userID, workspaceID, issueID string) (IssueDetail, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return IssueDetail{}, err
	}
	detail := IssueDetail{
		TestEnvironment: nil,
		Sessions:        []AgentSession{},
		Evidence:        []DeploymentEvidence{},
		Failures:        []SessionFailure{},
		ChangeNodes:     []IssueChangeNode{},
		ReviewEvidence:  []SessionReviewEvidence{},
		Handoffs:        []IssueHandoff{},
	}
	row := s.pool.QueryRow(dbctx, `
			SELECT
				i.id::text, i.workspace_id::text, COALESCE(i.project_id::text, ''), COALESCE(i.parent_issue_id::text, ''), i.sort_order, i.title, i.body, i.status, i.close_reason, i.triage_status, i.assignee, i.assignee_type, COALESCE(i.creator_name, ''), COALESCE(i.creator_avatar_url, ''), i.environment_url, i.created_at, i.updated_at,
				COALESCE(p.id::text, ''), COALESCE(p.workspace_id::text, ''), COALESCE(p.name, ''), COALESCE(p.repo_path, ''), COALESCE(p.source_type, ''), COALESCE(p.remote_url, ''), COALESCE(p.git_provider, ''), COALESCE(p.git_owner, ''), COALESCE(p.git_repo, ''), COALESCE(p.default_branch, ''), COALESCE(p.deploy_command, ''), COALESCE(p.validation_command, ''), COALESCE(p.kube_context, ''), COALESCE(p.kubeconfig_path, ''), COALESCE(p.namespace, ''), COALESCE(p.image_registry_prefix, ''), COALESCE(p.preview_domain, ''), COALESCE(p.ingress_class, ''), COALESCE(p.node_host, ''), COALESCE(p.default_cluster_id, ''), COALESCE(r.status, 'empty'), r.updated_at, COALESCE(r.source, ''), COALESCE(r.source_session_id, ''), 0, 0, NULL::timestamptz, p.created_at, p.updated_at
			FROM issues i
			LEFT JOIN projects p ON p.id = i.project_id
			LEFT JOIN project_runbooks r ON r.project_id = p.id
			WHERE i.workspace_id = $1 AND i.id = $2
	`, workspaceID, issueID)
	var err error
	detail.Issue, detail.Project, err = scanIssueAndProject(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return IssueDetail{}, ErrNotFound
	}
	if err != nil {
		return IssueDetail{}, err
	}
	labels, err := s.listIssueLabels(dbctx, workspaceID, issueID)
	if err != nil {
		return IssueDetail{}, err
	}
	detail.Labels = labels
	detail.Issue.WorkspaceID = workspaceID
	detail.ChildIssues, err = s.listChildIssues(dbctx, workspaceID, issueID)
	if err != nil {
		return IssueDetail{}, err
	}
	detail.Comments, err = s.listIssueComments(dbctx, workspaceID, issueID, userID)
	if err != nil {
		return IssueDetail{}, err
	}
	detail.Sessions, err = s.listIssueAgentSessions(dbctx, workspaceID, issueID)
	if err != nil {
		return IssueDetail{}, err
	}
	detail.ChangeNodes, err = s.listIssueChangeNodes(dbctx, workspaceID, issueID)
	if err != nil {
		return IssueDetail{}, err
	}
	detail.Evidence, err = s.listIssueDeploymentEvidence(dbctx, workspaceID, issueID)
	if err != nil {
		return IssueDetail{}, err
	}
	detail.ReviewEvidence, err = s.listIssueReviewEvidence(dbctx, workspaceID, issueID)
	if err != nil {
		return IssueDetail{}, err
	}
	detail.Failures, err = s.listIssueFailures(dbctx, workspaceID, issueID)
	if err != nil {
		return IssueDetail{}, err
	}
	detail.TestEnvironment, err = s.loadIssueTestEnvironmentOptional(dbctx, workspaceID, issueID)
	if err != nil {
		return IssueDetail{}, err
	}
	detail.Handoffs, err = s.listIssueHandoffs(dbctx, workspaceID, issueID)
	if err != nil {
		return IssueDetail{}, err
	}
	return detail, nil
}

func (s *PostgresStore) CreateAgentSession(ctx Context, userID, workspaceID, issueID string, input CreateAgentSessionInput) (AgentSession, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	userID = strings.TrimSpace(userID)
	issueID = strings.TrimSpace(issueID)
	normalized := normalizeCreateAgentSessionInput(input)
	if normalized.Command == "" {
		return AgentSession{}, errors.New("command is required")
	}
	if normalized.Provider != "codex" {
		return AgentSession{}, errors.New("provider must be codex")
	}
	workspace, err := loadWorkspaceForUser(dbctx, s.pool, workspaceID, userID)
	if err != nil {
		return AgentSession{}, err
	}
	if normalized.RuntimeMode == "" {
		normalized.RuntimeMode = workspace.Kind
	}
	if normalized.RuntimeMode != "personal" && normalized.RuntimeMode != "team" {
		return AgentSession{}, errors.New("runtimeMode must be personal or team")
	}
	if normalized.RuntimeMode != workspace.Kind {
		return AgentSession{}, ErrForbidden
	}
	hasActiveWorker, err := s.hasActiveCodexWorker(dbctx, workspaceID, normalized.RuntimeMode)
	if err != nil {
		return AgentSession{}, err
	}
	if !hasActiveWorker {
		return AgentSession{}, ErrNoActiveCodexWorker
	}
	issue, err := loadIssue(dbctx, s.pool, workspaceID, issueID)
	if err != nil {
		return AgentSession{}, err
	}
	if issue.ProjectID == "" {
		return AgentSession{}, errors.New("attach a project before starting an agent session")
	}
	project, err := resolveIssueProject(dbctx, s.pool, workspaceID, issue.ProjectID, "")
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
	runbook, _ := loadProjectRunbookSnapshot(dbctx, s.pool, workspaceID, project.ID)
	comments, err := s.listIssueComments(dbctx, workspaceID, issueID, userID)
	if err != nil {
		return AgentSession{}, err
	}
	labels, err := s.listIssueLabels(dbctx, workspaceID, issueID)
	if err != nil {
		return AgentSession{}, err
	}
	childIssues, err := s.listChildIssues(dbctx, workspaceID, issueID)
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
	task, err := s.CreateRuntimeTask(ctx, userID, workspaceID, CreateRuntimeTaskInput{
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

func (s *PostgresStore) GetSession(ctx Context, userID, workspaceID, sessionID string) (SessionDetail, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	sessionID = strings.TrimSpace(sessionID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return SessionDetail{}, err
	}
	task, err := s.getAgentSessionRuntimeTask(dbctx, workspaceID, sessionID)
	if err != nil {
		return SessionDetail{}, err
	}
	session, err := runtimeTaskToAgentSession(task)
	if err != nil {
		return SessionDetail{}, err
	}
	issue, err := loadIssue(dbctx, s.pool, workspaceID, task.IssueID)
	if err != nil {
		return SessionDetail{}, err
	}
	project := Project{}
	if task.ProjectID != "" {
		project, err = resolveIssueProject(dbctx, s.pool, workspaceID, task.ProjectID, "")
		if err != nil {
			return SessionDetail{}, err
		}
	}
	taskLogs, err := s.ListRuntimeTaskLogs(ctx, userID, workspaceID, task.ID)
	if err != nil {
		return SessionDetail{}, err
	}
	logs := make([]SessionLog, 0, len(taskLogs))
	for _, log := range taskLogs {
		logs = append(logs, SessionLog{
			ID:        log.ID,
			SessionID: session.ID,
			Stream:    log.Stream,
			Message:   log.Message,
			CreatedAt: log.CreatedAt,
		})
	}
	evidence, err := s.listIssueDeploymentEvidence(dbctx, workspaceID, task.IssueID)
	if err != nil {
		return SessionDetail{}, err
	}
	failures, err := s.listIssueFailures(dbctx, workspaceID, task.IssueID)
	if err != nil {
		return SessionDetail{}, err
	}
	sessionEvidence := make([]DeploymentEvidence, 0, len(evidence))
	for _, item := range evidence {
		if item.SessionID == session.ID {
			sessionEvidence = append(sessionEvidence, item)
		}
	}
	sessionFailures := make([]SessionFailure, 0, len(failures))
	for _, item := range failures {
		if item.SessionID == session.ID {
			sessionFailures = append(sessionFailures, item)
		}
	}
	return SessionDetail{
		Session:   session,
		Issue:     issue,
		Project:   project,
		Logs:      logs,
		Evidence:  sessionEvidence,
		Failures:  sessionFailures,
		Workspace: workspaceSnapshotFromRuntimeTask(task),
	}, nil
}

func (s *PostgresStore) getAgentSessionRuntimeTask(ctx context.Context, workspaceID, sessionID string) (RuntimeTask, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			id::text,
			workspace_id::text,
			issue_id,
			session_id,
			project_id,
			kind,
			status,
			priority,
			runtime_mode,
			required_capabilities,
			payload,
			result,
			COALESCE(claimed_by_worker_id::text, ''),
			claimed_at,
			started_at,
			finished_at,
			error,
			created_at,
			updated_at
		FROM runtime_tasks
		WHERE workspace_id = $1
			AND kind = 'agent_session'
			AND (session_id = $2 OR id::text = $2)
	`, workspaceID, sessionID)
	task, err := scanRuntimeTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return RuntimeTask{}, ErrNotFound
	}
	return task, err
}

func loadWorkspaceForUser(ctx context.Context, q queryer, workspaceID, userID string) (Workspace, error) {
	row := q.QueryRow(ctx, `
		SELECT w.id::text, w.name, w.slug, w.kind, wm.role, w.icon, w.description, w.created_at, w.updated_at
		FROM workspace_members wm
		JOIN workspaces w ON w.id = wm.workspace_id
		WHERE wm.workspace_id = $1 AND wm.user_id = $2
	`, workspaceID, userID)
	var workspace Workspace
	var createdAt, updatedAt time.Time
	if err := row.Scan(&workspace.ID, &workspace.Name, &workspace.Slug, &workspace.Kind, &workspace.Role, &workspace.Icon, &workspace.Description, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Workspace{}, ErrNotFound
		}
		return Workspace{}, err
	}
	workspace.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	workspace.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return workspace, nil
}

func loadProjectRunbookSnapshot(ctx context.Context, q queryer, workspaceID, projectID string) (ProjectRunbook, error) {
	row := q.QueryRow(ctx, `
		SELECT project_id::text, content, status, source, source_session_id, content_hash, created_at, updated_at
		FROM project_runbooks
		WHERE workspace_id = $1 AND project_id = $2
	`, workspaceID, projectID)
	runbook, err := scanProjectRunbook(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectRunbook{}, ErrNotFound
	}
	return runbook, err
}

func (s *PostgresStore) UpdateIssue(ctx Context, userID, workspaceID, issueID string, input UpdateIssueInput) (Issue, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return Issue{}, err
	}
	existing, err := loadIssue(dbctx, s.pool, workspaceID, issueID)
	if err != nil {
		return Issue{}, err
	}
	next := existing
	if input.ProjectID != nil {
		projectID := strings.TrimSpace(*input.ProjectID)
		if projectID != "" {
			project, err := resolveIssueProject(dbctx, s.pool, workspaceID, projectID, "")
			if err != nil {
				return Issue{}, err
			}
			next.ProjectID = project.ID
		} else {
			next.ProjectID = ""
		}
	}
	if input.Title != nil {
		next.Title = strings.TrimSpace(*input.Title)
		if next.Title == "" {
			return Issue{}, errors.New("issue title is required")
		}
	}
	if input.Body != nil {
		next.Body = strings.TrimSpace(*input.Body)
	}
	if input.Status != nil {
		status := normalizeIssueStatus(*input.Status)
		if err := validateIssueStatus(status); err != nil {
			return Issue{}, err
		}
		next.Status = status
	}

	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return Issue{}, err
	}
	defer tx.Rollback(dbctx)
	row := tx.QueryRow(dbctx, `
		UPDATE issues
		SET project_id = $3, title = $4, body = $5, status = $6, updated_at = now()
		WHERE workspace_id = $1 AND id = $2
		RETURNING id::text, workspace_id::text, COALESCE(project_id::text, ''), COALESCE(parent_issue_id::text, ''), sort_order, title, body, status, close_reason, triage_status, assignee, assignee_type, creator_name, creator_avatar_url, environment_url, created_at, updated_at
	`, workspaceID, issueID, nullableText(next.ProjectID), next.Title, next.Body, next.Status)
	updated, err := scanIssue(row)
	if err != nil {
		return Issue{}, err
	}
	if input.ProjectID != nil && updated.ParentIssueID == "" {
		if _, err := tx.Exec(dbctx, `
			UPDATE issues
			SET project_id = $3, updated_at = now()
			WHERE workspace_id = $1 AND parent_issue_id = $2
		`, workspaceID, issueID, nullableText(updated.ProjectID)); err != nil {
			return Issue{}, err
		}
	}
	if err := tx.Commit(dbctx); err != nil {
		return Issue{}, err
	}
	return updated, nil
}

func (s *PostgresStore) CreateIssueTask(ctx Context, userID, workspaceID, issueID string, input IssueTaskInput) (IssueListItem, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	task := normalizeIssueTaskInputs([]IssueTaskInput{input})
	if len(task) == 0 {
		return IssueListItem{}, errors.New("task title is required")
	}
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return IssueListItem{}, err
	}
	parent, err := loadIssue(dbctx, s.pool, workspaceID, issueID)
	if err != nil {
		return IssueListItem{}, err
	}
	var taskID string
	err = s.pool.QueryRow(dbctx, `
		WITH next_order AS (
			SELECT COALESCE(MAX(sort_order), 0) + 1 AS sort_order
			FROM issues
			WHERE workspace_id = $1 AND parent_issue_id = $2
		),
		inserted AS (
			INSERT INTO issues (workspace_id, project_id, parent_issue_id, sort_order, title, body, status, triage_status, assignee, assignee_type, creator_user_id, creator_name, creator_avatar_url)
			SELECT $1, $3, $2, sort_order, $4, $5, $6, 'none', $7, 'human', $8, $7, $9
			FROM next_order
			RETURNING id
		)
		SELECT id::text FROM inserted
	`, workspaceID, issueID, nullableText(parent.ProjectID), task[0].Title, task[0].Body, normalizeIssueStatus(task[0].Status), parent.CreatorName, userID, parent.CreatorAvatar).Scan(&taskID)
	if err != nil {
		return IssueListItem{}, err
	}
	items, err := s.listChildIssues(dbctx, workspaceID, issueID)
	if err != nil {
		return IssueListItem{}, err
	}
	for _, item := range items {
		if item.ID == taskID {
			return item, nil
		}
	}
	return IssueListItem{}, ErrNotFound
}

func (s *PostgresStore) DeleteIssueTask(ctx Context, userID, workspaceID, issueID, taskID string) error {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return err
	}
	tag, err := s.pool.Exec(dbctx, `
		DELETE FROM issues
		WHERE workspace_id = $1 AND id = $2 AND parent_issue_id = $3
	`, workspaceID, strings.TrimSpace(taskID), strings.TrimSpace(issueID))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) UpdateIssueLabels(ctx Context, userID, workspaceID, issueID string, input UpdateIssueLabelsInput) ([]IssueLabel, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return nil, err
	}
	if _, err := loadIssue(dbctx, s.pool, workspaceID, issueID); err != nil {
		return nil, err
	}
	labels, err := normalizeIssueLabelKeys(append(input.LabelKeys, input.Labels...))
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(dbctx)
	if err := replaceIssueLabels(dbctx, tx, workspaceID, issueID, labels); err != nil {
		return nil, err
	}
	nextTriageStatus := "none"
	if hasIssueLabelDimension(labels, issueLabelDimensionType) {
		nextTriageStatus = "classified"
	}
	if _, err := tx.Exec(dbctx, `
		UPDATE issues
		SET triage_status = $3, updated_at = now()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, issueID, nextTriageStatus); err != nil {
		return nil, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return nil, err
	}
	return s.listIssueLabels(dbctx, workspaceID, issueID)
}

func (s *PostgresStore) ApplyIssueTypeClassification(ctx Context, workspaceID, issueID string, labelKey string) error {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(dbctx)
	if err := applyIssueTypeClassification(dbctx, tx, workspaceID, issueID, labelKey); err != nil {
		return err
	}
	return tx.Commit(dbctx)
}

func (s *PostgresStore) MarkIssueTriageFailed(ctx Context, workspaceID, issueID string) error {
	dbctx := asContext(ctx)
	return markIssueTriageFailed(dbctx, s.pool, workspaceID, issueID)
}

func (s *PostgresStore) AddComment(ctx Context, user User, workspaceID, issueID string, input CreateCommentInput) (string, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, user.ID); err != nil {
		return "", err
	}
	if _, err := loadIssue(dbctx, s.pool, workspaceID, issueID); err != nil {
		return "", err
	}
	body := strings.TrimSpace(input.Body)
	if body == "" {
		return "", errors.New("comment body is required")
	}
	var commentID string
	if err := s.pool.QueryRow(dbctx, `
		INSERT INTO comments (workspace_id, issue_id, author_type, author_user_id, author_name, author_avatar_url, body)
		VALUES ($1, $2, 'human', $3, $4, $5, $6)
		RETURNING id::text
	`, workspaceID, issueID, user.ID, firstNonEmpty(input.AuthorName, user.Name), firstNonEmpty(input.AuthorAvatar, user.AvatarURL), body).Scan(&commentID); err != nil {
		return "", err
	}
	return commentID, nil
}

func (s *PostgresStore) UpdateComment(ctx Context, user User, workspaceID, issueID, commentID string, input UpdateCommentInput) (Comment, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	commentID = strings.TrimSpace(commentID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, user.ID); err != nil {
		return Comment{}, err
	}
	body := strings.TrimSpace(input.Body)
	if body == "" {
		return Comment{}, errors.New("comment body is required")
	}
	row := s.pool.QueryRow(dbctx, `
		UPDATE comments
		SET body = $5, updated_at = now(), edited_at = now()
		WHERE workspace_id = $1 AND issue_id = $2 AND id = $3 AND author_user_id = $4 AND author_type = 'human'
		RETURNING id::text, issue_id::text, author_type, COALESCE(author_user_id::text, ''), author_name, author_avatar_url, body, created_at, updated_at, edited_at
	`, workspaceID, issueID, commentID, user.ID, body)
	comment, err := scanComment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Comment{}, ErrNotFound
	}
	if err != nil {
		return Comment{}, err
	}
	comment.Reactions, err = listCommentReactionSummaries(dbctx, s.pool, workspaceID, issueID, comment.ID, user.ID)
	return comment, err
}

func (s *PostgresStore) CreateIssueAttachment(ctx Context, userID, workspaceID, issueID string, input CreateIssueAttachmentInput) (IssueAttachment, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, strings.TrimSpace(userID)); err != nil {
		return IssueAttachment{}, err
	}
	if _, err := loadIssue(dbctx, s.pool, workspaceID, issueID); err != nil {
		return IssueAttachment{}, err
	}
	filename := truncateString(strings.TrimSpace(input.Filename), 240)
	if filename == "" {
		filename = "image"
	}
	contentType := normalizeIssueAttachmentContentType(input.ContentType, input.Content)
	if !allowedIssueAttachmentContentType(contentType) {
		return IssueAttachment{}, errors.New("unsupported attachment content type")
	}
	if len(input.Content) == 0 {
		return IssueAttachment{}, errors.New("attachment content is required")
	}
	if len(input.Content) > maxIssueAttachmentBytes {
		return IssueAttachment{}, errors.New("attachment exceeds maximum size")
	}
	row := s.pool.QueryRow(dbctx, `
		INSERT INTO issue_attachments (workspace_id, issue_id, filename, content_type, size_bytes, storage_backend, content)
		VALUES ($1, $2, $3, $4, $5, 'postgres_blob', $6)
		RETURNING id::text, workspace_id::text, COALESCE(issue_id::text, ''), COALESCE(comment_id::text, ''), filename, content_type, size_bytes, storage_backend, storage_key, content, created_at, updated_at, bound_at
	`, workspaceID, issueID, filename, contentType, len(input.Content), input.Content)
	return scanIssueAttachment(row)
}

func (s *PostgresStore) GetIssueAttachment(ctx Context, userID, attachmentID string) (IssueAttachment, error) {
	dbctx := asContext(ctx)
	attachmentID = strings.TrimSpace(attachmentID)
	userID = strings.TrimSpace(userID)
	if attachmentID == "" || userID == "" {
		return IssueAttachment{}, ErrNotFound
	}
	row := s.pool.QueryRow(dbctx, `
		SELECT a.id::text, a.workspace_id::text, COALESCE(a.issue_id::text, ''), COALESCE(a.comment_id::text, ''), a.filename, a.content_type, a.size_bytes, a.storage_backend, a.storage_key, a.content, a.created_at, a.updated_at, a.bound_at
		FROM issue_attachments a
		JOIN workspace_members wm ON wm.workspace_id = a.workspace_id AND wm.user_id = $2
		WHERE a.id = $1
	`, attachmentID, userID)
	attachment, err := scanIssueAttachment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return IssueAttachment{}, ErrNotFound
	}
	return attachment, err
}

func (s *PostgresStore) SetCommentReaction(ctx Context, user User, workspaceID, issueID, commentID, reaction string) error {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	commentID = strings.TrimSpace(commentID)
	reaction = strings.TrimSpace(reaction)
	if err := validateCommentReaction(reaction); err != nil {
		return err
	}
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, user.ID); err != nil {
		return err
	}
	if err := ensureCommentInIssue(dbctx, s.pool, workspaceID, issueID, commentID); err != nil {
		return err
	}
	_, err := s.pool.Exec(dbctx, `
		INSERT INTO comment_reactions (workspace_id, issue_id, comment_id, reaction, user_id, actor_name, actor_avatar_url)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (comment_id, user_id, reaction) DO NOTHING
	`, workspaceID, issueID, commentID, reaction, user.ID, user.Name, user.AvatarURL)
	return err
}

func (s *PostgresStore) DeleteCommentReaction(ctx Context, userID, workspaceID, issueID, commentID, reaction string) error {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	commentID = strings.TrimSpace(commentID)
	reaction = strings.TrimSpace(reaction)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return err
	}
	if err := ensureCommentInIssue(dbctx, s.pool, workspaceID, issueID, commentID); err != nil {
		return err
	}
	_, err := s.pool.Exec(dbctx, `
		DELETE FROM comment_reactions
		WHERE workspace_id = $1 AND issue_id = $2 AND comment_id = $3 AND reaction = $4 AND user_id = $5
	`, workspaceID, issueID, commentID, reaction, userID)
	return err
}

func scanProject(row scanner) (Project, error) {
	var project Project
	var runbookUpdatedAt, latestIssueUpdatedAt sql.NullTime
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&project.ID,
		&project.WorkspaceID,
		&project.Name,
		&project.RepoPath,
		&project.SourceType,
		&project.RemoteURL,
		&project.GitProvider,
		&project.GitOwner,
		&project.GitRepo,
		&project.DefaultBranch,
		&project.DeployCommand,
		&project.ValidationCommand,
		&project.KubeContext,
		&project.KubeconfigPath,
		&project.Namespace,
		&project.ImageRegistryPrefix,
		&project.PreviewDomain,
		&project.IngressClass,
		&project.NodeHost,
		&project.DefaultClusterID,
		&project.RunbookStatus,
		&runbookUpdatedAt,
		&project.RunbookSource,
		&project.RunbookSourceSessionID,
		&project.IssueCount,
		&project.SessionCount,
		&latestIssueUpdatedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Project{}, err
	}
	if runbookUpdatedAt.Valid {
		project.RunbookUpdatedAt = runbookUpdatedAt.Time.UTC().Format(time.RFC3339)
	}
	if latestIssueUpdatedAt.Valid {
		project.LatestIssueUpdatedAt = latestIssueUpdatedAt.Time.UTC().Format(time.RFC3339)
	}
	project.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	project.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return project, nil
}

func scanProjectRunbook(row scanner) (ProjectRunbook, error) {
	var runbook ProjectRunbook
	var createdAt, updatedAt time.Time
	if err := row.Scan(&runbook.ProjectID, &runbook.Content, &runbook.Status, &runbook.Source, &runbook.SourceSessionID, &runbook.ContentHash, &createdAt, &updatedAt); err != nil {
		return ProjectRunbook{}, err
	}
	runbook.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	runbook.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return runbook, nil
}

func scanIssue(row scanner) (Issue, error) {
	var issue Issue
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&issue.ID,
		&issue.WorkspaceID,
		&issue.ProjectID,
		&issue.ParentIssueID,
		&issue.SortOrder,
		&issue.Title,
		&issue.Body,
		&issue.Status,
		&issue.CloseReason,
		&issue.TriageStatus,
		&issue.Assignee,
		&issue.AssigneeType,
		&issue.CreatorName,
		&issue.CreatorAvatar,
		&issue.EnvironmentURL,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Issue{}, err
	}
	issue.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	issue.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return issue, nil
}

func scanIssueListItem(row scanner) (IssueListItem, error) {
	var item IssueListItem
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&item.ID,
		&item.WorkspaceID,
		&item.ProjectID,
		&item.ProjectName,
		&item.ParentIssueID,
		&item.SortOrder,
		&item.Title,
		&item.Body,
		&item.Status,
		&item.CloseReason,
		&item.TriageStatus,
		&item.Assignee,
		&item.AssigneeType,
		&item.SessionCount,
		&item.ChildIssueCount,
		&item.CompletedChildIssueCount,
		&updatedAt,
		&createdAt,
	); err != nil {
		return IssueListItem{}, err
	}
	item.Labels = []IssueLabel{}
	item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	return item, nil
}

func scanIssueLabelDefinition(row scanner) (IssueLabelDefinition, error) {
	var definition IssueLabelDefinition
	var createdAt, updatedAt time.Time
	if err := row.Scan(&definition.ID, &definition.Key, &definition.Name, &definition.Dimension, &definition.Color, &definition.SortOrder, &definition.BuiltIn, &createdAt, &updatedAt); err != nil {
		return IssueLabelDefinition{}, err
	}
	definition.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	definition.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return definition, nil
}

func scanIssueLabel(row scanner) (IssueLabel, error) {
	var label IssueLabel
	var createdAt time.Time
	if err := row.Scan(&label.ID, &label.IssueID, &label.LabelID, &label.Key, &label.Name, &label.Dimension, &label.Color, &label.SortOrder, &createdAt); err != nil {
		return IssueLabel{}, err
	}
	label.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	return label, nil
}

func scanComment(row scanner) (Comment, error) {
	var comment Comment
	var editedAt sql.NullTime
	var createdAt, updatedAt time.Time
	if err := row.Scan(&comment.ID, &comment.IssueID, &comment.AuthorType, &comment.AuthorUserID, &comment.AuthorName, &comment.AuthorAvatar, &comment.Body, &createdAt, &updatedAt, &editedAt); err != nil {
		return Comment{}, err
	}
	comment.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	comment.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	if editedAt.Valid {
		comment.EditedAt = editedAt.Time.UTC().Format(time.RFC3339)
	}
	comment.Reactions = []CommentReactionSummary{}
	return comment, nil
}

func scanIssueAttachment(row scanner) (IssueAttachment, error) {
	var attachment IssueAttachment
	var boundAt sql.NullTime
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&attachment.ID,
		&attachment.WorkspaceID,
		&attachment.IssueID,
		&attachment.CommentID,
		&attachment.Filename,
		&attachment.ContentType,
		&attachment.SizeBytes,
		&attachment.StorageBackend,
		&attachment.StorageKey,
		&attachment.Content,
		&createdAt,
		&updatedAt,
		&boundAt,
	); err != nil {
		return IssueAttachment{}, err
	}
	attachment.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	attachment.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	if boundAt.Valid {
		attachment.BoundAt = boundAt.Time.UTC().Format(time.RFC3339)
	}
	return attachment, nil
}

func scanDeploymentEvidence(row scanner) (DeploymentEvidence, error) {
	var evidence DeploymentEvidence
	var createdAt time.Time
	if err := row.Scan(&evidence.ID, &evidence.IssueID, &evidence.SessionID, &evidence.Cluster, &evidence.Namespace, &evidence.Summary, &evidence.Details, &createdAt); err != nil {
		return DeploymentEvidence{}, err
	}
	evidence.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	return evidence, nil
}

func scanSessionFailure(row scanner) (SessionFailure, error) {
	var failure SessionFailure
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&failure.ID,
		&failure.IssueID,
		&failure.SessionID,
		&failure.Phase,
		&failure.Status,
		&failure.FailedCommand,
		&failure.ErrorSummary,
		&failure.ErrorExcerpt,
		&failure.Cluster,
		&failure.Namespace,
		&failure.ResourceKind,
		&failure.ResourceName,
		&failure.EvidenceID,
		&failure.ReviewEvidenceID,
		&failure.RetrySessionID,
		&failure.ContinuedSessionID,
		&createdAt,
		&updatedAt,
	); err != nil {
		return SessionFailure{}, err
	}
	failure.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	failure.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return failure, nil
}

func scanSessionReviewEvidence(row scanner) (SessionReviewEvidence, error) {
	var review SessionReviewEvidence
	var commandsBytes, testsBytes, buildBytes, deploymentBytes, risksBytes, followUpsBytes []byte
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&review.ID,
		&review.IssueID,
		&review.SessionID,
		&review.SourceSessionID,
		&review.SourceCommitSHA,
		&review.Branch,
		&review.AgentSummary,
		&commandsBytes,
		&testsBytes,
		&buildBytes,
		&deploymentBytes,
		&risksBytes,
		&followUpsBytes,
		&review.PreviewURL,
		&review.Cluster,
		&review.Namespace,
		&review.NamespaceStatus,
		&review.CleanupStatus,
		&createdAt,
		&updatedAt,
	); err != nil {
		return SessionReviewEvidence{}, err
	}
	review.CommandsRun = decodeJSONSlice[ReviewEvidenceCommand](commandsBytes)
	review.Tests = decodeJSONSlice[ReviewEvidenceCheck](testsBytes)
	review.BuildResult = decodeJSONStruct[ReviewEvidenceResult](buildBytes)
	review.DeploymentResult = decodeJSONStruct[ReviewEvidenceResult](deploymentBytes)
	review.Risks = decodeJSONSlice[string](risksBytes)
	review.FollowUps = decodeJSONSlice[string](followUpsBytes)
	review.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	review.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return review, nil
}

func decodeJSONSlice[T any](payload []byte) []T {
	var values []T
	if err := json.Unmarshal(payload, &values); err != nil || values == nil {
		return []T{}
	}
	return values
}

func decodeJSONStruct[T any](payload []byte) T {
	var value T
	_ = json.Unmarshal(payload, &value)
	return value
}

func (s *PostgresStore) listIssueAgentSessions(ctx context.Context, workspaceID, issueID string) ([]AgentSession, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			id::text,
			issue_id,
			session_id,
			project_id,
			status,
			runtime_mode,
			payload,
			result,
			error,
			created_at,
			updated_at
		FROM runtime_tasks
		WHERE workspace_id = $1 AND issue_id = $2 AND kind = 'agent_session'
		ORDER BY created_at DESC, id DESC
	`, strings.TrimSpace(workspaceID), strings.TrimSpace(issueID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := []AgentSession{}
	for rows.Next() {
		session, err := scanRuntimeTaskAgentSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (s *PostgresStore) listIssueChangeNodes(ctx context.Context, workspaceID, issueID string) ([]IssueChangeNode, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			id::text,
			issue_id,
			session_id,
			project_id,
			status,
			runtime_mode,
			payload,
			result,
			error,
			created_at,
			updated_at
		FROM runtime_tasks
		WHERE workspace_id = $1 AND issue_id = $2 AND kind = 'agent_session'
		ORDER BY created_at DESC, id DESC
	`, strings.TrimSpace(workspaceID), strings.TrimSpace(issueID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	nodes := []IssueChangeNode{}
	for rows.Next() {
		task, err := scanRuntimeTaskAgentSessionRow(rows)
		if err != nil {
			return nil, err
		}
		node := runtimeTaskChangeNode(task)
		if node.CommitSHA == "" && node.Error == "" {
			continue
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func (s *PostgresStore) listIssueDeploymentEvidence(ctx context.Context, workspaceID, issueID string) ([]DeploymentEvidence, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, issue_id::text, session_id, cluster, namespace, summary, details, created_at
		FROM deployment_evidence
		WHERE workspace_id = $1 AND issue_id = $2
		ORDER BY created_at DESC, id DESC
	`, strings.TrimSpace(workspaceID), strings.TrimSpace(issueID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	evidence := []DeploymentEvidence{}
	for rows.Next() {
		item, err := scanDeploymentEvidence(rows)
		if err != nil {
			return nil, err
		}
		evidence = append(evidence, item)
	}
	return evidence, rows.Err()
}

func (s *PostgresStore) listIssueReviewEvidence(ctx context.Context, workspaceID, issueID string) ([]SessionReviewEvidence, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, issue_id::text, session_id, source_session_id, source_commit_sha, branch, agent_summary, commands_json, tests_json, build_result_json, deployment_result_json, risks_json, follow_ups_json, preview_url, cluster, namespace, namespace_status, cleanup_status, created_at, updated_at
		FROM session_review_evidence
		WHERE workspace_id = $1 AND issue_id = $2
		ORDER BY updated_at DESC, id DESC
	`, strings.TrimSpace(workspaceID), strings.TrimSpace(issueID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	reviews := []SessionReviewEvidence{}
	for rows.Next() {
		review, err := scanSessionReviewEvidence(rows)
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, review)
	}
	return reviews, rows.Err()
}

func (s *PostgresStore) listIssueFailures(ctx context.Context, workspaceID, issueID string) ([]SessionFailure, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, issue_id::text, session_id, phase, status, failed_command, error_summary, error_excerpt, cluster, namespace, resource_kind, resource_name, evidence_id, review_evidence_id, retry_session_id, continued_session_id, created_at, updated_at
		FROM session_failures
		WHERE workspace_id = $1 AND issue_id = $2
		ORDER BY updated_at DESC, id DESC
	`, strings.TrimSpace(workspaceID), strings.TrimSpace(issueID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	failures := []SessionFailure{}
	for rows.Next() {
		failure, err := scanSessionFailure(rows)
		if err != nil {
			return nil, err
		}
		failures = append(failures, failure)
	}
	return failures, rows.Err()
}

func scanRuntimeTaskAgentSession(row scanner) (AgentSession, error) {
	task, err := scanRuntimeTaskAgentSessionRow(row)
	if err != nil {
		return AgentSession{}, err
	}
	return runtimeTaskToAgentSession(task)
}

func scanRuntimeTaskAgentSessionRow(row scanner) (RuntimeTask, error) {
	var runtimeTaskID, issueID, sessionID, projectID, status, runtimeMode, errorText string
	var payloadBytes, resultBytes []byte
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&runtimeTaskID,
		&issueID,
		&sessionID,
		&projectID,
		&status,
		&runtimeMode,
		&payloadBytes,
		&resultBytes,
		&errorText,
		&createdAt,
		&updatedAt,
	); err != nil {
		return RuntimeTask{}, err
	}
	return RuntimeTask{
		ID:                   runtimeTaskID,
		IssueID:              issueID,
		SessionID:            sessionID,
		ProjectID:            projectID,
		Kind:                 "agent_session",
		Status:               status,
		RuntimeMode:          runtimeMode,
		RequiredCapabilities: json.RawMessage(`{}`),
		Payload:              copyRawMessage(json.RawMessage(payloadBytes)),
		Result:               copyRawMessage(json.RawMessage(resultBytes)),
		Error:                errorText,
		CreatedAt:            createdAt.UTC().Format(time.RFC3339),
		UpdatedAt:            updatedAt.UTC().Format(time.RFC3339),
	}, nil
}

func runtimeTaskToAgentSession(task RuntimeTask) (AgentSession, error) {
	var payload struct {
		Prompt           string `json:"prompt"`
		AgentProfile     string `json:"agentProfile"`
		Provider         string `json:"provider"`
		Branch           string `json:"branch"`
		SourceSessionID  string `json:"sourceSessionId"`
		SourceCommitSHA  string `json:"sourceCommitSha"`
		TriggerCommentID string `json:"triggerCommentId"`
		ArtifactDir      string `json:"artifactDir"`
		Automation       string `json:"automation"`
		TestRunID        string `json:"testRunId"`
	}
	_ = json.Unmarshal(task.Payload, &payload)

	var result struct {
		ThreadID    string `json:"threadId"`
		TurnID      string `json:"turnId"`
		Workdir     string `json:"workdir"`
		ArtifactDir string `json:"artifactDir"`
		Source      struct {
			CommitSHA string `json:"commitSha"`
			Branch    string `json:"branch"`
		} `json:"source"`
	}
	_ = json.Unmarshal(task.Result, &result)

	sessionStatus, agentStatus := runtimeTaskSessionStatus(task.Status, task.Error, task.RuntimeMode)
	return AgentSession{
		ID:               firstNonEmpty(task.SessionID, task.ID),
		IssueID:          task.IssueID,
		Provider:         firstNonEmpty(payload.Provider, "codex"),
		AgentProfile:     firstNonEmpty(payload.AgentProfile, "codex"),
		RuntimeMode:      firstNonEmpty(task.RuntimeMode, "team"),
		RuntimeTaskID:    task.ID,
		Command:          strings.TrimSpace(payload.Prompt),
		Status:           sessionStatus,
		Branch:           firstNonEmpty(result.Source.Branch, payload.Branch),
		Workdir:          result.Workdir,
		CodexThreadID:    result.ThreadID,
		CodexTurnID:      result.TurnID,
		AgentStatus:      agentStatus,
		ArtifactDir:      firstNonEmpty(result.ArtifactDir, payload.ArtifactDir),
		SourceSessionID:  payload.SourceSessionID,
		SourceCommitSHA:  firstNonEmpty(result.Source.CommitSHA, payload.SourceCommitSHA),
		TriggerCommentID: payload.TriggerCommentID,
		CleanupStatus:    "retained",
		CreatedAt:        task.CreatedAt,
		UpdatedAt:        task.UpdatedAt,
	}, nil
}

func runtimeTaskSessionStatus(status, errorText, runtimeMode string) (string, string) {
	prefix := "runtime"
	switch strings.TrimSpace(runtimeMode) {
	case "team":
		prefix = "team-runtime"
	case "personal":
		prefix = "personal-runtime"
	}
	switch strings.TrimSpace(status) {
	case "queued":
		return "queued", prefix + "-queued"
	case "claimed":
		return "running", prefix + "-claimed"
	case "running":
		return "running", prefix + "-running"
	case "completed":
		return "completed", "completed"
	case "failed":
		return "failed", "failed"
	case "cancelled":
		return "cancelled", "cancelled"
	default:
		if strings.TrimSpace(errorText) != "" {
			return "failed", "failed"
		}
		return "queued", prefix
	}
}

func buildAgentSessionPayload(sessionID string, issue Issue, project Project, runbook ProjectRunbook, comments []Comment, labels []IssueLabel, childIssues []IssueListItem, input CreateAgentSessionInput) map[string]any {
	return map[string]any{
		"workdir":               "",
		"provider":              input.Provider,
		"prompt":                input.Command,
		"developerInstructions": defaultAgentSessionDeveloperInstructions(input.RuntimeMode),
		"approvalPolicy":        "never",
		"sandbox":               "danger-full-access",
		"env": map[string]string{
			"MSPACE_API_BASE_URL":      "",
			"MSPACE_ISSUE_ID":          issue.ID,
			"MSPACE_SESSION_ID":        sessionID,
			"MSPACE_AGENT_PROFILE":     input.AgentProfile,
			"MSPACE_SESSION_BRANCH":    input.Branch,
			"MSPACE_SOURCE_SESSION_ID": input.SourceSessionID,
			"MSPACE_SOURCE_COMMIT_SHA": input.SourceCommitSHA,
		},
		"issueId":          issue.ID,
		"sessionId":        sessionID,
		"projectId":        project.ID,
		"agentProfile":     input.AgentProfile,
		"branch":           input.Branch,
		"sourceSessionId":  input.SourceSessionID,
		"sourceCommitSha":  input.SourceCommitSHA,
		"triggerCommentId": input.TriggerCommentID,
		"automation":       input.Automation,
		"testRunId":        input.TestRunID,
		"contextMarkdown":  buildAgentSessionContext(issue, project, runbook, comments, labels, childIssues, input),
		"artifactDir":      "",
		"repository": map[string]string{
			"url":           firstNonEmpty(project.RemoteURL, project.RepoPath),
			"defaultBranch": project.DefaultBranch,
			"sourceType":    project.SourceType,
			"provider":      project.GitProvider,
			"owner":         project.GitOwner,
			"repo":          project.GitRepo,
		},
	}
}

func defaultAgentSessionDeveloperInstructions(runtimeMode string) string {
	mode := "runtime worker"
	if strings.TrimSpace(runtimeMode) == "team" {
		mode = "team runtime worker"
	} else if strings.TrimSpace(runtimeMode) == "personal" {
		mode = "personal runtime worker"
	}
	return strings.TrimSpace(`
You are running as a Codex coding agent inside an mspace ` + mode + `.

Follow these mspace rules:
- Work in the provided workdir for this task.
- Inspect the repository before changing code.
- Keep changes focused on the issue and avoid unrelated refactors.
- Do not push, create a pull request, or delete workdirs unless the task prompt explicitly asks for it.
- Run relevant validation when practical, and report exactly what passed or failed.
- Do not start or keep a development server running unless the user explicitly asks for a preview or a live server.
- For ordinary validation, prefer non-interactive checks such as lint, tests, typecheck, build, or short one-shot HTTP probes.
- If a temporary server is required for validation, stop it before finishing and report it only as an internal validation step.
	- Do not present container-local localhost or 127.0.0.1 URLs as user-accessible preview URLs. Only report a URL when mspace provides an explicit preview/test-environment URL or the user asked for a local preview and the host mapping is known.
	- Answer directly. Do not introduce yourself.
	- If you make source-code changes, write ${MSPACE_SESSION_ARTIFACT_DIR}/branch-name.json before finishing. Use JSON like { "branch": "fix/short-semantic-name" }. The branch must use a Conventional Commit type prefix such as feat/, fix/, chore/, docs/, refactor/, test/, perf/, build/, or ci/, and the slug should summarize the actual diff in lowercase words separated by hyphens.
	- Finish with a concise summary of changes, validation, and remaining risks.
	`)
}

func buildAgentSessionContext(issue Issue, project Project, runbook ProjectRunbook, comments []Comment, labels []IssueLabel, childIssues []IssueListItem, input CreateAgentSessionInput) string {
	var builder strings.Builder
	builder.WriteString("# mspace agent session\n\n")
	builder.WriteString("## Issue\n")
	builder.WriteString("ID: " + issue.ID + "\n")
	builder.WriteString("Title: " + issue.Title + "\n")
	builder.WriteString("Status: " + issue.Status + "\n")
	if issue.Body != "" {
		builder.WriteString("\n" + issue.Body + "\n")
	}
	if len(labels) > 0 {
		builder.WriteString("\n## Labels\n")
		for _, label := range labels {
			builder.WriteString("- " + label.Dimension + ": " + label.Name + "\n")
		}
	}
	if len(childIssues) > 0 {
		builder.WriteString("\n## Child Issues\n")
		for _, child := range childIssues {
			builder.WriteString("- [" + child.Status + "] " + child.Title + "\n")
		}
	}
	builder.WriteString("\n## Project\n")
	builder.WriteString("Name: " + project.Name + "\n")
	builder.WriteString("Repository: " + firstNonEmpty(project.RemoteURL, project.RepoPath, "not configured") + "\n")
	builder.WriteString("Default branch: " + firstNonEmpty(project.DefaultBranch, "main") + "\n")
	if runbook.Content != "" {
		builder.WriteString("\n## Project Runbook\n")
		builder.WriteString(runbook.Content + "\n")
	}
	if len(comments) > 0 {
		builder.WriteString("\n## Recent Comments\n")
		maxComments := len(comments)
		if maxComments > 12 {
			maxComments = 12
		}
		for i := maxComments - 1; i >= 0; i-- {
			comment := comments[i]
			author := firstNonEmpty(comment.AuthorName, comment.AuthorType, "unknown")
			builder.WriteString("\n### " + author + " at " + comment.CreatedAt + "\n")
			builder.WriteString(comment.Body + "\n")
		}
	}
	builder.WriteString("\n## Current Request\n")
	builder.WriteString(input.Command + "\n")
	return builder.String()
}

func defaultAgentSessionBranch(issueID, sessionID string) string {
	return "mspace/" + shortIdentifier(issueID) + "/" + shortSessionIdentifier(sessionID)
}

func shortIdentifier(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "-")
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

func shortSessionIdentifier(value string) string {
	trimmed := strings.Trim(strings.TrimSpace(value), "-")
	if suffix := strings.Trim(strings.TrimPrefix(trimmed, "session-"), "-"); suffix != "" && suffix != trimmed {
		return shortIdentifier(suffix)
	}
	return shortIdentifier(trimmed)
}

func runtimeTaskChangeNode(task RuntimeTask) IssueChangeNode {
	source := runtimeTaskSource(task)
	if source.CommitSHA == "" && strings.TrimSpace(task.Error) == "" {
		return IssueChangeNode{}
	}
	return IssueChangeNode{
		ID:             firstNonEmpty(source.CommitSHA, task.ID),
		IssueID:        task.IssueID,
		SessionID:      firstNonEmpty(task.SessionID, task.ID),
		CommitSHA:      source.CommitSHA,
		ShortCommitSHA: firstNonEmpty(source.ShortCommitSHA, shortCommitSHA(source.CommitSHA)),
		Branch:         source.Branch,
		Subject:        source.Subject,
		FilesChanged:   source.FilesChanged,
		Changes:        source.Changes,
		DiffPreview:    source.DiffPreview,
		DiffTruncated:  source.DiffTruncated,
		Error:          firstNonEmpty(source.Error, task.Error),
		Source:         "runtime-task",
		RemoteWorkdir:  source.Workdir,
		ArtifactDir:    source.ArtifactDir,
		CreatedAt:      task.UpdatedAt,
	}
}

type runtimeTaskSourceSnapshot struct {
	CommitSHA      string            `json:"commitSha"`
	ShortCommitSHA string            `json:"shortCommitSha"`
	Branch         string            `json:"branch"`
	Subject        string            `json:"subject"`
	FilesChanged   int               `json:"filesChanged"`
	Changes        []WorkspaceChange `json:"changes"`
	DiffPreview    string            `json:"diffPreview"`
	DiffTruncated  bool              `json:"diffTruncated"`
	Error          string            `json:"error"`
	Workdir        string            `json:"workdir"`
	ArtifactDir    string            `json:"artifactDir"`
}

func runtimeTaskSource(task RuntimeTask) runtimeTaskSourceSnapshot {
	var result struct {
		Workdir     string                    `json:"workdir"`
		ArtifactDir string                    `json:"artifactDir"`
		Source      runtimeTaskSourceSnapshot `json:"source"`
	}
	_ = json.Unmarshal(task.Result, &result)
	source := result.Source
	source.Workdir = firstNonEmpty(source.Workdir, result.Workdir)
	source.ArtifactDir = firstNonEmpty(source.ArtifactDir, result.ArtifactDir)
	if source.Branch == "" {
		var payload struct {
			Branch string `json:"branch"`
		}
		_ = json.Unmarshal(task.Payload, &payload)
		source.Branch = payload.Branch
	}
	if source.Error == "" && task.Status == "failed" {
		source.Error = task.Error
	}
	if source.Changes == nil {
		source.Changes = []WorkspaceChange{}
	}
	return source
}

func workspaceSnapshotFromRuntimeTask(task RuntimeTask) WorkspaceSnapshot {
	source := runtimeTaskSource(task)
	hasChanges := source.CommitSHA != "" || len(source.Changes) > 0
	return WorkspaceSnapshot{
		Exists:          source.Workdir != "",
		IsGitRepository: source.Workdir != "",
		HasChanges:      hasChanges,
		ChangedFiles:    source.FilesChanged,
		UntrackedFiles:  0,
		Head:            source.CommitSHA,
		ShortHead:       firstNonEmpty(source.ShortCommitSHA, shortCommitSHA(source.CommitSHA)),
		Branch:          source.Branch,
		StatusLines:     []string{},
		Changes:         source.Changes,
		DiffPreview:     source.DiffPreview,
		DiffTruncated:   source.DiffTruncated,
		Comparison: WorkspaceComparison{
			BaseRef:        "",
			MergeBase:      "",
			MergeBaseShort: "",
			AheadCount:     boolToInt(source.CommitSHA != ""),
			BehindCount:    0,
			CommitLines:    []string{},
			Changes:        source.Changes,
			DiffPreview:    source.DiffPreview,
			DiffTruncated:  source.DiffTruncated,
			Error:          source.Error,
		},
		Error: source.Error,
	}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func shortCommitSHA(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func scanIssueAndProject(row scanner) (Issue, Project, error) {
	var issue Issue
	var project Project
	var issueCreatedAt, issueUpdatedAt time.Time
	var runbookUpdatedAt, latestIssueUpdatedAt sql.NullTime
	var projectCreatedAt, projectUpdatedAt sql.NullTime
	if err := row.Scan(
		&issue.ID, &issue.WorkspaceID, &issue.ProjectID, &issue.ParentIssueID, &issue.SortOrder, &issue.Title, &issue.Body, &issue.Status, &issue.CloseReason, &issue.TriageStatus, &issue.Assignee, &issue.AssigneeType, &issue.CreatorName, &issue.CreatorAvatar, &issue.EnvironmentURL, &issueCreatedAt, &issueUpdatedAt,
		&project.ID, &project.WorkspaceID, &project.Name, &project.RepoPath, &project.SourceType, &project.RemoteURL, &project.GitProvider, &project.GitOwner, &project.GitRepo, &project.DefaultBranch, &project.DeployCommand, &project.ValidationCommand, &project.KubeContext, &project.KubeconfigPath, &project.Namespace, &project.ImageRegistryPrefix, &project.PreviewDomain, &project.IngressClass, &project.NodeHost, &project.DefaultClusterID, &project.RunbookStatus, &runbookUpdatedAt, &project.RunbookSource, &project.RunbookSourceSessionID, &project.IssueCount, &project.SessionCount, &latestIssueUpdatedAt, &projectCreatedAt, &projectUpdatedAt,
	); err != nil {
		return Issue{}, Project{}, err
	}
	issue.CreatedAt = issueCreatedAt.UTC().Format(time.RFC3339)
	issue.UpdatedAt = issueUpdatedAt.UTC().Format(time.RFC3339)
	if runbookUpdatedAt.Valid {
		project.RunbookUpdatedAt = runbookUpdatedAt.Time.UTC().Format(time.RFC3339)
	}
	if latestIssueUpdatedAt.Valid {
		project.LatestIssueUpdatedAt = latestIssueUpdatedAt.Time.UTC().Format(time.RFC3339)
	}
	if projectCreatedAt.Valid {
		project.CreatedAt = projectCreatedAt.Time.UTC().Format(time.RFC3339)
	}
	if projectUpdatedAt.Valid {
		project.UpdatedAt = projectUpdatedAt.Time.UTC().Format(time.RFC3339)
	}
	return issue, project, nil
}

func issueListQuery(whereClause, orderClause string) string {
	return `
		SELECT
			i.id::text,
			i.workspace_id::text,
				COALESCE(i.project_id::text, ''),
				COALESCE(p.name, ''),
			COALESCE(i.parent_issue_id::text, ''),
			i.sort_order,
			i.title,
			i.body,
			i.status,
			i.close_reason,
			i.triage_status,
			i.assignee,
			i.assignee_type,
			COUNT(DISTINCT t.id),
			COUNT(DISTINCT child.id),
			COUNT(DISTINCT child.id) FILTER (WHERE child.status = 'closed'),
			i.updated_at,
			i.created_at
			FROM issues i
			LEFT JOIN projects p ON p.id = i.project_id
			LEFT JOIN runtime_tasks t ON t.issue_id = i.id::text
			LEFT JOIN issues child ON child.parent_issue_id = i.id
		` + whereClause + `
			GROUP BY i.id, p.name
	` + orderClause
}

func loadIssue(ctx context.Context, q queryer, workspaceID, issueID string) (Issue, error) {
	row := q.QueryRow(ctx, `
			SELECT id::text, workspace_id::text, COALESCE(project_id::text, ''), COALESCE(parent_issue_id::text, ''), sort_order, title, body, status, close_reason, triage_status, assignee, assignee_type, creator_name, creator_avatar_url, environment_url, created_at, updated_at
			FROM issues
			WHERE workspace_id = $1 AND id = $2
	`, workspaceID, issueID)
	issue, err := scanIssue(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Issue{}, ErrNotFound
	}
	return issue, err
}

func resolveIssueProject(ctx context.Context, q queryer, workspaceID, projectID, text string) (Project, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID != "" {
		row := q.QueryRow(ctx, `
			SELECT id::text, workspace_id::text, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, kubeconfig_path, namespace, image_registry_prefix, preview_domain, ingress_class, node_host, default_cluster_id, 'empty', NULL::timestamptz, '', '', 0, 0, NULL::timestamptz, created_at, updated_at
			FROM projects
			WHERE workspace_id = $1 AND id = $2
		`, workspaceID, projectID)
		project, err := scanProject(row)
		if errors.Is(err, pgx.ErrNoRows) {
			return Project{}, ErrNotFound
		}
		return project, err
	}
	rows, err := q.Query(ctx, `
		SELECT id::text, workspace_id::text, name, repo_path, source_type, remote_url, git_provider, git_owner, git_repo, default_branch, deploy_command, validation_command, kube_context, kubeconfig_path, namespace, image_registry_prefix, preview_domain, ingress_class, node_host, default_cluster_id, 'empty', NULL::timestamptz, '', '', 0, 0, NULL::timestamptz, created_at, updated_at
		FROM projects
		WHERE workspace_id = $1
		ORDER BY updated_at DESC
	`, workspaceID)
	if err != nil {
		return Project{}, err
	}
	defer rows.Close()
	var best Project
	bestScore := -1
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return Project{}, err
		}
		score := issueProjectScore(project, text)
		if best.ID == "" || score > bestScore {
			best = project
			bestScore = score
		}
	}
	if err := rows.Err(); err != nil {
		return Project{}, err
	}
	if best.ID == "" {
		return Project{}, errors.New("create a project before creating issues")
	}
	return best, nil
}

func resolveOptionalIssueProjectID(ctx context.Context, q queryer, workspaceID, projectID, text string) (string, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID != "" {
		project, err := resolveIssueProject(ctx, q, workspaceID, projectID, text)
		if err != nil {
			return "", err
		}
		return project.ID, nil
	}
	var count int
	if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM projects WHERE workspace_id = $1`, workspaceID).Scan(&count); err != nil {
		return "", err
	}
	if count == 0 {
		return "", nil
	}
	if count > 1 {
		return "", nil
	}
	project, err := resolveIssueProject(ctx, q, workspaceID, "", text)
	if err != nil {
		if strings.Contains(err.Error(), "create a project before creating issues") {
			return "", nil
		}
		return "", err
	}
	return project.ID, nil
}

func nullableText(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func replaceIssueLabels(ctx context.Context, q queryer, workspaceID, issueID string, labels []IssueLabel) error {
	if _, err := q.Exec(ctx, `DELETE FROM issue_labels WHERE workspace_id = $1 AND issue_id = $2`, workspaceID, issueID); err != nil {
		return err
	}
	for _, label := range labels {
		if _, err := q.Exec(ctx, `
			INSERT INTO issue_labels (workspace_id, issue_id, label_id, name, color)
			SELECT $1, $2, id, name, color
			FROM issue_label_definitions
			WHERE key = $3
		`, workspaceID, issueID, label.Key); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) listIssueLabels(ctx context.Context, workspaceID, issueID string) ([]IssueLabel, error) {
	return listIssueLabels(ctx, s.pool, workspaceID, issueID)
}

func listIssueLabels(ctx context.Context, q queryer, workspaceID, issueID string) ([]IssueLabel, error) {
	rows, err := q.Query(ctx, `
		SELECT il.id::text, il.issue_id::text, COALESCE(il.label_id::text, ''), COALESCE(ld.key, ''), il.name, COALESCE(ld.dimension, ''), il.color, COALESCE(ld.sort_order, 0), il.created_at
		FROM issue_labels il
		LEFT JOIN issue_label_definitions ld ON ld.id = il.label_id
		WHERE il.workspace_id = $1 AND il.issue_id = $2
		ORDER BY CASE COALESCE(ld.dimension, '') WHEN 'type' THEN 0 WHEN 'priority' THEN 1 ELSE 2 END, COALESCE(ld.sort_order, 0), il.name
	`, workspaceID, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	labels := []IssueLabel{}
	for rows.Next() {
		label, err := scanIssueLabel(rows)
		if err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}

func (s *PostgresStore) listChildIssues(ctx context.Context, workspaceID, issueID string) ([]IssueListItem, error) {
	return listChildIssuesForQueryer(ctx, s.pool, workspaceID, issueID)
}

func listChildIssuesForQueryer(ctx context.Context, q queryer, workspaceID, issueID string) ([]IssueListItem, error) {
	rows, err := q.Query(ctx, issueListQuery(`
		WHERE i.workspace_id = $1 AND i.parent_issue_id = $2
	`, `
		ORDER BY i.sort_order ASC, i.created_at ASC
	`), workspaceID, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []IssueListItem{}
	for rows.Next() {
		item, err := scanIssueListItem(rows)
		if err != nil {
			return nil, err
		}
		labels, err := listIssueLabels(ctx, q, workspaceID, item.ID)
		if err != nil {
			return nil, err
		}
		item.Labels = labels
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) listIssueComments(ctx context.Context, workspaceID, issueID, viewerUserID string) ([]Comment, error) {
	return listIssueCommentsForQueryer(ctx, s.pool, workspaceID, issueID, viewerUserID)
}

func listIssueCommentsForQueryer(ctx context.Context, q queryer, workspaceID, issueID string, viewerUserID ...string) ([]Comment, error) {
	viewerID := ""
	if len(viewerUserID) > 0 {
		viewerID = strings.TrimSpace(viewerUserID[0])
	}
	rows, err := q.Query(ctx, `
		SELECT id::text, issue_id::text, author_type, COALESCE(author_user_id::text, ''), author_name, author_avatar_url, body, created_at, updated_at, edited_at
		FROM comments
		WHERE workspace_id = $1 AND issue_id = $2
		ORDER BY created_at DESC, id DESC
	`, workspaceID, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	comments := []Comment{}
	for rows.Next() {
		comment, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		comment.Reactions, err = listCommentReactionSummaries(ctx, q, workspaceID, issueID, comment.ID, viewerID)
		if err != nil {
			return nil, err
		}
		comments = append(comments, comment)
	}
	return comments, rows.Err()
}

func listCommentReactionSummaries(ctx context.Context, q queryer, workspaceID, issueID, commentID, viewerUserID string) ([]CommentReactionSummary, error) {
	rows, err := q.Query(ctx, `
		SELECT reaction, COUNT(*), BOOL_OR(user_id::text = $4)
		FROM comment_reactions
		WHERE workspace_id = $1 AND issue_id = $2 AND comment_id = $3
		GROUP BY reaction
		ORDER BY reaction
	`, workspaceID, issueID, commentID, viewerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	summaries := []CommentReactionSummary{}
	for rows.Next() {
		var summary CommentReactionSummary
		if err := rows.Scan(&summary.Reaction, &summary.Count, &summary.ReactedByMe); err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, rows.Err()
}

func ensureCommentInIssue(ctx context.Context, q queryer, workspaceID, issueID, commentID string) error {
	var exists bool
	err := q.QueryRow(ctx, `
		SELECT true
		FROM comments
		WHERE workspace_id = $1 AND issue_id = $2 AND id = $3
	`, workspaceID, issueID, commentID).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
