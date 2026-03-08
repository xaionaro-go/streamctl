package streamcontrol

import (
	"context"
	"fmt"
	"time"
)

type StreamCommon interface {
	fmt.Stringer

	SetStreamActive(ctx context.Context, active bool) error
	SetTitle(ctx context.Context, title string) error
	SetDescription(ctx context.Context, description string) error
	InsertAdsCuePoint(ctx context.Context, ts time.Time, duration time.Duration) error
	Flush(ctx context.Context) error
	GetStreamStatus(ctx context.Context) (*StreamStatus, error)
}
