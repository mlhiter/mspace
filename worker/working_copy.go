package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	agentSessionExecutionDetached         = "detached"
	agentSessionExecutionIssueWorkingCopy = "issue_working_copy"
	issueWorkingCopyCapability            = "issueWorkingCopyV1"
	workerStorageIDPrefix                 = "msws_"
)

type issueWorkingCopyResult struct {
	StorageID      string `json:"storageId"`
	Branch         string `json:"branch"`
	BaseCommitSHA  string `json:"baseCommitSha"`
	HeadCommitSHA  string `json:"headCommitSha"`
	ContentState   string `json:"contentState"`
	RecoveryReason string `json:"recoveryReason,omitempty"`
	Generation     int64  `json:"generation"`
}

type issueWorkingCopyMetadata struct {
	StorageID     string `json:"storageId"`
	Branch        string `json:"branch"`
	BaseCommitSHA string `json:"baseCommitSha"`
}

type issueWorkingCopyError struct {
	reason string
	err    error
}

func (err *issueWorkingCopyError) Error() string { return err.err.Error() }
func (err *issueWorkingCopyError) Unwrap() error { return err.err }

func newIssueWorkingCopyError(reason, format string, args ...any) error {
	return &issueWorkingCopyError{reason: reason, err: fmt.Errorf(format, args...)}
}

func issueWorkingCopyRecoveryReason(err error) string {
	var workingCopyErr *issueWorkingCopyError
	if errors.As(err, &workingCopyErr) {
		return workingCopyErr.reason
	}
	return ""
}

func ensureWorkerStorageID(workRoot string) (string, error) {
	stateDir := filepath.Join(workRoot, ".mspace")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", fmt.Errorf("create worker state directory: %w", err)
	}
	path := filepath.Join(stateDir, "storage-id")
	if value, err := readWorkerStorageID(path); err == nil {
		return value, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate worker storage id: %w", err)
	}
	storageID := workerStorageIDPrefix + base64.RawURLEncoding.EncodeToString(buffer)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return readWorkerStorageID(path)
	}
	if err != nil {
		return "", fmt.Errorf("create worker storage id: %w", err)
	}
	written := false
	defer func() {
		_ = file.Close()
		if !written {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.WriteString(storageID + "\n"); err != nil {
		return "", fmt.Errorf("write worker storage id: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("sync worker storage id: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close worker storage id: %w", err)
	}
	written = true
	return storageID, nil
}

func readWorkerStorageID(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("worker storage id path must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read worker storage id: %w", err)
	}
	storageID := strings.TrimSpace(string(data))
	if !validWorkerStorageID(storageID) {
		return "", errors.New("worker storage id is malformed")
	}
	return storageID, nil
}

func validWorkerStorageID(value string) bool {
	if !strings.HasPrefix(value, workerStorageIDPrefix) {
		return false
	}
	suffix := strings.TrimPrefix(value, workerStorageIDPrefix)
	if len(suffix) < 24 || len(suffix) > 128 {
		return false
	}
	for _, char := range suffix {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

func requireIssueWorkingCopyCapability(raw json.RawMessage) (json.RawMessage, error) {
	values := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, fmt.Errorf("decode worker capabilities: %w", err)
		}
	}
	values[issueWorkingCopyCapability] = true
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode worker capabilities: %w", err)
	}
	return encoded, nil
}

func (payload agentSessionPayload) usesIssueWorkingCopy() bool {
	return payload.ExecutionMode == agentSessionExecutionIssueWorkingCopy
}

func assignIssueWorkingCopyPaths(cfg config, payload agentSessionPayload) agentSessionPayload {
	if !payload.usesIssueWorkingCopy() {
		return payload
	}
	projectPart := safePathPart(payload.ProjectID)
	issuePart := safePathPart(payload.IssueID)
	sessionPart := safePathPart(payload.SessionID)
	worktreeDir := filepath.Join(cfg.WorkRoot, "workdirs", projectPart, issuePart)
	payload.WorkingCopyRoot = worktreeDir
	payload.Workdir = worktreeDir
	if subdir := normalizeRepositorySubdir(payload.Repository.Subdir); subdir != "" {
		payload.Workdir = filepath.Join(worktreeDir, filepath.FromSlash(subdir))
	}
	payload.ArtifactDir = filepath.Join(cfg.WorkRoot, "artifacts", projectPart, issuePart, sessionPart)
	payload.WorkerStorageID = cfg.StorageID
	payload.WorkerArtifactRoot = filepath.Join(cfg.WorkRoot, "artifacts")
	return payload
}

func ensurePayloadArtifactPath(payload agentSessionPayload, target string) error {
	if !payload.usesIssueWorkingCopy() {
		return ensureSessionArtifactPath(payload.Workdir, target)
	}
	trustedRoot := strings.TrimSpace(payload.WorkerArtifactRoot)
	if trustedRoot == "" {
		return errors.New("issue working-copy artifact root is not trusted")
	}
	expectedArtifactDir := filepath.Join(
		trustedRoot,
		safePathPart(payload.ProjectID),
		safePathPart(payload.IssueID),
		safePathPart(payload.SessionID),
	)
	if filepath.Clean(payload.ArtifactDir) != filepath.Clean(expectedArtifactDir) {
		return errors.New("issue working-copy artifactDir does not match the assigned session path")
	}
	if err := ensurePathWithin(payload.ArtifactDir, target); err != nil {
		return fmt.Errorf("session path %q is outside artifactDir: %w", target, err)
	}
	if err := ensurePathWithin(trustedRoot, target); err != nil {
		return fmt.Errorf("session path %q is outside worker artifact root: %w", target, err)
	}
	realWorkRoot, err := filepath.EvalSymlinks(filepath.Dir(trustedRoot))
	if err != nil {
		return fmt.Errorf("resolve worker work root: %w", err)
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return fmt.Errorf("resolve session path %q: %w", target, err)
	}
	realTrustedRoot := filepath.Join(realWorkRoot, filepath.Base(trustedRoot))
	realExpectedArtifactDir := filepath.Join(
		realTrustedRoot,
		safePathPart(payload.ProjectID),
		safePathPart(payload.IssueID),
		safePathPart(payload.SessionID),
	)
	realArtifactDir, err := filepath.EvalSymlinks(payload.ArtifactDir)
	if err != nil {
		return fmt.Errorf("resolve issue working-copy artifactDir: %w", err)
	}
	if filepath.Clean(realArtifactDir) != filepath.Clean(realExpectedArtifactDir) {
		return errors.New("issue working-copy artifactDir resolves through a symbolic link")
	}
	if err := ensurePathWithin(realExpectedArtifactDir, realTarget); err != nil {
		return fmt.Errorf("session path %q escapes its assigned artifact directory through a symbolic link: %w", target, err)
	}
	return nil
}

func prepareIssueWorkingCopy(ctx context.Context, gitPath, repoDir string, cfg config, payload agentSessionPayload) (agentSessionPayload, error) {
	payload = assignIssueWorkingCopyPaths(cfg, payload)

	if err := os.MkdirAll(filepath.Dir(payload.WorkingCopyRoot), 0o755); err != nil {
		return payload, fmt.Errorf("create issue working-copy parent: %w", err)
	}
	info, statErr := os.Stat(payload.WorkingCopyRoot)
	if errors.Is(statErr, os.ErrNotExist) {
		if !payload.Initialize {
			return payload, newIssueWorkingCopyError("worktree_missing", "issue working copy is missing")
		}
		return initializeIssueWorkingCopy(ctx, gitPath, repoDir, cfg, payload)
	}
	if statErr != nil {
		return payload, newIssueWorkingCopyError("workspace_probe_failed", "inspect issue working copy: %v", statErr)
	}
	if !info.IsDir() {
		return payload, newIssueWorkingCopyError("workspace_probe_failed", "issue working-copy path is not a directory")
	}
	return reuseIssueWorkingCopy(ctx, gitPath, cfg, payload)
}

func initializeIssueWorkingCopy(ctx context.Context, gitPath, repoDir string, cfg config, payload agentSessionPayload) (agentSessionPayload, error) {
	if err := validateWorkerBranchName(ctx, gitPath, repoDir, payload.Branch); err != nil {
		return payload, newIssueWorkingCopyError("branch_mismatch", "invalid issue working-copy branch: %v", err)
	}
	baseRef := payload.ExpectedHeadSHA
	if baseRef == "" {
		resolved, err := resolveWorkerBaseRef(ctx, gitPath, repoDir, payload.Repository.DefaultBranch)
		if err != nil {
			return payload, newIssueWorkingCopyError("workspace_probe_failed", "%v", err)
		}
		baseRef = resolved
	}
	baseCommit, err := runGitOutput(ctx, gitPath, repoDir, "rev-parse", "--verify", baseRef+"^{commit}")
	if err != nil {
		return payload, newIssueWorkingCopyError("head_mismatch", "resolve issue working-copy base commit: %v", err)
	}
	baseCommit = strings.TrimSpace(baseCommit)
	if err := runGitCommand(ctx, gitPath, repoDir, "worktree", "add", "-b", payload.Branch, payload.WorkingCopyRoot, baseCommit); err != nil {
		return payload, newIssueWorkingCopyError("workspace_probe_failed", "create issue working copy: %v", err)
	}
	payload.WorkingCopyBaseCommitSHA = baseCommit
	metadata := issueWorkingCopyMetadata{StorageID: cfg.StorageID, Branch: payload.Branch, BaseCommitSHA: baseCommit}
	if err := writeIssueWorkingCopyMetadata(cfg.WorkRoot, payload, metadata); err != nil {
		_ = runGitCommand(context.Background(), gitPath, repoDir, "worktree", "remove", "--force", payload.WorkingCopyRoot)
		return payload, newIssueWorkingCopyError("metadata_missing", "%v", err)
	}
	return payload, nil
}

func reuseIssueWorkingCopy(ctx context.Context, gitPath string, cfg config, payload agentSessionPayload) (agentSessionPayload, error) {
	if err := runGitCommand(ctx, gitPath, payload.WorkingCopyRoot, "rev-parse", "--is-inside-work-tree"); err != nil {
		return payload, newIssueWorkingCopyError("workspace_probe_failed", "issue working-copy directory is not a git worktree: %v", err)
	}
	branch, err := runGitOutput(ctx, gitPath, payload.WorkingCopyRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || strings.TrimSpace(branch) != payload.Branch {
		return payload, newIssueWorkingCopyError("branch_mismatch", "issue working-copy branch does not match the assigned branch")
	}
	head, err := runGitOutput(ctx, gitPath, payload.WorkingCopyRoot, "rev-parse", "HEAD")
	if err != nil {
		return payload, newIssueWorkingCopyError("workspace_probe_failed", "read issue working-copy HEAD: %v", err)
	}
	head = strings.TrimSpace(head)
	metadata, err := readIssueWorkingCopyMetadata(cfg.WorkRoot, payload)
	if err != nil {
		return payload, newIssueWorkingCopyError("metadata_missing", "%v", err)
	}
	if metadata.StorageID != cfg.StorageID {
		return payload, newIssueWorkingCopyError("metadata_missing", "issue working-copy metadata belongs to another storage")
	}
	if metadata.Branch != payload.Branch {
		return payload, newIssueWorkingCopyError("branch_mismatch", "issue working-copy metadata branch does not match the assigned branch")
	}
	if strings.TrimSpace(metadata.BaseCommitSHA) == "" {
		return payload, newIssueWorkingCopyError("metadata_missing", "issue working-copy metadata has no base commit")
	}
	payload.WorkingCopyBaseCommitSHA = metadata.BaseCommitSHA
	expectedHead := payload.ExpectedHeadSHA
	if expectedHead == "" && payload.Initialize {
		expectedHead = metadata.BaseCommitSHA
	}
	if expectedHead == "" || !strings.EqualFold(head, expectedHead) {
		return payload, newIssueWorkingCopyError("head_mismatch", "issue working-copy HEAD does not match the expected commit")
	}
	return payload, nil
}

func issueWorkingCopyMetadataPath(workRoot string, payload agentSessionPayload) string {
	return filepath.Join(workRoot, ".mspace", "working-copies", safePathPart(payload.ProjectID), safePathPart(payload.IssueID)+".json")
}

func writeIssueWorkingCopyMetadata(workRoot string, payload agentSessionPayload, metadata issueWorkingCopyMetadata) error {
	path := issueWorkingCopyMetadataPath(workRoot, payload)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create issue working-copy metadata directory: %w", err)
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode issue working-copy metadata: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".working-copy-*.tmp")
	if err != nil {
		return fmt.Errorf("create issue working-copy metadata: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure issue working-copy metadata: %w", err)
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write issue working-copy metadata: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync issue working-copy metadata: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close issue working-copy metadata: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish issue working-copy metadata: %w", err)
	}
	return nil
}

func readIssueWorkingCopyMetadata(workRoot string, payload agentSessionPayload) (issueWorkingCopyMetadata, error) {
	path := issueWorkingCopyMetadataPath(workRoot, payload)
	data, err := os.ReadFile(path)
	if err != nil {
		return issueWorkingCopyMetadata{}, fmt.Errorf("read issue working-copy metadata: %w", err)
	}
	var metadata issueWorkingCopyMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return issueWorkingCopyMetadata{}, fmt.Errorf("decode issue working-copy metadata: %w", err)
	}
	return metadata, nil
}

func inspectIssueWorkingCopy(ctx context.Context, payload agentSessionPayload, forcedReason string) *issueWorkingCopyResult {
	if !payload.usesIssueWorkingCopy() {
		return nil
	}
	result := &issueWorkingCopyResult{
		StorageID:      payload.WorkerStorageID,
		Branch:         payload.Branch,
		BaseCommitSHA:  payload.WorkingCopyBaseCommitSHA,
		ContentState:   "recovery_required",
		RecoveryReason: forcedReason,
		Generation:     payload.WorkingCopyGeneration,
	}
	if result.RecoveryReason == "" && payload.WorkingCopyRoot == "" {
		result.RecoveryReason = "worktree_missing"
		return result
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		if result.RecoveryReason == "" {
			result.RecoveryReason = "workspace_probe_failed"
		}
		return result
	}
	info, err := os.Stat(payload.WorkingCopyRoot)
	if err != nil || !info.IsDir() {
		if result.RecoveryReason == "" {
			if errors.Is(err, os.ErrNotExist) {
				result.RecoveryReason = "worktree_missing"
			} else {
				result.RecoveryReason = "workspace_probe_failed"
			}
		}
		return result
	}
	head, err := runGitOutput(ctx, gitPath, payload.WorkingCopyRoot, "rev-parse", "HEAD")
	if err != nil {
		if result.RecoveryReason == "" {
			result.RecoveryReason = "workspace_probe_failed"
		}
		return result
	}
	result.HeadCommitSHA = strings.TrimSpace(head)
	branch, err := runGitOutput(ctx, gitPath, payload.WorkingCopyRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || strings.TrimSpace(branch) != payload.Branch {
		if result.RecoveryReason == "" {
			result.RecoveryReason = "branch_mismatch"
		}
		return result
	}
	expectedAncestor := strings.TrimSpace(payload.ExpectedHeadSHA)
	if expectedAncestor == "" {
		expectedAncestor = strings.TrimSpace(payload.WorkingCopyBaseCommitSHA)
	}
	if expectedAncestor != "" && !strings.EqualFold(result.HeadCommitSHA, expectedAncestor) {
		if err := runGitCommand(ctx, gitPath, payload.WorkingCopyRoot, "merge-base", "--is-ancestor", expectedAncestor, result.HeadCommitSHA); err != nil {
			if result.RecoveryReason == "" {
				result.RecoveryReason = "head_mismatch"
			}
			return result
		}
	}
	if result.RecoveryReason != "" {
		return result
	}
	status, err := runGitOutput(ctx, gitPath, payload.WorkingCopyRoot, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		result.RecoveryReason = "workspace_probe_failed"
		return result
	}
	result.ContentState = "clean"
	if strings.TrimSpace(status) != "" {
		result.ContentState = "dirty"
	}
	return result
}
