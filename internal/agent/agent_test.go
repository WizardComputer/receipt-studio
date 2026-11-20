package agent

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"receiptstudio.local/agent/internal/api"
	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/printer"
)

func testAgent(t *testing.T) *Agent {
	t.Helper()
	a, err := openTestAgent(t, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.host = "127.0.0.1:17840"
	a.origin = "http://" + a.host
	return a
}

func addTestPrinter(t *testing.T, a *Agent, host string, port int) model.Printer {
	t.Helper()
	p := model.Printer{ID: "recepcija", Name: "Recepcija", Host: host, Port: port, TemplateID: a.config.Templates[0].ID}
	stored, err := a.SavePrinter(p, true)
	if err != nil {
		t.Fatal(err)
	}
	p = stored
	return p
}

func sampleFor(a *Agent, p model.Printer) json.RawMessage {
	t, _ := a.receiptTemplate(p.TemplateID)
	return t.SampleData
}

func request(a *Agent, method, path string, body any, token, origin string) *httptest.ResponseRecorder {
	return handlerRequest(a, a.handler(), method, path, body, token, origin)
}

func handlerRequest(a *Agent, handler http.Handler, method, path string, body any, token, origin string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, "http://"+a.host+path, reader)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestPublicAuthenticationAndOrigins(t *testing.T) {
	a := testAgent(t)
	for _, token := range []string{"", "wrong-key", "wrong-admin-key"} {
		if got := request(a, "GET", "/v1/health", nil, token, ""); got.Code != 401 {
			t.Fatal(got.Code)
		}
	}
	for _, origin := range []string{"", a.origin, "https://other.example"} {
		if got := request(a, "GET", "/v1/health", nil, a.config.APIKey, origin); got.Code != 200 {
			t.Fatal(got.Code)
		}
	}
	r := httptest.NewRequest("GET", a.origin+"/v1/health", nil)
	r.Header.Set("Authorization", "Bearer "+a.config.APIKey)
	r.Host = "evil.example:17840"
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("Host protection missing")
	}
	got := request(a, "OPTIONS", "/v1/print-jobs", nil, "", "https://app.example.com")
	if got.Code != 204 || got.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatal(got.Code)
	}
}

func TestPrinterPersistsAndRejectsStaleRevision(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "192.168.1.50", 9100)
	p.Name = "Novo ime"
	if _, err := a.SavePrinter(p, false); err != nil {
		t.Fatal(err)
	}
	_, err := a.SavePrinter(p, false)
	requireActionFailure(t, err, model.Conflict)
	restarted, err := openTestAgent(t, a.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.config.Printers) != 1 || restarted.config.Printers[0].Revision != 2 {
		t.Fatal("configuration lost")
	}
	if restarted.config.APIKey != a.config.APIKey {
		t.Fatal("pairing key changed after restart")
	}
}

func TestIdempotencySnapshotAndDataValidation(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "192.168.1.50", 9100)
	req := model.PrintRequest{Version: 1, JobID: "invoice-1", Data: sampleFor(a, p)}
	first := request(a, "POST", "/v1/print-jobs", req, a.config.APIKey, "")
	if first.Code != 202 {
		t.Fatal(first.Body.String())
	}
	second := request(a, "POST", "/v1/print-jobs", req, a.config.APIKey, "")
	if second.Code != 200 {
		t.Fatal(second.Body.String())
	}
	if countTestJobs(t, a) != 1 {
		t.Fatal("duplicate job")
	}
	before := append([]byte{}, mustJob(t, a, req.JobID).Bytes...)
	original := a.config.Templates[0]
	a.config.Templates[0].Template = json.RawMessage(`{}`)
	if !bytes.Equal(before, mustJob(t, a, req.JobID).Bytes) {
		t.Fatal("job snapshot changed")
	}
	req.Data = json.RawMessage(`{"total":"different"}`)
	if got := request(a, "POST", "/v1/print-jobs", req, a.config.APIKey, ""); got.Code != 409 {
		t.Fatal("conflicting job id accepted")
	}
	a.config.Templates[0] = original
	req.JobID = "invoice-2"
	if got := request(a, "POST", "/v1/print-jobs", req, a.config.APIKey, ""); got.Code != 422 {
		t.Fatal("invalid payload queued")
	}
	req.JobID = "../escape"
	req.Data = sampleFor(a, p)
	if got := request(a, "POST", "/v1/print-jobs", req, a.config.APIKey, ""); got.Code != 422 {
		t.Fatal("invalid id accepted")
	}
	if countTestJobs(t, a) != 1 {
		t.Fatal("invalid job persisted")
	}
}

func TestWorkerSendsExactBytesOnceAndRetainsStatus(t *testing.T) {
	for _, language := range []string{"esc-pos", "star-prnt", "star-line"} {
		t.Run(language, func(t *testing.T) { testWorkerSendsExactBytes(t, language) })
	}
}

func testWorkerSendsExactBytes(t *testing.T, language string) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	a := testAgent(t)
	if language != "esc-pos" {
		entry := a.config.Templates[0]
		var content map[string]any
		_ = json.Unmarshal(entry.Template, &content)
		content["language"] = language
		content["codePage"] = 5
		entry.Template, _ = json.Marshal(content)
		if _, err := a.SaveTemplate(entry, false); err != nil {
			t.Fatal(err)
		}
	}
	p := addTestPrinter(t, a, "127.0.0.1", port)
	received := make(chan []byte, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		data, _ := io.ReadAll(conn)
		received <- data
	}()
	job, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "test-tcp", Data: sampleFor(a, p)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go a.Worker(ctx)
	defer func() { cancel(); <-a.workerDone }()
	select {
	case actual := <-received:
		if !bytes.Equal(actual, job.Bytes) {
			t.Fatal("TCP bytes differ")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no print bytes received")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := mustJob(t, a, job.ID).Status
		if status == "sent" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	status := mustJob(t, a, job.ID).Status
	if status != "sent" {
		t.Fatalf("status=%s", status)
	}
	_, duplicate, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: job.ID, Data: sampleFor(a, p)})
	if err != nil || !duplicate {
		t.Fatal("duplicate not recognized")
	}
	restarted, err := openTestAgent(t, a.dir)
	if err != nil {
		t.Fatal(err)
	}
	if mustJob(t, restarted, job.ID).Status != "sent" {
		t.Fatal("status not persisted")
	}
}

func TestRestartMarksInterruptedJobUnknown(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "192.168.1.50", 9100)
	j, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "interrupted", Data: sampleFor(a, p)})
	if err != nil {
		t.Fatal(err)
	}
	j.Status = "sending"
	if err = persistTestJob(t, a, j); err != nil {
		t.Fatal(err)
	}
	_, _, err = a.Enqueue(model.PrintRequest{Version: 1, JobID: "still-queued", Data: sampleFor(a, p)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := openTestAgent(t, a.dir)
	if err != nil {
		t.Fatal(err)
	}
	if mustJob(t, b, j.ID).Status != "unknown" || mustJob(t, b, "still-queued").Status != "queued" {
		t.Fatal("unsafe restart state")
	}
}

func TestFailedDialAndExplicitReprint(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	listener.Close()
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", port)
	j, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "failed-print", Data: sampleFor(a, p)})
	if err != nil {
		t.Fatal(err)
	}
	status, _ := printer.Transmit(context.Background(), *j)
	if status != "failed" {
		t.Fatalf("status %s", status)
	}
	editTestJob(t, a, j.ID, func(j *model.Job) { j.Status = "unknown" })
	if _, err := a.ReprintJob(j.ID); err != nil {
		t.Fatal(err)
	}
	if countTestJobs(t, a) != 2 {
		t.Fatal("copy missing")
	}
	for id, copy := range allTestJobs(t, a) {
		if id != j.ID && (!bytes.Equal(copy.Bytes, j.Bytes) || copy.TemplateVersion != j.TemplateVersion) {
			t.Fatal("reprint differs from snapshot")
		}
	}
}

func TestRejectsUnsafePrinter(t *testing.T) {
	for _, host := range []string{"example.com", "8.8.8.8", "0.0.0.0", "224.0.0.1"} {
		if model.ValidateAddress(host, 9100) == nil {
			t.Fatalf("accepted %s", host)
		}
	}
	a := testAgent(t)
	template, data, _ := a.encoder.Defaults()
	var value map[string]any
	_ = json.Unmarshal(template, &value)
	value["outputTarget"] = "virtual-printer"
	template, _ = json.Marshal(value)
	p := model.ReceiptTemplate{ID: "wrong", Template: template, SampleData: data}
	_, err := a.SaveTemplate(p, true)
	requireActionFailure(t, err, model.InvalidInput)
}

func TestWebAssetsAndPrivateDataNotServed(t *testing.T) {
	a := testAgent(t)
	for _, p := range []string{"/", "/designer", "/src/app.js", "/src/rpe-runtime.js", "/src/vendor/receipt-printer-encoder-4.0.1.js", "/agent-ui/app.js"} {
		if got := request(a, "GET", p, nil, "", ""); got.Code != 404 {
			t.Fatal(p, got.Code)
		}
	}
	for _, p := range []string{"/config.json", "/receipt-studio.sqlite", "/receipt-studio.sqlite-wal", "/main.go", "/go.mod"} {
		if got := request(a, "GET", p, nil, "", ""); got.Code != 404 {
			t.Fatal(p, got.Code)
		}
	}
	var data []byte
	err := a.db.QueryRow(`SELECT value FROM settings WHERE key='api-key'`).Scan(&data)
	if err != nil || string(data) != a.config.APIKey {
		t.Fatal("configuration missing")
	}
}

func TestAPIKeyWorksWithoutOriginRegistration(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "192.168.1.50", 9100)
	for i, origin := range []string{"https://one.example", "https://two.example", "null", ""} {
		req := model.PrintRequest{Version: 1, JobID: "external-" + strconv.Itoa(i), Data: sampleFor(a, p)}
		denied := request(a, "POST", "/v1/print-jobs", req, "wrong-key", origin)
		if denied.Code != 401 {
			t.Fatal(denied.Code)
		}
		accepted := request(a, "POST", "/v1/print-jobs", req, a.config.APIKey, origin)
		if accepted.Code != 202 {
			t.Fatal(accepted.Body.String())
		}
		if origin != "" && accepted.Header().Get("Access-Control-Allow-Origin") != origin {
			t.Fatal("missing CORS response")
		}
	}
}

func TestTemplateLibraryTransferAndAssignments(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "192.168.1.50", 9100)
	original := a.config.Templates[0]
	first, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "before-edit", Data: original.SampleData})
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte{}, first.Bytes...)
	exported, err := a.ExportTemplate(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	exportedJSON := testJSON(t, exported)
	var file map[string]json.RawMessage
	_ = json.Unmarshal(exportedJSON, &file)
	if len(file) != 4 || file["sample_data"] == nil || bytes.Contains(exportedJSON, []byte(a.config.APIKey)) {
		t.Fatal("export leaks configuration")
	}
	b := testAgent(t)
	var imported model.ReceiptTemplate
	for i := 0; i < 2; i++ {
		imported, err = b.ImportTemplate(exportedJSON)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(b.config.Templates) != 3 || b.config.Templates[1].ID == imported.ID {
		t.Fatal("import overwrote template")
	}
	requireActionFailure(t, a.DeleteTemplate(original.ID), model.Conflict)
	second := p
	second.ID = "druga-kasa"
	if _, err := a.SavePrinter(second, true); err != nil {
		t.Fatal(err)
	}
	var layout map[string]any
	_ = json.Unmarshal(original.Template, &layout)
	layout["elements"].([]any)[0].(map[string]any)["text"] = "SHARED TEMPLATE"
	updated := original
	updated.Template, _ = json.Marshal(layout)
	if _, err := a.SaveTemplate(updated, false); err != nil {
		t.Fatal(err)
	}
	_, err = a.SaveTemplate(updated, false)
	requireActionFailure(t, err, model.Conflict)
	for _, id := range []string{p.ID, second.ID} {
		job, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "after-" + id, PrinterID: id, Data: original.SampleData})
		if err != nil {
			t.Fatal(err)
		}
		if job.TemplateVersion != 2 || !bytes.Contains(job.Bytes, []byte("SHARED TEMPLATE")) {
			t.Fatal("shared update not applied")
		}
	}
	if !bytes.Equal(first.Bytes, before) || first.TemplateVersion != 1 {
		t.Fatal("historical job changed")
	}
	// Transfer preserves the design but does not carry another agent's printer assignments.
	copy, err := a.ImportTemplate(exportedJSON)
	if err != nil {
		t.Fatal(err)
	}
	p.TemplateID = copy.ID
	if _, err := a.SavePrinter(p, false); err != nil {
		t.Fatal(err)
	}
	job, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "reassigned", PrinterID: p.ID, Data: original.SampleData})
	if err != nil || !bytes.Equal(job.Bytes, before) {
		t.Fatal("printer did not use selected template", err)
	}
	restarted, err := openTestAgent(t, a.dir)
	if err != nil || restarted.config.Printers[0].TemplateID != copy.ID {
		t.Fatal("assignment not persisted", err)
	}
	if err := b.DeleteTemplate(imported.ID); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{}`, `null`, `{"format":"receipt-studio-template","version":99,"template":{}}`} {
		_, err := b.ImportTemplate(json.RawMessage(raw))
		requireActionFailure(t, err, model.InvalidInput)
	}
	p.TemplateID = "missing"
	_, err = a.SavePrinter(p, false)
	requireActionFailure(t, err, model.InvalidInput)
}

func TestTransferIncludesSavedPreviewData(t *testing.T) {
	a, b := testAgent(t), testAgent(t)
	original := a.config.Templates[0]
	var sample map[string]any
	_ = json.Unmarshal(original.SampleData, &sample)
	sample["shop"] = "Druga kasa"
	sample["items"] = []any{map[string]any{"name": "Čokolada", "qty": "2", "price": "7,50"}}
	sample["total"] = "15,00 KM"
	original.SampleData, _ = json.Marshal(sample)
	if _, err := a.SaveTemplate(original, false); err != nil {
		t.Fatal(err)
	}
	file, err := a.ExportTemplate(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if file.Version != 2 {
		t.Fatal("expected portable bundle v2")
	}
	received, err := b.ImportTemplate(testJSON(t, file))
	if err != nil {
		t.Fatal(err)
	}
	assertEquivalentJSON := func(actual, expected json.RawMessage) {
		t.Helper()
		var x, y bytes.Buffer
		if err := json.Compact(&x, actual); err != nil {
			t.Fatal(err)
		}
		if err := json.Compact(&y, expected); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(x.Bytes(), y.Bytes()) {
			t.Fatalf("data changed: %s != %s", actual, expected)
		}
	}
	assertEquivalentJSON(received.SampleData, original.SampleData)
	before, err := a.encoder.Compile(original.Template, original.SampleData)
	if err != nil {
		t.Fatal(err)
	}
	after, err := b.encoder.Compile(received.Template, received.SampleData)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("imported preview/print differs", err)
	}
	restarted, err := openTestAgent(t, b.dir)
	if err != nil {
		t.Fatal(err)
	}
	persisted, ok := restarted.receiptTemplate(received.ID)
	if !ok {
		t.Fatal("import lost after restart")
	}
	assertEquivalentJSON(persisted.SampleData, original.SampleData)
	for _, bad := range []json.RawMessage{nil, json.RawMessage("null"), json.RawMessage("[]"), json.RawMessage(`{"shop":"missing required fields"}`)} {
		file.SampleData = bad
		count := len(b.config.Templates)
		_, err := b.ImportTemplate(testJSON(t, file))
		requireActionFailure(t, err, model.InvalidInput)
		if len(b.config.Templates) != count {
			t.Fatal("invalid bundle persisted")
		}
	}
	// Explicitly empty sample data is valid; missing samples and old formats are not.
	file.SampleData = json.RawMessage(`{}`)
	received, err = b.ImportTemplate(testJSON(t, file))
	if err != nil {
		t.Fatal(err)
	}
	assertEquivalentJSON(received.SampleData, json.RawMessage(`{}`))
	count := len(b.config.Templates)
	file.Version = 1
	for _, raw := range []json.RawMessage{testJSON(t, file), original.Template} {
		_, err := b.ImportTemplate(raw)
		requireActionFailure(t, err, model.InvalidInput)
	}
	if len(b.config.Templates) != count {
		t.Fatal("unsupported import persisted")
	}
}

func TestGraphicsTemplateTransfersWithoutExternalFiles(t *testing.T) {
	a, b := testAgent(t), testAgent(t)
	raw, err := os.ReadFile("../../testdata/encoder.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name           string
		Template, Data json.RawMessage
		Hex            string
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		if fixture.Name != "graphics" {
			continue
		}
		original := a.config.Templates[0]
		original.Template, original.SampleData = fixture.Template, fixture.Data
		if _, err := a.SaveTemplate(original, false); err != nil {
			t.Fatal(err)
		}
		exported, err := a.ExportTemplate(original.ID)
		if err != nil {
			t.Fatal(err)
		}
		exportedJSON := testJSON(t, exported)
		received, err := b.ImportTemplate(exportedJSON)
		if err != nil {
			t.Fatal(err)
		}
		restarted, err := openTestAgent(t, b.dir)
		if err != nil {
			t.Fatal(err)
		}
		persisted, ok := restarted.receiptTemplate(received.ID)
		if !ok {
			t.Fatal("graphic template was not persisted")
		}
		output, err := restarted.encoder.Compile(persisted.Template, persisted.SampleData)
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(output) != fixture.Hex {
			t.Fatal("QR/logo bytes changed after transfer and restart")
		}
		return
	}
	t.Fatal("graphics fixture missing")
}

func openTestAgent(t *testing.T, dir string) (*Agent, error) {
	a, err := NewAgent(dir)
	if err == nil {
		t.Cleanup(func() { a.db.Close() })
	}
	return a, err
}

func (a *Agent) handler() http.Handler { return api.New(a, a.origin, a.host) }
