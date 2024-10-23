package observability_test

import (
	"bytes"
	"fmt"
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
		observability.StructFieldSecretsFilter{},
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

	t.Run("error", func(t *testing.T) {
		e0 := fmt.Errorf("some error")
		e1 := fmt.Errorf("the parent error: %w", e0)
		var buf bytes.Buffer
		ll.SetOutput(&buf)
		l.Debugf("%v", e1)
		require.Equal(t, `time=unit-test level=debug msg="the parent error: some error"`+"\n", buf.String())
	})
}
