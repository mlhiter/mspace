package control

import (
	"strings"
	"testing"
)

func TestParseChineseCSVImportColumns(t *testing.T) {
	content := strings.Join([]string{
		"\ufeff用例ID,用例名称,所属模块,测试类别,前置条件,步骤描述,预期结果,备注,用例等级,执行结果",
		"OSV2-001,未登录访问 Sealos 平台进入登录页,A. 访问入口与桌面承载,鉴权入口,工作空间 ws-osv2-main 存在；测试数据前缀 osv2-001 在工作空间 ws-osv2-main 中唯一。,[1] 测试执行者打开无登录态浏览器会话 browser-osv2-001。 [2] 测试执行者访问 Sealos 平台地址 sealos-platform-url-osv2-001。 [3] 测试执行者等待页面完成首屏渲染。 [4] 测试执行者读取当前页面 URL login-url-osv2-001。 [5] 测试执行者记录登录页截图 screenshot-osv2-001。 [6] 测试执行者将登录页截图、Network 响应 写入执行记录 evidence-osv2-001。,浏览器停留在登录页，页面显示账号登录表单，Sealos 桌面未显示。,浏览器页面、Network。,P0,未执行",
	}, "\n")
	input := ImportTestCasesInput{Format: "csv", FileName: "object-storage.csv", Content: content}

	previewWithoutMapping, err := previewImportedTestCases(input)
	if err != nil {
		t.Fatalf("preview chinese csv import without mapping: %v", err)
	}
	if previewWithoutMapping.ImportableCount != 0 || previewWithoutMapping.SkippedCount != 1 {
		t.Fatalf("expected no importable cases without confirmed mapping, got %+v", previewWithoutMapping)
	}
	if !hasUnmatchedImportColumnMapping(previewWithoutMapping.ColumnMappings, "用例名称") ||
		!hasUnmatchedImportColumnMapping(previewWithoutMapping.ColumnMappings, "步骤描述") ||
		!hasUnmatchedImportColumnMapping(previewWithoutMapping.ColumnMappings, "用例等级") {
		t.Fatalf("expected chinese source columns to stay unmatched without worker/user mapping, got %+v", previewWithoutMapping.ColumnMappings)
	}

	input.ColumnMappings = []TestCaseImportColumnMapping{
		{Source: "用例ID", Field: "external_id", Index: 0, Confidence: 0.98, Strategy: "worker", Reason: "case identifier"},
		{Source: "用例名称", Field: "title", Index: 1, Confidence: 0.99, Strategy: "worker", Reason: "case name"},
		{Source: "所属模块", Field: "area", Index: 2, Confidence: 0.94, Strategy: "worker", Reason: "feature area"},
		{Source: "测试类别", Field: "tags", Index: 3, Confidence: 0.86, Strategy: "worker", Reason: "business category, not system type"},
		{Source: "前置条件", Field: "preconditions", Index: 4, Confidence: 0.98, Strategy: "worker", Reason: "setup text"},
		{Source: "步骤描述", Field: "steps", Index: 5, Confidence: 0.99, Strategy: "worker", Reason: "numbered steps"},
		{Source: "预期结果", Field: "expected_result", Index: 6, Confidence: 0.99, Strategy: "worker", Reason: "expected result"},
		{Source: "备注", Field: "environment_requirements", Index: 7, Confidence: 0.72, Strategy: "worker", Reason: "environment note"},
		{Source: "用例等级", Field: "priority", Index: 8, Confidence: 0.96, Strategy: "worker", Reason: "priority level"},
		{Source: "执行结果", Field: "latest_result", Index: 9, Confidence: 0.9, Strategy: "worker", Reason: "historical execution state"},
	}

	cases, skipped, err := parseImportedTestCases(input)
	if err != nil {
		t.Fatalf("parse chinese csv import: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("expected no skipped rows, got %+v", skipped)
	}
	if len(cases) != 1 {
		t.Fatalf("expected one parsed case, got %+v", cases)
	}
	testCase := cases[0]
	if testCase.Title != "未登录访问 Sealos 平台进入登录页" {
		t.Fatalf("expected title from 用例名称, got %+v", testCase)
	}
	if testCase.Type != "" {
		t.Fatalf("expected business 测试类别 not to map to system type, got %+v", testCase)
	}
	if testCase.Area != "A. 访问入口与桌面承载" || testCase.Priority != "P0" {
		t.Fatalf("unexpected area or priority: %+v", testCase)
	}
	if testCase.ExpectedResult != "浏览器停留在登录页，页面显示账号登录表单，Sealos 桌面未显示。" || testCase.EnvironmentRequirements != "浏览器页面、Network。" {
		t.Fatalf("unexpected expected/environment fields: %+v", testCase)
	}
	if len(testCase.Steps) != 6 || testCase.Steps[0].Action != "测试执行者打开无登录态浏览器会话 browser-osv2-001。" {
		t.Fatalf("expected inline numbered steps to split, got %+v", testCase.Steps)
	}
	if !containsString(testCase.Tags, "OSV2-001") || !containsString(testCase.Tags, "鉴权入口") {
		t.Fatalf("expected case id and business category tags, got %+v", testCase.Tags)
	}

	preview, err := previewImportedTestCases(input)
	if err != nil {
		t.Fatalf("preview chinese csv import: %v", err)
	}
	if preview.ImportableCount != 1 || preview.SkippedCount != 0 || preview.MissingFieldCounts["steps"] != 0 {
		t.Fatalf("unexpected preview counts: %+v", preview)
	}
	if !hasImportColumnMapping(preview.ColumnMappings, "用例名称", "title") ||
		!hasImportColumnMapping(preview.ColumnMappings, "步骤描述", "steps") ||
		!hasImportColumnMapping(preview.ColumnMappings, "用例等级", "priority") ||
		!hasImportColumnMapping(preview.ColumnMappings, "测试类别", "tags") {
		t.Fatalf("expected chinese csv column mappings, got %+v", preview.ColumnMappings)
	}
	if !hasImportColumnMapping(preview.ColumnMappings, "执行结果", "latest_result") {
		t.Fatalf("expected execution result column to map as historical state, got %+v", preview.ColumnMappings)
	}
}

func hasImportColumnMapping(values []TestCaseImportColumnMapping, source, field string) bool {
	for _, value := range values {
		if value.Source == source && value.Field == field && value.Matched {
			return true
		}
	}
	return false
}

func hasUnmatchedImportColumnMapping(values []TestCaseImportColumnMapping, source string) bool {
	for _, value := range values {
		if value.Source == source && value.Field == "" && !value.Matched {
			return true
		}
	}
	return false
}
