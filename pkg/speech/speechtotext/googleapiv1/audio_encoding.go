package googleapiv1

import (
	"fmt"

	"cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/xaionaro-go/streamctl/pkg/audio"
)

func AudioEncodingToThrift(
	audioEncoding audio.Encoding,
) (speechpb.RecognitionConfig_AudioEncoding, int32, error) {
	switch audioEncoding := audioEncoding.(type) {
	case audio.AudioEncodingPCM:
		switch audioEncoding.PCMFormat {
		case audio.PCMFormatS16LE:
			return speechpb.RecognitionConfig_LINEAR16, int32(audioEncoding.SampleRate), nil
		default:
			return 0, 0, fmt.Errorf("google Speech APIv1 does not support PCM format %s", audioEncoding.PCMFormat) // the linter complains if I write "Google" from the capital "G" here...
		}
	default:
		return 0, 0, fmt.Errorf("do not know how to convert %T", audioEncoding)
	}
}
