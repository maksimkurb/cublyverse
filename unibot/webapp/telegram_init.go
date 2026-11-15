package webapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ValidateTelegramInitData validates Telegram Mini App initData using the bot token.
// maxAge == 0 disables the auth_date age check.
func ValidateTelegramInitData(raw, botToken string, maxAge time.Duration) (url.Values, error) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return nil, err
	}
	gotHash := values.Get("hash")
	if gotHash == "" {
		return nil, errors.New("telegram initData has no hash")
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		if key != "hash" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}
	checkString := strings.Join(parts, "\n")

	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secretMAC.Write([]byte(botToken))
	secret := secretMAC.Sum(nil)

	dataMAC := hmac.New(sha256.New, secret)
	_, _ = dataMAC.Write([]byte(checkString))
	expected := hex.EncodeToString(dataMAC.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(strings.ToLower(gotHash))) {
		return nil, errors.New("invalid telegram initData hash")
	}

	if maxAge > 0 {
		// auth_date is Unix seconds, handled manually to avoid accepting malformed data.
		var unix int64
		for _, c := range values.Get("auth_date") {
			if c < '0' || c > '9' {
				return nil, errors.New("invalid telegram auth_date")
			}
			unix = unix*10 + int64(c-'0')
		}
		if unix == 0 {
			return nil, errors.New("missing telegram auth_date")
		}
		when := time.Unix(unix, 0)
		if time.Since(when) > maxAge || when.After(time.Now().Add(2*time.Minute)) {
			return nil, errors.New("telegram initData expired")
		}
	}
	return values, nil
}
