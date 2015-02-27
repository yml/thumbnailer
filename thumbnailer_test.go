package thumbnailer

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"
)

func testThumbnailerMessage() ThumbnailerMessage {
	pwd, err := os.Getwd()
	if err != nil {
		log.Fatal("Failed to get the current directory: %s", err)
	}
	fmt.Println("Current dir:", pwd)
	opt := ThumbnailOpt{Width: 100, Height: 100}
	opts := make([]ThumbnailOpt, 0)
	opts = append(opts, opt)
	return ThumbnailerMessage{
		SrcImage:  filepath.Join("file://", pwd, "testdata", "pic.jpg"),
		DstFolder: "file:///tmp",
		Opts:      opts}

}

func Test_thumbURL(t *testing.T) {
	expected := "/tmp/pic_s100x100.jpg"
	tm := testThumbnailerMessage()
	url, err := tm.thumbURL(tm.Opts[0])
	if err != nil {
		t.Fatal("Failed to generate the thumbURL :", err)
	}
	if url.Path != expected {
		t.Fatalf("got: %s, expected: %s", url.Path, expected)
	}
}

func Test_generateThumbnail(t *testing.T) {
	tm := testThumbnailerMessage()
	src, err := tm.Open()
	if err != nil {
		t.Fatalf("Failed to open tm.SrcImage: %v", err)
	}
	tm.generateThumbnail(src, tm.Opts[0])
}

func Test_GenerateThumbnails(t *testing.T) {
	tm := testThumbnailerMessage()
	results := tm.GenerateThumbnails()
	for result := range results {
		if result.Err != nil {
			t.Fatal("An error occured while generating a thumb :", result.Err)
		}
		// Clean up the generated thumb
		if err := os.Remove(result.Thumbnail.Path); err != nil {
			t.Fatal("Failed to delete the generated thumb:", err)
		}
	}
}
