package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"testing"

	"receiptstudio.local/agent/internal/model"
)

func TestPNGBandsAcceptLongReceiptsOverHTTP(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	var data map[string]any
	if err := json.Unmarshal(sampleFor(a, p), &data); err != nil {
		t.Fatal(err)
	}
	product := data["items"].([]any)[0]
	for _, count := range []int{400, 500} {
		items := make([]any, count)
		for i := range items {
			items[i] = product
		}
		data["items"] = items
		raw, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		req := model.PrintRequest{Version: 1, JobID: fmt.Sprintf("long-png-%d", count), Data: raw, ReturnPNG: true}
		response := request(a, "POST", "/v1/print-jobs", req, a.config.APIKey, "")
		if response.Code != 200 {
			t.Fatalf("%d items: %d %s", count, response.Code, response.Body.String())
		}
		var body struct{ PNG *model.ReceiptPNG }
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.PNG == nil || body.PNG.Width != 640 || body.PNG.Height < 30*count {
			t.Fatal("long receipt truncated")
		}
		if _, err := png.Decode(bytes.NewReader(body.PNG.Data)); err != nil {
			t.Fatal(err)
		}
		job := mustJob(t, a, req.JobID)
		if job.Status != "queued" || job.PNG == nil || !bytes.Equal(job.PNG.Data, body.PNG.Data) {
			t.Fatal("long receipt not durably queued with its PNG")
		}
	}
}
