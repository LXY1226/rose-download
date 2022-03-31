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
	Host   string
	conn   *net.TCPConn
	header []byte
	//proxyHeader []byte
	sync.Mutex // for header
}

const (
	UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/99.0.4844.74 Safari/537.36 Edg/99.0.1150.46 baiduboxapp/13.6.0.10"
	// Mozilla/5.0 (iPhone; CPU iPhone OS 15_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 SP-engine/2.44.0 baiduboxapp/13.6.0.10 (Baidu; P2 15.0)
)

func NewHeader(url string) *HttpHeader {
	h := new(HttpHeader)
	h.Reset(strings.Replace(url, "https", "http", 1))
	return h
}

func (h *HttpHeader) Reset(url string) {
	buf := bytes.Buffer{}
	buf.WriteString("GET ")
	buf.WriteString(url)
	buf.WriteString(" HTTP/1.1\r\n")
	buf.WriteString("Proxy-Connection: close\r\n")
	//buf.WriteString("dispatch_header: bdp_dispatch_header\r\n")
	buf.WriteString("User-Agent: " + UA + "\r\n")
	buf.WriteString("X-T5-Auth: 55149428\r\n")
	//buf.WriteString("X-BDBoxApp-NetEngine: 3\r\n")
	buf.WriteString("Referer: https://rosefile.net/\r\n")
	buf.WriteString("Range: bytes=")
	h.header = buf.Bytes() //[len(h.proxyHeader):]
}

//func (h *HttpHeader) SendProxyHeader(conn io.Writer) error {
//	_, err := conn.Write(h.proxyHeader)
//	return err
//}

func (h *HttpHeader) SendHeader(conn io.Writer, start int64) error {
	h.Lock()
	defer h.Unlock()
	header := strconv.AppendInt(h.header, start, 10)
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
