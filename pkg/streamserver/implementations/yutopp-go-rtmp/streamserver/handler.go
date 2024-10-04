package streamserver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"sync/atomic"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/pkg/errors"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
	flvtag "github.com/yutopp/go-flv/tag"
	"github.com/yutopp/go-rtmp"
	rtmpmsg "github.com/yutopp/go-rtmp/message"
)

var _ rtmp.Handler = (*Handler)(nil)

// Handler is an RTMP connection handler
type Handler struct {
	xsync.Mutex
	rtmp.DefaultHandler
	relayService *RelayService

	isClosed   atomic.Bool
	closedChan chan struct{}

	conn *rtmp.Conn
	pub  *Pub
	sub  *Sub
}

func NewHandler(relayService *RelayService) *Handler {
	return &Handler{
		relayService: relayService,
		closedChan:   make(chan struct{}),
	}
}

func (h *Handler) OnServe(conn *rtmp.Conn) {
	ctx := context.TODO()
	h.Mutex.Do(ctx, func() {
		if h.isClosed.Load() {
			logger.Errorf(ctx, "an attempt to call OnServe on a closed Handler")
			return
		}
		h.conn = conn
	})
}

func (h *Handler) OnConnect(timestamp uint32, cmd *rtmpmsg.NetConnectionConnect) error {
	log.Printf("OnConnect: %#v", cmd)

	// TODO: check app name to distinguish stream names per apps
	// cmd.Command.App

	return nil
}

func (h *Handler) OnCreateStream(timestamp uint32, cmd *rtmpmsg.NetConnectionCreateStream) error {
	log.Printf("OnCreateStream: %#v", cmd)
	return nil
}

func (h *Handler) OnPublish(_ *rtmp.StreamContext, timestamp uint32, cmd *rtmpmsg.NetStreamPublish) error {
	log.Printf("OnPublish: %#v", cmd)

	if h.sub != nil {
		return errors.New("Cannot publish to this stream")
	}

	// (example) Reject a connection when PublishingName is empty
	if cmd.PublishingName == "" {
		return errors.New("PublishingName is empty")
	}

	pubsub, err := h.relayService.NewPubsub(types.AppKey(cmd.PublishingName), h)
	if err != nil {
		return errors.Wrap(err, "Failed to create pubsub")
	}

	pub := pubsub.Pub()
	ctx := context.TODO()
	return xsync.DoR1(ctx, &h.Mutex, func() error {
		if h.isClosed.Load() {
			return fmt.Errorf("the handler is closed")
		}
		h.pub = pub
		return nil
	})
}

func (h *Handler) OnPlay(rtmpctx *rtmp.StreamContext, timestamp uint32, cmd *rtmpmsg.NetStreamPlay) error {
	if h.sub != nil {
		return errors.New("Cannot play on this stream")
	}

	pubsub := h.relayService.GetPubsub(types.AppKey(cmd.StreamName))
	if pubsub == nil {
		return fmt.Errorf("stream '%s' is not found", cmd.StreamName)
	}

	ctx := context.TODO()
	return xsync.DoR1(ctx, &h.Mutex, func() error {
		if h.isClosed.Load() {
			return fmt.Errorf("the handler is closed")
		}
		h.sub = pubsub.Sub(h.conn, onEventCallback(h.conn, rtmpctx.StreamID))
		return nil
	})
}

func (h *Handler) OnSetDataFrame(timestamp uint32, data *rtmpmsg.NetStreamSetDataFrame) error {
	r := bytes.NewReader(data.Payload)

	var script flvtag.ScriptData
	if err := flvtag.DecodeScriptData(r, &script); err != nil {
		log.Printf("Failed to decode script data: Err = %+v", err)
		return nil // ignore
	}

	log.Printf("SetDataFrame: Script = %#v", script)

	pub := h.pub
	if h.isClosed.Load() {
		return fmt.Errorf("the handler is closed")
	}

	_ = pub.Publish(&flvtag.FlvTag{
		TagType:   flvtag.TagTypeScriptData,
		Timestamp: timestamp,
		Data:      &script,
	})

	return nil
}

func (h *Handler) OnAudio(timestamp uint32, payload io.Reader) error {
	var audio flvtag.AudioData
	if err := flvtag.DecodeAudioData(payload, &audio); err != nil {
		return err
	}

	flvBody := new(bytes.Buffer)
	if _, err := io.Copy(flvBody, audio.Data); err != nil {
		return err
	}
	audio.Data = flvBody

	pub := h.pub
	if h.isClosed.Load() {
		return fmt.Errorf("the handler is closed")
	}

	_ = pub.Publish(&flvtag.FlvTag{
		TagType:   flvtag.TagTypeAudio,
		Timestamp: timestamp,
		Data:      &audio,
	})

	return nil
}

func (h *Handler) OnVideo(timestamp uint32, payload io.Reader) error {
	var video flvtag.VideoData
	if err := flvtag.DecodeVideoData(payload, &video); err != nil {
		return err
	}

	// Need deep copy because payload will be recycled
	flvBody := new(bytes.Buffer)
	if _, err := io.Copy(flvBody, video.Data); err != nil {
		return err
	}
	video.Data = flvBody

	pub := h.pub
	if h.isClosed.Load() {
		return fmt.Errorf("the handler is closed")
	}

	_ = pub.Publish(&flvtag.FlvTag{
		TagType:   flvtag.TagTypeVideo,
		Timestamp: timestamp,
		Data:      &video,
	})

	return nil
}

func (h *Handler) OnClose() {
	logger.Default().Debugf("OnClose")
	defer logger.Default().Debugf("/OnClose")

	ctx := context.TODO()
	h.Mutex.Do(ctx, func() {
		if h.isClosed.Load() {
			logger.Debugf(ctx, "OnClose on an already closed Handler")
			return
		}
		close(h.closedChan)

		pub, sub := h.pub, h.sub
		observability.Go(ctx, func() {
			if pub != nil {
				_ = pub.Close()
			}
			if sub != nil {
				_ = sub.Close()
			}
		})

		h.isClosed.Store(true)
	})
}

func (h *Handler) ClosedChan() <-chan struct{} {
	return h.closedChan
}
