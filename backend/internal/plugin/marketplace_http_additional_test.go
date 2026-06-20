package plugin

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMarketplaceGithubHTTPHelpers(t *testing.T) {
	previousTransport := http.DefaultTransport
	http.DefaultTransport = pluginRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/repos/owner/repo/releases/latest":
			if req.Header.Get("Authorization") != "Bearer token" || req.Header.Get("If-None-Match") != "etag-old" {
				t.Fatalf("release headers auth=%q etag=%q", req.Header.Get("Authorization"), req.Header.Get("If-None-Match"))
			}
			return pluginJSONResponse(req, http.StatusOK, `{"tag_name":"v1.2.3","assets":[{"name":"airgate-core-windows-amd64","browser_download_url":"https://download.test/plugin.exe"}]}`, "etag-new"), nil
		case "/repos/owner/repo/git/ref/tags/v1.2.3":
			return pluginJSONResponse(req, http.StatusOK, `{"object":{"type":"tag","sha":"`+strings.Repeat("a", 40)+`","url":"https://api.github.com/repos/owner/repo/git/tags/object"}}`, ""), nil
		case "/repos/owner/repo/git/tags/object":
			return pluginJSONResponse(req, http.StatusOK, `{"object":{"type":"commit","sha":"`+strings.Repeat("b", 40)+`"}}`, ""), nil
		case "/repos/owner/repo/git/ref/tags/direct":
			return pluginJSONResponse(req, http.StatusOK, `{"object":{"type":"commit","sha":"`+strings.Repeat("c", 40)+`"}}`, ""), nil
		case "/not-found":
			return pluginJSONResponse(req, http.StatusNotFound, `{}`, ""), nil
		default:
			t.Fatalf("unexpected request path %s", req.URL.Path)
			return nil, nil
		}
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	ctx := context.Background()
	release, etag, status, err := fetchLatestRelease(ctx, "owner/repo", "token", "etag-old")
	if err != nil {
		t.Fatalf("fetchLatestRelease() error = %v", err)
	}
	if release.TagName != "v1.2.3" || etag != "etag-new" || status != http.StatusOK {
		t.Fatalf("release=%+v etag=%q status=%d", release, etag, status)
	}

	sha, err := fetchGithubTagCommitSHA(ctx, "owner/repo", "v1.2.3", "")
	if err != nil || sha != strings.Repeat("b", 40) {
		t.Fatalf("tag object sha=%q err=%v", sha, err)
	}
	sha, err = fetchGithubTagCommitSHA(ctx, "owner/repo", "direct", "")
	if err != nil || sha != strings.Repeat("c", 40) {
		t.Fatalf("direct sha=%q err=%v", sha, err)
	}
	if got := resolveGithubTagCommitSHA(ctx, "", "v1", ""); got != "" {
		t.Fatalf("empty repo resolve sha = %q", got)
	}

	var target githubRefInfo
	if err := fetchGithubAPIJSON(ctx, "https://api.github.com/not-found", "", &target); err == nil {
		t.Fatal("fetchGithubAPIJSON non-OK error = nil")
	}
}

func TestFetchLatestReleaseNotModifiedAndErrors(t *testing.T) {
	previousTransport := http.DefaultTransport
	http.DefaultTransport = pluginRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/repos/owner/repo/releases/latest":
			return pluginJSONResponse(req, http.StatusNotModified, ``, ""), nil
		case "/repos/owner/missing/releases/latest":
			return pluginJSONResponse(req, http.StatusNotFound, `{}`, ""), nil
		default:
			t.Fatalf("unexpected request path %s", req.URL.Path)
			return nil, nil
		}
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	release, etag, status, err := fetchLatestRelease(context.Background(), "owner/repo", "", "etag-old")
	if err != nil || release != nil || etag != "etag-old" || status != http.StatusNotModified {
		t.Fatalf("not modified release=%+v etag=%q status=%d err=%v", release, etag, status, err)
	}
	if _, _, _, err := fetchLatestRelease(context.Background(), "owner/missing", "", ""); err == nil {
		t.Fatal("missing release error = nil")
	}
}

type pluginRoundTripFunc func(*http.Request) (*http.Response, error)

func (f pluginRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func pluginJSONResponse(req *http.Request, status int, body string, etag string) *http.Response {
	header := make(http.Header)
	if etag != "" {
		header.Set("ETag", etag)
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
