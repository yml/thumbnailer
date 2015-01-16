package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"log"
	"math/rand"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/bitly/go-nsq"
	"github.com/bitly/nsq/util"
	"github.com/disintegration/imaging"
	"gopkg.in/amz.v1/aws"
	"gopkg.in/amz.v1/s3"
)

var (
	showVersion      = flag.Bool("version", false, "print version string")
	topic            = flag.String("topic", "", "NSQ topic")
	channel          = flag.String("channel", "", "NSQ channel")
	concurrency      = flag.Int("concurrency", 1, "Handler concurrency default is 1")
	maxInFlight      = flag.Int("max-in-flight", 200, "max number of messages to allow in flight")
	consumerOpts     = util.StringArray{}
	nsqdTCPAddrs     = util.StringArray{}
	lookupdHTTPAddrs = util.StringArray{}
	awsAuth          aws.Auth
)

func init() {
	flag.Var(&consumerOpts, "consumer-opt", "option to passthrough to nsq.Consumer (may be given multiple times, http://godoc.org/github.com/bitly/go-nsq#Config)")
	flag.Var(&nsqdTCPAddrs, "nsqd-tcp-address", "nsqd TCP address (may be given multiple times)")
	flag.Var(&lookupdHTTPAddrs, "lookupd-http-address", "lookupd HTTP address (may be given multiple times)")
	awsAuth = newAwsAuth()
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
	return imaging.Open(s.URL.Path)
}

func (s fsImageOpenSaver) Save(img image.Image) error {
	return imaging.Save(img, s.URL.Path)
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
	return imaging.Decode(reader)
}

func (s s3ImageOpenSaver) Save(img image.Image) error {
	var buffer bytes.Buffer
	formats := map[string]imaging.Format{
		".jpg":  imaging.JPEG,
		".jpeg": imaging.JPEG,
		".png":  imaging.PNG,
		".tif":  imaging.TIFF,
		".tiff": imaging.TIFF,
		".bmp":  imaging.BMP,
		".gif":  imaging.GIF,
	}
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

	err = bucket.Put(s.URL.Path, buffer.Bytes(), fmt.Sprintf("image/%s", imaging.JPEG), s3.PublicRead)
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

type thumbnailOpt struct {
	Rect   *rectangle `json:"rect,omitempty"`
	Width  int        `json:"width"`
	Height int        `json:"height"`
}

type thumbnailerMessage struct {
	SrcImage  string         `json:"srcImage"`
	DstFolder string         `json:"dstFolder"`
	Opts      []thumbnailOpt `json:"opts"`
}

func (tm *thumbnailerMessage) thumbURL(baseName string, opt thumbnailOpt) *url.URL {
	fURL, err := url.Parse(tm.DstFolder)
	if err != nil {
		log.Fatalln("An error occured while parsing the DstFolder", err)
	}

	if opt.Rect != nil {
		fURL.Path = filepath.Join(
			fURL.Path,
			fmt.Sprintf("%s_c-%d-%d-%d-%d_s-%d-%d.jpeg", baseName, opt.Rect.Min[0], opt.Rect.Min[1], opt.Rect.Max[0], opt.Rect.Max[1], opt.Width, opt.Height))
	} else if opt.Width == 0 && opt.Height == 0 {
		fURL.Path = filepath.Join(fURL.Path, baseName)
	} else {
		fURL.Path = filepath.Join(fURL.Path, fmt.Sprintf("%s_s-%d-%d.jpeg", baseName, opt.Width, opt.Height))
	}
	return fURL
}

func (tm *thumbnailerMessage) generateThumbnail(errorChan chan error, srcURL *url.URL, img image.Image, opt thumbnailOpt) {
	var thumbImg *image.NRGBA
	if opt.Rect != nil {
		img = imaging.Crop(img, opt.Rect.newImageRect())
	}

	if opt.Width == 0 && opt.Height == 0 {
		thumbImg = imaging.Clone(img)
	} else {
		thumbImg = imaging.Resize(img, opt.Width, opt.Height, imaging.CatmullRom)
	}

	thumbURL := tm.thumbURL(filepath.Base(srcURL.Path), opt)
	log.Println("generating thumb:", thumbURL)

	thumb, err := NewImageOpenSaver(thumbURL)
	if err != nil {
		log.Println("An error occured while creating an instance of ImageOpenSaver", err)
		errorChan <- err
		return
	}
	err = thumb.Save(thumbImg)
	if err != nil {
		log.Println("An error occured while saving the thumb", err)
		errorChan <- err
		return
	}
	errorChan <- nil
	return
}

func (tm *thumbnailerMessage) generateThumbnails() error {
	sURL, err := url.Parse(tm.SrcImage)
	if err != nil {
		log.Println("An error occured while parsing the SrcImage", err)
		return err
	}
	src, err := NewImageOpenSaver(sURL)
	if err != nil {
		log.Println("An error occured while creating an instance of ImageOpenSaver", err)
		return err
	}
	img, err := src.Open()
	if err != nil {
		log.Println("An error occured while opening SrcImage", err)
		return err
	}
	errorChan := make(chan error, 1)
	for _, opt := range tm.Opts {
		go tm.generateThumbnail(errorChan, sURL, img, opt)
	}

	for i := 0; i < len(tm.Opts); i++ {
		select {
		case err := <-errorChan:
			if err != nil {
				return err
			}
		}

	}
	return nil
}

type thumbnailerHandler struct {
	sourceImage      string
	thumbnailCounter int
}

func (th *thumbnailerHandler) HandleMessage(m *nsq.Message) error {
	tm := thumbnailerMessage{}
	err := json.Unmarshal(m.Body, &tm)
	if err != nil {
		log.Printf("ERROR: failed to unmarshal m.Body into a thumbnailerMessage - %s", err)
		return err
	}
	return tm.generateThumbnails()
}

func main() {
	log.Println("Starting nsq_thumbnailing consumer")
	flag.Parse()

	if *showVersion {
		fmt.Printf("nsq_thumbnailer v%s\n", util.BINARY_VERSION)
		return
	}

	if *channel == "" {
		rand.Seed(time.Now().UnixNano())
		*channel = fmt.Sprintf("thumbnailer%06d#ephemeral", rand.Int()%999999)
	}

	if *topic == "" {
		log.Fatal("--topic is required")
	}

	if len(nsqdTCPAddrs) == 0 && len(lookupdHTTPAddrs) == 0 {
		log.Fatal("--nsqd-tcp-address or --lookupd-http-address required")
	}
	if len(nsqdTCPAddrs) > 0 && len(lookupdHTTPAddrs) > 0 {
		log.Fatal("use --nsqd-tcp-address or --lookupd-http-address not both")
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	cfg := nsq.NewConfig()
	cfg.UserAgent = fmt.Sprintf("nsq_thumbnailer/%s go-nsq/%s", util.BINARY_VERSION, nsq.VERSION)
	err := util.ParseOpts(cfg, consumerOpts)
	if err != nil {
		log.Fatal(err)
	}
	cfg.MaxInFlight = *maxInFlight

	consumer, err := nsq.NewConsumer(*topic, *channel, cfg)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("concurrency: ", *concurrency)
	consumer.AddConcurrentHandlers(&thumbnailerHandler{}, *concurrency)

	err = consumer.ConnectToNSQDs(nsqdTCPAddrs)
	if err != nil {
		log.Fatal(err)
	}

	err = consumer.ConnectToNSQLookupds(lookupdHTTPAddrs)
	if err != nil {
		log.Fatal(err)
	}

	for {
		select {
		case <-consumer.StopChan:
			return
		case <-sigChan:
			consumer.Stop()
		}
	}
}
