package auth_test

import (
	"testing"

	"github.com/maksimkurb/cublyverse/internal/auth"
)

// TestSafeReturnTo guards the open-redirect defence: only same-site relative
// paths may be used as a post-login target.
func TestSafeReturnTo(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{name: "empty uses the app", raw: "", want: "/app/", ok: true},
		{name: "app", raw: "/app/", want: "/app/", ok: true},
		{name: "query", raw: "/app/?tab=reminders", want: "/app/?tab=reminders", ok: true},
		{name: "nested", raw: "/a/b/c", want: "/a/b/c", ok: true},
		{name: "fragment", raw: "/app/#/reminders", want: "/app/#/reminders", ok: true},

		{name: "absolute https", raw: "https://evil.example/", ok: false},
		{name: "absolute http", raw: "http://evil.example/", ok: false},
		{name: "protocol relative", raw: "//evil.example/", ok: false},
		{name: "backslash relative", raw: "/\\evil.example/", ok: false},
		{name: "backslash", raw: "\\evil.example", ok: false},
		{name: "relative path", raw: "app/", ok: false},
		{name: "javascript scheme", raw: "javascript:alert(1)", ok: false},
		{name: "mailto scheme", raw: "mailto:user@example.com", ok: false},
		{name: "newline injection", raw: "/app/\r\nSet-Cookie: x=y", ok: false},
		{name: "null byte", raw: "/app/\x00", ok: false},
		{name: "bad escape", raw: "/%zz", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := auth.SafeReturnTo(tt.raw)
			if ok != tt.ok {
				t.Fatalf("SafeReturnTo(%q) ok = %v, want %v", tt.raw, ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Fatalf("SafeReturnTo(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestSafeReturnToRejectsLongValues(t *testing.T) {
	long := "/app/"
	for range 2048 {
		long += "a"
	}
	if _, ok := auth.SafeReturnTo(long); ok {
		t.Fatal("SafeReturnTo accepted an oversized value")
	}
}
