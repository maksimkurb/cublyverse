package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

// tokenBytes is the entropy of every bearer secret this package creates
// (state, confirmation token, session token).
const tokenBytes = 32

// newToken returns a fresh URL-safe random secret.
func newToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken returns the sha256 of a secret. PostgreSQL only ever stores this
// form, so a database leak does not expose usable tokens.
func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// tokensMatch compares two secrets in constant time. It is used where a
// presented secret has to be checked against a stored hash; the hash lookup in
// PostgreSQL itself is not a comparison of secrets.
func tokensMatch(storedHash, presentedHash []byte) bool {
	return subtle.ConstantTimeCompare(storedHash, presentedHash) == 1
}

// formatUUID renders a uuid column as the canonical 8-4-4-4-12 string. A
// principal id is users.id in exactly this form.
func formatUUID(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// parseUUID is the inverse of formatUUID.
func parseUUID(value string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(value); err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid uuid %q: %w", value, err)
	}
	if !u.Valid {
		return pgtype.UUID{}, errors.New("uuid must not be empty")
	}
	return u, nil
}

// marshalMetadata serialises binding metadata, keeping the column non-null.
func marshalMetadata(metadata map[string]string) ([]byte, error) {
	if len(metadata) == 0 {
		return []byte("{}"), nil
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal binding metadata: %w", err)
	}
	return encoded, nil
}
