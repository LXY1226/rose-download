package main

import (
	"bufio"
	"context"
	"errors"
	"log"
	"net"
	"os"
	"sync"
	"time"
	_ "unsafe"
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
		go runProxy(ip, txt)
	}
}

var ErrNext = errors.New("")

func runProxy(ip net.IP, address string) {
	addr := &net.TCPAddr{IP: ip, Port: 443}
	dialer := &sysDialer{network: "tcp", address: address}
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
			err := task.Go(dialer, addr, logger)
			logger.Printf("子任务结束 %v", err)
			if err == nil {
				break
			}

			if err != ErrNext {
				//logger.Println(err)
				time.Sleep(30 * time.Second)
			}
		}
		taskMutex.Lock()
		if task == curTask {
			logger.Println(curTask.filename, "任务结束")
			nextTask()
		} else {
			logger.Println(curTask.filename, "任务切换")
		}
		taskMutex.Unlock()
	}
	wg.Done()
}

// sysDialer contains a Dial's parameters and configuration.
type sysDialer struct {
	net.Dialer
	network, address string
}

//go:linkname doDialTCP net.(*sysDialer).doDialTCP
func doDialTCP(sd *sysDialer, ctx context.Context, laddr, raddr *net.TCPAddr) (*net.TCPConn, error)

func (sd *sysDialer) dialTCP(ctx context.Context, laddr, raddr *net.TCPAddr) (*net.TCPConn, error) {
	return doDialTCP(sd, ctx, laddr, raddr)
}
