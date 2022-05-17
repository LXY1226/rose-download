package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"regexp"
	"runtime"
	"unsafe"
)

var (
	errNoSig              = errors.New("rardecode: RAR signature not found")
	errVerMismatch        = errors.New("rardecode: volume version mistmatch")
	errCorruptHeader      = errors.New("rardecode: corrupt block header")
	errCorruptFileHeader  = errors.New("rardecode: corrupt file header")
	errBadHeaderCrc       = errors.New("rardecode: bad header crc")
	errUnknownArc         = errors.New("rardecode: unknown archive version")
	errUnknownDecoder     = errors.New("rardecode: unknown decoder version")
	errUnsupportedDecoder = errors.New("rardecode: unsupported decoder version")
	errArchiveContinues   = errors.New("rardecode: archive continues in next volume")
	errArchiveEnd         = errors.New("rardecode: archive end reached")
	errDecoderOutOfData   = errors.New("rardecode: decoder expected more data than is in packed file")

	reDigits = regexp.MustCompile(`\d+`)
)

type readBuf []byte

func (b *readBuf) byte() byte {
	v := (*b)[0]
	*b = (*b)[1:]
	return v
}

func (b *readBuf) uint16() uint16 {
	v := binary.LittleEndian.Uint16(*b)
	*b = (*b)[2:]
	return v
}

func (b *readBuf) uint32() uint32 {
	v := binary.LittleEndian.Uint32(*b)
	*b = (*b)[4:]
	return v
}

func (b *readBuf) bytes(n int) []byte {
	v := (*b)[:n]
	*b = (*b)[n:]
	return v
}

func (b *readBuf) uvarint() uint64 {
	var x uint64
	var s uint
	for i, n := range *b {
		if n < 0x80 {
			*b = (*b)[i+1:]
			return x | uint64(n)<<s
		}
		x |= uint64(n&0x7f) << s
		s += 7
	}
	// if we run out of bytes, just return 0
	*b = (*b)[len(*b):]
	return 0
}

// readFull wraps io.ReadFull to return io.ErrUnexpectedEOF instead
// of io.EOF when 0 bytes are read.
func wrapErrEOF(err, errIfEOF error) error {
	if err == io.EOF {
		return errIfEOF
	}
	return err
}

type wrappedBuffer struct{ bufio.Reader }

func wrapBuffer(rd *bufio.Reader) *wrappedBuffer {
	return (*wrappedBuffer)(unsafe.Pointer(rd))
}

func (b *wrappedBuffer) Get(n int) (readBuf, error) {
	data, err := b.Reader.Peek(n)
	b.Reader.Discard(n)
	if err == bufio.ErrBufferFull {
		runtime.Breakpoint()
	}
	return data, err
}

type archiveFile struct {
	f *os.File
	*wrappedBuffer
}

func (a *archiveFile) open(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	a.setFile(f)
	return nil
}

func (a *archiveFile) getPos() int64 {
	pos, _ := a.f.Seek(0, io.SeekCurrent)
	pos -= int64(a.Reader.Buffered())
	return pos
}

func (a *archiveFile) Discard(n int64) (discarded int64, err error) {
	_, err = a.Seek(n, io.SeekCurrent)
	return n, err
}

func (a *archiveFile) Seek(offset int64, whence int) (ret int64, err error) {
	//if whence == io.SeekEnd {
	//	panic(err)
	//}
	if whence == io.SeekCurrent {
		buffered := int64(a.Reader.Buffered())
		if offset > buffered && offset > 4096 {
			ret, err = a.f.Seek(offset+buffered, io.SeekCurrent)
			a.Reader.Reset(a.f)
			return ret, err
		}
		var b int
		ret, err = a.f.Seek(0, io.SeekCurrent)
		if err != nil || offset == 0 {
			return ret, err
		}
		b, err = a.Reader.Discard(int(offset))
		ret -= int64(b)
		if err != nil {
			return int64(b), err
		}
	}
	ret, err = a.f.Seek(offset, io.SeekStart)
	a.Reader.Reset(a.f)
	return ret, err
}

func (a *archiveFile) setFile(f *os.File) {
	a.f = f
	if a.wrappedBuffer == nil {
		a.wrappedBuffer  = wrapBuffer(bufio.NewReader(f))
	} else {
		a.Reader.Reset(f)
	}
}
