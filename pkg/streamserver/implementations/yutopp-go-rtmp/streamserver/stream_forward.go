package streamserver

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"sync/atomic"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/xlogger"
	flvtag "github.com/yutopp/go-flv/tag"
	"github.com/yutopp/go-rtmp"
	rtmpmsg "github.com/yutopp/go-rtmp/message"
)

const (
	chunkSize = 128
)

type ActiveStreamForwarding struct {
	StreamID      types.StreamID
	DestinationID types.DestinationID
	Client        *rtmp.ClientConn
	OutStream     *rtmp.Stream
	Sub           *Sub
	ReadCount     atomic.Uint64
	WriteCount    atomic.Uint64
}

func newActiveStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	dstID types.DestinationID,
	pubSub *Pubsub,
	urlString string,
) (*ActiveStreamForwarding, error) {
	urlParsed, err := url.Parse(urlString)
	if err != nil {
		return nil, fmt.Errorf("unable to parse URL '%s': %w", urlString, err)
	}
	client, err := rtmp.Dial("rtmp", urlParsed.Host, &rtmp.ConnConfig{
		Logger: xlogger.LogrusFieldLoggerFromCtx(ctx),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create a client to '%s': %w", urlString, err)
	}
	defer client.Close()
	logger.Tracef(ctx, "created a client to '%s'", urlString)

	if err := client.Connect(nil); err != nil {
		return nil, fmt.Errorf("unable to connect to '%s': %w", urlString, err)
	}
	logger.Tracef(ctx, "connected to '%s'", urlString)

	fwd := &ActiveStreamForwarding{
		StreamID:      streamID,
		DestinationID: dstID,
		Client:        client,
	}
	fwd.OutStream, err = client.CreateStream(nil, chunkSize)
	if err != nil {
		return nil, fmt.Errorf("unable to create a stream to '%s': %w", urlString, err)
	}

	if err := fwd.OutStream.Publish(&rtmpmsg.NetStreamPublish{
		PublishingName: pubSub.Name(),
		PublishingType: "live",
	}); err != nil {
		return nil, fmt.Errorf("unable to send the Publish message to '%s': %w", urlString, err)
	}

	logger.Tracef(ctx, "starting publishing to '%s'", urlString)

	fwd.Sub = pubSub.Sub()
	fwd.Sub.eventCallback = func(flv *flvtag.FlvTag) error {
		var buf bytes.Buffer

		switch d := flv.Data.(type) {
		case *flvtag.AudioData:
			// Consume flv payloads (d)
			if err := flvtag.EncodeAudioData(&buf, d); err != nil {
				return err
			}

			fwd.WriteCount.Add(uint64(buf.Len()))

			// TODO: Fix these values
			chunkStreamID := 5
			return fwd.OutStream.Write(chunkStreamID, flv.Timestamp, &rtmpmsg.AudioMessage{
				Payload: &buf,
			})

		case *flvtag.VideoData:
			// Consume flv payloads (d)
			if err := flvtag.EncodeVideoData(&buf, d); err != nil {
				return err
			}

			fwd.WriteCount.Add(uint64(buf.Len()))

			// TODO: Fix these values
			chunkStreamID := 6
			return fwd.OutStream.Write(chunkStreamID, flv.Timestamp, &rtmpmsg.VideoMessage{
				Payload: &buf,
			})

		case *flvtag.ScriptData:
			// Consume flv payloads (d)
			if err := flvtag.EncodeScriptData(&buf, d); err != nil {
				return err
			}

			fwd.WriteCount.Add(uint64(buf.Len()))

			// TODO: hide these implementation
			amdBuf := new(bytes.Buffer)
			amfEnc := rtmpmsg.NewAMFEncoder(amdBuf, rtmpmsg.EncodingTypeAMF0)
			if err := rtmpmsg.EncodeBodyAnyValues(amfEnc, &rtmpmsg.NetStreamSetDataFrame{
				Payload: buf.Bytes(),
			}); err != nil {
				return err
			}

			// TODO: Fix these values
			chunkStreamID := 8
			return fwd.OutStream.Write(chunkStreamID, flv.Timestamp, &rtmpmsg.DataMessage{
				Name:     "@setDataFrame", // TODO: fix
				Encoding: rtmpmsg.EncodingTypeAMF0,
				Body:     amdBuf,
			})

		default:
			panic("unreachable")
		}
	}

	return fwd, nil
}

func (fwd *ActiveStreamForwarding) Close() error {
	var result *multierror.Error
	result = multierror.Append(result, fwd.Sub.Close())
	result = multierror.Append(result, fwd.Client.Close())
	return result.ErrorOrNil()
}
