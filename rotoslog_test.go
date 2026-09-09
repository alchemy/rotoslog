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

// backdateCurrentFile ages the current log file so the next message looks as
// though it arrives in a new calendar period. Both timestamps move: the cached
// reference the boundary check reads, and the modification time the rotated
// file is stamped from.
func backdateCurrentFile(t *testing.T, h *Handler, to time.Time) {
	t.Helper()
	h.w.lastWrite = to
	if err := os.Chtimes(h.cnf.currentFilePath(), to, to); err != nil {
		t.Fatal(err)
	}
}

// currentFilePathFor reports the current-file path a handler configured with
// options would use, so fixtures cannot drift from the naming defaults.
func currentFilePathFor(options ...optFun) string {
	cnf := defaultConfig
	for _, opt := range options {
		opt(&cnf)
	}
	return cnf.currentFilePath()
}

// rotatedFileNames splits h's log directory into the names h recognizes as its
// own rotated files and everything else other than the current file.
func rotatedFileNames(t *testing.T, h *Handler) (rotated, other []string) {
	t.Helper()
	entries, err := os.ReadDir(h.cnf.logDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		switch _, _, ok := h.parseRotatedFileName(entry.Name()); {
		case ok:
			rotated = append(rotated, entry.Name())
		case entry.Name() != h.cnf.currentFileName():
			other = append(other, entry.Name())
		}
	}
	return rotated, other
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

	if rotated, _ := rotatedFileNames(t, h); len(rotated) != 1 {
		t.Fatalf("expected 1 rotated file, got %d", len(rotated))
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

	rotated, _ := rotatedFileNames(t, h)
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
	oldRotated := h.cnf.rotatedFileName(oldTS, 0)
	newRotated := h.cnf.rotatedFileName(newTS, 0)
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

func TestAutoRotateOptionsAccumulateAndNeverClears(t *testing.T) {
	cnf := defaultConfig
	AutoRotate(OnStart)(&cnf)
	AutoRotate(Hourly, Daily)(&cnf)
	if want := OnStart | Hourly | Daily; cnf.autoRotate != want {
		t.Fatalf("got triggers %04b, want %04b", cnf.autoRotate, want)
	}

	AutoRotate(Never)(&cnf)
	if cnf.autoRotate != Never {
		t.Fatalf("Never did not clear triggers: got %04b", cnf.autoRotate)
	}

	AutoRotate(Monthly)(&cnf)
	if cnf.autoRotate != Monthly {
		t.Fatalf("option following Never was not applied: got %04b", cnf.autoRotate)
	}
}

func TestCalendarBoundaries(t *testing.T) {
	loc := time.FixedZone("test", 2*60*60)
	tests := []struct {
		name    string
		trigger RotationTrigger
		last    time.Time
		now     time.Time
		crossed bool
	}{
		{
			name:    "same hour",
			trigger: Hourly,
			last:    time.Date(2026, time.September, 8, 10, 1, 0, 0, loc),
			now:     time.Date(2026, time.September, 8, 10, 59, 0, 0, loc),
		},
		{
			name:    "new hour",
			trigger: Hourly,
			last:    time.Date(2026, time.September, 8, 10, 59, 0, 0, loc),
			now:     time.Date(2026, time.September, 8, 11, 0, 0, 0, loc),
			crossed: true,
		},
		{
			name:    "new day",
			trigger: Daily,
			last:    time.Date(2026, time.September, 8, 23, 59, 0, 0, loc),
			now:     time.Date(2026, time.September, 9, 0, 0, 0, 0, loc),
			crossed: true,
		},
		{
			name:    "same calendar day",
			trigger: Daily,
			last:    time.Date(2026, time.September, 8, 0, 0, 0, 0, loc),
			now:     time.Date(2026, time.September, 8, 23, 59, 0, 0, loc),
		},
		{
			name:    "clock moved backward into prior hour",
			trigger: Hourly,
			last:    time.Date(2026, time.September, 8, 11, 1, 0, 0, loc),
			now:     time.Date(2026, time.September, 8, 10, 59, 0, 0, loc),
		},
		{
			name:    "same instant",
			trigger: Hourly,
			last:    time.Date(2026, time.September, 8, 11, 0, 0, 0, loc),
			now:     time.Date(2026, time.September, 8, 11, 0, 0, 0, loc),
		},
		{
			name:    "new month",
			trigger: Monthly,
			last:    time.Date(2026, time.September, 30, 23, 59, 0, 0, loc),
			now:     time.Date(2026, time.October, 1, 0, 0, 0, 0, loc),
			crossed: true,
		},
		{
			name:    "same month",
			trigger: Monthly,
			last:    time.Date(2026, time.September, 1, 0, 0, 0, 0, loc),
			now:     time.Date(2026, time.September, 30, 23, 59, 0, 0, loc),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := crossedCalendarBoundary(tt.last, tt.now, tt.trigger); got != tt.crossed {
				t.Fatalf("got %v, want %v", got, tt.crossed)
			}
		})
	}
}

func TestHourlyRotationPutsBoundaryMessageInNewFile(t *testing.T) {
	dir := tempTestDir(t)
	h, err := NewHandler(
		LogDir(dir),
		FilePrefix("hourly-"),
		MaxFileSize(0),
		AutoRotate(Hourly),
		LogHandlerBuilder(slog.NewTextHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	if err := h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "old hour", 0)); err != nil {
		t.Fatal(err)
	}
	backdateCurrentFile(t, h, time.Now().Add(-25*time.Hour))
	if err := h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "new hour", 0)); err != nil {
		t.Fatal(err)
	}

	rotatedNames, _ := rotatedFileNames(t, h)
	if len(rotatedNames) != 1 {
		t.Fatalf("got %d rotated files, want 1: %v", len(rotatedNames), rotatedNames)
	}
	rotated, err := os.ReadFile(h.cnf.filePath(rotatedNames[0]))
	if err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(h.cnf.currentFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(rotated, []byte("old hour")) || bytes.Contains(rotated, []byte("new hour")) {
		t.Fatalf("unexpected rotated contents: %q", rotated)
	}
	if !bytes.Contains(current, []byte("new hour")) || bytes.Contains(current, []byte("old hour")) {
		t.Fatalf("unexpected current contents: %q", current)
	}
}

func TestOnStartRotatesOnlyNonEmptyCurrentFile(t *testing.T) {
	t.Run("non-empty", func(t *testing.T) {
		dir := tempTestDir(t)
		current := currentFilePathFor(LogDir(dir), FilePrefix("startup-"))
		if err := os.WriteFile(current, []byte("previous run\n"), 0644); err != nil {
			t.Fatal(err)
		}

		h, err := NewHandler(
			LogDir(dir),
			FilePrefix("startup-"),
			MaxFileSize(0),
			AutoRotate(OnStart),
		)
		if err != nil {
			t.Fatal(err)
		}
		defer h.Close()

		if rotated, _ := rotatedFileNames(t, h); len(rotated) != 1 {
			t.Fatalf("got %d rotated files, want 1", len(rotated))
		}
		if h.w.Size() != 0 {
			t.Fatalf("new current file has size %d, want 0", h.w.Size())
		}
	})

	t.Run("empty", func(t *testing.T) {
		dir := tempTestDir(t)
		current := currentFilePathFor(LogDir(dir), FilePrefix("startup-"))
		if err := os.WriteFile(current, nil, 0644); err != nil {
			t.Fatal(err)
		}

		h, err := NewHandler(LogDir(dir), FilePrefix("startup-"), AutoRotate(OnStart))
		if err != nil {
			t.Fatal(err)
		}
		defer h.Close()

		if rotated, other := rotatedFileNames(t, h); len(rotated) != 0 || len(other) != 0 {
			t.Fatalf("empty current file was rotated: %v %v", rotated, other)
		}
	})
}

func TestPeriodicRotationUsesExistingFileModificationTime(t *testing.T) {
	dir := tempTestDir(t)
	current := currentFilePathFor(LogDir(dir), FilePrefix("existing-"))
	old := time.Now().AddDate(0, 0, -2)
	if err := os.WriteFile(current, []byte("previous day\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(current, old, old); err != nil {
		t.Fatal(err)
	}

	h, err := NewHandler(
		LogDir(dir),
		FilePrefix("existing-"),
		MaxFileSize(0),
		AutoRotate(Daily),
		LogHandlerBuilder(slog.NewTextHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	if err := h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "new day", 0)); err != nil {
		t.Fatal(err)
	}
	if rotated, _ := rotatedFileNames(t, h); len(rotated) != 1 {
		t.Fatalf("got %d rotated files, want 1", len(rotated))
	}
}

func TestPeriodicRotationIgnoresFutureFileModificationTime(t *testing.T) {
	dir := tempTestDir(t)
	current := currentFilePathFor(LogDir(dir), FilePrefix("future-"))
	future := time.Now().AddDate(1, 0, 0)
	if err := os.WriteFile(current, []byte("future timestamp\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(current, future, future); err != nil {
		t.Fatal(err)
	}

	h, err := NewHandler(
		LogDir(dir),
		FilePrefix("future-"),
		MaxFileSize(0),
		AutoRotate(Hourly, Daily, Monthly),
		LogHandlerBuilder(slog.NewTextHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	if err := h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "clock corrected", 0)); err != nil {
		t.Fatal(err)
	}
	if rotated, _ := rotatedFileNames(t, h); len(rotated) != 0 {
		t.Fatalf("future modification time caused rotation: %v", rotated)
	}
}

func TestClonedHandlersShareCalendarRotationState(t *testing.T) {
	dir := tempTestDir(t)
	h, err := NewHandler(
		LogDir(dir),
		FilePrefix("clone-"),
		MaxFileSize(0),
		AutoRotate(Hourly),
		LogHandlerBuilder(slog.NewTextHandler),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	clone := h.WithAttrs([]slog.Attr{slog.String("source", "clone")}).(*Handler)

	if err := h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "before", 0)); err != nil {
		t.Fatal(err)
	}
	backdateCurrentFile(t, h, time.Now().Add(-25*time.Hour))
	if err := clone.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "boundary", 0)); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "same hour", 0)); err != nil {
		t.Fatal(err)
	}

	if rotated, _ := rotatedFileNames(t, h); len(rotated) != 1 {
		t.Fatalf("got %d rotations at one boundary, want 1", len(rotated))
	}
}

func TestHourlyRotationOnDSTFallBack(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skipf("tz database unavailable: %v", err)
	}

	// 2026-10-25 02:30 occurs twice in Europe/Rome: once at +02:00 (CEST) and
	// again an hour later at +01:00 (CET). The wall-clock hour is unchanged, so
	// only the zone offset distinguishes the two instants.
	last := time.Date(2026, time.October, 25, 0, 30, 0, 0, time.UTC).In(loc)
	now := time.Date(2026, time.October, 25, 1, 30, 0, 0, time.UTC).In(loc)
	if last.Hour() != now.Hour() {
		t.Fatalf("test premise broken: hours differ (%v vs %v)", last, now)
	}
	_, lastOffset := last.Zone()
	_, nowOffset := now.Zone()
	if lastOffset == nowOffset {
		t.Fatalf("test premise broken: offsets equal (%v vs %v)", last, now)
	}

	if !crossedCalendarBoundary(last, now, Hourly) {
		t.Fatal("repeated wall-clock hour after DST fall-back not treated as a new hour")
	}
	if crossedCalendarBoundary(last, last.Add(20*time.Minute), Hourly) {
		t.Fatal("rotated within the same hour and offset")
	}
}

func TestMaxRotatedFilesWithPeriodShapedLayouts(t *testing.T) {
	// Calendar rotation makes coarse, dash-separated layouts a natural choice.
	// Their trailing digits must not be read as a sequence suffix, or the
	// unsuffixed file of each period is never counted nor deleted.
	layouts := []string{"2006-01", "2006-01-02", "20060102-15", "20060102150405"}
	for _, layout := range layouts {
		t.Run(layout, func(t *testing.T) {
			dir := tempTestDir(t)
			const maxRotated = 2
			h, err := NewHandler(
				LogDir(dir),
				DateTimeLayout(layout),
				MaxFileSize(1),
				MaxRotatedFiles(maxRotated),
				LogHandlerBuilder(slog.NewTextHandler),
			)
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()

			for i := 0; i < maxRotated+4; i++ {
				if err := h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "m", 0)); err != nil {
					t.Fatal(err)
				}
			}

			rotated, unrecognized := rotatedFileNames(t, h)
			if len(unrecognized) > 0 {
				t.Errorf("rotated files not recognized by the configured layout: %v", unrecognized)
			}
			if len(rotated) > maxRotated {
				t.Errorf("kept %d rotated files, want at most %d: %v", len(rotated), maxRotated, rotated)
			}
		})
	}
}

func TestRotatedFileIsNamedForThePeriodItHolds(t *testing.T) {
	// Calendar rotation is lazy: it fires on the first message of the new
	// period, so the file being rotated out holds the *previous* period's
	// records and must carry that period's timestamp.
	t.Run("calendar trigger", func(t *testing.T) {
		dir := tempTestDir(t)
		h, err := NewHandler(
			LogDir(dir),
			FilePrefix("daily-"),
			DateTimeLayout("2006-01-02"),
			MaxFileSize(0),
			AutoRotate(Daily),
			LogHandlerBuilder(slog.NewTextHandler),
		)
		if err != nil {
			t.Fatal(err)
		}
		defer h.Close()

		if err := h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "before boundary", 0)); err != nil {
			t.Fatal(err)
		}
		// Age the file so the next message lands on a new day.
		yesterday := time.Now().AddDate(0, 0, -1)
		backdateCurrentFile(t, h, yesterday)
		if err := h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "after boundary", 0)); err != nil {
			t.Fatal(err)
		}

		name := onlyRotatedFile(t, h)
		assertStampedFor(t, h, name, yesterday)
		content, err := os.ReadFile(h.cnf.filePath(name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(content, []byte("before boundary")) || bytes.Contains(content, []byte("after boundary")) {
			t.Errorf("rotated file holds %q, want only the record written before the boundary", content)
		}
	})

	// A file inherited from an earlier run is named for when that run last
	// logged, not for the moment this one started.
	t.Run("startup trigger", func(t *testing.T) {
		dir := tempTestDir(t)
		current := currentFilePathFor(LogDir(dir), FilePrefix("startup-"))
		if err := os.WriteFile(current, []byte("previous run\n"), 0644); err != nil {
			t.Fatal(err)
		}
		lastRun := time.Now().AddDate(0, 0, -3)
		if err := os.Chtimes(current, lastRun, lastRun); err != nil {
			t.Fatal(err)
		}

		h, err := NewHandler(
			LogDir(dir),
			FilePrefix("startup-"),
			DateTimeLayout("2006-01-02"),
			MaxFileSize(0),
			AutoRotate(OnStart),
			LogHandlerBuilder(slog.NewTextHandler),
		)
		if err != nil {
			t.Fatal(err)
		}
		defer h.Close()

		assertStampedFor(t, h, onlyRotatedFile(t, h), lastRun)
	})
}

// onlyRotatedFile returns the name of the single rotated file h's log
// directory is expected to hold.
func onlyRotatedFile(t *testing.T, h *Handler) string {
	t.Helper()
	rotated, _ := rotatedFileNames(t, h)
	if len(rotated) != 1 {
		t.Fatalf("got %d rotated files, want 1: %v", len(rotated), rotated)
	}
	return rotated[0]
}

// assertStampedFor checks that a rotated file's timestamp names the same
// period as want, ignoring the sequence number.
func assertStampedFor(t *testing.T, h *Handler, name string, want time.Time) {
	t.Helper()
	ts, _, ok := h.parseRotatedFileName(name)
	if !ok {
		t.Fatalf("rotated file %q does not parse under layout %q", name, h.cnf.dateTimeLayout)
	}
	gotStamp := ts.Format(h.cnf.dateTimeLayout)
	wantStamp := want.Format(h.cnf.dateTimeLayout)
	if gotStamp != wantStamp {
		t.Errorf("rotated file %q is stamped %s, want %s (the period its contents belong to)",
			name, gotStamp, wantStamp)
	}
}
