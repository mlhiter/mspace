package control

import (
	"context"
	"net/url"
	"path"
	"strings"
)

func suggestIssueTitle(_ context.Context, input SuggestIssueTitleInput) SuggestIssueTitleResult {
	explicitTitle := normalizeSuggestedIssueTitle(input.Title)
	if explicitTitle != "" {
		return SuggestIssueTitleResult{Title: explicitTitle, Source: "user"}
	}
	text := issueTitleSuggestionText(input)
	if strings.TrimSpace(text) == "" {
		return SuggestIssueTitleResult{}
	}
	return SuggestIssueTitleResult{Title: fallbackIssueTitle(text), Source: "fallback"}
}

func issueTitleSuggestionText(input SuggestIssueTitleInput) string {
	if strings.TrimSpace(input.Body) != "" {
		return input.Body
	}
	return input.Prompt
}

func fallbackIssueTitle(text string) string {
	repoTitle := fallbackGitHubRepoTitle(text)
	firstLineTitle := fallbackFirstLineTitle(text)
	switch {
	case repoTitle != "" && firstLineTitle != "":
		lowerFirst := strings.ToLower(firstLineTitle)
		if strings.Contains(lowerFirst, "优化") || strings.Contains(lowerFirst, "页面") || strings.Contains(lowerFirst, "ui") || strings.Contains(lowerFirst, "page") {
			return normalizeSuggestedIssueTitle(repoTitle + " page optimization")
		}
	case repoTitle != "":
		return normalizeSuggestedIssueTitle(repoTitle + " issue")
	}
	return normalizeSuggestedIssueTitle(firstLineTitle)
}

func fallbackGitHubRepoTitle(text string) string {
	fields := strings.Fields(strings.NewReplacer("(", " ", ")", " ", "[", " ", "]", " ", "\n", " ").Replace(text))
	for _, field := range fields {
		candidate := strings.Trim(field, ".,;:!?\"'`<>")
		if !strings.Contains(candidate, "github.com/") {
			continue
		}
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.Host == "" {
			if !strings.HasPrefix(candidate, "http://") && !strings.HasPrefix(candidate, "https://") {
				parsed, err = url.Parse("https://" + candidate)
			}
		}
		if err != nil || !strings.EqualFold(parsed.Host, "github.com") {
			continue
		}
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) < 2 {
			continue
		}
		repo := strings.TrimSuffix(parts[1], ".git")
		repo = strings.TrimSpace(repo)
		if repo != "" {
			return repo
		}
	}
	return ""
}

func fallbackFirstLineTitle(text string) string {
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if github := fallbackGitHubRepoTitle(line); github != "" && strings.Contains(line, "github.com/") {
			continue
		}
		return line
	}
	return strings.TrimSpace(text)
}

func normalizeSuggestedIssueTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	title = strings.Trim(title, "\"'` ")
	title = strings.TrimPrefix(title, "#")
	title = strings.TrimSpace(title)
	title = strings.Join(strings.Fields(title), " ")
	title = strings.Trim(title, ".,;:!? ")
	title = strings.Trim(path.Clean("/"+title), "/")
	if title == "." {
		return ""
	}
	runes := []rune(title)
	if len(runes) > 72 {
		return string(runes[:72])
	}
	return title
}
