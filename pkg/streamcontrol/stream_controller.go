package streamcontrol

import (
	"context"
	"fmt"
	"time"
)

type streamController[ProfileType StreamProfile] struct {
	AccountGeneric[ProfileType]
	StreamID StreamID
}

func (s *streamController[ProfileType]) String() string {
	return fmt.Sprintf("%s:%s", s.AccountGeneric, s.StreamID)
}

func (s *streamController[ProfileType]) GetStreamStatus(
	ctx context.Context,
) (*StreamStatus, error) {
	return s.AccountGeneric.GetStreamStatus(ctx, s.StreamID)
}

func (s *streamController[ProfileType]) SetStreamActive(
	ctx context.Context,
	active bool,
) error {
	return s.AccountGeneric.SetStreamActive(ctx, s.StreamID, active)
}

func (s *streamController[ProfileType]) SetTitle(
	ctx context.Context,
	title string,
) error {
	return s.AccountGeneric.SetTitle(ctx, s.StreamID, title)
}

func (s *streamController[ProfileType]) SetDescription(
	ctx context.Context,
	description string,
) error {
	return s.AccountGeneric.SetDescription(ctx, s.StreamID, description)
}

func (s *streamController[ProfileType]) ApplyProfile(
	ctx context.Context,
	profile ProfileType,
	customArgs ...any,
) error {
	return s.AccountGeneric.ApplyProfile(ctx, s.StreamID, profile, customArgs...)
}

func (s *streamController[ProfileType]) InsertAdsCuePoint(
	ctx context.Context,
	ts time.Time,
	duration time.Duration,
) error {
	return s.AccountGeneric.InsertAdsCuePoint(ctx, s.StreamID, ts, duration)
}

func (s *streamController[ProfileType]) Flush(
	ctx context.Context,
) error {
	return s.AccountGeneric.Flush(ctx, s.StreamID)
}
