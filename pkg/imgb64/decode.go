package imgb64

import (
	"encoding/base64"
	"fmt"
	"strings"
)

func Decode(in string) ([]byte, string, error) {
	if !strings.HasPrefix(in, "data:") {
		return nil, "", fmt.Errorf("the input does not start with 'data:'")
	}
	in = in[len("data:"):]

	splitIdx := strings.Index(in, ";")
	if splitIdx == -1 {
		return nil, "", fmt.Errorf("separator ';' is not found in the input")
	}

	mimeType := in[:splitIdx]
	if len(mimeType) > 50 {
		return nil, "", fmt.Errorf("MIME type is too big (%d > 50)", len(mimeType))
	}

	in = in[splitIdx+1:]

	if !strings.HasPrefix(in, "base64,") {
		return nil, "", fmt.Errorf("the data is not prefixed with 'base64,'")
	}
	in = in[len("base64,"):]

	data, err := base64.StdEncoding.DecodeString(in)
	if err != nil {
		return nil, "", fmt.Errorf("unable to decode the base64 input: %w", err)
	}

	return data, mimeType, nil
}
