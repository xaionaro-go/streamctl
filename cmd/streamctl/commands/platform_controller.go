package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type PlatformController struct {
	Accounts map[streamcontrol.AccountID]streamcontrol.AbstractAccount
}

func (c *PlatformController) getAccount(accountID streamcontrol.AccountID) (streamcontrol.AbstractAccount, error) {
	acc, ok := c.Accounts[accountID]
	if !ok {
		return nil, fmt.Errorf("account '%s' not found", accountID)
	}
	return acc, nil
}

func (c *PlatformController) StartStream(
	ctx context.Context,
	streamID streamcontrol.StreamIDFullyQualified,
	profile streamcontrol.StreamProfile,
	customArgs ...any,
) error {
	err := c.ApplyProfile(ctx, streamID, profile, customArgs...)
	if err != nil {
		return fmt.Errorf("unable to apply profile to %s: %w", streamID, err)
	}
	err = c.SetStreamActive(ctx, streamID, true)
	if err != nil {
		return fmt.Errorf("unable to start stream %s: %w", streamID, err)
	}
	return nil
}

func (c *PlatformController) EndStream(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified) error {
	return c.SetStreamActive(ctx, streamID, false)
}

func (c *PlatformController) SetTitle(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified, title string) error {
	acc, err := c.getAccount(streamID.AccountID)
	if err != nil {
		return err
	}
	return acc.SetTitle(ctx, streamID.StreamID, title)
}

func (c *PlatformController) SetDescription(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified, description string) error {
	acc, err := c.getAccount(streamID.AccountID)
	if err != nil {
		return err
	}
	return acc.SetDescription(ctx, streamID.StreamID, description)
}

func (c *PlatformController) SetStreamActive(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified, isActive bool) error {
	acc, err := c.getAccount(streamID.AccountID)
	if err != nil {
		return err
	}
	return acc.SetStreamActive(ctx, streamID.StreamID, isActive)
}

func (c *PlatformController) Flush(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified) error {
	acc, err := c.getAccount(streamID.AccountID)
	if err != nil {
		return err
	}
	return acc.Flush(ctx, streamID.StreamID)
}

func (c *PlatformController) GetStreamStatus(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified) (*streamcontrol.StreamStatus, error) {
	acc, err := c.getAccount(streamID.AccountID)
	if err != nil {
		return nil, err
	}
	return acc.GetStreamStatus(ctx, streamID.StreamID)
}

func (c *PlatformController) GetChatMessagesChan(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified) (<-chan streamcontrol.Event, error) {
	acc, err := c.getAccount(streamID.AccountID)
	if err != nil {
		return nil, err
	}
	return acc.GetChatMessagesChan(ctx, streamID.StreamID)
}

func (c *PlatformController) SendChatMessage(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified, message string) error {
	acc, err := c.getAccount(streamID.AccountID)
	if err != nil {
		return err
	}
	return acc.SendChatMessage(ctx, streamID.StreamID, message)
}

func (c *PlatformController) RemoveChatMessage(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified, messageID streamcontrol.EventID) error {
	acc, err := c.getAccount(streamID.AccountID)
	if err != nil {
		return err
	}
	return acc.RemoveChatMessage(ctx, streamID.StreamID, messageID)
}

func (c *PlatformController) BanUser(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified, userID streamcontrol.UserID, reason string, deadline time.Time) error {
	acc, err := c.getAccount(streamID.AccountID)
	if err != nil {
		return err
	}
	return acc.BanUser(ctx, streamID.StreamID, userID, reason, deadline)
}

func (c *PlatformController) Shoutout(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified, chanID streamcontrol.UserID) error {
	acc, err := c.getAccount(streamID.AccountID)
	if err != nil {
		return err
	}
	return acc.Shoutout(ctx, streamID.StreamID, chanID)
}

func (c *PlatformController) RaidTo(ctx context.Context, streamID streamcontrol.StreamIDFullyQualified, chanID streamcontrol.UserID) error {
	acc, err := c.getAccount(streamID.AccountID)
	if err != nil {
		return err
	}
	return acc.RaidTo(ctx, streamID.StreamID, chanID)
}

func (c *PlatformController) ApplyProfile(
	ctx context.Context,
	streamID streamcontrol.StreamIDFullyQualified,
	profile streamcontrol.StreamProfile,
	customArgs ...any,
) error {
	acc, err := c.getAccount(streamID.AccountID)
	if err != nil {
		return err
	}
	return acc.ApplyProfile(ctx, streamID.StreamID, profile, customArgs...)
}
