package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/yml/nsqthumbnailer"
)

var (
	addr     = flag.String("addr", "127.0.0.1:9900", "http addr (default is 127.0.0.1:9900)")
	URLNames = make(map[string]string)
)

func thumbHandler(w http.ResponseWriter, r *http.Request) {
	var thumbReq bytes.Buffer
	path := strings.TrimPrefix(r.URL.Path, URLNames["/thumb/"])
	_, err := io.Copy(&thumbReq, base64.NewDecoder(base64.URLEncoding, strings.NewReader(path)))
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to decode the thumb generation request: %s", err), http.StatusBadRequest)
	}
	fmt.Printf("thumbReq: %s\n", thumbReq.String())
	w.Write([]byte("[DEBUG] thumb request:\n"))
	w.Write(thumbReq.Bytes())

	tm := nsqthumbnailer.ThumbnailerMessage{}
	err = json.Unmarshal(thumbReq.Bytes(), &tm)
	if err != nil {
		err = errors.New(fmt.Sprintf("ERROR: failed to unmarshal `thumbReq` into a thumbnailerMessage - %s", err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	errChan := make(chan error)
	go tm.GenerateThumbnails(errChan)
	err = <-errChan
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tm.DeleteSrc == true {
		fmt.Println("Deleting", tm.SrcImage)
		err = tm.DeleteImage()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	return
}

func main() {
	flag.Parse()
	fmt.Println("Starting HTTP thumbnailer on: ", *addr)
	URLNames["/"] = "/"
	URLNames["/thumb/"] = "/thumb/"
	URLNames["/debug/"] = "/debug/"

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("This is the home page"))
	})
	mux.HandleFunc(URLNames["/debug/"], func(w http.ResponseWriter, r *http.Request) {
		thumbReq := []byte("{\"srcImage\": \"file:///home/yml/Dropbox/Devs/golang/nsq_sandbox/nsq-thumb-src-test/baignade.jpg\", \"opts\": [{\"rect\":{\"min\":[200, 200], \"max\":[600,600]},\"width\":150, \"height\":0}], \"dstFolder\":\"file:///home/yml/Dropbox/Devs/golang/nsq_sandbox/nsq-thumb-dst-test/\"}")
		fmt.Printf("thumbReq : %s\n", thumbReq)
		w.Write([]byte("\n"))
		enc := base64.NewEncoder(base64.URLEncoding, w)
		enc.Write(thumbReq)
		enc.Close()
		w.Write([]byte("\n\n"))
	})
	mux.HandleFunc(URLNames["/thumb/"], thumbHandler)
	http.ListenAndServe(*addr, mux)

}
