package p2p

import (
	"context"
	"crypto/ed25519"
	"crypto/sha1"

	"github.com/xaionaro-go/streamctl/pkg/p2p/implementations/weron"
	"github.com/xaionaro-go/streamctl/pkg/p2p/types"
)

func NewP2P(
	ctx context.Context,
	privKey ed25519.PrivateKey,
	peerName string,
	networkID string,
	psk []byte,
	networkCIDR string,
) (types.P2P, error) {
	pskHash := sha1.Sum(psk)
	return weron.NewP2P(
		ctx,
		privKey,
		peerName,
		networkID,
		pskHash[:16], // TODO; fix this
		networkCIDR,
	)
}
