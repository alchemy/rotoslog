rotoslog
--------

This package implements a simple log file rotator handler for slog.
It works out of the box using the standard JSONHandler (default) and
TextHandler for output formatting, but supports custom handlers.
The handler returned by `NewHandler` also implements `io.Closer`.

Automatic rotation
------------------

Files rotate when they reach `MaxFileSize`, and `AutoRotate` adds triggers that
split the log on program startup or on a calendar boundary. For a program that
produces a lot of output, that turns one enormous file into a series of files
you can reason about by name.

The same program usually wants different triggers in development and in
production. While developing a long-running service, a fresh file per run plus
one file per hour keeps the log you are reading down to what just happened:

```go
h, err := rotoslog.NewHandler(
	rotoslog.LogDir("log"),
	rotoslog.FilePrefix("app-"),
	rotoslog.DateTimeLayout("2006-01-02T15"), // one file per hour, so name it for the hour
	rotoslog.MaxFileSize(1024*1024),
	rotoslog.MaxRotatedFiles(4),
	rotoslog.AutoRotate(rotoslog.OnStart, rotoslog.Hourly),
)
```

```
log/app-2026-09-08T22.log   <- yesterday evening
log/app-2026-09-09T09.log   <- this run, before the 10:00 boundary
log/app-current.log         <- being written now
```

Deployed, the same service wants a year of monthly archives instead, and no
rotation on restart:

```go
h, err := rotoslog.NewHandler(
	rotoslog.LogDir("/var/log/app"),
	rotoslog.FilePrefix("app-"),
	rotoslog.DateTimeLayout("2006-01"),
	rotoslog.MaxFileSize(64*1024*1024),
	rotoslog.MaxRotatedFiles(12),
	rotoslog.AutoRotate(rotoslog.Monthly),
)
```

The triggers are `OnStart`, `Hourly`, `Daily` and `Monthly`, and they combine:
pass several to one call, or call `AutoRotate` more than once — `AutoRotate(OnStart, Hourly)`
and `AutoRotate(OnStart)` followed by `AutoRotate(Hourly)` mean the same thing.
`Never` is the default and clears the triggers set by preceding options; it is
only meaningful as the sole argument, and it does not affect size-based
rotation, which `MaxFileSize(0)` disables.

A few details worth knowing:

- Calendar rotation is **lazy** and uses the **local** calendar: the first
  message of the new hour, day or month performs the rotation, so a quiet
  period never produces empty files, and the last message before a boundary is
  never held back into the next file.
- A rotated file is named for the period it **holds**, not for the moment it
  was rotated, so `app-2026-09-08T22.log` contains the 22:00 hour even though
  it was renamed by the first message after 23:00.
- `OnStart` rotates only a non-empty file, and only when the handler is
  created — two handlers sharing a directory each rotate once.
- Coarse `DateTimeLayout`s are the natural pairing for calendar triggers, and
  rotated names may carry a `-1`, `-2`, … sequence suffix, which is what keeps
  them unique when more than one rotation falls inside the same timestamp — a
  size rotation can happen at any point in a period.
- `MaxRotatedFiles` counts every rotated file regardless of which trigger
  created it, and the oldest is deleted first.

Example using default configuration:
```go
package main

import (
	"log/slog"

	"github.com/alchemy/rotoslog"
)

func init() {
	h, err := rotoslog.NewHandler(rotoslog.FilePrefix("msg-"))
	if err != nil {
		panic(err)
	}
	defer h.Close()
	logger := slog.New(h)
	slog.SetDefault(logger)
}
```

Example using custom slog-formatter handler:
```go
package main

import (
	"io"
	"log/slog"

	"github.com/alchemy/rotoslog"

	formatter "github.com/samber/slog-formatter"
)

func init() {
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
	defer h.Close()
	logger := slog.New(h)
	slog.SetDefault(logger)
}
```
