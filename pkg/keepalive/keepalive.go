package keepalive

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

func MeasureTimeout(endpoint url.URL, timeout time.Duration) (time.Duration, bool, error) {

	req, err := http.NewRequest("GET", endpoint.String(), nil)
	req.Header["User-Agent"] = []string{"http-keepalive-monitor/1.0"}
	if err != nil {
		return 0, false, fmt.Errorf("Failed to create request: %w", err)
	}

	var conn net.Conn
	if endpoint.Scheme == "https" {
		conn, err = tls.Dial("tcp", endpoint.Host, &tls.Config{
			InsecureSkipVerify: true,
		})
	} else {
		conn, err = net.Dial("tcp", endpoint.Host)
	}
	if err != nil {
		return 0, false, fmt.Errorf("Connection failed: %w", err)
	}
	defer conn.Close()
	//multi := io.MultiWriter(os.Stderr, conn)
	if err := req.Write(conn); err != nil {
		return 0, false, fmt.Errorf("Sending intial request failed: %w", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return 0, false, fmt.Errorf("Failed to read initial response: %w", err)
	}
	if response.Header.Get("Connection") == "Close" {
		return 0, false, nil
	}
	if response.Body != nil {
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
	}

	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return 0, false, fmt.Errorf("Failed to set read deadline: %w", err)
	}

	dummy := make([]byte, 1)
	start := time.Now()
	_, err = conn.Read(dummy)
	if err == io.EOF {
		return time.Now().Sub(start), false, nil
	}
	if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
		return time.Now().Sub(start), true, nil
	}

	return time.Now().Sub(start), false, err
}
