package config

type GitRepoConfig struct {
	Enable           *bool
	URL              string `yaml:"url,omitempty"`
	PrivateKey       string `yaml:"private_key,omitempty" secret:""`
	LatestSyncCommit string `yaml:"latest_sync_commit,omitempty"` // TODO: deprecate this field, it's just a non-needed mechanism (better to check against git history).
}
