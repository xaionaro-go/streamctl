package goconv

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

func TestStreamIDFullyQualifiedFromGRPC(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		res := StreamIDFullyQualifiedFromGRPC(nil)
		require.Equal(t, streamcontrol.StreamIDFullyQualified{}, res)
	})

	t.Run("valid", func(t *testing.T) {
		in := &streamd_grpc.StreamIDFullyQualified{
			PlatformID: "youtube",
			AccountID:  "acc1",
			StreamID:   "stream1",
		}
		res := StreamIDFullyQualifiedFromGRPC(in)
		require.Equal(t, streamcontrol.PlatformID("youtube"), res.PlatformID)
		require.Equal(t, streamcontrol.AccountID("acc1"), res.AccountID)
		require.Equal(t, streamcontrol.StreamID("stream1"), res.StreamID)
	})

	t.Run("empty_platform_id_returns_zero_value", func(t *testing.T) {
		in := &streamd_grpc.StreamIDFullyQualified{
			PlatformID: "",
			AccountID:  "acc1",
			StreamID:   "stream1",
		}
		res := StreamIDFullyQualifiedFromGRPC(in)
		require.Equal(t, streamcontrol.StreamIDFullyQualified{}, res)
	})
}
