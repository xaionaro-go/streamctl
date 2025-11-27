package goconv

import (
	"fmt"

	"github.com/goccy/go-yaml"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
)

func ProfileGRPC2Go(
	platID streamcontrol.PlatformName,
	profileString string,
) (streamcontrol.AbstractStreamProfile, error) {
	var profile streamcontrol.AbstractStreamProfile
	var err error
	switch platID {
	case obs.ID:
		profile = &obs.StreamProfile{}
	case twitch.ID:
		profile = &twitch.StreamProfile{}
	case kick.ID:
		profile = &kick.StreamProfile{}
	case youtube.ID:
		profile = &youtube.StreamProfile{}
	default:
		return nil, fmt.Errorf("unexpected platform ID: '%s'", platID)
	}
	err = yaml.Unmarshal([]byte(profileString), profile)
	if err != nil {
		return nil, fmt.Errorf("(goconv) unable to unserialize the profile: %w", err)
	}
	return profile, nil
}

func ProfileGo2GRPC(
	profile streamcontrol.AbstractStreamProfile,
) (string, error) {
	b, err := yaml.Marshal(profile)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
