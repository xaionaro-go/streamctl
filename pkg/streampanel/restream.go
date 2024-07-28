package streampanel

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/dustin/go-humanize"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/config"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/xfyne"
)

func (p *Panel) startRestreamPage(
	ctx context.Context,
) {
	logger.Debugf(ctx, "startRestreamPage")
	defer logger.Debugf(ctx, "/startRestreamPage")

	p.restreamPageUpdaterLocker.Lock()
	defer p.restreamPageUpdaterLocker.Unlock()

	ctx, cancelFn := context.WithCancel(ctx)
	p.restreamPageUpdaterCancel = cancelFn

	p.initRestreamPage(ctx)

	go func(ctx context.Context) {
		p.updateRestreamPage(ctx)

		t := time.NewTicker(1 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}

			p.updateRestreamPage(ctx)
		}
	}(ctx)
}

func (p *Panel) initRestreamPage(
	ctx context.Context,
) {
	logger.Debugf(ctx, "initRestreamPage")
	defer logger.Debugf(ctx, "/initRestreamPage")

	streamServers, err := p.StreamD.ListStreamServers(ctx)
	if err != nil {
		p.DisplayError(err)
	} else {
		p.displayStreamServers(ctx, streamServers)
	}

	inStreams, err := p.StreamD.ListIncomingStreams(ctx)
	if err != nil {
		p.DisplayError(err)
	} else {
		p.displayIncomingServers(ctx, inStreams)
	}

	dsts, err := p.StreamD.ListStreamDestinations(ctx)
	if err != nil {
		p.DisplayError(err)
	} else {
		p.displayStreamDestinations(ctx, dsts)
	}

	streamFwds, err := p.StreamD.ListStreamForwards(ctx)
	if err != nil {
		p.DisplayError(err)
	} else {
		p.displayStreamForwards(ctx, streamFwds)
	}

	p.displayStreamPlayers(ctx)
}

func (p *Panel) openAddStreamServerWindow(ctx context.Context) {
	w := p.app.NewWindow(appName + ": Add Stream Server")
	resizeWindow(w, fyne.NewSize(400, 300))

	currentProtocol := api.StreamServerTypeRTMP
	protocolSelectLabel := widget.NewLabel("Protocol:")
	protocolSelect := widget.NewSelect([]string{
		api.StreamServerTypeRTMP.String(),
		api.StreamServerTypeRTSP.String(),
	}, func(s string) {
		currentProtocol = api.ParseStreamServerType(s)
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

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		listenHost := listenHostEntry.Text
		p.waitForResponse(func() {
			err := p.addStreamServer(ctx, currentProtocol, listenHost, listenPort)
			if err != nil {
				p.DisplayError(err)
				return
			}
			w.Close()
			p.initRestreamPage(ctx)
		})
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
		),
	))
	w.Show()
}

func (p *Panel) addStreamServer(
	ctx context.Context,
	proto api.StreamServerType,
	listenHost string,
	listenPort uint16,
) error {
	logger.Debugf(ctx, "addStreamServer")
	defer logger.Debugf(ctx, "/addStreamServer")
	return p.StreamD.StartStreamServer(ctx, proto, fmt.Sprintf("%s:%d", listenHost, listenPort))
}

func (p *Panel) displayStreamServers(
	ctx context.Context,
	streamServers []api.StreamServer,
) {
	logger.Debugf(ctx, "displayStreamServers")
	defer logger.Debugf(ctx, "/displayStreamServers")

	p.streamServersWidget.RemoveAll()
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
					p.waitForResponse(func() {
						err := p.StreamD.StopStreamServer(ctx, srv.ListenAddr)
						if err != nil {
							p.DisplayError(err)
							return
						}
					})
					p.initRestreamPage(ctx)
				},
				p.mainWindow,
			)
			w.Show()
		})
		label := widget.NewLabel(fmt.Sprintf("%s://%s", srv.Type, srv.ListenAddr))
		c.RemoveAll()
		c.Add(button)
		c.Add(label)
		c.Add(widget.NewSeparator())

		type numBytesID struct {
			ID string
		}
		key := numBytesID{ID: srv.ListenAddr}
		p.previousNumBytesLocker.Lock()
		prevNumBytes := p.previousNumBytes[key]
		now := time.Now()
		bwText := widget.NewRichTextWithText(bwString(srv.NumBytesProducerRead, prevNumBytes[0], srv.NumBytesConsumerWrote, prevNumBytes[1], now, p.previousNumBytesTS[key]))
		p.previousNumBytes[key] = [4]uint64{srv.NumBytesProducerRead, srv.NumBytesConsumerWrote}
		p.previousNumBytesTS[key] = now
		p.previousNumBytesLocker.Unlock()

		c.Add(bwText)
		p.streamServersWidget.Add(c)
	}
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
	w := p.app.NewWindow(appName + ": Add incoming stream")
	resizeWindow(w, fyne.NewSize(400, 300))

	streamIDEntry := widget.NewEntry()
	streamIDEntry.SetPlaceHolder("stream name")

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		p.waitForResponse(func() {
			err := p.addIncomingStream(ctx, api.StreamID(streamIDEntry.Text))
			if err != nil {
				p.DisplayError(err)
				return
			}
			w.Close()
			p.initRestreamPage(ctx)
		})
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

	p.streamsWidget.RemoveAll()
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
					p.waitForResponse(func() {
						err := p.StreamD.RemoveIncomingStream(ctx, stream.StreamID)
						if err != nil {
							p.DisplayError(err)
							return
						}
					})
					p.initRestreamPage(ctx)
				},
				p.mainWindow,
			)
			w.Show()
			p.initRestreamPage(ctx)
		})
		label := widget.NewLabel(string(stream.StreamID))
		c.RemoveAll()
		c.Add(button)
		c.Add(label)
		p.streamsWidget.Add(c)
	}
	p.streamsWidget.Refresh()
}

func (p *Panel) openAddDestinationWindow(ctx context.Context) {
	w := p.app.NewWindow(appName + ": Add stream destination")
	resizeWindow(w, fyne.NewSize(400, 300))

	destinationIDEntry := widget.NewEntry()
	destinationIDEntry.SetPlaceHolder("destination ID")

	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("URL")

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		p.waitForResponse(func() {
			err := p.addStreamDestination(ctx, api.DestinationID(destinationIDEntry.Text), urlEntry.Text)
			if err != nil {
				p.DisplayError(err)
				return
			}
			w.Close()
			p.initRestreamPage(ctx)
		})
	})

	w.SetContent(container.NewBorder(
		nil,
		container.NewHBox(saveButton),
		nil,
		nil,
		container.NewVBox(
			destinationIDEntry,
			urlEntry,
		),
	))
	w.Show()
}

func (p *Panel) addStreamDestination(
	ctx context.Context,
	destinationID api.DestinationID,
	url string,
) error {
	logger.Debugf(ctx, "addStreamDestination")
	defer logger.Debugf(ctx, "/addStreamDestination")
	return p.StreamD.AddStreamDestination(ctx, destinationID, url)
}

func (p *Panel) displayStreamDestinations(
	ctx context.Context,
	dsts []api.StreamDestination,
) {
	logger.Debugf(ctx, "displayStreamDestinations")
	defer logger.Debugf(ctx, "/displayStreamDestinations")

	p.destinationsWidget.RemoveAll()
	for idx, dst := range dsts {
		logger.Tracef(ctx, "dsts[%3d] == %#+v", idx, dst)
		c := container.NewHBox()
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
					p.waitForResponse(func() {
						err := p.StreamD.RemoveStreamDestination(ctx, dst.ID)
						if err != nil {
							p.DisplayError(err)
							return
						}
					})
					p.initRestreamPage(ctx)
				},
				p.mainWindow,
			)
			w.Show()
			p.initRestreamPage(ctx)
		})
		label := widget.NewLabel(string(dst.ID) + ": " + string(dst.URL))
		c.RemoveAll()
		c.Add(deleteButton)
		c.Add(label)
		p.destinationsWidget.Add(c)
	}
}

func (p *Panel) openAddPlayerWindow(ctx context.Context) {
	p.openAddOrEditPlayerWindow(ctx, "Add player", false, types.DefaultConfig(ctx), nil, p.addStreamPlayer)
}

func (p *Panel) openEditPlayerWindow(
	ctx context.Context,
	streamID api.StreamID,
) {
	player, ok := p.Config.StreamPlayers[streamID]
	if !ok {
		p.DisplayError(fmt.Errorf("unable to find a stream player for '%s'", streamID))
		return
	}
	p.openAddOrEditPlayerWindow(ctx, "Edit player", !player.Disabled, player.StreamPlayback, &streamID, p.updateStreamPlayer)
}

func (p *Panel) openAddOrEditPlayerWindow(
	ctx context.Context,
	title string,
	isEnabled bool,
	cfg types.Config,
	forceStreamID *api.StreamID,
	addOrEditStreamPlayer func(
		ctx context.Context,
		streamID api.StreamID,
		playerType player.Backend,
		disabled bool,
		streamPlaybackConfig types.Config,
	) error,
) {
	w := p.app.NewWindow(appName + ": " + title)
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

	jitterBufDuration := widget.NewEntry()
	jitterBufDuration.SetPlaceHolder("amount of seconds")
	jitterBufDuration.SetText(fmt.Sprintf("%f", cfg.JitterBufDuration.Seconds()))
	jitterBufDuration.OnChanged = func(s string) {
		filtered := removeNonDigitsAndDots(s)
		if s != filtered {
			jitterBufDuration.SetText(filtered)
		}
	}
	jitterBufDuration.OnSubmitted = func(s string) {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as float: %w", s, err))
			return
		}

		cfg.JitterBufDuration = time.Duration(f * float64(time.Second))
	}

	maxCatchupAtLag := widget.NewEntry()
	maxCatchupAtLag.SetPlaceHolder("amount of seconds")
	maxCatchupAtLag.SetText(fmt.Sprintf("%f", cfg.MaxCatchupAtLag.Seconds()))
	maxCatchupAtLag.OnChanged = func(s string) {
		filtered := removeNonDigitsAndDots(s)
		if s != filtered {
			maxCatchupAtLag.SetText(filtered)
		}
	}
	maxCatchupAtLag.OnSubmitted = func(s string) {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as float: %w", s, err))
			return
		}

		cfg.MaxCatchupAtLag = time.Duration(f * float64(time.Second))
	}

	startTimeout := widget.NewEntry()
	startTimeout.SetPlaceHolder("amount of seconds")
	startTimeout.SetText(fmt.Sprintf("%f", cfg.StartTimeout.Seconds()))
	startTimeout.OnChanged = func(s string) {
		filtered := removeNonDigitsAndDots(s)
		if s != filtered {
			startTimeout.SetText(filtered)
		}
	}
	startTimeout.OnSubmitted = func(s string) {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as float: %w", s, err))
			return
		}

		cfg.StartTimeout = time.Duration(f * float64(time.Second))
	}

	readTimeout := widget.NewEntry()
	readTimeout.SetPlaceHolder("amount of seconds")
	readTimeout.SetText(fmt.Sprintf("%f", cfg.ReadTimeout.Seconds()))
	readTimeout.OnChanged = func(s string) {
		filtered := removeNonDigitsAndDots(s)
		if s != filtered {
			readTimeout.SetText(filtered)
		}
	}
	readTimeout.OnSubmitted = func(s string) {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			p.DisplayError(fmt.Errorf("unable to parse '%s' as float: %w", s, err))
			return
		}

		cfg.ReadTimeout = time.Duration(f * float64(time.Second))
	}

	catchupMaxSpeedFactor := widget.NewEntry()
	catchupMaxSpeedFactor.SetPlaceHolder("1.0")
	catchupMaxSpeedFactor.SetText(fmt.Sprintf("%f", cfg.CatchupMaxSpeedFactor))
	catchupMaxSpeedFactor.OnChanged = func(s string) {
		filtered := removeNonDigitsAndDots(s)
		if s != filtered {
			catchupMaxSpeedFactor.SetText(filtered)
		}
	}
	catchupMaxSpeedFactor.OnSubmitted = func(s string) {
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
		p.waitForResponse(func() {
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
			p.initRestreamPage(ctx)
		})
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
) {
	logger.Debugf(ctx, "displayStreamPlayers")
	defer logger.Debugf(ctx, "/displayStreamPlayers")

	type PlayerConfig struct {
		StreamID api.StreamID
		config.PlayerConfig
	}

	players := make([]PlayerConfig, 0, len(p.Config.StreamPlayers))
	for streamID, player := range p.Config.StreamPlayers {
		players = append(players, PlayerConfig{
			StreamID:     api.StreamID(streamID),
			PlayerConfig: player,
		})
	}

	sort.Slice(players, func(i, j int) bool {
		return players[i].StreamID < players[j].StreamID
	})

	p.playersWidget.RemoveAll()
	for idx, player := range players {
		logger.Tracef(ctx, "players[%3d] == %#+v", idx, player)
		c := container.NewHBox()
		deleteButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			w := dialog.NewConfirm(
				fmt.Sprintf("Delete player for stream '%s' (%s) ?", player.StreamID, player.Player),
				"",
				func(b bool) {
					if !b {
						return
					}
					logger.Debugf(ctx, "remove player '%s' (%s)", player.StreamID, player.Player)
					defer logger.Debugf(ctx, "/remove player '%s' (%s)", player.StreamID, player.Player)
					p.waitForResponse(func() {
						err := p.removeStreamPlayer(ctx, player.StreamID)
						if err != nil {
							p.DisplayError(err)
							return
						}
					})
					p.initRestreamPage(ctx)
				},
				p.mainWindow,
			)
			w.Show()
			p.initRestreamPage(ctx)
		})
		editButton := widget.NewButtonWithIcon("", theme.ListIcon(), func() {
			p.openEditPlayerWindow(ctx, player.StreamID)
		})
		icon := theme.MediaStopIcon()
		label := "Stop"
		title := fmt.Sprintf("Stop %s on '%s' ?", player.Player, player.StreamID)
		if player.Disabled {
			icon = theme.MediaPlayIcon()
			label = "Start"
			title = fmt.Sprintf("Start %s on '%s' ?", player.Player, player.StreamID)
		}
		playPauseButton := widget.NewButtonWithIcon(label, icon, func() {
			w := dialog.NewConfirm(
				title,
				"",
				func(b bool) {
					if !b {
						return
					}
					logger.Debugf(ctx, "stop/start player %s on '%s': disabled:%v->%v", player.Player, player.StreamID, player.Disabled, !player.Disabled)
					defer logger.Debugf(ctx, "/stop/start player %s on '%s': disabled:%v->%v", player.Player, player.StreamID, player.Disabled, !player.Disabled)
					p.waitForResponse(func() {
						err := p.updateStreamPlayer(
							ctx,
							player.StreamID,
							player.Player,
							!player.Disabled,
							player.StreamPlayback,
						)
						if err != nil {
							p.DisplayError(err)
							return
						}
					})
					p.initRestreamPage(ctx)
				},
				p.mainWindow,
			)
			w.Show()
			p.initRestreamPage(ctx)
		})
		caption := widget.NewLabel(string(player.StreamID) + " (" + string(player.Player) + ")")
		c.RemoveAll()
		c.Add(deleteButton)
		c.Add(editButton)
		c.Add(playPauseButton)
		c.Add(caption)
		if !player.Disabled {
			player := p.StreamPlayers.Get(player.StreamID)
			if player != nil {
				pos := player.Player.GetPosition()
				c.Add(widget.NewSeparator())
				c.Add(widget.NewLabel(pos.String()))
			}
		}
		p.playersWidget.Add(c)
	}
}

func (p *Panel) openAddRestreamWindow(ctx context.Context) {
	w := p.app.NewWindow(appName + ": Add restreaming (stream forwarding)")
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

	var dstStrs []string
	dstMap := map[string]api.DestinationID{}
	for _, dst := range dsts {
		k := string(dst.ID) + ": " + dst.URL
		dstStrs = append(dstStrs, k)
		dstMap[k] = dst.ID
	}
	dstSelect := widget.NewSelect(dstStrs, func(s string) {})

	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		p.waitForResponse(func() {
			err := p.addStreamForward(
				ctx,
				api.StreamID(inStreamsSelect.Selected),
				dstMap[dstSelect.Selected],
				enabledCheck.Checked,
			)
			if err != nil {
				p.DisplayError(err)
				return
			}
			w.Close()
			p.initRestreamPage(ctx)
		})
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
		),
	))
	w.Show()
}

func (p *Panel) addStreamForward(
	ctx context.Context,
	streamID api.StreamID,
	dstID api.DestinationID,
	enabled bool,
) error {
	logger.Debugf(ctx, "addStreamForward")
	defer logger.Debugf(ctx, "/addStreamForward")
	return p.StreamD.AddStreamForward(
		ctx,
		streamID,
		dstID,
		enabled,
	)
}

func (p *Panel) displayStreamForwards(
	ctx context.Context,
	fwds []api.StreamForward,
) {
	logger.Debugf(ctx, "displayStreamForwards")
	defer logger.Debugf(ctx, "/displayStreamForwards")

	sort.Slice(fwds, func(i, j int) bool {
		if fwds[i].StreamID != fwds[j].StreamID {
			return fwds[i].StreamID < fwds[j].StreamID
		}
		return fwds[i].DestinationID < fwds[j].DestinationID
	})

	p.restreamsWidget.RemoveAll()
	logger.Tracef(ctx, "len(fwds) == %d", len(fwds))
	for idx, fwd := range fwds {
		logger.Tracef(ctx, "fwds[%3d] == %#+v", idx, fwd)
		c := container.NewHBox()
		deleteButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
			w := dialog.NewConfirm(
				fmt.Sprintf("Delete restreaming (stream forwarding) %s -> %s ?", fwd.StreamID, fwd.DestinationID),
				"",
				func(b bool) {
					if !b {
						return
					}
					logger.Debugf(ctx, "remove restreaming (stream forwarding)")
					defer logger.Debugf(ctx, "/remove restreaming (stream forwarding)")
					p.waitForResponse(func() {
						err := p.StreamD.RemoveStreamForward(ctx, fwd.StreamID, fwd.DestinationID)
						if err != nil {
							p.DisplayError(err)
							return
						}
					})
					p.initRestreamPage(ctx)
				},
				p.mainWindow,
			)
			w.Show()
			p.initRestreamPage(ctx)
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
					logger.Debugf(ctx, "pause/unpause restreaming (stream forwarding): enabled:%v->%v", fwd.Enabled, !fwd.Enabled)
					defer logger.Debugf(ctx, "/pause/unpause restreaming (stream forwarding): enabled:%v->%v", !fwd.Enabled, fwd.Enabled)
					p.waitForResponse(func() {
						err := p.StreamD.UpdateStreamForward(
							ctx,
							fwd.StreamID,
							fwd.DestinationID,
							!fwd.Enabled,
						)
						if err != nil {
							p.DisplayError(err)
							return
						}
					})
					p.initRestreamPage(ctx)
				},
				p.mainWindow,
			)
			w.Show()
			p.initRestreamPage(ctx)
		})
		caption := widget.NewLabel(string(fwd.StreamID) + " -> " + string(fwd.DestinationID))
		c.RemoveAll()
		c.Add(deleteButton)
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
			p.previousNumBytesLocker.Lock()
			prevNumBytes := p.previousNumBytes[key]
			bwText := widget.NewRichTextWithText(bwString(fwd.NumBytesRead, prevNumBytes[0], fwd.NumBytesWrote, prevNumBytes[1], now, p.previousNumBytesTS[key]))
			p.previousNumBytes[key] = [4]uint64{fwd.NumBytesRead, fwd.NumBytesWrote}
			p.previousNumBytesTS[key] = now
			p.previousNumBytesLocker.Unlock()

			c.Add(bwText)
		}
		p.restreamsWidget.Add(c)
	}
}

func (p *Panel) stopRestreamPage(
	ctx context.Context,
) {
	logger.Debugf(ctx, "stopRestreamPage")
	defer logger.Debugf(ctx, "/stopRestreamPage")

	p.restreamPageUpdaterLocker.Lock()
	defer p.restreamPageUpdaterLocker.Unlock()

	if p.restreamPageUpdaterCancel == nil {
		return
	}

	p.restreamPageUpdaterCancel()
	p.restreamPageUpdaterCancel = nil
}

func (p *Panel) updateRestreamPage(
	ctx context.Context,
) {
	logger.Tracef(ctx, "updateRestreamPage")
	defer logger.Tracef(ctx, "/updateRestreamPage")

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		p.initRestreamPage(ctx)
		// whatever
	}()
	wg.Wait()
}

func removeNonDigitsAndDots(input string) string {
	var result []rune
	for _, r := range input {
		if unicode.IsDigit(r) && r != '.' {
			result = append(result, r)
		}
	}
	return string(result)
}
