package memory

import (
	"context"
	"sync"

	unibot "github.com/maksimkurb/cublyverse/unibot"
)

// Store is a demo-only in-memory session store. Values are copied by assignment;
// prefer immutable/value session types or provide a persistent store in production.
type Store[T any] struct {
	mu    sync.RWMutex
	items map[unibot.PrincipalID]T
}

func New[T any]() *Store[T] {
	return &Store[T]{items: make(map[unibot.PrincipalID]T)}
}

func (s *Store[T]) Load(_ context.Context, principal unibot.PrincipalID) (T, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.items[principal]
	return value, ok, nil
}

func (s *Store[T]) Save(_ context.Context, principal unibot.PrincipalID, value T) error {
	s.mu.Lock()
	s.items[principal] = value
	s.mu.Unlock()
	return nil
}

func (s *Store[T]) Delete(_ context.Context, principal unibot.PrincipalID) error {
	s.mu.Lock()
	delete(s.items, principal)
	s.mu.Unlock()
	return nil
}
