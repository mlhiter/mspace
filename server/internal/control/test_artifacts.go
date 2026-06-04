package control

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type testEvidenceScreenshotCandidate struct {
	Path        string
	Filename    string
	ContentType string
	Data        []byte
	Metadata    map[string]any
}

func (s *PostgresStore) createTestArtifact(ctx context.Context, q queryer, input CreateTestArtifactInput) (TestArtifact, error) {
	normalized, err := normalizeCreateTestArtifactInput(input)
	if err != nil {
		return TestArtifact{}, err
	}
	sum := sha256.Sum256(normalized.Content)
	hash := hex.EncodeToString(sum[:])
	var id string
	row := q.QueryRow(ctx, `
		INSERT INTO test_artifacts (
			workspace_id,
			project_id,
			run_id,
			run_item_id,
			case_id,
			source_issue_id,
			source_task_id,
			source_session_id,
			kind,
			role,
			filename,
			content_type,
			size_bytes,
			sha256,
			storage_backend,
			content,
			metadata
		)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid, NULLIF($7, '')::uuid, $8, $9, $10, $11, $12, $13, $14, 'postgres_blob', $15, $16::jsonb)
		ON CONFLICT(run_item_id, sha256) DO UPDATE SET metadata = test_artifacts.metadata || excluded.metadata
		RETURNING id::text
	`, normalized.WorkspaceID, normalized.ProjectID, normalized.RunID, normalized.RunItemID, normalized.CaseID, normalized.SourceIssueID, normalized.SourceTaskID, normalized.SourceSessionID, normalized.Kind, normalized.Role, normalized.Filename, normalized.ContentType, len(normalized.Content), hash, normalized.Content, normalized.Metadata)
	if err := row.Scan(&id); err != nil {
		return TestArtifact{}, err
	}
	return loadTestArtifactByID(ctx, q, "", id)
}

func (s *PostgresStore) ListProjectTestRunArtifacts(ctx Context, userID, workspaceID, projectID, runID string) ([]TestArtifact, error) {
	dbctx := asContext(ctx)
	workspaceID = strings.TrimSpace(workspaceID)
	projectID = strings.TrimSpace(projectID)
	runID = strings.TrimSpace(runID)
	if err := ensureWorkspaceMember(dbctx, s.pool, workspaceID, userID); err != nil {
		return nil, err
	}
	if _, err := loadTestRun(dbctx, s.pool, workspaceID, projectID, runID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(dbctx, testArtifactSelectQuery(`
		JOIN workspace_members wm ON wm.workspace_id = a.workspace_id AND wm.user_id = $4
		WHERE a.workspace_id = $1 AND a.project_id = $2 AND a.run_id = $3
		ORDER BY a.created_at DESC, a.id DESC
	`), workspaceID, projectID, runID, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	artifacts := []TestArtifact{}
	for rows.Next() {
		artifact, err := scanTestArtifact(rows)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func (s *PostgresStore) GetTestArtifact(ctx Context, userID, artifactID string) (TestArtifact, error) {
	return loadTestArtifactByID(asContext(ctx), s.pool, strings.TrimSpace(userID), strings.TrimSpace(artifactID))
}

func loadTestArtifactByID(ctx context.Context, q queryer, userID, artifactID string) (TestArtifact, error) {
	artifactID = strings.TrimSpace(artifactID)
	userID = strings.TrimSpace(userID)
	if artifactID == "" {
		return TestArtifact{}, ErrNotFound
	}
	where := `WHERE a.id = $1`
	args := []any{artifactID}
	if userID != "" {
		where = `JOIN workspace_members wm ON wm.workspace_id = a.workspace_id AND wm.user_id = $2 WHERE a.id = $1`
		args = append(args, userID)
	}
	artifact, err := scanTestArtifact(q.QueryRow(ctx, testArtifactSelectQuery(where), args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return TestArtifact{}, ErrNotFound
	}
	return artifact, err
}

func testArtifactSelectQuery(whereClause string) string {
	return `SELECT
			a.id::text,
			a.workspace_id::text,
			a.project_id::text,
			a.run_id::text,
			a.run_item_id::text,
			a.case_id::text,
			COALESCE(a.source_issue_id::text, ''),
			COALESCE(a.source_task_id::text, ''),
			a.source_session_id,
			a.kind,
			a.role,
			a.filename,
			a.content_type,
			a.size_bytes,
			a.sha256,
			a.storage_backend,
			a.storage_key,
			a.content,
			a.metadata,
			a.created_at
		FROM test_artifacts a
	` + whereClause
}

func scanTestArtifact(row scanner) (TestArtifact, error) {
	var artifact TestArtifact
	var metadata []byte
	var createdAt time.Time
	if err := row.Scan(
		&artifact.ID,
		&artifact.WorkspaceID,
		&artifact.ProjectID,
		&artifact.RunID,
		&artifact.RunItemID,
		&artifact.CaseID,
		&artifact.SourceIssueID,
		&artifact.SourceTaskID,
		&artifact.SourceSessionID,
		&artifact.Kind,
		&artifact.Role,
		&artifact.Filename,
		&artifact.ContentType,
		&artifact.SizeBytes,
		&artifact.SHA256,
		&artifact.StorageBackend,
		&artifact.StorageKey,
		&artifact.Content,
		&metadata,
		&createdAt,
	); err != nil {
		return TestArtifact{}, err
	}
	artifact.Content = append([]byte(nil), artifact.Content...)
	artifact.Metadata = copyRawMessage(json.RawMessage(metadata))
	if len(artifact.Metadata) == 0 {
		artifact.Metadata = json.RawMessage(`{}`)
	}
	artifact.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	return artifact, nil
}

func normalizeCreateTestArtifactInput(input CreateTestArtifactInput) (CreateTestArtifactInput, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.RunID = strings.TrimSpace(input.RunID)
	input.RunItemID = strings.TrimSpace(input.RunItemID)
	input.CaseID = strings.TrimSpace(input.CaseID)
	input.SourceIssueID = strings.TrimSpace(input.SourceIssueID)
	input.SourceTaskID = strings.TrimSpace(input.SourceTaskID)
	input.SourceSessionID = strings.TrimSpace(input.SourceSessionID)
	input.Kind = normalizeTestArtifactKind(input.Kind)
	input.Role = normalizeTestArtifactRole(input.Role)
	input.Filename = sanitizeTestArtifactFilename(input.Filename, input.ContentType)
	input.ContentType = normalizeTestArtifactContentType(input.ContentType, input.Content)
	if input.WorkspaceID == "" || input.ProjectID == "" || input.RunID == "" || input.RunItemID == "" || input.CaseID == "" {
		return CreateTestArtifactInput{}, errors.New("test artifact must include workspace, project, run, run item, and case")
	}
	if input.Kind == "" {
		return CreateTestArtifactInput{}, errors.New("unsupported test artifact kind")
	}
	if input.Role == "" {
		input.Role = "evidence"
	}
	if len(input.Content) == 0 {
		return CreateTestArtifactInput{}, errors.New("test artifact content is required")
	}
	if len(input.Content) > maxTestResultArtifactBytes {
		return CreateTestArtifactInput{}, fmt.Errorf("test artifact exceeds %d bytes", maxTestResultArtifactBytes)
	}
	if !allowedTestArtifactContentType(input.Kind, input.ContentType) {
		return CreateTestArtifactInput{}, errors.New("unsupported test artifact content type")
	}
	if len(input.Metadata) == 0 || !json.Valid(input.Metadata) {
		input.Metadata = json.RawMessage(`{}`)
	} else {
		input.Metadata = cloneRawJSONObject(input.Metadata)
	}
	return input, nil
}

func normalizeTestArtifactKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "screenshot", "trace", "log", "dom_snapshot", "network", "resource", "other":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeTestArtifactRole(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "thumbnail", "original", "evidence":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "evidence"
	}
}

func normalizeTestArtifactContentType(contentType string, content []byte) string {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if contentType == "" && len(content) > 0 {
		contentType = strings.ToLower(http.DetectContentType(content))
	}
	return contentType
}

func allowedTestArtifactContentType(kind, contentType string) bool {
	switch normalizeTestArtifactKind(kind) {
	case "screenshot":
		return contentType == "image/png" || contentType == "image/jpeg" || contentType == "image/webp" || contentType == "image/gif"
	case "trace", "log", "dom_snapshot", "network", "resource", "other":
		return contentType == "application/json" || strings.HasPrefix(contentType, "text/")
	default:
		return false
	}
}

func sanitizeTestArtifactFilename(filename, contentType string) string {
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "." || filename == "/" || filename == "" {
		filename = "test-artifact"
	}
	filename = truncateString(filename, 240)
	if filepath.Ext(filename) == "" {
		if exts, err := mime.ExtensionsByType(strings.TrimSpace(contentType)); err == nil && len(exts) > 0 {
			filename += exts[0]
		}
	}
	return filename
}

func testArtifactRef(artifact TestArtifact) TestArtifactRef {
	path := "/api/test-artifacts/" + artifact.ID
	metadata := copyRawMessage(artifact.Metadata)
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	return TestArtifactRef{
		ID:           artifact.ID,
		Kind:         artifact.Kind,
		Role:         artifact.Role,
		Filename:     artifact.Filename,
		ContentType:  artifact.ContentType,
		SizeBytes:    artifact.SizeBytes,
		SHA256:       artifact.SHA256,
		URL:          path,
		ThumbnailURL: path,
		Metadata:     metadata,
		CreatedAt:    artifact.CreatedAt,
	}
}

func testArtifactRefs(artifacts []TestArtifact) []TestArtifactRef {
	refs := make([]TestArtifactRef, 0, len(artifacts))
	for _, artifact := range artifacts {
		refs = append(refs, testArtifactRef(artifact))
	}
	return refs
}

func rewriteTestResultEvidenceWithArtifacts(evidence json.RawMessage, artifacts []TestArtifact) json.RawMessage {
	record := map[string]any{}
	if len(evidence) > 0 {
		_ = json.Unmarshal(evidence, &record)
	}
	if len(record) == 0 {
		record = map[string]any{}
	}
	record = stripEmbeddedScreenshotPayloads(record)
	existingArtifacts := []any{}
	if values, ok := record["artifacts"].([]any); ok {
		existingArtifacts = append(existingArtifacts, values...)
	}
	for _, artifact := range artifacts {
		existingArtifacts = append(existingArtifacts, testArtifactRef(artifact))
	}
	if len(existingArtifacts) > 0 {
		record["artifacts"] = existingArtifacts
	}
	if values, ok := record["screenshotImages"].([]any); ok {
		rewritten := []any{}
		for _, value := range values {
			image, ok := value.(map[string]any)
			if !ok {
				continue
			}
			delete(image, "dataUrl")
			delete(image, "dataURL")
			delete(image, "base64")
			if len(image) > 0 {
				rewritten = append(rewritten, image)
			}
		}
		for _, artifact := range artifacts {
			ref := testArtifactRef(artifact)
			rewritten = append(rewritten, map[string]any{
				"path":         artifact.Filename,
				"artifactId":   artifact.ID,
				"contentType":  artifact.ContentType,
				"mime":         artifact.ContentType,
				"url":          ref.URL,
				"thumbnailUrl": ref.ThumbnailURL,
			})
		}
		if len(rewritten) > 0 {
			record["screenshotImages"] = rewritten
		} else {
			delete(record, "screenshotImages")
		}
	} else if len(artifacts) > 0 {
		images := []any{}
		for _, artifact := range artifacts {
			ref := testArtifactRef(artifact)
			images = append(images, map[string]any{
				"path":         artifact.Filename,
				"artifactId":   artifact.ID,
				"contentType":  artifact.ContentType,
				"mime":         artifact.ContentType,
				"url":          ref.URL,
				"thumbnailUrl": ref.ThumbnailURL,
			})
		}
		record["screenshotImages"] = images
	}
	data, err := json.Marshal(record)
	if err != nil {
		return cloneRawJSONObject(evidence)
	}
	return cloneRawJSONObject(data)
}

func testEvidenceScreenshotCandidates(evidence json.RawMessage) []testEvidenceScreenshotCandidate {
	record := map[string]any{}
	if len(evidence) == 0 || json.Unmarshal(evidence, &record) != nil {
		return nil
	}
	candidates := []testEvidenceScreenshotCandidate{}
	candidates = append(candidates, screenshotCandidatesFromEvidenceValue(record["screenshot"], "screenshot")...)
	candidates = append(candidates, screenshotCandidatesFromEvidenceValue(record["screenshots"], "screenshot")...)
	candidates = append(candidates, screenshotCandidatesFromEvidenceValue(record["screenshotImages"], "screenshot")...)
	candidates = append(candidates, screenshotCandidatesFromEvidenceValue(record["artifacts"], "screenshot")...)
	return dedupeTestEvidenceScreenshotCandidates(candidates)
}

func screenshotCandidatesFromEvidenceValue(value any, fallbackName string) []testEvidenceScreenshotCandidate {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		contentType, data, ok := decodeDataURL(typed)
		contentType = normalizeTestArtifactContentType(contentType, data)
		if !ok || !allowedTestArtifactContentType("screenshot", contentType) {
			return nil
		}
		filename := sanitizeTestArtifactFilename(fallbackName, contentType)
		return []testEvidenceScreenshotCandidate{{
			Filename:    filename,
			ContentType: contentType,
			Data:        data,
			Metadata: map[string]any{
				"source": fallbackName,
			},
		}}
	case []any:
		candidates := []testEvidenceScreenshotCandidate{}
		for _, item := range typed {
			candidates = append(candidates, screenshotCandidatesFromEvidenceValue(item, fallbackName)...)
		}
		return candidates
	case map[string]any:
		dataURL := firstStringFromMap(typed, "dataUrl", "dataURL", "data_url")
		contentType, data, ok := decodeDataURL(dataURL)
		contentType = normalizeTestArtifactContentType(contentType, data)
		if !ok {
			base64Payload := firstStringFromMap(typed, "base64")
			contentType = normalizeTestArtifactContentType(firstStringFromMap(typed, "mime", "contentType", "content_type"), nil)
			data, ok = decodeBase64ScreenshotPayload(base64Payload, contentType)
		}
		if !ok || !allowedTestArtifactContentType("screenshot", contentType) {
			return nil
		}
		if mimeValue := firstStringFromMap(typed, "mime", "contentType", "content_type"); mimeValue != "" {
			contentType = normalizeTestArtifactContentType(mimeValue, data)
		}
		path := firstStringFromMap(typed, "path", "filename", "name", "title")
		filename := sanitizeTestArtifactFilename(firstNonEmpty(path, fallbackName, "screenshot"), contentType)
		return []testEvidenceScreenshotCandidate{{
			Path:        path,
			Filename:    filename,
			ContentType: contentType,
			Data:        data,
			Metadata: map[string]any{
				"path":   path,
				"source": fallbackName,
			},
		}}
	default:
		return nil
	}
}

func dedupeTestEvidenceScreenshotCandidates(candidates []testEvidenceScreenshotCandidate) []testEvidenceScreenshotCandidate {
	seen := map[string]bool{}
	deduped := []testEvidenceScreenshotCandidate{}
	for _, candidate := range candidates {
		if len(candidate.Data) == 0 {
			continue
		}
		sum := sha256.Sum256(candidate.Data)
		key := hex.EncodeToString(sum[:])
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, candidate)
		if len(deduped) >= maxTestResultArtifactsPerItem {
			break
		}
	}
	return deduped
}

func firstStringFromMap(record map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := record[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stripEmbeddedScreenshotPayloads(record map[string]any) map[string]any {
	cleaned := map[string]any{}
	for key, value := range record {
		cleanedValue, keep := stripEmbeddedScreenshotPayloadValue(value)
		if !keep {
			continue
		}
		cleaned[key] = cleanedValue
	}
	return cleaned
}

func stripEmbeddedScreenshotPayloadValue(value any) (any, bool) {
	switch typed := value.(type) {
	case string:
		if strings.HasPrefix(strings.TrimSpace(typed), "data:image/") {
			return nil, false
		}
		return typed, true
	case []any:
		cleaned := []any{}
		for _, item := range typed {
			cleanedItem, keep := stripEmbeddedScreenshotPayloadValue(item)
			if keep {
				cleaned = append(cleaned, cleanedItem)
			}
		}
		return cleaned, len(cleaned) > 0
	case map[string]any:
		cleaned := map[string]any{}
		for key, item := range typed {
			switch key {
			case "dataUrl", "dataURL", "data_url", "base64":
				continue
			}
			cleanedItem, keep := stripEmbeddedScreenshotPayloadValue(item)
			if keep {
				cleaned[key] = cleanedItem
			}
		}
		return cleaned, len(cleaned) > 0
	default:
		return value, true
	}
}

func decodeDataURL(value string) (string, []byte, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "data:") {
		return "", nil, false
	}
	header, payload, ok := strings.Cut(strings.TrimPrefix(value, "data:"), ",")
	if !ok || !strings.Contains(header, ";base64") {
		return "", nil, false
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(header, ";")[0]))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(data) == 0 {
		return "", nil, false
	}
	return contentType, data, true
}

func decodeBase64ScreenshotPayload(value, contentType string) ([]byte, bool) {
	value = strings.TrimSpace(value)
	contentType = normalizeTestArtifactContentType(contentType, nil)
	if value == "" || !allowedTestArtifactContentType("screenshot", contentType) {
		return nil, false
	}
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}
