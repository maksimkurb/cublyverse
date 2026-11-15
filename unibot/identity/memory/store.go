package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	unibot "github.com/maksimkurb/cublyverse/unibot"
)

type Store struct {
	mu          sync.RWMutex
	byRemote    map[string]unibot.PrincipalID
	byPrincipal map[unibot.PrincipalID]map[string]unibot.Binding
}

func New() *Store {
	return &Store{
		byRemote:    make(map[string]unibot.PrincipalID),
		byPrincipal: make(map[unibot.PrincipalID]map[string]unibot.Binding),
	}
}

func bindingKey(platform unibot.Platform, remoteUserID string) string {
	return string(platform) + "\x00" + remoteUserID
}

func (s *Store) Bind(_ context.Context, binding unibot.Binding) error {
	if binding.PrincipalID == "" || binding.Platform == "" || binding.RemoteUserID == "" {
		return fmt.Errorf("principal, platform and remote user id are required")
	}
	if binding.VerifiedAt.IsZero() {
		binding.VerifiedAt = time.Now()
	}
	key := bindingKey(binding.Platform, binding.RemoteUserID)

	s.mu.Lock()
	defer s.mu.Unlock()

	if oldPrincipal, ok := s.byRemote[key]; ok && oldPrincipal != binding.PrincipalID {
		delete(s.byPrincipal[oldPrincipal], key)
	}
	s.byRemote[key] = binding.PrincipalID
	if s.byPrincipal[binding.PrincipalID] == nil {
		s.byPrincipal[binding.PrincipalID] = make(map[string]unibot.Binding)
	}
	s.byPrincipal[binding.PrincipalID][key] = binding
	return nil
}

func (s *Store) Resolve(_ context.Context, platform unibot.Platform, remoteUserID string) (unibot.PrincipalID, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	principal, ok := s.byRemote[bindingKey(platform, remoteUserID)]
	return principal, ok, nil
}

func (s *Store) Bindings(_ context.Context, principal unibot.PrincipalID) ([]unibot.Binding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := s.byPrincipal[principal]
	out := make([]unibot.Binding, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out, nil
}
