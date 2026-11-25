package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// DefaultPreciosaBaseURL is the public e-catalogue. It is the manufacturer's
// own product database: the seed-bead catalogue is loaded from
// /RUN/product-catalog-perlicky with the same filter the WebApp uses.
const DefaultPreciosaBaseURL = "https://catalog.preciosa-ornela.com"

// maxPreciosaPages guards against a pagination bug turning into an endless
// crawl. The real catalogue has a few hundred pages.
const maxPreciosaPages = 2000

// maxPreciosaDetails bounds the per-colour detail lookups of one run. A detail
// page carries the colour description; a missing one only costs the name, so
// the cap keeps one pathological run from turning into an unbounded crawl.
const maxPreciosaDetails = 2000

var (
	preciosaColorDescription = regexp.MustCompile(`(?s)<p>\s*Color description:\s*</p>\s*<p>\s*(?:<b>)?\s*([^<]*?)\s*(?:</b>)?\s*</p>`)
	preciosaShapeDescription = regexp.MustCompile(`(?s)<p>\s*Article \(shape\) description:\s*</p>\s*<p>\s*(?:<b>)?\s*([^<]*?)\s*(?:</b>)?\s*</p>`)
)

// Preciosa is the PRECIOSA Ornela seed-bead catalogue.
type Preciosa struct {
	client *http.Client
	// BaseURL is the e-catalogue root. Empty means DefaultPreciosaBaseURL.
	BaseURL string
	// Interval is the pause before every request. Zero means no pause;
	// production sources use DefaultInterval.
	Interval time.Duration
	// MaxDetails bounds colour-description lookups per run. Zero means
	// maxPreciosaDetails.
	MaxDetails int
}

var _ Source = (*Preciosa)(nil)

// NewPreciosa returns the PRECIOSA source. A nil client becomes
// NewHTTPClient().
func NewPreciosa(client *http.Client) *Preciosa {
	if client == nil {
		client = NewHTTPClient()
	}
	return &Preciosa{
		client:     client,
		BaseURL:    DefaultPreciosaBaseURL,
		Interval:   DefaultInterval,
		MaxDetails: maxPreciosaDetails,
	}
}

// Name is the source identifier stored with every row.
func (p *Preciosa) Name() string { return "preciosa" }

// preciosaProduct is one entry of the catalogue search response.
type preciosaProduct struct {
	Href      string   `json:"href"`
	Color     string   `json:"colour"`
	Article   string   `json:"article"`
	Size      string   `json:"size"`
	PhotoView string   `json:"photoView"`
	Product   []string `json:"product"`
}

// preciosaPage is one page of the search response. Pages mixes numbers with the
// literal "..." used by the WebApp pagination, hence RawMessage.
type preciosaPage struct {
	Results []preciosaProduct `json:"results"`
	Pages   []json.RawMessage `json:"pages"`
	Page    int               `json:"page"`
}

// Fetch downloads the whole seed-bead catalogue, then reads one detail page per
// distinct colour to learn the official colour description. The detail lookup
// is best effort: a failed page leaves the colour number as the name instead of
// failing the catalogue.
func (p *Preciosa) Fetch(ctx context.Context) ([]Entry, error) {
	base := strings.TrimRight(p.BaseURL, "/")
	if base == "" {
		base = DefaultPreciosaBaseURL
	}
	products, err := p.fetchProducts(ctx, base)
	if err != nil {
		return nil, err
	}

	colors, shapes := p.fetchDetails(ctx, base, products)

	entries := make([]Entry, 0, len(products))
	seen := make(map[string]bool, len(products))
	for _, product := range products {
		code := preciosaProductCode(product)
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true

		name := strings.TrimSpace(shapes[product.Article])
		if name == "" {
			name = "PRECIOSA seed beads"
		}
		name = strings.TrimSpace(name + " " + product.Size)
		colorName := strings.TrimSpace(colors[product.Color])
		if colorName == "" {
			colorName = product.Color
		}
		entry := Entry{
			Category:     CategorySeedBeads,
			Manufacturer: ManufacturerPreciosa,
			Article:      code,
			Series:       product.Article,
			Name:         name,
			ColorName:    colorName,
			Size:         product.Size,
			Finish:       "",
			Unit:         UnitPack,
			SourceURL:    base + product.Href,
			ImageURL:     absolute(base, product.PhotoView),
			ExternalID:   strings.TrimPrefix(product.Href, "/detail/"),
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// fetchProducts walks the paginated search response. The WebApp keeps its
// filters in the "perlicky" cookie; an empty filter object returns the whole
// seed-bead catalogue.
func (p *Preciosa) fetchProducts(ctx context.Context, base string) ([]preciosaProduct, error) {
	endpoint := base + "/RUN/product-catalog-perlicky"
	var out []preciosaProduct
	page := 1
	for page <= maxPreciosaPages {
		if err := wait(ctx, p.Interval); err != nil {
			return nil, err
		}
		body, err := p.postCatalogPage(ctx, endpoint, page)
		if err != nil {
			return nil, err
		}
		var parsed preciosaPage
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, fmt.Errorf("preciosa: parse catalogue page %d: %w", page, err)
		}
		if len(parsed.Results) == 0 {
			break
		}
		out = append(out, parsed.Results...)

		last := preciosaLastPage(parsed.Pages)
		if last == 0 || page >= last {
			break
		}
		page++
	}
	if len(out) == 0 {
		return nil, errors.New("preciosa: catalogue returned no products; the endpoint or filters changed")
	}
	return out, nil
}

// postCatalogPage performs one search request. The cookie holds the WebApp's
// filter state (language, page); page numbers start at 1.
func (p *Preciosa) postCatalogPage(ctx context.Context, endpoint string, page int) ([]byte, error) {
	form := url.Values{"search": {"1"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("preciosa: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", fmt.Sprintf(`perlicky={"lang":"en","page":"%d"}`, page))
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("preciosa: fetch catalogue page %d: %w", page, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("preciosa: fetch catalogue page %d: unexpected status %s", page, resp.Status)
	}
	body, err := readAll(resp)
	if err != nil {
		return nil, fmt.Errorf("preciosa: read catalogue page %d: %w", page, err)
	}
	return body, nil
}

// fetchDetails reads one detail page per distinct colour (for the official
// colour description) and one per article without a colour hit (for the shape
// description). It returns colour code -> description and article -> shape.
func (p *Preciosa) fetchDetails(ctx context.Context, base string, products []preciosaProduct) (map[string]string, map[string]string) {
	colors := make(map[string]string)
	shapes := make(map[string]string)

	colorHrefs := make(map[string]preciosaDetailRef)
	var colorOrder []string
	articleHrefs := make(map[string]string)
	for _, product := range products {
		if product.Color != "" {
			if _, ok := colorHrefs[product.Color]; !ok {
				colorHrefs[product.Color] = preciosaDetailRef{href: product.Href, article: product.Article}
				colorOrder = append(colorOrder, product.Color)
			}
		}
		if product.Article != "" {
			if _, ok := articleHrefs[product.Article]; !ok {
				articleHrefs[product.Article] = product.Href
			}
		}
	}

	limit := p.MaxDetails
	if limit <= 0 {
		limit = maxPreciosaDetails
	}
	requests := 0
	fetch := func(href string) (string, string, bool) {
		if requests >= limit {
			return "", "", false
		}
		requests++
		if err := wait(ctx, p.Interval); err != nil {
			return "", "", false
		}
		body, err := get(ctx, p.client, base+href)
		if err != nil {
			return "", "", false
		}
		return parsePreciosaColor(body), parsePreciosaShape(body), true
	}

	for _, color := range colorOrder {
		ref := colorHrefs[color]
		description, shape, ok := fetch(ref.href)
		if !ok {
			continue
		}
		if description != "" {
			colors[color] = description
		}
		if shape != "" && ref.article != "" {
			shapes[ref.article] = shape
		}
	}
	for article, href := range articleHrefs {
		if _, ok := shapes[article]; ok {
			continue
		}
		_, shape, ok := fetch(href)
		if ok && shape != "" {
			shapes[article] = shape
		}
	}
	return colors, shapes
}

// preciosaDetailRef is the detail page chosen for one colour and the article
// it belongs to.
type preciosaDetailRef struct {
	href    string
	article string
}

// preciosaProductCode is the unique article of one size/colour combination.
// The response's product[0] is exactly that code; it is rebuilt when absent.
func preciosaProductCode(product preciosaProduct) string {
	if len(product.Product) > 0 && product.Product[0] != "" {
		return product.Product[0]
	}
	if product.Article == "" || product.Color == "" {
		return ""
	}
	size := strings.ReplaceAll(product.Size, "/", "_")
	return strings.Trim(product.Article+"-"+size+"-"+product.Color, "-")
}

// preciosaLastPage returns the largest page number in the pagination array.
func preciosaLastPage(pages []json.RawMessage) int {
	last := 0
	for _, raw := range pages {
		var number int
		if err := json.Unmarshal(raw, &number); err == nil && number > last {
			last = number
		}
	}
	return last
}

// parsePreciosaColor extracts the official colour description from a detail
// page, for example "alabaster white".
func parsePreciosaColor(page []byte) string {
	return matchText(preciosaColorDescription, page)
}

// parsePreciosaShape extracts the official shape description, for example
// "Rocailles, round hole, loose".
func parsePreciosaShape(page []byte) string {
	return matchText(preciosaShapeDescription, page)
}

func matchText(re *regexp.Regexp, page []byte) string {
	match := re.FindSubmatch(page)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(string(match[1])))
}

// absolute turns a site-relative image path into an absolute URL.
func absolute(base, path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

// readAll reads a response body with a bound, so a broken endpoint cannot
// exhaust memory.
func readAll(resp *http.Response) ([]byte, error) {
	const maxPageSize = 4 << 20
	return io.ReadAll(io.LimitReader(resp.Body, maxPageSize))
}
