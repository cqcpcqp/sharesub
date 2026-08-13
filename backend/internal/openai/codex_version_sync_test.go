package openai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFetchLatestCodexVersion(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != codexLatestReleaseURL || req.Header.Get("User-Agent") != "share2api-codex-version-sync" {
			t.Fatalf("request = %s headers=%v", req.URL, req.Header)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"tag_name":"rust-v0.149.1","draft":false,"prerelease":false}`)), Request: req}, nil
	})}
	version, err := FetchLatestCodexVersion(context.Background(), client)
	if err != nil || version != "0.149.1" {
		t.Fatalf("version=%q err=%v", version, err)
	}
}

func TestFetchLatestCodexVersionFallsBackAndSelectsHighestStableClientRelease(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		var body string
		switch req.URL.String() {
		case codexLatestReleaseURL:
			body = `{"tag_name":"rusty-v8-v150.4.0","draft":false,"prerelease":false}`
		case codexRecentReleasesURL:
			body = `[
				{"tag_name":"rust-v0.150.0-alpha.1","draft":false,"prerelease":true},
				{"tag_name":"rust-vv0.999.0","draft":false,"prerelease":false},
				{"tag_name":"rust-v0.148.0","draft":false,"prerelease":false},
				{"tag_name":"rust-v0.149.1","draft":false,"prerelease":false},
				{"tag_name":"rust-v0.999.0","draft":true,"prerelease":false},
				{"tag_name":"v0.200.0","draft":false,"prerelease":false}
			]`
		default:
			t.Fatalf("unexpected request URL %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	version, err := FetchLatestCodexVersion(context.Background(), client)
	if err != nil || version != "0.149.1" || requests != 2 {
		t.Fatalf("version=%q requests=%d err=%v", version, requests, err)
	}
}

func TestSyncCodexVersionDoesNotDowngrade(t *testing.T) {
	previous := EffectiveCodexVersion()
	t.Cleanup(func() { codexEffectiveVersion.Store(previous) })
	codexEffectiveVersion.Store("0.150.0")
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"tag_name":"rust-v0.149.1","draft":false,"prerelease":false}`)), Request: req}, nil
	})}
	if err := SyncCodexVersion(context.Background(), client); err != nil {
		t.Fatal(err)
	}
	if version := EffectiveCodexVersion(); version != "0.150.0" {
		t.Fatalf("effective version downgraded to %q", version)
	}
}

func TestSetEffectiveCodexVersionRejectsOldAndMalformed(t *testing.T) {
	previous := EffectiveCodexVersion()
	t.Cleanup(func() { codexEffectiveVersion.Store(previous) })
	if setEffectiveCodexVersion("0.143.9") || setEffectiveCodexVersion("latest") {
		t.Fatal("accepted unsupported Codex version")
	}
	if !setEffectiveCodexVersion("v0.150.0") || EffectiveCodexVersion() != "0.150.0" {
		t.Fatalf("effective version = %q", EffectiveCodexVersion())
	}
}
