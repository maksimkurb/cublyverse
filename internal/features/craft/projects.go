package craft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// ProjectInput is the name and notes of a craft project.
type ProjectInput struct {
	Name  string
	Notes string
}

// ProjectMaterial is one material planned for a project together with the
// current stock and the shortage.
//
// Shortage is max(0, qty_planned - qty_used - stock): the quantity still to
// buy for the project. A material without a plan has no shortage.
type ProjectMaterial struct {
	Item       Item
	QtyPlanned *float64
	QtyUsed    float64
	Shortage   float64
}

// Project is a craft project and its materials.
type Project struct {
	ID            string
	Name          string
	Notes         string
	CreatedAt     time.Time
	MaterialCount int
	Materials     []ProjectMaterial
}

// CreateProject creates an empty project.
func (s *Service) CreateProject(ctx context.Context, userID, workspaceID string, in ProjectInput) (Project, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return Project{}, err
	}
	name, notes, err := projectData(in)
	if err != nil {
		return Project{}, err
	}
	var created db.CraftProject
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		var err error
		created, err = q.CreateCraftProject(ctx, db.CreateCraftProjectParams{
			WorkspaceID: wid, Name: name, Notes: notes,
		})
		if err != nil {
			return fmt.Errorf("create project: %w", err)
		}
		return nil
	})
	if err != nil {
		return Project{}, err
	}
	return s.loadProject(ctx, s.st.Queries(), wid, created.ID)
}

// ListProjects lists the projects of the workspace.
func (s *Service) ListProjects(ctx context.Context, userID, workspaceID string) ([]Project, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	rows, err := s.st.Queries().ListCraftProjects(ctx, wid)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	out := make([]Project, 0, len(rows))
	for _, row := range rows {
		out = append(out, Project{
			ID: workspace.FormatID(row.ID), Name: row.Name, Notes: row.Notes,
			CreatedAt: row.CreatedAt, MaterialCount: int(row.MaterialCount),
		})
	}
	return out, nil
}

// GetProject returns one project with its materials.
func (s *Service) GetProject(ctx context.Context, userID, workspaceID, projectID string) (Project, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return Project{}, err
	}
	pid, err := parseProject(projectID)
	if err != nil {
		return Project{}, err
	}
	return s.loadProject(ctx, s.st.Queries(), wid, pid)
}

// UpdateProject replaces the name and notes of a project.
func (s *Service) UpdateProject(ctx context.Context, userID, workspaceID, projectID string, in ProjectInput) (Project, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return Project{}, err
	}
	pid, err := parseProject(projectID)
	if err != nil {
		return Project{}, err
	}
	name, notes, err := projectData(in)
	if err != nil {
		return Project{}, err
	}
	row, err := s.st.Queries().UpdateCraftProject(ctx, db.UpdateCraftProjectParams{
		ID: pid, WorkspaceID: wid, Name: name, Notes: notes,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("update project: %w", err)
	}
	return s.loadProject(ctx, s.st.Queries(), wid, row.ID)
}

// DeleteProject removes a project with its material plan. Stock does not
// change; the inventory log rows keep their reason.
func (s *Service) DeleteProject(ctx context.Context, userID, workspaceID, projectID string) error {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return err
	}
	pid, err := parseProject(projectID)
	if err != nil {
		return err
	}
	rows, err := s.st.Queries().DeleteCraftProject(ctx, db.DeleteCraftProjectParams{ID: pid, WorkspaceID: wid})
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// SetProjectMaterial sets the planned quantity of a material. A nil planned
// quantity clears the plan. The material must be visible to the workspace.
func (s *Service) SetProjectMaterial(ctx context.Context, userID, workspaceID, projectID, itemID string, qtyPlanned *float64) (Project, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return Project{}, err
	}
	pid, err := parseProject(projectID)
	if err != nil {
		return Project{}, err
	}
	iid, err := parseItem(itemID)
	if err != nil {
		return Project{}, err
	}
	if qtyPlanned != nil && (*qtyPlanned < 0 || math.IsNaN(*qtyPlanned) || math.IsInf(*qtyPlanned, 0)) {
		return Project{}, invalid("Плановое количество не может быть отрицательным.")
	}
	var planned pgtype.Numeric
	if qtyPlanned != nil {
		planned = numeric(*qtyPlanned)
	}
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		if _, err := s.getProjectRow(ctx, q, wid, pid); err != nil {
			return err
		}
		if _, err := s.visible(ctx, q, wid, iid); err != nil {
			return err
		}
		if _, err := q.UpsertCraftProjectMaterial(ctx, db.UpsertCraftProjectMaterialParams{
			ProjectID: pid, CatalogItemID: iid, QtyPlanned: planned,
		}); err != nil {
			return fmt.Errorf("set project material: %w", err)
		}
		return nil
	})
	if err != nil {
		return Project{}, err
	}
	return s.loadProject(ctx, s.st.Queries(), wid, pid)
}

// RemoveProjectMaterial drops a material from the project plan.
func (s *Service) RemoveProjectMaterial(ctx context.Context, userID, workspaceID, projectID, itemID string) error {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return err
	}
	pid, err := parseProject(projectID)
	if err != nil {
		return err
	}
	iid, err := parseItem(itemID)
	if err != nil {
		return err
	}
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		if _, err := s.getProjectRow(ctx, q, wid, pid); err != nil {
			return err
		}
		rows, err := q.DeleteCraftProjectMaterial(ctx, db.DeleteCraftProjectMaterialParams{
			ProjectID: pid, CatalogItemID: iid,
		})
		if err != nil {
			return fmt.Errorf("remove project material: %w", err)
		}
		if rows == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// UseMaterial withdraws qty of a material from the stock and records it as used
// by the project. Both writes happen in one transaction: when the stock is
// insufficient nothing changes (ErrInsufficientStock).
func (s *Service) UseMaterial(ctx context.Context, userID, workspaceID, projectID, itemID string, qty float64) (Project, error) {
	if qty <= 0 || math.IsNaN(qty) || math.IsInf(qty, 0) {
		return Project{}, invalid("Количество должно быть положительным.")
	}
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return Project{}, err
	}
	pid, err := parseProject(projectID)
	if err != nil {
		return Project{}, err
	}
	iid, err := parseItem(itemID)
	if err != nil {
		return Project{}, err
	}
	actor, _ := workspace.ParseID(userID)
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		project, err := s.getProjectRow(ctx, q, wid, pid)
		if err != nil {
			return err
		}
		if _, err := q.GetCraftProjectMaterial(ctx, db.GetCraftProjectMaterialParams{
			ProjectID: pid, CatalogItemID: iid,
		}); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return fmt.Errorf("get project material: %w", err)
		}
		if _, err := s.adjustInTx(ctx, q, wid, iid, actor, pid, -qty, "проект: "+project.Name); err != nil {
			return err
		}
		if err := q.IncrementCraftProjectMaterialUsed(ctx, db.IncrementCraftProjectMaterialUsedParams{
			ProjectID: pid, CatalogItemID: iid, QtyUsed: numeric(qty),
		}); err != nil {
			return fmt.Errorf("increment used material: %w", err)
		}
		return nil
	})
	if err != nil {
		return Project{}, err
	}
	return s.loadProject(ctx, s.st.Queries(), wid, pid)
}

// ShortageToWishlist adds every material of the project that has a shortage to
// the wish list. Materials already wanted are skipped, so a repeated call adds
// nothing. The planned shortage becomes the wanted quantity.
func (s *Service) ShortageToWishlist(ctx context.Context, userID, workspaceID, projectID string) ([]Wish, error) {
	wid, err := s.authorize(ctx, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	pid, err := parseProject(projectID)
	if err != nil {
		return nil, err
	}
	actor, _ := workspace.ParseID(userID)
	var added []Wish
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		project, err := s.loadProject(ctx, q, wid, pid)
		if err != nil {
			return err
		}
		for _, material := range project.Materials {
			if material.Shortage <= 0 {
				continue
			}
			iid, err := parseItem(material.Item.ID)
			if err != nil {
				return err
			}
			_, err = q.GetWantedCraftWishlistItem(ctx, db.GetWantedCraftWishlistItemParams{
				WorkspaceID: wid, CatalogItemID: iid,
			})
			if err == nil {
				continue
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("get wanted item: %w", err)
			}
			created, err := q.CreateCraftWishlistItem(ctx, db.CreateCraftWishlistItemParams{
				WorkspaceID: wid, CatalogItemID: iid, Title: material.Item.Name,
				QtyWanted: numeric(material.Shortage), Priority: 0,
				Note: "проект: " + project.Name, CreatedBy: actor,
			})
			if err != nil {
				return mapWishUnique(err)
			}
			wish, err := s.loadWish(ctx, q, wid, workspace.FormatID(created.ID))
			if err != nil {
				return err
			}
			added = append(added, wish)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return added, nil
}

// --- internals -------------------------------------------------------------

func parseProject(id string) (pgtype.UUID, error) {
	u, err := workspace.ParseID(id)
	if err != nil {
		return pgtype.UUID{}, ErrNotFound
	}
	return u, nil
}

func projectData(in ProjectInput) (string, string, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", "", invalid("Укажите название проекта.")
	}
	if utf8.RuneCountInString(name) > 200 {
		return "", "", invalid("Название длиннее 200 символов.")
	}
	return name, strings.TrimSpace(in.Notes), nil
}

func (s *Service) getProjectRow(ctx context.Context, q *db.Queries, wid, pid pgtype.UUID) (db.CraftProject, error) {
	row, err := q.GetCraftProject(ctx, db.GetCraftProjectParams{ID: pid, WorkspaceID: wid})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.CraftProject{}, ErrNotFound
	}
	if err != nil {
		return db.CraftProject{}, fmt.Errorf("get project: %w", err)
	}
	return row, nil
}

func (s *Service) loadProject(ctx context.Context, q *db.Queries, wid, pid pgtype.UUID) (Project, error) {
	row, err := s.getProjectRow(ctx, q, wid, pid)
	if err != nil {
		return Project{}, err
	}
	rows, err := q.ListCraftProjectMaterials(ctx, db.ListCraftProjectMaterialsParams{
		WorkspaceID: wid, ProjectID: pid,
	})
	if err != nil {
		return Project{}, fmt.Errorf("list project materials: %w", err)
	}
	project := Project{
		ID: workspace.FormatID(row.ID), Name: row.Name, Notes: row.Notes,
		CreatedAt: row.CreatedAt, MaterialCount: len(rows),
		Materials: make([]ProjectMaterial, 0, len(rows)),
	}
	for _, material := range rows {
		project.Materials = append(project.Materials, projectMaterialFromRow(material))
	}
	return project, nil
}

func projectMaterialFromRow(r db.ListCraftProjectMaterialsRow) ProjectMaterial {
	attrs := map[string]any{}
	_ = json.Unmarshal(r.Attrs, &attrs)
	item := Item{
		ID: workspace.FormatID(r.ID), WorkspaceID: workspace.FormatID(r.WorkspaceID),
		Category: r.CategorySlug, CategoryName: r.CategoryName, Manufacturer: deref(r.ManufacturerName),
		Article: r.Article, Series: r.Series, Name: r.Name, ColorName: r.ColorName, ColorHex: r.ColorHex,
		ColorSource: r.ColorSource, Size: r.Size, Finish: r.Finish, Unit: r.Unit,
		PackSize: numFloatPtr(r.PackSize), Attrs: attrs, Source: r.Source,
		SourceURL: r.SourceUrl, ImageURL: r.ImageUrl,
		StockQty: numFloat(r.StockQty), InInventory: r.InInventory,
	}
	item = withStockColors(item, r.StockAttrs)
	used := numFloat(r.QtyUsed)
	material := ProjectMaterial{
		Item: item, QtyPlanned: numFloatPtr(r.QtyPlanned), QtyUsed: used,
	}
	if material.QtyPlanned != nil {
		shortage := *material.QtyPlanned - used - item.StockQty
		if shortage > 0 {
			material.Shortage = shortage
		}
	}
	return material
}
