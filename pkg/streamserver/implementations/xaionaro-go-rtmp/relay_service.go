package yutoppgortmp

import (
	"context"
	"fmt"
	"sort"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/xsync"
)

// TODO: Create this service per apps.
// In this example, this instance is singleton.
type RelayService struct {
	m xsync.Mutex

	streams        map[types.AppKey]*Pubsub
	streamsChanged chan struct{}
}

func NewRelayService() *RelayService {
	return &RelayService{
		streams:        make(map[types.AppKey]*Pubsub),
		streamsChanged: make(chan struct{}),
	}
}

func (s *RelayService) NewPubsub(key types.AppKey, publisherHandler *Handler) (*Pubsub, error) {
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

func (s *RelayService) GetPubsub(key types.AppKey) *Pubsub {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &s.m, func() *Pubsub {
		return s.streams[key]
	})
}

func (s *RelayService) WaitPubsub(
	ctx context.Context,
	key types.AppKey,
	waitForNext bool,
) *Pubsub {
	var curPubsub *Pubsub
	if waitForNext {
		curPubsub = xsync.DoR1(
			ctx, &s.m, func() *Pubsub {
				return s.streams[key]
			},
		)
	}

	for {
		ctx := context.TODO()
		pubSub, waitCh := xsync.DoR2(ctx, &s.m, func() (*Pubsub, chan struct{}) {
			return s.streams[key], s.streamsChanged
		})

		logger.Debugf(ctx, "WaitPubSub(%s): pubSub==%v", key, pubSub)
		if pubSub != nil && pubSub != curPubsub {
			return pubSub
		}
		logger.Debugf(ctx, "WaitPubSub(%s): waiting...", key)
		select {
		case <-ctx.Done():
			logger.Debugf(ctx, "WaitPubSub(%s): cancelled", key)
			return nil
		case <-waitCh:
			logger.Debugf(ctx, "WaitPubSub(%s): an event happened, rechecking", key)
		}
	}
}

func (s *RelayService) Pubsubs() map[types.AppKey]*Pubsub {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &s.m, func() map[types.AppKey]*Pubsub {
		m := make(map[types.AppKey]*Pubsub, len(s.streams))
		for k, v := range s.streams {
			m[k] = v
		}
		return m
	})
}

func (s *RelayService) PubsubNames() []types.AppKey {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &s.m, func() []types.AppKey {
		result := make([]types.AppKey, 0, len(s.streams))
		for k := range s.streams {
			result = append(result, k)
		}
		sort.Slice(result, func(i, j int) bool {
			return result[i] < result[j]
		})
		return result
	})
}

func (s *RelayService) RemovePubsub(key types.AppKey) error {
	ctx := context.TODO()
	return xsync.DoA1R1(ctx, &s.m, s.removePubsub, key)
}

func (s *RelayService) removePubsub(key types.AppKey) error {
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
