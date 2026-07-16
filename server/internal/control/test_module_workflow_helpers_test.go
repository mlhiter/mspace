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
		"platformUrl",
		"appEntry",
		"directFrontendUrl",
		"browserSessionStrategy",
		"namespace",
		"preconditionStatus",
		"sessionNotes",
		"bootstrapNotes",
		"platform entry",
		"direct app HTTP 200 as a health signal only",
		"platform-provided session",
		"Do not persist plaintext secrets",
		"presence, changed/unchanged, last-four characters, or a non-reversible hash",
		"Use Simplified Chinese",
	)
}

func TestBuildTestRunExecutionIssueBodyIncludesUIHarnessGuidance(t *testing.T) {
	runContext := json.RawMessage(`{"sealosUrl":"https://sealos.example.test","frontendUrl":"https://app.example.test","directFrontendUrl":"https://app.example.test","apiUrl":"https://api.example.test","appEntry":"Object Storage desktop tile","browserSessionStrategy":"reuse existing session"}`)
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
		"browser-backed cases",
		"real user entry path",
		"reuse an existing CDP page",
		"setup context and app/session bootstrap blockers",
		"directFrontendUrl",
		"platformUrl",
		"appEntry",
		"browserSessionStrategy",
		"preconditionStatus",
		"log in through that platform URL first",
		"direct app HTTP 200",
		"platform session",
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

func TestFunctionalPlatformCaseRequiresBrowserHarness(t *testing.T) {
	testCase := TestCase{
		ID:            "case-functional-platform",
		Title:         "访问密钥弹窗未展示 .env 下载入口时记录入口缺失",
		Type:          "functional",
		Preconditions: "账号 acct-osv2-owner 存在。",
		Steps: []TestCaseStep{
			{Action: "测试执行者使用账号 acct-osv2-owner 登录 Sealos 平台。"},
			{Action: "测试执行者在 Sealos 桌面点击对象存储图标。"},
			{Action: "测试执行者点击左侧底部访问密钥入口。"},
			{Action: "测试执行者等待访问密钥弹窗显示。"},
		},
		ExpectedResult: "执行记录包含 env-download-visible-osv2-335=false。",
	}

	if !testRunBatchRequiresBrowser([]TestCase{testCase}) {
		t.Fatal("expected functional Sealos desktop case to require browser/CDP")
	}
	capabilities, err := testRunExecutionRequiredCapabilities([]TestCase{testCase})
	if err != nil {
		t.Fatalf("required capabilities: %v", err)
	}
	capabilitiesText := string(capabilities)
	assertContainsAll(t, capabilitiesText, `"codex":true`, `"browser":true`, `"chrome_cdp":true`)

	body := buildTestRunExecutionIssueBody(TestRun{
		ID:           "test-run-functional-platform",
		Source:       "plan",
		TargetType:   "branch",
		Environment:  "62",
		RunContext:   json.RawMessage(`{"sealosUrl":"https://192.168.0.62.nip.io/","frontendUrl":"https://objectstorage.192.168.0.62.nip.io/","directFrontendUrl":"https://objectstorage.192.168.0.62.nip.io/","appEntry":"对象存储桌面图标"}`),
		ResultLocale: "zh-CN",
	}, []TestCase{testCase})
	assertContainsAll(t, body,
		"browser-backed cases",
		"log in through that platform URL first",
		"Use Simplified Chinese",
		"case-functional-platform",
	)

	planDetail := testPlanDetailForResponse(TestPlanDetail{
		Cases: []TestPlanCase{{TestCase: testCase}},
	})
	runDetail := testRunDetailForResponse(TestRunDetail{
		Items: []TestRunItem{{TestCase: testCase}},
	})
	assertContainsAll(t, string(planDetail.RequiredCapabilities), `"codex":true`, `"browser":true`, `"chrome_cdp":true`)
	assertContainsAll(t, string(runDetail.RequiredCapabilities), `"codex":true`, `"browser":true`, `"chrome_cdp":true`)
}

func assertContainsAll(t *testing.T, value string, needles ...string) {
	t.Helper()
	for _, needle := range needles {
		if !strings.Contains(value, needle) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", needle, value)
		}
	}
}
