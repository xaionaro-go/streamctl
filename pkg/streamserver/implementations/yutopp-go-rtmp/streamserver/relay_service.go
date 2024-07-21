package streamserver

import (
	"fmt"
	"sort"
	"sync"
)

// TODO: Create this service per apps.
// In this example, this instance is singleton.
type RelayService struct {
	streams map[string]*Pubsub
	m       sync.Mutex
}

func NewRelayService() *RelayService {
	return &RelayService{
		streams: make(map[string]*Pubsub),
	}
}

func (s *RelayService) NewPubsub(key string) (*Pubsub, error) {
	s.m.Lock()
	defer s.m.Unlock()

	if _, ok := s.streams[key]; ok {
		return nil, fmt.Errorf("already published: %s", key)
	}

	pubsub := NewPubsub(s, key)

	s.streams[key] = pubsub

	return pubsub, nil
}

func (s *RelayService) GetPubsub(key string) *Pubsub {
	s.m.Lock()
	defer s.m.Unlock()
	return s.streams[key]
}

func (s *RelayService) Pubsubs() map[string]*Pubsub {
	s.m.Lock()
	defer s.m.Unlock()
	m := make(map[string]*Pubsub, len(s.streams))
	for k, v := range s.streams {
		m[k] = v
	}
	return m
}

func (s *RelayService) PubsubNames() []string {
	s.m.Lock()
	defer s.m.Unlock()
	result := make([]string, 0, len(s.streams))
	for k := range s.streams {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}

func (s *RelayService) RemovePubsub(key string) error {
	s.m.Lock()
	defer s.m.Unlock()

	if _, ok := s.streams[key]; !ok {
		return fmt.Errorf("not published: %s", key)
	}

	delete(s.streams, key)

	return nil
}
