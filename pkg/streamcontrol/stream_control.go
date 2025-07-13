package streamcontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/observability"
)

type StreamProfileBase struct {
	Parent ProfileName
	Order  int
}

func (profile StreamProfileBase) GetParent() (ProfileName, bool) {
	if profile.Parent == "" {
		return "", false
	}
	return profile.Parent, true
}

func (profile StreamProfileBase) GetOrder() int {
	return profile.Order
}

func (profile *StreamProfileBase) SetOrder(v int) {
	profile.Order = v
}

type AbstractStreamProfile interface {
	GetParent() (ProfileName, bool)
	GetOrder() int
}

type StreamProfile interface {
	AbstractStreamProfile
}

func AssertStreamProfile[T StreamProfile](
	ctx context.Context,
	v AbstractStreamProfile,
) (*T, error) {
	profile, ok := v.(T)
	if ok {
		return &profile, nil
	}

	profilePtr, ok := (any)(v).(*T)
	if ok {
		return profilePtr, nil
	}

	var zeroProfile T
	return nil, ErrInvalidStreamProfileType{Expected: zeroProfile, Received: v}
}

func GetStreamProfile[T StreamProfile](
	ctx context.Context,
	v AbstractStreamProfile,
) (*T, error) {
	profilePtr, err := AssertStreamProfile[T](ctx, v)
	if err == nil {
		return profilePtr, nil
	}

	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize: %w: %#+v", err, v)
	}
	logger.Debugf(ctx, "JSON representation: <%s>", b)
	var profile T
	err = json.Unmarshal(b, &profile)
	if err != nil {
		return nil, fmt.Errorf("unable to deserialize: %w: <%s>", err, b)
	}
	logger.Tracef(ctx, "converted %#+v (%s) to %#+v", v, v, profile)
	return &profile, nil
}

func ConvertStreamProfiles[T StreamProfile](
	ctx context.Context,
	m map[ProfileName]AbstractStreamProfile,
) error {
	for k, v := range m {
		profile, err := GetStreamProfile[T](ctx, v)
		if err != nil {
			return err
		}
		m[k] = *profile
		logger.Tracef(ctx, "converted %#+v (%s) to %#+v", v, v, profile)
	}
	return nil
}

type StreamStatus struct {
	IsActive     bool
	ViewersCount *uint      `json:",omitempty"`
	StartedAt    *time.Time `json:",omitempty"`
	CustomData   any        `json:",omitempty"`
}

type Currency int

const (
	CurrencyNone = Currency(iota)
	CurrencyBits
	CurrencyUSD
	CurrencyOther
)

type Money struct {
	Currency Currency
	Amount   float64
}

type StreamControllerCommons interface {
	io.Closer

	SetTitle(ctx context.Context, title string) error
	SetDescription(ctx context.Context, description string) error
	InsertAdsCuePoint(ctx context.Context, ts time.Time, duration time.Duration) error
	Flush(ctx context.Context) error
	EndStream(ctx context.Context) error
	GetStreamStatus(ctx context.Context) (*StreamStatus, error)

	GetChatMessagesChan(ctx context.Context) (<-chan ChatMessage, error)
	SendChatMessage(ctx context.Context, message string) error
	RemoveChatMessage(ctx context.Context, messageID ChatMessageID) error
	BanUser(ctx context.Context, userID ChatUserID, reason string, deadline time.Time) error

	IsCapable(context.Context, Capability) bool

	IsChannelStreaming(ctx context.Context, chanID ChatUserID) (bool, error)
	Shoutout(ctx context.Context, chanID ChatUserID) error
	RaidTo(ctx context.Context, chanID ChatUserID) error
}

type StreamController[ProfileType StreamProfile] interface {
	StreamControllerCommons

	ApplyProfile(ctx context.Context, profile ProfileType, customArgs ...any) error
	StartStream(
		ctx context.Context,
		title string,
		description string,
		profile ProfileType,
		customArgs ...any,
	) error
}

type AbstractStreamController interface {
	StreamController[AbstractStreamProfile]
	GetImplementation() StreamControllerCommons
	StreamProfileType() reflect.Type
}

type abstractStreamController struct {
	StreamController       StreamControllerCommons
	applyProfile           func(ctx context.Context, profile AbstractStreamProfile, customArgs ...any) error
	startStream            func(ctx context.Context, title string, description string, profile AbstractStreamProfile, customArgs ...any) error
	StreamProfileTypeValue reflect.Type
}

func (c *abstractStreamController) Close() error {
	return c.StreamController.Close()
}

func (c *abstractStreamController) GetImplementation() StreamControllerCommons {
	return c.StreamController
}

func (c *abstractStreamController) ApplyProfile(
	ctx context.Context,
	profile AbstractStreamProfile,
	customArgs ...any,
) error {
	return c.applyProfile(ctx, profile, customArgs...)
}

func (c *abstractStreamController) SetTitle(
	ctx context.Context,
	title string,
) error {
	return c.GetImplementation().SetTitle(ctx, title)
}

func (c *abstractStreamController) SetDescription(
	ctx context.Context,
	description string,
) error {
	return c.GetImplementation().SetDescription(ctx, description)
}

func (c *abstractStreamController) InsertAdsCuePoint(
	ctx context.Context,
	ts time.Time,
	duration time.Duration,
) error {
	return c.GetImplementation().InsertAdsCuePoint(ctx, ts, duration)
}

func (c *abstractStreamController) Flush(
	ctx context.Context,
) error {
	return c.GetImplementation().Flush(ctx)
}

func (c *abstractStreamController) StartStream(
	ctx context.Context,
	title string,
	description string,
	profile AbstractStreamProfile,
	customArgs ...any,
) error {
	return c.startStream(ctx, title, description, profile, customArgs...)
}

func (c *abstractStreamController) EndStream(
	ctx context.Context,
) error {
	return c.GetImplementation().EndStream(ctx)
}

func (c *abstractStreamController) GetStreamStatus(
	ctx context.Context,
) (*StreamStatus, error) {
	return c.GetImplementation().GetStreamStatus(ctx)
}

func (c *abstractStreamController) StreamProfileType() reflect.Type {
	return c.StreamProfileTypeValue
}

func (c *abstractStreamController) GetChatMessagesChan(ctx context.Context) (<-chan ChatMessage, error) {
	return c.StreamController.GetChatMessagesChan(ctx)
}
func (c *abstractStreamController) SendChatMessage(ctx context.Context, message string) error {
	return c.StreamController.SendChatMessage(ctx, message)
}
func (c *abstractStreamController) RemoveChatMessage(ctx context.Context, messageID ChatMessageID) error {
	return c.StreamController.RemoveChatMessage(ctx, messageID)
}
func (c *abstractStreamController) BanUser(ctx context.Context, userID ChatUserID, reason string, deadline time.Time) error {
	return c.StreamController.BanUser(ctx, userID, reason, deadline)
}
func (c *abstractStreamController) IsCapable(ctx context.Context, cap Capability) bool {
	return c.StreamController.IsCapable(ctx, cap)
}

func (c *abstractStreamController) IsChannelStreaming(ctx context.Context, chanID ChatUserID) (bool, error) {
	return c.StreamController.IsChannelStreaming(ctx, chanID)
}
func (c *abstractStreamController) Shoutout(ctx context.Context, chanID ChatUserID) error {
	return c.StreamController.Shoutout(ctx, chanID)
}
func (c *abstractStreamController) RaidTo(ctx context.Context, chanID ChatUserID) error {
	return c.StreamController.RaidTo(ctx, chanID)
}

func ToAbstract[T StreamProfile](c StreamController[T]) AbstractStreamController {
	if c == nil {
		return nil
	}
	var zeroProfile T
	profileType := reflect.TypeOf(zeroProfile)
	return &abstractStreamController{
		StreamController: c,
		applyProfile: func(ctx context.Context, _profile AbstractStreamProfile, customArgs ...any) error {
			profile, err := AssertStreamProfile[T](ctx, _profile)
			if err != nil {
				return err
			}
			return c.ApplyProfile(ctx, *profile, customArgs...)
		},
		startStream: func(ctx context.Context, title string, description string, _profile AbstractStreamProfile, customArgs ...any) error {
			profile, err := AssertStreamProfile[T](ctx, _profile)
			if err != nil {
				return err
			}
			return c.StartStream(ctx, title, description, *profile, customArgs...)
		},
		StreamProfileTypeValue: profileType,
	}
}

type StreamControllers []AbstractStreamController

func (s StreamControllers) ApplyProfiles(
	ctx context.Context,
	profiles []AbstractStreamProfile,
) error {
	m := map[reflect.Type]AbstractStreamController{}
	for _, c := range s {
		m[c.StreamProfileType()] = c
	}
	var wg sync.WaitGroup
	errCh := make(chan error)
	for _, p := range profiles {
		wg.Add(1)
		{
			p := p
			observability.Go(ctx, func(ctx context.Context) {
				defer wg.Done()
				profileType := reflect.TypeOf(p)
				c, ok := m[profileType]
				if !ok {
					errCh <- ErrNoStreamControllerForProfile{StreamProfile: p}
					return
				}
				if err := c.ApplyProfile(ctx, p); err != nil {
					errCh <- fmt.Errorf("StreamController %T return error: %w", c.GetImplementation(), err)
					return
				}
			})
		}
	}
	observability.Go(ctx, func(ctx context.Context) {
		wg.Wait()
		close(errCh)
	})
	var result error
	for err := range errCh {
		result = multierror.Append(result, err)
	}
	return result
}

func (s StreamControllers) SetTitle(
	ctx context.Context,
	title string,
) error {
	return s.concurrently(ctx, func(c AbstractStreamController) error {
		err := c.SetTitle(ctx, title)
		logger.Debugf(ctx, "SetTitle: %T: <%s>: %v", c.GetImplementation(), title, err)
		if err != nil {
			return fmt.Errorf("StreamController %T return error: %w", c.GetImplementation(), err)
		}
		return nil
	})
}

func (s StreamControllers) SetDescription(
	ctx context.Context,
	description string,
) error {
	return s.concurrently(ctx, func(c AbstractStreamController) error {
		logger.Debugf(ctx, "SetDescription: %T: <%s>", c.GetImplementation(), description)
		if err := c.SetDescription(ctx, description); err != nil {
			return fmt.Errorf("StreamController %T return error: %w", c.GetImplementation(), err)
		}
		return nil
	})
}

func (s StreamControllers) InsertAdsCuePoint(
	ctx context.Context,
	ts time.Time,
	duration time.Duration,
) error {
	return s.concurrently(ctx, func(c AbstractStreamController) error {
		if err := c.InsertAdsCuePoint(ctx, ts, duration); err != nil {
			return fmt.Errorf("StreamController %T return error: %w", c.GetImplementation(), err)
		}
		return nil
	})
}

func (s StreamControllers) StartStream(
	ctx context.Context,
	title string,
	description string,
	profiles []AbstractStreamProfile,
	customArgs ...any,
) error {
	m := map[reflect.Type]AbstractStreamProfile{}
	for _, p := range profiles {
		m[reflect.TypeOf(p)] = p
	}
	return s.concurrently(ctx, func(c AbstractStreamController) error {
		profile := m[c.StreamProfileType()]
		logger.Debugf(ctx, "profile == %#+v", profile)
		if err := c.StartStream(ctx, title, description, profile, customArgs...); err != nil {
			return fmt.Errorf("StreamController %T return error: %w", c.GetImplementation(), err)
		}
		return nil
	})
}

func (s StreamControllers) EndStream(
	ctx context.Context,
) error {
	return s.concurrently(ctx, func(c AbstractStreamController) error {
		if err := c.EndStream(ctx); err != nil {
			return fmt.Errorf("StreamController %T return error: %w", c.GetImplementation(), err)
		}
		return nil
	})
}

func (s StreamControllers) Flush(
	ctx context.Context,
) error {
	return s.concurrently(ctx, func(c AbstractStreamController) error {
		if err := c.Flush(ctx); err != nil {
			return fmt.Errorf("StreamController %T return error: %w", c.GetImplementation(), err)
		}
		return nil
	})
}

func (s StreamControllers) concurrently(
	ctx context.Context,
	callback func(c AbstractStreamController) error,
) error {
	var wg sync.WaitGroup
	errCh := make(chan error)
	for _, c := range s {
		wg.Add(1)
		{
			c := c
			observability.Go(ctx, func(ctx context.Context) {
				defer wg.Done()
				if err := callback(c); err != nil {
					errCh <- err
				}
			})
		}
	}
	observability.Go(ctx, func(ctx context.Context) {
		wg.Wait()
		close(errCh)
	})

	var result error
	for err := range errCh {
		result = multierror.Append(result, err)
	}
	return result
}
