package rardecode

import (
	"io"
	"os"
	"path"
)

func (rc *ReadCloser) UnpackTo(baseDir string) error {
	for f, err := rc.Next(); err == nil; f, err = rc.Next() {
		if f.IsDir {
			continue
		}
		os.Stdout.WriteString(f.Name)
		f.Name = baseDir + f.Name
		dir, _ := path.Split(f.Name)
		if _, err := os.Stat(dir); err != nil {
			err = os.MkdirAll(dir, 0755)
			if err != nil {
				return err
			}
		}
		stat, err := os.Stat(f.Name)
		if err == nil && stat.Size() == f.UnPackedSize {
			os.Stdout.WriteString(" skipped\n")
			os.Chtimes(f.Name, f.AccessTime, f.ModificationTime)
			continue
		}
		file, err := os.OpenFile(f.Name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		os.Stdout.WriteString("\n")
		_, err = io.Copy(file, rc)
		file.Close()
		os.Chtimes(f.Name, f.AccessTime, f.ModificationTime)
		if err != nil {
			os.Remove(f.Name)
			return err
		}
	}
	return nil
}
