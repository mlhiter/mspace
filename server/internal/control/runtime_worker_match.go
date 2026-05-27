package control

import (
	"strings"
	"time"
)

const activeWorkerMaxAge = 45 * time.Second

func isActiveCodexWorker(worker RuntimeWorker, workspaceID, runtimeMode string, now time.Time) bool {
	if worker.WorkspaceID != strings.TrimSpace(workspaceID) {
		return false
	}
	if worker.Mode != strings.TrimSpace(runtimeMode) || worker.Status != "online" {
		return false
	}
	if !jsonObjectContains(worker.Capabilities, []byte(`{"codex":true}`)) {
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
