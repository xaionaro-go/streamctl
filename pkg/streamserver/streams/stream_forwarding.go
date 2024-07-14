package streams

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/hashicorp/go-multierror"
)

type StreamForwarding struct {
	sync.Mutex
	Stream        *Stream
	Consumer      core.Consumer
	StreamHandler *StreamHandler
	CancelFunc    context.CancelFunc
	URL           string
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

	cons, run, err := sf.StreamHandler.GetConsumer(url)
	if err != nil {
		return fmt.Errorf("unable to initialize consumer of '%s': %w", url, err)
	}
	sf.Stream = s
	sf.URL = url
	sf.Consumer = cons

	if err = s.AddConsumer(cons); err != nil {
		return fmt.Errorf("unable to add consumer: %w", err)
	}
	ctx, cancelFn := context.WithCancel(ctx)
	sf.CancelFunc = cancelFn

	go func() {
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
