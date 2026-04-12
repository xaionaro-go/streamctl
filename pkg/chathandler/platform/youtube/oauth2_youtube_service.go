package youtube

import (
	"context"
	"fmt"

	ytpkg "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	youtubesvc "google.golang.org/api/youtube/v3"
)

// newOAuth2YouTubeService creates a YouTube Data API v3 service authenticated
// with the OAuth2 credentials (ClientID, ClientSecret, Token) from the config.
func newOAuth2YouTubeService(
	ctx context.Context,
	cfg *ytpkg.Config,
) (*youtubesvc.Service, error) {
	oauthCfg := &oauth2.Config{
		ClientID:     cfg.Config.ClientID,
		ClientSecret: cfg.Config.ClientSecret.Get(),
		Endpoint:     google.Endpoint,
		Scopes: []string{
			"https://www.googleapis.com/auth/youtube.force-ssl",
			"https://www.googleapis.com/auth/youtube",
		},
	}

	token := cfg.Config.Token.GetPointer()
	tokenSource := oauthCfg.TokenSource(ctx, token)

	svc, err := youtubesvc.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("create YouTube service with OAuth2: %w", err)
	}

	return svc, nil
}

// hasOAuth2Credentials returns true if the config contains the required
// OAuth2 fields: ClientID, ClientSecret, and a non-nil Token.
func hasOAuth2Credentials(cfg *ytpkg.Config) bool {
	return cfg.Config.ClientID != "" &&
		cfg.Config.ClientSecret.Get() != "" &&
		cfg.Config.Token != nil
}
