package streamd

import (
	"fmt"
	"hash/crc64"
	"image"
)

func newImageHash(img image.Image) (imageHash, error) {
	switch img := img.(type) {
	case *image.RGBA:
		return newImageHashFromRGBA(img), nil
	case *image.Gray:
		return newImageHashFromGray(img), nil
	default:
		return 0, fmt.Errorf("the support of %T is not implemented", img)
	}
}

var crc64Table = crc64.MakeTable(crc64.ECMA)

func newImageHashFromRGBA(img *image.RGBA) imageHash {
	return imageHash(crc64.Checksum(img.Pix, crc64Table))
}

func newImageHashFromGray(img *image.Gray) imageHash {
	return imageHash(crc64.Checksum(img.Pix, crc64Table))
}
