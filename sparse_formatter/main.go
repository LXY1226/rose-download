package main

import (
	"bytes"
	"fmt"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"syscall"
	"unsafe"
)

type FileAllocatedRangeBuffer struct {
	FileOffset int64
	Length     int64
}

func main() {
	curDir, err := os.Open(".")
	if err != nil {
		panic(err)
	}
	list, err := curDir.ReadDir(0)
	if err != nil {
		panic(err)
	}
	var queryRange FileAllocatedRangeBuffer
	var respRange [1024]FileAllocatedRangeBuffer
	buf := make([]byte, 0x10000)
	for _, file := range list {
		if file.IsDir() {
			continue
		}
		f, err := os.Open(file.Name())
		if err != nil {
			panic(err)
		}
		queryRange.Length, _ = f.Seek(0, io.SeekEnd)
		h := syscall.Handle(f.Fd())
		var ret uint32
		err = syscall.DeviceIoControl(h, windows.FSCTL_QUERY_ALLOCATED_RANGES,
			(*byte)(unsafe.Pointer(&queryRange)),
			uint32(unsafe.Sizeof(queryRange)),
			(*byte)(unsafe.Pointer(&respRange)),
			uint32(unsafe.Sizeof(queryRange))*1024,
			&ret, nil)
		if err != nil {
			panic(err)
		}
		ret /= 8 * 2
		var i uint32
		var end int64
		var stat bytes.Buffer
		for end != queryRange.Length {
			if i > ret {
				break
			}
			for {
				if end < 0x10000 {
					end = 0
					break
				}
				end -= 0x10000
				f.ReadAt(buf, end)
				var last int64
				for i, v := range buf {
					if v != 0 {
						last = int64(i)
					}
				}
				if last != 0 {
					end = end + last
					break
				}
			}
			if respRange[i].FileOffset != 0 {
				fmt.Fprintf(&stat, "%d-%d\n", end, respRange[i].FileOffset)
				fmt.Println(f.Name(), end, respRange[i].FileOffset)
			}
			end = respRange[i].FileOffset + respRange[i].Length
			i++
		}
		if stat.Len() != 0 {
			os.WriteFile(f.Name()+".stat", stat.Bytes(), 0644)
		}
	}
}
