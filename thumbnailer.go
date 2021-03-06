package thumbnailer

import (
	"bytes"
	"fmt"
	"image"
	"log"
	"math"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"
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
	return Decode(file, filepath.Ext(s.URL.Path))
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
	return Decode(reader, filepath.Ext(s.URL.Path))
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

type ThumbnailResult struct {
	Thumbnail *url.URL
	Err       error
}

func (tm *ThumbnailerMessage) thumbURL(opt ThumbnailOpt) (*url.URL, error) {
	baseName := filepath.Base(tm.SrcImage)
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

func (tm *ThumbnailerMessage) Open() (image.Image, error) {
	sURL, err := url.Parse(tm.SrcImage)
	if err != nil {
		return nil, err
	}
	src, err := NewImageOpenSaver(sURL)
	if err != nil {
		return nil, err
	}
	return src.Open()
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

func (tm *ThumbnailerMessage) generateThumbnail(img image.Image, opt ThumbnailOpt) ThumbnailResult {
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

	thumbURL, err := tm.thumbURL(opt)
	if err != nil {
		log.Println("An error occured while contstructing thumbURL for", tm.SrcImage, err)
	}
	timerThumbDone := time.Now()
	log.Println("thumb :", thumbURL, " generated in : ", timerThumbDone.Sub(timerStart))

	timerSaveStart := time.Now()
	thumb, err := NewImageOpenSaver(thumbURL)
	if err != nil {
		log.Println("An error occured while creating an instance of ImageOpenSaver for", thumbURL, err)
		return ThumbnailResult{nil, err}
	}
	err = thumb.Save(thumbImg)
	if err != nil {
		log.Println("An error occured while saving,", thumbURL, err)
		return ThumbnailResult{nil, err}
	}
	timerEnd := time.Now()
	log.Println("thumb :", thumbURL, " saved in : ", timerEnd.Sub(timerSaveStart))
	return ThumbnailResult{thumbURL, nil}
}

func (tm *ThumbnailerMessage) GenerateThumbnails() <-chan ThumbnailResult {
	resultChan := make(chan ThumbnailResult)
	go func(rc chan<- ThumbnailResult) {
		defer close(rc)
		img, err := tm.Open()
		if err != nil {
			log.Println("An error occured while opening SrcImage", tm.SrcImage, err)
			rc <- ThumbnailResult{nil, err}
			return
		}
		// From now on we will deal with an NRGBA image
		img = toNRGBA(img)

		var maxThumb image.Image
		if len(tm.Opts) > 1 {
			// The resized image will be used to generate all the thumbs
			maxThumb = tm.maxThumbnail(img)
		}

		var wg sync.WaitGroup
		for _, opt := range tm.Opts {
			wg.Add(1)
			// TODO (yml) cap the number goroutines
			go func(out chan<- ThumbnailResult, opt ThumbnailOpt) {
				defer wg.Done()
				if opt.Rect == nil && maxThumb != nil {
					out <- tm.generateThumbnail(maxThumb, opt)
				} else {
					// we can't use the maxThumb optimization
					out <- tm.generateThumbnail(img, opt)
				}
			}(rc, opt)
		}
		wg.Wait()
		return
	}(resultChan)
	return resultChan
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
