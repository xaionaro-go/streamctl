package streamplayer

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	player "github.com/xaionaro-go/player/pkg/player/types"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

// mockPlayer implements player.Player
type mockPlayer struct {
	lock     sync.Mutex
	pos      time.Duration
	speed    float64
	isPaused bool
	closed   bool
	endChan  chan struct{}
}

func (m *mockPlayer) ProcessTitle(ctx context.Context) (string, error)     { return "mock", nil }
func (m *mockPlayer) OpenURL(ctx context.Context, link string) error       { return nil }
func (m *mockPlayer) GetLink(ctx context.Context) (string, error)          { return "http://mock", nil }
func (m *mockPlayer) EndChan(ctx context.Context) (<-chan struct{}, error) { return m.endChan, nil }
func (m *mockPlayer) IsEnded(ctx context.Context) (bool, error)            { return false, nil }
func (m *mockPlayer) GetPosition(ctx context.Context) (time.Duration, error) {
	m.lock.Lock()
	defer m.lock.Unlock()
	return m.pos, nil
}
func (m *mockPlayer) GetAudioPosition(ctx context.Context) (time.Duration, error) { return m.pos, nil }
func (m *mockPlayer) GetLength(ctx context.Context) (time.Duration, error)        { return 0, nil }
func (m *mockPlayer) GetSpeed(ctx context.Context) (float64, error) {
	m.lock.Lock()
	defer m.lock.Unlock()
	return m.speed, nil
}
func (m *mockPlayer) SetSpeed(ctx context.Context, s float64) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.speed = s
	return nil
}
func (m *mockPlayer) GetPause(ctx context.Context) (bool, error) {
	m.lock.Lock()
	defer m.lock.Unlock()
	return m.isPaused, nil
}
func (m *mockPlayer) SetPause(ctx context.Context, p bool) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.isPaused = p
	return nil
}
func (m *mockPlayer) Seek(ctx context.Context, pos time.Duration, isRelative bool, quick bool) error {
	return nil
}
func (m *mockPlayer) GetVideoTracks(ctx context.Context) (player.VideoTracks, error) { return nil, nil }
func (m *mockPlayer) GetAudioTracks(ctx context.Context) (player.AudioTracks, error) { return nil, nil }
func (m *mockPlayer) GetSubtitlesTracks(ctx context.Context) (player.SubtitlesTracks, error) {
	return nil, nil
}
func (m *mockPlayer) SetVideoTrack(ctx context.Context, vid int64) error     { return nil }
func (m *mockPlayer) SetAudioTrack(ctx context.Context, aid int64) error     { return nil }
func (m *mockPlayer) SetSubtitlesTrack(ctx context.Context, sid int64) error { return nil }
func (m *mockPlayer) Stop(ctx context.Context) error                         { return nil }
func (m *mockPlayer) Close(ctx context.Context) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.closed = true
	return nil
}
func (m *mockPlayer) SetupForStreaming(ctx context.Context) error { return nil }

// mockStreamServer implements StreamServer
type mockStreamServer struct {
	publisherChan chan Publisher
}

func (m *mockStreamServer) WaitPublisherChan(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
	waitForNext bool,
) (<-chan Publisher, error) {
	return m.publisherChan, nil
}
func (m *mockStreamServer) GetPortServers(ctx context.Context) ([]streamportserver.Config, error) {
	return nil, nil
}

// mockPublisher implements Publisher
type mockPublisher struct {
	closedChan chan struct{}
}

func (m *mockPublisher) ClosedChan() <-chan struct{} {
	return m.closedChan
}

// mockPlayerManager implements PlayerManager
type mockPlayerManager struct {
	lock    sync.Mutex
	players []*mockPlayer
}

func (m *mockPlayerManager) SupportedBackends() []player.Backend {
	return []player.Backend{"dummy"}
}

func (m *mockPlayerManager) NewPlayer(
	ctx context.Context,
	title string,
	backend player.Backend,
	opts ...player.Option,
) (player.Player, error) {
	m.lock.Lock()
	defer m.lock.Unlock()
	p := &mockPlayer{speed: 1.0, endChan: make(chan struct{})}
	m.players = append(m.players, p)
	return p, nil
}

func TestStreamPlayerLifecycleAndJitter(t *testing.T) {
	server := &mockStreamServer{publisherChan: make(chan Publisher, 10)}
	mgr := &mockPlayerManager{}

	sp := New(server, mgr)

	t.Run("CreateAndStatus", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		h, err := sp.Create(ctx, "stream1", "dummy",
			types.OptionCatchupMaxSpeedFactor(1.5),
			types.OptionJitterBufMaxDuration(time.Second),
			types.OptionMaxCatchupAtLag(2*time.Second),
		)
		require.NoError(t, err)
		assert.NotNil(t, h)
		assert.NotNil(t, h.WantSpeedAverage)

		h.Close()
	})
}

func TestStreamPlayerCollections(t *testing.T) {
	server := &mockStreamServer{publisherChan: make(chan Publisher)}
	mgr := &mockPlayerManager{}
	sp := New(server, mgr)

	assert.Empty(t, sp.GetAll())
}
