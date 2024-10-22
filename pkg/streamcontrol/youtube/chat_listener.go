package youtube

import (
	"context"
	"fmt"

	"google.golang.org/api/youtube/v3"
)

type YouTubeCommentService interface {
	List(
		ctx context.Context,
		videoID string,
	) (*youtube.CommentThreadListResponse, error)
}

type ChatListener struct{}

func NewChatListener(
	ctx context.Context,
	videoID string,
) (*ChatListener, error) {
	return nil, fmt.Errorf("not implemented, yet")
}
