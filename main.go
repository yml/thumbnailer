package main

import (
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
	"syscall"
	"time"

	"github.com/bitly/go-nsq"
	"github.com/bitly/nsq/util"
	"github.com/disintegration/imaging"
)

var (
	showVersion = flag.Bool("version", false, "print version string")
	topic       = flag.String("topic", "", "NSQ topic")
	channel     = flag.String("channel", "", "NSQ channel")

	maxInFlight = flag.Int("max-in-flight", 200, "max number of messages to allow in flight")

	consumerOpts     = util.StringArray{}
	nsqdTCPAddrs     = util.StringArray{}
	lookupdHTTPAddrs = util.StringArray{}
)

func init() {
	flag.Var(&consumerOpts, "consumer-opt", "option to passthrough to nsq.Consumer (may be given multiple times, http://godoc.org/github.com/bitly/go-nsq#Config)")
	flag.Var(&nsqdTCPAddrs, "nsqd-tcp-address", "nsqd TCP address (may be given multiple times)")
	flag.Var(&lookupdHTTPAddrs, "lookupd-http-address", "lookupd HTTP address (may be given multiple times)")
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

// NewImageOpenSaver return the relevant implementation of ImageOpenSaver based on
// the url.Scheme
func NewImageOpenSaver(url *url.URL) (ImageOpenSaver, error) {
	switch url.Scheme {
	case "file":
		return &fsImageOpenSaver{url}, nil
	default:
		return nil, imageOpenSaverError{url}
	}

}

type thumbnailOpt struct {
	Width, Height int
}

type thumbnailerMessage struct {
	SrcImage  string
	DstFolder string
	Opts      []thumbnailOpt
}

func (tm *thumbnailerMessage) thumbURL(baseName string, width, height int) *url.URL {
	fURL, err := url.Parse(tm.DstFolder)
	if err != nil {
		log.Fatalln("An error occured while parsing the DstFolder", err)
	}

	fURL.Path = filepath.Join(fURL.Path, fmt.Sprintf("%s-%d_%d.jpeg", baseName, width, height))
	return fURL
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
		go func(errorChan chan error, opt thumbnailOpt) {
			thumbImg := imaging.Resize(img, opt.Width, opt.Height, imaging.CatmullRom)
			thumbURL := tm.thumbURL(filepath.Base(sURL.Path), opt.Width, opt.Height)
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
		}(errorChan, opt)
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

	consumer.AddHandler(&thumbnailerHandler{})

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
