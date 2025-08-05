package kick

import (
	"context"

	"github.com/scorfly/gokick"
)

type Client interface {
	GetAuthorize(redirectURI, state, codeChallenge string, scope []gokick.Scope) (string, error)
	GetToken(ctx context.Context, redirectURI, code, codeVerifier string) (gokick.TokenResponse, error)
	RefreshToken(ctx context.Context, refreshToken string) (gokick.TokenResponse, error)
	OnUserAccessTokenRefreshed(callback func(accessToken, refreshToken string))
	UpdateStreamTitle(ctx context.Context, title string) (gokick.EmptyResponse, error)
	UpdateStreamCategory(ctx context.Context, categoryID int) (gokick.EmptyResponse, error)
	GetLivestreams(ctx context.Context, filter gokick.LivestreamListFilter) (gokick.LivestreamsResponseWrapper, error)
	GetChannels(ctx context.Context, filter gokick.ChannelListFilter) (gokick.ChannelsResponseWrapper, error)
	SendChatMessage(
		ctx context.Context,
		broadcasterUserID *int,
		content string,
		replyToMessageID *string,
		messageType gokick.MessageType,
	) (gokick.ChatResponseWrapper, error)
	BanUser(
		ctx context.Context,
		broadcasterUserID int,
		userID int,
		duration *int,
		reason *string,
	) (gokick.BanUserResponseWrapper, error)
	SetAppAccessToken(token string)
	SetUserAccessToken(token string)
	SetUserRefreshToken(token string)
}

type clientScorfly struct {
	*gokick.Client
}

func newClient(options *gokick.ClientOptions) (*clientScorfly, error) {
	client, err := gokick.NewClient(options)
	if err != nil {
		return nil, err
	}
	return &clientScorfly{Client: client}, nil
}

func (c *clientScorfly) OnUserAccessTokenRefreshed(callback func(accessToken, refreshToken string)) {
	c.Client.OnUserAccessTokenRefreshed(callback)
}
