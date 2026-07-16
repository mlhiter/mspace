package control

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

func TestIssueTypeTriageGeneratedTitlePreservesManualEdit(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "triage-title-editor",
		Login:          "triage-title-editor",
		Name:           "Triage Title Editor",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	issueID, err := store.CreateIssue(context.Background(), user, workspaces[0].ID, CreateIssueInput{
		Title:       "Draft title",
		TitleSource: issueTitleSourcePlainText,
		Body:        "Describe the issue in detail.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	manualTitle := "Human title"
	if _, err := store.UpdateIssue(context.Background(), user.ID, workspaces[0].ID, issueID, UpdateIssueInput{Title: &manualTitle}); err != nil {
		t.Fatalf("update issue title: %v", err)
	}

	task := RuntimeTask{
		WorkspaceID: workspaces[0].ID,
		IssueID:     issueID,
		Status:      "completed",
		Payload:     json.RawMessage(`{"expectedTitle":"Draft title"}`),
		Result:      json.RawMessage(`{"title":"Generated title","type":"fix","confidence":0.9,"reason":"bug fix"}`),
	}
	store.mu.Lock()
	store.reconcileIssueTypeTriageRuntimeResultLocked(task)
	store.mu.Unlock()

	detail, err := store.GetIssue(context.Background(), user.ID, workspaces[0].ID, issueID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if detail.Issue.Title != manualTitle {
		t.Fatalf("expected manual title to win, got %q", detail.Issue.Title)
	}
	if detail.Issue.TriageStatus != "classified" {
		t.Fatalf("expected type triage to complete, got %q", detail.Issue.TriageStatus)
	}
}

func TestIssueTypeTriageGeneratedTitleSurvivesManualTypeSelection(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "triage-title-label-editor",
		Login:          "triage-title-label-editor",
		Name:           "Triage Title Label Editor",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	issueID, err := store.CreateIssue(context.Background(), user, workspaces[0].ID, CreateIssueInput{
		Title:       "Draft title",
		TitleSource: issueTitleSourcePlainText,
		Body:        "Describe the issue in detail.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if _, err := store.UpdateIssueLabels(context.Background(), user.ID, workspaces[0].ID, issueID, UpdateIssueLabelsInput{LabelKeys: []string{"type:docs"}}); err != nil {
		t.Fatalf("select issue type: %v", err)
	}

	task := RuntimeTask{
		WorkspaceID: workspaces[0].ID,
		IssueID:     issueID,
		Status:      "completed",
		Payload:     json.RawMessage(`{"expectedTitle":"Draft title"}`),
		Result:      json.RawMessage(`{"title":"Generated title","type":"fix","confidence":0.9,"reason":"bug fix"}`),
	}
	store.mu.Lock()
	store.reconcileIssueTypeTriageRuntimeResultLocked(task)
	store.mu.Unlock()

	detail, err := store.GetIssue(context.Background(), user.ID, workspaces[0].ID, issueID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if detail.Issue.Title != "Generated title" {
		t.Fatalf("expected generated title after manual type selection, got %q", detail.Issue.Title)
	}
	if len(detail.Labels) != 1 || detail.Labels[0].Key != "type:docs" {
		t.Fatalf("expected manual type label to win, got %+v", detail.Labels)
	}
}

func TestIssueTypeTriagePayloadUsesCapturedDraftTitle(t *testing.T) {
	body := "First paragraph with the symptom.\n\nSecond paragraph with the unique root-cause context."
	payload := buildIssueTypeTriagePayload(issueTypeTriageDetail{Issue: Issue{Title: "Human title", Body: body}}, "Draft title")
	if payload["expectedTitle"] != "Draft title" {
		t.Fatalf("expected explicitly captured draft title in payload, got %#v", payload["expectedTitle"])
	}
	prompt, _ := payload["prompt"].(string)
	if !strings.Contains(prompt, "First paragraph with the symptom.") || !strings.Contains(prompt, "Second paragraph with the unique root-cause context.") {
		t.Fatalf("expected triage prompt to include the full issue body, got %q", prompt)
	}
}

func TestParseIssueTypeTriageResultStripsMarkdownFromGeneratedTitle(t *testing.T) {
	result, err := parseIssueTypeTriageResult(`{"title":"**修复镜像凭据残留**","type":"fix","confidence":0.9,"reason":"bug fix"}`)
	if err != nil {
		t.Fatalf("parse issue type triage result: %v", err)
	}
	if result.Title != "修复镜像凭据残留" {
		t.Fatalf("expected plain generated title, got %q", result.Title)
	}
}

func TestParseIssueTypeTriageResultLimitsGeneratedTitleTo72Runes(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"title":      strings.Repeat("界", 80),
		"type":       "fix",
		"confidence": 0.9,
		"reason":     "bug fix",
	})
	if err != nil {
		t.Fatalf("marshal triage result: %v", err)
	}
	result, err := parseIssueTypeTriageResult(string(payload))
	if err != nil {
		t.Fatalf("parse issue type triage result: %v", err)
	}
	if got := len([]rune(result.Title)); got != 72 {
		t.Fatalf("expected 72 runes, got %d in %q", got, result.Title)
	}
}

func TestEnsureIssueTypeTriageTaskUpgradesFallbackWithoutDuplicate(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "triage-task-upgrade",
		Login:          "triage-task-upgrade",
		Name:           "Triage Task Upgrade",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	issueID, err := store.CreateIssue(context.Background(), user, workspaces[0].ID, CreateIssueInput{
		Title:       "Draft title",
		TitleSource: issueTitleSourcePlainText,
		Body:        "Full issue details.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	server := NewServer(Config{}, store, fakeGitHubClient{})
	if err := server.enqueueIssueTypeTriage(context.Background(), user.ID, workspaces[0].ID, issueID, ""); err != nil {
		t.Fatalf("enqueue fallback triage: %v", err)
	}
	if err := server.enqueueIssueTypeTriage(context.Background(), user.ID, workspaces[0].ID, issueID, "Draft title"); err != nil {
		t.Fatalf("upgrade fallback triage: %v", err)
	}
	tasks, err := store.ListRuntimeTasks(context.Background(), user.ID, workspaces[0].ID)
	if err != nil {
		t.Fatalf("list runtime tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one triage task, got %+v", tasks)
	}
	if got := issueTypeTriageExpectedTitle(tasks[0]); got != "Draft title" {
		t.Fatalf("expected fallback task to capture the draft, got %q in %s", got, tasks[0].Payload)
	}
}

func TestEnsureIssueTypeTriageTaskIsAtomicInMemoryStore(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "triage-task-concurrency",
		Login:          "triage-task-concurrency",
		Name:           "Triage Task Concurrency",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	issueID, err := store.CreateIssue(context.Background(), user, workspaces[0].ID, CreateIssueInput{
		Title:       "Draft title",
		TitleSource: issueTitleSourcePlainText,
		Body:        "Full issue details.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	server := NewServer(Config{}, store, fakeGitHubClient{})
	const workers = 32
	var wait sync.WaitGroup
	errorsSeen := make(chan error, workers)
	for index := range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			expectedTitle := ""
			if index%2 == 0 {
				expectedTitle = "Draft title"
			}
			err := server.enqueueIssueTypeTriage(context.Background(), user.ID, workspaces[0].ID, issueID, expectedTitle)
			if err != nil && !errors.Is(err, errIssueTypeTriageNotNeeded) {
				errorsSeen <- err
			}
		}()
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Fatalf("concurrent enqueue failed: %v", err)
	}
	tasks, err := store.ListRuntimeTasks(context.Background(), user.ID, workspaces[0].ID)
	if err != nil {
		t.Fatalf("list runtime tasks: %v", err)
	}
	if len(tasks) != 1 || issueTypeTriageExpectedTitle(tasks[0]) != "Draft title" {
		t.Fatalf("expected one upgraded triage task, got %+v", tasks)
	}
}

func TestEnsureIssueTypeTriageTaskRechecksClassifiedIssueBeforeInsert(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "triage-task-stale-read",
		Login:          "triage-task-stale-read",
		Name:           "Triage Task Stale Read",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	workspaceID := workspaces[0].ID
	issueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{
		Title:       "Draft title",
		TitleSource: issueTitleSourcePlainText,
		Body:        "Full issue details.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := store.ApplyIssueTypeClassification(context.Background(), workspaceID, issueID, "type:fix"); err != nil {
		t.Fatalf("classify issue before stale enqueue: %v", err)
	}
	payload, err := json.Marshal(buildIssueTypeTriagePayload(issueTypeTriageDetail{Issue: Issue{ID: issueID, WorkspaceID: workspaceID}}, "Draft title"))
	if err != nil {
		t.Fatalf("marshal triage payload: %v", err)
	}
	server := NewServer(Config{}, store, fakeGitHubClient{})
	err = server.ensureIssueTypeTriageTask(context.Background(), user.ID, workspaceID, issueID, "Draft title", CreateRuntimeTaskInput{
		IssueID:              issueID,
		Kind:                 "issue_type_triage",
		RuntimeMode:          workspaces[0].Kind,
		RequiredCapabilities: json.RawMessage(`{"codex":true}`),
		Payload:              payload,
	})
	if !errors.Is(err, errIssueTypeTriageNotNeeded) {
		t.Fatalf("expected stale enqueue to be skipped, got %v", err)
	}
	tasks, err := store.ListRuntimeTasks(context.Background(), user.ID, workspaceID)
	if err != nil {
		t.Fatalf("list runtime tasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected no duplicate triage task after classification, got %+v", tasks)
	}
}

func TestIssueTypeTriageStateSurvivesSQLiteSnapshotRoundTrip(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "triage-snapshot",
		Login:          "triage-snapshot",
		Name:           "Triage Snapshot",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	workspaceID := workspaces[0].ID
	issueID, err := store.CreateIssue(context.Background(), user, workspaceID, CreateIssueInput{
		Title:       "Draft title",
		TitleSource: issueTitleSourcePlainText,
		Body:        "Full issue details.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	server := NewServer(Config{}, store, fakeGitHubClient{})
	if err := server.enqueueIssueTypeTriage(context.Background(), user.ID, workspaceID, issueID, "Draft title"); err != nil {
		t.Fatalf("enqueue issue triage: %v", err)
	}
	restored := roundTripMemoryStoreSnapshot(t, store)
	tasks, err := restored.ListRuntimeTasks(context.Background(), user.ID, workspaceID)
	if err != nil {
		t.Fatalf("list restored runtime tasks: %v", err)
	}
	if len(tasks) != 1 || issueTypeTriageExpectedTitle(tasks[0]) != "Draft title" {
		t.Fatalf("expected captured draft in restored task, got %+v", tasks)
	}
	task := tasks[0]
	task.Status = "completed"
	task.Result = json.RawMessage(`{"title":"**Generated title**","type":"fix","confidence":0.9,"reason":"bug fix"}`)
	restored.mu.Lock()
	restored.runtimeTasks[task.ID] = task
	restored.reconcileIssueTypeTriageRuntimeResultLocked(task)
	restored.mu.Unlock()
	restored = roundTripMemoryStoreSnapshot(t, restored)
	detail, err := restored.GetIssue(context.Background(), user.ID, workspaceID, issueID)
	if err != nil {
		t.Fatalf("get restored issue: %v", err)
	}
	if detail.Issue.Title != "Generated title" || detail.Issue.TriageStatus != "classified" || len(detail.Labels) != 1 || detail.Labels[0].Key != "type:fix" {
		t.Fatalf("unexpected restored triage result: %+v labels=%+v", detail.Issue, detail.Labels)
	}
}

func roundTripMemoryStoreSnapshot(t *testing.T, store *MemoryStore) *MemoryStore {
	t.Helper()
	payload, err := store.snapshotJSON()
	if err != nil {
		t.Fatalf("snapshot memory store: %v", err)
	}
	var snapshot memoryStoreSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatalf("parse memory store snapshot: %v", err)
	}
	restored := NewMemoryStore()
	restored.restoreSnapshot(snapshot)
	return restored
}

func TestIssueTypeTriageFailureKeepsDraftTitle(t *testing.T) {
	for _, test := range []struct {
		name   string
		status string
		result json.RawMessage
	}{
		{name: "failed", status: "failed", result: json.RawMessage(`{"title":"Generated title","type":"fix"}`)},
		{name: "cancelled", status: "cancelled", result: json.RawMessage(`{"title":"Generated title","type":"fix"}`)},
		{name: "malformed", status: "completed", result: json.RawMessage(`{"title":`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := NewMemoryStore()
			user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
				Provider:       "github",
				ProviderUserID: "triage-failure-" + test.name,
				Login:          "triage-failure-" + test.name,
				Name:           "Triage Failure",
			})
			if err != nil {
				t.Fatalf("upsert identity: %v", err)
			}
			issueID, err := store.CreateIssue(context.Background(), user, workspaces[0].ID, CreateIssueInput{Title: "Draft title", Body: "Issue body"})
			if err != nil {
				t.Fatalf("create issue: %v", err)
			}
			store.mu.Lock()
			store.reconcileIssueTypeTriageRuntimeResultLocked(RuntimeTask{
				WorkspaceID: workspaces[0].ID,
				IssueID:     issueID,
				Status:      test.status,
				Payload:     json.RawMessage(`{"expectedTitle":"Draft title"}`),
				Result:      test.result,
			})
			store.mu.Unlock()
			detail, err := store.GetIssue(context.Background(), user.ID, workspaces[0].ID, issueID)
			if err != nil {
				t.Fatalf("get issue: %v", err)
			}
			if detail.Issue.Title != "Draft title" {
				t.Fatalf("expected failed triage to keep draft, got %q", detail.Issue.Title)
			}
		})
	}
}

func TestIssueTypeTriageLegacyResultKeepsDraftAndClassifiesType(t *testing.T) {
	store := NewMemoryStore()
	user, workspaces, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "legacy-titleless-triage",
		Login:          "legacy-titleless-triage",
		Name:           "Legacy Titleless Triage",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	issueID, err := store.CreateIssue(context.Background(), user, workspaces[0].ID, CreateIssueInput{
		Title:       "Draft title",
		TitleSource: issueTitleSourcePlainText,
		Body:        "Describe the issue in detail.",
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	store.mu.Lock()
	store.reconcileIssueTypeTriageRuntimeResultLocked(RuntimeTask{
		WorkspaceID: workspaces[0].ID,
		IssueID:     issueID,
		Status:      "completed",
		Payload:     json.RawMessage(`{"expectedTitle":"Draft title"}`),
		Result:      json.RawMessage(`{"type":"fix","confidence":0.9,"reason":"bug fix"}`),
	})
	store.mu.Unlock()
	detail, err := store.GetIssue(context.Background(), user.ID, workspaces[0].ID, issueID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if detail.Issue.Title != "Draft title" || detail.Issue.TriageStatus != "classified" || len(detail.Labels) != 1 || detail.Labels[0].Key != "type:fix" {
		t.Fatalf("unexpected legacy triage reconciliation: %+v labels=%+v", detail.Issue, detail.Labels)
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
