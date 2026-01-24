package obs

import (
	"sync"

	"github.com/andreykaipov/goobs"
	"github.com/andreykaipov/goobs/api/requests/record"
	"github.com/andreykaipov/goobs/api/requests/scenes"
	"github.com/andreykaipov/goobs/api/requests/stream"
	"github.com/andreykaipov/goobs/api/typedefs"
)

type clientMock struct {
	locker sync.Mutex

	Streaming bool
	Recording bool

	CurrentScene string
	Scenes       []string

	incomingEvents chan any
}

func newClientMock() *clientMock {
	return &clientMock{
		Scenes:         []string{"Scene 1", "Scene 2"},
		CurrentScene:   "Scene 1",
		incomingEvents: make(chan any),
	}
}

var _ client = (*clientMock)(nil)

func (c *clientMock) IncomingEvents() chan any {
	return c.incomingEvents
}

func (c *clientMock) GetGoobsClient() *goobs.Client {
	return nil
}

func (c *clientMock) Disconnect() error {
	return nil
}

func (c *clientMock) GetStreamStatus() (*stream.GetStreamStatusResponse, error) {
	c.locker.Lock()
	defer c.locker.Unlock()
	return &stream.GetStreamStatusResponse{
		OutputActive: c.Streaming,
	}, nil
}

func (c *clientMock) StartStream() (*stream.StartStreamResponse, error) {
	c.locker.Lock()
	defer c.locker.Unlock()
	c.Streaming = true
	return &stream.StartStreamResponse{}, nil
}

func (c *clientMock) StopStream() (*stream.StopStreamResponse, error) {
	c.locker.Lock()
	defer c.locker.Unlock()
	c.Streaming = false
	return &stream.StopStreamResponse{}, nil
}

func (c *clientMock) GetRecordStatus() (*record.GetRecordStatusResponse, error) {
	c.locker.Lock()
	defer c.locker.Unlock()
	return &record.GetRecordStatusResponse{
		OutputActive: c.Recording,
	}, nil
}

func (c *clientMock) StartRecord() (*record.StartRecordResponse, error) {
	c.locker.Lock()
	defer c.locker.Unlock()
	c.Recording = true
	return &record.StartRecordResponse{}, nil
}

func (c *clientMock) StopRecord() (*record.StopRecordResponse, error) {
	c.locker.Lock()
	defer c.locker.Unlock()
	c.Recording = false
	return &record.StopRecordResponse{}, nil
}

func (c *clientMock) GetSceneList(params *scenes.GetSceneListParams) (*scenes.GetSceneListResponse, error) {
	c.locker.Lock()
	defer c.locker.Unlock()
	var scenesList []*typedefs.Scene
	for _, name := range c.Scenes {
		scenesList = append(scenesList, &typedefs.Scene{
			SceneName: name,
		})
	}
	return &scenes.GetSceneListResponse{
		CurrentProgramSceneName: c.CurrentScene,
		Scenes:                  scenesList,
	}, nil
}

func (c *clientMock) SetCurrentProgramScene(params *scenes.SetCurrentProgramSceneParams) (*scenes.SetCurrentProgramSceneResponse, error) {
	c.locker.Lock()
	defer c.locker.Unlock()
	if params.SceneName != nil {
		c.CurrentScene = *params.SceneName
	}
	return &scenes.SetCurrentProgramSceneResponse{}, nil
}
