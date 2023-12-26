// Copyright 2023 Filippo Veneri. All rights reserved.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.

package rotoslog

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func init() {
	os.RemoveAll(defaultConfig.logDir)
}

func countLinesInFile(filename string) (int, error) {
	lineSep := []byte{'\n'}
	buf, err := os.ReadFile(filename)
	if err != nil {
		return 0, err
	}
	return bytes.Count(buf, lineSep), nil
}

const (
	EXPECTED_NUMBER_OF_FILES = 3
	EXPECTED_LINE_NUMBER     = 32
)

func checkResults(h handler) error {
	entries, err := os.ReadDir(h.cnf.logDir)
	if err != nil {
		return err
	}
	var n uint64
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), h.cnf.filePrefix) {
			continue
		}
		n++
		path := h.cnf.filePath(entry.Name())
		l, err := countLinesInFile(path)
		if err != nil {
			return err
		}
		if l != EXPECTED_LINE_NUMBER {
			return fmt.Errorf("%s has the wrong number of lines: got %d, expected %d", path, l, EXPECTED_LINE_NUMBER)
		}
	}
	if n != EXPECTED_NUMBER_OF_FILES {
		return errors.New("too many log files")
	}
	return nil
}

func TestHandler(t *testing.T) {
	h, err := NewHandler(
		FilePrefix("test-"),
		CurrentFileSuffix("active"),
		FileExt(".txt"),
		DateTimeLayout(time.StampNano),
		MaxFileSize(2048),
		HandlerOptions(slog.HandlerOptions{Level: slog.LevelDebug}),
		MaxRotatedFiles(EXPECTED_NUMBER_OF_FILES-1),
		LogHandlerBuilder(NewTextHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(h)

	n := (EXPECTED_NUMBER_OF_FILES + 1) * 32 / 4
	for i := 0; i < n; i++ {
		logger.Debug("dbg msg", "i", i)
		logger.Info("nfo msg", "i", i)
		logger.Warn("wrn msg", "i", i)
		logger.Error("err msg", "i", i)
	}

	err = checkResults(h.(handler))
	if err != nil {
		t.Fatal(err)
	}
}
