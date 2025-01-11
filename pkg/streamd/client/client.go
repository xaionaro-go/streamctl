package client

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/grpcproxy/grpchttpproxy"
	"github.com/xaionaro-go/grpcproxy/protobuf/go/proxy_grpc"
	"github.com/xaionaro-go/obs-grpc-proxy/pkg/obsgrpcproxy"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/player/pkg/player"
	"github.com/xaionaro-go/player/pkg/player/protobuf/go/player_grpc"
	p2ptypes "github.com/xaionaro-go/streamctl/pkg/p2p/types"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	streamdconfig "github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/event"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/goconv"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/xsync"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

type Client struct {
	Stats struct {
		BytesIn  uint64
		BytesOut uint64
	}
	Target string
	Config Config

	PersistentConnectionLocker xsync.Mutex
	PersistentConnection       *grpc.ClientConn
	PersistentStreamDClient    streamd_grpc.StreamDClient
	PersistentOBSClient        obs_grpc.OBSClient
}

type OBSInstanceID = streamtypes.OBSInstanceID

var _ api.StreamD = (*Client)(nil)

func New(
	ctx context.Context,
	target string,
	opts ...Option,
) (*Client, error) {
	c := &Client{
		Target: target,
		Config: Options(opts).Config(ctx),
	}
	if err := c.init(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func WrapConn(
	ctx context.Context,
	clientConn *grpc.ClientConn,
	opts ...Option,
) *Client {
	cfg := Options(opts).Config(ctx)
	cfg.UsePersistentConnection = true
	c := &Client{
		Config:               cfg,
		PersistentConnection: clientConn,
		PersistentStreamDClient: streamd_grpc.NewStreamDClient(
			clientConn,
		),
		PersistentOBSClient: obs_grpc.NewOBSClient(clientConn),
	}
	return c
}

func (c *Client) init(ctx context.Context) error {
	var result *multierror.Error
	if c.Config.UsePersistentConnection {
		result = multierror.Append(
			result,
			c.initPersistentConnection(ctx),
		)
	}
	return result.ErrorOrNil()
}

func (c *Client) initPersistentConnection(
	ctx context.Context,
) error {
	conn, err := c.connect(ctx)
	if err != nil {
		return err
	}
	c.PersistentConnection = conn
	c.PersistentStreamDClient = streamd_grpc.NewStreamDClient(
		conn,
	)
	c.PersistentOBSClient = obs_grpc.NewOBSClient(conn)
	return nil
}

func (c *Client) TagRPC(
	ctx context.Context,
	_ *stats.RPCTagInfo,
) context.Context {
	return ctx
}

func (c *Client) HandleRPC(
	ctx context.Context,
	s stats.RPCStats,
) {
	switch s := s.(type) {
	case *stats.InPayload:
		atomic.AddUint64(&c.Stats.BytesIn, uint64(s.WireLength))
	case *stats.OutPayload:
		atomic.AddUint64(&c.Stats.BytesOut, uint64(s.WireLength))
	}
}

func (c *Client) TagConn(
	ctx context.Context,
	_ *stats.ConnTagInfo,
) context.Context {
	return ctx
}

func (c *Client) HandleConn(
	context.Context,
	stats.ConnStats,
) {
}

func (c *Client) connect(
	ctx context.Context,
) (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(
			insecure.NewCredentials(),
		),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  c.Config.Reconnect.InitialInterval,
				Multiplier: c.Config.Reconnect.IntervalMultiplier,
				Jitter:     0.2,
				MaxDelay:   c.Config.Reconnect.MaximalInterval,
			},
			MinConnectTimeout: c.Config.Reconnect.InitialInterval,
		}),
		grpc.WithStatsHandler(c),
	}
	wrapper := c.Config.ConnectWrapper
	if wrapper == nil {
		return c.doConnect(ctx, opts...)
	}

	return wrapper(
		ctx,
		func(ctx context.Context, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
			return c.doConnect(ctx, opts...)
		},
		opts...,
	)
}

func (c *Client) doConnect(
	ctx context.Context,
	opts ...grpc.DialOption,
) (*grpc.ClientConn, error) {
	logger.Tracef(
		ctx,
		"doConnect(ctx, %#+v): Config: %#+v",
		opts,
		c.Config,
	)
	delay := c.Config.Reconnect.InitialInterval
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		logger.Debugf(
			ctx,
			"trying to (re-)connect to %s",
			c.Target,
		)

		dialCtx, cancelFn := context.WithTimeout(
			ctx,
			time.Second,
		)
		conn, err := grpc.DialContext(
			dialCtx,
			c.Target,
			opts...)
		cancelFn()
		if err == nil {
			logger.Debugf(
				ctx,
				"successfully (re-)connected to %s",
				c.Target,
			)
			return conn, nil
		}
		logger.Debugf(
			ctx,
			"(re-)connection failed to %s: %v; sleeping %v before the next try",
			c.Target,
			err,
			delay,
		)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		delay = time.Duration(
			float64(
				delay,
			) * c.Config.Reconnect.IntervalMultiplier,
		)
		if delay > c.Config.Reconnect.MaximalInterval {
			delay = c.Config.Reconnect.MaximalInterval
		}
	}
}

func (c *Client) grpcClient(
	ctx context.Context,
) (streamd_grpc.StreamDClient, obs_grpc.OBSClient, io.Closer, error) {
	if c.Config.UsePersistentConnection {
		return c.grpcPersistentClient(ctx)
	} else {
		return c.grpcNewClient(ctx)
	}
}

func (c *Client) grpcNewClient(
	ctx context.Context,
) (streamd_grpc.StreamDClient, obs_grpc.OBSClient, *grpc.ClientConn, error) {
	conn, err := c.connect(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf(
			"unable to initialize a gRPC client: %w",
			err,
		)
	}

	streamDClient := streamd_grpc.NewStreamDClient(conn)
	obsClient := obs_grpc.NewOBSClient(conn)
	return streamDClient, obsClient, conn, nil
}

func (c *Client) grpcPersistentClient(
	ctx context.Context,
) (streamd_grpc.StreamDClient, obs_grpc.OBSClient, dummyCloser, error) {
	return xsync.DoA1R4(
		ctx,
		&c.PersistentConnectionLocker,
		c.grpcPersistentClientNoLock,
		ctx,
	)
}

func (c *Client) grpcPersistentClientNoLock(
	ctx context.Context,
) (streamd_grpc.StreamDClient, obs_grpc.OBSClient, dummyCloser, error) {
	return c.PersistentStreamDClient, c.PersistentOBSClient, dummyCloser{}, nil
}

func (c *Client) Run(ctx context.Context) error {
	return nil
}

func callWrapper[REQ any, REPLY any](
	ctx context.Context,
	c *Client,
	fn func(context.Context, *REQ, ...grpc.CallOption) (REPLY, error),
	req *REQ,
	opts ...grpc.CallOption,
) (REPLY, error) {

	var reply REPLY
	callFn := func(ctx context.Context, opts ...grpc.CallOption) error {
		var err error
		delay := c.Config.Reconnect.InitialInterval
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			reply, err = fn(ctx, req, opts...)
			if err == nil {
				return nil
			}
			err = c.processError(ctx, err)
			if err != nil {
				return err
			}
			logger.Debugf(
				ctx,
				"retrying; sleeping %v for the retry",
				delay,
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			delay = time.Duration(
				float64(
					delay,
				) * c.Config.Reconnect.IntervalMultiplier,
			)
			if delay > c.Config.Reconnect.MaximalInterval {
				delay = c.Config.Reconnect.MaximalInterval
			}
		}
	}

	wrapper := c.Config.CallWrapper
	if wrapper == nil {
		err := callFn(ctx, opts...)
		return reply, err
	}

	err := wrapper(ctx, req, callFn, opts...)
	return reply, err
}

func withStreamDClient[REPLY any](
	ctx context.Context,
	c *Client,
	fn func(context.Context, streamd_grpc.StreamDClient, io.Closer) (*REPLY, error),
) (*REPLY, error) {
	pc, _, _, _ := runtime.Caller(1)
	caller := runtime.FuncForPC(pc)
	ctx = belt.WithField(ctx, "caller_func", caller.Name())

	client, _, conn, err := c.grpcClient(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if client == nil {
		return nil, fmt.Errorf(
			"internal error: client is nil",
		)
	}
	return fn(ctx, client, conn)
}

type receiver[T any] interface {
	grpc.ClientStream

	Recv() (*T, error)
}

func unwrapStreamDChan[E any, R any, S receiver[R]](
	ctx context.Context,
	c *Client,
	fn func(ctx context.Context, client streamd_grpc.StreamDClient) (S, error),
	parse func(ctx context.Context, event *R) E,
) (<-chan E, error) {

	pc, _, _, _ := runtime.Caller(1)
	caller := runtime.FuncForPC(pc)
	ctx = belt.WithField(ctx, "caller_func", caller.Name())

	ctx, cancelFn := context.WithCancel(ctx)
	getSub := func() (S, io.Closer, error) {
		client, _, closer, err := c.grpcClient(ctx)
		if err != nil {
			var emptyS S
			return emptyS, nil, err
		}
		sub, err := fn(ctx, client)
		if err != nil {
			var emptyS S
			return emptyS, nil, fmt.Errorf(
				"unable to subscribe: %w",
				err,
			)
		}
		return sub, closer, nil
	}

	sub, closer, err := getSub()
	if err != nil {
		cancelFn()
		return nil, err
	}

	r := make(chan E)
	observability.Go(ctx, func() {
		defer closer.Close()
		defer cancelFn()
		for {
			event, err := sub.Recv()
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err != nil {
				switch {
				case errors.Is(err, io.EOF):
					logger.Debugf(
						ctx,
						"the receiver is closed: %v",
						err,
					)
					return
				case strings.Contains(err.Error(), grpc.ErrClientConnClosing.Error()):
					logger.Debugf(
						ctx,
						"apparently we are closing the client: %v",
						err,
					)
					return
				case strings.Contains(err.Error(), context.Canceled.Error()):
					logger.Debugf(
						ctx,
						"subscription was cancelled: %v",
						err,
					)
					return
				default:
					for {
						err = c.processError(ctx, err)
						if err != nil {
							logger.Errorf(
								ctx,
								"unable to read data: %v",
								err,
							)
							return
						}
						closer.Close()
						sub, closer, err = getSub()
						if err != nil {
							logger.Errorf(
								ctx,
								"unable to resubscribe: %v",
								err,
							)
							continue
						}
						break
					}
					continue
				}
			}

			r <- parse(ctx, event)
		}
	})
	return r, nil
}

func (c *Client) processError(
	ctx context.Context,
	err error,
) error {
	logger.Tracef(ctx, "processError(ctx, '%v'): %T", err, err)
	if s, ok := status.FromError(err); ok {
		logger.Tracef(ctx, "processError(ctx, '%v'): code == %#+v; msg == %#+v", err, s.Code(), s.Message())
		switch s.Code() {
		case codes.Unavailable:
			logger.Debugf(ctx, "suppressed the error (forcing a retry)")
			return nil
		}
	}
	return err
}

func (c *Client) Ping(
	ctx context.Context,
	beforeSend func(context.Context, *streamd_grpc.PingRequest),
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.PingReply, error) {
		req := &streamd_grpc.PingRequest{}
		beforeSend(ctx, req)
		return callWrapper(ctx, c, client.Ping, req)
	})
	return err
}

func (c *Client) InitCache(ctx context.Context) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.InitCacheReply, error) {
		return callWrapper(
			ctx,
			c,
			client.InitCache,
			&streamd_grpc.InitCacheRequest{},
		)
	})
	return err
}

func (c *Client) SaveConfig(ctx context.Context) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.SaveConfigReply, error) {
		return callWrapper(
			ctx,
			c,
			client.SaveConfig,
			&streamd_grpc.SaveConfigRequest{},
		)
	})
	return err
}

func (c *Client) ResetCache(ctx context.Context) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.ResetCacheReply, error) {
		return callWrapper(
			ctx,
			c,
			client.ResetCache,
			&streamd_grpc.ResetCacheRequest{},
		)
	})
	return err
}

func (c *Client) GetConfig(
	ctx context.Context,
) (*streamdconfig.Config, error) {
	reply, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.GetConfigReply, error) {
		return callWrapper(
			ctx,
			c,
			client.GetConfig,
			&streamd_grpc.GetConfigRequest{},
		)
	})
	if err != nil {
		return nil, err
	}

	var result streamdconfig.Config
	_, err = result.Read([]byte(reply.Config))
	if err != nil {
		return nil, fmt.Errorf(
			"unable to unserialize the received config: %w",
			err,
		)
	}
	return &result, nil
}

func (c *Client) SetConfig(
	ctx context.Context,
	cfg *streamdconfig.Config,
) error {
	var buf bytes.Buffer
	_, err := cfg.WriteTo(&buf)
	if err != nil {
		return fmt.Errorf(
			"unable to serialize the config: %w",
			err,
		)
	}

	_, err = withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.SetConfigReply, error) {
		return callWrapper(
			ctx,
			c,
			client.SetConfig,
			&streamd_grpc.SetConfigRequest{
				Config: buf.String(),
			},
		)
	})
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) IsBackendEnabled(
	ctx context.Context,
	id streamcontrol.PlatformName,
) (bool, error) {
	reply, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.IsBackendEnabledReply, error) {
		return callWrapper(
			ctx,
			c,
			client.IsBackendEnabled,
			&streamd_grpc.IsBackendEnabledRequest{
				PlatID: string(id),
			},
		)
	})
	if err != nil {
		return false, err
	}
	return reply.IsInitialized, nil
}

func (c *Client) StartStream(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	title string, description string,
	profile streamcontrol.AbstractStreamProfile,
	customArgs ...any,
) error {
	b, err := yaml.Marshal(profile)
	if err != nil {
		return fmt.Errorf(
			"unable to serialize the profile: %w",
			err,
		)
	}
	logger.Debugf(
		ctx,
		"serialized profile: '%#+v'",
		profile,
	)
	_, err = withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.StartStreamReply, error) {
		return callWrapper(
			ctx,
			c,
			client.StartStream,
			&streamd_grpc.StartStreamRequest{
				PlatID:      string(platID),
				Title:       title,
				Description: description,
				Profile:     string(b),
			},
		)
	})
	return err
}

func (c *Client) EndStream(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.EndStreamReply, error) {
		return callWrapper(
			ctx,
			c,
			client.EndStream,
			&streamd_grpc.EndStreamRequest{
				PlatID: string(platID),
			},
		)
	})
	return err
}

func (c *Client) GetBackendInfo(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (*api.BackendInfo, error) {
	reply, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.GetBackendInfoReply, error) {
		return callWrapper(
			ctx,
			c,
			client.GetBackendInfo,
			&streamd_grpc.GetBackendInfoRequest{
				PlatID: string(platID),
			},
		)
	})
	if err != nil {
		return nil, fmt.Errorf(
			"unable to get backend info: %w",
			err,
		)
	}

	caps := goconv.CapabilitiesGRPC2Go(ctx, reply.Capabilities)

	data, err := goconv.BackendDataGRPC2Go(platID, reply.GetData())
	if err != nil {
		return nil, fmt.Errorf("unable to deserialize data: %w", err)
	}

	return &api.BackendInfo{
		Data:         data,
		Capabilities: caps,
	}, nil
}

func (c *Client) Restart(ctx context.Context) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.RestartReply, error) {
		return callWrapper(
			ctx,
			c,
			client.Restart,
			&streamd_grpc.RestartRequest{},
		)
	})
	return err
}

func (c *Client) EXPERIMENTAL_ReinitStreamControllers(
	ctx context.Context,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.EXPERIMENTAL_ReinitStreamControllersReply, error) {
		return callWrapper(
			ctx,
			c,
			client.EXPERIMENTAL_ReinitStreamControllers,
			&streamd_grpc.EXPERIMENTAL_ReinitStreamControllersRequest{},
		)
	})
	return err
}

func (c *Client) GetStreamStatus(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (*streamcontrol.StreamStatus, error) {
	streamStatus, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.GetStreamStatusReply, error) {
		return callWrapper(
			ctx,
			c,
			client.GetStreamStatus,
			&streamd_grpc.GetStreamStatusRequest{
				PlatID: string(platID),
			},
		)
	})
	if err != nil {
		return nil, fmt.Errorf(
			"unable to get the stream status of '%s': %w",
			platID,
			err,
		)
	}

	var startedAt *time.Time
	if streamStatus != nil &&
		streamStatus.StartedAt != nil {
		v := *streamStatus.StartedAt
		startedAt = ptr(
			time.Unix(v/1000000000, v%1000000000),
		)
	}

	var customData any
	switch platID {
	case youtube.ID:
		d := youtube.StreamStatusCustomData{}
		err := json.Unmarshal(
			[]byte(streamStatus.GetCustomData()),
			&d,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to unserialize the custom data: %w",
				err,
			)
		}
		customData = d
	}

	var viewersCount *uint
	if streamStatus != nil && streamStatus.ViewersCount != nil {
		viewersCount = ptr(uint(*streamStatus.ViewersCount))
	}

	return &streamcontrol.StreamStatus{
		IsActive:     streamStatus.GetIsActive(),
		StartedAt:    startedAt,
		CustomData:   customData,
		ViewersCount: viewersCount,
	}, nil
}

func (c *Client) SetTitle(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	title string,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.SetTitleReply, error) {
		return callWrapper(
			ctx,
			c,
			client.SetTitle,
			&streamd_grpc.SetTitleRequest{
				PlatID: string(platID),
				Title:  title,
			},
		)
	})
	return err
}
func (c *Client) SetDescription(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	description string,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.SetDescriptionReply, error) {
		return callWrapper(
			ctx,
			c,
			client.SetDescription,
			&streamd_grpc.SetDescriptionRequest{
				PlatID:      string(platID),
				Description: description,
			},
		)
	})
	return err
}
func (c *Client) ApplyProfile(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	profile streamcontrol.AbstractStreamProfile,
	customArgs ...any,
) error {
	b, err := yaml.Marshal(profile)
	if err != nil {
		return fmt.Errorf(
			"unable to serialize the profile: %w",
			err,
		)
	}
	logger.Debugf(
		ctx,
		"serialized profile: '%#+v'",
		profile,
	)

	_, err = withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.ApplyProfileReply, error) {
		return callWrapper(
			ctx,
			c,
			client.ApplyProfile,
			&streamd_grpc.ApplyProfileRequest{
				PlatID:  string(platID),
				Profile: string(b),
			},
		)
	})
	return err
}

func (c *Client) UpdateStream(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	title string, description string,
	profile streamcontrol.AbstractStreamProfile,
	customArgs ...any,
) error {
	b, err := yaml.Marshal(profile)
	if err != nil {
		return fmt.Errorf(
			"unable to serialize the profile: %w",
			err,
		)
	}
	logger.Debugf(
		ctx,
		"serialized profile: '%#+v'",
		profile,
	)

	_, err = withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.UpdateStreamReply, error) {
		return callWrapper(
			ctx,
			c,
			client.UpdateStream,
			&streamd_grpc.UpdateStreamRequest{
				PlatID:      string(platID),
				Title:       title,
				Description: description,
				Profile:     string(b),
			},
		)
	})
	return err
}

func (c *Client) SubscribeToOAuthURLs(
	ctx context.Context,
	listenPort uint16,
) (<-chan *streamd_grpc.OAuthRequest, error) {
	return unwrapStreamDChan(
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_SubscribeToOAuthRequestsClient, error) {
			return callWrapper(
				ctx,
				c,
				client.SubscribeToOAuthRequests,
				&streamd_grpc.SubscribeToOAuthRequestsRequest{
					ListenPort: int32(listenPort),
				},
			)
		},
		func(
			ctx context.Context,
			event *streamd_grpc.OAuthRequest,
		) *streamd_grpc.OAuthRequest {
			return event
		},
	)
}

func (c *Client) GetVariable(
	ctx context.Context,
	key consts.VarKey,
) ([]byte, error) {
	reply, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.GetVariableReply, error) {
		return callWrapper(
			ctx,
			c,
			client.GetVariable,
			&streamd_grpc.GetVariableRequest{
				Key: string(key),
			},
		)
	})

	if err != nil {
		return nil, fmt.Errorf(
			"unable to get the variable '%s' value: %w",
			key,
			err,
		)
	}

	b := reply.GetValue()
	logger.Tracef(
		ctx,
		"downloaded variable value of size %d",
		len(b),
	)
	return b, nil
}

func (c *Client) GetVariableHash(
	ctx context.Context,
	key consts.VarKey,
	hashType crypto.Hash,
) ([]byte, error) {
	var hashTypeArg streamd_grpc.HashType
	switch hashType {
	case crypto.SHA1:
		hashTypeArg = streamd_grpc.HashType_HASH_SHA1
	default:
		return nil, fmt.Errorf(
			"unsupported hash type: %s",
			hashType,
		)
	}

	reply, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.GetVariableHashReply, error) {
		return callWrapper(
			ctx,
			c,
			client.GetVariableHash,
			&streamd_grpc.GetVariableHashRequest{
				Key:      string(key),
				HashType: hashTypeArg,
			},
		)
	})
	if err != nil {
		return nil, fmt.Errorf(
			"unable to get the variable '%s' hash: %w",
			key,
			err,
		)
	}

	b := reply.GetHash()
	logger.Tracef(
		ctx,
		"the downloaded hash of the variable '%s' is %X",
		key,
		b,
	)
	return b, nil
}

func (c *Client) SetVariable(
	ctx context.Context,
	key consts.VarKey,
	value []byte,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.SetVariableReply, error) {
		return callWrapper(
			ctx,
			c,
			client.SetVariable,
			&streamd_grpc.SetVariableRequest{
				Key:   string(key),
				Value: value,
			},
		)
	})
	return err
}

func (c *Client) OBS(
	ctx context.Context,
) (obs_grpc.OBSServer, context.CancelFunc, error) {
	logger.Tracef(ctx, "OBS()")
	defer logger.Tracef(ctx, "/OBS()")

	_, obsClient, closer, err := c.grpcClient(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"unable to initialize a gRPC client: %w",
			err,
		)
	}

	return &obsgrpcproxy.ClientAsServer{
			OBSClient: obsClient,
		}, func() {
			err := closer.Close()
			if err != nil {
				logger.Errorf(
					ctx,
					"unable to close the connection: %w",
					err,
				)
			}
		}, nil
}

func ptr[T any](in T) *T {
	return &in
}

func (c *Client) SubmitOAuthCode(
	ctx context.Context,
	req *streamd_grpc.SubmitOAuthCodeRequest,
) (*streamd_grpc.SubmitOAuthCodeReply, error) {
	return withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.SubmitOAuthCodeReply, error) {
		return callWrapper(
			ctx,
			c,
			client.SubmitOAuthCode,
			req,
		)
	})
}

func (c *Client) ListStreamServers(
	ctx context.Context,
) ([]api.StreamServer, error) {
	reply, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.ListStreamServersReply, error) {
		return callWrapper(
			ctx,
			c,
			client.ListStreamServers,
			&streamd_grpc.ListStreamServersRequest{},
		)
	})
	if err != nil {
		return nil, fmt.Errorf(
			"unable to request to list of the stream servers: %w",
			err,
		)
	}
	var result []api.StreamServer
	for _, server := range reply.GetStreamServers() {
		srvType, listenAddr, opts, err := goconv.StreamServerConfigGRPC2Go(
			ctx,
			server.Config,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to convert the server config: %w",
				err,
			)
		}
		result = append(result, api.StreamServer{
			Config: streamportserver.Config{
				ProtocolSpecificConfig: opts.ProtocolSpecificConfig(ctx),
				Type:                   srvType,
				ListenAddr:             listenAddr,
			},
			NumBytesConsumerWrote: uint64(
				server.GetStatistics().
					GetNumBytesConsumerWrote(),
			),
			NumBytesProducerRead: uint64(
				server.GetStatistics().
					GetNumBytesProducerRead(),
			),
		})
	}
	return result, nil
}

func (c *Client) StartStreamServer(
	ctx context.Context,
	serverType api.StreamServerType,
	listenAddr string,
	opts ...streamportserver.Option,
) error {
	cfg, err := goconv.StreamServerConfigGo2GRPC(
		ctx,
		serverType,
		listenAddr,
		opts...,
	)
	if err != nil {
		return fmt.Errorf(
			"unable to convert the server config: %w",
			err,
		)
	}

	logger.Debugf(
		ctx,
		"StartStreamServer with cfg: %#+v",
		cfg,
	)

	_, err = withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.StartStreamServerReply, error) {
		return callWrapper(
			ctx,
			c,
			client.StartStreamServer,
			&streamd_grpc.StartStreamServerRequest{
				Config: cfg,
			},
		)
	})
	if err != nil {
		return fmt.Errorf(
			"unable to request to start the stream server: %w",
			err,
		)
	}
	return nil
}

func (c *Client) StopStreamServer(
	ctx context.Context,
	listenAddr string,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.StopStreamServerReply, error) {
		return callWrapper(
			ctx,
			c,
			client.StopStreamServer,
			&streamd_grpc.StopStreamServerRequest{
				ListenAddr: listenAddr,
			},
		)
	})
	return err
}

func (c *Client) AddIncomingStream(
	ctx context.Context,
	streamID api.StreamID,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.AddIncomingStreamReply, error) {
		return callWrapper(
			ctx,
			c,
			client.AddIncomingStream,
			&streamd_grpc.AddIncomingStreamRequest{
				StreamID: string(streamID),
			},
		)
	})
	return err
}

func (c *Client) RemoveIncomingStream(
	ctx context.Context,
	streamID api.StreamID,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.RemoveIncomingStreamReply, error) {
		return callWrapper(
			ctx,
			c,
			client.RemoveIncomingStream,
			&streamd_grpc.RemoveIncomingStreamRequest{
				StreamID: string(streamID),
			},
		)
	})
	return err
}

func (c *Client) ListIncomingStreams(
	ctx context.Context,
) ([]api.IncomingStream, error) {
	reply, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.ListIncomingStreamsReply, error) {
		return callWrapper(
			ctx,
			c,
			client.ListIncomingStreams,
			&streamd_grpc.ListIncomingStreamsRequest{},
		)
	})
	if err != nil {
		return nil, fmt.Errorf(
			"unable to request to list the incoming streams: %w",
			err,
		)
	}

	var result []api.IncomingStream
	for _, stream := range reply.GetIncomingStreams() {
		result = append(result, api.IncomingStream{
			StreamID: api.StreamID(stream.GetStreamID()),
		})
	}
	return result, nil
}

func (c *Client) ListStreamDestinations(
	ctx context.Context,
) ([]api.StreamDestination, error) {
	reply, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.ListStreamDestinationsReply, error) {
		return callWrapper(
			ctx,
			c,
			client.ListStreamDestinations,
			&streamd_grpc.ListStreamDestinationsRequest{},
		)
	})
	if err != nil {
		return nil, fmt.Errorf(
			"unable to request to list the stream destinations: %w",
			err,
		)
	}

	var result []api.StreamDestination
	for _, dst := range reply.GetStreamDestinations() {
		result = append(result, api.StreamDestination{
			ID: api.DestinationID(
				dst.GetDestinationID(),
			),
			URL:       dst.GetUrl(),
			StreamKey: dst.GetStreamKey(),
		})
	}
	return result, nil
}

func (c *Client) AddStreamDestination(
	ctx context.Context,
	destinationID api.DestinationID,
	url string,
	streamKey string,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.AddStreamDestinationReply, error) {
		return callWrapper(
			ctx,
			c,
			client.AddStreamDestination,
			&streamd_grpc.AddStreamDestinationRequest{
				Config: &streamd_grpc.StreamDestination{
					DestinationID: string(destinationID),
					Url:           url,
					StreamKey:     streamKey,
				},
			},
		)
	})
	return err
}

func (c *Client) UpdateStreamDestination(
	ctx context.Context,
	destinationID api.DestinationID,
	url string,
	streamKey string,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.UpdateStreamDestinationReply, error) {
		return callWrapper(
			ctx,
			c,
			client.UpdateStreamDestination,
			&streamd_grpc.UpdateStreamDestinationRequest{
				Config: &streamd_grpc.StreamDestination{
					DestinationID: string(destinationID),
					Url:           url,
					StreamKey:     streamKey,
				},
			},
		)
	})
	return err
}

func (c *Client) RemoveStreamDestination(
	ctx context.Context,
	destinationID api.DestinationID,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.RemoveStreamDestinationReply, error) {
		return callWrapper(
			ctx,
			c,
			client.RemoveStreamDestination,
			&streamd_grpc.RemoveStreamDestinationRequest{
				DestinationID: string(destinationID),
			},
		)
	})
	return err
}

func (c *Client) ListStreamForwards(
	ctx context.Context,
) ([]api.StreamForward, error) {
	reply, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.ListStreamForwardsReply, error) {
		return callWrapper(
			ctx,
			c,
			client.ListStreamForwards,
			&streamd_grpc.ListStreamForwardsRequest{},
		)
	})
	if err != nil {
		return nil, fmt.Errorf(
			"unable to request to list the stream forwards: %w",
			err,
		)
	}

	var result []api.StreamForward
	for _, forward := range reply.GetStreamForwards() {
		item := api.StreamForward{
			Enabled: forward.Config.Enabled,
			StreamID: api.StreamID(
				forward.Config.GetStreamID(),
			),
			DestinationID: api.DestinationID(
				forward.Config.GetDestinationID(),
			),
			NumBytesWrote: uint64(
				forward.Statistics.NumBytesWrote,
			),
			NumBytesRead: uint64(
				forward.Statistics.NumBytesRead,
			),
		}
		restartUntilYoutubeRecognizesStream := forward.GetConfig().
			GetQuirks().
			GetRestartUntilYoutubeRecognizesStream()
		if restartUntilYoutubeRecognizesStream != nil {
			item.Quirks = api.StreamForwardingQuirks{
				RestartUntilYoutubeRecognizesStream: types.RestartUntilYoutubeRecognizesStream{
					Enabled: restartUntilYoutubeRecognizesStream.Enabled,
					StartTimeout: time.Duration(
						float64(
							time.Second,
						) * restartUntilYoutubeRecognizesStream.StartTimeout,
					),
					StopStartDelay: time.Duration(
						float64(
							time.Second,
						) * restartUntilYoutubeRecognizesStream.StopStartDelay,
					),
				},
				StartAfterYoutubeRecognizedStream: types.StartAfterYoutubeRecognizedStream{
					Enabled: forward.Config.Quirks.StartAfterYoutubeRecognizedStream.Enabled,
				},
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func (c *Client) AddStreamForward(
	ctx context.Context,
	streamID api.StreamID,
	destinationID api.DestinationID,
	enabled bool,
	quirks api.StreamForwardingQuirks,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.AddStreamForwardReply, error) {
		return callWrapper(
			ctx,
			c,
			client.AddStreamForward,
			&streamd_grpc.AddStreamForwardRequest{
				Config: &streamd_grpc.StreamForward{
					StreamID:      string(streamID),
					DestinationID: string(destinationID),
					Enabled:       enabled,
					Quirks: &streamd_grpc.StreamForwardQuirks{
						RestartUntilYoutubeRecognizesStream: &streamd_grpc.RestartUntilYoutubeRecognizesStream{
							Enabled:        quirks.RestartUntilYoutubeRecognizesStream.Enabled,
							StartTimeout:   quirks.RestartUntilYoutubeRecognizesStream.StartTimeout.Seconds(),
							StopStartDelay: quirks.RestartUntilYoutubeRecognizesStream.StopStartDelay.Seconds(),
						},
						StartAfterYoutubeRecognizedStream: &streamd_grpc.StartAfterYoutubeRecognizedStream{
							Enabled: quirks.StartAfterYoutubeRecognizedStream.Enabled,
						},
					},
				},
			},
		)
	})
	return err
}

func (c *Client) UpdateStreamForward(
	ctx context.Context,
	streamID api.StreamID,
	destinationID api.DestinationID,
	enabled bool,
	quirks api.StreamForwardingQuirks,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.UpdateStreamForwardReply, error) {
		return callWrapper(
			ctx,
			c,
			client.UpdateStreamForward,
			&streamd_grpc.UpdateStreamForwardRequest{
				Config: &streamd_grpc.StreamForward{
					StreamID:      string(streamID),
					DestinationID: string(destinationID),
					Enabled:       enabled,
					Quirks: &streamd_grpc.StreamForwardQuirks{
						RestartUntilYoutubeRecognizesStream: &streamd_grpc.RestartUntilYoutubeRecognizesStream{
							Enabled:        quirks.RestartUntilYoutubeRecognizesStream.Enabled,
							StartTimeout:   quirks.RestartUntilYoutubeRecognizesStream.StartTimeout.Seconds(),
							StopStartDelay: quirks.RestartUntilYoutubeRecognizesStream.StopStartDelay.Seconds(),
						},
						StartAfterYoutubeRecognizedStream: &streamd_grpc.StartAfterYoutubeRecognizedStream{
							Enabled: quirks.StartAfterYoutubeRecognizedStream.Enabled,
						},
					},
				},
			},
		)
	})
	return err
}

func (c *Client) RemoveStreamForward(
	ctx context.Context,
	streamID api.StreamID,
	destinationID api.DestinationID,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.RemoveStreamForwardReply, error) {
		return callWrapper(
			ctx,
			c,
			client.RemoveStreamForward,
			&streamd_grpc.RemoveStreamForwardRequest{
				Config: &streamd_grpc.StreamForward{
					StreamID:      string(streamID),
					DestinationID: string(destinationID),
				},
			},
		)
	})
	return err
}

func (c *Client) WaitForStreamPublisher(
	ctx context.Context,
	streamID api.StreamID,
) (<-chan struct{}, error) {
	return unwrapStreamDChan(
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_WaitForStreamPublisherClient, error) {
			return callWrapper(
				ctx,
				c,
				client.WaitForStreamPublisher,
				&streamd_grpc.WaitForStreamPublisherRequest{
					StreamID: ptr(string(streamID)),
				},
			)
		},
		func(
			ctx context.Context,
			event *streamd_grpc.StreamPublisher,
		) struct{} {
			return struct{}{}
		},
	)
}

func (c *Client) AddStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.AddStreamPlayerReply, error) {
		return callWrapper(
			ctx,
			c,
			client.AddStreamPlayer,
			&streamd_grpc.AddStreamPlayerRequest{
				Config: &streamd_grpc.StreamPlayerConfig{
					StreamID: string(streamID),
					PlayerType: goconv.StreamPlayerTypeGo2GRPC(
						playerType,
					),
					Disabled: disabled,
					StreamPlaybackConfig: goconv.StreamPlaybackConfigGo2GRPC(
						&streamPlaybackConfig,
					),
				},
			},
		)
	})
	return err
}

func (c *Client) UpdateStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
) (_err error) {
	logger.Debugf(
		ctx,
		"UpdateStreamPlayer(ctx, '%s', '%s', %v, %#+v)",
		streamID,
		playerType,
		disabled,
		streamPlaybackConfig,
	)
	defer func() {
		logger.Debugf(
			ctx,
			"/UpdateStreamPlayer(ctx, '%s', '%s', %v, %#+v): %v",
			streamID,
			playerType,
			disabled,
			streamPlaybackConfig,
			_err,
		)
	}()

	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.UpdateStreamPlayerReply, error) {
		return callWrapper(
			ctx,
			c,
			client.UpdateStreamPlayer,
			&streamd_grpc.UpdateStreamPlayerRequest{
				Config: &streamd_grpc.StreamPlayerConfig{
					StreamID: string(streamID),
					PlayerType: goconv.StreamPlayerTypeGo2GRPC(
						playerType,
					),
					Disabled: disabled,
					StreamPlaybackConfig: goconv.StreamPlaybackConfigGo2GRPC(
						&streamPlaybackConfig,
					),
				},
			},
		)
	})
	return err
}

func (c *Client) RemoveStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.RemoveStreamPlayerReply, error) {
		return callWrapper(
			ctx,
			c,
			client.RemoveStreamPlayer,
			&streamd_grpc.RemoveStreamPlayerRequest{
				StreamID: string(streamID),
			},
		)
	})
	return err
}

func (c *Client) ListStreamPlayers(
	ctx context.Context,
) ([]api.StreamPlayer, error) {
	resp, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.ListStreamPlayersReply, error) {
		return callWrapper(
			ctx,
			c,
			client.ListStreamPlayers,
			&streamd_grpc.ListStreamPlayersRequest{},
		)
	})
	if err != nil {
		return nil, fmt.Errorf("unable to query: %w", err)
	}

	result := make(
		[]api.StreamPlayer,
		0,
		len(resp.GetPlayers()),
	)
	for _, player := range resp.GetPlayers() {
		result = append(result, api.StreamPlayer{
			StreamID: streamtypes.StreamID(
				player.GetStreamID(),
			),
			PlayerType: goconv.StreamPlayerTypeGRPC2Go(
				player.PlayerType,
			),
			Disabled: player.GetDisabled(),
			StreamPlaybackConfig: goconv.StreamPlaybackConfigGRPC2Go(
				player.GetStreamPlaybackConfig(),
			),
		})
	}
	return result, nil
}

func (c *Client) GetStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (*api.StreamPlayer, error) {
	resp, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.GetStreamPlayerReply, error) {
		return callWrapper(
			ctx,
			c,
			client.GetStreamPlayer,
			&streamd_grpc.GetStreamPlayerRequest{
				StreamID: string(streamID),
			},
		)
	})
	if err != nil {
		return nil, fmt.Errorf("unable to query: %w", err)
	}

	cfg := resp.GetConfig()
	return &api.StreamPlayer{
		StreamID: streamID,
		PlayerType: goconv.StreamPlayerTypeGRPC2Go(
			cfg.PlayerType,
		),
		Disabled: cfg.GetDisabled(),
		StreamPlaybackConfig: goconv.StreamPlaybackConfigGRPC2Go(
			cfg.StreamPlaybackConfig,
		),
	}, nil
}

func (c *Client) StreamPlayerProcessTitle(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (string, error) {
	resp, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.StreamPlayerProcessTitleReply, error) {
		return callWrapper(
			ctx,
			c,
			client.StreamPlayerProcessTitle,
			&streamd_grpc.StreamPlayerProcessTitleRequest{
				StreamID: string(streamID),
			},
		)
	})
	if err != nil {
		return "", fmt.Errorf("unable to query: %w", err)
	}
	return resp.Reply.GetTitle(), nil
}

func (c *Client) StreamPlayerOpenURL(
	ctx context.Context,
	streamID streamtypes.StreamID,
	link string,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.StreamPlayerOpenReply, error) {
		return callWrapper(
			ctx,
			c,
			client.StreamPlayerOpen,
			&streamd_grpc.StreamPlayerOpenRequest{
				StreamID: string(streamID),
				Request:  &player_grpc.OpenRequest{},
			},
		)
	})
	return err
}

func (c *Client) StreamPlayerGetLink(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (string, error) {
	resp, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.StreamPlayerGetLinkReply, error) {
		return callWrapper(
			ctx,
			c,
			client.StreamPlayerGetLink,
			&streamd_grpc.StreamPlayerGetLinkRequest{
				StreamID: string(streamID),
				Request:  &player_grpc.GetLinkRequest{},
			},
		)
	})
	if err != nil {
		return "", fmt.Errorf("unable to query: %w", err)
	}
	return resp.GetReply().Link, nil
}

func (c *Client) StreamPlayerEndChan(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (<-chan struct{}, error) {
	return unwrapStreamDChan(
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_StreamPlayerEndChanClient, error) {
			return callWrapper(
				ctx,
				c,
				client.StreamPlayerEndChan,
				&streamd_grpc.StreamPlayerEndChanRequest{
					StreamID: string(streamID),
					Request:  &player_grpc.EndChanRequest{},
				},
			)
		},
		func(
			ctx context.Context,
			event *streamd_grpc.StreamPlayerEndChanReply,
		) struct{} {
			return struct{}{}
		},
	)
}

func (c *Client) StreamPlayerIsEnded(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (bool, error) {
	resp, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.StreamPlayerIsEndedReply, error) {
		return callWrapper(
			ctx,
			c,
			client.StreamPlayerIsEnded,
			&streamd_grpc.StreamPlayerIsEndedRequest{
				StreamID: string(streamID),
				Request:  &player_grpc.IsEndedRequest{},
			},
		)
	})
	if err != nil {
		return false, fmt.Errorf("unable to query: %w", err)
	}
	return resp.GetReply().IsEnded, nil
}

func (c *Client) StreamPlayerGetPosition(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (time.Duration, error) {
	resp, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.StreamPlayerGetPositionReply, error) {
		return callWrapper(
			ctx,
			c,
			client.StreamPlayerGetPosition,
			&streamd_grpc.StreamPlayerGetPositionRequest{
				StreamID: string(streamID),
				Request:  &player_grpc.GetPositionRequest{},
			},
		)
	})
	if err != nil {
		return 0, fmt.Errorf("unable to query: %w", err)
	}
	return time.Duration(
		float64(
			time.Second,
		) * resp.GetReply().
			GetPositionSecs(),
	), nil
}

func (c *Client) StreamPlayerGetLength(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (time.Duration, error) {
	resp, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.StreamPlayerGetLengthReply, error) {
		return callWrapper(
			ctx,
			c,
			client.StreamPlayerGetLength,
			&streamd_grpc.StreamPlayerGetLengthRequest{
				StreamID: string(streamID),
				Request:  &player_grpc.GetLengthRequest{},
			},
		)
	})
	if err != nil {
		return 0, fmt.Errorf("unable to query: %w", err)
	}
	return time.Duration(
		float64(
			time.Second,
		) * resp.GetReply().
			GetLengthSecs(),
	), nil
}

func (c *Client) StreamPlayerSetSpeed(
	ctx context.Context,
	streamID streamtypes.StreamID,
	speed float64,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.StreamPlayerSetSpeedReply, error) {
		return callWrapper(
			ctx,
			c,
			client.StreamPlayerSetSpeed,
			&streamd_grpc.StreamPlayerSetSpeedRequest{
				StreamID: string(streamID),
				Request: &player_grpc.SetSpeedRequest{
					Speed: speed,
				},
			},
		)
	})
	return err
}

func (c *Client) StreamPlayerSetPause(
	ctx context.Context,
	streamID streamtypes.StreamID,
	pause bool,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.StreamPlayerSetPauseReply, error) {
		return callWrapper(
			ctx,
			c,
			client.StreamPlayerSetPause,
			&streamd_grpc.StreamPlayerSetPauseRequest{
				StreamID: string(streamID),
				Request: &player_grpc.SetPauseRequest{
					IsPaused: pause,
				},
			},
		)
	})
	return err
}

func (c *Client) StreamPlayerStop(
	ctx context.Context,
	streamID streamtypes.StreamID,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.StreamPlayerStopReply, error) {
		return callWrapper(
			ctx,
			c,
			client.StreamPlayerStop,
			&streamd_grpc.StreamPlayerStopRequest{
				StreamID: string(streamID),
				Request:  &player_grpc.StopRequest{},
			},
		)
	})
	return err
}

func (c *Client) StreamPlayerClose(
	ctx context.Context,
	streamID streamtypes.StreamID,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.StreamPlayerCloseReply, error) {
		return callWrapper(
			ctx,
			c,
			client.StreamPlayerClose,
			&streamd_grpc.StreamPlayerCloseRequest{
				StreamID: string(streamID),
				Request:  &player_grpc.CloseRequest{},
			},
		)
	})
	return err
}

func (c *Client) SubscribeToConfigChanges(
	ctx context.Context,
) (<-chan api.DiffConfig, error) {
	return unwrapStreamDChan(
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_SubscribeToConfigChangesClient, error) {
			return callWrapper(
				ctx,
				c,
				client.SubscribeToConfigChanges,
				&streamd_grpc.SubscribeToConfigChangesRequest{},
			)
		},
		func(
			ctx context.Context,
			event *streamd_grpc.ConfigChange,
		) api.DiffConfig {
			return api.DiffConfig{}
		},
	)
}

func (c *Client) SubscribeToStreamsChanges(
	ctx context.Context,
) (<-chan api.DiffStreams, error) {
	return unwrapStreamDChan(
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_SubscribeToStreamsChangesClient, error) {
			return callWrapper(
				ctx,
				c,
				client.SubscribeToStreamsChanges,
				&streamd_grpc.SubscribeToStreamsChangesRequest{},
			)
		},
		func(
			ctx context.Context,
			event *streamd_grpc.StreamsChange,
		) api.DiffStreams {
			return api.DiffStreams{}
		},
	)
}

func (c *Client) SubscribeToStreamServersChanges(
	ctx context.Context,
) (<-chan api.DiffStreamServers, error) {
	return unwrapStreamDChan(
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_SubscribeToStreamServersChangesClient, error) {
			return callWrapper(
				ctx,
				c,
				client.SubscribeToStreamServersChanges,
				&streamd_grpc.SubscribeToStreamServersChangesRequest{},
			)
		},
		func(
			ctx context.Context,
			event *streamd_grpc.StreamServersChange,
		) api.DiffStreamServers {
			return api.DiffStreamServers{}
		},
	)
}

func (c *Client) SubscribeToStreamDestinationsChanges(
	ctx context.Context,
) (<-chan api.DiffStreamDestinations, error) {
	return unwrapStreamDChan(
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_SubscribeToStreamDestinationsChangesClient, error) {
			return callWrapper(
				ctx,
				c,
				client.SubscribeToStreamDestinationsChanges,
				&streamd_grpc.SubscribeToStreamDestinationsChangesRequest{},
			)
		},
		func(
			ctx context.Context,
			event *streamd_grpc.StreamDestinationsChange,
		) api.DiffStreamDestinations {
			return api.DiffStreamDestinations{}
		},
	)
}

func (c *Client) SubscribeToIncomingStreamsChanges(
	ctx context.Context,
) (<-chan api.DiffIncomingStreams, error) {
	return unwrapStreamDChan(
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_SubscribeToIncomingStreamsChangesClient, error) {
			return callWrapper(
				ctx,
				c,
				client.SubscribeToIncomingStreamsChanges,
				&streamd_grpc.SubscribeToIncomingStreamsChangesRequest{},
			)
		},
		func(
			ctx context.Context,
			event *streamd_grpc.IncomingStreamsChange,
		) api.DiffIncomingStreams {
			return api.DiffIncomingStreams{}
		},
	)
}

func (c *Client) SubscribeToStreamForwardsChanges(
	ctx context.Context,
) (<-chan api.DiffStreamForwards, error) {
	return unwrapStreamDChan(
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_SubscribeToStreamForwardsChangesClient, error) {
			return callWrapper(
				ctx,
				c,
				client.SubscribeToStreamForwardsChanges,
				&streamd_grpc.SubscribeToStreamForwardsChangesRequest{},
			)
		},
		func(
			ctx context.Context,
			event *streamd_grpc.StreamForwardsChange,
		) api.DiffStreamForwards {
			return api.DiffStreamForwards{}
		},
	)
}

func (c *Client) SubscribeToStreamPlayersChanges(
	ctx context.Context,
) (<-chan api.DiffStreamPlayers, error) {
	return unwrapStreamDChan(
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_SubscribeToStreamPlayersChangesClient, error) {
			return callWrapper(
				ctx,
				c,
				client.SubscribeToStreamPlayersChanges,
				&streamd_grpc.SubscribeToStreamPlayersChangesRequest{},
			)
		},
		func(
			ctx context.Context,
			event *streamd_grpc.StreamPlayersChange,
		) api.DiffStreamPlayers {
			return api.DiffStreamPlayers{}
		},
	)
}

func (c *Client) SetLoggingLevel(
	ctx context.Context,
	level logger.Level,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.SetLoggingLevelReply, error) {
		return callWrapper(
			ctx,
			c,
			client.SetLoggingLevel,
			&streamd_grpc.SetLoggingLevelRequest{
				LoggingLevel: goconv.LoggingLevelGo2GRPC(
					level,
				),
			},
		)
	})
	return err
}

func (c *Client) GetLoggingLevel(
	ctx context.Context,
) (logger.Level, error) {
	reply, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.GetLoggingLevelReply, error) {
		return callWrapper(
			ctx,
			c,
			client.GetLoggingLevel,
			&streamd_grpc.GetLoggingLevelRequest{},
		)
	})
	if err != nil {
		return logger.LevelUndefined, fmt.Errorf(
			"unable to get the logging level: %w",
			err,
		)
	}

	return goconv.LoggingLevelGRPC2Go(
		reply.GetLoggingLevel(),
	), nil
}

func (c *Client) AddTimer(
	ctx context.Context,
	triggerAt time.Time,
	action api.Action,
) (api.TimerID, error) {
	actionGRPC, err := goconv.ActionGo2GRPC(action)
	if err != nil {
		return 0, fmt.Errorf(
			"unable to convert the action: %w",
			err,
		)
	}
	reply, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.AddTimerReply, error) {
		return callWrapper(
			ctx,
			c,
			client.AddTimer,
			&streamd_grpc.AddTimerRequest{
				TriggerAtUnixNano: triggerAt.UnixNano(),
				Action:            actionGRPC,
			},
		)
	})
	if err != nil {
		return 0, fmt.Errorf("unable to add timer: %w", err)
	}

	return api.TimerID(reply.GetTimerID()), nil
}

func (c *Client) RemoveTimer(
	ctx context.Context,
	timerID api.TimerID,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.RemoveTimerReply, error) {
		return callWrapper(
			ctx,
			c,
			client.RemoveTimer,
			&streamd_grpc.RemoveTimerRequest{
				TimerID: int64(timerID),
			},
		)
	})
	if err != nil {
		return fmt.Errorf(
			"unable to remove the timer %d: %w",
			timerID,
			err,
		)
	}
	return nil
}

func (c *Client) ListTimers(
	ctx context.Context,
) ([]api.Timer, error) {
	reply, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.ListTimersReply, error) {
		return callWrapper(
			ctx,
			c,
			client.ListTimers,
			&streamd_grpc.ListTimersRequest{},
		)
	})
	if err != nil {
		return nil, fmt.Errorf(
			"unable to list timers: %w",
			err,
		)
	}

	timers := reply.GetTimers()
	result := make([]api.Timer, 0, len(timers))
	for _, timer := range timers {
		triggerAtUnixNano := timer.GetTriggerAtUnixNano()
		triggerAt := time.Unix(
			triggerAtUnixNano/int64(time.Second),
			triggerAtUnixNano%int64(time.Second),
		)
		action, err := goconv.ActionGRPC2Go(
			timer.GetAction(),
		)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to convert the action: %w",
				err,
			)
		}
		result = append(result, api.Timer{
			ID:        api.TimerID(timer.GetTimerID()),
			TriggerAt: triggerAt,
			Action:    action,
		})
	}
	return result, nil
}

func (c *Client) AddTriggerRule(
	ctx context.Context,
	triggerRule *api.TriggerRule,
) (api.TriggerRuleID, error) {
	triggerRuleGRPC, err := goconv.TriggerRuleGo2GRPC(triggerRule)
	if err != nil {
		return 0, fmt.Errorf("unable to convert the trigger rule %#+v: %w", triggerRule, err)
	}
	resp, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.AddTriggerRuleReply, error) {
		return callWrapper(
			ctx,
			c,
			client.AddTriggerRule,
			&streamd_grpc.AddTriggerRuleRequest{
				Rule: triggerRuleGRPC,
			},
		)
	})
	if err != nil {
		return 0, fmt.Errorf("unable to add the trigger rule %#+v: %w", triggerRule, err)
	}
	return api.TriggerRuleID(resp.GetRuleID()), nil
}

func (c *Client) UpdateTriggerRule(
	ctx context.Context,
	ruleID api.TriggerRuleID,
	triggerRule *api.TriggerRule,
) error {
	triggerRuleGRPC, err := goconv.TriggerRuleGo2GRPC(triggerRule)
	if err != nil {
		return fmt.Errorf("unable to convert the trigger rule %d:%#+v: %w", ruleID, triggerRule, err)
	}
	_, err = withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.UpdateTriggerRuleReply, error) {
		return callWrapper(
			ctx,
			c,
			client.UpdateTriggerRule,
			&streamd_grpc.UpdateTriggerRuleRequest{
				RuleID: uint64(ruleID),
				Rule:   triggerRuleGRPC,
			},
		)
	})
	if err != nil {
		return fmt.Errorf("unable to update the trigger rule %d to %#+v: %w", ruleID, triggerRule, err)
	}
	return nil
}
func (c *Client) RemoveTriggerRule(
	ctx context.Context,
	ruleID api.TriggerRuleID,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.RemoveTriggerRuleReply, error) {
		return callWrapper(
			ctx,
			c,
			client.RemoveTriggerRule,
			&streamd_grpc.RemoveTriggerRuleRequest{
				RuleID: uint64(ruleID),
			},
		)
	})
	if err != nil {
		return fmt.Errorf("unable to remove the rule %d: %w", ruleID, err)
	}
	return nil
}

func (c *Client) ListTriggerRules(
	ctx context.Context,
) (api.TriggerRules, error) {
	response, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.ListTriggerRulesReply, error) {
		return callWrapper(
			ctx,
			c,
			client.ListTriggerRules,
			&streamd_grpc.ListTriggerRulesRequest{},
		)
	})
	if err != nil {
		return nil, fmt.Errorf("unable to list the rules: %w", err)
	}

	rules := response.GetRules()
	result := make(api.TriggerRules, 0, len(rules))
	for _, ruleGRPC := range rules {
		rule, err := goconv.TriggerRuleGRPC2Go(ruleGRPC)
		if err != nil {
			return nil, fmt.Errorf("unable to convert the trigger rule %#+v: %w", rule, err)
		}
		result = append(result, rule)
	}
	return result, nil
}

func (c *Client) SubmitEvent(
	ctx context.Context,
	event event.Event,
) error {
	eventGRPC, err := goconv.EventGo2GRPC(event)
	if err != nil {
		return fmt.Errorf("unable to convert the event: %w", err)
	}
	_, err = withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.SubmitEventReply, error) {
		return callWrapper(
			ctx,
			c,
			client.SubmitEvent,
			&streamd_grpc.SubmitEventRequest{
				Event: eventGRPC,
			},
		)
	})
	if err != nil {
		return fmt.Errorf("unable to submit the event: %w", err)
	}
	return nil
}

func (c *Client) SubscribeToChatMessages(
	ctx context.Context,
) (<-chan api.ChatMessage, error) {
	return unwrapStreamDChan(
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_SubscribeToChatMessagesClient, error) {
			return callWrapper(
				ctx,
				c,
				client.SubscribeToChatMessages,
				&streamd_grpc.SubscribeToChatMessagesRequest{},
			)
		},
		func(
			ctx context.Context,
			event *streamd_grpc.ChatMessage,
		) api.ChatMessage {
			createdAtUnix := event.GetCreatedAtNano()
			return api.ChatMessage{
				ChatMessage: streamcontrol.ChatMessage{
					CreatedAt: time.Unix(
						int64(createdAtUnix)/int64(time.Second),
						(int64(createdAtUnix)%int64(time.Second))/int64(time.Nanosecond),
					),
					UserID:    streamcontrol.ChatUserID(event.GetUserID()),
					Username:  event.GetUsername(),
					MessageID: streamcontrol.ChatMessageID(event.GetMessageID()),
					Message:   event.GetMessage(),
				},
				Platform: streamcontrol.PlatformName(event.GetPlatID()),
			}
		},
	)
}

func (c *Client) RemoveChatMessage(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	msgID streamcontrol.ChatMessageID,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.RemoveChatMessageReply, error) {
		return callWrapper(
			ctx,
			c,
			client.RemoveChatMessage,
			&streamd_grpc.RemoveChatMessageRequest{
				PlatID:    string(platID),
				MessageID: string(msgID),
			},
		)
	})
	if err != nil {
		return fmt.Errorf("unable to submit the event: %w", err)
	}
	return nil
}
func (c *Client) BanUser(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	userID streamcontrol.ChatUserID,
	reason string,
	deadline time.Time,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.BanUserReply, error) {
		var deadlineNano *int64
		if !deadline.IsZero() {
			deadlineNano = ptr(int64(deadline.UnixNano()))
		}
		return callWrapper(
			ctx,
			c,
			client.BanUser,
			&streamd_grpc.BanUserRequest{
				PlatID:           string(platID),
				UserID:           string(userID),
				Reason:           reason,
				DeadlineUnixNano: deadlineNano,
			},
		)
	})
	if err != nil {
		return fmt.Errorf("unable to submit the event: %w", err)
	}
	return nil
}

func (c *Client) SendChatMessage(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	message string,
) error {
	_, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.SendChatMessageReply, error) {
		return callWrapper(
			ctx,
			c,
			client.SendChatMessage,
			&streamd_grpc.SendChatMessageRequest{
				PlatID:  string(platID),
				Message: message,
			},
		)
	})
	if err != nil {
		return fmt.Errorf("unable to submit the event: %w", err)
	}
	return nil
}

func (c *Client) DialContext(
	ctx context.Context,
	network string,
	addr string,
) (net.Conn, error) {
	conn, err := c.connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a gRPC client: %w", err)
	}

	proxyClient := proxy_grpc.NewNetworkProxyClient(conn)

	netConn, err := grpchttpproxy.NewDialer(proxyClient).DialContext(ctx, network, addr)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("unable to establish a proxied connection: %w", err)
	}
	return &proxiedConn{
		Conn:           netConn,
		grpcClientConn: conn,
	}, nil
}

type proxiedConn struct {
	net.Conn
	grpcClientConn *grpc.ClientConn
}

func (conn *proxiedConn) Close() error {
	var result *multierror.Error
	result = multierror.Append(result, conn.Conn.Close())
	result = multierror.Append(result, conn.grpcClientConn.Close())
	return result.ErrorOrNil()
}

func (c *Client) GetPeerIDs(ctx context.Context) ([]p2ptypes.PeerID, error) {
	resp, err := withStreamDClient(ctx, c, func(
		ctx context.Context,
		client streamd_grpc.StreamDClient,
		conn io.Closer,
	) (*streamd_grpc.GetPeerIDsReply, error) {
		return callWrapper(
			ctx,
			c,
			client.GetPeerIDs,
			&streamd_grpc.GetPeerIDsRequest{},
		)
	})
	if err != nil {
		return nil, fmt.Errorf("unable to submit the event: %w", err)
	}

	r := make([]p2ptypes.PeerID, 0, len(resp.GetPeerIDs()))
	for _, peerID := range resp.GetPeerIDs() {
		r = append(r, p2ptypes.PeerID(peerID))
	}
	return r, nil
}

func (c *Client) DialPeerByID(
	ctx context.Context,
	peerID p2ptypes.PeerID,
) (api.StreamD, error) {
	return nil, fmt.Errorf("not implemented, yet")
}
