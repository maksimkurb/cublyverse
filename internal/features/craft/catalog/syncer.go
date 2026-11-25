package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/db"
)

// maxNameRunes is the CHECK constraint on craft_catalog_items.name.
const maxNameRunes = 200

// maxRunError bounds what lands in craft_catalog_sync_runs.error.
const maxRunError = 1000

// manufacturerWebsites is informational data for the manufacturer card. A
// source does not carry it, but a newly created manufacturer should look
// complete in the WebApp.
var manufacturerWebsites = map[string]string{
	ManufacturerDMC:      "https://www.dmc.com/",
	ManufacturerPreciosa: "https://www.preciosa-ornela.com/",
}

// validUnits mirrors the CHECK constraint on craft_catalog_items.unit.
var validUnits = map[string]bool{
	"шт": true, "г": true, "моток": true, "м": true, "см": true, "упаковка": true, "набор": true,
}

// Result is the summary of one sync run. The counters are the same numbers the
// run row stores.
type Result struct {
	Source    string
	RunID     int64
	Created   int
	Updated   int
	Unchanged int
	// Deprecated counts items the run marked as no longer in the catalogue.
	Deprecated int
}

// Syncer writes the output of a Source into the global craft catalogue.
//
// Identity of a row is (manufacturer, category, article). Existing rows are
// updated field by field, except fields a human pinned in manual_override. A
// run never deletes: an item that disappears from the catalogue is deprecated
// only when the fetch kept more than half of the previous catalogue, so a
// truncated download is treated as a failed fetch rather than as a mass
// removal.
type Syncer struct {
	st     *store.Store
	logger *slog.Logger
}

// NewSyncer returns a syncer over st. A nil logger becomes slog.Default().
func NewSyncer(st *store.Store, logger *slog.Logger) *Syncer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Syncer{st: st, logger: logger}
}

// Run fetches source and applies it. The run row is created before the fetch,
// so a crash leaves a visible "running" record; a fetch error finishes it as
// "failed" and is returned to the caller.
func (s *Syncer) Run(ctx context.Context, source Source) (Result, error) {
	if source == nil {
		return Result{}, errors.New("catalog: nil source")
	}
	res := Result{Source: source.Name()}
	run, err := s.st.Queries().CreateCraftCatalogSyncRun(ctx, res.Source)
	if err != nil {
		return res, fmt.Errorf("catalog: start sync run: %w", err)
	}
	res.RunID = run.ID

	// The run row must be finished even when ctx was cancelled while fetching.
	finishCtx := context.WithoutCancel(ctx)
	finish := func(status string, runErr error) {
		message := ""
		if runErr != nil {
			message = truncateRunes(runErr.Error(), maxRunError)
		}
		if _, err := s.st.Queries().FinishCraftCatalogSyncRun(finishCtx, db.FinishCraftCatalogSyncRunParams{
			ID:        res.RunID,
			Status:    status,
			Created:   int32(res.Created),
			Updated:   int32(res.Updated),
			Unchanged: int32(res.Unchanged),
			Error:     message,
		}); err != nil {
			s.logger.Error("catalog: finish sync run", slog.String("source", res.Source), slog.String("error", err.Error()))
		}
	}

	fetched, err := source.Fetch(ctx)
	if err != nil {
		finish("failed", err)
		return res, fmt.Errorf("catalog: fetch %s: %w", res.Source, err)
	}
	entries := s.prepare(fetched)

	startedAt := timestamp(time.Now().UTC())
	var created, updated, unchanged, deprecated int
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		categories := make(map[string]db.CraftCategory)
		manufacturers := make(map[string]pgtype.UUID)
		previous := make(map[groupKey]int64)
		current := make(map[groupKey]int)

		for _, entry := range entries {
			category, ok := categories[entry.Category]
			if !ok {
				row, err := q.GetCraftCategoryBySlug(ctx, entry.Category)
				if errors.Is(err, pgx.ErrNoRows) {
					return fmt.Errorf("unknown category %q", entry.Category)
				}
				if err != nil {
					return fmt.Errorf("get category %q: %w", entry.Category, err)
				}
				category = row
				categories[entry.Category] = row
			}

			manufacturerID, ok := manufacturers[entry.Manufacturer]
			if !ok {
				row, err := q.UpsertCraftManufacturer(ctx, db.UpsertCraftManufacturerParams{
					Name:    entry.Manufacturer,
					Website: manufacturerWebsites[entry.Manufacturer],
				})
				if err != nil {
					return fmt.Errorf("upsert manufacturer %q: %w", entry.Manufacturer, err)
				}
				manufacturerID = row.ID
				manufacturers[entry.Manufacturer] = row.ID
			}

			key := groupKeyOf(manufacturerID, category.ID)
			if _, ok := previous[key]; !ok {
				count, err := q.CountActiveGlobalCraftCatalogItems(ctx, db.CountActiveGlobalCraftCatalogItemsParams{
					ManufacturerID: manufacturerID,
					CategoryID:     category.ID,
					Source:         res.Source,
				})
				if err != nil {
					return fmt.Errorf("count catalogue: %w", err)
				}
				previous[key] = count
			}
			current[key]++

			changed, createdRow, err := s.apply(ctx, q, entry, manufacturerID, category.ID, res.Source, startedAt)
			if err != nil {
				return err
			}
			switch {
			case createdRow:
				created++
			case changed:
				updated++
			default:
				unchanged++
			}
		}

		for key, before := range previous {
			// A fetch that kept half or less of the previous catalogue is a
			// partial download: deprecating would hide items that are still
			// sold. A first sync (before == 0) has nothing to remove.
			if before == 0 || int64(current[key])*2 <= before {
				if before > 0 {
					s.logger.Warn("catalog: partial fetch, nothing deprecated",
						slog.String("source", res.Source),
						slog.Int64("previous", before),
						slog.Int("fetched", current[key]),
					)
				}
				continue
			}
			removed, err := q.DeprecateMissingGlobalCraftCatalogItems(ctx, db.DeprecateMissingGlobalCraftCatalogItemsParams{
				ManufacturerID: pgtype.UUID{Bytes: key.manufacturer, Valid: true},
				CategoryID:     pgtype.UUID{Bytes: key.category, Valid: true},
				Source:         res.Source,
				FetchedAt:      startedAt,
			})
			if err != nil {
				return fmt.Errorf("deprecate missing items: %w", err)
			}
			deprecated += int(removed)
		}
		return nil
	})
	if err != nil {
		finish("failed", err)
		return res, fmt.Errorf("catalog: sync %s: %w", res.Source, err)
	}

	res.Created, res.Updated, res.Unchanged, res.Deprecated = created, updated, unchanged, deprecated
	finish("ok", nil)
	s.logger.Info("catalog: sync finished",
		slog.String("source", res.Source),
		slog.Int("created", res.Created),
		slog.Int("updated", res.Updated),
		slog.Int("unchanged", res.Unchanged),
		slog.Int("deprecated", res.Deprecated),
	)
	return res, nil
}

// prepare normalises entries, drops malformed ones and removes duplicates. A
// scraper bug costs the affected rows, not the whole run.
func (s *Syncer) prepare(entries []Entry) []Entry {
	out := make([]Entry, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		entry.Category = strings.ToLower(strings.TrimSpace(entry.Category))
		entry.Manufacturer = strings.TrimSpace(entry.Manufacturer)
		entry.Article = strings.TrimSpace(entry.Article)
		entry.Series = strings.TrimSpace(entry.Series)
		entry.Name = strings.TrimSpace(entry.Name)
		entry.ColorName = strings.TrimSpace(entry.ColorName)
		entry.Size = strings.TrimSpace(entry.Size)
		entry.Finish = strings.TrimSpace(entry.Finish)
		entry.Unit = strings.TrimSpace(entry.Unit)
		entry.SourceURL = strings.TrimSpace(entry.SourceURL)
		entry.ImageURL = strings.TrimSpace(entry.ImageURL)
		entry.ExternalID = strings.TrimSpace(entry.ExternalID)
		entry.ColorHex = normalizeHex(entry.ColorHex)

		if entry.Name == "" {
			entry.Name = strings.TrimSpace(entry.Manufacturer + " " + entry.Article)
		}
		entry.Name = truncateRunes(entry.Name, maxNameRunes)
		entry.ColorSource = normalizeColorSource(entry.ColorHex, entry.ColorSource)

		if entry.Category == "" || entry.Manufacturer == "" || entry.Article == "" ||
			entry.Name == "" || !validUnits[entry.Unit] {
			s.logger.Warn("catalog: skip malformed entry",
				slog.String("manufacturer", entry.Manufacturer),
				slog.String("article", entry.Article),
				slog.String("category", entry.Category),
				slog.String("unit", entry.Unit),
			)
			continue
		}
		key := entry.Manufacturer + "\x00" + entry.Category + "\x00" + entry.Article
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, entry)
	}
	return out
}

// apply inserts a new global item or updates the existing one. It returns
// whether the row is new and whether an existing row actually changed.
func (s *Syncer) apply(
	ctx context.Context, q *db.Queries, entry Entry,
	manufacturerID, categoryID pgtype.UUID, source string, fetchedAt pgtype.Timestamptz,
) (changed, created bool, err error) {
	existing, err := q.GetGlobalCraftCatalogItemByArticle(ctx, db.GetGlobalCraftCatalogItemByArticleParams{
		ManufacturerID: manufacturerID,
		CategoryID:     categoryID,
		Article:        entry.Article,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = q.CreateCraftCatalogItem(ctx, db.CreateCraftCatalogItemParams{
			WorkspaceID:    pgtype.UUID{}, // NULL: a global catalogue item
			CategoryID:     categoryID,
			ManufacturerID: manufacturerID,
			Article:        entry.Article,
			Series:         entry.Series,
			Name:           entry.Name,
			ColorName:      entry.ColorName,
			ColorHex:       entry.ColorHex,
			ColorSource:    entry.ColorSource,
			Size:           entry.Size,
			Finish:         entry.Finish,
			Unit:           entry.Unit,
			Attrs:          []byte("{}"),
			Source:         source,
			SourceUrl:      entry.SourceURL,
			ImageUrl:       entry.ImageURL,
			ExternalID:     entry.ExternalID,
		})
		if err != nil {
			return false, false, fmt.Errorf("create %s %q: %w", source, entry.Article, err)
		}
		return false, true, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("get %s %q: %w", source, entry.Article, err)
	}

	overrides := manualOverrides(existing.ManualOverride)
	currentFields := itemFields{
		series: existing.Series, name: existing.Name, colorName: existing.ColorName,
		colorHex: existing.ColorHex, colorSource: existing.ColorSource, size: existing.Size,
		finish: existing.Finish, unit: existing.Unit, source: existing.Source,
		sourceURL: existing.SourceUrl, imageURL: existing.ImageUrl, externalID: existing.ExternalID,
	}
	incoming := itemFields{
		series: entry.Series, name: entry.Name, colorName: entry.ColorName,
		colorHex: entry.ColorHex, colorSource: entry.ColorSource, size: entry.Size,
		finish: entry.Finish, unit: entry.Unit, source: source,
		sourceURL: entry.SourceURL, imageURL: entry.ImageURL, externalID: entry.ExternalID,
	}
	final := incoming.withOverrides(currentFields, overrides)

	if final == currentFields {
		// Same data: only record the successful fetch so a later partial fetch
		// does not deprecate the item, and clear a previous deprecation.
		if _, err := q.TouchGlobalCraftCatalogItem(ctx, db.TouchGlobalCraftCatalogItemParams{
			ID: existing.ID, FetchedAt: fetchedAt,
		}); err != nil {
			return false, false, fmt.Errorf("touch %s %q: %w", source, entry.Article, err)
		}
		return false, false, nil
	}

	_, err = q.UpdateGlobalCraftCatalogItem(ctx, db.UpdateGlobalCraftCatalogItemParams{
		ID:          existing.ID,
		Series:      final.series,
		Name:        final.name,
		ColorName:   final.colorName,
		ColorHex:    final.colorHex,
		ColorSource: final.colorSource,
		Size:        final.size,
		Finish:      final.finish,
		Unit:        final.unit,
		Source:      final.source,
		SourceUrl:   final.sourceURL,
		ImageUrl:    final.imageURL,
		ExternalID:  final.externalID,
		FetchedAt:   fetchedAt,
	})
	if err != nil {
		return false, false, fmt.Errorf("update %s %q: %w", source, entry.Article, err)
	}
	return true, false, nil
}

// itemFields is the set of columns a source owns. It is comparable, so the
// syncer can tell an unchanged row from a changed one without writing.
type itemFields struct {
	series      string
	name        string
	colorName   string
	colorHex    string
	colorSource string
	size        string
	finish      string
	unit        string
	source      string
	sourceURL   string
	imageURL    string
	externalID  string
}

// withOverrides keeps the current value of every field listed as true in the
// item's manual_override JSON object.
func (f itemFields) withOverrides(current itemFields, overrides map[string]bool) itemFields {
	if overrides["series"] {
		f.series = current.series
	}
	if overrides["name"] {
		f.name = current.name
	}
	if overrides["color_name"] {
		f.colorName = current.colorName
	}
	if overrides["color_hex"] {
		f.colorHex = current.colorHex
	}
	if overrides["color_source"] {
		f.colorSource = current.colorSource
	}
	if overrides["size"] {
		f.size = current.size
	}
	if overrides["finish"] {
		f.finish = current.finish
	}
	if overrides["unit"] {
		f.unit = current.unit
	}
	if overrides["source"] {
		f.source = current.source
	}
	if overrides["source_url"] {
		f.sourceURL = current.sourceURL
	}
	if overrides["image_url"] {
		f.imageURL = current.imageURL
	}
	if overrides["external_id"] {
		f.externalID = current.externalID
	}
	// Keeping a pinned empty hex must not leave a colour source behind: the
	// two fields describe the same fact.
	if f.colorHex == "" {
		f.colorSource = ""
	}
	return f
}

// manualOverrides decodes the manual_override jsonb. An unreadable value is
// treated as "nothing pinned": a malformed object must not block syncs.
func manualOverrides(raw []byte) map[string]bool {
	out := make(map[string]bool)
	if len(raw) == 0 {
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return make(map[string]bool)
	}
	return out
}

// groupKey identifies one source catalogue: manufacturer plus category.
type groupKey struct {
	manufacturer [16]byte
	category     [16]byte
}

func groupKeyOf(manufacturerID, categoryID pgtype.UUID) groupKey {
	return groupKey{manufacturer: manufacturerID.Bytes, category: categoryID.Bytes}
}

// normalizeColorSource keeps the value inside the CHECK constraint and ties it
// to the presence of a hex value.
func normalizeColorSource(hex, source string) string {
	if hex == "" {
		return ""
	}
	switch source {
	case "official", "approx":
		return source
	default:
		return "approx"
	}
}

func timestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}
