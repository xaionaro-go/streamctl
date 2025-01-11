package server

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/saferecoder/grpc/go/recoder_grpc"
	"github.com/xaionaro-go/streamctl/pkg/xcontext"
	"github.com/xaionaro-go/xsync"
	"google.golang.org/grpc"
)

type RecoderID uint64
type EncoderID uint64
type InputID uint64
type OutputID uint64

type GRPCServer struct {
	recoder_grpc.UnimplementedRecoderServer

	GRPCServer *grpc.Server
	IsStarted  bool

	BeltLocker xsync.Mutex
	Belt       *belt.Belt

	RecoderLocker xsync.Mutex
	Recoder       map[RecoderID]*recoder.Loop
	RecoderNextID atomic.Uint64

	EncoderLocker xsync.Mutex
	Encoder       map[EncoderID]recoder.Encoder
	EncoderNextID atomic.Uint64

	InputLocker xsync.Mutex
	Input       map[InputID]*recoder.Input
	InputNextID atomic.Uint64

	OutputLocker xsync.Mutex
	Output       map[OutputID]*recoder.Output
	OutputNextID atomic.Uint64
}

func NewServer() *GRPCServer {
	srv := &GRPCServer{
		GRPCServer: grpc.NewServer(),
		Recoder:    make(map[RecoderID]*recoder.Loop),
		Input:      make(map[InputID]*recoder.Input),
		Output:     make(map[OutputID]*recoder.Output),
	}
	recoder_grpc.RegisterRecoderServer(srv.GRPCServer, srv)
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
	req *recoder_grpc.SetLoggingLevelRequest,
) (*recoder_grpc.SetLoggingLevelReply, error) {
	ctx = srv.ctx(ctx)
	srv.BeltLocker.Do(ctx, func() {
		logLevel := logLevelProtobuf2Go(req.GetLevel())
		l := logger.FromBelt(srv.Belt).WithLevel(logLevel)
		srv.Belt = srv.Belt.WithTool(logger.ToolID, l)
	})
	return &recoder_grpc.SetLoggingLevelReply{}, nil
}

func (srv *GRPCServer) NewInput(
	ctx context.Context,
	req *recoder_grpc.NewInputRequest,
) (*recoder_grpc.NewInputReply, error) {
	ctx = srv.ctx(ctx)
	switch path := req.Path.GetResourcePath().(type) {
	case *recoder_grpc.ResourcePath_Url:
		return srv.newInputByURL(ctx, path, req.Config)
	default:
		return nil, fmt.Errorf("the support of path type '%T' is not implemented", path)
	}
}

func (srv *GRPCServer) newInputByURL(
	ctx context.Context,
	path *recoder_grpc.ResourcePath_Url,
	_ *recoder_grpc.InputConfig,
) (*recoder_grpc.NewInputReply, error) {
	config := recoder.InputConfig{}
	input, err := recoder.NewInputFromURL(ctx, path.Url.Url, path.Url.AuthKey, config)
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
	return &recoder_grpc.NewInputReply{
		Id: uint64(inputID),
	}, nil
}

func (srv *GRPCServer) CloseInput(
	ctx context.Context,
	req *recoder_grpc.CloseInputRequest,
) (*recoder_grpc.CloseInputReply, error) {
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
	return &recoder_grpc.CloseInputReply{}, nil
}

func (srv *GRPCServer) NewOutput(
	ctx context.Context,
	req *recoder_grpc.NewOutputRequest,
) (*recoder_grpc.NewOutputReply, error) {
	ctx = srv.ctx(ctx)
	switch path := req.Path.GetResourcePath().(type) {
	case *recoder_grpc.ResourcePath_Url:
		return srv.newOutputByURL(ctx, path, req.Config)
	default:
		return nil, fmt.Errorf("the support of path type '%T' is not implemented", path)
	}
}

func (srv *GRPCServer) newOutputByURL(
	ctx context.Context,
	path *recoder_grpc.ResourcePath_Url,
	_ *recoder_grpc.OutputConfig,
) (*recoder_grpc.NewOutputReply, error) {
	config := recoder.OutputConfig{}
	output, err := recoder.NewOutputFromURL(ctx, path.Url.Url, path.Url.AuthKey, config)
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
	return &recoder_grpc.NewOutputReply{
		Id: uint64(outputID),
	}, nil
}

func (srv *GRPCServer) CloseOutput(
	ctx context.Context,
	req *recoder_grpc.CloseOutputRequest,
) (*recoder_grpc.CloseOutputReply, error) {
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
	return &recoder_grpc.CloseOutputReply{}, nil
}

func (srv *GRPCServer) NewRecoder(
	ctx context.Context,
	req *recoder_grpc.NewRecoderRequest,
) (*recoder_grpc.NewRecoderReply, error) {
	ctx = srv.ctx(ctx)
	recoderInstance := recoder.NewLoop()
	recoderID := xsync.DoR1(ctx, &srv.RecoderLocker, func() RecoderID {
		recoderID := RecoderID(srv.RecoderNextID.Add(1))
		srv.Recoder[recoderID] = recoderInstance
		return recoderID
	})
	return &recoder_grpc.NewRecoderReply{
		Id: uint64(recoderID),
	}, nil
}

func (srv *GRPCServer) NewEncoder(
	ctx context.Context,
	req *recoder_grpc.NewEncoderRequest,
) (*recoder_grpc.NewEncoderReply, error) {
	ctx = srv.ctx(ctx)
	encoderInstance := recoder.NewEncoderCopy()
	encoderID := xsync.DoR1(ctx, &srv.EncoderLocker, func() EncoderID {
		encoderID := EncoderID(srv.EncoderNextID.Add(1))
		srv.Encoder[encoderID] = encoderInstance
		return encoderID
	})
	return &recoder_grpc.NewEncoderReply{
		Id: uint64(encoderID),
	}, nil
}

func (srv *GRPCServer) GetRecoderStats(
	ctx context.Context,
	req *recoder_grpc.GetRecoderStatsRequest,
) (*recoder_grpc.GetRecoderStatsReply, error) {
	recoderID := RecoderID(req.GetRecoderID())
	recoder := xsync.DoR1(ctx, &srv.RecoderLocker, func() *recoder.Loop {
		return srv.Recoder[recoderID]
	})
	return &recoder_grpc.GetRecoderStatsReply{
		BytesCountRead:  recoder.LoopStats.BytesCountRead.Load(),
		BytesCountWrote: recoder.LoopStats.BytesCountWrote.Load(),
	}, nil
}

func (srv *GRPCServer) StartRecoding(
	ctx context.Context,
	req *recoder_grpc.StartRecodingRequest,
) (*recoder_grpc.StartRecodingReply, error) {
	ctx = srv.ctx(ctx)

	recoderID := RecoderID(req.GetRecoderID())
	encoderID := EncoderID(req.GetEncoderID())
	inputID := InputID(req.GetInputID())
	outputID := OutputID(req.GetOutputID())

	srv.RecoderLocker.ManualLock(ctx)
	srv.RecoderLocker.ManualLock(ctx)
	srv.InputLocker.ManualLock(ctx)
	srv.OutputLocker.ManualLock(ctx)
	defer srv.RecoderLocker.ManualUnlock(ctx)
	defer srv.RecoderLocker.ManualUnlock(ctx)
	defer srv.InputLocker.ManualUnlock(ctx)
	defer srv.OutputLocker.ManualUnlock(ctx)

	rec := srv.Recoder[recoderID]
	if rec == nil {
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
	encoder := srv.Encoder[encoderID]
	if encoder == nil {
		return nil, fmt.Errorf("the encoder with ID '%v' does not exist", encoderID)
	}

	if err := rec.AddInput(ctx, input); err != nil {
		return nil, fmt.Errorf("unable to add the input to the recoder: %w", err)
	}
	if err := rec.AddOutput(ctx, output); err != nil {
		return nil, fmt.Errorf("unable to add the output to the recoder: %w", err)
	}

	err := rec.StartAndWait(xcontext.DetachDone(ctx), encoder)
	if err != nil {
		return nil, fmt.Errorf("unable to start recoding")
	}

	return &recoder_grpc.StartRecodingReply{}, nil
}

func (srv *GRPCServer) RecodingEndedChan(
	req *recoder_grpc.RecodingEndedChanRequest,
	streamSrv recoder_grpc.Recoder_RecodingEndedChanServer,
) (_ret error) {
	ctx := srv.ctx(streamSrv.Context())
	recoderID := RecoderID(req.GetRecoderID())

	logger.Tracef(ctx, "RecodingEndedChan(%v)", recoderID)
	defer func() { logger.Tracef(ctx, "/RecodingEndedChan(%v): %v", recoderID, _ret) }()

	recoder := xsync.DoR1(ctx, &srv.RecoderLocker, func() *recoder.Loop {
		return srv.Recoder[recoderID]
	})

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-recoder.WaiterChan:
	}

	return streamSrv.Send(&recoder_grpc.RecodingEndedChanReply{})
}
