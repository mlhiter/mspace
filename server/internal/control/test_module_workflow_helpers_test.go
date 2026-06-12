package control

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildTestRunSetupIssueBodyIncludesReusableBootstrapContext(t *testing.T) {
	body := buildTestRunSetupIssueBody(TestRun{
		ID:              "test-run-setup-prompt",
		TargetType:      "branch",
		TargetValue:     "feature/tests",
		Environment:     "staging",
		SetupSteps:      "Prepare the target app and verify preconditions.",
		ResultLocale:    "zh-CN",
		EnvironmentID:   "env-123",
		EnvironmentKind: "kubernetes",
	})

	assertContainsAll(t, body,
		"test-setup-result.json",
		"frontendUrl",
		"apiUrl",
		"sealosUrl",
		"browserSessionStrategy",
		"namespace",
		"preconditionStatus",
		"sessionNotes",
		"bootstrapNotes",
		"Do not persist plaintext secrets",
		"presence, changed/unchanged, last-four characters, or a non-reversible hash",
		"Use Simplified Chinese",
	)
}

func TestBuildTestRunExecutionIssueBodyIncludesUIHarnessGuidance(t *testing.T) {
	runContext := json.RawMessage(`{"frontendUrl":"https://app.example.test","apiUrl":"https://api.example.test","browserSessionStrategy":"reuse existing session"}`)
	body := buildTestRunExecutionIssueBody(TestRun{
		ID:           "test-run-ui-prompt",
		Source:       "plan",
		TargetType:   "preview_url",
		TargetValue:  "https://app.example.test",
		Environment:  "staging",
		RunContext:   runContext,
		ResultLocale: "en",
	}, []TestCase{
		{
			ID:            "case-ui-1",
			Title:         "UI session can open object details",
			Type:          "ui",
			Preconditions: "A bootstrapped browser session is available.",
			Steps: []TestCaseStep{
				{Action: "Open the page", Expected: "The page renders."},
			},
			ExpectedResult: "The case captures browser evidence.",
		},
	})

	assertContainsAll(t, body,
		"MSPACE_CHROME_CDP_URL",
		"reuse an existing CDP page",
		"setup context and app/session bootstrap blockers",
		"browserSessionStrategy",
		"preconditionStatus",
		"${MSPACE_SESSION_ARTIFACT_DIR}/screenshots/",
		"evidence.screenshotPaths",
		"evidence.assertions",
		"evidence.networkStatuses",
		"write one `blocked` or `failed` item per real case ID",
		"Do not persist plaintext secrets",
		"presence, changed/unchanged, last-four characters, or a non-reversible hash",
		"case-ui-1",
	)
}

func assertContainsAll(t *testing.T, value string, needles ...string) {
	t.Helper()
	for _, needle := range needles {
		if !strings.Contains(value, needle) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", needle, value)
		}
	}
}
