package ffstreamserver

import (
	"context"
	"sync"

	"github.com/xaionaro-go/streamctl/pkg/ffstream"
	"github.com/xaionaro-go/streamctl/pkg/ffstreamserver/grpc/go/ffstream_grpc"
	"github.com/xaionaro-go/streamctl/pkg/ffstreamserver/grpc/goconv"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder"
	"github.com/xaionaro-go/streamctl/pkg/xcontext"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GRPCServer struct {
	ffstream_grpc.UnimplementedFFStreamServer
	FFStream         *ffstream.FFStream
	locker           sync.Mutex
	stopRecodingFunc context.CancelFunc
}

func NewGRPCServer(ffStream *ffstream.FFStream) *GRPCServer {
	return &GRPCServer{
		FFStream: ffStream,
	}
}

func (srv *GRPCServer) SetLoggingLevel(
	ctx context.Context,
	req *ffstream_grpc.SetLoggingLevelRequest,
) (*ffstream_grpc.SetLoggingLevelReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SetLoggingLevel not implemented, yet")
}

func (srv *GRPCServer) AddInput(
	ctx context.Context,
	req *ffstream_grpc.AddInputRequest,
) (*ffstream_grpc.AddInputReply, error) {
	input, err := recoder.NewInputFromURL(ctx, req.GetUrl(), "", recoder.InputConfig{
		CustomOptions: goconv.CustomOptionsFromGRPC(req.GetCustomOptions()),
	})
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "unable to open the input: %v", err)
	}

	err = srv.FFStream.AddInput(ctx, input)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "unable to add the input: %v", err)
	}

	return &ffstream_grpc.AddInputReply{}, nil
}

func (srv *GRPCServer) AddOutput(
	ctx context.Context,
	req *ffstream_grpc.AddOutputRequest,
) (*ffstream_grpc.AddOutputReply, error) {
	output, err := recoder.NewOutputFromURL(ctx, req.GetUrl(), "", recoder.OutputConfig{
		CustomOptions: goconv.CustomOptionsFromGRPC(req.GetCustomOptions()),
	})
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "unable to open the output: %v", err)
	}

	err = srv.FFStream.AddOutput(ctx, output)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "unable to add the output: %v", err)
	}

	return &ffstream_grpc.AddOutputReply{}, nil
}

func (srv *GRPCServer) ConfigureEncoder(
	ctx context.Context,
	req *ffstream_grpc.ConfigureEncoderRequest,
) (*ffstream_grpc.ConfigureEncoderReply, error) {
	err := srv.FFStream.ConfigureEncoder(ctx, goconv.EncoderConfigFromGRPC(req))
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "unable to configure the encoder: %v", err)
	}
	return &ffstream_grpc.ConfigureEncoderReply{}, nil
}

func (srv *GRPCServer) Start(
	ctx context.Context,
	req *ffstream_grpc.StartRequest,
) (*ffstream_grpc.StartReply, error) {
	srv.locker.Lock()
	defer srv.locker.Unlock()
	if srv.stopRecodingFunc != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "recoding is already started")
	}
	ctx, cancelFn := context.WithCancel(xcontext.DetachDone(ctx))
	err := srv.FFStream.Start(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "unable to start the recoding: %v", err)
	}
	srv.stopRecodingFunc = cancelFn
	return &ffstream_grpc.StartReply{}, nil
}

func (srv *GRPCServer) GetEncoderStats(
	ctx context.Context,
	req *ffstream_grpc.GetEncoderStatsRequest,
) (*ffstream_grpc.GetEncoderStatsReply, error) {
	stats := srv.FFStream.GetEncoderStats(ctx)
	if stats == nil {
		return nil, status.Errorf(codes.Unknown, "unable to get the statistics")
	}

	return goconv.EncoderStatsToGRPC(stats), nil
}

func (srv *GRPCServer) GetOutputSRTStats(
	ctx context.Context,
	req *ffstream_grpc.GetOutputSRTStatsRequest,
) (*ffstream_grpc.GetOutputSRTStatsReply, error) {
	stats, err := srv.FFStream.GetOutputSRTStats(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "unable to get the output SRT statistics: %v", err)
	}

	return goconv.OutputSRTStatsToGRPC(stats), nil
}

func (srv *GRPCServer) WaitChan(
	req *ffstream_grpc.WaitRequest,
	reqSrv ffstream_grpc.FFStream_WaitChanServer,
) error {
	ctx := reqSrv.Context()
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	err := srv.FFStream.Wait(ctx)
	if err != nil {
		return status.Errorf(codes.Unknown, "unable to wait for the end: %v", err)
	}
	return reqSrv.Send(&ffstream_grpc.WaitReply{})
}

func (srv *GRPCServer) End(
	ctx context.Context,
	req *ffstream_grpc.EndRequest,
) (*ffstream_grpc.EndReply, error) {
	srv.locker.Lock()
	defer srv.locker.Unlock()
	if srv.stopRecodingFunc == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "recoding is not started")
	}
	srv.stopRecodingFunc()
	srv.stopRecodingFunc = nil
	return &ffstream_grpc.EndReply{}, nil
}
