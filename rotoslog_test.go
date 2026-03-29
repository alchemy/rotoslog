// Copyright 2023 Filippo Veneri. All rights reserved.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.

package rotoslog

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tempTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Logf("temp log dir: %s", dir)
	return dir
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

func checkResults(h Handler) error {
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
		return fmt.Errorf("wrong number of log files, got %d, expected %d", n, EXPECTED_NUMBER_OF_FILES)
	}
	return nil
}

func TestHandler(t *testing.T) {
	dir := tempTestDir(t)
	h, err := NewHandler(
		LogDir(dir),
		FilePrefix("test-"),
		CurrentFileSuffix("active"),
		FileExt(".txt"),
		DateTimeLayout("20060102150405.000000000"),
		MaxFileSize(2048),
		HandlerOptions(slog.HandlerOptions{Level: slog.LevelDebug}),
		MaxRotatedFiles(EXPECTED_NUMBER_OF_FILES-1),
		LogHandlerBuilder(slog.NewTextHandler),
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

	err = checkResults(*h)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRotateAtExactThreshold(t *testing.T) {
	dir := tempTestDir(t)
	h, err := NewHandler(
		LogDir(dir),
		FilePrefix("threshold-"),
		DateTimeLayout("20060102150405"),
		MaxFileSize(64),
		MaxRotatedFiles(8),
		LogHandlerBuilder(slog.NewTextHandler),
	)
	if err != nil {
		t.Fatal(err)
	}

	h.w.size = int64(h.cnf.maxFileSize)
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "rotate now", 0)
	if err := h.Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var rotated int
	for _, entry := range entries {
		if _, _, ok := h.parseRotatedFileName(entry.Name()); ok {
			rotated++
		}
	}
	if rotated != 1 {
		t.Fatalf("expected 1 rotated file, got %d", rotated)
	}
}

func TestRotationUsesUniqueNamesWithinSameSecond(t *testing.T) {
	dir := tempTestDir(t)
	h, err := NewHandler(
		LogDir(dir),
		FilePrefix("collision-"),
		DateTimeLayout("20060102150405"),
		MaxFileSize(64),
		MaxRotatedFiles(8),
		LogHandlerBuilder(slog.NewTextHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	h.w.size = int64(h.cnf.maxFileSize)
	if err := h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "first", 0)); err != nil {
		t.Fatal(err)
	}

	h.w.size = int64(h.cnf.maxFileSize)
	if err := h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "second", 0)); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var rotated []string
	for _, entry := range entries {
		if _, _, ok := h.parseRotatedFileName(entry.Name()); ok {
			rotated = append(rotated, entry.Name())
		}
	}
	if len(rotated) != 2 {
		t.Fatalf("expected 2 rotated files, got %d (%v)", len(rotated), rotated)
	}
	if rotated[0] == rotated[1] {
		t.Fatalf("expected unique rotated file names, got %v", rotated)
	}
}

func TestCleanupRotatedFilesIgnoresNonRotatedMatches(t *testing.T) {
	dir := tempTestDir(t)
	h, err := NewHandler(
		LogDir(dir),
		FilePrefix("retention-"),
		DateTimeLayout("20060102150405"),
		MaxRotatedFiles(1),
		LogHandlerBuilder(slog.NewTextHandler),
	)
	if err != nil {
		t.Fatal(err)
	}

	oldTS := time.Date(2024, 1, 1, 1, 1, 1, 0, time.UTC)
	newTS := oldTS.Add(time.Second)
	oldRotated := h.rotatedFileName(oldTS, 0)
	newRotated := h.rotatedFileName(newTS, 0)
	junk := h.cnf.filePrefix + "notes" + h.cnf.fileExtension

	for _, name := range []string{oldRotated, newRotated, junk} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := h.cleanupRotatedFiles(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, oldRotated)); !os.IsNotExist(err) {
		t.Fatalf("expected oldest rotated file to be removed, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, newRotated)); err != nil {
		t.Fatalf("expected newest rotated file to remain, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, junk)); err != nil {
		t.Fatalf("expected non-rotated prefix match to remain, got err=%v", err)
	}
}

func TestCloseIsIdempotentAndStopsWrites(t *testing.T) {
	dir := tempTestDir(t)
	h, err := NewHandler(
		LogDir(dir),
		FilePrefix("close-"),
		LogHandlerBuilder(slog.NewTextHandler),
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}

	err = h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "should fail", 0))
	if err == nil {
		t.Fatal("expected write after Close to fail")
	}
}
