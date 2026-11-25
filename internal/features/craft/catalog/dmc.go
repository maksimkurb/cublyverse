package catalog

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// DefaultDMC* are the public pages the DMC source reads. The product page is
// the manufacturer's own shade list (its embedded analytics array names every
// article of the range); the CSV is an open community compilation of shade
// names and approximate RGB values, because DMC does not publish machine
// readable colour values. See docs/CATALOG_SCRAPERS.md.
const (
	DefaultDMCPageURL   = "https://www.dmc.com/GB/en-GB/products/mouline-special"
	DefaultDMCColorsURL = "https://raw.githubusercontent.com/nathantspencer/DMC-ColorCodes/master/result.csv"
	// DefaultDMCSupplementURL is a second community chart that also knows the
	// newer shades (01-35) the CSV lacks. It only fills gaps.
	DefaultDMCSupplementURL = "https://stitchland.com/tools/dmc-color-chart/"
)

// DefaultInterval is the pause between two requests to one manufacturer. One
// request per second is slow enough for a weekly catalogue sync.
const DefaultInterval = time.Second

// dmcCode matches one article of the DMC Mouliné Spécial family in the
// product page's embedded analytics array, for example "117MC-E_310".
var dmcCode = regexp.MustCompile(`117MC-E_([A-Za-z0-9]+)`)

// dmcHex validates a #RRGGBB value from the third-party chart.
var dmcHex = regexp.MustCompile(`^#[0-9A-F]{6}$`)

// DMC is the DMC embroidery floss catalogue.
type DMC struct {
	client *http.Client
	// PageURL is the Mouliné Spécial product page with the embedded article
	// list. Empty means DefaultDMCPageURL.
	PageURL string
	// ColorsURL is the CSV with shade names and approximate hex values. Empty
	// means DefaultDMCColorsURL.
	ColorsURL string
	// SupplementURL is the second chart; empty skips it.
	SupplementURL string
	// Interval is the pause between the two requests. Zero means no pause;
	// production sources use DefaultInterval.
	Interval time.Duration
}

var _ Source = (*DMC)(nil)

// NewDMC returns the DMC source. A nil client becomes NewHTTPClient().
func NewDMC(client *http.Client) *DMC {
	if client == nil {
		client = NewHTTPClient()
	}
	return &DMC{
		client:        client,
		PageURL:       DefaultDMCPageURL,
		ColorsURL:     DefaultDMCColorsURL,
		SupplementURL: DefaultDMCSupplementURL,
		Interval:      DefaultInterval,
	}
}

// Name is the source identifier stored with every row.
func (d *DMC) Name() string { return "dmc" }

// Fetch downloads the official article list and joins it with the community
// colour chart. Articles missing from the chart are still imported (without a
// colour), so the catalogue follows the manufacturer, not the third party.
func (d *DMC) Fetch(ctx context.Context) ([]Entry, error) {
	pageURL := d.PageURL
	if pageURL == "" {
		pageURL = DefaultDMCPageURL
	}
	colorsURL := d.ColorsURL
	if colorsURL == "" {
		colorsURL = DefaultDMCColorsURL
	}

	page, err := get(ctx, d.client, pageURL)
	if err != nil {
		return nil, fmt.Errorf("dmc: read product page: %w", err)
	}
	articles, err := parseDMCArticles(page)
	if err != nil {
		return nil, err
	}
	if err := wait(ctx, d.Interval); err != nil {
		return nil, err
	}

	rawChart, err := get(ctx, d.client, colorsURL)
	if err != nil {
		return nil, fmt.Errorf("dmc: read colour chart: %w", err)
	}
	chart, err := parseDMCChart(rawChart)
	if err != nil {
		return nil, err
	}

	if d.SupplementURL != "" {
		// A failure fails the whole run: going on without the second chart
		// would erase the colours it filled before.
		if err := wait(ctx, d.Interval); err != nil {
			return nil, err
		}
		rawSupplement, err := get(ctx, d.client, d.SupplementURL)
		if err != nil {
			return nil, fmt.Errorf("dmc: read supplement chart: %w", err)
		}
		supplement, err := parseDMCSupplement(rawSupplement)
		if err != nil {
			return nil, err
		}
		fillGaps(chart, supplement)
	}

	entries := make([]Entry, 0, len(articles))
	for _, code := range articles {
		article := dmcArticle(code)
		shade, ok := chart[strings.ToUpper(code)]
		entry := Entry{
			Category:     CategoryFloss,
			Manufacturer: ManufacturerDMC,
			Article:      article,
			Series:       "Mouliné Spécial",
			Name:         "Mouliné Spécial " + article,
			Size:         "",
			Finish:       "",
			Unit:         UnitSkein,
			SourceURL:    pageURL,
			ExternalID:   "117MC-E_" + code,
		}
		if ok {
			entry.ColorName = shade.name
			if hex := normalizeHex(shade.hex); hex != "" {
				entry.ColorHex = hex
				// The value comes from a third-party chart, never from DMC.
				entry.ColorSource = "approx"
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// parseDMCArticles extracts the shade articles from the product page. The page
// embeds an analytics event whose content_ids list is the complete variant
// set; the order is the manufacturer's, so a catalogue built from it is stable.
func parseDMCArticles(page []byte) ([]string, error) {
	matches := dmcCode.FindAllSubmatch(page, -1)
	seen := make(map[string]bool, len(matches))
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		code := string(match[1])
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		out = append(out, code)
	}
	if len(out) == 0 {
		return nil, errors.New("dmc: product page has no article list; the page layout changed")
	}
	return out, nil
}

// dmcShade is one row of the community colour chart.
type dmcShade struct {
	name string
	hex  string
}

// parseDMCChart reads the CSV chart. The header is looked up by name so a
// reordered column does not break the import; a row without an article is
// skipped instead of failing the whole catalogue.
func parseDMCChart(raw []byte) (map[string]dmcShade, error) {
	reader := csv.NewReader(strings.NewReader(string(raw)))
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("dmc: parse colour chart: %w", err)
	}
	if len(rows) == 0 {
		return nil, errors.New("dmc: colour chart is empty")
	}
	header := rows[0]
	codeColumn, nameColumn, hexColumn := -1, -1, -1
	for i, column := range header {
		switch strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(column), "\ufeff")) {
		case "DMC_COLOR":
			codeColumn = i
		case "COLOR_NAME":
			nameColumn = i
		case "RGB_COLOR":
			hexColumn = i
		}
	}
	if codeColumn < 0 {
		return nil, errors.New("dmc: colour chart has no DMC_COLOR column")
	}

	chart := make(map[string]dmcShade, len(rows))
	for _, row := range rows[1:] {
		if codeColumn >= len(row) {
			continue
		}
		code := strings.ToUpper(strings.TrimSpace(row[codeColumn]))
		if code == "" {
			continue
		}
		shade := dmcShade{}
		if nameColumn >= 0 && nameColumn < len(row) {
			shade.name = strings.TrimSpace(row[nameColumn])
		}
		if hexColumn >= 0 && hexColumn < len(row) {
			shade.hex = normalizeHex(row[hexColumn])
		}
		if shade.name == "" && shade.hex == "" {
			// A code without a name or a colour carries no information.
			continue
		}
		chart[code] = shade
	}
	if len(chart) == 0 {
		return nil, errors.New("dmc: colour chart has no rows")
	}
	return chart, nil
}

// dmcSupplementRow matches one row of the supplement chart's table.
var dmcSupplementRow = regexp.MustCompile(
	`stl-dmcc-code-cell">DMC ([^<]+)</td><td class="stl-dmcc-name-cell">([^<]*)</td>.*?stl-dmcc-hex-cell">(#[0-9A-Fa-f]{6})<`)

// parseDMCSupplement reads the HTML table of the second chart.
func parseDMCSupplement(page []byte) (map[string]dmcShade, error) {
	matches := dmcSupplementRow.FindAllSubmatch(page, -1)
	if len(matches) == 0 {
		return nil, errors.New("dmc: supplement chart has no rows; the page layout changed")
	}
	chart := make(map[string]dmcShade, len(matches))
	for _, match := range matches {
		code := strings.ToUpper(strings.TrimSpace(string(match[1])))
		chart[code] = dmcShade{
			name: strings.TrimSpace(html.UnescapeString(string(match[2]))),
			hex:  normalizeHex(string(match[3])),
		}
	}
	return chart, nil
}

// fillGaps completes the first chart with the second; what the first chart
// knows stays.
func fillGaps(chart, supplement map[string]dmcShade) {
	for code, extra := range supplement {
		shade := chart[code]
		if shade.name == "" {
			shade.name = extra.name
		}
		if shade.hex == "" {
			shade.hex = extra.hex
		}
		chart[code] = shade
	}
}

// dmcArticle normalises the three special shade codes to the spelling DMC uses
// on the colour card. Numeric codes are already canonical.
func dmcArticle(code string) string {
	switch strings.ToUpper(code) {
	case "BLANC":
		return "Blanc"
	case "ECRU":
		return "Ecru"
	case "WHITE":
		return "White"
	default:
		return code
	}
}

// normalizeHex accepts #RRGGBB in any case and returns upper case; anything
// else becomes empty, so one malformed chart row cannot fail the import or trip
// the database CHECK constraint.
func normalizeHex(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if !dmcHex.MatchString(value) {
		return ""
	}
	return value
}
