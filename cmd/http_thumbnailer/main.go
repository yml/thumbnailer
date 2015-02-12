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
	"net/http"
	"strings"

	"github.com/yml/nsqthumbnailer"
)

var (
	addr      = flag.String("addr", "127.0.0.1:9900", "http addr (default is 127.0.0.1:9900)")
	srcFolder = flag.String("srcFolder", "", "Source folder including the scheme (file:///tmp/my.jpg)")
	dstFolder = flag.String("dstFolder", "", "Destination folder including the scheme (file:///tmp/my.jpg)")
	URLNames  = make(map[string]string)
)

func thumbHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		var width, height int
		var filename string
		path := strings.TrimPrefix(r.URL.Path, URLNames["/thumb/"])
		fmt.Sscanf(path, "%dx%d/%s", &width, &height, &filename)
		fmt.Printf("[DEBUG] width: %d , height: %d, filename: %s ", width, height, filename)
		// build the thumbReg and generate the thumb and return it or redirect

	} else {
		http.Error(w, fmt.Sprintf("Request method not supported: %s", r.Method), http.StatusBadRequest)
		return
	}

}

func thumbsHandler(w http.ResponseWriter, r *http.Request) {
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
	resultChan := make(chan nsqthumbnailer.ThumbnailResult)
	go tm.GenerateThumbnails(resultChan)

	results := make([]nsqthumbnailer.ThumbnailResult, 0)
	for i := 0; i < len(tm.Opts); i++ {
		result := <-resultChan
		results = append(results, result)
	}
	for _, r := range results {
		if r.Err != nil {
			err := fmt.Errorf("Error: At least one thumb generation failed - %s", r.Err, results)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if tm.DeleteSrc == true {
		fmt.Println("Deleting", tm.SrcImage)
		err = tm.DeleteImage()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	body, err := json.Marshal(results)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	w.Write(body)
	return
}

func main() {
	flag.Parse()
	fmt.Println("Starting HTTP thumbnailer on: ", *addr)
	URLNames["/"] = "/"
	URLNames["/thumbs/"] = "/thumbs/"
	URLNames["/thumb/"] = "/thumb/"
	URLNames["/base64Encode/"] = "/debug_base64Encode/"

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("This is the home page"))
	})
	mux.HandleFunc(URLNames["/base64Encode/"], func(w http.ResponseWriter, r *http.Request) {
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
	})
	mux.HandleFunc(URLNames["/thumbs/"], thumbsHandler)
	mux.HandleFunc(URLNames["/thumb/"], thumbHandler)
	http.ListenAndServe(*addr, mux)

}
