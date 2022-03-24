package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

var curTask *DownloadTask
var taskMutex sync.Mutex

func runProxys(filename string) {
	f, err := os.Open(filename)
	if err != nil {
		log.Fatal("打开代理ip列表出错", err)
	}

	bf := bufio.NewScanner(f)
	for bf.Scan() {
		txt := bf.Text()
		ip := net.ParseIP(txt)
		if ip == nil {
			log.Println("无效ip：", txt)
			continue
		}
		go runProxy(ip)
	}
}

var ErrNext = errors.New("")

func runProxy(ip net.IP) {
	wg.Add(1)
	var stat uint64
	var err error
	var conn *net.TCPConn
	for curTask != nil {
		curTask.Do(curTask.init)
		for {
			if err != nil {
				conn.Close()
				if err != ErrNext {
					log.Println(ip, err)
					time.Sleep(30 * time.Second)
				}
				time.Sleep(5 * time.Second) // 等待前一连接正常关闭
			}
			conn, err = net.DialTCP("tcp", nil, &net.TCPAddr{IP: ip, Port: 443})
			if err != nil {
				continue
			}
			conn.SetReadBuffer(64 << 10)
			br := bufio.NewReaderSize(conn, 256)
			taskMutex.Lock()
			task := curTask
			taskMutex.Unlock()
			err = task.header.SendProxyHeader(conn)
			if err != nil {
				err = errors.New("squid代理失败 " + err.Error())
				continue
			}
			stat, err = readHead(br)
			if stat != 200 {
				err = fmt.Errorf("squid响应无效 %d %s", stat, err.Error())
				continue
			}
			err = task.Go(conn, br)
			if err != nil {
				continue
			}
			conn.Close()
			time.Sleep(2 * time.Second) // 等待前一连接正常关闭
			taskMutex.Lock()
			if task == curTask {
				log.Println(curTask.filename, "任务完成")
				curTask.SaveStat()
				nextTask()
			}
			taskMutex.Unlock()
			break
		}
	}
	wg.Done()
}
