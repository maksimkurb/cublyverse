package craft

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// Templates of a new warehouse.
const (
	// TemplateCraft starts with the needlework categories, units and both
	// official catalogues. It is the default.
	TemplateCraft = "craft"
	// TemplateEmpty starts with one unit and nothing else.
	TemplateEmpty = "empty"
)

// craftUnits are the units of the needlework template.
var craftUnits = []string{"шт", "г", "моток", "м", "см", "упаковка", "набор"}

// Limits of the reference data.
const (
	maxFields        = 20
	maxOptions       = 50
	maxCategoryRunes = 80
	maxNameRunes     = 120
	maxUnitRunes     = 24
)

// Types of an extra field of a category.
const (
	FieldText   = "text"
	FieldNumber = "number"
	FieldBool   = "bool"
	FieldSelect = "select"
)

// Field is an extra field of the items of one category.
type Field struct {
	// Key is the stable name of the value in Item.Attrs. A new field without a
	// key gets a generated one.
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
	// Options are the allowed values of a select field.
	Options []string `json:"options,omitempty"`
}

// Category is a kind of material of one warehouse.
type Category struct {
	ID          string
	Slug        string
	Name        string
	DefaultUnit string
	Fields      []Field
	// CatalogSlug is set when official items are shown under this category.
	CatalogSlug string
	// Items counts the warehouse's own items of the category.
	Items int
}

// CategoryInput is the editable data of a category.
type CategoryInput struct {
	Name        string
	DefaultUnit string
	Fields      []Field
}

// Manufacturer is a producer of materials.
type Manufacturer struct {
	ID      string
	Name    string
	Website string
	// Official marks a manufacturer of a connected official catalogue; it is
	// read-only.
	Official bool
	// Items counts the warehouse's own items of the manufacturer.
	Items int
}

// Unit is a unit stock can be counted in.
type Unit struct {
	ID   string
	Name string
}

// CatalogSource is an official catalogue a warehouse can connect.
type CatalogSource struct {
	Source  string
	Name    string
	Enabled bool
	// Items is how many positions the catalogue has.
	Items int
}

// officialSources lists the catalogues and the reference categories their
// items live in.
var officialSources = []struct {
	source, name string
	categories   []string
}{
	{"dmc", "DMC (мулине)", []string{"floss"}},
	{"preciosa", "PRECIOSA Ornela (бисер)", []string{"seed-beads"}},
}

// seedWarehouse fills a new warehouse from a template. It runs in the
// transaction that creates the space.
func seedWarehouse(ctx context.Context, q *db.Queries, wid pgtype.UUID, template string) error {
	switch template {
	case TemplateEmpty:
		return q.EnsureCraftUnit(ctx, db.EnsureCraftUnitParams{WorkspaceID: wid, Name: "шт"})
	case "", TemplateCraft:
	default:
		return &workspace.ValidationError{Message: "Неизвестный шаблон склада."}
	}
	if err := q.SeedCraftCategories(ctx, wid); err != nil {
		return fmt.Errorf("seed categories: %w", err)
	}
	for _, unit := range craftUnits {
		if err := q.EnsureCraftUnit(ctx, db.EnsureCraftUnitParams{WorkspaceID: wid, Name: unit}); err != nil {
			return fmt.Errorf("seed units: %w", err)
		}
	}
	for _, official := range officialSources {
		if err := q.AddCraftSpaceCatalog(ctx, db.AddCraftSpaceCatalogParams{WorkspaceID: wid, Source: official.source}); err != nil {
			return fmt.Errorf("seed catalogues: %w", err)
		}
	}
	return nil
}

// --- categories --------------------------------------------------------------

// Categories lists the categories of a warehouse.
func (s *Service) Categories(ctx context.Context, userID, workspaceID string) ([]Category, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	rows, err := s.st.Queries().ListCraftCategories(ctx, wid)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	out := make([]Category, 0, len(rows))
	for _, r := range rows {
		category := toCategory(db.CraftCategory{
			ID: r.ID, Slug: r.Slug, Name: r.Name, DefaultUnit: r.DefaultUnit, Fields: r.Fields, CatalogSlug: r.CatalogSlug,
		})
		category.Items = int(r.OwnItems)
		out = append(out, category)
	}
	return out, nil
}

// SaveCategory creates a category (empty id) or replaces the data of one.
func (s *Service) SaveCategory(ctx context.Context, userID, workspaceID, id string, in CategoryInput) (Category, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return Category{}, err
	}
	name, err := referenceName(in.Name, maxCategoryRunes)
	if err != nil {
		return Category{}, err
	}
	fields, err := normalizeFields(in.Fields)
	if err != nil {
		return Category{}, err
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return Category{}, fmt.Errorf("encode fields: %w", err)
	}
	unit, err := s.unitName(ctx, s.st.Queries(), wid, in.DefaultUnit)
	if err != nil {
		return Category{}, err
	}
	var row db.CraftCategory
	if id == "" {
		slug, err := randomKey("c-")
		if err != nil {
			return Category{}, err
		}
		row, err = s.st.Queries().CreateCraftCategory(ctx, db.CreateCraftCategoryParams{
			WorkspaceID: wid, Slug: slug, Name: name, DefaultUnit: unit, Fields: encoded,
		})
		if err != nil {
			return Category{}, mapDuplicate(err, "Такая категория уже есть.", "create category")
		}
		return toCategory(row), nil
	}
	cid, err := parseItem(id)
	if err != nil {
		return Category{}, err
	}
	row, err = s.st.Queries().UpdateCraftCategory(ctx, db.UpdateCraftCategoryParams{
		ID: cid, WorkspaceID: wid, Name: name, DefaultUnit: unit, Fields: encoded,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Category{}, ErrNotFound
	}
	if err != nil {
		return Category{}, mapDuplicate(err, "Такая категория уже есть.", "update category")
	}
	return toCategory(row), nil
}

// DeleteCategory removes a category that no own item uses. Official items of a
// deleted linked category are shown under the catalogue's own category name.
func (s *Service) DeleteCategory(ctx context.Context, userID, workspaceID, id string) error {
	wid, cid, err := s.referenceTarget(ctx, userID, workspaceID, id)
	if err != nil {
		return err
	}
	return s.st.InTx(ctx, func(q *db.Queries) error {
		if _, err := q.GetCraftCategory(ctx, db.GetCraftCategoryParams{ID: cid, WorkspaceID: wid}); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return fmt.Errorf("get category: %w", err)
		}
		used, err := q.CountCraftItemsOfCategory(ctx, cid)
		if err != nil {
			return fmt.Errorf("count items: %w", err)
		}
		if used > 0 {
			return invalid("В категории есть свои позиции (%d). Сначала перенесите или удалите их.", used)
		}
		if _, err := q.DeleteCraftCategory(ctx, db.DeleteCraftCategoryParams{ID: cid, WorkspaceID: wid}); err != nil {
			return fmt.Errorf("delete category: %w", err)
		}
		return nil
	})
}

// --- manufacturers -----------------------------------------------------------

// Manufacturers lists the own manufacturers and those of the connected
// official catalogues.
func (s *Service) Manufacturers(ctx context.Context, userID, workspaceID string) ([]Manufacturer, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	rows, err := s.st.Queries().ListCraftManufacturers(ctx, wid)
	if err != nil {
		return nil, fmt.Errorf("list manufacturers: %w", err)
	}
	out := make([]Manufacturer, 0, len(rows))
	for _, r := range rows {
		out = append(out, Manufacturer{
			ID: workspace.FormatID(r.ID), Name: r.Name, Website: r.Website,
			Official: !r.WorkspaceID.Valid, Items: int(r.OwnItems),
		})
	}
	return out, nil
}

// SaveManufacturer creates (empty id) or changes an own manufacturer.
func (s *Service) SaveManufacturer(ctx context.Context, userID, workspaceID, id, name, website string) (Manufacturer, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return Manufacturer{}, err
	}
	if name, err = referenceName(name, maxNameRunes); err != nil {
		return Manufacturer{}, err
	}
	website = strings.TrimSpace(website)
	if website != "" && !strings.HasPrefix(website, "http://") && !strings.HasPrefix(website, "https://") {
		return Manufacturer{}, invalid("Сайт должен начинаться с http:// или https://.")
	}
	var row db.CraftManufacturer
	if id == "" {
		row, err = s.st.Queries().CreateCraftManufacturer(ctx, db.CreateCraftManufacturerParams{WorkspaceID: wid, Name: name, Website: website})
	} else {
		mid, parseErr := parseItem(id)
		if parseErr != nil {
			return Manufacturer{}, parseErr
		}
		row, err = s.st.Queries().UpdateCraftManufacturer(ctx, db.UpdateCraftManufacturerParams{ID: mid, WorkspaceID: wid, Name: name, Website: website})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Manufacturer{}, ErrNotFound
	}
	if err != nil {
		return Manufacturer{}, mapDuplicate(err, "Такой производитель уже есть.", "save manufacturer")
	}
	return Manufacturer{ID: workspace.FormatID(row.ID), Name: row.Name, Website: row.Website}, nil
}

// DeleteManufacturer removes an own manufacturer that no item uses.
func (s *Service) DeleteManufacturer(ctx context.Context, userID, workspaceID, id string) error {
	wid, mid, err := s.referenceTarget(ctx, userID, workspaceID, id)
	if err != nil {
		return err
	}
	return s.st.InTx(ctx, func(q *db.Queries) error {
		rows, err := q.DeleteCraftManufacturer(ctx, db.DeleteCraftManufacturerParams{ID: mid, WorkspaceID: wid})
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23503" {
			return invalid("Производитель указан у позиций. Сначала измените их.")
		}
		if err != nil {
			return fmt.Errorf("delete manufacturer: %w", err)
		}
		if rows == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// --- units ---------------------------------------------------------------------

// ListUnits lists the units of a warehouse.
func (s *Service) ListUnits(ctx context.Context, userID, workspaceID string) ([]Unit, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	rows, err := s.st.Queries().ListCraftUnits(ctx, wid)
	if err != nil {
		return nil, fmt.Errorf("list units: %w", err)
	}
	out := make([]Unit, 0, len(rows))
	for _, r := range rows {
		out = append(out, Unit{ID: workspace.FormatID(r.ID), Name: r.Name})
	}
	return out, nil
}

// SaveUnit creates a unit (empty id) or renames one. A rename follows into the
// own items and categories counted in it.
func (s *Service) SaveUnit(ctx context.Context, userID, workspaceID, id, name string) (Unit, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return Unit{}, err
	}
	if name, err = referenceName(name, maxUnitRunes); err != nil {
		return Unit{}, err
	}
	const duplicate = "Такая единица уже есть."
	if id == "" {
		row, err := s.st.Queries().CreateCraftUnit(ctx, db.CreateCraftUnitParams{WorkspaceID: wid, Name: name})
		if err != nil {
			return Unit{}, mapDuplicate(err, duplicate, "create unit")
		}
		return Unit{ID: workspace.FormatID(row.ID), Name: row.Name}, nil
	}
	uid, err := parseItem(id)
	if err != nil {
		return Unit{}, err
	}
	var renamed db.CraftUnit
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		current, err := q.GetCraftUnit(ctx, db.GetCraftUnitParams{ID: uid, WorkspaceID: wid})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get unit: %w", err)
		}
		if renamed, err = q.RenameCraftUnit(ctx, db.RenameCraftUnitParams{ID: uid, WorkspaceID: wid, Name: name}); err != nil {
			return mapDuplicate(err, duplicate, "rename unit")
		}
		if err := q.RenameCraftItemUnit(ctx, db.RenameCraftItemUnitParams{WorkspaceID: wid, OldName: current.Name, NewName: name}); err != nil {
			return fmt.Errorf("rename item units: %w", err)
		}
		if err := q.RenameCraftCategoryUnit(ctx, db.RenameCraftCategoryUnitParams{WorkspaceID: wid, OldName: current.Name, NewName: name}); err != nil {
			return fmt.Errorf("rename category units: %w", err)
		}
		return nil
	})
	if err != nil {
		return Unit{}, err
	}
	return Unit{ID: workspace.FormatID(renamed.ID), Name: renamed.Name}, nil
}

// DeleteUnit removes a unit that no own item and no category uses.
func (s *Service) DeleteUnit(ctx context.Context, userID, workspaceID, id string) error {
	wid, uid, err := s.referenceTarget(ctx, userID, workspaceID, id)
	if err != nil {
		return err
	}
	return s.st.InTx(ctx, func(q *db.Queries) error {
		unit, err := q.GetCraftUnit(ctx, db.GetCraftUnitParams{ID: uid, WorkspaceID: wid})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get unit: %w", err)
		}
		uses, err := q.CountCraftUnitUses(ctx, db.CountCraftUnitUsesParams{WorkspaceID: wid, Name: unit.Name})
		if err != nil {
			return fmt.Errorf("count unit uses: %w", err)
		}
		if uses > 0 {
			return invalid("Единица «%s» используется в категориях или позициях (%d).", unit.Name, uses)
		}
		if _, err := q.DeleteCraftUnit(ctx, db.DeleteCraftUnitParams{ID: uid, WorkspaceID: wid}); err != nil {
			return fmt.Errorf("delete unit: %w", err)
		}
		return nil
	})
}

// --- official catalogues -----------------------------------------------------

// CatalogSources lists the official catalogues and which are connected.
func (s *Service) CatalogSources(ctx context.Context, userID, workspaceID string) ([]CatalogSource, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	enabled, err := s.st.Queries().ListCraftSpaceCatalogs(ctx, wid)
	if err != nil {
		return nil, fmt.Errorf("list catalogues: %w", err)
	}
	counts, err := s.st.Queries().CountGlobalCraftItemsBySource(ctx)
	if err != nil {
		return nil, fmt.Errorf("count catalogue items: %w", err)
	}
	out := make([]CatalogSource, 0, len(officialSources))
	for _, official := range officialSources {
		source := CatalogSource{Source: official.source, Name: official.name, Enabled: slices.Contains(enabled, official.source)}
		for _, count := range counts {
			if count.Source == official.source {
				source.Items = int(count.Items)
			}
		}
		out = append(out, source)
	}
	return out, nil
}

// SetCatalogSources replaces the set of connected official catalogues.
// Connecting one adds the category and unit its items need.
func (s *Service) SetCatalogSources(ctx context.Context, userID, workspaceID string, sources []string) ([]CatalogSource, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		known := false
		for _, official := range officialSources {
			known = known || official.source == source
		}
		if !known {
			return nil, invalid("Неизвестный каталог «%s».", source)
		}
	}
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		before, err := q.ListCraftSpaceCatalogs(ctx, wid)
		if err != nil {
			return fmt.Errorf("list catalogues: %w", err)
		}
		if err := q.ClearCraftSpaceCatalogs(ctx, wid); err != nil {
			return fmt.Errorf("clear catalogues: %w", err)
		}
		for _, official := range officialSources {
			if !slices.Contains(sources, official.source) {
				continue
			}
			if err := q.AddCraftSpaceCatalog(ctx, db.AddCraftSpaceCatalogParams{WorkspaceID: wid, Source: official.source}); err != nil {
				return fmt.Errorf("add catalogue: %w", err)
			}
			if slices.Contains(before, official.source) {
				// Already connected: respect categories the user deleted.
				continue
			}
			for _, slug := range official.categories {
				reference, err := q.GetReferenceCraftCategoryBySlug(ctx, slug)
				if err != nil {
					return fmt.Errorf("reference category %s: %w", slug, err)
				}
				if err := q.EnsureCraftUnit(ctx, db.EnsureCraftUnitParams{WorkspaceID: wid, Name: reference.DefaultUnit}); err != nil {
					return fmt.Errorf("ensure unit: %w", err)
				}
				if err := q.LinkCraftCategoryToCatalog(ctx, db.LinkCraftCategoryToCatalogParams{WorkspaceID: wid, Slug: slug}); err != nil {
					return fmt.Errorf("link category: %w", err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.CatalogSources(ctx, userID, workspaceID)
}

// --- helpers -------------------------------------------------------------------

func (s *Service) referenceTarget(ctx context.Context, userID, workspaceID, id string) (pgtype.UUID, pgtype.UUID, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	target, err := parseItem(id)
	return wid, target, err
}

// unitName returns the stored spelling of a unit of the warehouse.
func (s *Service) unitName(ctx context.Context, q *db.Queries, wid pgtype.UUID, name string) (string, error) {
	row, err := q.FindCraftUnit(ctx, db.FindCraftUnitParams{WorkspaceID: wid, Name: strings.TrimSpace(name)})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", invalid("Неизвестная единица «%s». Добавьте её в настройках склада.", name)
	}
	if err != nil {
		return "", fmt.Errorf("find unit: %w", err)
	}
	return row.Name, nil
}

func referenceName(name string, limit int) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", invalid("Название не может быть пустым.")
	}
	if utf8.RuneCountInString(name) > limit {
		return "", invalid("Название длиннее %d символов.", limit)
	}
	return name, nil
}

func randomKey(prefix string) (string, error) {
	var raw [5]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}
	return prefix + hex.EncodeToString(raw[:]), nil
}

// normalizeFields validates the extra fields of a category.
func normalizeFields(fields []Field) ([]Field, error) {
	if len(fields) > maxFields {
		return nil, invalid("Не больше %d полей в категории.", maxFields)
	}
	out := make([]Field, 0, len(fields))
	keys, labels := map[string]bool{}, map[string]bool{}
	for _, field := range fields {
		label, err := referenceName(field.Label, maxCategoryRunes)
		if err != nil {
			return nil, invalid("У поля должно быть название.")
		}
		if labels[strings.ToLower(label)] {
			return nil, invalid("Поле «%s» указано дважды.", label)
		}
		labels[strings.ToLower(label)] = true
		normalized := Field{Key: strings.TrimSpace(field.Key), Label: label, Type: field.Type}
		if normalized.Key == "" {
			if normalized.Key, err = randomKey("f-"); err != nil {
				return nil, err
			}
		}
		if keys[normalized.Key] {
			return nil, invalid("Ключ поля «%s» повторяется.", normalized.Key)
		}
		keys[normalized.Key] = true
		switch field.Type {
		case FieldText, FieldNumber, FieldBool:
		case FieldSelect:
			for _, option := range field.Options {
				if option = strings.TrimSpace(option); option != "" && !slices.Contains(normalized.Options, option) {
					normalized.Options = append(normalized.Options, option)
				}
			}
			if len(normalized.Options) == 0 || len(normalized.Options) > maxOptions {
				return nil, invalid("У списка «%s» должно быть от 1 до %d вариантов.", label, maxOptions)
			}
		default:
			return nil, invalid("Тип поля: text, number, bool или select.")
		}
		out = append(out, normalized)
	}
	return out, nil
}

// checkAttrs validates the values of the category's fields. Values under other
// keys are kept as they are: they come from imports or from removed fields.
func checkAttrs(fields []Field, attrs map[string]any) error {
	for _, field := range fields {
		value, ok := attrs[field.Key]
		if !ok || value == nil || value == "" {
			continue
		}
		switch field.Type {
		case FieldNumber:
			if _, ok := value.(float64); !ok {
				return invalid("Поле «%s» должно быть числом.", field.Label)
			}
		case FieldBool:
			if _, ok := value.(bool); !ok {
				return invalid("Поле «%s» должно быть да или нет.", field.Label)
			}
		case FieldSelect:
			text, ok := value.(string)
			if !ok || !slices.Contains(field.Options, text) {
				return invalid("Поле «%s»: выберите один из вариантов: %s.", field.Label, strings.Join(field.Options, ", "))
			}
		default:
			if _, ok := value.(string); !ok {
				return invalid("Поле «%s» должно быть строкой.", field.Label)
			}
		}
	}
	return nil
}

func toCategory(r db.CraftCategory) Category {
	fields := []Field{}
	_ = json.Unmarshal(r.Fields, &fields)
	return Category{
		ID: workspace.FormatID(r.ID), Slug: r.Slug, Name: r.Name,
		DefaultUnit: r.DefaultUnit, Fields: fields, CatalogSlug: r.CatalogSlug,
	}
}

func mapDuplicate(err error, message, what string) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return invalid("%s", message)
	}
	return fmt.Errorf("%s: %w", what, err)
}
