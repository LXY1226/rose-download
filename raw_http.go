package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
)

type HttpHeader struct {
	Host        string
	conn        *net.TCPConn
	header      []byte
	proxyHeader []byte
	sync.Mutex  // for header
}

const (
	UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/99.0.4844.74 Safari/537.36 Edg/99.0.1150.46"
)

func NewHeader(url string) (*HttpHeader, string, string) {
	if strings.HasPrefix(url, "http") {
		i := strings.IndexByte(url, '/')
		i += 2
		url = url[i:]
	}
	i := strings.IndexByte(url, '/')
	host := url[:i]
	path := url[i:]
	h := new(HttpHeader)
	h.Reset(host, path)
	return h, host, path
}

func (h *HttpHeader) Reset(host, path string) {
	buf := bytes.Buffer{}
	buf.WriteString("CONNECT ")
	buf.WriteString(host)
	buf.WriteString(":80 HTTP/1.1\r\n")
	buf.WriteString("X-T5-Auth: ZjQxNDIh\r\n\r\n")
	h.proxyHeader = buf.Bytes()

	buf.WriteString("GET ")
	buf.WriteString(path)
	buf.WriteString(" HTTP/1.1\r\n")
	buf.WriteString("Connection: close\r\n")
	buf.WriteString("User-Agent: " + UA + "\r\n")
	buf.WriteString("Host: ")
	buf.WriteString(host)
	buf.WriteString("\r\n")
	buf.WriteString("Referer: https://rosefile.net/\r\n")
	buf.WriteString("Range: bytes=")
	h.header = buf.Bytes()[len(h.proxyHeader):]
}

func (h *HttpHeader) SendProxyHeader(conn io.Writer) error {
	_, err := conn.Write(h.proxyHeader)
	return err
}

func (h *HttpHeader) SendHeader(conn io.Writer, start uint64) error {
	h.Lock()
	defer h.Unlock()
	header := strconv.AppendUint(h.header, start, 10)
	header = append(header, "-\r\n\r\n"...)
	_, err := conn.Write(header)
	return err
}

//func (h *HttpHeader) SendHeaderRaw(start int64, conn *net.TCPConn)

func parseCode(str string) (code uint64, remain string) {
	b := ([]byte)(str)
	for i, c := range b {
		if c < '0' || c > '9' {
			remain = string(b[i:])
			return
		}
		code *= 10
		code += uint64(c - '0')
	}
	return
}

func readHead(conn *bufio.Reader) (status uint64, err error) {
	str, err := conn.ReadString('\n')
	if err != nil && len(str) < 9+3 {
		return
	}
	status, str = parseCode(str[9:])
	lastErr := errors.New(str[1 : len(str)-2])
	for {
		str, err = conn.ReadString('\n')
		if err != nil || len(str) == 2 {
			return status, lastErr
		}
		if strings.HasPrefix(str, "X-Squid-Error") {
			lastErr = errors.New(str[15 : len(str)-2])
			return status, lastErr
		}
		if status == 206 && strings.HasPrefix(str, "Content-Length") {
			lastErr = errors.New(str[16 : len(str)-2])
		}
	}
}
