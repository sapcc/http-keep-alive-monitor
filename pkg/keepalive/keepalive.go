package keepalive

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/sapcc/go-bits/errext"
)

func MeasureTimeout(ctx context.Context, endpoint url.URL, timeout time.Duration) (time.Duration, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), http.NoBody)
	req.Header["User-Agent"] = []string{"http-keepalive-monitor/1.0"}
	if err != nil {
		return 0, false, fmt.Errorf("Failed to create request: %w", err)
	}

	var conn net.Conn
	if endpoint.Scheme == "https" {
		conn, err = tls.Dial("tcp", endpoint.Host, &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // we want to test ingresses with wrong certs, too
		})
	} else {
		conn, err = net.Dial("tcp", endpoint.Host)
	}
	if err != nil {
		return 0, false, fmt.Errorf("Connection failed: %w", err)
	}
	defer conn.Close()
	// multi := io.MultiWriter(os.Stderr, conn)
	if err := req.Write(conn); err != nil {
		return 0, false, fmt.Errorf("Sending initial request failed: %w", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return 0, false, fmt.Errorf("Failed to read initial response: %w", err)
	}
	if response.Close {
		return 0, false, nil
	}
	if response.Body != nil {
		_, err = io.Copy(io.Discard, response.Body)
		if err != nil {
			return 0, false, fmt.Errorf("Copy failed: %w", err)
		}
		response.Body.Close()
	}

	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return 0, false, fmt.Errorf("Failed to set read deadline: %w", err)
	}

	dummy := make([]byte, 1)
	start := time.Now()
	_, err = conn.Read(dummy)
	if errors.Is(err, io.EOF) {
		return time.Since(start), false, nil
	}
	if nerr, ok := errext.As[net.Error](err); ok && nerr.Timeout() {
		return time.Since(start), true, nil
	}

	return time.Since(start), false, err
}
