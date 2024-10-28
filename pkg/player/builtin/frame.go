package builtin

import "image"

type frameAudio struct {
	data []byte
}

type frameVideo struct {
	image.Image
}
