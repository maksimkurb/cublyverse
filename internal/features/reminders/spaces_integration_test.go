package reminders_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/maksimkurb/cublyverse/internal/features/reminders"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

// TestSharedReminders covers reminders in a shared space: who sees them, who
// gets the message, who may confirm it and what happens when people leave.
func TestSharedReminders(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	author, partner, stranger := env.createUser("author"), env.createUser("partner"), env.createUser("stranger")
	spaces := workspace.New(env.store)

	rule := reminders.Input{
		Message: "Полить цветы", Timezone: "UTC", Kind: reminders.Daily,
		TimeHHMM: "12:00", RepeatMinutes: 10, Enabled: true,
	}

	// Without a space the reminder lands in a personal space created on demand,
	// and a second one reuses it.
	private, err := env.service.Create(ctx, author, rule)
	if err != nil {
		t.Fatalf("create private: %v", err)
	}
	again, err := env.service.Create(ctx, author, rule)
	if err != nil || again.SpaceID != private.SpaceID {
		t.Fatalf("second reminder space = %q, want %q (%v)", again.SpaceID, private.SpaceID, err)
	}
	if !slices.Equal(private.Recipients, []string{author}) {
		t.Fatalf("default recipients = %v", private.Recipients)
	}

	family, err := spaces.Create(ctx, author, workspace.AppReminders, "Семья")
	if err != nil {
		t.Fatal(err)
	}
	if err := spaces.AddMember(ctx, author, family.ID, partner); err != nil {
		t.Fatal(err)
	}

	shared := rule
	shared.Message = "Забрать посылку"
	shared.SpaceID = family.ID
	shared.Recipients = []string{author, stranger}
	var invalid *reminders.ValidationError
	if _, err := env.service.Create(ctx, author, shared); !errors.As(err, &invalid) {
		t.Fatalf("a non-member recipient must be refused, got %v", err)
	}
	shared.Recipients = []string{author, partner}
	created := env.dueReminder(author, shared)

	// Sharing one space does not share the others.
	visible, err := env.service.List(ctx, partner)
	if err != nil || len(visible) != 1 || visible[0].ID != created.ID {
		t.Fatalf("partner sees %v (%v)", visible, err)
	}
	if _, err := env.service.Get(ctx, partner, private.ID); !errors.Is(err, reminders.ErrNotFound) {
		t.Fatalf("partner read a private reminder: %v", err)
	}
	if _, err := env.service.Get(ctx, stranger, created.ID); !errors.Is(err, reminders.ErrNotFound) {
		t.Fatalf("stranger read a shared reminder: %v", err)
	}
	if _, err := env.service.Create(ctx, stranger, shared); !errors.Is(err, reminders.ErrSpaceNotFound) {
		t.Fatalf("stranger wrote into the space: %v", err)
	}
	warehouse, err := spaces.Create(ctx, author, workspace.AppWarehouse, "Склад")
	if err != nil {
		t.Fatal(err)
	}
	wrong := rule
	wrong.SpaceID = warehouse.ID
	if _, err := env.service.Create(ctx, author, wrong); !errors.Is(err, reminders.ErrSpaceNotFound) {
		t.Fatalf("a warehouse must not hold reminders: %v", err)
	}

	if err := env.scheduler().Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	var got []string
	for _, sent := range env.delivery.sent {
		if strings.Contains(sent.message.Text, shared.Message) {
			got = append(got, sent.principalID)
		}
	}
	slices.Sort(got)
	want := []string{author, partner}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("delivered to %v, want %v", got, want)
	}

	// Any member confirms; both delivered messages are rewritten.
	if _, err := env.service.Complete(ctx, partner, created.ID); err != nil {
		t.Fatalf("partner completes: %v", err)
	}
	if len(env.delivery.edits) != 1 || len(env.delivery.edits[0].sent) != 2 {
		t.Fatalf("edits = %+v", env.delivery.edits)
	}

	// Keeping the recipients while moving to a private space drops the others.
	moved := rule
	moved.SpaceID = private.SpaceID
	updated, err := env.service.Update(ctx, author, created.ID, moved)
	if err != nil || !slices.Equal(updated.Recipients, []string{author}) {
		t.Fatalf("moved recipients = %v (%v)", updated.Recipients, err)
	}
	if _, err := env.service.Get(ctx, partner, created.ID); !errors.Is(err, reminders.ErrNotFound) {
		t.Fatalf("partner still sees a moved reminder: %v", err)
	}
}

// TestRecipientsWhoLeft proves a shared reminder does not go silent when its
// only recipient leaves the space.
func TestRecipientsWhoLeft(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	author, partner := env.createUser("stays"), env.createUser("leaves")
	spaces := workspace.New(env.store)
	family, err := spaces.Create(ctx, author, workspace.AppReminders, "Семья")
	if err != nil {
		t.Fatal(err)
	}
	if err := spaces.AddMember(ctx, author, family.ID, partner); err != nil {
		t.Fatal(err)
	}
	env.dueReminder(author, reminders.Input{
		SpaceID: family.ID, Recipients: []string{partner}, Message: "Мусор", Timezone: "UTC",
		Kind: reminders.Daily, TimeHHMM: "12:00", RepeatMinutes: 10, Enabled: true,
	})
	if err := spaces.RemoveMember(ctx, author, family.ID, partner); err != nil {
		t.Fatal(err)
	}
	if err := env.scheduler().Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(env.delivery.sent) != 1 || env.delivery.sent[0].principalID != author {
		t.Fatalf("delivered = %+v, want only the remaining member", env.delivery.sent)
	}
}
