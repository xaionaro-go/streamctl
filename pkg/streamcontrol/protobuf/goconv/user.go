package goconv

import (
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/go/streamcontrol_grpc"
)

func UserGo2GRPC(
	user streamcontrol.User,
) *streamcontrol_grpc.User {
	return &streamcontrol_grpc.User{
		Id:   string(user.ID),
		Slug: user.Slug,
		Name: user.Name,
	}
}

func UserGRPC2Go(
	user *streamcontrol_grpc.User,
) streamcontrol.User {
	return streamcontrol.User{
		ID:   streamcontrol.UserID(user.GetId()),
		Slug: user.GetSlug(),
		Name: user.GetName(),
	}
}
