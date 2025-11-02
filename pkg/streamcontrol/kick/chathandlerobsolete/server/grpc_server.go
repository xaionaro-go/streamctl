package server

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/xaionaro-go/kickcom"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/chathandlerobsolete/protobuf/go/chathandlerobsolete_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/kickclientobsolete"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/goconv"
	"github.com/xaionaro-go/xcontext"
	"github.com/xaionaro-go/xgrpc"
	"github.com/xaionaro-go/xsync"
	"google.golang.org/grpc"
)

type GRPCServer struct {
	chathandlerobsolete_grpc.UnimplementedChatHandlerObsoleteServer
	GRPCServer *grpc.Server
	Belt       *belt.Belt
	Locker     xsync.Mutex
	Handler    *ChatHandlerOBSOLETE
}

func NewGRPCServer() *GRPCServer {
	srv := &GRPCServer{
		GRPCServer: grpc.NewServer(),
	}
	chathandlerobsolete_grpc.RegisterChatHandlerObsoleteServer(srv.GRPCServer, srv)
	return srv
}

func (srv *GRPCServer) Serve(
	listener net.Listener,
) error {
	return srv.GRPCServer.Serve(listener)
}

func logLevelProtobuf2Go(logLevel chathandlerobsolete_grpc.LoggingLevel) logger.Level {
	switch logLevel {
	case chathandlerobsolete_grpc.LoggingLevel_LoggingLevelNone:
		return logger.LevelFatal
	case chathandlerobsolete_grpc.LoggingLevel_LoggingLevelFatal:
		return logger.LevelFatal
	case chathandlerobsolete_grpc.LoggingLevel_LoggingLevelPanic:
		return logger.LevelPanic
	case chathandlerobsolete_grpc.LoggingLevel_LoggingLevelError:
		return logger.LevelError
	case chathandlerobsolete_grpc.LoggingLevel_LoggingLevelWarn:
		return logger.LevelWarning
	case chathandlerobsolete_grpc.LoggingLevel_LoggingLevelInfo:
		return logger.LevelInfo
	case chathandlerobsolete_grpc.LoggingLevel_LoggingLevelDebug:
		return logger.LevelDebug
	case chathandlerobsolete_grpc.LoggingLevel_LoggingLevelTrace:
		return logger.LevelTrace
	default:
		return logger.LevelUndefined
	}
}

func (srv *GRPCServer) getCtx(
	ctx context.Context,
	commons *chathandlerobsolete_grpc.RequestCommons,
) context.Context {
	loggingLevel := logLevelProtobuf2Go(commons.GetLoggingLevel())
	l := logrus.Default().WithLevel(loggingLevel)
	ctx = logger.CtxWithLogger(ctx, l)
	return ctx
}

func (srv *GRPCServer) Open(
	ctx context.Context,
	req *chathandlerobsolete_grpc.OpenRequest,
) (*chathandlerobsolete_grpc.OpenReply, error) {
	return xsync.DoA2R2(ctx, &srv.Locker, srv.open, ctx, req)
}

func (srv *GRPCServer) open(
	ctx context.Context,
	req *chathandlerobsolete_grpc.OpenRequest,
) (*chathandlerobsolete_grpc.OpenReply, error) {
	if srv.Handler != nil {
		return &chathandlerobsolete_grpc.OpenReply{}, fmt.Errorf("chat handler is already opened")
	}
	ctx = srv.getCtx(ctx, req.GetCommons())
	channelSlug := req.GetChannelSlug()

	reverseEngClient, err := kickclientobsolete.New()
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a client to Kick: %w", err)
	}

	var resp *kickcom.ChannelV1
	{
		ctx, cancelFn := context.WithTimeout(ctx, time.Minute)
		resp, err = reverseEngClient.GetChannelV1(ctx, channelSlug)
		cancelFn()
		if err != nil {
			return nil, fmt.Errorf("unable to get channel '%s' info: %w", channelSlug, err)
		}
	}

	srv.Handler, err = NewChatHandlerOBSOLETE(
		xcontext.DetachDone(ctx),
		reverseEngClient,
		resp.ID,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to open a chat handler: %w", err)
	}
	return &chathandlerobsolete_grpc.OpenReply{}, nil
}

func (srv *GRPCServer) Close(
	ctx context.Context,
	req *chathandlerobsolete_grpc.CloseRequest,
) (*chathandlerobsolete_grpc.CloseReply, error) {
	return xsync.DoA2R2(ctx, &srv.Locker, srv.close, ctx, req)
}

func (srv *GRPCServer) close(
	ctx context.Context,
	req *chathandlerobsolete_grpc.CloseRequest,
) (*chathandlerobsolete_grpc.CloseReply, error) {
	if srv.Handler == nil {
		return &chathandlerobsolete_grpc.CloseReply{}, fmt.Errorf("chat handler is not opened")
	}
	err := srv.Handler.Close(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to close chat handler: %w", err)
	}
	srv.Handler = nil
	return &chathandlerobsolete_grpc.CloseReply{}, nil
}

func (srv *GRPCServer) getHandler(
	ctx context.Context,
) (*ChatHandlerOBSOLETE, error) {
	srv.Locker.ManualRLock(ctx)
	defer srv.Locker.ManualRUnlock(ctx)
	if srv.Handler == nil {
		return nil, fmt.Errorf("chat handler is not opened")
	}
	return srv.Handler, nil
}

func (srv *GRPCServer) MessagesChan(
	req *chathandlerobsolete_grpc.MessagesChanRequest,
	sender chathandlerobsolete_grpc.ChatHandlerObsolete_MessagesChanServer,
) error {
	ctx := srv.getCtx(sender.Context(), req.GetCommons())
	h, err := srv.getHandler(ctx)
	if err != nil {
		return err
	}

	return xgrpc.WrapChan(
		ctx,
		func(ctx context.Context) (<-chan streamcontrol.Event, error) {
			return h.MessagesChan(), nil
		},
		sender,
		goconv.EventGo2GRPC,
	)
}
