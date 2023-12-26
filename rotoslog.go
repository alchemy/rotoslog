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
  - [DateTimeLayout]: <timestamp> layout to be used in calls to [time.Time.Format] (default: "20060102150405")
  - [MaxFileSize]: size threshold that triggers rotation (default: 32M)
  - [MaxRotatedFiles]: number of rotated files to keep (default: 8)
  - [HandlerOptions]: [slog.HandlerOptions] (default: zero value)
  - [LogHandlerBuilder]: a function that can build a slog.Handler used for formatting log data (default: [NewJSONHandler])
*/
package rotoslog

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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
	DEFAULT_FILE_DATE_FORMAT    = "20060102150405"
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
	builder           HandlerBuilder
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
	builder:           NewJSONHandler,
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
func MaxFileSize(size uint64) optFun {
	return func(cnf *config) {
		cnf.maxFileSize = size
	}
}

// MaxRotatedFiles sets the maximum number of rotated files.
// When this number is exceeded the oldest rotated fle is deleted.
func MaxRotatedFiles(n uint64) optFun {
	return func(cnf *config) {
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
type HandlerBuilder func(w io.Writer, opts *slog.HandlerOptions) slog.Handler

// NewJSONHandler is a HandlerBuilder that creates a slog.JSONHandler.
func NewJSONHandler(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
	return slog.NewJSONHandler(w, opts)
}

// NewTestHandler is a HandlerBuilder that creates a slog.TextHandler.
func NewTextHandler(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
	return slog.NewTextHandler(w, opts)
}

// LogHandlerBuilder sets the HandlerBuilder used for formatting.
func LogHandlerBuilder(builder HandlerBuilder) optFun {
	return func(cnf *config) {
		cnf.builder = builder
	}
}

type handler struct {
	w         *logFile
	formatter slog.Handler
	cnf       config
	mu        *sync.Mutex
}

// NewHandler creates a new handler with the given options.
func NewHandler(options ...optFun) (slog.Handler, error) {
	h := handler{
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
	return h, nil
}

func (h *handler) mkLogDir() error {
	path := h.cnf.currentFilePath()
	return os.MkdirAll(filepath.Dir(path), 0755)
}

func (h *handler) openLogFile() error {
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
func (h handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.formatter.Enabled(ctx, level)
}

// Handle implements the method of the slog.Handler interface.
func (h handler) Handle(ctx context.Context, r slog.Record) error {
	if !h.Enabled(ctx, r.Level) {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	// info, err := h.logFile.Stat()
	// if err != nil {
	// 	return err
	// }
	// if h.logFile.Size() != info.Size() {
	// 	panic(fmt.Errorf("calculated size (%d) differs from actual size (%d)", h.logFile.Size(), info.Size()))
	// }
	if h.w.Size() > int64(h.cnf.maxFileSize) {
		err := h.w.Close()
		if err != nil {
			return err
		}
		rotatedFilePath := h.cnf.rotatedFilePath(time.Now())
		err = os.Rename(h.cnf.currentFilePath(), rotatedFilePath)
		if err != nil {
			return err
		}

		err = h.searchAndRemoveOldestFile()
		if err != nil {
			return err
		}
		//go h.rotateLogFiles()

		err = h.openLogFile()
		if err != nil {
			return err
		}
	}

	return h.formatter.Handle(ctx, r)
}

func (h *handler) searchAndRemoveOldestFile() error {
	entries, err := os.ReadDir(h.cnf.logDir)
	if err != nil {
		return err
	}
	var n uint64
	var oldestEntry fs.DirEntry
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), h.cnf.filePrefix) {
			continue
		}
		n++
		info, err := entry.Info()
		if err != nil {
			return err
		}

		if oldestEntry == nil {
			oldestEntry = entry
			continue
		}

		oldestInfo, err := oldestEntry.Info()
		if err != nil {
			return err
		}

		if info.ModTime().Before(oldestInfo.ModTime()) {
			oldestEntry = entry
		}
	}

	if n > h.cnf.maxRotatedFiles {
		oldestFileName := h.cnf.filePath(oldestEntry.Name())
		err = os.Remove(oldestFileName)
		if err != nil {
			return err
		}
	}
	return nil
}

func (h handler) clone() *handler {
	return &handler{
		formatter: h.formatter,
		cnf:       h.cnf,
		mu:        h.mu,
		w:         h.w,
	}
}

// WithAttrs implements the method of the slog.Handler interface by
// cloning the current handler and calling the WithAttrs of the
// formatter handler.
func (h handler) WithAttrs(attr []slog.Attr) slog.Handler {
	nh := h.clone()
	nh.formatter = h.formatter.WithAttrs(attr)
	return nh
}

// WithGroup implements the method of the slog.Handler interface by
// cloning the current handler and calling the WithGroup of the
// formatter handler.
func (h handler) WithGroup(name string) slog.Handler {
	nh := h.clone()
	nh.formatter = h.formatter.WithGroup(name)
	return nh
}
