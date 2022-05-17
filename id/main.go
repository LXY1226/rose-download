package main

import (
	"bufio"
	"fmt"
	rar "github.com/nwaples/rardecode"
	"io"
	"os"
	"strings"
)

func main() {
	file := os.Args[1]
	i := strings.IndexByte(file, '.')
	dir := `R:/车库/lsp/guomoo/` + file[:i] + `/`
	println(dir)
	unpackRAR(os.Args[1], dir)
	os.Exit(0)
}

type rarArchives struct {
	curF       *os.File
	files      []string
	pos        int
	lastOffset int64
}

func (a *rarArchives) Rd() *bufio.Reader {
	//a.offsets = make([]int64, 1, len(a.files)) // [0, offsetOf(part2), offsetOf(part3)...]
	if err := a.NextFile(); err != nil {
		panic(err)
	}
	return bufio.NewReader(a)
}

func (a *rarArchives) NextFile() error {
	var err error
	if a.pos == len(a.files) { // 所有文件都已打开
		panic(io.EOF)
	}
	a.curF, err = os.Open(a.files[a.pos])
	if err != nil {
		return err
	}
	stat, err := a.curF.Stat()
	if err != nil {
		return err
	}
	a.lastOffset += stat.Size()
	a.pos++
	return nil
}

func (a *rarArchives) Read(p []byte) (n int, err error) {
	n, err = a.curF.Read(p)
	if err != nil {
		if err == io.EOF {
			err = a.NextFile()
		}
	}
	return
}

func (a *rarArchives) MarkRange(start, end int64) {
	fStat, err := a.curF.Stat()
	if err != nil {
		panic(err)
	}
	prevPos := a.lastOffset - fStat.Size()
	if start < prevPos { // 有上一文件的末尾损坏
		markInfect(a.files[a.pos-2], prevPos, start, end)
	}
	markInfect(a.files[a.pos-1], a.lastOffset, start, end)
}

func markInfect(filename string, endOffset, start, end int64) {
	stat, err := os.Stat(filename) // 上一文件
	if err != nil {
		panic(err)
	}
	endOffset -= stat.Size()
	start -= endOffset
	end -= endOffset
	if end > stat.Size() {
		end = stat.Size()
	}
	statF, err := os.OpenFile(filename+".stat", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		panic(err)
	}
	fmt.Sprintf("%d:%d\n", start, end)
	statF.Close()
}

type errFile struct {
	filename   string
	start, end int
}

func unpackRAR(firstFile string, workDir string) {
	r, err := rar.OpenReader(firstFile, "123123")
	if err != nil {
		panic(err)
	}
	r.UnpackTo(workDir)
}
