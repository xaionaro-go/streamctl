package streampanel

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sort"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/player/pkg/player"
	playertypes "github.com/xaionaro-go/player/pkg/player/types"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/config"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	streamplayertypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/xcontext"
	"github.com/xaionaro-go/xsync"
)

type monitorKey struct {
	StreamDAddr string
	StreamID    api.StreamID
}

type activeMonitor struct {
	*streamplayer.StreamPlayerHandler
}

type monitorPage struct {
	*Panel
	monitorsLocker   xsync.Mutex
	activeMonitors   map[monitorKey]activeMonitor
	streamPlayers    *streamplayer.StreamPlayers
	stopUpdatingFunc context.CancelFunc
}

func (p *Panel) initMonitorPage(
	ctx context.Context,
) error {
	return xsync.DoA1R1(ctx, &p.monitorPageLocker, p.initMonitorPageNoLock, ctx)
}

func (p *Panel) initMonitorPageNoLock(
	ctx context.Context,
) error {
	if p.monitorPage != nil {
		logger.Debugf(ctx, "monitor page is already initialized")
		return nil
	}
	p.monitorPage = &monitorPage{
		Panel:          p,
		activeMonitors: map[monitorKey]activeMonitor{},
		streamPlayers: streamplayer.New(
			streamDAsStreamPlayersServer(p),
			player.NewManager(),
		),
	}
	err := p.monitorPage.init(ctx)
	if err != nil {
		p.monitorPage = nil
		return fmt.Errorf("unable to initialize the monitor page: %w", err)
	}
	return nil
}

func (p *Panel) startMonitorPage(
	ctx context.Context,
) {
	logger.Debugf(ctx, "startMonitorPage")
	defer logger.Debugf(ctx, "/startMonitorPage")

	p.monitorPageLocker.Do(ctx, func() {
		if p.monitorPage == nil {
			observability.Go(ctx, func() { // TODO: get rid of this ugliness
				t := time.NewTicker(100 * time.Millisecond)
				defer t.Stop()
				for {
					select {
					case <-t.C:
						initialized := false
						p.monitorPageLocker.Do(ctx, func() {
							if p.monitorPage != nil {
								p.monitorPage.startUpdatingNoLock(ctx)
								initialized = true
								return
							}
						})
						if initialized {
							return
						}
					case <-ctx.Done():
						return
					}
				}
			})
			return
		}
		p.monitorPage.startUpdatingNoLock(ctx)
	})
}

func (p *Panel) stopMonitorPage(
	ctx context.Context,
) {
	logger.Debugf(ctx, "stopMonitorPage")
	defer logger.Debugf(ctx, "/stopMonitorPage")

	p.monitorPageLocker.Do(ctx, func() {
		p.monitorPage.stopUpdatingNoLock(ctx)
	})
}

func (p *monitorPage) parent() *Panel {
	return p.Panel
}

func getIP(
	ctx context.Context,
	addr string,
) (_ret net.IP, _err error) {
	defer func() { logger.Debugf(ctx, "getIP(ctx, '%s') -> %v %v", addr, _ret, _err) }()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("unable to split host:port from '%s': %w", addr, err)
	}
	logger.Debugf(ctx, "host: '%s'", host)

	ip := net.ParseIP(host)
	logger.Debugf(ctx, "ip: '%s'", host)
	if ip == nil {
		ips, err := net.LookupIP(host)
		if err != nil {
			return nil, fmt.Errorf("unable to lookup address '%s': %w", host, err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("address '%s' was resolved into zero IP addresses", host)
		}
		ip = ips[0]
		logger.Debugf(ctx, "ip: '%s'", host)
	}
	return ip, nil
}

func (p *monitorPage) init(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "init")
	defer logger.Debugf(ctx, "/init")

	streamD, err := p.GetStreamD(ctx)
	if err != nil {
		return fmt.Errorf("unable to get a StreamD: %w", err)
	}

	inStreams, err := streamD.ListIncomingStreams(ctx)
	if err != nil {
		return fmt.Errorf("unable to get the list of available streams endpoints: %w", err)
	}

	m := map[api.StreamID]api.IncomingStream{}
	for _, s := range inStreams {
		m[s.StreamID] = s
	}

	cfg := ignoreError(p.parent().GetConfig(ctx))
	streamDAddr := cfg.RemoteStreamDAddr

	if ip := ignoreError(getIP(ctx, streamDAddr)); ip.IsLoopback() || ip.IsUnspecified() {
		streamDAddr = ""
	}
	logger.Debugf(ctx, "streamDAddr: '%v'", streamDAddr)
	for _, mon := range cfg.Monitors.StreamMonitors {
		if !mon.IsEnabled {
			continue
		}
		if mon.StreamDAddr != streamDAddr {
			logger.Warnf(ctx,
				"we have a monitor configured for stream '%s' from another streamd: '%s' != %s",
				mon.StreamID, mon.StreamDAddr, streamDAddr,
			)
			continue
		}
		if _, ok := m[mon.StreamID]; !ok {
			logger.Warnf(ctx, "we have a monitor configured for a stream that does not exist: '%s'", mon.StreamID)
			continue
		}

		p.startMonitor(ctx, mon.StreamDAddr, mon.StreamID, mon.VideoTracks, mon.AudioTracks)
	}
	return nil
}

func (p *monitorPage) startUpdatingNoLock(
	ctx context.Context,
) {
	logger.Debugf(ctx, "startUpdatingNoLock")
	defer logger.Debugf(ctx, "/startUpdatingNoLock")
	if p == nil {
		logger.Errorf(ctx, "monitor page is not initialized, yet")
		return
	}

	if p.stopUpdatingFunc != nil {
		p.stopUpdatingNoLock(ctx)
	}
	ctx, cancelFn := context.WithCancel(ctx)
	p.stopUpdatingFunc = cancelFn

	streamD, err := p.GetStreamD(ctx)
	if err != nil {
		p.parent().DisplayError(fmt.Errorf("unable to get the StreamD: %w", err))
		return
	}

	observability.Go(ctx, func() {
		updateData := func() {
			inStreams, err := streamD.ListIncomingStreams(ctx)
			if err != nil {
				p.parent().DisplayError(err)
				return
			}

			p.displayStreamMonitors(ctx, inStreams)
		}
		updateData()

		ch, err := streamD.SubscribeToIncomingStreamsChanges(ctx)
		if err != nil {
			p.parent().DisplayError(err)
			return
		}
		for range ch {
			logger.Debugf(ctx, "got event IncomingStreamsChange")
			updateData()
		}
	})
}

func (p *monitorPage) stopUpdatingNoLock(
	ctx context.Context,
) {
	logger.Debugf(ctx, "stopUpdatingNoLock")
	defer logger.Debugf(ctx, "/stopUpdatingNoLock")
	if p == nil {
		return
	}
	if p.stopUpdatingFunc == nil {
		return
	}
	p.stopUpdatingFunc()
	p.stopUpdatingFunc = nil
}

func (p *monitorPage) displayStreamMonitors(
	ctx context.Context,
	inStreams []api.IncomingStream,
) {
	logger.Debugf(ctx, "displayStreamMonitors")
	defer func() { logger.Debugf(ctx, "/displayStreamMonitors") }()

	sort.Slice(inStreams, func(i, j int) bool {
		return inStreams[i].StreamID < inStreams[j].StreamID
	})

	cfg := ignoreError(p.parent().GetConfig(ctx))
	streamDAddr := cfg.RemoteStreamDAddr
	if ip := ignoreError(getIP(ctx, streamDAddr)); ip.IsLoopback() || ip.IsUnspecified() {
		streamDAddr = ""
	}
	logger.Debugf(ctx, "streamDAddr: '%v'", streamDAddr)
	isEnabled := map[api.StreamID]struct{}{}
	for _, mon := range cfg.Monitors.StreamMonitors {
		if !mon.IsEnabled {
			continue
		}
		if mon.StreamDAddr != streamDAddr {
			continue
		}
		isEnabled[mon.StreamID] = struct{}{}
	}

	var objs []fyne.CanvasObject
	for idx, stream := range inStreams {
		_, isEnabled := isEnabled[stream.StreamID]
		logger.Tracef(ctx, "monitors[%3d] == %#+v (%t)", idx, stream, isEnabled)
		c := container.NewHBox()
		var icon fyne.Resource
		var label string
		updateIconAndLabel := func() {
			if isEnabled {
				icon = theme.MediaStopIcon()
				label = "Stop"
			} else {
				icon = theme.MediaPlayIcon()
				label = "Start"
			}
		}
		updateIconAndLabel()
		var updateButton func()
		startStopButton := widget.NewButtonWithIcon(label, icon, func() {
			logger.Debugf(ctx, "%s monitor '%s'", label, stream.StreamID)
			defer logger.Debugf(ctx, "/%s monitor '%s'", label, stream.StreamID)
			var err error
			if isEnabled {
				err = p.disableMonitor(ctx, streamDAddr, stream.StreamID)
			} else {
				err = p.enableMonitor(ctx,
					streamDAddr, stream.StreamID,
					[]uint{}, []uint{0, 1, 2, 3, 4, 5, 6, 7},
				)
			}
			if err != nil {
				p.parent().DisplayError(err)
				return
			}
			isEnabled = !isEnabled
			updateButton()
		})
		updateButton = func() {
			logger.Debugf(ctx, "updateButton")
			updateIconAndLabel()
			startStopButton.SetIcon(icon)
			startStopButton.SetText(label)
			startStopButton.Refresh()
		}
		caption := widget.NewLabel(string(stream.StreamID) + " (audio only)")
		c.Add(startStopButton)
		c.Add(caption)
		objs = append(objs, c)
	}
	p.streamsMonitorWidget.Objects = objs
	p.streamsMonitorWidget.Refresh()
}

func (p *monitorPage) enableMonitor(
	ctx context.Context,
	streamDAddr string,
	streamID api.StreamID,
	videoTrackIDs []uint,
	audioTrackIDs []uint,
) (_err error) {
	logger.Debugf(ctx, "enableMonitor(ctx, '%s', '%s', %#+v, %#+v)", streamDAddr, streamID, videoTrackIDs, audioTrackIDs)
	defer func() {
		logger.Debugf(ctx, "/enableMonitor(ctx, '%s', '%s', %#+v, %#+v): %v", streamDAddr, streamID, videoTrackIDs, audioTrackIDs, _err)
	}()

	return xsync.DoR1(ctx, &p.monitorsLocker, func() error {
		return p.enableMonitorNoLock(ctx,
			streamDAddr, streamID,
			videoTrackIDs, audioTrackIDs,
		)
	})
}

func (p *monitorPage) enableMonitorNoLock(
	ctx context.Context,
	streamDAddr string,
	streamID api.StreamID,
	videoTrackIDs []uint,
	audioTrackIDs []uint,
) (_err error) {
	logger.Debugf(ctx, "enableMonitorNoLock(ctx, '%s', '%s', %#+v, %#+v)", streamDAddr, streamID, videoTrackIDs, audioTrackIDs)
	defer func() {
		logger.Debugf(ctx, "/enableMonitorNoLock(ctx, '%s', '%s', %#+v, %#+v): %v", streamDAddr, streamID, videoTrackIDs, audioTrackIDs, _err)
	}()

	var err error
	var shouldStopFirst *config.StreamMonitor
	p.configLocker.Do(ctx, func() {
		for idx := range p.Config.Monitors.StreamMonitors {
			mon := &p.Config.Monitors.StreamMonitors[idx]
			if mon.StreamDAddr != streamDAddr {
				continue
			}
			if mon.StreamID != streamID {
				continue
			}
			if mon.IsEnabled {
				shouldStopFirst = mon
			}
			mon.IsEnabled = true
			mon.VideoTracks = videoTrackIDs
			mon.AudioTracks = audioTrackIDs
			err = p.parent().saveConfigNoLock(ctx)
			return
		}
		p.Config.Monitors.StreamMonitors = append(p.Config.Monitors.StreamMonitors, config.StreamMonitor{
			IsEnabled:   true,
			StreamDAddr: streamDAddr,
			StreamID:    streamID,
			VideoTracks: videoTrackIDs,
			AudioTracks: audioTrackIDs,
		})
		err = p.parent().saveConfigNoLock(ctx)
	})
	if err != nil {
		return fmt.Errorf("unable to update the config: %w", err)
	}

	if shouldStopFirst != nil {
		err := p.stopMonitorNoLock(
			ctx,
			shouldStopFirst.StreamDAddr,
			shouldStopFirst.StreamID,
		)
		if err != nil {
			return fmt.Errorf("unable to stop the previous monitoring: %w", err)
		}
	}

	err = p.startMonitorNoLock(
		ctx,
		streamDAddr,
		streamID,
		videoTrackIDs,
		audioTrackIDs,
	)
	if err != nil {
		return fmt.Errorf("unable to start monitoring: %w", err)
	}
	return nil
}

func (p *monitorPage) disableMonitor(
	ctx context.Context,
	streamDAddr string,
	streamID api.StreamID,
) (_err error) {
	logger.Debugf(ctx, "disableMonitor(ctx, '%s', '%s')", streamDAddr, streamID)
	defer func() {
		logger.Debugf(ctx, "/disableMonitor(ctx, '%s', '%s'): %v", streamDAddr, streamID, _err)
	}()

	var err error
	var shouldStop *config.StreamMonitor
	p.configLocker.Do(ctx, func() {
		for idx := range p.Config.Monitors.StreamMonitors {
			mon := &p.Config.Monitors.StreamMonitors[idx]
			if mon.StreamDAddr != streamDAddr {
				continue
			}
			if mon.StreamID != streamID {
				continue
			}
			if !mon.IsEnabled {
				return
			}
			mon.IsEnabled = false
			shouldStop = mon
			err = p.parent().saveConfigNoLock(ctx)
			return
		}
	})
	if err != nil {
		return fmt.Errorf("unable to update the config: %w", err)
	}

	if shouldStop == nil {
		return nil
	}

	err = p.stopMonitorNoLock(
		ctx,
		shouldStop.StreamDAddr,
		shouldStop.StreamID,
	)
	if err != nil {
		return fmt.Errorf("unable to stop the previous monitoring: %w", err)
	}
	return nil
}

type getStreamDer interface {
	GetStreamD(context.Context) (api.StreamD, error)
}

type streamDAsStreamPlayersServerType struct {
	GetStreamDer getStreamDer
}

func streamDAsStreamPlayersServer(
	getStreamDer getStreamDer,
) *streamDAsStreamPlayersServerType {
	return &streamDAsStreamPlayersServerType{
		GetStreamDer: getStreamDer,
	}
}

func (w streamDAsStreamPlayersServerType) GetPortServers(
	ctx context.Context,
) ([]streamportserver.Config, error) {
	streamD, err := w.GetStreamDer.GetStreamD(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get StreamD: %w", err)
	}

	streamServers, err := streamD.ListStreamServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get the list of stream servers: %w", err)
	}

	var result []streamportserver.Config
	for _, srv := range streamServers {
		result = append(result, srv.Config)
	}
	return result, nil
}

func (w streamDAsStreamPlayersServerType) WaitPublisherChan(
	ctx context.Context,
	streamID api.StreamID,
	waitForNext bool,
) (_ret <-chan streamplayer.Publisher, _err error) {
	logger.Debugf(ctx, "WaitPublisherChan(ctx, '%s', %t)", streamID, waitForNext)
	defer func() {
		logger.Debugf(ctx, "/WaitPublisherChan(ctx, '%s', %t): %p %v", streamID, waitForNext, _ret, _err)
	}()
	streamD, err := w.GetStreamDer.GetStreamD(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get StreamD: %w", err)
	}

	ch, err := streamD.WaitForStreamPublisher(ctx, streamID, waitForNext)
	if err != nil {
		return nil, fmt.Errorf("unable to start waiting for stream publisher: %w", err)
	}

	result := make(chan streamplayer.Publisher)
	observability.Go(ctx, func() {
		defer close(result)
		select {
		case <-ctx.Done():
			logger.Debugf(ctx, "context is closed")
			return
		case _, ok := <-ch:
			if !ok {
				logger.Debugf(ctx, "chan is closed")
				return
			}
			logger.Debugf(ctx, "received an event")
			result <- nil
		}
	})

	return result, nil
}

func (p *monitorPage) startMonitor(
	ctx context.Context,
	streamDAddr string,
	streamID api.StreamID,
	videoTrackIDs []uint,
	audioTrackIDs []uint,
) (_err error) {
	logger.Debugf(ctx, "startMonitor(ctx, '%s', '%s', %#+v, %#+v)", streamDAddr, streamID, videoTrackIDs, audioTrackIDs)
	defer func() {
		logger.Debugf(ctx, "/startMonitor(ctx, '%s', '%s', %#+v, %#+v): %v", streamDAddr, streamID, videoTrackIDs, audioTrackIDs, _err)
	}()

	return xsync.DoR1(ctx, &p.monitorsLocker, func() error {
		return p.startMonitorNoLock(ctx,
			streamDAddr, streamID,
			videoTrackIDs, audioTrackIDs,
		)
	})
}

func (p *monitorPage) startMonitorNoLock(
	ctx context.Context,
	streamDAddr string,
	streamID api.StreamID,
	videoTrackIDs []uint,
	audioTrackIDs []uint,
) (_err error) {
	logger.Debugf(ctx, "startMonitorNoLock(ctx, '%s', '%s', %#+v, %#+v)", streamDAddr, streamID, videoTrackIDs, audioTrackIDs)
	defer func() {
		logger.Debugf(ctx, "/startMonitorNoLock(ctx, '%s', '%s', %#+v, %#+v): %v", streamDAddr, streamID, videoTrackIDs, audioTrackIDs, _err)
	}()

	var mediaURL *url.URL
	var err error
	if streamDAddr == "" {
		mediaURL, err = streamportserver.GetURLForLocalStreamID(
			ctx,
			streamDAsStreamPlayersServer(p.parent()), streamID,
			nil,
		)
	} else {
		isIPv6 := false
		host, _, err := net.SplitHostPort(streamDAddr)
		logger.Debugf(ctx, "getting the host: %v %v", host, err)
		if err == nil {
			ip := net.ParseIP(host)
			logger.Debugf(ctx, "parsing the IP: %v", ip)
			if len(ip) == net.IPv6len {
				isIPv6 = true
			}
		}

		logger.Debugf(ctx, "is-IPv6: %v", isIPv6)
		var streamDAddrV4, streamDAddrV6 string
		if isIPv6 {
			streamDAddrV6 = streamDAddr
		} else {
			streamDAddrV4 = streamDAddr
		}
		mediaURL, err = streamportserver.GetURLForRemoveStreamID(
			ctx,
			streamDAddrV4, streamDAddrV6,
			streamDAsStreamPlayersServer(p.parent()), streamID,
			nil,
		)
	}

	if err != nil {
		return fmt.Errorf("unable to construct the URL: %w", err)
	}
	logger.Debugf(ctx, "URL: %s", mediaURL)

	monitorKey := monitorKey{
		StreamDAddr: streamDAddr,
		StreamID:    streamID,
	}
	logger.Debugf(ctx, "monitorKey: %#+v", monitorKey)
	if _, ok := p.activeMonitors[monitorKey]; ok {
		return fmt.Errorf("there is already an active monitor for %#+v", monitorKey)
	}

	var opts streamplayertypes.Options
	opts = append(opts,
		streamplayertypes.OptionOverrideURL(mediaURL.String()),
		streamplayertypes.OptionForceWaitForPublisher(true),
	)
	if len(videoTrackIDs) == 0 {
		opts = append(opts, streamplayertypes.OptionCustomPlayerOptions{playertypes.OptionHideWindow(true)})
	}

	if h := p.streamPlayers.Get(streamID); h != nil {
		return fmt.Errorf("not implemented yet: we currently to do not support using the same streamID on multiple StreamD instances")
	}

	playerHandler, err := p.streamPlayers.Create(
		xcontext.DetachDone(ctx),
		streamID,
		playertypes.BackendBuiltinLibAV,
		opts...,
	)
	if err != nil {
		return fmt.Errorf("unable to create a stream player handler: %w", err)
	}

	p.activeMonitors[monitorKey] = activeMonitor{
		StreamPlayerHandler: playerHandler,
	}
	return nil
}

func (p *monitorPage) stopMonitor(
	ctx context.Context,
	streamDAddr string,
	streamID api.StreamID,
	videoTrackIDs []uint,
	audioTrackIDs []uint,
) (_err error) {
	logger.Debugf(ctx, "stopMonitor(ctx, '%s', '%s')", streamDAddr, streamID, videoTrackIDs, audioTrackIDs)
	defer func() {
		logger.Debugf(ctx, "/stopMonitor(ctx, '%s', '%s'): %v", streamDAddr, streamID, videoTrackIDs, audioTrackIDs, _err)
	}()

	return xsync.DoR1(ctx, &p.monitorsLocker, func() error {
		return p.stopMonitorNoLock(ctx,
			streamDAddr, streamID,
		)
	})
}

func (p *monitorPage) stopMonitorNoLock(
	ctx context.Context,
	streamDAddr string,
	streamID api.StreamID,
) (_err error) {
	logger.Debugf(ctx, "stopMonitorNoLock(ctx, '%s', '%s')", streamDAddr, streamID)
	defer func() {
		logger.Debugf(ctx, "/stopMonitorNoLock(ctx, '%s', '%s'): %v", streamDAddr, streamID, _err)
	}()

	monitorKey := monitorKey{
		StreamDAddr: streamDAddr,
		StreamID:    streamID,
	}
	logger.Debugf(ctx, "monitorKey: %#+v", monitorKey)

	activeMon, ok := p.activeMonitors[monitorKey]
	if !ok {
		return fmt.Errorf("there is no active monitor %#+v", monitorKey)
	}
	playerStreamID := activeMon.StreamPlayerHandler.StreamID
	delete(p.activeMonitors, monitorKey)

	err := activeMon.StreamPlayerHandler.Close()
	if err != nil {
		return fmt.Errorf("unable to close the stream player handler for '%s': %w", playerStreamID, err)
	}
	err = p.streamPlayers.Remove(ctx, playerStreamID)
	if err != nil {
		return fmt.Errorf("unable ")
	}

	return nil
}
