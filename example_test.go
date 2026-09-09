// Copyright 2023 Filippo Veneri. All rights reserved.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.

package rotoslog_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"testing"

	"github.com/alchemy/rotoslog"
	formatter "github.com/samber/slog-formatter"
)

func randomLevel() slog.Level {
	const min = -1
	const max = 2
	return slog.Level(4 * (rand.Intn(max-min+1) + min))
}

func ExampleLogHandlerBuilder() {
	const N = 10

	formatter1 := formatter.FormatByKey("pwd", func(v slog.Value) slog.Value {
		return slog.StringValue("***********")
	})
	formatter2 := formatter.ErrorFormatter("error")

	builder := func(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
		formattingMiddleware := formatter.NewFormatterHandler(formatter1, formatter2)
		textHandler := slog.NewTextHandler(w, opts)
		return formattingMiddleware(textHandler)
	}
	h, err := rotoslog.NewHandler(rotoslog.LogHandlerBuilder(builder))
	if err != nil {
		panic(err)
	}
	logger := slog.New(h).With("N", N, "pwd", "123456")
	slog.SetDefault(logger)

	ctx := context.TODO()
	for n := 0; n < N; n++ {
		l := randomLevel()
		if l == slog.LevelError {
			err := fmt.Errorf("random error n° %d", n)
			slog.Log(ctx, l, "tanto va la gatta al lardo che ci lascia lo zampino", "error", err)
			continue
		}
		slog.Log(ctx, l, "tanto va la gatta al lardo che ci lascia lo zampino")
	}
}

func ExampleNewHandler() {
	dir, err := os.MkdirTemp("", "rotoslog-example-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	h, err := rotoslog.NewHandler(
		rotoslog.LogDir(dir),
		rotoslog.FilePrefix("app-"),
		rotoslog.LogHandlerBuilder(slog.NewTextHandler),
	)
	if err != nil {
		panic(err)
	}
	defer h.Close()

	logger := slog.New(h)
	logger.Info("started", "component", "example")
}

// ExampleAutoRotate shows the development half of a typical pair of
// configurations: each run starts a fresh log file, and a run long enough to
// cross an hour boundary is split there too. In production the same program
// would keep the size and retention options but ask only for
// rotoslog.AutoRotate(rotoslog.Monthly).
func ExampleAutoRotate() {
	dir, err := os.MkdirTemp("", "rotoslog-autorotate-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	// Two runs of the same program, sharing one log directory.
	run := func(message string) {
		h, err := rotoslog.NewHandler(
			rotoslog.LogDir(dir),
			rotoslog.FilePrefix("app-"),
			// One file per hour, so name the rotated files for the hour.
			rotoslog.DateTimeLayout("2006-01-02T15"),
			rotoslog.MaxFileSize(1024*1024),
			rotoslog.MaxRotatedFiles(4),
			rotoslog.AutoRotate(rotoslog.OnStart, rotoslog.Hourly),
			rotoslog.LogHandlerBuilder(slog.NewTextHandler),
		)
		if err != nil {
			panic(err)
		}
		defer h.Close()

		slog.New(h).Info(message)
	}

	run("first run")
	// OnStart moves the first run's log aside, so the two runs never share a
	// file even though neither of them filled one or crossed an hour.
	run("second run")

	entries, err := os.ReadDir(dir)
	if err != nil {
		panic(err)
	}
	// The live file keeps a fixed name, <prefix>current<ext>; the run that was
	// moved aside is named for the hour it holds.
	var rotated int
	for _, entry := range entries {
		if entry.Name() != "app-current.log" {
			rotated++
		}
	}
	fmt.Printf("%d files: the current log plus %d rotated\n", len(entries), rotated)

	// Output:
	// 2 files: the current log plus 1 rotated
}

func TestExamples(t *testing.T) {
	ExampleLogHandlerBuilder()
	ExampleNewHandler()
	ExampleAutoRotate()
}
