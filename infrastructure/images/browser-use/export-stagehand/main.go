// export-stagehand unpacks the Chromium extension embedded in the pinned SDK.
package main

import (
	"archive/zip"
	"bytes"
	"log"
	"os"

	stagehand "github.com/browserbase/stagehand/packages/sdk-go/v4"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: export-stagehand OUTPUT_DIRECTORY")
	}
	archive := stagehand.ExtensionArchive()
	files, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		log.Fatal(err)
	}
	if err := os.CopyFS(os.Args[1], files); err != nil {
		log.Fatal(err)
	}
}
