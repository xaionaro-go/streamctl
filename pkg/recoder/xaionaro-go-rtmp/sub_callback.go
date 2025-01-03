package xaionarogortmp

import (
	"bytes"
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/go-rtmp"
	rtmpmsg "github.com/xaionaro-go/go-rtmp/message"
	flvtag "github.com/yutopp/go-flv/tag"
)

func (r *Recoder) subCallback(
	stream *rtmp.Stream,
) func(
	ctx context.Context,
	flv *flvtag.FlvTag,
) error {
	return func(
		ctx context.Context,
		flv *flvtag.FlvTag,
	) error {
		logger.Tracef(ctx, "flvtag == %#+v", *flv)
		var buf bytes.Buffer

		switch d := flv.Data.(type) {
		case *flvtag.AudioData:
			// Consume flv payloads (d)
			if err := flvtag.EncodeAudioData(&buf, d); err != nil {
				err = fmt.Errorf("flvtag.Data == %#+v; err == %w", *d, err)
				return err
			}

			payloadLen := uint64(buf.Len())
			r.WriteCount.Add(payloadLen)
			logger.Tracef(ctx, "flvtag.Data == %#+v; payload len == %d", *d, payloadLen)

			// TODO: Fix these values
			chunkStreamID := 5
			if err := stream.Write(ctx, chunkStreamID, flv.Timestamp, &rtmpmsg.AudioMessage{
				Payload: &buf,
			}); err != nil {
				err = fmt.Errorf("stream.Write (%T) return an error: %w", d, err)
				return err
			}

		case *flvtag.VideoData:
			// Consume flv payloads (d)
			if err := flvtag.EncodeVideoData(&buf, d); err != nil {
				err = fmt.Errorf("flvtag.Data == %#+v; err == %w", *d, err)
				return err
			}

			payloadLen := uint64(buf.Len())
			r.WriteCount.Add(payloadLen)
			logger.Tracef(ctx, "flvtag.Data == %#+v; payload len == %d", *d, payloadLen)

			// TODO: Fix these values
			chunkStreamID := 6
			if err := stream.Write(ctx, chunkStreamID, flv.Timestamp, &rtmpmsg.VideoMessage{
				Payload: &buf,
			}); err != nil {
				err = fmt.Errorf("stream.Write (%T) return an error: %w", d, err)
				return err
			}

		case *flvtag.ScriptData:
			// Consume flv payloads (d)
			if err := flvtag.EncodeScriptData(&buf, d); err != nil {
				err = fmt.Errorf("flvtag.Data == %#+v; err == %v", *d, err)
				return err
			}

			payloadLen := uint64(buf.Len())
			r.WriteCount.Add(payloadLen)
			logger.Tracef(ctx, "flvtag.Data == %#+v; payload len == %d", *d, payloadLen)

			// TODO: hide these implementation
			amdBuf := new(bytes.Buffer)
			amfEnc := rtmpmsg.NewAMFEncoder(amdBuf, rtmpmsg.EncodingTypeAMF0)
			if err := rtmpmsg.EncodeBodyAnyValues(amfEnc, &rtmpmsg.NetStreamSetDataFrame{
				Payload: buf.Bytes(),
			}); err != nil {
				err = fmt.Errorf("flvtag.Data == %#+v; payload len == %d; err == %v", *d, payloadLen, err)
				return err
			}

			// TODO: Fix these values
			chunkStreamID := 8
			if err := stream.Write(ctx, chunkStreamID, flv.Timestamp, &rtmpmsg.DataMessage{
				Name:     "@setDataFrame", // TODO: fix
				Encoding: rtmpmsg.EncodingTypeAMF0,
				Body:     amdBuf,
			}); err != nil {
				err = fmt.Errorf("stream.Write (%T) return an error: %w", d, err)
				return err
			}

		default:
			logger.Errorf(ctx, "unexpected data type: %T", flv.Data)
		}
		return nil
	}
}
