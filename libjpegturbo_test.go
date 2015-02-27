// +build libjpegturbo

package thumbnailer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodelibJpegTurbo(t *testing.T) {
	img := "sof-issue-supported-by-libjpegturbo.jpg"
	pwd, err := os.Getwd()
	if err != nil {
		t.Fatal("Failed to get the current directory: %s", err)
	}
	fmt.Println("Current dir:", pwd)

	file, err := os.Open(filepath.Join(pwd, img))
	defer file.Close()
	Decode(file, filepath.Ext(img))
}
