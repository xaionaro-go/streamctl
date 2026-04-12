package streampanel

import (
	"context"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
)

func (p *Panel) obsSetSceneItemEnabled(
	ctx context.Context,
	sceneName string,
	sceneItemID int64,
	enabled bool,
) (_err error) {
	logger.Tracef(ctx, "obsSetSceneItemEnabled: scene=%s item=%d enabled=%v", sceneName, sceneItemID, enabled)
	defer func() {
		logger.Tracef(ctx, "/obsSetSceneItemEnabled: scene=%s item=%d enabled=%v: %v", sceneName, sceneItemID, enabled, _err)
	}()

	obsServer, obsServerClose, err := p.StreamD.OBS(ctx)
	if obsServerClose != nil {
		defer obsServerClose()
	}
	if err != nil {
		return fmt.Errorf("unable to initialize a client to OBS: %w", err)
	}

	_, err = obsServer.SetSceneItemEnabled(ctx, &obs_grpc.SetSceneItemEnabledRequest{
		SceneName:        &sceneName,
		SceneItemID:      sceneItemID,
		SceneItemEnabled: enabled,
	})
	if err != nil {
		return fmt.Errorf("unable to set scene item '%d' enabled=%v in scene '%s': %w", sceneItemID, enabled, sceneName, err)
	}
	return nil
}

func (p *Panel) refreshOBSSceneItems(
	ctx context.Context,
	obsServer obs_grpc.OBSServer,
	currentSceneName string,
) {
	logger.Tracef(ctx, "refreshOBSSceneItems: scene=%s", currentSceneName)
	defer logger.Tracef(ctx, "/refreshOBSSceneItems: scene=%s", currentSceneName)

	resp, err := obsServer.GetSceneItemList(ctx, &obs_grpc.GetSceneItemListRequest{
		SceneName: &currentSceneName,
	})
	if err != nil {
		p.ReportError(fmt.Errorf("unable to get scene items for '%s': %w", currentSceneName, err))
		return
	}

	checks := make([]fyne.CanvasObject, 0, len(resp.SceneItems))
	for _, item := range resp.SceneItems {
		sceneName := currentSceneName
		sourceName := item.SourceName
		sceneItemID := item.SceneItemID
		enabled := item.SceneItemEnabled

		check := widget.NewCheck(sourceName, nil)
		check.Checked = enabled
		check.OnChanged = func(checked bool) {
			err := p.obsSetSceneItemEnabled(ctx, sceneName, sceneItemID, checked)
			if err != nil {
				p.DisplayError(err)
			}
		}
		checks = append(checks, check)
	}

	// Assign all objects at once to avoid per-item layout passes.
	p.obsSceneItemsContainer.Objects = checks
	p.obsSceneItemsContainer.Refresh()
}
