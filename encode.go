package thumbnailer

import (
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/disintegration/imaging"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// Encode writes the image img to w in the specified format (JPEG, PNG, GIF, TIFF or BMP).
// copied from `imaging` and modified
func Encode(w io.Writer, img image.Image, format imaging.Format) error {
	var err error
	switch format {
	case imaging.JPEG:
		var rgba *image.RGBA
		if nrgba, ok := img.(*image.NRGBA); ok {
			if nrgba.Opaque() {
				rgba = &image.RGBA{
					Pix:    nrgba.Pix,
					Stride: nrgba.Stride,
					Rect:   nrgba.Rect,
				}
			}
		}
		if rgba != nil {
			err = jpeg.Encode(w, rgba, &jpeg.Options{Quality: 75})
		} else {
			err = jpeg.Encode(w, img, &jpeg.Options{Quality: 75})
		}

	case imaging.PNG:
		err = png.Encode(w, img)
	case imaging.GIF:
		err = gif.Encode(w, img, &gif.Options{NumColors: 256})
	case imaging.TIFF:
		err = tiff.Encode(w, img, &tiff.Options{Compression: tiff.Deflate, Predictor: true})
	case imaging.BMP:
		err = bmp.Encode(w, img)
	default:
		err = imaging.ErrUnsupportedFormat
	}
	return err
}
