package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const (
	issueLabelDimensionType     = "type"
	issueLabelDimensionPriority = "priority"
)

var issueTypeLabelKeys = []string{
	"feat",
	"fix",
	"docs",
	"style",
	"refactor",
	"perf",
	"test",
	"build",
	"ci",
	"chore",
	"revert",
}

var priorityLabelKeys = []string{
	"p0",
	"p1",
	"p2",
	"p3",
}

func builtInIssueLabelDefinitions(now string) []issueLabelDefinition {
	definitions := make([]issueLabelDefinition, 0, len(issueTypeLabelKeys)+len(priorityLabelKeys))
	for index, name := range issueTypeLabelKeys {
		definitions = append(definitions, issueLabelDefinition{
			ID:        "type-" + name,
			Key:       "type:" + name,
			Name:      name,
			Dimension: issueLabelDimensionType,
			SortOrder: index + 10,
			BuiltIn:   true,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	for index, name := range priorityLabelKeys {
		definitions = append(definitions, issueLabelDefinition{
			ID:        "priority-" + name,
			Key:       "priority:" + name,
			Name:      strings.ToUpper(name),
			Dimension: issueLabelDimensionPriority,
			SortOrder: index + 10,
			BuiltIn:   true,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	return definitions
}

func (a *app) seedIssueLabelDefinitions() error {
	now := nowString()
	for _, definition := range builtInIssueLabelDefinitions(now) {
		if _, err := a.db.Exec(`
			INSERT INTO issue_label_definitions (id, key, name, dimension, color, sort_order, built_in, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET
				name = excluded.name,
				dimension = excluded.dimension,
				color = excluded.color,
				sort_order = excluded.sort_order,
				built_in = excluded.built_in,
				updated_at = excluded.updated_at
		`, definition.ID, definition.Key, definition.Name, definition.Dimension, definition.Color, definition.SortOrder, boolToInt(definition.BuiltIn), definition.CreatedAt, definition.UpdatedAt); err != nil {
			return fmt.Errorf("seed issue label definition %s: %w", definition.Key, err)
		}
	}
	return nil
}

func (a *app) backfillIssueLabelIDs() error {
	if _, err := a.db.Exec(`
		UPDATE issue_labels
		SET label_id = (
			SELECT id
			FROM issue_label_definitions
			WHERE key = CASE
				WHEN lower(issue_labels.name) IN ('feat', 'fix', 'docs', 'style', 'refactor', 'perf', 'test', 'build', 'ci', 'chore', 'revert') THEN 'type:' || lower(issue_labels.name)
				WHEN lower(issue_labels.name) IN ('p0', 'p1', 'p2', 'p3') THEN 'priority:' || lower(issue_labels.name)
				ELSE ''
			END
		)
		WHERE label_id = ''
			AND lower(name) IN ('feat', 'fix', 'docs', 'style', 'refactor', 'perf', 'test', 'build', 'ci', 'chore', 'revert', 'p0', 'p1', 'p2', 'p3')
	`); err != nil {
		return fmt.Errorf("backfill issue label ids: %w", err)
	}
	return nil
}

func (a *app) loadIssueLabelDefinitionByKey(key string) (issueLabelDefinition, error) {
	var definition issueLabelDefinition
	row := a.db.QueryRow(`
		SELECT id, key, name, dimension, color, sort_order, built_in, created_at, updated_at
		FROM issue_label_definitions
		WHERE key = ?
	`, key)
	var builtIn int
	if err := row.Scan(&definition.ID, &definition.Key, &definition.Name, &definition.Dimension, &definition.Color, &definition.SortOrder, &builtIn, &definition.CreatedAt, &definition.UpdatedAt); err != nil {
		return definition, err
	}
	definition.BuiltIn = builtIn == 1
	return definition, nil
}

func (a *app) listIssueLabelDefinitions() ([]issueLabelDefinition, error) {
	rows, err := a.db.Query(`
		SELECT id, key, name, dimension, color, sort_order, built_in, created_at, updated_at
		FROM issue_label_definitions
		ORDER BY
			CASE dimension
				WHEN 'type' THEN 0
				WHEN 'priority' THEN 1
				ELSE 2
			END,
			sort_order ASC,
			name COLLATE NOCASE ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	definitions := []issueLabelDefinition{}
	for rows.Next() {
		var definition issueLabelDefinition
		var builtIn int
		if err := rows.Scan(&definition.ID, &definition.Key, &definition.Name, &definition.Dimension, &definition.Color, &definition.SortOrder, &builtIn, &definition.CreatedAt, &definition.UpdatedAt); err != nil {
			return nil, err
		}
		definition.BuiltIn = builtIn == 1
		definitions = append(definitions, definition)
	}
	return definitions, rows.Err()
}

func (a *app) normalizeIssueLabelKeys(values []string) ([]issueLabelDefinition, error) {
	definitions := make([]issueLabelDefinition, 0, len(values))
	seen := map[string]bool{}
	seenDimension := map[string]bool{}
	for _, raw := range values {
		key := normalizeIssueLabelKey(raw)
		if key == "" {
			continue
		}
		if seen[key] {
			continue
		}
		definition, err := a.loadIssueLabelDefinitionByKey(key)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("unknown issue label %q", raw)
		}
		if err != nil {
			return nil, err
		}
		if seenDimension[definition.Dimension] {
			return nil, fmt.Errorf("an issue can have only one %s label", definition.Dimension)
		}
		seen[key] = true
		seenDimension[definition.Dimension] = true
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

func normalizeIssueLabelKey(value string) string {
	label := strings.TrimSpace(value)
	label = strings.TrimSpace(strings.TrimPrefix(label, "#"))
	label = strings.Join(strings.Fields(label), " ")
	if label == "" {
		return ""
	}
	lower := strings.ToLower(label)
	if strings.Contains(lower, ":") {
		parts := strings.SplitN(lower, ":", 2)
		return strings.TrimSpace(parts[0]) + ":" + strings.TrimSpace(parts[1])
	}
	for _, name := range issueTypeLabelKeys {
		if lower == name {
			return "type:" + name
		}
	}
	for _, name := range priorityLabelKeys {
		if lower == name || lower == strings.ToUpper(name) {
			return "priority:" + name
		}
	}
	return lower
}

func isAllowedIssueTypeLabel(value string) bool {
	key := normalizeIssueLabelKey(value)
	for _, name := range issueTypeLabelKeys {
		if key == "type:"+name {
			return true
		}
	}
	return false
}

func hasIssueLabelDimension(labels []issueLabelDefinition, dimension string) bool {
	for _, label := range labels {
		if label.Dimension == dimension {
			return true
		}
	}
	return false
}
