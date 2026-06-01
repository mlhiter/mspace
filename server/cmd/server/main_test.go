package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mlhiter/mspace/server/internal/control"
)

func TestLoadEnvFileKeepsExistingEnvironment(t *testing.T) {
	t.Setenv("MSPACE_GITHUB_CLIENT_ID", "from-shell")

	path := filepath.Join(t.TempDir(), ".env.local")
	content := []byte(`
# local mspace config
MSPACE_GITHUB_CLIENT_ID=from-file
export MSPACE_GITHUB_CLIENT_SECRET="secret-from-file"
MSPACE_GITHUB_REDIRECT_URI='http://127.0.0.1:8787/api/auth/github/callback'
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	if err := loadEnvFile(path, map[string]bool{"MSPACE_GITHUB_CLIENT_ID": true}); err != nil {
		t.Fatalf("load env file: %v", err)
	}

	if got := os.Getenv("MSPACE_GITHUB_CLIENT_ID"); got != "from-shell" {
		t.Fatalf("existing env should win, got %q", got)
	}
	if got := os.Getenv("MSPACE_GITHUB_CLIENT_SECRET"); got != "secret-from-file" {
		t.Fatalf("expected quoted secret to be unwrapped, got %q", got)
	}
	if got := os.Getenv("MSPACE_GITHUB_REDIRECT_URI"); got != "http://127.0.0.1:8787/api/auth/github/callback" {
		t.Fatalf("expected quoted redirect uri to be unwrapped, got %q", got)
	}
}

func TestEnsureBootstrapAdminCreatesTeamWorkspaceAndRuntimeToken(t *testing.T) {
	store := control.NewMemoryStore()
	runtimeToken := "msw_testbootstrapruntime0000000000000000000000000000000000000000000000000000"

	err := ensureBootstrapAdmin(context.Background(), store, control.Config{
		BootstrapAdminLogin:        "admin",
		BootstrapAdminPassword:     "correct-password",
		BootstrapAdminName:         "Admin",
		BootstrapTeamWorkspaceName: "Customer Team",
		BootstrapRuntimeToken:      runtimeToken,
		BootstrapRuntimeTokenName:  "Helm fixed worker",
		BootstrapRuntimeTokenTTL:   24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("ensure bootstrap admin: %v", err)
	}

	user, workspaces, err := store.AuthenticatePassword(context.Background(), control.PasswordAuthInput{
		Login:    "admin",
		Password: "correct-password",
	})
	if err != nil {
		t.Fatalf("get bootstrap admin: %v", err)
	}
	var teamWorkspace control.Workspace
	for _, workspace := range workspaces {
		if workspace.Kind == "team" && workspace.Name == "Customer Team" && workspace.Role == "owner" {
			teamWorkspace = workspace
			break
		}
	}
	if teamWorkspace.ID == "" {
		t.Fatalf("expected bootstrap admin to own default team workspace, got %+v", workspaces)
	}

	registration, err := store.AuthenticateRuntimeRegistrationToken(context.Background(), runtimeToken)
	if err != nil {
		t.Fatalf("authenticate bootstrap runtime token: %v", err)
	}
	if registration.WorkspaceID != teamWorkspace.ID {
		t.Fatalf("runtime token workspace mismatch: registration=%+v team=%+v user=%+v", registration, teamWorkspace, user)
	}

	if err := ensureBootstrapAdmin(context.Background(), store, control.Config{
		BootstrapAdminLogin:        "admin",
		BootstrapAdminPassword:     "different-password",
		BootstrapAdminName:         "Admin",
		BootstrapTeamWorkspaceName: "customer team",
		BootstrapRuntimeToken:      runtimeToken,
		BootstrapRuntimeTokenName:  "Helm fixed worker",
		BootstrapRuntimeTokenTTL:   48 * time.Hour,
	}); err != nil {
		t.Fatalf("ensure bootstrap admin idempotent: %v", err)
	}

	_, refreshedWorkspaces, err := store.AuthenticatePassword(context.Background(), control.PasswordAuthInput{
		Login:    "admin",
		Password: "correct-password",
	})
	if err != nil {
		t.Fatalf("get refreshed bootstrap admin: %v", err)
	}
	teamCount := 0
	for _, workspace := range refreshedWorkspaces {
		if workspace.Kind == "team" && strings.EqualFold(workspace.Name, "Customer Team") {
			teamCount++
		}
	}
	if teamCount != 1 {
		t.Fatalf("expected idempotent bootstrap to keep one team workspace, got %+v", refreshedWorkspaces)
	}
}

func TestEnsureBootstrapAdminRejectsShortRuntimeToken(t *testing.T) {
	store := control.NewMemoryStore()

	err := ensureBootstrapAdmin(context.Background(), store, control.Config{
		BootstrapAdminLogin:        "admin",
		BootstrapAdminPassword:     "correct-password",
		BootstrapTeamWorkspaceName: "Customer Team",
		BootstrapRuntimeToken:      "msw_short",
	})
	if err == nil || !strings.Contains(err.Error(), "at least 32 characters") {
		t.Fatalf("expected short token error, got %v", err)
	}
}
