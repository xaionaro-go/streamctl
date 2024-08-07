package streamserver

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/facebookincubator/go-belt/tool/logger"
)

// TODO: Create this service per apps.
// In this example, this instance is singleton.
type RelayService struct {
	m sync.Mutex

	streams        map[string]*Pubsub
	streamsChanged chan struct{}
}

func NewRelayService() *RelayService {
	return &RelayService{
		streams:        make(map[string]*Pubsub),
		streamsChanged: make(chan struct{}),
	}
}

func (s *RelayService) NewPubsub(key string) (*Pubsub, error) {
	s.m.Lock()
	defer s.m.Unlock()

	logger.Default().Debugf("NewPubsub(%s)", key)

	if _, ok := s.streams[key]; ok {
		return nil, fmt.Errorf("already published: %s", key)
	}

	pubsub := NewPubsub(s, key)

	s.streams[key] = pubsub

	var oldCh chan struct{}
	oldCh, s.streamsChanged = s.streamsChanged, make(chan struct{})
	close(oldCh)

	return pubsub, nil
}

func (s *RelayService) GetPubsub(key string) *Pubsub {
	s.m.Lock()
	defer s.m.Unlock()
	return s.streams[key]
}

func (s *RelayService) WaitPubsub(ctx context.Context, key string) *Pubsub {
	for {
		s.m.Lock()
		pubSub := s.streams[key]
		waitCh := s.streamsChanged
		s.m.Unlock()

		logger.Debugf(ctx, "WaitPubSub(%s): pubSub==%v", key, pubSub)
		if pubSub != nil {
			return pubSub
		}
		logger.Debugf(ctx, "WaitPubSub(%s): waiting...", key)
		select {
		case <-ctx.Done():
			return nil
		case <-waitCh:
		}
	}
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

	return s.removePubsub(key)
}
func (s *RelayService) removePubsub(key string) error {
	logger.Default().Tracef("removePubsub(%s)", key)

	if _, ok := s.streams[key]; !ok {
		return fmt.Errorf("not published: %s", key)
	}

	delete(s.streams, key)

	var oldCh chan struct{}
	oldCh, s.streamsChanged = s.streamsChanged, make(chan struct{})
	close(oldCh)

	return nil
}
