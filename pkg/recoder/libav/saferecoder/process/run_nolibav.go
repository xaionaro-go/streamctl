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

type RecoderID uint64

type InputID uint64
type InputConfig = recoder.InputConfig

type OutputID uint64
type OutputConfig = recoder.OutputConfig

type EncoderID uint64
type EncoderConfig = recoder.EncoderConfig

func (c *Client) NewInputFromURL(
	ctx context.Context,
	url string,
	authKey string,
	config InputConfig,
) (InputID, error) {
	return 0, fmt.Errorf("not compiled with libav support")
}

func (c *Client) NewOutputFromURL(
	ctx context.Context,
	url string,
	streamKey string,
	config OutputConfig,
) (OutputID, error) {
	return 0, fmt.Errorf("not compiled with libav support")
}

func (c *Client) StartRecoding(
	ctx context.Context,
	recoderID RecoderID,
	encoderID EncoderID,
	inputID InputID,
	outputID OutputID,
) error {
	return fmt.Errorf("not compiled with libav support")
}

func (c *Client) SetEncoderConfig(
	ctx context.Context,
	encoderID EncoderID,
	cfg EncoderConfig,
) error {
	return fmt.Errorf("not compiled with libav support")
}

func (c *Client) NewRecoder(
	ctx context.Context,
) (RecoderID, error) {
	return 0, fmt.Errorf("not compiled with libav support")
}

func (c *Client) NewEncoder(
	ctx context.Context,
	cfg EncoderConfig,
) (EncoderID, error) {
	return 0, fmt.Errorf("not compiled with libav support")
}

func (c *Client) CloseEncoder(
	ctx context.Context,
	encoderID EncoderID,
) error {
	return fmt.Errorf("not compiled with libav support")
}

type EncoderStats = recoder.Stats

func (c *Client) GetRecoderStats(
	ctx context.Context,
	encoderID RecoderID,
) (*EncoderStats, error) {
	return nil, fmt.Errorf("not compiled with libav support")
}

func (c *Client) RecodingEndedChan(
	ctx context.Context,
	recoderID RecoderID,
) (<-chan struct{}, error) {
	return nil, fmt.Errorf("not compiled with libav support")
}

func (c *Client) CloseInput(
	ctx context.Context,
	inputID InputID,
) error {
	return fmt.Errorf("not compiled with libav support")
}

func (c *Client) CloseOutput(
	ctx context.Context,
	outputID OutputID,
) error {
	return fmt.Errorf("not compiled with libav support")
}
