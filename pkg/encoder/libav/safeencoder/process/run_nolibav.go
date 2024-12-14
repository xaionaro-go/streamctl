//go:build !with_libav
// +build !with_libav

package process

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/encoder"
)

type Encoder struct {
	*Client
}

func (r *Encoder) Kill() error {
	return fmt.Errorf("not compiled with libav support")
}

func Run(
	ctx context.Context,
) (*Encoder, error) {
	return nil, fmt.Errorf("not compiled with libav support")
}

type Client struct{}

type InputID uint64
type InputConfig = encoder.InputConfig

type OutputID uint64
type OutputConfig = encoder.OutputConfig

type EncoderID uint64
type EncoderConfig = encoder.Config

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

func (c *Client) StartEncoding(
	ctx context.Context,
	encoderID EncoderID,
	inputID InputID,
	outputID OutputID,
) error {
	return fmt.Errorf("not compiled with libav support")
}

func (c *Client) NewEncoder(
	ctx context.Context,
) (EncoderID, error) {
	return 0, fmt.Errorf("not compiled with libav support")
}

type EncoderStats = encoder.Stats

func (c *Client) GetEncoderStats(
	ctx context.Context,
	encoderID EncoderID,
) (*EncoderStats, error) {
	return nil, fmt.Errorf("not compiled with libav support")
}

func (c *Client) EncodingEndedChan(
	ctx context.Context,
	recoderID EncoderID,
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
