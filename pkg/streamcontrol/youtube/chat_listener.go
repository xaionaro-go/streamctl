package youtube

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/grpc/ytgrpc"
	"golang.org/x/oauth2"
	"google.golang.org/api/youtube/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	grpcoauth "google.golang.org/grpc/credentials/oauth"
	"google.golang.org/grpc/status"
)

const (
	youtubeGRPCHost      = "youtube.googleapis.com:443"
	streamReconnectDelay = 5 * time.Second
	maxReconnectDelay    = 60 * time.Second

	highQuotaReconnectDelay = 60 * time.Second
	highQuotaUsageThreshold = 5000
)

type QuotaTracker interface {
	ReportQuotaConsumption(ctx context.Context, points uint)
	UsedQuotaPoints() uint64
}

type ChatListener struct {
	videoID         string
	liveChatID      string
	tokenSource     oauth2.TokenSource
	quotaTracker    QuotaTracker
	wg              sync.WaitGroup
	cancelFunc      context.CancelFunc
	messagesOutChan chan streamcontrol.Event
}

type ChatClient interface {
	GetLiveChatMessages(ctx context.Context, chatID string, pageToken string, parts []string) (*youtube.LiveChatMessageListResponse, error)
}

func NewChatListener(
	ctx context.Context,
	videoID string,
	liveChatID string,
	tokenSource oauth2.TokenSource,
	quotaTracker QuotaTracker,
) (*ChatListener, error) {
	if videoID == "" {
		return nil, fmt.Errorf("video ID is empty")
	}
	if liveChatID == "" {
		return nil, fmt.Errorf("live chat ID is empty")
	}
	logger.Infof(ctx, "creating a new ChatListener for videoID=%q, liveChatID=%q", videoID, liveChatID)

	ctx, cancelFunc := context.WithCancel(ctx)
	l := &ChatListener{
		videoID:         videoID,
		liveChatID:      liveChatID,
		tokenSource:     tokenSource,
		quotaTracker:    quotaTracker,
		cancelFunc:      cancelFunc,
		messagesOutChan: make(chan streamcontrol.Event, 100),
	}
	l.wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer l.wg.Done()
		defer func() {
			logger.Debugf(ctx, "the stream listener loop is finished")
			close(l.messagesOutChan)
		}()
		err := l.listenLoop(ctx)
		if err != nil && !errors.Is(err, context.Canceled) && !errors.As(err, &ErrChatEnded{}) {
			logger.Errorf(ctx, "the stream listener loop returned an error: %v", err)
		}
	})

	return l, nil
}

func (l *ChatListener) listenLoop(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "listenLoop (gRPC streaming)")
	defer func() { logger.Debugf(ctx, "/listenLoop (gRPC streaming): %v", _err) }()

	conn, err := grpc.NewClient(
		youtubeGRPCHost,
		grpc.WithTransportCredentials(credentials.NewTLS(nil)),
		grpc.WithPerRPCCredentials(&grpcoauth.TokenSource{TokenSource: l.tokenSource}),
	)
	if err != nil {
		return fmt.Errorf("unable to create gRPC client: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			logger.Warnf(ctx, "unable to close gRPC connection: %v", err)
		}
	}()

	client := ytgrpc.NewV3DataLiveChatMessageServiceClient(conn)

	var pageToken string
	reconnectDelay := streamReconnectDelay
	var lastStreamStartedAt time.Time

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if !lastStreamStartedAt.IsZero() {
			if remaining := l.quotaThrottleRemaining(lastStreamStartedAt); remaining > 0 {
				logger.Infof(ctx, "quota >= %d, waiting %v before reconnecting", highQuotaUsageThreshold, remaining)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(remaining):
				}
			}
		}

		lastStreamStartedAt = time.Now()
		err := l.streamMessages(ctx, client, &pageToken)
		if err == nil {
			reconnectDelay = streamReconnectDelay
			continue
		}

		st, ok := status.FromError(err)
		if ok {
			switch st.Code() {
			case codes.FailedPrecondition:
				return ErrChatEnded{ChatID: l.liveChatID}
			case codes.NotFound:
				return ErrChatNotFound{ChatID: l.liveChatID}
			case codes.PermissionDenied:
				return ErrChatDisabled{ChatID: l.liveChatID}
			case codes.ResourceExhausted:
				logger.Errorf(ctx, "gRPC stream died: rate limited by YouTube (ResourceExhausted), backing off for %v", reconnectDelay)
			case codes.Unavailable:
				logger.Errorf(ctx, "gRPC stream died: server unavailable (Unavailable), reconnecting in %v", reconnectDelay)
			case codes.Canceled:
				return ctx.Err()
			default:
				logger.Errorf(ctx, "gRPC stream died: code=%s, err=%v, reconnecting in %v", st.Code(), err, reconnectDelay)
			}
		} else if errors.Is(err, context.Canceled) {
			return err
		} else {
			logger.Errorf(ctx, "gRPC stream died: %v, reconnecting in %v", err, reconnectDelay)
		}

		reconnectDelay = max(reconnectDelay, l.quotaThrottleRemaining(lastStreamStartedAt))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(reconnectDelay):
		}

		reconnectDelay = min(reconnectDelay*2, maxReconnectDelay)
	}
}

func (l *ChatListener) quotaThrottleRemaining(lastStreamStartedAt time.Time) time.Duration {
	if l.quotaTracker == nil {
		return 0
	}
	if l.quotaTracker.UsedQuotaPoints() < highQuotaUsageThreshold {
		return 0
	}

	elapsed := time.Since(lastStreamStartedAt)
	remaining := highQuotaReconnectDelay - elapsed
	if remaining <= 0 {
		return 0
	}
	return remaining
}

func (l *ChatListener) streamMessages(
	ctx context.Context,
	client ytgrpc.V3DataLiveChatMessageServiceClient,
	pageToken *string,
) error {
	req := &ytgrpc.LiveChatMessageListRequest{
		LiveChatId: l.liveChatID,
		Part:       []string{"snippet", "authorDetails"},
	}
	if *pageToken != "" {
		req.PageToken = *pageToken
	}

	logger.Debugf(ctx, "opening gRPC stream for liveChatId=%q", l.liveChatID)

	stream, err := client.StreamList(ctx, req)
	if err != nil {
		return fmt.Errorf("unable to open StreamList: %w", err)
	}
	logger.Infof(ctx, "gRPC stream opened successfully for liveChatId=%q", l.liveChatID)

	if l.quotaTracker != nil {
		l.quotaTracker.ReportQuotaConsumption(ctx, 1)
	}

	streamStartedAt := time.Now()
	var recvCount int
	for {
		resp, err := stream.Recv()
		if err != nil {
			streamDuration := time.Since(streamStartedAt)
			if errors.Is(err, io.EOF) {
				logger.Warnf(ctx, "stream ended (EOF) after %d Recv calls in %v", recvCount, streamDuration)
				return nil
			}
			return fmt.Errorf("stream failed after %d Recv calls in %v: %w", recvCount, streamDuration, err)
		}
		recvCount++

		if resp.NextPageToken != "" {
			*pageToken = resp.NextPageToken
		}

		for _, item := range resp.Items {
			if item.Snippet != nil {
				switch item.Snippet.Type {
				case ytgrpc.LiveChatMessageSnippet_TOMBSTONE:
					continue
				case ytgrpc.LiveChatMessageSnippet_CHAT_ENDED_EVENT:
					return ErrChatEnded{ChatID: l.liveChatID}
				}
			}

			msg := l.convertMessage(ctx, item)
			select {
			case l.messagesOutChan <- msg:
			default:
				logger.Errorf(ctx, "the queue is full, have to drop %#+v", msg)
			}
		}
	}
}

func (l *ChatListener) convertMessage(
	ctx context.Context,
	item *ytgrpc.LiveChatMessage,
) streamcontrol.Event {
	var publishedAt time.Time
	if item.Snippet != nil {
		var err error
		publishedAt, err = ParseTimestamp(item.Snippet.PublishedAt)
		if err != nil {
			logger.Errorf(ctx, "unable to parse the timestamp '%s': %v", item.Snippet.PublishedAt, err)
		}
	}

	var user streamcontrol.User
	if item.AuthorDetails != nil {
		user = streamcontrol.User{
			ID:   streamcontrol.UserID(item.AuthorDetails.ChannelId),
			Slug: item.AuthorDetails.ChannelId,
			Name: item.AuthorDetails.DisplayName,
		}
	}

	var displayMessage string
	eventType := streamcontrol.EventTypeChatMessage
	var paid *streamcontrol.Money
	var targetUser *streamcontrol.User
	var tier *streamcontrol.Tier
	var inReplyTo *streamcontrol.EventID

	if item.Snippet != nil {
		displayMessage = item.Snippet.DisplayMessage

		switch item.Snippet.Type {
		case ytgrpc.LiveChatMessageSnippet_TEXT_MESSAGE_EVENT,
			ytgrpc.LiveChatMessageSnippet_INVALID_TYPE:
			eventType = streamcontrol.EventTypeChatMessage

		case ytgrpc.LiveChatMessageSnippet_SUPER_CHAT_EVENT:
			eventType = streamcontrol.EventTypeCheer
			if sc := item.Snippet.GetSuperChatDetails(); sc != nil {
				paid = &streamcontrol.Money{
					Currency: parseCurrencyString(sc.Currency),
					Amount:   float64(sc.AmountMicros) / 1000000,
				}
				if displayMessage == "" {
					displayMessage = sc.UserComment
				}
			}

		case ytgrpc.LiveChatMessageSnippet_SUPER_STICKER_EVENT:
			eventType = streamcontrol.EventTypeCheer
			if ss := item.Snippet.GetSuperStickerDetails(); ss != nil {
				paid = &streamcontrol.Money{
					Currency: parseCurrencyString(ss.Currency),
					Amount:   float64(ss.AmountMicros) / 1000000,
				}
			}

		case ytgrpc.LiveChatMessageSnippet_FAN_FUNDING_EVENT:
			eventType = streamcontrol.EventTypeCheer

		case ytgrpc.LiveChatMessageSnippet_NEW_SPONSOR_EVENT:
			eventType = streamcontrol.EventTypeSubscriptionNew
			if ns := item.Snippet.GetNewSponsorDetails(); ns != nil {
				t := streamcontrol.Tier(ns.MemberLevelName)
				tier = &t
			}

		case ytgrpc.LiveChatMessageSnippet_MEMBER_MILESTONE_CHAT_EVENT:
			eventType = streamcontrol.EventTypeSubscriptionRenewed
			if mm := item.Snippet.GetMemberMilestoneChatDetails(); mm != nil {
				t := streamcontrol.Tier(mm.MemberLevelName)
				tier = &t
				if displayMessage == "" {
					displayMessage = mm.UserComment
				}
			}

		case ytgrpc.LiveChatMessageSnippet_MEMBERSHIP_GIFTING_EVENT:
			eventType = streamcontrol.EventTypeGiftedSubscription
			if mg := item.Snippet.GetMembershipGiftingDetails(); mg != nil {
				t := streamcontrol.Tier(mg.GiftMembershipsLevelName)
				tier = &t
				if displayMessage == "" {
					displayMessage = fmt.Sprintf("%d memberships gifted", mg.GiftMembershipsCount)
				}
			}

		case ytgrpc.LiveChatMessageSnippet_GIFT_MEMBERSHIP_RECEIVED_EVENT:
			eventType = streamcontrol.EventTypeGiftedSubscription
			if gr := item.Snippet.GetGiftMembershipReceivedDetails(); gr != nil {
				t := streamcontrol.Tier(gr.MemberLevelName)
				tier = &t
			}

		case ytgrpc.LiveChatMessageSnippet_USER_BANNED_EVENT:
			eventType = streamcontrol.EventTypeBan
			if bd := item.Snippet.GetUserBannedDetails(); bd != nil {
				if bd.BannedUserDetails != nil {
					tu := streamcontrol.User{
						ID:   streamcontrol.UserID(bd.BannedUserDetails.ChannelId),
						Slug: bd.BannedUserDetails.ChannelId,
						Name: bd.BannedUserDetails.DisplayName,
					}
					targetUser = &tu
				}
				switch bd.BanType {
				case ytgrpc.LiveChatUserBannedMessageDetails_PERMANENT:
					displayMessage = "permanent ban"
				case ytgrpc.LiveChatUserBannedMessageDetails_TEMPORARY:
					displayMessage = fmt.Sprintf("temporary ban (%ds)", bd.BanDurationSeconds)
				}
			}

		case ytgrpc.LiveChatMessageSnippet_MESSAGE_DELETED_EVENT:
			eventType = streamcontrol.EventTypeOther
			if md := item.Snippet.GetMessageDeletedDetails(); md != nil {
				replyTo := streamcontrol.EventID(md.DeletedMessageId)
				inReplyTo = &replyTo
			}

		case ytgrpc.LiveChatMessageSnippet_MESSAGE_RETRACTED_EVENT:
			eventType = streamcontrol.EventTypeOther
			if mr := item.Snippet.GetMessageRetractedDetails(); mr != nil {
				replyTo := streamcontrol.EventID(mr.RetractedMessageId)
				inReplyTo = &replyTo
			}

		case ytgrpc.LiveChatMessageSnippet_POLL_EVENT:
			eventType = streamcontrol.EventTypeOther
			if pd := item.Snippet.GetPollDetails(); pd != nil && pd.Metadata != nil {
				displayMessage = pd.Metadata.QuestionText
			}

		case ytgrpc.LiveChatMessageSnippet_CHAT_ENDED_EVENT:
			eventType = streamcontrol.EventTypeStreamOffline

		case ytgrpc.LiveChatMessageSnippet_SPONSOR_ONLY_MODE_STARTED_EVENT,
			ytgrpc.LiveChatMessageSnippet_SPONSOR_ONLY_MODE_ENDED_EVENT:
			eventType = streamcontrol.EventTypeOther

		default:
			logger.Warnf(ctx, "unknown chat message type: %v", item.Snippet.Type)
			eventType = streamcontrol.EventTypeOther
		}
	}

	return streamcontrol.Event{
		ID:         streamcontrol.EventID(item.Id),
		CreatedAt:  publishedAt,
		Type:       eventType,
		User:       user,
		TargetUser: targetUser,
		Message: &streamcontrol.Message{
			Content:   displayMessage,
			Format:    streamcontrol.TextFormatTypePlain,
			InReplyTo: inReplyTo,
		},
		Paid: paid,
		Tier: tier,
	}
}

func parseCurrencyString(s string) streamcontrol.Currency {
	switch s {
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

func (l *ChatListener) Close(ctx context.Context) error {
	l.cancelFunc()
	return nil
}

func (l *ChatListener) MessagesChan() <-chan streamcontrol.Event {
	return l.messagesOutChan
}

func (l *ChatListener) GetVideoID() string {
	return l.videoID
}
