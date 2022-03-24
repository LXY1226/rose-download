package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type DownloadTask struct {
	webUrl string

	filename   string
	f          *os.File
	lastActive time.Time
	header     *HttpHeader

	stat   *os.File
	ranges []*DownloadThread
	length uint64
	remain uint64
	sync.Mutex
	sync.Once // init
}

type ThreadState byte

type DownloadThread struct {
	f        *os.File
	cur, end uint64
	state    ThreadState
}

const (
	stateNoWork ThreadState = iota
	stateReady
	stateReceive
)

// only once
func (t *DownloadTask) init() {
	furl := getFileURL(t.webUrl)
	if furl == "" {
		log.Println("任务", t.webUrl, "失败")
		return
	}

	var host, path string
	t.header, host, path = NewHeader(furl)
	_, filename := filepath.Split(path)
	var err error
	t.f, err = os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	fStat, err := t.f.Stat()
	if err == nil {
		t.length = uint64(fStat.Size())
	}
	stat, err := os.OpenFile(filename+".stat", os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		panic(err)
	}

	scan := bufio.NewScanner(stat)
	for scan.Scan() {
		line := scan.Bytes()
		if len(line) < 3 {
			continue
		}
		if line[0] == '#' { // TODO check if-is filename
			//if line[2:] ==
			continue
		}
		thread := new(DownloadThread)
		var ln string
		thread.cur, ln = parseCode(string(line))
		thread.end, _ = parseCode(ln[1:])
		if thread.cur < thread.end {
			t.ranges = append(t.ranges, thread)
		}
	}
	//stat.Seek(0, 0)
	//stat.WriteString("# ")
	//stat.WriteString(filename)
	//stat.WriteString("\n")
	//stat.WriteString("\n# ")
	//stat.WriteString(path)
	t.filename = filename
	t.stat = stat
	if t.ranges == nil {
		thread := new(DownloadThread)
		thread.cur = 0
		for i := 0; i < 5; i++ {
			conn, err := net.Dial("tcp", host+":80")
			if err != nil {
				log.Println("本地连接失败", err)
				continue
			}
			br := bufio.NewReader(conn)
			err = t.header.SendHeader(conn, 0)
			if err != nil {
				log.Println("发送请求失败" + err.Error())
				continue
			}
			var resp uint64
			resp, err = readHead(br)
			if resp != 206 {
				log.Println("上游响应无效", stat, err)
			}
			conn.Close()
			_len, _ := parseCode(err.Error())
			if t.length != 0 && t.length != _len {
				err = fmt.Errorf("文件长度不一致 %d!=%d", _len, t.length)
				time.Sleep(25 * time.Second)
			}
			t.f.Truncate(int64(_len))
			t.length = _len
			break
		}
		thread.end = t.length
		t.ranges = append(t.ranges, thread)
	}
	log.Println(t.filename, "任务开始")
	go func() {
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for range tick.C {
			if t.ranges == nil {
				continue
			}
			if len(t.ranges) == 0 {
				t.remain = 0
				t.stat.Close()
				os.Remove(t.filename + ".stat")
				return
			}
			t.SaveStat()
		}
	}()
}

func (t *DownloadTask) SaveStat() {
	l := 0
	t.stat.Seek(int64(l), 0)
	var remain uint64
	buf := bytes.Buffer{}
	active := 0
	t.Lock()
	for _, r := range t.ranges {
		if r.cur > r.end {
			continue
		}
		fmt.Fprintf(&buf, "%d:%d\n", r.cur, r.end)
		if r.state == stateReceive {
			active++
		}
		remain += r.end - r.cur
	}
	t.Unlock()
	l += buf.Len()
	buf.WriteTo(t.stat)
	t.stat.Truncate(int64(l))
	b := '\r'
	if t != curTask {
		b = ' '
	}
	if t.remain > remain {
		fmt.Printf("%s/%s %s/s #%d  %c", formatSize(t.length-remain), formatSize(t.length), formatSize((t.remain-remain)/2), active, b)
	}
	t.remain = remain
	//t.stat.Sync()
}

func (t *DownloadTask) Go(conn *net.TCPConn, br *bufio.Reader) error {
	thread, parent := t.getThread()
	var stat uint64
	if thread == nil {
		return nil
	}
	revertParent := func() {
		t.Lock()
		if parent != nil {
			parent.end = thread.end
			for i, r := range t.ranges {
				if r == thread {
					copy(t.ranges[i:], t.ranges[i+1:])
					t.ranges = t.ranges[:len(t.ranges)-1]
				}
			}
		}
		t.Unlock()
	}
	err := t.header.SendHeader(conn, thread.cur)
	if err != nil {
		revertParent()
		return errors.New("发送请求失败 " + err.Error())
	}

	stat, err = readHead(br)
	if stat != 206 {
		revertParent()
		return fmt.Errorf("上游响应无效 %d %s", stat, err)
	}
	_len, _ := parseCode(err.Error())
	_len += thread.cur
	if t.length != 0 && t.length != _len {
		revertParent()
		err = fmt.Errorf("文件长度不一致 %d!=%d", _len, t.length)
		time.Sleep(25 * time.Second)
		return err
	}

	thread.state = stateReceive
	thread.f = t.f
	br.WriteTo(thread)

	_, err = thread.Download(conn)
	t.Lock()
	thread.state = stateNoWork
	t.Unlock()
	if err == nil {
		err = ErrNext
	}
	return err
}

func (t *DownloadTask) getThread() (cur, prev *DownloadThread) {
	cur = new(DownloadThread) // [0:0:0]
	var l uint64
	t.Lock()
	var i int
	for ; i < len(t.ranges); i++ {
		if t.ranges[i].state == stateNoWork {
			cur = t.ranges[i]
			prev = nil
			break
		}
		// 找最大的未下载段，取一半下载
		_len := t.ranges[i].end - t.ranges[i].cur
		if _len > l {
			l = _len
			prev = t.ranges[i]
		}
	}
	cur.state = stateReady
	if prev != nil {
		if l < 64<<10 && l != 0 { // 最大未下载片段小于64K，停止再次分片
			cur = nil
		} else {
			cur.cur = (prev.cur + prev.end) / 2
			cur.end = prev.end
			prev.end = cur.cur - 1
			t.ranges = append(t.ranges, cur)
			//pos = len(t.ranges) -1
		}
		/*		if i == len(t.ranges) {
					t.ranges = append(t.ranges, thread)
				} else {
					t.ranges = append(t.ranges, nil)
					copy(t.ranges[pos+1:], t.ranges[pos:])
					t.ranges[pos] = thread
				}*/
	}
	t.Unlock()
	return
}

func (t *DownloadThread) Download(conn *net.TCPConn) (n int, err error) {
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

func (t *DownloadThread) ReadFrom(_ io.Reader) (n int64, err error) {
	return 0, nil
}

func (t *DownloadThread) String() string {
	return fmt.Sprintf("[%d:%d]", t.cur, t.end)
}

func (t *DownloadThread) Write(p []byte) (n int, err error) {
	n, err = t.f.WriteAt(p, int64(t.cur))
	t.cur += uint64(n)
	return
}

func formatSize(size uint64) string {
	ending := []string{" B", "KB", "MB", "GB", "TB"}
	sf := float32(size)
	n := 0
	for sf > 1024 {
		sf /= 1024
		n++
	}
	return fmt.Sprintf("%.02f%s", sf, ending[n])
}
