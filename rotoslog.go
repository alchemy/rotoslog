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
	dateTimeFormat    string
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
	dateTimeStr := modTime.Format(cnf.dateTimeFormat)
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
	dateTimeFormat:    DEFAULT_FILE_DATE_FORMAT,
	maxFileSize:       DEFAULT_MAX_FILE_SIZE,
	maxRotatedFiles:   DEFAULT_MAX_ROTATED_FILES,
	handlerOptions:    slog.HandlerOptions{},
	builder:           NewJSONHandler,
}

type optFun func(*config)

func WithLogDir(dir string) optFun {
	return func(cnf *config) {
		cnf.logDir = dir
	}
}

func WithFilePrefix(prefix string) optFun {
	return func(cnf *config) {
		cnf.filePrefix = prefix
	}
}

func WithCurrentFileSuffix(suffix string) optFun {
	return func(cnf *config) {
		cnf.currentFileSuffix = suffix
	}
}

func WithDateTimeFormat(format string) optFun {
	return func(cnf *config) {
		cnf.dateTimeFormat = format
	}
}

func WithMaxFileSize(size uint64) optFun {
	return func(cnf *config) {
		cnf.maxFileSize = size
	}
}

func WithMaxRotatedFiles(n uint64) optFun {
	return func(cnf *config) {
		cnf.maxRotatedFiles = n
	}
}

func WithHandlerOptions(opts slog.HandlerOptions) optFun {
	return func(cnf *config) {
		cnf.handlerOptions = opts
	}
}

type HandlerBuilder func(w io.Writer, opts *slog.HandlerOptions) slog.Handler

func NewJSONHandler(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
	return slog.NewJSONHandler(w, opts)
}

func NewTextHandler(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
	return slog.NewTextHandler(w, opts)
}

func WithHandlerBuilder(builder HandlerBuilder) optFun {
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

func (h handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.formatter.Enabled(ctx, level)
}

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

		err = h.rotateLogFiles()
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

func (h *handler) rotateLogFiles() error {
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

func (h handler) WithAttrs(attr []slog.Attr) slog.Handler {
	nh := h.clone()
	nh.formatter = h.formatter.WithAttrs(attr)
	return nh
}

func (h handler) WithGroup(name string) slog.Handler {
	nh := h.clone()
	nh.formatter = h.formatter.WithGroup(name)
	return nh
}
