package thumbnailer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// func Test_generateThumbnail(t *testing.T){
//
// }

func Test_GenerateThumbnails(t *testing.T) {
	pwd, err := os.Getwd()
	if err != nil {
		t.Fatal("Failed to get the current directory: %s", err)
	}
	fmt.Println("Current dir:", pwd)
	opt := ThumbnailOpt{Width: 100, Height: 100}
	opts := make([]ThumbnailOpt, 0)
	opts = append(opts, opt)
	tm := ThumbnailerMessage{
		SrcImage:  filepath.Join("file://", pwd, "testdata", "pic.jpg"),
		DstFolder: "file:///tmp",
		Opts:      opts}
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
