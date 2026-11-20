package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/storage"
)

func insertDiagnostic(t *testing.T, a *Agent, at time.Time, level, component, message, jobID string) int64 {
	t.Helper()
	result, err := a.db.Exec(`INSERT INTO diagnostics(created_at,level,component,message,job_id) VALUES(?,?,?,?,?)`, at.UTC().Format(storage.DiagnosticTimeFormat), level, component, message, jobID)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestDiagnosticsFiltersPaginationAndDetails(t *testing.T) {
	a := testAgent(t)
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 235; i++ {
		insertDiagnostic(t, a, at, "info", "agent", fmt.Sprintf("Event %d", i), "")
	}
	cursor := int64(0)
	seen := map[int64]bool{}
	for n := 0; n < 3; n++ {
		page, err := a.store.ListDiagnostics(model.DiagnosticQuery{Before: cursor})
		if err != nil {
			t.Fatal(err)
		}
		expected := 100
		if n == 2 {
			expected = 35
		}
		if len(page.Items) != expected {
			t.Fatal(n, len(page.Items))
		}
		for _, entry := range page.Items {
			if seen[entry.ID] {
				t.Fatal("duplicate page entry")
			}
			seen[entry.ID] = true
		}
		cursor = page.NextBefore
		if n == 0 {
			insertDiagnostic(t, a, at.Add(time.Hour), "warning", "printer", "New event", "")
		}
	}
	if len(seen) != 235 || cursor != 0 {
		t.Fatal("pagination lost original entries")
	}
	id := insertDiagnostic(t, a, at.Add(time.Minute), "error", "printer", "Failed 100%_literally\nFull detail", "job-42")
	for _, q := range []model.DiagnosticQuery{
		{Level: "error", Component: "printer", Search: "100%_", From: at, To: at.Add(2 * time.Minute)},
		{Search: "JOB-42"},
		{Level: "error", From: at.Add(time.Minute).In(time.FixedZone("UTC+1", 3600)), To: at.Add(2 * time.Minute)},
	} {
		page, err := a.store.ListDiagnostics(q)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != id {
			t.Fatal(q, page, err)
		}
	}
	for _, q := range []model.DiagnosticQuery{{Search: "' OR 1=1 --"}, {Component: "database"}, {Level: "error", To: at.Add(time.Minute)}} {
		page, err := a.store.ListDiagnostics(q)
		if err != nil || len(page.Items) != 0 {
			t.Fatal(q, page, err)
		}
	}
	entry, err := a.store.DiagnosticDetails(id)
	if err != nil || entry.JobID != "job-42" || entry.Message != "Failed 100%_literally\nFull detail" {
		t.Fatal(entry, err)
	}
	_, err = a.store.DiagnosticDetails(-1)
	requireActionFailure(t, err, model.NotFound)
	for _, q := range []model.DiagnosticQuery{{Level: "debug"}, {Component: "invalid"}, {Limit: -1}, {Limit: 501}, {Before: -1}, {From: at, To: at}, {Search: strings.Repeat("x", 501)}} {
		_, err := a.store.ListDiagnostics(q)
		requireActionFailure(t, err, model.InvalidInput)
	}
}

func TestDiagnosticWriterPersistsAndDoesNotExposeReceiptData(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	data := sampleFor(a, p)
	req := model.PrintRequest{Version: 1, JobID: "diagnostic-job", Data: data}
	if r := request(a, "POST", "/v1/print-jobs", req, a.config.APIKey, ""); r.Code != 202 {
		t.Fatal(r.Body.String())
	}
	if r := request(a, "POST", "/v1/print-jobs", req, a.config.APIKey, ""); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	if _, err := (storage.NewLog(a.db)).Write([]byte("Unexpected error\n")); err != nil {
		t.Fatal(err)
	}
	b, err := openTestAgent(t, a.dir)
	if err != nil {
		t.Fatal(err)
	}
	page, err := b.store.ListDiagnostics(model.DiagnosticQuery{})
	if err != nil || len(page.Items) != 4 {
		t.Fatal(page, err)
	}
	if page.Items[0].Level != "error" || page.Items[0].Component != "agent" || page.Items[1].Component != "api" || page.Items[1].JobID != req.JobID {
		t.Fatal(page)
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), a.config.APIKey) || strings.Contains(string(raw), "MALA RADNJA") {
		t.Fatal("diagnostics leaked receipt or credential")
	}
}
