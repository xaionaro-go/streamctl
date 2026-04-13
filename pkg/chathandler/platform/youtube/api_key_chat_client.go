package youtube

import (
	"context"

	youtubesvc "google.golang.org/api/youtube/v3"
)

// APIKeyChatClient wraps a youtube.Service to implement youtube.ChatClient.
type APIKeyChatClient struct {
	Service *youtubesvc.Service
}

func (c *APIKeyChatClient) GetLiveChatMessages(
	ctx context.Context,
	chatID string,
	pageToken string,
	parts []string,
) (*youtubesvc.LiveChatMessageListResponse, error) {
	q := c.Service.LiveChatMessages.List(chatID, parts).Context(ctx)
	if pageToken != "" {
		q = q.PageToken(pageToken)
	}
	return q.Do()
}
