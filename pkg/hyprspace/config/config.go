package config

import (
	"fmt"

	"github.com/hyprspace/hyprspace/config"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

func New() (*config.Config, error) {
	privKey, _, err := crypto.GenerateKeyPair(crypto.Ed25519, 256)
	if err != nil {
		return nil, fmt.Errorf("unable to generate a key pair: %w", err)
	}

	var addrs []multiaddr.Multiaddr
	for _, addrString := range []string{
		"/ip4/0.0.0.0/tcp/8001",
		"/ip4/0.0.0.0/udp/8001/quic-v1",
		"/ip6/::/tcp/8001",
		"/ip6/::/udp/8001/quic-v1",
	} {
		addr, err := multiaddr.NewMultiaddr(addrString)
		if err != nil {
			return nil, fmt.Errorf("unable to parse address '%s': %w", addrString, err)
		}
		addrs = append(addrs, addr)
	}

	cfg := &config.Config{
		PrivateKey:      privKey,
		ListenAddresses: addrs,
	}

	// it's done, but let's validate:
	_, err = peer.IDFromPrivateKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("unable to generate a peer ID from the private key: %w", err)
	}

	return cfg, nil
}
