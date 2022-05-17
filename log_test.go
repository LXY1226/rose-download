package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"testing"
)

func TestNewHeader(t *testing.T) {
	header := NewHeader("https://d17.rosefile.net/d/MDAwMDAwMDAwMJOAeZ2w0KXfgMpqq7KEcKiyhXJ9lKF-d4h8oIyVl3ithXyN3b2ryNJ_z3ycx5qSnMp6e6eWoIqYnXugrIy6Ztl-oo3dsbrLl4Dbf64/2205092.part1.rar")
	conn, err := net.Dial("tcp", "110.242.70.68:443")
	if err != nil {
		panic(err)
	}
	err = header.SendHeader(conn, 0)
	if err != nil {
		panic(err)
	}
	rd := bufio.NewReader(conn)
	status, err := readHead(rd)
	fmt.Println(status, err)
}

func TestAvc(t *testing.T) {
	f, err := os.Open("E:\\gm\\ips.txt")
	if err != nil {
		log.Fatal("打开代理ip列表出错", err)
	}
	bm := make(map[string]string)

	bf := bufio.NewScanner(f)
	for bf.Scan() {
		txt := bf.Text()
		ip := net.ParseIP(txt)
		if ip == nil {
			log.Println("无效ip：", txt)
			continue
		}
		b := []byte("                ")
		copy(b, ip.String())
		bm[string(b)] = ""
		//go runProxy(ip)
	}
	f, err = os.Open("E:\\gm\\detect.txt")
	if err != nil {
		panic(err)
	}
	bf = bufio.NewScanner(f)
	for bf.Scan() {
		txt := bf.Text()
		// 		07:04:35 220.181.33.174  新子任务 0xc0000f8240
		if _, ok := bm[txt[9:25]]; ok {
			bm[txt[9:25]] = txt
		}

	}
	for _, v := range bm {
		t.Log(v)
	}
}
