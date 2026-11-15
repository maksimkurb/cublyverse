package auth

import (
	"net/url"
	"strings"
)

// defaultReturnTo is where a browser lands after a plain SSO login.
const defaultReturnTo = "/app/"

// SafeReturnTo validates the post-login redirect target.
//
// Only same-site relative paths are accepted: an absolute URL, a protocol
// relative path ("//host"), a backslash trick ("/\host") and any control
// character are rejected. Without this an attacker could turn the login link
// into an open redirect.
func SafeReturnTo(raw string) (string, bool) {
	if raw == "" {
		return defaultReturnTo, true
	}
	if len(raw) > 2048 {
		return "", false
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	if !strings.HasPrefix(raw, "/") {
		return "", false
	}
	// Browsers treat "//host" and "/\host" as scheme-relative URLs.
	if strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "/\\") {
		return "", false
	}
	if strings.Contains(raw, "\\") {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil {
		return "", false
	}
	// A path that does not start with "/" after normalisation is relative to
	// wherever the browser currently is, which is hard to reason about.
	if !strings.HasPrefix(parsed.Path, "/") {
		return "", false
	}
	return raw, true
}
