package streampanel

import (
	"context"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

type raidPage struct {
	Container *fyne.Container
	Panel     *Panel
}

func newRaidPage(
	ctx context.Context,
	p *Panel,
) *raidPage {
	cfg, err := p.GetStreamDConfig(ctx)
	if err != nil {
		p.DisplayError(err)
		cfg = &config.Config{}
	}
	var (
		youtubeUsers []string
		twitchUsers  []string
		kickUsers    []string
	)
	for _, userID := range cfg.Raid.AutoRaidOnStreamEnd {
		switch userID.Platform {
		case youtube.ID:
			youtubeUsers = append(youtubeUsers, string(userID.User))
		case twitch.ID:
			twitchUsers = append(twitchUsers, string(userID.User))
		case kick.ID:
			kickUsers = append(kickUsers, string(userID.User))
		}
	}

	youtubeUsersEntry := widget.NewEntry()
	youtubeUsersEntry.SetText(strings.Join(youtubeUsers, ", "))
	twitchUsersEntry := widget.NewEntry()
	twitchUsersEntry.SetText(strings.Join(twitchUsers, ", "))
	kickUsersEntry := widget.NewEntry()
	kickUsersEntry.SetText(strings.Join(kickUsers, ", "))
	result := &raidPage{
		Panel: p,
	}
	getUserIDs := func(platID streamcontrol.PlatformName, text string) []config.ChatUserID {
		var result []config.ChatUserID
		for word := range strings.SplitSeq(text, ",") {
			word = strings.Trim(word, " \n\t\r")
			if len(word) == 0 {
				continue
			}
			result = append(result, config.ChatUserID{
				Platform: platID,
				User:     streamcontrol.ChatUserID(word),
			})
		}
		return result
	}
	saveButton := widget.NewButtonWithIcon("Save", theme.DocumentSaveIcon(), func() {
		logger.Debugf(ctx, "raidPage: save")
		defer logger.Debugf(ctx, "raidPage: /save")
		cfg, err := result.Panel.GetStreamDConfig(ctx)
		if err != nil {
			result.Panel.DisplayError(err)
			return
		}
		cfg.Raid.AutoRaidOnStreamEnd = cfg.Raid.AutoRaidOnStreamEnd[:0]
		cfg.Raid.AutoRaidOnStreamEnd = append(cfg.Raid.AutoRaidOnStreamEnd, getUserIDs(youtube.ID, youtubeUsersEntry.Text)...)
		cfg.Raid.AutoRaidOnStreamEnd = append(cfg.Raid.AutoRaidOnStreamEnd, getUserIDs(twitch.ID, twitchUsersEntry.Text)...)
		cfg.Raid.AutoRaidOnStreamEnd = append(cfg.Raid.AutoRaidOnStreamEnd, getUserIDs(kick.ID, kickUsersEntry.Text)...)
		logger.Debugf(ctx, "raidPage: save: %v", cfg.Raid.AutoRaidOnStreamEnd)
		err = result.Panel.SetStreamDConfig(ctx, cfg)
		if err != nil {
			result.Panel.DisplayError(err)
			return
		}
		err = result.Panel.StreamD.SaveConfig(ctx)
		if err != nil {
			result.Panel.DisplayError(err)
			return
		}
	})
	result.Container = container.NewBorder(
		nil,
		saveButton,
		nil,
		nil,
		container.NewVBox(
			widget.NewLabel("The comma-separated list of YouTube users to raid:"),
			youtubeUsersEntry,
			widget.NewLabel("The comma-separated list of Twitch users to raid:"),
			twitchUsersEntry,
			widget.NewLabel("The comma-separated list of Kick users to raid:"),
			kickUsersEntry,
		),
	)
	return result
}

func (p *raidPage) Show() {
	p.Container.Show()
}

func (p *raidPage) Hide() {
	p.Container.Hide()
}

func (p *raidPage) CanvasObject() fyne.CanvasObject {
	return p.Container
}
