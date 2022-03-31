package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"testing"
)

/*func TestDownloadTask(te *testing.T) {
	t := DownloadTask{}
	t.ranges = append(t.ranges)
	_len := uint64(226310164)
	for i := 0; i < 10; i++ {
		thread, prevThread := t.getThread()
		if thread.end == 0 {
			thread.end = _len
		}
		thread.state = true
		if prevThread != nil {
			if prevThread != thread {
				prevThread.end = thread.start - 1
			}
			i := sort.Search(len(t.ranges), func(i int) bool {
				return thread.end < t.ranges[i].start
			})
			t.ranges = append(t.ranges, nil)
			copy(t.ranges[i+1:], t.ranges[i:])
			t.ranges[i] = thread
			//sort.Slice(t.ranges, func(i, j int) bool {
			//	return t.ranges[i].end < t.ranges[j].start
			//})
		}
		//t.ranges = append(t.ranges, thread)
	}
	for i, thread := range t.ranges {
		fmt.Println(thread, i)
	}
}*/

func TestOCR(t *testing.T) {
	var testF *os.File
	lr := io.LimitReader(testF, 64)

}
