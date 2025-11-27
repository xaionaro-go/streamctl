package streampanel

import (
	"context"
)

type backgroundRenderer struct {
	panel *Panel
	Q     chan func()
}

func newBackgroundRenderer(
	ctx context.Context,
	panel *Panel,
) *backgroundRenderer {
	r := &backgroundRenderer{
		panel: panel,
		Q:     make(chan func(), 1024),
	}
	go r.run(ctx)
	return r
}

func (r *backgroundRenderer) run(
	ctx context.Context,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case f := <-r.Q:
			r.panel.app.Driver().DoFromGoroutine(f, false)
		}
	}
}
