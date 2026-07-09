package control

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
)

const builtinSkillSource = "mlhiter/skills"
const builtinSkillRoot = "builtin_skills/mlhiter/skills"

//go:embed builtin_skills/mlhiter/skills
var builtinSkillFS embed.FS

var (
	builtinSkillOnce    sync.Once
	builtinSkillItems   []SkillCatalogItem
	builtinSkillBundles map[string]RuntimeSkillBundle
	builtinSkillErr     error
)

func listBuiltinSkills() ([]SkillCatalogItem, error) {
	if err := loadBuiltinSkills(); err != nil {
		return nil, err
	}
	items := append([]SkillCatalogItem(nil), builtinSkillItems...)
	return items, nil
}

func builtinSkillBundle(slug, revision string) (RuntimeSkillBundle, error) {
	if err := loadBuiltinSkills(); err != nil {
		return RuntimeSkillBundle{}, err
	}
	slug = normalizeSkillSlug(slug)
	if slug == "" {
		return RuntimeSkillBundle{}, errors.New("skill slug is required")
	}
	bundle, ok := builtinSkillBundles[slug]
	if !ok {
		return RuntimeSkillBundle{}, ErrNotFound
	}
	revision = strings.TrimSpace(revision)
	if revision != "" && revision != "latest" && revision != bundle.Revision {
		return RuntimeSkillBundle{}, ErrNotFound
	}
	return cloneSkillBundle(bundle), nil
}

func builtinSkillReference(slug string) (AgentSessionSkillReference, RuntimeSkillBundle, error) {
	bundle, err := builtinSkillBundle(slug, "latest")
	if err != nil {
		return AgentSessionSkillReference{}, RuntimeSkillBundle{}, err
	}
	return skillReferenceFromBundle(bundle), bundle, nil
}

func resolveAgentSessionSkillBundles(input *CreateAgentSessionInput) error {
	if input == nil {
		return nil
	}
	slugs, err := normalizeAgentSessionSkillSlugs(input.SkillSlugs)
	if err != nil {
		return err
	}
	input.SkillSlugs = slugs
	if len(slugs) == 0 {
		return nil
	}
	bundles := append([]RuntimeSkillBundle(nil), input.SkillBundles...)
	existing := map[string]bool{}
	for _, bundle := range bundles {
		if slug := normalizeSkillSlug(bundle.Slug); slug != "" {
			existing[slug] = true
		}
	}
	for _, slug := range slugs {
		if existing[slug] {
			continue
		}
		_, bundle, err := builtinSkillReference(slug)
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("skill slug must reference a built-in skill: %s", slug)
		}
		if err != nil {
			return err
		}
		bundles = append(bundles, bundle)
		existing[slug] = true
	}
	input.SkillBundles = bundles
	return nil
}

func normalizeAgentSessionSkillSlugs(values []string) ([]string, error) {
	normalized := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		slug := normalizeSkillSlug(value)
		if slug == "" {
			return nil, fmt.Errorf("skill slug must use letters, numbers, hyphen, or underscore: %s", value)
		}
		if seen[slug] {
			continue
		}
		seen[slug] = true
		normalized = append(normalized, slug)
	}
	return normalized, nil
}

func loadBuiltinSkills() error {
	builtinSkillOnce.Do(func() {
		items, bundles, err := readBuiltinSkillFS()
		if err != nil {
			builtinSkillErr = err
			return
		}
		builtinSkillItems = items
		builtinSkillBundles = bundles
	})
	return builtinSkillErr
}

func readBuiltinSkillFS() ([]SkillCatalogItem, map[string]RuntimeSkillBundle, error) {
	entries, err := builtinSkillFS.ReadDir(builtinSkillRoot)
	if err != nil {
		return nil, nil, err
	}
	items := []SkillCatalogItem{}
	bundles := map[string]RuntimeSkillBundle{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		slug := normalizeSkillSlug(entry.Name())
		if slug == "" {
			continue
		}
		bundle, err := readBuiltinSkillBundle(slug)
		if err != nil {
			return nil, nil, err
		}
		bundles[slug] = bundle
		items = append(items, SkillCatalogItem{
			Slug:        bundle.Slug,
			Name:        bundle.Name,
			Description: bundle.Description,
			Source:      bundle.Source,
			Revision:    bundle.Revision,
			ContentHash: bundle.ContentHash,
			BuiltIn:     bundle.BuiltIn,
			FileCount:   len(bundle.Files),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Slug < items[j].Slug
	})
	return items, bundles, nil
}

func readBuiltinSkillBundle(slug string) (RuntimeSkillBundle, error) {
	root := path.Join(builtinSkillRoot, slug)
	files := []RuntimeSkillFile{}
	if err := fs.WalkDir(builtinSkillFS, root, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative := strings.TrimPrefix(strings.TrimPrefix(filePath, root), "/")
		if !isSafeSkillFilePath(relative) {
			return fmt.Errorf("unsafe built-in skill path %q", relative)
		}
		body, err := builtinSkillFS.ReadFile(filePath)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		files = append(files, RuntimeSkillFile{
			Path:    relative,
			Content: string(body),
			SHA256:  "sha256:" + hex.EncodeToString(sum[:]),
		})
		return nil
	}); err != nil {
		return RuntimeSkillBundle{}, err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	if len(files) == 0 {
		return RuntimeSkillBundle{}, fmt.Errorf("built-in skill %q has no files", slug)
	}
	name, description := skillFrontmatter(slug, files)
	contentHash := skillBundleContentHash(files)
	return RuntimeSkillBundle{
		Slug:        slug,
		Name:        name,
		Description: description,
		Source:      builtinSkillSource,
		Revision:    strings.TrimPrefix(contentHash, "sha256:")[:12],
		ContentHash: contentHash,
		BuiltIn:     true,
		Files:       files,
	}, nil
}

func skillBundleContentHash(files []RuntimeSkillFile) string {
	hash := sha256.New()
	for _, file := range files {
		hash.Write([]byte(file.Path))
		hash.Write([]byte{0})
		hash.Write([]byte(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(file.SHA256)), "sha256:")))
		hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func skillFrontmatter(slug string, files []RuntimeSkillFile) (string, string) {
	name := slug
	description := ""
	for _, file := range files {
		if file.Path != "SKILL.md" {
			continue
		}
		metadata := parseSimpleFrontmatter(file.Content)
		if value := strings.TrimSpace(metadata["name"]); value != "" {
			name = value
		}
		if value := strings.TrimSpace(metadata["description"]); value != "" {
			description = value
		}
		break
	}
	return name, description
}

func parseSimpleFrontmatter(content string) map[string]string {
	result := map[string]string{}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return result
	}
	for index := 1; index < len(lines); index++ {
		line := lines[index]
		if strings.TrimSpace(line) == "---" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if value == "|" || value == ">" {
			block := []string{}
			for next := index + 1; next < len(lines); next++ {
				candidate := lines[next]
				if strings.TrimSpace(candidate) == "---" || (candidate != "" && candidate[0] != ' ' && candidate[0] != '\t') {
					break
				}
				block = append(block, strings.TrimSpace(candidate))
				index = next
			}
			value = strings.Join(block, " ")
		}
		result[key] = strings.Trim(value, `"'`)
	}
	return result
}

func normalizeSkillSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Trim(value, "/")
	if value == "" || strings.Contains(value, "..") || strings.ContainsAny(value, `\`) {
		return ""
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return ""
	}
	return value
}

func isSafeSkillFilePath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return false
	}
	cleaned := path.Clean(value)
	return cleaned == value && cleaned != "." && !strings.HasPrefix(cleaned, "../") && !strings.Contains(cleaned, "/../")
}

func skillReferenceFromBundle(bundle RuntimeSkillBundle) AgentSessionSkillReference {
	return AgentSessionSkillReference{
		Slug:        bundle.Slug,
		Name:        bundle.Name,
		Source:      bundle.Source,
		Revision:    bundle.Revision,
		ContentHash: bundle.ContentHash,
		BuiltIn:     bundle.BuiltIn,
	}
}

func skillReferencesFromBundles(bundles []RuntimeSkillBundle) []AgentSessionSkillReference {
	if len(bundles) == 0 {
		return nil
	}
	references := make([]AgentSessionSkillReference, 0, len(bundles))
	for _, bundle := range bundles {
		references = append(references, skillReferenceFromBundle(bundle))
	}
	return references
}

func cloneSkillBundle(bundle RuntimeSkillBundle) RuntimeSkillBundle {
	bundle.Files = append([]RuntimeSkillFile(nil), bundle.Files...)
	return bundle
}
