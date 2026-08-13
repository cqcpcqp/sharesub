package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	codexLatestReleaseURL  = "https://api.github.com/repos/openai/codex/releases/latest"
	codexRecentReleasesURL = "https://api.github.com/repos/openai/codex/releases?per_page=30"
	codexReleaseTagPrefix  = "rust-v"
)

type codexRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

var codexVersionPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){1,3}$`)

func normalizeCodexVersion(version string) string {
	version = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(version), "v"))
	if !codexVersionPattern.MatchString(version) {
		return ""
	}
	return version
}

func compareCodexVersions(left, right string) int {
	leftParts, rightParts := strings.Split(left, "."), strings.Split(right, ".")
	for index := 0; index < len(leftParts) || index < len(rightParts); index++ {
		var l, r int64
		if index < len(leftParts) {
			l, _ = strconv.ParseInt(leftParts[index], 10, 64)
		}
		if index < len(rightParts) {
			r, _ = strconv.ParseInt(rightParts[index], 10, 64)
		}
		if l < r {
			return -1
		}
		if l > r {
			return 1
		}
	}
	return 0
}

func fetchCodexReleases(ctx context.Context, client *http.Client, url string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "share2api-codex-version-sync")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Codex release lookup returned status %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(target)
}

func latestStableCodexVersion(releases []codexRelease) string {
	best := ""
	for _, release := range releases {
		if release.Draft || release.Prerelease {
			continue
		}
		tag := strings.TrimSpace(release.TagName)
		if !strings.HasPrefix(tag, codexReleaseTagPrefix) {
			continue
		}
		version := strings.TrimPrefix(tag, codexReleaseTagPrefix)
		if !codexVersionPattern.MatchString(version) {
			continue
		}
		if version != "" && (best == "" || compareCodexVersions(version, best) > 0) {
			best = version
		}
	}
	return best
}

func FetchLatestCodexVersion(ctx context.Context, client *http.Client) (string, error) {
	if client == nil {
		return "", fmt.Errorf("Codex version sync HTTP client is required")
	}
	var latest codexRelease
	latestErr := fetchCodexReleases(ctx, client, codexLatestReleaseURL, &latest)
	if latestErr == nil {
		if version := latestStableCodexVersion([]codexRelease{latest}); version != "" {
			return version, nil
		}
	}

	var recent []codexRelease
	if err := fetchCodexReleases(ctx, client, codexRecentReleasesURL, &recent); err != nil {
		if latestErr != nil {
			return "", fmt.Errorf("latest Codex release lookup failed: %v; recent release lookup failed: %w", latestErr, err)
		}
		return "", fmt.Errorf("recent Codex release lookup failed: %w", err)
	}
	if version := latestStableCodexVersion(recent); version != "" {
		return version, nil
	}
	return "", fmt.Errorf("no stable Codex client release found")
}

func SyncCodexVersion(ctx context.Context, client *http.Client) error {
	version, err := FetchLatestCodexVersion(ctx, client)
	if err != nil {
		return err
	}
	if compareCodexVersions(version, EffectiveCodexVersion()) <= 0 {
		return nil
	}
	if !setEffectiveCodexVersion(version) {
		return fmt.Errorf("Codex release version %q is below the supported minimum", version)
	}
	return nil
}

func RunCodexVersionSync(ctx context.Context, client *http.Client, interval time.Duration, report func(error)) {
	if interval <= 0 {
		interval = 6 * time.Hour
	}
	sync := func() {
		requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := SyncCodexVersion(requestCtx, client); err != nil && report != nil {
			report(err)
		}
	}
	sync()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sync()
		}
	}
}
