// Copyright 2023 Filippo Veneri. All rights reserved.
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.

package rotoslog

import (
	"context"
	"log/slog"
	"math/rand"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

var benchmarkRunID atomic.Uint64

func getLogger(dir string, options ...optFun) *slog.Logger {
	h, err := NewHandler(append([]optFun{
		LogDir(dir),
		MaxRotatedFiles(1),
		LogHandlerBuilder(slog.NewTextHandler),
	}, options...)...)
	if err != nil {
		panic(err)
	}
	return slog.New(h)
}

func randomLevel() slog.Level {
	const min = -1
	const max = 2
	return slog.Level(4 * (rand.Intn(max-min+1) + min))
}

func BenchmarkLog(b *testing.B) {
	ctx := context.TODO()
	logger := getLogger(b.TempDir()).With("N", b.N)
	for n := 0; n < b.N; n++ {
		l := randomLevel()
		logger.Log(ctx, l, "tanto va la gatta al lardo che ci lascia lo zampino")
	}
}

// BenchmarkLogHourly is BenchmarkLog with a calendar trigger enabled, so the
// two measure the per-message cost of AutoRotate against the same workload.
func BenchmarkLogHourly(b *testing.B) {
	ctx := context.TODO()
	logger := getLogger(b.TempDir(), AutoRotate(Hourly)).With("N", b.N)
	for n := 0; n < b.N; n++ {
		l := randomLevel()
		logger.Log(ctx, l, "tanto va la gatta al lardo che ci lascia lo zampino")
	}
}

func BenchmarkParallelLog(b *testing.B) {
	ctx := context.TODO()
	logger := getLogger(b.TempDir()).With("N", b.N)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l := randomLevel()
			logger.Log(ctx, l, "tanto va la gatta al lardo che ci lascia lo zampino")
		}
	})
}

func parallelLog(rootDir string, k, n int) {
	if n <= 0 {
		return
	}

	var wg sync.WaitGroup
	runDir := filepath.Join(rootDir, strconv.FormatUint(benchmarkRunID.Add(1), 10))

	q := n / k
	r := n % k
	i := 0
	for ; i < k && q > 0; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			loggerDir := filepath.Join(runDir, strconv.Itoa(i))
			logger := getLogger(loggerDir).With("i", i, "q", q)
			ctx := context.TODO()
			for j := 0; j < q; j++ {
				l := randomLevel()
				logger.Log(ctx, l, "tanto va la gatta al lardo che ci lascia lo zampino")
			}
		}()
	}
	if r > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			loggerDir := filepath.Join(runDir, strconv.Itoa(i))
			logger := getLogger(loggerDir).With("i", i, "r", r)
			ctx := context.TODO()
			for j := 0; j < r; j++ {
				l := randomLevel()
				logger.Log(ctx, l, "tanto va la gatta al lardo che ci lascia lo zampino")
			}
		}()
	}
	wg.Wait()
}

func BenchmarkParallelLog1(b *testing.B) {
	rootDir := b.TempDir()
	for n := 0; n < b.N; n++ {
		parallelLog(rootDir, 1, 256)
	}
}

func BenchmarkParallelLog2(b *testing.B) {
	rootDir := b.TempDir()
	for n := 0; n < b.N; n++ {
		parallelLog(rootDir, 2, 256)
	}
}

func BenchmarkParallelLog4(b *testing.B) {
	rootDir := b.TempDir()
	for n := 0; n < b.N; n++ {
		parallelLog(rootDir, 4, 256)
	}
}

func BenchmarkParallelLog8(b *testing.B) {
	rootDir := b.TempDir()
	for n := 0; n < b.N; n++ {
		parallelLog(rootDir, 8, 256)
	}
}

func BenchmarkParallelLog16(b *testing.B) {
	rootDir := b.TempDir()
	for n := 0; n < b.N; n++ {
		parallelLog(rootDir, 16, 256)
	}
}

func BenchmarkRotateHeavy(b *testing.B) {
	ctx := context.TODO()
	h, err := NewHandler(
		LogDir(b.TempDir()),
		MaxFileSize(256),
		MaxRotatedFiles(4),
		LogHandlerBuilder(slog.NewTextHandler),
	)
	if err != nil {
		b.Fatal(err)
	}
	defer h.Close()

	logger := slog.New(h).With("N", b.N)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		l := randomLevel()
		logger.Log(ctx, l, "tanto va la gatta al lardo che ci lascia lo zampino")
	}
}
