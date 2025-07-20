package twitch

import (
	"time"

	"github.com/nicklaw5/helix/v2"
)

type clientMock struct{}

func newClientMock() *clientMock {
	return &clientMock{}
}

var _ client = (*clientMock)(nil)

func (c *clientMock) GetAppAccessToken() string {
	return ""
}
func (c *clientMock) GetUserAccessToken() string {
	return ""
}
func (c *clientMock) GetRefreshToken() string {
	return ""
}
func (c *clientMock) SetAppAccessToken(accessToken string) {}

func (c *clientMock) SetUserAccessToken(accessToken string) {}

func (c *clientMock) SetRefreshToken(refreshToken string) {}

func (c *clientMock) OnUserAccessTokenRefreshed(f func(newAccessToken, newRefreshToken string)) {}

func (c *clientMock) GetChannelInformation(params *helix.GetChannelInformationParams) (*helix.GetChannelInformationResponse, error) {
	return &helix.GetChannelInformationResponse{
		Data: helix.ManyChannelInformation{
			Channels: []helix.ChannelInformation{{
				BroadcasterID:       "BroadcasterID",
				BroadcasterName:     "BroadcasterName",
				BroadcasterLanguage: "BroadcasterLanguage",
				GameID:              "GameID",
				GameName:            "GameName",
				Title:               "Title",
				Delay:               1,
				Tags:                []string{"Tag"},
			}},
		},
	}, nil
}

func (c *clientMock) RequestUserAccessToken(code string) (*helix.UserAccessTokenResponse, error) {
	return &helix.UserAccessTokenResponse{}, nil
}

func (c *clientMock) RequestAppAccessToken(scopes []string) (*helix.AppAccessTokenResponse, error) {
	return &helix.AppAccessTokenResponse{}, nil
}

func (c *clientMock) EditChannelInformation(params *helix.EditChannelInformationParams) (*helix.EditChannelInformationResponse, error) {
	return &helix.EditChannelInformationResponse{}, nil
}

func (c *clientMock) GetGames(params *helix.GamesParams) (*helix.GamesResponse, error) {
	return &helix.GamesResponse{
		Data: helix.ManyGames{
			Games: []helix.Game{{
				ID:        "ID",
				Name:      "Name",
				BoxArtURL: "BoxArtURL",
			}},
		},
	}, nil
}

func (c *clientMock) GetStreams(params *helix.StreamsParams) (*helix.StreamsResponse, error) {
	return &helix.StreamsResponse{
		Data: helix.ManyStreams{
			Streams: []helix.Stream{{
				ID:           "ID",
				UserID:       "UserID",
				UserLogin:    "UserLogin",
				UserName:     "UserName",
				GameID:       "GameID",
				GameName:     "GameName",
				TagIDs:       []string{"TagID"},
				Tags:         []string{"Tag"},
				IsMature:     false,
				Type:         "Type",
				Title:        "Title",
				ViewerCount:  1,
				StartedAt:    time.Now(),
				Language:     "Language",
				ThumbnailURL: "ThumbnailURL",
			}},
		},
	}, nil
}

func (c *clientMock) GetTopGames(params *helix.TopGamesParams) (*helix.TopGamesResponse, error) {
	return &helix.TopGamesResponse{
		Data: helix.ManyGamesWithPagination{
			ManyGames: helix.ManyGames{
				Games: []helix.Game{{
					ID:        "ID",
					Name:      "Name",
					BoxArtURL: "BoxArtURL",
				}},
			},
		},
	}, nil
}

func (c *clientMock) SendChatMessage(params *helix.SendChatMessageParams) (*helix.ChatMessageResponse, error) {
	return &helix.ChatMessageResponse{}, nil
}

func (c *clientMock) DeleteChatMessage(params *helix.DeleteChatMessageParams) (*helix.DeleteChatMessageResponse, error) {
	return &helix.DeleteChatMessageResponse{}, nil
}

func (c *clientMock) BanUser(params *helix.BanUserParams) (*helix.BanUserResponse, error) {
	return &helix.BanUserResponse{}, nil
}

func (c *clientMock) StartRaid(params *helix.StartRaidParams) (*helix.RaidResponse, error) {
	return &helix.RaidResponse{}, nil
}

func (c *clientMock) SendShoutout(params *helix.SendShoutoutParams) (*helix.SendShoutoutResponse, error) {
	return &helix.SendShoutoutResponse{}, nil
}

func (c *clientMock) CreateEventSubSubscription(payload *helix.EventSubSubscription) (*helix.EventSubSubscriptionsResponse, error) {
	return &helix.EventSubSubscriptionsResponse{}, nil
}

func (c *clientMock) GetUsers(params *helix.UsersParams) (*helix.UsersResponse, error) {
	return &helix.UsersResponse{
		Data: helix.ManyUsers{
			Users: []helix.User{{
				ID:              "ID",
				Login:           "Login",
				DisplayName:     "DisplayName",
				Type:            "Type",
				BroadcasterType: "BroadcasterType",
				Description:     "Description",
				ProfileImageURL: "ProfileImageURL",
				OfflineImageURL: "OfflineImageURL",
				ViewCount:       1,
				Email:           "Email",
				CreatedAt:       helix.Time{Time: time.Now()},
			}},
		},
	}, nil
}
