package control

import (
	"bytes"
	"context"
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

func TestGitHubHTTPClientFetchPullRequestFallsBackToCurrentUserCLI(t *testing.T) {
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
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(bytes.NewBufferString(`{"message":"Not Found"}`)),
				Header:     make(http.Header),
			}, nil
		})},
	}

	pr, err := client.FetchPullRequest(context.Background(), gitPullRequestRef{
		owner:  "mlhiter",
		repo:   "private-repo",
		number: 7,
	}, "")
	if err != nil {
		t.Fatalf("fetch pull request with gh fallback: %v", err)
	}
	if pr.Number != 7 || !pr.Merged || pr.MergedAt == "" || pr.HeadCommitSHA != "3333333333333333333333333333333333333333" {
		t.Fatalf("unexpected pull request from gh fallback: %+v", pr)
	}
}
