package chatmessagesstorage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/xsync"
)

func (s *ChatMessagesStorage) Store(ctx context.Context) error {
	return xsync.DoA1R1(ctx, &s.Mutex, s.storeLocked, ctx)
}

func (s *ChatMessagesStorage) storeLocked(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "storeLocked(ctx)")
	defer func() { logger.Tracef(ctx, "/storeLocked(ctx): %v", _err) }()

	if !s.IsChanged {
		return nil
	}
	newFilePath := s.FilePath + ".new"
	f, err := os.OpenFile(newFilePath, os.O_WRONLY|os.O_CREATE, 0640)
	if err != nil {
		return fmt.Errorf("unable to open file '%s' for writing: %w", newFilePath, err)
	}

	s.sortAndDeduplicateAndTruncate(ctx)

	d := json.NewEncoder(f)
	err = d.Encode(s.Messages)
	f.Close()
	if err != nil {
		return fmt.Errorf("unable to serialize the messages and write them to file '%s': %w", newFilePath, err)
	}

	oldFilePath := s.FilePath + ".old"
	err = os.Rename(s.FilePath, oldFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("unable to move '%s' to '%s': %w", s.FilePath, oldFilePath)
		}
	}

	err = os.Rename(newFilePath, s.FilePath)
	if err != nil {
		return fmt.Errorf("unable to move '%s' to '%s': %w", newFilePath, s.FilePath)
	}

	err = os.Remove(oldFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Errorf(ctx, "unable to delete '%s': %v", s.FilePath, err)
		}
	}
	return nil
}
