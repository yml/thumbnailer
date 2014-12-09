package main

import (
	"encoding/json"
	"flag"
	"fmt"
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

type thumbnailOpt struct {
	Width, Height int
}

type thumbnailerMessage struct {
	SrcImage  string
	DstFolder string
	Opts      []thumbnailOpt
}

func (tm *thumbnailerMessage) srcURL() (*url.URL, error) {
	return url.Parse(tm.SrcImage)
}

func (tm *thumbnailerMessage) dstURL() (*url.URL, error) {
	return url.Parse(tm.DstFolder)
}

func (tm *thumbnailerMessage) thumbPath(baseName string, width, height int) string {
	fURL, err := tm.dstURL()
	if err != nil {
		log.Fatalln("An error occured while parsing the DstFolder", err)
	}

	return filepath.Join(fURL.Path, fmt.Sprintf("%s-%d_%d.jpeg", baseName, width, height))
}

func (tm *thumbnailerMessage) generateThumbnails() error {
	sURL, err := tm.srcURL()
	if err != nil {
		log.Println("An error occured while parsing the SrcImage", err)
		return err
	}

	img, err := imaging.Open(sURL.Path)
	if err != nil {
		log.Println("An error occured while opening SrcImage", err)
		return err
	}
	for _, opt := range tm.Opts {
		thumb := imaging.Resize(img, opt.Width, opt.Height, imaging.CatmullRom)
		tp := tm.thumbPath(filepath.Base(sURL.Path), opt.Width, opt.Height)
		log.Println("Generating thumb:", tp)
		err := imaging.Save(thumb, tp)
		if err != nil {
			log.Fatalln("An error occured while saving the thumb", tp, err)
		}
	}
	return nil
}

type ThumbnailerHandler struct {
	sourceImage      string
	thumbnailCounter int
}

func (th *ThumbnailerHandler) HandleMessage(m *nsq.Message) error {
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

	consumer.AddHandler(&ThumbnailerHandler{})

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
