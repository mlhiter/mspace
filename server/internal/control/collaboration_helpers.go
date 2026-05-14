package control

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

type issueTaskDraft struct {
	Title  string
	Body   string
	Status string
}

type gitOwnerRepo struct {
	owner string
	repo  string
}

const (
	issueLabelDimensionType     = "type"
	issueLabelDimensionPriority = "priority"
)

func normalizeProjectInput(input ProjectInput) (ProjectInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.SourceType = strings.ToLower(strings.TrimSpace(input.SourceType))
	input.RepoPath = strings.TrimSpace(input.RepoPath)
	input.RepoURL = strings.TrimSpace(input.RepoURL)
	input.DefaultBranch = strings.TrimSpace(input.DefaultBranch)
	input.KubeContext = strings.TrimSpace(input.KubeContext)
	input.KubeconfigPath = strings.TrimSpace(input.KubeconfigPath)
	input.Namespace = strings.TrimSpace(input.Namespace)
	input.ImageRegistryPrefix = strings.TrimSpace(input.ImageRegistryPrefix)
	input.PreviewDomain = strings.TrimSpace(input.PreviewDomain)
	input.IngressClass = strings.TrimSpace(input.IngressClass)
	input.NodeHost = strings.TrimSpace(input.NodeHost)
	input.DefaultClusterID = strings.TrimSpace(input.DefaultClusterID)

	if input.SourceType == "" {
		if input.RepoURL != "" && input.RepoPath == "" {
			input.SourceType = "github"
		} else {
			input.SourceType = "local"
		}
	}
	if input.SourceType != "local" && input.SourceType != "github" {
		return ProjectInput{}, errors.New("sourceType must be local or github")
	}
	if input.SourceType == "github" && input.RepoURL == "" {
		return ProjectInput{}, errors.New("repoUrl is required for GitHub projects")
	}
	if input.SourceType == "local" && input.RepoPath == "" {
		return ProjectInput{}, errors.New("repoPath is required for local projects")
	}
	if input.Name == "" {
		if input.SourceType == "github" {
			input.Name = projectNameFromRepoURL(input.RepoURL)
		} else {
			input.Name = filepath.Base(strings.TrimRight(input.RepoPath, "/"))
		}
	}
	if input.Name == "" {
		return ProjectInput{}, errors.New("project name is required")
	}
	if input.DefaultBranch == "" {
		input.DefaultBranch = "main"
	}
	return input, nil
}

func projectNameFromRepoURL(value string) string {
	parts := gitOwnerRepoFromURL(value)
	if parts.repo != "" {
		return strings.TrimSuffix(parts.repo, ".git")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(parsed.Path), ".git")
}

func gitProviderFromURL(value string) string {
	if strings.Contains(strings.ToLower(value), "github.com") {
		return "github"
	}
	return ""
}

func gitOwnerRepoFromURL(value string) gitOwnerRepo {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" {
		return gitOwnerRepo{}
	}
	if !strings.Contains(strings.ToLower(parsed.Host), "github.com") {
		return gitOwnerRepo{}
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return gitOwnerRepo{}
	}
	return gitOwnerRepo{owner: parts[0], repo: strings.TrimSuffix(parts[1], ".git")}
}

func normalizeRunbookStatus(status, content string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		if strings.TrimSpace(content) == "" {
			return "empty"
		}
		return "learned"
	}
	switch status {
	case "empty", "learned", "stale":
		return status
	default:
		return "learned"
	}
}

func normalizeCreateIssueInput(input CreateIssueInput, user User) (CreateIssueInput, []issueTaskDraft, []IssueLabel, error) {
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.Title = strings.TrimSpace(input.Title)
	input.Body = strings.TrimSpace(input.Body)
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.Assignee = strings.TrimSpace(input.Assignee)
	input.AssigneeType = normalizeAssigneeType(input.AssigneeType)
	input.CreatorName = normalizeHumanName(firstNonEmpty(input.CreatorName, user.Name))
	input.CreatorAvatar = strings.TrimSpace(firstNonEmpty(input.CreatorAvatar, user.AvatarURL))
	if input.Body == "" {
		input.Body = input.Prompt
	}

	parentBody, taskDrafts := extractIssueTaskDrafts(input.Body)
	taskDrafts = append(taskDrafts, normalizeIssueTaskStrings(input.Tasks)...)
	taskDrafts = append(taskDrafts, normalizeIssueTaskInputs(input.ChildIssues)...)
	if input.Title == "" {
		if parentBody != "" {
			input.Title = deriveIssueTitle(parentBody)
		} else if len(taskDrafts) > 0 {
			input.Title = deriveIssueTitle(taskDrafts[0].Title)
		} else {
			input.Title = deriveIssueTitle(input.Body)
		}
	}
	if input.Title == "" {
		return CreateIssueInput{}, nil, nil, errors.New("issue cannot be empty")
	}
	input.Body = parentBody
	if input.AssigneeType == "" {
		input.AssigneeType = "human"
	}
	if input.AssigneeType != "human" && input.AssigneeType != "agent" {
		return CreateIssueInput{}, nil, nil, errors.New("assignee type must be human or agent")
	}
	if input.AssigneeType == "human" {
		input.Assignee = normalizeHumanName(firstNonEmpty(input.Assignee, input.CreatorName))
	}
	for _, task := range taskDrafts {
		if err := validateIssueStatus(task.Status); err != nil {
			return CreateIssueInput{}, nil, nil, err
		}
	}
	labels, err := normalizeIssueLabelKeys(append(input.LabelKeys, input.Labels...))
	if err != nil {
		return CreateIssueInput{}, nil, nil, err
	}
	return input, taskDrafts, labels, nil
}

func deriveIssueTitle(body string) string {
	title := strings.TrimSpace(body)
	if title == "" {
		return ""
	}
	if idx := strings.IndexByte(title, '\n'); idx >= 0 {
		title = strings.TrimSpace(title[:idx])
	}
	title = strings.Join(strings.Fields(title), " ")
	runes := []rune(title)
	if len(runes) > 64 {
		return string(runes[:64]) + "..."
	}
	return title
}

func extractIssueTaskDrafts(body string) (string, []issueTaskDraft) {
	normalizedBody := strings.ReplaceAll(body, "\r\n", "\n")
	normalizedBody = strings.ReplaceAll(normalizedBody, "\r", "\n")
	lines := strings.Split(normalizedBody, "\n")
	parentLines := make([]string, 0, len(lines))
	tasks := make([]issueTaskDraft, 0)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		status := ""
		title := ""
		switch {
		case strings.HasPrefix(trimmed, "- [ ] "):
			status = "open"
			title = strings.TrimSpace(trimmed[len("- [ ] "):])
		case strings.HasPrefix(trimmed, "- [x] "):
			status = "closed"
			title = strings.TrimSpace(trimmed[len("- [x] "):])
		case strings.HasPrefix(trimmed, "- [X] "):
			status = "closed"
			title = strings.TrimSpace(trimmed[len("- [X] "):])
		default:
			parentLines = append(parentLines, line)
			continue
		}
		if title == "" {
			parentLines = append(parentLines, line)
			continue
		}
		tasks = append(tasks, issueTaskDraft{Title: title, Status: status})
	}
	return strings.TrimSpace(strings.Join(parentLines, "\n")), tasks
}

func normalizeIssueTaskStrings(values []string) []issueTaskDraft {
	tasks := make([]issueTaskDraft, 0, len(values))
	for _, value := range values {
		title := strings.TrimSpace(value)
		if title != "" {
			tasks = append(tasks, issueTaskDraft{Title: title, Status: "open"})
		}
	}
	return tasks
}

func normalizeIssueTaskInputs(values []IssueTaskInput) []issueTaskDraft {
	tasks := make([]issueTaskDraft, 0, len(values))
	for _, value := range values {
		title := strings.TrimSpace(value.Title)
		body := strings.TrimSpace(value.Body)
		if title == "" {
			title = deriveIssueTitle(body)
		}
		if title == "" {
			continue
		}
		status := normalizeIssueStatus(value.Status)
		if status == "" && value.Completed {
			status = "closed"
		}
		if status == "" {
			status = "open"
		}
		tasks = append(tasks, issueTaskDraft{Title: title, Body: body, Status: status})
	}
	return tasks
}

func normalizeIssueStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "open", "needs_review", "changes_requested", "ready_for_test", "blocked", "closed", "cancelled":
		return value
	case "review", "in_review":
		return "needs_review"
	case "ready_for_testing", "ready_to_test", "waiting_for_test", "awaiting_test":
		return "ready_for_test"
	case "testing", "test_in_progress":
		return "needs_review"
	case "todo", "queued", "running", "in_progress":
		return "open"
	case "done", "completed":
		return "closed"
	default:
		return value
	}
}

func validateIssueStatus(value string) error {
	switch normalizeIssueStatus(value) {
	case "open", "needs_review", "changes_requested", "ready_for_test", "blocked", "closed", "cancelled":
		return nil
	default:
		return fmt.Errorf("unsupported issue status %q", value)
	}
}

func normalizeAssigneeType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "human"
	}
	return value
}

func normalizeHumanName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "me" {
		return "mlhiter"
	}
	return value
}

func issueProjectScore(project Project, text string) int {
	normalizedText := strings.ToLower(text)
	score := 0
	score += projectTokenScore(normalizedText, project.Name, 6)
	score += projectTokenScore(normalizedText, project.GitRepo, 5)
	score += projectTokenScore(normalizedText, project.GitOwner+"/"+project.GitRepo, 7)
	score += projectTokenScore(normalizedText, filepath.Base(project.RepoPath), 4)
	score += projectTokenScore(normalizedText, project.RemoteURL, 2)
	return score
}

func projectTokenScore(text, token string, weight int) int {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" || !strings.Contains(text, token) {
		return 0
	}
	return weight
}

func normalizeIssueLabelKeys(values []string) ([]IssueLabel, error) {
	definitions := builtInIssueLabelDefinitions()
	byKey := map[string]IssueLabelDefinition{}
	for _, definition := range definitions {
		byKey[definition.Key] = definition
	}
	seen := map[string]bool{}
	labels := []IssueLabel{}
	for _, value := range values {
		key := knownIssueLabelKey(value)
		if key == "" {
			continue
		}
		if seen[key] {
			continue
		}
		definition, ok := byKey[key]
		if !ok {
			return nil, fmt.Errorf("unknown issue label %q", value)
		}
		seen[key] = true
		labels = append(labels, IssueLabel{
			ID:        definition.ID,
			LabelID:   definition.ID,
			Key:       definition.Key,
			Name:      definition.Name,
			Dimension: definition.Dimension,
			Color:     definition.Color,
			SortOrder: definition.SortOrder,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		})
	}
	return labels, nil
}

func builtInIssueLabelDefinitions() []IssueLabelDefinition {
	typeKeys := []string{"feat", "fix", "docs", "style", "refactor", "perf", "test", "build", "ci", "chore", "revert"}
	priorityKeys := []string{"p0", "p1", "p2", "p3"}
	definitions := make([]IssueLabelDefinition, 0, len(typeKeys)+len(priorityKeys))
	for index, key := range typeKeys {
		definitions = append(definitions, IssueLabelDefinition{
			ID:        "type-" + key,
			Key:       "type:" + key,
			Name:      key,
			Dimension: issueLabelDimensionType,
			SortOrder: (index + 1) * 10,
			BuiltIn:   true,
		})
	}
	for index, key := range priorityKeys {
		definitions = append(definitions, IssueLabelDefinition{
			ID:        "priority-" + key,
			Key:       "priority:" + key,
			Name:      strings.ToUpper(key),
			Dimension: issueLabelDimensionPriority,
			SortOrder: (index + 1) * 10,
			BuiltIn:   true,
		})
	}
	return definitions
}

func knownIssueLabelKey(value string) string {
	normalized := normalizeIssueLabelKey(value)
	for _, definition := range builtInIssueLabelDefinitions() {
		if definition.Key == normalized {
			return normalized
		}
	}
	return ""
}

func normalizeIssueLabelKey(value string) string {
	label := strings.TrimSpace(strings.TrimPrefix(value, "#"))
	if label == "" {
		return ""
	}
	lower := strings.ToLower(label)
	if strings.Contains(lower, ":") {
		parts := strings.SplitN(lower, ":", 2)
		return strings.TrimSpace(parts[0]) + ":" + strings.TrimSpace(parts[1])
	}
	for _, key := range []string{"feat", "fix", "docs", "style", "refactor", "perf", "test", "build", "ci", "chore", "revert"} {
		if lower == key {
			return "type:" + lower
		}
	}
	for _, key := range []string{"p0", "p1", "p2", "p3"} {
		if lower == key {
			return "priority:" + lower
		}
	}
	return lower
}

func hasIssueLabelDimension(labels []IssueLabel, dimension string) bool {
	for _, label := range labels {
		if label.Dimension == dimension {
			return true
		}
	}
	return false
}

func validateCommentReaction(reaction string) error {
	switch strings.TrimSpace(reaction) {
	case "thumbs_up", "thumbs_down", "laugh", "hooray", "confused", "heart", "rocket", "eyes":
		return nil
	default:
		return fmt.Errorf("unsupported reaction %q", reaction)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
