//go:build e2e_tests
// +build e2e_tests

package e2e

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	xlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/pojntfx/weron/pkg/wrtcsgl"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/p2p"
	p2pconsts "github.com/xaionaro-go/streamctl/pkg/p2p/implementations/weron/consts"
	"github.com/xaionaro-go/streamctl/pkg/p2p/types"
)

const (
	signalerAddr = "127.0.0.1:28573"
)

func newSignaler(ctx context.Context) (*wrtcsgl.Signaler, error) {
	signaler := wrtcsgl.NewSignaler(
		signalerAddr,
		"",
		"",
		&wrtcsgl.SignalerConfig{
			Heartbeat:            10 * time.Second,
			Cleanup:              false,
			EphemeralCommunities: true,
			APIUsername:          "",
			APIPassword:          "",
			OIDCIssuer:           "",
			OIDCClientID:         "",
			OnConnect: func(raddr, community string) {
				logger.FromCtx(ctx).
					WithField("address", raddr).
					WithField("community", community).
					Infof("connected to a client")
			},
			OnDisconnect: func(raddr, community string, err any) {
				logger.FromCtx(ctx).
					WithField("address", raddr).
					WithField("community", community).
					WithField("error", err).
					Infof("disconnected from the client")
			},
		},
		ctx,
	)

	if err := signaler.Open(); err != nil {
		return nil, err
	}
	return signaler, nil
}

func TestE2E(t *testing.T) {
	var wg sync.WaitGroup

	ctx := logger.CtxWithLogger(context.Background(), xlogrus.Default().WithLevel(logger.LevelTrace))
	ctx, cancelFn := context.WithCancel(ctx)
	logger.Default = func() logger.Logger {
		return logger.FromCtx(ctx)
	}
	defer belt.Flush(ctx)

	log.Logger = &zerolog.Logger{Logger: logger.FromCtx(ctx)}

	signaler, err := newSignaler(ctx)
	require.NoError(t, err)
	defer signaler.Close()

	p2pconsts.SignalerURL = "ws://" + signalerAddr

	peer0PubKey, peer0PrivKey, err := ed25519.GenerateKey(rand.Reader)
	peer1PubKey, peer1PrivKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	const (
		peer0Name   = "peer0"
		peer1Name   = "peer1"
		networkID   = "network"
		psk         = "psk"
		networkCIDR = "fd51:2eaf:7a4e::/64"
	)

	p2pPeer0, err := p2p.NewP2P(
		ctx,
		peer0PrivKey,
		peer0Name,
		networkID,
		[]byte(psk),
		networkCIDR,
		nil,
		nil,
	)
	require.NoError(t, err)

	err = p2pPeer0.Start(ctx)
	require.NoError(t, err)

	p2pPeer1, err := p2p.NewP2P(
		ctx,
		peer1PrivKey,
		peer1Name,
		networkID,
		[]byte(psk),
		networkCIDR,
		nil,
		nil,
	)
	require.NoError(t, err)

	time.Sleep(1 * time.Second)

	err = p2pPeer1.Start(ctx)
	require.NoError(t, err)

	//p2pPeer0.WaitForPeer(peer1PubKey)
	//p2pPeer1.WaitForPeer(peer0PubKey)
	time.Sleep(5 * time.Second)

	peer0Peers, getPeers0Err := p2pPeer0.GetPeers()
	peer1Peers, getPeers1Err := p2pPeer1.GetPeers()
	require.NoError(t, getPeers0Err)
	require.Len(t, peer0Peers, 1)
	require.Equal(t, peer1Name, peer0Peers[0].GetName())
	require.Equal(t, types.PeerID(peer1PubKey), peer0Peers[0].GetID())

	require.NoError(t, getPeers1Err)
	require.Len(t, peer1Peers, 1)
	require.Equal(t, peer0Name, peer1Peers[0].GetName())
	require.Equal(t, types.PeerID(peer0PubKey), peer1Peers[0].GetID())

	t.Run("http", func(t *testing.T) {
		var wg sync.WaitGroup
		mux := http.NewServeMux()
		mux.HandleFunc("GET /somePath/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "OK\n")
		})

		finalEndpointListener, err := net.ListenTCP("tcp", &net.TCPAddr{
			IP:   net.ParseIP("127.0.0.1"),
			Port: 0,
		})
		require.NoError(t, err)

		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Debugf(ctx, "the final endpoint server start")
			defer logger.Debugf(ctx, "the final endpoint server ended")

			logger.Infof(ctx, "started the final endpoint server at '%s'", finalEndpointListener.Addr())
			err := http.Serve(finalEndpointListener, mux)
			require.Contains(t, err.Error(), "closed network")
		}()

		httpClient := &http.Client{
			Transport: &http.Transport{
				DialContext: peer0Peers[0].DialContext,
			},
		}

		u := &url.URL{
			Scheme: "http",
			Host:   finalEndpointListener.Addr().String(),
			Path:   "/somePath",
		}

		for i := 0; i < 2; i++ {
			resp, err := httpClient.Get(u.String())
			require.NoError(t, err)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, "OK\n", string(body))
		}

		err = finalEndpointListener.Close()
		require.NoError(t, err)
		wg.Wait()
	})

	cancelFn()
	wg.Wait()
}
