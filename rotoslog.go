// Copyright 2023 Filippo Veneri. All rights reserved.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.

/*
Package rotoslog provides a [slog.Handler] implementation that writes to a rotating set of files.
Log file names have the following structure: <prefix>(<suffix>|<timestamp>)<extension>.
When creating a new handler the user can set various options:
  - [LogDir]: directory where log files are created (default: "log")
  - [FilePrefix]: file name <prefix> (default: "")
  - [CurrentFileSuffix]: current file name <suffix> (default : "current")
  - [FileExt]: file <extension> (default: ".log")
  - [DateTimeLayout]: <timestamp> layout to be used in calls to [time.Time.Format] (default: "20060102150405.000000000")
  - [MaxFileSize]: size threshold that triggers rotation (default: 32M)
  - [MaxRotatedFiles]: number of rotated files to keep (default: 8)
  - [AutoRotate]: calendar and startup rotation triggers (default: [Never])
  - [HandlerOptions]: [slog.HandlerOptions] (default: zero value)
  - [LogHandlerBuilder]: a function that can build a slog.Handler used for formatting log data (default: [NewJSONHandler])

The returned [Handler] also implements [io.Closer].
*/
package rotoslog

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"log/slog"
)

const (
	DEFAULT_FILE_DIR            = "log"
	DEFAULT_FILE_NAME_PREFIX    = ""
	DEFAULT_CURRENT_FILE_SUFFIX = "current"
	DEFAULT_FILE_EXTENSION      = ".log"
	DEFAULT_CURRENT_FILE_NAME   = DEFAULT_FILE_NAME_PREFIX + DEFAULT_CURRENT_FILE_SUFFIX + DEFAULT_FILE_EXTENSION
	DEFAULT_FILE_DATE_FORMAT    = "20060102150405.000000000"
	DEFAULT_MAX_FILE_SIZE       = 32 * 1024 * 1024
	DEFAULT_MAX_ROTATED_FILES   = 8
)

type config struct {
	logDir            string
	filePrefix        string
	currentFileSuffix string
	fileExtension     string
	dateTimeLayout    string
	maxFileSize       uint64
	maxRotatedFiles   uint64
	autoRotate        RotationTrigger
	handlerOptions    slog.HandlerOptions
	builder           handlerBuilder
	_currentFilePath  string
}

func (cnf *config) currentFileName() string {
	return cnf.filePrefix + cnf.currentFileSuffix + cnf.fileExtension
}

func (cnf *config) rotatedFileName(modTime time.Time, seq int) string {
	var builder strings.Builder
	builder.Grow(len(cnf.filePrefix) + len(cnf.dateTimeLayout) + 3 + len(cnf.fileExtension))
	builder.WriteString(cnf.filePrefix)
	builder.WriteString(modTime.Format(cnf.dateTimeLayout))
	if seq > 0 {
		builder.WriteString("-")
		builder.WriteString(strconv.Itoa(seq))
	}
	builder.WriteString(cnf.fileExtension)
	return builder.String()
}

func (cnf *config) filePath(fileName string) string {
	return filepath.Join(cnf.logDir, fileName)
}

func (cnf *config) currentFilePath() string {
	if cnf._currentFilePath == "" {
		cnf._currentFilePath = cnf.filePath(cnf.currentFileName())
	}
	return cnf._currentFilePath
}

func (cnf *config) rotatedFilePath(modTime time.Time, seq int) string {
	return cnf.filePath(cnf.rotatedFileName(modTime, seq))
}

var defaultConfig = config{
	logDir:            DEFAULT_FILE_DIR,
	filePrefix:        DEFAULT_FILE_NAME_PREFIX,
	currentFileSuffix: DEFAULT_CURRENT_FILE_SUFFIX,
	fileExtension:     DEFAULT_FILE_EXTENSION,
	dateTimeLayout:    DEFAULT_FILE_DATE_FORMAT,
	maxFileSize:       DEFAULT_MAX_FILE_SIZE,
	maxRotatedFiles:   DEFAULT_MAX_ROTATED_FILES,
	autoRotate:        Never,
	handlerOptions:    slog.HandlerOptions{},
	builder: func(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
		return slog.NewJSONHandler(w, opts)
	},
}

type optFun func(*config)

// RotationTrigger identifies an event that causes log file rotation.
type RotationTrigger uint8

// Never disables startup and calendar-based rotation. Size-based rotation is
// controlled separately by MaxFileSize.
const Never RotationTrigger = 0

const (
	// OnStart rotates a non-empty current log file when a handler is created.
	OnStart RotationTrigger = 1 << iota
	// Hourly rotates before the first message in a new local calendar hour.
	Hourly
	// Daily rotates before the first message on a new local calendar day.
	Daily
	// Monthly rotates before the first message in a new local calendar month.
	Monthly
)

// periodicTriggers is the subset of triggers evaluated on every message.
const periodicTriggers = Hourly | Daily | Monthly

// LogDir sets the path to the logging directory
func LogDir(dir string) optFun {
	return func(cnf *config) {
		cnf.logDir = dir
	}
}

// FilePrefix sets the logging file prefix.
func FilePrefix(prefix string) optFun {
	return func(cnf *config) {
		cnf.filePrefix = prefix
	}
}

// CurrentFileSuffix sets the current logging file suffix.
func CurrentFileSuffix(suffix string) optFun {
	return func(cnf *config) {
		cnf.currentFileSuffix = suffix
	}
}

// FileExt sets the log file extension.
func FileExt(ext string) optFun {
	return func(cnf *config) {
		cnf.fileExtension = ext
	}
}

// DateTimeLayout sets the timestamp layout used in rotated file names.
func DateTimeLayout(layout string) optFun {
	return func(cnf *config) {
		cnf.dateTimeLayout = layout
	}
}

// MaxFileSize sets the size threshold that triggers file rotation.
// If size is 0 size-based rotation is disabled; the triggers set by
// AutoRotate still apply.
func MaxFileSize(size uint64) optFun {
	return func(cnf *config) {
		cnf.maxFileSize = size
	}
}

// MaxRotatedFiles sets the maximum number of rotated files.
// When the number of rotated files exceedes this number the
// oldest rotated file is deleted.
// Any value of n less than 1 is equivalent to passing 1.
func MaxRotatedFiles(n uint64) optFun {
	return func(cnf *config) {
		if n < 1 {
			n = 1
		}
		cnf.maxRotatedFiles = n
	}
}

// AutoRotate adds startup or calendar-based rotation triggers. Multiple calls
// accumulate triggers, so AutoRotate(OnStart, Hourly) and AutoRotate(OnStart)
// followed by AutoRotate(Hourly) are equivalent. Never clears the triggers set
// by preceding options and is only meaningful as the sole argument; it does not
// disable size-based rotation, which MaxFileSize(0) controls.
func AutoRotate(triggers ...RotationTrigger) optFun {
	return func(cnf *config) {
		for _, trigger := range triggers {
			if trigger == Never {
				cnf.autoRotate = Never
				continue
			}
			cnf.autoRotate |= trigger
		}
	}
}

// HandlerOptions sets the slog.HandlerOptions for the handler.
func HandlerOptions(opts slog.HandlerOptions) optFun {
	return func(cnf *config) {
		cnf.handlerOptions = opts
	}
}

// HandlerBuilder is a type representing functions used to create
// handlers to control formatting of logging data.
type HandlerBuilder[H slog.Handler] func(w io.Writer, opts *slog.HandlerOptions) H
type handlerBuilder func(w io.Writer, opts *slog.HandlerOptions) slog.Handler

// LogHandlerBuilder sets the HandlerBuilder used for formatting.
func LogHandlerBuilder[H slog.Handler](builder HandlerBuilder[H]) optFun {
	return func(cnf *config) {
		cnf.builder = func(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
			return builder(w, opts)
		}
	}
}

// Handler is a slog.Handler implementation that writes to rotating log files.
// It also implements io.Closer.
type Handler struct {
	w         *logFile
	formatter slog.Handler
	cnf       config
	mu        *sync.Mutex
	nextSeq   *int
}

// NewHandler creates a new handler with the given options.
func NewHandler(options ...optFun) (*Handler, error) {
	h := Handler{
		cnf:     defaultConfig,
		mu:      &sync.Mutex{},
		w:       &logFile{},
		nextSeq: new(int),
	}
	for _, opt := range options {
		opt(&h.cnf)
	}
	err := h.mkLogDir()
	if err != nil {
		return nil, err
	}

	*h.nextSeq, err = h.findNextSequenceNumber()
	if err != nil {
		return nil, err
	}

	err = h.openLogFile()
	if err != nil {
		return nil, err
	}
	if h.cnf.autoRotate&OnStart != 0 && h.w.Size() > 0 {
		if err := h.rotate(); err != nil {
			_ = h.w.Close()
			return nil, err
		}
	}
	h.formatter = h.cnf.builder(h.w, &h.cnf.handlerOptions)
	return &h, nil
}

func (h *Handler) mkLogDir() error {
	path := h.cnf.currentFilePath()
	return os.MkdirAll(filepath.Dir(path), 0755)
}

func (h *Handler) openLogFile() error {
	path := h.cnf.currentFilePath()

	// If the log file doesn't exist, create it, or append to the file
	err := h.w.Open(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	return nil
}

func (h *Handler) findNextSequenceNumber() (int, error) {
	entries, err := os.ReadDir(h.cnf.logDir)
	if err != nil {
		return 0, err
	}

	var latestSeq int = -1
	for _, entry := range entries {
		_, seq, ok := h.parseRotatedFileName(entry.Name())
		if !ok {
			continue
		}
		if latestSeq < seq {
			latestSeq = seq
		}
	}
	return latestSeq + 1, nil
}

// Enabled implements the method of the slog.Handler interface
// by calling the same method of the formatter handler.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.formatter.Enabled(ctx, level)
}

// Handle implements the method of the slog.Handler interface.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if h.cnf.maxFileSize == 0 && !h.periodic() {
		return h.formatter.Handle(ctx, r)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// The calendar comparison needs the current time, so the same reading also
	// serves as the record's write time. A size-only configuration needs
	// neither and skips the clock entirely.
	var now time.Time
	if h.periodic() {
		now = time.Now()
	}
	if err := h.rotateIfNecessary(now); err != nil {
		return err
	}

	err := h.formatter.Handle(ctx, r)
	if err == nil && h.periodic() {
		h.w.lastWrite = now
	}
	return err
}

// periodic reports whether any calendar trigger is configured.
func (h *Handler) periodic() bool {
	return h.cnf.autoRotate&periodicTriggers != 0
}

// rotateIfNecessary rotates the current file if any configured trigger has
// fired. now is only read on the calendar path, and is the zero time when no
// calendar trigger is configured.
func (h *Handler) rotateIfNecessary(now time.Time) error {
	if h.cnf.maxFileSize > 0 && h.w.Size() >= int64(h.cnf.maxFileSize) {
		return h.rotate()
	}

	// An empty current file has nothing worth rotating, and its recorded write
	// time is the moment it was created rather than the age of any content.
	if !h.periodic() || h.w.Size() == 0 {
		return nil
	}

	if crossedCalendarBoundary(h.w.lastWrite, now, h.cnf.autoRotate) {
		return h.rotate()
	}

	return nil
}

// crossedCalendarBoundary reports whether now falls in a later calendar period
// than last, for the finest period any of triggers asks about.
func crossedCalendarBoundary(last, now time.Time, triggers RotationTrigger) bool {
	if !now.After(last) {
		return false
	}
	last = last.In(now.Location())
	lastYear, lastMonth, lastDay := last.Date()
	nowYear, nowMonth, nowDay := now.Date()

	if lastYear != nowYear || lastMonth != nowMonth {
		return triggers&(Monthly|Daily|Hourly) != 0
	}
	if lastDay != nowDay {
		return triggers&(Daily|Hourly) != 0
	}
	if triggers&Hourly == 0 {
		return false
	}
	// A DST fall-back repeats a wall-clock hour at a new offset; the two are
	// different hours even though Hour() reports the same number.
	_, lastOffset := last.Zone()
	_, nowOffset := now.Zone()
	return last.Hour() != now.Hour() || lastOffset != nowOffset
}

// Close closes the current log file. Further writes return an error.
func (h *Handler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.w.Close()
}

func (h *Handler) rotate() error {
	// The kernel keeps the modification time at the moment of the last write,
	// which is the stamp the outgoing file should carry. Stat and the name
	// choice only read, so both happen while the file is still open: that
	// leaves the rename as the sole step needing recovery.
	modTime, err := h.w.ModTime()
	if err != nil {
		return err
	}
	rotatedFilePath, err := h.nextRotatedFilePath(modTime)
	if err != nil {
		return err
	}
	// The file is closed first because Windows refuses to rename an open file.
	if err := h.w.Close(); err != nil {
		return err
	}
	err = os.Rename(h.cnf.currentFilePath(), rotatedFilePath)
	if openErr := h.openLogFile(); err == nil {
		err = openErr
	}
	if err != nil {
		return err
	}
	// Rotation has succeeded at this point: a failure to drop the oldest
	// rotated file must not discard the record being logged.
	_ = h.cleanupRotatedFiles()
	return nil
}

func (h *Handler) nextRotatedFilePath(now time.Time) (string, error) {
	for {
		path := h.cnf.rotatedFilePath(now, *h.nextSeq)
		*h.nextSeq++
		_, err := os.Stat(path)
		if err == nil {
			continue
		}
		if os.IsNotExist(err) {
			return path, nil
		}
		return "", err
	}
}

func (h *Handler) cleanupRotatedFiles() error {
	entries, err := os.ReadDir(h.cnf.logDir)
	if err != nil {
		return err
	}

	var count int
	var oldestName string
	var oldestTS time.Time
	var oldestSeq int
	for _, entry := range entries {
		ts, seq, ok := h.parseRotatedFileName(entry.Name())
		if !ok {
			continue
		}
		count++
		if oldestName == "" || ts.Before(oldestTS) || (ts.Equal(oldestTS) && seq < oldestSeq) {
			oldestName = entry.Name()
			oldestTS = ts
			oldestSeq = seq
		}
	}

	limit := int(h.cnf.maxRotatedFiles)
	if count <= limit {
		return nil
	}
	if oldestName == "" {
		return nil
	}
	return os.Remove(h.cnf.filePath(oldestName))
}

func (h *Handler) parseRotatedFileName(name string) (time.Time, int, bool) {
	if name == h.cnf.currentFileName() {
		return time.Time{}, 0, false
	}
	if !strings.HasPrefix(name, h.cnf.filePrefix) || !strings.HasSuffix(name, h.cnf.fileExtension) {
		return time.Time{}, 0, false
	}

	stem := strings.TrimSuffix(strings.TrimPrefix(name, h.cnf.filePrefix), h.cnf.fileExtension)

	// A sequence suffix is tried first because rotated names usually carry
	// one, and a failed time.Parse allocates its error. The whole stem is the
	// fallback, which is what recognizes both an unsuffixed name and a layout
	// like "2006-01" or "2006-01-02" whose own separator would otherwise be
	// mistaken for the suffix.
	if sep := strings.LastIndex(stem, "-"); sep >= 0 {
		if seq, err := strconv.Atoi(stem[sep+1:]); err == nil {
			if ts, err := time.Parse(h.cnf.dateTimeLayout, stem[:sep]); err == nil {
				return ts, seq, true
			}
		}
	}

	ts, err := time.Parse(h.cnf.dateTimeLayout, stem)
	if err != nil {
		return time.Time{}, 0, false
	}
	return ts, 0, true
}

func (h *Handler) clone() *Handler {
	return &Handler{
		formatter: h.formatter,
		cnf:       h.cnf,
		mu:        h.mu,
		w:         h.w,
		nextSeq:   h.nextSeq,
	}
}

// WithAttrs implements the method of the slog.Handler interface by
// cloning the current handler and calling the WithAttrs of the
// formatter handler.
func (h *Handler) WithAttrs(attr []slog.Attr) slog.Handler {
	nh := h.clone()
	nh.formatter = h.formatter.WithAttrs(attr)
	return nh
}

// WithGroup implements the method of the slog.Handler interface by
// cloning the current handler and calling the WithGroup of the
// formatter handler.
func (h *Handler) WithGroup(name string) slog.Handler {
	nh := h.clone()
	nh.formatter = h.formatter.WithGroup(name)
	return nh
}
