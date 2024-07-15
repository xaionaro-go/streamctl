package rtsp

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/rtsp"
	"github.com/AlexxIT/go2rtc/pkg/tcp"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/consts"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/streams"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type RTSPServer struct {
	Config        Config
	Listener      net.Listener
	DefaultMedias []*core.Media
	StreamHandler *streams.StreamHandler
	Handlers      []HandlerFunc
	CancelFn      context.CancelFunc
}

type Config struct {
	ListenAddr   string
	Username     string
	Password     string
	DefaultQuery string
	PacketSize   uint16
}

func New(
	ctx context.Context,
	cfg Config,
	streamHandler *streams.StreamHandler,
) (*RTSPServer, error) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:8554"
	}
	if cfg.DefaultQuery == "" {
		cfg.DefaultQuery = "video&audio"
	}

	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return nil, fmt.Errorf("unable to listen '%s': %w", cfg.ListenAddr, err)
	}

	logger.Default().WithField("addr", cfg.ListenAddr).Info("[rtsp] listen")

	ctx, cancelFn := context.WithCancel(ctx)
	s := &RTSPServer{
		Config:        cfg,
		Listener:      ln,
		StreamHandler: streamHandler,
		CancelFn:      cancelFn,
	}

	if query, err := url.ParseQuery(cfg.DefaultQuery); err == nil {
		s.DefaultMedias = ParseQuery(query)
	}

	go func() {
		<-ctx.Done()
		logger.Infof(ctx, "closing %s", cfg.ListenAddr)
		err := ln.Close()
		errmon.ObserveErrorCtx(ctx, err)
	}()
	logger.Infof(ctx, "started RTSP server at %d", cfg.ListenAddr)

	go func() {
		for {
			if ctx.Err() != nil {
				return
			}

			conn, err := ln.Accept()
			if err != nil {
				return
			}

			c := rtsp.NewServer(conn)
			c.PacketSize = cfg.PacketSize
			// skip check auth for localhost
			if cfg.Username != "" && !conn.RemoteAddr().(*net.TCPAddr).IP.IsLoopback() {
				c.Auth(cfg.Username, cfg.Password)
			}
			go s.tcpHandler(c)
		}
	}()

	return s, nil
}

type HandlerFunc func(conn *rtsp.Conn) bool

func (s *RTSPServer) HandleFunc(handler HandlerFunc) {
	s.Handlers = append(s.Handlers, handler)
}

func Handler(rawURL string) (core.Producer, error) {
	rawURL, rawQuery, _ := strings.Cut(rawURL, "#")

	conn := rtsp.NewClient(rawURL)
	conn.Backchannel = true
	conn.UserAgent = consts.UserAgent

	if rawQuery != "" {
		query := streams.ParseQuery(rawQuery)
		conn.Backchannel = query.Get("backchannel") == "1"
		conn.Media = query.Get("media")
		conn.Timeout = core.Atoi(query.Get("timeout"))
		conn.Transport = query.Get("transport")
	}

	if logger.Default().Level() >= logger.LevelTrace {
		conn.Listen(func(msg any) {
			switch msg := msg.(type) {
			case *tcp.Request:
				logger.Default().Tracef("[rtsp] client request:\n%s", msg)
			case *tcp.Response:
				logger.Default().Tracef("[rtsp] client response:\n%s", msg)
			case string:
				logger.Default().Tracef("[rtsp] client msg: %s", msg)
			}
		})
	}

	if err := conn.Dial(); err != nil {
		return nil, err
	}

	if err := conn.Describe(); err != nil {
		if !conn.Backchannel {
			return nil, err
		}
		logger.Default().Tracef("[rtsp] describe (backchannel=%t) err: %v", conn.Backchannel, err)

		// second try without backchannel, we need to reconnect
		conn.Backchannel = false
		if err = conn.Dial(); err != nil {
			return nil, err
		}
		if err = conn.Describe(); err != nil {
			return nil, err
		}
	}

	return conn, nil
}

func (s *RTSPServer) tcpHandler(conn *rtsp.Conn) {
	var name string
	var closer func()

	trace := logger.Default().Level() >= logger.LevelTrace

	conn.Listen(func(msg any) {
		if trace {
			switch msg := msg.(type) {
			case *tcp.Request:
				logger.Default().Tracef("[rtsp] server request:\n%s", msg)
			case *tcp.Response:
				logger.Default().Tracef("[rtsp] server response:\n%s", msg)
			}
		}

		switch msg {
		case rtsp.MethodDescribe:
			if len(conn.URL.Path) == 0 {
				logger.Default().Warn("[rtsp] server empty URL on DESCRIBE")
				return
			}

			name = conn.URL.Path[1:]

			stream := s.StreamHandler.Get(name)
			if stream == nil {
				return
			}

			logger.Default().WithField("stream", name).Debug("[rtsp] new consumer")

			conn.SessionName = consts.UserAgent

			query := conn.URL.Query()
			conn.Medias = ParseQuery(query)
			if conn.Medias == nil {
				for _, media := range s.DefaultMedias {
					conn.Medias = append(conn.Medias, media.Clone())
				}
			}

			if s := query.Get("pkt_size"); s != "" {
				conn.PacketSize = uint16(core.Atoi(s))
			}

			if err := stream.AddConsumer(conn); err != nil {
				logger.Default().WithField("error", err).WithField("stream", name).Warn("[rtsp]")
				return
			}

			closer = func() {
				stream.RemoveConsumer(conn)
			}

		case rtsp.MethodAnnounce:
			if len(conn.URL.Path) == 0 {
				logger.Default().Warn("[rtsp] server empty URL on ANNOUNCE")
				return
			}

			name = conn.URL.Path[1:]

			stream := s.StreamHandler.Get(name)
			if stream == nil {
				return
			}

			query := conn.URL.Query()
			if s := query.Get("timeout"); s != "" {
				conn.Timeout = core.Atoi(s)
			}

			logger.Default().WithField("stream", name).Debug("[rtsp] new producer")

			stream.AddProducer(conn)

			closer = func() {
				stream.RemoveProducer(conn)
			}
		}
	})

	if err := conn.Accept(); err != nil {
		if err != io.EOF {
			logger.Default().WithField("error", err).Warn()
		}
		if closer != nil {
			closer()
		}
		_ = conn.Close()
		return
	}

	for _, handler := range s.Handlers {
		if handler(conn) {
			return
		}
	}

	if closer != nil {
		if err := conn.Handle(); err != nil {
			logger.Default().WithField("error", err).Debug("[rtsp] handle")
		}

		closer()

		logger.Default().WithField("stream", name).Debug("[rtsp] disconnect")
	}

	_ = conn.Close()
}

func (s *RTSPServer) Type() types.ServerType {
	return types.ServerTypeRTSP
}
func (s *RTSPServer) ListenAddr() string {
	return s.Listener.Addr().String()
}
func (s *RTSPServer) Close() error {
	logger.Default().Tracef("(*RTSPServer).Close()")
	s.CancelFn()
	return nil
}

func ParseQuery(query map[string][]string) []*core.Media {
	if v := query["mp4"]; v != nil {
		return []*core.Media{
			{
				Kind:      core.KindVideo,
				Direction: core.DirectionSendonly,
				Codecs: []*core.Codec{
					{Name: core.CodecH264},
					{Name: core.CodecH265},
				},
			},
			{
				Kind:      core.KindAudio,
				Direction: core.DirectionSendonly,
				Codecs: []*core.Codec{
					{Name: core.CodecAAC},
				},
			},
		}
	}

	return core.ParseQuery(query)
}
