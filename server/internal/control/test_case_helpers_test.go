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
	if !hasUnmatchedImportColumnMapping(preview.ColumnMappings, "执行结果") {
		t.Fatalf("expected execution result column to stay unmatched, got %+v", preview.ColumnMappings)
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
