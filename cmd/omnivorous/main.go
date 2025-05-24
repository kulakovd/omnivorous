package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"omnivorous/internal/downloaders/boomstream"
	"os"
)

func main() {
	flag.Usage = func() {
		fmt.Println("Usage: omnivorous [options] <url>")
		flag.PrintDefaults()
	}

	if len(os.Args) < 2 {
		flag.Usage()
		os.Exit(1)
	}

	fUrl := os.Args[1]

	if fUrl == "" {
		fmt.Println("Error: URL is required")
		flag.Usage()
		os.Exit(1)
	}

	err := flag.CommandLine.Parse(os.Args[2:])
	if err != nil {
		flag.Usage()
		os.Exit(1)
	}

	// parse the URL
	parsedUrl, err := url.Parse(fUrl)
	if err != nil {
		fmt.Println("Error: Invalid URL")
		flag.Usage()
		os.Exit(1)
	}

	ctx := context.Background()

	// get the host
	host := parsedUrl.Host
	if host == "play.boomstream.com" {
		err = boomstream.Download(ctx, parsedUrl)
	}

	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
