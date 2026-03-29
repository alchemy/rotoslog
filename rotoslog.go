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
	handlerOptions    slog.HandlerOptions
	builder           handlerBuilder
	_currentFilePath  string
}

func (cnf *config) currentFileName() string {
	return cnf.filePrefix + cnf.currentFileSuffix + cnf.fileExtension
}

func (cnf *config) rotatedFileName(modTime time.Time) string {
	dateTimeStr := modTime.Format(cnf.dateTimeLayout)
	return cnf.filePrefix + dateTimeStr + cnf.fileExtension
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

func (cnf *config) rotatedFilePath(modTime time.Time) string {
	return cnf.filePath(cnf.rotatedFileName(modTime))
}

var defaultConfig = config{
	logDir:            DEFAULT_FILE_DIR,
	filePrefix:        DEFAULT_FILE_NAME_PREFIX,
	currentFileSuffix: DEFAULT_CURRENT_FILE_SUFFIX,
	fileExtension:     DEFAULT_FILE_EXTENSION,
	dateTimeLayout:    DEFAULT_FILE_DATE_FORMAT,
	maxFileSize:       DEFAULT_MAX_FILE_SIZE,
	maxRotatedFiles:   DEFAULT_MAX_ROTATED_FILES,
	handlerOptions:    slog.HandlerOptions{},
	builder: func(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
		return slog.NewJSONHandler(w, opts)
	},
}

type optFun func(*config)

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
// If size is 0 file rotation is disabled.
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
}

// NewHandler creates a new handler with the given options.
func NewHandler(options ...optFun) (*Handler, error) {
	h := Handler{
		cnf: defaultConfig,
		mu:  &sync.Mutex{},
		w:   &logFile{},
	}
	for _, opt := range options {
		opt(&h.cnf)
	}
	err := h.mkLogDir()
	if err != nil {
		return nil, err
	}
	err = h.openLogFile()
	if err != nil {
		return nil, err
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

// Enabled implements the method of the slog.Handler interface
// by calling the same method of the formatter habdler.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.formatter.Enabled(ctx, level)
}

// Handle implements the method of the slog.Handler interface.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if h.cnf.maxFileSize <= 0 {
		return h.formatter.Handle(ctx, r)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.w.Size() >= int64(h.cnf.maxFileSize) {
		err := h.rotate(time.Now())
		if err != nil {
			return err
		}
	}

	return h.formatter.Handle(ctx, r)
}

// Close closes the current log file. Further writes return an error.
func (h *Handler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.w.Close()
}

func (h *Handler) rotate(now time.Time) error {
	if err := h.w.Close(); err != nil {
		return err
	}
	rotatedFilePath, err := h.nextRotatedFilePath(now)
	if err != nil {
		return err
	}
	if err := os.Rename(h.cnf.currentFilePath(), rotatedFilePath); err != nil {
		return err
	}
	if err := h.cleanupRotatedFiles(); err != nil {
		return err
	}
	return h.openLogFile()
}

func (h *Handler) nextRotatedFilePath(now time.Time) (string, error) {
	for seq := 0; ; seq++ {
		path := h.cnf.filePath(h.rotatedFileName(now, seq))
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

func (h *Handler) rotatedFileName(ts time.Time, seq int) string {
	base := h.cnf.rotatedFileName(ts)
	if seq == 0 {
		return base
	}
	return strings.TrimSuffix(base, h.cnf.fileExtension) + "-" + strconv.Itoa(seq) + h.cnf.fileExtension
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
	seq := 0
	tsStem := stem
	if sep := strings.LastIndex(stem, "-"); sep >= 0 {
		parsedSeq, err := strconv.Atoi(stem[sep+1:])
		if err == nil {
			seq = parsedSeq
			tsStem = stem[:sep]
		}
	}

	ts, err := time.Parse(h.cnf.dateTimeLayout, tsStem)
	if err != nil {
		return time.Time{}, 0, false
	}
	return ts, seq, true
}

func (h *Handler) clone() *Handler {
	return &Handler{
		formatter: h.formatter,
		cnf:       h.cnf,
		mu:        h.mu,
		w:         h.w,
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
