package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	defaultTestRunBatchSize        = 5
	maxTestRunBatchSize            = 20
	maxTestPlanCases               = 200
	maxAdHocTestRunCases           = 50
	maxArtifactTestCaseProposals   = 50
	maxArtifactTestResultItems     = 500
	maxTestResultArtifactBytes     = 2 << 20
	maxTestResultArtifactsPerItem  = 10
	testCaseOptimizationAutomation = "test_case_optimization"
	testCaseGenerationAutomation   = "test_case_generation"
	testRunExecutionAutomation     = "test_run_execution"
)

func normalizeReviewNote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 4000 {
		return value[:4000]
	}
	return value
}

func normalizeAgentProfile(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "codex"
	}
	return value
}

func normalizeProposalType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "create", "new", "generated":
		return "create"
	case "update", "refine", "refined", "optimization", "optimize":
		return "update"
	case "archive", "remove":
		return "archive"
	default:
		return ""
	}
}

func normalizeProposalStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "pending", "applied", "rejected", "invalid":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func normalizeTestPlanInput(input TestPlanInput) (TestPlanInput, error) {
	input.Title = collapseWhitespace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = "draft"
	}
	switch input.Status {
	case "draft", "ready", "archived":
	default:
		return TestPlanInput{}, errors.New("status must be draft, ready, or archived")
	}
	input.TargetType = normalizeTestTargetType(input.TargetType)
	if input.TargetType == "" {
		input.TargetType = "branch"
	}
	input.TargetValue = strings.TrimSpace(input.TargetValue)
	input.Environment = strings.TrimSpace(input.Environment)
	input.CaseIDs = uniqueStrings(input.CaseIDs)
	if input.Title == "" {
		return TestPlanInput{}, errors.New("title is required")
	}
	if len(input.CaseIDs) == 0 {
		return TestPlanInput{}, errors.New("caseIds are required")
	}
	if len(input.CaseIDs) > maxTestPlanCases {
		return TestPlanInput{}, fmt.Errorf("caseIds must contain %d or fewer cases", maxTestPlanCases)
	}
	return input, nil
}

func normalizeTestTargetType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "branch", "commit", "source_session", "image", "offline_package", "version_url", "preview_url":
		return strings.ToLower(strings.TrimSpace(value))
	case "source-session":
		return "source_session"
	case "offline-package":
		return "offline_package"
	case "version-url":
		return "version_url"
	case "preview-url":
		return "preview_url"
	default:
		return ""
	}
}

func normalizeTestRunSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "ad_hoc", "plan", "retry", "incremental":
		return strings.ToLower(strings.TrimSpace(value))
	case "adhoc", "ad-hoc", "selected_cases", "selected-cases":
		return "ad_hoc"
	default:
		return ""
	}
}

func normalizeCreateTestRunInput(input CreateTestRunInput, plan TestPlan) (CreateTestRunInput, error) {
	input.TargetType = normalizeTestTargetType(firstNonEmpty(input.TargetType, plan.TargetType))
	if input.TargetType == "" {
		input.TargetType = "branch"
	}
	input.TargetValue = strings.TrimSpace(firstNonEmpty(input.TargetValue, plan.TargetValue))
	input.Environment = strings.TrimSpace(firstNonEmpty(input.Environment, plan.Environment))
	input.AgentProfile = normalizeAgentProfile(input.AgentProfile)
	input.RuntimeMode = strings.ToLower(strings.TrimSpace(input.RuntimeMode))
	if input.BatchSize <= 0 {
		input.BatchSize = defaultTestRunBatchSize
	}
	if input.BatchSize > maxTestRunBatchSize {
		input.BatchSize = maxTestRunBatchSize
	}
	return input, nil
}

func normalizeCreateAdHocTestRunInput(input CreateAdHocTestRunInput) (CreateAdHocTestRunInput, error) {
	input.CaseIDs = uniqueStrings(input.CaseIDs)
	input.TargetType = normalizeTestTargetType(input.TargetType)
	if input.TargetType == "" {
		input.TargetType = "branch"
	}
	input.TargetValue = strings.TrimSpace(input.TargetValue)
	input.Environment = strings.TrimSpace(input.Environment)
	input.AgentProfile = normalizeAgentProfile(input.AgentProfile)
	input.RuntimeMode = strings.ToLower(strings.TrimSpace(input.RuntimeMode))
	if input.BatchSize <= 0 {
		input.BatchSize = defaultTestRunBatchSize
	}
	if input.BatchSize > maxTestRunBatchSize {
		input.BatchSize = maxTestRunBatchSize
	}
	if len(input.CaseIDs) == 0 {
		return CreateAdHocTestRunInput{}, errors.New("caseIds are required")
	}
	if len(input.CaseIDs) > maxAdHocTestRunCases {
		return CreateAdHocTestRunInput{}, fmt.Errorf("caseIds must contain %d or fewer cases", maxAdHocTestRunCases)
	}
	return input, nil
}

func normalizeRetryTestRunInput(input RetryTestRunInput) RetryTestRunInput {
	input.ItemIDs = uniqueStrings(input.ItemIDs)
	input.AgentProfile = normalizeAgentProfile(input.AgentProfile)
	input.RuntimeMode = strings.ToLower(strings.TrimSpace(input.RuntimeMode))
	return input
}

func normalizeTestRunItemStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "queued", "running", "passed", "failed", "blocked", "skipped":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func isFinalTestRunItemStatus(value string) bool {
	switch strings.TrimSpace(value) {
	case "passed", "failed", "blocked", "skipped":
		return true
	default:
		return false
	}
}

func testCaseToInput(testCase TestCase) TestCaseInput {
	return TestCaseInput{
		Title:                   testCase.Title,
		Type:                    testCase.Type,
		Area:                    testCase.Area,
		Priority:                testCase.Priority,
		Status:                  testCase.Status,
		Source:                  testCase.Source,
		Preconditions:           testCase.Preconditions,
		Steps:                   testCaseStepsOrEmpty(testCase.Steps),
		ExpectedResult:          testCase.ExpectedResult,
		EnvironmentRequirements: testCase.EnvironmentRequirements,
		Dependencies:            stringsOrEmpty(testCase.Dependencies),
		Tags:                    stringsOrEmpty(testCase.Tags),
	}
}

func cloneTestCasePointer(testCase TestCase) *TestCase {
	snapshot := testCaseSnapshot(testCase)
	return &snapshot
}

func copyTestCaseInput(input TestCaseInput) TestCaseInput {
	input.Steps = testCaseStepsOrEmpty(input.Steps)
	input.Dependencies = stringsOrEmpty(input.Dependencies)
	input.Tags = stringsOrEmpty(input.Tags)
	return input
}

func cloneRawJSONObject(payload json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}
	normalized, err := normalizeJSONObjectPayload(payload)
	if err != nil {
		return json.RawMessage(`{"error":"invalid evidence"}`)
	}
	return copyRawMessage(normalized)
}

func decodeTestCaseInput(payload []byte) TestCaseInput {
	var input TestCaseInput
	_ = json.Unmarshal(payload, &input)
	return copyTestCaseInput(input)
}

func buildTestCaseOptimizationIssueBody(project Project, cases []TestCase, prompt string) string {
	var builder strings.Builder
	builder.WriteString("Optimize the selected test cases for project `" + project.Name + "`.\n\n")
	builder.WriteString("Codex must write `${MSPACE_SESSION_ARTIFACT_DIR}/test-case-proposals.json` and must not edit canonical test cases directly.\n\n")
	builder.WriteString("Expected artifact shape:\n\n")
	builder.WriteString("```json\n")
	builder.WriteString(`{"proposals":[{"type":"update","caseId":"...","title":"...","summary":"...","rationale":"...","proposedCase":{"title":"...","type":"functional|ui|api|deployment","steps":[{"action":"...","expected":"..."}],"expectedResult":"..."}}]}`)
	builder.WriteString("\n```\n\n")
	if strings.TrimSpace(prompt) != "" {
		builder.WriteString("Human guidance:\n" + strings.TrimSpace(prompt) + "\n\n")
	}
	builder.WriteString("Selected cases:\n")
	for _, testCase := range cases {
		builder.WriteString("- " + testCase.ID + ": " + testCase.Title + " [" + testCase.Status + "]\n")
	}
	return builder.String()
}

func buildTestCaseGenerationIssueBody(project Project, input GenerateTestCasesInput) string {
	var builder strings.Builder
	builder.WriteString("Generate baseline test case proposals for project `" + project.Name + "`.\n\n")
	builder.WriteString("Codex must write `${MSPACE_SESSION_ARTIFACT_DIR}/test-case-proposals.json` and must not edit canonical test cases directly.\n\n")
	builder.WriteString("Every proposal must use type `create` and include a `proposedCase` with `type:functional|ui|api|deployment`, steps, expected result, and environment requirements when relevant.\n")
	if strings.TrimSpace(input.Area) != "" {
		builder.WriteString("\nFocus area: " + strings.TrimSpace(input.Area) + "\n")
	}
	if strings.TrimSpace(input.Prompt) != "" {
		builder.WriteString("\nHuman guidance:\n" + strings.TrimSpace(input.Prompt) + "\n")
	}
	return builder.String()
}

func testRunTitle(plan *TestPlan, run TestRun, cases []TestCase) string {
	if plan != nil && plan.Title != "" {
		return "Test run: " + plan.Title
	}
	if len(cases) == 1 && cases[0].Title != "" {
		return "Test run: " + cases[0].Title
	}
	return fmt.Sprintf("Test run: %d selected cases", len(cases))
}

func testRunExecutionScopeLabel(plan *TestPlan, cases []TestCase) string {
	if plan != nil && plan.Title != "" {
		return "test plan `" + plan.Title + "`"
	}
	if len(cases) == 1 && cases[0].Title != "" {
		return "selected test case `" + cases[0].Title + "`"
	}
	return fmt.Sprintf("%d selected test cases", len(cases))
}

func buildTestRunParentIssueBody(plan *TestPlan, run TestRun, cases []TestCase) string {
	var builder strings.Builder
	builder.WriteString("Execute " + testRunExecutionScopeLabel(plan, cases) + ".\n\n")
	builder.WriteString("Source: `" + firstNonEmpty(run.Source, "ad_hoc") + "`\n")
	builder.WriteString("Target: `" + run.TargetType + "` `" + firstNonEmpty(run.TargetValue, "not specified") + "`\n")
	builder.WriteString("Environment: `" + firstNonEmpty(run.Environment, "not specified") + "`\n\n")
	if len(cases) > 0 {
		builder.WriteString("Cases:\n")
		for _, testCase := range cases {
			builder.WriteString("- `" + testCase.ID + "` " + testCase.Title + "\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("This issue tracks the overall run. Execution details live in the child issues and test run items.\n\n")
	builder.WriteString("Codex execution sessions must write `${MSPACE_SESSION_ARTIFACT_DIR}/test-result.json` with:\n\n")
	builder.WriteString(`{"runId":"` + run.ID + `","items":[{"caseId":"...","status":"passed|failed|blocked|skipped","actualResult":"...","failureSummary":"...","evidence":{}}]}`)
	return strings.TrimSpace(builder.String())
}

func buildTestRunExecutionIssueBody(run TestRun, cases []TestCase) string {
	var builder strings.Builder
	builder.WriteString("Execute this batch of test cases.\n\n")
	builder.WriteString("Run ID: `" + run.ID + "`\n")
	builder.WriteString("Source: `" + firstNonEmpty(run.Source, "ad_hoc") + "`\n")
	builder.WriteString("Target: `" + run.TargetType + "` `" + firstNonEmpty(run.TargetValue, "not specified") + "`\n")
	builder.WriteString("Environment: `" + firstNonEmpty(run.Environment, "not specified") + "`\n\n")
	builder.WriteString("Write `${MSPACE_SESSION_ARTIFACT_DIR}/test-result.json` with one item per case in this batch.\n\n")
	for _, testCase := range cases {
		builder.WriteString("## " + testCase.ID + ": " + testCase.Title + "\n")
		builder.WriteString("Type: " + firstNonEmpty(testCase.Type, defaultTestCaseType) + "\n")
		if testCase.Preconditions != "" {
			builder.WriteString("Preconditions: " + testCase.Preconditions + "\n")
		}
		for index, step := range testCase.Steps {
			builder.WriteString(fmt.Sprintf("%d. %s\n", index+1, step.Action))
			if step.Expected != "" {
				builder.WriteString("   Expected: " + step.Expected + "\n")
			}
		}
		if testCase.ExpectedResult != "" {
			builder.WriteString("Expected result: " + testCase.ExpectedResult + "\n")
		}
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func testRunCounts(items []TestRunItem) (passed, failed, blocked, skipped int) {
	for _, item := range items {
		switch item.Status {
		case "passed":
			passed++
		case "failed":
			failed++
		case "blocked":
			blocked++
		case "skipped":
			skipped++
		}
	}
	return passed, failed, blocked, skipped
}

func containsString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func syncTestRunCaseSnapshotLocked(items map[string]TestRunItem, testCase *TestCase) {
	if testCase == nil {
		return
	}
	for id, item := range items {
		if item.TestCaseID == testCase.ID {
			item.TestCase = testCaseSnapshot(*testCase)
			items[id] = item
		}
	}
}
