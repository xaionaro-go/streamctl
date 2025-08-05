package kick

import (
	"context"

	"github.com/scorfly/gokick"
)

type clientMock struct{}

var _ Client = (*clientMock)(nil)

func newClientMock() *clientMock {
	return &clientMock{}
}

func (clientMock) GetAuthorize(redirectURI, state, codeChallenge string, scope []gokick.Scope) (string, error) {
	return "", nil
}

func (clientMock) RefreshToken(ctx context.Context, refreshToken string) (gokick.TokenResponse, error) {
	return gokick.TokenResponse{}, nil
}

func (clientMock) GetToken(ctx context.Context, redirectURI, code, codeVerifier string) (gokick.TokenResponse, error) {
	return gokick.TokenResponse{}, nil
}

func (clientMock) OnUserAccessTokenRefreshed(callback func(accessToken, refreshToken string)) {}

func (clientMock) UpdateStreamTitle(ctx context.Context, title string) (gokick.EmptyResponse, error) {
	return gokick.EmptyResponse{}, nil
}

func (clientMock) UpdateStreamCategory(ctx context.Context, categoryID int) (gokick.EmptyResponse, error) {
	return gokick.EmptyResponse{}, nil
}

func (clientMock) GetLivestreams(ctx context.Context, filter gokick.LivestreamListFilter) (gokick.LivestreamsResponseWrapper, error) {
	return gokick.LivestreamsResponseWrapper{}, nil
}

func (clientMock) GetChannels(ctx context.Context, filter gokick.ChannelListFilter) (gokick.ChannelsResponseWrapper, error) {
	return gokick.ChannelsResponseWrapper{
		Result: []gokick.ChannelResponse{{
			BannerPicture:     "BannerPicture",
			BroadcasterUserID: 1,
			Category: gokick.CategoryResponse{
				ID:        2,
				Name:      "Name",
				Thumbnail: "Thumbnail",
			},
			ChannelDescription: "ChannelDescription",
			Slug:               "Slug",
			Stream: gokick.StreamResponse{
				Key:         "Key",
				URL:         "URL",
				IsLive:      true,
				IsMature:    false,
				Language:    "Language",
				StartTime:   "StartTime",
				Thumbnail:   "Thumbnail",
				ViewerCount: 3,
			},
			StreamTitle: "StreamTitle",
		}},
	}, nil
}

func (clientMock) SendChatMessage(
	ctx context.Context,
	broadcasterUserID *int,
	content string,
	replyToMessageID *string,
	messageType gokick.MessageType,
) (gokick.ChatResponseWrapper, error) {
	return gokick.ChatResponseWrapper{
		Result: gokick.ChatResponse{
			IsSent:    true,
			MessageID: "MessageID",
		},
	}, nil
}

func (clientMock) BanUser(
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

func (clientMock) SetAppAccessToken(token string)   {}
func (clientMock) SetUserAccessToken(token string)  {}
func (clientMock) SetUserRefreshToken(token string) {}
