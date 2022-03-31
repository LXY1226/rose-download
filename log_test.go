package main

import (
	"bufio"
	"log"
	"net"
	"os"
	"testing"
)

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
