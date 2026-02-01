package kick

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/scorfly/gokick"
	"github.com/xaionaro-go/kickcom"
)

type clientMock struct {
	locker      sync.Mutex
	isLive      bool
	channels    map[int]gokick.ChannelResponse
	livestreams map[int]gokick.LivestreamResponse
}

var _ Client = (*clientMock)(nil)

func newClientMock() *clientMock {
	return &clientMock{
		isLive:      true,
		channels:    make(map[int]gokick.ChannelResponse),
		livestreams: make(map[int]gokick.LivestreamResponse),
	}
}

func (c *clientMock) GetAuthorize(redirectURI, state, codeChallenge string, scope []gokick.Scope) (string, error) {
	return "http://localhost/mock-auth", nil
}

func (c *clientMock) RefreshToken(ctx context.Context, refreshToken string) (gokick.TokenResponse, error) {
	return gokick.TokenResponse{
		AccessToken:  "mock-access-token",
		RefreshToken: "mock-refresh-token",
	}, nil
}

func (c *clientMock) GetToken(ctx context.Context, redirectURI, code, codeVerifier string) (gokick.TokenResponse, error) {
	return gokick.TokenResponse{
		AccessToken:  "mock-access-token",
		RefreshToken: "mock-refresh-token",
	}, nil
}

func (c *clientMock) OnUserAccessTokenRefreshed(callback func(accessToken, refreshToken string)) {}

func (c *clientMock) UpdateStreamTitle(ctx context.Context, title string) (gokick.EmptyResponse, error) {
	c.locker.Lock()
	defer c.locker.Unlock()
	for id, ch := range c.channels {
		ch.StreamTitle = title
		c.channels[id] = ch
	}
	return gokick.EmptyResponse{}, nil
}

func (c *clientMock) UpdateStreamCategory(ctx context.Context, categoryID int) (gokick.EmptyResponse, error) {
	c.locker.Lock()
	defer c.locker.Unlock()
	for id, ch := range c.channels {
		ch.Category.ID = categoryID
		c.channels[id] = ch
	}
	return gokick.EmptyResponse{}, nil
}

func (c *clientMock) GetLivestreams(ctx context.Context, filter gokick.LivestreamListFilter) (gokick.LivestreamsResponseWrapper, error) {
	c.locker.Lock()
	defer c.locker.Unlock()
	var result []gokick.LivestreamResponse
	for _, ls := range c.livestreams {
		result = append(result, ls)
	}
	return gokick.LivestreamsResponseWrapper{Result: result}, nil
}

func (c *clientMock) GetChannels(ctx context.Context, filter gokick.ChannelListFilter) (gokick.ChannelsResponseWrapper, error) {
	c.locker.Lock()
	defer c.locker.Unlock()
	var result []gokick.ChannelResponse

	// Note: We can't easily peek into the filter without adding methods to gokick.
	// For now, return all channels we have, or a default one if empty.

	if len(c.channels) > 0 {
		for _, ch := range c.channels {
			result = append(result, ch)
		}
	} else {
		result = append(result, gokick.ChannelResponse{
			BroadcasterUserID: 1,
			Slug:              "mock-slug",
			Stream: gokick.StreamResponse{
				IsLive:      c.isLive,
				ViewerCount: 123,
				StartTime:   time.Now().Format(time.RFC3339),
			},
		})
	}
	return gokick.ChannelsResponseWrapper{Result: result}, nil
}

func (c *clientMock) GetChannelV1(ctx context.Context, channel string) (*kickcom.ChannelV1, error) {
	return &kickcom.ChannelV1{
		ID:     1,
		UserID: 1,
		Slug:   channel,
		User: kickcom.UserV1{
			ID: 1,
		},
		Chatroom: kickcom.ChatroomV1Short{
			ID: 1,
		},
	}, nil
}

func (c *clientMock) GetLivestreamV2(ctx context.Context, channelSlug string) (*kickcom.LivestreamV2Reply, error) {
	if !c.isLive {
		return &kickcom.LivestreamV2Reply{
			Data: nil,
		}, nil
	}
	return &kickcom.LivestreamV2Reply{
		Data: &kickcom.LivestreamV2{
			ID:        1,
			Slug:      channelSlug,
			Viewers:   123,
			CreatedAt: time.Now(),
		},
	}, nil
}

func (c *clientMock) GetSubcategoriesV1(ctx context.Context) (*kickcom.CategoriesV1Reply, error) {
	return &kickcom.CategoriesV1Reply{
		{ID: 1, Name: "Mock Category 1"},
		{ID: 2, Name: "Mock Category 2"},
	}, nil
}

func (c *clientMock) SendChatMessage(
	ctx context.Context,
	broadcasterUserID *int,
	content string,
	replyToMessageID *string,
	messageType gokick.MessageType,
) (gokick.ChatResponseWrapper, error) {
	return gokick.ChatResponseWrapper{
		Result: gokick.ChatResponse{
			IsSent:    true,
			MessageID: fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		},
	}, nil
}

func (c *clientMock) BanUser(
	ctx context.Context,
	broadcasterUserID int,
	userID int,
	duration *int,
	reason *string,
) (gokick.BanUserResponseWrapper, error) {
	return gokick.BanUserResponseWrapper{
		Result: gokick.BanUserResponse{},
	}, nil
}

func (c *clientMock) SetAppAccessToken(token string)   {}
func (c *clientMock) SetUserAccessToken(token string)  {}
func (c *clientMock) SetUserRefreshToken(token string) {}

func (*clientMock) CreateSubscriptions(
	ctx context.Context,
	method gokick.SubscriptionMethod,
	subscriptions []gokick.SubscriptionRequest,
	broadcasterUserID *int,
) (gokick.CreateSubscriptionsResponseWrapper, error) {
	return gokick.CreateSubscriptionsResponseWrapper{}, fmt.Errorf("not implemented in mock client")
}
