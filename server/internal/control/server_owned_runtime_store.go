package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const defaultImportedClusterImageRegistryPrefix = "crpi-7jr40k6elhldekqp.cn-hangzhou.personal.cr.aliyuncs.com/mlhiter"

const (
	testDeployAutomation                = "test_deploy"
	autoDeployTestEnvironmentAutomation = "auto_test_deploy"
)

func (s *PostgresStore) GetWorkspaceSettings(ctx Context, userID, workspaceID string) (WorkspaceSettings, error) {
	dbctx := asContext(ctx)
	if err := ensureWorkspaceMember(dbctx, s.pool, strings.TrimSpace(workspaceID), strings.TrimSpace(userID)); err != nil {
		return WorkspaceSettings{}, err
	}
	return ensureWorkspaceSettings(dbctx, s.pool, workspaceID)
}

func (s *PostgresStore) UpdateWorkspaceSettings(ctx Context, userID, workspaceID string, input WorkspaceSettingsInput) (WorkspaceSettings, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, strings.TrimSpace(userID), "owner", "admin"); err != nil {
		return WorkspaceSettings{}, err
	}
	row := s.pool.QueryRow(dbctx, `
		INSERT INTO workspace_settings (workspace_id, auto_create_draft_pr, auto_deploy_test_environment)
		VALUES ($1, $2, $3)
		ON CONFLICT (workspace_id) DO UPDATE
		SET auto_create_draft_pr = EXCLUDED.auto_create_draft_pr,
			auto_deploy_test_environment = EXCLUDED.auto_deploy_test_environment,
			updated_at = now()
		RETURNING auto_create_draft_pr, auto_deploy_test_environment, created_at, updated_at
	`, workspaceID, input.AutoCreateDraftPR, input.AutoDeployTestEnvironment)
	return scanWorkspaceSettings(row)
}

func (s *PostgresStore) GetWorkspaceGitHubAppInstallation(ctx Context, userID, workspaceID string) (WorkspaceGitHubAppInstallation, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, strings.TrimSpace(userID)); err != nil {
		return WorkspaceGitHubAppInstallation{}, err
	}
	row := s.pool.QueryRow(dbctx, `
		SELECT
			status,
			installation_id,
			account_login,
			account_type,
			repository_selection,
			permissions_json::text,
			html_url,
			repositories_url,
			error,
			last_synced_at,
			created_at,
			updated_at
		FROM workspace_github_app_installations
		WHERE workspace_id = $1
	`, workspaceID)
	installation, err := scanWorkspaceGitHubAppInstallation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return defaultWorkspaceGitHubAppInstallation(), nil
	}
	return installation, err
}

func (s *PostgresStore) UpsertWorkspaceGitHubAppInstallation(ctx Context, workspaceID string, input WorkspaceGitHubAppInstallation) (WorkspaceGitHubAppInstallation, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	installation := normalizeStoredWorkspaceGitHubAppInstallation(input)
	permissionsJSON, err := json.Marshal(installation.Permissions)
	if err != nil {
		return WorkspaceGitHubAppInstallation{}, err
	}
	row := s.pool.QueryRow(dbctx, `
		INSERT INTO workspace_github_app_installations (
			workspace_id,
			status,
			installation_id,
			account_login,
			account_type,
			repository_selection,
			permissions_json,
			html_url,
			repositories_url,
			error,
			last_synced_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, NULLIF($11, '')::timestamptz)
		ON CONFLICT (workspace_id) DO UPDATE
		SET status = EXCLUDED.status,
			installation_id = EXCLUDED.installation_id,
			account_login = EXCLUDED.account_login,
			account_type = EXCLUDED.account_type,
			repository_selection = EXCLUDED.repository_selection,
			permissions_json = EXCLUDED.permissions_json,
			html_url = EXCLUDED.html_url,
			repositories_url = EXCLUDED.repositories_url,
			error = EXCLUDED.error,
			last_synced_at = EXCLUDED.last_synced_at,
			updated_at = now()
		RETURNING
			status,
			installation_id,
			account_login,
			account_type,
			repository_selection,
			permissions_json::text,
			html_url,
			repositories_url,
			error,
			last_synced_at,
			created_at,
			updated_at
	`, workspaceID, installation.Status, installation.InstallationID, installation.AccountLogin, installation.AccountType, installation.RepositorySelection, string(permissionsJSON), installation.HTMLURL, installation.RepositoriesURL, installation.Error, installation.LastSyncedAt)
	return scanWorkspaceGitHubAppInstallation(row)
}

func (s *PostgresStore) ListSkills(ctx Context, userID, workspaceID string) ([]SkillCatalogItem, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, strings.TrimSpace(userID)); err != nil {
		return nil, err
	}
	return s.listSkills(dbctx, workspaceID)
}

func ensureWorkspaceSettings(ctx context.Context, q queryer, workspaceID string) (WorkspaceSettings, error) {
	row := q.QueryRow(ctx, `
		INSERT INTO workspace_settings (workspace_id)
		VALUES ($1)
		ON CONFLICT (workspace_id) DO UPDATE
		SET workspace_id = EXCLUDED.workspace_id
		RETURNING auto_create_draft_pr, auto_deploy_test_environment, created_at, updated_at
	`, strings.TrimSpace(workspaceID))
	return scanWorkspaceSettings(row)
}

func scanWorkspaceSettings(row scanner) (WorkspaceSettings, error) {
	var settings WorkspaceSettings
	var createdAt, updatedAt time.Time
	if err := row.Scan(&settings.AutoCreateDraftPR, &settings.AutoDeployTestEnvironment, &createdAt, &updatedAt); err != nil {
		return WorkspaceSettings{}, err
	}
	settings.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	settings.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return settings, nil
}

func scanWorkspaceGitHubAppInstallation(row scanner) (WorkspaceGitHubAppInstallation, error) {
	var installation WorkspaceGitHubAppInstallation
	var permissionsJSON string
	var lastSyncedAt sql.NullTime
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&installation.Status,
		&installation.InstallationID,
		&installation.AccountLogin,
		&installation.AccountType,
		&installation.RepositorySelection,
		&permissionsJSON,
		&installation.HTMLURL,
		&installation.RepositoriesURL,
		&installation.Error,
		&lastSyncedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return WorkspaceGitHubAppInstallation{}, err
	}
	if strings.TrimSpace(permissionsJSON) != "" {
		_ = json.Unmarshal([]byte(permissionsJSON), &installation.Permissions)
	}
	if lastSyncedAt.Valid {
		installation.LastSyncedAt = lastSyncedAt.Time.UTC().Format(time.RFC3339)
	}
	installation.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	installation.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return normalizeStoredWorkspaceGitHubAppInstallation(installation), nil
}

func (s *PostgresStore) ListEnvironments(ctx Context, userID, workspaceID string) ([]Environment, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, strings.TrimSpace(userID)); err != nil {
		return nil, err
	}
	clusters, err := s.ListClusters(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	environments := make([]Environment, 0, len(clusters))
	for _, cluster := range clusters {
		environments = append(environments, environmentFromCluster(cluster))
	}
	rows, err := s.pool.Query(dbctx, `
		SELECT
			e.id::text,
			e.workspace_id::text,
			e.name,
			e.kind,
			e.status,
			e.ssh_host,
			e.ssh_port,
			e.ssh_user,
			e.ssh_auth_ref,
			(e.ssh_auth_method <> ''),
			e.workdir,
			e.service_hint,
			e.labels,
			e.last_checked_at,
			e.created_at,
			e.updated_at,
			COUNT(DISTINCT p.id) FILTER (WHERE p.default_cluster_id = e.id::text),
			COUNT(DISTINCT ite.issue_id) FILTER (WHERE ite.environment_id = e.id::text),
			COUNT(DISTINCT tp.id) FILTER (WHERE tp.environment_id = e.id::text),
			COUNT(DISTINCT tr.id) FILTER (WHERE tr.environment_id = e.id::text)
		FROM environments e
		LEFT JOIN projects p ON p.workspace_id = e.workspace_id AND p.default_cluster_id = e.id::text
		LEFT JOIN issue_test_environments ite ON ite.workspace_id = e.workspace_id AND ite.environment_id = e.id::text
		LEFT JOIN test_plans tp ON tp.workspace_id = e.workspace_id AND tp.environment_id = e.id::text
		LEFT JOIN test_runs tr ON tr.workspace_id = e.workspace_id AND tr.environment_id = e.id::text
		WHERE e.workspace_id = $1
		GROUP BY e.id
		ORDER BY e.updated_at DESC, e.created_at DESC
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		environment, err := scanVirtualMachineEnvironment(rows)
		if err != nil {
			return nil, err
		}
		environments = append(environments, environment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachEnvironmentUsageCounts(dbctx, workspaceID, environments); err != nil {
		return nil, err
	}
	sort.Slice(environments, func(i, j int) bool {
		if environments[i].UpdatedAt == environments[j].UpdatedAt {
			return environments[i].CreatedAt > environments[j].CreatedAt
		}
		return environments[i].UpdatedAt > environments[j].UpdatedAt
	})
	return environments, nil
}

func (s *PostgresStore) attachEnvironmentUsageCounts(ctx context.Context, workspaceID string, environments []Environment) error {
	byID := map[string]*Environment{}
	for index := range environments {
		byID[environments[index].ID] = &environments[index]
	}
	rows, err := s.pool.Query(ctx, `
		SELECT environment_id, SUM(projects)::int, SUM(issue_environments)::int, SUM(test_plans)::int, SUM(test_runs)::int
		FROM (
			SELECT default_cluster_id AS environment_id, COUNT(*) AS projects, 0 AS issue_environments, 0 AS test_plans, 0 AS test_runs
			FROM projects
			WHERE workspace_id = $1 AND default_cluster_id <> ''
			GROUP BY default_cluster_id
			UNION ALL
			SELECT COALESCE(NULLIF(environment_id, ''), cluster_id::text) AS environment_id, 0, COUNT(*), 0, 0
			FROM issue_test_environments
			WHERE workspace_id = $1 AND (environment_id <> '' OR cluster_id IS NOT NULL)
			GROUP BY COALESCE(NULLIF(environment_id, ''), cluster_id::text)
			UNION ALL
			SELECT environment_id, 0, 0, COUNT(*), 0
			FROM test_plans
			WHERE workspace_id = $1 AND environment_id <> ''
			GROUP BY environment_id
			UNION ALL
			SELECT environment_id, 0, 0, 0, COUNT(*)
			FROM test_runs
			WHERE workspace_id = $1 AND environment_id <> ''
			GROUP BY environment_id
		) refs
		WHERE environment_id <> ''
		GROUP BY environment_id
	`, workspaceID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var environmentID string
		var projectCount, issueEnvironmentCount, testPlanCount, testRunCount int
		if err := rows.Scan(&environmentID, &projectCount, &issueEnvironmentCount, &testPlanCount, &testRunCount); err != nil {
			return err
		}
		if environment := byID[environmentID]; environment != nil {
			environment.ProjectCount = projectCount
			environment.IssueEnvironmentCount = issueEnvironmentCount
			environment.TestPlanCount = testPlanCount
			environment.TestRunCount = testRunCount
		}
	}
	return rows.Err()
}

func (s *PostgresStore) CreateEnvironment(ctx Context, userID, workspaceID string, input EnvironmentInput) (Environment, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	normalized, err := normalizeEnvironmentInput(Environment{}, input)
	if err != nil {
		return Environment{}, err
	}
	if normalized.Kind == "kubernetes" {
		cluster, err := s.CreateCluster(ctx, userID, workspaceID, clusterInputFromEnvironmentInput(input))
		if err != nil {
			return Environment{}, err
		}
		return environmentFromCluster(cluster), nil
	}
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, strings.TrimSpace(userID), "owner", "admin"); err != nil {
		return Environment{}, err
	}
	storedAuth, err := normalizeVirtualMachineStoredSSHAuth(input.SSHAuth)
	if err != nil {
		return Environment{}, err
	}
	applyVirtualMachineStoredSSHAuth(&normalized, storedAuth)
	status, err := virtualMachineSSHStatus(dbctx, normalized, storedAuth)
	if err != nil {
		return Environment{}, err
	}
	normalized.Status = status
	row := s.pool.QueryRow(dbctx, `
		INSERT INTO environments (
			workspace_id, name, kind, status,
			ssh_host, ssh_port, ssh_user, ssh_auth_ref,
			workdir, service_hint, labels,
			ssh_auth_method, ssh_auth_password, ssh_auth_private_key, ssh_auth_passphrase,
			last_checked_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13, $14, $15, now())
		RETURNING id::text, workspace_id::text, name, kind, status, ssh_host, ssh_port, ssh_user, ssh_auth_ref, (ssh_auth_method <> ''), workdir, service_hint, labels, last_checked_at, created_at, updated_at, 0, 0, 0, 0
	`, workspaceID, normalized.Name, normalized.Kind, normalized.Status, normalized.VirtualMachine.SSHHost, normalized.VirtualMachine.SSHPort, normalized.VirtualMachine.SSHUser, normalized.VirtualMachine.SSHAuthRef, normalized.VirtualMachine.Workdir, normalized.VirtualMachine.ServiceHint, jsonOrObject(normalized.VirtualMachine.Labels), storedAuth.Method, storedAuth.Password, storedAuth.PrivateKey, storedAuth.Passphrase)
	return scanVirtualMachineEnvironment(row)
}

func (s *PostgresStore) UpdateEnvironment(ctx Context, userID, workspaceID, environmentID string, input EnvironmentInput) (Environment, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	environmentID = strings.TrimSpace(environmentID)
	existing, err := s.loadEnvironment(dbctx, workspaceID, environmentID)
	if err != nil {
		return Environment{}, err
	}
	if existing.Kind == "kubernetes" {
		cluster, err := s.UpdateCluster(ctx, userID, workspaceID, environmentID, clusterInputFromEnvironmentInput(input))
		if err != nil {
			return Environment{}, err
		}
		return environmentFromCluster(cluster), nil
	}
	normalized, err := normalizeEnvironmentInput(existing, input)
	if err != nil {
		return Environment{}, err
	}
	if normalized.Kind != existing.Kind {
		return Environment{}, errors.New("environment kind cannot be changed")
	}
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, strings.TrimSpace(userID), "owner", "admin"); err != nil {
		return Environment{}, err
	}
	storedAuth, err := s.loadVirtualMachineStoredSSHAuth(dbctx, workspaceID, environmentID)
	if err != nil {
		return Environment{}, err
	}
	if input.SSHAuth != nil {
		storedAuth, err = normalizeVirtualMachineStoredSSHAuth(input.SSHAuth)
		if err != nil {
			return Environment{}, err
		}
	}
	applyVirtualMachineStoredSSHAuth(&normalized, storedAuth)
	status, err := virtualMachineSSHStatus(dbctx, normalized, storedAuth)
	if err != nil {
		return Environment{}, err
	}
	normalized.Status = status
	row := s.pool.QueryRow(dbctx, `
		UPDATE environments
		SET name = $3,
			status = $4,
			ssh_host = $5,
			ssh_port = $6,
			ssh_user = $7,
			ssh_auth_ref = $8,
			workdir = $9,
			service_hint = $10,
			labels = $11::jsonb,
			ssh_auth_method = $12,
			ssh_auth_password = $13,
			ssh_auth_private_key = $14,
			ssh_auth_passphrase = $15,
			last_checked_at = now(),
			updated_at = now()
		WHERE workspace_id = $1 AND id = $2
		RETURNING id::text, workspace_id::text, name, kind, status, ssh_host, ssh_port, ssh_user, ssh_auth_ref, (ssh_auth_method <> ''), workdir, service_hint, labels, last_checked_at, created_at, updated_at, 0, 0, 0, 0
	`, workspaceID, environmentID, normalized.Name, normalized.Status, normalized.VirtualMachine.SSHHost, normalized.VirtualMachine.SSHPort, normalized.VirtualMachine.SSHUser, normalized.VirtualMachine.SSHAuthRef, normalized.VirtualMachine.Workdir, normalized.VirtualMachine.ServiceHint, jsonOrObject(normalized.VirtualMachine.Labels), storedAuth.Method, storedAuth.Password, storedAuth.PrivateKey, storedAuth.Passphrase)
	environment, err := scanVirtualMachineEnvironment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, ErrNotFound
	}
	return environment, err
}

func (s *PostgresStore) CheckEnvironment(ctx Context, userID, workspaceID, environmentID string, input EnvironmentCheckInput) (Environment, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	environmentID = strings.TrimSpace(environmentID)
	existing, err := s.loadEnvironment(dbctx, workspaceID, environmentID)
	if err != nil {
		return Environment{}, err
	}
	if existing.Kind == environmentKindKubernetes {
		cluster, err := s.CheckCluster(ctx, userID, workspaceID, environmentID)
		if err != nil {
			return Environment{}, err
		}
		return environmentFromCluster(cluster), nil
	}
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, strings.TrimSpace(userID), "owner", "admin"); err != nil {
		return Environment{}, err
	}
	storedAuth, err := s.loadVirtualMachineStoredSSHAuth(dbctx, workspaceID, environmentID)
	if err != nil {
		return Environment{}, err
	}
	if input.SSHAuth != nil {
		storedAuth, err = normalizeVirtualMachineStoredSSHAuth(input.SSHAuth)
		if err != nil {
			return Environment{}, err
		}
	}
	applyVirtualMachineStoredSSHAuth(&existing, storedAuth)
	status, err := virtualMachineSSHStatus(dbctx, existing, storedAuth)
	if err != nil {
		return Environment{}, err
	}
	row := s.pool.QueryRow(dbctx, `
		UPDATE environments
		SET status = $3,
			ssh_auth_ref = $4,
			ssh_auth_method = $5,
			ssh_auth_password = $6,
			ssh_auth_private_key = $7,
			ssh_auth_passphrase = $8,
			last_checked_at = now(),
			updated_at = now()
		WHERE workspace_id = $1 AND id = $2
		RETURNING id::text, workspace_id::text, name, kind, status, ssh_host, ssh_port, ssh_user, ssh_auth_ref, (ssh_auth_method <> ''), workdir, service_hint, labels, last_checked_at, created_at, updated_at, 0, 0, 0, 0
	`, workspaceID, environmentID, status, existing.VirtualMachine.SSHAuthRef, storedAuth.Method, storedAuth.Password, storedAuth.PrivateKey, storedAuth.Passphrase)
	environment, err := scanVirtualMachineEnvironment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, ErrNotFound
	}
	return environment, err
}

func (s *PostgresStore) DeleteEnvironment(ctx Context, userID, workspaceID, environmentID string) error {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	environmentID = strings.TrimSpace(environmentID)
	environment, err := s.loadEnvironment(dbctx, workspaceID, environmentID)
	if err != nil {
		return err
	}
	if environment.Kind == "kubernetes" {
		return s.DeleteCluster(ctx, userID, workspaceID, environmentID)
	}
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, strings.TrimSpace(userID), "owner", "admin"); err != nil {
		return err
	}
	var references int
	if err := s.pool.QueryRow(dbctx, `
		SELECT COUNT(*)
		FROM (
			SELECT environment_id FROM issue_test_environments WHERE workspace_id = $1 AND environment_id = $2
			UNION ALL
			SELECT environment_id FROM test_plans WHERE workspace_id = $1 AND environment_id = $2
			UNION ALL
			SELECT environment_id FROM test_runs WHERE workspace_id = $1 AND environment_id = $2
		) refs
	`, workspaceID, environmentID).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return ErrForbidden
	}
	tag, err := s.pool.Exec(dbctx, `DELETE FROM environments WHERE workspace_id = $1 AND id = $2`, workspaceID, environmentID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) loadEnvironment(ctx context.Context, workspaceID, environmentID string) (Environment, error) {
	if cluster, err := loadCluster(ctx, s.pool, workspaceID, environmentID); err == nil {
		return environmentFromCluster(cluster), nil
	} else if !errors.Is(err, ErrNotFound) {
		return Environment{}, err
	}
	row := s.pool.QueryRow(ctx, `
		SELECT id::text, workspace_id::text, name, kind, status, ssh_host, ssh_port, ssh_user, ssh_auth_ref, (ssh_auth_method <> ''), workdir, service_hint, labels, last_checked_at, created_at, updated_at, 0, 0, 0, 0
		FROM environments
		WHERE workspace_id = $1 AND id = $2
	`, strings.TrimSpace(workspaceID), strings.TrimSpace(environmentID))
	environment, err := scanVirtualMachineEnvironment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, ErrNotFound
	}
	return environment, err
}

func (s *PostgresStore) loadVirtualMachineStoredSSHAuth(ctx context.Context, workspaceID, environmentID string) (virtualMachineStoredSSHAuth, error) {
	var auth virtualMachineStoredSSHAuth
	row := s.pool.QueryRow(ctx, `
		SELECT ssh_auth_method, ssh_auth_password, ssh_auth_private_key, ssh_auth_passphrase
		FROM environments
		WHERE workspace_id = $1 AND id = $2
	`, strings.TrimSpace(workspaceID), strings.TrimSpace(environmentID))
	if err := row.Scan(&auth.Method, &auth.Password, &auth.PrivateKey, &auth.Passphrase); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return virtualMachineStoredSSHAuth{}, ErrNotFound
		}
		return virtualMachineStoredSSHAuth{}, err
	}
	return auth, nil
}

func (s *PostgresStore) ListAgentProfiles(ctx Context, userID, workspaceID string) ([]AgentProfile, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, strings.TrimSpace(userID)); err != nil {
		return nil, err
	}
	if err := seedDefaultAgentProfiles(dbctx, s.pool, workspaceID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(dbctx, `
		SELECT id, name, mention, provider, description, instructions, enabled, built_in, sort_order, created_at, updated_at
		FROM agent_profiles
		WHERE workspace_id = $1
		ORDER BY sort_order ASC, created_at ASC, name ASC
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	profiles := []AgentProfile{}
	for rows.Next() {
		profile, err := scanAgentProfile(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

func (s *PostgresStore) CreateAgentProfile(ctx Context, userID, workspaceID string, input AgentProfileInput) (AgentProfile, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, strings.TrimSpace(userID), "owner", "admin"); err != nil {
		return AgentProfile{}, err
	}
	profile, err := normalizeAgentProfileInput(AgentProfile{}, input, false)
	if err != nil {
		return AgentProfile{}, err
	}
	row := s.pool.QueryRow(dbctx, `
		INSERT INTO agent_profiles (workspace_id, id, name, mention, provider, description, instructions, enabled, built_in, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, false, $9)
		RETURNING id, name, mention, provider, description, instructions, enabled, built_in, sort_order, created_at, updated_at
	`, workspaceID, profile.ID, profile.Name, profile.Mention, profile.Provider, profile.Description, profile.Instructions, profile.Enabled, profile.SortOrder)
	return scanAgentProfile(row)
}

func (s *PostgresStore) UpdateAgentProfile(ctx Context, userID, workspaceID, agentID string, input AgentProfileInput) (AgentProfile, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	agentID = agentProfileLookupKey(agentID)
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, strings.TrimSpace(userID), "owner", "admin"); err != nil {
		return AgentProfile{}, err
	}
	existing, err := loadAgentProfile(dbctx, s.pool, workspaceID, agentID)
	if err != nil {
		return AgentProfile{}, err
	}
	updated, err := normalizeAgentProfileInput(existing, input, true)
	if err != nil {
		return AgentProfile{}, err
	}
	row := s.pool.QueryRow(dbctx, `
		UPDATE agent_profiles
		SET name = $3,
			mention = $4,
			provider = $5,
			description = $6,
			instructions = $7,
			enabled = $8,
			sort_order = $9,
			updated_at = now()
		WHERE workspace_id = $1 AND id = $2
		RETURNING id, name, mention, provider, description, instructions, enabled, built_in, sort_order, created_at, updated_at
	`, workspaceID, existing.ID, updated.Name, updated.Mention, updated.Provider, updated.Description, updated.Instructions, updated.Enabled, updated.SortOrder)
	profile, err := scanAgentProfile(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentProfile{}, ErrNotFound
	}
	return profile, err
}

func seedDefaultAgentProfiles(ctx context.Context, q queryer, workspaceID string) error {
	now := time.Now().UTC()
	for _, profile := range defaultAgentProfiles(now) {
		if _, err := q.Exec(ctx, `
			INSERT INTO agent_profiles (workspace_id, id, name, mention, provider, description, instructions, enabled, built_in, sort_order, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, true, $9, $10, $10)
			ON CONFLICT (workspace_id, id) DO NOTHING
		`, workspaceID, profile.ID, profile.Name, profile.Mention, profile.Provider, profile.Description, profile.Instructions, profile.Enabled, profile.SortOrder, now); err != nil {
			return err
		}
	}
	return nil
}

func defaultAgentProfiles(now time.Time) []AgentProfile {
	timestamp := now.UTC().Format(time.RFC3339)
	return []AgentProfile{
		{
			ID:          "triage",
			Name:        "Triage",
			Mention:     "@triage",
			Provider:    "codex",
			Description: "Automatic issue type classification for new issues.",
			Instructions: strings.TrimSpace(`
Use the triage profile only for classification. Read the issue title, body, and project context, then choose exactly one Conventional Commit type: feat, fix, docs, style, refactor, perf, test, build, ci, chore, or revert. Do not assign priority, change status, or perform implementation work.
`),
			Enabled:   false,
			BuiltIn:   true,
			SortOrder: 5,
			CreatedAt: timestamp,
			UpdatedAt: timestamp,
		},
		{
			ID:          "codex",
			Name:        "Codex",
			Mention:     "@codex",
			Provider:    "codex",
			Description: "General implementation, explanation, and follow-up work.",
			Instructions: strings.TrimSpace(`
Use the general Codex profile. Answer questions directly, make focused code changes when requested, and choose the smallest practical path through implementation and validation.
`),
			Enabled:   true,
			BuiltIn:   true,
			SortOrder: 10,
			CreatedAt: timestamp,
			UpdatedAt: timestamp,
		},
		{
			ID:          "bugfix",
			Name:        "Bugfix",
			Mention:     "@bugfix",
			Provider:    "codex",
			Description: "Debugging, reproduction, root cause analysis, and narrow fixes.",
			Instructions: strings.TrimSpace(`
Use the bugfix profile. Start by identifying the failing behavior and the most likely root cause. Reproduce or inspect the real path before editing when practical. Keep the fix narrow, add or update regression coverage when the codebase supports it, and report the exact validation result.
`),
			Enabled:   true,
			BuiltIn:   true,
			SortOrder: 20,
			CreatedAt: timestamp,
			UpdatedAt: timestamp,
		},
		{
			ID:          "design",
			Name:        "Design",
			Mention:     "@design",
			Provider:    "codex",
			Description: "UI, UX, interaction polish, and product surface decisions.",
			Instructions: strings.TrimSpace(`
Use the design profile. Improve product UI with the existing design system and the Issue/document-first mspace direction. Keep the interface quiet, text-led, keyboard-accessible, and avoid dashboard/card-heavy patterns unless they are already the local convention for that surface.
`),
			Enabled:   true,
			BuiltIn:   true,
			SortOrder: 30,
			CreatedAt: timestamp,
			UpdatedAt: timestamp,
		},
	}
}

func loadAgentProfile(ctx context.Context, q queryer, workspaceID, value string) (AgentProfile, error) {
	key := agentProfileLookupKey(value)
	if key == "" {
		key = "codex"
	}
	mention := "@" + key
	row := q.QueryRow(ctx, `
		SELECT id, name, mention, provider, description, instructions, enabled, built_in, sort_order, created_at, updated_at
		FROM agent_profiles
		WHERE workspace_id = $1 AND (lower(id) = $2 OR lower(mention) = $3)
	`, workspaceID, key, mention)
	profile, err := scanAgentProfile(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentProfile{}, ErrNotFound
	}
	return profile, err
}

func scanAgentProfile(row scanner) (AgentProfile, error) {
	var profile AgentProfile
	var createdAt, updatedAt time.Time
	if err := row.Scan(&profile.ID, &profile.Name, &profile.Mention, &profile.Provider, &profile.Description, &profile.Instructions, &profile.Enabled, &profile.BuiltIn, &profile.SortOrder, &createdAt, &updatedAt); err != nil {
		return AgentProfile{}, err
	}
	profile.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	profile.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return profile, nil
}

func normalizeAgentProfileInput(existing AgentProfile, input AgentProfileInput, isUpdate bool) (AgentProfile, error) {
	profile := existing
	profile.Name = strings.Join(strings.Fields(strings.TrimSpace(input.Name)), " ")
	if profile.Name == "" {
		return AgentProfile{}, errors.New("agent name is required")
	}
	mentionInput := input.Mention
	if strings.TrimSpace(mentionInput) == "" {
		mentionInput = profile.Name
	}
	mention, err := normalizeAgentMention(mentionInput)
	if err != nil {
		return AgentProfile{}, err
	}
	profile.Mention = mention
	if !isUpdate {
		profile.ID = strings.TrimPrefix(mention, "@")
		profile.BuiltIn = false
		profile.SortOrder = 100
	}
	provider := strings.ToLower(strings.TrimSpace(input.Provider))
	if provider == "" && isUpdate {
		provider = existing.Provider
	}
	if provider == "" {
		provider = "codex"
	}
	if provider != "codex" {
		return AgentProfile{}, errors.New("only the codex provider is supported right now")
	}
	profile.Provider = provider
	profile.Description = strings.TrimSpace(input.Description)
	profile.Instructions = strings.TrimSpace(input.Instructions)
	if profile.Instructions == "" {
		return AgentProfile{}, errors.New("agent instructions are required")
	}
	if input.Enabled == nil {
		profile.Enabled = !isUpdate || existing.Enabled
	} else {
		profile.Enabled = *input.Enabled
	}
	return profile, nil
}

func normalizeAgentMention(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "@")))
	value = strings.ReplaceAll(value, " ", "-")
	if value == "" {
		return "", errors.New("agent mention is required")
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return "", fmt.Errorf("agent mention %q can only use letters, numbers, hyphen, or underscore", value)
	}
	return "@" + value, nil
}

func agentProfileLookupKey(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "@")))
}

func (s *PostgresStore) ListClusters(ctx Context, userID, workspaceID string) ([]Cluster, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, strings.TrimSpace(userID)); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(dbctx, clusterSelectQuery(`
		WHERE c.workspace_id = $1
		GROUP BY c.id
		ORDER BY c.updated_at DESC, c.created_at DESC
	`), workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	clusters := []Cluster{}
	for rows.Next() {
		cluster, err := scanCluster(rows)
		if err != nil {
			return nil, err
		}
		clusters = append(clusters, cluster)
	}
	return clusters, rows.Err()
}

func (s *PostgresStore) CreateCluster(ctx Context, userID, workspaceID string, input ClusterInput) (Cluster, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	normalized, err := normalizeClusterInput(Cluster{}, input)
	if err != nil {
		return Cluster{}, err
	}
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, strings.TrimSpace(userID), "owner", "admin"); err != nil {
		return Cluster{}, err
	}
	normalized.Status = kubeconfigStatus(dbctx, normalized.KubeconfigPath, normalized.KubeContext)
	row := s.pool.QueryRow(dbctx, `
		WITH inserted AS (
			INSERT INTO clusters (workspace_id, name, kubeconfig_path, kube_context, image_registry_prefix, exposure_mode, node_host, preview_domain, ingress_class, status, last_checked_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
			RETURNING *
		)
	`+clusterSelectProjection("inserted"), workspaceID, normalized.Name, normalized.KubeconfigPath, normalized.KubeContext, normalized.ImageRegistryPrefix, normalized.ExposureMode, normalized.NodeHost, normalized.PreviewDomain, normalized.IngressClass, normalized.Status)
	return scanCluster(row)
}

func (s *PostgresStore) UpdateCluster(ctx Context, userID, workspaceID, clusterID string, input ClusterInput) (Cluster, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	clusterID = strings.TrimSpace(clusterID)
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, strings.TrimSpace(userID), "owner", "admin"); err != nil {
		return Cluster{}, err
	}
	existing, err := loadCluster(dbctx, s.pool, workspaceID, clusterID)
	if err != nil {
		return Cluster{}, err
	}
	updated, err := normalizeClusterInput(existing, input)
	if err != nil {
		return Cluster{}, err
	}
	updated.Status = kubeconfigStatus(dbctx, updated.KubeconfigPath, updated.KubeContext)
	row := s.pool.QueryRow(dbctx, `
		WITH updated AS (
			UPDATE clusters
			SET name = $3,
				kubeconfig_path = $4,
				kube_context = $5,
				image_registry_prefix = $6,
				exposure_mode = $7,
				node_host = $8,
				preview_domain = $9,
				ingress_class = $10,
				status = $11,
				last_checked_at = now(),
				updated_at = now()
			WHERE workspace_id = $1 AND id = $2
			RETURNING *
		)
	`+clusterSelectProjection("updated"), workspaceID, clusterID, updated.Name, updated.KubeconfigPath, updated.KubeContext, updated.ImageRegistryPrefix, updated.ExposureMode, updated.NodeHost, updated.PreviewDomain, updated.IngressClass, updated.Status)
	cluster, err := scanCluster(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Cluster{}, ErrNotFound
	}
	return cluster, err
}

func (s *PostgresStore) CheckCluster(ctx Context, userID, workspaceID, clusterID string) (Cluster, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	clusterID = strings.TrimSpace(clusterID)
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, strings.TrimSpace(userID), "owner", "admin"); err != nil {
		return Cluster{}, err
	}
	cluster, err := loadCluster(dbctx, s.pool, workspaceID, clusterID)
	if err != nil {
		return Cluster{}, err
	}
	cluster.Status = kubeconfigStatus(dbctx, cluster.KubeconfigPath, cluster.KubeContext)
	return updateClusterRecord(dbctx, s.pool, cluster)
}

func (s *PostgresStore) DeleteCluster(ctx Context, userID, workspaceID, clusterID string) error {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	clusterID = strings.TrimSpace(clusterID)
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, strings.TrimSpace(userID), "owner", "admin"); err != nil {
		return err
	}
	var references int
	if err := s.pool.QueryRow(dbctx, `
		SELECT COUNT(*)
		FROM (
			SELECT default_cluster_id AS cluster_id FROM projects WHERE workspace_id = $1 AND default_cluster_id = $2
			UNION ALL
			SELECT cluster_id::text FROM issue_test_environments WHERE workspace_id = $1 AND cluster_id::text = $2
			UNION ALL
			SELECT environment_id FROM test_plans WHERE workspace_id = $1 AND environment_id = $2
			UNION ALL
			SELECT environment_id FROM test_runs WHERE workspace_id = $1 AND environment_id = $2
		) refs
	`, workspaceID, clusterID).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return ErrForbidden
	}
	tag, err := s.pool.Exec(dbctx, `DELETE FROM clusters WHERE workspace_id = $1 AND id = $2`, workspaceID, clusterID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) DiscoverDefaultKubeconfigs(ctx Context, userID, workspaceID string) (KubeconfigDiscoveryResult, error) {
	dbctx := asContext(ctx)
	if err := ensureWorkspaceRole(dbctx, s.pool, strings.TrimSpace(workspaceID), strings.TrimSpace(userID), "owner", "admin"); err != nil {
		return KubeconfigDiscoveryResult{}, err
	}
	return discoverDefaultKubeconfigs()
}

func (s *PostgresStore) ImportKubeconfigs(ctx Context, userID, workspaceID string, paths []string) (KubeconfigImportResult, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, strings.TrimSpace(userID), "owner", "admin"); err != nil {
		return KubeconfigImportResult{}, err
	}
	result := KubeconfigImportResult{Imported: []Cluster{}, Skipped: []KubeconfigImportSkip{}}
	for _, kubeconfigPath := range uniqueStrings(paths) {
		normalizedPath, err := normalizeKubeconfigPath(kubeconfigPath)
		if err != nil {
			result.Skipped = append(result.Skipped, KubeconfigImportSkip{Path: kubeconfigPath, Reason: err.Error()})
			continue
		}
		contexts, err := kubeconfigContexts(normalizedPath)
		if err != nil {
			result.Skipped = append(result.Skipped, KubeconfigImportSkip{Path: normalizedPath, Reason: err.Error()})
			continue
		}
		if len(contexts) == 0 {
			result.Skipped = append(result.Skipped, KubeconfigImportSkip{Path: normalizedPath, Reason: "no kube contexts found"})
			continue
		}
		for _, kubeContext := range contexts {
			cluster, err := s.importKubeconfigContext(dbctx, workspaceID, normalizedPath, kubeContext)
			if err != nil {
				result.Skipped = append(result.Skipped, KubeconfigImportSkip{Path: normalizedPath, Context: kubeContext, Reason: err.Error()})
				continue
			}
			result.Imported = append(result.Imported, cluster)
		}
	}
	return result, nil
}

func (s *PostgresStore) importKubeconfigContext(ctx context.Context, workspaceID, kubeconfigPath, kubeContext string) (Cluster, error) {
	status := kubeconfigStatus(ctx, kubeconfigPath, kubeContext)
	existing, err := loadClusterByKubeconfig(ctx, s.pool, workspaceID, kubeconfigPath, kubeContext)
	if err == nil {
		if existing.ImageRegistryPrefix == "" {
			existing.ImageRegistryPrefix = defaultImportedClusterImageRegistryPrefix
		}
		if existing.ExposureMode == "" {
			existing.ExposureMode = "nodeport"
		}
		existing.Status = status
		return updateClusterRecord(ctx, s.pool, existing)
	}
	if !errors.Is(err, ErrNotFound) {
		return Cluster{}, err
	}
	row := s.pool.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO clusters (workspace_id, name, kubeconfig_path, kube_context, image_registry_prefix, exposure_mode, status, last_checked_at)
			VALUES ($1, $2, $3, $4, $5, 'nodeport', $6, now())
			RETURNING *
		)
	`+clusterSelectProjection("inserted"), workspaceID, importedClusterName(kubeconfigPath, kubeContext), kubeconfigPath, kubeContext, defaultImportedClusterImageRegistryPrefix, status)
	return scanCluster(row)
}

func clusterSelectQuery(suffix string) string {
	return `
		SELECT
			c.id::text,
			c.workspace_id::text,
			c.name,
			c.kubeconfig_path,
			c.kube_context,
			c.image_registry_prefix,
			c.exposure_mode,
			c.node_host,
			c.preview_domain,
			c.ingress_class,
			c.status,
			c.last_checked_at,
			COUNT(DISTINCT p.id),
			COUNT(DISTINCT e.issue_id),
			c.created_at,
			c.updated_at
		FROM clusters c
		LEFT JOIN projects p ON p.workspace_id = c.workspace_id AND p.default_cluster_id = c.id::text
		LEFT JOIN issue_test_environments e ON e.workspace_id = c.workspace_id AND e.cluster_id = c.id
	` + suffix
}

func clusterSelectProjection(alias string) string {
	return fmt.Sprintf(`
		SELECT
			c.id::text,
			c.workspace_id::text,
			c.name,
			c.kubeconfig_path,
			c.kube_context,
			c.image_registry_prefix,
			c.exposure_mode,
			c.node_host,
			c.preview_domain,
			c.ingress_class,
			c.status,
			c.last_checked_at,
			0,
			0,
			c.created_at,
			c.updated_at
		FROM %s c
	`, alias)
}

func scanCluster(row scanner) (Cluster, error) {
	var cluster Cluster
	var lastCheckedAt sql.NullTime
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&cluster.ID,
		&cluster.WorkspaceID,
		&cluster.Name,
		&cluster.KubeconfigPath,
		&cluster.KubeContext,
		&cluster.ImageRegistryPrefix,
		&cluster.ExposureMode,
		&cluster.NodeHost,
		&cluster.PreviewDomain,
		&cluster.IngressClass,
		&cluster.Status,
		&lastCheckedAt,
		&cluster.ProjectCount,
		&cluster.EnvironmentCount,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Cluster{}, err
	}
	if lastCheckedAt.Valid {
		cluster.LastCheckedAt = lastCheckedAt.Time.UTC().Format(time.RFC3339)
	}
	cluster.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	cluster.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return cluster, nil
}

func loadCluster(ctx context.Context, q queryer, workspaceID, clusterID string) (Cluster, error) {
	row := q.QueryRow(ctx, clusterSelectQuery(`
		WHERE c.workspace_id = $1 AND c.id = $2
		GROUP BY c.id
	`), strings.TrimSpace(workspaceID), strings.TrimSpace(clusterID))
	cluster, err := scanCluster(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Cluster{}, ErrNotFound
	}
	return cluster, err
}

func loadClusterByKubeconfig(ctx context.Context, q queryer, workspaceID, kubeconfigPath, kubeContext string) (Cluster, error) {
	row := q.QueryRow(ctx, clusterSelectQuery(`
		WHERE c.workspace_id = $1 AND c.kubeconfig_path = $2 AND c.kube_context = $3
		GROUP BY c.id
		LIMIT 1
	`), workspaceID, kubeconfigPath, kubeContext)
	cluster, err := scanCluster(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Cluster{}, ErrNotFound
	}
	return cluster, err
}

func updateClusterRecord(ctx context.Context, q queryer, cluster Cluster) (Cluster, error) {
	row := q.QueryRow(ctx, `
		WITH updated AS (
			UPDATE clusters
			SET name = $3,
				kubeconfig_path = $4,
				kube_context = $5,
				image_registry_prefix = $6,
				exposure_mode = $7,
				node_host = $8,
				preview_domain = $9,
				ingress_class = $10,
				status = $11,
				last_checked_at = now(),
				updated_at = now()
			WHERE workspace_id = $1 AND id = $2
			RETURNING *
		)
	`+clusterSelectProjection("updated"), cluster.WorkspaceID, cluster.ID, cluster.Name, cluster.KubeconfigPath, cluster.KubeContext, cluster.ImageRegistryPrefix, cluster.ExposureMode, cluster.NodeHost, cluster.PreviewDomain, cluster.IngressClass, cluster.Status)
	return scanCluster(row)
}

func normalizeClusterInput(existing Cluster, input ClusterInput) (Cluster, error) {
	cluster := existing
	cluster.Name = strings.Join(strings.Fields(strings.TrimSpace(input.Name)), " ")
	cluster.KubeconfigPath = strings.TrimSpace(input.KubeconfigPath)
	cluster.KubeContext = strings.TrimSpace(input.KubeContext)
	cluster.ImageRegistryPrefix = strings.TrimSpace(input.ImageRegistryPrefix)
	cluster.NodeHost = strings.TrimSpace(input.NodeHost)
	cluster.PreviewDomain = strings.TrimSpace(input.PreviewDomain)
	cluster.IngressClass = strings.TrimSpace(input.IngressClass)
	exposureMode, err := normalizeExposureMode(input.ExposureMode)
	if err != nil {
		return Cluster{}, err
	}
	if exposureMode == "" {
		exposureMode = existing.ExposureMode
	}
	if exposureMode == "" {
		exposureMode = "nodeport"
	}
	cluster.ExposureMode = exposureMode
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status == "" {
		status = existing.Status
	}
	if status == "" {
		status = "configured"
	}
	switch status {
	case "configured", "ready", "unreachable":
		cluster.Status = status
	default:
		return Cluster{}, fmt.Errorf("unsupported cluster status %q", input.Status)
	}
	if cluster.Name == "" {
		cluster.Name = importedClusterName(cluster.KubeconfigPath, cluster.KubeContext)
	}
	if cluster.Name == "" {
		return Cluster{}, errors.New("cluster name is required")
	}
	return cluster, nil
}

func normalizeExposureMode(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "":
		return "", nil
	case "nodeport", "node_port", "node-port":
		return "nodeport", nil
	case "ingress":
		return "ingress", nil
	default:
		return "", fmt.Errorf("unsupported exposure mode %q", value)
	}
}

func normalizeKubeconfigPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("kubeconfig path is required")
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", errors.New("kubeconfig path must be a file")
	}
	return abs, nil
}

func discoverDefaultKubeconfigs() (KubeconfigDiscoveryResult, error) {
	result := KubeconfigDiscoveryResult{Candidates: []KubeconfigCandidate{}, Skipped: []KubeconfigImportSkip{}}
	home, err := os.UserHomeDir()
	if err != nil {
		return result, err
	}
	kubeDir := filepath.Join(home, ".kube")
	entries, err := os.ReadDir(kubeDir)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	paths := []string{filepath.Join(kubeDir, "config")}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		paths = append(paths, filepath.Join(kubeDir, entry.Name()))
	}
	for _, candidatePath := range uniqueStrings(paths) {
		normalizedPath, err := normalizeKubeconfigPath(candidatePath)
		if err != nil {
			result.Skipped = append(result.Skipped, KubeconfigImportSkip{Path: candidatePath, Reason: err.Error()})
			continue
		}
		contexts, err := kubeconfigContexts(normalizedPath)
		if err != nil {
			result.Skipped = append(result.Skipped, KubeconfigImportSkip{Path: normalizedPath, Reason: err.Error()})
			continue
		}
		if len(contexts) > 0 {
			result.Candidates = append(result.Candidates, KubeconfigCandidate{Path: normalizedPath, Contexts: contexts})
		}
	}
	return result, nil
}

func kubeconfigContexts(kubeconfigPath string) ([]string, error) {
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return nil, err
	}
	contexts := make([]string, 0, len(config.Contexts))
	for name := range config.Contexts {
		contexts = append(contexts, name)
	}
	contexts = uniqueStrings(contexts)
	sort.Strings(contexts)
	return contexts, nil
}

func validateKubeconfigContext(ctx context.Context, kubeconfigPath, kubeContext string) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	overrides := &clientcmd.ConfigOverrides{}
	if strings.TrimSpace(kubeContext) != "" {
		overrides.CurrentContext = strings.TrimSpace(kubeContext)
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		overrides,
	).ClientConfig()
	if err != nil {
		return err
	}
	config.UserAgent = "mspace-server/kubeconfig-check"
	config.Timeout = 8 * time.Second
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	_, err = clientset.Discovery().ServerVersion()
	if err != nil {
		return err
	}
	_, err = clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1})
	return err
}

func kubeconfigStatus(ctx context.Context, kubeconfigPath, kubeContext string) string {
	if err := validateKubeconfigContext(ctx, kubeconfigPath, kubeContext); err != nil {
		return "unreachable"
	}
	return "ready"
}

func importedClusterName(kubeconfigPath, kubeContext string) string {
	if strings.TrimSpace(kubeContext) != "" {
		return strings.TrimSpace(kubeContext)
	}
	base := filepath.Base(strings.TrimSpace(kubeconfigPath))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "Kubernetes cluster"
	}
	return base
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func (s *PostgresStore) StartIssueTestDeploy(ctx Context, userID, workspaceID, issueID string, input StartTestDeployInput) (TestEnvironmentSessionResult, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	userID = strings.TrimSpace(userID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, strings.TrimSpace(userID)); err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	return s.queueIssueTestDeploy(ctx, dbctx, userID, workspaceID, issueID, input, false)
}

func (s *PostgresStore) queueIssueTestDeploy(ctx Context, dbctx context.Context, userID, workspaceID, issueID string, input StartTestDeployInput, automated bool) (TestEnvironmentSessionResult, error) {
	detail, err := s.GetIssue(ctx, userID, workspaceID, issueID)
	if err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	if detail.Project.ID == "" {
		return TestEnvironmentSessionResult{}, errors.New("attach a project before starting a test deployment")
	}
	if hasActiveAgentSession(detail.Sessions) {
		return TestEnvironmentSessionResult{}, errors.New("issue already has an active session")
	}
	return s.queueIssueTestDeployForDetail(ctx, dbctx, userID, workspaceID, issueID, detail, input, automated)
}

func (s *PostgresStore) queueIssueTestDeployForDetail(ctx Context, dbctx context.Context, userID, workspaceID, issueID string, detail IssueDetail, input StartTestDeployInput, automated bool) (TestEnvironmentSessionResult, error) {
	environment, err := s.buildIssueTestEnvironment(dbctx, detail, input)
	if err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	sourceNode, err := selectIssueChangeNodeForDeploy(detail.ChangeNodes, input.SourceCommitSHA, input.SourceSessionID)
	if err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	environment.SourceSessionID = sourceNode.SessionID
	environment.SourceCommitSHA = sourceNode.CommitSHA
	session, err := s.CreateAgentSession(ctx, userID, workspaceID, issueID, CreateAgentSessionInput{
		Provider:        "codex",
		AgentProfile:    firstNonEmpty(strings.TrimSpace(input.AgentProfile), "codex"),
		Command:         buildIssueTestDeployPrompt(detail, environment, sourceNode, automated),
		SourceSessionID: sourceNode.SessionID,
		SourceCommitSHA: sourceNode.CommitSHA,
		Automation:      testDeployAutomationMarker(automated),
	})
	if err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	environment.LastDeploySessionID = session.ID
	environment.NamespaceStatus = "deploying"
	if err := s.saveIssueTestEnvironment(dbctx, workspaceID, environment); err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	verb := "Queued test deployment"
	if automated {
		verb = "Auto-queued test deployment"
	}
	_ = s.addSystemComment(dbctx, workspaceID, issueID, fmt.Sprintf(
		"%s session `%s` for namespace `%s`.\n\nSource commit: `%s`\nCluster: `%s`\nRegistry: `%s`\nExposure: %s",
		verb,
		shortIdentifier(session.ID),
		environment.Namespace,
		shortCommitSHA(sourceNode.CommitSHA),
		environment.ClusterID,
		environment.ImageRegistryPrefix,
		previewStrategyLabel(environment),
	))
	return TestEnvironmentSessionResult{SessionID: session.ID, TestEnvironment: environment}, nil
}

func (s *PostgresStore) RequestIssueTestEnvironmentCleanup(ctx Context, userID, workspaceID, issueID string, input StartTestDeployInput) (TestEnvironmentSessionResult, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, strings.TrimSpace(userID)); err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	detail, err := s.GetIssue(ctx, userID, workspaceID, issueID)
	if err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	if detail.TestEnvironment == nil || strings.TrimSpace(detail.TestEnvironment.Namespace) == "" {
		return TestEnvironmentSessionResult{}, errors.New("issue has no test namespace to clean up")
	}
	if hasActiveAgentSession(detail.Sessions) {
		return TestEnvironmentSessionResult{}, errors.New("issue already has an active session")
	}
	environment := *detail.TestEnvironment
	environment.NamespaceStatus = "cleanup_requested"
	environment.CleanupStatus = "cleanup_requested"
	session, err := s.CreateAgentSession(ctx, userID, workspaceID, issueID, CreateAgentSessionInput{
		Provider:     "codex",
		AgentProfile: firstNonEmpty(strings.TrimSpace(input.AgentProfile), "codex"),
		Command:      buildIssueTestCleanupPrompt(detail, environment),
	})
	if err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	environment.LastCleanupSessionID = session.ID
	if err := s.saveIssueTestEnvironment(dbctx, workspaceID, environment); err != nil {
		return TestEnvironmentSessionResult{}, err
	}
	_ = s.addSystemComment(dbctx, workspaceID, issueID, fmt.Sprintf("Queued namespace cleanup session `%s` for `%s`.", shortIdentifier(session.ID), environment.Namespace))
	return TestEnvironmentSessionResult{SessionID: session.ID, TestEnvironment: environment}, nil
}

func (s *PostgresStore) RetainIssueTestEnvironment(ctx Context, userID, workspaceID, issueID string) (IssueTestEnvironment, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, strings.TrimSpace(userID)); err != nil {
		return IssueTestEnvironment{}, err
	}
	environment, err := s.loadIssueTestEnvironment(dbctx, workspaceID, issueID)
	if err != nil {
		return IssueTestEnvironment{}, err
	}
	environment.CleanupStatus = "retained"
	if err := s.saveIssueTestEnvironment(dbctx, workspaceID, environment); err != nil {
		return IssueTestEnvironment{}, err
	}
	_ = s.addSystemComment(dbctx, workspaceID, issueID, fmt.Sprintf("Retained test namespace `%s` for later inspection.", environment.Namespace))
	return environment, nil
}

func (s *PostgresStore) GetIssueTestEnvironmentResources(ctx Context, userID, workspaceID, issueID string) (IssueTestEnvironmentResources, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, strings.TrimSpace(userID)); err != nil {
		return IssueTestEnvironmentResources{}, err
	}
	environment, err := s.loadIssueTestEnvironment(dbctx, workspaceID, issueID)
	if err != nil {
		return IssueTestEnvironmentResources{}, err
	}
	clusterName := ""
	if environment.ClusterID != "" {
		cluster, err := loadCluster(dbctx, s.pool, workspaceID, environment.ClusterID)
		if err == nil {
			clusterName = cluster.Name
			environment = mergeEnvironmentClusterDefaults(environment, cluster)
		} else if !errors.Is(err, ErrNotFound) {
			return IssueTestEnvironmentResources{}, err
		}
	}
	return listIssueTestEnvironmentResources(asContext(ctx), environment, clusterName)
}

func (s *PostgresStore) ProbeIssueTestEnvironment(ctx Context, userID, workspaceID, issueID string) (IssueTestEnvironment, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, strings.TrimSpace(userID)); err != nil {
		return IssueTestEnvironment{}, err
	}
	environment, err := s.loadIssueTestEnvironment(dbctx, workspaceID, issueID)
	if err != nil {
		return IssueTestEnvironment{}, err
	}
	if strings.TrimSpace(environment.PreviewURL) == "" {
		return environment, nil
	}
	ctxProbe, cancel := context.WithTimeout(asContext(ctx), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctxProbe, http.MethodGet, environment.PreviewURL, nil)
	if err != nil {
		environment.NamespaceStatus = "preview_unverified"
		_ = s.saveIssueTestEnvironment(dbctx, workspaceID, environment)
		return environment, nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		environment.NamespaceStatus = "preview_unverified"
		_ = s.saveIssueTestEnvironment(dbctx, workspaceID, environment)
		return environment, nil
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 500 {
		environment.NamespaceStatus = "active"
	}
	if err := s.saveIssueTestEnvironment(dbctx, workspaceID, environment); err != nil {
		return IssueTestEnvironment{}, err
	}
	return environment, nil
}

func (s *PostgresStore) CreateIssuePullRequestHandoff(ctx Context, userID, workspaceID, issueID string, input CreatePullRequestInput) (IssueHandoff, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, strings.TrimSpace(userID)); err != nil {
		return IssueHandoff{}, err
	}
	detail, err := s.GetIssue(ctx, userID, workspaceID, issueID)
	if err != nil {
		return IssueHandoff{}, err
	}
	if detail.Project.ID == "" {
		return IssueHandoff{}, errors.New("attach a project before creating a handoff")
	}
	sourceNode, err := selectIssueChangeNodeForDeploy(detail.ChangeNodes, input.SourceCommitSHA, input.SourceSessionID)
	if err != nil {
		return IssueHandoff{}, err
	}
	handoff := IssueHandoff{
		IssueID:         issueID,
		SourceSessionID: sourceNode.SessionID,
		SourceCommitSHA: sourceNode.CommitSHA,
		Branch:          sourceNode.Branch,
		HeadCommitSHA:   sourceNode.CommitSHA,
		Commits: []IssueHandoffCommit{{
			SHA:      sourceNode.CommitSHA,
			ShortSHA: shortCommitSHA(sourceNode.CommitSHA),
			Subject:  sourceNode.Subject,
		}},
		Kind:            "pr",
		PRTitle:         strings.TrimSpace(input.Title),
		PreviewURL:      issueHandoffPreviewURL(detail, sourceNode.SessionID, sourceNode.CommitSHA),
		EvidenceSummary: issueHandoffEvidenceSummary(detail, sourceNode.SessionID, sourceNode.CommitSHA),
		CreatedVia:      "server",
	}
	if handoff.PRTitle == "" {
		handoff.PRTitle = firstNonEmpty(sourceNode.Subject, detail.Issue.Title)
	}
	stored, err := s.storeIssueHandoff(dbctx, workspaceID, handoff)
	if err != nil {
		return IssueHandoff{}, err
	}
	_ = s.addSystemComment(dbctx, workspaceID, issueID, issueHandoffComment(stored))
	return stored, nil
}

func (s *PostgresStore) RefreshIssueHandoff(ctx Context, userID, workspaceID, issueID, handoffID string) (IssueHandoff, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	issueID = strings.TrimSpace(issueID)
	handoffID = strings.TrimSpace(handoffID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, strings.TrimSpace(userID)); err != nil {
		return IssueHandoff{}, err
	}
	handoff, err := s.loadIssueHandoff(dbctx, workspaceID, issueID, handoffID)
	if err != nil {
		return IssueHandoff{}, err
	}
	handoff.LastCheckedAt = time.Now().UTC().Format(time.RFC3339)
	handoff.Error = "GitHub PR sync is server-owned but no GitHub App PR executor is configured yet."
	return s.storeIssueHandoff(dbctx, workspaceID, handoff)
}

func hasActiveAgentSession(sessions []AgentSession) bool {
	for _, session := range sessions {
		if session.Status == "queued" || session.Status == "running" {
			return true
		}
	}
	return false
}

func (s *PostgresStore) buildIssueTestEnvironment(ctx context.Context, detail IssueDetail, input StartTestDeployInput) (IssueTestEnvironment, error) {
	environment := IssueTestEnvironment{}
	if detail.TestEnvironment != nil {
		environment = *detail.TestEnvironment
	}
	environment.IssueID = detail.Issue.ID
	if strings.TrimSpace(environment.Namespace) == "" {
		environment.Namespace = defaultIssueNamespace(detail)
	}
	environmentID := firstNonEmpty(input.EnvironmentID, input.ClusterID, environment.EnvironmentID, environment.ClusterID, detail.Project.DefaultEnvironmentID, detail.Project.DefaultClusterID)
	if environmentID == "" {
		return environment, errors.New("environment is required before starting a test deployment")
	}
	selectedEnvironment, err := s.loadEnvironment(ctx, detail.Issue.WorkspaceID, environmentID)
	if err != nil {
		return environment, err
	}
	if selectedEnvironment.Kind != environmentKindKubernetes || selectedEnvironment.Kubernetes == nil {
		return environment, errors.New("test deployment currently requires a Kubernetes environment")
	}
	selectedCluster, err := loadCluster(ctx, s.pool, detail.Issue.WorkspaceID, selectedEnvironment.Kubernetes.ClusterID)
	if err != nil {
		return environment, err
	}
	exposureMode, err := normalizeExposureMode(input.ExposureMode)
	if err != nil {
		return environment, err
	}
	if exposureMode == "" {
		exposureMode = selectedCluster.ExposureMode
	}
	if exposureMode == "" {
		exposureMode = "nodeport"
	}
	environment.NamespaceStatus = "planned"
	environment.CleanupStatus = "retained"
	environment.ClusterID = selectedCluster.ID
	environment.EnvironmentID = selectedEnvironment.ID
	environment.EnvironmentKind = selectedEnvironment.Kind
	environment.EnvironmentSnapshot = environmentSnapshot(selectedEnvironment)
	environment.KubeconfigPath = selectedCluster.KubeconfigPath
	environment.KubeContext = selectedCluster.KubeContext
	environment.ImageRegistryPrefix = selectedCluster.ImageRegistryPrefix
	environment.ExposureMode = exposureMode
	environment.NodeHost = firstNonEmpty(input.NodeHost, selectedCluster.NodeHost)
	if exposureMode == "ingress" {
		environment.PreviewDomain = firstNonEmpty(input.PreviewDomain, selectedCluster.PreviewDomain)
		environment.IngressClass = firstNonEmpty(input.IngressClass, selectedCluster.IngressClass)
	} else {
		environment.PreviewDomain = ""
		environment.IngressClass = ""
	}
	if environment.KubeconfigPath == "" {
		return environment, errors.New("selected cluster needs a kubeconfig path before starting a test deployment")
	}
	if environment.ImageRegistryPrefix == "" {
		return environment, errors.New("selected cluster needs an image registry prefix before starting a test deployment")
	}
	if environment.ExposureMode == "ingress" && environment.PreviewDomain == "" {
		return environment, errors.New("preview domain is required when ingress exposure is selected")
	}
	return environment, nil
}

func mergeEnvironmentClusterDefaults(environment IssueTestEnvironment, cluster Cluster) IssueTestEnvironment {
	if environment.KubeconfigPath == "" {
		environment.KubeconfigPath = cluster.KubeconfigPath
	}
	if environment.KubeContext == "" {
		environment.KubeContext = cluster.KubeContext
	}
	if environment.NodeHost == "" {
		environment.NodeHost = cluster.NodeHost
	}
	if environment.ExposureMode == "" {
		environment.ExposureMode = cluster.ExposureMode
	}
	return environment
}

func defaultIssueNamespace(detail IssueDetail) string {
	return dnsLabel("mspace-" + detail.Project.Name + "-" + shortIdentifier(detail.Issue.ID))
}

func dnsLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastHyphen := false
	for _, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			builder.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen && builder.Len() > 0 {
			builder.WriteByte('-')
			lastHyphen = true
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		result = "mspace-issue"
	}
	if len(result) > 63 {
		result = strings.TrimRight(result[:63], "-")
	}
	if result == "" {
		return "mspace-issue"
	}
	return result
}

func selectIssueChangeNodeForDeploy(nodes []IssueChangeNode, sourceCommitSHA, sourceSessionID string) (IssueChangeNode, error) {
	sourceCommitSHA = strings.TrimSpace(sourceCommitSHA)
	sourceSessionID = strings.TrimSpace(sourceSessionID)
	if len(nodes) == 0 {
		return IssueChangeNode{}, errors.New("no source commits found for this issue; run an agent session that changes code before deploying")
	}
	for _, node := range nodes {
		commitMatches := sourceCommitSHA == "" || node.CommitSHA == sourceCommitSHA || strings.HasPrefix(node.CommitSHA, sourceCommitSHA)
		sessionMatches := sourceSessionID == "" || node.SessionID == sourceSessionID
		if commitMatches && sessionMatches {
			if node.Error != "" {
				return IssueChangeNode{}, fmt.Errorf("source commit cannot be deployed: %s", node.Error)
			}
			return node, nil
		}
	}
	if sourceCommitSHA == "" && sourceSessionID == "" {
		node := nodes[0]
		if node.Error != "" {
			return IssueChangeNode{}, fmt.Errorf("source commit cannot be deployed: %s", node.Error)
		}
		return node, nil
	}
	return IssueChangeNode{}, errors.New("selected source commit was not found on this issue")
}

func buildIssueTestDeployPrompt(detail IssueDetail, environment IssueTestEnvironment, source IssueChangeNode, automated bool) string {
	var builder strings.Builder
	builder.WriteString("Deploy a test environment for this issue.\n\n")
	if automated {
		builder.WriteString("mspace automatically queued this deployment after a successful source session because the workspace auto-deploy test environment setting is enabled. Do not create a PR unless explicitly asked in a separate turn.\n\n")
	} else {
		builder.WriteString("The user manually triggered this deployment after agent work, so do not create a PR unless explicitly asked in a separate turn.\n\n")
	}
	builder.WriteString("Source code to deploy:\n")
	builder.WriteString(fmt.Sprintf("- Source commit: %s\n", source.CommitSHA))
	builder.WriteString(fmt.Sprintf("- Source session: %s\n", shortIdentifier(source.SessionID)))
	builder.WriteString(fmt.Sprintf("- Source branch: %s\n", firstNonEmpty(source.Branch, "not recorded")))
	builder.WriteString(fmt.Sprintf("- Source subject: %s\n", firstNonEmpty(source.Subject, "not recorded")))
	builder.WriteString("- The prepared session worktree is checked out at the source commit. Before building, verify `git rev-parse HEAD` matches the source commit.\n")
	builder.WriteString("- Build and deploy exactly this commit, not the latest branch tip or another session's worktree.\n\n")
	builder.WriteString("Deployment contract:\n")
	builder.WriteString(fmt.Sprintf("- Environment ID: %s\n", firstNonEmpty(environment.EnvironmentID, environment.ClusterID)))
	builder.WriteString(fmt.Sprintf("- Environment kind: %s\n", firstNonEmpty(environment.EnvironmentKind, environmentKindKubernetes)))
	builder.WriteString(fmt.Sprintf("- Kubernetes cluster ID: %s\n", environment.ClusterID))
	builder.WriteString(fmt.Sprintf("- Kubeconfig path: %s\n", environment.KubeconfigPath))
	if environment.KubeContext != "" {
		builder.WriteString(fmt.Sprintf("- Kube context: %s\n", environment.KubeContext))
	}
	builder.WriteString(fmt.Sprintf("- Issue namespace to create and manage: %s\n", environment.Namespace))
	builder.WriteString(fmt.Sprintf("- Image registry prefix: %s\n", environment.ImageRegistryPrefix))
	builder.WriteString(fmt.Sprintf("- Project deploy command hint: %s\n", firstNonEmpty(detail.Project.DeployCommand, "not configured")))
	builder.WriteString(fmt.Sprintf("- Project validation command hint: %s\n", firstNonEmpty(detail.Project.ValidationCommand, "not configured")))
	builder.WriteString("- Build and push linux/amd64 images unless the issue explicitly requires another platform.\n")
	builder.WriteString("- Create the namespace yourself if it does not exist.\n")
	builder.WriteString("- Deploy or update only namespaced resources inside the issue namespace.\n")
	builder.WriteString("- Do not read Kubernetes Secrets.\n")
	builder.WriteString("- Return a team-accessible preview URL after deployment.\n")
	if environment.ExposureMode == "ingress" {
		builder.WriteString(fmt.Sprintf("- Preferred preview exposure: Ingress or HTTPRoute under domain %s.\n", environment.PreviewDomain))
		if environment.IngressClass != "" {
			builder.WriteString(fmt.Sprintf("- Preferred ingress class: %s.\n", environment.IngressClass))
		}
	} else {
		builder.WriteString("- Preferred preview exposure: Service type=NodePort.\n")
		if environment.NodeHost != "" {
			builder.WriteString(fmt.Sprintf("- Use this node host when forming the preview URL: %s.\n", environment.NodeHost))
		} else {
			builder.WriteString("- Discover a usable node ExternalIP, node hostname, or fallback node address for the NodePort URL.\n")
		}
	}
	builder.WriteString("\nRequired completion evidence:\n")
	builder.WriteString("- Image reference(s) pushed.\n")
	builder.WriteString("- Kubernetes resources applied.\n")
	builder.WriteString("- Pods, Services, Ingress/HTTPRoute if any, and recent Events inspected.\n")
	builder.WriteString("- Preview URL probed with curl or an equivalent HTTP check.\n")
	builder.WriteString("- Final answer includes the preview URL, namespace, image reference, validation result, and any blocker.\n")
	builder.WriteString("- Also write a JSON file to `${MSPACE_SESSION_ARTIFACT_DIR}/test-environment.json` with at least `previewUrl` when a URL is available.\n")
	builder.WriteString("- Also write `${MSPACE_SESSION_ARTIFACT_DIR}/review-evidence.json` with `commandsRun`, `tests`, `buildResult`, `deploymentResult`, `agentSummary`, `risks`, and `followUps`.\n")
	return builder.String()
}

func buildIssueTestCleanupPrompt(detail IssueDetail, environment IssueTestEnvironment) string {
	var builder strings.Builder
	builder.WriteString("Clean up the test environment for this issue.\n\n")
	builder.WriteString("The user manually chose to remove this issue's test namespace. Use the provided kubeconfig and delete only this namespace and resources inside it.\n\n")
	builder.WriteString(fmt.Sprintf("- Kubeconfig path: %s\n", environment.KubeconfigPath))
	if environment.KubeContext != "" {
		builder.WriteString(fmt.Sprintf("- Kube context: %s\n", environment.KubeContext))
	}
	builder.WriteString(fmt.Sprintf("- Namespace to delete: %s\n", environment.Namespace))
	builder.WriteString(fmt.Sprintf("- Issue: %s\n", detail.Issue.Title))
	builder.WriteString("\nAfter cleanup, verify whether the namespace still exists and report the result. Do not delete any other namespace or cluster-scoped resource.\n")
	return builder.String()
}

func previewStrategyLabel(environment IssueTestEnvironment) string {
	if environment.ExposureMode == "ingress" || environment.PreviewDomain != "" {
		if environment.IngressClass != "" {
			return fmt.Sprintf("Ingress `%s` on `%s`", environment.IngressClass, environment.PreviewDomain)
		}
		return fmt.Sprintf("Ingress on `%s`", environment.PreviewDomain)
	}
	if environment.NodeHost != "" {
		return fmt.Sprintf("NodePort via `%s`", environment.NodeHost)
	}
	return "NodePort with discovered node address"
}

func testDeployAutomationMarker(automated bool) string {
	if automated {
		return autoDeployTestEnvironmentAutomation
	}
	return testDeployAutomation
}

func runtimeTaskAutomation(task RuntimeTask) string {
	var payload struct {
		Automation string `json:"automation"`
	}
	_ = json.Unmarshal(task.Payload, &payload)
	return strings.TrimSpace(payload.Automation)
}

func isAutoDeployTestEnvironmentTask(task RuntimeTask) bool {
	return runtimeTaskAutomation(task) == autoDeployTestEnvironmentAutomation
}

func isIssueTestDeployTask(task RuntimeTask) bool {
	switch runtimeTaskAutomation(task) {
	case testDeployAutomation, autoDeployTestEnvironmentAutomation:
		return true
	default:
		return false
	}
}

func runtimeTaskIsDryRun(task RuntimeTask) bool {
	var result struct {
		DryRun bool `json:"dryRun"`
	}
	_ = json.Unmarshal(task.Result, &result)
	return result.DryRun
}

func (s *PostgresStore) loadIssueTestEnvironment(ctx context.Context, workspaceID, issueID string) (IssueTestEnvironment, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT issue_id::text, COALESCE(cluster_id::text, ''), environment_id, environment_kind, environment_snapshot, namespace, namespace_status, cleanup_status, preview_url, image_registry_prefix, kubeconfig_path, kube_context, exposure_mode, preview_domain, ingress_class, node_host, last_deploy_session_id, last_cleanup_session_id, source_session_id, source_commit_sha, created_at, updated_at
		FROM issue_test_environments
		WHERE workspace_id = $1 AND issue_id = $2
	`, workspaceID, issueID)
	environment, err := scanIssueTestEnvironment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return IssueTestEnvironment{}, ErrNotFound
	}
	return environment, err
}

func (s *PostgresStore) loadIssueTestEnvironmentOptional(ctx context.Context, workspaceID, issueID string) (*IssueTestEnvironment, error) {
	environment, err := s.loadIssueTestEnvironment(ctx, workspaceID, issueID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &environment, nil
}

func (s *PostgresStore) saveIssueTestEnvironment(ctx context.Context, workspaceID string, environment IssueTestEnvironment) error {
	environment.EnvironmentID = firstNonEmpty(environment.EnvironmentID, environment.ClusterID)
	environment.EnvironmentKind = firstNonEmpty(environment.EnvironmentKind, environmentKindKubernetes)
	if len(environment.EnvironmentSnapshot) == 0 {
		environment.EnvironmentSnapshot = json.RawMessage(`{}`)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO issue_test_environments (workspace_id, issue_id, cluster_id, environment_id, environment_kind, environment_snapshot, namespace, namespace_status, cleanup_status, preview_url, image_registry_prefix, kubeconfig_path, kube_context, exposure_mode, preview_domain, ingress_class, node_host, last_deploy_session_id, last_cleanup_session_id, source_session_id, source_commit_sha)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6::jsonb, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
		ON CONFLICT (issue_id) DO UPDATE SET
			workspace_id = EXCLUDED.workspace_id,
			cluster_id = EXCLUDED.cluster_id,
			environment_id = EXCLUDED.environment_id,
			environment_kind = EXCLUDED.environment_kind,
			environment_snapshot = EXCLUDED.environment_snapshot,
			namespace = EXCLUDED.namespace,
			namespace_status = EXCLUDED.namespace_status,
			cleanup_status = EXCLUDED.cleanup_status,
			preview_url = EXCLUDED.preview_url,
			image_registry_prefix = EXCLUDED.image_registry_prefix,
			kubeconfig_path = EXCLUDED.kubeconfig_path,
			kube_context = EXCLUDED.kube_context,
			exposure_mode = EXCLUDED.exposure_mode,
			preview_domain = EXCLUDED.preview_domain,
			ingress_class = EXCLUDED.ingress_class,
			node_host = EXCLUDED.node_host,
			last_deploy_session_id = EXCLUDED.last_deploy_session_id,
			last_cleanup_session_id = EXCLUDED.last_cleanup_session_id,
			source_session_id = EXCLUDED.source_session_id,
			source_commit_sha = EXCLUDED.source_commit_sha,
			updated_at = now()
	`, workspaceID, environment.IssueID, environment.ClusterID, environment.EnvironmentID, environment.EnvironmentKind, jsonOrObject(environment.EnvironmentSnapshot), environment.Namespace, environment.NamespaceStatus, environment.CleanupStatus, environment.PreviewURL, environment.ImageRegistryPrefix, environment.KubeconfigPath, environment.KubeContext, environment.ExposureMode, environment.PreviewDomain, environment.IngressClass, environment.NodeHost, environment.LastDeploySessionID, environment.LastCleanupSessionID, environment.SourceSessionID, environment.SourceCommitSHA)
	return err
}

func scanIssueTestEnvironment(row scanner) (IssueTestEnvironment, error) {
	var environment IssueTestEnvironment
	var snapshotBytes []byte
	var createdAt, updatedAt time.Time
	if err := row.Scan(&environment.IssueID, &environment.ClusterID, &environment.EnvironmentID, &environment.EnvironmentKind, &snapshotBytes, &environment.Namespace, &environment.NamespaceStatus, &environment.CleanupStatus, &environment.PreviewURL, &environment.ImageRegistryPrefix, &environment.KubeconfigPath, &environment.KubeContext, &environment.ExposureMode, &environment.PreviewDomain, &environment.IngressClass, &environment.NodeHost, &environment.LastDeploySessionID, &environment.LastCleanupSessionID, &environment.SourceSessionID, &environment.SourceCommitSHA, &createdAt, &updatedAt); err != nil {
		return IssueTestEnvironment{}, err
	}
	environment.EnvironmentID = firstNonEmpty(environment.EnvironmentID, environment.ClusterID)
	environment.EnvironmentKind = firstNonEmpty(environment.EnvironmentKind, environmentKindKubernetes)
	environment.EnvironmentSnapshot = copyRawMessage(json.RawMessage(snapshotBytes))
	environment.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	environment.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return environment, nil
}

func listIssueTestEnvironmentResources(ctx context.Context, environment IssueTestEnvironment, clusterName string) (IssueTestEnvironmentResources, error) {
	namespace := strings.TrimSpace(environment.Namespace)
	if namespace == "" {
		return IssueTestEnvironmentResources{}, errors.New("issue test namespace is empty")
	}
	clientset, err := issueTestEnvironmentKubernetesClient(environment)
	if err != nil {
		return IssueTestEnvironmentResources{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	resources := IssueTestEnvironmentResources{
		IssueID:         environment.IssueID,
		ClusterID:       environment.ClusterID,
		ClusterName:     clusterName,
		Context:         environment.KubeContext,
		Namespace:       namespace,
		NamespaceStatus: environment.NamespaceStatus,
		CleanupStatus:   environment.CleanupStatus,
		ExposureMode:    environment.ExposureMode,
		PreviewURL:      environment.PreviewURL,
		NodeHost:        environment.NodeHost,
		RefreshedAt:     time.Now().UTC().Format(time.RFC3339),
		Pods:            []KubernetesPodResource{},
		Services:        []KubernetesServiceResource{},
		Deployments:     []KubernetesDeploymentResource{},
		Ingresses:       []KubernetesIngressResource{},
		Events:          []KubernetesEventResource{},
		Errors:          []KubernetesResourceFetchError{},
	}
	if pods, err := listKubernetesPods(ctx, clientset, namespace); err != nil {
		resources.Errors = append(resources.Errors, KubernetesResourceFetchError{Section: "pods", Message: err.Error()})
	} else {
		resources.Pods = pods
	}
	if services, err := listKubernetesServices(ctx, clientset, namespace, environment.NodeHost); err != nil {
		resources.Errors = append(resources.Errors, KubernetesResourceFetchError{Section: "services", Message: err.Error()})
	} else {
		resources.Services = services
	}
	if deployments, err := listKubernetesDeployments(ctx, clientset, namespace); err != nil {
		resources.Errors = append(resources.Errors, KubernetesResourceFetchError{Section: "deployments", Message: err.Error()})
	} else {
		resources.Deployments = deployments
	}
	if ingresses, err := listKubernetesIngresses(ctx, clientset, namespace); err != nil {
		resources.Errors = append(resources.Errors, KubernetesResourceFetchError{Section: "ingresses", Message: err.Error()})
	} else {
		resources.Ingresses = ingresses
	}
	if events, err := listKubernetesEvents(ctx, clientset, namespace); err != nil {
		resources.Errors = append(resources.Errors, KubernetesResourceFetchError{Section: "events", Message: err.Error()})
	} else {
		resources.Events = events
	}
	return resources, nil
}

func issueTestEnvironmentKubernetesClient(environment IssueTestEnvironment) (*kubernetes.Clientset, error) {
	kubeconfigPath, err := normalizeKubeconfigPath(environment.KubeconfigPath)
	if err != nil {
		return nil, err
	}
	overrides := &clientcmd.ConfigOverrides{}
	if kubeContext := strings.TrimSpace(environment.KubeContext); kubeContext != "" {
		overrides.CurrentContext = kubeContext
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath},
		overrides,
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	config.UserAgent = "mspace-server/test-environment-resources"
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	return clientset, nil
}

func listKubernetesPods(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]KubernetesPodResource, error) {
	list, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	pods := make([]KubernetesPodResource, 0, len(list.Items))
	for _, pod := range list.Items {
		pods = append(pods, mapKubernetesPod(pod))
	}
	sort.Slice(pods, func(i, j int) bool { return pods[i].Name < pods[j].Name })
	return pods, nil
}

func mapKubernetesPod(pod corev1.Pod) KubernetesPodResource {
	resource := KubernetesPodResource{
		Name:       pod.Name,
		Phase:      string(pod.Status.Phase),
		NodeName:   pod.Spec.NodeName,
		PodIP:      pod.Status.PodIP,
		HostIP:     pod.Status.HostIP,
		CreatedAt:  kubernetesTimeString(pod.CreationTimestamp),
		Containers: []KubernetesPodContainer{},
	}
	statusesByName := map[string]corev1.ContainerStatus{}
	for _, status := range pod.Status.ContainerStatuses {
		statusesByName[status.Name] = status
	}
	for _, container := range pod.Spec.Containers {
		status, ok := statusesByName[container.Name]
		item := KubernetesPodContainer{Name: container.Name, State: "waiting"}
		if ok {
			state, reason := kubernetesContainerState(status.State)
			item.Ready = status.Ready
			item.RestartCount = status.RestartCount
			item.State = state
			item.Reason = reason
			resource.Restarts += status.RestartCount
			if status.Ready {
				resource.ReadyContainers++
			}
		}
		resource.Containers = append(resource.Containers, item)
	}
	resource.TotalContainers = len(pod.Spec.Containers)
	return resource
}

func kubernetesContainerState(state corev1.ContainerState) (string, string) {
	if state.Waiting != nil {
		return "waiting", state.Waiting.Reason
	}
	if state.Running != nil {
		return "running", ""
	}
	if state.Terminated != nil {
		return "terminated", firstNonEmpty(state.Terminated.Reason, fmt.Sprintf("exit %d", state.Terminated.ExitCode))
	}
	return "unknown", ""
}

func listKubernetesServices(ctx context.Context, clientset *kubernetes.Clientset, namespace, nodeHost string) ([]KubernetesServiceResource, error) {
	list, err := clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	services := make([]KubernetesServiceResource, 0, len(list.Items))
	for _, service := range list.Items {
		services = append(services, mapKubernetesService(service, nodeHost))
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	return services, nil
}

func mapKubernetesService(service corev1.Service, nodeHost string) KubernetesServiceResource {
	externalIPs := append([]string{}, service.Spec.ExternalIPs...)
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if strings.TrimSpace(ingress.IP) != "" {
			externalIPs = append(externalIPs, ingress.IP)
		}
		if strings.TrimSpace(ingress.Hostname) != "" {
			externalIPs = append(externalIPs, ingress.Hostname)
		}
	}
	resource := KubernetesServiceResource{
		Name:       service.Name,
		Type:       string(service.Spec.Type),
		ClusterIP:  service.Spec.ClusterIP,
		ExternalIP: strings.Join(uniqueStrings(externalIPs), ", "),
		CreatedAt:  kubernetesTimeString(service.CreationTimestamp),
		Ports:      []KubernetesServicePort{},
	}
	for _, port := range service.Spec.Ports {
		item := KubernetesServicePort{
			Name:       port.Name,
			Protocol:   string(port.Protocol),
			Port:       port.Port,
			TargetPort: port.TargetPort.String(),
			NodePort:   port.NodePort,
		}
		if service.Spec.Type == corev1.ServiceTypeNodePort || service.Spec.Type == corev1.ServiceTypeLoadBalancer {
			item.URL = nodePortPreviewURL(nodeHost, port.NodePort)
		}
		resource.Ports = append(resource.Ports, item)
	}
	return resource
}

func nodePortPreviewURL(nodeHost string, nodePort int32) string {
	host := strings.TrimSpace(nodeHost)
	if host == "" || nodePort <= 0 {
		return ""
	}
	scheme := "http"
	if parsed, err := url.Parse(host); err == nil && parsed.Scheme != "" {
		scheme = parsed.Scheme
		host = parsed.Hostname()
	}
	if strings.Count(host, ":") > 1 && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, nodePort)
}

func listKubernetesDeployments(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]KubernetesDeploymentResource, error) {
	list, err := clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	deployments := make([]KubernetesDeploymentResource, 0, len(list.Items))
	for _, deployment := range list.Items {
		deployments = append(deployments, mapKubernetesDeployment(deployment))
	}
	sort.Slice(deployments, func(i, j int) bool { return deployments[i].Name < deployments[j].Name })
	return deployments, nil
}

func mapKubernetesDeployment(deployment appsv1.Deployment) KubernetesDeploymentResource {
	resource := KubernetesDeploymentResource{
		Name:              deployment.Name,
		Replicas:          deployment.Status.Replicas,
		ReadyReplicas:     deployment.Status.ReadyReplicas,
		UpdatedReplicas:   deployment.Status.UpdatedReplicas,
		AvailableReplicas: deployment.Status.AvailableReplicas,
		CreatedAt:         kubernetesTimeString(deployment.CreationTimestamp),
		Conditions:        []KubernetesCondition{},
	}
	for _, condition := range deployment.Status.Conditions {
		resource.Conditions = append(resource.Conditions, KubernetesCondition{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		})
	}
	return resource
}

func listKubernetesIngresses(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]KubernetesIngressResource, error) {
	list, err := clientset.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	ingresses := make([]KubernetesIngressResource, 0, len(list.Items))
	for _, ingress := range list.Items {
		ingresses = append(ingresses, mapKubernetesIngress(ingress))
	}
	sort.Slice(ingresses, func(i, j int) bool { return ingresses[i].Name < ingresses[j].Name })
	return ingresses, nil
}

func mapKubernetesIngress(ingress networkingv1.Ingress) KubernetesIngressResource {
	className := ""
	if ingress.Spec.IngressClassName != nil {
		className = *ingress.Spec.IngressClassName
	}
	hosts := []string{}
	for _, rule := range ingress.Spec.Rules {
		if strings.TrimSpace(rule.Host) != "" {
			hosts = append(hosts, rule.Host)
		}
	}
	for _, tls := range ingress.Spec.TLS {
		hosts = append(hosts, tls.Hosts...)
	}
	addresses := []string{}
	for _, item := range ingress.Status.LoadBalancer.Ingress {
		if strings.TrimSpace(item.IP) != "" {
			addresses = append(addresses, item.IP)
		}
		if strings.TrimSpace(item.Hostname) != "" {
			addresses = append(addresses, item.Hostname)
		}
	}
	return KubernetesIngressResource{
		Name:      ingress.Name,
		ClassName: className,
		Hosts:     uniqueStrings(hosts),
		Addresses: uniqueStrings(addresses),
		CreatedAt: kubernetesTimeString(ingress.CreationTimestamp),
	}
}

func listKubernetesEvents(ctx context.Context, clientset *kubernetes.Clientset, namespace string) ([]KubernetesEventResource, error) {
	list, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	sort.Slice(list.Items, func(i, j int) bool {
		return kubernetesEventSortTime(list.Items[i]).After(kubernetesEventSortTime(list.Items[j]))
	})
	limit := len(list.Items)
	if limit > 50 {
		limit = 50
	}
	events := make([]KubernetesEventResource, 0, limit)
	for _, event := range list.Items[:limit] {
		events = append(events, mapKubernetesEvent(event))
	}
	return events, nil
}

func mapKubernetesEvent(event corev1.Event) KubernetesEventResource {
	return KubernetesEventResource{
		Type:         event.Type,
		Reason:       event.Reason,
		Message:      event.Message,
		InvolvedKind: event.InvolvedObject.Kind,
		InvolvedName: event.InvolvedObject.Name,
		Count:        event.Count,
		FirstSeen:    kubernetesEventTimeString(event.FirstTimestamp.Time),
		LastSeen:     kubernetesEventTimeString(kubernetesEventSortTime(event)),
		CreatedAt:    kubernetesTimeString(event.CreationTimestamp),
	}
}

func kubernetesEventSortTime(event corev1.Event) time.Time {
	if !event.EventTime.Time.IsZero() {
		return event.EventTime.Time
	}
	if !event.LastTimestamp.Time.IsZero() {
		return event.LastTimestamp.Time
	}
	if !event.FirstTimestamp.Time.IsZero() {
		return event.FirstTimestamp.Time
	}
	return event.CreationTimestamp.Time
}

func kubernetesTimeString(value metav1.Time) string {
	if value.Time.IsZero() {
		return ""
	}
	return value.Time.UTC().Format(time.RFC3339)
}

func kubernetesEventTimeString(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func (s *PostgresStore) storeIssueHandoff(ctx context.Context, workspaceID string, handoff IssueHandoff) (IssueHandoff, error) {
	kind := strings.ToLower(strings.TrimSpace(handoff.Kind))
	if kind == "" {
		kind = "branch"
		if strings.TrimSpace(handoff.PRURL) != "" {
			kind = "pr"
		}
	}
	if kind != "branch" && kind != "pr" {
		return IssueHandoff{}, fmt.Errorf("unsupported handoff kind %q", handoff.Kind)
	}
	commits, err := json.Marshal(handoff.Commits)
	if err != nil {
		return IssueHandoff{}, err
	}
	var lastChecked any
	if strings.TrimSpace(handoff.LastCheckedAt) != "" {
		parsed, err := time.Parse(time.RFC3339, handoff.LastCheckedAt)
		if err != nil {
			return IssueHandoff{}, err
		}
		lastChecked = parsed
	}
	if kind == "pr" {
		existing, err := s.loadCurrentIssuePullRequestHandoff(ctx, workspaceID, handoff.IssueID)
		if err == nil && existing.ID != handoff.ID {
			handoff.ID = existing.ID
		} else if err != nil && !errors.Is(err, ErrNotFound) {
			return IssueHandoff{}, err
		}
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO issue_handoffs (id, workspace_id, issue_id, source_session_id, source_commit_sha, branch, head_commit_sha, commits_json, kind, pr_url, pr_number, pr_state, pr_title, preview_url, evidence_summary, created_via, last_checked_at, error)
		VALUES (COALESCE(NULLIF($1, '')::uuid, gen_random_uuid()), $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		ON CONFLICT (id) DO UPDATE SET
			source_session_id = EXCLUDED.source_session_id,
			source_commit_sha = EXCLUDED.source_commit_sha,
			branch = EXCLUDED.branch,
			head_commit_sha = EXCLUDED.head_commit_sha,
			commits_json = EXCLUDED.commits_json,
			kind = EXCLUDED.kind,
			pr_url = EXCLUDED.pr_url,
			pr_number = EXCLUDED.pr_number,
			pr_state = EXCLUDED.pr_state,
			pr_title = EXCLUDED.pr_title,
			preview_url = EXCLUDED.preview_url,
			evidence_summary = EXCLUDED.evidence_summary,
			created_via = EXCLUDED.created_via,
			last_checked_at = EXCLUDED.last_checked_at,
			error = EXCLUDED.error,
			updated_at = now()
		RETURNING id::text, issue_id::text, source_session_id, source_commit_sha, branch, head_commit_sha, commits_json, kind, pr_url, pr_number, pr_state, pr_title, preview_url, evidence_summary, created_via, last_checked_at, error, created_at, updated_at
	`, handoff.ID, workspaceID, handoff.IssueID, handoff.SourceSessionID, handoff.SourceCommitSHA, handoff.Branch, handoff.HeadCommitSHA, commits, kind, handoff.PRURL, handoff.PRNumber, handoff.PRState, handoff.PRTitle, handoff.PreviewURL, handoff.EvidenceSummary, firstNonEmpty(handoff.CreatedVia, "server"), lastChecked, handoff.Error)
	return scanIssueHandoff(row)
}

func (s *PostgresStore) loadIssueHandoff(ctx context.Context, workspaceID, issueID, handoffID string) (IssueHandoff, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id::text, issue_id::text, source_session_id, source_commit_sha, branch, head_commit_sha, commits_json, kind, pr_url, pr_number, pr_state, pr_title, preview_url, evidence_summary, created_via, last_checked_at, error, created_at, updated_at
		FROM issue_handoffs
		WHERE workspace_id = $1 AND issue_id = $2 AND id = $3
	`, workspaceID, issueID, handoffID)
	handoff, err := scanIssueHandoff(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return IssueHandoff{}, ErrNotFound
	}
	return handoff, err
}

func (s *PostgresStore) loadCurrentIssuePullRequestHandoff(ctx context.Context, workspaceID, issueID string) (IssueHandoff, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id::text, issue_id::text, source_session_id, source_commit_sha, branch, head_commit_sha, commits_json, kind, pr_url, pr_number, pr_state, pr_title, preview_url, evidence_summary, created_via, last_checked_at, error, created_at, updated_at
		FROM issue_handoffs
		WHERE workspace_id = $1 AND issue_id = $2 AND kind = 'pr'
		ORDER BY updated_at DESC
		LIMIT 1
	`, workspaceID, issueID)
	handoff, err := scanIssueHandoff(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return IssueHandoff{}, ErrNotFound
	}
	return handoff, err
}

func (s *PostgresStore) listIssueHandoffs(ctx context.Context, workspaceID, issueID string) ([]IssueHandoff, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, issue_id::text, source_session_id, source_commit_sha, branch, head_commit_sha, commits_json, kind, pr_url, pr_number, pr_state, pr_title, preview_url, evidence_summary, created_via, last_checked_at, error, created_at, updated_at
		FROM issue_handoffs
		WHERE workspace_id = $1 AND issue_id = $2
		ORDER BY updated_at DESC, created_at DESC
	`, workspaceID, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	handoffs := []IssueHandoff{}
	for rows.Next() {
		handoff, err := scanIssueHandoff(rows)
		if err != nil {
			return nil, err
		}
		handoffs = append(handoffs, handoff)
	}
	return handoffs, rows.Err()
}

func scanIssueHandoff(row scanner) (IssueHandoff, error) {
	var handoff IssueHandoff
	var commitsBytes []byte
	var lastCheckedAt sql.NullTime
	var createdAt, updatedAt time.Time
	if err := row.Scan(&handoff.ID, &handoff.IssueID, &handoff.SourceSessionID, &handoff.SourceCommitSHA, &handoff.Branch, &handoff.HeadCommitSHA, &commitsBytes, &handoff.Kind, &handoff.PRURL, &handoff.PRNumber, &handoff.PRState, &handoff.PRTitle, &handoff.PreviewURL, &handoff.EvidenceSummary, &handoff.CreatedVia, &lastCheckedAt, &handoff.Error, &createdAt, &updatedAt); err != nil {
		return IssueHandoff{}, err
	}
	if len(commitsBytes) > 0 {
		_ = json.Unmarshal(commitsBytes, &handoff.Commits)
	}
	if handoff.Commits == nil {
		handoff.Commits = []IssueHandoffCommit{}
	}
	if lastCheckedAt.Valid {
		handoff.LastCheckedAt = lastCheckedAt.Time.UTC().Format(time.RFC3339)
	}
	handoff.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	handoff.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return handoff, nil
}

func issueHandoffPreviewURL(detail IssueDetail, sourceSessionID, sourceCommitSHA string) string {
	if detail.TestEnvironment != nil {
		if sourceCommitSHA == "" || detail.TestEnvironment.SourceCommitSHA == "" || detail.TestEnvironment.SourceCommitSHA == sourceCommitSHA || strings.HasPrefix(detail.TestEnvironment.SourceCommitSHA, sourceCommitSHA) {
			return detail.TestEnvironment.PreviewURL
		}
		if sourceSessionID != "" && detail.TestEnvironment.SourceSessionID == sourceSessionID {
			return detail.TestEnvironment.PreviewURL
		}
	}
	return ""
}

func issueHandoffEvidenceSummary(detail IssueDetail, sourceSessionID, sourceCommitSHA string) string {
	for _, review := range detail.ReviewEvidence {
		if sourceSessionID != "" && review.SourceSessionID != "" && review.SourceSessionID != sourceSessionID {
			continue
		}
		if sourceCommitSHA != "" && review.SourceCommitSHA != "" && review.SourceCommitSHA != sourceCommitSHA {
			continue
		}
		lines := []string{}
		if strings.TrimSpace(review.AgentSummary) != "" {
			lines = append(lines, "Agent summary: "+strings.TrimSpace(review.AgentSummary))
		}
		for _, command := range review.CommandsRun {
			lines = append(lines, fmt.Sprintf("- `%s`: %s", command.Command, firstNonEmpty(command.Summary, command.Status)))
		}
		if len(review.Risks) > 0 {
			lines = append(lines, "Risks: "+strings.Join(review.Risks, "; "))
		}
		if len(lines) > 0 {
			return strings.Join(lines, "\n")
		}
	}
	return "No review evidence has been captured for this handoff yet."
}

func issueHandoffComment(handoff IssueHandoff) string {
	if strings.TrimSpace(handoff.PRURL) != "" {
		state := strings.TrimSpace(handoff.PRState)
		if state == "" {
			state = "recorded"
		}
		label := handoff.PRURL
		if handoff.PRNumber > 0 {
			label = fmt.Sprintf("PR #%d", handoff.PRNumber)
		}
		return fmt.Sprintf("PR handoff [%s](%s) is %s.\n\nBranch: `%s`\nSource commit: `%s`", label, handoff.PRURL, state, firstNonEmpty(handoff.Branch, "not recorded"), shortCommitSHA(handoff.SourceCommitSHA))
	}
	return fmt.Sprintf("Branch handoff recorded for `%s`.\n\nSource commit: `%s`", firstNonEmpty(handoff.Branch, "not recorded"), shortCommitSHA(handoff.SourceCommitSHA))
}

func (s *PostgresStore) addSystemComment(ctx context.Context, workspaceID, issueID, body string) error {
	return addSystemCommentRecord(ctx, s.pool, workspaceID, issueID, body)
}

func addSystemCommentRecord(ctx context.Context, q queryer, workspaceID, issueID, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	_, err := q.Exec(ctx, `
		INSERT INTO comments (workspace_id, issue_id, author_type, author_name, body)
		VALUES ($1, $2, 'system', 'mspace', $3)
	`, workspaceID, issueID, body)
	return err
}

func (s *PostgresStore) reconcileAgentSessionRuntimeResult(ctx context.Context, q queryer, task RuntimeTask) error {
	var artifacts RuntimeTaskArtifactResult
	_ = json.Unmarshal(task.Result, &artifacts)
	session, err := runtimeTaskToAgentSession(task)
	if err != nil {
		return err
	}
	if runtimeTaskAutomation(task) == issueAnalysisAutomation {
		if task.Status == "failed" || task.Status == "cancelled" {
			return s.storeRuntimeSessionFailure(ctx, q, task, session)
		}
		return nil
	}
	if task.Status == "cancelled" {
		if err := s.markIssueTestEnvironmentInterrupted(ctx, q, task); err != nil {
			return err
		}
	}
	if task.Status == "completed" {
		if err := s.reconcileSuccessfulIssueTestEnvironment(ctx, q, task, session, artifacts); err != nil {
			return err
		}
	}
	if task.Status == "failed" {
		if err := s.reconcileFailedIssueTestEnvironment(ctx, q, task); err != nil {
			return err
		}
	}
	if artifacts.ReviewEvidence != nil {
		if err := s.storeRuntimeReviewEvidence(ctx, q, task, session, *artifacts.ReviewEvidence); err != nil {
			return err
		}
	}
	if task.Status == "completed" && artifacts.TestCaseProposals != nil {
		if err := s.storeTestCaseProposalArtifacts(ctx, q, task, *artifacts.TestCaseProposals); err != nil {
			return err
		}
	}
	if runtimeTaskAutomation(task) == testRunSetupAutomation && isFinalRuntimeTaskStatus(task.Status) {
		if err := s.reconcileTestSetupArtifact(ctx, q, task, artifacts.TestSetup); err != nil {
			return err
		}
	}
	if task.Status == "completed" && artifacts.TestResult != nil {
		if err := s.reconcileTestResultArtifact(ctx, q, task, *artifacts.TestResult); err != nil {
			return err
		}
	}
	if task.Status == "failed" || task.Status == "cancelled" {
		if err := s.storeRuntimeSessionFailure(ctx, q, task, session); err != nil {
			return err
		}
	}
	if task.Status == "completed" {
		if err := s.queueAutomaticTestDeployIfEnabled(ctx, q, task); err != nil {
			return err
		}
	}
	return nil
}

func (s *PostgresStore) queueAutomaticTestDeployIfEnabled(ctx context.Context, q queryer, task RuntimeTask) error {
	if strings.TrimSpace(task.IssueID) == "" || isIssueTestDeployTask(task) || runtimeTaskAutomation(task) == issueAnalysisAutomation || runtimeTaskIsDryRun(task) {
		return nil
	}
	source := runtimeTaskSource(task)
	if strings.TrimSpace(source.CommitSHA) == "" || strings.TrimSpace(source.Error) != "" {
		return nil
	}
	settings, err := ensureWorkspaceSettings(ctx, q, task.WorkspaceID)
	if err != nil {
		return err
	}
	if !settings.AutoDeployTestEnvironment {
		return nil
	}
	userID, err := runtimeTaskCreator(ctx, q, task.WorkspaceID, task.ID)
	if errors.Is(err, ErrNotFound) || userID == "" {
		return nil
	}
	if err != nil {
		return err
	}
	issue, err := loadIssue(ctx, q, task.WorkspaceID, task.IssueID)
	if errors.Is(err, ErrNotFound) || issue.ProjectID == "" {
		return nil
	}
	if err != nil {
		return err
	}
	project, err := resolveIssueProject(ctx, q, task.WorkspaceID, issue.ProjectID, "")
	if err != nil {
		return err
	}
	workspace, err := loadWorkspaceForUser(ctx, q, task.WorkspaceID, userID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := ensureRuntimeModeAllowedForWorkspace(ctx, q, task.WorkspaceID, firstNonEmpty(task.RuntimeMode, workspace.Kind)); err != nil {
		return err
	}
	hasActiveWorker, err := hasActiveCodexWorkerRecord(ctx, q, task.WorkspaceID, firstNonEmpty(task.RuntimeMode, workspace.Kind))
	if err != nil {
		return err
	}
	if !hasActiveWorker {
		_ = addSystemCommentRecord(ctx, q, task.WorkspaceID, task.IssueID, "Skipped automatic test deployment: no active Codex worker is connected.")
		return nil
	}
	if hasActiveQueuedOrRunningAgentSession(ctx, q, task.WorkspaceID, task.IssueID, task.ID) {
		return nil
	}
	runbook, _ := loadProjectRunbookSnapshot(ctx, q, task.WorkspaceID, project.ID)
	comments, err := listIssueCommentsForQueryer(ctx, q, task.WorkspaceID, task.IssueID)
	if err != nil {
		return err
	}
	labels, err := listIssueLabels(ctx, q, task.WorkspaceID, task.IssueID)
	if err != nil {
		return err
	}
	childIssues, err := listChildIssuesForQueryer(ctx, q, task.WorkspaceID, task.IssueID)
	if err != nil {
		return err
	}
	detail := IssueDetail{
		Issue:           issue,
		Project:         project,
		TestEnvironment: nil,
		ChildIssues:     childIssues,
		Labels:          labels,
		Comments:        comments,
		Sessions:        []AgentSession{},
		ChangeNodes:     []IssueChangeNode{runtimeTaskChangeNode(task)},
	}
	existingEnvironment, err := loadIssueTestEnvironmentRecord(ctx, q, task.WorkspaceID, task.IssueID)
	if errors.Is(err, ErrNotFound) {
		existingEnvironment = IssueTestEnvironment{}
	} else if err != nil {
		return err
	}
	if existingEnvironment.IssueID != "" {
		detail.TestEnvironment = &existingEnvironment
	}
	environment, err := s.buildIssueTestEnvironment(ctx, detail, StartTestDeployInput{})
	if err != nil {
		_ = addSystemCommentRecord(ctx, q, task.WorkspaceID, task.IssueID, fmt.Sprintf("Skipped automatic test deployment: %s", err.Error()))
		return nil
	}
	sourceNode := detail.ChangeNodes[0]
	environment.SourceSessionID = sourceNode.SessionID
	environment.SourceCommitSHA = sourceNode.CommitSHA
	sessionID, err := newAgentSessionID()
	if err != nil {
		return err
	}
	input := CreateAgentSessionInput{
		Provider:        "codex",
		AgentProfile:    "codex",
		RuntimeMode:     firstNonEmpty(task.RuntimeMode, workspace.Kind),
		Command:         buildIssueTestDeployPrompt(detail, environment, sourceNode, true),
		SourceSessionID: sourceNode.SessionID,
		SourceCommitSHA: sourceNode.CommitSHA,
		Automation:      testDeployAutomationMarker(true),
	}
	if input.RuntimeMode != workspace.Kind {
		return ErrForbidden
	}
	input.Branch = defaultAgentSessionBranch(task.IssueID, sessionID)
	payload, err := json.Marshal(buildAgentSessionPayload(sessionID, issue, project, runbook, comments, labels, childIssues, input))
	if err != nil {
		return err
	}
	capabilities, err := json.Marshal(map[string]bool{"codex": true})
	if err != nil {
		return err
	}
	queued, err := insertRuntimeTaskRecord(ctx, q, task.WorkspaceID, userID, CreateRuntimeTaskInput{
		IssueID:              issue.ID,
		SessionID:            sessionID,
		ProjectID:            project.ID,
		Kind:                 "agent_session",
		Priority:             0,
		RuntimeMode:          input.RuntimeMode,
		RequiredCapabilities: capabilities,
		Payload:              payload,
	})
	if err != nil {
		return err
	}
	environment.LastDeploySessionID = sessionID
	environment.NamespaceStatus = "deploying"
	if err := saveIssueTestEnvironmentRecord(ctx, q, task.WorkspaceID, environment); err != nil {
		return err
	}
	_ = addSystemCommentRecord(ctx, q, task.WorkspaceID, task.IssueID, fmt.Sprintf(
		"Auto-queued test deployment session `%s` for namespace `%s`.\n\nSource commit: `%s`\nCluster: `%s`\nRegistry: `%s`\nExposure: %s",
		shortIdentifier(sessionID),
		environment.Namespace,
		shortCommitSHA(sourceNode.CommitSHA),
		environment.ClusterID,
		environment.ImageRegistryPrefix,
		previewStrategyLabel(environment),
	))
	_ = insertRuntimeTaskEvent(ctx, q, queued.WorkspaceID, queued.ID, "", userID, "auto_deploy_queued", map[string]any{
		"sourceTaskId":    task.ID,
		"sourceSessionId": firstNonEmpty(task.SessionID, task.ID),
		"sourceCommitSha": sourceNode.CommitSHA,
		"testEnvironment": environment.Namespace,
	})
	return nil
}

func runtimeTaskCreator(ctx context.Context, q queryer, workspaceID, taskID string) (string, error) {
	var userID string
	err := q.QueryRow(ctx, `
		SELECT COALESCE(created_by_user_id::text, '')
		FROM runtime_tasks
		WHERE workspace_id = $1 AND id = $2
	`, strings.TrimSpace(workspaceID), strings.TrimSpace(taskID)).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(userID), nil
}

func hasActiveQueuedOrRunningAgentSession(ctx context.Context, q queryer, workspaceID, issueID, excludedTaskID string) bool {
	var exists bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM runtime_tasks
			WHERE workspace_id = $1
				AND issue_id = $2
				AND kind = 'agent_session'
				AND ($3 = '' OR id::text <> $3)
				AND status IN ('queued', 'claimed', 'running')
			LIMIT 1
		)
	`, strings.TrimSpace(workspaceID), strings.TrimSpace(issueID), strings.TrimSpace(excludedTaskID)).Scan(&exists)
	return err == nil && exists
}

func (s *PostgresStore) reconcileSuccessfulIssueTestEnvironment(ctx context.Context, q queryer, task RuntimeTask, session AgentSession, artifacts RuntimeTaskArtifactResult) error {
	if strings.TrimSpace(task.IssueID) == "" {
		return nil
	}
	environment, err := loadIssueTestEnvironmentRecord(ctx, q, task.WorkspaceID, task.IssueID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	previewURL := ""
	if artifacts.TestEnvironment != nil {
		previewURL = firstNonEmpty(artifacts.TestEnvironment.PreviewURL, artifacts.TestEnvironment.PreviewURLSnake, artifacts.TestEnvironment.URL)
	}
	changed := false
	switch {
	case environment.LastDeploySessionID == session.ID:
		environment.NamespaceStatus = "active"
		environment.CleanupStatus = "retained"
		if previewURL != "" {
			environment.PreviewURL = previewURL
		}
		changed = true
		if err := updateIssueStatusInternal(ctx, q, task.WorkspaceID, task.IssueID, "ready_for_test"); err != nil {
			return err
		}
	case environment.LastCleanupSessionID == session.ID:
		environment.NamespaceStatus = "cleaned"
		environment.CleanupStatus = "cleaned"
		changed = true
	case previewURL != "" && environment.LastCleanupSessionID != session.ID:
		environment.PreviewURL = previewURL
		environment.NamespaceStatus = "active"
		environment.CleanupStatus = "retained"
		environment.LastDeploySessionID = session.ID
		environment.SourceSessionID = firstNonEmpty(session.SourceSessionID, environment.SourceSessionID)
		environment.SourceCommitSHA = firstNonEmpty(session.SourceCommitSHA, environment.SourceCommitSHA)
		changed = true
		if err := updateIssueStatusInternal(ctx, q, task.WorkspaceID, task.IssueID, "ready_for_test"); err != nil {
			return err
		}
	}
	if !changed {
		return nil
	}
	return saveIssueTestEnvironmentRecord(ctx, q, task.WorkspaceID, environment)
}

func (s *PostgresStore) reconcileFailedIssueTestEnvironment(ctx context.Context, q queryer, task RuntimeTask) error {
	environment, err := loadIssueTestEnvironmentRecord(ctx, q, task.WorkspaceID, task.IssueID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	changed := false
	switch {
	case environment.LastDeploySessionID == firstNonEmpty(task.SessionID, task.ID):
		environment.NamespaceStatus = "deploy_failed"
		changed = true
	case environment.LastCleanupSessionID == firstNonEmpty(task.SessionID, task.ID):
		environment.NamespaceStatus = "cleanup_failed"
		environment.CleanupStatus = "cleanup_failed"
		changed = true
	}
	if !changed {
		return nil
	}
	if err := saveIssueTestEnvironmentRecord(ctx, q, task.WorkspaceID, environment); err != nil {
		return err
	}
	return updateIssueStatusInternal(ctx, q, task.WorkspaceID, task.IssueID, "blocked")
}

func (s *PostgresStore) markIssueTestEnvironmentInterrupted(ctx context.Context, q queryer, task RuntimeTask) error {
	environment, err := loadIssueTestEnvironmentRecord(ctx, q, task.WorkspaceID, task.IssueID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	sessionID := firstNonEmpty(task.SessionID, task.ID)
	changed := false
	switch {
	case environment.LastDeploySessionID == sessionID:
		environment.NamespaceStatus = "deploy_interrupted"
		changed = true
	case environment.LastCleanupSessionID == sessionID:
		environment.NamespaceStatus = "cleanup_failed"
		environment.CleanupStatus = "cleanup_failed"
		changed = true
	}
	if !changed {
		return nil
	}
	if err := saveIssueTestEnvironmentRecord(ctx, q, task.WorkspaceID, environment); err != nil {
		return err
	}
	return updateIssueStatusInternal(ctx, q, task.WorkspaceID, task.IssueID, "blocked")
}

func loadIssueTestEnvironmentRecord(ctx context.Context, q queryer, workspaceID, issueID string) (IssueTestEnvironment, error) {
	row := q.QueryRow(ctx, `
		SELECT issue_id::text, COALESCE(cluster_id::text, ''), environment_id, environment_kind, environment_snapshot, namespace, namespace_status, cleanup_status, preview_url, image_registry_prefix, kubeconfig_path, kube_context, exposure_mode, preview_domain, ingress_class, node_host, last_deploy_session_id, last_cleanup_session_id, source_session_id, source_commit_sha, created_at, updated_at
		FROM issue_test_environments
		WHERE workspace_id = $1 AND issue_id = $2
	`, workspaceID, issueID)
	environment, err := scanIssueTestEnvironment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return IssueTestEnvironment{}, ErrNotFound
	}
	return environment, err
}

func saveIssueTestEnvironmentRecord(ctx context.Context, q queryer, workspaceID string, environment IssueTestEnvironment) error {
	environment.EnvironmentID = firstNonEmpty(environment.EnvironmentID, environment.ClusterID)
	environment.EnvironmentKind = firstNonEmpty(environment.EnvironmentKind, environmentKindKubernetes)
	if len(environment.EnvironmentSnapshot) == 0 {
		environment.EnvironmentSnapshot = json.RawMessage(`{}`)
	}
	_, err := q.Exec(ctx, `
		INSERT INTO issue_test_environments (workspace_id, issue_id, cluster_id, environment_id, environment_kind, environment_snapshot, namespace, namespace_status, cleanup_status, preview_url, image_registry_prefix, kubeconfig_path, kube_context, exposure_mode, preview_domain, ingress_class, node_host, last_deploy_session_id, last_cleanup_session_id, source_session_id, source_commit_sha)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6::jsonb, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
		ON CONFLICT (issue_id) DO UPDATE SET
			workspace_id = EXCLUDED.workspace_id,
			cluster_id = EXCLUDED.cluster_id,
			environment_id = EXCLUDED.environment_id,
			environment_kind = EXCLUDED.environment_kind,
			environment_snapshot = EXCLUDED.environment_snapshot,
			namespace = EXCLUDED.namespace,
			namespace_status = EXCLUDED.namespace_status,
			cleanup_status = EXCLUDED.cleanup_status,
			preview_url = EXCLUDED.preview_url,
			image_registry_prefix = EXCLUDED.image_registry_prefix,
			kubeconfig_path = EXCLUDED.kubeconfig_path,
			kube_context = EXCLUDED.kube_context,
			exposure_mode = EXCLUDED.exposure_mode,
			preview_domain = EXCLUDED.preview_domain,
			ingress_class = EXCLUDED.ingress_class,
			node_host = EXCLUDED.node_host,
			last_deploy_session_id = EXCLUDED.last_deploy_session_id,
			last_cleanup_session_id = EXCLUDED.last_cleanup_session_id,
			source_session_id = EXCLUDED.source_session_id,
			source_commit_sha = EXCLUDED.source_commit_sha,
			updated_at = now()
	`, workspaceID, environment.IssueID, environment.ClusterID, environment.EnvironmentID, environment.EnvironmentKind, jsonOrObject(environment.EnvironmentSnapshot), environment.Namespace, environment.NamespaceStatus, environment.CleanupStatus, environment.PreviewURL, environment.ImageRegistryPrefix, environment.KubeconfigPath, environment.KubeContext, environment.ExposureMode, environment.PreviewDomain, environment.IngressClass, environment.NodeHost, environment.LastDeploySessionID, environment.LastCleanupSessionID, environment.SourceSessionID, environment.SourceCommitSHA)
	return err
}

func updateIssueStatusInternal(ctx context.Context, q queryer, workspaceID, issueID, status string) error {
	status = normalizeIssueStatus(status)
	if err := validateIssueStatus(status); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `
		UPDATE issues
		SET status = $3, updated_at = now()
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, issueID, status)
	return err
}

func (s *PostgresStore) storeRuntimeReviewEvidence(ctx context.Context, q queryer, task RuntimeTask, session AgentSession, artifact SessionReviewEvidenceArtifact) error {
	review := buildRuntimeReviewEvidence(task, session, artifact)
	commandsJSON, err := json.Marshal(review.CommandsRun)
	if err != nil {
		return err
	}
	testsJSON, err := json.Marshal(review.Tests)
	if err != nil {
		return err
	}
	buildJSON, err := json.Marshal(review.BuildResult)
	if err != nil {
		return err
	}
	deploymentJSON, err := json.Marshal(review.DeploymentResult)
	if err != nil {
		return err
	}
	risksJSON, err := json.Marshal(review.Risks)
	if err != nil {
		return err
	}
	followUpsJSON, err := json.Marshal(review.FollowUps)
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `
		UPDATE session_review_evidence
		SET
			issue_id = $2,
			source_session_id = $4,
			source_commit_sha = $5,
			branch = $6,
			agent_summary = $7,
			commands_json = $8,
			tests_json = $9,
			build_result_json = $10,
			deployment_result_json = $11,
			risks_json = $12,
			follow_ups_json = $13,
			preview_url = $14,
			cluster = $15,
			namespace = $16,
			namespace_status = $17,
			cleanup_status = $18,
			updated_at = now()
		WHERE workspace_id = $1 AND session_id = $3
	`, task.WorkspaceID, review.IssueID, review.SessionID, review.SourceSessionID, review.SourceCommitSHA, review.Branch, review.AgentSummary, commandsJSON, testsJSON, buildJSON, deploymentJSON, risksJSON, followUpsJSON, review.PreviewURL, review.Cluster, review.Namespace, review.NamespaceStatus, review.CleanupStatus)
	if err != nil || tag.RowsAffected() > 0 {
		return err
	}
	_, err = q.Exec(ctx, `
		INSERT INTO session_review_evidence (workspace_id, issue_id, session_id, source_session_id, source_commit_sha, branch, agent_summary, commands_json, tests_json, build_result_json, deployment_result_json, risks_json, follow_ups_json, preview_url, cluster, namespace, namespace_status, cleanup_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`, task.WorkspaceID, review.IssueID, review.SessionID, review.SourceSessionID, review.SourceCommitSHA, review.Branch, review.AgentSummary, commandsJSON, testsJSON, buildJSON, deploymentJSON, risksJSON, followUpsJSON, review.PreviewURL, review.Cluster, review.Namespace, review.NamespaceStatus, review.CleanupStatus)
	return err
}

func buildRuntimeReviewEvidence(task RuntimeTask, session AgentSession, artifact SessionReviewEvidenceArtifact) SessionReviewEvidence {
	review := SessionReviewEvidence{
		IssueID:          task.IssueID,
		SessionID:        session.ID,
		SourceSessionID:  firstNonEmpty(session.SourceSessionID, session.ID),
		SourceCommitSHA:  session.SourceCommitSHA,
		Branch:           session.Branch,
		AgentSummary:     strings.TrimSpace(artifact.AgentSummary),
		CommandsRun:      normalizeRuntimeReviewCommands(artifact.CommandsRun),
		Tests:            normalizeRuntimeReviewChecks(artifact.Tests),
		BuildResult:      normalizeRuntimeReviewResult(artifact.BuildResult),
		DeploymentResult: normalizeRuntimeReviewResult(artifact.DeploymentResult),
		Risks:            normalizeRuntimeStringList(artifact.Risks),
		FollowUps:        normalizeRuntimeStringList(artifact.FollowUps),
	}
	if environment, ok := runtimeTaskResultEnvironment(task); ok {
		review.PreviewURL = firstNonEmpty(environment.PreviewURL, environment.PreviewURLSnake, environment.URL)
	}
	return review
}

func (s *PostgresStore) storeRuntimeSessionFailure(ctx context.Context, q queryer, task RuntimeTask, session AgentSession) error {
	if strings.TrimSpace(task.IssueID) == "" {
		return nil
	}
	failure := buildRuntimeSessionFailure(task, session)
	tag, err := q.Exec(ctx, `
		UPDATE session_failures
		SET
			issue_id = $2,
			phase = $4,
			status = $5,
			failed_command = $6,
			error_summary = $7,
			error_excerpt = $8,
			updated_at = now()
		WHERE workspace_id = $1 AND session_id = $3
	`, task.WorkspaceID, failure.IssueID, failure.SessionID, failure.Phase, failure.Status, failure.FailedCommand, failure.ErrorSummary, failure.ErrorExcerpt)
	if err != nil || tag.RowsAffected() > 0 {
		return err
	}
	_, err = q.Exec(ctx, `
		INSERT INTO session_failures (workspace_id, issue_id, session_id, phase, status, failed_command, error_summary, error_excerpt, cluster, namespace, resource_kind, resource_name, evidence_id, review_evidence_id, retry_session_id, continued_session_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '', '', '', '', '', '', '', '')
	`, task.WorkspaceID, failure.IssueID, failure.SessionID, failure.Phase, failure.Status, failure.FailedCommand, failure.ErrorSummary, failure.ErrorExcerpt)
	return err
}

func buildRuntimeSessionFailure(task RuntimeTask, session AgentSession) SessionFailure {
	errorText := strings.TrimSpace(task.Error)
	if errorText == "" {
		errorText = "Runtime task did not finish successfully."
	}
	return SessionFailure{
		IssueID:       task.IssueID,
		SessionID:     session.ID,
		Phase:         runtimeFailurePhase(task),
		Status:        runtimeFailureStatus(task),
		FailedCommand: truncateString(session.Command, 1000),
		ErrorSummary:  truncateString(errorText, 600),
		ErrorExcerpt:  truncateString(errorText, 2000),
	}
}

func runtimeTaskResultEnvironment(task RuntimeTask) (RuntimeTaskTestEnvironmentArtifact, bool) {
	var artifacts RuntimeTaskArtifactResult
	if err := json.Unmarshal(task.Result, &artifacts); err != nil || artifacts.TestEnvironment == nil {
		return RuntimeTaskTestEnvironmentArtifact{}, false
	}
	return *artifacts.TestEnvironment, true
}

func runtimeFailurePhase(task RuntimeTask) string {
	if task.Status == "cancelled" {
		return "agent_interrupted"
	}
	lower := strings.ToLower(task.Error)
	switch {
	case strings.Contains(lower, "cleanup"):
		return "cleanup"
	case strings.Contains(lower, "build"):
		return "build"
	case strings.Contains(lower, "test") || strings.Contains(lower, "typecheck") || strings.Contains(lower, "lint"):
		return "test"
	case strings.Contains(lower, "push") || strings.Contains(lower, "registry"):
		return "image_push"
	case strings.Contains(lower, "preview") || strings.Contains(lower, "probe") || strings.Contains(lower, "http"):
		return "preview_probe"
	default:
		return "unknown"
	}
}

func runtimeFailureStatus(task RuntimeTask) string {
	if task.Status == "cancelled" {
		return "stopped"
	}
	return "open"
}

func normalizeRuntimeReviewCommands(commands []ReviewEvidenceCommand) []ReviewEvidenceCommand {
	result := make([]ReviewEvidenceCommand, 0, len(commands))
	for _, command := range commands {
		command.Command = strings.TrimSpace(command.Command)
		if command.Command == "" {
			continue
		}
		command.Status = normalizeRuntimeReviewStatus(command.Status)
		command.Category = strings.TrimSpace(command.Category)
		command.Summary = truncateString(strings.TrimSpace(command.Summary), 600)
		result = append(result, command)
	}
	return result
}

func normalizeRuntimeReviewChecks(checks []ReviewEvidenceCheck) []ReviewEvidenceCheck {
	result := make([]ReviewEvidenceCheck, 0, len(checks))
	for _, check := range checks {
		check.Name = strings.TrimSpace(check.Name)
		if check.Name == "" {
			continue
		}
		check.Status = normalizeRuntimeReviewStatus(check.Status)
		check.Summary = truncateString(strings.TrimSpace(check.Summary), 600)
		result = append(result, check)
	}
	return result
}

func normalizeRuntimeReviewResult(result ReviewEvidenceResult) ReviewEvidenceResult {
	result.Status = normalizeRuntimeReviewStatus(result.Status)
	result.Summary = strings.TrimSpace(result.Summary)
	result.Details = truncateString(strings.TrimSpace(result.Details), 1200)
	return result
}

func normalizeRuntimeReviewStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "success", "succeeded", "ok", "pass":
		return "passed"
	case "failure", "error", "fail":
		return "failed"
	default:
		return status
	}
}

func normalizeRuntimeStringList(values []string) []string {
	result := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func truncateString(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 1 {
		return string(runes[:limit])
	}
	return string(runes[:limit-1]) + "…"
}
