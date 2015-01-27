package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/yml/nsqthumbnailer"
)

var (
	supportedExt = []string{
		".jpeg", ".jpg", ".gif", ".tiff", ".tif", ".png", ".bmp"}
	srcDir            string
	srcPath           string
	dstDir            string
	thumbOpts         string
	postURL           string
	preserveStructure bool
	tOpts             []nsqthumbnailer.ThumbnailOpt
)

func init() {
	flag.StringVar(&srcDir, "src-directory", "", "Directory containing the src images.")
	flag.StringVar(&dstDir, "dst-directory", "", "Destination Directory.")
	flag.StringVar(&thumbOpts, "thumbnail-options", "", "Thumbnail options")
	flag.StringVar(&postURL, "post-url", "", "Url to post the thumbnail generation request")
	flag.BoolVar(&preserveStructure, "preserve-structure", false, "Preseve the folder structure from `src-directory` to `dst-directory`")
}

func thumbnailFileRequest(file string) error {
	var dstPath string
	if preserveStructure {
		rel, err := filepath.Rel(srcPath, file)
		if err != nil {
			return fmt.Errorf("[ERROR] failed to retrieve the relative path", err)
		}
		dstPath = fmt.Sprintf("%s%s", dstDir, filepath.Dir(rel))
	} else {
		dstPath = dstDir
	}
	tmJson, err := json.Marshal(nsqthumbnailer.ThumbnailerMessage{
		SrcImage:  fmt.Sprintf("file://%s", file),
		DstFolder: dstPath,
		Opts:      tOpts,
	})

	resp, err := http.Post(postURL, "application/json", bytes.NewReader(tmJson))
	if err != nil {
		return fmt.Errorf("[ERROR] An error occured while POSTing the thumbnail generation request, %s, ", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("[ERROR] Status code for the thumbnail request %d", resp.StatusCode)
	}
	// consume the entire response body
	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()
	return nil
}

func fileWalkFn(file string, info os.FileInfo, err error) error {
	ext := strings.ToLower(filepath.Ext(file))
	if !info.IsDir() {
		for _, e := range supportedExt {
			if e == ext {
				err := thumbnailFileRequest(file)
				if err != nil {
					// Print the error and carry on
					fmt.Println(err)
				}
				return nil
			}
		}
		fmt.Println("[ERROR] This extension is not supported:", ext, file)
		return nil
	}
	return nil
}

func fileWalk(p string) {
	filepath.Walk(p, fileWalkFn)
}

func main() {
	var srcURL *url.URL
	flag.Parse()
	if srcDir == "" {
		fmt.Println("\nbulk-loader requires a `src-directory`\n")
		flag.Usage()
		return
	}
	srcURL, err := url.Parse(srcDir)
	if err != nil {
		fmt.Println("\nfailed to parse srcDir into an URL, %s \n\n")
		flag.Usage()
		return
	}

	if dstDir == "" {
		fmt.Println("\nbulk-loader requires a `dst-directory`\n")
		flag.Usage()
		return
	}
	if postURL == "" {
		fmt.Println("\nbulk-loader requires a `post-url`\n")
		flag.Usage()
		return
	}
	if thumbOpts == "" {
		fmt.Println("\nbulk-loader requires a `thumbnail-options`\n")
		flag.Usage()
		return
	}
	err = json.Unmarshal([]byte(thumbOpts), &tOpts)
	if err != nil {
		fmt.Printf("\nFailed to parse the thumbnail-options, %s \n\n", err)
		flag.Usage()
		return
	}

	if srcURL.Scheme == "file" {
		srcPath = srcURL.Path
		fileWalk(srcPath)
		return
	} else {
		fmt.Printf("\nsrc-directory scheme (%s) is not supported\n\n", srcURL.Scheme)
		flag.Usage()
		return
	}
}
