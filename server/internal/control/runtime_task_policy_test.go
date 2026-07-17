package control

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNormalizeCreateRuntimeTaskInputAllowsOnlyUnboundDebugTasks(t *testing.T) {
	for _, kind := range []string{"noop", "protocol_smoke"} {
		normalized, err := normalizeCreateRuntimeTaskInput(CreateRuntimeTaskInput{
			Kind:                 kind,
			RuntimeMode:          "personal",
			RequiredCapabilities: json.RawMessage(`{"protocolSmoke":true}`),
			Payload:              json.RawMessage(`{"prompt":"smoke"}`),
		})
		if err != nil {
			t.Fatalf("normalize public %s task: %v", kind, err)
		}
		if normalized.Kind != kind || normalized.ServerManaged {
			t.Fatalf("unexpected normalized public task: %+v", normalized)
		}
	}

	for _, test := range []struct {
		name  string
		input CreateRuntimeTaskInput
		want  string
	}{
		{
			name:  "agent session",
			input: CreateRuntimeTaskInput{Kind: "agent_session", RuntimeMode: "personal", RequiredCapabilities: json.RawMessage(`{"pi":true}`), Payload: json.RawMessage(`{"agentEngine":"pi"}`)},
			want:  "server-managed",
		},
		{
			name:  "triage workflow",
			input: CreateRuntimeTaskInput{Kind: "issue_type_triage", RuntimeMode: "personal", RequiredCapabilities: json.RawMessage(`{"codex":true}`), Payload: json.RawMessage(`{}`)},
			want:  "server-managed",
		},
		{
			name:  "import mapping workflow",
			input: CreateRuntimeTaskInput{Kind: "test_case_import_mapping", RuntimeMode: "personal", RequiredCapabilities: json.RawMessage(`{"codex":true}`), Payload: json.RawMessage(`{}`)},
			want:  "server-managed",
		},
		{
			name:  "unknown kind",
			input: CreateRuntimeTaskInput{Kind: "custom_exec", RuntimeMode: "personal", RequiredCapabilities: json.RawMessage(`{}`), Payload: json.RawMessage(`{}`)},
			want:  "server-managed",
		},
		{
			name:  "product binding",
			input: CreateRuntimeTaskInput{IssueID: "issue-1", Kind: "noop", RuntimeMode: "personal", RequiredCapabilities: json.RawMessage(`{}`), Payload: json.RawMessage(`{}`)},
			want:  "cannot bind",
		},
		{
			name:  "automation payload",
			input: CreateRuntimeTaskInput{Kind: "noop", RuntimeMode: "personal", RequiredCapabilities: json.RawMessage(`{}`), Payload: json.RawMessage(`{"automation":"test_run_execution"}`)},
			want:  "automation is server-managed",
		},
		{
			name:  "skill bundle payload",
			input: CreateRuntimeTaskInput{Kind: "noop", RuntimeMode: "personal", RequiredCapabilities: json.RawMessage(`{}`), Payload: json.RawMessage(`{"requiredSkills":[{"slug":"think"}]}`)},
			want:  "requiredSkills is server-managed",
		},
		{
			name:  "mixed case control payload",
			input: CreateRuntimeTaskInput{Kind: "noop", RuntimeMode: "personal", RequiredCapabilities: json.RawMessage(`{}`), Payload: json.RawMessage(`{"Automation":"test_run_execution"}`)},
			want:  "automation is server-managed",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := normalizeCreateRuntimeTaskInput(test.input); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected error containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestMemoryStoreRawRuntimeTaskRequiresWorkspaceManager(t *testing.T) {
	store := NewMemoryStore()
	owner, _, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "raw-task-owner",
		Login:          "raw-task-owner",
		Name:           "Raw Task Owner",
	})
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, _, err := store.CreateWorkspace(context.Background(), owner.ID, CreateWorkspaceInput{Name: "Raw Task Team", Kind: "team"})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	member, _, err := store.UpsertIdentity(context.Background(), IdentityProfile{
		Provider:       "github",
		ProviderUserID: "raw-task-member",
		Login:          "raw-task-member",
		Name:           "Raw Task Member",
	})
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	store.mu.Lock()
	store.workspaceMembers[workspace.ID][member.ID] = "member"
	store.mu.Unlock()

	debugInput := CreateRuntimeTaskInput{
		Kind:                 "noop",
		RuntimeMode:          "team",
		RequiredCapabilities: json.RawMessage(`{"protocolSmoke":true}`),
		Payload:              json.RawMessage(`{"prompt":"smoke"}`),
	}
	if _, err := store.CreateRuntimeTask(context.Background(), member.ID, workspace.ID, debugInput); !errors.Is(err, ErrForbidden) {
		t.Fatalf("workspace member should not create raw tasks, got %v", err)
	}
	if _, err := store.CreateRuntimeTask(context.Background(), owner.ID, workspace.ID, debugInput); err != nil {
		t.Fatalf("workspace owner should create a bounded debug task: %v", err)
	}
	if _, err := store.CreateRuntimeTask(context.Background(), owner.ID, workspace.ID, CreateRuntimeTaskInput{
		Kind:                 "issue_type_triage",
		RuntimeMode:          "team",
		RequiredCapabilities: json.RawMessage(`{"codex":true}`),
		Payload:              json.RawMessage(`{}`),
	}); err == nil || !strings.Contains(err.Error(), "server-managed") {
		t.Fatalf("even owners must not create system workflow tasks through the raw Store API, got %v", err)
	}
}

func TestPostgresStoreRejectsRawSystemTaskBeforeDatabaseAccess(t *testing.T) {
	store := &PostgresStore{}
	_, err := store.CreateRuntimeTask(context.Background(), "user", "workspace", CreateRuntimeTaskInput{
		Kind:                 "issue_type_triage",
		RuntimeMode:          "team",
		RequiredCapabilities: json.RawMessage(`{"codex":true}`),
		Payload:              json.RawMessage(`{}`),
	})
	if err == nil || !strings.Contains(err.Error(), "server-managed") {
		t.Fatalf("Postgres raw task path should fail before opening a transaction, got %v", err)
	}
}
