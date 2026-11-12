package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestFetchGitHubStatsSendsTheGivenToken(t *testing.T) {
	previousTransport := http.DefaultTransport
	t.Cleanup(func() {
		http.DefaultTransport = previousTransport
	})
	var gotAuth, gotVersion string
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotAuth = req.Header.Get("Authorization")
		gotVersion = req.Header.Get("X-GitHub-Api-Version")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"stargazers_count": 12,
				"forks_count": 3,
				"open_issues_count": 4
			}`)),
		}, nil
	})

	stats, err := fetchGitHubStats("https://github.com/openai/openai-go", "test-token")
	if err != nil {
		t.Fatalf("fetch github stats: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("expected bearer token, got %q", gotAuth)
	}
	if gotVersion != "2022-11-28" {
		t.Fatalf("expected github api version header, got %q", gotVersion)
	}
	if stats.Stars == nil || *stats.Stars != 12 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}
