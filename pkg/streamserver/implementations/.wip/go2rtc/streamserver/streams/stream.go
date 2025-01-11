package streams

import (
	"context"
	"encoding/json"
	"sync/atomic"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/xsync"
)

type Stream struct {
	streamHandler *StreamHandler

	producers   []*Producer
	consumers   []core.Consumer
	mu          xsync.Mutex
	pending     atomic.Int32
	forwardings []*StreamForwarding
}

func (s *StreamHandler) NewStream(source any) *Stream {
	switch source := source.(type) {
	case string:
		return &Stream{
			producers:     []*Producer{s.NewProducer(func() string { return source })},
			streamHandler: s,
		}
	case func() string:
		return &Stream{
			producers:     []*Producer{s.NewProducer(source)},
			streamHandler: s,
		}
	case []any:
		stream := new(Stream)
		stream.streamHandler = s
		for _, src := range source {
			str, ok := src.(string)
			if !ok {
				logger.Default().Errorf("[stream] NewStream: Expected string, got %v", src)
				continue
			}
			stream.producers = append(stream.producers, s.NewProducer(func() string { return str }))
		}
		return stream
	case map[string]any:
		return s.NewStream(source["url"])
	case nil:
		return &Stream{
			streamHandler: s,
		}
	default:
		panic(core.Caller())
	}
}

func (s *Stream) Sources() (sources []string) {
	for _, prod := range s.producers {
		sources = append(sources, prod.urlFunc())
	}
	return
}

func (s *Stream) SetSource(source string) {
	for _, prod := range s.producers {
		prod.SetSource(source)
	}
}

func (s *Stream) RemoveConsumer(cons core.Consumer) {
	_ = cons.Stop()

	ctx := context.TODO()
	s.mu.Do(ctx, func() {
		for i, consumer := range s.consumers {
			if consumer == cons {
				s.consumers = append(s.consumers[:i], s.consumers[i+1:]...)
				break
			}
		}
	})

	s.stopProducers()
}

func (s *Stream) AddProducer(prod core.Producer) {
	producer := &Producer{conn: prod, state: stateExternal}
	ctx := context.TODO()
	s.mu.Do(ctx, func() {
		s.producers = append(s.producers, producer)
	})
}

func (s *Stream) RemoveProducer(prod core.Producer) {
	ctx := context.TODO()
	s.mu.Do(ctx, func() {
		for i, producer := range s.producers {
			if producer.conn == prod {
				s.producers = append(s.producers[:i], s.producers[i+1:]...)
				break
			}
		}
	})
}

func (s *Stream) stopProducers() {
	if s.pending.Load() > 0 {
		logger.Default().Tracef("[streams] skip stop pending producer")
		return
	}

	ctx := context.TODO()
	s.mu.Do(ctx, func() {
	producers:
		for _, producer := range s.producers {
			for _, track := range producer.receivers {
				if len(track.Senders()) > 0 {
					continue producers
				}
			}
			for _, track := range producer.senders {
				if len(track.Senders()) > 0 {
					continue producers
				}
			}
			producer.stop()
		}
	})
}

func (s *Stream) MarshalJSON() ([]byte, error) {
	var info = struct {
		Producers []*Producer     `json:"producers"`
		Consumers []core.Consumer `json:"consumers"`
	}{
		Producers: s.producers,
		Consumers: s.consumers,
	}
	return json.Marshal(info)
}
