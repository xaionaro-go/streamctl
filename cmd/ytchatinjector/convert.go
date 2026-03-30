package main

import (
	"fmt"
	"time"

	"github.com/xaionaro-go/chatwebhook/pkg/grpc/protobuf/go/chatwebhook_grpc"
	"github.com/xaionaro-go/youtubeapiproxy/grpc/ytgrpc"
)

func convertLiveChatMessage(
	msg *ytgrpc.LiveChatMessage,
	useRawMessage bool,
) (*chatwebhook_grpc.Event, error) {
	snippet := msg.GetSnippet()
	if snippet == nil {
		return nil, fmt.Errorf("message has no snippet")
	}

	createdAtNano, err := parsePublishedAt(snippet.GetPublishedAt())
	if err != nil {
		return nil, fmt.Errorf("unable to parse published_at %q: %w", snippet.GetPublishedAt(), err)
	}

	eventType := convertSnippetType(snippet.GetType())

	var messageText string
	switch {
	case useRawMessage:
		if td := snippet.GetTextMessageDetails(); td != nil {
			messageText = td.GetMessageText()
		}
		if messageText == "" {
			messageText = snippet.GetDisplayMessage()
		}
	default:
		messageText = snippet.GetDisplayMessage()
		if messageText == "" {
			if td := snippet.GetTextMessageDetails(); td != nil {
				messageText = td.GetMessageText()
			}
		}
	}

	ev := &chatwebhook_grpc.Event{
		Id:                msg.GetId(),
		CreatedAtUNIXNano: createdAtNano,
		EventType:         eventType,
		User:              convertAuthor(msg.GetAuthorDetails()),
		Message: &chatwebhook_grpc.Message{
			Content:    messageText,
			FormatType: chatwebhook_grpc.TextFormatType_TEXT_FORMAT_TYPE_PLAIN,
		},
	}

	if sc := snippet.GetSuperChatDetails(); sc != nil {
		ev.Money = convertSuperChatMoney(sc)
		if comment := sc.GetUserComment(); comment != "" {
			ev.Message.Content = comment
		}
	}

	if ss := snippet.GetSuperStickerDetails(); ss != nil {
		ev.Money = convertSuperStickerMoney(ss)
	}

	return ev, nil
}

func parsePublishedAt(raw string) (uint64, error) {
	if raw == "" {
		return uint64(time.Now().UnixNano()), nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t, err = time.Parse(time.RFC3339, raw)
	}
	if err != nil {
		return 0, fmt.Errorf("unable to parse %q: %w", raw, err)
	}
	return uint64(t.UnixNano()), nil
}

func convertSnippetType(
	t ytgrpc.LiveChatMessageSnippet_Type,
) chatwebhook_grpc.PlatformEventType {
	switch t {
	case ytgrpc.LiveChatMessageSnippet_TEXT_MESSAGE_EVENT:
		return chatwebhook_grpc.PlatformEventType_platformEventTypeChatMessage
	case ytgrpc.LiveChatMessageSnippet_SUPER_CHAT_EVENT:
		return chatwebhook_grpc.PlatformEventType_platformEventTypeCheer
	case ytgrpc.LiveChatMessageSnippet_SUPER_STICKER_EVENT:
		return chatwebhook_grpc.PlatformEventType_platformEventTypeCheer
	case ytgrpc.LiveChatMessageSnippet_NEW_SPONSOR_EVENT:
		return chatwebhook_grpc.PlatformEventType_platformEventTypeSubscriptionNew
	case ytgrpc.LiveChatMessageSnippet_MEMBER_MILESTONE_CHAT_EVENT:
		return chatwebhook_grpc.PlatformEventType_platformEventTypeSubscriptionRenewed
	case ytgrpc.LiveChatMessageSnippet_MEMBERSHIP_GIFTING_EVENT:
		return chatwebhook_grpc.PlatformEventType_platformEventTypeGiftedSubscription
	case ytgrpc.LiveChatMessageSnippet_GIFT_MEMBERSHIP_RECEIVED_EVENT:
		return chatwebhook_grpc.PlatformEventType_platformEventTypeGiftedSubscription
	case ytgrpc.LiveChatMessageSnippet_USER_BANNED_EVENT:
		return chatwebhook_grpc.PlatformEventType_platformEventTypeBan
	default:
		return chatwebhook_grpc.PlatformEventType_platformEventTypeOther
	}
}

func convertAuthor(
	author *ytgrpc.LiveChatMessageAuthorDetails,
) *chatwebhook_grpc.User {
	if author == nil {
		return &chatwebhook_grpc.User{}
	}
	return &chatwebhook_grpc.User{
		Id:   author.GetChannelId(),
		Slug: author.GetChannelId(),
		Name: author.GetDisplayName(),
	}
}

func convertSuperChatMoney(
	sc *ytgrpc.LiveChatSuperChatDetails,
) *chatwebhook_grpc.Money {
	return &chatwebhook_grpc.Money{
		Amount: float64(sc.GetAmountMicros()) / 1_000_000.0,
	}
}

func convertSuperStickerMoney(
	ss *ytgrpc.LiveChatSuperStickerDetails,
) *chatwebhook_grpc.Money {
	return &chatwebhook_grpc.Money{
		Amount: float64(ss.GetAmountMicros()) / 1_000_000.0,
	}
}
