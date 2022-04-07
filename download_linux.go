//go:build linux
// +build linux

package main

import (
	"net"
	"os"
	"syscall"
	"time"
	"unsafe"
)

const (
	spliceMove     = 0x1
	spliceNonblock = 0x2
	spliceMore     = 0x4

	bufSize  = 1 << 20 // max 1M
	onceRead = bufSize / 4
)

var pipeSize = &syscall.Flock_t{}

func (t *DownloadThread) Download(conn *net.TCPConn) (err error) {
	conn.SetReadBuffer(128 << 10)
	var file *os.File
	file, _ = os.OpenFile(t.f.Name(), os.O_WRONLY, 0644)
	file.Seek(t.cur, 0)
	defer file.Close()

	fileFF := (*linuxFileStub)(unsafe.Pointer(file)).file
	connFF := (*linuxFileStub)(unsafe.Pointer(conn)).file

	syscall.SetNonblock(fileFF.Sysfd, false)

	// 生成pipe对
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	rFF := (*linuxFileStub)(unsafe.Pointer(r)).file
	wFF := (*linuxFileStub)(unsafe.Pointer(w)).file
	fcntl(rFF.Sysfd, syscall.F_SETPIPE_SZ, bufSize)
	fcntl(wFF.Sysfd, syscall.F_SETPIPE_SZ, bufSize)
	pdConn := connFF.pd
	pdFile := fileFF.pd

	for t.cur < t.end {
		var buffered int64
		// 从连接读到pipe
		buffered, err = syscall.Splice(connFF.Sysfd, nil, wFF.Sysfd, nil, bufSize, spliceMove|spliceMore|spliceNonblock)
		if err != nil {
			if err == syscall.EAGAIN { // 没读到东西
				if t.cur >= t.end {
					return nil
				}
				setDeadlineImpl(connFF, 1*time.Minute, 'r')
				err = pdConn.waitRead(connFF.isFile)
				if err != nil {
					return err
				}
				continue
			}
			if err == syscall.EINTR {
				continue
			}
			// wrap error
			err = pdConn.prepareRead(connFF.isFile)
			return err
		}
		// 从pipe写到文件
		for buffered > 0 {
			var n int64
			n, err = syscall.Splice(rFF.Sysfd, nil, fileFF.Sysfd, nil, int(buffered), spliceMove|spliceMore)
			if err == nil {
				t.cur += n
				buffered -= n
				break
			} else {
				err = pdFile.prepareWrite(fileFF.isFile)
				if err != nil {
					return err
				}
				err = pdFile.waitWrite(fileFF.isFile)
				if err != nil {
					return err
				}
			}
		}
	}
	return
}

type pollDesc struct {
	runtimeCtx uintptr
}

//go:linkname wait internal/poll.(*pollDesc).wait
func wait(pd *pollDesc, mode int, isFile bool) error

//go:linkname prepare internal/poll.(*pollDesc).prepare
func prepare(pd *pollDesc, mode int, isFile bool) error

func (pd *pollDesc) waitRead(isFile bool) error {
	return wait(pd, 'r', isFile)
}

func (pd *pollDesc) waitWrite(isFile bool) error {
	return wait(pd, 'w', isFile)
}

func (pd *pollDesc) prepareRead(isFile bool) error {
	return prepare(pd, 'r', isFile)
}

func (pd *pollDesc) prepareWrite(isFile bool) error {
	return prepare(pd, 'w', isFile)
}

type linuxFileStub struct {
	file *fd
}

// fdMutex is a specialized synchronization primitive that manages
// lifetime of an fd and serializes access to Read, Write and Close
// methods on FD.
type fdMutex struct {
	state uint64
	rsema uint32
	wsema uint32
}

//go:linkname increfAndClose internal/poll.(*fdMutex).increfAndClose
func increfAndClose(mu *fdMutex) bool

//go:linkname incref internal/poll.(*fdMutex).incref
func incref(mu *fdMutex) bool

//go:linkname decref internal/poll.(*fdMutex).decref
func decref(mu *fdMutex) bool

// incref adds a reference to mu.
// It reports whether mu is available for reading or writing.
func (mu *fdMutex) incref() bool {
	return incref(mu)
}

// increfAndClose sets the state of mu to closed.
// It returns false if the file was already closed.
func (mu *fdMutex) increfAndClose() bool {
	return increfAndClose(mu)
}

// decref removes a reference from mu.
// It reports whether there is no remaining reference.
func (mu *fdMutex) decref() bool {
	return decref(mu)
}

//go:linkname runtime_pollSetDeadline internal/poll.runtime_pollSetDeadline
func runtime_pollSetDeadline(ctx uintptr, d int64, mode int)

func setDeadlineImpl(fd *fd, t time.Duration, mode int) {
	runtime_pollSetDeadline(fd.pd.runtimeCtx, int64(t), mode)
}

//go:linkname fcntl syscall.fcntl
func fcntl(fd int, cmd int, arg int) (val int, err error)

type fd struct {
	// Lock sysfd and serialize access to Read and Write methods.
	fdmu fdMutex

	// System file descriptor. Immutable until Close.
	Sysfd int

	// I/O poller.
	pd pollDesc

	// Writev cache.
	iovecs *[]syscall.Iovec

	// Semaphore signaled when file is closed.
	csema uint32

	// Non-zero if this file has been set to blocking mode.
	isBlocking uint32

	// Whether this is a streaming descriptor, as opposed to a
	// packet-based descriptor like a UDP socket. Immutable.
	IsStream bool

	// Whether a zero byte read indicates EOF. This is false for a
	// message based socket connection.
	ZeroReadIsEOF bool

	// Whether this is a file rather than a network socket.
	isFile bool
}

//go:linkname fdClose internal/poll.(*FD).Close
func fdClose(fd *fd) error

func (p *fd) Close() error {
	return fdClose(p)
}
