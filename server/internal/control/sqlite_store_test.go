package control

import (
	"context"
	"testing"
)

func TestSQLiteStorePersistsSnapshot(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/mspace.db"

	store, err := NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	auth, workspaces, err := store.CreatePasswordIdentity(ctx, PasswordAuthInput{
		Login:    "local-user",
		Password: "password-123456",
		Name:     "Local User",
	})
	if err != nil {
		t.Fatalf("create local identity: %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("expected default workspace, got %d", len(workspaces))
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

	loadedAuth, loadedWorkspaces, err := reopened.AuthenticatePassword(ctx, PasswordAuthInput{
		Login:    "local-user",
		Password: "password-123456",
	})
	if err != nil {
		t.Fatalf("authenticate persisted identity: %v", err)
	}
	if loadedAuth.ID != auth.ID {
		t.Fatalf("expected user %q, got %q", auth.ID, loadedAuth.ID)
	}
	if len(loadedWorkspaces) != 1 || loadedWorkspaces[0].ID != workspaces[0].ID {
		t.Fatalf("expected persisted workspace %+v, got %+v", workspaces, loadedWorkspaces)
	}
}
