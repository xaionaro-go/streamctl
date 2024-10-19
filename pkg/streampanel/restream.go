package streampanel

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
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	sstypes "github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/streamctl/pkg/xcontext"
	"github.com/xaionaro-go/streamctl/pkg/xfyne"
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

	observability.Go(ctx, func() {
		updateData := func() {
			inStreams, err := p.StreamD.ListIncomingStreams(ctx)
			if err != nil {
				p.DisplayError(err)
			} else {
				p.displayIncomingServers(ctx, inStreams)
			}
		}
		updateData()

		ch, err := p.StreamD.SubscribeToIncomingStreamsChanges(ctx)
		if err != nil {
			p.DisplayError(err)
			return
		}
		for range ch {
			logger.Debugf(ctx, "got event IncomingStreamsChange")
			updateData()
		}
	})

	observability.Go(ctx, func() {
		updateData := func() {
			streamServers, err := p.StreamD.ListStreamServers(ctx)
			if err != nil {
				p.DisplayError(err)
			} else {
				p.displayStreamServers(ctx, streamServers)
			}
		}
		updateData()

		ch, err := p.StreamD.SubscribeToStreamServersChanges(ctx)
		if err != nil {
			p.DisplayError(err)
			return
		}
		for range ch {
			logger.Debugf(ctx, "got event StreamServersChange")
			updateData()
		}
	})

	observability.Go(ctx, func() {
		updateData := func() {
			dsts, err := p.StreamD.ListStreamDestinations(ctx)
			if err != nil {
				p.DisplayError(err)
			} else {
				p.displayStreamDestinations(ctx, dsts)
			}
		}
		updateData()

		ch, err := p.StreamD.SubscribeToStreamDestinationsChanges(ctx)
		if err != nil {
			p.DisplayError(err)
			return
		}
		for range ch {
			logger.Debugf(ctx, "got event StreamDestinationsChange")
			updateData()
		}
	})

	observability.Go(ctx, func() {
		updateData := func() {
			streamFwds, err := p.StreamD.ListStreamForwards(ctx)
			if err != nil {
				p.DisplayError(err)
			} else {
				p.displayStreamForwards(ctx, streamFwds)
			}
		}
		updateData()

		ch, err := p.StreamD.SubscribeToStreamForwardsChanges(ctx)
		if err != nil {
			p.DisplayError(err)
			return
		}
		for range ch {
			logger.Debugf(ctx, "got event StreamForwardsChange")
			updateData()
		}
	})

	observability.Go(ctx, func() {
		updateData := func() {
			streamPlayers, err := p.StreamD.ListStreamPlayers(ctx)
			if err != nil {
				p.DisplayError(err)
			} else {
				p.displayStreamPlayers(ctx, streamPlayers)
			}
		}
		updateData()

		ch, err := p.StreamD.SubscribeToStreamPlayersChanges(ctx)
		if err != nil {
			p.DisplayError(err)
			return
		}
		for range ch {
			logger.Debugf(ctx, "got event StreamPlayersChange")
			updateData()
		}
	})
}

func (p *Panel) openAddStreamServerWindow(ctx context.Context) {
	w := p.app.NewWindow(AppName + ": Add Stream Server")
	resizeWindow(w, fyne.NewSize(400, 300))

	currentProtocol := streamtypes.ServerTypeRTMP
	protocolSelectLabel := widget.NewLabel("Protocol:")
	protocolSelect := widget.NewSelect([]string{
		streamtypes.ServerTypeRTMP.String(),
		streamtypes.ServerTypeRTSP.String(),
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
			now := time.Now()
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
	p.streamServersWidget.Objects = objs
	p.streamServersWidget.Refresh()

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
	w := p.app.NewWindow(AppName + ": Add incoming stream")
	resizeWindow(w, fyne.NewSize(400, 300))

	streamIDEntry := widget.NewEntry()
	streamIDEntry.SetPlaceHolder("stream name")

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		err := p.addIncomingStream(ctx, api.StreamID(streamIDEntry.Text))
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
			streamIDEntry,
		),
	))
	w.Show()
}

func (p *Panel) addIncomingStream(
	ctx context.Context,
	streamID api.StreamID,
) error {
	logger.Debugf(ctx, "addIncomingStream")
	defer logger.Debugf(ctx, "/addIncomingStream")
	return p.StreamD.AddIncomingStream(ctx, streamID)
}

func (p *Panel) displayIncomingServers(
	ctx context.Context,
	inStreams []api.IncomingStream,
) {
	logger.Debugf(ctx, "displayIncomingServers")
	defer logger.Debugf(ctx, "/displayIncomingServers")
	sort.Slice(inStreams, func(i, j int) bool {
		return inStreams[i].StreamID < inStreams[j].StreamID
	})

	var objs []fyne.CanvasObject
	for idx, stream := range inStreams {
		logger.Tracef(ctx, "inStream[%3d] == %#+v", idx, stream)
		c := container.NewHBox()
		button := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			w := dialog.NewConfirm(
				fmt.Sprintf("Delete incoming server %s ?", stream.StreamID),
				"",
				func(b bool) {
					if !b {
						return
					}
					logger.Debugf(ctx, "remove incoming stream")
					defer logger.Debugf(ctx, "/remove incoming stream")
					err := p.StreamD.RemoveIncomingStream(ctx, stream.StreamID)
					if err != nil {
						p.DisplayError(err)
						return
					}
				},
				p.mainWindow,
			)
			w.Show()
		})
		label := widget.NewLabel(string(stream.StreamID))
		c.RemoveAll()
		c.Add(button)
		c.Add(label)
		objs = append(objs, c)
	}
	p.streamsWidget.Objects = objs
	p.streamsWidget.Refresh()
}

func (p *Panel) openAddDestinationWindow(ctx context.Context) {
	p.openAddOrEditDestinationWindow(
		ctx,
		"Add stream destination",
		api.StreamDestination{},
		p.addStreamDestination,
	)
}

func (p *Panel) openEditDestinationWindow(
	ctx context.Context,
	dst api.StreamDestination,
) {
	p.openAddOrEditDestinationWindow(
		ctx,
		fmt.Sprintf("Edit stream destination '%s'", dst.ID),
		dst,
		p.updateStreamDestination,
	)
}

func (p *Panel) openAddOrEditDestinationWindow(
	ctx context.Context,
	title string,
	destination api.StreamDestination,
	commitFn func(
		ctx context.Context,
		destinationID api.DestinationID,
		url string,
		streamKey string,
	) error,
) {
	w := p.app.NewWindow(AppName + ": " + title)
	resizeWindow(w, fyne.NewSize(400, 300))

	destinationIDEntry := widget.NewEntry()
	destinationIDEntry.SetPlaceHolder("destination ID")
	if destination.ID != "" {
		destinationIDEntry.SetText(string(destination.ID))
		destinationIDEntry.Disable()
	}

	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("URL")
	urlEntry.SetText(destination.URL)

	streamKeyEntry := widget.NewEntry()
	streamKeyEntry.SetPlaceHolder("stream key")
	streamKeyEntry.SetText(destination.StreamKey)

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		err := commitFn(
			ctx,
			api.DestinationID(destinationIDEntry.Text),
			urlEntry.Text,
			streamKeyEntry.Text,
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
			destinationIDEntry,
			urlEntry,
			streamKeyEntry,
		),
	))
	w.Show()
}

func (p *Panel) addStreamDestination(
	ctx context.Context,
	destinationID api.DestinationID,
	url string,
	streamKey string,
) error {
	logger.Debugf(ctx, "addStreamDestination")
	defer logger.Debugf(ctx, "/addStreamDestination")
	return p.StreamD.AddStreamDestination(ctx, destinationID, url, streamKey)
}

func (p *Panel) updateStreamDestination(
	ctx context.Context,
	destinationID api.DestinationID,
	url string,
	streamKey string,
) error {
	logger.Debugf(ctx, "updateStreamDestination")
	defer logger.Debugf(ctx, "/updateStreamDestination")
	return p.StreamD.UpdateStreamDestination(ctx, destinationID, url, streamKey)
}

func (p *Panel) displayStreamDestinations(
	ctx context.Context,
	dsts []api.StreamDestination,
) {
	logger.Debugf(ctx, "displayStreamDestinations")
	defer logger.Debugf(ctx, "/displayStreamDestinations")

	var objs []fyne.CanvasObject
	for idx, dst := range dsts {
		logger.Tracef(ctx, "dsts[%3d] == %#+v", idx, dst)
		deleteButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			w := dialog.NewConfirm(
				fmt.Sprintf("Delete destination %s ?", dst.ID),
				"",
				func(b bool) {
					if !b {
						return
					}
					logger.Debugf(ctx, "remove destination")
					defer logger.Debugf(ctx, "/remove destination")
					err := p.StreamD.RemoveStreamDestination(ctx, dst.ID)
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
			p.openEditDestinationWindow(ctx, dst)
		})

		label := widget.NewLabel(string(dst.ID) + ": " + string(dst.URL))
		objs = append(objs, container.NewHBox(
			deleteButton,
			editButton,
			label,
		))
	}
	p.destinationsWidget.Objects = objs
	p.destinationsWidget.Refresh()
}

func (p *Panel) openAddPlayerWindow(ctx context.Context) {
	p.openAddOrEditPlayerWindow(
		ctx,
		"Add player",
		false,
		sptypes.DefaultConfig(ctx),
		nil,
		p.StreamD.AddStreamPlayer,
	)
}

func (p *Panel) openEditPlayerWindow(
	ctx context.Context,
	streamID api.StreamID,
) {
	cfg, err := p.StreamD.GetConfig(ctx)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to get the current config: %w", err))
		return
	}
	streamCfg, ok := cfg.StreamServer.Streams[streamID]
	if !ok {
		p.DisplayError(fmt.Errorf("unable to find a stream '%s'", streamID))
		return
	}
	playerCfg := streamCfg.Player
	if playerCfg == nil {
		p.DisplayError(fmt.Errorf("unable to find a stream player for '%s'", streamID))
		return
	}
	p.openAddOrEditPlayerWindow(
		ctx,
		"Edit player",
		!playerCfg.Disabled,
		playerCfg.StreamPlayback,
		&streamID,
		p.StreamD.UpdateStreamPlayer,
	)
}

func (p *Panel) openAddOrEditPlayerWindow(
	ctx context.Context,
	title string,
	isEnabled bool,
	cfg sptypes.Config,
	forceStreamID *api.StreamID,
	addOrEditStreamPlayer func(
		ctx context.Context,
		streamID api.StreamID,
		playerType player.Backend,
		disabled bool,
		streamPlaybackConfig sptypes.Config,
	) error,
) {
	w := p.app.NewWindow(AppName + ": " + title)
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
	playerSelect.SetSelectedIndex(0)

	inStreams, err := p.StreamD.ListIncomingStreams(ctx)
	if err != nil {
		p.DisplayError(err)
		return
	}

	var inStreamStrs []string
	for _, inStream := range inStreams {
		inStreamStrs = append(inStreamStrs, string(inStream.StreamID))
	}
	inStreamsSelect := widget.NewSelect(inStreamStrs, func(s string) {})
	if forceStreamID != nil {
		inStreamsSelect.SetSelected(string(*forceStreamID))
		inStreamsSelect.Disable()
	}

	overrideURL := widget.NewEntry()
	overrideURL.SetText(cfg.OverrideURL)
	overrideURL.SetPlaceHolder("rtmp://127.0.0.1/some/stream")
	overrideURL.OnChanged = func(s string) {
		cfg.OverrideURL = s
	}

	jitterBufDuration := xfyne.NewNumericalEntry()
	jitterBufDuration.SetPlaceHolder("amount of seconds")
	jitterBufDuration.SetText(fmt.Sprintf("%v", cfg.JitterBufDuration.Seconds()))
	jitterBufDuration.OnChanged = func(s string) {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as float: %w", s, err))
			return
		}

		cfg.JitterBufDuration = time.Duration(f * float64(time.Second))
	}

	maxCatchupAtLag := xfyne.NewNumericalEntry()
	maxCatchupAtLag.SetPlaceHolder("amount of seconds")
	maxCatchupAtLag.SetText(fmt.Sprintf("%v", cfg.MaxCatchupAtLag.Seconds()))
	maxCatchupAtLag.OnChanged = func(s string) {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as float: %w", s, err))
			return
		}

		cfg.MaxCatchupAtLag = time.Duration(f * float64(time.Second))
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
			api.StreamID(inStreamsSelect.Selected),
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
			widget.NewLabel("Stream:"),
			inStreamsSelect,
			widget.NewLabel("Override URL:"),
			overrideURL,
			widget.NewSeparator(),
			widget.NewSeparator(),
			widget.NewLabel("Start timeout (seconds):"),
			startTimeout,
			widget.NewLabel("Read timeout (seconds):"),
			readTimeout,
			widget.NewLabel("Jitter buffer size (seconds):"),
			jitterBufDuration,
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
		return players[i].StreamID < players[j].StreamID
	})

	var objs []fyne.CanvasObject
	for idx, player := range players {
		logger.Tracef(ctx, "players[%3d] == %#+v", idx, player)
		c := container.NewHBox()
		deleteButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			w := dialog.NewConfirm(
				fmt.Sprintf(
					"Delete player for stream '%s' (%s) ?",
					player.StreamID,
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
						player.StreamID,
						player.PlayerType,
					)
					defer logger.Debugf(
						ctx,
						"/remove player '%s' (%s)",
						player.StreamID,
						player.PlayerType,
					)
					err := p.StreamD.RemoveStreamPlayer(ctx, player.StreamID)
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
			p.openEditPlayerWindow(ctx, player.StreamID)
		})
		icon := theme.MediaStopIcon()
		label := "Stop"
		title := fmt.Sprintf("Stop %s on '%s' ?", player.PlayerType, player.StreamID)
		if player.Disabled {
			icon = theme.MediaPlayIcon()
			label = "Start"
			title = fmt.Sprintf("Start %s on '%s' ?", player.PlayerType, player.StreamID)
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
						player.StreamID,
						player.Disabled,
						!player.Disabled,
					)
					defer logger.Debugf(
						ctx,
						"/stop/start player %s on '%s': disabled:%v->%v",
						player.PlayerType,
						player.StreamID,
						player.Disabled,
						!player.Disabled,
					)
					err := p.StreamD.UpdateStreamPlayer(
						xcontext.DetachDone(ctx),
						player.StreamID,
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
		caption := widget.NewLabel(string(player.StreamID) + " (" + string(player.PlayerType) + ")")
		c.RemoveAll()
		c.Add(deleteButton)
		c.Add(editButton)
		c.Add(playPauseButton)
		c.Add(caption)
		if !player.Disabled {
			pos, err := p.StreamD.StreamPlayerGetPosition(ctx, player.StreamID)
			if err != nil {
				logger.Errorf(
					ctx,
					"unable to get the current position at player '%s': %v",
					player.StreamID,
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
	p.playersWidget.Objects = objs
	p.playersWidget.Refresh()

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
	streamID streamtypes.StreamID,
	dstID streamtypes.DestinationID,
) {
	cfg, err := p.StreamD.GetConfig(ctx)
	if err != nil {
		p.DisplayError(fmt.Errorf("unable to get the current config: %w", err))
		return
	}
	streamCfg, ok := cfg.StreamServer.Streams[streamID]
	if !ok {
		p.DisplayError(fmt.Errorf("unable to find a stream '%s'", streamID))
		return
	}
	fwd, ok := streamCfg.Forwardings[dstID]
	if !ok {
		p.DisplayError(fmt.Errorf("unable to find a stream forwarding %s -> %s", streamID, dstID))
		return
	}
	p.openAddOrEditRestreamWindow(
		ctx,
		"Edit the restreaming (stream forwarding)",
		streamID,
		dstID,
		fwd,
		p.updateStreamForward,
	)
}

func (p *Panel) openAddRestreamWindow(ctx context.Context) {
	p.openAddOrEditRestreamWindow(
		ctx,
		"Add a restreaming (stream forwarding)",
		"",
		"",
		sstypes.ForwardingConfig{
			Disabled: false,
			Quirks: sstypes.ForwardingQuirks{
				RestartUntilYoutubeRecognizesStream: sstypes.DefaultRestartUntilYoutubeRecognizesStreamConfig(),
			},
		},
		p.addStreamForward,
	)
}

func (p *Panel) openAddOrEditRestreamWindow(
	ctx context.Context,
	title string,
	streamID streamtypes.StreamID,
	dstID api.DestinationID,
	fwd sstypes.ForwardingConfig,
	addOrEditStreamForward func(
		ctx context.Context,
		streamID api.StreamID,
		dstID api.DestinationID,
		enabled bool,
		quirks sstypes.ForwardingQuirks,
	) error,
) {
	logger.Debugf(
		ctx,
		"openAddOrEditRestreamWindow(ctx, '%s', '%s', '%s', %#+v)",
		title,
		streamID,
		dstID,
		fwd,
	)
	defer logger.Debugf(
		ctx,
		"/openAddOrEditRestreamWindow(ctx, '%s', '%s', '%s', %#+v)",
		title,
		streamID,
		dstID,
		fwd,
	)
	w := p.app.NewWindow(AppName + ": " + title)
	resizeWindow(w, fyne.NewSize(400, 300))

	enabledCheck := widget.NewCheck("Enable", func(b bool) {})

	inStreams, err := p.StreamD.ListIncomingStreams(ctx)
	if err != nil {
		p.DisplayError(err)
		return
	}

	dsts, err := p.StreamD.ListStreamDestinations(ctx)
	if err != nil {
		p.DisplayError(err)
		return
	}

	var inStreamStrs []string
	for _, inStream := range inStreams {
		inStreamStrs = append(inStreamStrs, string(inStream.StreamID))
	}
	inStreamsSelect := widget.NewSelect(inStreamStrs, func(s string) {})
	if streamID != "" {
		inStreamsSelect.SetSelected(string(streamID))
		inStreamsSelect.Disable()
	}

	var dstStrs []string
	dstMapCaption2ID := map[string]api.DestinationID{}
	dstMapID2Caption := map[api.DestinationID]string{}
	for _, dst := range dsts {
		k := string(dst.ID) + ": " + dst.URL
		dstStrs = append(dstStrs, k)
		dstMapCaption2ID[k] = dst.ID
		dstMapID2Caption[dst.ID] = k
	}
	dstSelect := widget.NewSelect(dstStrs, func(s string) {})
	if dstID != "" {
		dstSelect.SetSelected(dstMapID2Caption[dstID])
		dstSelect.Disable()
	}

	var restartUntilYouTubeStarts *widget.Check

	quirksStartAfterYoutube := fwd.Quirks.StartAfterYoutubeRecognizedStream
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

	quirksYoutubeRestart := fwd.Quirks.RestartUntilYoutubeRecognizesStream

	if !quirksYoutubeRestart.Enabled {
		if quirksYoutubeRestart.StartTimeout == 0 {
			quirksYoutubeRestart.StartTimeout = sstypes.DefaultRestartUntilYoutubeRecognizesStreamConfig().StartTimeout
		}
		if quirksYoutubeRestart.StopStartDelay == 0 {
			quirksYoutubeRestart.StopStartDelay = sstypes.DefaultRestartUntilYoutubeRecognizesStreamConfig().StopStartDelay
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
			streamtypes.StreamID(inStreamsSelect.Selected),
			dstMapCaption2ID[dstSelect.Selected],
			enabledCheck.Checked,
			sstypes.ForwardingQuirks{
				RestartUntilYoutubeRecognizesStream: quirksYoutubeRestart,
				StartAfterYoutubeRecognizedStream:   quirksStartAfterYoutube,
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
			dstSelect,
			widget.NewSeparator(),
			widget.NewSeparator(),
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
	streamID api.StreamID,
	dstID api.DestinationID,
	enabled bool,
	quirks sstypes.ForwardingQuirks,
) error {
	logger.Debugf(ctx, "updateStreamForward")
	defer logger.Debugf(ctx, "/updateStreamForward")
	return p.StreamD.UpdateStreamForward(
		ctx,
		streamID,
		dstID,
		enabled,
		quirks,
	)
}

func (p *Panel) addStreamForward(
	ctx context.Context,
	streamID api.StreamID,
	dstID api.DestinationID,
	enabled bool,
	quirks sstypes.ForwardingQuirks,
) error {
	logger.Debugf(ctx, "addStreamForward")
	defer logger.Debugf(ctx, "/addStreamForward")
	return p.StreamD.AddStreamForward(
		ctx,
		streamID,
		dstID,
		enabled,
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
		if fwds[i].StreamID != fwds[j].StreamID {
			return fwds[i].StreamID < fwds[j].StreamID
		}
		return fwds[i].DestinationID < fwds[j].DestinationID
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
					fwd.StreamID,
					fwd.DestinationID,
				),
				"",
				func(b bool) {
					if !b {
						return
					}
					logger.Debugf(ctx, "remove restreaming (stream forwarding)")
					defer logger.Debugf(ctx, "/remove restreaming (stream forwarding)")
					err := p.StreamD.RemoveStreamForward(ctx, fwd.StreamID, fwd.DestinationID)
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
			p.openEditRestreamWindow(ctx, fwd.StreamID, fwd.DestinationID)
		})
		icon := theme.MediaPauseIcon()
		label := "Pause"
		title := fmt.Sprintf("Pause forwarding %s -> %s ?", fwd.StreamID, fwd.DestinationID)
		if !fwd.Enabled {
			icon = theme.MediaPlayIcon()
			label = "Unpause"
			title = fmt.Sprintf("Unpause forwarding %s -> %s ?", fwd.StreamID, fwd.DestinationID)
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
						fwd.StreamID,
						fwd.DestinationID,
						!fwd.Enabled,
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
		captionStr := string(fwd.StreamID) + " -> " + string(fwd.DestinationID)
		var quirksStrings []string
		if fwd.Quirks.RestartUntilYoutubeRecognizesStream.Enabled {
			quirksStrings = append(quirksStrings, "YT-restart")
		}
		if fwd.Quirks.StartAfterYoutubeRecognizedStream.Enabled {
			quirksStrings = append(quirksStrings, "after-YT")
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
				StrID api.StreamID
				DstID api.DestinationID
			}
			key := numBytesID{StrID: fwd.StreamID, DstID: fwd.DestinationID}
			now := time.Now()
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
	p.restreamsWidget.Objects = objs
	p.restreamsWidget.Refresh()

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
	observability.Go(ctx, func() {
		logger.Debugf(ctx, "streamServersUpdater: updater")
		defer logger.Debugf(ctx, "streamServersUpdater: /updater")

		updateData := func() {
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

		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			updateData()
		}
	})
	return cancelFn
}

func (p *Panel) startStreamPlayersUpdater(
	ctx context.Context,
) context.CancelFunc {
	ctx, cancelFn := context.WithCancel(ctx)
	observability.Go(ctx, func() {
		logger.Debugf(ctx, "startStreamPlayersUpdater: updater")
		defer logger.Debugf(ctx, "startStreamPlayersUpdater: /updater")

		updateData := func() {
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

		t := time.NewTicker(time.Second)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			updateData()
		}
	})
	return cancelFn
}

func (p *Panel) startStreamForwardersUpdater(
	ctx context.Context,
) context.CancelFunc {
	ctx, cancelFn := context.WithCancel(ctx)
	observability.Go(ctx, func() {
		logger.Debugf(ctx, "startStreamForwardersUpdater: updater")
		defer logger.Debugf(ctx, "startStreamForwardersUpdater: /updater")

		updateData := func() {
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

		t := time.NewTicker(time.Second)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			updateData()
		}
	})
	return cancelFn
}
