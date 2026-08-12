package htmlsanitize

import (
	"strings"
	"testing"
)

func TestSanitizePreservesRichHTMLStyles(t *testing.T) {
	body := `<style>.notice{color:#92400e}</style><div class="notice" style="background:#fef3c7;color:#92400e;padding:16px"><img src="https://example.com/logo.png" alt="Logo">Notice</div>`

	cleaned := Sanitize(body)
	for _, expected := range []string{
		`<style>.notice{color:#92400e}</style>`,
		`class="notice"`,
		`style="background:#fef3c7;color:#92400e;padding:16px"`,
		`src="https://example.com/logo.png"`,
	} {
		if !strings.Contains(cleaned, expected) {
			t.Fatalf("Sanitize() = %q, missing %q", cleaned, expected)
		}
	}
}

func TestSanitizeRemovesActiveContent(t *testing.T) {
	body := `<div style="color:red;beh/**/avior:url(x)" onclick="alert(1)">Safe<script>alert(1)</script><a href="javascript:alert(1)">link</a></div>`

	cleaned := Sanitize(body)
	for _, dangerous := range []string{"<script", "alert(1)", "onclick", "behavior", "javascript:"} {
		if strings.Contains(strings.ToLower(cleaned), dangerous) {
			t.Fatalf("Sanitize() = %q, contains %q", cleaned, dangerous)
		}
	}
	if !strings.Contains(cleaned, `style="color:red"`) {
		t.Fatalf("Sanitize() removed safe CSS declaration: %q", cleaned)
	}
	if !strings.Contains(cleaned, "Safe") || !strings.Contains(cleaned, ">link</a>") {
		t.Fatalf("Sanitize() removed safe content: %q", cleaned)
	}
}

func TestSanitizeRemovesTopLevelScript(t *testing.T) {
	cleaned := Sanitize(`<script>alert(1)</script><p>Safe</p>`)
	if strings.Contains(strings.ToLower(cleaned), "script") || cleaned != "<p>Safe</p>" {
		t.Fatalf("Sanitize() = %q", cleaned)
	}
}
