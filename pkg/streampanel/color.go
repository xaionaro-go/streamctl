package streampanel

import (
	"crypto/sha1"
	"encoding/binary"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
)

func colorForPlatform(platID streamcontrol.PlatformName) fyne.ThemeColorName {
	switch platID {
	case twitch.ID:
		return theme.ColorNameHyperlink
	case kick.ID:
		return theme.ColorNameSuccess
	case youtube.ID:
		return theme.ColorNameError
	default:
		return ""
	}
}

var brightColors = []fyne.ThemeColorName{
	theme.ColorNameError,
	theme.ColorNameForeground,
	theme.ColorNameHyperlink,
	theme.ColorNamePrimary,
	theme.ColorNameSelection,
	theme.ColorNameSuccess,
	theme.ColorNameWarning,
}

func colorForUsername(username string) fyne.ThemeColorName {
	h := sha1.Sum([]byte(username))
	h64 := binary.BigEndian.Uint64(h[:])

	return brightColors[h64%uint64(len(brightColors))]
}
