package rotoslog

import (
	"io/fs"
	"os"
)

type logFile struct {
	file *os.File
	size int64
}

func (f *logFile) Open(name string, flag int, perm os.FileMode) (err error) {
	if f.file != nil {
		return nil
	}
	f.file, err = os.OpenFile(name, flag, perm)
	if err != nil {
		return err
	}
	info, err := f.file.Stat()
	if err != nil {
		return err
	}
	f.size = info.Size()
	return nil
}

func (f *logFile) Close() (err error) {
	err = f.file.Close()
	f.file = nil
	return
}

func (f *logFile) Stat() (info fs.FileInfo, err error) {
	info, err = f.file.Stat()
	return
}

func (f *logFile) Write(p []byte) (n int, err error) {
	n, err = f.file.Write(p)
	f.size += int64(n)
	return
}

func (f *logFile) Size() int64 {
	return f.size
}
