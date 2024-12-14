package streamtypes

import (
	"encoding/json"
	"fmt"
	"image"
	"strings"

	"github.com/goccy/go-yaml"
)

type VideoConvertConfig struct {
	OutputAudioTracks []AudioTrackConfig
	OutputVideoTracks []VideoTrackConfig
}

type VideoTrackConfig struct {
	InputVideoTrackIDs []uint
	EncodeVideoConfig
}

type EncodeVideoConfig struct {
	FlipV   bool             `json:"flip_v,omitempty"  yaml:"flip_v,omitempty"`
	FlipH   bool             `json:"flip_h,omitempty"  yaml:"flip_h,omitempty"`
	Crop    *image.Rectangle `json:"crop,omitempty"    yaml:"crop,omitempty"`
	Scale   *image.Point     `json:"scale,omitempty"   yaml:"scale,omitempty"`
	Codec   VideoCodec       `json:"codec,omitempty"   yaml:"codec,omitempty"`
	Quality VideoQuality     `json:"quality,omitempty" yaml:"quality,omitempty"`
}

func (c *EncodeVideoConfig) UnmarshalJSON(b []byte) (_err error) {
	c.Quality = videoQualitySerializable{}
	err := json.Unmarshal(b, c)
	if err != nil {
		return fmt.Errorf("unable to un-JSON-ize: %w", err)
	}
	if c.Quality != nil {
		c.Quality, err = c.Quality.(videoQualitySerializable).Convert()
		if err != nil {
			return fmt.Errorf("unable to convert the 'quality' field: %w", err)
		}
	}
	return nil
}

func (c *EncodeVideoConfig) UnmarshalYAML(b []byte) (_err error) {
	c.Quality = videoQualitySerializable{}
	err := yaml.Unmarshal(b, c)
	if err != nil {
		return fmt.Errorf("unable to un-JSON-ize: %w", err)
	}
	if c.Quality != nil {
		c.Quality, err = c.Quality.(videoQualitySerializable).Convert()
		if err != nil {
			return fmt.Errorf("unable to convert the 'quality' field: %w", err)
		}
	}
	return nil
}

type AudioTrackConfig struct {
	InputAudioTrackIDs []uint
	EncodeAudioConfig
}

type EncodeAudioConfig struct {
	Codec   AudioCodec   `json:"codec,omitempty"   yaml:"codec,omitempty"`
	Quality AudioQuality `json:"quality,omitempty" yaml:"quality,omitempty"`
}

func (c *EncodeAudioConfig) UnmarshalJSON(b []byte) (_err error) {
	c.Quality = audioQualitySerializable{}
	err := json.Unmarshal(b, c)
	if err != nil {
		return fmt.Errorf("unable to un-JSON-ize: %w", err)
	}
	if c.Quality != nil {
		c.Quality, err = c.Quality.(audioQualitySerializable).Convert()
		if err != nil {
			return fmt.Errorf("unable to convert the 'quality' field: %w", err)
		}
	}
	return nil
}

func (c *EncodeAudioConfig) UnmarshalYAML(b []byte) (_err error) {
	c.Quality = audioQualitySerializable{}
	err := yaml.Unmarshal(b, c)
	if err != nil {
		return fmt.Errorf("unable to un-JSON-ize: %w", err)
	}
	if c.Quality != nil {
		c.Quality, err = c.Quality.(audioQualitySerializable).Convert()
		if err != nil {
			return fmt.Errorf("unable to convert the 'quality' field: %w", err)
		}
	}
	return nil
}

type AudioQuality interface {
	audioQuality()
	typeName() string
	setValues(vq audioQualitySerializable) error
}

type AudioQualityConstantBitrate uint

func (AudioQualityConstantBitrate) typeName() string {
	return "constant_bitrate"
}

func (AudioQualityConstantBitrate) audioQuality() {}

func (aq AudioQualityConstantBitrate) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":    aq.typeName(),
		"bitrate": uint(aq),
	})
}

func (aq *AudioQualityConstantBitrate) setValues(in audioQualitySerializable) error {
	bitrate, ok := in["bitrate"].(float64)
	if !ok {
		return fmt.Errorf("have not found float64 value using key 'bitrate' in %#+v", in)
	}

	*aq = AudioQualityConstantBitrate(bitrate)
	return nil
}

type audioQualitySerializable map[string]any

func (audioQualitySerializable) audioQuality() {}

func (aq audioQualitySerializable) typeName() string {
	result, _ := aq["type"].(string)
	return result
}

func (aq audioQualitySerializable) setValues(in audioQualitySerializable) error {
	for k := range aq {
		delete(aq, k)
	}
	for k, v := range in {
		aq[k] = v
	}
	return nil
}

func (aq audioQualitySerializable) Convert() (AudioQuality, error) {
	typeName, ok := aq["type"].(string)
	if !ok {
		return nil, fmt.Errorf("field 'type' is not set")
	}

	var r AudioQuality
	for _, sample := range []AudioQuality{
		ptr(AudioQualityConstantBitrate(0)),
	} {
		if sample.typeName() == typeName {
			r = sample
			break
		}
	}
	if r == nil {
		return nil, fmt.Errorf("unknown type '%s'", typeName)
	}

	if err := r.setValues(aq); err != nil {
		return nil, fmt.Errorf("unable to convert the value: %w", err)
	}
	return r, nil
}

type AudioCodec uint

const (
	AudioCodecUndefined = AudioCodec(iota)
	AudioCodecCopy
	AudioCodecAAC
	AudioCodecVorbis
	AudioCodecOpus
	EndOfAudioCodec
)

func (ac *AudioCodec) String() string {
	if ac == nil {
		return "null"
	}

	switch *ac {
	case AudioCodecUndefined:
		return "<undefined>"
	case AudioCodecCopy:
		return "<copy>"
	case AudioCodecAAC:
		return "aac"
	case AudioCodecVorbis:
		return "vorbis"
	case AudioCodecOpus:
		return "opus"
	}
	return fmt.Sprintf("unexpected_audio_codec_id_%d", uint(*ac))
}

func (ac AudioCodec) MarshalJSON() ([]byte, error) {
	return []byte(`"` + ac.String() + `"`), nil
}

func (ac *AudioCodec) UnmarshalJSON(b []byte) error {
	if ac == nil {
		return fmt.Errorf("AudioCodec is nil")
	}
	s := strings.ToLower(strings.Trim(string(b), `"`))
	for cmp := AudioCodecUndefined; cmp < EndOfAudioCodec; cmp++ {
		if cmp.String() == s {
			*ac = cmp
			return nil
		}
	}
	return fmt.Errorf("unknown value of the AudioCodec: '%s'", s)
}

type VideoQuality interface {
	videoQuality()
	typeName() string
	setValues(vq videoQualitySerializable) error
}

type VideoQualityConstantBitrate uint

func (VideoQualityConstantBitrate) typeName() string {
	return "constant_bitrate"
}

func (VideoQualityConstantBitrate) videoQuality() {}

func (vq VideoQualityConstantBitrate) MarshalJSON() ([]byte, error) {
	return json.Marshal(videoQualitySerializable{
		"type":    vq.typeName(),
		"bitrate": uint(vq),
	})
}

func (vq *VideoQualityConstantBitrate) setValues(in videoQualitySerializable) error {
	bitrate, ok := in["bitrate"].(float64)
	if !ok {
		return fmt.Errorf("have not found float64 value using key 'bitrate' in %#+v", in)
	}

	*vq = VideoQualityConstantBitrate(bitrate)
	return nil
}

type VideoQualityConstantQuality uint8

func (VideoQualityConstantQuality) typeName() string {
	return "constant_quality"
}

func (VideoQualityConstantQuality) videoQuality() {}

func (vq VideoQualityConstantQuality) MarshalJSON() ([]byte, error) {
	return json.Marshal(videoQualitySerializable{
		"type":    vq.typeName(),
		"quality": uint(vq),
	})
}

func (vq *VideoQualityConstantQuality) setValues(in videoQualitySerializable) error {
	bitrate, ok := in["quality"].(float64)
	if !ok {
		return fmt.Errorf("have not found float64 value using key 'quality' in %#+v", in)
	}

	*vq = VideoQualityConstantQuality(bitrate)
	return nil
}

type videoQualitySerializable map[string]any

func (videoQualitySerializable) videoQuality() {}

func (vq videoQualitySerializable) typeName() string {
	result, _ := vq["type"].(string)
	return result
}

func (vq videoQualitySerializable) setValues(in videoQualitySerializable) error {
	for k := range vq {
		delete(vq, k)
	}
	for k, v := range in {
		vq[k] = v
	}
	return nil
}

func (vq videoQualitySerializable) Convert() (VideoQuality, error) {
	typeName, ok := vq["type"].(string)
	if !ok {
		return nil, fmt.Errorf("field 'type' is not set")
	}

	var r VideoQuality
	for _, sample := range []VideoQuality{
		ptr(VideoQualityConstantBitrate(0)),
		ptr(VideoQualityConstantQuality(0)),
	} {
		if sample.typeName() == typeName {
			r = sample
			break
		}
	}
	if r == nil {
		return nil, fmt.Errorf("unknown type '%s'", typeName)
	}

	if err := r.setValues(vq); err != nil {
		return nil, fmt.Errorf("unable to convert the value: %w", err)
	}
	return r, nil
}

func ptr[T any](in T) *T {
	return &in
}

type VideoCodec uint

const (
	VideoCodecUndefined = VideoCodec(iota)
	VideoCodecCopy
	VideoCodecH264
	VideoCodecHEVC
	VideoCodecAV1
	EndOfVideoCodec
)

func (vc *VideoCodec) String() string {
	if vc == nil {
		return "null"
	}

	switch *vc {
	case VideoCodecUndefined:
		return "<undefined>"
	case VideoCodecCopy:
		return "<copy>"
	case VideoCodecH264:
		return "h264"
	case VideoCodecHEVC:
		return "hevc"
	case VideoCodecAV1:
		return "av1"
	}
	return fmt.Sprintf("unexpected_video_codec_id_%d", uint(*vc))
}

func (vc VideoCodec) MarshalJSON() ([]byte, error) {
	return []byte(`"` + vc.String() + `"`), nil
}

func (vc *VideoCodec) UnmarshalJSON(b []byte) error {
	if vc == nil {
		return fmt.Errorf("VideoCodec is nil")
	}
	s := strings.ToLower(strings.Trim(string(b), `"`))
	for cmp := VideoCodecUndefined; cmp < EndOfVideoCodec; cmp++ {
		if cmp.String() == s {
			*vc = cmp
			return nil
		}
	}
	return fmt.Errorf("unknown value of the VideoCodec: '%s'", s)
}
