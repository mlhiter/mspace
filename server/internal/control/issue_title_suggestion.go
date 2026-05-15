package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"
)

const issueTitleSuggestionTimeout = 45 * time.Second

type issueTitleSuggestionPayload struct {
	Title string `json:"title"`
}

func suggestIssueTitle(ctx context.Context, input SuggestIssueTitleInput) SuggestIssueTitleResult {
	explicitTitle := normalizeSuggestedIssueTitle(input.Title)
	if explicitTitle != "" {
		return SuggestIssueTitleResult{Title: explicitTitle, Source: "user"}
	}
	text := issueTitleSuggestionText(input)
	if strings.TrimSpace(text) == "" {
		return SuggestIssueTitleResult{}
	}
	fallback := fallbackIssueTitle(text)
	if title, err := suggestIssueTitleWithCodex(ctx, text); err == nil {
		if normalized := normalizeSuggestedIssueTitle(title); normalized != "" {
			return SuggestIssueTitleResult{Title: normalized, Source: "ai"}
		}
	}
	return SuggestIssueTitleResult{Title: fallback, Source: "fallback"}
}

func issueTitleSuggestionText(input SuggestIssueTitleInput) string {
	if strings.TrimSpace(input.Body) != "" {
		return input.Body
	}
	return input.Prompt
}

func suggestIssueTitleWithCodex(parent context.Context, text string) (string, error) {
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return "", errors.New("codex CLI is not available on PATH")
	}
	ctx, cancel := context.WithTimeout(parent, issueTitleSuggestionTimeout)
	defer cancel()
	client, err := startIssueTitleCodexAppServer(codexPath)
	if err != nil {
		return "", err
	}
	defer client.close()

	var initResp codexInitializeResponse
	if err := client.request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "mspace-title",
			"title":   "mspace title",
			"version": "0.1.0",
		},
		"capabilities": map[string]any{
			"experimentalApi": true,
		},
	}, &initResp); err != nil {
		return "", fmt.Errorf("initialize codex app-server: %w", err)
	}

	var threadResp codexThreadStartResponse
	if err := client.request(ctx, "thread/start", map[string]any{
		"cwd":                    os.TempDir(),
		"approvalPolicy":         "never",
		"approvalsReviewer":      "user",
		"sandbox":                "danger-full-access",
		"developerInstructions":  buildIssueTitleDeveloperInstructions(),
		"personality":            "pragmatic",
		"ephemeral":              true,
		"sessionStartSource":     "startup",
		"serviceName":            "mspace-title",
		"experimentalRawEvents":  false,
		"persistExtendedHistory": false,
	}, &threadResp); err != nil {
		return "", fmt.Errorf("start codex title thread: %w", err)
	}
	if strings.TrimSpace(threadResp.Thread.ID) == "" {
		return "", errors.New("codex app-server returned an empty title thread id")
	}

	var turnResp codexTurnStartResponse
	if err := client.request(ctx, "turn/start", map[string]any{
		"threadId": threadResp.Thread.ID,
		"input": []map[string]any{
			{
				"type":          "text",
				"text":          buildIssueTitlePrompt(text),
				"text_elements": []map[string]any{},
			},
		},
		"cwd":            os.TempDir(),
		"approvalPolicy": "never",
		"sandboxPolicy": map[string]any{
			"type": "dangerFullAccess",
		},
		"responsesapiClientMetadata": map[string]string{
			"mspace.task": "issue_title_suggestion",
		},
	}, &turnResp); err != nil {
		return "", fmt.Errorf("start codex title turn: %w", err)
	}
	if strings.TrimSpace(turnResp.Turn.ID) == "" {
		return "", errors.New("codex app-server returned an empty title turn id")
	}

	message, err := waitCodexTitleTurn(ctx, client, threadResp.Thread.ID, turnResp.Turn.ID)
	if err != nil {
		return "", err
	}
	return parseIssueTitleSuggestion(message)
}

func startIssueTitleCodexAppServer(codexPath string) (*codexAppServerClient, error) {
	return startCodexAppServer(codexPath, os.TempDir())
}

func waitCodexTitleTurn(ctx context.Context, client *codexAppServerClient, threadID, turnID string) (string, error) {
	var lastAgentMessage string
	for {
		select {
		case <-ctx.Done():
			interruptCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = client.request(interruptCtx, "turn/interrupt", map[string]any{
				"threadId": threadID,
				"turnId":   turnID,
			}, nil)
			cancel()
			return "", ctx.Err()
		case notification, ok := <-client.notifications:
			if !ok {
				return "", errors.New("codex app-server exited before title suggestion completed")
			}
			switch notification.Method {
			case "item/completed":
				var payload codexItemNotification
				if decodeCodexParams(notification.Params, &payload) == nil && payload.ThreadID == threadID && payload.TurnID == turnID && payload.Item.Type == "agentMessage" {
					if strings.TrimSpace(payload.Item.Text) != "" {
						lastAgentMessage = payload.Item.Text
					}
				}
			case "error":
				var payload codexErrorNotification
				if decodeCodexParams(notification.Params, &payload) == nil && payload.ThreadID == threadID && payload.TurnID == turnID && !payload.WillRetry {
					message := payload.Error.Error()
					if message == "" {
						message = "Codex app-server reported an unknown title suggestion error."
					}
					return "", errors.New(message)
				}
			case "turn/completed":
				var payload codexTurnNotification
				if decodeCodexParams(notification.Params, &payload) == nil && payload.ThreadID == threadID && payload.Turn.ID == turnID {
					if payload.Turn.Status != "completed" {
						if payload.Turn.Error != nil && payload.Turn.Error.Error() != "" {
							return "", errors.New(payload.Turn.Error.Error())
						}
						return "", fmt.Errorf("codex title turn ended with status %s", payload.Turn.Status)
					}
					if strings.TrimSpace(lastAgentMessage) == "" {
						return "", errors.New("codex title suggestion returned an empty response")
					}
					return lastAgentMessage, nil
				}
			}
		}
	}
}

func buildIssueTitleDeveloperInstructions() string {
	return strings.TrimSpace(`
You write concise mspace issue titles for engineering work.

Rules:
- Return only one compact JSON object.
- Do not wrap the JSON in Markdown.
- Use the same language as the user's issue note when it is clear.
- Keep the title action-oriented, specific, and under 72 characters.
- Do not include raw Markdown links, URLs, brackets, quotes, trailing punctuation, or labels like "Issue:".
- Do not edit files or run commands.
`)
}

func buildIssueTitlePrompt(text string) string {
	var builder strings.Builder
	builder.WriteString("# Issue Title Suggestion\n\n")
	builder.WriteString("Return exactly this JSON shape:\n")
	builder.WriteString(`{"title":"Improve orcai page UI"}`)
	builder.WriteString("\n\n")
	builder.WriteString("## Issue note\n\n")
	builder.WriteString(strings.TrimSpace(text))
	return builder.String()
}

func parseIssueTitleSuggestion(value string) (string, error) {
	raw := strings.TrimSpace(value)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return "", errors.New("title response did not contain a JSON object")
	}
	raw = raw[start : end+1]
	var result issueTitleSuggestionPayload
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return "", fmt.Errorf("parse title JSON: %w", err)
	}
	return normalizeSuggestedIssueTitle(result.Title), nil
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
