rotoslog
--------

This package implements a simple log file rotator handler for slog.
It works out of the box using the standard JSONHandler (default) and
TextHandler for output formatting, but supports custom handlers.

Example using default configuration:
```go
package main

import (
    "log/slog"

    "github/alchemy/rotoslog"
)

func init() {
	h, err := flor.NewFlorHandler(flor.WithFilePrefix("msg-"))
	if err != nil {
		panic(err)
	}
	logger := slog.New(h)
	slog.SetDefault(logger)
}
```

Example using custom slog-formatter handler:
```go
package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"

    "github/alchemy/rotoslog"

	formatter "github.com/samber/slog-formatter"
)

func init() {
	formatter1 := formatter.FormatByKey("pwd", func(v slog.Value) slog.Value {
		return slog.StringValue("***********")
	})
	formatter2 := formatter.ErrorFormatter("error")

	builder := func(w io.Writer, opts *slog.HandlerOptions) slog.Handler {
		formattingMiddleware := formatter.NewFormatterHandler(formatter1, formatter2)
		textHandler := NewTextHandler(w, opts)
		return formattingMiddleware(textHandler)
	}
	h, err := NewHandler(WithHandlerBuilder(builder))
	if err != nil {
		panic(err)
	}
	logger := slog.New(h)
	slog.SetDefault(logger)
}
```
