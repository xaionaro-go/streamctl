// Package streampanel provides a Fyne-based graphical user interface for controlling
// and monitoring live streams. This file implements the "Restream" page, allowing
// management of stream sources, servers, sinks, forwards, and players.
package streampanel

import (
	"github.com/xaionaro-go/streamctl/pkg/clock"
)

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/dustin/go-humanize"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/player/pkg/player"
	"github.com/xaionaro-go/recoder"
	"github.com/xaionaro-go/streamctl/pkg/consts"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	sstypes "github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/xcontext"
	xfyne "github.com/xaionaro-go/xfyne/widget"
)

const (
	trackRemove = "<remove>"
)

func (p *Panel) startRestreamPage(
	ctx context.Context,
) {
	logger.Debugf(ctx, "startRestreamPage")
	defer logger.Debugf(ctx, "/startRestreamPage")

	p.initRestreamPage(ctx)
}

func (p *Panel) initRestreamPage(
	ctx context.Context,
) {
	logger.Debugf(ctx, "initRestreamPage")
	defer logger.Debugf(ctx, "/initRestreamPage")

	p.restreamPageLocker.Do(ctx, func() {
		if p.restreamPageInitialized {
			return
		}
		p.restreamPageInitialized = true

		observability.Go(ctx, func(ctx context.Context) {
			updateData := func() {
				inStreams, err := p.StreamD.ListStreamSources(ctx)
				if err != nil {
					p.DisplayError(err)
				} else {
					p.displayStreamSources(ctx, inStreams)
				}
			}
			updateData()

			ch, restartCh, err := autoResubscribe(ctx, p.StreamD.SubscribeToStreamSourcesChanges)
			if err != nil {
				p.DisplayError(err)
				return
			}
			for range ch {
				var ok bool
				select {
				case _, ok = <-ch:
					if ok {
						logger.Debugf(ctx, "got event StreamSourcesChange")
					}
				case _, ok = <-restartCh:
					if ok {
						logger.Debugf(ctx, "restarted SubscribeToStreamSourcesChanges")
					}
				}
				if !ok {
					break
				}
				updateData()
			}
		})

		observability.Go(ctx, func(ctx context.Context) {
			updateData := func() {
				streamServers, err := p.StreamD.ListStreamServers(ctx)
				if err != nil {
					p.DisplayError(err)
				} else {
					p.displayStreamServers(ctx, streamServers)
				}
			}
			updateData()

			ch, restartCh, err := autoResubscribe(ctx, p.StreamD.SubscribeToStreamServersChanges)
			if err != nil {
				p.DisplayError(err)
				return
			}
			for {
				var ok bool
				select {
				case _, ok = <-ch:
					if ok {
						logger.Debugf(ctx, "got event StreamServersChange")
					}
				case _, ok = <-restartCh:
					if ok {
						logger.Debugf(ctx, "restarted SubscribeToStreamServersChanges")
					}
				}
				if !ok {
					break
				}
				updateData()
			}
		})

		observability.Go(ctx, func(ctx context.Context) {
			defer logger.Debugf(ctx, "/SubscribeToStreamSinksChanges")
			updateData := func() {
				sinks, err := p.StreamD.ListStreamSinks(ctx)
				if err != nil {
					p.DisplayError(err)
				} else {
					p.displayStreamSinks(ctx, sinks)
				}
			}
			updateData()

			ch, restartCh, err := autoResubscribe(ctx, p.StreamD.SubscribeToStreamSinksChanges)
			if err != nil {
				p.DisplayError(err)
				return
			}
			for {
				var ok bool
				select {
				case _, ok = <-ch:
					if ok {
						logger.Debugf(ctx, "got event StreamSinksChange")
					}
				case _, ok = <-restartCh:
					if ok {
						logger.Debugf(ctx, "restarted SubscribeToStreamSinksChanges")
					}
				}
				if !ok {
					break
				}
				updateData()
			}
		})

		observability.Go(ctx, func(ctx context.Context) {
			defer logger.Debugf(ctx, "/SubscribeToStreamForwardsChanges")
			updateData := func() {
				streamFwds, err := p.StreamD.ListStreamForwards(ctx)
				if err != nil {
					p.DisplayError(err)
				} else {
					p.displayStreamForwards(ctx, streamFwds)
				}
			}
			updateData()

			ch, restartCh, err := autoResubscribe(ctx, p.StreamD.SubscribeToStreamForwardsChanges)
			if err != nil {
				p.DisplayError(err)
				return
			}
			for {
				var ok bool
				select {
				case _, ok = <-ch:
					if ok {
						logger.Debugf(ctx, "got event StreamForwardsChange")
					}
				case _, ok = <-restartCh:
					if ok {
						logger.Debugf(ctx, "restarted SubscribeToStreamForwardsChanges")
					}
				}
				if !ok {
					break
				}
				updateData()
			}
		})

		observability.Go(ctx, func(ctx context.Context) {
			defer logger.Debugf(ctx, "/SubscribeToStreamPlayersChanges")
			updateData := func() {
				streamPlayers, err := p.StreamD.ListStreamPlayers(ctx)
				if err != nil {
					p.DisplayError(err)
				} else {
					p.displayStreamPlayers(ctx, streamPlayers)
				}
			}
			updateData()

			ch, restartCh, err := autoResubscribe(ctx, p.StreamD.SubscribeToStreamPlayersChanges)
			if err != nil {
				p.DisplayError(err)
				return
			}
			for {
				var ok bool
				select {
				case _, ok = <-ch:
					if ok {
						logger.Debugf(ctx, "got event StreamPlayersChange")
					}
				case _, ok = <-restartCh:
					if ok {
						logger.Debugf(ctx, "restarted SubscribeToStreamPlayersChanges")
					}
				}
				if !ok {
					break
				}
				updateData()
			}
		})
	})
}

func (p *Panel) openAddStreamServerWindow(ctx context.Context) {
	w := p.app.NewWindow(consts.AppName + ": Add Stream Server")
	resizeWindow(w, fyne.NewSize(400, 300))

	currentProtocol := streamtypes.ServerTypeRTMP
	protocolSelectLabel := widget.NewLabel("Protocol:")
	protocolSelect := widget.NewSelect([]string{
		ptr(streamtypes.ServerTypeRTMP).String(),
		ptr(streamtypes.ServerTypeRTSP).String(),
		ptr(streamtypes.ServerTypeSRT).String(),
	}, func(s string) {
		currentProtocol = streamtypes.ParseServerType(s)
	})
	protocolSelect.SetSelected(currentProtocol.String())

	listenHostEntry := widget.NewEntry()
	listenHostEntry.SetPlaceHolder("host, ex.: 192.168.0.37")
	listenPortEntry := xfyne.NewNumericalEntry()
	listenPortEntry.SetPlaceHolder("port, ex.: 1935")
	var listenPort uint16
	oldListenPortValue := ""
	listenPortEntry.OnChanged = func(s string) {
		if s == "" {
			listenPort = 0
			return
		}
		_listenPort, err := strconv.ParseUint(listenPortEntry.Text, 10, 16)
		if err != nil {
			listenPortEntry.SetText(oldListenPortValue)
			return
		}
		listenPort = uint16(_listenPort)
	}

	enableTLS := false
	enableTLSCheckbox := widget.NewCheck("Enable TLS", func(b bool) {
		enableTLS = b
	})

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		listenHost := listenHostEntry.Text
		err := p.addStreamServer(ctx, currentProtocol, listenHost, listenPort, enableTLS)
		if err != nil {
			p.DisplayError(err)
			return
		}
		w.Close()
	})

	w.SetContent(container.NewBorder(
		nil,
		container.NewHBox(saveButton),
		nil,
		nil,
		container.NewVBox(
			container.NewHBox(
				protocolSelectLabel,
				protocolSelect,
			),
			widget.NewLabel("Listen address:"),
			listenHostEntry,
			listenPortEntry,
			enableTLSCheckbox,
		),
	))
	w.Show()
}

func (p *Panel) addStreamServer(
	ctx context.Context,
	proto api.StreamServerType,
	listenHost string,
	listenPort uint16,
	enableTLS bool,
) error {
	logger.Debugf(ctx, "addStreamServer")
	defer logger.Debugf(ctx, "/addStreamServer")
	var opts streamportserver.Options
	opts = append(opts, streamportserver.OptionIsTLS(enableTLS))
	return p.StreamD.StartStreamServer(
		ctx,
		proto,
		fmt.Sprintf("%s:%d", listenHost, listenPort),
		opts...,
	)
}

func (p *Panel) displayStreamServers(
	ctx context.Context,
	streamServers []api.StreamServer,
) {
	logger.Debugf(ctx, "displayStreamServers")
	defer logger.Debugf(ctx, "/displayStreamServers")

	hasDynamicValue := false

	var objs []fyne.CanvasObject
	for idx, srv := range streamServers {
		logger.Tracef(ctx, "streamServer[%3d] == %#+v", idx, srv)
		c := container.NewHBox()
		button := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			w := dialog.NewConfirm(
				fmt.Sprintf("Delete Stream Server %s://%s ?", srv.Type, srv.ListenAddr),
				"",
				func(b bool) {
					if !b {
						return
					}
					logger.Debugf(ctx, "remove stream server")
					defer logger.Debugf(ctx, "/remove stream server")
					err := p.StreamD.StopStreamServer(ctx, srv.ListenAddr)
					if err != nil {
						p.DisplayError(err)
						return
					}
				},
				p.mainWindow,
			)
			w.Show()
		})
		protoName := srv.Type.String()
		if srv.IsTLS {
			protoName += "s"
		}
		label := widget.NewLabel(fmt.Sprintf("%s://%s", protoName, srv.ListenAddr))
		c.RemoveAll()
		c.Add(button)
		c.Add(label)
		c.Add(widget.NewSeparator())

		type numBytesID struct {
			ID string
		}
		key := numBytesID{ID: srv.ListenAddr}
		p.previousNumBytesLocker.Do(ctx, func() {
			prevNumBytes := p.previousNumBytes[key]
			now := clock.Get().Now()
			bwStr := bwString(
				srv.NumBytesProducerRead,
				prevNumBytes[0],
				srv.NumBytesConsumerWrote,
				prevNumBytes[1],
				now,
				p.previousNumBytesTS[key],
			)
			bwText := widget.NewRichTextWithText(bwStr)
			hasDynamicValue = hasDynamicValue || bwStr != ""
			p.previousNumBytes[key] = [4]uint64{srv.NumBytesProducerRead, srv.NumBytesConsumerWrote}
			p.previousNumBytesTS[key] = now
			c.Add(bwText)
		})

		objs = append(objs, c)
	}
	p.updateObjects(p.streamServersWidget, objs)

	p.streamServersLocker.Do(ctx, func() {
		cancelFn := p.streamServersUpdaterCanceller
		if hasDynamicValue {
			if cancelFn == nil {
				p.streamServersUpdaterCanceller = p.streamServersUpdater(ctx)
			}
		} else {
			if cancelFn != nil {
				cancelFn()
				p.streamServersUpdaterCanceller = nil
			}
		}
	})
}

func bwString(
	nRead, nReadPrev uint64,
	nWrote, nWrotePrev uint64,
	ts, tsPrev time.Time,
) string {
	var nReadStr, nWroteStr string

	duration := ts.Sub(tsPrev)

	if nRead != math.MaxUint64 {
		n := 8 * (nRead - nReadPrev)
		nReadStr = humanize.Bytes(uint64(float64(n) * float64(time.Second) / float64(duration)))
		nReadStr = nReadStr[:len(nReadStr)-1] + "bps"
	}

	if nWrote != math.MaxUint64 {
		n := 8 * (nWrote - nWrotePrev)
		nWroteStr = humanize.Bytes(uint64(float64(n) * float64(time.Second) / float64(duration)))
		nWroteStr = nWroteStr[:len(nWroteStr)-1] + "bps"
	}

	if nReadStr == "" && nWroteStr == "" {
		return ""
	}

	return fmt.Sprintf("%s | %s", nReadStr, nWroteStr)
}

func (p *Panel) openAddStreamWindow(ctx context.Context) {
	w := p.app.NewWindow(consts.AppName + ": Add stream source")
	resizeWindow(w, fyne.NewSize(400, 300))

	streamSourceIDEntry := widget.NewEntry()
	streamSourceIDEntry.SetPlaceHolder("stream name")

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		err := p.addStreamSource(ctx, api.StreamSourceID(streamSourceIDEntry.Text))
		if err != nil {
			p.DisplayError(err)
			return
		}
		w.Close()
	})

	w.SetContent(container.NewBorder(
		nil,
		container.NewHBox(saveButton),
		nil,
		nil,
		container.NewVBox(
			streamSourceIDEntry,
		),
	))
	w.Show()
}

func (p *Panel) addStreamSource(
	ctx context.Context,
	streamSourceID api.StreamSourceID,
) error {
	logger.Debugf(ctx, "addStreamSource")
	defer logger.Debugf(ctx, "/addStreamSource")
	return p.StreamD.AddStreamSource(ctx, streamSourceID)
}

func (p *Panel) displayStreamSources(
	ctx context.Context,
	inStreams []api.StreamSource,
) {
	logger.Debugf(ctx, "displayStreamSources")
	defer logger.Debugf(ctx, "/displayStreamSources")
	sort.Slice(inStreams, func(i, j int) bool {
		return inStreams[i].StreamSourceID < inStreams[j].StreamSourceID
	})

	var objs []fyne.CanvasObject
	for idx, stream := range inStreams {
		logger.Tracef(ctx, "inStream[%3d] == %#+v", idx, stream)
		c := container.NewHBox()
		playButton := widget.NewButtonWithIcon("", theme.MediaPlayIcon(), func() {
			p.DisplayError(fmt.Errorf("playback is not implemented, yet"))
		})
		playButton.Hide() // TODO: unhide when it will be implemented
		deleteButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			w := dialog.NewConfirm(
				fmt.Sprintf("Delete stream source %s ?", stream.StreamSourceID),
				"",
				func(b bool) {
					if !b {
						return
					}
					logger.Debugf(ctx, "remove stream source")
					defer logger.Debugf(ctx, "/remove stream source")
					err := p.StreamD.RemoveStreamSource(ctx, stream.StreamSourceID)
					if err != nil {
						p.DisplayError(err)
						return
					}
				},
				p.mainWindow,
			)
			w.Show()
		})
		label := widget.NewLabel(string(stream.StreamSourceID))
		c.RemoveAll()
		c.Add(playButton)
		c.Add(deleteButton)
		c.Add(label)
		objs = append(objs, c)
	}
	p.updateObjects(p.streamsWidget, objs)
}

func (p *Panel) openAddSinkWindow(ctx context.Context) {
	p.openAddOrEditSinkWindow(
		ctx,
		"Add stream sink",
		api.StreamSink{},
		p.addStreamSink,
	)
}

func (p *Panel) openEditSinkWindow(
	ctx context.Context,
	sink api.StreamSink,
) {
	p.openAddOrEditSinkWindow(
		ctx,
		fmt.Sprintf("Edit stream sink '%s'", sink.ID),
		sink,
		p.updateStreamSink,
	)
}

func (p *Panel) openAddOrEditSinkWindow(
	ctx context.Context,
	title string,
	sink api.StreamSink,
	commitFn func(
		ctx context.Context,
		streamSinkID api.StreamSinkIDFullyQualified,
		config sstypes.StreamSinkConfig,
	) error,
) {
	w := p.app.NewWindow(consts.AppName + ": " + title)
	resizeWindow(w, fyne.NewSize(400, 300))

	streamSinkIDEntry := widget.NewEntry()
	streamSinkIDEntry.SetPlaceHolder("sink ID")
	if sink.ID.ID != "" {
		streamSinkIDEntry.SetText(string(sink.ID.ID))
		streamSinkIDEntry.Disable()
	}

	isStatic := sink.ID.Type != sstypes.StreamSinkTypeCustom

	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("URL")
	urlEntry.SetText(sink.URL)
	if !isStatic {
		urlEntry.Disable()
	}

	streamKeyEntry := widget.NewEntry()
	streamKeyEntry.SetPlaceHolder("stream key")
	streamKeyEntry.SetText(sink.StreamKey.Get())

	activeStreams, err := p.StreamD.GetActiveStreamIDs(ctx)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to list active streams: %w", err))
	}
	activeStreamOptions := []string{"<none>"}
	for _, s := range activeStreams {
		activeStreamOptions = append(activeStreamOptions, s.String())
	}

	var selectedStreamSourceID *streamcontrol.StreamIDFullyQualified
	if sink.StreamSourceID != nil {
		selectedStreamSourceID = sink.StreamSourceID
	}
	activeStreamSelect := widget.NewSelect(activeStreamOptions, func(s string) {
		if s == "<none>" {
			selectedStreamSourceID = nil
			return
		}
		for _, as := range activeStreams {
			if as.String() == s {
				selectedStreamSourceID = &as
				return
			}
		}
	})
	if selectedStreamSourceID != nil {
		activeStreamSelect.SetSelected(selectedStreamSourceID.String())
	} else {
		activeStreamSelect.SetSelected("<none>")
	}

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		err := commitFn(
			ctx,
			api.StreamSinkIDFullyQualified{
				Type: sstypes.StreamSinkTypeCustom,
				ID:   api.StreamSinkID(streamSinkIDEntry.Text),
			},
			sstypes.StreamSinkConfig{
				URL:            urlEntry.Text,
				StreamKey:      secret.New(streamKeyEntry.Text),
				StreamSourceID: selectedStreamSourceID,
			},
		)
		if err != nil {
			p.DisplayError(err)
			return
		}
		w.Close()
	})

	w.SetContent(container.NewBorder(
		nil,
		container.NewHBox(saveButton),
		nil,
		nil,
		container.NewVBox(
			widget.NewLabel("Sink ID:"),
			streamSinkIDEntry,
			widget.NewLabel("Dynamic source (optional):"),
			activeStreamSelect,
			widget.NewLabel("Static URL (if no dynamic source):"),
			urlEntry,
			widget.NewLabel("Static stream key (if no dynamic source):"),
			streamKeyEntry,
		),
	))
	w.Show()
}

func (p *Panel) addStreamSink(
	ctx context.Context,
	streamSinkID api.StreamSinkIDFullyQualified,
	config sstypes.StreamSinkConfig,
) error {
	logger.Debugf(ctx, "addStreamSink")
	defer logger.Debugf(ctx, "/addStreamSink")
	return p.StreamD.AddStreamSink(ctx, streamSinkID, config)
}

func (p *Panel) updateStreamSink(
	ctx context.Context,
	streamSinkID api.StreamSinkIDFullyQualified,
	config sstypes.StreamSinkConfig,
) error {
	logger.Debugf(ctx, "updateStreamSink")
	defer logger.Debugf(ctx, "/updateStreamSink")
	return p.StreamD.UpdateStreamSink(ctx, streamSinkID, config)
}

func (p *Panel) displayStreamSinks(
	ctx context.Context,
	sinks []api.StreamSink,
) {
	logger.Debugf(ctx, "displayStreamSinks")
	defer logger.Debugf(ctx, "/displayStreamSinks")

	var objs []fyne.CanvasObject
	for idx, sink := range sinks {
		logger.Tracef(ctx, "sinks[%3d] == %#+v", idx, sink)
		deleteButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			w := dialog.NewConfirm(
				fmt.Sprintf("Delete sink %s ?", sink.ID),
				"",
				func(b bool) {
					if !b {
						return
					}
					logger.Debugf(ctx, "remove sink")
					defer logger.Debugf(ctx, "/remove sink")
					err := p.StreamD.RemoveStreamSink(ctx, sink.ID)
					if err != nil {
						p.DisplayError(err)
						return
					}
				},
				p.mainWindow,
			)
			w.Show()
		})
		editButton := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
			p.openEditSinkWindow(ctx, sink)
		})

		label := widget.NewLabel(sink.ID.String() + ": " + sink.URL)
		objs = append(objs, container.NewHBox(
			deleteButton,
			editButton,
			label,
		))
	}
	p.updateObjects(p.sinksWidget, objs)
}

func (p *Panel) openAddPlayerWindow(ctx context.Context) {
	p.openAddOrEditPlayerWindow(
		ctx,
		"Add player",
		false,
		"",
		sptypes.DefaultConfig(ctx),
		nil,
		p.StreamD.AddStreamPlayer,
	)
}

func (p *Panel) openEditPlayerWindow(
	ctx context.Context,
	streamSourceID api.StreamSourceID,
) {
	cfg, err := p.GetStreamDConfig(ctx)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to get the current config: %w", err))
		return
	}
	streamCfg, ok := cfg.StreamServer.Streams[streamSourceID]
	if !ok {
		p.DisplayError(fmt.Errorf("unable to find a stream '%s'", streamSourceID))
		return
	}
	playerCfg := streamCfg.Player
	if playerCfg == nil {
		p.DisplayError(fmt.Errorf("unable to find a stream player for '%s'", streamSourceID))
		return
	}
	p.openAddOrEditPlayerWindow(
		ctx,
		"Edit player",
		!playerCfg.Disabled,
		playerCfg.Player,
		playerCfg.StreamPlayback,
		&streamSourceID,
		p.StreamD.UpdateStreamPlayer,
	)
}

func (p *Panel) openAddOrEditPlayerWindow(
	ctx context.Context,
	title string,
	isEnabled bool,
	backend player.Backend,
	cfg sptypes.Config,
	forceStreamSourceID *api.StreamSourceID,
	addOrEditStreamPlayer func(
		ctx context.Context,
		streamSourceID api.StreamSourceID,
		playerType player.Backend,
		disabled bool,
		streamPlaybackConfig sptypes.Config,
	) error,
) {
	w := p.app.NewWindow(consts.AppName + ": " + title)
	resizeWindow(w, fyne.NewSize(400, 300))

	var playerStrs []string
	for _, p := range player.SupportedBackends() {
		playerStrs = append(playerStrs, string(p))
	}
	if len(playerStrs) == 0 {
		p.DisplayError(fmt.Errorf("no players supported in this build of the application"))
		return
	}
	playerSelect := widget.NewSelect(playerStrs, func(s string) {})
	if backend != "" {
		playerSelect.SetSelected(string(backend))
	} else {
		playerSelect.SetSelectedIndex(0)
	}

	inStreams, err := p.StreamD.ListStreamSources(ctx)
	if err != nil {
		p.DisplayError(err)
		return
	}

	var inStreamStrs []string
	for _, inStream := range inStreams {
		inStreamStrs = append(inStreamStrs, string(inStream.StreamSourceID))
	}
	inStreamsSelect := widget.NewSelect(inStreamStrs, func(s string) {})
	if forceStreamSourceID != nil {
		inStreamsSelect.SetSelected(string(*forceStreamSourceID))
		inStreamsSelect.Disable()
	}

	enableObserverCheckbox := widget.NewCheck("Enable stream validator", func(b bool) {
		cfg.EnableObserver = b
	})
	enableObserverCheckbox.SetChecked(cfg.EnableObserver)

	forceWaitForLocalPublisherCheckbox := widget.NewCheck("Force waiting for local publisher", func(b bool) {
		cfg.ForceWaitForPublisher = b
	})
	forceWaitForLocalPublisherCheckbox.SetChecked(cfg.ForceWaitForPublisher)

	overrideURL := widget.NewEntry()
	overrideURL.SetText(cfg.OverrideURL)
	overrideURL.SetPlaceHolder("rtmp://127.0.0.1/some/stream")
	overrideURL.OnChanged = func(s string) {
		cfg.OverrideURL = s
		if s == "" {
			forceWaitForLocalPublisherCheckbox.Disable()
		} else {
			forceWaitForLocalPublisherCheckbox.Enable()
		}
	}
	overrideURL.OnChanged(overrideURL.Text)

	jitterBufMinDuration := xfyne.NewNumericalEntry()
	jitterBufMinDuration.SetPlaceHolder("amount of seconds")
	jitterBufMinDuration.SetText(fmt.Sprintf("%v", cfg.JitterBufMinDuration.Seconds()))
	jitterBufMinDuration.OnChanged = func(s string) {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as float: %w", s, err))
			return
		}

		cfg.JitterBufMinDuration = time.Duration(f * float64(time.Second))
	}

	jitterBufMaxDuration := xfyne.NewNumericalEntry()
	jitterBufMaxDuration.SetPlaceHolder("amount of seconds")
	jitterBufMaxDuration.SetText(fmt.Sprintf("%v", cfg.JitterBufMaxDuration.Seconds()))
	jitterBufMaxDuration.OnChanged = func(s string) {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as float: %w", s, err))
			return
		}

		cfg.JitterBufMaxDuration = time.Duration(f * float64(time.Second))
	}

	maxCatchupAtLag := xfyne.NewNumericalEntry()
	maxCatchupAtLag.SetPlaceHolder("amount of seconds")
	maxCatchupAtLag.SetText(fmt.Sprintf("%v", cfg.CatchupAtMaxLag.Seconds()))
	maxCatchupAtLag.OnChanged = func(s string) {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as float: %w", s, err))
			return
		}

		cfg.CatchupAtMaxLag = time.Duration(f * float64(time.Second))
	}

	startTimeout := xfyne.NewNumericalEntry()
	startTimeout.SetPlaceHolder("amount of seconds")
	startTimeout.SetText(fmt.Sprintf("%v", cfg.StartTimeout.Seconds()))
	startTimeout.OnChanged = func(s string) {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as float: %w", s, err))
			return
		}

		cfg.StartTimeout = time.Duration(f * float64(time.Second))
	}

	readTimeout := xfyne.NewNumericalEntry()
	readTimeout.SetPlaceHolder("amount of seconds")
	readTimeout.SetText(fmt.Sprintf("%v", cfg.ReadTimeout.Seconds()))
	readTimeout.OnChanged = func(s string) {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as float: %w", s, err))
			return
		}

		cfg.ReadTimeout = time.Duration(f * float64(time.Second))
	}

	catchupMaxSpeedFactor := xfyne.NewNumericalEntry()
	catchupMaxSpeedFactor.SetPlaceHolder("2.0")
	catchupMaxSpeedFactor.SetText(fmt.Sprintf("%v", cfg.CatchupMaxSpeedFactor))
	catchupMaxSpeedFactor.OnChanged = func(s string) {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as float: %w", s, err))
			return
		}

		cfg.CatchupMaxSpeedFactor = f
	}

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		if inStreamsSelect.Selected == "" {
			p.DisplayError(fmt.Errorf("stream is not selected"))
			return
		}
		err := addOrEditStreamPlayer(
			ctx,
			api.StreamSourceID(inStreamsSelect.Selected),
			player.Backend(playerSelect.Selected),
			!isEnabled,
			cfg,
		)
		if err != nil {
			p.DisplayError(err)
			return
		}
		w.Close()
	})

	w.SetContent(container.NewBorder(
		nil,
		container.NewHBox(saveButton),
		nil,
		nil,
		container.NewVBox(
			widget.NewLabel("Player:"),
			playerSelect,
			enableObserverCheckbox,
			widget.NewLabel("Stream:"),
			inStreamsSelect,
			widget.NewLabel("Override URL:"),
			overrideURL,
			forceWaitForLocalPublisherCheckbox,
			widget.NewSeparator(),
			widget.NewSeparator(),
			widget.NewLabel("Start timeout (seconds):"),
			startTimeout,
			widget.NewLabel("Read timeout (seconds):"),
			readTimeout,
			widget.NewLabel("Jitter buffer min size (seconds):"),
			jitterBufMinDuration,
			widget.NewLabel("Jitter buffer max size (seconds):"),
			jitterBufMaxDuration,
			widget.NewLabel("Maximal catchup speed (float):"),
			catchupMaxSpeedFactor,
			widget.NewLabel("Maximal catchup at lab (seconds):"),
			maxCatchupAtLag,
		),
	))
	w.Show()
}

func (p *Panel) displayStreamPlayers(
	ctx context.Context,
	players []api.StreamPlayer,
) {
	logger.Debugf(ctx, "displayStreamPlayers")
	defer logger.Debugf(ctx, "/displayStreamPlayers")

	hasDynamicValue := false

	sort.Slice(players, func(i, j int) bool {
		return players[i].StreamSourceID < players[j].StreamSourceID
	})

	var objs []fyne.CanvasObject
	for idx, player := range players {
		logger.Tracef(ctx, "players[%3d] == %#+v", idx, player)
		c := container.NewHBox()
		deleteButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			w := dialog.NewConfirm(
				fmt.Sprintf(
					"Delete player for stream '%s' (%s) ?",
					player.StreamSourceID,
					player.PlayerType,
				),
				"",
				func(b bool) {
					if !b {
						return
					}
					logger.Debugf(
						ctx,
						"remove player '%s' (%s)",
						player.StreamSourceID,
						player.PlayerType,
					)
					defer logger.Debugf(
						ctx,
						"/remove player '%s' (%s)",
						player.StreamSourceID,
						player.PlayerType,
					)
					err := p.StreamD.RemoveStreamPlayer(ctx, player.StreamSourceID)
					if err != nil {
						p.DisplayError(err)
						return
					}
				},
				p.mainWindow,
			)
			w.Show()
		})
		editButton := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
			p.openEditPlayerWindow(ctx, player.StreamSourceID)
		})
		icon := theme.MediaStopIcon()
		label := "Stop"
		title := fmt.Sprintf("Stop %s on '%s' ?", player.PlayerType, player.StreamSourceID)
		if player.Disabled {
			icon = theme.MediaPlayIcon()
			label = "Start"
			title = fmt.Sprintf("Start %s on '%s' ?", player.PlayerType, player.StreamSourceID)
		}
		playPauseButton := widget.NewButtonWithIcon(label, icon, func() {
			w := dialog.NewConfirm(
				title,
				"",
				func(b bool) {
					if !b {
						return
					}
					logger.Debugf(
						ctx,
						"stop/start player %s on '%s': disabled:%v->%v",
						player.PlayerType,
						player.StreamSourceID,
						player.Disabled,
						!player.Disabled,
					)
					defer logger.Debugf(
						ctx,
						"/stop/start player %s on '%s': disabled:%v->%v",
						player.PlayerType,
						player.StreamSourceID,
						player.Disabled,
						!player.Disabled,
					)
					err := p.StreamD.UpdateStreamPlayer(
						xcontext.DetachDone(ctx),
						player.StreamSourceID,
						player.PlayerType,
						!player.Disabled,
						player.StreamPlaybackConfig,
					)
					if err != nil {
						p.DisplayError(err)
						return
					}
				},
				p.mainWindow,
			)
			w.Show()
		})
		caption := widget.NewLabel(string(player.StreamSourceID) + " (" + string(player.PlayerType) + ")")
		c.RemoveAll()
		c.Add(deleteButton)
		c.Add(editButton)
		c.Add(playPauseButton)
		c.Add(caption)
		if !player.Disabled {
			pos, err := p.StreamD.StreamPlayerGetPosition(ctx, player.StreamSourceID)
			if err != nil {
				logger.Debugf(
					ctx,
					"unable to get the current position at player '%s': %v",
					player.StreamSourceID,
					err,
				)
			} else {
				c.Add(widget.NewSeparator())
				posStr := pos.String()
				posLabel := widget.NewLabel(posStr)
				hasDynamicValue = hasDynamicValue || posStr != ""
				c.Add(posLabel)
			}
		}
		objs = append(objs, c)
	}
	p.updateObjects(p.playersWidget, objs)

	p.streamPlayersLocker.Do(ctx, func() {
		cancelFn := p.streamPlayersUpdaterCanceller
		if hasDynamicValue {
			if cancelFn == nil {
				p.streamPlayersUpdaterCanceller = p.startStreamPlayersUpdater(ctx)
			}
		} else {
			if cancelFn != nil {
				cancelFn()
				p.streamPlayersUpdaterCanceller = nil
			}
		}
	})
}

func (p *Panel) openEditRestreamWindow(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
	streamSinkID api.StreamSinkIDFullyQualified,
) {
	cfg, err := p.GetStreamDConfig(ctx)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to get the current config: %w", err))
		return
	}
	streamCfg, ok := cfg.StreamServer.Streams[streamSourceID]
	if !ok {
		p.DisplayError(fmt.Errorf("unable to find a stream '%s'", streamSourceID))
		return
	}
	fwd, ok := streamCfg.Forwardings[streamSinkID]
	if !ok {
		p.DisplayError(fmt.Errorf("unable to find a stream forwarding %s -> %s", streamSourceID, streamSinkID))
		return
	}
	p.openAddOrEditRestreamWindow(
		ctx,
		"Edit the restreaming (stream forwarding)",
		streamSourceID,
		streamSinkID,
		fwd,
		p.updateStreamForward,
	)
}

func (p *Panel) openAddRestreamWindow(ctx context.Context) {
	p.openAddOrEditRestreamWindow(
		ctx,
		"Add a restreaming (stream forwarding)",
		"",
		api.StreamSinkIDFullyQualified{},
		sstypes.ForwardingConfig{
			Disabled: false,
			Quirks: sstypes.ForwardingQuirks{
				RestartUntilPlatformRecognizesStream: sstypes.DefaultRestartUntilPlatformRecognizesStreamConfig(),
			},
		},
		p.addStreamForward,
	)
}

func (p *Panel) openAddOrEditRestreamWindow(
	ctx context.Context,
	title string,
	streamSourceID streamtypes.StreamSourceID,
	streamSinkID api.StreamSinkIDFullyQualified,
	fwd sstypes.ForwardingConfig,
	addOrEditStreamForward func(
		ctx context.Context,
		streamSourceID api.StreamSourceID,
		streamSinkID api.StreamSinkIDFullyQualified,
		enabled bool,
		encode sstypes.EncodeConfig,
		quirks sstypes.ForwardingQuirks,
	) error,
) {
	logger.Debugf(
		ctx,
		"openAddOrEditRestreamWindow(ctx, '%s', '%s', '%s', %#+v)",
		title,
		streamSourceID,
		streamSinkID,
		fwd,
	)
	defer logger.Debugf(
		ctx,
		"/openAddOrEditRestreamWindow(ctx, '%s', '%s', '%s', %#+v)",
		title,
		streamSourceID,
		streamSinkID,
		fwd,
	)
	w := p.app.NewWindow(consts.AppName + ": " + title)
	resizeWindow(w, fyne.NewSize(400, 300))

	enabledCheck := widget.NewCheck("Enable", func(b bool) {})

	inStreams, err := p.StreamD.ListStreamSources(ctx)
	if err != nil {
		p.DisplayError(err)
		return
	}

	sinks, err := p.StreamD.ListStreamSinks(ctx)
	if err != nil {
		p.DisplayError(err)
		return
	}

	var inStreamStrs []string
	for _, inStream := range inStreams {
		inStreamStrs = append(inStreamStrs, string(inStream.StreamSourceID))
	}
	inStreamsSelect := widget.NewSelect(inStreamStrs, func(s string) {})
	if streamSourceID != "" {
		inStreamsSelect.SetSelected(string(streamSourceID))
		inStreamsSelect.Disable()
	}

	var sinkStrs []string
	sinkMapCaption2ID := map[string]api.StreamSinkIDFullyQualified{}
	sinkMapID2Caption := map[api.StreamSinkIDFullyQualified]string{}
	for _, sink := range sinks {
		k := sink.ID.String() + ": " + sink.URL
		sinkStrs = append(sinkStrs, k)
		sinkMapCaption2ID[k] = sink.ID
		sinkMapID2Caption[sink.ID] = k
	}
	sinkSelect := widget.NewSelect(sinkStrs, func(s string) {})
	if streamSinkID.ID != "" {
		sinkSelect.SetSelected(sinkMapID2Caption[streamSinkID])
		sinkSelect.Disable()
	}

	if len(fwd.Encode.OutputVideoTracks) == 0 {
		fwd.Encode.OutputVideoTracks = append(fwd.Encode.OutputVideoTracks, recoder.VideoTrackEncodingConfig{
			InputTrackIDs: []int{0, 1, 2, 3, 4, 5, 6, 7},
			Config: recoder.EncodeVideoConfig{
				Codec:         recoder.VideoCodecCopy,
				Quality:       ptr(recoder.VideoQualityConstantBitrate(6000000)), // Twitch has the 6Mbps limitation
				CustomOptions: nil,
			},
		})
	}

	if len(fwd.Encode.OutputAudioTracks) == 0 {
		fwd.Encode.OutputAudioTracks = append(fwd.Encode.OutputAudioTracks, recoder.AudioTrackEncodingConfig{
			InputTrackIDs: []int{0, 1, 2, 3, 4, 5, 6, 7},
			Config: recoder.EncodeAudioConfig{
				Codec:         recoder.AudioCodecCopy,
				Quality:       ptr(recoder.AudioQualityConstantBitrate(128000)), // https://wiki.xiph.org/Opus_Recommended_Settings
				CustomOptions: nil,
			},
		})
	}

	var videoCodecStrs []string
	var videoCodecs []recoder.VideoCodec
	for videoCodec := recoder.VideoCodecCopy; videoCodec < recoder.EndOfVideoCodec; videoCodec++ {
		videoCodecs = append(videoCodecs, videoCodec)
		videoCodecStrs = append(videoCodecStrs, ptr(videoCodec).String())
	}
	videoCodecStrs = append(videoCodecStrs, trackRemove)

	var audioCodecStrs []string
	var audioCodecs []recoder.AudioCodec
	for audioCodec := recoder.AudioCodecCopy; audioCodec < recoder.EndOfAudioCodec; audioCodec++ {
		audioCodecs = append(audioCodecs, audioCodec)
		audioCodecStrs = append(audioCodecStrs, ptr(audioCodec).String())
	}
	audioCodecStrs = append(audioCodecStrs, trackRemove)

	recodingVideoLabel := widget.NewLabel("Video:")
	recodingAudioLabel := widget.NewLabel("Audio:")

	recodingVideoBitrate := xfyne.NewNumericalEntry()
	recodingVideoBitrate.OnChanged = func(s string) {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			p.DisplayError(err)
		}
		fwd.Encode.OutputVideoTracks[0].Config.Quality = ptr(recoder.VideoQualityConstantBitrate(v))
	}
	switch q := fwd.Encode.OutputVideoTracks[0].Config.Quality.(type) {
	case *recoder.VideoQualityConstantBitrate:
		recodingVideoBitrate.SetText(fmt.Sprintf("%d", uint(*q)))
	}
	recodingVideoCodecSelector := widget.NewSelect(videoCodecStrs, func(s string) {
		switch s {
		case ptr(recoder.VideoCodecCopy).String():
			recodingVideoBitrate.Disable()
			fwd.Encode.OutputVideoTracks = fwd.Encode.OutputVideoTracks[:1]
			fwd.Encode.OutputVideoTracks[0].Config.Codec = recoder.VideoCodecCopy
		case trackRemove:
			recodingVideoBitrate.Disable()
			fwd.Encode.OutputVideoTracks = fwd.Encode.OutputVideoTracks[:0]
		default:
			recodingVideoBitrate.Enable()
			fwd.Encode.OutputVideoTracks = fwd.Encode.OutputVideoTracks[:1]
			for _, videoCodec := range videoCodecs {
				if ptr(videoCodec).String() == s {
					fwd.Encode.OutputVideoTracks[0].Config.Codec = videoCodec
				}
			}
		}
	})
	if fwd.Encode.OutputVideoTracks[0].Config.Codec == recoder.UndefinedVideoCodec {
		recodingVideoCodecSelector.SetSelectedIndex(0)
	} else {
		recodingVideoCodecSelector.SetSelected(fwd.Encode.OutputVideoTracks[0].Config.Codec.String())
	}
	recodingAudioBitrate := xfyne.NewNumericalEntry()
	recodingAudioBitrate.OnChanged = func(s string) {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			p.DisplayError(err)
		}
		fwd.Encode.OutputAudioTracks[0].Config.Quality = ptr(recoder.AudioQualityConstantBitrate(v))
	}
	switch q := fwd.Encode.OutputAudioTracks[0].Config.Quality.(type) {
	case *recoder.AudioQualityConstantBitrate:
		recodingAudioBitrate.SetText(fmt.Sprintf("%d", uint(*q)))
	}
	recodingAudioCodecSelector := widget.NewSelect(audioCodecStrs, func(s string) {
		switch s {
		case ptr(recoder.AudioCodecCopy).String():
			recodingAudioBitrate.Disable()
			fwd.Encode.OutputAudioTracks = fwd.Encode.OutputAudioTracks[:1]
			fwd.Encode.OutputAudioTracks[0].Config.Codec = recoder.AudioCodecCopy
		case trackRemove:
			recodingAudioBitrate.Disable()
			fwd.Encode.OutputAudioTracks = fwd.Encode.OutputAudioTracks[:0]
		default:
			recodingAudioBitrate.Enable()
			fwd.Encode.OutputAudioTracks = fwd.Encode.OutputAudioTracks[:1]
			for _, audioCodec := range audioCodecs {
				if ptr(audioCodec).String() == s {
					fwd.Encode.OutputAudioTracks[0].Config.Codec = audioCodec
				}
			}
		}
	})
	if fwd.Encode.OutputAudioTracks[0].Config.Codec == recoder.UndefinedAudioCodec {
		recodingAudioCodecSelector.SetSelectedIndex(0)
	} else {
		recodingAudioCodecSelector.SetSelected(fwd.Encode.OutputAudioTracks[0].Config.Codec.String())
	}
	enableRecodingCheckbox := widget.NewCheck("Enable recoding", func(b bool) {
		fwd.Encode.Enabled = b
		if b {
			recodingVideoLabel.Show()
			recodingAudioLabel.Show()
			recodingVideoCodecSelector.Enable()
			recodingVideoCodecSelector.OnChanged(recodingVideoCodecSelector.Selected)
			recodingAudioCodecSelector.Enable()
			recodingAudioCodecSelector.OnChanged(recodingAudioCodecSelector.Selected)
		} else {
			recodingVideoCodecSelector.Disable()
			recodingVideoBitrate.Disable()
			recodingAudioCodecSelector.Disable()
			recodingAudioBitrate.Disable()
			recodingVideoLabel.Hide()
			recodingAudioLabel.Hide()
		}
	})
	enableRecodingCheckbox.SetChecked(fwd.Encode.Enabled)
	enableRecodingCheckbox.OnChanged(fwd.Encode.Enabled)

	var restartUntilYouTubeStarts *widget.Check

	quirksStartAfterYoutube := fwd.Quirks.WaitUntilPlatformRecognizesStream
	startAfterYoutubeCheckbox := widget.NewCheck(
		"Start this stream only after YouTube recognized a stream",
		func(b bool) {
			quirksStartAfterYoutube.Enabled = b
			if b {
				restartUntilYouTubeStarts.SetChecked(false)
				restartUntilYouTubeStarts.Disable()
			} else {
				restartUntilYouTubeStarts.Enable()
			}
		},
	)

	quirksYoutubeRestart := fwd.Quirks.RestartUntilPlatformRecognizesStream

	if !quirksYoutubeRestart.Enabled {
		if quirksYoutubeRestart.StartTimeout == 0 {
			quirksYoutubeRestart.StartTimeout = sstypes.DefaultRestartUntilPlatformRecognizesStreamConfig().StartTimeout
		}
		if quirksYoutubeRestart.StopStartDelay == 0 {
			quirksYoutubeRestart.StopStartDelay = sstypes.DefaultRestartUntilPlatformRecognizesStreamConfig().StopStartDelay
		}
	}

	youtubeStartTimeout := xfyne.NewNumericalEntry()
	youtubeStartTimeout.SetText(fmt.Sprintf("%v", quirksYoutubeRestart.StartTimeout.Seconds()))
	youtubeStartTimeout.OnChanged = func(s string) {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(err)
			return
		}
		quirksYoutubeRestart.StartTimeout = time.Duration(v * float64(time.Second))
	}
	youtubeStopStartDelay := xfyne.NewNumericalEntry()
	youtubeStopStartDelay.SetText(fmt.Sprintf("%v", quirksYoutubeRestart.StopStartDelay.Seconds()))
	youtubeStopStartDelay.OnChanged = func(s string) {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(err)
			return
		}
		quirksYoutubeRestart.StopStartDelay = time.Duration(v * float64(time.Second))
	}
	restartUntilYouTubeStartsParams := container.NewVBox(
		widget.NewLabel("Wait until try restarting (seconds):"),
		youtubeStartTimeout,
		widget.NewLabel("Delay between stopping and starting (seconds):"),
		youtubeStopStartDelay,
	)
	restartUntilYouTubeStartsParams.Hide()

	restartUntilYouTubeStarts = widget.NewCheck(
		"Restart until YouTube recognizes the stream",
		func(b bool) {
			quirksYoutubeRestart.Enabled = b
			if b {
				restartUntilYouTubeStartsParams.Show()
				startAfterYoutubeCheckbox.SetChecked(false)
				startAfterYoutubeCheckbox.Disable()
			} else {
				restartUntilYouTubeStartsParams.Hide()
				startAfterYoutubeCheckbox.Enable()
			}
		},
	)
	startAfterYoutubeCheckbox.SetChecked(quirksStartAfterYoutube.Enabled)
	restartUntilYouTubeStarts.SetChecked(quirksYoutubeRestart.Enabled)

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		err := addOrEditStreamForward(
			ctx,
			streamtypes.StreamSourceID(inStreamsSelect.Selected),
			sinkMapCaption2ID[sinkSelect.Selected],
			enabledCheck.Checked,
			fwd.Encode,
			sstypes.ForwardingQuirks{
				RestartUntilPlatformRecognizesStream: quirksYoutubeRestart,
				WaitUntilPlatformRecognizesStream:    quirksStartAfterYoutube,
			},
		)
		if err != nil {
			p.DisplayError(err)
			return
		}
		w.Close()
	})

	w.SetContent(container.NewBorder(
		nil,
		container.NewHBox(saveButton),
		nil,
		nil,
		container.NewVBox(
			widget.NewLabel("From:"),
			inStreamsSelect,
			widget.NewLabel("To:"),
			sinkSelect,
			widget.NewSeparator(),
			widget.NewSeparator(),
			widget.NewRichTextFromMarkdown("## Recode"),
			enableRecodingCheckbox,
			recodingVideoLabel,
			recodingVideoCodecSelector,
			recodingVideoBitrate,
			recodingAudioLabel,
			recodingAudioCodecSelector,
			recodingAudioBitrate,
			widget.NewRichTextFromMarkdown("## Quirks"),
			startAfterYoutubeCheckbox,
			restartUntilYouTubeStarts,
			restartUntilYouTubeStartsParams,
		),
	))
	w.Show()
}

func (p *Panel) updateStreamForward(
	ctx context.Context,
	streamSourceID api.StreamSourceID,
	streamSinkID api.StreamSinkIDFullyQualified,
	enabled bool,
	encode sstypes.EncodeConfig,
	quirks sstypes.ForwardingQuirks,
) error {
	logger.Debugf(ctx, "updateStreamForward")
	defer logger.Debugf(ctx, "/updateStreamForward")
	return p.StreamD.UpdateStreamForward(
		ctx,
		streamSourceID,
		streamSinkID,
		enabled,
		encode,
		quirks,
	)
}

func (p *Panel) addStreamForward(
	ctx context.Context,
	streamSourceID api.StreamSourceID,
	streamSinkID api.StreamSinkIDFullyQualified,
	enabled bool,
	encode sstypes.EncodeConfig,
	quirks sstypes.ForwardingQuirks,
) error {
	logger.Debugf(ctx, "addStreamForward")
	defer logger.Debugf(ctx, "/addStreamForward")
	return p.StreamD.AddStreamForward(
		ctx,
		streamSourceID,
		streamSinkID,
		enabled,
		encode,
		quirks,
	)
}

func (p *Panel) displayStreamForwards(
	ctx context.Context,
	fwds []api.StreamForward,
) {
	logger.Debugf(ctx, "displayStreamForwards")
	defer logger.Debugf(ctx, "/displayStreamForwards")

	hasDynamicValue := false

	sort.Slice(fwds, func(i, j int) bool {
		if fwds[i].StreamSourceID != fwds[j].StreamSourceID {
			return fwds[i].StreamSourceID < fwds[j].StreamSourceID
		}
		return fwds[i].StreamSinkID.String() < fwds[j].StreamSinkID.String()
	})

	var objs []fyne.CanvasObject
	logger.Tracef(ctx, "len(fwds) == %d", len(fwds))
	for idx, fwd := range fwds {
		logger.Tracef(ctx, "fwds[%3d] == %#+v", idx, fwd)
		c := container.NewHBox()
		deleteButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			w := dialog.NewConfirm(
				fmt.Sprintf(
					"Delete restreaming (stream forwarding) %s -> %s ?",
					fwd.StreamSourceID,
					fwd.StreamSinkID,
				),
				"",
				func(b bool) {
					if !b {
						return
					}
					logger.Debugf(ctx, "remove restreaming (stream forwarding)")
					defer logger.Debugf(ctx, "/remove restreaming (stream forwarding)")
					err := p.StreamD.RemoveStreamForward(ctx, fwd.StreamSourceID, fwd.StreamSinkID)
					if err != nil {
						p.DisplayError(err)
						return
					}
				},
				p.mainWindow,
			)
			w.Show()
		})
		editButton := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
			p.openEditRestreamWindow(ctx, fwd.StreamSourceID, fwd.StreamSinkID)
		})
		icon := theme.MediaPauseIcon()
		label := "Pause"
		title := fmt.Sprintf("Pause forwarding %s -> %s ?", fwd.StreamSourceID, fwd.StreamSinkID)
		if !fwd.Enabled {
			icon = theme.MediaPlayIcon()
			label = "Unpause"
			title = fmt.Sprintf("Unpause forwarding %s -> %s ?", fwd.StreamSourceID, fwd.StreamSinkID)
		}
		playPauseButton := widget.NewButtonWithIcon(label, icon, func() {
			w := dialog.NewConfirm(
				title,
				"",
				func(b bool) {
					if !b {
						return
					}
					logger.Debugf(
						ctx,
						"pause/unpause restreaming (stream forwarding): enabled:%v->%v",
						fwd.Enabled,
						!fwd.Enabled,
					)
					defer logger.Debugf(
						ctx,
						"/pause/unpause restreaming (stream forwarding): enabled:%v->%v",
						!fwd.Enabled,
						fwd.Enabled,
					)
					err := p.StreamD.UpdateStreamForward(
						ctx,
						fwd.StreamSourceID,
						fwd.StreamSinkID,
						!fwd.Enabled,
						fwd.Encode,
						fwd.Quirks,
					)
					if err != nil {
						p.DisplayError(err)
						return
					}
				},
				p.mainWindow,
			)
			w.Show()
		})
		captionStr := string(fwd.StreamSourceID) + " -> " + fwd.StreamSinkID.String()
		var quirksStrings []string
		if fwd.Quirks.RestartUntilPlatformRecognizesStream.Enabled {
			quirksStrings = append(quirksStrings, "YT-restart")
		}
		if fwd.Quirks.WaitUntilPlatformRecognizesStream.Enabled {
			quirksStrings = append(quirksStrings, "after-YT")
		}
		if fwd.Encode.Enabled {
			audioTrackCodecString := "<removed>"
			videoTrackCodecString := "<removed>"
			if len(fwd.Encode.OutputVideoTracks) > 0 {
				videoTrackCodecString = fwd.Encode.OutputVideoTracks[0].Config.Codec.String()
			}
			if len(fwd.Encode.OutputAudioTracks) > 0 {
				audioTrackCodecString = fwd.Encode.OutputAudioTracks[0].Config.Codec.String()
			}
			captionStr += fmt.Sprintf(" [%s/%s]", videoTrackCodecString, audioTrackCodecString)
		}
		if len(quirksStrings) != 0 {
			captionStr += fmt.Sprintf(" (%s)", strings.Join(quirksStrings, ","))
		}
		caption := widget.NewLabel(captionStr)
		c.RemoveAll()
		c.Add(deleteButton)
		c.Add(editButton)
		c.Add(playPauseButton)
		c.Add(caption)
		if fwd.Enabled {
			c.Add(widget.NewSeparator())

			type numBytesID struct {
				StreamSourceID api.StreamSourceID
				DstID          api.StreamSinkIDFullyQualified
			}
			key := numBytesID{StreamSourceID: fwd.StreamSourceID, DstID: fwd.StreamSinkID}
			now := clock.Get().Now()
			p.previousNumBytesLocker.Do(ctx, func() {
				prevNumBytes := p.previousNumBytes[key]
				bwStr := bwString(
					fwd.NumBytesRead,
					prevNumBytes[0],
					fwd.NumBytesWrote,
					prevNumBytes[1],
					now,
					p.previousNumBytesTS[key],
				)
				bwText := widget.NewRichTextWithText(bwStr)
				hasDynamicValue = hasDynamicValue || bwStr != ""
				p.previousNumBytes[key] = [4]uint64{fwd.NumBytesRead, fwd.NumBytesWrote}
				p.previousNumBytesTS[key] = now
				c.Add(bwText)
			})
		}
		objs = append(objs, c)
	}
	p.updateObjects(p.restreamsWidget, objs)

	p.streamForwardersLocker.Do(ctx, func() {
		cancelFn := p.streamForwardersUpdaterCanceller
		if hasDynamicValue {
			if cancelFn == nil {
				p.streamForwardersUpdaterCanceller = p.startStreamForwardersUpdater(ctx)
			}
		} else {
			if cancelFn != nil {
				cancelFn()
				p.streamForwardersUpdaterCanceller = nil
			}
		}
	})
}

func (p *Panel) streamServersUpdater(
	ctx context.Context,
) context.CancelFunc {
	ctx, cancelFn := context.WithCancel(ctx)
	observability.Go(ctx, func(ctx context.Context) {
		logger.Debugf(ctx, "streamServersUpdater: updater")
		defer logger.Debugf(ctx, "streamServersUpdater: /updater")

		updateData := func(ctx context.Context) {
			streamServers, err := p.StreamD.ListStreamServers(ctx)
			if err != nil {
				p.DisplayError(err)
			} else {
				p.displayStreamServers(ctx, streamServers)
			}
		}

		defer func() {
			p.streamServersLocker.Do(ctx, func() {
				cancelFn()
				p.streamServersUpdaterCanceller = nil
			})
		}()

		logger.Debugf(ctx, "streamServersUpdater")
		defer logger.Debugf(ctx, "/streamServersUpdater")

		t := clock.Get().Ticker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			updateData(ctx)
		}
	})
	return cancelFn
}

func (p *Panel) startStreamPlayersUpdater(
	ctx context.Context,
) context.CancelFunc {
	ctx, cancelFn := context.WithCancel(ctx)
	observability.Go(ctx, func(ctx context.Context) {
		logger.Debugf(ctx, "startStreamPlayersUpdater: updater")
		defer logger.Debugf(ctx, "startStreamPlayersUpdater: /updater")

		updateData := func(ctx context.Context) {
			streamPlayers, err := p.StreamD.ListStreamPlayers(ctx)
			if err != nil {
				p.DisplayError(err)
			} else {
				p.displayStreamPlayers(ctx, streamPlayers)
			}
		}

		defer func() {
			p.streamPlayersLocker.Do(ctx, func() {
				p.streamPlayersUpdaterCanceller = nil
				observability.Go(ctx, updateData)
			})
		}()

		logger.Debugf(ctx, "streamPlayersUpdater")
		defer logger.Debugf(ctx, "/streamPlayersUpdater")

		t := clock.Get().Ticker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			updateData(ctx)
		}
	})
	return cancelFn
}

func (p *Panel) startStreamForwardersUpdater(
	ctx context.Context,
) context.CancelFunc {
	ctx, cancelFn := context.WithCancel(ctx)
	observability.Go(ctx, func(ctx context.Context) {
		logger.Debugf(ctx, "startStreamForwardersUpdater: updater")
		defer logger.Debugf(ctx, "startStreamForwardersUpdater: /updater")

		updateData := func(ctx context.Context) {
			streamFwds, err := p.StreamD.ListStreamForwards(ctx)
			if err != nil {
				p.DisplayError(err)
			} else {
				p.displayStreamForwards(ctx, streamFwds)
			}
		}

		defer func() {
			p.streamForwardersLocker.Do(ctx, func() {
				p.streamForwardersUpdaterCanceller = nil
				observability.Go(ctx, updateData)
			})
		}()

		logger.Debugf(ctx, "streamForwardersUpdater")
		defer logger.Debugf(ctx, "/streamForwardersUpdater")

		t := clock.Get().Ticker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			updateData(ctx)
		}
	})
	return cancelFn
}
