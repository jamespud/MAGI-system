package magi

import (
	"strings"
	"testing"
)

func TestWebFetch_StripHTML_RemovesScriptStyle(t *testing.T) {
	input := `<html><head><style>body { color: red; }</style><script>alert('xss')</script></head><body><p>Hello</p></body></html>`
	result := stripHTML(input)
	if strings.Contains(result, "alert") {
		t.Errorf("script content not stripped: %q", result)
	}
	if strings.Contains(result, "color") {
		t.Errorf("style content not stripped: %q", result)
	}
	if !strings.Contains(result, "Hello") {
		t.Errorf("body content missing: %q", result)
	}
}
