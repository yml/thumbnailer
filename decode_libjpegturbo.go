// +build libjpegturbo

package thumbnailer

import (
	"image"
	"io"
	"strings"

	"github.com/kjk/golibjpegturbo"
)

// Decode decodes an image that has been encoded in a registered format.
func Decode(r io.Reader, ext string) (image.Image, error) {
	ext = strings.ToLower(ext)
	if ext == ".jpg" || ext == ".jpeg" {
		return golibjpegturbo.Decode(r)
	}
	img, _, err := image.Decode(r)
	return img, err
}
