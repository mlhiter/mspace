package control

import (
	"context"
	"net/url"
	"path"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var issueTitleMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

func suggestIssueTitle(_ context.Context, input SuggestIssueTitleInput) SuggestIssueTitleResult {
	explicitTitle := normalizePlainSuggestedIssueTitle(input.Title)
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
	return normalizeSuggestedIssueTitleText(plainIssueTitleFromMarkdown(title))
}

func normalizePlainSuggestedIssueTitle(title string) string {
	return normalizeSuggestedIssueTitleText(plainIssueTitleFromText(title))
}

func normalizeSuggestedIssueTitleText(title string) string {
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

func plainIssueTitleFromMarkdown(value string) string {
	title := firstNonEmptyIssueTitleLine(value)
	if title == "" {
		return ""
	}
	source := []byte(title)
	document := issueTitleMarkdown.Parser().Parse(text.NewReader(source))
	var plain strings.Builder
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := node.(type) {
		case *ast.Text:
			value := node.Value(source)
			if _, inCodeSpan := node.Parent().(*ast.CodeSpan); !inCodeSpan {
				value = visibleIssueTitleText(value)
			}
			plain.Write(value)
		case *ast.String:
			plain.Write(visibleIssueTitleText(node.Value))
		case *ast.AutoLink:
			plain.Write(node.Label(source))
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return plainIssueTitleFromText(plain.String())
}

func plainIssueTitleFromText(value string) string {
	return strings.Join(strings.Fields(firstNonEmptyIssueTitleLine(value)), " ")
}

func visibleIssueTitleText(value []byte) []byte {
	value = util.UnescapePunctuations(value)
	value = util.ResolveNumericReferences(value)
	return util.ResolveEntityNames(value)
}

func firstNonEmptyIssueTitleLine(value string) string {
	for start := 0; start <= len(value); {
		endOffset := strings.IndexAny(value[start:], "\r\n")
		if endOffset < 0 {
			return strings.TrimSpace(value[start:])
		}
		end := start + endOffset
		if line := strings.TrimSpace(value[start:end]); line != "" {
			return line
		}
		start = end + 1
		if value[end] == '\r' && start < len(value) && value[start] == '\n' {
			start++
		}
	}
	return ""
}
