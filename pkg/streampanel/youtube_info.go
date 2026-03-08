package streampanel

import (
	"context"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/clock"
)

type youTubeInfoPage struct {
	Panel     *Panel
	container *fyne.Container

	quotaLabel       *widget.Label
	quotaBar         *widget.ProgressBar
	quotaResetLabel  *widget.Label
	googleQuotaLabel *widget.Label

	activeBroadcastsBox   *fyne.Container
	upcomingBroadcastsBox *fyne.Container
	chatListenersBox      *fyne.Container

	cancelRefresh context.CancelFunc
}

func newYouTubeInfoPage(
	ctx context.Context,
	p *Panel,
) *youTubeInfoPage {
	page := &youTubeInfoPage{
		Panel: p,
	}

	page.quotaLabel = widget.NewLabel("Quota: —")
	page.quotaBar = widget.NewProgressBar()
	page.quotaResetLabel = widget.NewLabel("")
	page.googleQuotaLabel = widget.NewLabel("")

	page.activeBroadcastsBox = container.NewVBox()
	page.upcomingBroadcastsBox = container.NewVBox()
	page.chatListenersBox = container.NewVBox()

	refreshButton := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), func() {
		page.refresh(ctx)
	})

	page.container = container.NewBorder(
		refreshButton,
		nil,
		nil,
		nil,
		container.NewVScroll(container.NewVBox(
			widget.NewRichTextFromMarkdown("## Quota"),
			page.quotaLabel,
			page.quotaBar,
			page.quotaResetLabel,
			page.googleQuotaLabel,
			widget.NewSeparator(),
			widget.NewRichTextFromMarkdown("## Active Broadcasts"),
			page.activeBroadcastsBox,
			widget.NewSeparator(),
			widget.NewRichTextFromMarkdown("## Upcoming Broadcasts"),
			page.upcomingBroadcastsBox,
			widget.NewSeparator(),
			widget.NewRichTextFromMarkdown("## Chat Listeners"),
			page.chatListenersBox,
		)),
	)

	return page
}

func (page *youTubeInfoPage) refresh(ctx context.Context) {
	logger.Debugf(ctx, "youTubeInfoPage.refresh")
	defer logger.Debugf(ctx, "/youTubeInfoPage.refresh")

	info, err := page.Panel.StreamD.GetYouTubeInfo(ctx)
	if err != nil {
		logger.Errorf(ctx, "unable to get YouTube info: %v", err)
		page.quotaLabel.SetText(fmt.Sprintf("Error: %v", err))
		return
	}

	// Quota
	page.quotaLabel.SetText(fmt.Sprintf(
		"Used: %d / %d points",
		info.QuotaUsage.UsedPoints,
		info.QuotaUsage.DailyLimit,
	))
	if info.QuotaUsage.DailyLimit > 0 {
		page.quotaBar.SetValue(
			float64(info.QuotaUsage.UsedPoints) / float64(info.QuotaUsage.DailyLimit),
		)
	}
	if !info.QuotaUsage.ResetTime.IsZero() {
		page.quotaResetLabel.SetText(fmt.Sprintf(
			"Resets: %s",
			info.QuotaUsage.ResetTime.Local().Format("2006-01-02 15:04 MST"),
		))
	}

	if info.QuotaUsage.GoogleReportedUsage != nil && info.QuotaUsage.GoogleReportedLimit != nil {
		age := ""
		if info.QuotaUsage.GoogleReportedAt != nil {
			age = fmt.Sprintf(" (as of %s)", info.QuotaUsage.GoogleReportedAt.Local().Format("Jan 2, 15:04 MST"))
		}
		page.googleQuotaLabel.SetText(fmt.Sprintf(
			"Google reported: %d / %d points%s",
			*info.QuotaUsage.GoogleReportedUsage,
			*info.QuotaUsage.GoogleReportedLimit,
			age,
		))
		page.googleQuotaLabel.Show()
	} else {
		page.googleQuotaLabel.Hide()
	}

	// Active broadcasts
	page.activeBroadcastsBox.RemoveAll()
	if len(info.ActiveBroadcasts) == 0 {
		page.activeBroadcastsBox.Add(widget.NewLabel("No active broadcasts"))
	}
	for _, bc := range info.ActiveBroadcasts {
		dur := ""
		if !bc.ActualStart.IsZero() {
			dur = fmt.Sprintf(" — started %s ago", clock.Get().Since(bc.ActualStart).Truncate(time.Second))
		}
		page.activeBroadcastsBox.Add(widget.NewLabel(
			fmt.Sprintf("• %s — %d viewers%s", bc.Title, bc.ViewerCount, dur),
		))
	}

	// Upcoming broadcasts
	page.upcomingBroadcastsBox.RemoveAll()
	if len(info.UpcomingBroadcasts) == 0 {
		page.upcomingBroadcastsBox.Add(widget.NewLabel("No upcoming broadcasts"))
	}
	for _, bc := range info.UpcomingBroadcasts {
		scheduled := ""
		if !bc.ScheduledStart.IsZero() {
			scheduled = fmt.Sprintf(" — %s", bc.ScheduledStart.Local().Format("Jan 2, 15:04"))
		}
		page.upcomingBroadcastsBox.Add(widget.NewLabel(
			fmt.Sprintf("• %s%s", bc.Title, scheduled),
		))
	}

	// Chat listeners
	page.chatListenersBox.RemoveAll()
	if len(info.ChatListeners) == 0 {
		page.chatListenersBox.Add(widget.NewLabel("No active chat listeners"))
	}
	for _, cl := range info.ChatListeners {
		status := "inactive"
		if cl.IsActive {
			status = "active"
		}
		page.chatListenersBox.Add(widget.NewLabel(
			fmt.Sprintf("• %s: %s", cl.VideoID, status),
		))
	}
}

func (page *youTubeInfoPage) StartRefresh(ctx context.Context) {
	page.StopRefresh()

	var refreshCtx context.Context
	refreshCtx, page.cancelRefresh = context.WithCancel(ctx)

	page.refresh(refreshCtx)

	observability.Go(refreshCtx, func(ctx context.Context) {
		t := clock.Get().Ticker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				page.refresh(ctx)
			}
		}
	})
}

func (page *youTubeInfoPage) StopRefresh() {
	if page.cancelRefresh != nil {
		page.cancelRefresh()
		page.cancelRefresh = nil
	}
}

func (page *youTubeInfoPage) Show() {
	page.container.Show()
}

func (page *youTubeInfoPage) Hide() {
	page.StopRefresh()
	page.container.Hide()
}

func (page *youTubeInfoPage) CanvasObject() fyne.CanvasObject {
	return page.container
}
