package main

import (
	"errors"
	"hash"
	"io"
	"os"
	"time"
)

// FileHeader HostOS types
const (
	HostOSUnknown = 0
	HostOSMSDOS   = 1
	HostOSOS2     = 2
	HostOSWindows = 3
	HostOSUnix    = 4
	HostOSMacOS   = 5
	HostOSBeOS    = 6
)

const (
	maxPassword = 128
)

var (
	errShortFile        = errors.New("rardecode: decoded file too short")
	errInvalidFileBlock = errors.New("rardecode: invalid file block")
	errUnexpectedArcEnd = errors.New("rardecode: unexpected end of archive")
	errBadFileChecksum  = errors.New("rardecode: bad file checksum")
)

type byteReader interface {
	io.Reader
	io.ByteReader
}

// hash32 implements fileChecksum for 32-bit hashes
type hash32 struct {
	hash.Hash32        // hash to write file contents to
	sum         uint32 // 32bit checksum for file
}

func (h *hash32) ReadFrom(r io.Reader) (n int64, err error) {
	h.Reset()
	datChan := make(chan []byte)
	var lastErr error
	go func() {
		var _n int
		for {
			buf := make([]byte, 64<<10)
			_n, err = io.ReadFull(r, buf)
			n += int64(_n)
			datChan <- buf[:_n]
			if err != nil {
				close(datChan)
				if err != io.EOF {
					lastErr = err
				}
				return
			}
		}
	}()
	for {
		dat, ok := <-datChan
		if !ok {
			break
		}
		_, err = h.Write(dat)
		if err != nil {
			lastErr = err
		}
	}
	if lastErr != nil {
		return 0, err
	}
	return n, nil
}

func (h *hash32) Correct() bool { return h.Sum32() == h.sum }

// fileBlockHeader represents a file block in a RAR archive.
// Files may comprise one or more file blocks.
// Solid files retain decode tables and dictionary from previous solid files in the archive.
type fileBlockHeader struct {
	first   bool // first block in file
	last    bool // last block in file
	solid   bool // file is solid
	winSize uint // log base 2 of decode window size
	//decoder    decoder    // decoder to use for file
	key []byte // key for AES, non-empty if file encrypted
	iv  []byte // iv for AES, non-empty if file encrypted
	FileHeader
}

type fileChecksum interface {
	io.Writer
	io.ReaderFrom
	hash.Hash
	Correct() bool
}

// FileHeader represents a single file in a RAR archive.
type FileHeader struct {
	Name             string    // file name using '/' as the directory separator
	IsDir            bool      // is a directory
	HostOS           byte      // Host OS the archive was created on
	Attributes       int64     // Host OS specific file attributes
	PackedSize       int64     // packed file size (or first block if the file spans volumes)
	UnPackedSize     int64     // unpacked file size
	UnKnownSize      bool      // unpacked file size is not known
	ModificationTime time.Time // modification time (non-zero if set)
	CreationTime     time.Time // creation time (non-zero if set)
	AccessTime       time.Time // access time (non-zero if set)
	Version          int       // file version
	StartOffset      int64     // current file offset of archive (include header)
	EndOffset        int64     // current file offset of archive

	hash fileChecksum
}

func (h *FileHeader) CheckFile(f *os.File) error {
	if h.hash == nil {
		return errArchiveContinues
	}
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	if stat.Size() != h.UnPackedSize {
		return errShortFile
	}
	f.Seek(0, io.SeekStart)
	_, err = h.hash.ReadFrom(f)
	if err != nil {
		return err
	}
	if !h.hash.Correct() {
		err = errBadFileChecksum
	}
	return err
}
