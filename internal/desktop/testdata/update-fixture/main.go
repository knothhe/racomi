package main

import (
	"fmt"
	"os"
	"time"
)

var identity = "{}"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--build-info" {
		fmt.Println(identity)
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--hold" {
		time.Sleep(time.Minute)
		return
	}
	if marker := os.Getenv("RACOMI_TEST_UPDATE_MARKER"); marker != "" {
		_ = os.WriteFile(marker, []byte(identity), 0o600)
	}
}
