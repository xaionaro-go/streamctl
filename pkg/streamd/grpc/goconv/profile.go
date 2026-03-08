package goconv

import (
	"fmt"

	"github.com/goccy/go-yaml"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func ProfileGRPC2Go(
	platID streamcontrol.PlatformID,
	profileString string,
) (streamcontrol.AbstractStreamProfile, error) {
	profile := streamcontrol.NewStreamProfile(platID)
	err := yaml.Unmarshal([]byte(profileString), profile)
	if err != nil {
		return nil, fmt.Errorf("(goconv) unable to unserialize the profile: '%s': %w", profileString, err)
	}
	return streamcontrol.ToRawMessage(profile), nil
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
