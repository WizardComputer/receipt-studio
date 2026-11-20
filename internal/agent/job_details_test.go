package agent

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"receiptstudio.local/agent/internal/model"
)

func TestJobDetailsAndPreviewUsePrivateOriginalSnapshot(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 9100)
	j, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "details", Data: sampleFor(a, p)})
	if err != nil {
		t.Fatal(err)
	}
	<-a.wake
	for _, path := range []string{"/api/jobs/details", "/api/jobs/details/preview"} {
		if r := request(a, "GET", path, nil, a.config.APIKey, ""); r.Code != 404 {
			t.Fatal("public key accessed private job details")
		}
		if r := request(a, "GET", path, nil, "wrong-admin-key", "https://untrusted.example"); r.Code != 404 {
			t.Fatal("external origin accessed private details")
		}
	}
	a.config.Templates[0].Template = json.RawMessage(`{}`)
	a.config.Printers[0].Host = "192.168.1.99"
	detail, err := a.JobDetails("details")
	if err != nil {
		t.Fatal(err)
	}
	if model.CompactJSON(detail["data"].(json.RawMessage)) != model.CompactJSON(j.Data) || detail["printer"].(model.Printer).Host != "127.0.0.1" || detail["template"].(JobTemplateInfo).Name == "" || detail["can_reprint"] != false {
		t.Fatal("details not based on saved job")
	}
	public := request(a, "GET", "/v1/print-jobs/details", nil, a.config.APIKey, "")
	var summary map[string]any
	_ = json.Unmarshal(public.Body.Bytes(), &summary)
	if _, ok := summary["data"]; ok {
		t.Fatal("payload leaked to public summary")
	}
	png, err := a.JobPreview("details")
	if err != nil {
		t.Fatal(err)
	}
	if mustJob(t, a, j.ID).Status != "queued" || countTestJobs(t, a) != 1 || len(a.wake) != 0 {
		t.Fatal("opening preview affected printing")
	}
	if png.Width != 640 || !bytes.HasPrefix(png.Data, []byte("\x89PNG")) {
		t.Fatal("invalid preview")
	}
	again, err := a.JobPreview("details")
	if err != nil || !bytes.Equal(testJSON(t, png), testJSON(t, again)) {
		t.Fatal("cached preview changed", err)
	}
	_, err = a.JobDetails("missing")
	requireActionFailure(t, err, model.NotFound)
	editTestJob(t, a, j.ID, func(j *model.Job) { j.PNG = nil })
	editTestJob(t, a, j.ID, func(j *model.Job) { j.Bytes = []byte{1} })
	_, err = a.JobPreview("details")
	requireActionFailure(t, err, model.InvalidReceipt)
}

func TestReprintRecordsParentAndResetsAttemptTimes(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 9100)
	j, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "original", Data: sampleFor(a, p)})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	original := mustJob(t, a, j.ID)
	original.Status = "sent"
	original.StartedAt = &now
	original.CompletedAt = &now
	putTestJob(t, a, original)
	summary, err := a.ReprintJob("original")
	if err != nil {
		t.Fatal(err)
	}
	copy := mustJob(t, a, summary["id"].(string))
	if copy.OriginalJobID != j.ID || copy.StartedAt != nil || copy.CompletedAt != nil || copy.Status != "queued" || !bytes.Equal(copy.Bytes, j.Bytes) {
		t.Fatal("reprint snapshot or times incorrect")
	}
}
