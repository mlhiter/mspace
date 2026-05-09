package main

import (
	"os"
	"path/filepath"
	"testing"
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
