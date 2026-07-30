package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type GitHubClient interface {
	ExchangeCode(ctx context.Context, code, redirectURI string) (string, error)
	FetchUser(ctx context.Context, accessToken string) (IdentityProfile, error)
	FetchPullRequest(ctx context.Context, ref gitPullRequestRef, accessToken string) (GitHubPullRequest, error)
}

type GitHubHTTPClient struct {
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client
	CLIPath      string
	DisableCLI   bool
}

type GitHubPullRequest struct {
	URL           string
	Number        int
	State         string
	Title         string
	Draft         bool
	Merged        bool
	MergedAt      string
	HeadCommitSHA string
}

type GitHubAPIError struct {
	Operation  string
	StatusCode int
	Message    string
}

type GitHubCLIError struct {
	Operation string
	Message   string
	Err       error
}

func (e GitHubAPIError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = http.StatusText(e.StatusCode)
	}
	if message == "" {
		message = "request failed"
	}
	operation := strings.TrimSpace(e.Operation)
	if operation == "" {
		return fmt.Sprintf("github api status %d: %s", e.StatusCode, message)
	}
	return fmt.Sprintf("%s: github api status %d: %s", operation, e.StatusCode, message)
}

func (e GitHubCLIError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "request failed"
	}
	operation := strings.TrimSpace(e.Operation)
	if operation == "" {
		return "github cli: " + message
	}
	return operation + ": " + message
}

func (c GitHubHTTPClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c GitHubHTTPClient) ExchangeCode(ctx context.Context, code, redirectURI string) (string, error) {
	if strings.TrimSpace(c.ClientID) == "" || strings.TrimSpace(c.ClientSecret) == "" {
		return "", errors.New("github oauth is not configured")
	}

	body := url.Values{
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"code":          {code},
	}
	if redirectURI != "" {
		body.Set("redirect_uri", redirectURI)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", bytes.NewBufferString(body.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("exchange github code: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("exchange github code: status %d", resp.StatusCode)
	}

	var parsed struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return "", fmt.Errorf("parse github token response: %w", err)
	}
	if parsed.Error != "" {
		if parsed.Description != "" {
			return "", errors.New(parsed.Description)
		}
		return "", errors.New(parsed.Error)
	}
	if parsed.AccessToken == "" {
		return "", errors.New("github token response did not include an access token")
	}
	return parsed.AccessToken, nil
}

func (c GitHubHTTPClient) FetchUser(ctx context.Context, accessToken string) (IdentityProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return IdentityProfile{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return IdentityProfile{}, fmt.Errorf("fetch github user: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return IdentityProfile{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return IdentityProfile{}, fmt.Errorf("fetch github user: status %d", resp.StatusCode)
	}

	var user struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(payload, &user); err != nil {
		return IdentityProfile{}, fmt.Errorf("parse github user: %w", err)
	}
	if user.ID == 0 {
		return IdentityProfile{}, errors.New("github user response did not include id")
	}
	name := strings.TrimSpace(user.Name)
	if name == "" {
		name = user.Login
	}

	return IdentityProfile{
		Provider:       "github",
		ProviderUserID: strconv.FormatInt(user.ID, 10),
		Login:          user.Login,
		Name:           name,
		Email:          strings.ToLower(strings.TrimSpace(user.Email)),
		AvatarURL:      user.AvatarURL,
		RawProfile:     json.RawMessage(payload),
	}, nil
}

func (c GitHubHTTPClient) FetchPullRequest(ctx context.Context, ref gitPullRequestRef, accessToken string) (GitHubPullRequest, error) {
	owner := strings.TrimSpace(ref.owner)
	repo := strings.TrimSpace(ref.repo)
	if owner == "" || repo == "" || ref.number <= 0 {
		return GitHubPullRequest{}, errors.New("github pull request reference is incomplete")
	}
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repo), ref.number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return GitHubPullRequest{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token := strings.TrimSpace(accessToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return GitHubPullRequest{}, fmt.Errorf("fetch github pull request: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return GitHubPullRequest{}, err
	}
	if resp.StatusCode != http.StatusOK {
		var parsed struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(payload, &parsed)
		apiErr := GitHubAPIError{
			Operation:  "fetch github pull request",
			StatusCode: resp.StatusCode,
			Message:    parsed.Message,
		}
		if strings.TrimSpace(accessToken) == "" {
			if pr, err := c.fetchPullRequestWithCLI(ctx, ref); err == nil {
				return pr, nil
			} else if !isGitHubCLINotFound(err) {
				return GitHubPullRequest{}, err
			}
		}
		return GitHubPullRequest{}, apiErr
	}

	return parseGitHubPullRequest(payload)
}

func parseGitHubPullRequest(payload []byte) (GitHubPullRequest, error) {
	var parsed struct {
		HTMLURL  string  `json:"html_url"`
		Number   int     `json:"number"`
		State    string  `json:"state"`
		Title    string  `json:"title"`
		Draft    bool    `json:"draft"`
		Merged   bool    `json:"merged"`
		MergedAt *string `json:"merged_at"`
		Head     struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return GitHubPullRequest{}, fmt.Errorf("parse github pull request response: %w", err)
	}
	mergedAt := ""
	if parsed.MergedAt != nil {
		mergedAt = strings.TrimSpace(*parsed.MergedAt)
	}
	return GitHubPullRequest{
		URL:           strings.TrimSpace(parsed.HTMLURL),
		Number:        parsed.Number,
		State:         strings.TrimSpace(parsed.State),
		Title:         strings.TrimSpace(parsed.Title),
		Draft:         parsed.Draft,
		Merged:        parsed.Merged,
		MergedAt:      mergedAt,
		HeadCommitSHA: strings.TrimSpace(parsed.Head.SHA),
	}, nil
}

func (c GitHubHTTPClient) fetchPullRequestWithCLI(ctx context.Context, ref gitPullRequestRef) (GitHubPullRequest, error) {
	if c.DisableCLI {
		return GitHubPullRequest{}, GitHubCLIError{Operation: "fetch github pull request with gh", Message: "disabled"}
	}
	endpoint := fmt.Sprintf("repos/%s/%s/pulls/%d", ref.owner, ref.repo, ref.number)
	var lastErr error
	for _, cli := range githubCLIPathCandidates(c.CLIPath) {
		cmd := exec.CommandContext(ctx, cli, "api", endpoint)
		output, err := cmd.Output()
		if err == nil {
			return parseGitHubPullRequest(output)
		}
		lastErr = err
		if !errors.Is(err, exec.ErrNotFound) {
			break
		}
	}
	return GitHubPullRequest{}, GitHubCLIError{
		Operation: "fetch github pull request with gh",
		Message:   githubCLIErrorMessage(lastErr),
		Err:       lastErr,
	}
}

func githubCLIPathCandidates(configured string) []string {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		return []string{configured}
	}
	candidates := []string{"gh"}
	if path, err := exec.LookPath("gh"); err == nil && strings.TrimSpace(path) != "" && path != "gh" {
		candidates = append([]string{path}, candidates...)
	}
	for _, path := range []string{"/opt/homebrew/bin/gh", "/usr/local/bin/gh"} {
		if _, err := os.Stat(path); err == nil {
			candidates = append(candidates, path)
		}
	}
	seen := map[string]bool{}
	unique := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate = strings.TrimSpace(candidate); candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		unique = append(unique, candidate)
	}
	return unique
}

func isGitHubCLINotFound(err error) bool {
	var cliErr GitHubCLIError
	if errors.As(err, &cliErr) {
		return errors.Is(cliErr.Err, exec.ErrNotFound)
	}
	return false
}

func githubCLIErrorMessage(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		message := strings.TrimSpace(string(exitErr.Stderr))
		if message != "" {
			return truncateGitHubCLIError(message)
		}
		return fmt.Sprintf("gh exited with status %d", exitErr.ExitCode())
	}
	if errors.Is(err, exec.ErrNotFound) {
		return "gh executable was not found"
	}
	return truncateGitHubCLIError(err.Error())
}

func truncateGitHubCLIError(message string) string {
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > 240 {
		return message[:240] + "..."
	}
	return message
}
