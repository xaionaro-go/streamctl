package youtube

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

const (
	// After the first message from the primary source, if no further
	// messages arrive within this duration, we log an error. Uses a
	// long timeout to avoid false positives on quiet streams.
	primaryHealthTimeout = 60 * time.Second

	// Maximum number of message IDs to track for deduplication.
	dedupCacheCapacity = 1000
)

// RedundantChatListener runs two chat sources simultaneously (Data API v3
// as primary, InnerTube scraper as secondary) and merges their output with
// deduplication. Primary events take priority: when both sources report the
// same message, the primary's copy is used.
type RedundantChatListener struct {
	videoID    string
	cancelFunc context.CancelFunc

	primary   chatListener
	secondary chatListener

	primaryStarted atomic.Bool

	messagesOutChan chan streamcontrol.Event

	wg sync.WaitGroup

	seenIDs *dedupCache
}

func NewRedundantChatListener(
	ctx context.Context,
	ytClient ChatClient,
	videoID string,
	chatID string,
	onClose func(context.Context, *RedundantChatListener),
) (_ *RedundantChatListener, _err error) {
	logger.Debugf(ctx, "NewRedundantChatListener(ctx, ..., '%s', '%s')", videoID, chatID)
	defer func() { logger.Debugf(ctx, "/NewRedundantChatListener: %v", _err) }()

	var primary chatListener
	var secondary chatListener
	var errs []error

	apiListener, err := NewChatListener(ctx, ytClient, videoID, chatID)
	switch {
	case err == nil:
		primary = apiListener
	default:
		logger.Errorf(ctx, "primary chat listener (Data API) failed for '%s': %v", videoID, err)
		errs = append(errs, err)
	}

	obsoleteListener, err := NewChatListenerOBSOLETE(ctx, videoID, nil)
	switch {
	case err == nil:
		secondary = obsoleteListener
	default:
		logger.Warnf(ctx, "secondary chat listener (InnerTube) failed for '%s': %v", videoID, err)
		errs = append(errs, err)
	}

	if primary == nil && secondary == nil {
		return nil, errors.Join(errs...)
	}

	ctx, cancelFunc := context.WithCancel(ctx)
	l := &RedundantChatListener{
		videoID:         videoID,
		cancelFunc:      cancelFunc,
		primary:         primary,
		secondary:       secondary,
		messagesOutChan: make(chan streamcontrol.Event, 100),
		seenIDs:         newDedupCache(dedupCacheCapacity),
	}

	l.wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer l.wg.Done()
		if onClose != nil {
			defer onClose(ctx, l)
		}
		defer close(l.messagesOutChan)
		l.run(ctx)
	})

	return l, nil
}

func (l *RedundantChatListener) run(ctx context.Context) {
	logger.Debugf(ctx, "RedundantChatListener.run")
	defer func() { logger.Debugf(ctx, "/RedundantChatListener.run") }()

	emit := func(ctx context.Context, ev streamcontrol.Event) {
		select {
		case <-ctx.Done():
		case l.messagesOutChan <- ev:
		default:
			logger.Errorf(ctx, "redundant chat listener: message queue overflow, dropping message %s", ev.ID)
		}
	}

	// sourceReader reads from a chat source and forwards new messages.
	// First source to deliver a given message ID wins; duplicates from
	// the other source are silently dropped.
	sourceReader := func(ctx context.Context, source chatListener, name string) {
		if source == nil {
			return
		}
		ch := source.MessagesChan()

		var lastMessageTime time.Time
		var healthAlerted bool
		isPrimary := source == l.primary

		// Reusable timer avoids allocating a new timer on every loop
		// iteration (time.After leaks until it fires).
		healthTimer := time.NewTimer(primaryHealthTimeout)
		healthTimer.Stop()

		for {
			if isPrimary && l.primaryStarted.Load() {
				healthTimer.Reset(primaryHealthTimeout)
			}

			select {
			case <-ctx.Done():
				healthTimer.Stop()
				return
			case <-healthTimer.C:
				if !healthAlerted {
					healthAlerted = true
					logger.Errorf(
						ctx,
						"primary chat listener (Data API) stopped producing messages for video '%s' — running on secondary (InnerTube) only; last message at %v",
						l.videoID,
						lastMessageTime,
					)
				}
				continue
			case ev, ok := <-ch:
				if !ok {
					logger.Debugf(ctx, "%s chat listener channel closed for '%s'", name, l.videoID)
					healthTimer.Stop()
					return
				}

				if isPrimary {
					if healthAlerted {
						logger.Errorf(ctx, "primary chat listener (Data API) recovered for video '%s'", l.videoID)
						healthAlerted = false
					}
					lastMessageTime = time.Now()
					l.primaryStarted.Store(true)
				}

				if !l.seenIDs.Add(ev.ID) {
					continue
				}

				emit(ctx, ev)
			}
		}
	}

	var wg sync.WaitGroup

	startWorker := func(ctx context.Context, source chatListener, name string) {
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			logger.Debugf(ctx, "RedundantChatListener: %s started", name)
			defer func() { logger.Debugf(ctx, "RedundantChatListener: %s stopped", name) }()
			sourceReader(ctx, source, name)
		})
	}

	startWorker(ctx, l.primary, "primary")
	startWorker(ctx, l.secondary, "secondary")

	wg.Wait()
}

func (l *RedundantChatListener) Close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "RedundantChatListener.Close")
	defer func() { logger.Debugf(ctx, "/RedundantChatListener.Close: %v", _err) }()

	l.cancelFunc()

	var errs []error
	if l.primary != nil {
		if err := l.primary.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if l.secondary != nil {
		if err := l.secondary.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (l *RedundantChatListener) MessagesChan() <-chan streamcontrol.Event {
	return l.messagesOutChan
}

func (l *RedundantChatListener) GetVideoID() string {
	return l.videoID
}

// dedupCache is a bounded, thread-safe cache of recently seen event IDs.
// It uses a ring buffer to evict the oldest entries when capacity is reached.
type dedupCache struct {
	mu       sync.Mutex
	seen     map[streamcontrol.EventID]struct{}
	ring     []streamcontrol.EventID
	pos      int
	capacity int
}

func newDedupCache(capacity int) *dedupCache {
	return &dedupCache{
		seen:     make(map[streamcontrol.EventID]struct{}, capacity),
		ring:     make([]streamcontrol.EventID, capacity),
		capacity: capacity,
	}
}

// Add inserts an ID into the cache. Returns true if the ID was new (not
// previously seen), false if it was a duplicate.
func (c *dedupCache) Add(id streamcontrol.EventID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.seen[id]; exists {
		return false
	}

	// Evict oldest if at capacity.
	if old := c.ring[c.pos]; old != "" {
		delete(c.seen, old)
	}
	c.ring[c.pos] = id
	c.seen[id] = struct{}{}
	c.pos = (c.pos + 1) % c.capacity
	return true
}

// Contains checks if an ID is in the cache without adding it.
func (c *dedupCache) Contains(id streamcontrol.EventID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, exists := c.seen[id]
	return exists
}
