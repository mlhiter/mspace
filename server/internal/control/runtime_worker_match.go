package control

import (
	"encoding/json"
	"sort"
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

func evaluateRuntimeAvailability(workspaceID, workspaceKind, runtimeMode string, requiredCapabilities json.RawMessage, workers []RuntimeWorker, now time.Time) RuntimeAvailability {
	workspaceID = strings.TrimSpace(workspaceID)
	workspaceKind = strings.TrimSpace(workspaceKind)
	runtimeMode = strings.TrimSpace(runtimeMode)
	if runtimeMode == "" {
		runtimeMode = workspaceKind
	}
	if len(requiredCapabilities) == 0 {
		requiredCapabilities = json.RawMessage(`{}`)
	}
	normalizedCapabilities, err := normalizeJSONObjectPayload(requiredCapabilities)
	if err != nil {
		normalizedCapabilities = json.RawMessage(`{}`)
	}
	result := RuntimeAvailability{
		WorkspaceID:          workspaceID,
		RuntimeMode:          runtimeMode,
		RequiredCapabilities: copyRawMessage(normalizedCapabilities),
		State:                "unavailable",
		ReasonCode:           "no_worker",
		CanQueue:             false,
		RetryAfterMs:         int((5 * time.Second).Milliseconds()),
		ActiveWorkerMaxAgeMs: int64(activeWorkerMaxAge / time.Millisecond),
	}
	if workspaceKind != "" && runtimeMode != workspaceKind {
		result.ReasonCode = "wrong_runtime_mode"
		return result
	}

	modeWorkers := make([]RuntimeWorker, 0, len(workers))
	for _, worker := range workers {
		if worker.WorkspaceID == workspaceID && worker.Mode == runtimeMode {
			modeWorkers = append(modeWorkers, worker)
		}
	}
	if len(modeWorkers) == 0 {
		result.CanAutoStart = runtimeMode == "personal"
		return result
	}
	sort.SliceStable(modeWorkers, func(i, j int) bool {
		return modeWorkers[i].LastSeenAt > modeWorkers[j].LastSeenAt
	})

	var capabilityWorker *RuntimeWorker
	var nonOnlineWorker *RuntimeWorker
	var staleWorker *RuntimeWorker
	for _, worker := range modeWorkers {
		if !jsonObjectContains(worker.Capabilities, normalizedCapabilities) {
			continue
		}
		current := worker
		if capabilityWorker == nil {
			capabilityWorker = &current
		}
		if worker.Status != "online" {
			if nonOnlineWorker == nil {
				nonOnlineWorker = &current
			}
			continue
		}
		lastSeenAt, ok := parseRuntimeWorkerLastSeen(worker.LastSeenAt)
		if !ok || lastSeenAt.Before(now.Add(-activeWorkerMaxAge)) {
			if staleWorker == nil {
				staleWorker = &current
			}
			continue
		}
		result.State = "ready"
		result.ReasonCode = "ready"
		result.CanQueue = true
		result.CanAutoStart = false
		result.RetryAfterMs = 0
		result.MatchedWorker = &current
		result.LastSeenAt = current.LastSeenAt
		return result
	}

	if capabilityWorker == nil {
		selected := modeWorkers[0]
		result.MatchedWorker = &selected
		result.LastSeenAt = selected.LastSeenAt
		result.ReasonCode = "missing_capability"
		result.MissingCapabilities = missingRuntimeCapabilities(selected.Capabilities, normalizedCapabilities)
		result.CanAutoStart = runtimeMode == "personal"
		return result
	}
	if staleWorker != nil {
		result.MatchedWorker = staleWorker
		result.LastSeenAt = staleWorker.LastSeenAt
		result.ReasonCode = "stale_heartbeat"
		result.CanAutoStart = runtimeMode == "personal"
		return result
	}
	if nonOnlineWorker != nil {
		result.MatchedWorker = nonOnlineWorker
		result.LastSeenAt = nonOnlineWorker.LastSeenAt
		if nonOnlineWorker.Status == "draining" {
			result.ReasonCode = "worker_draining"
		} else {
			result.ReasonCode = "worker_offline"
		}
		result.CanAutoStart = runtimeMode == "personal"
		return result
	}
	return result
}

func parseRuntimeWorkerLastSeen(value string) (time.Time, bool) {
	lastSeenAt, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		lastSeenAt, err = time.Parse(time.RFC3339, value)
	}
	return lastSeenAt, err == nil
}

func missingRuntimeCapabilities(workerCapabilities, requiredCapabilities json.RawMessage) []string {
	var requiredMap map[string]any
	if err := json.Unmarshal(requiredCapabilities, &requiredMap); err != nil {
		return nil
	}
	missing := make([]string, 0)
	for key, requiredValue := range requiredMap {
		if requiredValue == false {
			continue
		}
		if !jsonObjectContains(workerCapabilities, mustMarshalJSONObject(map[string]any{key: requiredValue})) {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}

func mustMarshalJSONObject(value map[string]any) json.RawMessage {
	payload, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(payload)
}
