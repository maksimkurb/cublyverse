package auth

import (
	"testing"
	"time"
)

func TestPATRateLimiterBurstAndRefill(t *testing.T) {
	limiter := newPATRateLimiter()
	const ip = "203.0.113.7"

	// A fresh bucket allows exactly the burst and then rejects.
	for attempt := 0; attempt < patFailureBurst; attempt++ {
		if !limiter.allow(ip) {
			t.Fatalf("attempt %d was rejected before the burst was spent", attempt+1)
		}
	}
	if limiter.allow(ip) {
		t.Fatal("the bucket still allows requests after the burst")
	}

	// A successful lookup resets the bucket: a legitimate client is never
	// locked out because of a typo.
	limiter.succeed(ip)
	if !limiter.allow(ip) {
		t.Fatal("a successful lookup did not reset the bucket")
	}

	// A different address has its own bucket.
	if !limiter.allow("198.51.100.9") {
		t.Fatal("another address shares the bucket of the first one")
	}
}

func TestPATRateLimiterRefills(t *testing.T) {
	limiter := newPATRateLimiter()
	const ip = "203.0.113.8"

	for attempt := 0; attempt < patFailureBurst; attempt++ {
		limiter.allow(ip)
	}
	if limiter.allow(ip) {
		t.Fatal("the bucket is not empty")
	}

	// Move the bucket's clock backwards by one refill step so a token is back
	// without sleeping in the test.
	limiter.mu.Lock()
	limiter.buckets[ip].last = time.Now().Add(-patFailureRefill)
	limiter.mu.Unlock()

	if !limiter.allow(ip) {
		t.Fatal("the bucket did not refill after the refill interval")
	}
}

func TestNormalizeScopes(t *testing.T) {
	tests := []struct {
		name      string
		requested []string
		want      []string
		wantErr   bool
	}{
		{
			name:      "deduplicates and sorts by the registry",
			requested: []string{ScopeRemindersWrite, ScopeProfileRead, ScopeRemindersWrite},
			want:      []string{ScopeProfileRead, ScopeRemindersWrite},
		},
		{
			name:      "trims whitespace",
			requested: []string{" reminders:read "},
			want:      []string{ScopeRemindersRead},
		},
		{
			name:      "rejects unknown scopes",
			requested: []string{"reminders:admin"},
			wantErr:   true,
		},
		{
			name:      "rejects an empty list",
			requested: nil,
			wantErr:   true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeScopes(test.requested)
			if test.wantErr {
				if err == nil {
					t.Fatalf("normalizeScopes(%v) returned no error", test.requested)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeScopes(%v): %v", test.requested, err)
			}
			if len(got) != len(test.want) {
				t.Fatalf("normalizeScopes(%v) = %v, want %v", test.requested, got, test.want)
			}
			for i := range got {
				if got[i] != test.want[i] {
					t.Fatalf("normalizeScopes(%v) = %v, want %v", test.requested, got, test.want)
				}
			}
		})
	}
}

func TestSessionHasScope(t *testing.T) {
	session := &Session{} // interactive session
	if !session.HasScope(ScopeRemindersWrite) {
		t.Fatal("a session must not be limited by token scopes")
	}

	readOnly := Session{APITokenID: "token", Scopes: []string{ScopeRemindersRead}}
	if !readOnly.HasScope(ScopeRemindersRead) {
		t.Fatal("a granted scope was rejected")
	}
	if !readOnly.HasScope(ScopeProfileRead) {
		t.Fatal("any granted scope must imply profile:read")
	}
	if readOnly.HasScope(ScopeRemindersWrite) {
		t.Fatal("an ungranted scope was accepted")
	}
}

func TestIsAPIToken(t *testing.T) {
	if !IsAPIToken("cv_pat_abcdef") {
		t.Fatal("a cv_pat_ value was not recognised")
	}
	if IsAPIToken("not-a-pat") {
		t.Fatal("an ordinary value was mistaken for a token")
	}
}
