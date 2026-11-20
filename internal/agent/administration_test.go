package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"receiptstudio.local/agent/internal/model"
)

func TestAdministrationSnapshotsDoNotAliasStoredState(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	before, _ := json.Marshal(a.ConfigSnapshot())
	snapshot := a.ConfigSnapshot()
	snapshot.Printers[0].Name = "Unsaved"
	snapshot.Templates[0].Template[0] = '!'
	snapshot.Templates[0].SampleData[0] = '!'
	after, _ := json.Marshal(a.ConfigSnapshot())
	if !bytes.Equal(before, after) {
		t.Fatal("editing snapshot modified committed state")
	}

	entry := a.ConfigSnapshot().Templates[0]
	saved, err := a.SaveTemplate(entry, false)
	if err != nil {
		t.Fatal(err)
	}
	committed := a.ConfigSnapshot().Templates[0]
	entry.Template[0], saved.Template[0], saved.SampleData[0] = '!', '!', '!'
	actual := a.ConfigSnapshot().Templates[0]
	if !bytes.Equal(committed.Template, actual.Template) || !bytes.Equal(committed.SampleData, actual.SampleData) {
		t.Fatal("save arguments/results share stored buffers")
	}
	_, err = a.SaveTemplate(entry, false)
	if err == nil {
		t.Fatal("invalid direct save bypassed validation")
	}

	result, err := a.PrintTestReceipt(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	id := result["id"].(string)
	job := mustJob(t, a, id)
	started := time.Now().UTC()
	job.StartedAt = &started
	putTestJob(t, a, job)
	details, err := a.JobDetails(id)
	if err != nil {
		t.Fatal(err)
	}
	details["data"].(json.RawMessage)[0] = '!'
	*details["started_at"].(*time.Time) = time.Time{}
	job = mustJob(t, a, id)
	if job.Data[0] != '{' || job.StartedAt.IsZero() {
		t.Fatal("job details expose mutable stored state")
	}
	if _, err := a.ReprintJob(id); err == nil {
		t.Fatal("queued job can be reprinted")
	} else {
		var failure *model.ActionError
		if !errors.As(err, &failure) || failure.Kind != model.Conflict {
			t.Fatal(err)
		}
	}
	preview, err := a.JobPreview(id)
	if err != nil {
		t.Fatal(err)
	}
	preview.Data[0] = 0
	again, err := a.JobPreview(id)
	if err != nil || again.Data[0] != 0x89 {
		t.Fatal("preview buffer modified stored PNG", err)
	}
}

func TestAdministrationKeyRotationRevokesOldPublicKey(t *testing.T) {
	a := testAgent(t)
	old := a.ConfigSnapshot().APIKey
	key, err := a.RotateAPIKey()
	if err != nil || key == old || key == "" {
		t.Fatal("key rotation failed", err)
	}
	if got := request(a, "GET", "/v1/health", nil, old, ""); got.Code != 401 {
		t.Fatal("old key still accepted")
	}
	if got := request(a, "GET", "/v1/health", nil, key, ""); got.Code != 200 {
		t.Fatal("new key rejected")
	}
	reopened, err := openTestAgent(t, a.dir)
	if err != nil || reopened.ConfigSnapshot().APIKey != key {
		t.Fatal("key not durable", err)
	}
}

func requireActionFailure(t *testing.T, err error, kind model.FailureKind) {
	t.Helper()
	var failure *model.ActionError
	if !errors.As(err, &failure) || failure.Kind != kind {
		t.Fatalf("expected failure %d, got %v", kind, err)
	}
}

func testJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
