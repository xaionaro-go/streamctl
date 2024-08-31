package streams

import (
	"context"
	"errors"
	"time"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/xaionaro-go/streamctl/pkg/observability"
)

func (s *Stream) Play(source string) error {
	ctx := context.TODO()
	s.mu.Do(ctx, func() {
		for _, producer := range s.producers {
			if producer.state == stateInternal && producer.conn != nil {
				_ = producer.conn.Stop()
			}
		}
	})

	if source == "" {
		return nil
	}

	var src core.Producer

	for _, producer := range s.producers {
		if producer.conn == nil {
			continue
		}

		cons, ok := producer.conn.(core.Consumer)
		if !ok {
			continue
		}

		if src == nil {
			var err error
			if src, err = s.streamHandler.GetProducer(source); err != nil {
				return err
			}
		}

		if !matchMedia(src, cons) {
			continue
		}

		s.AddInternalProducer(src)

		observability.Go(ctx, func() {
			_ = src.Start()
			s.RemoveProducer(src)
		})

		return nil
	}

	for _, producer := range s.producers {
		// start new client
		dst, err := s.streamHandler.GetProducer(producer.urlFunc())
		if err != nil {
			continue
		}

		// check if client support consumer interface
		cons, ok := dst.(core.Consumer)
		if !ok {
			_ = dst.Stop()
			continue
		}

		// start new producer
		if src == nil {
			if src, err = s.streamHandler.GetProducer(source); err != nil {
				return err
			}
		}

		if !matchMedia(src, cons) {
			_ = dst.Stop()
			continue
		}

		s.AddInternalProducer(src)
		s.AddInternalConsumer(cons)

		observability.Go(ctx, func() {
			_ = dst.Start()
			_ = src.Stop()
			s.RemoveInternalConsumer(cons)
		})

		observability.Go(ctx, func() {
			_ = src.Start()
			// little timeout before stop dst, so the buffer can be transferred
			time.Sleep(time.Second)
			_ = dst.Stop()
			s.RemoveProducer(src)
		})

		return nil
	}

	return errors.New("can't find consumer")
}

func (s *Stream) AddInternalProducer(conn core.Producer) {
	producer := &Producer{conn: conn, state: stateInternal}
	ctx := context.TODO()
	s.mu.Do(ctx, func() {
		s.producers = append(s.producers, producer)
	})
}

func (s *Stream) AddInternalConsumer(conn core.Consumer) {
	ctx := context.TODO()
	s.mu.Do(ctx, func() {
		s.consumers = append(s.consumers, conn)
	})
}

func (s *Stream) RemoveInternalConsumer(conn core.Consumer) {
	ctx := context.TODO()
	s.mu.Do(ctx, func() {
		for i, consumer := range s.consumers {
			if consumer == conn {
				s.consumers = append(s.consumers[:i], s.consumers[i+1:]...)
				break
			}
		}
	})
}

func matchMedia(prod core.Producer, cons core.Consumer) bool {
	for _, consMedia := range cons.GetMedias() {
		for _, prodMedia := range prod.GetMedias() {
			if prodMedia.Direction != core.DirectionRecvonly {
				continue
			}

			prodCodec, consCodec := prodMedia.MatchMedia(consMedia)
			if prodCodec == nil {
				continue
			}

			track, err := prod.GetTrack(prodMedia, prodCodec)
			if err != nil {
				continue
			}

			if err = cons.AddTrack(consMedia, consCodec, track); err != nil {
				continue
			}

			return true
		}
	}

	return false
}
