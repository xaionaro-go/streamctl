package ffstreamserver

import (
	"context"
	"sync"

	"github.com/xaionaro-go/libsrt"
	"github.com/xaionaro-go/libsrt/threadsafe"
	"github.com/xaionaro-go/recoder/libav/recoder"
	"github.com/xaionaro-go/streamctl/pkg/ffstream"
	"github.com/xaionaro-go/streamctl/pkg/ffstreamserver/grpc/go/ffstream_grpc"
	"github.com/xaionaro-go/streamctl/pkg/ffstreamserver/grpc/goconv"
	"github.com/xaionaro-go/xcontext"
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

	return &ffstream_grpc.AddInputReply{
		Id: int64(input.ID),
	}, nil
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

	return &ffstream_grpc.AddOutputReply{
		Id: uint64(output.ID),
	}, nil
}

func (srv *GRPCServer) RemoveOutput(
	ctx context.Context,
	req *ffstream_grpc.RemoveOutputRequest,
) (*ffstream_grpc.RemoveOutputReply, error) {
	err := srv.FFStream.RemoveOutput(ctx, recoder.OutputID(req.GetId()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "unable to remove the output %d: %v", req.GetId(), err)
	}

	return &ffstream_grpc.RemoveOutputReply{}, nil
}

func (srv *GRPCServer) GetEncoderConfig(
	ctx context.Context,
	req *ffstream_grpc.GetEncoderConfigRequest,
) (*ffstream_grpc.GetEncoderConfigReply, error) {
	cfg := srv.FFStream.GetEncoderConfig(ctx)
	return &ffstream_grpc.GetEncoderConfigReply{
		Config: goconv.EncoderConfigToGRPC(cfg),
	}, nil
}

func (srv *GRPCServer) SetEncoderConfig(
	ctx context.Context,
	req *ffstream_grpc.SetEncoderConfigRequest,
) (*ffstream_grpc.SetEncoderConfigReply, error) {
	err := srv.FFStream.SetEncoderConfig(ctx, goconv.EncoderConfigFromGRPC(req.GetConfig()))
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "unable to configure the encoder: %v", err)
	}
	return &ffstream_grpc.SetEncoderConfigReply{}, nil
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
		cancelFn()
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
	var stats *libsrt.Tracebstats
	err := srv.FFStream.WithSRTOutput(ctx, func(sock *threadsafe.Socket) error {
		result, err := sock.Bistats(false, true)
		if err == nil {
			stats = ptr(result.Convert())
		}
		return err
	})
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "unable to get the output SRT statistics: %v", err)
	}

	return goconv.OutputSRTStatsToGRPC(stats), nil
}

func (srv *GRPCServer) GetFlagInt(
	ctx context.Context,
	req *ffstream_grpc.GetFlagIntRequest,
) (*ffstream_grpc.GetFlagIntReply, error) {
	sockOpt, ok := goconv.SockoptIntFromGRPC(req.GetFlag())
	if !ok {
		return nil, status.Errorf(codes.Unknown, "unknown SRT socket option: %d", req.GetFlag())
	}

	var v libsrt.BlobInt
	err := srv.FFStream.WithSRTOutput(ctx, func(sock *threadsafe.Socket) error {
		return sock.Getsockflag(sockOpt, &v)
	})
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "unable to set the SRT socket option: %v", err)
	}

	return &ffstream_grpc.GetFlagIntReply{
		Value: int64(v),
	}, nil
}

func (srv *GRPCServer) SetFlagInt(
	ctx context.Context,
	req *ffstream_grpc.SetFlagIntRequest,
) (*ffstream_grpc.SetFlagIntReply, error) {
	sockOpt, ok := goconv.SockoptIntFromGRPC(req.GetFlag())
	if !ok {
		return nil, status.Errorf(codes.Unknown, "unknown SRT socket option: %d", req.GetFlag())
	}

	err := srv.FFStream.WithSRTOutput(ctx, func(sock *threadsafe.Socket) error {
		v := libsrt.BlobInt(req.GetValue())
		return sock.Setsockflag(sockOpt, &v)
	})
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "unable to set the SRT socket option: %v", err)
	}

	return &ffstream_grpc.SetFlagIntReply{}, nil
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
