package catalog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // product photos
	_ "image/png"  // product photos
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/db"
)

const (
	// maxPhotoBytes and maxPhotoSide bound what one photo may cost.
	maxPhotoBytes = 4 << 20
	maxPhotoSide  = 4096
	// maxPhotoFailures stops a pass when the site keeps failing.
	maxPhotoFailures = 10
	// photoRetry is how long an unreadable photo is left alone.
	photoRetry = 30 * 24 * time.Hour
	// photoProgress is how often a long pass reports itself.
	photoProgress = 250
)

// errUnreadablePhoto means asking again soon will not help: the photo is gone,
// too large or not a picture this program reads.
var errUnreadablePhoto = errors.New("unreadable photo")

// PhotoColors reads an approximate colour from the product photos of a source
// that publishes no colour values. It only downloads from that source's own
// site, one photo at a time, and saves every result at once, so an interrupted
// pass continues where it stopped.
type PhotoColors struct {
	st     *store.Store
	logger *slog.Logger
	client *http.Client
	// Source is the catalogue whose photos are read.
	Source string
	// BaseURL is the only site photos are taken from.
	BaseURL string
	// Interval is the pause before every request.
	Interval time.Duration
}

// NewPhotoColors returns the reader of PRECIOSA photos. A nil client becomes
// NewHTTPClient(); a nil logger becomes slog.Default().
func NewPhotoColors(st *store.Store, logger *slog.Logger, client *http.Client) *PhotoColors {
	if client == nil {
		client = NewHTTPClient()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &PhotoColors{
		st: st, logger: logger, client: client,
		Source: "preciosa", BaseURL: DefaultPreciosaBaseURL, Interval: DefaultInterval,
	}
}

// PhotoResult is the summary of one pass.
type PhotoResult struct {
	// Colored photos gave a colour.
	Colored int
	// Unreadable photos are left for a later retry.
	Unreadable int
	// Failed requests will be repeated by the next pass.
	Failed int
}

// Run reads every photo that has no result yet.
func (p *PhotoColors) Run(ctx context.Context) (PhotoResult, error) {
	var res PhotoResult
	base, err := url.Parse(p.BaseURL)
	if err != nil || base.Host == "" {
		return res, fmt.Errorf("catalog: photo base url %q is not usable", p.BaseURL)
	}
	rows, err := p.st.Queries().ListCraftPhotoColorCandidates(ctx, db.ListCraftPhotoColorCandidatesParams{
		Source: p.Source, RetryBefore: time.Now().Add(-photoRetry),
	})
	if err != nil {
		return res, fmt.Errorf("catalog: list photos: %w", err)
	}
	if len(rows) == 0 {
		return res, nil
	}
	p.logger.Info("catalog: reading photo colours", slog.String("source", p.Source), slog.Int("photos", len(rows)))

	// A redirect must not lead the server to another site.
	client := *p.client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !sameSite(base, req.URL) {
			return errors.New("redirect leaves the catalogue site")
		}
		return nil
	}

	failures := 0
	for n, row := range rows {
		if err := wait(ctx, p.Interval); err != nil {
			return res, err
		}
		hex, err := readPhotoColor(ctx, &client, base, row.ImageUrl)
		switch {
		case err == nil:
			res.Colored++
		case errors.Is(err, errUnreadablePhoto):
			res.Unreadable++
			p.logger.Warn("catalog: photo skipped", slog.String("url", row.ImageUrl), slog.String("error", err.Error()))
		default:
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			res.Failed++
			failures++
			if failures >= maxPhotoFailures {
				return res, fmt.Errorf("catalog: %d photos failed in a row, last: %w", failures, err)
			}
			continue
		}
		failures = 0
		if err := p.st.Queries().SetCraftPhotoColor(ctx, db.SetCraftPhotoColorParams{
			ID: row.ID, Hex: hex, Url: row.ImageUrl,
		}); err != nil {
			return res, fmt.Errorf("catalog: save photo colour: %w", err)
		}
		if (n+1)%photoProgress == 0 {
			p.logger.Info("catalog: photo colours in progress", slog.Int("done", n+1), slog.Int("of", len(rows)))
		}
	}
	p.logger.Info("catalog: photo colours finished", slog.String("source", p.Source),
		slog.Int("colored", res.Colored), slog.Int("unreadable", res.Unreadable), slog.Int("failed", res.Failed))
	return res, nil
}

func sameSite(base, target *url.URL) bool {
	return target.Scheme == base.Scheme && target.Host == base.Host
}

// readPhotoColor downloads one photo and returns its colour as #RRGGBB.
func readPhotoColor(ctx context.Context, client *http.Client, base *url.URL, raw string) (string, error) {
	target, err := url.Parse(raw)
	if err != nil || !sameSite(base, target) {
		return "", fmt.Errorf("%w: not on the catalogue site", errUnreadablePhoto)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch photo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return "", fmt.Errorf("%w: %s", errUnreadablePhoto, resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch photo: unexpected status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPhotoBytes+1))
	if err != nil {
		return "", fmt.Errorf("read photo: %w", err)
	}
	if len(body) > maxPhotoBytes {
		return "", fmt.Errorf("%w: larger than %d bytes", errUnreadablePhoto, maxPhotoBytes)
	}
	// The size is checked before decoding allocates the pixels. The format is
	// taken from the content: the site labels JPEG files as WebP.
	config, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("%w: %w", errUnreadablePhoto, err)
	}
	if config.Width > maxPhotoSide || config.Height > maxPhotoSide {
		return "", fmt.Errorf("%w: %d×%d pixels", errUnreadablePhoto, config.Width, config.Height)
	}
	photo, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("%w: %w", errUnreadablePhoto, err)
	}
	return PhotoColor(photo), nil
}

// PhotoColor is the median of each channel over the lower left of a product
// photo, where PRECIOSA beads lie densest. The median ignores the shadows and
// glints that mislead the most frequent colour. Transparent beads still take
// some of the background and silver-lined ones come out darker, so the result
// is only approximate.
func PhotoColor(photo image.Image) string {
	b := photo.Bounds()
	left, right := b.Min.X+b.Dx()*3/100, b.Min.X+b.Dx()*45/100
	top, bottom := b.Min.Y+b.Dy()*55/100, b.Min.Y+b.Dy()*97/100
	if right <= left || bottom <= top {
		left, right, top, bottom = b.Min.X, b.Max.X, b.Min.Y, b.Max.Y
	}
	var red, green, blue []uint8
	for y := top; y < bottom; y++ {
		for x := left; x < right; x++ {
			r, g, bl, _ := photo.At(x, y).RGBA()
			red = append(red, uint8(r>>8))
			green = append(green, uint8(g>>8))
			blue = append(blue, uint8(bl>>8))
		}
	}
	if len(red) == 0 {
		return ""
	}
	middle := func(values []uint8) uint8 {
		slices.Sort(values)
		return values[len(values)/2]
	}
	return fmt.Sprintf("#%02X%02X%02X", middle(red), middle(green), middle(blue))
}
