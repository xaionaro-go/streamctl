package p2p

import (
	"context"
	"crypto"
	"crypto/sha1"

	"github.com/xaionaro-go/streamctl/pkg/p2p/implementations/weron"
	"github.com/xaionaro-go/streamctl/pkg/p2p/types"
)

type P2P = types.P2P

func NewP2P(
	ctx context.Context,
	privKey crypto.PrivateKey,
	peerName string,
	networkID string,
	psk []byte,
	networkCIDR string,
) (P2P, error) {
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
