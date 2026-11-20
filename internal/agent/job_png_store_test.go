package agent

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"receiptstudio.local/agent/internal/model"
)

func TestPNGStoredAsBinaryAndReprintPreservesArtifact(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	j, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "binary-png", Data: sampleFor(a, p), ReturnPNG: true})
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	var storageType string
	var size int
	if err := a.db.QueryRow(`SELECT data,typeof(data),length(data) FROM job_pngs WHERE job_id=?`, j.ID).Scan(&raw, &storageType, &size); err != nil {
		t.Fatal(err)
	}
	if storageType != "blob" || size != len(j.PNG.Data) || !bytes.Equal(raw, j.PNG.Data) || !bytes.HasPrefix(raw, []byte("\x89PNG")) {
		t.Fatal("PNG not stored as raw bytes")
	}
	if _, err := a.db.Exec(`UPDATE jobs SET status='sent' WHERE id=?`, j.ID); err != nil {
		t.Fatal(err)
	}
	// The archived preview must survive reprint without re-rendering, even if
	// its command stream cannot be interpreted by a newer renderer.
	if _, err := a.db.Exec(`UPDATE job_receipts SET commands=? WHERE job_id=?`, []byte{1}, j.ID); err != nil {
		t.Fatal(err)
	}
	summary, err := a.ReprintJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := a.JobPreview(summary["id"].(string))
	if err != nil || !bytes.Equal(testJSON(t, preview), testJSON(t, j.PNG)) {
		t.Fatal("reprint lost original PNG", err)
	}
}

func TestDetailsAndSenderNeverLoadPNG(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	j, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "separate-png", Data: sampleFor(a, p), ReturnPNG: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE job_pngs SET width='unreadable' WHERE job_id=?`, j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.JobDetails(j.ID); err != nil {
		t.Fatal("details loaded PNG", err)
	}
	loaded, err := a.store.LoadJob(j.ID)
	if err != nil || loaded.PNG != nil {
		t.Fatal("receipt snapshot loaded PNG", err)
	}
	claimed, err := a.store.ClaimNextJob(time.Now())
	if err != nil || claimed == nil {
		t.Fatal(err)
	}
	if claimed.PNG != nil || claimed.Template != nil || claimed.Data != nil || !bytes.Equal(claimed.Bytes, j.Bytes) {
		t.Fatal("sender loaded unnecessary data")
	}
	if _, err := a.JobPreview(j.ID); err == nil {
		t.Fatal("preview did not read PNG metadata")
	}
}

func TestPNGIsLazyAndWriteFailureIsAtomic(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	j, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "lazy-png", Data: sampleFor(a, p)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.JobDetails(j.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := a.db.QueryRow(`SELECT count(*) FROM job_pngs`).Scan(&count); err != nil || count != 0 {
		t.Fatal("details generated PNG", err)
	}
	png, err := a.JobPreview(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	changed := *png
	changed.Data = []byte("second writer")
	if err := a.store.SaveJobPNG(j.ID, &changed); err != nil {
		t.Fatal(err)
	}
	again, err := a.JobPreview(j.ID)
	if err != nil || !bytes.Equal(again.Data, png.Data) {
		t.Fatal("cached PNG overwritten", err)
	}
	if _, err := a.db.Exec(`CREATE TRIGGER fail_png BEFORE INSERT ON job_pngs BEGIN SELECT RAISE(ABORT,'PNG failure'); END`); err != nil {
		t.Fatal(err)
	}
	_, _, err = a.Enqueue(model.PrintRequest{Version: 1, JobID: "failed-png", Data: sampleFor(a, p), ReturnPNG: true})
	if err == nil || countTestJobs(t, a) != 1 {
		t.Fatal("PNG failure left a queued job", err)
	}
	var orphanCount int
	if err := a.db.QueryRow(`SELECT count(*) FROM job_receipts WHERE job_id='failed-png'`).Scan(&orphanCount); err != nil || orphanCount != 0 {
		t.Fatal("PNG failure left a snapshot", err)
	}
	// The wire representation remains Base64 JSON, unchanged by BLOB storage.
	raw, err := json.Marshal(png)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct{ Data string }
	if err := json.Unmarshal(raw, &wire); err != nil || wire.Data == "" {
		t.Fatal("PNG API representation changed", err)
	}
}
