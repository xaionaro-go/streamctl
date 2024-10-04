package streamserver

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/experimental/metrics"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/yutopp-go-rtmp/streamforward"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
	flvtag "github.com/yutopp/go-flv/tag"
)

type Pubsub struct {
	srv              *RelayService
	name             types.AppKey
	publisherHandler *Handler

	pub *Pub

	nextSubID uint64
	subs      map[uint64]*Sub

	m xsync.Mutex
}

func NewPubsub(srv *RelayService, name types.AppKey, publisherHandler *Handler) *Pubsub {
	return &Pubsub{
		publisherHandler: publisherHandler,
		srv:              srv,
		name:             name,

		subs: map[uint64]*Sub{},
	}
}

func (pb *Pubsub) PublisherHandler() *Handler {
	return pb.publisherHandler
}

func (pb *Pubsub) Name() types.AppKey {
	return pb.name
}

func (pb *Pubsub) Deregister() error {
	logger.Default().Debugf("Pub.Deregister")
	defer logger.Default().Debugf("/Pub.Deregister")

	ctx := context.TODO()
	return xsync.DoR1(ctx, &pb.m, pb.deregister)
}

func (pb *Pubsub) deregister() error {
	subs := pb.subs
	if subs != nil {
		for l := range pb.subs {
			delete(pb.subs, l)
		}
		observability.GoSafe(context.TODO(), func() {
			for _, sub := range subs {
				_ = sub.Close()
			}
		})
	}

	var result *multierror.Error
	result = multierror.Append(result, pb.srv.removePubsub(pb.name))
	h := pb.publisherHandler
	if h != nil {
		pb.publisherHandler = nil
		result = multierror.Append(result, h.conn.Close())
	}
	return result.ErrorOrNil()
}

func (pb *Pubsub) Pub() *Pub {
	pub := &Pub{
		pb: pb,
	}
	pb.pub = pub
	return pub
}

const sendQueueLength = 2 * 60 * 10 // presumably it will give about 10 seconds of queue: 2 tracks * 60FPS * 30 seconds

func (pb *Pubsub) Sub(connCloser io.Closer, eventCallback func(context.Context, *flvtag.FlvTag) error) *Sub {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &pb.m, func() *Sub {
		subID := pb.nextSubID
		ctx, cancelFn := context.WithCancel(ctx)
		sub := &Sub{
			connCloser:    connCloser,
			pubSub:        pb,
			subID:         subID,
			eventCallback: eventCallback,
			closedChan:    make(chan struct{}),
			sendQueue:     make(chan *flvtag.FlvTag, sendQueueLength),
			cancelFunc:    cancelFn,
		}
		observability.Go(ctx, func() { sub.senderLoop(ctx) })

		pb.nextSubID++
		pb.subs[subID] = sub
		return sub
	})
}

func (pb *Pubsub) RemoveSub(s *Sub) {
	ctx := context.TODO()
	pb.m.Do(ctx, func() {
		delete(pb.subs, s.subID)
	})
}

type Pub struct {
	m  xsync.Mutex
	pb *Pubsub

	aacSeqHeaderBeforeKey *flvtag.FlvTag
	avcSeqHeaderBeforeKey *flvtag.FlvTag
	aacSeqHeaderAfterKey  *flvtag.FlvTag
	avcSeqHeaderAfterKey  *flvtag.FlvTag
	lastKeyFrame          *flvtag.FlvTag
	sincePrevKeyFrame     []*flvtag.FlvTag
}

func (p *Pub) InitSub(sub *Sub) bool {
	hasToInit := false
	sub.initOnce.Do(func() {
		hasToInit = true
		ctx := context.TODO()
		logger.Debugf(ctx, "initializing sub %p", sub)
		defer logger.Debugf(ctx, "/initializing sub %p", sub)

		initTags := xsync.DoR1(ctx, &p.m, func() []*flvtag.FlvTag {
			initTags := make([]*flvtag.FlvTag, 0, len(p.sincePrevKeyFrame)+3)
			if p.lastKeyFrame != nil {
				if p.avcSeqHeaderBeforeKey != nil {
					tag := cloneView(p.avcSeqHeaderBeforeKey)
					initTags = append(initTags, tag)
				}
				tag := cloneView(p.lastKeyFrame)
				initTags = append(initTags, tag)
				if p.aacSeqHeaderBeforeKey != nil {
					tag := cloneView(p.aacSeqHeaderBeforeKey)
					initTags = append(initTags, tag)
				}
			} else {
				if p.avcSeqHeaderAfterKey != nil {
					tag := cloneView(p.avcSeqHeaderAfterKey)
					initTags = append(initTags, tag)
				}
				if p.aacSeqHeaderAfterKey != nil {
					tag := cloneView(p.aacSeqHeaderAfterKey)
					initTags = append(initTags, tag)
				}
			}
			for _, tag := range p.sincePrevKeyFrame {
				initTags = append(initTags, cloneView(tag))
			}
			return initTags
		})

		if len(initTags) == 0 {
			return
		}

		for _, tag := range initTags {
			sub.Submit(ctx, tag)
		}
	})
	return hasToInit
}

// TODO: Should check codec types and so on.
// In this example, checks only sequence headers and assume that AAC and AVC.
func (p *Pub) Publish(flv *flvtag.FlvTag) error {
	ctx := context.TODO()
	subs := xsync.DoR1(xsync.WithNoLogging(ctx, true), &p.pb.m, func() map[uint64]*Sub {
		return p.pb.subs
	})

	addTagAfterKeyFrame := func() {
		if p.sincePrevKeyFrame == nil {
			return
		}
		tag := cloneView(flv)
		p.sincePrevKeyFrame = append(p.sincePrevKeyFrame, tag)
	}

	switch d := flv.Data.(type) {
	case *flvtag.ScriptData:
		p.m.Do(ctx, addTagAfterKeyFrame)

	case *flvtag.AudioData:
		switch {
		case d.AACPacketType == flvtag.AACPacketTypeSequenceHeader:
			logger.Tracef(ctx, "got a new AACPacketTypeSequenceHeader")
			p.m.Do(ctx, func() {
				p.aacSeqHeaderAfterKey = cloneView(flv)
			})
		}
		p.m.Do(ctx, addTagAfterKeyFrame)

	case *flvtag.VideoData:
		switch {
		case d.AVCPacketType == flvtag.AVCPacketTypeSequenceHeader:
			logger.Tracef(ctx, "got a new AVCPacketTypeSequenceHeader")
			p.m.Do(ctx, func() {
				p.avcSeqHeaderAfterKey = cloneView(flv)
				addTagAfterKeyFrame()
			})
		case d.FrameType == flvtag.FrameTypeKeyFrame:
			logger.Debugf(ctx, "got a new FrameTypeKeyFrame")
			p.m.Do(ctx, func() {
				p.lastKeyFrame = cloneView(flv)
				p.sincePrevKeyFrame = p.sincePrevKeyFrame[:0]
				if p.avcSeqHeaderAfterKey != nil {
					p.avcSeqHeaderBeforeKey = p.avcSeqHeaderAfterKey
				}
				if p.aacSeqHeaderAfterKey != nil {
					p.aacSeqHeaderBeforeKey = p.aacSeqHeaderAfterKey
				}
			})
		default:
			p.m.Do(ctx, addTagAfterKeyFrame)
		}

	default:
		panic("unexpected")
	}

	for _, sub := range subs {
		if !p.InitSub(sub) {
			sub.Submit(ctx, cloneView(flv))
		}
	}
	return nil
}

func (p *Pub) Close() error {
	return p.pb.Deregister()
}

type Sub struct {
	locker     xsync.Mutex
	connCloser io.Closer
	pubSub     *Pubsub
	subID      uint64

	sendQueue chan *flvtag.FlvTag

	closed         bool
	initOnce       sync.Once
	closedChanOnce sync.Once
	closedChan     chan struct{}
	cancelFunc     context.CancelFunc

	//timestampOffset uint32
	eventCallback func(context.Context, *flvtag.FlvTag) error
}

func (s *Sub) senderLoop(
	ctx context.Context,
) {
	ctx = belt.WithField(ctx, "subscriber_id", s.subID, metrics.AllowInMetrics)

	logger.Debugf(ctx, "Sub[%d].senderLoop", s.subID)
	defer logger.Debugf(ctx, "/Sub[%d].senderLoop", s.subID)

	for {
		select {
		case <-ctx.Done():
			logger.Debugf(ctx, "Sub[%d].senderLoop: the context is closed: %v", s.subID, ctx.Err())
			return

		case tag, ok := <-s.sendQueue:
			if !ok {
				logger.Debugf(ctx, "Sub[%d].senderLoop: the queue is closed; closing the client", s.subID)
				s.Close()
				return
			}
			metrics.FromCtx(ctx).Count("submit_pulled").Add(1)

			logger.Tracef(ctx, "Sub[%d].senderLoop: received a tag, processing", s.subID)
			err := s.onEvent(ctx, tag)
			if err != nil {
				metrics.FromCtx(ctx).Count("submit_process_error").Add(1)
				logger.Errorf(ctx, "Sub[%d].senderLoop: unable to send an FLV tag: %v", s.subID, err)
			} else {
				metrics.FromCtx(ctx).Count("submit_process_success").Add(1)
			}
		}
	}
}

func (s *Sub) Submit(
	ctx context.Context,
	flv *flvtag.FlvTag,
) {
	ctx = belt.WithField(ctx, "subscriber_id", s.subID, metrics.AllowInMetrics)
	metrics.FromCtx(ctx).Count("submit_requests").Add(1)
	select {
	case s.sendQueue <- flv:
		metrics.FromCtx(ctx).Count("submit_pushed").Add(1)
	default:
		logger.Errorf(ctx, "subscriber #%d queue is full, cannot send a tag; closing the connection, because cannot restore from this", s.subID)
		observability.Go(ctx, func() {
			s.CloseOrLog(ctx)
		})
	}
}

func (s *Sub) onEvent(
	ctx context.Context,
	flv *flvtag.FlvTag,
) error {
	if s.closed {
		return nil
	}

	/*func() {
		switch d := flv.Data.(type) {
		case *flvtag.ScriptData:
			return
		case *flvtag.AudioData:
			if d.AACPacketType == flvtag.AACPacketTypeSequenceHeader {
				return
			}
		case *flvtag.VideoData:
		}
		flv.Timestamp += s.timestampOffset
	}()
	*/
	return s.eventCallback(ctx, flv)
}

func (s *Sub) CloseOrLog(
	ctx context.Context,
) {
	err := s.Close()
	if err != nil {
		logger.Errorf(ctx, "unable to close subscriber #%d: %v", s.subID, err)
	}
}

func (s *Sub) Close() error {
	if s == nil {
		return nil
	}
	logger.Default().Debugf("Sub.Close")
	defer logger.Default().Debugf("/Sub.Close")
	s.cancelFunc()

	ctx := context.TODO()
	return xsync.DoR1(ctx, &s.locker, func() error {
		var result *multierror.Error

		connCloser := s.connCloser
		if connCloser != nil {
			s.connCloser = nil
			err := connCloser.Close()
			result = multierror.Append(result, err)
		}
		s.closedChanOnce.Do(func() { close(s.closedChan) })
		if s.closed {
			return result.ErrorOrNil()
		}
		s.closed = true
		s.pubSub.RemoveSub(s)
		return result.ErrorOrNil()
	})
}

func (s *Sub) ClosedChan() <-chan struct{} {
	return s.closedChan
}

func (s *Sub) Wait() {
	<-s.ClosedChan()
}

func cloneView(flv *flvtag.FlvTag) *flvtag.FlvTag {
	// Need to clone the view because Binary data will be consumed
	v := *flv

	switch flv.Data.(type) {
	case *flvtag.AudioData:
		dCloned := *v.Data.(*flvtag.AudioData)
		v.Data = &dCloned

		dCloned.Data = bytes.NewBuffer(dCloned.Data.(*bytes.Buffer).Bytes())

	case *flvtag.VideoData:
		dCloned := *v.Data.(*flvtag.VideoData)
		v.Data = &dCloned

		dCloned.Data = bytes.NewBuffer(dCloned.Data.(*bytes.Buffer).Bytes())

	case *flvtag.ScriptData:
		dCloned := *v.Data.(*flvtag.ScriptData)
		v.Data = &dCloned

	default:
		panic("unreachable")
	}

	return &v
}

type pubsubAdapter struct {
	*Pubsub
}

var _ streamforward.Pubsub = (*pubsubAdapter)(nil)

func (pubsub *pubsubAdapter) Sub(
	conn io.Closer,
	callback func(ctx context.Context, flv *flvtag.FlvTag) error,
) streamforward.Sub {
	return pubsub.Pubsub.Sub(conn, callback)
}
