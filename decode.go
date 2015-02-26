// +build !libjpegturbo

package thumbnailer

import (
	"image"
	"io"
)

// Decode decodes an image that has been encoded in a registered format.
func Decode(r io.Reader, _ string) (image.Image, error) {
	img, _, err := image.Decode(r)
	return img, err
}
