//go:build with_libvlc
// +build with_libvlc

package player

import (
	"fmt"
	"net/url"
	"sync/atomic"
	"time"

	vlc "github.com/adrg/libvlc-go/v3"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/xsync"
	"golang.org/x/net/context"
)

type VLC struct {
	Title            string
	StatusMutex      xsync.Mutex
	Player           *vlc.Player
	Media            *vlc.Media
	EventManager     *vlc.EventManager
	DetachEventsFunc context.CancelFunc
	LastURL          string

	IsStopped bool

	EndCh chan struct{}
}

var vlcPlayerCounter int64 = 0

func NewVLC(title string) (*VLC, error) {
	if atomic.AddInt64(&vlcPlayerCounter, 1) != 1 {
		return nil, fmt.Errorf("currently we do not support more than one VLC player at once")
	}
	args := []string{fmt.Sprintf("--video-title=%s", title)}
	if err := vlc.Init(args...); err != nil {
		return nil, fmt.Errorf("unable to initialize VLC with arguments: %v", args)
	}

	p := &VLC{
		Title: title,
		EndCh: make(chan struct{}),
	}

	var err error
	p.Player, err = vlc.NewPlayer()
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a VLC player: %w", err)
	}

	manager, err := p.Player.EventManager()
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a VLC event manager: %w", err)
	}

	eventID, err := manager.Attach(vlc.MediaPlayerEndReached, func(e vlc.Event, i interface{}) {
		p.StatusMutex.Do(context.TODO(), func() {
			p.IsStopped = true
			var oldCh chan struct{}
			oldCh, p.EndCh = p.EndCh, make(chan struct{})
			close(oldCh)
		})
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to attach the 'EndReached' event handler: %w", err)
	}
	p.DetachEventsFunc = func() {
		manager.Detach(eventID)
	}

	return p, nil
}

func (p *VLC) ProcessTitle() string {
	return p.Title
}

func (p *VLC) OpenURL(link string) error {
	return xsync.DoA1R1(context.TODO(), &p.StatusMutex, p.openURL, link)
}

func (p *VLC) openURL(link string) error {
	if p.Media != nil {
		return fmt.Errorf("some media is already opened in this player")
	}

	var (
		media *vlc.Media
		err   error
	)
	if urlParsed, _err := url.Parse(link); _err == nil && urlParsed.Scheme != "" {
		media, err = p.Player.LoadMediaFromURL(link)
	} else {
		media, err = p.Player.LoadMediaFromPath(link)
	}
	if err != nil {
		return fmt.Errorf("unable to open '%s': %w", link, err)
	}
	p.Media = media
	p.LastURL = link

	if err := p.play(); err != nil {
		return fmt.Errorf("opened, but unable to start playing '%s': %w", link, err)
	}

	return nil
}

func (p *VLC) GetLink() string {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &p.StatusMutex, func() string {
		return p.LastURL
	})
}

func (p *VLC) EndChan() <-chan struct{} {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &p.StatusMutex, func() <-chan struct{} {
		return p.EndCh
	})
}

func (p *VLC) IsEnded() bool {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &p.StatusMutex, func() bool {
		return p.IsStopped
	})
}

func (p *VLC) GetPosition() time.Duration {
	ts, err := p.Player.MediaTime()
	if err != nil {
		logger.Tracef(context.TODO(), "unable to get current position: %v", err)
		return 0
	}
	return time.Duration(ts) * time.Millisecond
}

func (p *VLC) GetLength() time.Duration {
	ts, err := p.Player.MediaLength()
	if err != nil {
		logger.Debugf(context.TODO(), "unable to get the total length: %v", err)
		return 0
	}
	return time.Duration(ts) * time.Millisecond
}

func (p *VLC) GetSpeed() (float64, error) {
	return float64(p.Player.PlaybackRate()), nil
}

func (p *VLC) SetSpeed(speed float64) error {
	return p.Player.SetPlaybackRate(float32(speed))
}

func (p *VLC) Play() error {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &p.StatusMutex, p.play)
}

func (p *VLC) play() error {
	err := p.Player.Play()
	if err != nil {
		return err
	}
	p.IsStopped = false
	return nil
}

func (p *VLC) GetPause() (bool, error) {
	return !p.Player.IsPlaying(), nil
}

func (p *VLC) SetPause(pause bool) error {
	return p.Player.SetPause(pause)
}

func (p *VLC) Stop() error {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &p.StatusMutex, p.stop)
}

func (p *VLC) stop() error {
	err := p.Player.Stop()
	if err != nil {
		return err
	}
	p.IsStopped = false
	return nil
}

func (p *VLC) Close() error {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &p.StatusMutex, p.close)
}

func (p *VLC) close() error {
	if p.DetachEventsFunc != nil {
		p.DetachEventsFunc()
		p.DetachEventsFunc = nil
	}

	err := multierror.Append(
		p.Player.Stop(),
		p.Media.Release(),
		p.Player.Release(),
		vlc.Release(),
	).ErrorOrNil()
	atomic.AddInt64(&vlcPlayerCounter, -1)
	return err
}
