package openai

import (
	"net/http"
	"strings"
)

const (
	codexDefaultOriginator = "codex-tui"
	codexProbeVersion      = "0.146.0"
	codexProbeUserAgent    = codexDefaultOriginator + "/" + codexProbeVersion + " (Ubuntu 22.4.0; x86_64) xterm-256color"
)

// applyCodexOAuthIdentity is the single source of the client identity sent to
// ChatGPT Codex OAuth endpoints. Models discovery may provide its explicit
// client_version as version; all other callers use the current default.
func applyCodexOAuthIdentity(headers http.Header, version string) {
	version = strings.TrimSpace(version)
	if version == "" {
		version = codexProbeVersion
	}
	headers.Set("Originator", codexDefaultOriginator)
	headers.Set("Version", version)
	headers.Set("User-Agent", codexProbeUserAgent)
}
