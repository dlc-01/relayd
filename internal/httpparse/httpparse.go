package httpparse

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"strings"
	"time"
)

const (
	maxHeaderSize = 8192
	readTimeout   = 5 * time.Second
)

type Result struct {
	Host   string
	Peeked []byte
}

func PeekHost(conn net.Conn) (Result, error) {
	conn.SetReadDeadline(time.Now().Add(readTimeout))
	defer conn.SetReadDeadline(time.Time{})

	buf := make([]byte, maxHeaderSize)
	total := 0

	for total < maxHeaderSize {
		// bufio.Reader читать может много надо с запасом
		n, err := conn.Read(buf[total : total+1])
		if err != nil {
			return Result{}, fmt.Errorf("read: %w", err)
		}
		total += n
		if total >= 4 && bytes.Equal(buf[total-4:total], []byte("\r\n\r\n")) {
			break
		}
	}

	peeked := make([]byte, total)
	copy(peeked, buf[:total])

	host, err := extractHost(peeked)
	if err != nil {
		return Result{Peeked: peeked}, err
	}

	return Result{Host: host, Peeked: peeked}, nil
}

func extractHost(headers []byte) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(headers))
	scanner.Scan()

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "host:") {
			host := strings.TrimSpace(line[5:])
			if idx := strings.LastIndex(host, ":"); idx != -1 {
				host = host[:idx]
			}
			return host, nil
		}
	}
	return "", fmt.Errorf("host header not found")
}
