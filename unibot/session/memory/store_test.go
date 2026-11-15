package memory

import (
	"context"
	"testing"
)

type state struct{ Count int }

func TestStore(t *testing.T) {
	ctx := context.Background()
	s := New[state]()
	if _, ok, err := s.Load(ctx, "principal"); err != nil || ok {
		t.Fatalf("initial Load: ok=%v err=%v", ok, err)
	}
	if err := s.Save(ctx, "principal", state{Count: 2}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Load(ctx, "principal")
	if err != nil || !ok || got.Count != 2 {
		t.Fatalf("Load: got=%+v ok=%v err=%v", got, ok, err)
	}
}
