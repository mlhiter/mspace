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
	maxImportedTestCases          = 1000
	maxImportedTestCaseBytes      = 2 * 1024 * 1024
	maxImportedWorkbookBytes      = 2 * 1024 * 1024
	maxImportedWorkbookUnzipBytes = 16 * 1024 * 1024
	maxImportedWorkbookXMLBytes   = 4 * 1024 * 1024
	maxImportPreviewSamples       = 5
	maxImportMappingSampleRows    = 5
	defaultTestCaseType           = "functional"
	defaultTestCaseSource         = "manual"
	defaultImportedCaseSource     = "import"
)

var requiredImportPreviewFields = []string{"title", "preconditions", "steps", "expectedResult", "environmentRequirements"}

var (
	importBulletPattern = regexp.MustCompile(`^\s*(?:[-*+]\s+\[[ xX]\]|\d+[\.)]|[-*+])\s+`)
	importStepPattern   = regexp.MustCompile(`(?:^|\s+)\[(\d+)\]\s*`)
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
	existingDataActionPatterns = []string{
		"delete",
		"remove",
		"archive",
		"update",
		"edit",
		"modify",
		"rename",
		"disable",
		"enable",
		"删除",
		"移除",
		"归档",
		"更新",
		"编辑",
		"修改",
		"重命名",
		"禁用",
		"启用",
	}
	dataSourcePatterns = []string{
		"create",
		"created",
		"creates",
		"new ",
		"add ",
		"seed",
		"fixture",
		"mock data",
		"test data",
		"setup",
		"runcontext",
		"prepared",
		"provision",
		"dependency",
		"depends on",
		"创建",
		"新建",
		"新增",
		"准备",
		"预置",
		"种子",
		"测试数据",
		"模拟数据",
		"前置",
		"依赖",
	}
	allowedTestCaseTypes = map[string]struct{}{
		"functional": {},
		"ui":         {},
		"api":        {},
		"deployment": {},
	}
)

type importColumnDefinition struct {
	field    string
	required bool
	aliases  []string
}

var importColumnDefinitions = []importColumnDefinition{
	{field: "title", required: true, aliases: []string{"case", "test_case", "name", "title/name", "case_name"}},
	{field: "type", aliases: []string{"test_type", "case_type", "test_kind", "kind"}},
	{field: "area", aliases: []string{"area", "module", "feature", "belonging_module"}},
	{field: "priority", aliases: []string{"priority", "level", "severity"}},
	{field: "preconditions", required: true, aliases: []string{"precondition", "preconditions", "setup"}},
	{field: "steps", required: true, aliases: []string{"step", "steps", "actions", "test_steps"}},
	{field: "expected_result", required: true, aliases: []string{"expected", "expected_result", "expectedresults", "expectation"}},
	{field: "environment_requirements", required: true, aliases: []string{"env", "environment", "environment_requirements", "environment_requirement", "test_environment"}},
	{field: "tags", aliases: []string{"tag", "tags", "labels"}},
	{field: "external_id", aliases: []string{"id", "case_id", "test_case_id"}},
	{field: "latest_result", aliases: []string{"latest_result", "execution_result", "last_result"}},
}

var importColumnAliasToField = buildImportColumnAliasToField()

func buildImportColumnAliasToField() map[string]string {
	result := map[string]string{}
	for _, definition := range importColumnDefinitions {
		result[definition.field] = definition.field
		for _, alias := range definition.aliases {
			result[normalizeImportColumnKey(alias)] = definition.field
		}
	}
	return result
}

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
	input.ColumnMappings = normalizeImportColumnMappingOverrides(input.ColumnMappings)
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
		return ImportTestCasesInput{}, fmt.Errorf("workbook must be smaller than %d MB", maxImportedWorkbookBytes/(1024*1024))
	}
	if input.Format != "xlsx" && len([]byte(input.Content)) > maxImportedTestCaseBytes {
		return ImportTestCasesInput{}, fmt.Errorf("import file content must be smaller than %d MB", maxImportedTestCaseBytes/(1024*1024))
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
		return parseCSVTestCases(normalized)
	case "xlsx":
		return parseExcelTestCases(normalized)
	default:
		return parseLineBasedTestCases(normalized.Content), []TestCaseImportSkip{}, nil
	}
}

func previewImportedTestCases(input ImportTestCasesInput) (ImportTestCasesPreview, error) {
	normalized, err := normalizeImportTestCasesInput(input)
	if err != nil {
		return ImportTestCasesPreview{}, err
	}
	inputs, skipped, err := parseImportedTestCases(normalized)
	if err != nil {
		return ImportTestCasesPreview{}, err
	}
	parsedCount := len(inputs)
	if normalized.Format == "csv" || normalized.Format == "xlsx" {
		if records, recordErr := importPreviewRecords(normalized); recordErr == nil {
			_, start := importHeader(records, normalized.ColumnMappings)
			if start > 0 && len(records) >= start {
				parsedCount = len(records[start:])
			}
		}
	}
	contentBytes := len([]byte(normalized.Content))
	if normalized.Format == "xlsx" {
		if workbookBytes, decodeErr := base64.StdEncoding.DecodeString(normalized.Content); decodeErr == nil {
			contentBytes = len(workbookBytes)
		}
	}
	preview := ImportTestCasesPreview{
		Format:                 normalized.Format,
		FileName:               normalized.FileName,
		ContentBytes:           contentBytes,
		MaxContentBytes:        maxImportedTestCaseBytes,
		MaxWorkbookBytes:       maxImportedWorkbookBytes,
		MaxImportableCases:     maxImportedTestCases,
		ParsedCount:            parsedCount,
		SkippedCount:           len(skipped),
		ReachedImportCaseLimit: len(inputs) >= maxImportedTestCases,
		MissingFieldCounts:     map[string]int{},
		QualityFindingCounts:   map[string]int{},
		ImportableCaseSamples:  []TestCaseImportPreviewCase{},
		SkippedSamples:         sampleImportSkips(skipped),
	}
	if normalized.Format == "csv" || normalized.Format == "xlsx" {
		preview.ColumnMappings = importColumnMappings(normalized)
	}
	if normalized.Format == "xlsx" {
		preview.MaxContentBytes = base64.StdEncoding.EncodedLen(maxImportedWorkbookBytes)
	}
	for _, field := range requiredImportPreviewFields {
		preview.MissingFieldCounts[field] = 0
	}
	for _, imported := range inputs {
		normalizedInput, score, findings, err := normalizeTestCaseInput(imported, defaultImportedCaseSource)
		if err != nil {
			preview.SkippedCount++
			if len(preview.SkippedSamples) < maxImportPreviewSamples {
				preview.SkippedSamples = append(preview.SkippedSamples, TestCaseImportSkip{Reason: err.Error(), Content: imported.Title})
			}
			continue
		}
		preview.ImportableCount++
		preview.ReadyCount += boolToInt(normalizedInput.Status == "ready")
		preview.NeedsReviewCount += boolToInt(normalizedInput.Status == "needs_review")
		for _, field := range missingImportFields(normalizedInput) {
			preview.MissingFieldCounts[field]++
		}
		for _, finding := range findings {
			preview.QualityFindingCounts[finding.Code]++
		}
		if len(preview.ImportableCaseSamples) < maxImportPreviewSamples {
			preview.ImportableCaseSamples = append(preview.ImportableCaseSamples, TestCaseImportPreviewCase{
				Title:           normalizedInput.Title,
				Type:            normalizedInput.Type,
				Status:          normalizedInput.Status,
				QualityScore:    score,
				MissingFields:   missingImportFields(normalizedInput),
				QualityFindings: qualityFindingsOrEmpty(findings),
			})
		}
	}
	if preview.ImportableCount == 0 && preview.SkippedCount == 0 {
		return ImportTestCasesPreview{}, errors.New("content cannot be empty")
	}
	return normalizeImportTestCasesPreview(preview), nil
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

func sampleImportSkips(values []TestCaseImportSkip) []TestCaseImportSkip {
	limit := minInt(len(values), maxImportPreviewSamples)
	result := make([]TestCaseImportSkip, 0, limit)
	for _, value := range values[:limit] {
		result = append(result, truncateImportSkip(value))
	}
	return result
}

func truncateImportSkip(value TestCaseImportSkip) TestCaseImportSkip {
	value.Content = truncateString(value.Content, 180)
	return value
}

func missingImportFields(input TestCaseInput) []string {
	missing := []string{}
	if strings.TrimSpace(input.Title) == "" {
		missing = append(missing, "title")
	}
	if strings.TrimSpace(input.Preconditions) == "" {
		missing = append(missing, "preconditions")
	}
	if len(input.Steps) == 0 {
		missing = append(missing, "steps")
	}
	if strings.TrimSpace(input.ExpectedResult) == "" {
		missing = append(missing, "expectedResult")
	}
	if strings.TrimSpace(input.EnvironmentRequirements) == "" {
		missing = append(missing, "environmentRequirements")
	}
	return missing
}

func parseCSVTestCases(input ImportTestCasesInput) ([]TestCaseInput, []TestCaseImportSkip, error) {
	reader := csv.NewReader(strings.NewReader(input.Content))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("content must be valid CSV: %w", err)
	}
	if len(records) == 0 {
		return nil, nil, errors.New("content is required")
	}
	cases, skipped := recordsToTestCaseInputs(records, input.ColumnMappings)
	return cases, skipped, nil
}

func parseExcelTestCases(input ImportTestCasesInput) ([]TestCaseInput, []TestCaseImportSkip, error) {
	workbookBytes, err := base64.StdEncoding.DecodeString(input.Content)
	if err != nil {
		return nil, nil, errors.New("content must be a base64 encoded .xlsx file")
	}
	if len(workbookBytes) == 0 {
		return nil, nil, errors.New("content is required")
	}
	if len(workbookBytes) > maxImportedWorkbookBytes {
		return nil, nil, fmt.Errorf("workbook must be smaller than %d MB", maxImportedWorkbookBytes/(1024*1024))
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
		cases, skipped := importRecordsToTestCaseInputs(records, input.ColumnMappings)
		if len(cases) == 0 && len(skipped) == 0 {
			continue
		}
		return cases, skipped, nil
	}
	return nil, nil, errors.New("workbook must contain at least one non-empty worksheet")
}

func recordsToTestCaseInputs(records [][]string, mappings []TestCaseImportColumnMapping) ([]TestCaseInput, []TestCaseImportSkip) {
	return importRecordsToTestCaseInputs(recordsToImportRecords(records), mappings)
}

type importRecord struct {
	line   int
	values []string
}

func importRecordsToTestCaseInputs(records []importRecord, mappings []TestCaseImportColumnMapping) ([]TestCaseInput, []TestCaseImportSkip) {
	header, start := importHeader(records, mappings)
	cases := make([]TestCaseInput, 0, minInt(len(records), maxImportedTestCases))
	skipped := []TestCaseImportSkip{}
	for lineIndex, record := range records[start:] {
		lineNumber := lineIndex + start + 1
		if record.line > 0 {
			lineNumber = record.line
		}
		input := csvRecordToTestCaseInput(record.values, header, start == 0)
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

func importHeader(records []importRecord, mappings []TestCaseImportColumnMapping) (map[string]int, int) {
	header := map[string]int{}
	start := 0
	if len(records) == 0 {
		return header, start
	}
	mappingHeader := importHeaderFromMappings(records[0].values, mappings)
	if len(mappingHeader) > 0 {
		for field, index := range mappingHeader {
			header[field] = index
		}
		start = 1
		return header, start
	}
	matchedColumns := 0
	matchedRequiredColumn := false
	for index, name := range records[0].values {
		key := normalizeCSVHeader(name)
		if key != "" {
			if _, exists := header[key]; exists {
				continue
			}
			header[key] = index
			matchedColumns++
			if isRequiredImportColumn(key) {
				matchedRequiredColumn = true
			}
		}
	}
	if _, ok := header["title"]; ok || (matchedColumns >= 2 && matchedRequiredColumn) {
		start = 1
	} else if looksLikeImportHeader(records) {
		start = 1
	}
	return header, start
}

func looksLikeImportHeader(records []importRecord) bool {
	if len(records) < 2 || len(records[0].values) < 2 {
		return false
	}
	first := records[0].values
	second := records[1].values
	firstNonEmpty := 0
	for _, value := range first {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		firstNonEmpty++
		if len([]rune(value)) > 32 {
			return false
		}
	}
	if firstNonEmpty < 2 {
		return false
	}
	secondLongValues := 0
	for _, value := range second {
		if len([]rune(strings.TrimSpace(value))) > 32 {
			secondLongValues++
		}
	}
	return secondLongValues > 0
}

func importHeaderFromMappings(headers []string, mappings []TestCaseImportColumnMapping) map[string]int {
	result := map[string]int{}
	usedIndexes := map[int]bool{}
	for _, mapping := range mappings {
		field := normalizeImportMappingField(mapping.Field)
		if field == "" {
			continue
		}
		index := mapping.Index
		if index < 0 && strings.TrimSpace(mapping.Source) != "" {
			index = importColumnIndexBySource(headers, mapping.Source)
		}
		if index < 0 || index >= len(headers) || usedIndexes[index] {
			continue
		}
		if _, exists := result[field]; exists {
			continue
		}
		result[field] = index
		usedIndexes[index] = true
	}
	return result
}

func importColumnIndexBySource(headers []string, source string) int {
	sourceKey := normalizeImportColumnKey(source)
	if sourceKey == "" {
		return -1
	}
	for index, header := range headers {
		if normalizeImportColumnKey(header) == sourceKey {
			return index
		}
	}
	return -1
}

func isRequiredImportColumn(field string) bool {
	for _, definition := range importColumnDefinitions {
		if definition.field == field {
			return definition.required
		}
	}
	return false
}

func recordsToImportRecords(records [][]string) []importRecord {
	importRecords := make([]importRecord, 0, len(records))
	for index, record := range records {
		importRecords = append(importRecords, importRecord{line: index + 1, values: record})
	}
	return importRecords
}

func importColumnMappings(input ImportTestCasesInput) []TestCaseImportColumnMapping {
	records, err := importPreviewRecords(input)
	if err != nil || len(records) == 0 {
		return []TestCaseImportColumnMapping{}
	}
	header, start := importHeader(records, input.ColumnMappings)
	if start == 0 {
		return []TestCaseImportColumnMapping{}
	}
	required := map[string]bool{}
	for _, definition := range importColumnDefinitions {
		required[definition.field] = definition.required
	}
	result := make([]TestCaseImportColumnMapping, 0, len(records[0].values))
	for index, name := range records[0].values {
		source := strings.TrimPrefix(strings.TrimSpace(name), "\ufeff")
		field := fieldForImportColumn(records[0].values, input.ColumnMappings, index, source)
		result = append(result, TestCaseImportColumnMapping{
			Source:     source,
			Field:      field,
			Index:      index,
			Matched:    field != "",
			Required:   field != "" && required[field],
			Confidence: importMappingConfidence(input.ColumnMappings, index, source),
			Reason:     importMappingReason(input.ColumnMappings, index, source),
			Strategy:   importMappingStrategy(input.ColumnMappings, index, source),
		})
	}
	for _, definition := range importColumnDefinitions {
		if !definition.required {
			continue
		}
		if _, ok := header[definition.field]; ok {
			continue
		}
		result = append(result, TestCaseImportColumnMapping{
			Field:    definition.field,
			Index:    -1,
			Matched:  false,
			Required: true,
		})
	}
	return result
}

func fieldForImportColumn(headers []string, mappings []TestCaseImportColumnMapping, index int, source string) string {
	for _, mapping := range mappings {
		field := normalizeImportMappingField(mapping.Field)
		if field == "" {
			continue
		}
		if mapping.Index == index {
			return field
		}
		if mapping.Index < 0 && strings.TrimSpace(mapping.Source) != "" && importColumnIndexBySource(headers, mapping.Source) == index {
			return field
		}
	}
	return normalizeCSVHeader(source)
}

func importMappingConfidence(mappings []TestCaseImportColumnMapping, index int, source string) float64 {
	mapping, ok := importColumnMappingForIndex(mappings, index, source)
	if !ok {
		return 0
	}
	return mapping.Confidence
}

func importMappingReason(mappings []TestCaseImportColumnMapping, index int, source string) string {
	mapping, ok := importColumnMappingForIndex(mappings, index, source)
	if !ok {
		return ""
	}
	return mapping.Reason
}

func importMappingStrategy(mappings []TestCaseImportColumnMapping, index int, source string) string {
	mapping, ok := importColumnMappingForIndex(mappings, index, source)
	if !ok {
		return ""
	}
	return mapping.Strategy
}

func importColumnMappingForIndex(mappings []TestCaseImportColumnMapping, index int, source string) (TestCaseImportColumnMapping, bool) {
	for _, mapping := range mappings {
		if mapping.Index == index {
			return mapping, true
		}
		if mapping.Index < 0 && strings.TrimSpace(mapping.Source) != "" && normalizeImportColumnKey(mapping.Source) == normalizeImportColumnKey(source) {
			return mapping, true
		}
	}
	return TestCaseImportColumnMapping{}, false
}

func importPreviewRecords(input ImportTestCasesInput) ([]importRecord, error) {
	switch input.Format {
	case "csv":
		reader := csv.NewReader(strings.NewReader(input.Content))
		reader.TrimLeadingSpace = true
		reader.FieldsPerRecord = -1
		records, err := reader.ReadAll()
		if err != nil {
			return nil, err
		}
		return recordsToImportRecords(records), nil
	case "xlsx":
		workbookBytes, err := base64.StdEncoding.DecodeString(input.Content)
		if err != nil {
			return nil, err
		}
		file, err := excelize.OpenReader(bytes.NewReader(workbookBytes), excelize.Options{
			UnzipSizeLimit:    maxImportedWorkbookUnzipBytes,
			UnzipXMLSizeLimit: maxImportedWorkbookXMLBytes,
		})
		if err != nil {
			return nil, err
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
				return nil, err
			}
			records := nonEmptyImportRecords(rows)
			if len(records) > 0 {
				return records, nil
			}
		}
	}
	return []importRecord{}, nil
}

func buildImportMappingRuntimeTaskInput(project Project, runtimeMode string, input TestCaseImportMappingTaskInput) (CreateRuntimeTaskInput, error) {
	normalized, err := normalizeImportTestCasesInput(ImportTestCasesInput{
		Format:   input.Format,
		Content:  input.Content,
		FileName: input.FileName,
	})
	if err != nil {
		return CreateRuntimeTaskInput{}, err
	}
	if normalized.Format != "csv" && normalized.Format != "xlsx" {
		return CreateRuntimeTaskInput{}, errors.New("worker column mapping is only available for csv or xlsx imports")
	}
	records, err := importPreviewRecords(normalized)
	if err != nil {
		return CreateRuntimeTaskInput{}, err
	}
	if len(records) == 0 {
		return CreateRuntimeTaskInput{}, errors.New("content is required")
	}
	header := make([]string, 0, len(records[0].values))
	for _, value := range records[0].values {
		header = append(header, strings.TrimPrefix(strings.TrimSpace(value), "\ufeff"))
	}
	sampleRows := make([][]string, 0, maxImportMappingSampleRows)
	for _, record := range records[1:] {
		if len(sampleRows) >= maxImportMappingSampleRows {
			break
		}
		row := make([]string, 0, len(record.values))
		for _, value := range record.values {
			row = append(row, truncateImportSampleValue(collapseWhitespace(value), 240))
		}
		sampleRows = append(sampleRows, row)
	}
	payload := map[string]any{
		"workspaceId": project.WorkspaceID,
		"projectId":   project.ID,
		"project": map[string]string{
			"id":   project.ID,
			"name": project.Name,
		},
		"format":                normalized.Format,
		"fileName":              normalized.FileName,
		"headers":               header,
		"sampleRows":            sampleRows,
		"systemFields":          importMappingSystemFields(),
		"prompt":                buildImportMappingPrompt(project, normalized, header, sampleRows),
		"developerInstructions": defaultImportMappingDeveloperInstructions(),
	}
	payloadBody, err := json.Marshal(payload)
	if err != nil {
		return CreateRuntimeTaskInput{}, err
	}
	capabilities, err := json.Marshal(map[string]bool{"codex": true})
	if err != nil {
		return CreateRuntimeTaskInput{}, err
	}
	return CreateRuntimeTaskInput{
		ProjectID:            project.ID,
		Kind:                 "test_case_import_mapping",
		Priority:             0,
		RuntimeMode:          strings.TrimSpace(runtimeMode),
		RequiredCapabilities: capabilities,
		Payload:              payloadBody,
		ServerManaged:        true,
	}, nil
}

func importMappingSystemFields() []map[string]string {
	return []map[string]string{
		{"field": "title", "required": "true", "description": "Test case title or name."},
		{"field": "type", "required": "false", "description": "Fixed mspace case type: functional, ui, api, or deployment."},
		{"field": "area", "required": "false", "description": "Feature/module/business area."},
		{"field": "priority", "required": "false", "description": "Priority or severity label such as P0/P1."},
		{"field": "preconditions", "required": "true", "description": "Setup or preconditions required before execution."},
		{"field": "steps", "required": "true", "description": "Execution steps, actions, or step description."},
		{"field": "expected_result", "required": "true", "description": "Expected outcome/result."},
		{"field": "environment_requirements", "required": "true", "description": "Environment, notes, browser/device/network, or setup requirements."},
		{"field": "tags", "required": "false", "description": "Business category, labels, component tags, or free-form grouping."},
		{"field": "external_id", "required": "false", "description": "Source case id or external identifier."},
		{"field": "latest_result", "required": "false", "description": "Historical execution state; useful for preview context but not canonical case definition."},
	}
}

func buildImportMappingPrompt(project Project, input ImportTestCasesInput, headers []string, sampleRows [][]string) string {
	body := map[string]any{
		"task":         "Suggest column mappings for a test case import preview.",
		"projectName":  project.Name,
		"format":       input.Format,
		"fileName":     input.FileName,
		"headers":      headers,
		"sampleRows":   sampleRows,
		"systemFields": importMappingSystemFields(),
		"rules": []string{
			"Return suggestions only; do not import, persist, or modify test cases.",
			"Map each source column to at most one system field.",
			"Map a system field at most once.",
			"Use latest_result for historical execution status/result columns instead of type or expected_result.",
			"Use tags for business categories that are not one of the fixed system types.",
			"Leave unrelated columns unmapped by omitting them from suggestions.",
			"Use confidence from 0 to 1 and include a short reason.",
		},
	}
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return "Suggest column mappings for this test case import. Return JSON only."
	}
	return "Analyze this import sample and return JSON only.\n\n" + string(data) + "\n\nExpected output shape:\n{\"suggestions\":[{\"source\":\"用例名称\",\"field\":\"title\",\"index\":1,\"confidence\":0.98,\"reason\":\"column contains case names\"}],\"warnings\":[]}"
}

func defaultImportMappingDeveloperInstructions() string {
	return strings.TrimSpace(`
You are helping mspace preview a test-case import. You only produce column mapping suggestions.
Return exactly one JSON object and no markdown fences or prose.
Allowed fields are: title, type, area, priority, preconditions, steps, expected_result, environment_requirements, tags, external_id, latest_result.
Never claim data was imported or modified. The user must confirm mappings before the server writes anything.
`)
}

func truncateImportSampleValue(value string, limit int) string {
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
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

func csvRecordToTestCaseInput(record []string, header map[string]int, allowFallback bool) TestCaseInput {
	value := func(key string, fallbackIndex int) string {
		if index, ok := header[key]; ok && index >= 0 && index < len(record) {
			return strings.TrimSpace(record[index])
		}
		if allowFallback && fallbackIndex >= 0 && fallbackIndex < len(record) {
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
	if externalID := value("external_id", -1); externalID != "" {
		input.Tags = append(input.Tags, externalID)
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
	return importColumnAliasToField[normalizeImportColumnKey(value)]
}

func normalizeImportColumnMappingOverrides(values []TestCaseImportColumnMapping) []TestCaseImportColumnMapping {
	result := make([]TestCaseImportColumnMapping, 0, len(values))
	usedIndexes := map[int]bool{}
	usedFields := map[string]bool{}
	for _, value := range values {
		field := normalizeImportMappingField(value.Field)
		if field == "" {
			continue
		}
		value.Source = strings.TrimPrefix(strings.TrimSpace(value.Source), "\ufeff")
		value.Reason = collapseWhitespace(value.Reason)
		value.Strategy = strings.ToLower(strings.TrimSpace(value.Strategy))
		value.Field = field
		if value.Index < 0 && value.Source == "" {
			continue
		}
		if value.Index >= 0 {
			if usedIndexes[value.Index] {
				continue
			}
			usedIndexes[value.Index] = true
		}
		if usedFields[field] {
			continue
		}
		usedFields[field] = true
		if value.Confidence < 0 {
			value.Confidence = 0
		}
		if value.Confidence > 1 {
			value.Confidence = 1
		}
		value.Matched = true
		value.Required = isRequiredImportColumn(field)
		if value.Strategy == "" {
			value.Strategy = "confirmed"
		}
		result = append(result, value)
	}
	return result
}

func normalizeImportMappingField(value string) string {
	field := strings.TrimSpace(value)
	switch field {
	case "expectedResult":
		field = "expected_result"
	case "environmentRequirements":
		field = "environment_requirements"
	case "externalId":
		field = "external_id"
	case "latestResult":
		field = "latest_result"
	default:
		field = normalizeCSVHeader(field)
		if field == "" {
			field = strings.ToLower(strings.TrimSpace(value))
		}
	}
	if !isAllowedImportMappingField(field) {
		return ""
	}
	return field
}

func isAllowedImportMappingField(field string) bool {
	for _, definition := range importColumnDefinitions {
		if definition.field == field {
			return true
		}
	}
	return false
}

func normalizeImportColumnKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "\ufeff")
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.ReplaceAll(value, "（", "(")
	value = strings.ReplaceAll(value, "）", ")")
	return value
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
	if needsExistingDataSource(input, combined) && !declaresExistingDataSource(input, combined) {
		add("missing_data_source", "State whether the target data is created by the case, setup, or a dependency.", 18)
	}
	if score < 0 {
		score = 0
	}
	return score, findings
}

func needsExistingDataSource(input TestCaseInput, combined string) bool {
	if strings.TrimSpace(combined) == "" {
		combined = strings.ToLower(strings.Join([]string{input.Title, input.Preconditions, stepsToText(input.Steps), input.ExpectedResult}, " "))
	}
	return containsAnyPattern(combined, existingDataActionPatterns)
}

func declaresExistingDataSource(input TestCaseInput, combined string) bool {
	if len(input.Dependencies) > 0 {
		return true
	}
	if strings.TrimSpace(combined) == "" {
		combined = strings.ToLower(strings.Join([]string{input.Title, input.Preconditions, stepsToText(input.Steps), input.ExpectedResult}, " "))
	}
	return containsAnyPattern(combined, dataSourcePatterns)
}

func containsAnyPattern(value string, patterns []string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, pattern := range patterns {
		if strings.Contains(value, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
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
	if len(parts) == 1 {
		parts = splitNumberedInlineSteps(value)
	}
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

func splitNumberedInlineSteps(value string) []string {
	matches := importStepPattern.FindAllStringSubmatchIndex(value, -1)
	if len(matches) == 0 {
		return []string{value}
	}
	parts := make([]string, 0, len(matches))
	for index, match := range matches {
		start := match[1]
		end := len(value)
		if index+1 < len(matches) {
			end = matches[index+1][0]
		}
		part := strings.TrimSpace(value[start:end])
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return []string{value}
	}
	return parts
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
