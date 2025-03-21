package rtmp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/flv"
	"github.com/AlexxIT/go2rtc/pkg/rtmp"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/rs/zerolog/log"
	"github.com/xaionaro-go/datacounter"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/consts"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/go2rtc/streamserver/streams"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type RTMPServer struct {
	Config         Config
	StreamHandler  *streams.StreamHandler
	Listener       net.Listener
	CancelFn       context.CancelFunc
	TrafficCounter types.TrafficCounter
}

type Config struct {
	Listen string `yaml:"listen" json:"listen"`
}

func New(
	ctx context.Context,
	cfg Config,
	streamHandler *streams.StreamHandler,
) (*RTMPServer, error) {
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:1935"
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("unable to start listening '%s': %w", cfg.Listen, err)
	}

	ctx, cancelFn := context.WithCancel(ctx)
	s := &RTMPServer{
		Config:        cfg,
		StreamHandler: streamHandler,
		CancelFn:      cancelFn,
		Listener:      ln,
	}

	observability.Go(ctx, func() {
		<-ctx.Done()
		logger.Infof(ctx, "closing %s", cfg.Listen)
		err := ln.Close()
		errmon.ObserveErrorCtx(ctx, err)
	})
	logger.Infof(ctx, "started RTMP server at %s", cfg.Listen)

	observability.Go(ctx, func() {
		for {
			if ctx.Err() != nil {
				return
			}

			conn, err := ln.Accept()
			if err != nil {
				errmon.ObserveErrorCtx(ctx, err)
				return
			}

			observability.Go(ctx, func() {
				if err = s.tcpHandle(conn); err != nil {
					errmon.ObserveErrorCtx(ctx, err)
				}
			})
		}
	})

	return s, nil
}

func (s *RTMPServer) Type() streamtypes.ServerType {
	return streamtypes.ServerTypeRTMP
}
func (s *RTMPServer) ListenAddr() string {
	return s.Listener.Addr().String()
}
func (s *RTMPServer) Close() error {
	logger.Default().Tracef("(*RTMPServer).Close()")
	s.CancelFn()
	return nil
}

func (s *RTMPServer) tcpHandle(netConn net.Conn) error {
	rtmpConn, err := rtmp.NewServer(netConn)
	if err != nil {
		return err
	}

	if err = rtmpConn.ReadCommands(); err != nil {
		return err
	}

	switch rtmpConn.Intent {
	case rtmp.CommandPlay:
		stream := s.StreamHandler.Get(rtmpConn.App)
		if stream == nil {
			return errors.New("stream not found: " + rtmpConn.App)
		}

		cons := flv.NewConsumer()
		if err = stream.AddConsumer(cons); err != nil {
			return err
		}

		defer stream.RemoveConsumer(cons)

		if err = rtmpConn.WriteStart(); err != nil {
			return err
		}

		wc := datacounter.NewWriterCounter(rtmpConn)
		ctx := context.TODO()
		s.TrafficCounter.Do(ctx, func() {
			s.TrafficCounter.WriterCounter = wc
		})

		_, _ = cons.WriteTo(wc)

		return nil

	case rtmp.CommandPublish:
		stream := s.StreamHandler.Get(rtmpConn.App)
		if stream == nil {
			return errors.New("stream not found: " + rtmpConn.App)
		}

		if err = rtmpConn.WriteStart(); err != nil {
			return err
		}

		prod, err := rtmpConn.Producer()
		if err != nil {
			return err
		}

		stream.AddProducer(prod)

		defer stream.RemoveProducer(prod)

		rc := types.NewIntPtrCounter(&prod.Recv)
		ctx := context.TODO()
		s.TrafficCounter.Do(ctx, func() {
			s.TrafficCounter.ReaderCounter = rc
		})

		err = prod.Start()
		if err != nil {
			logger.Default().Error(err)
		}

		return nil
	}

	return errors.New("rtmp: unknown command: " + rtmpConn.Intent)
}

func (s *RTMPServer) NumBytesConsumerWrote() uint64 {
	return s.TrafficCounter.NumBytesWrote()
}
func (s *RTMPServer) NumBytesProducerRead() uint64 {
	return s.TrafficCounter.NumBytesRead()
}

func StreamsHandle(url string) (core.Producer, error) {
	return rtmp.DialPlay(url)
}

func StreamsConsumerHandle(
	url string,
) (core.Consumer, types.NumBytesReaderWroter, func(context.Context) error, error) {
	cons := flv.NewConsumer()
	trafficCounter := &types.TrafficCounter{}
	run := func(ctx context.Context) error {
		wr, err := rtmp.DialPublish(url)
		if err != nil {
			return fmt.Errorf("unable to connect to RTMP destination '%s': %w", url, err)
		}

		wrc := datacounter.NewWriterCounter(wr)
		trafficCounter.Do(ctx, func() {
			trafficCounter.WriterCounter = wrc
		})

		ctx, cancelFn := context.WithCancel(ctx)
		defer cancelFn()
		observability.Go(ctx, func() {
			<-ctx.Done()
			cancelFn()
			err := wr.(io.Closer).Close()
			errmon.ObserveErrorCtx(ctx, err)
		})

		_, err = cons.WriteTo(wrc)
		if err != nil {
			return fmt.Errorf("unable to write: %w", err)
		}
		return nil
	}

	return cons, trafficCounter, run, nil
}

func (s *RTMPServer) apiHandle(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		s.outputFLV(w, r)
	} else {
		s.inputFLV(w, r)
	}
}

func (s *RTMPServer) outputFLV(w http.ResponseWriter, r *http.Request) {
	src := r.URL.Query().Get("src")
	stream := s.StreamHandler.Get(src)
	if stream == nil {
		http.Error(w, consts.StreamNotFound, http.StatusNotFound)
		return
	}

	cons := flv.NewConsumer()
	cons.WithRequest(r)

	if err := stream.AddConsumer(cons); err != nil {
		log.Error().Err(err).Caller().Send()
		return
	}

	h := w.Header()
	h.Set("Content-Type", "video/x-flv")

	_, _ = cons.WriteTo(w)

	stream.RemoveConsumer(cons)
}

func (s *RTMPServer) inputFLV(w http.ResponseWriter, r *http.Request) {
	dst := r.URL.Query().Get("dst")
	stream := s.StreamHandler.Get(dst)
	if stream == nil {
		http.Error(w, consts.StreamNotFound, http.StatusNotFound)
		return
	}

	client, err := flv.Open(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stream.AddProducer(client)

	if err = client.Start(); err != nil && err != io.EOF {
		log.Warn().Err(err).Caller().Send()
	}

	stream.RemoveProducer(client)
}
