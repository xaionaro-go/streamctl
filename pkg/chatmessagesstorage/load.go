package chatmessagesstorage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/xsync"
)

func (s *ChatMessagesStorage) Load(ctx context.Context) error {
	return xsync.DoA1R1(ctx, &s.Mutex, s.loadLocked, ctx)
}

func (s *ChatMessagesStorage) loadLocked(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "storeLocked(ctx)")
	defer func() { logger.Tracef(ctx, "/storeLocked(ctx): %v", _err) }()

	f, err := os.OpenFile(s.FilePath, os.O_RDONLY, 0)
	if err != nil {
		if os.IsNotExist(err) {
			s.Messages = s.Messages[:0]
			return nil
		}
		return fmt.Errorf("unable to open file '%s' for reading: %w", s.FilePath, err)
	}
	defer f.Close()

	d := json.NewDecoder(f)
	err = d.Decode(&s.Messages)
	if err != nil {
		return fmt.Errorf("unable to parse file '%s': %w", s.FilePath, err)
	}
	logger.Debugf(ctx, "loaded %d messages", len(s.Messages))
	s.sortAndDeduplicateAndTruncate(ctx)
	return nil
}
