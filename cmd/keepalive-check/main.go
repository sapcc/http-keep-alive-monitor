package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/sapcc/http-keep-alive-monitor/pkg/keepalive"
)

func main() {
	var timeout time.Duration

	flag.DurationVar(&timeout, "timeout", 101*time.Second, "Maxium time to wait for timeout")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s URL\n\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	url, err := url.Parse(flag.Arg(0))
	if err != nil {
		log.Fatalf("Failed to parse url: %s", err)
	}
	log.Printf("Checking keepalive timeout for %s...", url.String())
	interval, _, err := keepalive.MeasureTimeout(*url, timeout)
	if err != nil {
		log.Fatalf("check failed after %s: %s", interval, err)
	}
	log.Printf("Connection closed after %s", interval)

}
