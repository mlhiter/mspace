package control

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	maxImportedTestCases      = 100
	maxImportedTestCaseBytes  = 256 * 1024
	defaultTestCaseType       = "functional"
	defaultTestCaseSource     = "manual"
	defaultImportedCaseSource = "import"
)

var (
	importBulletPattern = regexp.MustCompile(`^\s*(?:[-*+]\s+\[[ xX]\]|\d+[\.)]|[-*+])\s+`)
	vagueCasePatterns   = []string{
		"verify it works",
		"check it works",
		"check whether it is normal",
		"make sure it works",
		"works correctly",
		"正常",
		"是否正常",
		"没问题",
	}
)

func normalizeTestCaseInput(input TestCaseInput, sourceFallback string) (TestCaseInput, int, []TestCaseQualityFinding, error) {
	input.Title = collapseWhitespace(input.Title)
	input.Type = strings.ToLower(strings.TrimSpace(input.Type))
	input.Area = collapseWhitespace(input.Area)
	input.Priority = strings.ToLower(strings.TrimSpace(input.Priority))
	input.Status = strings.ToLower(strings.TrimSpace(input.Status))
	input.Source = strings.ToLower(strings.TrimSpace(input.Source))
	input.Preconditions = strings.TrimSpace(input.Preconditions)
	input.ExpectedResult = strings.TrimSpace(input.ExpectedResult)
	input.EnvironmentRequirements = strings.TrimSpace(input.EnvironmentRequirements)
	input.Dependencies = uniqueStrings(input.Dependencies)
	input.Tags = normalizeTags(input.Tags)
	input.Steps = normalizeTestCaseSteps(input.Steps)

	if input.Title == "" {
		return TestCaseInput{}, 0, nil, errors.New("title is required")
	}
	if input.Type == "" {
		input.Type = defaultTestCaseType
	}
	if input.Type != defaultTestCaseType {
		return TestCaseInput{}, 0, nil, errors.New("type must be functional")
	}
	switch input.Priority {
	case "", "p0", "p1", "p2", "p3":
	default:
		return TestCaseInput{}, 0, nil, errors.New("priority must be p0, p1, p2, p3, or empty")
	}
	if input.Source == "" {
		input.Source = strings.TrimSpace(sourceFallback)
	}
	if input.Source == "" {
		input.Source = defaultTestCaseSource
	}
	switch input.Source {
	case "manual", "import", "codex_generated", "codex_refined":
	default:
		return TestCaseInput{}, 0, nil, errors.New("source must be manual, import, codex_generated, or codex_refined")
	}

	score, findings := scoreTestCaseQuality(input)
	if input.Status == "" {
		if score >= 70 {
			input.Status = "ready"
		} else {
			input.Status = "needs_review"
		}
	}
	switch input.Status {
	case "draft", "needs_review", "ready", "archived":
	default:
		return TestCaseInput{}, 0, nil, errors.New("status must be draft, needs_review, ready, or archived")
	}
	return input, score, findings, nil
}

func normalizeImportTestCasesInput(input ImportTestCasesInput) (ImportTestCasesInput, error) {
	input.Format = strings.ToLower(strings.TrimSpace(input.Format))
	input.Content = strings.TrimSpace(input.Content)
	if input.Format == "" {
		input.Format = "markdown"
	}
	switch input.Format {
	case "markdown", "text", "csv":
	default:
		return ImportTestCasesInput{}, errors.New("format must be markdown, text, or csv")
	}
	if input.Content == "" {
		return ImportTestCasesInput{}, errors.New("content is required")
	}
	if len([]byte(input.Content)) > maxImportedTestCaseBytes {
		return ImportTestCasesInput{}, fmt.Errorf("content must be smaller than %d bytes", maxImportedTestCaseBytes)
	}
	return input, nil
}

func parseImportedTestCases(input ImportTestCasesInput) ([]TestCaseInput, []TestCaseImportSkip, error) {
	normalized, err := normalizeImportTestCasesInput(input)
	if err != nil {
		return nil, nil, err
	}
	if normalized.Format == "csv" {
		return parseCSVTestCases(normalized.Content)
	}
	return parseLineBasedTestCases(normalized.Content), nil, nil
}

func parseLineBasedTestCases(content string) []TestCaseInput {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	lines := strings.Split(content, "\n")
	cases := make([]TestCaseInput, 0, minInt(len(lines), maxImportedTestCases))
	for _, line := range lines {
		title := cleanImportedCaseTitle(line)
		if title == "" || strings.HasSuffix(title, ":") || strings.HasPrefix(title, "#") {
			continue
		}
		input := importedTitleToTestCaseInput(title)
		cases = append(cases, input)
		if len(cases) >= maxImportedTestCases {
			break
		}
	}
	return cases
}

func parseCSVTestCases(content string) ([]TestCaseInput, []TestCaseImportSkip, error) {
	reader := csv.NewReader(strings.NewReader(content))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("content must be valid CSV: %w", err)
	}
	if len(records) == 0 {
		return nil, nil, errors.New("content is required")
	}
	header := map[string]int{}
	start := 0
	for index, name := range records[0] {
		key := normalizeCSVHeader(name)
		if key != "" {
			header[key] = index
		}
	}
	if _, ok := header["title"]; ok {
		start = 1
	}
	cases := make([]TestCaseInput, 0, minInt(len(records), maxImportedTestCases))
	skipped := []TestCaseImportSkip{}
	for lineIndex, record := range records[start:] {
		lineNumber := lineIndex + start + 1
		input := csvRecordToTestCaseInput(record, header)
		if input.Title == "" {
			skipped = append(skipped, TestCaseImportSkip{Line: lineNumber, Reason: "missing title", Content: strings.Join(record, ",")})
			continue
		}
		cases = append(cases, input)
		if len(cases) >= maxImportedTestCases {
			break
		}
	}
	return cases, skipped, nil
}

func csvRecordToTestCaseInput(record []string, header map[string]int) TestCaseInput {
	value := func(key string, fallbackIndex int) string {
		if index, ok := header[key]; ok && index >= 0 && index < len(record) {
			return strings.TrimSpace(record[index])
		}
		if fallbackIndex >= 0 && fallbackIndex < len(record) {
			return strings.TrimSpace(record[fallbackIndex])
		}
		return ""
	}
	input := TestCaseInput{
		Title:                   value("title", 0),
		Area:                    value("area", -1),
		Priority:                value("priority", -1),
		Preconditions:           value("preconditions", -1),
		ExpectedResult:          value("expected_result", -1),
		EnvironmentRequirements: value("environment_requirements", -1),
		Tags:                    splitCSVList(value("tags", -1)),
	}
	stepText := value("steps", -1)
	if stepText != "" {
		input.Steps = textToTestCaseSteps(stepText)
	}
	if input.ExpectedResult == "" {
		input.ExpectedResult = value("expected", -1)
	}
	if input.Title != "" && len(input.Steps) == 0 {
		input.Steps = []TestCaseStep{{Action: input.Title}}
	}
	input.Source = defaultImportedCaseSource
	return input
}

func importedTitleToTestCaseInput(title string) TestCaseInput {
	action := title
	expected := ""
	for _, separator := range []string{"->", "=>", " should ", " then "} {
		if index := strings.Index(strings.ToLower(title), separator); index > 0 {
			action = strings.TrimSpace(title[:index])
			expected = strings.TrimSpace(title[index+len(separator):])
			break
		}
	}
	if expected == "" {
		expected = "The behavior matches the documented expectation."
	}
	return TestCaseInput{
		Title:          title,
		Type:           defaultTestCaseType,
		Status:         "needs_review",
		Source:         defaultImportedCaseSource,
		Steps:          []TestCaseStep{{Action: action}},
		ExpectedResult: expected,
	}
}

func cleanImportedCaseTitle(line string) string {
	title := strings.TrimSpace(line)
	title = importBulletPattern.ReplaceAllString(title, "")
	title = strings.Trim(title, "`")
	return collapseWhitespace(title)
}

func normalizeCSVHeader(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "-", "_")
	switch value {
	case "case", "test_case", "name":
		return "title"
	case "expected_result", "expectedresults":
		return "expected_result"
	case "expected":
		return "expected"
	case "env", "environment", "environment_requirements":
		return "environment_requirements"
	default:
		return value
	}
}

func scoreTestCaseQuality(input TestCaseInput) (int, []TestCaseQualityFinding) {
	score := 100
	findings := []TestCaseQualityFinding{}
	add := func(code, message string, penalty int) {
		findings = append(findings, TestCaseQualityFinding{Code: code, Message: message})
		score -= penalty
	}
	if strings.TrimSpace(input.Preconditions) == "" {
		add("missing_preconditions", "Preconditions are missing.", 12)
	}
	if strings.TrimSpace(input.ExpectedResult) == "" {
		add("missing_expected_result", "Expected result is missing.", 24)
	}
	if strings.TrimSpace(input.EnvironmentRequirements) == "" {
		add("missing_environment", "Environment requirements are missing.", 10)
	}
	if len(input.Steps) == 0 {
		add("missing_steps", "Test steps are missing.", 28)
	} else if len(input.Steps) == 1 && len([]rune(input.Steps[0].Action)) < 24 {
		add("vague_steps", "Steps are too short to guide execution reliably.", 12)
	}
	combined := strings.ToLower(strings.Join([]string{input.Title, input.Preconditions, stepsToText(input.Steps), input.ExpectedResult}, " "))
	for _, pattern := range vagueCasePatterns {
		if strings.Contains(combined, strings.ToLower(pattern)) {
			add("vague_language", "The case uses vague language that should be clarified.", 12)
			break
		}
	}
	if score < 0 {
		score = 0
	}
	return score, findings
}

func normalizeTestCaseSteps(values []TestCaseStep) []TestCaseStep {
	steps := make([]TestCaseStep, 0, len(values))
	for _, value := range values {
		action := collapseWhitespace(value.Action)
		expected := strings.TrimSpace(value.Expected)
		if action == "" && expected == "" {
			continue
		}
		steps = append(steps, TestCaseStep{Action: action, Expected: expected})
	}
	return steps
}

func normalizeTags(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		value = strings.TrimPrefix(value, "#")
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func splitCSVList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '|' || r == '\n'
	})
	return uniqueStrings(fields)
}

func textToTestCaseSteps(value string) []TestCaseStep {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	parts := strings.Split(value, "\n")
	steps := make([]TestCaseStep, 0, len(parts))
	for _, part := range parts {
		action := cleanImportedCaseTitle(part)
		if action != "" {
			steps = append(steps, TestCaseStep{Action: action})
		}
	}
	if len(steps) == 0 && strings.TrimSpace(value) != "" {
		steps = []TestCaseStep{{Action: collapseWhitespace(value)}}
	}
	return steps
}

func stepsToText(steps []TestCaseStep) string {
	parts := make([]string, 0, len(steps)*2)
	for _, step := range steps {
		parts = append(parts, step.Action, step.Expected)
	}
	return strings.Join(parts, " ")
}

func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func encodeJSON(value any) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, errors.New("payload must be valid JSON")
	}
	return body, nil
}

func decodeTestCaseSteps(payload []byte) []TestCaseStep {
	var values []TestCaseStep
	if err := json.Unmarshal(payload, &values); err != nil || values == nil {
		return []TestCaseStep{}
	}
	return normalizeTestCaseSteps(values)
}

func decodeStringSlice(payload []byte) []string {
	var values []string
	if err := json.Unmarshal(payload, &values); err != nil || values == nil {
		return []string{}
	}
	return uniqueStrings(values)
}

func decodeQualityFindings(payload []byte) []TestCaseQualityFinding {
	var values []TestCaseQualityFinding
	if err := json.Unmarshal(payload, &values); err != nil || values == nil {
		return []TestCaseQualityFinding{}
	}
	return values
}

func decodeTestCaseSnapshot(payload []byte) TestCase {
	var value TestCase
	_ = json.Unmarshal(payload, &value)
	if value.Steps == nil {
		value.Steps = []TestCaseStep{}
	}
	if value.Dependencies == nil {
		value.Dependencies = []string{}
	}
	if value.Tags == nil {
		value.Tags = []string{}
	}
	if value.QualityFindings == nil {
		value.QualityFindings = []TestCaseQualityFinding{}
	}
	return value
}

func testCaseSnapshot(value TestCase) TestCase {
	value.WorkspaceID = strings.TrimSpace(value.WorkspaceID)
	value.ProjectID = strings.TrimSpace(value.ProjectID)
	value.Steps = append([]TestCaseStep(nil), value.Steps...)
	value.Dependencies = append([]string(nil), value.Dependencies...)
	value.Tags = append([]string(nil), value.Tags...)
	value.QualityFindings = append([]TestCaseQualityFinding(nil), value.QualityFindings...)
	return value
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
