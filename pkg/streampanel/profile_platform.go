package streampanel

import (
	"context"

	"fyne.io/fyne/v2"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type platformUI interface {
	Placement() platformProfilePlacement
	RenderStream(
		ctx context.Context,
		p *Panel,
		w fyne.Window,
		platID streamcontrol.PlatformID,
		sID streamcontrol.StreamIDFullyQualified,
		backendData any,
		config any,
	) (fyne.CanvasObject, func() (any, error))
	FilterMatch(
		platProfile streamcontrol.RawMessage,
		filterValue string,
	) bool
	IsReadyToStart(
		ctx context.Context,
		p *Panel,
	) bool
	AfterStartStream(
		ctx context.Context,
		p *Panel,
	) error
	IsAlwaysChecked(
		ctx context.Context,
		p *Panel,
	) bool
	ShouldStopParallel() bool
	AfterStopStream(
		ctx context.Context,
		p *Panel,
	) error
	UpdateStatus(
		ctx context.Context,
		p *Panel,
	)
	GetUserInfoItems(
		ctx context.Context,
		p *Panel,
		platID streamcontrol.PlatformID,
		accountID streamcontrol.AccountID,
		accountRaw []byte,
	) ([]fyne.CanvasObject, func() ([]byte, error), error)
	GetColor() fyne.ThemeColorName
}

type platformProfilePlacement int

const (
	platformProfilePlacementRight = platformProfilePlacement(iota)
	platformProfilePlacementLeft
)

var platformUIs = make(map[streamcontrol.PlatformID]platformUI)

func registerPlatformUI(platID streamcontrol.PlatformID, ui platformUI) {
	platformUIs[platID] = ui
}
