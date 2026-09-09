// Copyright 2023 Filippo Veneri. All rights reserved.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.

package rotoslog

import (
	"io/fs"
	"os"
	"time"
)

type logFile struct {
	file *os.File
	size int64
	// lastWrite is when the file last received a complete log record. It is
	// the reference point for calendar rotation, which needs it on every
	// message and so cannot afford to stat the file for it; the handler stamps
	// it with the timestamp it already reads to test the triggers. Open seeds
	// it from the modification time, so a file inherited from an earlier run
	// keeps its age.
	lastWrite time.Time
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
		_ = f.file.Close()
		f.file = nil
		return err
	}
	f.size = info.Size()
	f.lastWrite = info.ModTime()
	return nil
}

func (f *logFile) Close() (err error) {
	if f.file == nil {
		return nil
	}
	err = f.file.Close()
	f.file = nil
	f.size = 0
	f.lastWrite = time.Time{}
	return
}

func (f *logFile) Stat() (info fs.FileInfo, err error) {
	if f.file == nil {
		return nil, fs.ErrClosed
	}
	info, err = f.file.Stat()
	return
}

func (f *logFile) Write(p []byte) (n int, err error) {
	if f.file == nil {
		return 0, fs.ErrClosed
	}
	n, err = f.file.Write(p)
	f.size += int64(n)
	return
}

func (f *logFile) Size() int64 {
	return f.size
}

func (f *logFile) ModTime() (time.Time, error) {
	info, err := f.Stat()
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}
