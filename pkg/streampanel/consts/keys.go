package consts

import (
	"github.com/xaionaro-go/streamctl/pkg/streamd/consts"
)

type ImageID = consts.ImageID

const (
	ImageScreenshot = ImageID("screenshot")
)

type VarKey = consts.VarKey

func VarKeyImage(imageID ImageID) VarKey {
	return consts.VarKeyImage(imageID)
}

type Page string

const (
	PageControl      = Page("Control")
	PageMoreControl  = Page("More control")
	PageChat         = Page("Chat")
	PageDashboard    = Page("Dashboard")
	PageOBS          = Page("OBS")
	PageRestream     = Page("Restream")
	PageCamera       = Page("Camera")
	PageSelectServer = Page("Select server")
	PageMonitor      = Page("Monitor (local)")
)
