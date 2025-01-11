package client

import (
	"context"
	"fmt"
	"net"
	"net/url"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/recoder"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/saferecoder"
	"github.com/xaionaro-go/xsync"
)

type Encoder struct {
	encoder            *saferecoder.Encoder
	input              *saferecoder.Input
	inputLocker        xsync.Mutex
	output             *saferecoder.Output
	outputLocker       xsync.Mutex
	listener           *net.UDPConn
	destinationsLocker xsync.Mutex
	destinationAddrs   []*net.UDPAddr
	destinationConns   []*net.UDPConn
}

func NewEncoder(
	ctx context.Context,
	srcURL string,
) (_ *Encoder, _err error) {
	logger.Debugf(ctx, "NewEncoder()")
	defer func() { logger.Debugf(ctx, "/NewEncoder(): %v", _err) }()

	proc, err := saferecoder.NewProcess(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize an encoder host-process: %w", err)
	}

	defer func() {
		if _err != nil {
			proc.Kill()
		}
	}()

	enc, err := proc.NewEncoder(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize an encoder: %w", err)
	}

	listener, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, fmt.Errorf("unable to start listening an UDP port: %w", err)
	}

	return &Encoder{
		encoder:  enc,
		listener: listener,
	}, nil
}

func (enc *Encoder) AddDestination(
	addr *net.UDPAddr,
) error {

}

func (enc *Encoder) SetInput(
	ctx context.Context,
	srcURL string,
	streamKey string,
) error {
	input, err := enc.encoder.Process.NewInputFromURL(ctx, srcURL, streamKey, recoder.InputConfig{})
	if err != nil {
		return fmt.Errorf("unable to open the source '%s': %w", srcURL, err)
	}

	enc.inputLocker.Do(ctx, func() {
		if input := enc.input; input != nil {
			err := input.Close()
			logger.Debugf(ctx, "inputClose(): %v", err)
			enc.input = nil
		}

		enc.input = input
	})
	return nil
}

func (enc *Encoder) stopProxy() {

}

func (enc *Encoder) SetOutput(
	ctx context.Context,
	dstURLString string,
	streamKey string,
) error {
	dstURL, err := url.Parse(dstURLString)
	if err != nil {
		return fmt.Errorf("the link '%s' is invalid: %w", dstURLString, err)
	}

	enc.outputLocker.Do(ctx, func() {
		proxyURL := *dstURL
		proxyURL.Host = enc.listener.LocalAddr().String()

		enc.stopProxy()

		output, err := enc.encoder.Process.NewOutputFromURL(ctx, proxyURL, streamKey, recoder.OutputConfig{})
		if err != nil {
			return fmt.Errorf("unable to open the source '%s': %w", dstURLString, err)
		}

		if output := enc.output; output != nil {
			err := output.Close()
			logger.Debugf(ctx, "outputClose(): %v", err)
			enc.output = nil
		}

		enc.output = output
	})
	return nil
}
