package memory

import (
	"context"
	"sync"
	"time"

	unibot "github.com/maksimkurb/cublyverse/unibot"
)

type Store struct {
	mu      sync.RWMutex
	tokens  map[string]unibot.Interaction
	message map[string]unibot.Interaction
}

func New() *Store {
	return &Store{
		tokens:  make(map[string]unibot.Interaction),
		message: make(map[string]unibot.Interaction),
	}
}

func (s *Store) PutToken(_ context.Context, token string, interaction unibot.Interaction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = interaction
	return nil
}

func (s *Store) ResolveToken(_ context.Context, token string) (unibot.Interaction, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	interaction, ok := s.tokens[token]
	if ok && !interaction.ExpiresAt.IsZero() && time.Now().After(interaction.ExpiresAt) {
		return unibot.Interaction{}, false, nil
	}
	return interaction, ok, nil
}

func messageKey(platform unibot.Platform, conversationID, messageID, key string) string {
	return string(platform) + "\x00" + conversationID + "\x00" + messageID + "\x00" + key
}

func (s *Store) PutMessageKey(_ context.Context, platform unibot.Platform, conversationID, messageID, key string, interaction unibot.Interaction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.message[messageKey(platform, conversationID, messageID, key)] = interaction
	return nil
}

func (s *Store) ResolveMessageKey(_ context.Context, platform unibot.Platform, conversationID, messageID, key string) (unibot.Interaction, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	interaction, ok := s.message[messageKey(platform, conversationID, messageID, key)]
	if ok && !interaction.ExpiresAt.IsZero() && time.Now().After(interaction.ExpiresAt) {
		return unibot.Interaction{}, false, nil
	}
	return interaction, ok, nil
}
