package agent

import (
	"bytes"
	"encoding/json"
	"image/png"
	"testing"

	"receiptstudio.local/agent/internal/model"
)

func TestReturnPNGIsOptionalAndIdempotent(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 9100)
	req := model.PrintRequest{Version: 1, JobID: "with-image", PrinterID: p.ID, Data: sampleFor(a, p), ReturnPNG: true}
	if unauth := request(a, "POST", "/v1/print-jobs", req, "", ""); unauth.Code != 401 {
		t.Fatal("PNG bypassed authentication")
	}
	response := request(a, "POST", "/v1/print-jobs", req, a.config.APIKey, "")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var body struct {
		Status    string
		Duplicate bool
		PNG       *model.ReceiptPNG
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "queued" || body.Duplicate || body.PNG == nil {
		t.Fatal("wrong PNG/job response")
	}
	artifact := body.PNG
	decoded, err := png.Decode(bytes.NewReader(artifact.Data))
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Encoding != "base64" || artifact.ContentType != "image/png" || artifact.Width != 640 || artifact.Scale != 1 || artifact.DPI != 203.2 || decoded.Bounds().Dx() != artifact.Width || decoded.Bounds().Dy() != artifact.Height {
		t.Fatal("bad PNG metadata")
	}
	originalBytes := append([]byte{}, mustJob(t, a, req.JobID).Bytes...)
	// Changing the current template must not change an existing job's PNG.
	a.config.Templates[0].Template = json.RawMessage(`{"version":1,"profile":"narrow"}`)
	response = request(a, "POST", "/v1/print-jobs", req, a.config.APIKey, "")
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	_ = json.Unmarshal(response.Body.Bytes(), &body)
	if !body.Duplicate || !bytes.Equal(artifact.Data, body.PNG.Data) || countTestJobs(t, a) != 1 || !bytes.Equal(originalBytes, mustJob(t, a, req.JobID).Bytes) {
		t.Fatal("retry changed PNG or queued a second print")
	}
	req.ReturnPNG = false
	response = request(a, "POST", "/v1/print-jobs", req, a.config.APIKey, "")
	var summary map[string]json.RawMessage
	_ = json.Unmarshal(response.Body.Bytes(), &summary)
	if _, ok := summary["png"]; ok {
		t.Fatal("PNG returned without opt-in")
	}
	req.ReturnPNG = true
	req.Data = json.RawMessage(`{"other":42}`)
	if Conflict := request(a, "POST", "/v1/print-jobs", req, a.config.APIKey, ""); Conflict.Code != 409 {
		t.Fatal("PNG bypassed dedup conflict")
	}
	restarted, err := openTestAgent(t, a.dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mustJob(t, restarted, req.JobID).PNG.Data, artifact.Data) {
		t.Fatal("PNG not durable")
	}
}

func TestPNGCanBeRequestedLaterUsingOriginalBytes(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 9100)
	req := model.PrintRequest{Version: 1, JobID: "later-image", Data: sampleFor(a, p)}
	response := request(a, "POST", "/v1/print-jobs", req, a.config.APIKey, "")
	if response.Code != 202 || mustJob(t, a, req.JobID).PNG != nil {
		t.Fatal("default behavior changed")
	}
	expected, err := a.encoder.RenderPNG(mustJob(t, a, req.JobID).Template, mustJob(t, a, req.JobID).Bytes)
	if err != nil {
		t.Fatal(err)
	}
	a.config.Templates[0].Template = json.RawMessage(`{}`)
	req.ReturnPNG = true
	response = request(a, "POST", "/v1/print-jobs", req, a.config.APIKey, "")
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var body struct {
		Duplicate bool
		PNG       *model.ReceiptPNG
	}
	_ = json.Unmarshal(response.Body.Bytes(), &body)
	if !body.Duplicate || body.PNG == nil || !bytes.Equal(expected.Data, body.PNG.Data) || countTestJobs(t, a) != 1 {
		t.Fatal("later PNG used changed template or created a new job")
	}
}
