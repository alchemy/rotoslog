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

}

func TestExamples(t *testing.T) {
	ExampleLogHandlerBuilder()
}
