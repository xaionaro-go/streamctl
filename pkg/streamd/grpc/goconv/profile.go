package goconv

import (
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"gopkg.in/yaml.v2"
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
	case youtube.ID:
		profile = &youtube.StreamProfile{}
	default:
		return nil, fmt.Errorf("unexpected platform ID: '%s'", platID)
	}
	err = yaml.Unmarshal([]byte(profileString), profile)
	if err != nil {
		return nil, fmt.Errorf("unable to unserialize the profile: %w", err)
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
