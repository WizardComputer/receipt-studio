package agent

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/storage"
)

func TestSQLiteIncompatibleSchemaPreservesAllData(t *testing.T) {
	for _, change := range []struct{ name, sql string }{
		{"changed fingerprint", `UPDATE settings SET value='previous-schema' WHERE key='schema-fingerprint'`},
		{"missing fingerprint", `DELETE FROM settings WHERE key='schema-fingerprint'`},
		{"newer database", `PRAGMA user_version=2`},
		{"unversioned existing database", `PRAGMA user_version=0`},
		{"missing API key", `DELETE FROM settings WHERE key='api-key'`},
	} {
		t.Run(change.name, func(t *testing.T) {
			a := testAgent(t)
			p := addTestPrinter(t, a, "127.0.0.1", 9)
			if _, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "keep-job", Data: sampleFor(a, p), ReturnPNG: true}); err != nil {
				t.Fatal(err)
			}
			if err := a.SavePreferences(model.Preferences{Language: "en"}); err != nil {
				t.Fatal(err)
			}
			if err := a.store.MarkDesktopInitialized(); err != nil {
				t.Fatal(err)
			}
			if _, err := a.db.Exec(change.sql); err != nil {
				t.Fatal(err)
			}
			before := databaseSnapshot(t, a.db)
			if b, err := NewAgent(a.dir); err == nil {
				b.db.Close()
				t.Fatal("opened unsupported schema")
			}
			after := databaseSnapshot(t, a.db)
			if !bytes.Equal(before, after) {
				t.Fatal("rejected database was modified")
			}
		})
	}
}

// Snapshot schema and every persisted value, including opaque PNG bytes.
func databaseSnapshot(t *testing.T, db *sql.DB) []byte {
	t.Helper()
	read := func(query string) [][]any {
		rows, err := db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		result := [][]any{}
		for rows.Next() {
			values := make([]any, len(cols))
			refs := make([]any, len(cols))
			for i := range values {
				refs[i] = &values[i]
			}
			if err := rows.Scan(refs...); err != nil {
				t.Fatal(err)
			}
			for i, v := range values {
				if b, ok := v.([]byte); ok {
					values[i] = bytes.Clone(b)
				}
			}
			result = append(result, values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return result
	}
	snapshot := map[string]any{"schema": read(`SELECT type,name,tbl_name,sql FROM sqlite_master ORDER BY name`), "version": read(`PRAGMA user_version`)}
	for _, table := range []string{"settings", "printers", "templates", "template_versions", "jobs", "job_receipts", "job_pngs", "diagnostics"} {
		snapshot[table] = read(`SELECT * FROM ` + table + ` ORDER BY rowid`)
	}
	return testJSON(t, snapshot)
}

func TestSQLiteUnrelatedDatabaseIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, storage.DatabaseName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE unrelated(value TEXT); INSERT INTO unrelated VALUES('keep me')`); err != nil {
		t.Fatal(err)
	}
	if a, err := NewAgent(dir); err == nil {
		a.db.Close()
		t.Fatal("opened unrelated database")
	}
	var value string
	if err := db.QueryRow(`SELECT value FROM unrelated`).Scan(&value); err != nil || value != "keep me" {
		t.Fatal("destroyed unrelated database", err)
	}
	var tables int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table'`).Scan(&tables); err != nil || tables != 1 {
		t.Fatal("created agent tables in unrelated database", err)
	}
}

func TestSQLiteSameSchemaKeepsData(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 9)
	if _, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "queued-job", Data: sampleFor(a, p)}); err != nil {
		t.Fatal(err)
	}
	if _, err := (storage.NewLog(a.db)).Write([]byte("keep log")); err != nil {
		t.Fatal(err)
	}
	before := testJSON(t, a.ConfigSnapshot())
	if err := a.db.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := openTestAgent(t, a.dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, testJSON(t, b.ConfigSnapshot())) || mustJob(t, b, "queued-job") == nil {
		t.Fatal("unchanged schema reset data")
	}
	var logs int
	if err := b.db.QueryRow(`SELECT count(*) FROM diagnostics`).Scan(&logs); err != nil || logs != 2 {
		t.Fatal(logs, err)
	}
}

func TestSQLiteFreshInstallDoesNotImportLegacyFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "jobs"), 0700); err != nil {
		t.Fatal(err)
	}
	files := []string{"config.json", "jobs/old.json", "log", "desktop-initialized"}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("old data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := openTestAgent(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	assertNoTemplateSource(t, a)
	if len(a.config.APIKey) < 32 || len(a.config.Templates) != 1 || len(a.config.Printers) != 0 || countTestJobs(t, a) != 0 || a.store.DesktopInitialized() {
		t.Fatal("unexpected initial state")
	}
	var tables int
	if err := a.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table'`).Scan(&tables); err != nil || tables != 8 {
		t.Fatal(tables, err)
	}
	for _, name := range files {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(raw) != "old data" {
			t.Fatal("old file modified", name, err)
		}
	}
	b, err := openTestAgent(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	if b.config.APIKey != a.config.APIKey {
		t.Fatal("restart changed API key")
	}
}

func TestTemplateHistoryRestoreAndAtomicFailure(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 9)
	original := a.config.Templates[0]
	job, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "before-restore", Data: original.SampleData})
	if err != nil {
		t.Fatal(err)
	}
	changed := original
	var design map[string]any
	json.Unmarshal(changed.Template, &design)
	design["name"] = "Novo zaglavlje"
	changed.Template, _ = json.Marshal(design)
	var sample map[string]any
	json.Unmarshal(original.SampleData, &sample)
	sample["shop"] = "Druga radnja"
	changed.SampleData, _ = json.Marshal(sample)
	if _, err := a.SaveTemplate(changed, false); err != nil {
		t.Fatal(err)
	}
	base := "/api/templates/" + original.ID + "/versions"
	for _, path := range []string{base, base + "/1", base + "/1/preview"} {
		if r := request(a, "GET", path, nil, a.config.APIKey, ""); r.Code != 404 {
			t.Fatal("public key accessed history", r.Code)
		}
	}
	if _, err := a.TemplateHistory(original.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := a.TemplateVersion(original.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := a.TemplateVersionPreview(original.ID, 1); err != nil {
		t.Fatal(err)
	}
	_, err = a.RestoreTemplateVersion(original.ID, 1, 1)
	requireActionFailure(t, err, model.Conflict)
	if _, err := a.RestoreTemplateVersion(original.ID, 1, 2); err != nil {
		t.Fatal(err)
	}
	current, _ := a.receiptTemplate(original.ID)
	if current.Version != 3 || model.CompactJSON(current.Template) != model.CompactJSON(original.Template) || model.CompactJSON(current.SampleData) != model.CompactJSON(original.SampleData) {
		t.Fatal("restore did not make exact copy as new version")
	}
	v, err := a.store.Revision(original.ID, 3)
	if err != nil || v.RestoredFrom != 1 {
		t.Fatal("restore provenance missing", err)
	}
	v, err = a.store.Revision(original.ID, 2)
	if err != nil || model.CompactJSON(v.SampleData) != model.CompactJSON(changed.SampleData) {
		t.Fatal("previous version overwritten", err)
	}
	if a.config.Printers[0].TemplateID != p.TemplateID || !bytes.Equal(mustJob(t, a, job.ID).Bytes, job.Bytes) || mustJob(t, a, job.ID).TemplateVersion != 1 {
		t.Fatal("restore altered jobs or assignment")
	}
	newJob, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "after-restore", Data: original.SampleData})
	if err != nil || newJob.TemplateVersion != 3 || !bytes.Equal(newJob.Bytes, job.Bytes) {
		t.Fatal("future print did not use restored version", err)
	}
	// Force a snapshot write failure: current config must roll back with it.
	if _, err := a.db.Exec(`CREATE TRIGGER fail_revision BEFORE INSERT ON template_versions WHEN NEW.version=4 BEGIN SELECT RAISE(ABORT,'test failure'); END;`); err != nil {
		t.Fatal(err)
	}
	_, err = a.RestoreTemplateVersion(original.ID, 2, 3)
	requireActionFailure(t, err, model.StorageFailure)
	b, err := openTestAgent(t, a.dir)
	if err != nil {
		t.Fatal(err)
	}
	current, _ = b.receiptTemplate(original.ID)
	if current.Version != 3 {
		t.Fatal("failed restore changed persisted current version")
	}
	if _, err := b.store.Revision(original.ID, 4); err == nil {
		t.Fatal("failed transaction retained snapshot")
	}
}

func TestTemplateHistoryPagination(t *testing.T) {
	a := testAgent(t)
	for i := 0; i < 54; i++ {
		entry := a.config.Templates[0]
		if _, err := a.SaveTemplate(entry, false); err != nil {
			t.Fatal(err)
		}
	}
	page, err := a.TemplateHistory(a.config.Templates[0].ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Versions) != 50 || page.Versions[0].Version != 55 || page.NextBefore != 6 {
		t.Fatal("bad first page", page)
	}
	page, err = a.TemplateHistory(a.config.Templates[0].ID, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Versions) != 5 || page.Versions[4].Version != 1 {
		t.Fatal("old versions inaccessible", page)
	}
}

func assertNoTemplateSource(t *testing.T, a *Agent) {
	t.Helper()
	var count int
	if err := a.db.QueryRow(`SELECT count(*) FROM pragma_table_info('template_versions') WHERE name='source'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("source column remains", count, err)
	}
	history, err := a.TemplateHistory(a.config.Templates[0].ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(testJSON(t, history), []byte(`"source"`)) {
		t.Fatal("history exposes removed source")
	}
}
