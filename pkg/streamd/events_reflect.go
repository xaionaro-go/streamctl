package streamd

import (
	"context"
	"reflect"

	"github.com/facebookincubator/go-belt/tool/logger"
)

type Event interface{}

func eventTopic(
	event Event,
) string {
	t := reflect.ValueOf(event).Type()
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}

func (d *StreamD) publishEvent(
	ctx context.Context,
	event Event,
) {
	topic := eventTopic(event)
	logger.Debugf(ctx, "publishEvent(ctx, %#+v): %s", event, topic)
	defer logger.Debugf(ctx, "/publishEvent(ctx, %#+v): %s", event, topic)
	d.EventBus.Publish(topic, event)
}
