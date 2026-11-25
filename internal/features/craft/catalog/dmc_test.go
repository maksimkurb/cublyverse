package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// fixtureServer serves files from testdata. Handlers are looked up by path, so
// a test never touches the network.
func fixtureServer(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		body, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Errorf("read fixture %s: %v", name, err)
			http.Error(w, "fixture missing", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDMCFetchJoinsOfficialArticlesWithCommunityChart(t *testing.T) {
	server := fixtureServer(t, map[string]string{
		"/page":   "dmc_page.html",
		"/colors": "dmc_colors.csv",
	})
	source := &DMC{
		client:    server.Client(),
		PageURL:   server.URL + "/page",
		ColorsURL: server.URL + "/colors",
	}

	entries, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(entries) != 508 {
		t.Fatalf("Fetch() returned %d entries, want 508 from the saved official page", len(entries))
	}

	byArticle := make(map[string]Entry, len(entries))
	for _, entry := range entries {
		byArticle[entry.Article] = entry
		if entry.Category != CategoryFloss || entry.Manufacturer != ManufacturerDMC {
			t.Fatalf("entry %q category/manufacturer = %q/%q", entry.Article, entry.Category, entry.Manufacturer)
		}
		if entry.Unit != UnitSkein {
			t.Fatalf("entry %q unit = %q, want %q", entry.Article, entry.Unit, UnitSkein)
		}
	}

	black, ok := byArticle["310"]
	if !ok {
		t.Fatal("article 310 missing")
	}
	if black.ColorName != "Black" || black.ColorHex != "#000000" || black.ColorSource != "approx" {
		t.Fatalf("310 colour = %q %q %q, want Black #000000 approx", black.ColorName, black.ColorHex, black.ColorSource)
	}
	if black.Name != "Mouliné Spécial 310" {
		t.Fatalf("310 name = %q", black.Name)
	}
	if black.ExternalID != "117MC-E_310" {
		t.Fatalf("310 external id = %q", black.ExternalID)
	}

	// An official article absent from the third-party chart is imported
	// without a colour: the manufacturer list wins.
	missing, ok := byArticle["01"]
	if !ok {
		t.Fatal("article 01 missing: an article absent from the chart must still be imported")
	}
	if missing.ColorHex != "" || missing.ColorSource != "" {
		t.Fatalf("01 colour = %q %q, want empty", missing.ColorHex, missing.ColorSource)
	}

	// The chart spells the special shades differently from the SKU list; the
	// display article keeps DMC's colour-card spelling.
	blanc, ok := byArticle["Blanc"]
	if !ok {
		t.Fatal("article Blanc missing")
	}
	if blanc.ColorName != "White" || blanc.ColorHex != "#EEEEEE" {
		t.Fatalf("Blanc colour = %q %q", blanc.ColorName, blanc.ColorHex)
	}
}

func TestDMCChartToleratesBadRows(t *testing.T) {
	chart, err := parseDMCChart([]byte("\ufeffDMC_COLOR,COLOR_NAME,RGB_COLOR\n310,Black,#000000\nbroken,,#not-a-colour\n,NoCode,#ffffff\n"))
	if err != nil {
		t.Fatalf("parseDMCChart() error = %v", err)
	}
	if len(chart) != 1 {
		t.Fatalf("chart has %d rows, want only 310", len(chart))
	}
	if chart["310"].name != "Black" || chart["310"].hex != "#000000" {
		t.Fatalf("310 = %+v", chart["310"])
	}
}

func TestDMCArticlesFailsWhenPageLayoutChanged(t *testing.T) {
	if _, err := parseDMCArticles([]byte("<html><body>nothing here</body></html>")); err == nil {
		t.Fatal("parseDMCArticles() accepted a page without articles")
	}
}

func TestNormalizeHex(t *testing.T) {
	tests := map[string]string{
		"#fff7e7":   "#FFF7E7",
		" #000000 ": "#000000",
		"#12345":    "",
		"red":       "",
		"":          "",
	}
	for input, want := range tests {
		if got := normalizeHex(input); got != want {
			t.Errorf("normalizeHex(%q) = %q, want %q", input, got, want)
		}
	}
}
