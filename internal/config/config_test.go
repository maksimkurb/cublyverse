package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// env returns a getenv function backed by a map, so tests never touch the real
// process environment.
func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestLoadFromDefaults(t *testing.T) {
	cfg, err := LoadFrom(env(map[string]string{
		"DATABASE_URL": "postgres://user:pass@localhost:5432/cublyverse?sslmode=disable",
	}))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got, want := cfg.HTTPAddr, DefaultHTTPAddr; got != want {
		t.Errorf("HTTPAddr = %q, want %q", got, want)
	}
	if got, want := cfg.LogFormat, LogFormatText; got != want {
		t.Errorf("LogFormat = %q, want %q", got, want)
	}
	if got, want := cfg.Matrix.CryptoDBPath, "matrix-crypto.db"; got != want {
		t.Errorf("Matrix.CryptoDBPath = %q, want %q", got, want)
	}
	if cfg.TelegramEnabled() {
		t.Error("TelegramEnabled() = true, want false without TELEGRAM_BOT_TOKEN")
	}
	if cfg.MatrixEnabled() {
		t.Error("MatrixEnabled() = true, want false without MATRIX_HOMESERVER")
	}
}

func TestLoadFromValues(t *testing.T) {
	cfg, err := LoadFrom(env(map[string]string{
		"HTTP_ADDR":          "127.0.0.1:9000",
		"PUBLIC_BASE_URL":    "https://cublyverse.example.com/",
		"DATABASE_URL":       "postgresql://cublyverse@db:5432/cublyverse",
		"LOG_FORMAT":         "json",
		"OIDC_ISSUER":        "https://id.example.com",
		"OIDC_CLIENT_ID":     "cublyverse",
		"OIDC_CLIENT_SECRET": "secret",
		"TELEGRAM_BOT_TOKEN": "123:abc",
		"MATRIX_HOMESERVER":  "https://matrix.example.com",
		"MATRIX_USERNAME":    "cubly",
		"MATRIX_PASSWORD":    "pass",
		"MATRIX_PICKLE_KEY":  "pickle",
		"MATRIX_CRYPTO_DB":   "/tmp/crypto.db",
	}))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got, want := cfg.HTTPAddr, "127.0.0.1:9000"; got != want {
		t.Errorf("HTTPAddr = %q, want %q", got, want)
	}
	if got, want := cfg.PublicBaseURL, "https://cublyverse.example.com"; got != want {
		t.Errorf("PublicBaseURL = %q, want %q (trailing slash must be trimmed)", got, want)
	}
	if got, want := cfg.LogFormat, LogFormatJSON; got != want {
		t.Errorf("LogFormat = %q, want %q", got, want)
	}
	if got, want := cfg.OIDC.ClientID, "cublyverse"; got != want {
		t.Errorf("OIDC.ClientID = %q, want %q", got, want)
	}
	if !cfg.TelegramEnabled() {
		t.Error("TelegramEnabled() = false, want true")
	}
	if !cfg.MatrixEnabled() {
		t.Error("MatrixEnabled() = false, want true")
	}
}

func TestLoadFromErrors(t *testing.T) {
	tests := []struct {
		name    string
		values  map[string]string
		wantErr []string
	}{
		{
			name:    "missing database url",
			values:  map[string]string{},
			wantErr: []string{"missing required environment variables", "DATABASE_URL"},
		},
		{
			name: "unknown log format",
			values: map[string]string{
				"DATABASE_URL": "postgres://localhost/cublyverse",
				"LOG_FORMAT":   "xml",
			},
			wantErr: []string{"invalid configuration", `LOG_FORMAT must be "text" or "json"`, "xml"},
		},
		{
			name: "database url with wrong scheme",
			values: map[string]string{
				"DATABASE_URL": "mysql://localhost/cublyverse",
			},
			wantErr: []string{"DATABASE_URL must be a postgres:// or postgresql:// URL"},
		},
		{
			name: "relative public base url",
			values: map[string]string{
				"DATABASE_URL":    "postgres://localhost/cublyverse",
				"PUBLIC_BASE_URL": "/app/",
			},
			wantErr: []string{"PUBLIC_BASE_URL must be an absolute URL"},
		},
		{
			name: "matrix homeserver without credentials",
			values: map[string]string{
				"DATABASE_URL":      "postgres://localhost/cublyverse",
				"MATRIX_HOMESERVER": "https://matrix.example.com",
			},
			wantErr: []string{
				"invalid configuration",
				"MATRIX_PASSWORD is required when MATRIX_HOMESERVER is set",
				"MATRIX_PICKLE_KEY is required when MATRIX_HOMESERVER is set",
				"MATRIX_USERNAME is required when MATRIX_HOMESERVER is set",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadFrom(env(tt.values))
			if err == nil {
				t.Fatal("LoadFrom returned no error")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

func TestNewLoggerFormats(t *testing.T) {
	tests := []struct {
		format  string
		wantSub string
	}{
		{format: LogFormatText, wantSub: "msg=hello"},
		{format: LogFormatJSON, wantSub: `"msg":"hello"`},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			var buf bytes.Buffer
			logger := Config{LogFormat: tt.format}.NewLogger(&buf)
			logger.Info("hello", slog.String("key", "value"))
			if !strings.Contains(buf.String(), tt.wantSub) {
				t.Errorf("log output %q does not contain %q", buf.String(), tt.wantSub)
			}
		})
	}
}
