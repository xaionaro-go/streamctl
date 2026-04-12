package streampanel

import (
	"context"
	"sync"
	"testing"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
)

type mockOBSServer struct {
	obs_grpc.UnimplementedOBSServer

	mu                       sync.Mutex
	sceneItems               []*obs_grpc.SceneItem
	setEnabledCalls          []setEnabledCall
	getSceneItemListErr      error
	setSceneItemEnabledErr   error
}

type setEnabledCall struct {
	SceneName        string
	SceneItemID      int64
	SceneItemEnabled bool
}

func (m *mockOBSServer) GetSceneItemList(
	_ context.Context,
	req *obs_grpc.GetSceneItemListRequest,
) (*obs_grpc.GetSceneItemListResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.getSceneItemListErr != nil {
		return nil, m.getSceneItemListErr
	}
	return &obs_grpc.GetSceneItemListResponse{
		SceneItems: m.sceneItems,
	}, nil
}

func (m *mockOBSServer) SetSceneItemEnabled(
	_ context.Context,
	req *obs_grpc.SetSceneItemEnabledRequest,
) (*obs_grpc.SetSceneItemEnabledResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.setSceneItemEnabledErr != nil {
		return nil, m.setSceneItemEnabledErr
	}
	var sceneName string
	if req.SceneName != nil {
		sceneName = *req.SceneName
	}
	m.setEnabledCalls = append(m.setEnabledCalls, setEnabledCall{
		SceneName:        sceneName,
		SceneItemID:      req.SceneItemID,
		SceneItemEnabled: req.SceneItemEnabled,
	})
	return &obs_grpc.SetSceneItemEnabledResponse{}, nil
}

func newTestPanel(t *testing.T) *Panel {
	t.Helper()
	app := test.NewApp()
	t.Cleanup(app.Quit)
	p := &Panel{}
	p.obsSceneItemsContainer = container.NewVBox()
	return p
}

func TestRefreshOBSSceneItems_PopulatesContainer(t *testing.T) {
	ctx := context.Background()
	p := newTestPanel(t)

	mock := &mockOBSServer{
		sceneItems: []*obs_grpc.SceneItem{
			{SourceName: "Camera", SceneItemID: 1, SceneItemEnabled: true},
			{SourceName: "Music - Chill", SceneItemID: 2, SceneItemEnabled: false},
			{SourceName: "Overlay", SceneItemID: 3, SceneItemEnabled: true},
		},
	}

	p.refreshOBSSceneItems(ctx, mock, "Main Scene")

	objects := p.obsSceneItemsContainer.Objects
	require.Len(t, objects, 3, "expected 3 scene item checks")

	// Verify each check widget has correct name and state.
	check0 := objects[0].(*widget.Check)
	testifyassert.Equal(t, "Camera", check0.Text)
	testifyassert.True(t, check0.Checked, "Camera should be enabled")

	check1 := objects[1].(*widget.Check)
	testifyassert.Equal(t, "Music - Chill", check1.Text)
	testifyassert.False(t, check1.Checked, "Music - Chill should be disabled")

	check2 := objects[2].(*widget.Check)
	testifyassert.Equal(t, "Overlay", check2.Text)
	testifyassert.True(t, check2.Checked, "Overlay should be enabled")
}

func TestRefreshOBSSceneItems_EmptyScene(t *testing.T) {
	ctx := context.Background()
	p := newTestPanel(t)

	mock := &mockOBSServer{
		sceneItems: nil,
	}

	p.refreshOBSSceneItems(ctx, mock, "Empty Scene")

	testifyassert.Empty(t, p.obsSceneItemsContainer.Objects, "expected no checks for empty scene")
}

func TestRefreshOBSSceneItems_ReplacesOldItems(t *testing.T) {
	ctx := context.Background()
	p := newTestPanel(t)

	// First refresh: 2 items.
	mock := &mockOBSServer{
		sceneItems: []*obs_grpc.SceneItem{
			{SourceName: "A", SceneItemID: 1, SceneItemEnabled: true},
			{SourceName: "B", SceneItemID: 2, SceneItemEnabled: false},
		},
	}
	p.refreshOBSSceneItems(ctx, mock, "Scene1")
	require.Len(t, p.obsSceneItemsContainer.Objects, 2)

	// Second refresh: different items.
	mock.mu.Lock()
	mock.sceneItems = []*obs_grpc.SceneItem{
		{SourceName: "X", SceneItemID: 10, SceneItemEnabled: false},
	}
	mock.mu.Unlock()

	p.refreshOBSSceneItems(ctx, mock, "Scene2")
	require.Len(t, p.obsSceneItemsContainer.Objects, 1, "old items should be replaced")

	check := p.obsSceneItemsContainer.Objects[0].(*widget.Check)
	testifyassert.Equal(t, "X", check.Text)
	testifyassert.False(t, check.Checked)
}

func TestRefreshOBSSceneItems_WiresOnChangedCallback(t *testing.T) {
	ctx := context.Background()

	mock := &mockOBSServer{
		sceneItems: []*obs_grpc.SceneItem{
			{SourceName: "Music", SceneItemID: 42, SceneItemEnabled: true},
		},
	}

	// Panel needs StreamD.OBS() for the toggle callback.
	// We test obsSetSceneItemEnabled separately instead.
	// Here we verify the check widget is wired and can be toggled.
	p := newTestPanel(t)
	p.refreshOBSSceneItems(ctx, mock, "Scene")

	check := p.obsSceneItemsContainer.Objects[0].(*widget.Check)
	testifyassert.Equal(t, "Music", check.Text)
	testifyassert.True(t, check.Checked, "initial state should be enabled")
	testifyassert.NotNil(t, check.OnChanged, "callback should be wired")
}
