package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func validatePublicRuntimeTaskInput(input CreateRuntimeTaskInput) error {
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	switch kind {
	case "noop", "protocol_smoke":
	default:
		return fmt.Errorf("runtime task kind %s is server-managed; raw tasks support only noop or protocol_smoke", kind)
	}
	if strings.TrimSpace(input.IssueID) != "" || strings.TrimSpace(input.SessionID) != "" || strings.TrimSpace(input.ProjectID) != "" {
		return errors.New("raw runtime tasks cannot bind issueId, sessionId, or projectId")
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(input.Payload, &payload); err != nil {
		return fmt.Errorf("payload must be a JSON object: %w", err)
	}
	serverManagedFields := map[string]string{
		"agentengine":           "agentEngine",
		"agentprofile":          "agentProfile",
		"provider":              "provider",
		"automation":            "automation",
		"testrunid":             "testRunId",
		"testrunbatchsize":      "testRunBatchSize",
		"sourcesessionid":       "sourceSessionId",
		"sourcecommitsha":       "sourceCommitSha",
		"triggercommentid":      "triggerCommentId",
		"requiredskills":        "requiredSkills",
		"skills":                "skills",
		"skillbundles":          "skillBundles",
		"env":                   "env",
		"workdir":               "workdir",
		"issueid":               "issueId",
		"sessionid":             "sessionId",
		"projectid":             "projectId",
		"artifactdir":           "artifactDir",
		"repository":            "repository",
		"sourcecapture":         "sourceCapture",
		"developerinstructions": "developerInstructions",
		"approvalpolicy":        "approvalPolicy",
		"sandbox":               "sandbox",
		"contextmarkdown":       "contextMarkdown",
		"branch":                "branch",
	}
	for key := range payload {
		if canonical, ok := serverManagedFields[strings.ToLower(key)]; ok {
			return fmt.Errorf("raw runtime task payload field %s is server-managed", canonical)
		}
	}
	return nil
}
