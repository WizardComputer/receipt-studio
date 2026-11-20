package agent

import (
	"bytes"
	"context"
	"testing"
	"time"

	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/storage"
)

func TestDiagnosticRetentionKeepsBoundaryAndOtherData(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	job, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "keep-job", Data: sampleFor(a, p)})
	if err != nil {
		t.Fatal(err)
	}
	config := testJSON(t, a.ConfigSnapshot())
	savedJob := testJSON(t, mustJob(t, a, job.ID))
	now := time.Now().In(time.FixedZone("UTC+1", 3600))
	cutoff := now.Add(-storage.DiagnosticRetention)
	for _, level := range []string{"info", "warning", "error"} {
		insertDiagnostic(t, a, cutoff.Add(-time.Nanosecond), level, "printer", "expired", job.ID)
	}
	keep := []int64{
		insertDiagnostic(t, a, cutoff, "info", "agent", "exact boundary", ""),
		insertDiagnostic(t, a, cutoff.Add(time.Nanosecond), "error", "agent", "newer", ""),
		insertDiagnostic(t, a, now, "warning", "agent", "recent", ""),
	}
	count, err := a.store.PruneDiagnostics(context.Background(), now)
	if err != nil || count != 3 {
		t.Fatal("wrong cleanup count", count, err)
	}
	for _, id := range keep {
		if _, err := a.store.DiagnosticDetails(id); err != nil {
			t.Fatal("retained entry missing", id, err)
		}
	}
	count, err = a.store.PruneDiagnostics(context.Background(), now)
	if err != nil || count != 0 {
		t.Fatal("cleanup not idempotent", count, err)
	}
	afterJob := testJSON(t, mustJob(t, a, job.ID))
	if !bytes.Equal(savedJob, afterJob) || !bytes.Equal(config, testJSON(t, a.ConfigSnapshot())) {
		t.Fatal("cleanup modified receipts or configuration")
	}
	var versions int
	if err := a.db.QueryRow(`SELECT count(*) FROM template_versions`).Scan(&versions); err != nil || versions != 1 {
		t.Fatal("cleanup modified template history", versions, err)
	}
}

func TestDiagnosticMaintenanceCleansOnStartupAndLaterTicks(t *testing.T) {
	a := testAgent(t)
	now := time.Now()
	insertDiagnostic(t, a, now.Add(-16*24*time.Hour), "error", "agent", "expired at startup", "")
	ticks := make(chan time.Time)
	close(ticks)
	a.maintainDiagnostics(context.Background(), ticks)
	page, err := a.store.ListDiagnostics(model.DiagnosticQuery{Search: "expired at startup"})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("startup did not clean old entries", page, err)
	}
	lastDiagnostic(t, a, "info", "database", "Obrisano 1 dijagnostičkih zapisa")
	insertDiagnostic(t, a, now.Add(-14*24*time.Hour), "warning", "agent", "expires later", "")
	ticks = make(chan time.Time, 1)
	ticks <- now.Add(2 * 24 * time.Hour)
	close(ticks)
	a.maintainDiagnostics(context.Background(), ticks)
	page, err = a.store.ListDiagnostics(model.DiagnosticQuery{Search: "expires later"})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("scheduled cleanup did not run", page, err)
	}
	lastDiagnostic(t, a, "info", "database", "Obrisano 1 dijagnostičkih zapisa")
}

func TestDiagnosticMaintenanceFailureAndCancellation(t *testing.T) {
	a := testAgent(t)
	id := insertDiagnostic(t, a, time.Now().Add(-16*24*time.Hour), "info", "agent", "keep on failure", "")
	if _, err := a.db.Exec(`CREATE TRIGGER fail_cleanup BEFORE DELETE ON diagnostics BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	a.cleanDiagnostics(context.Background(), time.Now())
	lastDiagnostic(t, a, "error", "database", "Automatsko čišćenje dijagnostike nije uspjelo.")
	if _, err := a.store.DiagnosticDetails(id); err != nil {
		t.Fatal("failed cleanup deleted entry", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before, err := a.store.ListDiagnostics(model.DiagnosticQuery{})
	if err != nil {
		t.Fatal(err)
	}
	done := a.StartDiagnosticMaintenance(ctx)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("maintenance did not stop")
	}
	after, err := a.store.ListDiagnostics(model.DiagnosticQuery{})
	if err != nil || len(before.Items) != len(after.Items) {
		t.Fatal("cancelled maintenance logged a false failure", before, after, err)
	}
}
