package control

import (
	"strings"
	"testing"
)

func TestDefaultAgentSessionDeveloperInstructionsAvoidDevServerPreviewURLs(t *testing.T) {
	instructions := defaultAgentSessionDeveloperInstructions("team")
	required := []string{
		"team runtime worker",
		"Do not start or keep a development server running unless the user explicitly asks",
		"prefer non-interactive checks",
		"If a temporary server is required for validation, stop it before finishing",
		"Do not present container-local localhost or 127.0.0.1 URLs as user-accessible preview URLs",
		"branch-name.json",
		"fix/short-semantic-name",
	}
	for _, text := range required {
		if !strings.Contains(instructions, text) {
			t.Fatalf("expected default instructions to contain %q, got:\n%s", text, instructions)
		}
	}
}
