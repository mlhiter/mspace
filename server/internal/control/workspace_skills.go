package control

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	skillSourceTypeBuiltin = "builtin"
	skillSourceTypeCustom  = "custom"
	workspaceSkillSource   = "workspace"

	builtinSkillIDPrefix = "builtin:"

	maxWorkspaceSkillFiles        = 32
	maxWorkspaceSkillFileBytes    = 256 * 1024
	maxWorkspaceSkillTotalBytes   = 1024 * 1024
	maxWorkspaceSkillRequestBytes = maxWorkspaceSkillTotalBytes + 512*1024
)

func builtinSkillID(slug string) string {
	return builtinSkillIDPrefix + normalizeSkillSlug(slug)
}

func builtinSkillExists(slug string) bool {
	_, err := builtinSkillBundle(slug, "latest")
	return err == nil
}

func slugFromBuiltinSkillID(value string) (string, bool) {
	if !strings.HasPrefix(strings.TrimSpace(value), builtinSkillIDPrefix) {
		return "", false
	}
	slug := normalizeSkillSlug(strings.TrimPrefix(strings.TrimSpace(value), builtinSkillIDPrefix))
	return slug, slug != ""
}

func skillBoolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func normalizeWorkspaceSkillCreateInput(input SkillInput) (SkillInput, []RuntimeSkillFile, error) {
	slug := normalizeSkillSlug(input.Slug)
	if slug == "" {
		return SkillInput{}, nil, errors.New("skill slug must use letters, numbers, hyphen, or underscore")
	}
	files, err := normalizeWorkspaceSkillFiles(input.Files)
	if err != nil {
		return SkillInput{}, nil, err
	}
	name, description := skillFrontmatter(slug, files)
	if strings.TrimSpace(input.Name) != "" {
		name = strings.TrimSpace(input.Name)
	}
	if strings.TrimSpace(input.Description) != "" {
		description = strings.TrimSpace(input.Description)
	}
	normalized := SkillInput{
		Slug:        slug,
		Name:        name,
		Description: description,
		Enabled:     boolPointer(skillBoolValue(input.Enabled, true)),
		Invocable:   boolPointer(skillBoolValue(input.Invocable, true)),
		Files:       files,
	}
	return normalized, files, nil
}

func normalizeWorkspaceSkillUpdateInput(existing SkillDetail, input SkillInput) (SkillInput, []RuntimeSkillFile, bool, error) {
	if slug := strings.TrimSpace(input.Slug); slug != "" && normalizeSkillSlug(slug) != existing.Slug {
		return SkillInput{}, nil, false, errors.New("skill slug cannot be changed")
	}
	normalized := SkillInput{
		Slug:        existing.Slug,
		Name:        existing.Name,
		Description: existing.Description,
		Enabled:     boolPointer(skillBoolValue(input.Enabled, existing.Enabled)),
		Invocable:   boolPointer(skillBoolValue(input.Invocable, existing.Invocable)),
	}
	if strings.TrimSpace(input.Name) != "" {
		normalized.Name = strings.TrimSpace(input.Name)
	}
	if strings.TrimSpace(input.Description) != "" {
		normalized.Description = strings.TrimSpace(input.Description)
	}
	if input.Files == nil {
		return normalized, nil, false, nil
	}
	files, err := normalizeWorkspaceSkillFiles(input.Files)
	if err != nil {
		return SkillInput{}, nil, false, err
	}
	name, description := skillFrontmatter(existing.Slug, files)
	if strings.TrimSpace(input.Name) == "" {
		normalized.Name = name
	}
	if strings.TrimSpace(input.Description) == "" {
		normalized.Description = description
	}
	normalized.Files = files
	return normalized, files, true, nil
}

func normalizeWorkspaceSkillFiles(files []RuntimeSkillFile) ([]RuntimeSkillFile, error) {
	if len(files) == 0 {
		return nil, errors.New("skill files cannot be empty")
	}
	if len(files) > maxWorkspaceSkillFiles {
		return nil, fmt.Errorf("skill files cannot exceed %d files", maxWorkspaceSkillFiles)
	}
	seen := map[string]bool{}
	totalBytes := 0
	normalized := make([]RuntimeSkillFile, 0, len(files))
	hasSkillMD := false
	for _, file := range files {
		pathValue := strings.TrimSpace(file.Path)
		if !isSafeSkillFilePath(pathValue) {
			return nil, fmt.Errorf("skill file path is not safe: %s", pathValue)
		}
		if seen[pathValue] {
			return nil, fmt.Errorf("skill file path is duplicated: %s", pathValue)
		}
		seen[pathValue] = true
		content := file.Content
		if len(content) > maxWorkspaceSkillFileBytes {
			return nil, fmt.Errorf("skill file is too large: %s", pathValue)
		}
		totalBytes += len(content)
		if totalBytes > maxWorkspaceSkillTotalBytes {
			return nil, fmt.Errorf("skill files cannot exceed %d bytes", maxWorkspaceSkillTotalBytes)
		}
		if pathValue == "SKILL.md" {
			hasSkillMD = true
			if strings.TrimSpace(content) == "" {
				return nil, errors.New("SKILL.md cannot be empty")
			}
		}
		sum := sha256.Sum256([]byte(content))
		normalized = append(normalized, RuntimeSkillFile{
			Path:    pathValue,
			Content: content,
			SHA256:  "sha256:" + hex.EncodeToString(sum[:]),
		})
	}
	if !hasSkillMD {
		return nil, errors.New("skill files must include SKILL.md")
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Path < normalized[j].Path
	})
	return normalized, nil
}

func workspaceSkillRevisionFromFiles(files []RuntimeSkillFile) (string, string) {
	contentHash := skillBundleContentHash(files)
	revision := strings.TrimPrefix(contentHash, "sha256:")
	if len(revision) > 12 {
		revision = revision[:12]
	}
	return revision, contentHash
}

func skillCatalogItemFromBundle(bundle RuntimeSkillBundle, setting WorkspaceBuiltinSkillSetting) SkillCatalogItem {
	enabled := true
	invocable := true
	updatedAt := ""
	if setting.Slug != "" {
		enabled = setting.Enabled
		invocable = setting.Invocable
		updatedAt = setting.UpdatedAt
	}
	return SkillCatalogItem{
		ID:          builtinSkillID(bundle.Slug),
		Slug:        bundle.Slug,
		Name:        bundle.Name,
		Description: bundle.Description,
		Source:      bundle.Source,
		SourceType:  skillSourceTypeBuiltin,
		Revision:    bundle.Revision,
		ContentHash: bundle.ContentHash,
		Enabled:     enabled,
		Invocable:   invocable,
		BuiltIn:     true,
		Editable:    false,
		Deletable:   false,
		FileCount:   len(bundle.Files),
		CreatedAt:   "",
		UpdatedAt:   updatedAt,
	}
}

func skillDetailFromBundle(bundle RuntimeSkillBundle, setting WorkspaceBuiltinSkillSetting) SkillDetail {
	return SkillDetail{
		SkillCatalogItem: skillCatalogItemFromBundle(bundle, setting),
		Files:            append([]RuntimeSkillFile(nil), bundle.Files...),
	}
}

func skillCatalogItemFromWorkspaceSkill(skill WorkspaceSkill, revision WorkspaceSkillRevision) SkillCatalogItem {
	return SkillCatalogItem{
		ID:          skill.ID,
		Slug:        skill.Slug,
		Name:        skill.Name,
		Description: skill.Description,
		Source:      workspaceSkillSource,
		SourceType:  skillSourceTypeCustom,
		Revision:    revision.Revision,
		ContentHash: revision.ContentHash,
		Enabled:     skill.Enabled,
		Invocable:   skill.Invocable,
		BuiltIn:     false,
		Editable:    true,
		Deletable:   true,
		FileCount:   len(revision.Files),
		CreatedAt:   skill.CreatedAt,
		UpdatedAt:   skill.UpdatedAt,
	}
}

func skillDetailFromWorkspaceSkill(skill WorkspaceSkill, revision WorkspaceSkillRevision) SkillDetail {
	return SkillDetail{
		SkillCatalogItem: skillCatalogItemFromWorkspaceSkill(skill, revision),
		Files:            append([]RuntimeSkillFile(nil), revision.Files...),
	}
}

func runtimeSkillBundleFromWorkspaceSkill(skill WorkspaceSkill, revision WorkspaceSkillRevision) RuntimeSkillBundle {
	return RuntimeSkillBundle{
		Slug:        skill.Slug,
		Name:        skill.Name,
		Description: skill.Description,
		Source:      workspaceSkillSource,
		Revision:    revision.Revision,
		ContentHash: revision.ContentHash,
		BuiltIn:     false,
		Files:       append([]RuntimeSkillFile(nil), revision.Files...),
	}
}

func boolPointer(value bool) *bool {
	return &value
}
