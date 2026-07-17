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
	maxTestPlanSetupStepsLength    = 12000
	testCaseOptimizationAutomation = "test_case_optimization"
	testCaseGenerationAutomation   = "test_case_generation"
	testRunSetupAutomation         = "test_run_setup"
	testRunExecutionAutomation     = "test_run_execution"
)

func agentSessionRequiredCapabilities(input CreateAgentSessionInput) (json.RawMessage, error) {
	capability, err := agentEngineCapability(input.AgentEngine)
	if err != nil {
		return nil, err
	}
	required := map[string]bool{capability: true}
	if len(input.RequiredCapabilities) > 0 {
		var extras map[string]bool
		if err := json.Unmarshal(input.RequiredCapabilities, &extras); err != nil {
			return nil, fmt.Errorf("requiredCapabilities must be a JSON object")
		}
		for key, value := range extras {
			key = strings.TrimSpace(key)
			if value && isAgentEngineCapabilityKey(key) && key != capability {
				return nil, fmt.Errorf("requiredCapabilities conflicts with agentEngine %s", input.AgentEngine)
			}
			if key != "" && value {
				required[key] = true
			}
		}
	}
	return json.Marshal(required)
}

func isAgentEngineCapabilityKey(key string) bool {
	switch key {
	case "codex", "claudeCode", "pi":
		return true
	default:
		return false
	}
}

func testRunExecutionRequiredCapabilities(cases []TestCase) (json.RawMessage, error) {
	required := map[string]bool{"codex": true}
	if testRunBatchRequiresBrowser(cases) {
		required["browser"] = true
		required["chrome_cdp"] = true
	}
	return json.Marshal(required)
}

func testPlanDetailForResponse(detail TestPlanDetail) TestPlanDetail {
	testCases := make([]TestCase, 0, len(detail.Cases))
	for _, planCase := range detail.Cases {
		testCases = append(testCases, planCase.TestCase)
	}
	detail.RequiredCapabilities, _ = testRunExecutionRequiredCapabilities(testCases)
	return detail
}

func testRunDetailForResponse(detail TestRunDetail) TestRunDetail {
	testCases := make([]TestCase, 0, len(detail.Items))
	for _, item := range detail.Items {
		testCases = append(testCases, item.TestCase)
	}
	detail.RequiredCapabilities, _ = testRunExecutionRequiredCapabilities(testCases)
	return detail
}

func testRunBatchRequiresBrowser(cases []TestCase) bool {
	for _, testCase := range cases {
		if testCaseRequiresBrowser(testCase) {
			return true
		}
	}
	return false
}

func testCaseRequiresBrowser(testCase TestCase) bool {
	if strings.EqualFold(strings.TrimSpace(testCase.Type), "ui") {
		return true
	}
	text := strings.ToLower(strings.Join(testCaseBrowserSignals(testCase), "\n"))
	for _, signal := range []string{
		"browser",
		"cdp",
		"frontend_url",
		"frontend url",
		"screenshot",
		" ui ",
		"sealos platform",
		"sealos desktop",
		"object storage icon",
		"app icon",
		"access-key",
		"access key",
		"s3 service",
		"浏览器",
		"截图",
		"界面",
		"sealos 平台",
		"sealos 桌面",
		"对象存储图标",
		"应用图标",
		"访问密钥",
		"s3 服务参数",
	} {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func testCaseBrowserSignals(testCase TestCase) []string {
	signals := []string{
		testCase.Title,
		testCase.Preconditions,
		testCase.ExpectedResult,
		testCase.EnvironmentRequirements,
	}
	for _, step := range testCase.Steps {
		signals = append(signals, step.Action, step.Expected)
	}
	return signals
}

func testRunExecutionCapabilitySets(cases []TestCase, batchSize int) ([]json.RawMessage, error) {
	if batchSize <= 0 {
		batchSize = defaultTestRunBatchSize
	}
	if batchSize > maxTestRunBatchSize {
		batchSize = maxTestRunBatchSize
	}
	sets := []json.RawMessage{}
	for _, projectCases := range testCasesGroupedByProject(cases) {
		for start := 0; start < len(projectCases); start += batchSize {
			end := start + batchSize
			if end > len(projectCases) {
				end = len(projectCases)
			}
			requiredCapabilities, err := testRunExecutionRequiredCapabilities(projectCases[start:end])
			if err != nil {
				return nil, err
			}
			sets = append(sets, requiredCapabilities)
		}
	}
	return dedupeCapabilitySets(sets), nil
}

func testCasesGroupedByProject(cases []TestCase) [][]TestCase {
	groups := [][]TestCase{}
	indexByProjectID := map[string]int{}
	for _, testCase := range cases {
		projectID := strings.TrimSpace(testCase.ProjectID)
		index, ok := indexByProjectID[projectID]
		if !ok {
			index = len(groups)
			indexByProjectID[projectID] = index
			groups = append(groups, []TestCase{})
		}
		groups[index] = append(groups[index], testCase)
	}
	return groups
}

func dedupeCapabilitySets(sets []json.RawMessage) []json.RawMessage {
	seen := map[string]bool{}
	deduped := []json.RawMessage{}
	for _, set := range sets {
		normalized, err := normalizeJSONObjectPayload(set)
		if err != nil {
			continue
		}
		key := string(normalized)
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, normalized)
	}
	return deduped
}

func normalizeReviewNote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 4000 {
		return value[:4000]
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
	input.SetupSteps = normalizeTestPlanSetupSteps(input.SetupSteps)
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
	input.EnvironmentID = strings.TrimSpace(input.EnvironmentID)
	input.EnvironmentKind = strings.TrimSpace(input.EnvironmentKind)
	if input.EnvironmentKind != "" {
		input.EnvironmentKind = normalizeEnvironmentKind(input.EnvironmentKind)
	}
	input.CaseIDs = uniqueStrings(input.CaseIDs)
	input.Cases = uniqueTestPlanCaseInputs(input.Cases)
	if input.Title == "" {
		return TestPlanInput{}, errors.New("title is required")
	}
	caseCount := len(input.CaseIDs) + len(input.Cases)
	if caseCount == 0 {
		return TestPlanInput{}, errors.New("caseIds are required")
	}
	if caseCount > maxTestPlanCases {
		return TestPlanInput{}, fmt.Errorf("caseIds must contain %d or fewer cases", maxTestPlanCases)
	}
	return input, nil
}

func normalizedPlanCaseInputs(input TestPlanInput, defaultProjectID string) ([]TestPlanCaseInput, error) {
	return normalizedCaseInputs(input.Cases, input.CaseIDs, defaultProjectID, maxTestPlanCases, "caseIds")
}

func normalizeTestPlanSetupSteps(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > maxTestPlanSetupStepsLength {
		return value[:maxTestPlanSetupStepsLength]
	}
	return value
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

func normalizeTestResultLocale(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "zh", "zh-cn", "zh_hans", "zh-hans", "chinese", "simplified_chinese", "simplified-chinese":
		return "zh-CN"
	default:
		return "en"
	}
}

func testResultLanguageInstruction(locale string) string {
	switch normalizeTestResultLocale(locale) {
	case "zh-CN":
		return "Use Simplified Chinese for all user-facing result text in `summary`, `failureSummary`, `actualResult`, assertion summaries, and evidence notes. Keep field names, IDs, commands, URLs, and code/log excerpts unchanged."
	default:
		return "Use English for all user-facing result text in `summary`, `failureSummary`, `actualResult`, assertion summaries, and evidence notes. Keep field names, IDs, commands, URLs, and code/log excerpts unchanged."
	}
}

func normalizeCreateTestRunInput(input CreateTestRunInput, plan TestPlan) (CreateTestRunInput, error) {
	input.TargetType = normalizeTestTargetType(firstNonEmpty(input.TargetType, plan.TargetType))
	if input.TargetType == "" {
		input.TargetType = "branch"
	}
	input.TargetValue = strings.TrimSpace(firstNonEmpty(input.TargetValue, plan.TargetValue))
	input.Environment = strings.TrimSpace(firstNonEmpty(input.Environment, plan.Environment))
	input.EnvironmentID = strings.TrimSpace(firstNonEmpty(input.EnvironmentID, plan.EnvironmentID))
	input.EnvironmentKind = strings.TrimSpace(firstNonEmpty(input.EnvironmentKind, plan.EnvironmentKind))
	if input.EnvironmentKind != "" {
		input.EnvironmentKind = normalizeEnvironmentKind(input.EnvironmentKind)
	}
	engine, err := requireCodexWorkflowAgentEngine(input.AgentEngine, input.LegacyProvider, input.LegacyAgentProfile)
	if err != nil {
		return CreateTestRunInput{}, err
	}
	input.AgentEngine = engine
	input.LegacyProvider = ""
	input.LegacyAgentProfile = ""
	input.RuntimeMode = strings.ToLower(strings.TrimSpace(input.RuntimeMode))
	input.ResultLocale = normalizeTestResultLocale(input.ResultLocale)
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
	input.Cases = uniqueTestPlanCaseInputs(input.Cases)
	input.TargetType = normalizeTestTargetType(input.TargetType)
	if input.TargetType == "" {
		input.TargetType = "branch"
	}
	input.TargetValue = strings.TrimSpace(input.TargetValue)
	input.Environment = strings.TrimSpace(input.Environment)
	input.EnvironmentID = strings.TrimSpace(input.EnvironmentID)
	input.EnvironmentKind = strings.TrimSpace(input.EnvironmentKind)
	if input.EnvironmentKind != "" {
		input.EnvironmentKind = normalizeEnvironmentKind(input.EnvironmentKind)
	}
	engine, err := requireCodexWorkflowAgentEngine(input.AgentEngine, input.LegacyProvider, input.LegacyAgentProfile)
	if err != nil {
		return CreateAdHocTestRunInput{}, err
	}
	input.AgentEngine = engine
	input.LegacyProvider = ""
	input.LegacyAgentProfile = ""
	input.RuntimeMode = strings.ToLower(strings.TrimSpace(input.RuntimeMode))
	input.ResultLocale = normalizeTestResultLocale(input.ResultLocale)
	if input.BatchSize <= 0 {
		input.BatchSize = defaultTestRunBatchSize
	}
	if input.BatchSize > maxTestRunBatchSize {
		input.BatchSize = maxTestRunBatchSize
	}
	caseCount := len(input.CaseIDs) + len(input.Cases)
	if caseCount == 0 {
		return CreateAdHocTestRunInput{}, errors.New("caseIds are required")
	}
	if caseCount > maxAdHocTestRunCases {
		return CreateAdHocTestRunInput{}, fmt.Errorf("caseIds must contain %d or fewer cases", maxAdHocTestRunCases)
	}
	return input, nil
}

func normalizedAdHocRunCaseInputs(input CreateAdHocTestRunInput, defaultProjectID string) ([]TestPlanCaseInput, error) {
	return normalizedCaseInputs(input.Cases, input.CaseIDs, defaultProjectID, maxAdHocTestRunCases, "caseIds")
}

func normalizedCaseInputs(cases []TestPlanCaseInput, caseIDs []string, defaultProjectID string, maxCount int, fieldName string) ([]TestPlanCaseInput, error) {
	defaultProjectID = strings.TrimSpace(defaultProjectID)
	result := make([]TestPlanCaseInput, 0, len(cases)+len(caseIDs))
	seen := map[string]bool{}
	add := func(projectID, caseID string) error {
		projectID = strings.TrimSpace(projectID)
		caseID = strings.TrimSpace(caseID)
		if projectID == "" {
			projectID = defaultProjectID
		}
		if projectID == "" || caseID == "" {
			return errors.New(fieldName + " must include projectId and caseId")
		}
		key := projectID + "\x00" + caseID
		if seen[key] {
			return nil
		}
		seen[key] = true
		result = append(result, TestPlanCaseInput{ProjectID: projectID, CaseID: caseID})
		return nil
	}
	for _, item := range cases {
		if err := add(item.ProjectID, item.CaseID); err != nil {
			return nil, err
		}
	}
	for _, caseID := range caseIDs {
		if err := add(defaultProjectID, caseID); err != nil {
			return nil, err
		}
	}
	if len(result) == 0 {
		return nil, errors.New(fieldName + " are required")
	}
	if len(result) > maxCount {
		return nil, fmt.Errorf("%s must contain %d or fewer cases", fieldName, maxCount)
	}
	return result, nil
}

func uniqueTestPlanCaseInputs(values []TestPlanCaseInput) []TestPlanCaseInput {
	if len(values) == 0 {
		return []TestPlanCaseInput{}
	}
	result := make([]TestPlanCaseInput, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		projectID := strings.TrimSpace(value.ProjectID)
		caseID := strings.TrimSpace(value.CaseID)
		if projectID == "" && caseID == "" {
			continue
		}
		key := projectID + "\x00" + caseID
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, TestPlanCaseInput{ProjectID: projectID, CaseID: caseID})
	}
	return result
}

func normalizeRetryTestRunInput(input RetryTestRunInput) (RetryTestRunInput, error) {
	input.ItemIDs = uniqueStrings(input.ItemIDs)
	engine, err := requireCodexWorkflowAgentEngine(input.AgentEngine, input.LegacyProvider, input.LegacyAgentProfile)
	if err != nil {
		return RetryTestRunInput{}, err
	}
	input.AgentEngine = engine
	input.LegacyProvider = ""
	input.LegacyAgentProfile = ""
	input.RuntimeMode = strings.ToLower(strings.TrimSpace(input.RuntimeMode))
	input.ResultLocale = normalizeTestResultLocale(input.ResultLocale)
	return input, nil
}

func normalizeTestRunItemStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "queued", "running", "passed", "failed", "blocked", "skipped", "cancelled":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func isFinalTestRunItemStatus(value string) bool {
	switch strings.TrimSpace(value) {
	case "passed", "failed", "blocked", "skipped", "cancelled":
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

func isLegacyTestAutomationIssue(title, body string) bool {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if body == "" {
		return false
	}
	const proposalArtifactInstruction = "Codex must write `${MSPACE_SESSION_ARTIFACT_DIR}/test-case-proposals.json` and must not edit canonical test cases directly."
	switch title {
	case "Optimize test cases":
		return strings.HasPrefix(body, "Optimize the selected test cases for project `") && strings.Contains(body, proposalArtifactInstruction)
	case "Generate test cases":
		return strings.HasPrefix(body, "Generate baseline test case proposals for project `") && strings.Contains(body, proposalArtifactInstruction)
	default:
		return false
	}
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
	if run.EnvironmentID != "" {
		builder.WriteString("Environment target: `" + run.EnvironmentKind + "` `" + run.EnvironmentID + "`\n\n")
	}
	if strings.TrimSpace(run.SetupSteps) != "" {
		builder.WriteString("Plan setup runs first. Case execution starts only after setup writes a passing `${MSPACE_SESSION_ARTIFACT_DIR}/test-setup-result.json` artifact.\n\n")
	}
	if len(cases) > 0 {
		builder.WriteString("Cases:\n")
		for _, testCase := range cases {
			builder.WriteString("- `" + testCase.ID + "` " + testCase.Title + "\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("This issue tracks the overall run. Execution details live in the child issues and test run items.\n\n")
	builder.WriteString(testResultLanguageInstruction(run.ResultLocale) + "\n\n")
	builder.WriteString("Codex execution sessions must write `${MSPACE_SESSION_ARTIFACT_DIR}/test-result.json` with:\n\n")
	builder.WriteString(`{"runId":"` + run.ID + `","items":[{"caseId":"...","status":"passed|failed|blocked|skipped","actualResult":"...","failureSummary":"...","evidence":{}}]}`)
	builder.WriteString("\n\nUse only real case IDs from this run. Never use synthetic IDs such as `batch`, `all`, or `summary`; if a global blocker stops multiple cases, write one final result item per affected case with that case's real `caseId`.")
	return strings.TrimSpace(builder.String())
}

func buildTestRunSetupIssueBody(run TestRun) string {
	var builder strings.Builder
	builder.WriteString("Prepare this test run before executing any cases.\n\n")
	builder.WriteString("Run ID: `" + run.ID + "`\n")
	builder.WriteString("Target: `" + run.TargetType + "` `" + firstNonEmpty(run.TargetValue, "not specified") + "`\n")
	builder.WriteString("Environment: `" + firstNonEmpty(run.Environment, "not specified") + "`\n")
	if run.EnvironmentID != "" {
		builder.WriteString("Environment target: `" + run.EnvironmentKind + "` `" + run.EnvironmentID + "`\n")
	}
	builder.WriteString("\nSetup steps:\n\n")
	builder.WriteString(strings.TrimSpace(run.SetupSteps))
	builder.WriteString("\n\nWrite `${MSPACE_SESSION_ARTIFACT_DIR}/test-setup-result.json` before finishing.\n\n")
	builder.WriteString(testResultLanguageInstruction(run.ResultLocale) + "\n\n")
	builder.WriteString("Expected artifact shape:\n\n")
	builder.WriteString("```json\n")
	builder.WriteString(`{"runId":"` + run.ID + `","status":"passed|failed","summary":"what is ready","failureSummary":"","outputs":{},"evidence":{},"steps":[{"title":"...","status":"passed|failed","command":"...","summary":"..."}]}`)
	builder.WriteString("\n```\n\n")
	builder.WriteString("Put reusable outputs for later case execution in `outputs`. Keep this generic and environment-driven: examples include `previewUrl`, `frontendUrl`, `apiUrl`, `sealosUrl`, `platformUrl`, `appEntry`, `directFrontendUrl`, `image`, `namespace`, `sshTarget`, `browserSessionStrategy`, `preconditionStatus`, `sessionNotes`, or `bootstrapNotes`. Use `evidence` for compact proof such as checked URLs, command summaries, readiness signals, or bootstrap/session observations that later UI cases can trust.\n\n")
	builder.WriteString("When an app is normally launched from a platform shell such as Sealos Desktop, distinguish platform entry from direct app reachability. Record the platform URL in `sealosUrl` or `platformUrl`, the app launch affordance in `appEntry`, and direct ingress reachability in `directFrontendUrl` or `frontendUrl`. Treat direct app HTTP 200 as a health signal only unless the test explicitly allows direct access; if later cases need platform-provided session, quota, workspace, or app-token context, verify that bootstrap path or write `status:\"failed\"` with the blocker.\n\n")
	builder.WriteString("Do not persist plaintext secrets in `outputs`, `evidence`, `steps`, screenshots, notes, or summaries. When secret handling must be proven, record only safe metadata such as presence, changed/unchanged, last-four characters, or a non-reversible hash.\n\n")
	builder.WriteString("If setup cannot safely complete, write `status:\"failed\"` and include a compact `failureSummary` plus any reusable blocker context, for example missing session bootstrap, unreachable frontend/API, failed namespace readiness, or unresolved preconditions.")
	return strings.TrimSpace(builder.String())
}

func buildTestRunExecutionIssueBody(run TestRun, cases []TestCase) string {
	var builder strings.Builder
	builder.WriteString("Execute this batch of test cases.\n\n")
	builder.WriteString("Run ID: `" + run.ID + "`\n")
	builder.WriteString("Source: `" + firstNonEmpty(run.Source, "ad_hoc") + "`\n")
	builder.WriteString("Target: `" + run.TargetType + "` `" + firstNonEmpty(run.TargetValue, "not specified") + "`\n")
	builder.WriteString("Environment: `" + firstNonEmpty(run.Environment, "not specified") + "`\n\n")
	if run.EnvironmentID != "" {
		builder.WriteString("Environment target: `" + run.EnvironmentKind + "` `" + run.EnvironmentID + "`\n\n")
	}
	if len(run.RunContext) > 0 && strings.TrimSpace(string(run.RunContext)) != "{}" {
		builder.WriteString("Setup context:\n")
		builder.WriteString("```json\n")
		builder.WriteString(strings.TrimSpace(string(run.RunContext)))
		builder.WriteString("\n```\n\n")
	}
	builder.WriteString("Write `${MSPACE_SESSION_ARTIFACT_DIR}/test-result.json` with one item per case in this batch. Use only the real case IDs listed below. Never use synthetic IDs such as `batch`, `all`, or `summary`; if a global setup, network, browser, or script failure stops this batch, write one `blocked` or `failed` item per affected case with that case's real `caseId`.\n\n")
	if testRunBatchRequiresBrowser(cases) {
		builder.WriteString("This batch includes browser-backed cases, either because their type is `ui` or because their steps explicitly mention browser/CDP/screenshot/frontend URL, Sealos Desktop/platform entry, app icons, access-key/S3 service parameter flows, or similar session-bearing UI entry points. Use the browser-capable runtime (`MSPACE_CHROME_CDP_URL` when provided) to exercise the real user entry path. If the CDP endpoint does not support creating a new target/page, reuse an existing CDP page instead of failing immediately. Before running assertions, check setup context and app/session bootstrap blockers such as missing `frontendUrl`, `directFrontendUrl`, `apiUrl`, `sealosUrl`, `platformUrl`, `appEntry`, `browserSessionStrategy`, authentication/session state, or unresolved `preconditionStatus`.\n\n")
		builder.WriteString("If `sealosUrl` or `platformUrl` is present and the case steps mention Sealos, Desktop, workspace, app icon, object storage entry, or access-key/session behavior, log in through that platform URL first and open the app from the platform shell. Use `frontendUrl` or `directFrontendUrl` only as a health check or explicitly documented fallback; do not treat direct app HTTP 200 as proof that platform session, Authorization, workspace quota, or app-token context is available.\n\n")
		builder.WriteString("Capture at least one screenshot per browser-backed case, save screenshots under `${MSPACE_SESSION_ARTIFACT_DIR}/screenshots/`, and reference them from each result item with `evidence.screenshotPaths`. Also include useful `evidence.assertions` and `evidence.networkStatuses` when observable. If browser/session execution is impossible, write one `blocked` or `failed` item per real case ID affected by the blocker, and put the concrete browser/session/app bootstrap reason in `failureSummary` instead of returning text-only evidence.\n\n")
	}
	builder.WriteString("Do not persist plaintext secrets in `test-result.json`, screenshots, notes, DOM snippets, network excerpts, or summaries. When a case validates secret behavior, record only safe metadata such as presence, changed/unchanged, last-four characters, or a non-reversible hash.\n\n")
	builder.WriteString(testResultLanguageInstruction(run.ResultLocale) + "\n\n")
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
