package control

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	maxImportedTestCases          = 100
	maxImportedTestCaseBytes      = 256 * 1024
	maxImportedWorkbookBytes      = 2 * 1024 * 1024
	maxImportedWorkbookUnzipBytes = 16 * 1024 * 1024
	maxImportedWorkbookXMLBytes   = 4 * 1024 * 1024
	defaultTestCaseType           = "functional"
	defaultTestCaseSource         = "manual"
	defaultImportedCaseSource     = "import"
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
	allowedTestCaseTypes = map[string]struct{}{
		"functional": {},
		"ui":         {},
		"api":        {},
		"deployment": {},
	}
)

func normalizeTestCaseInput(input TestCaseInput, sourceFallback string) (TestCaseInput, int, []TestCaseQualityFinding, error) {
	input.Title = collapseWhitespace(input.Title)
	input.Type = normalizeTestCaseType(input.Type)
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
	if !isAllowedTestCaseType(input.Type) {
		return TestCaseInput{}, 0, nil, errors.New("type must be functional, ui, api, or deployment")
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
	input.FileName = strings.TrimSpace(input.FileName)
	if input.Format == "" {
		input.Format = "markdown"
	}
	if input.Format == "excel" {
		input.Format = "xlsx"
	}
	switch input.Format {
	case "markdown", "text", "csv", "xlsx":
	default:
		return ImportTestCasesInput{}, errors.New("format must be markdown, text, csv, or xlsx")
	}
	if input.Content == "" {
		return ImportTestCasesInput{}, errors.New("content is required")
	}
	if input.Format == "xlsx" && len([]byte(input.Content)) > base64.StdEncoding.EncodedLen(maxImportedWorkbookBytes) {
		return ImportTestCasesInput{}, fmt.Errorf("workbook must be smaller than %d bytes", maxImportedWorkbookBytes)
	}
	if input.Format != "xlsx" && len([]byte(input.Content)) > maxImportedTestCaseBytes {
		return ImportTestCasesInput{}, fmt.Errorf("content must be smaller than %d bytes", maxImportedTestCaseBytes)
	}
	return input, nil
}

func parseImportedTestCases(input ImportTestCasesInput) ([]TestCaseInput, []TestCaseImportSkip, error) {
	normalized, err := normalizeImportTestCasesInput(input)
	if err != nil {
		return nil, nil, err
	}
	switch normalized.Format {
	case "csv":
		return parseCSVTestCases(normalized.Content)
	case "xlsx":
		return parseExcelTestCases(normalized.Content)
	default:
		return parseLineBasedTestCases(normalized.Content), []TestCaseImportSkip{}, nil
	}
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
	cases, skipped := recordsToTestCaseInputs(records)
	return cases, skipped, nil
}

func parseExcelTestCases(content string) ([]TestCaseInput, []TestCaseImportSkip, error) {
	workbookBytes, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, nil, errors.New("content must be a base64 encoded .xlsx file")
	}
	if len(workbookBytes) == 0 {
		return nil, nil, errors.New("content is required")
	}
	if len(workbookBytes) > maxImportedWorkbookBytes {
		return nil, nil, fmt.Errorf("workbook must be smaller than %d bytes", maxImportedWorkbookBytes)
	}
	file, err := excelize.OpenReader(bytes.NewReader(workbookBytes), excelize.Options{
		UnzipSizeLimit:    maxImportedWorkbookUnzipBytes,
		UnzipXMLSizeLimit: maxImportedWorkbookXMLBytes,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("content must be a valid .xlsx workbook: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()
	for _, sheet := range file.GetSheetList() {
		rows, err := file.GetRows(sheet)
		if err != nil {
			if errors.Is(err, io.EOF) {
				continue
			}
			return nil, nil, fmt.Errorf("read worksheet %q: %w", sheet, err)
		}
		records := nonEmptyImportRecords(rows)
		if len(records) == 0 {
			continue
		}
		cases, skipped := importRecordsToTestCaseInputs(records)
		if len(cases) == 0 && len(skipped) == 0 {
			continue
		}
		return cases, skipped, nil
	}
	return nil, nil, errors.New("workbook must contain at least one non-empty worksheet")
}

func recordsToTestCaseInputs(records [][]string) ([]TestCaseInput, []TestCaseImportSkip) {
	importRecords := make([]importRecord, 0, len(records))
	for index, record := range records {
		importRecords = append(importRecords, importRecord{line: index + 1, values: record})
	}
	return importRecordsToTestCaseInputs(importRecords)
}

type importRecord struct {
	line   int
	values []string
}

func importRecordsToTestCaseInputs(records []importRecord) ([]TestCaseInput, []TestCaseImportSkip) {
	header := map[string]int{}
	start := 0
	for index, name := range records[0].values {
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
		if record.line > 0 {
			lineNumber = record.line
		}
		input := csvRecordToTestCaseInput(record.values, header)
		if input.Title == "" {
			skipped = append(skipped, TestCaseImportSkip{Line: lineNumber, Reason: "missing title", Content: strings.Join(record.values, ",")})
			continue
		}
		cases = append(cases, input)
		if len(cases) >= maxImportedTestCases {
			break
		}
	}
	return cases, skipped
}

func nonEmptyImportRecords(records [][]string) []importRecord {
	result := make([]importRecord, 0, len(records))
	for index, record := range records {
		if recordHasValue(record) {
			result = append(result, importRecord{line: index + 1, values: record})
		}
	}
	return result
}

func recordHasValue(record []string) bool {
	for _, value := range record {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
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
		Type:                    value("type", -1),
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
	case "case", "test_case", "name", "title/name", "标题", "用例标题":
		return "title"
	case "test_type", "case_type", "test_kind", "kind", "用例类型", "测试类型", "类型":
		return "type"
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

func isAllowedTestCaseType(value string) bool {
	_, ok := allowedTestCaseTypes[strings.TrimSpace(value)]
	return ok
}

func normalizeTestCaseType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, " ", "_")
	switch value {
	case "":
		return ""
	case "functional", "function", "functional_test", "functional_testing", "功能", "功能测试", "功能_测试":
		return "functional"
	case "ui", "ui_test", "ui_testing", "ui_测试", "界面", "界面测试", "前端", "前端测试", "用户界面", "用户界面测试":
		return "ui"
	case "api", "api_test", "api_testing", "api_测试", "接口", "接口测试":
		return "api"
	case "deployment", "deploy", "deployment_test", "deployment_testing", "deploy_test", "deploy_testing", "部署", "部署测试", "部署_测试", "发布", "发布测试", "发布_测试":
		return "deployment"
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
	value.Steps = testCaseStepsOrEmpty(value.Steps)
	value.Dependencies = stringsOrEmpty(value.Dependencies)
	value.Tags = stringsOrEmpty(value.Tags)
	value.QualityFindings = qualityFindingsOrEmpty(value.QualityFindings)
	return value
}

func testCaseStepsOrEmpty(values []TestCaseStep) []TestCaseStep {
	if values == nil {
		return []TestCaseStep{}
	}
	return append([]TestCaseStep{}, values...)
}

func stringsOrEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return append([]string{}, values...)
}

func qualityFindingsOrEmpty(values []TestCaseQualityFinding) []TestCaseQualityFinding {
	if values == nil {
		return []TestCaseQualityFinding{}
	}
	return append([]TestCaseQualityFinding{}, values...)
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
