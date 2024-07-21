package streampanel

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/dustin/go-humanize"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
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
					logger.Debugf(ctx, "pause/unpause restreaming (stream forwarding): disabled:%v->%v", fwd.Enabled, !fwd.Enabled)
					defer logger.Debugf(ctx, "/pause/unpause restreaming (stream forwarding): disabled:%v->%v", !fwd.Enabled, fwd.Enabled)
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
