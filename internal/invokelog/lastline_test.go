package invokelog

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAppendAcrossDayWithEmptyCurrentFile(t *testing.T) {
	dir := t.TempDir()
	logger, moveTo := clockAt(time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC))
	logger.opts.Dir = logsDir(dir)
	logger.opts.RetentionDays = 90
	mustAppend(t, logger, "op.one")
	moveTo(time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC))
	if err := os.WriteFile(filepath.Join(logger.opts.Dir, "2026-03-02.jsonl"), nil, fileMode); err != nil {
		t.Fatal(err)
	}
	mustAppend(t, logger, "op.two")
	report, err := VerifyWith(dir, nil)
	if err != nil || report.Broken {
		t.Fatalf("Verify() = %+v, %v; want intact chain", report, err)
	}
}

func seedDay(b *testing.B, n int) *Logger {
	logger, _ := clockAt(time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC))
	logger.opts.Dir = logsDir(b.TempDir())
	logger.opts.RetentionDays = 90
	if err := os.MkdirAll(logger.opts.Dir, dirMode); err != nil {
		b.Fatal(err)
	}
	var buf bytes.Buffer
	line := []byte(`{"seq":1,"operation":"` + strings.Repeat("a", 150) + `"}` + "\n")
	for i := 0; i < n-1; i++ {
		buf.Write(line)
	}
	if err := os.WriteFile(filepath.Join(logger.opts.Dir, "2026-04-01.jsonl"), buf.Bytes(), fileMode); err != nil {
		b.Fatal(err)
	}
	return logger
}

func benchFields() Fields {
	return Fields{Path: "cli", Operation: "bench.append", Effect: "read", Result: "success"}
}

func BenchmarkAppendGrowingDay(b *testing.B) {
	for _, n := range []int{1000, 10000, 100000} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			logger := seedDay(b, n)
			path := filepath.Join(logger.opts.Dir, "2026-04-01.jsonl")
			info, _ := os.Stat(path)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := logger.Append(benchFields()); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(info.Size()), "dayfile-bytes")
		})
	}
}

func BenchmarkAppendParallel(b *testing.B) {
	logger := seedDay(b, 10000)
	var mu sync.Mutex
	b.SetParallelism(4)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := logger.Append(benchFields()); err != nil {
				mu.Lock()
				b.Error(err)
				mu.Unlock()
				return
			}
		}
	})
}

// TestAppendRefusesATornLastLine keeps a line that does not parse at the end of the log, torn by a crash or
// changed, from being chained over: Append fails and writes nothing, and Verify reports the line.
func TestAppendRefusesATornLastLine(t *testing.T) {
	dir := t.TempDir()
	logger, _ := clockAt(time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC))
	logger.opts.Dir = logsDir(dir)
	mustAppend(t, logger, "op.one")
	path := filepath.Join(logger.opts.Dir, "2026-03-01.jsonl")
	lines := readLines(t, path)
	torn := append(append([]byte(nil), lines[0]...), "\n"...)
	torn = append(torn, lines[0][:len(lines[0])/2]...)
	if err := os.WriteFile(path, torn, fileMode); err != nil {
		t.Fatal(err)
	}
	if err := logger.Append(benchFields()); err == nil {
		t.Fatal("Append() chained over a torn last line")
	}
	if data, _ := os.ReadFile(path); !bytes.Equal(data, torn) {
		t.Fatalf("Append() wrote after a torn last line: %q", data)
	}
	if report := verifyWith(t, dir, nil); !report.Broken || report.Days[0].Problem != "a line does not parse as a log entry" {
		t.Fatalf("report = %+v, want the torn line reported", report)
	}
}

// TestSeedDaysSmall checks SeedDayFiles at a size that costs next to nothing, including that
// retention removes nothing and the empty newest file is skipped.
func TestSeedDaysSmall(t *testing.T) {
	for _, emptyNewest := range []bool{false, true} {
		now := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
		vault := t.TempDir()
		logger, _ := clockAt(now)
		logger.opts.Dir = logsDir(vault)
		logger.opts.RetentionDays = 100000
		if err := SeedDayFiles(vault, 5, emptyNewest, now); err != nil {
			t.Fatal(err)
		}
		mustAppend(t, logger, "op.next")
		files, err := dayFiles(logger.opts.Dir)
		if err != nil || len(files) != 5 {
			t.Fatalf("emptyNewest=%v: files = %v, err = %v; want 5 and nothing removed", emptyNewest, files, err)
		}
		lines := readLines(t, filepath.Join(logger.opts.Dir, files[len(files)-1]))
		if raw := lines[len(lines)-1]; !bytes.Contains(raw, []byte("op.next")) {
			t.Fatalf("emptyNewest=%v: last line = %q", emptyNewest, raw)
		}
	}
}
