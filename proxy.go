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
	logger := new(log.Logger)
	logger.SetFlags(log.Flags())
	logger.SetOutput(log.Writer())
	logger.SetPrefix(ip.String() + " ")
	logger.Println("已加载")
	for {
		taskMutex.Lock()
		curTask.Do(curTask.init) // TODO simple CAS
		task := curTask
		taskMutex.Unlock()
		for {
			if conn != nil {
				conn.Close()
				time.Sleep(5 * time.Second) // 等待前一连接正常关闭
			}
			if err != nil && err != ErrNext {
				time.Sleep(30 * time.Second)
			}
			conn, err = net.DialTCP("tcp", nil, &net.TCPAddr{IP: ip, Port: 443})
			if err != nil {
				continue
			}
			br := bufio.NewReaderSize(conn, 256)
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
			err = task.Go(conn, logger, br)
			if err == nil {
				taskMutex.Lock()
				if task == curTask {
					logger.Println(curTask.filename, "任务结束")
					nextTask()
				}
				taskMutex.Unlock()
				break
			}
		}
	}
	wg.Done()
}
