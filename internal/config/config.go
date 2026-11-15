// Package config loads the service configuration from the process environment.
//
// Every value has a single source of truth: an environment variable. The
// package validates the configuration before the process starts so that
// misconfiguration is reported once, in one message, instead of failing much
// later with a confusing runtime error.
package config

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"strings"
)

// DefaultHTTPAddr is used when HTTP_ADDR is not set.
const DefaultHTTPAddr = ":8080"

// Supported values for LOG_FORMAT.
const (
	LogFormatText = "text"
	LogFormatJSON = "json"
)

// Config is the complete process configuration.
type Config struct {
	// HTTPAddr is the listen address of the HTTP server, for example ":8080".
	HTTPAddr string
	// PublicBaseURL is the externally reachable base URL without a trailing
	// slash, for example "https://cublyverse.example.com". It may be empty
	// while running locally.
	PublicBaseURL string
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string
	// LogFormat is either LogFormatText or LogFormatJSON.
	LogFormat string

	OIDC     OIDCConfig
	Telegram TelegramConfig
	Matrix   MatrixConfig
}

// OIDCConfig holds the OpenID Connect client settings used for sign in.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	// RedirectURL is the absolute /auth/callback URL registered at the
	// provider. It is derived from PUBLIC_BASE_URL when empty.
	RedirectURL string
}

// TelegramConfig holds the Telegram transport settings.
type TelegramConfig struct {
	BotToken string
}

// MatrixConfig holds the Matrix transport settings.
type MatrixConfig struct {
	Homeserver   string
	Username     string
	Password     string
	CryptoDBPath string
	PickleKey    string
}

// Load reads the configuration from the process environment.
func Load() (Config, error) {
	return LoadFrom(os.Getenv)
}

// LoadFrom reads the configuration using getenv. It keeps tests hermetic: no
// process environment is touched.
func LoadFrom(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:      defaultString(getenv("HTTP_ADDR"), DefaultHTTPAddr),
		PublicBaseURL: strings.TrimRight(strings.TrimSpace(getenv("PUBLIC_BASE_URL")), "/"),
		DatabaseURL:   strings.TrimSpace(getenv("DATABASE_URL")),
		LogFormat:     defaultString(getenv("LOG_FORMAT"), LogFormatText),
		OIDC: OIDCConfig{
			Issuer:       strings.TrimSpace(getenv("OIDC_ISSUER")),
			ClientID:     strings.TrimSpace(getenv("OIDC_CLIENT_ID")),
			ClientSecret: strings.TrimSpace(getenv("OIDC_CLIENT_SECRET")),
			RedirectURL:  strings.TrimSpace(getenv("OIDC_REDIRECT_URL")),
		},
		Telegram: TelegramConfig{
			BotToken: strings.TrimSpace(getenv("TELEGRAM_BOT_TOKEN")),
		},
		Matrix: MatrixConfig{
			Homeserver:   strings.TrimSpace(getenv("MATRIX_HOMESERVER")),
			Username:     strings.TrimSpace(getenv("MATRIX_USERNAME")),
			Password:     strings.TrimSpace(getenv("MATRIX_PASSWORD")),
			CryptoDBPath: defaultString(getenv("MATRIX_CRYPTO_DB"), "matrix-crypto.db"),
			PickleKey:    getenv("MATRIX_PICKLE_KEY"),
		},
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate reports the first group of problems found in the configuration.
// Missing required variables and invalid values are reported together so a
// single start attempt lists everything that has to be fixed.
func (c Config) Validate() error {
	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	var problems []string
	switch c.LogFormat {
	case LogFormatText, LogFormatJSON:
	default:
		problems = append(problems, fmt.Sprintf("LOG_FORMAT must be %q or %q, got %q", LogFormatText, LogFormatJSON, c.LogFormat))
	}
	if strings.TrimSpace(c.HTTPAddr) == "" {
		problems = append(problems, "HTTP_ADDR must not be empty")
	}
	if c.PublicBaseURL != "" {
		u, err := url.Parse(c.PublicBaseURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			problems = append(problems, fmt.Sprintf("PUBLIC_BASE_URL must be an absolute URL, got %q", c.PublicBaseURL))
		} else if u.Scheme != "http" && u.Scheme != "https" {
			problems = append(problems, fmt.Sprintf("PUBLIC_BASE_URL must use http or https, got %q", u.Scheme))
		}
	}
	if u, err := url.Parse(c.DatabaseURL); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		problems = append(problems, "DATABASE_URL must be a postgres:// or postgresql:// URL")
	}
	if c.OIDC.Issuer != "" {
		if c.OIDC.ClientID == "" {
			problems = append(problems, "OIDC_CLIENT_ID is required when OIDC_ISSUER is set")
		}
		if c.OIDC.RedirectURL == "" && c.PublicBaseURL == "" {
			problems = append(problems, "OIDC_REDIRECT_URL or PUBLIC_BASE_URL is required when OIDC_ISSUER is set")
		}
	}
	if c.OIDC.RedirectURL != "" && !isAbsoluteHTTPURL(c.OIDC.RedirectURL) {
		problems = append(problems, fmt.Sprintf("OIDC_REDIRECT_URL must be an absolute http(s) URL, got %q", c.OIDC.RedirectURL))
	}
	if c.Matrix.Homeserver != "" {
		for name, value := range map[string]string{
			"MATRIX_USERNAME":   c.Matrix.Username,
			"MATRIX_PASSWORD":   c.Matrix.Password,
			"MATRIX_PICKLE_KEY": c.Matrix.PickleKey,
		} {
			if value == "" {
				problems = append(problems, name+" is required when MATRIX_HOMESERVER is set")
			}
		}
	}

	if len(problems) > 0 {
		// Sorting keeps the message deterministic for tests and humans.
		slices.Sort(problems)
		return errors.New("invalid configuration: " + strings.Join(problems, "; "))
	}
	return nil
}

// NewLogger builds the process logger from the configured format. It is the
// only logger the process uses; log/slog is the single logging API.
func (c Config) NewLogger(w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var handler slog.Handler
	if c.LogFormat == LogFormatJSON {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}
	return slog.New(handler)
}

// TelegramEnabled reports whether the Telegram transport has a bot token.
func (c Config) TelegramEnabled() bool { return c.Telegram.BotToken != "" }

// MatrixEnabled reports whether the Matrix transport has a homeserver.
func (c Config) MatrixEnabled() bool { return c.Matrix.Homeserver != "" }

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

// isAbsoluteHTTPURL reports whether value is an absolute http(s) URL with a
// host, which is what OIDC providers require for a redirect URI.
func isAbsoluteHTTPURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
