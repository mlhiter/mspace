package control

import (
	"fmt"
	"sort"
	"strings"
)

func defaultWorkspaceGitHubAppRequiredPermissions() map[string]string {
	return map[string]string{
		"contents":      "write",
		"metadata":      "read",
		"pull_requests": "write",
	}
}

func defaultWorkspaceGitHubAppInstallation() WorkspaceGitHubAppInstallation {
	return WorkspaceGitHubAppInstallation{
		Status:              WorkspaceGitHubAppStatusNotConnected,
		Permissions:         map[string]string{},
		RequiredPermissions: defaultWorkspaceGitHubAppRequiredPermissions(),
		MissingPermissions:  []string{},
	}
}

func normalizeStoredWorkspaceGitHubAppInstallation(input WorkspaceGitHubAppInstallation) WorkspaceGitHubAppInstallation {
	normalized := normalizeWorkspaceGitHubAppInstallation(input)
	if normalized.Status == WorkspaceGitHubAppStatusUnavailable {
		normalized.Status = WorkspaceGitHubAppStatusNotConnected
		normalized.Available = false
		normalized.MissingPermissions = []string{}
	}
	return normalized
}

func normalizeWorkspaceGitHubAppInstallation(input WorkspaceGitHubAppInstallation) WorkspaceGitHubAppInstallation {
	normalized := input
	normalized.InstallationID = strings.TrimSpace(normalized.InstallationID)
	normalized.AccountLogin = strings.TrimSpace(normalized.AccountLogin)
	normalized.AccountType = strings.TrimSpace(normalized.AccountType)
	normalized.RepositorySelection = strings.TrimSpace(normalized.RepositorySelection)
	normalized.HTMLURL = strings.TrimSpace(normalized.HTMLURL)
	normalized.RepositoriesURL = strings.TrimSpace(normalized.RepositoriesURL)
	normalized.Error = strings.TrimSpace(normalized.Error)
	normalized.LastSyncedAt = strings.TrimSpace(normalized.LastSyncedAt)
	normalized.CreatedAt = strings.TrimSpace(normalized.CreatedAt)
	normalized.UpdatedAt = strings.TrimSpace(normalized.UpdatedAt)
	normalized.Permissions = copyStringMap(normalized.Permissions)
	normalized.RequiredPermissions = defaultWorkspaceGitHubAppRequiredPermissions()

	switch strings.TrimSpace(normalized.Status) {
	case WorkspaceGitHubAppStatusUnavailable,
		WorkspaceGitHubAppStatusNotConnected,
		WorkspaceGitHubAppStatusConnected,
		WorkspaceGitHubAppStatusNeedsAction:
		normalized.Status = strings.TrimSpace(normalized.Status)
	default:
		switch {
		case normalized.InstallationID == "":
			normalized.Status = WorkspaceGitHubAppStatusNotConnected
		case normalized.Error != "":
			normalized.Status = WorkspaceGitHubAppStatusNeedsAction
		default:
			normalized.Status = WorkspaceGitHubAppStatusConnected
		}
	}

	if normalized.Status == WorkspaceGitHubAppStatusConnected || normalized.Status == WorkspaceGitHubAppStatusNeedsAction {
		normalized.MissingPermissions = workspaceGitHubAppMissingPermissions(normalized.Permissions, normalized.RequiredPermissions)
		if len(normalized.MissingPermissions) > 0 {
			normalized.Status = WorkspaceGitHubAppStatusNeedsAction
			if normalized.Error == "" {
				normalized.Error = "GitHub App installation is missing required permissions."
			}
		}
	} else {
		normalized.MissingPermissions = []string{}
	}

	if normalized.Permissions == nil {
		normalized.Permissions = map[string]string{}
	}
	return normalized
}

func workspaceGitHubAppInstallationForServer(state WorkspaceGitHubAppInstallation, available bool) WorkspaceGitHubAppInstallation {
	state = normalizeWorkspaceGitHubAppInstallation(state)
	state.Available = available
	if !available {
		state.Status = WorkspaceGitHubAppStatusUnavailable
		state.MissingPermissions = []string{}
		state.Error = "GitHub App automation is not configured on this server."
		return state
	}
	if state.Status == WorkspaceGitHubAppStatusUnavailable {
		state.Status = WorkspaceGitHubAppStatusNotConnected
		state.Error = ""
	}
	return normalizeWorkspaceGitHubAppInstallation(state)
}

func workspaceGitHubAppMissingPermissions(permissions, required map[string]string) []string {
	missing := []string{}
	for permission, minimum := range required {
		actual := strings.ToLower(strings.TrimSpace(permissions[permission]))
		if !githubPermissionLevelSatisfies(actual, minimum) {
			missing = append(missing, fmt.Sprintf("%s:%s", permission, minimum))
		}
	}
	sort.Strings(missing)
	return missing
}

func githubPermissionLevelSatisfies(actual, minimum string) bool {
	switch strings.ToLower(strings.TrimSpace(minimum)) {
	case "read":
		return actual == "read" || actual == "write"
	case "write":
		return actual == "write"
	default:
		return actual == strings.ToLower(strings.TrimSpace(minimum))
	}
}

func copyStringMap(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for key, value := range input {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		output[key] = strings.TrimSpace(value)
	}
	return output
}

func (s *Server) githubAppConfigured() bool {
	return strings.TrimSpace(s.config.GitHubAppID) != "" &&
		strings.TrimSpace(s.config.GitHubAppClientID) != "" &&
		strings.TrimSpace(s.config.GitHubAppPrivateKey) != ""
}
