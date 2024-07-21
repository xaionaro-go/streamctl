package streams

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type StreamForwarding struct {
	sync.Mutex
	Stream         *Stream
	Consumer       core.Consumer
	StreamHandler  *StreamHandler
	CancelFunc     context.CancelFunc
	URL            string
	TrafficCounter types.NumBytesReaderWroter
}

func NewStreamForwarding(streamHandler *StreamHandler) *StreamForwarding {
	return &StreamForwarding{StreamHandler: streamHandler}
}

func (sf *StreamForwarding) Start(
	ctx context.Context,
	s *Stream,
	url string,
) error {
	sf.Lock()
	defer sf.Unlock()

	cons, trafficCounter, run, err := sf.StreamHandler.GetConsumer(url)
	if err != nil {
		return fmt.Errorf("unable to initialize consumer of '%s': %w", url, err)
	}
	sf.Stream = s
	sf.URL = url
	sf.Consumer = cons
	sf.TrafficCounter = trafficCounter

	ctx, cancelFn := context.WithCancel(ctx)
	sf.CancelFunc = cancelFn

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			err = s.AddConsumer(cons)
			if errors.Is(err, ErrNoProducer{}) {
				logger.Debugf(ctx, "waiting for a producer")
				time.Sleep(time.Second)
				continue
			}
			if err != nil {
				logger.Errorf(ctx, "unable to add consumer of '%s': %v", sf.URL, err)
				time.Sleep(time.Second * 5)
				continue
			}
			break
		}

		err := run(ctx)
		errmon.ObserveErrorCtx(ctx, err)
		s.RemoveConsumer(cons)

		// TODO: more smart retry
		time.Sleep(5 * time.Second)
		_, err = s.Publish(ctx, url)
		if err != nil {
			errmon.ObserveErrorCtx(ctx, err)
			err := sf.Close()
			errmon.ObserveErrorCtx(ctx, err)
		}
	}()

	return nil
}

func (sf *StreamForwarding) IsClosed() bool {
	sf.Lock()
	defer sf.Unlock()
	return sf.isClosed()
}

func (sf *StreamForwarding) isClosed() bool {
	return sf.CancelFunc == nil
}

func (sf *StreamForwarding) Close() error {
	sf.Lock()
	defer sf.Unlock()
	if sf.isClosed() {
		return nil
	}
	sf.CancelFunc()

	var result *multierror.Error
	err := sf.Consumer.Stop()
	if err != nil {
		result = multierror.Append(result, err)
	}
	sf.Stream = nil
	sf.Consumer = nil
	sf.CancelFunc = nil
	return result.ErrorOrNil()
}
