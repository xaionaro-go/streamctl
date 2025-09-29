package config

import (
	"github.com/xaionaro-go/secret"
)

type GitRepoConfig struct {
	Enable           *bool
	URL              string        `yaml:"url,omitempty"`
	PrivateKey       secret.String `yaml:"private_key,omitempty"`
	LatestSyncCommit string        `yaml:"latest_sync_commit,omitempty"` // TODO: deprecate this field, it's just a non-needed mechanism (better to check against git history).
}
