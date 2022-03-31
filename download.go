package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const freshInt = 2

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
	once uint32
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

func (t *DownloadTask) initURL() (fName, fUrl string) {
	fUrl = getFileURL(t.webUrl)
	if fUrl == "" {
		return
	}
	log.Println(fUrl)

	t.header = NewHeader(fUrl)
	i := strings.LastIndexByte(fUrl, '/')
	if i != -1 {
		fName = fUrl[i+1:]
	}
	return
}

// only once
func (t *DownloadTask) init() (err error) {
	filename, fUrl := t.initURL()
	if fUrl == "" {
		log.Println("任务", t.webUrl, "失败")
		return
	}
	if filename != t.filename && filename != "" {
		log.Printf("文件名不匹配 %s => %s", t.filename, filename)
		t.filename = filename
	}
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
	t.filename = filename
	t.stat = stat
	log.Printf("%s 任务开始，从分片文件中读取到 %d 个分片范围 %p", t.filename, len(t.ranges), t)
	if t.ranges == nil {
		thread := new(DownloadThread)
		thread.cur = 0
		var _len uint64
		_len, err = httpContentLength(fUrl)
		if err != nil {
			return
		}
		if t.length != 0 && t.length != _len {
			err = fmt.Errorf("文件长度不一致 %d!=%d", _len, t.length)
			time.Sleep(25 * time.Second)
		}
		t.f.Truncate(int64(_len))
		t.length = _len
		thread.end = t.length
		t.ranges = append(t.ranges, thread)
	}
	go func() {
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for range tick.C {
			if len(t.ranges) == 0 {
				t.remain = 0
				t.f.Close()
				t.stat.Close()
				os.Remove(t.filename + ".stat")
				log.Println(t.filename, "任务完成")
				return
			}
			t.SaveStat()
		}
	}()
	return
}

func (t *DownloadTask) SaveStat() {
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
	b := '\r'
	if t != curTask {
		b = ' '
	}
	if t.remain >= remain {
		fmt.Printf("%s/%s %s/s #%d  %c",
			formatSize(t.length-remain),
			formatSize(t.length),
			formatSize((t.remain-remain)/freshInt),
			active, b)
	}
	t.remain = remain
	if buf.Len() == 0 {
		t.ranges = nil
		return
	}
	t.stat.Seek(0, 0)
	t.stat.Truncate(int64(buf.Len()))
	buf.WriteTo(t.stat)
	//t.stat.Sync()
}

func (t *DownloadTask) Go(addr *net.TCPAddr, logger *log.Logger) (err error) {
	thread, _ := t.getThread()
	logger.Printf("子任务开始 %p %p", t, thread)

	if thread == nil {
		return
	}
	if thread.cur < thread.end {
		var conn *net.TCPConn
		conn, err = net.DialTCP("tcp", nil, addr)
		if err == nil {
			err = t.run(conn, thread)
			conn.Close()
		}
	} else {
		logger.Println("cur >= end, skip")
	}

	t.Lock()
	var pos int
	var tag, mergedOrFinished bool
	for i, r := range t.ranges {
		if r == thread {
			pos = i
			if tag {
				break
			}
			tag = true
		}
		if r.end+1 == thread.cur {
			r.end = thread.end
			mergedOrFinished = true
			if tag {
				break
			}
			tag = true
		}
	}
	if err == nil { // finished
		mergedOrFinished = true
		err = ErrNext
	}
	if mergedOrFinished {
		copy(t.ranges[pos:], t.ranges[pos+1:])
		t.ranges = t.ranges[:len(t.ranges)-1]
	}
	thread.state = stateNoWork
	t.Unlock()
	return
}

func (t *DownloadTask) run(conn *net.TCPConn, thread *DownloadThread) (err error) {
	var stat uint64
	err = t.header.SendHeader(conn, thread.cur)
	if err != nil {
		return errors.New("发送请求失败 " + err.Error())
	}
	br := bufio.NewReader(conn)
	stat, err = readHead(br)
	if stat != 206 {
		if stat == 200 {
			t.initURL()
			return fmt.Errorf("下载链接失效，刷新")
		}
		return fmt.Errorf("响应无效 %d %s", stat, err)
	}
	_len, _ := parseCode(err.Error())
	_len += thread.cur
	if t.length != 0 && t.length != _len {
		err = fmt.Errorf("文件长度不一致 %d!=%d", _len, t.length)
		time.Sleep(25 * time.Second)
		return err
	}

	thread.state = stateReceive // 不需要锁
	thread.f = t.f
	br.WriteTo(thread)

	err = thread.Download(conn)
	return err
}

func (t *DownloadTask) getThread() (cur, prev *DownloadThread) {
	cur = new(DownloadThread) // [0:0:0]
	var l uint64
	t.Lock()
	defer t.Unlock()
	if len(t.ranges) == 0 {
		return nil, nil
	}
	var i int
	for ; i < len(t.ranges); i++ {
		_len := t.ranges[i].end - t.ranges[i].cur
		// 找最大的未下载段，取一半下载
		if _len > l {
			l = _len
			prev = t.ranges[i]
		}
		if t.ranges[i].state == stateNoWork {
			cur = t.ranges[i]
			prev = nil
			break
		}
	}
	cur.state = stateReady
	// 从已有片段分拆的新片段
	if prev != nil {
		if l > 64<<10 {
			cur.cur = (prev.cur + prev.end) / 2
			cur.end = prev.end
			prev.end = cur.cur - 1
			t.ranges = append(t.ranges, cur)
		} else { // 分片过小
			cur = nil
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

func httpContentLength(url string) (length uint64, err error) {
	var req *http.Request
	var resp *http.Response
	req, err = http.NewRequest("HEAD", url, nil)
	if err != nil {
		return
	}
	//req.Header.Set("Content-Range", "0-")
	req.Header.Set("User-Agent", UA)
	req.Header.Set("Referer", "https://rosefile.net")
	for i := 0; i < 5; i++ {
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
	}
	if err != nil {
		return
	}
	if resp.StatusCode != 200 {
		err = fmt.Errorf("unexpected resp: %d", resp.StatusCode)
		return
	}
	//length, _ = parseCode(resp.Header.Get("Content-Length"))
	return uint64(resp.ContentLength), err
}

func formatSize(size uint64) string {
	ending := []string{" B", "KB", "MB", "GB", "TB"}
	sf := float32(size)
	n := 0
	for sf > 1024 {
		sf /= 1024
		n++
	}
	return fmt.Sprintf("%.03f%s", sf, ending[n])
}
