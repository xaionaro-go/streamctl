package streamserver

import (
	"bytes"
	"sync"

	"github.com/facebookincubator/go-belt/tool/logger"
	flvtag "github.com/yutopp/go-flv/tag"
)

type Pubsub struct {
	srv  *RelayService
	name string

	pub *Pub

	nextSubID uint64
	subs      map[uint64]*Sub

	m sync.Mutex
}

func NewPubsub(srv *RelayService, name string) *Pubsub {
	return &Pubsub{
		srv:  srv,
		name: name,

		subs: map[uint64]*Sub{},
	}
}

func (pb *Pubsub) Name() string {
	return pb.name
}

func (pb *Pubsub) Deregister() error {
	pb.m.Lock()
	defer pb.m.Unlock()

	go func() {
		for _, sub := range pb.subs {
			_ = sub.Close()
		}
	}()

	return pb.srv.removePubsub(pb.name)
}

func (pb *Pubsub) Pub() *Pub {
	pub := &Pub{
		pb: pb,
	}
	pb.pub = pub
	return pub
}

func (pb *Pubsub) Sub(eventCallback func(ft *flvtag.FlvTag) error) *Sub {
	pb.m.Lock()
	defer pb.m.Unlock()

	subID := pb.nextSubID
	sub := &Sub{
		pubSub:        pb,
		subID:         subID,
		eventCallback: eventCallback,
	}

	pb.nextSubID++
	pb.subs[subID] = sub
	return sub
}

func (pb *Pubsub) RemoveSub(s *Sub) {
	pb.m.Lock()
	defer pb.m.Unlock()

	delete(pb.subs, s.subID)
}

type Pub struct {
	m  sync.Mutex
	pb *Pubsub

	aacSeqHeader *flvtag.FlvTag
	avcSeqHeader *flvtag.FlvTag
	lastKeyFrame *flvtag.FlvTag
}

func (p *Pub) InitSub(sub *Sub) {
	p.m.Lock()
	avcSeqHeader := p.avcSeqHeader
	lastKeyFrame := p.lastKeyFrame
	aacSeqHeader := p.aacSeqHeader
	p.m.Unlock()
	if avcSeqHeader != nil {
		logger.Default().Tracef("sending avcSeqHeader")
		_ = sub.onEvent(cloneView(avcSeqHeader))
	}
	if lastKeyFrame != nil {
		logger.Default().Tracef("sending lastKeyFrame")
		_ = sub.onEvent(cloneView(lastKeyFrame))
	}
	if aacSeqHeader != nil {
		logger.Default().Tracef("sending aacSeqHeader")
		_ = sub.onEvent(cloneView(aacSeqHeader))
	}

	sub.initialized = true
}

// TODO: Should check codec types and so on.
// In this example, checks only sequence headers and assume that AAC and AVC.
func (p *Pub) Publish(flv *flvtag.FlvTag) error {
	p.pb.m.Lock()
	subs := p.pb.subs
	p.pb.m.Unlock()

	switch d := flv.Data.(type) {
	case *flvtag.ScriptData:
		for _, sub := range subs {
			_ = sub.onEvent(cloneView(flv))
		}

	case *flvtag.AudioData:
		if d.AACPacketType == flvtag.AACPacketTypeSequenceHeader {
			p.m.Lock()
			p.aacSeqHeader = cloneView(flv)
			p.aacSeqHeader.Timestamp = 0
			p.m.Unlock()
		}

		for _, sub := range subs {
			_ = sub.onEvent(cloneView(flv))
		}

	case *flvtag.VideoData:
		if d.AVCPacketType == flvtag.AVCPacketTypeSequenceHeader {
			p.m.Lock()
			p.avcSeqHeader = cloneView(flv)
			p.avcSeqHeader.Timestamp = 0
			p.m.Unlock()
		} else {
			if d.FrameType == flvtag.FrameTypeKeyFrame {
				p.m.Lock()
				p.lastKeyFrame = cloneView(flv)
				p.lastKeyFrame.Timestamp = 0
				p.m.Unlock()
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
	pubSub *Pubsub
	subID  uint64

	closed      bool
	initialized bool

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
	if s.closed {
		return nil
	}
	s.closed = true
	s.pubSub.RemoveSub(s)
	return nil
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
