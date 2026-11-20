//go:build desktop && (darwin || windows)

package desktop

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"receiptstudio.local/agent/internal/agent"
	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/storage"
)

var testDatabases sync.Map

func testAgent(t *testing.T) *agent.Agent {
	t.Helper()
	dir := t.TempDir()
	a, err := agent.NewAgent(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.SetEndpoint("http://127.0.0.1:17840", "127.0.0.1:17840")
	db, err := storage.OpenDatabase(dir)
	if err != nil {
		a.Close()
		t.Fatal(err)
	}
	testDatabases.Store(a, db)
	t.Cleanup(func() { testDatabases.Delete(a); db.Close(); a.Close() })
	return a
}
func testDB(t *testing.T, a *agent.Agent) *sql.DB {
	t.Helper()
	v, ok := testDatabases.Load(a)
	if !ok {
		t.Fatal("missing fixture database")
	}
	return v.(*sql.DB)
}
func addTestPrinter(t *testing.T, a *agent.Agent, host string, port int) model.Printer {
	t.Helper()
	p, err := a.SavePrinter(model.Printer{ID: "recepcija", Name: "Recepcija", Host: host, Port: port, TemplateID: a.ConfigSnapshot().Templates[0].ID}, true)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func sampleFor(a *agent.Agent, p model.Printer) json.RawMessage {
	for _, t := range a.ConfigSnapshot().Templates {
		if t.ID == p.TemplateID {
			return t.SampleData
		}
	}
	return nil
}
func countTestJobs(t *testing.T, a *agent.Agent) int {
	t.Helper()
	p, e := a.ListJobs(model.JobQuery{Limit: 1})
	if e != nil {
		t.Fatal(e)
	}
	return p.Total
}
func mustJob(t *testing.T, a *agent.Agent, id string) *model.Job {
	t.Helper()
	j, e := a.JobStatus(id)
	if e != nil {
		t.Fatal(e)
	}
	return j
}
func editTestJob(t *testing.T, a *agent.Agent, id string, edit func(*model.Job)) {
	t.Helper()
	j := mustJob(t, a, id)
	edit(j)
	_, e := testDB(t, a).Exec("UPDATE jobs SET status=?,message=? WHERE id=?", j.Status, j.Message, id)
	if e != nil {
		t.Fatal(e)
	}
}
func insertDiagnostic(t *testing.T, a *agent.Agent, at time.Time, level, component, message, jobID string) int64 {
	t.Helper()
	res, e := testDB(t, a).Exec("INSERT INTO diagnostics(created_at,level,component,message,job_id) VALUES(?,?,?,?,?)", at.UTC().Format(storage.DiagnosticTimeFormat), level, component, message, jobID)
	if e != nil {
		t.Fatal(e)
	}
	id, e := res.LastInsertId()
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func lastDiagnostic(t *testing.T, a *agent.Agent, level, component, message string) model.Diagnostic {
	t.Helper()
	p, e := a.ListDiagnostics(model.DiagnosticQuery{Limit: 1})
	if e != nil || len(p.Items) != 1 {
		t.Fatalf("missing diagnostic: %+v %v", p, e)
	}
	d := p.Items[0]
	if d.Level != level || d.Component != component || !strings.Contains(d.Message, message) {
		t.Fatalf("unexpected diagnostic: %+v", d)
	}
	return d
}
func readNativeTestConfig(t *testing.T, path string) model.Config {
	t.Helper()
	absolute, e := filepath.Abs(path)
	if e != nil {
		t.Fatal(e)
	}
	uri := url.URL{Scheme: "file", Path: absolute, RawQuery: "mode=ro"}
	db, e := sql.Open("sqlite", uri.String())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	c, e := storage.ReadConfig(db)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
