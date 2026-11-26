package catalog_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/features/craft/catalog"
)

func TestPhotoColorsReadsEachPhotoOnce(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)

	picture := image.NewRGBA(image.Rect(0, 0, 64, 48))
	draw.Draw(picture, picture.Bounds(), image.NewUniform(color.RGBA{200, 20, 30, 255}), image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var outside atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { outside.Add(1) }))
	defer other.Close()
	var requests atomic.Int32
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/red.webp":
			// The real site labels JPEG files as WebP.
			w.Header().Set("Content-Type", "image/webp")
			_, _ = w.Write(encoded.Bytes())
		case "/moved.webp":
			http.Redirect(w, r, other.URL+"/secret", http.StatusFound)
		case "/broken.webp":
			_, _ = w.Write([]byte("<html>maintenance</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer site.Close()

	photo := func(article, url string) catalog.Entry {
		entry := bead(article, "Bead "+article, "red")
		entry.ImageURL = url
		return entry
	}
	source := &fakeSource{name: "preciosa", entries: []catalog.Entry{
		photo("red", site.URL+"/red.webp"),
		photo("gone", site.URL+"/gone.webp"),
		photo("broken", site.URL+"/broken.webp"),
		photo("elsewhere", other.URL+"/photo.webp"),
		photo("moved", site.URL+"/moved.webp"),
	}}
	if _, err := catalog.NewSyncer(st, quietLogger()).Run(ctx, source); err != nil {
		t.Fatalf("sync: %v", err)
	}

	reader := catalog.NewPhotoColors(st, quietLogger(), nil)
	reader.BaseURL, reader.Interval = site.URL, 0
	got, err := reader.Run(ctx)
	if err != nil {
		t.Fatalf("read colours: %v", err)
	}
	if want := (catalog.PhotoResult{Colored: 1, Unreadable: 3, Failed: 1}); got != want {
		t.Errorf("first pass = %+v, want %+v", got, want)
	}
	if hex := itemByArticle(t, st, "red").PhotoColorHex; hex < "#C0" || hex > "#D0" {
		t.Errorf("photo colour = %q, want a red close to #C8141E", hex)
	}
	if hex := itemByArticle(t, st, "red").ColorHex; hex != "" {
		t.Errorf("published colour = %q, want it left empty", hex)
	}
	if n := outside.Load(); n != 0 {
		t.Errorf("%d requests left the catalogue site", n)
	}

	before := requests.Load()
	got, err = reader.Run(ctx)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	// Only the failed request is repeated.
	if want := (catalog.PhotoResult{Failed: 1}); got != want || requests.Load() != before+1 {
		t.Errorf("second pass = %+v with %d requests, want %+v with one", got, requests.Load()-before, want)
	}
}
