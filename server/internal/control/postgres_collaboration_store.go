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
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
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
		Evidence:        []any{},
		Failures:        []any{},
		ChangeNodes:     []any{},
		ReviewEvidence:  []any{},
		Handoffs:        []any{},
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
	return detail, nil
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
	if err := tx.Commit(dbctx); err != nil {
		return nil, err
	}
	return s.listIssueLabels(dbctx, workspaceID, issueID)
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

func scanRuntimeTaskAgentSession(row scanner) (AgentSession, error) {
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
		return AgentSession{}, err
	}

	var payload struct {
		Prompt          string `json:"prompt"`
		AgentProfile    string `json:"agentProfile"`
		Branch          string `json:"branch"`
		SourceCommitSHA string `json:"sourceCommitSha"`
		ArtifactDir     string `json:"artifactDir"`
	}
	_ = json.Unmarshal(payloadBytes, &payload)

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
	_ = json.Unmarshal(resultBytes, &result)

	sessionStatus, agentStatus := runtimeTaskSessionStatus(status, errorText)
	return AgentSession{
		ID:              firstNonEmpty(sessionID, runtimeTaskID),
		IssueID:         issueID,
		Provider:        "codex",
		AgentProfile:    firstNonEmpty(payload.AgentProfile, "codex"),
		RuntimeMode:     firstNonEmpty(runtimeMode, "team"),
		RuntimeTaskID:   runtimeTaskID,
		Command:         strings.TrimSpace(payload.Prompt),
		Status:          sessionStatus,
		Branch:          firstNonEmpty(result.Source.Branch, payload.Branch),
		Workdir:         result.Workdir,
		CodexThreadID:   result.ThreadID,
		CodexTurnID:     result.TurnID,
		AgentStatus:     agentStatus,
		ArtifactDir:     firstNonEmpty(result.ArtifactDir, payload.ArtifactDir),
		SourceCommitSHA: firstNonEmpty(result.Source.CommitSHA, payload.SourceCommitSHA),
		CleanupStatus:   "retained",
		CreatedAt:       createdAt.UTC().Format(time.RFC3339),
		UpdatedAt:       updatedAt.UTC().Format(time.RFC3339),
	}, nil
}

func runtimeTaskSessionStatus(status, errorText string) (string, string) {
	switch strings.TrimSpace(status) {
	case "queued":
		return "queued", "team-runtime-queued"
	case "claimed":
		return "running", "team-runtime-claimed"
	case "running":
		return "running", "team-runtime-running"
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
		return "queued", "team-runtime"
	}
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
	rows, err := s.pool.Query(ctx, `
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
	rows, err := s.pool.Query(ctx, issueListQuery(`
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
		labels, err := s.listIssueLabels(ctx, workspaceID, item.ID)
		if err != nil {
			return nil, err
		}
		item.Labels = labels
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) listIssueComments(ctx context.Context, workspaceID, issueID, viewerUserID string) ([]Comment, error) {
	rows, err := s.pool.Query(ctx, `
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
		comment.Reactions, err = listCommentReactionSummaries(ctx, s.pool, workspaceID, issueID, comment.ID, viewerUserID)
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
