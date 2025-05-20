// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/sapcc/go-api-declarations/bininfo"
	"github.com/sapcc/go-bits/httpext"

	"github.com/sapcc/http-keep-alive-monitor/pkg/keepalive"
)

func main() {
	var timeout time.Duration

	flag.DurationVar(&timeout, "timeout", 5*time.Minute, "Maximum time to wait for timeout")
	flag.BoolFunc("version", "Show version information", func(_ string) error {
		fmt.Print(bininfo.Version())
		os.Exit(0)
		return nil
	})
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s URL\n\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	ctx := httpext.ContextWithSIGINT(context.Background(), 100*time.Millisecond)

	parsedURL, err := url.Parse(flag.Arg(0))
	if err != nil {
		log.Fatalf("Failed to parse url: %s", err)
	}
	log.Printf("Checking keepalive timeout for %s...", parsedURL.String())
	interval, timedOut, err := keepalive.MeasureTimeout(ctx, *parsedURL, timeout)
	if err != nil {
		log.Fatalf("check failed after %s: %s", interval, err)
		os.Exit(1)
	}
	if timedOut {
		log.Printf("Timeout waiting for a timeout :) after %s", interval)
		os.Exit(1)
	}

	log.Printf("Connection closed by the server after %s", interval)
}
