// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.2.0
// - protoc             v3.21.12
// source: player.proto

package player_grpc

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

// PlayerClient is the client API for Player service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type PlayerClient interface {
	Open(ctx context.Context, in *OpenRequest, opts ...grpc.CallOption) (*OpenReply, error)
	SetupForStreaming(ctx context.Context, in *SetupForStreamingRequest, opts ...grpc.CallOption) (*SetupForStreamingReply, error)
	ProcessTitle(ctx context.Context, in *ProcessTitleRequest, opts ...grpc.CallOption) (*ProcessTitleReply, error)
	GetLink(ctx context.Context, in *GetLinkRequest, opts ...grpc.CallOption) (*GetLinkReply, error)
	EndChan(ctx context.Context, in *EndChanRequest, opts ...grpc.CallOption) (Player_EndChanClient, error)
	IsEnded(ctx context.Context, in *IsEndedRequest, opts ...grpc.CallOption) (*IsEndedReply, error)
	GetPosition(ctx context.Context, in *GetPositionRequest, opts ...grpc.CallOption) (*GetPositionReply, error)
	GetLength(ctx context.Context, in *GetLengthRequest, opts ...grpc.CallOption) (*GetLengthReply, error)
	GetSpeed(ctx context.Context, in *GetSpeedRequest, opts ...grpc.CallOption) (*GetSpeedReply, error)
	SetSpeed(ctx context.Context, in *SetSpeedRequest, opts ...grpc.CallOption) (*SetSpeedReply, error)
	GetPause(ctx context.Context, in *GetPauseRequest, opts ...grpc.CallOption) (*GetPauseReply, error)
	SetPause(ctx context.Context, in *SetPauseRequest, opts ...grpc.CallOption) (*SetPauseReply, error)
	Stop(ctx context.Context, in *StopRequest, opts ...grpc.CallOption) (*StopReply, error)
	Close(ctx context.Context, in *CloseRequest, opts ...grpc.CallOption) (*CloseReply, error)
}

type playerClient struct {
	cc grpc.ClientConnInterface
}

func NewPlayerClient(cc grpc.ClientConnInterface) PlayerClient {
	return &playerClient{cc}
}

func (c *playerClient) Open(ctx context.Context, in *OpenRequest, opts ...grpc.CallOption) (*OpenReply, error) {
	out := new(OpenReply)
	err := c.cc.Invoke(ctx, "/player.Player/Open", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *playerClient) SetupForStreaming(ctx context.Context, in *SetupForStreamingRequest, opts ...grpc.CallOption) (*SetupForStreamingReply, error) {
	out := new(SetupForStreamingReply)
	err := c.cc.Invoke(ctx, "/player.Player/SetupForStreaming", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *playerClient) ProcessTitle(ctx context.Context, in *ProcessTitleRequest, opts ...grpc.CallOption) (*ProcessTitleReply, error) {
	out := new(ProcessTitleReply)
	err := c.cc.Invoke(ctx, "/player.Player/ProcessTitle", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *playerClient) GetLink(ctx context.Context, in *GetLinkRequest, opts ...grpc.CallOption) (*GetLinkReply, error) {
	out := new(GetLinkReply)
	err := c.cc.Invoke(ctx, "/player.Player/GetLink", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *playerClient) EndChan(ctx context.Context, in *EndChanRequest, opts ...grpc.CallOption) (Player_EndChanClient, error) {
	stream, err := c.cc.NewStream(ctx, &Player_ServiceDesc.Streams[0], "/player.Player/EndChan", opts...)
	if err != nil {
		return nil, err
	}
	x := &playerEndChanClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type Player_EndChanClient interface {
	Recv() (*EndChanReply, error)
	grpc.ClientStream
}

type playerEndChanClient struct {
	grpc.ClientStream
}

func (x *playerEndChanClient) Recv() (*EndChanReply, error) {
	m := new(EndChanReply)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *playerClient) IsEnded(ctx context.Context, in *IsEndedRequest, opts ...grpc.CallOption) (*IsEndedReply, error) {
	out := new(IsEndedReply)
	err := c.cc.Invoke(ctx, "/player.Player/IsEnded", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *playerClient) GetPosition(ctx context.Context, in *GetPositionRequest, opts ...grpc.CallOption) (*GetPositionReply, error) {
	out := new(GetPositionReply)
	err := c.cc.Invoke(ctx, "/player.Player/GetPosition", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *playerClient) GetLength(ctx context.Context, in *GetLengthRequest, opts ...grpc.CallOption) (*GetLengthReply, error) {
	out := new(GetLengthReply)
	err := c.cc.Invoke(ctx, "/player.Player/GetLength", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *playerClient) GetSpeed(ctx context.Context, in *GetSpeedRequest, opts ...grpc.CallOption) (*GetSpeedReply, error) {
	out := new(GetSpeedReply)
	err := c.cc.Invoke(ctx, "/player.Player/GetSpeed", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *playerClient) SetSpeed(ctx context.Context, in *SetSpeedRequest, opts ...grpc.CallOption) (*SetSpeedReply, error) {
	out := new(SetSpeedReply)
	err := c.cc.Invoke(ctx, "/player.Player/SetSpeed", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *playerClient) GetPause(ctx context.Context, in *GetPauseRequest, opts ...grpc.CallOption) (*GetPauseReply, error) {
	out := new(GetPauseReply)
	err := c.cc.Invoke(ctx, "/player.Player/GetPause", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *playerClient) SetPause(ctx context.Context, in *SetPauseRequest, opts ...grpc.CallOption) (*SetPauseReply, error) {
	out := new(SetPauseReply)
	err := c.cc.Invoke(ctx, "/player.Player/SetPause", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *playerClient) Stop(ctx context.Context, in *StopRequest, opts ...grpc.CallOption) (*StopReply, error) {
	out := new(StopReply)
	err := c.cc.Invoke(ctx, "/player.Player/Stop", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *playerClient) Close(ctx context.Context, in *CloseRequest, opts ...grpc.CallOption) (*CloseReply, error) {
	out := new(CloseReply)
	err := c.cc.Invoke(ctx, "/player.Player/Close", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PlayerServer is the server API for Player service.
// All implementations must embed UnimplementedPlayerServer
// for forward compatibility
type PlayerServer interface {
	Open(context.Context, *OpenRequest) (*OpenReply, error)
	SetupForStreaming(context.Context, *SetupForStreamingRequest) (*SetupForStreamingReply, error)
	ProcessTitle(context.Context, *ProcessTitleRequest) (*ProcessTitleReply, error)
	GetLink(context.Context, *GetLinkRequest) (*GetLinkReply, error)
	EndChan(*EndChanRequest, Player_EndChanServer) error
	IsEnded(context.Context, *IsEndedRequest) (*IsEndedReply, error)
	GetPosition(context.Context, *GetPositionRequest) (*GetPositionReply, error)
	GetLength(context.Context, *GetLengthRequest) (*GetLengthReply, error)
	GetSpeed(context.Context, *GetSpeedRequest) (*GetSpeedReply, error)
	SetSpeed(context.Context, *SetSpeedRequest) (*SetSpeedReply, error)
	GetPause(context.Context, *GetPauseRequest) (*GetPauseReply, error)
	SetPause(context.Context, *SetPauseRequest) (*SetPauseReply, error)
	Stop(context.Context, *StopRequest) (*StopReply, error)
	Close(context.Context, *CloseRequest) (*CloseReply, error)
	mustEmbedUnimplementedPlayerServer()
}

// UnimplementedPlayerServer must be embedded to have forward compatible implementations.
type UnimplementedPlayerServer struct {
}

func (UnimplementedPlayerServer) Open(context.Context, *OpenRequest) (*OpenReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Open not implemented")
}
func (UnimplementedPlayerServer) SetupForStreaming(context.Context, *SetupForStreamingRequest) (*SetupForStreamingReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SetupForStreaming not implemented")
}
func (UnimplementedPlayerServer) ProcessTitle(context.Context, *ProcessTitleRequest) (*ProcessTitleReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ProcessTitle not implemented")
}
func (UnimplementedPlayerServer) GetLink(context.Context, *GetLinkRequest) (*GetLinkReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetLink not implemented")
}
func (UnimplementedPlayerServer) EndChan(*EndChanRequest, Player_EndChanServer) error {
	return status.Errorf(codes.Unimplemented, "method EndChan not implemented")
}
func (UnimplementedPlayerServer) IsEnded(context.Context, *IsEndedRequest) (*IsEndedReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method IsEnded not implemented")
}
func (UnimplementedPlayerServer) GetPosition(context.Context, *GetPositionRequest) (*GetPositionReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetPosition not implemented")
}
func (UnimplementedPlayerServer) GetLength(context.Context, *GetLengthRequest) (*GetLengthReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetLength not implemented")
}
func (UnimplementedPlayerServer) GetSpeed(context.Context, *GetSpeedRequest) (*GetSpeedReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetSpeed not implemented")
}
func (UnimplementedPlayerServer) SetSpeed(context.Context, *SetSpeedRequest) (*SetSpeedReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SetSpeed not implemented")
}
func (UnimplementedPlayerServer) GetPause(context.Context, *GetPauseRequest) (*GetPauseReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetPause not implemented")
}
func (UnimplementedPlayerServer) SetPause(context.Context, *SetPauseRequest) (*SetPauseReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SetPause not implemented")
}
func (UnimplementedPlayerServer) Stop(context.Context, *StopRequest) (*StopReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Stop not implemented")
}
func (UnimplementedPlayerServer) Close(context.Context, *CloseRequest) (*CloseReply, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Close not implemented")
}
func (UnimplementedPlayerServer) mustEmbedUnimplementedPlayerServer() {}

// UnsafePlayerServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to PlayerServer will
// result in compilation errors.
type UnsafePlayerServer interface {
	mustEmbedUnimplementedPlayerServer()
}

func RegisterPlayerServer(s grpc.ServiceRegistrar, srv PlayerServer) {
	s.RegisterService(&Player_ServiceDesc, srv)
}

func _Player_Open_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(OpenRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PlayerServer).Open(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/player.Player/Open",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PlayerServer).Open(ctx, req.(*OpenRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Player_SetupForStreaming_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SetupForStreamingRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PlayerServer).SetupForStreaming(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/player.Player/SetupForStreaming",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PlayerServer).SetupForStreaming(ctx, req.(*SetupForStreamingRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Player_ProcessTitle_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ProcessTitleRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PlayerServer).ProcessTitle(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/player.Player/ProcessTitle",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PlayerServer).ProcessTitle(ctx, req.(*ProcessTitleRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Player_GetLink_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetLinkRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PlayerServer).GetLink(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/player.Player/GetLink",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PlayerServer).GetLink(ctx, req.(*GetLinkRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Player_EndChan_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(EndChanRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(PlayerServer).EndChan(m, &playerEndChanServer{stream})
}

type Player_EndChanServer interface {
	Send(*EndChanReply) error
	grpc.ServerStream
}

type playerEndChanServer struct {
	grpc.ServerStream
}

func (x *playerEndChanServer) Send(m *EndChanReply) error {
	return x.ServerStream.SendMsg(m)
}

func _Player_IsEnded_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(IsEndedRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PlayerServer).IsEnded(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/player.Player/IsEnded",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PlayerServer).IsEnded(ctx, req.(*IsEndedRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Player_GetPosition_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetPositionRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PlayerServer).GetPosition(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/player.Player/GetPosition",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PlayerServer).GetPosition(ctx, req.(*GetPositionRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Player_GetLength_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetLengthRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PlayerServer).GetLength(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/player.Player/GetLength",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PlayerServer).GetLength(ctx, req.(*GetLengthRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Player_GetSpeed_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetSpeedRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PlayerServer).GetSpeed(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/player.Player/GetSpeed",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PlayerServer).GetSpeed(ctx, req.(*GetSpeedRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Player_SetSpeed_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SetSpeedRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PlayerServer).SetSpeed(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/player.Player/SetSpeed",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PlayerServer).SetSpeed(ctx, req.(*SetSpeedRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Player_GetPause_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetPauseRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PlayerServer).GetPause(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/player.Player/GetPause",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PlayerServer).GetPause(ctx, req.(*GetPauseRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Player_SetPause_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SetPauseRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PlayerServer).SetPause(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/player.Player/SetPause",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PlayerServer).SetPause(ctx, req.(*SetPauseRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Player_Stop_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StopRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PlayerServer).Stop(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/player.Player/Stop",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PlayerServer).Stop(ctx, req.(*StopRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Player_Close_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CloseRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PlayerServer).Close(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/player.Player/Close",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PlayerServer).Close(ctx, req.(*CloseRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// Player_ServiceDesc is the grpc.ServiceDesc for Player service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var Player_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "player.Player",
	HandlerType: (*PlayerServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Open",
			Handler:    _Player_Open_Handler,
		},
		{
			MethodName: "SetupForStreaming",
			Handler:    _Player_SetupForStreaming_Handler,
		},
		{
			MethodName: "ProcessTitle",
			Handler:    _Player_ProcessTitle_Handler,
		},
		{
			MethodName: "GetLink",
			Handler:    _Player_GetLink_Handler,
		},
		{
			MethodName: "IsEnded",
			Handler:    _Player_IsEnded_Handler,
		},
		{
			MethodName: "GetPosition",
			Handler:    _Player_GetPosition_Handler,
		},
		{
			MethodName: "GetLength",
			Handler:    _Player_GetLength_Handler,
		},
		{
			MethodName: "GetSpeed",
			Handler:    _Player_GetSpeed_Handler,
		},
		{
			MethodName: "SetSpeed",
			Handler:    _Player_SetSpeed_Handler,
		},
		{
			MethodName: "GetPause",
			Handler:    _Player_GetPause_Handler,
		},
		{
			MethodName: "SetPause",
			Handler:    _Player_SetPause_Handler,
		},
		{
			MethodName: "Stop",
			Handler:    _Player_Stop_Handler,
		},
		{
			MethodName: "Close",
			Handler:    _Player_Close_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "EndChan",
			Handler:       _Player_EndChan_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "player.proto",
}
