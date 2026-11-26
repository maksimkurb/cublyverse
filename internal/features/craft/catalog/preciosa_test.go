package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"testing"
)

// preciosaServer mimics the e-catalogue: the search endpoint is POSTed to with
// the page in a cookie, detail pages are GETs.
type preciosaServer struct {
	*httptest.Server
	mu           sync.Mutex
	pages        []int
	details      []string
	detailsFail  bool
	detailRoutes map[string]string
}

func newPreciosaServer(t *testing.T) *preciosaServer {
	t.Helper()
	s := &preciosaServer{detailRoutes: map[string]string{
		"/detail/1512": "preciosa_detail_1512.html",
		"/detail/1515": "preciosa_detail_1515.html",
		"/detail/1526": "preciosa_detail_1526.html",
		"/detail/1527": "preciosa_detail_1527.html",
	}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/RUN/product-catalog-perlicky" {
			page := preciosaPageFromCookie(r)
			s.mu.Lock()
			s.pages = append(s.pages, page)
			s.mu.Unlock()
			name := "preciosa_page1.json"
			if page == 2 {
				name = "preciosa_page2.json"
			}
			serveFixture(t, w, name)
			return
		}
		s.mu.Lock()
		s.details = append(s.details, r.URL.Path)
		s.mu.Unlock()
		if s.detailsFail {
			http.Error(w, "detail unavailable", http.StatusInternalServerError)
			return
		}
		name, ok := s.detailRoutes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		serveFixture(t, w, name)
	}))
	t.Cleanup(s.Close)
	return s
}

func serveFixture(t *testing.T, w http.ResponseWriter, name string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Errorf("read fixture %s: %v", name, err)
		http.Error(w, "fixture missing", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(body)
}

var pageCookie = regexp.MustCompile(`"page":"([0-9]+)"`)

func preciosaPageFromCookie(r *http.Request) int {
	match := pageCookie.FindStringSubmatch(r.Header.Get("Cookie"))
	if len(match) != 2 {
		return 1
	}
	page, err := strconv.Atoi(match[1])
	if err != nil || page < 1 {
		return 1
	}
	return page
}

func TestPreciosaFetchWalksPagesAndReadsColourNames(t *testing.T) {
	server := newPreciosaServer(t)
	source := &Preciosa{client: server.Client(), BaseURL: server.URL}

	entries, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("Fetch() returned %d entries, want 4 (two per page)", len(entries))
	}
	if len(server.pages) != 2 || server.pages[0] != 1 || server.pages[1] != 2 {
		t.Fatalf("requested pages = %v, want [1 2]", server.pages)
	}
	if len(server.details) != 4 {
		t.Fatalf("requested %d detail pages, want one per colour", len(server.details))
	}

	byArticle := make(map[string]Entry, len(entries))
	for _, entry := range entries {
		byArticle[entry.Article] = entry
		if entry.Category != CategorySeedBeads || entry.Manufacturer != ManufacturerPreciosa {
			t.Fatalf("entry %q category/manufacturer = %q/%q", entry.Article, entry.Category, entry.Manufacturer)
		}
		if entry.Unit != UnitGram {
			t.Fatalf("entry %q unit = %q, want %q", entry.Article, entry.Unit, UnitGram)
		}
		if entry.ColorHex != "" || entry.ColorSource != "" {
			t.Fatalf("entry %q has a colour the manufacturer does not publish", entry.Article)
		}
	}

	alabaster, ok := byArticle["311-19001-10_0-02090"]
	if !ok {
		t.Fatalf("article 311-19001-10_0-02090 missing, got %v", keys(byArticle))
	}
	if alabaster.ColorName != "alabaster white" {
		t.Errorf("colour name = %q, want alabaster white", alabaster.ColorName)
	}
	if alabaster.Name != "Rocailles, round hole, loose 10/0" {
		t.Errorf("name = %q", alabaster.Name)
	}
	if alabaster.Series != "311-19001" || alabaster.Size != "10/0" {
		t.Errorf("series/size = %q/%q", alabaster.Series, alabaster.Size)
	}
	if alabaster.SourceURL != server.URL+"/detail/1515" {
		t.Errorf("source url = %q", alabaster.SourceURL)
	}
	if alabaster.ImageURL != server.URL+"/produkty/perlicky/fotky/311-19001-10_0-02090.webp?v=1793709269" {
		t.Errorf("image url = %q", alabaster.ImageURL)
	}
	if alabaster.ExternalID != "1515" {
		t.Errorf("external id = %q", alabaster.ExternalID)
	}
}

func TestPreciosaFetchSurvivesMissingDetailPages(t *testing.T) {
	server := newPreciosaServer(t)
	server.detailsFail = true
	source := &Preciosa{client: server.Client(), BaseURL: server.URL}

	entries, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(entries) != 4 {
		t.Fatalf("Fetch() returned %d entries, want 4", len(entries))
	}
	for _, entry := range entries {
		if entry.ColorName != entry.Article[len(entry.Article)-5:] {
			t.Errorf("entry %q colour name = %q, want the colour number", entry.Article, entry.ColorName)
		}
		if entry.Name != "PRECIOSA seed beads 10/0" && entry.Name != "PRECIOSA seed beads 1/0" {
			t.Errorf("entry %q name = %q, want the generic fallback", entry.Article, entry.Name)
		}
	}
}

func TestParsePreciosaDetail(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("testdata", "preciosa_detail_1515.html"))
	if err != nil {
		t.Fatal(err)
	}
	if got := parsePreciosaColor(page); got != "alabaster white" {
		t.Errorf("colour = %q", got)
	}
	if got := parsePreciosaShape(page); got != "Rocailles, round hole, loose" {
		t.Errorf("shape = %q", got)
	}
}

func keys(m map[string]Entry) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}
