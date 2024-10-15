//go:build !with_libav
// +build !with_libav

package process

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/recoder"
)

type Recoder struct {
	*Client
}

func (r *Recoder) Kill() error {
	return fmt.Errorf("not compiled with libav support")
}

func Run(
	ctx context.Context,
) (*Recoder, error) {
	return nil, fmt.Errorf("not compiled with libav support")
}

type Client struct{}

type InputID uint64
type InputConfig = recoder.InputConfig

type OutputID uint64
type OutputConfig = recoder.OutputConfig

type RecoderID uint64
type RecoderConfig = recoder.Config

func (c *Client) NewInputFromURL(
	ctx context.Context,
	url string,
	config InputConfig,
) (InputID, error) {
	return 0, fmt.Errorf("not compiled with libav support")
}

func (c *Client) NewOutputFromURL(
	ctx context.Context,
	url string,
	config OutputConfig,
) (OutputID, error) {
	return 0, fmt.Errorf("not compiled with libav support")
}

func (c *Client) StartRecoding(
	ctx context.Context,
	recoderID RecoderID,
	inputID InputID,
	outputID OutputID,
) error {
	return fmt.Errorf("not compiled with libav support")
}

func (c *Client) NewRecoder(
	ctx context.Context,
	config RecoderConfig,
) (RecoderID, error) {
	return 0, fmt.Errorf("not compiled with libav support")
}

type RecoderStats = recoder.Stats

func (c *Client) GetRecoderStats(
	ctx context.Context,
	recoderID RecoderID,
) (*RecoderStats, error) {
	return nil, fmt.Errorf("not compiled with libav support")
}

func (c *Client) RecodingEndedChan(
	ctx context.Context,
	recoderID RecoderID,
) (<-chan struct{}, error) {
	return nil, fmt.Errorf("not compiled with libav support")
}
