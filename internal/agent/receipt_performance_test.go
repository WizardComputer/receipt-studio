package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	resources "receiptstudio.local/agent"
)

// Opt-in performance experiment, not a latency assertion. Run without -race.
// Only a temporary database and a loopback sink are used; no real printer.
func TestReceiptPNGPerformance(t *testing.T) {
	output := os.Getenv("RECEIPT_BENCHMARK_OUTPUT")
	if output == "" {
		t.Skip("set RECEIPT_BENCHMARK_OUTPUT to run the manual benchmark")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	save := func(name string, v any) {
		raw, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, name), append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a := testAgent(t)
	entry := a.config.Templates[0]
	counts := []int{10, 50, 100, 150, 200, 300, 400, 500}
	const repetitions = 5
	type caseData struct {
		Items int             `json:"items"`
		Data  json.RawMessage `json:"data"`
	}
	cases := []caseData{}
	var example map[string]any
	if err := json.Unmarshal(entry.SampleData, &example); err != nil {
		t.Fatal(err)
	}
	products := example["items"].([]any)
	for _, n := range counts {
		rows := make([]any, n)
		for i := range rows {
			rows[i] = products[i%len(products)]
		}
		example["items"] = rows
		example["number"] = fmt.Sprintf("BENCH-%03d", n)
		example["total"] = fmt.Sprintf("%d,00 KM", n*5)
		data, err := json.Marshal(example)
		if err != nil {
			t.Fatal(err)
		}
		cases = append(cases, caseData{n, data})
	}
	save("template.json", entry.Template)
	save("cases.json", cases)
	metadata := map[string]any{"go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "logical_cpus": runtime.NumCPU(), "gomaxprocs": runtime.GOMAXPROCS(0), "app_version": resources.AppVersion, "started_at": time.Now().UTC(), "repetitions": repetitions, "warmups_per_size": 1, "template": "unmodified initialTemplate; 80 mm; ESC/POS; text and 3-column table; cyclic default sample products; no QR/logo", "png_measurement": "Encoder.RenderPNG on precompiled commands, includes decoding/layout, rasterization, downsampling and PNG compression; excludes command compilation and persistence", "http_measurement": "separate Python process, fresh TCP connection per request on loopback, unique job_id, return_png=true, full response body read; includes JSON preparation on client, authentication, command compilation, PNG, SQLite commit and Base64 JSON response; excludes client JSON/base64 decoding, process startup and physical printing", "physical_printing": false}
	if b, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
		metadata["cpu"] = string(bytes.TrimSpace(b))
	}
	if b, err := exec.Command("sysctl", "-n", "hw.memsize").Output(); err == nil {
		metadata["ram_bytes"] = string(bytes.TrimSpace(b))
	}
	save("metadata.json", metadata)
	type sample struct {
		MS         float64 `json:"ms"`
		Error      string  `json:"error,omitempty"`
		PNGBytes   int     `json:"png_bytes,omitempty"`
		Width      int     `json:"width,omitempty"`
		Height     int     `json:"height,omitempty"`
		AllocBytes uint64  `json:"allocated_bytes"`
	}
	type result struct {
		Items        int      `json:"items"`
		CommandBytes int      `json:"command_bytes"`
		Warmup       sample   `json:"warmup"`
		Samples      []sample `json:"samples"`
	}
	direct := []result{}
	for _, c := range cases {
		commands, err := a.encoder.Compile(entry.Template, c.Data)
		if err != nil {
			t.Fatal(err)
		}
		row := result{Items: c.Items, CommandBytes: len(commands)}
		for i := -1; i < repetitions; i++ {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			started := time.Now()
			png, err := a.encoder.RenderPNG(entry.Template, commands)
			elapsed := time.Since(started)
			runtime.ReadMemStats(&after)
			s := sample{MS: float64(elapsed) / float64(time.Millisecond), AllocBytes: after.TotalAlloc - before.TotalAlloc}
			if err != nil {
				s.Error = err.Error()
			} else {
				s.PNGBytes = len(png.Data)
				s.Width = png.Width
				s.Height = png.Height
			}
			if i == -1 {
				row.Warmup = s
			} else {
				row.Samples = append(row.Samples, s)
			}
			if i == repetitions-1 && png != nil {
				if err := os.WriteFile(filepath.Join(output, fmt.Sprintf("receipt-%d.png", c.Items)), png.Data, 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		direct = append(direct, row)
		save("png.json", direct)
		t.Logf("PNG %d items: %.1f ms, %d bytes, error=%q", c.Items, row.Samples[0].MS, row.Samples[0].PNGBytes, row.Samples[0].Error)
	}
	// Exercise the real handler, database and worker. The only destination drains
	// bytes on loopback and cannot contact a physical printer.
	sink, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	go func() {
		for {
			c, err := sink.Accept()
			if err != nil {
				return
			}
			io.Copy(io.Discard, c)
			c.Close()
		}
	}()
	printer := addTestPrinter(t, a, "127.0.0.1", sink.Addr().(*net.TCPAddr).Port)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a.host = listener.Addr().String()
	a.origin = "http://" + a.host
	server := &http.Server{Handler: a.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer func() { server.Close(); <-done }()
	ctx, cancel := context.WithCancel(context.Background())
	go a.Worker(ctx)
	defer func() { cancel(); <-a.workerDone }()
	client := exec.Command("python3", "scripts/benchmark-client.py")
	input, err := json.Marshal(map[string]any{"url": a.origin, "api_key": a.config.APIKey, "printer_id": printer.ID, "repetitions": repetitions, "cases": cases})
	if err != nil {
		t.Fatal(err)
	}
	client.Stdin = bytes.NewReader(input)
	client.Stderr = os.Stderr
	response, err := client.Output()
	if err != nil {
		t.Fatalf("benchmark client: %v", err)
	}
	var httpResults any
	if err := json.Unmarshal(response, &httpResults); err != nil {
		t.Fatal(err)
	}
	save("http.json", httpResults)
	t.Log("Results:", output)
}
