package streams

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

type state byte

const (
	stateNone state = iota
	stateMedias
	stateTracks
	stateStart
	stateExternal
	stateInternal
)

type Producer struct {
	core.Listener

	urlFunc  func() string
	template string

	conn      core.Producer
	receivers []*core.Receiver
	senders   []*core.Receiver

	state    state
	mu       xsync.Mutex
	workerID int

	streamHandler *StreamHandler
}

const SourceTemplate = "{input}"

func (s *StreamHandler) NewProducer(source func() string) *Producer {
	if strings.Contains(source(), SourceTemplate) {
		return &Producer{streamHandler: s, template: source()}
	}

	return &Producer{streamHandler: s, urlFunc: source}
}

func (p *Producer) SetSource(s string) {
	if p.template != "" {
		s = strings.Replace(p.template, SourceTemplate, s, 1)
	}
	p.urlFunc = func() string { return s }
}

func (p *Producer) Dial() error {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &p.mu, func() error {
		if p.state == stateNone {
			conn, err := p.streamHandler.GetProducer(p.urlFunc())
			if err != nil {
				return err
			}

			p.conn = conn
			p.state = stateMedias
		}
		return nil
	})
}

func (p *Producer) GetMedias() []*core.Media {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &p.mu, func() []*core.Media {
		if p.conn == nil {
			return nil
		}
		return p.conn.GetMedias()
	})
}

func (p *Producer) GetTrack(media *core.Media, codec *core.Codec) (*core.Receiver, error) {
	ctx := context.TODO()
	return xsync.DoA2R2(ctx, &p.mu, p.getTrack, media, codec)
}

func (p *Producer) getTrack(media *core.Media, codec *core.Codec) (*core.Receiver, error) {
	if p.state == stateNone {
		return nil, errors.New("get track from none state")
	}

	for _, track := range p.receivers {
		if track.Codec == codec {
			return track, nil
		}
	}

	track, err := p.conn.GetTrack(media, codec)
	if err != nil {
		return nil, err
	}

	p.receivers = append(p.receivers, track)

	if p.state == stateMedias {
		p.state = stateTracks
	}

	return track, nil
}

func (p *Producer) AddTrack(media *core.Media, codec *core.Codec, track *core.Receiver) error {
	ctx := context.TODO()
	return xsync.DoA3R1(ctx, &p.mu, p.addTrack, media, codec, track)
}

func (p *Producer) addTrack(media *core.Media, codec *core.Codec, track *core.Receiver) error {

	if p.state == stateNone {
		return errors.New("add track from none state")
	}

	if err := p.conn.(core.Consumer).AddTrack(media, codec, track); err != nil {
		return err
	}

	p.senders = append(p.senders, track)

	if p.state == stateMedias {
		p.state = stateTracks
	}

	return nil
}

func (p *Producer) MarshalJSON() ([]byte, error) {
	if conn := p.conn; conn != nil {
		return json.Marshal(conn)
	}
	info := map[string]string{"url": p.urlFunc()}
	return json.Marshal(info)
}

func (p *Producer) start() {
	ctx := context.TODO()
	p.mu.Do(ctx, func() {
		if p.state != stateTracks {
			return
		}

		logger.Default().Debugf("[streams] start producer url=%s", p.urlFunc)

		p.state = stateStart
		p.workerID++

		{
			conn, workerID := p.conn, p.workerID
			observability.Go(context.TODO(), func() { p.worker(conn, workerID) })
		}
	})
}

func (p *Producer) worker(conn core.Producer, workerID int) {
	if err := conn.Start(); err != nil {
		var closed bool
		ctx := context.TODO()
		p.mu.Do(ctx, func() {
			closed = p.workerID != workerID
		})
		if closed {
			return
		}

		logger.Default().Warn(struct{ URL string }{URL: p.urlFunc()}, err)
	}

	p.reconnect(workerID, 0)
}

func (p *Producer) reconnect(workerID, retry int) {
	ctx := context.TODO()
	xsync.DoA2(ctx, &p.mu, p.reconnectNoLock, workerID, retry)
}

func (p *Producer) reconnectNoLock(workerID, retry int) {
	if p.workerID != workerID {
		logger.Default().Tracef("[streams] stop reconnect url=%s", p.urlFunc)
		return
	}

	logger.Default().Debugf("[streams] retry=%d to url=%s", retry, p.urlFunc)

	conn, err := p.streamHandler.GetProducer(p.urlFunc())
	if err != nil {
		logger.Default().Debugf("[streams] producer=%s", err)

		var timeout time.Duration
		switch {
		case retry < 5:
			timeout = time.Second
		case retry < 10:
			timeout = time.Second * 5
		case retry < 20:
			timeout = time.Second * 10
		default:
			timeout = time.Minute
		}

		time.AfterFunc(timeout, func() {
			p.reconnect(workerID, retry+1)
		})
		return
	}

	for _, media := range conn.GetMedias() {
		switch media.Direction {
		case core.DirectionRecvonly:
			for i, receiver := range p.receivers {
				codec := media.MatchCodec(receiver.Codec)
				if codec == nil {
					continue
				}

				track, err := conn.GetTrack(media, codec)
				if err != nil {
					continue
				}

				receiver.Replace(track)
				p.receivers[i] = track
				break
			}

		case core.DirectionSendonly:
			for _, sender := range p.senders {
				codec := media.MatchCodec(sender.Codec)
				if codec == nil {
					continue
				}

				_ = conn.(core.Consumer).AddTrack(media, codec, sender)
			}
		}
	}

	// stop previous connection after moving tracks (fix ghost exec/ffmpeg)
	_ = p.conn.Stop()
	// swap connections
	p.conn = conn

	observability.Go(context.TODO(), func() { p.worker(conn, workerID) })
}

func (p *Producer) stop() {
	ctx := context.TODO()
	p.mu.Do(ctx, func() {
		p.stopNoLock()
	})
}
func (p *Producer) stopNoLock() {
	switch p.state {
	case stateExternal:
		logger.Default().Tracef("[streams] skip stop external producer")
		return
	case stateNone:
		logger.Default().Tracef("[streams] skip stop none producer")
		return
	case stateStart:
		p.workerID++
	}

	logger.Default().Tracef("[streams] stop producer url=%s", p.urlFunc)

	if p.conn != nil {
		_ = p.conn.Stop()
		p.conn = nil
	}

	p.state = stateNone
	p.receivers = nil
	p.senders = nil
}
