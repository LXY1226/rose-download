package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	// block types
	blockArc     blockType = 0x73
	blockFile    blockType = 0x74
	blockService blockType = 0x7a
	blockEnd     blockType = 0x7b

	// block flags
	blockHasData = 0x8000

	// archive block flags
	arcVolume    = 0x0001
	arcSolid     = 0x0008
	arcNewNaming = 0x0010
	arcEncrypted = 0x0080

	// file block flags
	fileSplitBefore = 0x0001
	fileSplitAfter  = 0x0002
	fileEncrypted   = 0x0004
	fileSolid       = 0x0010
	fileWindowMask  = 0x00e0
	fileLargeData   = 0x0100
	fileUnicode     = 0x0200
	fileSalt        = 0x0400
	fileVersion     = 0x0800
	fileExtTime     = 0x1000

	// end block flags
	endArcNotLast = 0x0001

	saltSize    = 8 // size of salt for calculating AES keys
	cacheSize30 = 4 // number of AES keys to cache
	hashRounds  = 0x40000
)

var (
	errInvalidFile = errors.New("invalid file header")
	errCrypted     = errors.New("encrypted archive but no password provided")
)

type (
	blockType     byte
	blockHeader15 struct {
		start    int64 // start offset of file
		size     uint16
		htype    blockType // block header type
		flags    uint16
		data     readBuf // header data
		dataSize int64   // size of extra block data
	}

	crypto15 struct {
		pass     []uint16
		keyCache [cacheSize30]struct { // cache of previously calculated decryption keys
			salt []byte
			key  []byte
			iv   []byte
		}
	}

	archive15 struct {
		h        blockHeader15
		f        archiveFile
		checksum hash32
		crypto   *crypto15
		//dec        decoder       // current decoder
		decVer byte // current decoder version
		multi  bool // archive is multi-volume
		old    bool // archive uses old naming scheme
		solid  bool // archive is a solid archive
	}
)

// parseDosTime converts a 32bit DOS time value to time.Time
func parseDosTime(t uint32) time.Time {
	n := int(t)
	sec := n & 0x1f << 1
	min := n >> 5 & 0x3f
	hr := n >> 11 & 0x1f
	day := n >> 16 & 0x1f
	mon := time.Month(n >> 21 & 0x0f)
	yr := n>>25&0x7f + 1980
	return time.Date(yr, mon, day, hr, min, sec, 0, time.Local)
}

// Calculates the key and iv for AES decryption given a password and salt.
func calcAes30Params(pass []uint16, salt []byte) (key, iv []byte) {
	p := make([]byte, 0, len(pass)*2+len(salt))
	for _, v := range pass {
		p = append(p, byte(v), byte(v>>8))
	}
	p = append(p, salt...)

	hash := sha1.New()
	iv = make([]byte, 16)
	s := make([]byte, 0, hash.Size())
	for i := 0; i < hashRounds; i++ {
		hash.Write(p)
		hash.Write([]byte{byte(i), byte(i >> 8), byte(i >> 16)})
		if i%(hashRounds/16) == 0 {
			s = hash.Sum(s[:0])
			iv[i/(hashRounds/16)] = s[4*4+3]
		}
	}
	key = hash.Sum(s[:0])
	key = key[:16]

	for k := key; len(k) >= 4; k = k[4:] {
		k[0], k[1], k[2], k[3] = k[3], k[2], k[1], k[0]
	}
	return key, iv
}

func openArchive15(f *os.File, password string) (*archive15, error) {
	ar := new(archive15)
	if password != "" {
		ar.crypto = new(crypto15)
		ar.crypto.pass = utf16.Encode([]rune(password)) // convert to UTF-16
	}
	err := ar.open(f)
	if err != nil {
		return nil, err
	}
	return ar, nil
}
func (a *archive15) open(f *os.File) error {
	var crypto *crypto15
	if a.crypto != nil {
		crypto, a.crypto = a.crypto, nil
	}
	a.f.setFile(f)
	data, err := a.f.Get(7)
	if err != nil {
		return err
	}

	if !bytes.Equal(data, []byte{0x52, 0x61, 0x72, 0x21, 0x1a, 0x07, 0x00}) {
		return errInvalidFile
	}
	a.checksum.Hash32 = crc32.NewIEEE()
	err = a.readBlockHeader()
	if err != nil {
		return err
	}
	a.crypto = crypto
	return a.checkArc()
}

func (c *crypto15) getKeys(salt []byte) (key, iv []byte) {
	// check cache of keys
	for _, v := range c.keyCache {
		if bytes.Equal(v.salt[:], salt) {
			return v.key, v.iv
		}
	}
	key, iv = calcAes30Params(c.pass, salt)

	// save a copy in the cache
	copy(c.keyCache[1:], c.keyCache[:])
	c.keyCache[0].salt = append([]byte(nil), salt...) // copy so byte slice can be reused
	c.keyCache[0].key = key
	c.keyCache[0].iv = iv

	return key, iv
}

func (a *archive15) readBlockHeader() error {
	r := a.f.wrappedBuffer
	start := a.f.getPos()
	if a.crypto != nil {
		salt, err := r.Get(saltSize)
		if err != nil {
			return err
		}
		key, iv := a.crypto.getKeys(salt)
		r = wrapBuffer(bufio.NewReader(newAesDecryptReader(a.f.wrappedBuffer, key, iv)))
	}

	b, err := r.Get(7)
	if err != nil {
		return wrapErrEOF(err, errCorruptHeader)
	}

	crc := b.uint16()
	hash := crc32.NewIEEE()
	hash.Write(b)
	a.h.start = start
	a.h.htype = blockType(b.byte())
	a.h.flags = b.uint16()
	size := b.uint16()
	a.h.size = size
	if a.crypto != nil {
		a.h.size += saltSize
	}
	if size < 7 {
		return errCorruptHeader
	}
	size -= 7
	a.h.data, err = r.Get(int(size))
	if err != nil {
		return wrapErrEOF(err, io.ErrUnexpectedEOF)
	}
	hash.Write(a.h.data)
	if crc != uint16(hash.Sum32()) {
		return errBadHeaderCrc
	}
	if a.h.flags&blockHasData > 0 {
		if len(a.h.data) < 4 {
			return errCorruptHeader
		}
		a.h.dataSize = int64(a.h.data.uint32())
	}
	if (a.h.htype == blockService || a.h.htype == blockFile) && a.h.flags&fileLargeData > 0 {
		if len(a.h.data) < 25 {
			return errCorruptHeader
		}
		b := a.h.data[21:25]
		a.h.dataSize |= int64(b.uint32()) << 32
	}
	return nil
}

// decodeName decodes a non-unicode filename from a file header.
func decodeName(buf []byte) string {
	i := bytes.IndexByte(buf, 0)
	if i < 0 {
		return string(buf) // filename is UTF-8
	}

	name := buf[:i]
	encName := readBuf(buf[i+1:])
	if len(encName) < 2 {
		return "" // invalid encoding
	}
	highByte := uint16(encName.byte()) << 8
	flags := encName.byte()
	flagBits := 8
	var wchars []uint16 // decoded characters are UTF-16
	for len(wchars) < len(name) && len(encName) > 0 {
		if flagBits == 0 {
			flags = encName.byte()
			flagBits = 8
			if len(encName) == 0 {
				break
			}
		}
		switch flags >> 6 {
		case 0:
			wchars = append(wchars, uint16(encName.byte()))
		case 1:
			wchars = append(wchars, uint16(encName.byte())|highByte)
		case 2:
			if len(encName) < 2 {
				break
			}
			wchars = append(wchars, encName.uint16())
		case 3:
			n := encName.byte()
			b := name[len(wchars):]
			if l := int(n&0x7f) + 2; l < len(b) {
				b = b[:l]
			}
			if n&0x80 > 0 {
				if len(encName) < 1 {
					break
				}
				ec := encName.byte()
				for _, c := range b {
					wchars = append(wchars, uint16(c+ec)|highByte)
				}
			} else {
				for _, c := range b {
					wchars = append(wchars, uint16(c))
				}
			}
		}
		flags <<= 2
		flagBits -= 2
	}
	return string(utf16.Decode(wchars))
}

// readExtTimes reads and parses the optional extra time field from the file header.
func readExtTimes(f *fileBlockHeader, b *readBuf) {
	if len(*b) < 2 {
		return // invalid, not enough data
	}
	flags := b.uint16()

	ts := []*time.Time{&f.ModificationTime, &f.CreationTime, &f.AccessTime}

	for i, t := range ts {
		n := flags >> uint((3-i)*4)
		if n&0x8 == 0 {
			continue
		}
		if i != 0 { // ModificationTime already read so skip
			if len(*b) < 4 {
				return // invalid, not enough data
			}
			*t = parseDosTime(b.uint32())
		}
		if n&0x4 > 0 {
			*t = t.Add(time.Second)
		}
		n &= 0x3
		if n == 0 {
			continue
		}
		if len(*b) < int(n) {
			return // invalid, not enough data
		}
		// add extra time data in 100's of nanoseconds
		d := time.Duration(0)
		for j := 3 - n; j < n; j++ {
			d |= time.Duration(b.byte()) << (j * 8)
		}
		d *= 100
		*t = t.Add(d)
	}
}

func (a *archive15) parseFileHeader() (*fileBlockHeader, error) {
	f := new(fileBlockHeader)
	f.first = a.h.flags&fileSplitBefore == 0
	f.last = a.h.flags&fileSplitAfter == 0

	f.solid = a.h.flags&fileSolid > 0
	f.IsDir = a.h.flags&fileWindowMask == fileWindowMask
	if !f.IsDir {
		f.winSize = uint(a.h.flags&fileWindowMask)>>5 + 16
	}

	b := a.h.data
	if len(b) < 21 {
		return nil, errCorruptFileHeader
	}

	f.StartOffset = a.h.start
	f.EndOffset = a.h.start + int64(a.h.size) + a.h.dataSize
	if a.crypto != nil {
		i := int64(a.h.size+saltSize) % blockSize
		if i != 0 {
			f.EndOffset += 16 - i
		}
	}
	f.PackedSize = a.h.dataSize
	f.UnPackedSize = int64(b.uint32())
	f.HostOS = b.byte() + 1
	if f.HostOS > HostOSBeOS {
		f.HostOS = HostOSUnknown
	}
	a.checksum.sum = b.uint32()
	if f.last { // split
		f.hash = &a.checksum
	}

	f.ModificationTime = parseDosTime(b.uint32())
	unpackver := b.byte()     // decoder version
	method := b.byte() - 0x30 // decryption method
	namesize := int(b.uint16())
	f.Attributes = int64(b.uint32())
	if a.h.flags&fileLargeData > 0 {
		if len(b) < 8 {
			return nil, errCorruptFileHeader
		}
		_ = b.uint32() // already read large PackedSize in readBlockHeader
		f.UnPackedSize |= int64(b.uint32()) << 32
		f.UnKnownSize = f.UnPackedSize == -1
	} else if int32(f.UnPackedSize) == -1 {
		f.UnKnownSize = true
		f.UnPackedSize = -1
	}
	if len(b) < namesize {
		return nil, errCorruptFileHeader
	}
	name := b.bytes(namesize)
	if a.h.flags&fileUnicode == 0 {
		f.Name = string(name)
	} else {
		f.Name = decodeName(name)
	}
	// Rar 4.x uses '\' as file separator
	f.Name = strings.Replace(f.Name, "\\", "/", -1)

	if a.h.flags&fileVersion > 0 {
		// file version is stored as ';n' appended to file name
		i := strings.LastIndex(f.Name, ";")
		if i > 0 {
			j, err := strconv.Atoi(f.Name[i+1:])
			if err == nil && j >= 0 {
				f.Version = j
				f.Name = f.Name[:i]
			}
		}
	}

	var salt []byte
	if a.h.flags&fileSalt > 0 {
		if len(b) < saltSize {
			return nil, errCorruptFileHeader
		}
		salt = b.bytes(saltSize)
	}
	if a.h.flags&fileExtTime > 0 {
		readExtTimes(f, &b)
	}

	if !f.first {
		return f, nil
	}
	// fields only needed for first block in a file
	if a.h.flags&fileEncrypted > 0 && len(salt) == saltSize {
		f.key, f.iv = a.crypto.getKeys(salt)
	}

	if method == 0 {
		return f, nil
	}
	_ = unpackver
	//if a.dec == nil {
	//	switch unpackver {
	//	case 15, 20, 26:
	//		return nil, errUnsupportedDecoder
	//	case 29:
	//		a.dec = new(decoder29)
	//	default:
	//		return nil, errUnknownDecoder
	//	}
	//	a.decVer = unpackver
	//} else if a.decVer != unpackver {
	//	return nil, errMultipleDecoders
	//}
	//f.decoder = a.dec
	return f, nil
}

func (a *archive15) checkArc() error {
	if (a.h.flags&arcEncrypted == 0) != (a.crypto == nil) {
		return errCrypted
	}
	a.multi = a.h.flags&arcVolume > 0
	a.old = a.h.flags&arcNewNaming == 0
	a.solid = a.h.flags&arcSolid > 0
	return nil
}

func (a *archive15) nextBlock() (*fileBlockHeader, error) {
	for {
		// could return an io.EOF here as 1.5 archives may not have an end block.
		err := a.readBlockHeader()
		if err != nil {
			return nil, err
		}
		switch a.h.htype {
		case blockFile:
			return a.parseFileHeader()
		case blockArc:
			err = a.checkArc()
			if err != nil {
				return nil, err
			}
		case blockEnd:
			if a.h.flags&endArcNotLast == 0 || !a.multi {
				return nil, errArchiveEnd
			}
			return nil, errArchiveContinues
		default:
			_, err = a.f.Discard(a.h.dataSize)
		}
		if err != nil {
			return nil, err
		}
	}
}

func (a *archive15) Next() (*FileHeader, error) {
	//if a.h.data != nil {
	//	a.f.Seek(a.h.start+int64(a.h.size)+a.h.dataSize, io.SeekStart)
	//}
	h, err := a.nextBlock()
	if err != nil {
		return nil, err
	}
	return &h.FileHeader, nil
}

func main() {
	f, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	ar, err := openArchive15(f, "gmw1024")
	if err != nil {
		panic(err)
	}
	var dir string
	{
		name := os.Args[1]
		i := strings.LastIndexByte(name, '/')
		if i != -1 {
			name = name[i:]
		}
		i = strings.IndexByte(name, '.')
		dir = `R:/车库/lsp/guomoo/` + name[:i] + `/`
	}
	for err == nil {
		h, err := ar.Next()
		if err != nil {
			if err == errArchiveEnd {
				break
			}
			panic(err)
		}
		ar.f.Seek(h.EndOffset, io.SeekStart)
		if h.IsDir {
			continue
		}
		inner, err := os.Open(dir + h.Name)
		if err != nil {
			goto printErr
		}
		err = h.CheckFile(inner)
		inner.Close()
		if err == nil {
			fmt.Fprint(os.Stdout, h.Name, " OK         \r")
			continue
		}
	printErr:
		fmt.Println(h.Name, ar.f.f.Name(), h.StartOffset, h.EndOffset, err)
	}
}
