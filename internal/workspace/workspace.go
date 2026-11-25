// Package workspace implements shared spaces: a named space of one mini-app
// whose members all have full, equal access to its data. A user can belong to
// several spaces, and sharing a space of one mini-app shares nothing else. There are no roles and no invitation links; a member is added by
// picking a user who has already signed in at least once.
//
// Features that keep data per workspace call Service.Require to authorise a
// request: it answers ErrNotFound both for an unknown workspace and for one the
// user does not belong to, so the existence of a foreign workspace is not
// revealed.
package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/store"
	"github.com/maksimkurb/cublyverse/internal/store/db"
)

// MaxNameRunes bounds a workspace name.
const MaxNameRunes = 80

// Mini-apps that keep their data in spaces.
const (
	AppWarehouse = "warehouse"
	AppReminders = "reminders"
)

// PersonalName is the name of the space created for a user who has none.
const PersonalName = "Личное"

// ValidApp reports whether app names a mini-app with spaces.
func ValidApp(app string) bool { return app == AppWarehouse || app == AppReminders }

var (
	// ErrNotFound means the workspace does not exist or the user is not a
	// member of it.
	ErrNotFound = errors.New("workspace: not found")
	// ErrUserNotFound means the user to add does not exist.
	ErrUserNotFound = errors.New("workspace: user not found")
	// ErrLastMember means the operation would leave a workspace without
	// members.
	ErrLastMember = errors.New("workspace: cannot remove the last member")
)

// ValidationError is a client mistake with a message fit for the user.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

// Workspace is a shared space.
type Workspace struct {
	ID   string
	Name string
	// App is the mini-app the space belongs to.
	App string
	// Personal marks the creator's own default space of the mini-app.
	Personal  bool
	CreatedAt time.Time
}

// Member is a user in a workspace.
type Member struct {
	UserID      string
	DisplayName string
	Email       string
	// AvatarURL is the Gravatar image of the e-mail; it falls back to a
	// generated identicon when the address has no Gravatar.
	AvatarURL string
	JoinedAt  time.Time
}

// Candidate is a user who can be added to a workspace.
type Candidate struct {
	UserID      string
	DisplayName string
	Email       string
	AvatarURL   string
}

// Service is the workspace business logic.
type Service struct {
	st *store.Store
}

// New returns the service.
func New(st *store.Store) *Service { return &Service{st: st} }

// Create makes a space of a mini-app owned by the user, who becomes its first
// member. The first space a user creates for a mini-app is their personal one.
func (s *Service) Create(ctx context.Context, userID, app, name string) (Workspace, error) {
	uid, err := ParseID(userID)
	if err != nil {
		return Workspace{}, err
	}
	if !ValidApp(app) {
		return Workspace{}, &ValidationError{Message: "Неизвестный мини-апп."}
	}
	name, err = canonicalName(name)
	if err != nil {
		return Workspace{}, err
	}
	var created db.Workspace
	err = s.st.InTx(ctx, func(q *db.Queries) error {
		var err error
		created, err = CreateTx(ctx, q, uid, app, name)
		return err
	})
	if err != nil {
		return Workspace{}, err
	}
	return toWorkspace(created), nil
}

// CreateTx is Create over an open transaction, so a mini-app can fill the new
// space atomically. The name must already be valid.
func CreateTx(ctx context.Context, q *db.Queries, userID pgtype.UUID, app, name string) (db.Workspace, error) {
	has, err := q.HasPersonalWorkspace(ctx, db.HasPersonalWorkspaceParams{CreatedBy: userID, App: app})
	if err != nil {
		return db.Workspace{}, fmt.Errorf("check personal workspace: %w", err)
	}
	created, err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{Name: name, CreatedBy: userID, App: app, Personal: !has})
	if err != nil {
		return db.Workspace{}, fmt.Errorf("create workspace: %w", err)
	}
	if err := q.AddWorkspaceMember(ctx, db.AddWorkspaceMemberParams{WorkspaceID: created.ID, UserID: userID, AddedBy: userID}); err != nil {
		return db.Workspace{}, fmt.Errorf("add owner: %w", err)
	}
	return created, nil
}

// DefaultTx returns the space of the mini-app things go to when none is named:
// the user's personal space, else the oldest one they created. Without either a
// «Личное» space is created.
func DefaultTx(ctx context.Context, q *db.Queries, userID pgtype.UUID, app string) (db.Workspace, error) {
	params := db.GetDefaultWorkspaceParams{UserID: userID, App: app}
	row, err := q.GetDefaultWorkspace(ctx, params)
	if !errors.Is(err, pgx.ErrNoRows) {
		return row, err
	}
	// A personal space the user left must give up the flag, or it would block
	// the new one.
	if err := q.ClearPersonalWorkspace(ctx, db.ClearPersonalWorkspaceParams{CreatedBy: userID, App: app}); err != nil {
		return db.Workspace{}, fmt.Errorf("clear personal workspace: %w", err)
	}
	row, err = q.CreatePersonalWorkspace(ctx, db.CreatePersonalWorkspaceParams{Name: PersonalName, CreatedBy: userID, App: app})
	if errors.Is(err, pgx.ErrNoRows) {
		return q.GetDefaultWorkspace(ctx, params)
	}
	if err != nil {
		return db.Workspace{}, fmt.Errorf("create personal workspace: %w", err)
	}
	if err := q.AddWorkspaceMember(ctx, db.AddWorkspaceMemberParams{WorkspaceID: row.ID, UserID: userID, AddedBy: userID}); err != nil {
		return db.Workspace{}, fmt.Errorf("add owner: %w", err)
	}
	return row, nil
}

// List returns the spaces of the user, of one mini-app or of all of them when
// app is empty.
func (s *Service) List(ctx context.Context, userID, app string) ([]Workspace, error) {
	uid, err := ParseID(userID)
	if err != nil {
		return nil, err
	}
	rows, err := s.st.Queries().ListWorkspacesForUser(ctx, db.ListWorkspacesForUserParams{UserID: uid, App: app})
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	out := make([]Workspace, 0, len(rows))
	for _, row := range rows {
		out = append(out, toWorkspace(row))
	}
	return out, nil
}

// RequireApp is Require that also demands a space of the given mini-app: a
// reminders space is not a warehouse.
func (s *Service) RequireApp(ctx context.Context, userID, workspaceID, app string) (Workspace, error) {
	ws, err := s.Require(ctx, userID, workspaceID)
	if err != nil {
		return Workspace{}, err
	}
	if ws.App != app {
		return Workspace{}, ErrNotFound
	}
	return ws, nil
}

// Require returns the workspace when the user is a member of it, ErrNotFound
// otherwise. It is the single authorisation check of workspace data.
func (s *Service) Require(ctx context.Context, userID, workspaceID string) (Workspace, error) {
	uid, err := ParseID(userID)
	if err != nil {
		return Workspace{}, err
	}
	wid, err := ParseID(workspaceID)
	if err != nil {
		return Workspace{}, ErrNotFound
	}
	row, err := s.st.Queries().GetWorkspaceForUser(ctx, db.GetWorkspaceForUserParams{ID: wid, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, fmt.Errorf("get workspace: %w", err)
	}
	return toWorkspace(row), nil
}

// Rename changes the workspace name.
func (s *Service) Rename(ctx context.Context, userID, workspaceID, name string) (Workspace, error) {
	if _, err := s.Require(ctx, userID, workspaceID); err != nil {
		return Workspace{}, err
	}
	name, err := canonicalName(name)
	if err != nil {
		return Workspace{}, err
	}
	wid, _ := ParseID(workspaceID)
	row, err := s.st.Queries().RenameWorkspace(ctx, db.RenameWorkspaceParams{ID: wid, Name: name})
	if err != nil {
		return Workspace{}, fmt.Errorf("rename workspace: %w", err)
	}
	return toWorkspace(row), nil
}

// Delete removes the workspace with everything in it. Any member may do it:
// membership is full access.
func (s *Service) Delete(ctx context.Context, userID, workspaceID string) error {
	if _, err := s.Require(ctx, userID, workspaceID); err != nil {
		return err
	}
	wid, _ := ParseID(workspaceID)
	if _, err := s.st.Queries().DeleteWorkspace(ctx, wid); err != nil {
		return fmt.Errorf("delete workspace: %w", err)
	}
	return nil
}

// Members lists the members of a workspace the caller belongs to.
func (s *Service) Members(ctx context.Context, userID, workspaceID string) ([]Member, error) {
	if _, err := s.Require(ctx, userID, workspaceID); err != nil {
		return nil, err
	}
	wid, _ := ParseID(workspaceID)
	rows, err := s.st.Queries().ListWorkspaceMembers(ctx, wid)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	out := make([]Member, 0, len(rows))
	for _, row := range rows {
		email := deref(row.Email)
		out = append(out, Member{
			UserID:      FormatID(row.ID),
			DisplayName: deref(row.DisplayName),
			Email:       email,
			AvatarURL:   GravatarURL(email, 96),
			JoinedAt:    row.JoinedAt,
		})
	}
	return out, nil
}

// Candidates lists users who can be added: everyone who has signed in at least
// once and is not a member yet. query filters by name or e-mail.
func (s *Service) Candidates(ctx context.Context, userID, workspaceID, query string) ([]Candidate, error) {
	if _, err := s.Require(ctx, userID, workspaceID); err != nil {
		return nil, err
	}
	wid, _ := ParseID(workspaceID)
	rows, err := s.st.Queries().ListUsersNotInWorkspace(ctx, db.ListUsersNotInWorkspaceParams{
		WorkspaceID: wid, Column2: strings.TrimSpace(query),
	})
	if err != nil {
		return nil, fmt.Errorf("list candidates: %w", err)
	}
	out := make([]Candidate, 0, len(rows))
	for _, row := range rows {
		email := deref(row.Email)
		out = append(out, Candidate{
			UserID:      FormatID(row.ID),
			DisplayName: deref(row.DisplayName),
			Email:       email,
			AvatarURL:   GravatarURL(email, 96),
		})
	}
	return out, nil
}

// AddMember adds an existing user to the workspace. Adding a member twice is
// not an error.
func (s *Service) AddMember(ctx context.Context, userID, workspaceID, newUserID string) error {
	if _, err := s.Require(ctx, userID, workspaceID); err != nil {
		return err
	}
	wid, _ := ParseID(workspaceID)
	nid, err := ParseID(newUserID)
	if err != nil {
		return ErrUserNotFound
	}
	actor, _ := ParseID(userID)
	if _, err := s.st.Queries().GetUserByID(ctx, nid); errors.Is(err, pgx.ErrNoRows) {
		return ErrUserNotFound
	} else if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	return s.st.Queries().AddWorkspaceMember(ctx, db.AddWorkspaceMemberParams{
		WorkspaceID: wid, UserID: nid, AddedBy: actor,
	})
}

// RemoveMember removes a member; a user may remove themselves (leave). The last
// member cannot be removed: delete the workspace instead.
func (s *Service) RemoveMember(ctx context.Context, userID, workspaceID, memberID string) error {
	if _, err := s.Require(ctx, userID, workspaceID); err != nil {
		return err
	}
	wid, _ := ParseID(workspaceID)
	mid, err := ParseID(memberID)
	if err != nil {
		return ErrUserNotFound
	}
	return s.st.InTx(ctx, func(q *db.Queries) error {
		n, err := q.CountWorkspaceMembers(ctx, wid)
		if err != nil {
			return fmt.Errorf("count members: %w", err)
		}
		if n <= 1 {
			return ErrLastMember
		}
		rows, err := q.RemoveWorkspaceMember(ctx, db.RemoveWorkspaceMemberParams{WorkspaceID: wid, UserID: mid})
		if err != nil {
			return fmt.Errorf("remove member: %w", err)
		}
		if rows == 0 {
			return ErrUserNotFound
		}
		return nil
	})
}

// GravatarURL returns the Gravatar image URL for an e-mail. Gravatar accepts
// the sha256 of the trimmed, lower-cased address; d=identicon gives addresses
// without a Gravatar a stable generated picture, and an empty e-mail still
// yields a valid URL.
func GravatarURL(email string, size int) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return fmt.Sprintf("https://www.gravatar.com/avatar/%s?s=%d&d=identicon", hex.EncodeToString(sum[:]), size)
}

// ParseID parses a UUID string.
func ParseID(value string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(value); err != nil || !u.Valid {
		return pgtype.UUID{}, fmt.Errorf("invalid id %q", value)
	}
	return u, nil
}

// FormatID renders a UUID in the canonical form.
func FormatID(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func canonicalName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", &ValidationError{Message: "Название не может быть пустым."}
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return "", &ValidationError{Message: fmt.Sprintf("Название длиннее %d символов.", MaxNameRunes)}
	}
	return name, nil
}

func toWorkspace(row db.Workspace) Workspace {
	return Workspace{ID: FormatID(row.ID), Name: row.Name, App: row.App, Personal: row.Personal, CreatedAt: row.CreatedAt}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
