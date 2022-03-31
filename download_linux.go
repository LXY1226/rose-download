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
	spliceMove = 0x1
	// spliceNonblock makes calls to splice(2) non-blocking.
	spliceNonblock = 0x2
	spliceMore     = 0x4
)

func (t *DownloadThread) Download(conn *net.TCPConn) (err error) {
	conn.SetReadBuffer(128 << 10)
	defer conn.Close()
	var file *os.File
	file, _ = os.OpenFile(t.f.Name(), os.O_WRONLY, 0644)
	file.Seek(int64(t.cur), 0)
	defer file.Close()

	fileFF := (*linuxFileStub)(unsafe.Pointer(file)).file
	connFF := (*linuxFileStub)(unsafe.Pointer(conn)).file

	fileFF.fdmu.incref()
	connFF.fdmu.incref()

	// 生成pipe对
	r, w, _ := os.Pipe()
	defer r.Close()
	defer w.Close()
	rFF := (*linuxFileStub)(unsafe.Pointer(r)).file
	wFF := (*linuxFileStub)(unsafe.Pointer(w)).file
	rFF.fdmu.incref()
	wFF.fdmu.incref()
	pdConn := connFF.pd

	for t.cur < t.end {
		var n int64
		// 从连接读到pipe
		setDeadlineImpl(connFF, 1*time.Minute, 'r')
		err = pdConn.prepareRead(true)
		if err != nil {
			return err
		}
		n, err = syscall.Splice(connFF.Sysfd, nil, wFF.Sysfd, nil, 1<<20, spliceMove|spliceMore|spliceNonblock)
		if err != nil {
			switch err {
			case syscall.EINTR:
				continue
			case syscall.EAGAIN:
				err = pdConn.waitRead(connFF.isFile)
				if err != nil {
					return err
				}
				continue
			default:
				return err
			}
		}
		t.cur += uint64(n)
		// 从pipe写到文件
		n, err = syscall.Splice(rFF.Sysfd, nil, fileFF.Sysfd, nil, int(n), spliceMove|spliceMore) // block here
		if err != nil && err != syscall.EAGAIN {
			return err
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

func (pd *pollDesc) prepareRead(isFile bool) error {
	return prepare(pd, 'r', isFile)
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

// Return the appropriate closing error based on isFile.
//go:linkname errClosing internal/poll.errClosing
func errClosing(isFile bool) error

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
