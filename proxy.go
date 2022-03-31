package main

import (
	"bufio"
	"errors"
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
	logger := new(log.Logger)
	logger.SetFlags(log.Flags())
	logger.SetOutput(log.Writer())
	b := []byte("                ")
	copy(b, ip.String())
	logger.SetPrefix(string(b))
	logger.Println("已加载")
	for {
		taskMutex.Lock()
		if curTask == nil {
			break
		}
		if curTask.once == 0 {
			curTask.init()
			curTask.once = 1
		}
		taskMutex.Unlock()
		task := curTask
		for {
			err := task.Go(&net.TCPAddr{IP: ip, Port: 443}, logger)
			if err != nil {
				if err != ErrNext {
					logger.Println(err)
					time.Sleep(30 * time.Second)
				} else {
					time.Sleep(5 * time.Second) // wait for prev connection close
				}
			} else {
				break
			}
		}
		taskMutex.Lock()
		if task == curTask {
			logger.Println(curTask.filename, "任务结束")
			nextTask()
		}
		taskMutex.Unlock()
	}
	wg.Done()
}
