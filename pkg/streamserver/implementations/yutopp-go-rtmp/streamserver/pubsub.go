package streamserver

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
	flvtag "github.com/yutopp/go-flv/tag"
)

type Pubsub struct {
	srv              *RelayService
	name             string
	publisherHandler *Handler

	pub *Pub

	nextSubID uint64
	subs      map[uint64]*Sub

	m xsync.Mutex
}

func NewPubsub(srv *RelayService, name string, publisherHandler *Handler) *Pubsub {
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

func (pb *Pubsub) Name() string {
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
		pb.subs = nil
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

func (pb *Pubsub) Sub(connCloser io.Closer, eventCallback func(ft *flvtag.FlvTag) error) *Sub {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &pb.m, func() *Sub {
		subID := pb.nextSubID
		sub := &Sub{
			connCloser:    connCloser,
			pubSub:        pb,
			subID:         subID,
			eventCallback: eventCallback,
			closedChan:    make(chan struct{}),
		}

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

	aacSeqHeader *flvtag.FlvTag
	avcSeqHeader *flvtag.FlvTag
	lastKeyFrame *flvtag.FlvTag
}

func (p *Pub) InitSub(sub *Sub) {
	type keyTagsT struct {
		AVCSeqHeader *flvtag.FlvTag
		LastKeyFrame *flvtag.FlvTag
		AACSeqHeader *flvtag.FlvTag
	}
	ctx := context.TODO()
	keyTags := xsync.DoR1(ctx, &p.m, func() keyTagsT {
		return keyTagsT{
			AVCSeqHeader: p.avcSeqHeader,
			LastKeyFrame: p.lastKeyFrame,
			AACSeqHeader: p.aacSeqHeader,
		}
	})

	if keyTags.AVCSeqHeader != nil {
		logger.Default().Tracef("sending avcSeqHeader")
		_ = sub.onEvent(cloneView(keyTags.AVCSeqHeader))
	}
	if keyTags.LastKeyFrame != nil {
		logger.Default().Tracef("sending lastKeyFrame")
		_ = sub.onEvent(cloneView(keyTags.LastKeyFrame))
	}
	if keyTags.AACSeqHeader != nil {
		logger.Default().Tracef("sending aacSeqHeader")
		_ = sub.onEvent(cloneView(keyTags.AACSeqHeader))
	}

	sub.initialized = true
}

// TODO: Should check codec types and so on.
// In this example, checks only sequence headers and assume that AAC and AVC.
func (p *Pub) Publish(flv *flvtag.FlvTag) error {
	ctx := context.TODO()
	subs := xsync.DoR1(xsync.WithNoLogging(ctx, true), &p.pb.m, func() map[uint64]*Sub {
		return p.pb.subs
	})

	switch d := flv.Data.(type) {
	case *flvtag.ScriptData:
		for _, sub := range subs {
			_ = sub.onEvent(cloneView(flv))
		}

	case *flvtag.AudioData:
		if d.AACPacketType == flvtag.AACPacketTypeSequenceHeader {
			p.m.Do(ctx, func() {
				p.aacSeqHeader = cloneView(flv)
				p.aacSeqHeader.Timestamp = 0
			})
		}

		for _, sub := range subs {
			_ = sub.onEvent(cloneView(flv))
		}

	case *flvtag.VideoData:
		if d.AVCPacketType == flvtag.AVCPacketTypeSequenceHeader {
			p.m.Do(ctx, func() {
				p.avcSeqHeader = cloneView(flv)
				p.avcSeqHeader.Timestamp = 0
			})
		} else {
			if d.FrameType == flvtag.FrameTypeKeyFrame {
				p.m.Do(ctx, func() {
					p.lastKeyFrame = cloneView(flv)
					p.lastKeyFrame.Timestamp = 0
				})
			}
		}

		for _, sub := range subs {
			if !sub.initialized {
				p.InitSub(sub)
			}
			_ = sub.onEvent(cloneView(flv))
		}

	default:
		panic("unexpected")
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

	closed         bool
	initialized    bool
	closedChanOnce sync.Once
	closedChan     chan struct{}

	lastTimestamp uint32
	eventCallback func(*flvtag.FlvTag) error
}

func (s *Sub) onEvent(flv *flvtag.FlvTag) error {
	if s.closed {
		return nil
	}

	if flv.Timestamp != 0 && s.lastTimestamp == 0 {
		s.lastTimestamp = flv.Timestamp
	}
	flv.Timestamp -= s.lastTimestamp

	return s.eventCallback(flv)
}

func (s *Sub) Close() error {
	if s == nil {
		return nil
	}
	logger.Default().Debugf("Sub.Close")
	defer logger.Default().Debugf("/Sub.Close")

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
