package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) GetSkill(ctx Context, userID, workspaceID, skillID string) (SkillDetail, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, strings.TrimSpace(userID), "owner", "admin"); err != nil {
		return SkillDetail{}, err
	}
	return s.skillDetail(dbctx, workspaceID, skillID)
}

func (s *PostgresStore) CreateSkill(ctx Context, userID, workspaceID string, input SkillInput) (SkillDetail, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	userID = strings.TrimSpace(userID)
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, userID, "owner", "admin"); err != nil {
		return SkillDetail{}, err
	}
	normalized, files, err := normalizeWorkspaceSkillCreateInput(input)
	if err != nil {
		return SkillDetail{}, err
	}
	if builtinSkillExists(normalized.Slug) {
		return SkillDetail{}, fmt.Errorf("skill slug conflicts with built-in skill: %s", normalized.Slug)
	}
	exists, err := s.workspaceSkillIdentifierExists(dbctx, workspaceID, normalized.Slug)
	if err != nil {
		return SkillDetail{}, err
	}
	if exists {
		return SkillDetail{}, ErrConflict
	}
	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return SkillDetail{}, err
	}
	defer tx.Rollback(dbctx)
	detail, err := s.createWorkspaceSkill(dbctx, tx, workspaceID, userID, normalized, files)
	if err != nil {
		return SkillDetail{}, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return SkillDetail{}, err
	}
	return detail, nil
}

func (s *PostgresStore) UpdateSkill(ctx Context, userID, workspaceID, skillID string, input SkillInput) (SkillDetail, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	userID = strings.TrimSpace(userID)
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, userID, "owner", "admin"); err != nil {
		return SkillDetail{}, err
	}
	if slug, ok := slugFromBuiltinSkillID(skillID); ok || builtinSkillExists(strings.TrimSpace(skillID)) {
		if !ok {
			slug = normalizeSkillSlug(skillID)
		}
		return s.updateBuiltinSkillSetting(dbctx, workspaceID, slug, input)
	}
	existing, err := s.skillDetail(dbctx, workspaceID, skillID)
	if err != nil {
		return SkillDetail{}, err
	}
	normalized, files, hasFiles, err := normalizeWorkspaceSkillUpdateInput(existing, input)
	if err != nil {
		return SkillDetail{}, err
	}
	tx, err := s.pool.Begin(dbctx)
	if err != nil {
		return SkillDetail{}, err
	}
	defer tx.Rollback(dbctx)
	var currentRevisionID string
	if hasFiles {
		revision, err := s.createWorkspaceSkillRevision(dbctx, tx, workspaceID, existing.ID, userID, files)
		if err != nil {
			return SkillDetail{}, err
		}
		currentRevisionID = revision.ID
	} else {
		currentRevisionID = existing.SkillCatalogItem.Revision
	}
	if hasFiles {
		tag, execErr := tx.Exec(dbctx, `
			UPDATE workspace_skills
			SET name = $3,
				description = $4,
				enabled = $5,
				invocable = $6,
				current_revision_id = $7::uuid,
				updated_at = now()
			WHERE workspace_id = $1
				AND id = $2::uuid
				AND deleted_at IS NULL
				AND source_type = 'custom'
		`, workspaceID, existing.ID, normalized.Name, normalized.Description, skillBoolValue(normalized.Enabled, existing.Enabled), skillBoolValue(normalized.Invocable, existing.Invocable), currentRevisionID)
		err = execErr
		if err == nil && tag.RowsAffected() == 0 {
			err = ErrNotFound
		}
	} else {
		tag, execErr := tx.Exec(dbctx, `
			UPDATE workspace_skills
			SET name = $3,
				description = $4,
				enabled = $5,
				invocable = $6,
				updated_at = now()
			WHERE workspace_id = $1
				AND id = $2::uuid
				AND deleted_at IS NULL
				AND source_type = 'custom'
		`, workspaceID, existing.ID, normalized.Name, normalized.Description, skillBoolValue(normalized.Enabled, existing.Enabled), skillBoolValue(normalized.Invocable, existing.Invocable))
		err = execErr
		if err == nil && tag.RowsAffected() == 0 {
			err = ErrNotFound
		}
	}
	if err != nil {
		return SkillDetail{}, err
	}
	if err := tx.Commit(dbctx); err != nil {
		return SkillDetail{}, err
	}
	return s.skillDetail(dbctx, workspaceID, existing.ID)
}

func (s *PostgresStore) DeleteSkill(ctx Context, userID, workspaceID, skillID string) error {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	userID = strings.TrimSpace(userID)
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, userID, "owner", "admin"); err != nil {
		return err
	}
	if _, ok := slugFromBuiltinSkillID(skillID); ok || builtinSkillExists(strings.TrimSpace(skillID)) {
		return ErrForbidden
	}
	skill, _, err := s.workspaceSkillByIdentifier(dbctx, workspaceID, skillID)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(dbctx, `
		UPDATE workspace_skills
		SET deleted_at = now(),
			enabled = false,
			invocable = false,
			updated_at = now()
		WHERE workspace_id = $1
			AND id = $2::uuid
			AND deleted_at IS NULL
			AND source_type = 'custom'
	`, workspaceID, skill.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) DuplicateSkill(ctx Context, userID, workspaceID, skillID string, input DuplicateSkillInput) (SkillDetail, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	userID = strings.TrimSpace(userID)
	if err := ensureWorkspaceRole(dbctx, s.pool, workspaceID, userID, "owner", "admin"); err != nil {
		return SkillDetail{}, err
	}
	source, err := s.skillDetail(dbctx, workspaceID, skillID)
	if err != nil {
		return SkillDetail{}, err
	}
	slug := normalizeSkillSlug(input.Slug)
	if slug == "" {
		slug = s.nextWorkspaceSkillCopySlug(dbctx, workspaceID, source.Slug)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = strings.TrimSpace(source.Name)
		if name == "" {
			name = source.Slug
		}
		name += " copy"
	}
	description := strings.TrimSpace(input.Description)
	if description == "" {
		description = source.Description
	}
	return s.CreateSkill(ctx, userID, workspaceID, SkillInput{
		Slug:        slug,
		Name:        name,
		Description: description,
		Enabled:     boolPointer(skillBoolValue(input.Enabled, true)),
		Invocable:   boolPointer(skillBoolValue(input.Invocable, true)),
		Files:       source.Files,
	})
}

func (s *PostgresStore) resolveAgentSessionSkillBundles(ctx context.Context, workspaceID string, input *CreateAgentSessionInput) error {
	return resolveAgentSessionSkillBundles(input, func(slug string) (AgentSessionSkillReference, RuntimeSkillBundle, error) {
		return s.resolveWorkspaceSkillBundle(ctx, workspaceID, slug)
	})
}

func (s *PostgresStore) listSkills(ctx context.Context, workspaceID string) ([]SkillCatalogItem, error) {
	builtins, err := listBuiltinSkills()
	if err != nil {
		return nil, err
	}
	settings, err := s.listBuiltinSkillSettings(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	items := make([]SkillCatalogItem, 0, len(builtins))
	for _, builtin := range builtins {
		bundle, err := builtinSkillBundle(builtin.Slug, builtin.Revision)
		if err != nil {
			return nil, err
		}
		items = append(items, skillCatalogItemFromBundle(bundle, settings[builtin.Slug]))
	}
	rows, err := s.pool.Query(ctx, `
		SELECT
			ws.id::text,
			ws.workspace_id::text,
			ws.slug,
			ws.name,
			ws.description,
			ws.source_type,
			ws.enabled,
			ws.invocable,
			COALESCE(ws.current_revision_id::text, ''),
			COALESCE(ws.created_by::text, ''),
			ws.deleted_at,
			ws.created_at,
			ws.updated_at,
			wr.id::text,
			wr.workspace_id::text,
			wr.skill_id::text,
			wr.revision,
			wr.content_hash,
			wr.files_json::text,
			COALESCE(wr.created_by::text, ''),
			wr.created_at
		FROM workspace_skills ws
		JOIN workspace_skill_revisions wr ON wr.id = ws.current_revision_id
		WHERE ws.workspace_id = $1
			AND ws.deleted_at IS NULL
		ORDER BY ws.updated_at DESC, ws.created_at DESC
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		skill, revision, err := scanWorkspaceSkillWithRevision(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, skillCatalogItemFromWorkspaceSkill(skill, revision))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].BuiltIn != items[j].BuiltIn {
			return items[i].BuiltIn
		}
		return items[i].Slug < items[j].Slug
	})
	return items, nil
}

func (s *PostgresStore) skillDetail(ctx context.Context, workspaceID, identifier string) (SkillDetail, error) {
	identifier = strings.TrimSpace(identifier)
	if slug, ok := slugFromBuiltinSkillID(identifier); ok || builtinSkillExists(identifier) {
		if !ok {
			slug = normalizeSkillSlug(identifier)
		}
		bundle, err := builtinSkillBundle(slug, "latest")
		if err != nil {
			return SkillDetail{}, err
		}
		setting, err := s.getBuiltinSkillSetting(ctx, workspaceID, slug)
		if err != nil {
			return SkillDetail{}, err
		}
		return skillDetailFromBundle(bundle, setting), nil
	}
	skill, revision, err := s.workspaceSkillByIdentifier(ctx, workspaceID, identifier)
	if err != nil {
		return SkillDetail{}, err
	}
	return skillDetailFromWorkspaceSkill(skill, revision), nil
}

func (s *PostgresStore) resolveWorkspaceSkillBundle(ctx context.Context, workspaceID, slug string) (AgentSessionSkillReference, RuntimeSkillBundle, error) {
	slug = normalizeSkillSlug(slug)
	if slug == "" {
		return AgentSessionSkillReference{}, RuntimeSkillBundle{}, errors.New("skill slug is required")
	}
	skill, revision, err := s.workspaceSkillByIdentifier(ctx, workspaceID, slug)
	if err == nil {
		if !skill.Enabled || !skill.Invocable {
			return AgentSessionSkillReference{}, RuntimeSkillBundle{}, fmt.Errorf("skill slug is disabled: %s", slug)
		}
		bundle := runtimeSkillBundleFromWorkspaceSkill(skill, revision)
		return skillReferenceFromBundle(bundle), bundle, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return AgentSessionSkillReference{}, RuntimeSkillBundle{}, err
	}
	bundle, err := builtinSkillBundle(slug, "latest")
	if err != nil {
		return AgentSessionSkillReference{}, RuntimeSkillBundle{}, err
	}
	setting, err := s.getBuiltinSkillSetting(ctx, workspaceID, slug)
	if err != nil {
		return AgentSessionSkillReference{}, RuntimeSkillBundle{}, err
	}
	if !setting.Enabled || !setting.Invocable {
		return AgentSessionSkillReference{}, RuntimeSkillBundle{}, fmt.Errorf("skill slug is disabled: %s", slug)
	}
	return skillReferenceFromBundle(bundle), bundle, nil
}

func (s *PostgresStore) createWorkspaceSkill(ctx context.Context, q queryer, workspaceID, userID string, input SkillInput, files []RuntimeSkillFile) (SkillDetail, error) {
	var skill WorkspaceSkill
	var createdAt, updatedAt time.Time
	err := q.QueryRow(ctx, `
		INSERT INTO workspace_skills (
			workspace_id,
			slug,
			name,
			description,
			source_type,
			enabled,
			invocable,
			created_by
		)
		VALUES ($1, $2, $3, $4, 'custom', $5, $6, NULLIF($7, '')::uuid)
		RETURNING
			id::text,
			workspace_id::text,
			slug,
			name,
			description,
			source_type,
			enabled,
			invocable,
			COALESCE(current_revision_id::text, ''),
			COALESCE(created_by::text, ''),
			deleted_at,
			created_at,
			updated_at
	`, workspaceID, input.Slug, input.Name, input.Description, skillBoolValue(input.Enabled, true), skillBoolValue(input.Invocable, true), userID).Scan(
		&skill.ID,
		&skill.WorkspaceID,
		&skill.Slug,
		&skill.Name,
		&skill.Description,
		&skill.SourceType,
		&skill.Enabled,
		&skill.Invocable,
		&skill.CurrentRevisionID,
		&skill.CreatedBy,
		&sql.NullTime{},
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return SkillDetail{}, ErrConflict
		}
		return SkillDetail{}, err
	}
	skill.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	skill.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	revision, err := s.createWorkspaceSkillRevision(ctx, q, workspaceID, skill.ID, userID, files)
	if err != nil {
		return SkillDetail{}, err
	}
	_, err = q.Exec(ctx, `
		UPDATE workspace_skills
		SET current_revision_id = $3::uuid,
			updated_at = now()
		WHERE workspace_id = $1
			AND id = $2::uuid
	`, workspaceID, skill.ID, revision.ID)
	if err != nil {
		return SkillDetail{}, err
	}
	skill.CurrentRevisionID = revision.ID
	skill.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return skillDetailFromWorkspaceSkill(skill, revision), nil
}

func (s *PostgresStore) createWorkspaceSkillRevision(ctx context.Context, q queryer, workspaceID, skillID, userID string, files []RuntimeSkillFile) (WorkspaceSkillRevision, error) {
	revisionValue, contentHash := workspaceSkillRevisionFromFiles(files)
	filesJSON, err := json.Marshal(files)
	if err != nil {
		return WorkspaceSkillRevision{}, err
	}
	row := q.QueryRow(ctx, `
		INSERT INTO workspace_skill_revisions (
			workspace_id,
			skill_id,
			revision,
			content_hash,
			files_json,
			created_by
		)
		VALUES ($1, $2::uuid, $3, $4, $5::jsonb, NULLIF($6, '')::uuid)
		RETURNING
			id::text,
			workspace_id::text,
			skill_id::text,
			revision,
			content_hash,
			files_json::text,
			COALESCE(created_by::text, ''),
			created_at
	`, workspaceID, skillID, revisionValue, contentHash, string(filesJSON), userID)
	return scanWorkspaceSkillRevision(row)
}

func (s *PostgresStore) updateBuiltinSkillSetting(ctx context.Context, workspaceID, slug string, input SkillInput) (SkillDetail, error) {
	bundle, err := builtinSkillBundle(slug, "latest")
	if err != nil {
		return SkillDetail{}, err
	}
	if input.Files != nil || strings.TrimSpace(input.Name) != "" || strings.TrimSpace(input.Description) != "" {
		return SkillDetail{}, errors.New("built-in skill content cannot be edited")
	}
	existing, err := s.getBuiltinSkillSetting(ctx, workspaceID, slug)
	if err != nil {
		return SkillDetail{}, err
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO workspace_builtin_skill_settings (workspace_id, slug, enabled, invocable)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (workspace_id, slug) DO UPDATE
		SET enabled = EXCLUDED.enabled,
			invocable = EXCLUDED.invocable,
			updated_at = now()
		RETURNING workspace_id::text, slug, enabled, invocable, created_at, updated_at
	`, workspaceID, normalizeSkillSlug(slug), skillBoolValue(input.Enabled, existing.Enabled), skillBoolValue(input.Invocable, existing.Invocable))
	setting, err := scanBuiltinSkillSetting(row)
	if err != nil {
		return SkillDetail{}, err
	}
	return skillDetailFromBundle(bundle, setting), nil
}

func (s *PostgresStore) listBuiltinSkillSettings(ctx context.Context, workspaceID string) (map[string]WorkspaceBuiltinSkillSetting, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT workspace_id::text, slug, enabled, invocable, created_at, updated_at
		FROM workspace_builtin_skill_settings
		WHERE workspace_id = $1
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	settings := map[string]WorkspaceBuiltinSkillSetting{}
	for rows.Next() {
		setting, err := scanBuiltinSkillSetting(rows)
		if err != nil {
			return nil, err
		}
		settings[setting.Slug] = setting
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return settings, nil
}

func (s *PostgresStore) getBuiltinSkillSetting(ctx context.Context, workspaceID, slug string) (WorkspaceBuiltinSkillSetting, error) {
	slug = normalizeSkillSlug(slug)
	row := s.pool.QueryRow(ctx, `
		SELECT workspace_id::text, slug, enabled, invocable, created_at, updated_at
		FROM workspace_builtin_skill_settings
		WHERE workspace_id = $1 AND slug = $2
	`, workspaceID, slug)
	setting, err := scanBuiltinSkillSetting(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkspaceBuiltinSkillSetting{WorkspaceID: workspaceID, Slug: slug, Enabled: true, Invocable: true}, nil
	}
	return setting, err
}

func (s *PostgresStore) workspaceSkillByIdentifier(ctx context.Context, workspaceID, identifier string) (WorkspaceSkill, WorkspaceSkillRevision, error) {
	identifier = strings.TrimSpace(identifier)
	rows, err := s.pool.Query(ctx, `
		SELECT
			ws.id::text,
			ws.workspace_id::text,
			ws.slug,
			ws.name,
			ws.description,
			ws.source_type,
			ws.enabled,
			ws.invocable,
			COALESCE(ws.current_revision_id::text, ''),
			COALESCE(ws.created_by::text, ''),
			ws.deleted_at,
			ws.created_at,
			ws.updated_at,
			wr.id::text,
			wr.workspace_id::text,
			wr.skill_id::text,
			wr.revision,
			wr.content_hash,
			wr.files_json::text,
			COALESCE(wr.created_by::text, ''),
			wr.created_at
		FROM workspace_skills ws
		JOIN workspace_skill_revisions wr ON wr.id = ws.current_revision_id
		WHERE ws.workspace_id = $1
			AND ws.deleted_at IS NULL
			AND (ws.id::text = $2 OR ws.slug = $3)
		ORDER BY CASE WHEN ws.id::text = $2 THEN 0 ELSE 1 END
		LIMIT 1
	`, workspaceID, identifier, normalizeSkillSlug(identifier))
	if err != nil {
		return WorkspaceSkill{}, WorkspaceSkillRevision{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return WorkspaceSkill{}, WorkspaceSkillRevision{}, ErrNotFound
	}
	skill, revision, err := scanWorkspaceSkillWithRevision(rows)
	if err != nil {
		return WorkspaceSkill{}, WorkspaceSkillRevision{}, err
	}
	return skill, revision, rows.Err()
}

func (s *PostgresStore) workspaceSkillSlugExists(ctx context.Context, workspaceID, slug string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM workspace_skills
			WHERE workspace_id = $1
				AND slug = $2
				AND deleted_at IS NULL
		)
	`, workspaceID, normalizeSkillSlug(slug)).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (s *PostgresStore) workspaceSkillIdentifierExists(ctx context.Context, workspaceID, identifier string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM workspace_skills
			WHERE workspace_id = $1
				AND deleted_at IS NULL
				AND (slug = $2 OR id::text = $2)
		)
	`, workspaceID, normalizeSkillSlug(identifier)).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (s *PostgresStore) nextWorkspaceSkillCopySlug(ctx context.Context, workspaceID, base string) string {
	base = normalizeSkillSlug(base)
	if base == "" {
		base = "skill"
	}
	for index := 1; ; index++ {
		candidate := base + "-copy"
		if index > 1 {
			candidate = fmt.Sprintf("%s-copy-%d", base, index)
		}
		if builtinSkillExists(candidate) {
			continue
		}
		exists, err := s.workspaceSkillSlugExists(ctx, workspaceID, candidate)
		if err != nil || !exists {
			return candidate
		}
	}
}

func scanWorkspaceSkillWithRevision(row scanner) (WorkspaceSkill, WorkspaceSkillRevision, error) {
	var skill WorkspaceSkill
	var revision WorkspaceSkillRevision
	var deletedAt sql.NullTime
	var skillCreatedAt, skillUpdatedAt, revisionCreatedAt time.Time
	var filesJSON string
	if err := row.Scan(
		&skill.ID,
		&skill.WorkspaceID,
		&skill.Slug,
		&skill.Name,
		&skill.Description,
		&skill.SourceType,
		&skill.Enabled,
		&skill.Invocable,
		&skill.CurrentRevisionID,
		&skill.CreatedBy,
		&deletedAt,
		&skillCreatedAt,
		&skillUpdatedAt,
		&revision.ID,
		&revision.WorkspaceID,
		&revision.SkillID,
		&revision.Revision,
		&revision.ContentHash,
		&filesJSON,
		&revision.CreatedBy,
		&revisionCreatedAt,
	); err != nil {
		return WorkspaceSkill{}, WorkspaceSkillRevision{}, err
	}
	if deletedAt.Valid {
		skill.DeletedAt = deletedAt.Time.UTC().Format(time.RFC3339)
	}
	skill.CreatedAt = skillCreatedAt.UTC().Format(time.RFC3339)
	skill.UpdatedAt = skillUpdatedAt.UTC().Format(time.RFC3339)
	revision.CreatedAt = revisionCreatedAt.UTC().Format(time.RFC3339)
	if err := json.Unmarshal([]byte(filesJSON), &revision.Files); err != nil {
		return WorkspaceSkill{}, WorkspaceSkillRevision{}, err
	}
	return skill, revision, nil
}

func scanWorkspaceSkillRevision(row scanner) (WorkspaceSkillRevision, error) {
	var revision WorkspaceSkillRevision
	var filesJSON string
	var createdAt time.Time
	if err := row.Scan(
		&revision.ID,
		&revision.WorkspaceID,
		&revision.SkillID,
		&revision.Revision,
		&revision.ContentHash,
		&filesJSON,
		&revision.CreatedBy,
		&createdAt,
	); err != nil {
		return WorkspaceSkillRevision{}, err
	}
	revision.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	if err := json.Unmarshal([]byte(filesJSON), &revision.Files); err != nil {
		return WorkspaceSkillRevision{}, err
	}
	return revision, nil
}

func scanBuiltinSkillSetting(row scanner) (WorkspaceBuiltinSkillSetting, error) {
	var setting WorkspaceBuiltinSkillSetting
	var createdAt, updatedAt time.Time
	if err := row.Scan(&setting.WorkspaceID, &setting.Slug, &setting.Enabled, &setting.Invocable, &createdAt, &updatedAt); err != nil {
		return WorkspaceBuiltinSkillSetting{}, err
	}
	setting.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	setting.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return setting, nil
}
