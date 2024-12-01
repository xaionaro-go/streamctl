package types

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
)

type PeerID ed25519.PublicKey

func (id PeerID) String() string {
	return base64.StdEncoding.EncodeToString((ed25519.PublicKey)(id))
}

func (id PeerID) Less(cmp PeerID) bool {
	return id.String() < cmp.String()
}

func (id PeerID) Equal(cmp PeerID) bool {
	return id.String() == cmp.String()
}

func ParsePeerID(s string) (PeerID, error) {
	peerIDBytes, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("unable to base64-decode the peerID: %w", err)
	}

	peerID := ed25519.PublicKey(peerIDBytes)
	if len(peerID) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("expected to see an ED25519 public key (size %d), but received a sequence of size %d", ed25519.PublicKeySize, len(peerID))
	}

	return PeerID(peerID), nil
}
