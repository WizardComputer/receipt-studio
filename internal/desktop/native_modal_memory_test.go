//go:build desktop && (darwin || windows)

package desktop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"receiptstudio.local/agent/internal/agent"
	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/storage"
)

type modalMemorySample struct {
	HeapBytes       uint64 `json:"heap_bytes_after_gc"`
	HeapInuseBytes  uint64 `json:"heap_inuse_bytes_after_gc"`
	TotalAllocBytes uint64 `json:"total_allocated_bytes"`
}

func sampleModalMemory() modalMemorySample {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return modalMemorySample{m.HeapAlloc, m.HeapInuse, m.TotalAlloc}
}

func prepareModalBenchmarkJob(t *testing.T, a *agent.Agent, count int) []byte {
	t.Helper()
	dir := os.Getenv("RECEIPT_MODAL_MEMORY_INPUT")
	read := func(name string) []byte {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	template := json.RawMessage(read("template.json"))
	var cases []struct {
		Items int
		Data  json.RawMessage
	}
	if err := json.Unmarshal(read("cases.json"), &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if c.Items != count {
			continue
		}
		commands, err := a.Encoder().Compile(template, c.Data)
		if err != nil {
			t.Fatal(err)
		}
		data := read(fmt.Sprintf("receipt-%d.png", count))
		config, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		j := &model.Job{
			Connection: model.Connection{Kind: "tcp"}, ID: "modal-benchmark",
			PrinterID: "benchmark", PrinterName: fmt.Sprintf("Benchmark · %d", count),
			TemplateVersion: 1, Status: "sent", Message: "Simulated benchmark; no printing",
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Host: "127.0.0.1", Port: 1,
			Template: template, Data: c.Data, Bytes: commands, ByteCount: len(commands),
			PNG: &model.ReceiptPNG{ContentType: "image/png", Encoding: "base64", Width: config.Width, Height: config.Height,
				DPI: 203.2, PaperWidthMM: 80, MarginLeft: 32, MarginRight: 32, Scale: 1, Data: data},
		}
		if err := storage.New(testDB(t, a)).InsertJob(j); err != nil {
			t.Fatal(err)
		}
		return data
	}
	t.Fatal("missing benchmark receipt", count)
	return nil
}

func measureDecodedBitmap(t *testing.T, data []byte) map[string]any {
	t.Helper()
	before := sampleModalMemory()
	started := time.Now()
	bitmap, err := ui.DecodeBitmap(data)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	after := sampleModalMemory()
	runtime.KeepAlive(bitmap)
	return map[string]any{"decode_ms": float64(elapsed) / float64(time.Millisecond), "live_heap_delta_bytes": int64(after.HeapBytes) - int64(before.HeapBytes), "allocated_bytes": after.TotalAllocBytes - before.TotalAllocBytes}
}

// Manual measurement only. The real nativeStudio modal/widget code runs in
// MyGo's software-rendered tester, NOT a GPU window. Each size has its own
// process/database, and cached PNGs exclude receipt generation from timings.
func TestNativeReceiptModalMemory(t *testing.T) {
	output := os.Getenv("RECEIPT_MODAL_MEMORY_OUTPUT")
	if output == "" {
		t.Skip("set RECEIPT_MODAL_MEMORY_OUTPUT and RECEIPT_MODAL_MEMORY_INPUT")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	if value := os.Getenv("RECEIPT_MODAL_MEMORY_COUNT"); value == "" {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		for _, count := range []int{10, 50, 100, 150, 200, 300, 400, 500} {
			child := exec.Command(exe, "-test.run=^TestNativeReceiptModalMemory$", "-test.v", "-test.timeout=5m")
			child.Env = append(os.Environ(), fmt.Sprintf("RECEIPT_MODAL_MEMORY_COUNT=%d", count))
			var log bytes.Buffer
			child.Stdout = &log
			child.Stderr = &log
			if err := child.Run(); err != nil {
				t.Fatalf("%d items: %v\n%s", count, err, log.String())
			}
			t.Logf("%d items: completed isolated modal measurement", count)
		}
		return
	}
	count, err := strconv.Atoi(os.Getenv("RECEIPT_MODAL_MEMORY_COUNT"))
	if err != nil {
		t.Fatal(err)
	}
	a := testAgent(t)
	data := prepareModalBenchmarkJob(t, a, count)
	decodes := []map[string]any{}
	for i := 0; i < 3; i++ {
		decodes = append(decodes, measureDecodedBitmap(t, data))
	}
	s := newNativeStudio(a)
	s.page = "Poslovi printanja"
	page, err := a.ListJobs(model.JobQuery{})
	if err != nil {
		t.Fatal(err)
	}
	s.jobs, s.cursor, s.counts = page.Items, page.Cursor, page.Counts
	tt := ui.NewTester(s.view, 1000, 850)
	tt.Advance(time.Second)
	baseline := sampleModalMemory()
	cycles := []map[string]any{}
	var closed modalMemorySample
	for i := 0; i < 3; i++ {
		started := time.Now()
		s.openJob("modal-benchmark")
		tt.Frame()
		detailsMS := float64(time.Since(started)) / float64(time.Millisecond)
		if s.modal != "job" {
			t.Fatal("job modal failed to open")
		}
		details := sampleModalMemory()
		started = time.Now()
		if err := tt.Click("Pregled"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		previewMS := float64(time.Since(started)) / float64(time.Millisecond)
		if s.jobBitmap == nil {
			t.Fatal("no bitmap in preview")
		}
		tt.Advance(time.Second)
		preview := sampleModalMemory()
		started = time.Now()
		if err := tt.Click("Zatvori detalje"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		closeMS := float64(time.Since(started)) / float64(time.Millisecond)
		tt.Advance(time.Second)
		if s.modal != "" {
			t.Fatal("modal stayed open")
		}
		closed = sampleModalMemory()
		runtime.KeepAlive(s)
		runtime.KeepAlive(tt)
		cycles = append(cycles, map[string]any{"cycle": i + 1, "details_ms": detailsMS, "preview_ms": previewMS, "close_ms": closeMS, "details": details, "preview": preview, "closed": closed, "bitmap_referenced_after_close": s.jobBitmap != nil, "preview_heap_delta_bytes": int64(preview.HeapBytes) - int64(baseline.HeapBytes), "closed_heap_delta_bytes": int64(closed.HeapBytes) - int64(baseline.HeapBytes)})
	}
	// Diagnostic control on this throwaway studio ONLY, not a production fix.
	// Keep history/SQLite untouched while dropping the unused modal references.
	s.jobBitmap = nil
	s.job = nil
	tt.Advance(time.Second)
	tt.Frame()
	cleared := sampleModalMemory()
	runtime.KeepAlive(s)
	runtime.KeepAlive(tt)
	runtime.KeepAlive(data)
	report := map[string]any{"items": count, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "png_bytes": len(data), "method": "real nativeStudio modal in MyGo software tester; isolated process per size; cached PNG; 3 open/preview/close cycles; forced GC at memory checkpoints; CPU Go heap only, no GPU/driver/RSS measurement; control clears references in test instance only", "decode_samples": decodes, "baseline": baseline, "cycles": cycles, "after_reference_clear": cleared, "bytes_freed_by_reference_clear": int64(closed.HeapBytes) - int64(cleared.HeapBytes), "cleared_heap_delta_bytes": int64(cleared.HeapBytes) - int64(baseline.HeapBytes)}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, fmt.Sprintf("modal-%d.json", count)), append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
