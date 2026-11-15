package auth

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestNewTokenIsRandomAndHashed(t *testing.T) {
	first, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	second, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	if first == second {
		t.Fatal("two generated tokens are identical")
	}
	// base64url of 32 bytes is 43 characters.
	if len(first) < 40 {
		t.Fatalf("token %q is too short", first)
	}
	hash := hashToken(first)
	if len(hash) != 32 {
		t.Fatalf("hash length = %d, want 32", len(hash))
	}
	if string(hash) == first {
		t.Fatal("the token is stored verbatim")
	}
	if !tokensMatch(hash, hashToken(first)) {
		t.Error("tokensMatch rejected the matching hash")
	}
	if tokensMatch(hash, hashToken(second)) {
		t.Error("tokensMatch accepted a different token")
	}
}

func TestUUIDRoundTrip(t *testing.T) {
	const canonical = "6f1c0a3e-5b9d-4a1f-9c2b-7e8d0f1a2b3c"
	parsed, err := parseUUID(canonical)
	if err != nil {
		t.Fatalf("parseUUID: %v", err)
	}
	if got := formatUUID(parsed); got != canonical {
		t.Fatalf("formatUUID = %q, want %q", got, canonical)
	}
	if _, err := parseUUID("not-a-uuid"); err == nil {
		t.Error("parseUUID accepted garbage")
	}
	if got := formatUUID(pgtype.UUID{}); got != "" {
		t.Errorf("formatUUID(invalid) = %q, want empty", got)
	}
}

func TestValidateTimezone(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "UTC", value: "UTC"},
		{name: "Moscow", value: "Europe/Moscow"},
		{name: "fixed offset", value: "Etc/GMT-3"},
		{name: "empty", value: "", wantErr: true},
		{name: "unknown", value: "Mars/Olympus", wantErr: true},
		{name: "server local", value: "Local", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTimezone(tt.value)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidateTimezone(%q) = nil, want an error", tt.value)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateTimezone(%q) = %v", tt.value, err)
			}
		})
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	tests := []struct {
		name       string
		publicURL  string
		wantSecure bool
	}{
		{name: "https deployment", publicURL: "https://cublyverse.example.com", wantSecure: true},
		{name: "http deployment", publicURL: "http://localhost:8080", wantSecure: false},
		{name: "unset", publicURL: "", wantSecure: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &Service{cfg: Config{PublicBaseURL: tt.publicURL}}
			cookie := service.SessionCookie("token", time.Now().Add(time.Hour))
			if cookie.Name != SessionCookieName {
				t.Errorf("cookie name = %q, want %q", cookie.Name, SessionCookieName)
			}
			if cookie.Path != "/" {
				t.Errorf("cookie path = %q, want /", cookie.Path)
			}
			if !cookie.HttpOnly {
				t.Error("cookie is not HttpOnly")
			}
			if cookie.SameSite != http.SameSiteLaxMode {
				t.Errorf("cookie SameSite = %v, want Lax", cookie.SameSite)
			}
			if cookie.Secure != tt.wantSecure {
				t.Errorf("cookie Secure = %v, want %v", cookie.Secure, tt.wantSecure)
			}
			cleared := service.ClearedSessionCookie()
			if !strings.Contains(cleared, "Max-Age=0") {
				t.Errorf("cleared cookie = %q, want Max-Age=0", cleared)
			}
			if !strings.Contains(cleared, SessionCookieName+"=") {
				t.Errorf("cleared cookie = %q, want the session name", cleared)
			}
		})
	}
}
