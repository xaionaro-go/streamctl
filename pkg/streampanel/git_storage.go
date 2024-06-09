package streampanel

import (
	"crypto"
)

type gitStorage struct {
	RemoteURL  string
	PrivateKey crypto.PrivateKey
}

func newGitStorage(
	remoteURL string,
	privateKey crypto.PrivateKey,
) *gitStorage {
	return &gitStorage{
		RemoteURL:  remoteURL,
		PrivateKey: privateKey,
	}
}

func (s *gitStorage) Sync(data *panelData, onUpdate func()) error {
	if data == nil {
		return nil
	}
	return nil
}
