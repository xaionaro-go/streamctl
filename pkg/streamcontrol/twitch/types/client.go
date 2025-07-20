package twitch

import (
	"github.com/nicklaw5/helix/v2"
)

type Client interface {
	GetAppAccessToken() string
	GetUserAccessToken() string
	GetRefreshToken() string
	SetAppAccessToken(accessToken string)
	SetUserAccessToken(accessToken string)
	SetRefreshToken(refreshToken string)
	OnUserAccessTokenRefreshed(f func(newAccessToken, newRefreshToken string))
	GetChannelInformation(params *helix.GetChannelInformationParams) (*helix.GetChannelInformationResponse, error)
	RequestUserAccessToken(code string) (*helix.UserAccessTokenResponse, error)
	RequestAppAccessToken(scopes []string) (*helix.AppAccessTokenResponse, error)
	EditChannelInformation(params *helix.EditChannelInformationParams) (*helix.EditChannelInformationResponse, error)
	GetGames(params *helix.GamesParams) (*helix.GamesResponse, error)
	GetStreams(params *helix.StreamsParams) (*helix.StreamsResponse, error)
	GetTopGames(params *helix.TopGamesParams) (*helix.TopGamesResponse, error)
	SendChatMessage(params *helix.SendChatMessageParams) (*helix.ChatMessageResponse, error)
	DeleteChatMessage(params *helix.DeleteChatMessageParams) (*helix.DeleteChatMessageResponse, error)
	BanUser(params *helix.BanUserParams) (*helix.BanUserResponse, error)
	StartRaid(params *helix.StartRaidParams) (*helix.RaidResponse, error)
	SendShoutout(params *helix.SendShoutoutParams) (*helix.SendShoutoutResponse, error)
	CreateEventSubSubscription(payload *helix.EventSubSubscription) (*helix.EventSubSubscriptionsResponse, error)
	GetUsers(params *helix.UsersParams) (*helix.UsersResponse, error)
}
