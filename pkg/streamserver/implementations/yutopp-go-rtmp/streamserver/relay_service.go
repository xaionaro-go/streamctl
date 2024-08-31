package streamserver

import (
	"context"
	"fmt"
	"sort"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

// TODO: Create this service per apps.
// In this example, this instance is singleton.
type RelayService struct {
	m xsync.Mutex

	streams        map[string]*Pubsub
	streamsChanged chan struct{}
}

func NewRelayService() *RelayService {
	return &RelayService{
		streams:        make(map[string]*Pubsub),
		streamsChanged: make(chan struct{}),
	}
}

func (s *RelayService) NewPubsub(key string, publisherHandler *Handler) (*Pubsub, error) {
	ctx := context.TODO()
	return xsync.DoR2(ctx, &s.m, func() (*Pubsub, error) {
		logger.Debugf(ctx, "NewPubsub(%s)", key)

		if oldStream, ok := s.streams[key]; ok {
			err := oldStream.deregister()
			if err != nil {
				logger.Warnf(ctx, "unable to close the old stream: %v", err)
			}
		}

		pubsub := NewPubsub(s, key, publisherHandler)

		s.streams[key] = pubsub

		var oldCh chan struct{}
		oldCh, s.streamsChanged = s.streamsChanged, make(chan struct{})
		close(oldCh)

		return pubsub, nil
	})
}

func (s *RelayService) GetPubsub(key string) *Pubsub {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &s.m, func() *Pubsub {
		return s.streams[key]
	})
}

func (s *RelayService) WaitPubsub(ctx context.Context, key string) *Pubsub {
	for {
		ctx := context.TODO()
		pubSub, waitCh := xsync.DoR2(ctx, &s.m, func() (*Pubsub, chan struct{}) {
			return s.streams[key], s.streamsChanged
		})

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
	ctx := context.TODO()
	return xsync.DoR1(ctx, &s.m, func() map[string]*Pubsub {
		m := make(map[string]*Pubsub, len(s.streams))
		for k, v := range s.streams {
			m[k] = v
		}
		return m
	})
}

func (s *RelayService) PubsubNames() []string {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &s.m, func() []string {
		result := make([]string, 0, len(s.streams))
		for k := range s.streams {
			result = append(result, k)
		}
		sort.Strings(result)
		return result
	})
}

func (s *RelayService) RemovePubsub(key string) error {
	ctx := context.TODO()
	return xsync.DoA1R1(ctx, &s.m, s.removePubsub, key)
}

func (s *RelayService) removePubsub(key string) error {
	logger.Default().Debugf("removePubsub(%s)", key)

	if _, ok := s.streams[key]; !ok {
		return fmt.Errorf("not published: %s", key)
	}

	delete(s.streams, key)

	var oldCh chan struct{}
	oldCh, s.streamsChanged = s.streamsChanged, make(chan struct{})
	close(oldCh)

	return nil
}
