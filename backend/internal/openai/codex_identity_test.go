package openai

import (
	"net/http"
	"testing"
)

func TestApplyCodexOAuthIdentity(t *testing.T) {
	if codexDefaultOriginator != "codex-tui" || codexProbeVersion != "0.146.0" ||
		codexProbeUserAgent != "codex-tui/0.146.0 (Ubuntu 22.4.0; x86_64) xterm-256color" {
		t.Fatalf("unexpected Codex identity constants: %q %q %q", codexDefaultOriginator, codexProbeVersion, codexProbeUserAgent)
	}

	header := http.Header{
		"Originator": []string{"untrusted-client"},
		"Version":    []string{"9.9.9"},
		"User-Agent": []string{"untrusted-client/9.9.9"},
	}
	applyCodexOAuthIdentity(header, "")
	if header.Get("Originator") != codexDefaultOriginator || header.Get("Version") != codexProbeVersion || header.Get("User-Agent") != codexProbeUserAgent {
		t.Fatalf("default identity = %#v", header)
	}

	applyCodexOAuthIdentity(header, "0.137.0")
	if header.Get("Originator") != codexDefaultOriginator || header.Get("Version") != "0.137.0" || header.Get("User-Agent") != codexProbeUserAgent {
		t.Fatalf("explicit-version identity = %#v", header)
	}
}
