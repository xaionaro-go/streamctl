package secret

import (
	"golang.org/x/oauth2"
)

// OAuth2Token stores an OAuth2 token in an encrypted state in memory, so that
// you don't accidentally leak these secrets via logging or whatever.
type OAuth2Token = Any[oauth2.Token]
