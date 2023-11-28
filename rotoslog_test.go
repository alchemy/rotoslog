package rotoslog

import (
	"context"
	"log/slog"
	"math/rand"
	"os"
	"sync"
	"testing"
)

func init() {
	os.RemoveAll(defaultConfig.logDir)
	h, err := NewHandler(WithMaxRotatedFiles(4))
	if err != nil {
		panic(err)
	}
	logger := slog.New(h)
	slog.SetDefault(logger)
}

func randomLevel() slog.Level {
	const min = -1
	const max = 2
	return slog.Level(4 * (rand.Intn(max-min+1) + min))
}

func BenchmarkLog(b *testing.B) {
	ctx := context.TODO()
	logger := slog.Default().With("N", b.N)
	for n := 0; n < b.N; n++ {
		l := randomLevel()
		logger.Log(ctx, l, "tanto va la gatta al lardo che ci lascia lo zampino")
	}
}

func BenchmarkParallelLog(b *testing.B) {
	ctx := context.TODO()
	logger := slog.Default().With("N", b.N)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l := randomLevel()
			logger.Log(ctx, l, "tanto va la gatta al lardo che ci lascia lo zampino")
		}
	})
}

func parallelLog(k, n int) {
	if n <= 0 {
		return
	}

	var wg sync.WaitGroup

	q := n / k
	r := n % k
	i := 0
	for ; i < k && q > 0; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			logger := slog.Default().With("i", i, "q", q)
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
			logger := slog.Default().With("i", i, "r", r)
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
	for n := 0; n < b.N; n++ {
		parallelLog(1, 256)
	}
}

func BenchmarkParallelLog2(b *testing.B) {
	for n := 0; n < b.N; n++ {
		parallelLog(2, 256)
	}
}

func BenchmarkParallelLog4(b *testing.B) {
	for n := 0; n < b.N; n++ {
		parallelLog(4, 256)
	}
}

func BenchmarkParallelLog8(b *testing.B) {
	for n := 0; n < b.N; n++ {
		parallelLog(8, 256)
	}
}

func BenchmarkParallelLog16(b *testing.B) {
	for n := 0; n < b.N; n++ {
		parallelLog(16, 256)
	}
}