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

func (t *DownloadTask) initURL() (host, path string) {
	furl := getFileURL(t.webUrl)
	if furl == "" {
		log.Println("任务", t.webUrl, "失败")
		return
	}

	t.header, host, path = NewHeader(furl)
	return
}

// only once
func (t *DownloadTask) init() {
	host, path := t.initURL()
	if path == "" {
		return
	}
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
	log.Println(t.filename, "任务开始，从分片文件中读取到", len(t.ranges), "个分片范围")
	go func() {
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for range tick.C {
			if t.ranges == nil {
				continue
			}
			t.SaveStat()
			if len(t.ranges) == 0 {
				t.remain = 0
				t.f.Close()
				t.stat.Close()
				os.Remove(t.filename + ".stat")
				log.Println(t.filename, "任务完成")
				return
			}
		}
	}()
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
		fmt.Printf("%s/%s %s/s #%d  %c", formatSize(t.length-remain), formatSize(t.length), formatSize((t.remain-remain)/2), active, b)
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

func (t *DownloadTask) Go(conn *net.TCPConn, logger *log.Logger, br *bufio.Reader) (err error) {
	thread, parent := t.getThread()
	logger.Println(parent, thread, "start")
	if thread == nil {
		return nil
	}
	if thread.cur < thread.end {
		err = t.run(conn, br, thread)
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
			logger.Println("merging current range")
			r.end = thread.end
			mergedOrFinished = true
			if tag {
				break
			}
			tag = true
		}
	}
	if err != nil { // merge+delete(if merged)
		logger.Println(err)
	} else { // finished
		mergedOrFinished = true
		err = ErrNext
	}
	if mergedOrFinished {
		copy(t.ranges[pos:], t.ranges[pos+1:])
		t.ranges = t.ranges[:len(t.ranges)-1]
		logger.Println("deleting current range")
	}
	thread.state = stateNoWork
	t.Unlock()
	logger.Println(parent, thread, "end")
	return err
}

func (t *DownloadTask) run(conn *net.TCPConn, br *bufio.Reader, thread *DownloadThread) (err error) {
	var stat uint64
	err = t.header.SendHeader(conn, thread.cur)
	if err != nil {
		return errors.New("发送请求失败 " + err.Error())
	}

	stat, err = readHead(br)
	if stat != 206 {
		if stat == 200 {
			t.initURL()
			return fmt.Errorf("下载链接失效，刷新")
		}
		return fmt.Errorf("上游响应无效 %d %s", stat, err)
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

	_, err = thread.Download(conn)
	return err
}

func (t *DownloadTask) getThread() (cur, prev *DownloadThread) {
	if t.ranges == nil {
		return nil, nil
	}
	cur = new(DownloadThread) // [0:0:0]
	var l uint64
	t.Lock()
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
	t.Unlock()
	return
}

func (t *DownloadThread) Download(conn *net.TCPConn) (n int, err error) {
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
	return fmt.Sprintf("%.03f%s", sf, ending[n])
}
