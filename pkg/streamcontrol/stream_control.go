package streamcontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
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

func GetStreamProfile[T StreamProfile](
	ctx context.Context,
	v AbstractStreamProfile,
) (*T, error) {
	var profile T
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize: %w: %#+v", err, v)
	}
	err = json.Unmarshal(b, &profile)
	if err != nil {
		return nil, fmt.Errorf("unable to deserialize: %w: <%s>", err, b)
	}
	logger.Debugf(ctx, "converted %#+v to %#+v", v, profile)
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
		logger.Debugf(ctx, "converted %#+v to %#+v", v, profile)
	}
	return nil
}

type StreamControllerCommons interface {
	SetTitle(ctx context.Context, title string) error
	SetDescription(ctx context.Context, description string) error
	InsertAdsCuePoint(ctx context.Context, ts time.Time, duration time.Duration) error
	Flush(ctx context.Context) error
	EndStream(ctx context.Context) error
}

type StreamController[ProfileType StreamProfile] interface {
	StreamControllerCommons

	ApplyProfile(ctx context.Context, profile ProfileType, customArgs ...any) error
	StartStream(ctx context.Context, title string, description string, profile ProfileType, customArgs ...any) error
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

func (c *abstractStreamController) StreamProfileType() reflect.Type {
	return c.StreamProfileTypeValue
}

func ToAbstract[T StreamProfile](c StreamController[T]) AbstractStreamController {
	var zeroProfile T
	profileType := reflect.TypeOf(zeroProfile)
	return &abstractStreamController{
		StreamController: c,
		applyProfile: func(ctx context.Context, _profile AbstractStreamProfile, customArgs ...any) error {
			if _profile == nil {
				return nil
			}
			profile, ok := _profile.(T)
			if !ok {
				return ErrInvalidStreamProfileType{Expected: zeroProfile, Received: _profile}
			}
			return c.ApplyProfile(ctx, profile, customArgs...)
		},
		startStream: func(ctx context.Context, title string, description string, _profile AbstractStreamProfile, customArgs ...any) error {
			if _profile == nil {
				return nil
			}
			profile, ok := _profile.(T)
			if !ok {
				return ErrInvalidStreamProfileType{Expected: zeroProfile, Received: _profile}
			}
			return c.StartStream(ctx, title, description, profile, customArgs...)
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
		go func(p AbstractStreamProfile) {
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
		}(p)
	}
	go func() {
		wg.Wait()
		close(errCh)
	}()
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
	return s.concurrently(func(c AbstractStreamController) error {
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
	return s.concurrently(func(c AbstractStreamController) error {
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
	return s.concurrently(func(c AbstractStreamController) error {
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
	return s.concurrently(func(c AbstractStreamController) error {
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
	return s.concurrently(func(c AbstractStreamController) error {
		if err := c.EndStream(ctx); err != nil {
			return fmt.Errorf("StreamController %T return error: %w", c.GetImplementation(), err)
		}
		return nil
	})
}

func (s StreamControllers) Flush(
	ctx context.Context,
) error {
	return s.concurrently(func(c AbstractStreamController) error {
		if err := c.Flush(ctx); err != nil {
			return fmt.Errorf("StreamController %T return error: %w", c.GetImplementation(), err)
		}
		return nil
	})
}

func (s StreamControllers) concurrently(callback func(c AbstractStreamController) error) error {
	var wg sync.WaitGroup
	errCh := make(chan error)
	for _, c := range s {
		wg.Add(1)
		go func(c AbstractStreamController) {
			defer wg.Done()
			if err := callback(c); err != nil {
				errCh <- err
			}
		}(c)
	}
	go func() {
		wg.Wait()
		close(errCh)
	}()

	var result error
	for err := range errCh {
		result = multierror.Append(result, err)
	}
	return result
}
