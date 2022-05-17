//go:build !linux

package main

import (
	"net"
	"time"
)

func (t *DownloadThread) Download(conn *net.TCPConn) (err error) {
	var n int
	conn.SetReadBuffer(64 << 10)
	buf := make([]byte, 256<<10) // 256K
	for t.cur < t.end {
		conn.SetReadDeadline(time.Now().Add(1 * time.Minute))
		n, err = conn.Read(buf)
		if err != nil {
			return
		}
		n, err = t.Write(buf[:n])
		if err != nil {
			return
		}
	}
	return
}
