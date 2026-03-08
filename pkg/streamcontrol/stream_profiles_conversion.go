package streamcontrol

import (
	"github.com/goccy/go-yaml"
)

func GetStreamProfiles[S StreamProfile](
	streamProfiles map[StreamID]StreamProfiles[RawMessage],
) map[StreamID]StreamProfiles[S] {
	s := make(map[StreamID]StreamProfiles[S], len(streamProfiles))
	for sID, sProfs := range streamProfiles {
		s[sID] = make(map[ProfileName]S, len(sProfs))
		for k, p := range sProfs {
			var v S
			b := p
			if err := yaml.Unmarshal(b, &v); err != nil {
				panic(err)
			}
			s[sID][k] = v
		}
	}
	return s
}

func ToStreamProfiles[S StreamProfile](
	in map[StreamID]map[ProfileName]S,
) map[StreamID]StreamProfiles[RawMessage] {
	m := make(map[StreamID]StreamProfiles[RawMessage], len(in))
	for sID, sProfs := range in {
		m[sID] = make(StreamProfiles[RawMessage], len(sProfs))
		for k, v := range sProfs {
			m[sID][k] = ToRawMessage(v)
		}
	}
	return m
}
