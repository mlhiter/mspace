package control

import (
	"context"
	"sync"
	"testing"
)

func TestPlainIssueTitleFromMarkdown(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "bold Chinese issue title",
			input: "**应用管理，一开始选择私有化镜像，后变更改成公开镜像后，imagePullSecret 不会删除，报错。**",
			want:  "应用管理，一开始选择私有化镜像，后变更改成公开镜像后，imagePullSecret 不会删除，报错。",
		},
		{
			name:  "mixed inline formatting",
			input: "## Fix **private images** and [`imagePullSecrets`](https://kubernetes.io/docs)",
			want:  "Fix private images and imagePullSecrets",
		},
		{
			name:  "task list prefix",
			input: "- [ ] **Remove stale image pull secret**",
			want:  "Remove stale image pull secret",
		},
		{
			name:  "literal wildcard",
			input: "Keep *.yaml matching",
			want:  "Keep *.yaml matching",
		},
		{
			name:  "underscores inside identifier",
			input: "Keep image_pull_secret unchanged",
			want:  "Keep image_pull_secret unchanged",
		},
		{
			name:  "first non-empty line",
			input: "\r\n\r\n> ~~Old~~ title\r\nBody stays separate",
			want:  "Old title",
		},
		{
			name:  "escaped formatting characters",
			input: `Use \*literal\* in glob`,
			want:  "Use *literal* in glob",
		},
		{
			name:  "link destination with parentheses",
			input: `[spec](https://example.com/a_(b)) failed`,
			want:  "spec failed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := plainIssueTitleFromMarkdown(test.input); got != test.want {
				t.Fatalf("plainIssueTitleFromMarkdown() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPlainIssueTitleFromTextPreservesLiteralMarkup(t *testing.T) {
	tests := []string{
		"Fix <select> rendering",
		"Use *literal* in glob",
		"[repo](https://github.com/org/repo) optimization",
	}
	for _, input := range tests {
		if got := plainIssueTitleFromText(input); got != input {
			t.Fatalf("plainIssueTitleFromText(%q) = %q", input, got)
		}
	}
}

func TestPlainIssueTitleFromMarkdownConcurrent(t *testing.T) {
	const workers = 32
	var wait sync.WaitGroup
	errors := make(chan string, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			got := plainIssueTitleFromMarkdown(`## Fix **private images** and [spec](https://example.com/a_(b))`)
			if got != "Fix private images and spec" {
				errors <- got
			}
		}()
	}
	wait.Wait()
	close(errors)
	for got := range errors {
		t.Fatalf("unexpected concurrent title result %q", got)
	}
}

func TestConditionalIssueTitleUpdatePreservesConcurrentEdit(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "title-editor",
		Login:          "title-editor",
		Name:           "Title Editor",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	issueID, err := store.CreateIssue(context.Background(), user, workspaces[0].ID, CreateIssueInput{
		Title: "Draft title",
		Body:  "Original body",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	manualTitle := "Manual title"
	manualBody := "Manually updated body"
	if _, err := store.UpdateIssue(context.Background(), user.ID, workspaces[0].ID, issueID, UpdateIssueInput{
		Title: &manualTitle,
		Body:  &manualBody,
	}); err != nil {
		t.Fatalf("manual issue update: %v", err)
	}
	expectedTitle := "Draft title"
	suggestedTitle := "Suggested title"
	updated, err := store.UpdateIssue(context.Background(), user.ID, workspaces[0].ID, issueID, UpdateIssueInput{
		Title:         &suggestedTitle,
		ExpectedTitle: &expectedTitle,
	})
	if err != nil {
		t.Fatalf("conditional title update: %v", err)
	}
	if updated.Title != manualTitle || updated.Body != manualBody {
		t.Fatalf("expected concurrent manual edit to win, got title=%q body=%q", updated.Title, updated.Body)
	}
	expectedTitle = manualTitle
	updated, err = store.UpdateIssue(context.Background(), user.ID, workspaces[0].ID, issueID, UpdateIssueInput{
		Title:         &suggestedTitle,
		ExpectedTitle: &expectedTitle,
	})
	if err != nil {
		t.Fatalf("matching conditional title update: %v", err)
	}
	if updated.Title != suggestedTitle || updated.Body != manualBody {
		t.Fatalf("expected matching title update to preserve body, got title=%q body=%q", updated.Title, updated.Body)
	}
	formattedTitle := "Fix <select> and *literal*"
	updated, err = store.UpdateIssue(context.Background(), user.ID, workspaces[0].ID, issueID, UpdateIssueInput{
		Title: &formattedTitle,
	})
	if err != nil {
		t.Fatalf("plain title update: %v", err)
	}
	if updated.Title != formattedTitle || updated.Body != manualBody {
		t.Fatalf("expected ordinary title update to store plain text only, got title=%q body=%q", updated.Title, updated.Body)
	}
}

func TestNormalizeIssueTaskTitlesAsPlainText(t *testing.T) {
	tasks := normalizeIssueTaskStrings([]string{"Remove <select> and *literal*"})
	if len(tasks) != 1 || tasks[0].Title != "Remove <select> and *literal*" {
		t.Fatalf("expected string task title to be plain text, got %+v", tasks)
	}
	tasks = normalizeIssueTaskInputs([]IssueTaskInput{{
		Body: "[Review deployment](https://example.com/runbook)\n\nKeep task body Markdown: **important**",
	}})
	if len(tasks) != 1 || tasks[0].Title != "Review deployment" || tasks[0].Body != "[Review deployment](https://example.com/runbook)\n\nKeep task body Markdown: **important**" {
		t.Fatalf("expected child issue title only to be normalized, got %+v", tasks)
	}
}

func TestNormalizeCreateIssueInputKeepsMarkdownInBodyOnly(t *testing.T) {
	body := "**Fix private image handling**\n\nKeep the formatted issue description."
	normalized, _, _, err := normalizeCreateIssueInput(CreateIssueInput{
		Body: body,
	}, User{ID: "user-1", Name: "Issue User"})
	if err != nil {
		t.Fatalf("normalize create issue input: %v", err)
	}
	if normalized.Title != "Fix private image handling" {
		t.Fatalf("expected a plain title, got %q", normalized.Title)
	}
	if normalized.Body != body {
		t.Fatalf("expected Markdown body to remain unchanged, got %q", normalized.Body)
	}

	normalized, _, _, err = normalizeCreateIssueInput(CreateIssueInput{
		Title: "**Fix private image handling**",
		Body:  body,
	}, User{ID: "user-1", Name: "Issue User"})
	if err != nil {
		t.Fatalf("normalize legacy create issue input: %v", err)
	}
	if normalized.Title != "Fix private image handling" {
		t.Fatalf("expected legacy Markdown draft title to be normalized, got %q", normalized.Title)
	}

	literalTitle := "Fix <select> and *literal*"
	normalized, _, _, err = normalizeCreateIssueInput(CreateIssueInput{
		Title:       literalTitle,
		TitleSource: "plain_text",
		Body:        "Fix `<select>` and \\*literal\\*",
	}, User{ID: "user-1", Name: "Issue User"})
	if err != nil {
		t.Fatalf("normalize create issue input with explicit title: %v", err)
	}
	if normalized.Title != literalTitle {
		t.Fatalf("expected explicit title to remain plain text, got %q", normalized.Title)
	}

	literalTitle = "[repo](https://github.com/org/repo) optimization"
	normalized, _, _, err = normalizeCreateIssueInput(CreateIssueInput{
		Title:       literalTitle,
		TitleSource: "plain_text",
		Body:        literalTitle,
	}, User{ID: "user-1", Name: "Issue User"})
	if err != nil {
		t.Fatalf("normalize plain link-like title: %v", err)
	}
	if normalized.Title != literalTitle {
		t.Fatalf("expected link-like plain title to remain unchanged, got %q", normalized.Title)
	}
}

func TestFallbackIssueTitleRemovesMarkdownFormatting(t *testing.T) {
	title := fallbackIssueTitle("**Fix private image handling**\n\nMore detail")
	if title != "Fix private image handling" {
		t.Fatalf("expected a plain fallback title, got %q", title)
	}
}
