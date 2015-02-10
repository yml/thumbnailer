package nsqthumbnailer

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"math"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"github.com/kjk/golibjpegturbo"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
	"gopkg.in/amz.v1/aws"
	"gopkg.in/amz.v1/s3"
)

var (
	awsAuth aws.Auth
	formats = map[string]imaging.Format{
		".jpg":  imaging.JPEG,
		".jpeg": imaging.JPEG,
		".png":  imaging.PNG,
		".tif":  imaging.TIFF,
		".tiff": imaging.TIFF,
		".bmp":  imaging.BMP,
		".gif":  imaging.GIF,
	}
)

func init() {
	awsAuth = newAwsAuth()
}

// This function used internally to convert any image type to NRGBA if needed.
// copied from `imaging`
func toNRGBA(img image.Image) *image.NRGBA {
	srcBounds := img.Bounds()
	if srcBounds.Min.X == 0 && srcBounds.Min.Y == 0 {
		if src0, ok := img.(*image.NRGBA); ok {
			return src0
		}
	}
	return imaging.Clone(img)
}

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

func newAwsAuth() aws.Auth {
	// Authenticate and Create an aws S3 service
	auth, err := aws.EnvAuth()
	if err != nil {
		panic(err.Error())
	}
	return auth
}

type imageOpenSaverError struct {
	url *url.URL
}

func (e imageOpenSaverError) Error() string {
	return fmt.Sprintf("imageOpenSaverError with URL:%v", e.url)
}

// ImageOpenSaver interface that can Open and Close images from a given backend:fs,  s3, ...
type ImageOpenSaver interface {
	Open() (image.Image, error)
	Save(img image.Image) error
}

// filesystem implementation of the ImageOpenSaver interface
type fsImageOpenSaver struct {
	URL *url.URL
}

func (s fsImageOpenSaver) Open() (image.Image, error) {
	file, err := os.Open(s.URL.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(s.URL.Path))
	if ext == ".jpg" || ext == ".jpeg" {
		return golibjpegturbo.Decode(file)
	}
	img, _, err := image.Decode(file)
	return img, err
}

// Save saves the image to file with the specified filename.
// The format is determined from the filename extension: "jpg" (or "jpeg"), "png", "gif", "tif" (or "tiff") and "bmp" are supported.
func (s fsImageOpenSaver) Save(img image.Image) error {
	ext := strings.ToLower(filepath.Ext(s.URL.Path))
	f, ok := formats[ext]
	if !ok {
		return imaging.ErrUnsupportedFormat
	}

	file, err := os.Create(s.URL.Path)
	if err != nil {
		return err
	}
	defer file.Close()

	return Encode(file, img, f)
}

// s3 implementation of the s3ImageOpenSaver interface
type s3ImageOpenSaver struct {
	URL *url.URL
}

func (s s3ImageOpenSaver) Open() (image.Image, error) {
	conn := s3.New(awsAuth, aws.USEast)
	bucket := conn.Bucket(s.URL.Host)
	reader, err := bucket.GetReader(s.URL.Path)
	if err != nil {
		return nil, err
	}

	defer reader.Close()
	ext := strings.ToLower(filepath.Ext(s.URL.Path))
	if ext == ".jpg" || ext == ".jpeg" {
		return golibjpegturbo.Decode(reader)
	}
	img, _, err := image.Decode(reader)
	return img, err

}

func (s s3ImageOpenSaver) Save(img image.Image) error {
	var buffer bytes.Buffer
	ext := strings.ToLower(filepath.Ext(s.URL.Path))
	f, ok := formats[ext]
	if !ok {
		return imaging.ErrUnsupportedFormat
	}
	err := imaging.Encode(&buffer, img, f)
	if err != nil {
		log.Println("An error occured while encoding ", s.URL)
		return err
	}
	conn := s3.New(awsAuth, aws.USEast)
	bucket := conn.Bucket(s.URL.Host)

	err = bucket.Put(s.URL.Path, buffer.Bytes(), mime.TypeByExtension(ext), s3.PublicRead)
	if err != nil {
		log.Println("An error occured while putting on S3", s.URL)
		return err
	}
	return nil
}

// NewImageOpenSaver return the relevant implementation of ImageOpenSaver based on
// the url.Scheme
func NewImageOpenSaver(url *url.URL) (ImageOpenSaver, error) {
	switch url.Scheme {
	case "file":
		return &fsImageOpenSaver{url}, nil
	case "s3":
		return &s3ImageOpenSaver{url}, nil
	default:
		return nil, imageOpenSaverError{url}
	}
}

type rectangle struct {
	Min [2]int `json:"min"`
	Max [2]int `json:"max"`
}

func (r *rectangle) String() string {
	return fmt.Sprintf("min: %v, max: %v", r.Min, r.Max)
}

func (r *rectangle) newImageRect() image.Rectangle {
	return image.Rect(r.Min[0], r.Min[1], r.Max[0], r.Max[1])
}

type ThumbnailOpt struct {
	DstImage string     `json:"dstImage,omitempty"`
	Rect     *rectangle `json:"rect,omitempty"`
	Width    int        `json:"width"`
	Height   int        `json:"height"`
}

type ThumbnailerMessage struct {
	SrcImage  string         `json:"srcImage"`
	DeleteSrc bool           `json:"deleteSrc",omitempty"`
	DstFolder string         `json:"dstFolder"`
	Opts      []ThumbnailOpt `json:"opts"`
}

func (tm *ThumbnailerMessage) thumbURL(baseName string, opt ThumbnailOpt) (*url.URL, error) {
	if opt.DstImage == "" {
		fURL, err := url.Parse(tm.DstFolder)
		if err != nil {
			return nil, fmt.Errorf("An error occured while parsing the DstFolder %s", err)
		}
		// TODO (yml): I am pretty sure that we do not really want to always do this.
		ext := filepath.Ext(tm.SrcImage)
		baseName = strings.TrimSuffix(baseName, ext)
		ext = strings.ToLower(ext)

		if opt.Rect != nil {
			fURL.Path = filepath.Join(
				fURL.Path,
				fmt.Sprintf("%s_c%d-%d-%d-%d_s%dx%d%s", baseName, opt.Rect.Min[0], opt.Rect.Min[1], opt.Rect.Max[0], opt.Rect.Max[1], opt.Width, opt.Height, ext))
		} else if opt.Width == 0 && opt.Height == 0 {
			fURL.Path = filepath.Join(fURL.Path, baseName)
		} else {
			fURL.Path = filepath.Join(fURL.Path, fmt.Sprintf("%s_s%dx%d%s", baseName, opt.Width, opt.Height, ext))
		}
		return fURL, nil
	} else {
		fURL, err := url.Parse(opt.DstImage)
		if err != nil {
			return nil, fmt.Errorf("An error occured while parsing the DstImage %s", err)
		}
		return fURL, nil
	}
}

// Resize the src image to the biggest thumb sizes in tm.opts.
func (tm *ThumbnailerMessage) maxThumbnail(src image.Image) image.Image {
	maxW, maxH := 0, 0
	srcW := src.Bounds().Max.X
	srcH := src.Bounds().Max.Y
	for _, opt := range tm.Opts {
		dstW, dstH := opt.Width, opt.Height
		// if new width or height is 0 then preserve aspect ratio, minimum 1px
		if dstW == 0 {
			tmpW := float64(dstH) * float64(srcW) / float64(srcH)
			dstW = int(math.Max(1.0, math.Floor(tmpW+0.5)))
		}
		if dstH == 0 {
			tmpH := float64(dstW) * float64(srcH) / float64(srcW)
			dstH = int(math.Max(1.0, math.Floor(tmpH+0.5)))
		}
		if dstW > maxW {
			maxW = dstW
		}
		if dstH > maxH {
			maxH = dstH
		}
	}
	fmt.Println("thumbnail max: ", maxW, maxH, "for :", tm.Opts)
	return imaging.Resize(src, maxW, maxH, imaging.CatmullRom)
}

func (tm *ThumbnailerMessage) generateThumbnail(errorChan chan error, srcURL *url.URL, img image.Image, opt ThumbnailOpt) {
	timerStart := time.Now()
	var thumbImg *image.NRGBA
	if opt.Rect != nil {
		img = imaging.Crop(img, opt.Rect.newImageRect())
	}

	if opt.Width == 0 && opt.Height == 0 {
		thumbImg = toNRGBA(img)
	} else {
		thumbImg = imaging.Resize(img, opt.Width, opt.Height, imaging.CatmullRom)
	}

	// TODO (yml) not sure we always want to do this
	thumBounds := thumbImg.Bounds()
	if opt.Width == 0 {
		opt.Width = thumBounds.Max.X
	}
	if opt.Height == 0 {
		opt.Height = thumBounds.Max.Y
	}

	thumbURL, err := tm.thumbURL(filepath.Base(srcURL.Path), opt)
	if err != nil {
		log.Println("An error occured while contstructing thumbURL for", tm.SrcImage, err)
	}
	timerThumbDone := time.Now()
	log.Println("thumb :", thumbURL, " generated in : ", timerThumbDone.Sub(timerStart))

	timerSaveStart := time.Now()
	thumb, err := NewImageOpenSaver(thumbURL)
	if err != nil {
		log.Println("An error occured while creating an instance of ImageOpenSaver for", thumbURL, err)
		errorChan <- err
		return
	}
	err = thumb.Save(thumbImg)
	if err != nil {
		log.Println("An error occured while saving,", thumbURL, err)
		errorChan <- err
		return
	}
	errorChan <- nil
	timerEnd := time.Now()
	log.Println("thumb :", thumbURL, " saved in : ", timerEnd.Sub(timerSaveStart))
	return
}

func (tm *ThumbnailerMessage) GenerateThumbnails(errChan chan error) {
	sURL, err := url.Parse(tm.SrcImage)
	if err != nil {
		log.Println("An error occured while parsing the SrcImage", tm.SrcImage, err)
		errChan <- err
		return
	}
	src, err := NewImageOpenSaver(sURL)
	if err != nil {
		log.Println("An error occured while creating an instance of ImageOpenSaver", tm.SrcImage, err)
		errChan <- err
		return
	}
	img, err := src.Open()
	if err != nil {
		log.Println("An error occured while opening SrcImage", tm.SrcImage, err)
		errChan <- err
		return
	}
	// From now on we will deal with an NRGBA image
	img = toNRGBA(img)

	var maxThumb image.Image
	if len(tm.Opts) > 1 {
		// The resized image will be used to generate all the thumbs
		maxThumb = tm.maxThumbnail(img)
	}

	errorChan := make(chan error, 1)
	for _, opt := range tm.Opts {
		if opt.Rect == nil && maxThumb != nil {
			go tm.generateThumbnail(errorChan, sURL, maxThumb, opt)
		} else {
			// we can't use the maxThumb optimization
			go tm.generateThumbnail(errorChan, sURL, img, opt)
		}
	}
	var firstErr error
	for i := 0; i < len(tm.Opts); i++ {
		err := <-errorChan
		if err != nil && firstErr == nil {
			// We preserve the first error
			firstErr = err
		}
	}
	errChan <- firstErr
	return
}

func (tm *ThumbnailerMessage) DeleteImage() error {
	sURL, err := url.Parse(tm.SrcImage)
	if err != nil {
		log.Println("An error occured while parsing the SrcImage", tm.SrcImage, err)
		return err
	}
	if sURL.Scheme == "file" {
		err := os.Remove(sURL.Path)
		if err != nil {
			return fmt.Errorf("Failed to remove %s,%s", sURL.Path, err)
		}
	} else {
		return fmt.Errorf("DeleteImage is not implemented for %s", tm.SrcImage)
	}
	return nil
}
