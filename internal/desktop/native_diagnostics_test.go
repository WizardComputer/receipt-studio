//go:build desktop && (darwin || windows)

package desktop

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"receiptstudio.local/agent/internal/model"
)

func waitDiagnostics(t *testing.T, s *nativeStudio, tt *ui.Tester) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for s.diagnostics.pending != nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		tt.Frame()
	}
	if s.diagnostics.pending != nil || s.diagnostics.failure != "" {
		t.Fatal("diagnostics did not load", s.diagnostics.failure)
	}
	tt.Frame()
}

func TestNativeDiagnosticsFiltersDetailsAndLanguages(t *testing.T) {
	a := testAgent(t)
	at := time.Now().UTC()
	id := insertDiagnostic(t, a, at, "error", "printer", "Printer unavailable\nConnection timed out", "")
	insertDiagnostic(t, a, at, "info", "agent", "Agent je pokrenut.", "")
	s := newNativeStudio(a)
	s.diagnosticsVisible = true
	tt := ui.NewTester(s.view, 1000, 650)
	if err := tt.Click("Dijagnostika"); err != nil {
		t.Fatal(err)
	}
	waitDiagnostics(t, s, tt)
	if len(s.diagnostics.items) != 2 {
		t.Fatal("missing entries")
	}
	if dir := os.Getenv("RECEIPT_NATIVE_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, "Diagnostics.png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}

	s.diagnostics.draft.Level = "error"
	tt.Frame()
	if err := tt.Click("Primijeni"); err != nil {
		t.Fatal(err)
	}
	waitDiagnostics(t, s, tt)
	if len(s.diagnostics.items) != 1 || s.diagnostics.items[0].ID != id {
		t.Fatal("filter not applied")
	}
	if err := tt.Click(fmt.Sprintf("Detalji zapisa #%d", id)); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.modal != "diagnostic" || s.diagnostics.selected.ID != id {
		t.Fatal("detail modal missing")
	}
	for _, label := range []string{"Kopiraj zapis", "Zatvori detalje", "Printer unavailable\nConnection timed out"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatal("missing detail", label)
		}
	}
	if !strings.Contains(diagnosticCopyText(s.diagnostics.selected), "Connection timed out") {
		t.Fatal("copy loses full message")
	}
	if dir := os.Getenv("RECEIPT_NATIVE_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(filepath.Join(dir, "Diagnostics-details.png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tt.Click("Zatvori detalje"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("English"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, label := range []string{"Diagnostics", "Level", "Component", "Refresh diagnostics", "Clear filters"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatal("missing English label", label)
		}
	}
	if err := tt.Click("Clear filters"); err != nil {
		t.Fatal(err)
	}
	waitDiagnostics(t, s, tt)
	if len(s.diagnostics.items) != 3 {
		t.Fatal("clear filters did not restore entries")
	}
	s.diagnostics.draft.Search = "no matching message"
	tt.Frame()
	if err := tt.Click("Apply"); err != nil {
		t.Fatal(err)
	}
	waitDiagnostics(t, s, tt)
	if _, ok := tt.Find("No diagnostic entries match these filters."); !ok {
		t.Fatal("empty state missing")
	}
}

func TestDiagnosticDateRangeIncludesEndMinute(t *testing.T) {
	f := diagnosticFilters{From: "2026-11-16 12:00", To: "2026-11-16 12:00"}
	q, err := f.query()
	if err != nil || q.To.Sub(q.From) != time.Minute {
		t.Fatal(q, err)
	}
	for _, f := range []diagnosticFilters{{From: "invalid"}, {From: "2026-11-17 12:00", To: "2026-11-16 12:00"}} {
		if _, err := f.query(); err == nil {
			t.Fatal("invalid range accepted")
		}
	}
}

func TestNativeDiagnosticOpensRelatedJob(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	job, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "related-job", Data: sampleFor(a, p)})
	if err != nil {
		t.Fatal(err)
	}
	id := insertDiagnostic(t, a, time.Now().UTC(), "info", "api", "Zahtjev za printanje je prihvaćen.", job.ID)
	s := newNativeStudio(a)
	s.diagnosticsVisible = true
	s.page = "Dijagnostika"
	s.diagnostics.selected, err = a.DiagnosticDetails(id)
	if err != nil {
		t.Fatal(err)
	}
	s.modal = "diagnostic"
	tt := ui.NewTester(s.view, 1000, 650)
	if err := tt.Click("Otvori posao"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.modal != "job" || s.job["id"] != job.ID || countTestJobs(t, a) != 1 {
		t.Fatal("related job did not open safely")
	}
}

func TestNativeDiagnosticsLoadsMoreOnScroll(t *testing.T) {
	a := testAgent(t)
	for i := 0; i < 105; i++ {
		insertDiagnostic(t, a, time.Now().UTC(), "info", "agent", fmt.Sprintf("Diagnostic %d", i), "")
	}
	s := newNativeStudio(a)
	s.diagnosticsVisible = true
	tt := ui.NewTester(s.view, 1000, 650)
	if err := tt.Click("Dijagnostika"); err != nil {
		t.Fatal(err)
	}
	waitDiagnostics(t, s, tt)
	if len(s.diagnostics.items) != 100 || s.diagnostics.before == 0 {
		t.Fatal("initial page incorrect")
	}
	tt.Scroll(800, 550, 0, 20000)
	tt.Frame()
	waitDiagnostics(t, s, tt)
	if len(s.diagnostics.items) != 105 || s.diagnostics.before != 0 {
		t.Fatal("scroll did not load remaining entries", len(s.diagnostics.items))
	}
}
