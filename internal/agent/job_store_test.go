package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/storage"
)

func seedJobHistory(t *testing.T, a *Agent, count int) {
	t.Helper()
	tx, err := a.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	at := time.Now().Add(-time.Hour).UTC().Format(storage.JobTimeFormat)
	_, err = tx.Exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i+1<?)
 INSERT INTO jobs (`+storage.JobColumns+`) SELECT printf('history-%05d',i),'old-printer','Old printer',1,'sent','Saved',?,?,NULL,NULL,'',3 FROM n`, count, at, at)
	if err != nil {
		t.Fatal(err)
	}
	template := a.config.Templates[0]
	_, err = tx.Exec(`INSERT INTO job_receipts(job_id,connection,queue,host,port,template,data,commands) SELECT id,'tcp','','127.0.0.1',1,?,?,? FROM jobs WHERE id LIKE 'history-%'`, []byte(template.Template), []byte(template.SampleData), []byte{27, 64, 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestLargeJobHistoryIsQueriedWithoutLoadingReceipts(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	seedJobHistory(t, a, 10050)
	// Unreadable historical image metadata must not break startup/listing.
	// Only explicitly requesting this receipt may touch its image.
	if _, err := a.db.Exec(`INSERT INTO job_pngs(job_id,data,width,height,dpi,paper_width_mm,margin_left,margin_right,scale) VALUES('history-00000',?,'invalid-width',1,203,80,32,32,1)`, []byte("invalid-png")); err != nil {
		t.Fatal(err)
	}
	b, err := openTestAgent(t, a.dir)
	if err != nil {
		t.Fatal("startup read old receipt data", err)
	}
	page, err := b.store.ListJobs(model.JobQuery{Status: "sent"})
	if err != nil || len(page.Items) != 100 || page.Total != 10050 || page.Counts["sent"] != 10050 {
		t.Fatal("large history unavailable", page.Total, len(page.Items), err)
	}
	for _, item := range page.Items {
		for _, key := range []string{"png", "data", "bytes", "template"} {
			if _, exists := item[key]; exists {
				t.Fatal("receipt content in list", key)
			}
		}
	}
	if j, err := b.store.JobStatus("history-00000"); err != nil || j.PNG != nil || j.Bytes != nil {
		t.Fatal("status read receipt", err)
	}
	if _, err := b.JobPreview("history-00000"); err == nil {
		t.Fatal("bad image was unexpectedly readable")
	}
	req := model.PrintRequest{Version: 1, JobID: "after-ten-thousand", PrinterID: p.ID, Data: sampleFor(a, p)}
	j, duplicate, err := b.Enqueue(req)
	if err != nil || duplicate || j == nil {
		t.Fatal("history size blocked enqueue", err)
	}
	if _, err := b.ReprintJob("history-10049"); err != nil {
		t.Fatal("history size blocked reprint", err)
	}
	if countTestJobs(t, b) != 10052 {
		t.Fatal("wrong number of durable jobs")
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		page, err := b.store.ListJobs(model.JobQuery{Status: "sent", Limit: 500, Before: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range page.Items {
			id := row["id"].(string)
			if seen[id] {
				t.Fatal("duplicate across pages", id)
			}
			seen[id] = true
		}
		cursor = page.Cursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 10050 {
		t.Fatal("history truncated", len(seen))
	}
	for _, query := range []struct {
		sql, index string
		args       []any
	}{
		{`SELECT ` + storage.JobColumns + ` FROM jobs WHERE status=? AND (created_at,id)<(?,?) ORDER BY created_at DESC,id DESC LIMIT 101`, "jobs_status_created_id", []any{"sent", time.Now().UTC().Format(storage.JobTimeFormat), "z"}},
		{`SELECT ` + storage.JobColumns + ` FROM jobs WHERE (created_at,id)<(?,?) ORDER BY created_at DESC,id DESC LIMIT 101`, "jobs_created_id", []any{time.Now().UTC().Format(storage.JobTimeFormat), "z"}},
	} {
		rows, err := b.db.Query("EXPLAIN QUERY PLAN "+query.sql, query.args...)
		if err != nil {
			t.Fatal(err)
		}
		plan := ""
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan += detail
		}
		rows.Close()
		if !strings.Contains(plan, query.index) || strings.Contains(plan, "TEMP B-TREE") {
			t.Fatal("history pagination is not indexed", plan)
		}
	}
}

func TestConcurrentJobIDsRemainIdempotentAndReceiptsAreAtomic(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	// Two connections exercise the primary-key race in addition to the local mutex.
	b, err := openTestAgent(t, a.dir)
	if err != nil {
		t.Fatal(err)
	}
	req := model.PrintRequest{Version: 1, JobID: "concurrent", Data: sampleFor(a, p)}
	var wg sync.WaitGroup
	results := make(chan bool, 12)
	failures := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			agent := a
			if i%2 == 1 {
				agent = b
			}
			_, duplicate, err := agent.Enqueue(req)
			results <- duplicate
			if err != nil {
				failures <- err
			}
		}(i)
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	fresh := 0
	for duplicate := range results {
		if !duplicate {
			fresh++
		}
	}
	if fresh != 1 || countTestJobs(t, a) != 1 {
		t.Fatal("duplicate print jobs created", fresh, countTestJobs(t, a))
	}
	if _, err := a.db.Exec(`CREATE TRIGGER fail_receipt BEFORE INSERT ON job_receipts BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	req.JobID = "rollback"
	_, _, err = a.Enqueue(req)
	requireActionFailure(t, err, model.StorageFailure)
	if countTestJobs(t, a) != 1 {
		t.Fatal("orphan queue entry committed")
	}
	if _, err := a.db.Exec(`DROP TRIGGER fail_receipt`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Enqueue(req); err != nil {
		t.Fatal("retry after rollback failed", err)
	}
}

func TestClaimIsAtomicAndPNGDoesNotOverwriteCompletion(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	for i := 0; i < 8; i++ {
		_, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: fmt.Sprintf("claim-%d", i), Data: sampleFor(a, p)})
		if err != nil {
			t.Fatal(err)
		}
	}
	claimed := make(chan *model.Job, 16)
	failures := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, err := a.store.ClaimNextJob(time.Now())
			if err != nil {
				failures <- err
			}
			if j != nil {
				claimed <- j
			}
		}()
	}
	wg.Wait()
	close(claimed)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for j := range claimed {
		if seen[j.ID] {
			t.Fatal("job claimed twice", j.ID)
		}
		seen[j.ID] = true
		j.Status = "sent"
		j.Message = "done"
		j.UpdatedAt = time.Now().UTC()
		j.CompletedAt = &j.UpdatedAt
		// Concurrent updates target disjoint columns/tables.
		png := &model.ReceiptPNG{Data: []byte("cached-image")}
		var finishErr, pngErr error
		var pair sync.WaitGroup
		pair.Add(2)
		go func() { defer pair.Done(); finishErr = a.store.FinishJob(j) }()
		go func() { defer pair.Done(); pngErr = a.store.SaveJobPNG(j.ID, png) }()
		pair.Wait()
		if finishErr != nil || pngErr != nil {
			t.Fatal(finishErr, pngErr)
		}
		saved := mustJob(t, a, j.ID)
		if saved.Status != "sent" || saved.CompletedAt == nil || !bytes.Equal(saved.PNG.Data, png.Data) {
			t.Fatal("concurrent completion/PNG lost data")
		}
	}
	if len(seen) != 8 {
		t.Fatal("claims missing", len(seen))
	}
	if j, err := a.store.ClaimNextJob(time.Now()); err != nil || j != nil {
		t.Fatal("terminal job requeued", err)
	}
}

func TestWorkerStopsQueueAfterOutcomeWriteFailure(t *testing.T) {
	a := testAgent(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	p := addTestPrinter(t, a, "127.0.0.1", listener.Addr().(*net.TCPAddr).Port)
	received := make(chan []byte, 2)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			raw, _ := io.ReadAll(conn)
			conn.Close()
			received <- raw
		}
	}()
	for _, id := range []string{"first", "second"} {
		if _, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: id, Data: sampleFor(a, p)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.db.Exec(`CREATE TRIGGER fail_outcome BEFORE UPDATE ON jobs WHEN OLD.status='sending' BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go a.Worker(ctx)
	defer func() { cancel(); <-a.workerDone }()
	select {
	case <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("nothing sent")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		page, err := a.store.ListDiagnostics(model.DiagnosticQuery{Component: "database", Level: "error"})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("outcome failure not recorded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-a.workerDone
	if mustJob(t, a, "first").Status != "sending" || mustJob(t, a, "second").Status != "queued" {
		t.Fatal("queue continued after storage failure")
	}
	select {
	case <-received:
		t.Fatal("another receipt was sent")
	default:
	}
	if _, err := a.db.Exec(`DROP TRIGGER fail_outcome`); err != nil {
		t.Fatal(err)
	}
	reopened, err := openTestAgent(t, a.dir)
	if err != nil {
		t.Fatal(err)
	}
	if mustJob(t, reopened, "first").Status != "unknown" || mustJob(t, reopened, "second").Status != "queued" {
		t.Fatal("restart recovery unsafe")
	}
}

func TestClaimFailureLeavesReceiptQueued(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	if _, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "claim-failure", Data: sampleFor(a, p)}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`CREATE TRIGGER fail_claim BEFORE UPDATE ON jobs WHEN NEW.status='sending' BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	if job, err := a.store.ClaimNextJob(time.Now()); err == nil || job != nil {
		t.Fatal("failed claim returned printable job", job, err)
	}
	if mustJob(t, a, "claim-failure").Status != "queued" {
		t.Fatal("failed claim committed")
	}
	var receipts int
	if err := a.db.QueryRow(`SELECT count(*) FROM job_receipts`).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatal(receipts, err)
	}
}
