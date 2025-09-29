package config

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/goombaio/namegenerator"
	"github.com/xaionaro-go/secret"
)

type P2PPrivateKey struct {
	ED25519 secret.Bytes `yaml:"ed25519"`
}

type P2PVPNConfig struct {
	Network string `yaml:"network"`
}

type P2PNetwork struct {
	NetworkID  string        `yaml:"network_id"`
	PeerName   string        `yaml:"peer_name"`
	PrivateKey P2PPrivateKey `yaml:"private_key"`
	PSK        secret.Bytes  `yaml:"psk"`
	VPN        P2PVPNConfig  `yaml:"vpn"`
}

func (cfg *P2PNetwork) IsZero() bool {
	return reflect.DeepEqual(*cfg, P2PNetwork{})
}

func (cfg *P2PPrivateKey) Get() (crypto.PrivateKey, error) {
	return ed25519.PrivateKey(cfg.ED25519.Get()), nil
}

func GetRandomP2PConfig() P2PNetwork {
	return P2PNetwork{
		NetworkID: randomNetworkID(),
		PeerName:  thisHostName(),
		PrivateKey: P2PPrivateKey{
			ED25519: secret.New([]byte(randomED25519())),
		},
		PSK: secret.New(randomPSK()),
		VPN: P2PVPNConfig{
			Network: "fd51:2eaf:7a4e::/64",
		},
	}
}

func randomNetworkID() string {
	var netID uuid.UUID
	_, err := rand.Read(netID[:])
	if err != nil {
		panic(err)
	}
	return netID.String()
}

func randomPSK() []byte {
	var psk [16]byte
	_, err := rand.Read(psk[:])
	if err != nil {
		panic(err)
	}
	return psk[:]
}

func randomED25519() ed25519.PrivateKey {
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	return privKey
}

func thisHostName() string {
	if hostname, err := os.Hostname(); err == nil {
		return hostname
	}

	nameGenerator := namegenerator.NewNameGenerator(time.Now().UTC().UnixNano())
	return nameGenerator.Generate()
}
