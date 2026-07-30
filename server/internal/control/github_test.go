package control

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGitHubHTTPClientFetchPullRequestUsesCurrentUserCLIWithoutAnonymousREST(t *testing.T) {
	cliPath := filepath.Join(t.TempDir(), "gh")
	script := `#!/bin/sh
if [ "$1" != "api" ] || [ "$2" != "repos/mlhiter/private-repo/pulls/7" ]; then
  echo "unexpected args: $*" >&2
  exit 2
fi
cat <<'JSON'
{
  "html_url": "https://github.com/mlhiter/private-repo/pull/7",
  "number": 7,
  "state": "closed",
  "title": "fix private repo",
  "draft": false,
  "merged": true,
  "merged_at": "2026-07-30T06:00:00Z",
  "head": {
    "sha": "3333333333333333333333333333333333333333"
  }
}
JSON
`
	if err := os.WriteFile(cliPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	client := GitHubHTTPClient{
		CLIPath: cliPath,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return nil, errors.New("anonymous github rest should not be called")
		})},
	}

	pr, err := client.FetchPullRequest(context.Background(), gitPullRequestRef{
		owner:  "mlhiter",
		repo:   "private-repo",
		number: 7,
	}, "")
	if err != nil {
		t.Fatalf("fetch pull request with gh: %v", err)
	}
	if pr.Number != 7 || !pr.Merged || pr.MergedAt == "" || pr.HeadCommitSHA != "3333333333333333333333333333333333333333" {
		t.Fatalf("unexpected pull request from gh: %+v", pr)
	}
}

func TestGitHubHTTPClientFetchPullRequestUsesTokenREST(t *testing.T) {
	client := GitHubHTTPClient{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if got := req.Header.Get("Authorization"); got != "Bearer gho_test" {
				t.Fatalf("Authorization header = %q, want Bearer gho_test", got)
			}
			if got := req.URL.String(); got != "https://api.github.com/repos/mlhiter/private-repo/pulls/7" {
				t.Fatalf("request URL = %q", got)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewBufferString(`{
  "html_url": "https://github.com/mlhiter/private-repo/pull/7",
  "number": 7,
  "state": "open",
  "title": "fix private repo",
  "draft": true,
  "merged": false,
  "merged_at": null,
  "head": {
    "sha": "4444444444444444444444444444444444444444"
  }
}`)),
				Header: make(http.Header),
			}, nil
		})},
	}

	pr, err := client.FetchPullRequest(context.Background(), gitPullRequestRef{
		owner:  "mlhiter",
		repo:   "private-repo",
		number: 7,
	}, "gho_test")
	if err != nil {
		t.Fatalf("fetch pull request with token: %v", err)
	}
	if pr.Number != 7 || !pr.Draft || pr.Merged || pr.HeadCommitSHA != "4444444444444444444444444444444444444444" {
		t.Fatalf("unexpected pull request from token REST: %+v", pr)
	}
}
