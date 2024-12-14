package server

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/encoder/libav/encoder"
	"github.com/xaionaro-go/streamctl/pkg/encoder/libav/safeencoder/grpc/go/encoder_grpc"
	"github.com/xaionaro-go/streamctl/pkg/xcontext"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
	"google.golang.org/grpc"
)

type EncoderID uint64
type InputID uint64
type OutputID uint64

type GRPCServer struct {
	encoder_grpc.UnimplementedEncoderServer

	GRPCServer *grpc.Server
	IsStarted  bool

	BeltLocker xsync.Mutex
	Belt       *belt.Belt

	EncoderLocker xsync.Mutex
	Encoder       map[EncoderID]*encoder.Encoder
	EncoderNextID atomic.Uint64

	InputLocker xsync.Mutex
	Input       map[InputID]*encoder.Input
	InputNextID atomic.Uint64

	OutputLocker xsync.Mutex
	Output       map[OutputID]*encoder.Output
	OutputNextID atomic.Uint64
}

func NewServer() *GRPCServer {
	srv := &GRPCServer{
		GRPCServer: grpc.NewServer(),
		Encoder:    make(map[EncoderID]*encoder.Encoder),
		Input:      make(map[InputID]*encoder.Input),
		Output:     make(map[OutputID]*encoder.Output),
	}
	encoder_grpc.RegisterEncoderServer(srv.GRPCServer, srv)
	return srv
}

func (srv *GRPCServer) Serve(
	ctx context.Context,
	listener net.Listener,
) error {
	if srv.IsStarted {
		panic("this GRPC server was already started at least once")
	}
	srv.IsStarted = true
	srv.Belt = belt.CtxBelt(ctx)
	return srv.GRPCServer.Serve(listener)
}

func (srv *GRPCServer) belt() *belt.Belt {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &srv.BeltLocker, func() *belt.Belt {
		return srv.Belt
	})
}

func (srv *GRPCServer) ctx(ctx context.Context) context.Context {
	return belt.CtxWithBelt(ctx, srv.belt())
}

func (srv *GRPCServer) SetLoggingLevel(
	ctx context.Context,
	req *encoder_grpc.SetLoggingLevelRequest,
) (*encoder_grpc.SetLoggingLevelReply, error) {
	ctx = srv.ctx(ctx)
	srv.BeltLocker.Do(ctx, func() {
		logLevel := logLevelProtobuf2Go(req.GetLevel())
		l := logger.FromBelt(srv.Belt).WithLevel(logLevel)
		srv.Belt = srv.Belt.WithTool(logger.ToolID, l)
	})
	return &encoder_grpc.SetLoggingLevelReply{}, nil
}

func (srv *GRPCServer) NewInput(
	ctx context.Context,
	req *encoder_grpc.NewInputRequest,
) (*encoder_grpc.NewInputReply, error) {
	ctx = srv.ctx(ctx)
	switch path := req.Path.GetResourcePath().(type) {
	case *encoder_grpc.ResourcePath_Url:
		return srv.newInputByURL(ctx, path, req.Config)
	default:
		return nil, fmt.Errorf("the support of path type '%T' is not implemented", path)
	}
}

func (srv *GRPCServer) newInputByURL(
	ctx context.Context,
	path *encoder_grpc.ResourcePath_Url,
	_ *encoder_grpc.InputConfig,
) (*encoder_grpc.NewInputReply, error) {
	config := encoder.InputConfig{}
	input, err := encoder.NewInputFromURL(ctx, path.Url.Url, path.Url.AuthKey, config)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to initialize an input using URL '%s' and config %#+v",
			path.Url,
			config,
		)
	}

	inputID := xsync.DoR1(ctx, &srv.InputLocker, func() InputID {
		inputID := InputID(srv.InputNextID.Add(1))
		srv.Input[inputID] = input
		return inputID
	})
	return &encoder_grpc.NewInputReply{
		Id: uint64(inputID),
	}, nil
}

func (srv *GRPCServer) CloseInput(
	ctx context.Context,
	req *encoder_grpc.CloseInputRequest,
) (*encoder_grpc.CloseInputReply, error) {
	inputID := InputID(req.GetInputID())
	err := xsync.DoR1(ctx, &srv.InputLocker, func() error {
		input := srv.Input[inputID]
		if input == nil {
			return fmt.Errorf("there is no open input with ID %d", inputID)
		}
		input.Close()
		delete(srv.Input, inputID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &encoder_grpc.CloseInputReply{}, nil
}

func (srv *GRPCServer) NewOutput(
	ctx context.Context,
	req *encoder_grpc.NewOutputRequest,
) (*encoder_grpc.NewOutputReply, error) {
	ctx = srv.ctx(ctx)
	switch path := req.Path.GetResourcePath().(type) {
	case *encoder_grpc.ResourcePath_Url:
		return srv.newOutputByURL(ctx, path, req.Config)
	default:
		return nil, fmt.Errorf("the support of path type '%T' is not implemented", path)
	}
}

func (srv *GRPCServer) newOutputByURL(
	ctx context.Context,
	path *encoder_grpc.ResourcePath_Url,
	_ *encoder_grpc.OutputConfig,
) (*encoder_grpc.NewOutputReply, error) {
	config := encoder.OutputConfig{}
	output, err := encoder.NewOutputFromURL(ctx, path.Url.Url, path.Url.AuthKey, config)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to initialize an output using URL '%s' and config %#+v: %w",
			path.Url,
			config,
			err,
		)
	}

	outputID := xsync.DoR1(ctx, &srv.OutputLocker, func() OutputID {
		outputID := OutputID(srv.OutputNextID.Add(1))
		srv.Output[outputID] = output
		return outputID
	})
	return &encoder_grpc.NewOutputReply{
		Id: uint64(outputID),
	}, nil
}

func (srv *GRPCServer) CloseOutput(
	ctx context.Context,
	req *encoder_grpc.CloseOutputRequest,
) (*encoder_grpc.CloseOutputReply, error) {
	outputID := OutputID(req.GetOutputID())
	err := xsync.DoR1(ctx, &srv.InputLocker, func() error {
		output := srv.Output[outputID]
		if output == nil {
			return fmt.Errorf("there is no open output with ID %d", outputID)
		}
		output.Close()
		delete(srv.Output, outputID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &encoder_grpc.CloseOutputReply{}, nil
}

func (srv *GRPCServer) NewEncoder(
	ctx context.Context,
	req *encoder_grpc.NewEncoderRequest,
) (*encoder_grpc.NewEncoderReply, error) {
	ctx = srv.ctx(ctx)
	recoderInstance := encoder.New()
	recoderID := xsync.DoR1(ctx, &srv.EncoderLocker, func() EncoderID {
		recoderID := EncoderID(srv.EncoderNextID.Add(1))
		srv.Encoder[recoderID] = recoderInstance
		return recoderID
	})
	return &encoder_grpc.NewEncoderReply{
		Id: uint64(recoderID),
	}, nil
}

func (srv *GRPCServer) GetEncoderStats(
	ctx context.Context,
	req *encoder_grpc.GetEncoderStatsRequest,
) (*encoder_grpc.GetEncoderStatsReply, error) {
	recoderID := EncoderID(req.GetEncoderID())
	recoder := xsync.DoR1(ctx, &srv.EncoderLocker, func() *encoder.Encoder {
		return srv.Encoder[recoderID]
	})
	return &encoder_grpc.GetEncoderStatsReply{
		BytesCountRead:  recoder.EncoderStats.BytesCountRead.Load(),
		BytesCountWrote: recoder.EncoderStats.BytesCountWrote.Load(),
	}, nil
}

func (srv *GRPCServer) StartRecoding(
	ctx context.Context,
	req *encoder_grpc.StartEncodingRequest,
) (*encoder_grpc.StartEncodingReply, error) {
	ctx = srv.ctx(ctx)

	recoderID := EncoderID(req.GetEncoderID())
	inputID := InputID(req.GetInputID())
	outputID := OutputID(req.GetOutputID())

	srv.EncoderLocker.ManualLock(ctx)
	srv.InputLocker.ManualLock(ctx)
	srv.OutputLocker.ManualLock(ctx)
	defer srv.EncoderLocker.ManualUnlock(ctx)
	defer srv.InputLocker.ManualUnlock(ctx)
	defer srv.OutputLocker.ManualUnlock(ctx)

	recoder := srv.Encoder[recoderID]
	if recoder == nil {
		return nil, fmt.Errorf("the recorder with ID '%v' does not exist", recoderID)
	}
	input := srv.Input[inputID]
	if input == nil {
		return nil, fmt.Errorf("the input with ID '%v' does not exist", inputID)
	}
	output := srv.Output[outputID]
	if output == nil {
		return nil, fmt.Errorf("the output with ID '%v' does not exist", outputID)
	}

	err := recoder.StartEncoding(xcontext.DetachDone(ctx), input, output)
	if err != nil {
		return nil, fmt.Errorf("unable to start recoding")
	}

	return &encoder_grpc.StartEncodingReply{}, nil
}

func (srv *GRPCServer) EncodingEndedChan(
	req *encoder_grpc.EncodingEndedChanRequest,
	streamSrv encoder_grpc.Encoder_EncodingEndedChanServer,
) (_ret error) {
	ctx := srv.ctx(streamSrv.Context())
	recoderID := EncoderID(req.GetEncoderID())

	logger.Tracef(ctx, "EncodingEndedChan(%v)", recoderID)
	defer func() { logger.Tracef(ctx, "/EncodingEndedChan(%v): %v", recoderID, _ret) }()

	recoder := xsync.DoR1(ctx, &srv.EncoderLocker, func() *encoder.Encoder {
		return srv.Encoder[recoderID]
	})

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-recoder.WaiterChan:
	}

	return streamSrv.Send(&encoder_grpc.EncodingEndedChanReply{})
}
