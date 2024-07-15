package streampanel

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
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

	p.initRestartPage(ctx)

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

func (p *Panel) initRestartPage(
	ctx context.Context,
) {
	logger.Debugf(ctx, "initRestartPage")
	defer logger.Debugf(ctx, "/initRestartPage")

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
		err := p.addStreamServer(ctx, currentProtocol, listenHost, listenPort)
		if err != nil {
			p.DisplayError(err)
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

	for _, srv := range streamServers {
		_ = srv
	}

}

func (p *Panel) openAddStreamWindow() {}

func (p *Panel) displayIncomingServers(
	ctx context.Context,
	inStreams []api.IncomingStream,
) {
	logger.Debugf(ctx, "displayIncomingServers")
	defer logger.Debugf(ctx, "/displayIncomingServers")

}

func (p *Panel) openAddDestinationWindow() {}

func (p *Panel) displayStreamDestinations(
	ctx context.Context,
	dsts []api.StreamDestination,
) {
	logger.Debugf(ctx, "displayStreamDestinations")
	defer logger.Debugf(ctx, "/displayStreamDestinations")

}

func (p *Panel) openAddRestreamWindow() {}

func (p *Panel) displayStreamForwards(
	ctx context.Context,
	dsts []api.StreamForward,
) {
	logger.Debugf(ctx, "displayStreamForwards")
	defer logger.Debugf(ctx, "/displayStreamForwards")

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

		// whatever
	}()
	wg.Wait()
}
