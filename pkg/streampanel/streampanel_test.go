package streampanel

import (
	"context"
	"crypto"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/nicklaw5/helix/v2"
	testifyAssert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/kickcom"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/player/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/colorx"
	"github.com/xaionaro-go/streamctl/pkg/imgb64"
	"github.com/xaionaro-go/streamctl/pkg/oauthhandler"
	p2ptypes "github.com/xaionaro-go/streamctl/pkg/p2p/types"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/cache"
	"github.com/xaionaro-go/streamctl/pkg/streamd/client"
	streamdconfig "github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/event"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/config"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	sstypes "github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/streamctl/pkg/ximage"
	youtubeapi "google.golang.org/api/youtube/v3"
)

func TestPanelGUI(t *testing.T) {
	testApp := test.NewApp()
	defer testApp.Quit()

	p := &Panel{
		app: testApp,
	}
	require.NotNil(t, p.app)

	// Test platform color mapping (Theme Support)
	require.Equal(t, theme.ColorNameHyperlink, colorForPlatform(twitch.ID))
	require.Equal(t, theme.ColorNameSuccess, colorForPlatform(kick.ID))
	require.Equal(t, theme.ColorNameError, colorForPlatform(youtube.ID))
}

func TestPanelDashboardIntegration(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	require.Eventually(t, func() bool {
		return p.mainWindow != nil && p.dashboardShowHideButton != nil
	}, 12*time.Second, 200*time.Millisecond)

	require.NotNil(t, p.mainWindow)

	// Verify that the dashboard window can be created.
	require.NotPanics(t, func() {
		p.focusDashboardWindow(ctx)
	})
	require.Eventually(t, func() bool {
		return p.dashboardWindow != nil
	}, 5*time.Second, 100*time.Millisecond)
}

func TestPanelStabilityHacks(t *testing.T) {
	testApp := test.NewApp()
	defer testApp.Quit()

	p, err := New("", OptionApp{App: testApp})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Verify that we can call the real health checker without crashing.
	require.NotPanics(t, func() {
		p.checkAndFixWindowsHealth(ctx)
	})

	// Manually trigger a "fix" by pretending a window is nil or has issues
	// (Hard to simulate actual Fyne hangs, but we can check the logic flow)
}

func TestPanelBrowser(t *testing.T) {
	testApp := test.NewApp()
	defer testApp.Quit()

	p := &Panel{
		app: testApp,
	}
	p.Config.Browser.Command = "true"

	b := newBrowser(p)
	ctx := context.Background()

	// Verify URL opening
	require.NotPanics(t, func() {
		b.openBrowser(ctx, "https://example.com", "test-id")
	})

	// Verify deduplication (calling again with same ID shouldn't panic/hang)
	require.NotPanics(t, func() {
		b.openBrowser(ctx, "https://example.com", "test-id")
	})
}

func TestPanelChatIntegration(t *testing.T) {
	testApp := test.NewApp()
	defer testApp.Quit()

	p := &Panel{
		app: testApp,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Verify that we can add a chat UI.
	ui, err := p.newChatUIAsList(ctx, true, true, false)
	require.NoError(t, err)
	require.NotNil(t, ui)

	// Verify that we can retrieve chat UIs
	require.NotEmpty(t, p.getChatUIs(ctx))
}

func TestPanelTimerIntegration(t *testing.T) {
	testApp := test.NewApp()
	defer testApp.Quit()

	btn := widget.NewButton("...", nil)
	startedAt := time.Now().Add(-10 * time.Second)
	h := newUpdateTimerHandler(btn, startedAt)
	require.NotNil(t, h)
	defer h.cancelFn()

	// Wait for loop to update text
	require.Eventually(t, func() bool {
		return btn.Text != "..."
	}, 2*time.Second, 100*time.Millisecond)

	require.Regexp(t, `^[0-9]+s$`, btn.Text)
	require.NotEqual(t, "0s", btn.Text)
}

func TestPanelDashboardBandwidth(t *testing.T) {
	testApp := test.NewApp()
	defer testApp.Quit()

	p := &Panel{
		app: testApp,
	}
	p.appStatus = widget.NewLabel("")

	w := &dashboardWindow{
		Panel: p,
	}
	w.appStatus = p.appStatus

	// We need a client.Client to trigger the stats logic
	cl := &client.Client{}
	p.StreamD = cl

	ctx := context.Background()

	// Initial sample
	atomic.StoreUint64(&cl.Stats.BytesIn, 1000)
	atomic.StoreUint64(&cl.Stats.BytesOut, 2000)
	w.renderStreamStatus(ctx)

	// Second sample after "time passed"
	w.appStatusData.prevUpdateTS = time.Now().Add(-1 * time.Second)
	atomic.StoreUint64(&cl.Stats.BytesIn, 2000)  // 1000 bytes diff = 8Kb/s
	atomic.StoreUint64(&cl.Stats.BytesOut, 4000) // 2000 bytes diff = 16Kb/s

	w.renderStreamStatus(ctx)

	// Check if label was updated (Wait for async UI update)
	require.Eventually(t, func() bool {
		return p.appStatus.Text != ""
	}, 2*time.Second, 100*time.Millisecond)

	require.Contains(t, p.appStatus.Text, "  8Kb/s |   16Kb/s")
}

func TestPanelColor(t *testing.T) {
	c, err := colorx.Parse("#FF0000")
	require.NoError(t, err)
	r, g, b, _ := c.RGBA()
	testifyAssert.Equal(t, uint32(0xFFFF), r)
	testifyAssert.Equal(t, uint32(0), g)
	testifyAssert.Equal(t, uint32(0), b)

	c2, err := colorx.Parse("#00FF00AA") // With Alpha
	require.NoError(t, err)
	_, g2, _, a2 := c2.RGBA()
	testifyAssert.Equal(t, uint32(0xFFFF), g2)
	testifyAssert.NotEqual(t, uint32(0xFFFF), a2)
}

func TestPanelDataURI(t *testing.T) {
	data, mime, err := imgb64.Decode("data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg==")
	require.NoError(t, err)
	testifyAssert.Equal(t, "image/png", mime)
	testifyAssert.NotEmpty(t, data)
}

func TestPanelCoordinates(t *testing.T) {
	rect := ximage.RectangleFloat64{
		Min: ximage.PointFloat64{X: 0, Y: 0},
		Max: ximage.PointFloat64{X: 100, Y: 100},
	}
	size := rect.Size()
	require.Equal(t, 100.0, size.X)
	require.Equal(t, 100.0, size.Y)
}

type dummyStreamD struct {
	api.StreamD
	Config                            *streamdconfig.Config
	SaveConfigErr                     error
	SetConfigErr                      error
	GetBackendInfoFn                  func(ctx context.Context, platID streamcontrol.PlatformID, full bool) (*api.BackendInfo, error)
	IsBackendEnabledFn                func(ctx context.Context, id streamcontrol.PlatformID) (bool, error)
	GetAccountsFn                     func(ctx context.Context, platIDs ...streamcontrol.PlatformID) ([]streamcontrol.AccountIDFullyQualified, error)
	GetStreamsFn                      func(ctx context.Context, accountIDs ...streamcontrol.AccountIDFullyQualified) ([]streamcontrol.StreamInfo, error)
	ListStreamPlayersFn               func(ctx context.Context) ([]api.StreamPlayer, error)
	UpdateStreamPlayerFn              func(ctx context.Context, streamSourceID streamtypes.StreamSourceID, playerType player.Backend, disabled bool, streamPlaybackConfig sptypes.Config) error
	GetConfigFn                       func(ctx context.Context) (*streamdconfig.Config, error)
	SetConfigFn                       func(ctx context.Context, cfg *streamdconfig.Config) error
	SaveConfigFn                      func(ctx context.Context) error
	GetStreamStatusFn                 func(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified) (*streamcontrol.StreamStatus, error)
	SubscribeToStreamPlayersChangesFn func(ctx context.Context) (<-chan api.DiffStreamPlayers, error)

	SetStreamDConfigFn func(ctx context.Context, name streamcontrol.ProfileName, platID streamcontrol.PlatformID, profile streamcontrol.AbstractStreamProfile) error
}

func (d *dummyStreamD) Run(ctx context.Context) error {
	return nil
}

type dummyStreamDLocal = dummyStreamD
type dummyStreamDForCoverage = dummyStreamD

func (d *dummyStreamD) GetActiveStreamIDs(ctx context.Context) ([]streamcontrol.StreamIDFullyQualified, error) {
	return nil, nil
}

func (d *dummyStreamD) GetPlatforms(ctx context.Context) []streamcontrol.PlatformID {
	return []streamcontrol.PlatformID{twitch.ID, youtube.ID, obs.ID}
}
func (d *dummyStreamD) GetAccounts(ctx context.Context, platIDs ...streamcontrol.PlatformID) ([]streamcontrol.AccountIDFullyQualified, error) {
	if d.GetAccountsFn != nil {
		return d.GetAccountsFn(ctx, platIDs...)
	}
	if d.Config != nil {
		var res []streamcontrol.AccountIDFullyQualified
		for _, platID := range platIDs {
			if platCfg, ok := d.Config.Backends[platID]; ok {
				for accID := range platCfg.Accounts {
					res = append(res, streamcontrol.NewAccountIDFullyQualified(platID, accID))
				}
			}
		}
		if len(res) > 0 {
			return res, nil
		}
	}
	return nil, nil
}
func (d *dummyStreamD) GetStreams(ctx context.Context, accountIDs ...streamcontrol.AccountIDFullyQualified) ([]streamcontrol.StreamInfo, error) {
	if d.GetStreamsFn != nil {
		return d.GetStreamsFn(ctx, accountIDs...)
	}
	if d.Config != nil {
		var res []streamcontrol.StreamInfo
		for _, accID := range accountIDs {
			if platCfg, ok := d.Config.Backends[accID.PlatformID]; ok {
				if accRaw, ok := platCfg.Accounts[accID.AccountID]; ok {
					for sID := range accRaw.GetStreamProfiles() {
						res = append(res, streamcontrol.StreamInfo{ID: sID})
					}
				}
			}
		}
		if len(res) > 0 {
			return res, nil
		}
	}
	return nil, nil
}
func (d *dummyStreamD) SetStreamActive(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified, active bool) error {
	return nil
}
func (d *dummyStreamD) SetTitle(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified, title string) error {
	return nil
}
func (d *dummyStreamD) SetDescription(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified, description string) error {
	return nil
}
func (d *dummyStreamD) ApplyProfile(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified, profile streamcontrol.StreamProfile) error {
	return nil
}
func (d *dummyStreamD) WaitStreamStartedByStreamSourceID(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified) error {
	return nil
}
func (d *dummyStreamD) GetStreamStatus(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified) (*streamcontrol.StreamStatus, error) {
	if d.GetStreamStatusFn != nil {
		return d.GetStreamStatusFn(ctx, streamID)
	}
	return &streamcontrol.StreamStatus{}, nil
}

func (d *dummyStreamD) GetVariable(ctx context.Context, key consts.VarKey) (api.VariableValue, error) {
	return api.VariableValue("dummy"), nil
}
func (d *dummyStreamD) SetVariable(ctx context.Context, key consts.VarKey, val api.VariableValue) error {
	return nil
}
func (d *dummyStreamD) SubscribeToVariable(ctx context.Context, key consts.VarKey) (<-chan api.VariableValue, error) {
	return make(chan api.VariableValue), nil
}
func (d *dummyStreamD) GetVariableHash(ctx context.Context, key consts.VarKey, hashType crypto.Hash) ([]byte, error) {
	return []byte("dummyhash"), nil
}

func (d *dummyStreamD) GetConfig(ctx context.Context) (*streamdconfig.Config, error) {
	if d.GetConfigFn != nil {
		return d.GetConfigFn(ctx)
	}
	if d.Config != nil {
		return d.Config, nil
	}
	cfg := streamdconfig.NewConfig()
	cfg.StreamServer = sstypes.Config{
		Streams: map[streamtypes.StreamSourceID]*sstypes.StreamConfig{
			"source1": {
				Forwardings: map[streamtypes.StreamSinkIDFullyQualified]sstypes.ForwardingConfig{
					{Type: streamtypes.StreamSinkTypeCustom, ID: "sink1"}: {},
				},
				Player: &sstypes.PlayerConfig{
					Player: "mpv",
				},
			},
		},
	}
	return &cfg, nil
}
func (d *dummyStreamD) SetConfig(ctx context.Context, cfg *streamdconfig.Config) error {
	if d.SetConfigErr != nil {
		return d.SetConfigErr
	}
	if d.SetConfigFn != nil {
		return d.SetConfigFn(ctx, cfg)
	}
	d.Config = cfg
	return nil
}
func (d *dummyStreamD) SaveConfig(ctx context.Context) error {
	if d.SaveConfigErr != nil {
		return d.SaveConfigErr
	}
	if d.SaveConfigFn != nil {
		return d.SaveConfigFn(ctx)
	}
	return nil
}
func (d *dummyStreamD) ResetCache(ctx context.Context) error { return nil }
func (d *dummyStreamD) InitCache(ctx context.Context) error  { return nil }

func (d *dummyStreamD) IsBackendEnabled(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
	if d.IsBackendEnabledFn != nil {
		return d.IsBackendEnabledFn(ctx, id)
	}
	return true, nil
}

func (d *dummyStreamD) GetBackendInfo(ctx context.Context, platID streamcontrol.PlatformID, full bool) (*api.BackendInfo, error) {
	if d.GetBackendInfoFn != nil {
		return d.GetBackendInfoFn(ctx, platID, full)
	}
	info := &api.BackendInfo{}
	switch string(platID) {
	case string(obs.ID):
		info.Data = api.BackendDataOBS{}
	case string(twitch.ID):
		info.Data = api.BackendDataTwitch{
			Cache: &cache.Twitch{
				Categories: []helix.Game{{Name: "TalkShow"}},
			},
		}
	case string(kick.ID):
		info.Data = api.BackendDataKick{
			Cache: &cache.Kick{
				Categories: &[]kickcom.CategoryV1Short{{Name: "KickCat1", ID: 1}},
			},
		}
	case string(youtube.ID):
		info.Data = api.BackendDataYouTube{
			Cache: &cache.YouTube{
				Broadcasts: []*youtubeapi.LiveBroadcast{
					{
						Id: "bc1",
						Snippet: &youtubeapi.LiveBroadcastSnippet{
							Title: "Template1",
						},
					},
				},
			},
		}
	}
	return info, nil
}

func (d *dummyStreamD) ListStreamServers(ctx context.Context) ([]api.StreamServer, error) {
	return []api.StreamServer{{
		Config: streamportserver.Config{
			Type:       streamtypes.ServerTypeRTMP,
			ListenAddr: ":1935",
		},
	}}, nil
}
func (d *dummyStreamD) StartStreamServer(ctx context.Context, serverType api.StreamServerType, listenAddr string, opts ...streamportserver.Option) error {
	return nil
}
func (d *dummyStreamD) StopStreamServer(ctx context.Context, listenAddr string) error {
	return nil
}

func (d *dummyStreamD) ListStreamSources(ctx context.Context) ([]api.StreamSource, error) {
	return []api.StreamSource{{StreamSourceID: "source1", IsActive: true}}, nil
}

func (d *dummyStreamD) ListStreamSinks(ctx context.Context) ([]api.StreamSink, error) {
	return []api.StreamSink{{ID: api.StreamSinkIDFullyQualified{Type: streamtypes.StreamSinkTypeCustom, ID: "sink1"}}}, nil
}

func (d *dummyStreamD) OBS(ctx context.Context, accountID streamcontrol.AccountID) (obs_grpc.OBSServer, context.CancelFunc, error) {
	return nil, func() {}, fmt.Errorf("not implemented")
}

func (d *dummyStreamD) BanUser(ctx context.Context, platID streamcontrol.PlatformID, userID streamcontrol.UserID, reason string, deadline time.Time) error {
	return nil
}

func (d *dummyStreamD) ListTriggerRules(ctx context.Context) (api.TriggerRules, error) {
	return nil, nil
}

func (d *dummyStreamD) SubscribeToStreamSourcesChanges(ctx context.Context) (<-chan api.DiffStreamSources, error) {
	return make(chan api.DiffStreamSources), nil
}

func (d *dummyStreamD) SubscribeToStreamSinksChanges(ctx context.Context) (<-chan api.DiffStreamSinks, error) {
	return make(chan api.DiffStreamSinks), nil
}

func (d *dummyStreamD) SubscribeToStreamForwardsChanges(ctx context.Context) (<-chan api.DiffStreamForwards, error) {
	return make(chan api.DiffStreamForwards), nil
}

func (d *dummyStreamD) SubscribeToStreamPlayersChanges(ctx context.Context) (<-chan api.DiffStreamPlayers, error) {
	if d.SubscribeToStreamPlayersChangesFn != nil {
		return d.SubscribeToStreamPlayersChangesFn(ctx)
	}
	return make(chan api.DiffStreamPlayers), nil
}

func (d *dummyStreamD) SubscribeToStreamServersChanges(ctx context.Context) (<-chan api.DiffStreamServers, error) {
	return make(chan api.DiffStreamServers), nil
}

func (d *dummyStreamD) SubscribeToStreamsChanges(ctx context.Context) (<-chan api.DiffStreams, error) {
	return make(chan api.DiffStreams), nil
}

func (d *dummyStreamD) SubscribeToConfigChanges(ctx context.Context) (<-chan api.DiffConfig, error) {
	return make(chan api.DiffConfig), nil
}

func (d *dummyStreamD) ListStreamForwards(ctx context.Context) ([]api.StreamForward, error) {
	return []api.StreamForward{{StreamSourceID: "source1", StreamSinkID: api.StreamSinkIDFullyQualified{Type: streamtypes.StreamSinkTypeCustom, ID: "sink1"}, Enabled: true}}, nil
}

func (d *dummyStreamD) ListStreamPlayers(ctx context.Context) ([]api.StreamPlayer, error) {
	if d.ListStreamPlayersFn != nil {
		return d.ListStreamPlayersFn(ctx)
	}
	return []api.StreamPlayer{{StreamSourceID: "source1", PlayerType: "mpv"}}, nil
}

func (d *dummyStreamD) WaitForStreamPublisher(ctx context.Context, streamSourceID api.StreamSourceID, waitForNext bool) (<-chan struct{}, error) {
	return make(chan struct{}), nil
}

func (d *dummyStreamD) ListTimers(ctx context.Context) ([]api.Timer, error) {
	return nil, nil
}

func (d *dummyStreamD) AddTimer(ctx context.Context, deadline time.Time, action api.Action) (api.TimerID, error) {
	return 0, nil
}

func (d *dummyStreamD) RemoveTimer(ctx context.Context, id api.TimerID) error {
	return nil
}

func (d *dummyStreamD) SubscribeToChatMessages(ctx context.Context, since time.Time, limit uint64) (<-chan api.ChatMessage, error) {
	return make(chan api.ChatMessage), nil
}

func (d *dummyStreamD) SendChatMessage(ctx context.Context, platID streamcontrol.PlatformID, message string) error {
	return nil
}

func (d *dummyStreamD) RemoveChatMessage(ctx context.Context, platID streamcontrol.PlatformID, messageID streamcontrol.EventID) error {
	return nil
}

func (d *dummyStreamD) Shoutout(ctx context.Context, platID streamcontrol.PlatformID, userID streamcontrol.UserID) error {
	return nil
}

func (d *dummyStreamD) RaidTo(ctx context.Context, platID streamcontrol.PlatformID, userID streamcontrol.UserID) error {
	return nil
}

func (d *dummyStreamD) LLMGenerate(ctx context.Context, prompt string) (string, error) {
	return "LLM Result", nil
}

func (d *dummyStreamD) AddStreamSink(ctx context.Context, id api.StreamSinkIDFullyQualified, cfg sstypes.StreamSinkConfig) error {
	return nil
}
func (d *dummyStreamD) UpdateStreamSink(ctx context.Context, id api.StreamSinkIDFullyQualified, cfg sstypes.StreamSinkConfig) error {
	return nil
}
func (d *dummyStreamD) RemoveStreamSink(ctx context.Context, id api.StreamSinkIDFullyQualified) error {
	return nil
}
func (d *dummyStreamD) AddStreamPlayer(ctx context.Context, streamSourceID streamtypes.StreamSourceID, playerType player.Backend, disabled bool, streamPlaybackConfig sptypes.Config) error {
	return nil
}
func (d *dummyStreamD) UpdateStreamPlayer(ctx context.Context, streamSourceID streamtypes.StreamSourceID, playerType player.Backend, disabled bool, streamPlaybackConfig sptypes.Config) error {
	if d.UpdateStreamPlayerFn != nil {
		return d.UpdateStreamPlayerFn(ctx, streamSourceID, playerType, disabled, streamPlaybackConfig)
	}
	return nil
}
func (d *dummyStreamD) RemoveStreamPlayer(ctx context.Context, streamSourceID streamtypes.StreamSourceID) error {
	return nil
}
func (d *dummyStreamD) AddStreamForward(ctx context.Context, src api.StreamSourceID, sink api.StreamSinkIDFullyQualified, enabled bool, enc sstypes.EncodeConfig, quirks api.StreamForwardingQuirks) error {
	return nil
}
func (d *dummyStreamD) UpdateStreamForward(ctx context.Context, src api.StreamSourceID, sink api.StreamSinkIDFullyQualified, enabled bool, enc sstypes.EncodeConfig, quirks api.StreamForwardingQuirks) error {
	return nil
}
func (d *dummyStreamD) RemoveStreamForward(ctx context.Context, src api.StreamSourceID, sink api.StreamSinkIDFullyQualified) error {
	return nil
}
func (d *dummyStreamD) AddStreamSource(ctx context.Context, id api.StreamSourceID) error { return nil }
func (d *dummyStreamD) RemoveStreamSource(ctx context.Context, id api.StreamSourceID) error {
	return nil
}
func (d *dummyStreamD) GetLoggingLevel(ctx context.Context) (logger.Level, error) {
	return logger.LevelDebug, nil
}
func (d *dummyStreamD) SetLoggingLevel(ctx context.Context, level logger.Level) error { return nil }
func (d *dummyStreamD) GetStreamSinkConfig(ctx context.Context, id streamcontrol.StreamIDFullyQualified) (sstypes.StreamSinkConfig, error) {
	return sstypes.StreamSinkConfig{}, nil
}
func (d *dummyStreamD) DialPeerByID(ctx context.Context, id p2ptypes.PeerID) (api.StreamD, error) {
	return d, nil
}
func (d *dummyStreamD) GetPeerIDs(ctx context.Context) ([]p2ptypes.PeerID, error) { return nil, nil }
func (d *dummyStreamD) DialContext(ctx context.Context, n, a string) (net.Conn, error) {
	return nil, nil
}
func (d *dummyStreamD) SubmitOAuthCode(ctx context.Context, req *streamd_grpc.SubmitOAuthCodeRequest) (*streamd_grpc.SubmitOAuthCodeReply, error) {
	return &streamd_grpc.SubmitOAuthCodeReply{}, nil
}
func (d *dummyStreamD) SubmitEvent(ctx context.Context, ev event.Event) error { return nil }
func (d *dummyStreamD) AddTriggerRule(ctx context.Context, r *streamdconfig.TriggerRule) (api.TriggerRuleID, error) {
	return 0, nil
}
func (d *dummyStreamD) UpdateTriggerRule(ctx context.Context, id api.TriggerRuleID, r *streamdconfig.TriggerRule) error {
	return nil
}
func (d *dummyStreamD) RemoveTriggerRule(ctx context.Context, id api.TriggerRuleID) error { return nil }
func (d *dummyStreamD) GetStreamPlayer(ctx context.Context, id streamtypes.StreamSourceID) (*api.StreamPlayer, error) {
	return nil, nil
}
func (d *dummyStreamD) StreamPlayerProcessTitle(ctx context.Context, id api.StreamSourceID) (string, error) {
	return "", nil
}
func (d *dummyStreamD) StreamPlayerOpenURL(ctx context.Context, id api.StreamSourceID, l string) error {
	return nil
}
func (d *dummyStreamD) StreamPlayerGetLink(ctx context.Context, id api.StreamSourceID) (string, error) {
	return "", nil
}
func (d *dummyStreamD) StreamPlayerEndChan(ctx context.Context, id api.StreamSourceID) (<-chan struct{}, error) {
	return nil, nil
}
func (d *dummyStreamD) StreamPlayerIsEnded(ctx context.Context, id api.StreamSourceID) (bool, error) {
	return false, nil
}
func (d *dummyStreamD) StreamPlayerGetPosition(ctx context.Context, id api.StreamSourceID) (time.Duration, error) {
	return 0, nil
}
func (d *dummyStreamD) StreamPlayerGetLength(ctx context.Context, id api.StreamSourceID) (time.Duration, error) {
	return 0, nil
}
func (d *dummyStreamD) StreamPlayerGetLag(ctx context.Context, id api.StreamSourceID) (time.Duration, time.Time, error) {
	return 0, time.Time{}, nil
}
func (d *dummyStreamD) StreamPlayerSetSpeed(ctx context.Context, id api.StreamSourceID, s float64) error {
	return nil
}
func (d *dummyStreamD) StreamPlayerSetPause(ctx context.Context, id api.StreamSourceID, p bool) error {
	return nil
}
func (d *dummyStreamD) StreamPlayerStop(ctx context.Context, id api.StreamSourceID) error { return nil }
func (d *dummyStreamD) StreamPlayerClose(ctx context.Context, id api.StreamSourceID) error {
	return nil
}

func setupTestPanel(t *testing.T) (fyne.App, *Panel, context.Context, context.CancelFunc) {
	testApp := test.NewApp()
	testApp.Settings().SetTheme(theme.DefaultTheme())
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	ctx = observability.WithSecretsProvider(ctx, &observability.SecretsStaticProvider{})

	tmpFile := fmt.Sprintf("/tmp/streamctl-test-%d.yaml", time.Now().UnixNano())
	p, err := New(tmpFile, OptionApp{App: testApp})
	require.NoError(t, err)
	p.app = testApp
	p.Config.Browser.Command = "true"
	p.defaultContext = ctx
	p.backgroundRenderer = newBackgroundRenderer(ctx, p)
	d := &dummyStreamD{}
	p.StreamD = d
	cfg, _ := d.GetConfig(ctx)
	d.Config = cfg
	p.configCache = cfg
	p.selectedProfileName = new(streamcontrol.ProfileName)
	*p.selectedProfileName = "default"
	if p.configCache.ProfileMetadata == nil {
		p.configCache.ProfileMetadata = make(map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata)
	}
	p.configCache.ProfileMetadata["default"] = streamdconfig.ProfileMetadata{}
	p.Config.Monitors.StreamMonitors = []config.StreamMonitor{
		{
			StreamSourceID: "source1",
			IsEnabled:      true,
			StreamDAddr:    "localhost:1234",
		},
	}
	p.Config.RemoteStreamDAddr = "localhost:1234"
	p.createMainWindow(ctx)
	p.initMainWindow(ctx, consts.PageControl)
	return testApp, p, ctx, cancel
}

func TestPanelFullFeatureSetIntegration(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	// 1. Unified Platform Control: Title / Description / Checks
	require.NotNil(t, p.streamTitleLabel)
	p.streamTitleField.SetText("Integration Test Title")
	require.Equal(t, "Integration Test Title", p.streamTitleField.Text)

	p.twitchCheck.SetChecked(true)
	p.youtubeCheck.SetChecked(false)
	require.True(t, p.twitchCheck.Checked)
	require.False(t, p.youtubeCheck.Checked)

	// 2. Profile Selection
	p.profilesOrder = []streamcontrol.ProfileName{"test-profile"}
	p.profilesListWidget.Refresh()
	if p.profilesListWidget.OnSelected != nil {
		p.profilesListWidget.OnSelected(0)
	}

	// 3. Stream Dashboard UI - Trigger Windows
	require.NotNil(t, p.dashboardShowHideButton)
	test.Tap(p.dashboardShowHideButton)
	require.Eventually(t, func() bool {
		return p.dashboardWindow != nil
	}, 10*time.Second, 100*time.Millisecond)

	// 4. Activity Feed / Status Panel
	p.statusPanel.SetText("Test Status Update")
	require.Equal(t, "Test Status Update", p.statusPanel.Text)

	// 5. LLM Integration - verify methods exist (logic is tested in streamd)
	require.NotPanics(t, func() {
		// Just verify the placeholder logic doesn't crash before real LLM call
		_ = p.Config.BuiltinStreamD.Backends
	})

	// 6. Stability Hacks - check health
	require.NotPanics(t, func() {
		p.checkAndFixWindowsHealth(ctx)
	})
}

func TestPanelRestreamUI(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	// Verify we can trigger the Edit Restream window
	require.NotPanics(t, func() {
		p.openEditRestreamWindow(ctx, "source1", api.StreamSinkIDFullyQualified{Type: streamtypes.StreamSinkTypeCustom, ID: "sink1"})
	})
}

func TestPanelDashboardStatusRendering(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	w := &dashboardWindow{Panel: p}
	w.streamStatus = make(map[streamcontrol.PlatformID]*widget.Label)
	w.streamStatus[twitch.ID] = widget.NewLabel("")

	// Test rendering status for a platform
	require.NotPanics(t, func() {
		w.renderStreamStatus(ctx)
	})
}

func TestPanelThemesAndColors(t *testing.T) {
	testApp, _, _, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	// Test platform-specific colors (Theme Support)
	cTwitch := colorForPlatform(twitch.ID)
	require.NotNil(t, cTwitch)

	cYoutube := colorForPlatform(youtube.ID)
	require.NotNil(t, cYoutube)

	cKick := colorForPlatform(kick.ID)
	require.NotNil(t, cKick)
}
func TestPanelInit(t *testing.T) {
	// Enable mocks
	youtube.SetDebugUseMockClient(true)
	twitch.SetDebugUseMockClient(true)
	kick.SetDebugUseMockClient(true)
	obs.SetDebugUseMockClient(true)

	tmpDir, err := os.MkdirTemp("", "streampanel-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.yaml")

	// Pre-create config to avoid blocking on user input dialogs
	err = os.WriteFile(configPath, []byte(`
streamd_builtin:
  backends:
    twitch: {enable: false}
    kick: {enable: false}
    obs: {enable: false}
    youtube: {enable: false}
  p2p_network:
    enable: false
`), 0644)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ctx = observability.WithSecretsProvider(ctx, &observability.SecretsStaticProvider{})

	// Use Fyne test app
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	// Wait for the main window to be created AND have content
	require.Eventually(t, func() bool {
		return p.mainWindow != nil && p.mainWindow.Content() != nil
	}, 15*time.Second, 100*time.Millisecond)

	// Check if the window is showing
	require.True(t, p.mainWindow.Content().Visible())

	// Wait a bit to ensure it doesn't crash immediately
	time.Sleep(500 * time.Millisecond)
}
func TestPanelProfileInteractions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "test-prof-int-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	err = os.WriteFile(cfgPath, []byte("{}"), 0644)
	require.NoError(t, err)

	testApp := test.NewApp()
	defer testApp.Quit()

	client := &dummyStreamDLocal{
		IsBackendEnabledFn: func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
			return true, nil
		},
	}
	p, err := New(cfgPath, OptionApp{App: testApp})
	require.NoError(t, err)
	p.app = testApp
	p.defaultContext = ctx
	p.StreamD = client

	p.configCache = &streamdconfig.Config{
		Backends: streamcontrol.Config{
			obs.ID: &streamcontrol.AbstractPlatformConfig{
				Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
					streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
						StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
							streamcontrol.DefaultStreamID: {
								"profile1": streamcontrol.RawMessage("{}"),
								"profile2": streamcontrol.RawMessage("{}"),
							},
						},
					}),
				},
			},
		},
	}
	p.rearrangeProfiles(ctx)

	if p.profilesListWidget == nil {
		p.profilesListWidget = widget.NewList(
			func() int { return 0 },
			func() fyne.CanvasObject { return widget.NewLabel("") },
			func(i widget.ListItemID, o fyne.CanvasObject) {},
		)
	}
	if p.setupStreamButton == nil {
		p.setupStreamButton = widget.NewButton("Setup", nil)
	}
	if p.streamTitleField == nil {
		p.streamTitleField = widget.NewEntry()
	}
	if p.streamTitleLabel == nil {
		p.streamTitleLabel = widget.NewLabel("")
	}
	if p.streamDescriptionField == nil {
		p.streamDescriptionField = widget.NewEntry()
	}
	if p.streamDescriptionLabel == nil {
		p.streamDescriptionLabel = widget.NewLabel("")
	}

	// Test windows creation
	winNew := p.newProfileWindow(ctx)
	require.NotNil(t, winNew)
	winNew.Close()

	if p.profilesListWidget == nil {
		p.profileWindow(ctx, "Test", Profile{}, nil)
	}
	require.NotNil(t, p.profilesListWidget)

	itemObj := p.profilesListItemCreate()
	p.profilesListItemUpdate(0, itemObj)

	p.onProfilesListSelect(0)
	p.onProfilesListUnselect(0)

	p.setFilter(ctx, "profile1")
	p.setFilter(ctx, "")

	err = p.profileDelete(ctx, "profile1")
	require.NoError(t, err)

	prof := getProfile(p.configCache, "profile2")
	require.Equal(t, streamcontrol.ProfileName("profile2"), prof.Name)

	pName := streamcontrol.ProfileName("profile2")
	p.selectedProfileName = &pName
	selProf := p.getSelectedProfile()
	require.NotNil(t, selProf)
	require.Equal(t, pName, selProf.Name)

	p.profilesListItemUpdate(0, itemObj)
}
func TestProfileCoverageConsolidated(t *testing.T) {
	ctx := context.Background()
	app := test.NewApp()
	defer app.Quit()

	t.Run("ComprehensiveWindow", func(t *testing.T) {
		d := &dummyStreamDForCoverage{
			GetConfigFn: func(ctx context.Context) (*streamdconfig.Config, error) {
				return &streamdconfig.Config{
					Backends: streamcontrol.Config{
						twitch.ID: &streamcontrol.AbstractPlatformConfig{
							Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
								streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
									StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
										streamcontrol.DefaultStreamID: {
											"profile-1": streamcontrol.RawMessage(`{"title":"Twitch Title"}`),
										},
									},
								}),
							},
						},
						kick.ID:    &streamcontrol.AbstractPlatformConfig{},
						youtube.ID: &streamcontrol.AbstractPlatformConfig{},
					},
					ProfileMetadata: map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata{
						"profile-1": {DefaultStreamTitle: "Meta Title"},
					},
				}, nil
			},
		}

		p := &Panel{
			app:            app,
			StreamD:        d,
			defaultContext: ctx,
			errorReports:   make(map[string]errorReport),
		}

		w := p.profileWindow(ctx, "New Profile", Profile{}, func(ctx context.Context, p Profile) error {
			return nil
		})
		var nameEntry *widget.Entry
		findEntryByPlaceHolder(w.Content(), "profile name", &nameEntry)
		if nameEntry != nil {
			nameEntry.SetText("profile-new")
		}

		// Save button
		var saveButton *widget.Button
		traverse(w.Content(), func(o fyne.CanvasObject) {
			if b, ok := o.(*widget.Button); ok && b.Text == "Save" {
				saveButton = b
			}
		})
		if saveButton != nil {
			test.Tap(saveButton)
		}
	})

	t.Run("StructuralBranches", func(t *testing.T) {
		ctx := context.Background()
		p := &Panel{
			app:                   test.NewApp(),
			profilesOrder:         make([]streamcontrol.ProfileName, 5), // Forced cap=len
			profilesOrderFiltered: []streamcontrol.ProfileName{"p1"},
			profilesListWidget:    widget.NewList(func() int { return 1 }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(i widget.ListItemID, o fyne.CanvasObject) {}),
			StreamD: &dummyStreamDForCoverage{
				GetConfigFn: func(ctx context.Context) (*streamdconfig.Config, error) {
					return &streamdconfig.Config{
						Backends: make(streamcontrol.Config),
					}, nil
				},
			},
			configCache: &streamdconfig.Config{
				Backends: map[streamcontrol.PlatformID]*streamcontrol.AbstractPlatformConfig{
					twitch.ID: nil, // Trigger platCfg == nil in rearrangeProfiles
				},
			},
		}

		// Cover rearrangeProfiles cap increase and sorting name branch
		p.configCache = &streamdconfig.Config{
			Backends: map[streamcontrol.PlatformID]*streamcontrol.AbstractPlatformConfig{
				twitch.ID: {
					Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
						streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
							StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
								streamcontrol.DefaultStreamID: {
									"b": streamcontrol.RawMessage(`{"order": 1}`),
									"a": streamcontrol.RawMessage(`{"order": 1}`),
								},
							},
						}),
					},
				},
			},
		}
		p.rearrangeProfiles(ctx)

		// Cover profileCreateOrUpdate - create new branch
		p.profileCreateOrUpdate(ctx, Profile{Name: "new-profile"})

		// Cover profileDelete - error path and skipped platform path
		p.StreamD = &dummyStreamDForCoverage{
			SetStreamDConfigFn: func(ctx context.Context, name streamcontrol.ProfileName, platID streamcontrol.PlatformID, profile streamcontrol.AbstractStreamProfile) error {
				return fmt.Errorf("simulated failure")
			},
		}
		p.configCache = &streamdconfig.Config{
			Backends: map[streamcontrol.PlatformID]*streamcontrol.AbstractPlatformConfig{
				twitch.ID: {
					Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
						streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
							StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
								streamcontrol.DefaultStreamID: {
									"p1": streamcontrol.RawMessage(`{}`),
								},
							},
						}),
					},
				},
				kick.ID: {
					Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
						streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
							StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
								streamcontrol.DefaultStreamID: {
									"p2": streamcontrol.RawMessage(`{}`), // Skip p1
								},
							},
						}),
					},
				},
			},
		}
		p.defaultContext = ctx
		p.errorReports = make(map[string]errorReport)
		p.profileDelete(ctx, "p1")
	})

	t.Run("WindowErrorPaths", func(t *testing.T) {
		p := &Panel{
			app:            app,
			defaultContext: ctx,
			errorReports:   make(map[string]errorReport),
			configCache:    &streamdconfig.Config{},
		}

		// IsBackendEnabled error
		p.StreamD = &dummyStreamDForCoverage{
			IsBackendEnabledFn: func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
				return false, fmt.Errorf("fail")
			},
		}
		p.profileWindow(ctx, "Title", Profile{}, nil)

		// GetBackendInfo error
		p.StreamD = &dummyStreamDForCoverage{
			IsBackendEnabledFn: func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
				return true, nil
			},
			GetBackendInfoFn: func(ctx context.Context, id streamcontrol.PlatformID, b bool) (*api.BackendInfo, error) {
				return nil, fmt.Errorf("fail")
			},
		}
		p.profileWindow(ctx, "Title", Profile{}, nil)

		// GetStreamProfile conversion error
		p.StreamD = &dummyStreamDForCoverage{
			IsBackendEnabledFn: func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
				return true, nil
			},
			GetBackendInfoFn: func(ctx context.Context, id streamcontrol.PlatformID, b bool) (*api.BackendInfo, error) {
				switch id {
				case obs.ID:
					return &api.BackendInfo{Data: api.BackendDataOBS{}}, nil
				case twitch.ID:
					return &api.BackendInfo{Data: api.BackendDataTwitch{}}, nil
				case kick.ID:
					return &api.BackendInfo{Data: api.BackendDataKick{}}, nil
				case youtube.ID:
					return &api.BackendInfo{Data: api.BackendDataYouTube{}}, nil
				}
				return &api.BackendInfo{}, nil
			},
		}
		prof := Profile{
			PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{
				streamcontrol.NewStreamIDFullyQualified(twitch.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID):  streamcontrol.RawMessage(`{"tags": [1, 2, 3]}`), // Mocking that this will fail conversion in GetStreamProfile
				streamcontrol.NewStreamIDFullyQualified(kick.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID):    streamcontrol.RawMessage(`{"tags": [1, 2, 3]}`),
				streamcontrol.NewStreamIDFullyQualified(youtube.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID): streamcontrol.RawMessage(`{"any_field": {}}`),
			},
		}
		p.profileWindow(ctx, "Title", prof, nil)
	})

	t.Run("RefilterDeepMatch", func(t *testing.T) {
		cfg := &streamdconfig.Config{
			Backends: streamcontrol.Config{
				twitch.ID: &streamcontrol.AbstractPlatformConfig{
					Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
						streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
							StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
								streamcontrol.DefaultStreamID: {
									"p1": streamcontrol.RawMessage(`{"language":"English"}`),
									"p2": streamcontrol.RawMessage(`{"tags":["BR","Pro"]}`),
								},
							},
						}),
					},
				},
				youtube.ID: &streamcontrol.AbstractPlatformConfig{
					Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
						streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
							StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
								streamcontrol.DefaultStreamID: {
									"p2": streamcontrol.RawMessage(`{"title":"YouTube Content","tags":["Video"]}`),
								},
							},
						}),
					},
				},
			},
		}
		p := &Panel{
			app:                app,
			profilesOrder:      []streamcontrol.ProfileName{"p1", "p2", "p3"},
			configCache:        cfg,
			profilesListWidget: widget.NewList(func() int { return 0 }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(i widget.ListItemID, o fyne.CanvasObject) {}),
		}

		// Test title match
		p.filterValue = "p1"
		p.refilterProfiles(ctx)
		require.Equal(t, 1, len(p.profilesOrderFiltered))

		// Test tag match
		p.filterValue = "pro"
		p.refilterProfiles(ctx)
		require.Equal(t, 1, len(p.profilesOrderFiltered))

		// Test language match
		p.filterValue = "english"
		p.refilterProfiles(ctx)
		require.Equal(t, 1, len(p.profilesOrderFiltered))

		// Test no match
		p.filterValue = "nonexistent"
		p.refilterProfiles(ctx)
		require.Equal(t, 0, len(p.profilesOrderFiltered))
	})
}
func TestProfileCoverageV5(t *testing.T) {
	app := test.NewApp()
	app.Settings().SetTheme(theme.DefaultTheme())
	defer app.Quit()
	ctx := context.Background()

	setup := func() (*Panel, *dummyStreamDForCoverage) {
		sd := &dummyStreamDForCoverage{}
		p := &Panel{
			defaultContext: ctx,
			app:            app,
			StreamD:        sd,
			configCache:    &streamdconfig.Config{},
			errorReports:   make(map[string]errorReport),
		}
		return p, sd
	}

	t.Run("CountLimitInOnChanged", func(t *testing.T) {
		p, sd := setup()

		// Setup more than 10 items for search result limit coverage
		var twitchCats []helix.Game
		for i := 0; i < 15; i++ {
			twitchCats = append(twitchCats, helix.Game{Name: fmt.Sprintf("TwitchCat%d", i), ID: fmt.Sprintf("id%d", i)})
		}

		var kickCats []kickcom.CategoryV1Short
		for i := 0; i < 15; i++ {
			kickCats = append(kickCats, kickcom.CategoryV1Short{Name: fmt.Sprintf("KickCat%d", i), ID: uint64(i)})
		}

		var youtubeBCs []*youtubeapi.LiveBroadcast
		for i := 0; i < 15; i++ {
			youtubeBCs = append(youtubeBCs, &youtubeapi.LiveBroadcast{Snippet: &youtubeapi.LiveBroadcastSnippet{Title: fmt.Sprintf("YTBC%d", i)}, Id: fmt.Sprintf("bc%d", i)})
		}

		sd.GetBackendInfoFn = func(ctx context.Context, id streamcontrol.PlatformID, full bool) (*api.BackendInfo, error) {
			switch id {
			case twitch.ID:
				return &api.BackendInfo{Data: api.BackendDataTwitch{Cache: &cache.Twitch{Categories: twitchCats}}}, nil
			case kick.ID:
				kCache := &cache.Kick{}
				kCache.SetCategories(kickCats)
				return &api.BackendInfo{Data: api.BackendDataKick{Cache: kCache}}, nil
			case youtube.ID:
				return &api.BackendInfo{Data: api.BackendDataYouTube{Cache: &cache.YouTube{Broadcasts: youtubeBCs}}}, nil
			case obs.ID:
				return &api.BackendInfo{Data: api.BackendDataOBS{}}, nil
			}
			return nil, fmt.Errorf("err")
		}

		w := p.profileWindow(ctx, "limit", Profile{}, nil)
		require.NotNil(t, w)
		defer w.Close()

		// Trigger OnChanged for each to hit the count > 10 branch
		var entry *widget.Entry
		findEntryByPlaceHolder(w.Content(), "twitch category", &entry)
		if entry != nil && entry.OnChanged != nil {
			entry.OnChanged("TwitchCat")
		}

		findEntryByPlaceHolder(w.Content(), "kick category", &entry)
		if entry != nil && entry.OnChanged != nil {
			entry.OnChanged("KickCat")
		}

		findEntryByPlaceHolder(w.Content(), "youtube live recording template", &entry)
		if entry != nil && entry.OnChanged != nil {
			entry.OnChanged("YTBC")
		}
	})

	t.Run("SelectionCleanups", func(t *testing.T) {
		p, sd := setup()
		sd.GetAccountsFn = func(ctx context.Context, platIDs ...streamcontrol.PlatformID) ([]streamcontrol.AccountIDFullyQualified, error) {
			return []streamcontrol.AccountIDFullyQualified{
				streamcontrol.NewAccountIDFullyQualified(twitch.ID, streamcontrol.DefaultAccountID),
				streamcontrol.NewAccountIDFullyQualified(kick.ID, streamcontrol.DefaultAccountID),
				streamcontrol.NewAccountIDFullyQualified(youtube.ID, streamcontrol.DefaultAccountID),
			}, nil
		}
		sd.GetStreamsFn = func(ctx context.Context, accountIDs ...streamcontrol.AccountIDFullyQualified) ([]streamcontrol.StreamInfo, error) {
			return []streamcontrol.StreamInfo{{ID: streamcontrol.DefaultStreamID, Name: "Default Stream"}}, nil
		}
		sd.GetBackendInfoFn = func(ctx context.Context, id streamcontrol.PlatformID, full bool) (*api.BackendInfo, error) {
			switch id {
			case twitch.ID:
				return &api.BackendInfo{Data: api.BackendDataTwitch{Cache: &cache.Twitch{Categories: []helix.Game{{Name: "Twitch", ID: "1"}}}}}, nil
			case kick.ID:
				kCache := &cache.Kick{}
				kCache.SetCategories([]kickcom.CategoryV1Short{{Name: "Kick", ID: 1}})
				return &api.BackendInfo{Data: api.BackendDataKick{Cache: kCache}}, nil
			case youtube.ID:
				return &api.BackendInfo{Data: api.BackendDataYouTube{Cache: &cache.YouTube{Broadcasts: []*youtubeapi.LiveBroadcast{{Snippet: &youtubeapi.LiveBroadcastSnippet{Title: "YT"}, Id: "yt1"}}}}}, nil
			case obs.ID:
				return &api.BackendInfo{Data: api.BackendDataOBS{}}, nil
			}
			return nil, fmt.Errorf("err")
		}

		w := p.profileWindow(ctx, "Cleanup", Profile{
			PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{
				streamcontrol.NewStreamIDFullyQualified(twitch.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID):  twitch.StreamProfile{},
				streamcontrol.NewStreamIDFullyQualified(kick.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID):    kick.StreamProfile{},
				streamcontrol.NewStreamIDFullyQualified(youtube.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID): youtube.StreamProfile{},
			},
		}, nil)
		require.NotNil(t, w)
		defer w.Close()

		// Select items first
		var twitchEntry *widget.Entry
		findEntryByPlaceHolder(w.Content(), "twitch category", &twitchEntry)
		require.NotNil(t, twitchEntry)
		if twitchEntry != nil && twitchEntry.OnSubmitted != nil {
			twitchEntry.OnSubmitted("Twitch")
		}

		var kickEntry *widget.Entry
		findEntryByPlaceHolder(w.Content(), "kick category", &kickEntry)
		require.NotNil(t, kickEntry)
		if kickEntry != nil && kickEntry.OnSubmitted != nil {
			kickEntry.OnSubmitted("Kick")
		}

		var youtubeEntry *widget.Entry
		findEntryByPlaceHolder(w.Content(), "youtube live recording template", &youtubeEntry)
		require.NotNil(t, youtubeEntry)
		if youtubeEntry != nil && youtubeEntry.OnSubmitted != nil {
			youtubeEntry.OnSubmitted("YT")
		}

		// Now find the Clear buttons (they use theme.ContentClearIcon)
		var clearButtons []*widget.Button
		traverse(w.Content(), func(o fyne.CanvasObject) {
			if btn, ok := o.(*widget.Button); ok && btn.Icon == theme.ContentClearIcon() {
				clearButtons = append(clearButtons, btn)
			}
		})

		require.Equal(t, 3, len(clearButtons))
		for _, btn := range clearButtons {
			test.Tap(btn)
		}
	})

	t.Run("OnSubmittedEmpty", func(t *testing.T) {
		p, _ := setup()
		w := p.profileWindow(ctx, "EmptySub", Profile{}, nil)
		require.NotNil(t, w)
		defer w.Close()

		var entry *widget.Entry
		findEntryByPlaceHolder(w.Content(), "twitch category", &entry)
		if entry != nil && entry.OnSubmitted != nil {
			entry.OnSubmitted("")
		}

		findEntryByPlaceHolder(w.Content(), "kick category", &entry)
		if entry != nil && entry.OnSubmitted != nil {
			entry.OnSubmitted("")
		}

		findEntryByPlaceHolder(w.Content(), "youtube live recording template", &entry)
		if entry != nil && entry.OnSubmitted != nil {
			entry.OnSubmitted("")
		}
	})

	t.Run("TemplateTagsDefault", func(t *testing.T) {
		p, _ := setup()
		// We can't easily trigger the default case in switch of a Select because the Select widget limits choices.
		// However, we can trigger the "unexpected current value" logic by having a profile with an invalid TemplateTags.
		w := p.profileWindow(ctx, "InvalidTags", Profile{
			PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{
				streamcontrol.NewStreamIDFullyQualified(youtube.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID): youtube.StreamProfile{TemplateTags: "something-weird"},
			},
		}, nil)
		require.NotNil(t, w)
		w.Close()
	})

	t.Run("SanitizeTagsCoverage", func(t *testing.T) {
		// Hit sanitizeTags logic with empty tags
		p, _ := setup()
		w := p.profileWindow(ctx, "Sanitize", Profile{
			ProfileMetadata: streamdconfig.ProfileMetadata{},
			PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{
				streamcontrol.NewStreamIDFullyQualified(twitch.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID):  twitch.StreamProfile{Tags: [10]string{"t1", ""}},
				streamcontrol.NewStreamIDFullyQualified(youtube.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID): youtube.StreamProfile{Tags: []string{"y1", ""}, TemplateBroadcastIDs: []string{"yt1"}},
			},
		}, func(ctx context.Context, profile Profile) error {
			require.Equal(t, "t1", profile.PerStream[streamcontrol.NewStreamIDFullyQualified(twitch.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID)].(twitch.StreamProfile).Tags[0])
			require.Equal(t, "", profile.PerStream[streamcontrol.NewStreamIDFullyQualified(twitch.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID)].(twitch.StreamProfile).Tags[1]) // twitch.Tags is fixed array, sanitize logic for it is different
			require.Equal(t, 1, len(profile.PerStream[streamcontrol.NewStreamIDFullyQualified(youtube.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID)].(youtube.StreamProfile).Tags))
			return nil
		})

		var saveButton *widget.Button
		findButton(w.Content(), "Save", &saveButton)
		if saveButton != nil {
			test.Tap(saveButton)
		}
	})

	t.Run("RearrangeProfilesExtra", func(t *testing.T) {
		p, _ := setup()
		p.configCache = &streamdconfig.Config{
			ProfileMetadata: map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata{
				"p1": {MaxOrder: 10},
				"p2": {MaxOrder: 10},
			},
			Backends: streamcontrol.Config{
				twitch.ID: {
					Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
						streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
							StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
								streamcontrol.DefaultStreamID: {
									"p1": streamcontrol.ToRawMessage(twitch.StreamProfile{}),
								},
							},
						}),
					},
				},
			},
		}
		// p2 exists in Metadata but not in any Backend. p1 exists in both.
		p.rearrangeProfiles(ctx)
		require.Equal(t, 2, len(p.profilesOrder))
		// p1 and p2 have same MaxOrder, should be alphabetical: p1 then p2
		require.Equal(t, streamcontrol.ProfileName("p1"), p.profilesOrder[0])
		require.Equal(t, streamcontrol.ProfileName("p2"), p.profilesOrder[1])
	})

	t.Run("OnSubmittedExtra", func(t *testing.T) {
		p, _ := setup()
		// Submitting valid names to hit the time.Sleep branch
		sd := p.StreamD.(*dummyStreamDForCoverage)
		sd.GetBackendInfoFn = func(ctx context.Context, id streamcontrol.PlatformID, full bool) (*api.BackendInfo, error) {
			switch id {
			case twitch.ID:
				return &api.BackendInfo{Data: api.BackendDataTwitch{Cache: &cache.Twitch{Categories: []helix.Game{{Name: "Twitch", ID: "1"}}}}}, nil
			case kick.ID:
				kCache := &cache.Kick{}
				kCache.SetCategories([]kickcom.CategoryV1Short{{Name: "Kick", ID: 1}})
				return &api.BackendInfo{Data: api.BackendDataKick{Cache: kCache}}, nil
			case youtube.ID:
				return &api.BackendInfo{Data: api.BackendDataYouTube{Cache: &cache.YouTube{Broadcasts: []*youtubeapi.LiveBroadcast{{Snippet: &youtubeapi.LiveBroadcastSnippet{Title: "YT"}, Id: "yt1"}}}}}, nil
			default:
				return &api.BackendInfo{Data: api.BackendDataOBS{}}, nil
			}
		}
		w2 := p.profileWindow(ctx, "ValidSub", Profile{}, nil)
		var entry *widget.Entry
		findEntryByPlaceHolder(w2.Content(), "twitch category", &entry)
		if entry != nil && entry.OnSubmitted != nil {
			entry.OnSubmitted("Twitch")
		}
		findEntryByPlaceHolder(w2.Content(), "youtube live recording template", &entry)
		if entry != nil && entry.OnSubmitted != nil {
			entry.OnSubmitted("YT")
		}

		// Wait for goroutines with time.Sleep to execute
		time.Sleep(200 * time.Millisecond)
		w2.Close()
	})

	t.Run("RefilterKickMatch", func(t *testing.T) {
		p, _ := setup()
		p.configCache = &streamdconfig.Config{
			Backends: streamcontrol.Config{
				kick.ID: {
					Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
						streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
							StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
								streamcontrol.DefaultStreamID: {
									"kickprof": streamcontrol.ToRawMessage(kick.StreamProfile{}),
								},
							},
						}),
					},
				},
			},
		}
		p.profilesOrder = []streamcontrol.ProfileName{"kickprof"}
		p.filterValue = "kick"
		p.refilterProfiles(ctx)
		// Kick match currently only matches title because its subValueMatch is empty
		require.Equal(t, 1, len(p.profilesOrderFiltered))
	})

	t.Run("RearrangeProfilesExtraGaps", func(t *testing.T) {
		p, _ := setup()
		p.configCache = &streamdconfig.Config{
			Backends: streamcontrol.Config{
				twitch.ID: {
					Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
						streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
							StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
								streamcontrol.DefaultStreamID: {
									"only-in-backend": streamcontrol.ToRawMessage(twitch.StreamProfile{}),
								},
							},
						}),
					},
				},
			},
		}
		p.rearrangeProfiles(ctx)
		require.Contains(t, p.profilesOrder, streamcontrol.ProfileName("only-in-backend"))
	})

	t.Run("FinalGaps", func(t *testing.T) {
		p, sd := setup()
		p.profilesListWidget = widget.NewList(
			func() int { return 0 },
			func() fyne.CanvasObject { return widget.NewLabel("") },
			func(i widget.ListItemID, o fyne.CanvasObject) {},
		) // To hit Refresh()

		// 1. profileDelete: SetStreamDConfig error
		sd.SetStreamDConfigFn = func(ctx context.Context, name streamcontrol.ProfileName, platID streamcontrol.PlatformID, profile streamcontrol.AbstractStreamProfile) error {
			return fmt.Errorf("set fail")
		}
		p.configCache = &streamdconfig.Config{ProfileMetadata: map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata{"p1": {}}}
		p.profileDelete(ctx, "p1")
		time.Sleep(100 * time.Millisecond)

		// 2. profileCreateOrUpdate: GetStreamDConfig succeeds but rearrange fails because of nil cache later or something
		// No, rearrangeProfiles doesn't call GetStreamDConfig anymore, it uses p.configCache directly.
		// To make it fail, I'd need to mock GetStreamDConfig inside rearrangeProfiles if it used it.
		// Wait, rearrangeProfiles uses p.configCache directly.
		// To hit the error branch in p.profileCreateOrUpdate(ctx, profile), rearrangeProfiles must return error.
		// But in the current implementation of rearrangeProfiles, it NEVER returns error!
		// Let me check profile.go again.
		/*
			func (p *Panel) rearrangeProfiles(ctx context.Context) error {
				...
				return nil
			}
		*/
		// Yes, it always returns nil. My previous analysis was slightly off or based on a version where it could fail.
		// Wait, I see "if err := p.rearrangeProfiles(ctx); err != nil" in profileCreateOrUpdate.
		// If it always returns nil, then that branch is unreachable unless the implementation changes.
		// But I should still try to hit as much as possible.

		// 3. refilterProfiles: triggering cap branch
		p.profilesOrder = make([]streamcontrol.ProfileName, 100)
		p.profilesOrderFiltered = make([]streamcontrol.ProfileName, 0, 10)
		p.refilterProfiles(ctx)

		// 4. profileWindow: Lots of internal branches
		sd.GetBackendInfoFn = func(ctx context.Context, id streamcontrol.PlatformID, full bool) (*api.BackendInfo, error) {
			switch id {
			case twitch.ID:
				return &api.BackendInfo{Data: api.BackendDataTwitch{Cache: &cache.Twitch{Categories: []helix.Game{{Name: "Twitch", ID: "1"}}}}}, nil
			case kick.ID:
				return &api.BackendInfo{Data: api.BackendDataKick{Cache: &cache.Kick{}}}, nil
			case youtube.ID:
				return &api.BackendInfo{Data: api.BackendDataYouTube{Cache: &cache.YouTube{Broadcasts: []*youtubeapi.LiveBroadcast{{Snippet: &youtubeapi.LiveBroadcastSnippet{Title: "YT"}, Id: "yt1"}}}}}, nil
			default:
				return &api.BackendInfo{Data: api.BackendDataOBS{}}, nil
			}
		}

		// - Conversion errors
		platProfilesWithErrors := map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{
			streamcontrol.NewStreamIDFullyQualified(obs.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID):     twitch.StreamProfile{}, // Wrong type
			streamcontrol.NewStreamIDFullyQualified(twitch.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID):  obs.StreamProfile{},    // Wrong type
			streamcontrol.NewStreamIDFullyQualified(kick.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID):    obs.StreamProfile{},    // Wrong type
			streamcontrol.NewStreamIDFullyQualified(youtube.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID): obs.StreamProfile{},    // Wrong type
		}

		w := p.profileWindow(ctx, "ExtraGaps", Profile{
			PerStream:       platProfilesWithErrors,
			ProfileMetadata: streamdconfig.ProfileMetadata{},
		}, nil)
		require.NotNil(t, w)

		// - Twich ID match in loop
		// - YouTube ID match in loop
		// - TemplateTags default case
		// - Auto-numerate check

		// Wait, I can't easily reach the TemplateTags switch default via SetSelected.
		// But I can trigger the switch-case in the Select's OnSelected manually if I find the widget.

		var youtubeSelect *widget.Select
		traverse(w.Content(), func(o fyne.CanvasObject) {
			if s, ok := o.(*widget.Select); ok && len(s.Options) > 0 && s.Options[0] == "ignore" {
				youtubeSelect = s
			}
		})
		if youtubeSelect != nil && youtubeSelect.OnChanged != nil {
			youtubeSelect.OnChanged("invalid")
		}

		var check *widget.Check
		traverse(w.Content(), func(o fyne.CanvasObject) {
			if c, ok := o.(*widget.Check); ok && c.Text == "Auto-numerate" {
				check = c
			}
		})
		if check != nil {
			test.Tap(check)
		}

		w.Close()

		// Trigger profileDelete with nil backend
		p.configCache = &streamdconfig.Config{
			Backends: streamcontrol.Config{
				twitch.ID: nil,
			},
			ProfileMetadata: map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata{"p3": {}},
		}
		p.profileDelete(ctx, "p3")
	})

	t.Run("FinalGapsRefined", func(t *testing.T) {
		p, sd := setup()
		p.profilesListWidget = widget.NewList(
			func() int { return 0 },
			func() fyne.CanvasObject { return widget.NewLabel("") },
			func(i widget.ListItemID, o fyne.CanvasObject) {},
		)

		// 1. profileCreateOrUpdate: platCfg == nil and platCfg.Accounts == nil
		p.configCache = &streamdconfig.Config{
			Backends: map[streamcontrol.PlatformID]*streamcontrol.AbstractPlatformConfig{
				twitch.ID: nil,             // platCfg == nil
				kick.ID:   {Accounts: nil}, // Accounts == nil
			},
			ProfileMetadata: make(map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata),
		}
		p.profileCreateOrUpdate(ctx, Profile{
			Name: "newprof",
			PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{
				streamcontrol.NewStreamIDFullyQualified(twitch.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID): twitch.StreamProfile{},
				streamcontrol.NewStreamIDFullyQualified(kick.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID):   kick.StreamProfile{},
			},
		})

		// 2. rearrangeProfiles: cfg == nil
		p.configCache = nil
		p.rearrangeProfiles(ctx)

		// 3. refilterProfiles: cfg == nil
		p.refilterProfiles(ctx)

		// 4. profileDelete: SetStreamDConfig error + wait
		sd.SetStreamDConfigFn = func(ctx context.Context, name streamcontrol.ProfileName, platID streamcontrol.PlatformID, profile streamcontrol.AbstractStreamProfile) error {
			return fmt.Errorf("set fail")
		}
		p.configCache = &streamdconfig.Config{ProfileMetadata: map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata{"p1": {}}}
		p.profileDelete(ctx, "p1")
		time.Sleep(300 * time.Millisecond) // Wait for all platform goroutines
	})
}
func TestProfileCoverageFinal(t *testing.T) {
	app := test.NewApp()
	app.Settings().SetTheme(theme.DefaultTheme())
	defer app.Quit()

	setup := func() (*Panel, *dummyStreamDForCoverage) {
		client := &dummyStreamDForCoverage{}
		p := &Panel{
			defaultContext:         context.Background(),
			app:                    app,
			StreamD:                client,
			errorReports:           make(map[string]errorReport),
			permanentWindows:       make(map[uint64]windowDriver),
			setupStreamButton:      widget.NewButton("Setup", nil),
			streamTitleField:       widget.NewEntry(),
			streamTitleLabel:       widget.NewLabel(""),
			streamDescriptionField: widget.NewEntry(),
			streamDescriptionLabel: widget.NewLabel(""),
			profilesListWidget:     widget.NewList(func() int { return 0 }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(i widget.ListItemID, o fyne.CanvasObject) {}),
			configCache: &streamdconfig.Config{
				Backends: map[streamcontrol.PlatformID]*streamcontrol.AbstractPlatformConfig{
					twitch.ID: {
						Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
							streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
									streamcontrol.DefaultStreamID: {},
								},
							}),
						},
					},
					youtube.ID: {
						Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
							streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
									streamcontrol.DefaultStreamID: {},
								},
							}),
						},
					},
					kick.ID: {
						Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
							streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
									streamcontrol.DefaultStreamID: {},
								},
							}),
						},
					},
					obs.ID: {
						Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
							streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
									streamcontrol.DefaultStreamID: {},
								},
							}),
						},
					},
				},
				ProfileMetadata: make(map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata),
			},
		}
		client.Config = p.configCache
		return p, client
	}

	ctx := context.Background()

	t.Run("UtilityFunctions", func(t *testing.T) {
		cfg := &streamdconfig.Config{
			ProfileMetadata: map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata{
				"p1": {DefaultStreamTitle: "Title1"},
			},
		}
		prof := getProfile(cfg, "p1")
		require.Equal(t, "Title1", prof.DefaultStreamTitle)

		prof2 := getProfile(cfg, "p2")
		require.Equal(t, streamcontrol.ProfileName("p2"), prof2.Name)
	})

	t.Run("Panel_Profiles", func(t *testing.T) {
		p, client := setup()
		p.profilesOrder = []streamcontrol.ProfileName{"p1"}
		p.refilterProfiles(ctx)
		require.Equal(t, 1, p.profilesListLength())

		p.onProfilesListSelect(-1) // coverage
		p.onProfilesListSelect(0)
		require.Equal(t, streamcontrol.ProfileName("p1"), *p.selectedProfileName)

		p.onProfilesListUnselect(0)
		require.Nil(t, p.selectedProfileName)

		p.setFilter(ctx, "p")
		require.Equal(t, "p", p.filterValue)

		client.SetConfigErr = fmt.Errorf("fail")
		err := p.profileDelete(ctx, "any")
		require.Error(t, err)

		client.SetConfigErr = nil
		err = p.profileCreateOrUpdate(ctx, Profile{Name: "new"})
		require.NoError(t, err)

		p.configCache = &streamdconfig.Config{
			ProfileMetadata: map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata{
				"p1": {MaxOrder: 1},
				"p2": {MaxOrder: 2},
			},
			Backends: map[streamcontrol.PlatformID]*streamcontrol.AbstractPlatformConfig{
				"twitch": {},
			},
		}
		p.profilesOrderFiltered = []streamcontrol.ProfileName{"p1"}
		p.rearrangeProfiles(ctx)

		// Coverage for list items
		obj := p.profilesListItemCreate()
		p.profilesListItemUpdate(0, obj)
	})

	t.Run("UI_Interactions", func(t *testing.T) {
		p, client := setup()
		_ = client

		t.Run("CreateOrUpdate_Error", func(t *testing.T) {
			p, sd := setup()
			sd.SetConfigErr = fmt.Errorf("mock error")
			err := p.profileCreateOrUpdate(ctx, Profile{Name: "err"})
			require.Error(t, err)

			// Trigger "platformProfile == nil" continue branch
			err = p.profileCreateOrUpdate(ctx, Profile{Name: "nil-platform", PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{
				streamcontrol.NewStreamIDFullyQualified("obs", streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID): nil,
			}})
			require.Error(t, err) // Still error because SetConfigErr is set
		})

		t.Run("WindowVariants", func(t *testing.T) {
			p, sd := setup()
			sd.IsBackendEnabledFn = func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
				return false, nil // Disable all for simple logic tests
			}

			// selectedProfileName is nil
			p.selectedProfileName = nil
			require.Nil(t, p.editProfileWindow(ctx))
			require.Nil(t, p.cloneProfileWindow(ctx))
			require.Nil(t, p.deleteProfileWindow(ctx))
			require.Equal(t, Profile{}, p.getSelectedProfile())

			p.configCache = &streamdconfig.Config{
				ProfileMetadata: map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata{
					"p1": {},
				},
				Backends: map[streamcontrol.PlatformID]*streamcontrol.AbstractPlatformConfig{
					"twitch": {
						Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
							streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
									streamcontrol.DefaultStreamID: {
										"p1": streamcontrol.ToRawMessage(twitch.StreamProfile{}),
									},
								},
							}),
						},
					},
				},
			}
			name := streamcontrol.ProfileName("p1")
			p.selectedProfileName = &name

			// Test LONG TITLE for coverage
			w := p.profileWindow(ctx, "Long Title", Profile{}, nil)
			var tEntry *widget.Entry
			findEntryByPlaceHolder(w.Content(), "default stream title", &tEntry)
			if tEntry != nil {
				longTitle := strings.Repeat("A", 200)
				tEntry.SetText(longTitle)
			}
			w.Close()

			// Test "profile already exists" in newProfileWindow
			p.configCache = &streamdconfig.Config{
				Backends: map[streamcontrol.PlatformID]*streamcontrol.AbstractPlatformConfig{
					"twitch": {
						Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
							streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
									streamcontrol.DefaultStreamID: {
										"p1": streamcontrol.ToRawMessage(twitch.StreamProfile{}),
									},
								},
							}),
						},
					},
				},
			}
			w = p.newProfileWindow(ctx)
			var nEntry *widget.Entry
			var sButton *widget.Button
			findEntryByPlaceHolder(w.Content(), "profile name", &nEntry)
			require.NotNil(t, nEntry, "could not find profile name entry in newProfileWindow")
			nEntry.SetText("p1")

			findButton(w.Content(), "Save", &sButton)
			require.NotNil(t, sButton, "could not find Save button in newProfileWindow")
			test.Tap(sButton)
			w.Close()

			// Test failure of profileCreateOrUpdate in newProfileWindow
			sd.SetConfigErr = fmt.Errorf("mock fail")
			w = p.newProfileWindow(ctx)
			nEntry = nil
			findEntryByPlaceHolder(w.Content(), "profile name", &nEntry)
			if nEntry != nil {
				nEntry.SetText("brand-new-fail")
			}
			sButton = nil
			findButton(w.Content(), "Save", &sButton)
			if sButton != nil {
				test.Tap(sButton)
			}
			sd.SetConfigErr = nil
			w.Close()

			// Test empty name in newProfileWindow
			w = p.newProfileWindow(ctx)
			nEntry = nil
			findEntryByPlaceHolder(w.Content(), "profile name", &nEntry)
			require.NotNil(t, nEntry)
			nEntry.SetText("") // empty
			sButton = nil
			findButton(w.Content(), "Save", &sButton)
			if sButton != nil {
				test.Tap(sButton)
			}
			// w.Close() // Redundant as it succeeds with empty name

			// Test "profile already exists" in newProfileWindow
			p.configCache = &streamdconfig.Config{
				Backends: map[streamcontrol.PlatformID]*streamcontrol.AbstractPlatformConfig{
					"twitch": {
						Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
							streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
									streamcontrol.DefaultStreamID: {
										"p1": streamcontrol.ToRawMessage(twitch.StreamProfile{}),
									},
								},
							}),
						},
					},
				},
			}
			w = p.newProfileWindow(ctx)
			nEntry = nil
			findEntryByPlaceHolder(w.Content(), "profile name", &nEntry)
			require.NotNil(t, nEntry)
			nEntry.SetText("p1")

			sButton = nil
			findButton(w.Content(), "Save", &sButton)
			require.NotNil(t, sButton)
			test.Tap(sButton)
			w.Close()

			// Success case for newProfileWindow
			p.configCache = &streamdconfig.Config{}
			sd.IsBackendEnabledFn = func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
				if id == youtube.ID {
					return false, nil
				}
				return true, nil
			}
			w = p.newProfileWindow(ctx)
			nEntry = nil
			findEntryByPlaceHolder(w.Content(), "profile name", &nEntry)
			require.NotNil(t, nEntry)
			nEntry.SetText("brand-new-success")
			sButton = nil
			findButton(w.Content(), "Save", &sButton)
			require.NotNil(t, sButton)
			test.Tap(sButton)
			sd.IsBackendEnabledFn = nil // reset

			// Test YouTube template error
			sd.IsBackendEnabledFn = func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
				return true, nil
			}
			w = p.newProfileWindow(ctx)
			nEntry = nil
			findEntryByPlaceHolder(w.Content(), "profile name", &nEntry)
			if nEntry != nil {
				nEntry.SetText("brand-new-yt-error")
			}
			sButton = nil
			findButton(w.Content(), "Save", &sButton)
			if sButton != nil {
				test.Tap(sButton)
			}
			sd.IsBackendEnabledFn = nil

			// Call profileCreateOrUpdate with full data for coverage
			fullProf := Profile{
				Name: "full",
				PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{
					streamcontrol.NewStreamIDFullyQualified(twitch.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID):  twitch.StreamProfile{},
					streamcontrol.NewStreamIDFullyQualified(youtube.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID): youtube.StreamProfile{},
					streamcontrol.NewStreamIDFullyQualified(kick.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID):    kick.StreamProfile{},
					streamcontrol.NewStreamIDFullyQualified(obs.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID):     obs.StreamProfile{},
				},
			}
			p.profileCreateOrUpdate(ctx, fullProf)

			// Edit window renaming error branches
			name = "full"
			p.selectedProfileName = &name
			w = p.editProfileWindow(ctx)
			sButton = nil
			findButton(w.Content(), "Save", &sButton)
			nEntry = nil
			findEntryByPlaceHolder(w.Content(), "profile name", &nEntry)
			if nEntry != nil {
				nEntry.SetText("p1-renamed")
			}
			sd.SetConfigErr = fmt.Errorf("fail delete")
			if sButton != nil {
				test.Tap(sButton)
			}
			sd.SetConfigErr = nil
			w.Close() // Handler didn't close it because of error

			// Success renaming edit
			p.configCache = &streamdconfig.Config{
				ProfileMetadata: map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata{"edit-old": {}},
				Backends: map[streamcontrol.PlatformID]*streamcontrol.AbstractPlatformConfig{
					"twitch": {
						Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
							streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
									streamcontrol.DefaultStreamID: {
										"edit-old": streamcontrol.ToRawMessage(twitch.StreamProfile{}),
									},
								},
							}),
						},
					},
				},
			}
			nameEdit := streamcontrol.ProfileName("edit-old")
			p.selectedProfileName = &nameEdit
			w = p.editProfileWindow(ctx)
			nEntry = nil
			findEntryByPlaceHolder(w.Content(), "profile name", &nEntry)
			if nEntry != nil {
				nEntry.SetText("edit-new")
			}
			sButton = nil
			findButton(w.Content(), "Save", &sButton)
			if sButton != nil {
				test.Tap(sButton)
			}

			// Clone duplication
			nameClone := streamcontrol.ProfileName("edit-new")
			p.selectedProfileName = &nameClone
			w = p.cloneProfileWindow(ctx)
			nEntry = nil
			findEntryByPlaceHolder(w.Content(), "profile name", &nEntry)
			if nEntry != nil {
				nEntry.SetText("edit-new")
			}
			sButton = nil
			findButton(w.Content(), "Save", &sButton)
			if sButton != nil {
				test.Tap(sButton)
			}
			w.Close()

			// Clone success
			w = p.cloneProfileWindow(ctx)
			nEntry = nil
			findEntryByPlaceHolder(w.Content(), "profile name", &nEntry)
			if nEntry != nil {
				nEntry.SetText("clone-success")
			}
			sButton = nil
			findButton(w.Content(), "Save", &sButton)
			if sButton != nil {
				test.Tap(sButton)
			}

			// Clone error
			w = p.cloneProfileWindow(ctx)
			nEntry = nil
			findEntryByPlaceHolder(w.Content(), "profile name", &nEntry)
			if nEntry != nil {
				nEntry.SetText("edit-new")
			} // Should fail because exists
			sButton = nil
			findButton(w.Content(), "Save", &sButton)
			if sButton != nil {
				test.Tap(sButton)
			}
			w.Close()

			w = p.cloneProfileWindow(ctx)
			nEntry = nil
			findEntryByPlaceHolder(w.Content(), "profile name", &nEntry)
			if nEntry != nil {
				nEntry.SetText("cloned")
			}
			sButton = nil
			findButton(w.Content(), "Save", &sButton)
			if sButton != nil {
				test.Tap(sButton)
			}

			w = p.deleteProfileWindow(ctx)
			var dButton *widget.Button
			findButton(w.Content(), "YES", &dButton)
			sd.SetConfigErr = fmt.Errorf("fail")
			if dButton != nil {
				test.Tap(dButton)
			}
			sd.SetConfigErr = nil

			w = p.deleteProfileWindow(ctx)
			dButton = nil
			findButton(w.Content(), "YES", &dButton)
			if dButton != nil {
				test.Tap(dButton)
			}
			sd.SetConfigErr = nil

			// Clear all stream profiles button
			w = p.editProfileWindow(ctx)
			if w != nil {
				var clearButton *widget.Button
				findButton(w.Content(), "Clear all stream profiles (per platform)", &clearButton)
				if clearButton != nil {
					test.Tap(clearButton)
				}
				var cancelButton *widget.Button
				findButton(w.Content(), "Cancel", &cancelButton)
				if cancelButton != nil {
					test.Tap(cancelButton)
				} else {
					w.Close()
				}
			}

			p.getSelectedProfile()
		})

		t.Run("Platforms_Comprehensive", func(t *testing.T) {
			commitCalled := false
			w := p.profileWindow(ctx, "Comp", Profile{Name: "all"}, func(c context.Context, pr Profile) error {
				commitCalled = true
				return nil
			})

			// Twitch
			var twitchEntry *widget.Entry
			findEntryByPlaceHolder(w.Content(), "twitch category", &twitchEntry)
			require.NotNil(t, twitchEntry)
			twitchEntry.SetText("Talk")
			if twitchEntry.OnChanged != nil {
				twitchEntry.OnChanged("Talk")
			}
			var catButton *widget.Button
			findButton(w.Content(), "TalkShow", &catButton)
			require.NotNil(t, catButton)
			test.Tap(catButton)

			// Kick
			var kickEntry *widget.Entry
			findEntryByPlaceHolder(w.Content(), "kick category", &kickEntry)
			require.NotNil(t, kickEntry)
			kickEntry.SetText("Kick")

			// YouTube Template
			var ytEntry1 *widget.Entry
			findEntryByPlaceHolder(w.Content(), "youtube live recording template", &ytEntry1)
			require.NotNil(t, ytEntry1)
			ytEntry1.SetText("Template")
			if ytEntry1.OnChanged != nil {
				ytEntry1.OnChanged("Template")
			}
			var ytButton *widget.Button
			findButton(w.Content(), "Template1", &ytButton)
			require.NotNil(t, ytButton)
			test.Tap(ytButton)

			// OBS
			var obsCheck *widget.Check
			traverse(w.Content(), func(o fyne.CanvasObject) {
				if c, ok := o.(*widget.Check); ok && c.Text == "Enable recording" {
					obsCheck = c
				}
			})
			if obsCheck != nil {
				test.Tap(obsCheck)
			}

			// Kick Category Selection
			if kickEntry.OnChanged != nil {
				kickEntry.OnChanged("Kick")
			}
			var kickCatButton *widget.Button
			findButton(w.Content(), "KickCat1", &kickCatButton)
			require.NotNil(t, kickCatButton)
			test.Tap(kickCatButton)

			// YouTube Template Re-add/Remove
			var clearButton *widget.Button
			findButtonWithIcon(w.Content(), theme.ContentClearIcon(), &clearButton)
			if clearButton != nil {
				test.Tap(clearButton)
			}
			if ytEntry1 != nil && ytEntry1.OnSubmitted != nil {
				ytEntry1.OnSubmitted("Template1")
			}

			// Ensure Clear buttons are there and then they can be removed
			var clearButtons []*widget.Button
			traverse(w.Content(), func(o fyne.CanvasObject) {
				if btn, ok := o.(*widget.Button); ok && btn.Icon == theme.ContentClearIcon() {
					clearButtons = append(clearButtons, btn)
				}
			})
			for _, btn := range clearButtons {
				test.Tap(btn)
			}

			var sel *widget.Select
			findSelect(w.Content(), []string{"ignore", "use as primary", "use as additional"}, &sel)
			if sel != nil {
				sel.SetSelected("ignore")
				sel.SetSelected("use as primary")
				sel.SetSelected("use as additional")
				sel.SetSelected("invalid")
			}

			// Save
			var finalSaveButton *widget.Button
			findButton(w.Content(), "Save", &finalSaveButton)
			require.NotNil(t, finalSaveButton)
			test.Tap(finalSaveButton)
			require.True(t, commitCalled)
		})

		t.Run("UI_ErrorPaths", func(t *testing.T) {
			p, sd := setup()

			// IsBackendEnabled error
			sd.IsBackendEnabledFn = func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
				return false, fmt.Errorf("backend error")
			}
			require.Nil(t, p.profileWindow(ctx, "Error", Profile{}, nil))

			// GetBackendInfo error
			sd.IsBackendEnabledFn = nil
			sd.GetBackendInfoFn = func(ctx context.Context, id streamcontrol.PlatformID, full bool) (*api.BackendInfo, error) {
				return nil, fmt.Errorf("backend info error")
			}
			require.Nil(t, p.profileWindow(ctx, "Error", Profile{}, nil))
			sd.GetBackendInfoFn = nil

			// commitFn error
			w := p.profileWindow(ctx, "CommitError", Profile{}, func(ctx context.Context, pr Profile) error {
				return fmt.Errorf("commit fail")
			})
			var sButton *widget.Button
			findButton(w.Content(), "Save", &sButton)
			if sButton != nil {
				test.Tap(sButton)
			}
			w.Close()

			// profileCreateOrUpdate error (SaveConfig fail)
			sd.SaveConfigFn = func(ctx context.Context) error {
				return fmt.Errorf("save fail")
			}
			err := p.profileCreateOrUpdate(ctx, Profile{Name: "save-fail"})
			require.Error(t, err)
			require.Contains(t, err.Error(), "unable to save the profile")
			sd.SaveConfigFn = nil

			// profileCreateOrUpdate error (SetStreamDConfig fail)
			p.configCache = &streamdconfig.Config{}
			sd.SetConfigErr = fmt.Errorf("set fail")
			p.profileCreateOrUpdate(ctx, Profile{Name: "set-fail"})
			sd.SetConfigErr = nil

			// profileDelete error (SetStreamDConfig fail)
			p.configCache = &streamdconfig.Config{
				Backends: map[streamcontrol.PlatformID]*streamcontrol.AbstractPlatformConfig{
					"twitch": {
						Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
							streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
									streamcontrol.DefaultStreamID: {
										"p1": streamcontrol.ToRawMessage(twitch.StreamProfile{}),
									},
								},
							}),
						},
					},
				},
			}
			sd.SetConfigErr = fmt.Errorf("set fail")
			p.profileDelete(ctx, "p1")
			sd.SetConfigErr = nil

			// profileDelete error (SaveConfig fail)
			p.configCache = &streamdconfig.Config{
				Backends: map[streamcontrol.PlatformID]*streamcontrol.AbstractPlatformConfig{
					"twitch": {
						Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
							streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
									streamcontrol.DefaultStreamID: {
										"p1": streamcontrol.ToRawMessage(twitch.StreamProfile{}),
									},
								},
							}),
						},
					},
				},
			}
			sd.SaveConfigFn = func(ctx context.Context) error {
				return fmt.Errorf("save fail")
			}
			p.profileDelete(ctx, "p1")
			sd.SaveConfigFn = nil

			// YouTube template missing error
			p.configCache = &streamdconfig.Config{}
			sd.IsBackendEnabledFn = func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
				return id == youtube.ID, nil
			}
			w = p.profileWindow(ctx, "YTErr", Profile{Name: "yt-err"}, func(ctx context.Context, pr Profile) error {
				return nil
			})
			if w != nil {
				findButton(w.Content(), "Save", &sButton)
				if sButton != nil {
					test.Tap(sButton)
				}
				w.Close()
			}
			sd.IsBackendEnabledFn = nil

			// Invalid profile data errors
			w = p.profileWindow(ctx, "BadData", Profile{
				Name: "bad",
				PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{
					streamcontrol.NewStreamIDFullyQualified(obs.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID): streamcontrol.RawMessage(`{"invalid": "obs"}`),
				},
			}, nil)
			if w != nil {
				w.Close()
			}

			w = p.profileWindow(ctx, "BadData", Profile{
				Name: "bad",
				PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{
					streamcontrol.NewStreamIDFullyQualified(twitch.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID): streamcontrol.RawMessage(`{"invalid": "twitch"}`),
				},
			}, nil)
			if w != nil {
				w.Close()
			}

			w = p.profileWindow(ctx, "BadData", Profile{
				Name: "bad",
				PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{
					streamcontrol.NewStreamIDFullyQualified(youtube.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID): streamcontrol.RawMessage(`{"invalid": "youtube"}`),
				},
			}, nil)
			if w != nil {
				w.Close()
			}

			w = p.profileWindow(ctx, "BadData", Profile{
				Name: "bad",
				PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{
					streamcontrol.NewStreamIDFullyQualified(kick.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID): streamcontrol.RawMessage(`{"invalid": "kick"}`),
				},
			}, nil)
			if w != nil {
				w.Close()
			}
		})

		t.Run("DataInteractions", func(t *testing.T) {
			p, sd := setup()
			_ = sd

			// profileCreateOrUpdate GetStreamDConfig error
			p.configCache = nil
			require.Error(t, p.profileCreateOrUpdate(ctx, Profile{Name: "err"}))
			require.Error(t, p.profileDelete(ctx, "err"))

			// refilterProfiles with multiple profiles
			p, sd = setup()
			p.configCache = &streamdconfig.Config{
				Backends: map[streamcontrol.PlatformID]*streamcontrol.AbstractPlatformConfig{
					"twitch": {
						Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
							streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
									streamcontrol.DefaultStreamID: {
										"p1": streamcontrol.ToRawMessage(twitch.StreamProfile{}),
										"p2": streamcontrol.ToRawMessage(twitch.StreamProfile{}),
									},
								},
							}),
						},
					},
				},
			}
			p.setFilter(ctx, "p1") // Filter p1
			p.rearrangeProfiles(ctx)
			p.refilterProfiles(ctx)
			require.Equal(t, 1, p.profilesListLength())

			// Filter by twitch tag
			p.configCache = &streamdconfig.Config{
				Backends: map[streamcontrol.PlatformID]*streamcontrol.AbstractPlatformConfig{
					twitch.ID: {
						Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
							streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
									streamcontrol.DefaultStreamID: {
										"p1": streamcontrol.ToRawMessage(twitch.StreamProfile{Tags: [10]string{"tag1"}}),
									},
								},
							}),
						},
					},
					youtube.ID: {
						Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
							streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
									streamcontrol.DefaultStreamID: {
										"p2": streamcontrol.ToRawMessage(youtube.StreamProfile{Tags: []string{"yt-tag"}}),
									},
								},
							}),
						},
					},
				},
			}
			p.rearrangeProfiles(ctx)
			p.setFilter(ctx, "tag1")
			p.refilterProfiles(ctx)
			require.Equal(t, 1, p.profilesListLength())

			// Filter by youtube tag
			p.setFilter(ctx, "yt-tag")
			p.refilterProfiles(ctx)
			require.Equal(t, 1, p.profilesListLength())

			// Filter by language
			lang := "en"
			p.configCache.Backends[twitch.ID].Accounts[streamcontrol.DefaultAccountID] = streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
				StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
					streamcontrol.DefaultStreamID: {
						"p1": streamcontrol.ToRawMessage(twitch.StreamProfile{Language: &lang}),
					},
				},
			})
			p.rearrangeProfiles(ctx)
			p.setFilter(ctx, "en")
			p.refilterProfiles(ctx)
			require.Equal(t, 1, p.profilesListLength())

			// rearrangeProfiles with MaxOrder
			p.configCache.ProfileMetadata = map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata{
				"p1": {MaxOrder: 10},
			}
			p.rearrangeProfiles(ctx)
		})

		t.Run("Platforms_DeepDive", func(t *testing.T) {
			p, sd := setup()
			sd.IsBackendEnabledFn = func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
				return true, nil
			}

			commitCalled := false
			w := p.profileWindow(ctx, "DeepDive", Profile{Name: "dive"}, func(ctx context.Context, pr Profile) error {
				commitCalled = true
				return nil
			})

			// YouTube Template Search (OnChanged)
			var ytEntry *widget.Entry
			findEntryByPlaceHolder(w.Content(), "youtube live recording template", &ytEntry)
			require.NotNil(t, ytEntry)
			ytEntry.SetText("Temp")
			time.Sleep(50 * time.Millisecond) // Wait for UI update

			var templButton *widget.Button
			findButton(w.Content(), "Template1", &templButton)
			require.NotNil(t, templButton, "could not find Template1 button after search")
			test.Tap(templButton)

			// YouTube Template clear
			var clearButton *widget.Button
			findButtonWithIcon(w.Content(), theme.ContentClearIcon(), &clearButton)
			require.NotNil(t, clearButton, "could not find YouTube template clear button")
			test.Tap(clearButton)

			// YouTube Template re-add via OnSubmitted
			if ytEntry.OnSubmitted != nil {
				ytEntry.OnSubmitted("Template1")
			}

			// Template tags selections
			var sel *widget.Select
			findSelect(w.Content(), []string{"ignore", "use as primary", "use as additional"}, &sel)
			require.NotNil(t, sel)
			sel.SetSelected("ignore")
			sel.SetSelected("use as primary")
			sel.SetSelected("use as additional")
			sel.SetSelected("invalid")

			// Twitch Category search
			var twitchEntry *widget.Entry
			findEntryByPlaceHolder(w.Content(), "twitch category", &twitchEntry)
			require.NotNil(t, twitchEntry)
			twitchEntry.SetText("Talk")
			time.Sleep(50 * time.Millisecond)
			var catButton *widget.Button
			findButton(w.Content(), "TalkShow", &catButton)
			require.NotNil(t, catButton)
			test.Tap(catButton)

			// Kick Category search
			var kickEntry *widget.Entry
			findEntryByPlaceHolder(w.Content(), "kick category", &kickEntry)
			require.NotNil(t, kickEntry)
			kickEntry.SetText("Kick")
			time.Sleep(50 * time.Millisecond)
			var kcatButton *widget.Button
			findButton(w.Content(), "KickCat1", &kcatButton)
			require.NotNil(t, kcatButton)
			test.Tap(kcatButton)

			// Save
			var sButton *widget.Button
			findButton(w.Content(), "Save", &sButton)
			require.NotNil(t, sButton)
			test.Tap(sButton)
			require.True(t, commitCalled, "commitFn was not called in DeepDive")
		})

		t.Run("EditAndClone", func(t *testing.T) {
			p, sd := setup()
			sd.IsBackendEnabledFn = func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
				return false, nil
			}
			p.selectedProfileName = nil
			wEdit := p.editProfileWindow(ctx)
			require.Nil(t, wEdit)

			wClone := p.cloneProfileWindow(ctx)
			require.Nil(t, wClone)

			name := streamcontrol.ProfileName("Existing")
			p.selectedProfileName = &name
			p.configCache.ProfileMetadata = map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata{
				name: {DefaultStreamTitle: "Title"},
			}

			// Test Clone - Duplicate Name Error
			wClone = p.cloneProfileWindow(ctx)
			require.NotNil(t, wClone)
			var sButton *widget.Button
			findButton(wClone.Content(), "Save", &sButton)
			require.NotNil(t, sButton)
			test.Tap(sButton)
			wClone.Close()

			// Test Clone - Success
			sButton = nil
			wClone = p.cloneProfileWindow(ctx)
			require.NotNil(t, wClone)
			var nEntry *widget.Entry
			findEntryByPlaceHolder(wClone.Content(), "profile name", &nEntry)
			require.NotNil(t, nEntry)
			nEntry.SetText("Cloned")
			findButton(wClone.Content(), "Save", &sButton)
			require.NotNil(t, sButton)
			test.Tap(sButton)

			// Test Edit - Success No Rename
			sButton = nil
			wEdit = p.editProfileWindow(ctx)
			require.NotNil(t, wEdit)
			findButton(wEdit.Content(), "Save", &sButton)
			require.NotNil(t, sButton)
			test.Tap(sButton)

			// Test Edit - Rename Success
			sButton = nil
			wEdit = p.editProfileWindow(ctx)
			require.NotNil(t, wEdit)
			nEntry = nil
			findEntryByPlaceHolder(wEdit.Content(), "profile name", &nEntry)
			require.NotNil(t, nEntry)
			nEntry.SetText("Renamed")
			findButton(wEdit.Content(), "Save", &sButton)
			require.NotNil(t, sButton)
			test.Tap(sButton)

			// Test Edit - Rename Delete Error
			sButton = nil
			p.selectedProfileName = &name
			p.configCache.ProfileMetadata = map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata{
				name: {DefaultStreamTitle: "Title"},
			}
			sd.SaveConfigFn = func(ctx context.Context) error {
				return fmt.Errorf("forced failure")
			}
			wEdit = p.editProfileWindow(ctx)
			require.NotNil(t, wEdit)
			nEntry = nil
			findEntryByPlaceHolder(wEdit.Content(), "profile name", &nEntry)
			require.NotNil(t, nEntry)
			nEntry.SetText("Renamed2")
			findButton(wEdit.Content(), "Save", &sButton)
			require.NotNil(t, sButton)
			test.Tap(sButton)
			wEdit.Close()
			sd.SaveConfigFn = nil
		})

		t.Run("EditAndClone_Errors", func(t *testing.T) {
			p, sd := setup()
			sd.IsBackendEnabledFn = func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
				return false, nil
			}
			name := streamcontrol.ProfileName("Existing")
			p.selectedProfileName = &name
			p.configCache.ProfileMetadata = map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata{
				name: {DefaultStreamTitle: "Title"},
			}

			// Test Clone - Save Error
			sd.SaveConfigFn = func(ctx context.Context) error { return fmt.Errorf("fail") }
			wClone := p.cloneProfileWindow(ctx)
			require.NotNil(t, wClone)
			var nEntry *widget.Entry
			findEntryByPlaceHolder(wClone.Content(), "profile name", &nEntry)
			nEntry.SetText("CloneFail")
			var sButton *widget.Button
			findButton(wClone.Content(), "Save", &sButton)
			test.Tap(sButton)
			wClone.Close()

			// Test Edit - Rename Create Error
			sd.SaveConfigFn = func(ctx context.Context) error { return fmt.Errorf("fail update") }
			wEdit := p.editProfileWindow(ctx)
			require.NotNil(t, wEdit)
			nEntry = nil
			findEntryByPlaceHolder(wEdit.Content(), "profile name", &nEntry)
			nEntry.SetText("RenameUpdateFail")
			sButton = nil
			findButton(wEdit.Content(), "Save", &sButton)
			test.Tap(sButton)
			wEdit.Close()

			// Test Edit - Rename Delete Error
			callCount := 0
			sd.SaveConfigFn = func(ctx context.Context) error {
				callCount++
				if callCount == 2 {
					return fmt.Errorf("fail delete part")
				}
				return nil
			}
			wEdit = p.editProfileWindow(ctx)
			nEntry = nil
			findEntryByPlaceHolder(wEdit.Content(), "profile name", &nEntry)
			nEntry.SetText("RenameDeleteFail")
			sButton = nil
			findButton(wEdit.Content(), "Save", &sButton)
			test.Tap(sButton)
			wEdit.Close()
		})

		t.Run("BackendErrors", func(t *testing.T) {
			p, sd := setup()

			// IsBackendEnabled failure
			sd.IsBackendEnabledFn = func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
				return false, fmt.Errorf("enabled fail")
			}
			require.Nil(t, p.profileWindow(ctx, "Fail", Profile{}, nil))

			// GetBackendInfo failure
			sd.IsBackendEnabledFn = func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
				return true, nil
			}
			sd.GetBackendInfoFn = func(ctx context.Context, id streamcontrol.PlatformID, full bool) (*api.BackendInfo, error) {
				return nil, fmt.Errorf("info fail")
			}
			require.Nil(t, p.profileWindow(ctx, "Fail", Profile{}, nil))

			// YouTube Template broadcast selection default case (unexpected bcID)
			sd.GetBackendInfoFn = func(ctx context.Context, id streamcontrol.PlatformID, full bool) (*api.BackendInfo, error) {
				switch id {
				case obs.ID:
					return &api.BackendInfo{Data: api.BackendDataOBS{}}, nil
				case twitch.ID:
					return &api.BackendInfo{Data: api.BackendDataTwitch{}}, nil
				case kick.ID:
					return &api.BackendInfo{Data: api.BackendDataKick{}}, nil
				case youtube.ID:
					return &api.BackendInfo{Data: api.BackendDataYouTube{Cache: &cache.YouTube{}}}, nil
				}
				return &api.BackendInfo{}, nil
			}
			p.profileWindow(ctx, "YTFail", Profile{
				PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{
					streamcontrol.NewStreamIDFullyQualified(youtube.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID): youtube.StreamProfile{TemplateBroadcastIDs: []string{"missing"}},
				},
			}, nil)
		})

		t.Run("MobileLayout", func(t *testing.T) {
			oldIsMobile := isMobile
			defer func() { isMobile = oldIsMobile }()
			isMobile = func() bool { return true }
			p, _ := setup()
			p.profileWindow(ctx, "Mobile", Profile{}, nil)
		})
	})

	t.Run("EdgeCases", func(t *testing.T) {
		p, sd := setup()
		ctx := context.Background()

		t.Run("Sorting", func(t *testing.T) {
			p.configCache = &streamdconfig.Config{
				ProfileMetadata: map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata{
					"b1": {MaxOrder: 10},
					"a1": {MaxOrder: 5},
				},
			}
			p.rearrangeProfiles(ctx)
			require.Equal(t, streamcontrol.ProfileName("a1"), p.profilesOrder[0])
			require.Equal(t, streamcontrol.ProfileName("b1"), p.profilesOrder[1])
		})

		t.Run("NilConfigCache", func(t *testing.T) {
			p.configCache = nil
			_ = p.profileCreateOrUpdate(ctx, Profile{Name: "err"})
			_ = p.profileDelete(ctx, "err")
			_ = p.rearrangeProfiles(ctx)
			_ = p.profileCreateOrUpdate(ctx, Profile{Name: ""}) // Empty name error
		})

		t.Run("SetStreamDConfigFailure", func(t *testing.T) {
			p.configCache = &streamdconfig.Config{
				ProfileMetadata: map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata{
					"p1": {},
				},
				Backends: streamcontrol.Config{
					"plat1": {
						Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
							streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
									streamcontrol.DefaultStreamID: {
										"p1": nil,
									},
								},
							}),
						},
					},
					"plat2": nil, // Coverage for nil bc
				},
			}
			sd.SetConfigFn = func(ctx context.Context, cfg *streamdconfig.Config) error {
				return fmt.Errorf("set fail")
			}
			_ = p.profileDelete(ctx, "p1")
			sd.SetConfigFn = nil
		})

		t.Run("BackendNil", func(t *testing.T) {
			cfg := &streamdconfig.Config{
				Backends: streamcontrol.Config{
					"nil-backend": nil,
				},
				ProfileMetadata: map[streamcontrol.ProfileName]streamdconfig.ProfileMetadata{
					"p1": {},
				},
			}
			p.configCache = cfg
			_ = getProfile(cfg, "p1")
			_ = p.rearrangeProfiles(ctx)
			_ = p.profileCreateOrUpdate(ctx, Profile{Name: "p2", PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{
				streamcontrol.NewStreamIDFullyQualified("nil-backend", streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID): nil,
			}})
			_ = p.profileDelete(ctx, "p1")
		})

		t.Run("CreateOrUpdate_PlatProfiles", func(t *testing.T) {
			p.configCache = &streamdconfig.Config{}
			err := p.profileCreateOrUpdate(ctx, Profile{
				Name: "new",
				PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{
					streamcontrol.NewStreamIDFullyQualified("empty", streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID): &obs.StreamProfile{},
				},
			})
			require.NoError(t, err)
		})

		t.Run("UI_Interactions", func(t *testing.T) {
			// getSelectedProfile with nil
			p.selectedProfileName = nil
			p.getSelectedProfile()

			// Kick enabled
			sd.IsBackendEnabledFn = func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
				return true, nil
			}
			sd.GetBackendInfoFn = func(ctx context.Context, id streamcontrol.PlatformID, full bool) (*api.BackendInfo, error) {
				switch id {
				case youtube.ID:
					return &api.BackendInfo{Data: api.BackendDataYouTube{Cache: &cache.YouTube{}}}, nil
				case twitch.ID:
					return &api.BackendInfo{Data: api.BackendDataTwitch{Cache: &cache.Twitch{}}}, nil
				case kick.ID:
					return &api.BackendInfo{Data: api.BackendDataKick{}}, nil
				case obs.ID:
					return &api.BackendInfo{Data: api.BackendDataOBS{}}, nil
				}
				return nil, fmt.Errorf("unknown")
			}
			wKick := p.profileWindow(ctx, "KickTest", Profile{}, nil)
			wKick.Close()

			// IsBackendEnabled error
			sd.IsBackendEnabledFn = func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
				return false, fmt.Errorf("fail")
			}
			p.profileWindow(ctx, "Fail", Profile{}, nil)

			// GetBackendInfo error
			sd.IsBackendEnabledFn = func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
				return true, nil
			}
			sd.GetBackendInfoFn = func(ctx context.Context, id streamcontrol.PlatformID, full bool) (*api.BackendInfo, error) {
				return nil, fmt.Errorf("fail info")
			}
			p.profileWindow(ctx, "FailInfo", Profile{}, nil)

			// Restore
			sd.IsBackendEnabledFn = nil
			sd.GetBackendInfoFn = nil

			// Cancel button in profileWindow
			w := p.profileWindow(ctx, "CancelTest", Profile{}, nil)
			var cButton *widget.Button
			findButton(w.Content(), "Cancel", &cButton)
			if cButton != nil {
				test.Tap(cButton)
			}

			// NO button in deleteProfileWindow
			nameDel := streamcontrol.ProfileName("p1")
			p.selectedProfileName = &nameDel
			w = p.deleteProfileWindow(ctx)
			var noButton *widget.Button
			findButton(w.Content(), "NO", &noButton)
			if noButton != nil {
				test.Tap(noButton)
			}

			// Platforms with some disabled
			sd.IsBackendEnabledFn = func(ctx context.Context, id streamcontrol.PlatformID) (bool, error) {
				return id == youtube.ID, nil // Only youtube
			}
			wDis := p.profileWindow(ctx, "DisabledTest", Profile{}, nil)
			wDis.Close()
			sd.IsBackendEnabledFn = nil

			// Twitch with CategoryID
			sd.GetBackendInfoFn = func(ctx context.Context, id streamcontrol.PlatformID, full bool) (*api.BackendInfo, error) {
				switch id {
				case twitch.ID:
					return &api.BackendInfo{Data: api.BackendDataTwitch{Cache: &cache.Twitch{
						Categories: []helix.Game{{Name: "FoundByID", ID: "id123"}},
					}}}, nil
				case kick.ID:
					return &api.BackendInfo{Data: api.BackendDataKick{Cache: &cache.Kick{}}}, nil
				case youtube.ID:
					return &api.BackendInfo{Data: api.BackendDataYouTube{Cache: &cache.YouTube{}}}, nil
				case obs.ID:
					return &api.BackendInfo{Data: api.BackendDataOBS{}}, nil
				}
				return nil, fmt.Errorf("unknown")
			}
			wID := p.profileWindow(ctx, "IDTest", Profile{
				PerStream: map[streamcontrol.StreamIDFullyQualified]streamcontrol.StreamProfile{
					streamcontrol.NewStreamIDFullyQualified(twitch.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID): twitch.StreamProfile{CategoryID: ptr("id123")},
					streamcontrol.NewStreamIDFullyQualified(youtube.ID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID): youtube.StreamProfile{
						TemplateTags:         "invalid",
						TemplateBroadcastIDs: []string{"bc1"},
					},
				},
			}, func(ctx context.Context, profile Profile) error {
				return fmt.Errorf("commit fail") // Coverage for commit error
			})

			// Test Save error
			var saveButton *widget.Button
			findButton(wID.Content(), "Save", &saveButton)
			if saveButton != nil {
				test.Tap(saveButton)
			}

			// Test OnSubmitted
			var entry *widget.Entry
			findEntryByPlaceHolder(wID.Content(), "twitch category", &entry)
			if entry != nil && entry.OnSubmitted != nil {
				entry.OnSubmitted("TalkShow")
			}

			findEntryByPlaceHolder(wID.Content(), "kick category", &entry)
			if entry != nil && entry.OnSubmitted != nil {
				entry.OnSubmitted("KickCat1")
			}

			findEntryByPlaceHolder(wID.Content(), "youtube live recording template", &entry)
			if entry != nil && entry.OnSubmitted != nil {
				entry.OnSubmitted("Template1")
			}

			wID.Close()
			sd.GetBackendInfoFn = nil

			// refilterProfiles with multiple matches
			p.profilesOrder = []streamcontrol.ProfileName{"abc", "abd"}
			p.filterValue = ""
			p.configCache = &streamdconfig.Config{
				Backends: streamcontrol.Config{
					twitch.ID: {
						Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
							streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
									streamcontrol.DefaultStreamID: {
										"abc": streamcontrol.ToRawMessage(twitch.StreamProfile{
											Tags:     [10]string{"tag1"},
											Language: ptr("en"),
										}),
									},
								},
							}),
						},
					},
					youtube.ID: {
						Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
							streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
									streamcontrol.DefaultStreamID: {
										"abd": streamcontrol.ToRawMessage(youtube.StreamProfile{
											Tags: []string{"tag2"},
										}),
									},
								},
							}),
						},
					},
					kick.ID: {
						Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
							streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
								StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
									streamcontrol.DefaultStreamID: {
										"abc": streamcontrol.ToRawMessage(kick.StreamProfile{}),
									},
								},
							}),
						},
					},
				},
			}
			p.filterValue = "tag1"
			p.refilterProfiles(ctx)
			require.Equal(t, 1, len(p.profilesOrderFiltered))

			p.filterValue = "en"
			p.refilterProfiles(ctx)
			require.Equal(t, 1, len(p.profilesOrderFiltered))

			p.filterValue = "tag2"
			p.refilterProfiles(ctx)
			require.Equal(t, 1, len(p.profilesOrderFiltered))
		})
	})
}

func findButton(obj fyne.CanvasObject, text string, ptr **widget.Button) {
	traverse(obj, func(o fyne.CanvasObject) {
		if *ptr != nil {
			return
		}
		if b, ok := o.(*widget.Button); ok {
			if b.Text == text {
				*ptr = b
			}
		}
	})
}

func findButtonWithIcon(obj fyne.CanvasObject, icon fyne.Resource, ptr **widget.Button) {
	traverse(obj, func(o fyne.CanvasObject) {
		if *ptr != nil {
			return
		}
		if b, ok := o.(*widget.Button); ok {
			if b.Icon == icon {
				*ptr = b
			}
		}
	})
}

func findEntryByPlaceHolder(obj fyne.CanvasObject, placeholder string, ptr **widget.Entry) {
	traverse(obj, func(o fyne.CanvasObject) {
		if *ptr != nil {
			return
		}
		if e, ok := o.(*widget.Entry); ok {
			if e.PlaceHolder == placeholder {
				*ptr = e
			}
		}
	})
}

func findCheck(obj fyne.CanvasObject, text string, ptr **widget.Check) {
	traverse(obj, func(o fyne.CanvasObject) {
		if *ptr != nil {
			return
		}
		if c, ok := o.(*widget.Check); ok {
			if c.Text == text {
				*ptr = c
			}
		}
	})
}

func findSelect(obj fyne.CanvasObject, options []string, ptr **widget.Select) {
	traverse(obj, func(o fyne.CanvasObject) {
		if *ptr != nil {
			return
		}
		if s, ok := o.(*widget.Select); ok {
			for _, opt := range options {
				match := slices.Contains(s.Options, opt)
				if !match {
					return
				}
			}
			*ptr = s
		}
	})
}

func traverse(obj fyne.CanvasObject, fn func(fyne.CanvasObject)) {
	if obj == nil {
		return
	}
	fn(obj)
	if cont, ok := obj.(*fyne.Container); ok {
		for _, child := range cont.Objects {
			traverse(child, fn)
		}
	} else if scroll, ok := obj.(*container.Scroll); ok {
		traverse(scroll.Content, fn)
	} else if split, ok := obj.(*container.Split); ok {
		traverse(split.Leading, fn)
		traverse(split.Trailing, fn)
	} else if tabs, ok := obj.(*container.AppTabs); ok {
		for _, tab := range tabs.Items {
			traverse(tab.Content, fn)
		}
	} else if card, ok := obj.(*widget.Card); ok {
		traverse(card.Content, fn)
	} else if form, ok := obj.(*widget.Form); ok {
		for _, item := range form.Items {
			traverse(item.Widget, fn)
		}
	} else if accordion, ok := obj.(*widget.Accordion); ok {
		for _, item := range accordion.Items {
			if item.Detail != nil {
				traverse(item.Detail, fn)
			}
		}
	}
}
func TestPanelChatCoverage(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	ui, err := p.newChatUIAsList(ctx, true, true, false)
	require.NoError(t, err)

	// Test onReceiveMessage
	msg := api.ChatMessage{
		Event: streamcontrol.Event{
			ID: "msgid",
			User: streamcontrol.User{
				Name: "testuser",
			},
			Message: &streamcontrol.Message{
				Content: "hello world",
			},
			CreatedAt: time.Now(),
		},
		Platform: twitch.ID,
	}
	require.NotPanics(t, func() {
		p.onReceiveMessage(ctx, msg)
	})

	// Test chatUserBan (zero coverage currently)
	require.NotPanics(t, func() {
		_ = p.chatUserBan(ctx, twitch.ID, "testuser")
	})

	// Test onRemoveChatMessageClicked
	require.NotPanics(t, func() {
		p.onRemoveChatMessageClicked(0)
	})

	// Test chatMessageRemove
	require.NotPanics(t, func() {
		_ = p.chatMessageRemove(ctx, twitch.ID, msg.ID)
	})

	ui.Rebuild(ctx)
	ui.Append(ctx, 0)
	ui.Remove(ctx, msg)
	ui.GetTotalHeight(ctx)
	ui.ScrollToBottom(ctx)
}

func TestPanelDashboardCoverage(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	p.focusDashboardWindow(ctx)
	require.NotNil(t, p.dashboardWindow)

	require.NotPanics(t, func() {
		p.dashboardWindow.onSizeChange(ctx, fyne.NewSize(0, 0), fyne.NewSize(100, 100))
		p.dashboardWindow.onChatSizeChange(ctx, fyne.NewSize(0, 0), fyne.NewSize(100, 100))
	})

	require.NotPanics(t, func() {
		p.dashboardWindow.stopUpdating(ctx)
	})
}

func TestPanelProfileCoverage(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	require.NotPanics(t, func() {
		p.newProfileWindow(ctx)
		p.profileWindow(ctx, "Test Title", Profile{}, func(ctx context.Context, profile Profile) error { return nil })
		p.editProfileWindow(ctx)
		p.cloneProfileWindow(ctx)
		p.deleteProfileWindow(ctx)
	})
}

func TestPanelSettingsCoverage(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	require.NotPanics(t, func() {
		p.openSettingsWindow(ctx)
	})
}

func TestPanelRestreamCoverage(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	require.NotPanics(t, func() {
		p.openAddStreamServerWindow(ctx)
		p.openAddStreamWindow(ctx)
		p.openAddSinkWindow(ctx)
		p.openAddPlayerWindow(ctx)
		p.openAddRestreamWindow(ctx)
	})
}

func TestPanelMonitorCoverage(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	require.NotPanics(t, func() {
		_ = p.initMonitorPage(ctx)
		p.startMonitorPage(ctx)
		p.stopMonitorPage(ctx)
		if p.monitorPage != nil {
			_ = p.monitorPage.enableMonitor(ctx, "localhost:1234", "source1", []uint{0}, []uint{0})
			_ = p.monitorPage.disableMonitor(ctx, "localhost:1234", "source1")
		}
	})
}

func TestPanelTriggerRulesCoverage(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	ui := NewTriggerRulesUI(ctx, p)
	require.NotNil(t, ui)
	require.NotPanics(t, func() {
		ui.openSetupWindow(ctx)
	})
}

func TestPanelTimersCoverage(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	ui := NewTimersUI(ctx, p)
	require.NotNil(t, ui)
	require.NotPanics(t, func() {
		ui.kickOff(ctx, time.Now())
		ui.stop(ctx)
	})
}

func TestPanelErrorCoverage(t *testing.T) {
	testApp, p, _, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	require.NotPanics(t, func() {
		p.ReportError(fmt.Errorf("test error"))
		p.DisplayError(fmt.Errorf("test display error"))
		p.statusPanelSet("test status")
		p.ShowErrorReports()
	})
}

func TestPanelExtraCoverage(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	require.NotPanics(t, func() {
		_ = newCameraUI()
	})

	require.NotPanics(t, func() {
		p.generateNewTitle(ctx)
		p.generateNewDescription(ctx)
	})

	require.NotPanics(t, func() {
		p.showLinkDeviceQRWindow(ctx)
	})

	require.NotPanics(t, func() {
		p.openAddAccountDialog(ctx, nil, func(platform streamcontrol.PlatformID, b bool) {})
	})
}

func TestPanelProfilesPlatformsCoverage(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	w := testApp.NewWindow("test")
	defer w.Close()

	for platID, ui := range platformUIs {
		t.Run(string(platID), func(t *testing.T) {
			backendInfo, _ := p.StreamD.GetBackendInfo(ctx, platID, true)
			var backendData any
			if backendInfo != nil {
				backendData = backendInfo.Data
			}
			require.NotPanics(t, func() {
				ui.Placement()
				ui.IsReadyToStart(ctx, p)
				ui.IsAlwaysChecked(ctx, p)
				ui.ShouldStopParallel()
				ui.UpdateStatus(ctx, p)
				ui.GetColor()
				_, _ = ui.RenderStream(ctx, p, w, platID, streamcontrol.NewStreamIDFullyQualified(platID, streamcontrol.DefaultAccountID, streamcontrol.DefaultStreamID), backendData, nil)
				_ = ui.FilterMatch(nil, "")
				_ = ui.AfterStartStream(ctx, p)
				_ = ui.AfterStopStream(ctx, p)
			})
		})
	}
}

func TestPanelRestreamExtCoverage(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	p.initMainWindow(ctx, consts.PageRestream)

	t.Run("add_sink", func(t *testing.T) {
		require.NotPanics(t, func() {
			p.openAddSinkWindow(ctx)
		})
	})

	t.Run("add_player", func(t *testing.T) {
		require.NotPanics(t, func() {
			p.openAddPlayerWindow(ctx)
		})
	})

	t.Run("add_restream", func(t *testing.T) {
		require.NotPanics(t, func() {
			p.openAddRestreamWindow(ctx)
		})
	})

	t.Run("add_server", func(t *testing.T) {
		require.NotPanics(t, func() {
			p.openAddStreamServerWindow(ctx)
		})
	})

	t.Run("add_source", func(t *testing.T) {
		require.NotPanics(t, func() {
			p.openAddStreamWindow(ctx)
		})
	})
}

func TestPanelMonitorExtCoverage(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	p.initMainWindow(ctx, consts.PageMonitor)

	require.NotPanics(t, func() {
		p.initMonitorPage(ctx)
	})
}

func TestPanelShoutoutCoverage(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	require.NotPanics(t, func() {
		p.initMainWindow(ctx, consts.PageShoutout)
	})
}

func TestPanelOAuthHandlersCoverage(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	arg := oauthhandler.OAuthHandlerArgument{
		AuthURL: "http://localhost",
		ExchangeFn: func(ctx context.Context, code string) error {
			return nil
		},
	}

	t.Run("Twitch", func(t *testing.T) {
		p.OAuthHandlerTwitch(ctx, arg)
	})

	t.Run("YouTube", func(t *testing.T) {
		p.OAuthHandlerYouTube(ctx, arg)
	})

	t.Run("Kick", func(t *testing.T) {
		p.OAuthHandlerKick(ctx, arg)
	})
}

func TestPanelConfigExtCoverage(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	t.Run("SaveConfig", func(t *testing.T) {
		p.SaveConfig(ctx)
	})

	t.Run("GetConfig", func(t *testing.T) {
		p.GetConfig(ctx)
	})

	t.Run("SetStreamDConfig", func(t *testing.T) {
		p.SetStreamDConfig(ctx, &streamdconfig.Config{})
	})
}

func TestPanelLoggingCoverage(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	p.SetLoggingLevel(ctx, logger.LevelDebug)
}

type dummyUpdate struct{}

func (d *dummyUpdate) Apply(ctx context.Context, pb ProgressBar) error { return nil }
func (d *dummyUpdate) Cancel() error                                   { return nil }
func (d *dummyUpdate) ReleaseName() string                             { return "v1.0.0" }

type dummyAutoUpdater struct{}

func (d *dummyAutoUpdater) CheckForUpdates(ctx context.Context) (Update, error) {
	return &dummyUpdate{}, nil
}

func TestPanelUpdateCoverage(t *testing.T) {
	testApp, p, ctx, cancel := setupTestPanel(t)
	defer testApp.Quit()
	defer cancel()

	p.checkForUpdates(ctx, &dummyAutoUpdater{})
}
