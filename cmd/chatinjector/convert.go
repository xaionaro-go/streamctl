package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/youtubeapiproxy/grpc/ytgrpc"
)

const (
	timeLayout         = "2006-01-02T15:04:05-0700"
	timeLayoutFallback = time.RFC3339
	microsPerUnit      = 1_000_000
)

func convertMessage(
	ctx context.Context,
	msg *ytgrpc.LiveChatMessage,
	useRawMessage bool,
) streamcontrol.Event {
	logger.Tracef(ctx, "convertMessage")
	defer func() { logger.Tracef(ctx, "/convertMessage") }()

	ev := streamcontrol.Event{
		ID: streamcontrol.EventID(msg.GetId()),
	}

	if ad := msg.GetAuthorDetails(); ad != nil {
		ev.User = streamcontrol.User{
			ID:   streamcontrol.UserID(ad.ChannelId),
			Slug: ad.ChannelId,
			Name: strings.TrimPrefix(ad.DisplayName, "@"),
		}
	}

	snippet := msg.GetSnippet()
	if snippet == nil {
		ev.Type = streamcontrol.EventTypeOther
		return ev
	}

	ev.Type = eventTypeFromYT(snippet.Type)
	ev.CreatedAt = parsePublishedAt(ctx, snippet.PublishedAt)

	messageText := extractMessageText(snippet, useRawMessage)

	messageText = convertSnippetDetails(snippet, &ev, messageText)

	if messageText != "" {
		ev.Message = &streamcontrol.Message{
			Content: messageText,
			Format:  streamcontrol.TextFormatTypePlain,
		}
	}

	return ev
}

func extractMessageText(
	snippet *ytgrpc.LiveChatMessageSnippet,
	useRawMessage bool,
) string {
	switch {
	case useRawMessage:
		if td := snippet.GetTextMessageDetails(); td != nil && td.MessageText != "" {
			return td.MessageText
		}
		return snippet.DisplayMessage
	default:
		if snippet.DisplayMessage != "" {
			return snippet.DisplayMessage
		}
		if td := snippet.GetTextMessageDetails(); td != nil {
			return td.MessageText
		}
		return ""
	}
}

func convertSnippetDetails(
	snippet *ytgrpc.LiveChatMessageSnippet,
	ev *streamcontrol.Event,
	messageText string,
) string {
	switch snippet.Type {
	case ytgrpc.LiveChatMessageSnippet_SUPER_CHAT_EVENT:
		sc := snippet.GetSuperChatDetails()
		if sc == nil {
			break
		}
		if sc.UserComment != "" {
			messageText = sc.UserComment
		}
		ev.Paid = &streamcontrol.Money{
			Currency: currencyFromString(sc.Currency),
			Amount:   float64(sc.AmountMicros) / microsPerUnit,
		}

	case ytgrpc.LiveChatMessageSnippet_SUPER_STICKER_EVENT:
		ss := snippet.GetSuperStickerDetails()
		if ss == nil {
			break
		}
		ev.Paid = &streamcontrol.Money{
			Currency: currencyFromString(ss.Currency),
			Amount:   float64(ss.AmountMicros) / microsPerUnit,
		}
		if ss.SuperStickerMetadata != nil && messageText == "" {
			messageText = ss.SuperStickerMetadata.AltText
		}

	case ytgrpc.LiveChatMessageSnippet_FAN_FUNDING_EVENT:
		ev.Paid = &streamcontrol.Money{
			Currency: streamcontrol.CurrencyOther,
		}

	case ytgrpc.LiveChatMessageSnippet_NEW_SPONSOR_EVENT:
		ns := snippet.GetNewSponsorDetails()
		if ns == nil {
			break
		}
		tier := streamcontrol.Tier(ns.MemberLevelName)
		ev.Tier = &tier

	case ytgrpc.LiveChatMessageSnippet_MEMBER_MILESTONE_CHAT_EVENT:
		mc := snippet.GetMemberMilestoneChatDetails()
		if mc == nil {
			break
		}
		tier := streamcontrol.Tier(mc.MemberLevelName)
		ev.Tier = &tier
		if mc.UserComment != "" {
			messageText = mc.UserComment
		}

	case ytgrpc.LiveChatMessageSnippet_MEMBERSHIP_GIFTING_EVENT:
		mg := snippet.GetMembershipGiftingDetails()
		if mg == nil {
			break
		}
		tier := streamcontrol.Tier(mg.GiftMembershipsLevelName)
		ev.Tier = &tier
		if messageText == "" {
			messageText = fmt.Sprintf("gifted %d memberships", mg.GiftMembershipsCount)
		}

	case ytgrpc.LiveChatMessageSnippet_GIFT_MEMBERSHIP_RECEIVED_EVENT:
		gr := snippet.GetGiftMembershipReceivedDetails()
		if gr == nil {
			break
		}
		tier := streamcontrol.Tier(gr.MemberLevelName)
		ev.Tier = &tier

	case ytgrpc.LiveChatMessageSnippet_USER_BANNED_EVENT:
		ub := snippet.GetUserBannedDetails()
		if ub == nil || ub.BannedUserDetails == nil {
			break
		}
		ev.TargetUser = &streamcontrol.User{
			ID:   streamcontrol.UserID(ub.BannedUserDetails.ChannelId),
			Slug: ub.BannedUserDetails.ChannelId,
			Name: ub.BannedUserDetails.DisplayName,
		}

	case ytgrpc.LiveChatMessageSnippet_MESSAGE_DELETED_EVENT:
		md := snippet.GetMessageDeletedDetails()
		if md != nil && messageText == "" {
			messageText = fmt.Sprintf("deleted message %s", md.DeletedMessageId)
		}

	case ytgrpc.LiveChatMessageSnippet_MESSAGE_RETRACTED_EVENT:
		mr := snippet.GetMessageRetractedDetails()
		if mr != nil && messageText == "" {
			messageText = fmt.Sprintf("retracted message %s", mr.RetractedMessageId)
		}

	case ytgrpc.LiveChatMessageSnippet_POLL_EVENT:
		pd := snippet.GetPollDetails()
		if pd != nil && pd.Metadata != nil && messageText == "" {
			messageText = fmt.Sprintf("Poll: %s", pd.Metadata.QuestionText)
		}
	}

	return messageText
}

func eventTypeFromYT(t ytgrpc.LiveChatMessageSnippet_Type) streamcontrol.EventType {
	switch t {
	case ytgrpc.LiveChatMessageSnippet_TEXT_MESSAGE_EVENT,
		ytgrpc.LiveChatMessageSnippet_SUPER_CHAT_EVENT,
		ytgrpc.LiveChatMessageSnippet_SUPER_STICKER_EVENT,
		ytgrpc.LiveChatMessageSnippet_FAN_FUNDING_EVENT:
		return streamcontrol.EventTypeChatMessage

	case ytgrpc.LiveChatMessageSnippet_NEW_SPONSOR_EVENT:
		return streamcontrol.EventTypeSubscriptionNew

	case ytgrpc.LiveChatMessageSnippet_MEMBER_MILESTONE_CHAT_EVENT:
		return streamcontrol.EventTypeSubscriptionRenewed

	case ytgrpc.LiveChatMessageSnippet_MEMBERSHIP_GIFTING_EVENT,
		ytgrpc.LiveChatMessageSnippet_GIFT_MEMBERSHIP_RECEIVED_EVENT:
		return streamcontrol.EventTypeGiftedSubscription

	case ytgrpc.LiveChatMessageSnippet_GIFT_EVENT:
		return streamcontrol.EventTypeCheer

	case ytgrpc.LiveChatMessageSnippet_USER_BANNED_EVENT:
		return streamcontrol.EventTypeBan

	case ytgrpc.LiveChatMessageSnippet_CHAT_ENDED_EVENT:
		return streamcontrol.EventTypeStreamOffline

	default:
		return streamcontrol.EventTypeOther
	}
}

func currencyFromString(s string) streamcontrol.Currency {
	switch strings.ToUpper(s) {
	case "USD":
		return streamcontrol.CurrencyUSD
	case "EUR":
		return streamcontrol.CurrencyEUR
	case "GBP":
		return streamcontrol.CurrencyGBP
	case "JPY":
		return streamcontrol.CurrencyJPY
	default:
		return streamcontrol.CurrencyOther
	}
}

func parsePublishedAt(
	ctx context.Context,
	s string,
) time.Time {
	logger.Tracef(ctx, "parsePublishedAt")
	defer func() { logger.Tracef(ctx, "/parsePublishedAt") }()

	if s == "" {
		return time.Now()
	}

	ts, err := time.Parse(timeLayout, s)
	if err == nil {
		return ts
	}

	ts, err = time.Parse(timeLayoutFallback, s)
	if err == nil {
		return ts
	}

	logger.Warnf(ctx, "unable to parse timestamp %q, using current time", s)
	return time.Now()
}
