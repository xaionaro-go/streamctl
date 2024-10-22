package observability_test

import (
	"bytes"
	"runtime"
	"testing"

	"github.com/facebookincubator/go-belt/tool/logger"
	xlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
)

func TestSecretsFilter(t *testing.T) {
	ll := xlogrus.DefaultLogrusLogger()
	ll.Formatter.(*logrus.TextFormatter).TimestampFormat = "unit-test"
	ll.Formatter.(*logrus.TextFormatter).CallerPrettyfier = func(f *runtime.Frame) (function string, file string) {
		return "", ""
	}
	l := xlogrus.New(ll).WithLevel(logger.LevelTrace).WithPreHooks(
		observability.SecretsFilter{},
	)

	t.Run("youtubeConfig", func(t *testing.T) {
		var buf bytes.Buffer
		ll.SetOutput(&buf)
		sample := youtube.PlatformSpecificConfig{
			ChannelID:    "someChannel",
			ClientID:     "someID",
			ClientSecret: "someSecret",
		}
		l.Debugf("%#+v", sample)
		require.Equal(t, `time=unit-test level=debug msg="youtube.PlatformSpecificConfig{ChannelID:\"someChannel\", ClientID:\"\", ClientSecret:\"\", Token:(*oauth2.Token)(nil), CustomOAuthHandler:(youtube.OAuthHandler)(nil), GetOAuthListenPorts:(func() []uint16)(nil)}"`+"\n", buf.String())
	})
}
