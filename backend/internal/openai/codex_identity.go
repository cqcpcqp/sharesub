package openai

import (
	"net/http"
	"strings"
	"sync/atomic"
)

const (
	codexDefaultOriginator = "codex-tui"
	codexProbeVersion      = "0.146.0"
	codexProbeUserAgent    = codexDefaultOriginator + "/" + codexProbeVersion + " (Ubuntu 22.4.0; x86_64) xterm-256color"
)

var codexEffectiveVersion atomic.Value

func init() {
	codexEffectiveVersion.Store(codexProbeVersion)
}

func EffectiveCodexVersion() string {
	version, _ := codexEffectiveVersion.Load().(string)
	if strings.TrimSpace(version) == "" {
		return codexProbeVersion
	}
	return version
}

func setEffectiveCodexVersion(version string) bool {
	version = normalizeCodexVersion(version)
	if version == "" || compareCodexVersions(version, codexProbeVersion) < 0 {
		return false
	}
	codexEffectiveVersion.Store(version)
	return true
}

// applyCodexOAuthIdentity is the single source of the client identity sent to
// ChatGPT Codex OAuth endpoints. Models discovery may provide its explicit
// client_version as version; all other callers use the current default.
func applyCodexOAuthIdentity(headers http.Header, version string) {
	version = EffectiveCodexVersion()
	headers.Set("Originator", codexDefaultOriginator)
	headers.Set("Version", version)
	headers.Set("User-Agent", codexDefaultOriginator+"/"+version+" (Ubuntu 22.4.0; x86_64) xterm-256color")
}

// codexOAuthCredentialIdentity returns the Codex identity used by OAuth token
// exchange and refresh requests. The credential endpoint expects the client
// originator and user agent, but not the inference-plane Version header.
func codexOAuthCredentialIdentity() map[string]string {
	version := EffectiveCodexVersion()
	return map[string]string{
		"Originator": codexDefaultOriginator,
		"User-Agent": codexDefaultOriginator + "/" + version + " (Ubuntu 22.4.0; x86_64) xterm-256color",
	}
}
