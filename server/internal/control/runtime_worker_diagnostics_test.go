package control

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeAgentEngineDiagnostics(t *testing.T) {
	longVersion := strings.Repeat("v", 140)
	payload := json.RawMessage(`{
		"codex":{"status":"READY","reasonCode":" auth_ok ","version":"` + longVersion + `","checkedAt":"2026-07-17T16:00:00+08:00","secret":"do-not-store"},
		"claude_code":{"status":"needs_setup","reasonCode":"unknown_reason","version":"/Users/example/.claude/raw-error.log","checkedAt":"not-a-time"},
		"pi":{"status":"unverified","reasonCode":"probe_unsupported","version":"https://example.invalid/leak","checkedAt":"2026-07-17T08:00:00.123456789Z"},
		"unknown":{"status":"ready"}
	}`)

	normalized, err := normalizeAgentEngineDiagnostics(payload)
	if err != nil {
		t.Fatalf("normalize diagnostics: %v", err)
	}
	var diagnostics map[string]map[string]string
	if err := json.Unmarshal(normalized, &diagnostics); err != nil {
		t.Fatalf("decode normalized diagnostics: %v", err)
	}
	if len(diagnostics) != 3 {
		t.Fatalf("expected only canonical valid engines, got %s", normalized)
	}
	if diagnostics["codex"]["status"] != "ready" || diagnostics["codex"]["reasonCode"] != "auth_ok" {
		t.Fatalf("unexpected codex diagnostics: %+v", diagnostics["codex"])
	}
	if len([]rune(diagnostics["codex"]["version"])) != 128 {
		t.Fatalf("expected bounded version, got %d runes", len([]rune(diagnostics["codex"]["version"])))
	}
	if diagnostics["codex"]["checkedAt"] != "2026-07-17T08:00:00Z" {
		t.Fatalf("expected normalized checkedAt, got %+v", diagnostics["codex"])
	}
	if _, ok := diagnostics["codex"]["secret"]; ok {
		t.Fatalf("unexpected unrecognized field: %+v", diagnostics["codex"])
	}
	if diagnostics["claude_code"]["status"] != "needs_setup" {
		t.Fatalf("unexpected claude diagnostics: %+v", diagnostics["claude_code"])
	}
	if _, ok := diagnostics["claude_code"]["reasonCode"]; ok {
		t.Fatalf("unknown reason code should be removed: %+v", diagnostics["claude_code"])
	}
	if _, ok := diagnostics["claude_code"]["version"]; ok {
		t.Fatalf("path-like version should be removed: %+v", diagnostics["claude_code"])
	}
	if _, ok := diagnostics["claude_code"]["checkedAt"]; ok {
		t.Fatalf("invalid checkedAt should be removed: %+v", diagnostics["claude_code"])
	}
	if diagnostics["pi"]["reasonCode"] != "probe_unsupported" || diagnostics["pi"]["checkedAt"] != "2026-07-17T08:00:00.123456789Z" {
		t.Fatalf("unexpected pi diagnostics: %+v", diagnostics["pi"])
	}
	if _, ok := diagnostics["pi"]["version"]; ok {
		t.Fatalf("URI version should be removed: %+v", diagnostics["pi"])
	}
}

func TestDiagnosticVersionRejectsUnsafeValues(t *testing.T) {
	for _, value := range []string{
		"/usr/local/bin/codex",
		`C:\\Users\\example\\claude.exe`,
		"https://example.invalid/version",
		"file:///tmp/version",
		"codex 1.2.3\nraw stderr",
		"codex 1.2.3 <secret>",
	} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal version: %v", err)
		}
		if sanitized, ok := diagnosticVersion(raw); ok || sanitized != "" {
			t.Fatalf("unsafe version should be removed: %q -> %q", value, sanitized)
		}
	}

	raw, _ := json.Marshal("Claude Code v1.2.3 (arm64)")
	if sanitized, ok := diagnosticVersion(raw); !ok || sanitized != "Claude Code v1.2.3 (arm64)" {
		t.Fatalf("safe version should remain, got %q ok=%v", sanitized, ok)
	}
}

func TestNormalizeAgentEngineDiagnosticsRejectsInvalidPayloads(t *testing.T) {
	for _, payload := range []json.RawMessage{
		json.RawMessage(`{"codex":`),
		json.RawMessage(`[]`),
		json.RawMessage(`{"codex":{"status":"ready","version":"` + strings.Repeat("x", maxAgentEngineDiagnosticsBytes) + `"}}`),
	} {
		if _, err := normalizeAgentEngineDiagnostics(payload); err == nil {
			t.Fatalf("expected invalid payload to fail: %.80s", payload)
		}
	}

	normalized, err := normalizeAgentEngineDiagnostics(nil)
	if err != nil || string(normalized) != "{}" {
		t.Fatalf("missing diagnostics should normalize to empty object, got %s err=%v", normalized, err)
	}
}

func TestDiagnosticsCanOnlyDowngradeEngineCapabilities(t *testing.T) {
	normalized, err := normalizeRuntimeWorkerInput(RuntimeWorkerInput{
		Name:         "diagnostic-worker",
		Mode:         "personal",
		Capabilities: json.RawMessage(`{"codex":true,"claudeCode":true,"pi":true,"docker":true}`),
		AgentEngineDiagnostics: json.RawMessage(`{
			"codex":{"status":"needs_setup"},
			"claude_code":{"status":"probe_error"},
			"pi":{"status":"unverified"}
		}`),
	})
	if err != nil {
		t.Fatalf("normalize worker: %v", err)
	}
	var capabilities map[string]bool
	if err := json.Unmarshal(normalized.Capabilities, &capabilities); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if capabilities["codex"] || capabilities["claudeCode"] || !capabilities["pi"] || !capabilities["docker"] {
		t.Fatalf("unexpected downgraded capabilities: %s", normalized.Capabilities)
	}

	normalized, err = normalizeRuntimeWorkerInput(RuntimeWorkerInput{
		Name:                   "no-upgrade-worker",
		Mode:                   "personal",
		Capabilities:           json.RawMessage(`{"codex":false,"pi":false}`),
		AgentEngineDiagnostics: json.RawMessage(`{"codex":{"status":"ready"},"pi":{"status":"unverified"}}`),
	})
	if err != nil {
		t.Fatalf("normalize no-upgrade worker: %v", err)
	}
	if err := json.Unmarshal(normalized.Capabilities, &capabilities); err != nil {
		t.Fatalf("decode no-upgrade capabilities: %v", err)
	}
	if capabilities["codex"] || capabilities["pi"] {
		t.Fatalf("diagnostics must never upgrade capabilities: %s", normalized.Capabilities)
	}
}

func TestRuntimeAvailabilityCountsClaimableWorkers(t *testing.T) {
	now := time.Now().UTC()
	worker := func(id, status string, lastSeen time.Time) RuntimeWorker {
		return RuntimeWorker{
			ID:           id,
			WorkspaceID:  "workspace",
			Mode:         "personal",
			Status:       status,
			Capabilities: json.RawMessage(`{"codex":true}`),
			LastSeenAt:   lastSeen.Format(time.RFC3339Nano),
		}
	}
	availability := evaluateRuntimeAvailability(
		"workspace",
		"personal",
		"personal",
		json.RawMessage(`{"codex":true}`),
		[]RuntimeWorker{
			worker("ready-1", "online", now),
			worker("ready-2", "online", now.Add(-time.Second)),
			worker("stale", "online", now.Add(-activeWorkerMaxAge-time.Second)),
			worker("draining", "draining", now),
			{ID: "other-workspace", WorkspaceID: "other", Mode: "personal", Status: "online", Capabilities: json.RawMessage(`{"codex":true}`), LastSeenAt: now.Format(time.RFC3339Nano)},
		},
		now,
	)
	if availability.State != "ready" || availability.ClaimableWorkerCount != 2 {
		t.Fatalf("expected two claimable workers, got %+v", availability)
	}
}

func TestRuntimeWorkerDiagnosticsMigration(t *testing.T) {
	migration, err := migrationFS.ReadFile("migrations/031_runtime_worker_agent_engine_diagnostics.sql")
	if err != nil {
		t.Fatalf("read migration 031: %v", err)
	}
	contents := string(migration)
	for _, expected := range []string{"agent_engine_diagnostics", "JSONB NOT NULL", "DEFAULT '{}'::jsonb"} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("migration 031 missing %q: %s", expected, contents)
		}
	}
}

func TestLegacyRuntimeWorkerSnapshotDefaultsDiagnostics(t *testing.T) {
	store := NewMemoryStore()
	store.restoreSnapshot(memoryStoreSnapshot{
		RuntimeWorkers: map[string]RuntimeWorker{
			"workspace:worker": {
				ID:          "worker",
				WorkspaceID: "workspace",
				Name:        "legacy-worker",
			},
		},
	})

	worker := store.runtimeWorkers["workspace:worker"]
	if string(worker.AgentEngineDiagnostics) != "{}" {
		t.Fatalf("legacy snapshot should default diagnostics to an empty object, got %s", worker.AgentEngineDiagnostics)
	}
}

func TestSQLitePersistsReconciledWorkerDiagnostics(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "mspace.db")
	store, err := NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	user, workspaces, err := store.UpsertIdentity(ctx, IdentityProfile{
		Provider:       "github",
		ProviderUserID: "sqlite-diagnostics-owner",
		Login:          "sqlite-diagnostics-owner",
		Name:           "SQLite Diagnostics Owner",
	})
	if err != nil {
		t.Fatalf("upsert identity: %v", err)
	}
	token, err := store.CreateRuntimeRegistrationToken(ctx, user.ID, workspaces[0].ID, CreateRuntimeRegistrationTokenInput{
		Name:           "sqlite diagnostics worker",
		ExpiresInHours: 1,
	})
	if err != nil {
		t.Fatalf("create runtime token: %v", err)
	}
	registration, err := store.AuthenticateRuntimeRegistrationToken(ctx, token.Token)
	if err != nil {
		t.Fatalf("authenticate runtime token: %v", err)
	}
	worker, err := store.RegisterRuntimeWorker(ctx, registration, RuntimeWorkerInput{
		Name:                   "sqlite-diagnostics-worker",
		Mode:                   "personal",
		Capabilities:           json.RawMessage(`{"codex":true,"docker":true}`),
		AgentEngineDiagnostics: json.RawMessage(`{"codex":{"status":"needs_setup","reasonCode":"auth_required","checkedAt":"2026-07-17T08:00:00Z"}}`),
	})
	if err != nil {
		t.Fatalf("register runtime worker: %v", err)
	}
	if jsonObjectContains(worker.Capabilities, json.RawMessage(`{"codex":true}`)) {
		t.Fatalf("registration should persist downgraded capabilities, got %s", worker.Capabilities)
	}
	if err := store.Persist(); err != nil {
		t.Fatalf("persist sqlite store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close sqlite store: %v", err)
	}

	reopened, err := NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatalf("reopen sqlite store: %v", err)
	}
	defer reopened.Close()
	workers, err := reopened.ListRuntimeWorkers(ctx, user.ID, workspaces[0].ID)
	if err != nil {
		t.Fatalf("list persisted runtime workers: %v", err)
	}
	if len(workers) != 1 || string(workers[0].AgentEngineDiagnostics) != `{"codex":{"status":"needs_setup","reasonCode":"auth_required","checkedAt":"2026-07-17T08:00:00Z"}}` {
		t.Fatalf("unexpected persisted worker diagnostics: %+v", workers)
	}
	if jsonObjectContains(workers[0].Capabilities, json.RawMessage(`{"codex":true}`)) || !jsonObjectContains(workers[0].Capabilities, json.RawMessage(`{"docker":true}`)) {
		t.Fatalf("unexpected persisted worker capabilities: %s", workers[0].Capabilities)
	}
}
