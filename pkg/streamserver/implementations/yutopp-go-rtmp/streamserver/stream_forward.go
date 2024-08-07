package streamserver

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/xlogger"
	flvtag "github.com/yutopp/go-flv/tag"
	"github.com/yutopp/go-rtmp"
	rtmpmsg "github.com/yutopp/go-rtmp/message"
)

const (
	chunkSize = 128
)

type ActiveStreamForwarding struct {
	Locker        sync.Mutex
	StreamID      types.StreamID
	DestinationID types.DestinationID
	URL           *url.URL
	Client        *rtmp.ClientConn
	OutStream     *rtmp.Stream
	Sub           *Sub
	RelayService  *RelayService
	CancelFunc    context.CancelFunc
	ReadCount     atomic.Uint64
	WriteCount    atomic.Uint64
	eventChan     chan *flvtag.FlvTag
}

func NewActiveStreamForward(
	ctx context.Context,
	streamID types.StreamID,
	dstID types.DestinationID,
	urlString string,
	relayService *RelayService,
) (*ActiveStreamForwarding, error) {
	urlParsed, err := url.Parse(urlString)
	if err != nil {
		return nil, fmt.Errorf("unable to parse URL '%s': %w", urlString, err)
	}
	fwd := &ActiveStreamForwarding{
		StreamID:      streamID,
		DestinationID: dstID,
		URL:           urlParsed,
		RelayService:  relayService,
		eventChan:     make(chan *flvtag.FlvTag),
	}
	if err := fwd.Start(ctx); err != nil {
		return nil, fmt.Errorf("unable to start the forwarder: %w", err)
	}
	return fwd, nil
}

func (fwd *ActiveStreamForwarding) Start(ctx context.Context) error {
	fwd.Locker.Lock()
	defer fwd.Locker.Unlock()
	if fwd.CancelFunc != nil {
		return fmt.Errorf("the stream forwarder is already running")
	}
	ctx, cancelFn := context.WithCancel(ctx)
	fwd.CancelFunc = cancelFn
	observability.Go(ctx, func() {
		for {
			err := fwd.waitForPublisherAndStart(
				ctx,
			)
			select {
			case <-ctx.Done():
				fwd.Close()
				return
			default:
			}
			if err != nil {
				logger.Errorf(ctx, "%s", err)
			}
		}
	})
	return nil
}

func (fwd *ActiveStreamForwarding) Stop() error {
	return fwd.Close()
}

func (fwd *ActiveStreamForwarding) waitForPublisherAndStart(
	ctx context.Context,
) (_ret error) {
	defer func() {
		if r := recover(); r != nil {
			_ret = fmt.Errorf("got panic: %v", r)
		}
		if _ret == nil {
			return
		}
		logger.Errorf(ctx, "%v", _ret)
	}()

	pathParts := strings.SplitN(fwd.URL.Path, "/", -2)
	remoteAppName := "live"
	apiKey := pathParts[len(pathParts)-1]
	if len(pathParts) >= 2 {
		remoteAppName = strings.Trim(strings.Join(pathParts[:len(pathParts)-1], "/"), "/")
	}
	streamID := fwd.StreamID
	streamIDParts := strings.Split(string(streamID), "/")
	localAppName := string(streamID)
	if len(streamIDParts) == 2 {
		localAppName = streamIDParts[1]
	}

	ctx = belt.WithField(belt.WithField(ctx, "appNameLocal", localAppName), "appNameRemote", apiKey)

	logger.Tracef(ctx, "wait for stream '%s'", streamID)
	pubSub := fwd.RelayService.WaitPubsub(ctx, localAppName)
	logger.Tracef(ctx, "wait for stream '%s' result: %#+v", streamID, pubSub)
	if pubSub == nil {
		return fmt.Errorf(
			"unable to find stream ID '%s', available stream IDs: %s",
			streamID,
			strings.Join(fwd.RelayService.PubsubNames(), ", "),
		)
	}

	urlParsed := ptr(*fwd.URL)
	logger.Tracef(ctx, "connecting to '%s'", fwd.URL.String())
	if fwd.URL.Port() == "" {
		switch urlParsed.Scheme {
		case "rtmp":
			urlParsed.Host += ":1935"
		case "rtmps":
			urlParsed.Host += ":443"
		default:
			return fmt.Errorf("unexpected scheme '%s' in URL '%s'", urlParsed.Scheme, urlParsed.String())
		}
	}
	var dialFunc func(protocol, addr string, config *rtmp.ConnConfig) (*rtmp.ClientConn, error)
	switch urlParsed.Scheme {
	case "rtmp":
		dialFunc = rtmp.Dial
	case "rtmps":
		dialFunc = func(protocol, addr string, config *rtmp.ConnConfig) (*rtmp.ClientConn, error) {
			return rtmp.TLSDial(protocol, addr, config, http.DefaultTransport.(*http.Transport).TLSClientConfig)
		}
	default:
		return fmt.Errorf("unexpected scheme '%s' in URL '%s'", urlParsed.Scheme, urlParsed.String())
	}
	client, err := dialFunc(urlParsed.Scheme, urlParsed.Host, &rtmp.ConnConfig{
		Logger: xlogger.LogrusFieldLoggerFromCtx(ctx),
	})
	if err != nil {
		return fmt.Errorf("unable to connect to '%s': %w", urlParsed.String(), err)
	}
	fwd.Client = client

	logger.Tracef(ctx, "connected to '%s'", urlParsed.String())

	tcURL := *urlParsed
	tcURL.Path = "/" + remoteAppName
	if tcURL.Port() == "1935" {
		tcURL.Host = tcURL.Hostname()
	}

	if err := client.Connect(&rtmpmsg.NetConnectionConnect{
		Command: rtmpmsg.NetConnectionConnectCommand{
			App:      remoteAppName,
			Type:     "nonprivate",
			FlashVer: "StreamPanel",
			TCURL:    tcURL.String(),
		},
	}); err != nil {
		return fmt.Errorf("unable to connect the stream to '%s': %w", urlParsed.String(), err)
	}
	logger.Tracef(ctx, "connected the stream to '%s'", urlParsed.String())

	fwd.OutStream, err = client.CreateStream(&rtmpmsg.NetConnectionCreateStream{}, chunkSize)
	if err != nil {
		return fmt.Errorf("unable to create a stream to '%s': %w", urlParsed.String(), err)
	}

	logger.Tracef(ctx, "calling Publish at '%s'", urlParsed.String())
	if err := fwd.OutStream.Publish(&rtmpmsg.NetStreamPublish{
		PublishingName: apiKey,
		PublishingType: "live",
	}); err != nil {
		return fmt.Errorf("unable to send the Publish message to '%s': %w", urlParsed.String(), err)
	}

	logger.Tracef(ctx, "starting publishing to '%s'", urlParsed.String())
	fwd.Sub = pubSub.Sub(func(flv *flvtag.FlvTag) error {
		logger.Tracef(ctx, "flvtag == %#+v", *flv)
		var buf bytes.Buffer

		switch d := flv.Data.(type) {
		case *flvtag.AudioData:
			// Consume flv payloads (d)
			if err := flvtag.EncodeAudioData(&buf, d); err != nil {
				err = fmt.Errorf("flvtag.Data == %#+v; err == %w", *d, err)
				return err
			}

			payloadLen := uint64(buf.Len())
			fwd.WriteCount.Add(payloadLen)
			logger.Tracef(ctx, "flvtag.Data == %#+v; payload len == %d", *d, payloadLen)

			// TODO: Fix these values
			chunkStreamID := 5
			if err := fwd.OutStream.Write(chunkStreamID, flv.Timestamp, &rtmpmsg.AudioMessage{
				Payload: &buf,
			}); err != nil {
				err = fmt.Errorf("fwd.OutStream.Write return an error: %w", err)
				return err
			}

		case *flvtag.VideoData:
			// Consume flv payloads (d)
			if err := flvtag.EncodeVideoData(&buf, d); err != nil {
				err = fmt.Errorf("flvtag.Data == %#+v; err == %w", *d, err)
				return err
			}

			payloadLen := uint64(buf.Len())
			fwd.WriteCount.Add(payloadLen)
			logger.Tracef(ctx, "flvtag.Data == %#+v; payload len == %d", *d, payloadLen)

			// TODO: Fix these values
			chunkStreamID := 6
			if err := fwd.OutStream.Write(chunkStreamID, flv.Timestamp, &rtmpmsg.VideoMessage{
				Payload: &buf,
			}); err != nil {
				err = fmt.Errorf("fwd.OutStream.Write return an error: %w", err)
				return err
			}

		case *flvtag.ScriptData:
			// Consume flv payloads (d)
			if err := flvtag.EncodeScriptData(&buf, d); err != nil {
				err = fmt.Errorf("flvtag.Data == %#+v; err == %v", *d, err)
				return err
			}

			payloadLen := uint64(buf.Len())
			fwd.WriteCount.Add(payloadLen)
			logger.Tracef(ctx, "flvtag.Data == %#+v; payload len == %d", *d, payloadLen)

			// TODO: hide these implementation
			amdBuf := new(bytes.Buffer)
			amfEnc := rtmpmsg.NewAMFEncoder(amdBuf, rtmpmsg.EncodingTypeAMF0)
			if err := rtmpmsg.EncodeBodyAnyValues(amfEnc, &rtmpmsg.NetStreamSetDataFrame{
				Payload: buf.Bytes(),
			}); err != nil {
				err = fmt.Errorf("flvtag.Data == %#+v; payload len == %d; err == %v", *d, payloadLen, err)
				return err
			}

			// TODO: Fix these values
			chunkStreamID := 8
			if err := fwd.OutStream.Write(chunkStreamID, flv.Timestamp, &rtmpmsg.DataMessage{
				Name:     "@setDataFrame", // TODO: fix
				Encoding: rtmpmsg.EncodingTypeAMF0,
				Body:     amdBuf,
			}); err != nil {
				err = fmt.Errorf("fwd.OutStream.Write return an error: %v", err)
				return err
			}

		default:
			panic("unreachable")
		}
		return nil
	})

	logger.Tracef(ctx, "started publishing to '%s'", urlParsed.String())
	<-ctx.Done()
	return nil
}

func (fwd *ActiveStreamForwarding) Close() error {
	fwd.Locker.Lock()
	defer fwd.Locker.Unlock()
	if fwd.CancelFunc == nil {
		return fmt.Errorf("the stream was not started yet")
	}

	var result *multierror.Error
	fwd.CancelFunc()
	fwd.CancelFunc = nil
	if fwd.Sub != nil {
		result = multierror.Append(result, fwd.Sub.Close())
		fwd.Sub = nil
	}
	if fwd.Client != nil {
		result = multierror.Append(result, fwd.Client.Close())
		fwd.Client = nil
	}
	return result.ErrorOrNil()
}
