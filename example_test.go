// Copyright 2023 Filippo Veneri. All rights reserved.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.

package rotoslog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	formatter "github.com/samber/slog-formatter"
)

func ExampleFormatter() {
	const N = 10

	formatter1 := formatter.FormatByKey("pwd", func(v slog.Value) slog.Value {
		return slog.StringValue("***********")
	})
	formatter2 := formatter.ErrorFormatter("error")

	builder := func(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
		formattingMiddleware := formatter.NewFormatterHandler(formatter1, formatter2)
		textHandler := NewTextHandler(w, opts)
		return formattingMiddleware(textHandler)
	}
	h, err := NewHandler(LogHandlerBuilder(builder))
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

func TestExamples(t *testing.T) {
	ExampleFormatter()
}
