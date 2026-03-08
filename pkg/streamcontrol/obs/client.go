package obs

import (
	"github.com/andreykaipov/goobs"
	"github.com/andreykaipov/goobs/api/requests/record"
	"github.com/andreykaipov/goobs/api/requests/scenes"
	"github.com/andreykaipov/goobs/api/requests/stream"
)

type client interface {
	Disconnect() error
	GetStreamStatus() (*stream.GetStreamStatusResponse, error)
	StartStream() (*stream.StartStreamResponse, error)
	StopStream() (*stream.StopStreamResponse, error)
	GetRecordStatus() (*record.GetRecordStatusResponse, error)
	StartRecord() (*record.StartRecordResponse, error)
	StopRecord() (*record.StopRecordResponse, error)
	GetSceneList(params *scenes.GetSceneListParams) (*scenes.GetSceneListResponse, error)
	SetCurrentProgramScene(params *scenes.SetCurrentProgramSceneParams) (*scenes.SetCurrentProgramSceneResponse, error)
	IncomingEvents() chan any
	GetGoobsClient() *goobs.Client
}

type clientGoobs struct {
	*goobs.Client
}

func (c *clientGoobs) GetGoobsClient() *goobs.Client {
	return c.Client
}

func (c *clientGoobs) IncomingEvents() chan any {
	return c.Client.IncomingEvents
}

func (c *clientGoobs) GetStreamStatus() (*stream.GetStreamStatusResponse, error) {
	return c.Client.Stream.GetStreamStatus()
}
func (c *clientGoobs) StartStream() (*stream.StartStreamResponse, error) {
	return c.Client.Stream.StartStream()
}
func (c *clientGoobs) StopStream() (*stream.StopStreamResponse, error) {
	return c.Client.Stream.StopStream()
}
func (c *clientGoobs) GetRecordStatus() (*record.GetRecordStatusResponse, error) {
	return c.Client.Record.GetRecordStatus()
}
func (c *clientGoobs) StartRecord() (*record.StartRecordResponse, error) {
	return c.Client.Record.StartRecord()
}
func (c *clientGoobs) StopRecord() (*record.StopRecordResponse, error) {
	return c.Client.Record.StopRecord()
}
func (c *clientGoobs) GetSceneList(params *scenes.GetSceneListParams) (*scenes.GetSceneListResponse, error) {
	return c.Client.Scenes.GetSceneList(params)
}
func (c *clientGoobs) SetCurrentProgramScene(params *scenes.SetCurrentProgramSceneParams) (*scenes.SetCurrentProgramSceneResponse, error) {
	return c.Client.Scenes.SetCurrentProgramScene(params)
}
func (c *clientGoobs) Disconnect() error {
	return c.Client.Disconnect()
}
