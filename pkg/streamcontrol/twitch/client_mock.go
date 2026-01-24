package twitch

import (
	"context"
	"sync"
	"time"

	"github.com/nicklaw5/helix/v2"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type clientMock struct {
	locker               sync.Mutex
	isLive               bool
	channelInfo          map[string]helix.ChannelInformation
	streams              map[string]helix.Stream
	users                map[string]helix.User
	userAccessToken      string
	appAccessToken       string
	refreshToken         string
	tokenRefreshCallback func(newAccessToken, newRefreshToken string)
	chatMessagesChan     chan streamcontrol.Event
}

func newClientMock() *clientMock {
	m := &clientMock{
		isLive:           true,
		channelInfo:      make(map[string]helix.ChannelInformation),
		streams:          make(map[string]helix.Stream),
		users:            make(map[string]helix.User),
		userAccessToken:  "mock-token", // Set a default token to satisfy controller initialization
		chatMessagesChan: make(chan streamcontrol.Event, 100),
	}
	m.users["mock-broadcaster-id"] = helix.User{
		ID:    "mock-broadcaster-id",
		Login: "mockuser",
	}
	m.channelInfo["mock-broadcaster-id"] = helix.ChannelInformation{
		BroadcasterID:   "mock-broadcaster-id",
		BroadcasterName: "mockuser",
		Title:           "Mock Stream",
	}
	return m
}

var _ client = (*clientMock)(nil)
var _ ChatHandler = (*clientMock)(nil)

func (c *clientMock) MessagesChan() <-chan streamcontrol.Event {
	return c.chatMessagesChan
}

func (c *clientMock) Close(ctx context.Context) error {
	return nil
}

func (c *clientMock) GetAppAccessToken() string {
	c.locker.Lock()
	defer c.locker.Unlock()
	return c.appAccessToken
}
func (c *clientMock) GetUserAccessToken() string {
	c.locker.Lock()
	defer c.locker.Unlock()
	return c.userAccessToken
}
func (c *clientMock) GetRefreshToken() string {
	c.locker.Lock()
	defer c.locker.Unlock()
	return c.refreshToken
}
func (c *clientMock) SetAppAccessToken(accessToken string) {
	c.locker.Lock()
	defer c.locker.Unlock()
	c.appAccessToken = accessToken
}

func (c *clientMock) SetUserAccessToken(accessToken string) {
	c.locker.Lock()
	defer c.locker.Unlock()
	c.userAccessToken = accessToken
}

func (c *clientMock) SetRefreshToken(refreshToken string) {
	c.locker.Lock()
	defer c.locker.Unlock()
	c.refreshToken = refreshToken
}

func (c *clientMock) OnUserAccessTokenRefreshed(f func(newAccessToken, newRefreshToken string)) {
	c.locker.Lock()
	defer c.locker.Unlock()
	c.tokenRefreshCallback = f
}

func (c *clientMock) GetChannelInformation(params *helix.GetChannelInformationParams) (*helix.GetChannelInformationResponse, error) {
	c.locker.Lock()
	defer c.locker.Unlock()

	var channels []helix.ChannelInformation
	for _, id := range params.BroadcasterIDs {
		if info, ok := c.channelInfo[id]; ok {
			channels = append(channels, info)
		} else {
			// Return a default if not found to avoid breaking logic that expects something
			channels = append(channels, helix.ChannelInformation{
				BroadcasterID:   id,
				BroadcasterName: "MockUser-" + id,
				Title:           "Mock Title",
			})
		}
	}

	return &helix.GetChannelInformationResponse{
		Data: helix.ManyChannelInformation{
			Channels: channels,
		},
	}, nil
}

func (c *clientMock) RequestUserAccessToken(code string) (*helix.UserAccessTokenResponse, error) {
	return &helix.UserAccessTokenResponse{
		Data: helix.AccessCredentials{
			AccessToken:  "mock-user-token",
			RefreshToken: "mock-refresh-token",
			ExpiresIn:    3600,
		},
	}, nil
}

func (c *clientMock) RequestAppAccessToken(scopes []string) (*helix.AppAccessTokenResponse, error) {
	return &helix.AppAccessTokenResponse{
		Data: helix.AccessCredentials{
			AccessToken: "mock-app-token",
			ExpiresIn:   3600,
		},
	}, nil
}

func (c *clientMock) EditChannelInformation(params *helix.EditChannelInformationParams) (*helix.EditChannelInformationResponse, error) {
	c.locker.Lock()
	defer c.locker.Unlock()

	info := c.channelInfo[params.BroadcasterID]
	info.BroadcasterID = params.BroadcasterID
	if params.Title != "" {
		info.Title = params.Title
	}
	if params.GameID != "" {
		info.GameID = params.GameID
	}
	c.channelInfo[params.BroadcasterID] = info

	return &helix.EditChannelInformationResponse{}, nil
}

func (c *clientMock) GetGames(params *helix.GamesParams) (*helix.GamesResponse, error) {
	var games []helix.Game
	for _, name := range params.Names {
		games = append(games, helix.Game{
			ID:   "game-" + name,
			Name: name,
		})
	}
	return &helix.GamesResponse{
		Data: helix.ManyGames{
			Games: games,
		},
	}, nil
}

func (c *clientMock) GetStreams(params *helix.StreamsParams) (*helix.StreamsResponse, error) {
	c.locker.Lock()
	defer c.locker.Unlock()

	var streams []helix.Stream
	if !c.isLive {
		return &helix.StreamsResponse{
			Data: helix.ManyStreams{
				Streams: []helix.Stream{},
			},
		}, nil
	}
	if len(params.UserIDs) > 0 {
		for _, id := range params.UserIDs {
			streams = append(streams, helix.Stream{
				UserID:   id,
				UserName: "user-" + id,
				Type:     "live",
			})
		}
	} else {
		streams = append(streams, helix.Stream{
			UserID:   "mock-user-id",
			UserName: "mock-user",
			Type:     "live",
		})
	}

	return &helix.StreamsResponse{
		Data: helix.ManyStreams{
			Streams: streams,
		},
	}, nil
}

func (c *clientMock) GetTopGames(params *helix.TopGamesParams) (*helix.TopGamesResponse, error) {
	return &helix.TopGamesResponse{
		Data: helix.ManyGamesWithPagination{
			ManyGames: helix.ManyGames{
				Games: []helix.Game{{
					ID:   "123",
					Name: "Mock Game",
				}},
			},
		},
	}, nil
}

func (c *clientMock) SendChatMessage(params *helix.SendChatMessageParams) (*helix.ChatMessageResponse, error) {
	c.chatMessagesChan <- streamcontrol.Event{
		ID:        streamcontrol.EventID("msg-123"),
		CreatedAt: time.Now(),
		Type:      streamcontrol.EventTypeChatMessage,
		User: streamcontrol.User{
			ID:   streamcontrol.UserID(params.SenderID),
			Name: "Mock Sender",
		},
		Message: &streamcontrol.Message{
			Content: params.Message,
		},
	}
	return &helix.ChatMessageResponse{
		Data: helix.ManyChatMessages{
			Messages: []helix.ChatMessage{{
				MessageID: "msg-123",
				IsSent:    true,
			}},
		},
	}, nil
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
	c.locker.Lock()
	defer c.locker.Unlock()

	var users []helix.User
	if len(params.IDs) == 0 && len(params.Logins) == 0 {
		users = append(users, helix.User{
			ID:    "mock-user-id",
			Login: "mock-user",
		})
	}
	for _, id := range params.IDs {
		if u, ok := c.users[id]; ok {
			users = append(users, u)
		} else {
			users = append(users, helix.User{
				ID:    id,
				Login: "user-" + id,
			})
		}
	}
	for _, login := range params.Logins {
		found := false
		for _, u := range c.users {
			if u.Login == login {
				users = append(users, u)
				found = true
				break
			}
		}
		if !found {
			users = append(users, helix.User{
				ID:    "id-" + login,
				Login: login,
			})
		}
	}

	return &helix.UsersResponse{
		Data: helix.ManyUsers{
			Users: users,
		},
	}, nil
}

func (c *clientMock) GetStreamKey(params *helix.StreamKeyParams) (*helix.StreamKeysResponse, error) {
	return &helix.StreamKeysResponse{
		Data: helix.ManyStreamKeys{
			Data: []struct {
				StreamKey string `json:"stream_key"`
			}{
				{
					StreamKey: "mock_stream_key",
				},
			},
		},
	}, nil
}
