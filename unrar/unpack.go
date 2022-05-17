package rardecode

import (
	"fmt"
	"io"
	"log"
	"os"
	"path"
)

type onlyWriter struct {
	io.Writer
}

func (r *ReadCloser) UnpackTo(baseDir string) {
	var lastErrPos int64
	for {
		f, err := r.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			if f != nil {
				fmt.Println(f.Offset, f.Offset+f.PackedSize, err)
			}
			continue
		}
		if lastErrPos != 0 {
			fmt.Println("Error at", lastErrPos, f.Offset, f.VolName)
		}
		if f.IsDir {
			continue
		}
		fmt.Print(f.Name, " ", f.VolName, " ", f.Offset, " ", f.Offset+f.PackedSize)
		f.Name = baseDir + f.Name
		dir, _ := path.Split(f.Name)
		if _, err := os.Stat(dir); err != nil {
			err = os.MkdirAll(dir, 0755)
			if err != nil {
				log.Println(err)
				continue
			}
		}
		stat, err := os.Stat(f.Name)
		if err == nil && stat.Size() == f.UnPackedSize {
			os.Stdout.WriteString(" skipped \r")
			os.Chtimes(f.Name, f.AccessTime, f.ModificationTime)
			continue
		}
		file, err := os.OpenFile(f.Name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		_, err = io.Copy(onlyWriter{file}, r)
		file.Close()
		if err != nil {
			os.Stdout.WriteString(" ")
			os.Stdout.WriteString(err.Error())
			os.Remove(f.Name)
		}
		os.Stdout.WriteString(" \n")
		os.Chtimes(f.Name, f.AccessTime, f.ModificationTime)
	}
}
