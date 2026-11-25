// Package catalog fills the global craft catalogue from official manufacturer
// data: DMC embroidery floss and PRECIOSA Ornela seed beads.
//
// A Source turns one manufacturer's public catalogue into Entry values. The
// Syncer stores them as global catalogue items (craft_catalog_items with
// workspace_id IS NULL) and never touches workspace-private items, which are a
// human's own corrections. Sources are deliberately thin: HTML/JSON/CSV
// parsing lives here, business rules and persistence live in the Syncer.
package catalog

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Category slugs and units defined by the craft migration. They are repeated
// here because this package must not import its parent (craft imports catalog).
const (
	CategoryFloss     = "floss"
	CategorySeedBeads = "seed-beads"

	ManufacturerDMC      = "DMC"
	ManufacturerPreciosa = "PRECIOSA Ornela"

	UnitSkein = "моток"
	UnitPack  = "упаковка"
)

// userAgent identifies the scraper to the manufacturers. It is intentionally
// generic: no personal contact data is committed to the repository.
const userAgent = "cublyverse-catalog/1.0"

// requestTimeout bounds a single catalogue request.
const requestTimeout = 60 * time.Second

// Entry is one catalogue row a source produced. Fields mirror the columns of
// craft_catalog_items; the Syncer decides which ones a manual override keeps.
type Entry struct {
	// Category is a category slug such as "floss" or "seed-beads".
	Category string
	// Manufacturer is the display name stored in craft_manufacturers.
	Manufacturer string
	// Article is the manufacturer article number; with the manufacturer and
	// category it identifies the row across runs.
	Article string
	// Series groups articles of one product line, for example "Mouliné Spécial"
	// or the PRECIOSA shape article "311-19001".
	Series      string
	Name        string
	ColorName   string
	ColorHex    string
	ColorSource string
	Size        string
	Finish      string
	Unit        string
	SourceURL   string
	ImageURL    string
	ExternalID  string
}

// Source is one official catalogue.
type Source interface {
	// Name is the stable source identifier written to craft_catalog_items.source
	// and craft_catalog_sync_runs.source: "dmc" or "preciosa".
	Name() string
	// Fetch downloads and parses the whole catalogue. It must be safe to call
	// once per run and must not write anything.
	Fetch(ctx context.Context) ([]Entry, error)
}

// NewHTTPClient returns the client every source uses by default: a bounded
// timeout and a truthful User-Agent so the manufacturer can identify the load.
func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout: requestTimeout,
		Transport: &userAgentTransport{
			base:      http.DefaultTransport,
			userAgent: userAgent,
		},
	}
}

// userAgentTransport sets the User-Agent on every request, including redirects.
type userAgentTransport struct {
	base      http.RoundTripper
	userAgent string
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("User-Agent", t.userAgent)
	return t.base.RoundTrip(clone)
}

// get performs one GET and returns the body. A non-2xx status is an error, so a
// captive portal or a maintenance page never becomes a partial catalogue.
func get(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil, fmt.Errorf("fetch %s: unexpected status %s", url, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	return body, nil
}

// wait pauses between requests to keep the load on the manufacturer polite.
// A zero interval (tests) does not sleep at all.
func wait(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return nil
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
