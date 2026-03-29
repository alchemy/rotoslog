// Copyright 2023 Filippo Veneri. All rights reserved.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.

package rotoslog

import (
	"errors"
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
		_ = f.file.Close()
		f.file = nil
		return err
	}
	f.size = info.Size()
	return nil
}

func (f *logFile) Close() (err error) {
	if f.file == nil {
		return nil
	}
	err = f.file.Close()
	f.file = nil
	f.size = 0
	return
}

func (f *logFile) Stat() (info fs.FileInfo, err error) {
	if f.file == nil {
		return nil, errors.New("log file is closed")
	}
	info, err = f.file.Stat()
	return
}

func (f *logFile) Write(p []byte) (n int, err error) {
	if f.file == nil {
		return 0, errors.New("log file is closed")
	}
	n, err = f.file.Write(p)
	f.size += int64(n)
	return
}

func (f *logFile) Size() int64 {
	return f.size
}
