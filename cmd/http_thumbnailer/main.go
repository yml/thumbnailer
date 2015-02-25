package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/yml/nsqthumbnailer"
)

var (
	addr      = flag.String("addr", "127.0.0.1:9900", "http addr (default is 127.0.0.1:9900)")
	srcFolder = flag.String("srcFolder", "", "Source folder including the scheme (file:///tmp/my.jpg)")
	dstFolder = flag.String("dstFolder", "", "Destination folder including the scheme (file:///tmp/my.jpg)")
	URLNames  = make(map[string]string)
)

func processThumbResults(tm nsqthumbnailer.ThumbnailerMessage) ([]nsqthumbnailer.ThumbnailResult, error) {
	resultChan := tm.GenerateThumbnails()
	results := make([]nsqthumbnailer.ThumbnailResult, 0)
	for result := range resultChan {
		results = append(results, result)
	}

	for _, result := range results {
		if result.Err != nil {
			err := fmt.Errorf("Error: At least one thumb generation failed - %s", result.Err, results)
			return results, err
		}
	}

	if tm.DeleteSrc == true {
		log.Println("Deleting", tm.SrcImage)
		err := tm.DeleteImage()
		if err != nil {
			return results, err
		}
	}
	return results, nil

}

// ThumbHandler is an http endpoint that generate the requested thumb when it receives a GET request :
// * 50x50/my-picture.jpg
func ThumbHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		var width, height int
		var filename string
		path := strings.TrimPrefix(r.URL.Path, URLNames["/thumb/"])
		fmt.Sscanf(path, "%dx%d/%s", &width, &height, &filename)
		fmt.Printf("[DEBUG] width: %d , height: %d, filename: %s ", width, height, filename)
		opt := nsqthumbnailer.ThumbnailOpt{
			Width:  width,
			Height: height,
		}
		// build the thumbReg and generate the thumb and return it or redirect
		tm := nsqthumbnailer.ThumbnailerMessage{}
		// TODO (yml) generalized this approach to support other scheme
		// Assume file:// to start
		// there is security implication that need to be verified here.
		tm.SrcImage = filepath.Join(*srcFolder, filename)
		tm.DstFolder = *dstFolder
		tm.Opts = append(tm.Opts, opt)
		results, err := processThumbResults(tm)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		body, err := json.Marshal(results)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		w.Write(body)
		return

	} else {
		http.Error(w, fmt.Sprintf("Request method not supported: %s", r.Method), http.StatusBadRequest)
		return
	}

}

// ThumbsHandler generates the requested thumbs. It accepts the request as :
// * GET (/thumbs/<base64 encoded json request>)
// * POST the json thumbnail request
// curl 127.0.0.1:9900/thumbs/ -d '{"srcImage": "s3://nsq-thumb-src-test/baignade.jpg", "opts": [{"width":0, "height":350}], "dstFolder":"s3://nsq-thumb-dst-test/"}']
func ThumbsHandler(w http.ResponseWriter, r *http.Request) {
	var thumbReq bytes.Buffer
	if r.Method == "GET" {
		// In this case we are going to look for the thumbReq in the URL.
		// It should be base 64 encoded
		path := strings.TrimPrefix(r.URL.Path, URLNames["/thumbs/"])
		_, err := io.Copy(&thumbReq, base64.NewDecoder(base64.URLEncoding, strings.NewReader(path)))
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to decode the thumb generation request: %s", err), http.StatusBadRequest)
			return
		}
	} else if r.Method == "POST" {
		// Grab the JSON representation of thumbReq directly from the r.Body
		_, err := io.Copy(&thumbReq, r.Body)
		defer r.Body.Close()
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to decode the thumb generation request: %s", err), http.StatusBadRequest)
			return
		}
	} else {
		http.Error(w, fmt.Sprintf("Request method not supported: %s", r.Method), http.StatusBadRequest)
		return
	}
	fmt.Printf("thumbReq: %s\n", thumbReq.String())

	tm := nsqthumbnailer.ThumbnailerMessage{}
	err := json.Unmarshal(thumbReq.Bytes(), &tm)
	if err != nil {
		err = errors.New(fmt.Sprintf("ERROR: failed to unmarshal `thumbReq` into a thumbnailerMessage - %s", err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	results, err := processThumbResults(tm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	body, err := json.Marshal(results)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	w.Write(body)
	return
}

// base64EncodeHandler is used for debugging, it generates the base64encoded string required to test ThumbsHandler
func base64EncodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		thumbReq, err := ioutil.ReadAll(r.Body)
		defer r.Body.Close()

		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

		w.Write([]byte("\n"))
		enc := base64.NewEncoder(base64.URLEncoding, w)
		enc.Write(thumbReq)
		enc.Close()
		w.Write([]byte("\n"))
	} else {
		http.Error(w, "This request.Method is not implemented for this endpoint", http.StatusInternalServerError)
	}
}

func main() {
	flag.Parse()
	fmt.Println("Starting HTTP thumbnailer on: ", *addr)
	URLNames["/"] = "/"
	URLNames["/thumbs/"] = "/thumbs/"
	URLNames["/thumb/"] = "/thumb/"
	URLNames["/base64Encode/"] = "/debug-base64Encode/"

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("This is the home page"))
	})

	mux.HandleFunc(URLNames["/base64Encode/"], base64EncodeHandler)
	mux.HandleFunc(URLNames["/thumbs/"], ThumbsHandler)
	mux.HandleFunc(URLNames["/thumb/"], ThumbHandler)
	http.ListenAndServe(*addr, mux)
}
