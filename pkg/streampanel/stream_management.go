package streampanel

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func (p *Panel) NewStreamManagementUI(
	ctx context.Context,
	platID streamcontrol.PlatformID,
	accountID streamcontrol.AccountID,
	allowlistedStreamIDs []streamcontrol.StreamID,
	onAllowlistChanged func([]streamcontrol.StreamID),
) fyne.CanvasObject {
	activeStreamsContainer := container.NewVBox()
	if accountID == "" {
		return activeStreamsContainer
	}

	activeStreamsContainer.Add(widget.NewLabel("Streams:"))

	searchEntry := widget.NewEntry()
	searchEntry.SetPlaceHolder("Search streams...")

	streamsListContainer := container.NewVBox()
	activeStreamsContainer.Add(searchEntry)
	activeStreamsContainer.Add(streamsListContainer)

	var refreshStreams func()
	refreshStreams = func() {
		streamsListContainer.Objects = nil
		loadingLabel := widget.NewLabel("Loading streams...")
		streamsListContainer.Add(loadingLabel)
		streamsListContainer.Refresh()

		observability.Go(ctx, func(ctx context.Context) {
			ctx, cancel := context.WithTimeout(p.defaultContext, 10*time.Second)
			defer cancel()

			streams, err := p.StreamD.GetStreams(ctx, streamcontrol.NewAccountIDFullyQualified(platID, accountID))
			p.app.Driver().DoFromGoroutine(func() {
				streamsListContainer.Remove(loadingLabel)
				if err != nil {
					streamsListContainer.Add(widget.NewLabel(fmt.Sprintf("Error loading streams: %v", err)))
					return
				}

				selectedStreamIDs := make(map[streamcontrol.StreamID]struct{})
				for _, id := range allowlistedStreamIDs {
					selectedStreamIDs[id] = struct{}{}
				}

				updateAllowlist := func() {
					var newList []streamcontrol.StreamID
					for id := range selectedStreamIDs {
						newList = append(newList, id)
					}
					onAllowlistChanged(newList)
				}

				filter := strings.ToLower(searchEntry.Text)
				for _, stream := range streams {
					stream := stream
					if filter != "" && !strings.Contains(strings.ToLower(stream.Name), filter) {
						continue
					}
					check := widget.NewCheck(stream.Name, func(b bool) {
						if b {
							selectedStreamIDs[stream.ID] = struct{}{}
						} else {
							delete(selectedStreamIDs, stream.ID)
						}
						updateAllowlist()
					})
					_, isSelected := selectedStreamIDs[stream.ID]
					check.SetChecked(isSelected)

					deleteButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
						p.app.Driver().DoFromGoroutine(func() {
							w := p.app.Driver().AllWindows()[0]
							widget.NewModalPopUp(container.NewVBox(
								widget.NewLabel(fmt.Sprintf("Are you sure you want to delete stream '%s'?", stream.Name)),
								container.NewHBox(
									widget.NewButton("Cancel", func() {
										p.app.Driver().AllWindows()[len(p.app.Driver().AllWindows())-1].Close()
									}),
									widget.NewButtonWithIcon("Delete", theme.DeleteIcon(), func() {
										p.app.Driver().AllWindows()[len(p.app.Driver().AllWindows())-1].Close()
										observability.Go(ctx, func(ctx context.Context) {
											err := p.StreamD.DeleteStream(ctx, streamcontrol.NewStreamIDFullyQualified(platID, accountID, stream.ID))
											p.app.Driver().DoFromGoroutine(func() {
												if err != nil {
													p.DisplayError(err)
												}
												refreshStreams()
											}, true)
										})
									}),
								),
							), w.Canvas()).Show()
						}, true)
					})
					streamsListContainer.Add(container.NewHBox(check, layout.NewSpacer(), deleteButton))
				}
				streamsListContainer.Refresh()
			}, true)
		})
	}

	searchEntry.OnChanged = func(string) {
		refreshStreams()
	}

	createButton := widget.NewButtonWithIcon("Create Stream", theme.ContentAddIcon(), func() {
		titleEntry := widget.NewEntry()
		titleEntry.SetPlaceHolder("Stream title")
		w := p.app.Driver().AllWindows()[0]
		widget.NewModalPopUp(container.NewVBox(
			widget.NewLabel("Create Stream"),
			titleEntry,
			container.NewHBox(
				widget.NewButton("Cancel", func() {
					p.app.Driver().AllWindows()[len(p.app.Driver().AllWindows())-1].Close()
				}),
				widget.NewButton("Create", func() {
					title := titleEntry.Text
					p.app.Driver().AllWindows()[len(p.app.Driver().AllWindows())-1].Close()
					observability.Go(ctx, func(ctx context.Context) {
						_, err := p.StreamD.CreateStream(ctx, streamcontrol.NewAccountIDFullyQualified(platID, accountID), title)
						p.app.Driver().DoFromGoroutine(func() {
							if err != nil {
								p.DisplayError(err)
							}
							refreshStreams()
						}, true)
					})
				}),
			),
		), w.Canvas()).Show()
	})
	activeStreamsContainer.Add(createButton)

	refreshStreams()

	return activeStreamsContainer
}
