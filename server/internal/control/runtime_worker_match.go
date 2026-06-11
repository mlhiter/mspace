package control

import (
	"encoding/json"
	"strings"
	"time"
)

const activeWorkerMaxAge = 45 * time.Second
const staleRunningTaskReclaimAge = 30 * time.Minute

func isActiveCodexWorker(worker RuntimeWorker, workspaceID, runtimeMode string, now time.Time) bool {
	return isActiveWorkerWithCapabilities(worker, workspaceID, runtimeMode, json.RawMessage(`{"codex":true}`), now)
}

func isActiveWorkerWithCapabilities(worker RuntimeWorker, workspaceID, runtimeMode string, requiredCapabilities json.RawMessage, now time.Time) bool {
	if worker.WorkspaceID != strings.TrimSpace(workspaceID) {
		return false
	}
	if worker.Mode != strings.TrimSpace(runtimeMode) || worker.Status != "online" {
		return false
	}
	if len(requiredCapabilities) == 0 {
		requiredCapabilities = json.RawMessage(`{}`)
	}
	if !jsonObjectContains(worker.Capabilities, requiredCapabilities) {
		return false
	}
	lastSeenAt, err := time.Parse(time.RFC3339Nano, worker.LastSeenAt)
	if err != nil {
		lastSeenAt, err = time.Parse(time.RFC3339, worker.LastSeenAt)
	}
	if err != nil {
		return false
	}
	return !lastSeenAt.Before(now.Add(-activeWorkerMaxAge))
}
