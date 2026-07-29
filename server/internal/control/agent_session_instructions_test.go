package control

import (
	"strings"
	"testing"
)

func TestDefaultAgentSessionDeveloperInstructionsAvoidDevServerPreviewURLs(t *testing.T) {
	instructions := defaultAgentSessionDeveloperInstructions("team", true)
	required := []string{
		"team runtime worker",
		"Do not start or keep a development server running unless the user explicitly asks",
		"If ${MSPACE_SESSION_CONTEXT} is set, read that file before acting",
		"If ${MSPACE_PROJECT_SUBDIR} is set, treat that path inside the workdir as the project focus",
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

func TestIssueWorkingCopyDeveloperInstructionsDoNotRequestBranchRename(t *testing.T) {
	instructions := defaultAgentSessionDeveloperInstructions("personal", false)
	if strings.Contains(instructions, "branch-name.json") || strings.Contains(instructions, "fix/short-semantic-name") {
		t.Fatalf("server-owned Issue branch must not receive a branch proposal instruction:\n%s", instructions)
	}
}

func TestDefaultAgentSessionBranchUsesUniqueSessionSuffix(t *testing.T) {
	branch := defaultAgentSessionBranch("9840afc5-issue", "session-6bf73ec87ccd6701")

	if branch != "mspace/9840afc5/6bf73ec8" {
		t.Fatalf("expected branch to use the unique session id suffix, got %q", branch)
	}
	if strings.Contains(branch, "/session-") {
		t.Fatalf("expected branch not to collapse every session id to session-, got %q", branch)
	}
}
