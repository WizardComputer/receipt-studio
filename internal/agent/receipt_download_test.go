package agent

import (
	"bytes"
	"encoding/json"
	"image/png"
	"net/http"
	"strconv"
	"testing"

	"receiptstudio.local/agent/internal/model"
)

func TestReceiptDownloadUsesAuthAndOriginalArtifactWithoutPrinting(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	j, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "invoice-9007199254740993", Data: sampleFor(a, p)})
	if err != nil {
		t.Fatal(err)
	}
	<-a.wake
	path := "/v1/print-jobs/" + j.ID + "/receipt"
	for _, key := range []string{"", "wrong-key"} {
		r := request(a, "GET", path, nil, key, "")
		if r.Code != 401 || r.Header().Get("Content-Type") != "application/json" {
			t.Fatal("unauthorized download", r.Code)
		}
	}
	var count int
	if err := a.db.QueryRow(`SELECT count(*) FROM job_pngs`).Scan(&count); err != nil || count != 0 {
		t.Fatal("auth failure generated image", err)
	}
	r := request(a, "GET", path, nil, a.config.APIKey, "https://app.example")
	if r.Code != 200 || r.Header().Get("Content-Type") != "image/png" || r.Header().Get("Content-Length") != strconv.Itoa(r.Body.Len()) {
		t.Fatal("invalid binary response", r.Code, r.Header())
	}
	if r.Header().Get("Access-Control-Allow-Origin") != "https://app.example" || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing integration headers")
	}
	if r.Header().Get("Content-Disposition") != "attachment; filename="+j.ID+".png" {
		t.Fatal("missing download filename", r.Header())
	}
	if _, err := png.Decode(bytes.NewReader(r.Body.Bytes())); err != nil {
		t.Fatal("body is not PNG", err)
	}
	expected, err := a.JobPreview(j.ID)
	if err != nil || !bytes.Equal(r.Body.Bytes(), expected.Data) {
		t.Fatal("different image", err)
	}
	// A cached artifact survives changes to the current design and even commands
	// unsupported by the current renderer. GET never creates another print job.
	if _, err := a.db.Exec(`UPDATE job_receipts SET commands=? WHERE job_id=?`, []byte{1}, j.ID); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"queued", "sending", "sent", "failed", "unknown"} {
		if _, err := a.db.Exec(`UPDATE jobs SET status=? WHERE id=?`, status, j.ID); err != nil {
			t.Fatal(err)
		}
		for _, method := range []string{"GET", "HEAD"} {
			again := request(a, method, path, nil, a.config.APIKey, "")
			if again.Code != 200 {
				t.Fatal(status, method, again.Code)
			}
			if method == "GET" && !bytes.Equal(again.Body.Bytes(), expected.Data) {
				t.Fatal("cached image changed")
			}
			if method == "HEAD" && again.Body.Len() != 0 {
				t.Fatal("HEAD returned body")
			}
		}
		stored, err := a.store.JobStatus(j.ID)
		if err != nil || stored.Status != status || countTestJobs(t, a) != 1 || len(a.wake) != 0 {
			t.Fatal("download altered print queue", err)
		}
	}
}

func TestReceiptDownloadErrorsAreJSON(t *testing.T) {
	a := testAgent(t)
	check := func(id string, status int) {
		t.Helper()
		r := request(a, "GET", "/v1/print-jobs/"+id+"/receipt", nil, a.config.APIKey, "")
		var body map[string]string
		if r.Code != status || r.Header().Get("Content-Type") != "application/json" || json.Unmarshal(r.Body.Bytes(), &body) != nil || body["error"] == "" {
			t.Fatalf("expected %d JSON error, got %d: %s", status, r.Code, r.Body.String())
		}
	}
	check("missing", http.StatusNotFound)
	check("invalid.id", http.StatusNotFound)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	j, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "unsupported", Data: sampleFor(a, p)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE job_receipts SET commands=? WHERE job_id=?`, []byte{1}, j.ID); err != nil {
		t.Fatal(err)
	}
	check(j.ID, http.StatusUnprocessableEntity)
	if _, err := a.db.Exec(`DROP TABLE job_pngs`); err != nil {
		t.Fatal(err)
	}
	check(j.ID, http.StatusInternalServerError)
	if stored, err := a.store.JobStatus(j.ID); err != nil || stored.Status != "queued" || countTestJobs(t, a) != 1 {
		t.Fatal("download failure altered job", err)
	}
}
