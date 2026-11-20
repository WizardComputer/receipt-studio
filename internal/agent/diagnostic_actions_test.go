package agent

import (
	"strings"
	"testing"

	"receiptstudio.local/agent/internal/model"
)

func lastDiagnostic(t *testing.T, a *Agent, level, component, message string) model.Diagnostic {
	t.Helper()
	page, err := a.store.ListDiagnostics(model.DiagnosticQuery{Limit: 1})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("missing diagnostic: %+v, %v", page, err)
	}
	entry := page.Items[0]
	if entry.Level != level || entry.Component != component || !strings.Contains(entry.Message, message) {
		t.Fatalf("got %+v; want %s/%s containing %q", entry, level, component, message)
	}
	return entry
}

func TestLanguageChangeRecordsDiagnostic(t *testing.T) {
	a := testAgent(t)
	for _, language := range []string{"en", "bs"} {
		if err := a.SavePreferences(model.Preferences{Language: language}); err != nil {
			t.Fatal(err)
		}
		entry := lastDiagnostic(t, a, "info", "agent", "Jezik interfejsa je promijenjen")
		if err := a.SavePreferences(model.Preferences{Language: language}); err != nil {
			t.Fatal(err)
		}
		if got := lastDiagnostic(t, a, "info", "agent", "Jezik interfejsa je promijenjen"); got.ID != entry.ID {
			t.Fatal("unchanged language logged")
		}
	}
	page, err := a.store.ListDiagnostics(model.DiagnosticQuery{Component: "agent"})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("language changes: got %d entries, want 2: %v", len(page.Items), err)
	}
	requireActionFailure(t, a.SavePreferences(model.Preferences{Language: "de"}), model.InvalidInput)
	lastDiagnostic(t, a, "warning", "agent", "Jezik nije sačuvan.")
	if _, err := a.db.Exec(`CREATE TRIGGER fail_language BEFORE INSERT ON settings WHEN NEW.key='language' BEGIN SELECT RAISE(ABORT,'secret-error-value'); END`); err != nil {
		t.Fatal(err)
	}
	requireActionFailure(t, a.SavePreferences(model.Preferences{Language: "en"}), model.StorageFailure)
	entry := lastDiagnostic(t, a, "error", "database", "Jezik nije sačuvan.")
	if strings.Contains(entry.Message, "secret-error-value") {
		t.Fatal("raw database error leaked")
	}
	pref, err := a.store.Preferences()
	if err != nil || pref.Language != "bs" {
		t.Fatal("failed change altered language", pref, err)
	}
}

func TestAdministrationActionsRecordRealOutcomes(t *testing.T) {
	a := testAgent(t)
	oldKey := a.config.APIKey
	key, err := a.RotateAPIKey()
	if err != nil || key == oldKey {
		t.Fatal(err)
	}
	lastDiagnostic(t, a, "info", "agent", "API ključ je promijenjen.")
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	lastDiagnostic(t, a, "info", "printer", "Printer je dodan: "+p.ID)
	p.Name = "PRIVATE-NAME"
	p, err = a.SavePrinter(p, false)
	if err != nil {
		t.Fatal(err)
	}
	lastDiagnostic(t, a, "info", "printer", "Postavke printera su sačuvane: "+p.ID)
	_, err = a.SavePrinter(p, true)
	requireActionFailure(t, err, model.Conflict)
	lastDiagnostic(t, a, "warning", "printer", "Printer nije sačuvan.")
	tmpl := a.ConfigSnapshot().Templates[0]
	tmpl.ID = "new-template"
	tmpl, err = a.SaveTemplate(tmpl, true)
	if err != nil {
		t.Fatal(err)
	}
	lastDiagnostic(t, a, "info", "agent", "Predložak je kreiran: new-template (verzija 1).")
	tmpl, err = a.SaveTemplate(tmpl, false)
	if err != nil {
		t.Fatal(err)
	}
	lastDiagnostic(t, a, "info", "agent", "Predložak je sačuvan: new-template (verzija 2).")
	_, err = a.RestoreTemplateVersion(tmpl.ID, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	lastDiagnostic(t, a, "info", "agent", "vraćena verzija 1 kao nova verzija 3")
	file, err := a.ExportTemplate(tmpl.ID)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := a.ImportTemplate(testJSON(t, file))
	if err != nil {
		t.Fatal(err)
	}
	lastDiagnostic(t, a, "info", "agent", "Predložak je uvezen: "+imported.ID)
	if err := a.DeleteTemplate(imported.ID); err != nil {
		t.Fatal(err)
	}
	lastDiagnostic(t, a, "info", "agent", "Predložak je obrisan: "+imported.ID)
	_, err = a.ImportTemplate([]byte(`{"secret":"PRIVATE-DATA"}`))
	requireActionFailure(t, err, model.InvalidInput)
	lastDiagnostic(t, a, "warning", "agent", "Uvoz predloška nije uspio.")
	result, err := a.PrintTestReceipt(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	entry := lastDiagnostic(t, a, "info", "printer", "Probni račun je dodan u red.")
	if entry.JobID != result["id"] {
		t.Fatal("test receipt not linked")
	}
	_, err = a.ReprintJob(entry.JobID)
	requireActionFailure(t, err, model.Conflict)
	lastDiagnostic(t, a, "warning", "printer", "Ponovno printanje nije dodano u red.")
	// Complete the test fixture without starting a worker or sending to any printer.
	editTestJob(t, a, entry.JobID, func(j *model.Job) { j.Status = "sent" })
	reprint, err := a.ReprintJob(entry.JobID)
	if err != nil {
		t.Fatal(err)
	}
	re := lastDiagnostic(t, a, "info", "printer", "Izvorni posao: "+entry.JobID)
	if re.JobID != reprint["id"] || re.JobID == entry.JobID {
		t.Fatal("reprint not linked to new job")
	}
	if _, err := a.db.Exec(`CREATE TRIGGER fail_settings BEFORE INSERT ON settings BEGIN SELECT RAISE(ABORT,'PRIVATE-DATA'); END`); err != nil {
		t.Fatal(err)
	}
	_, err = a.RotateAPIKey()
	requireActionFailure(t, err, model.StorageFailure)
	lastDiagnostic(t, a, "error", "database", "Ključ nije sačuvan.")
	if a.config.APIKey != key {
		t.Fatal("failed rotation changed key")
	}
	page, err := a.store.ListDiagnostics(model.DiagnosticQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range page.Items {
		for _, private := range []string{oldKey, key, "PRIVATE-NAME", "PRIVATE-DATA", "MALA RADNJA"} {
			if strings.Contains(entry.Message, private) {
				t.Fatalf("private data leaked: %+v", entry)
			}
		}
	}
}

func TestAPIRejectionsDuplicatesAndStorageDiagnostics(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	req := model.PrintRequest{Version: 1, JobID: "logged-job", Data: sampleFor(a, p)}
	cases := []struct {
		body                       any
		token                      string
		status                     int
		level, component, fragment string
	}{
		{req, "WRONG-SECRET", 401, "warning", "api", "HTTP 401"},
		{map[string]any{"unknown": "PRIVATE-DATA"}, a.config.APIKey, 400, "warning", "api", "HTTP 400"},
		{model.PrintRequest{Version: 7, JobID: "bad", Data: req.Data}, a.config.APIKey, 422, "warning", "api", "HTTP 422"},
		{req, a.config.APIKey, 202, "info", "api", "prihvaćen"},
		{req, a.config.APIKey, 200, "info", "api", "postojeći posao"},
		{model.PrintRequest{Version: 1, JobID: req.JobID, Data: []byte(`{"shop":"PRIVATE-DATA"}`)}, a.config.APIKey, 409, "warning", "api", "HTTP 409"},
	}
	for _, c := range cases {
		r := request(a, "POST", "/v1/print-jobs", c.body, c.token, "")
		if r.Code != c.status {
			t.Fatalf("got %d want %d: %s", r.Code, c.status, r.Body.String())
		}
		lastDiagnostic(t, a, c.level, c.component, c.fragment)
	}
	if countTestJobs(t, a) != 1 {
		t.Fatal("rejected or duplicate request created a job")
	}
	if _, err := a.db.Exec(`CREATE TRIGGER fail_job BEFORE INSERT ON jobs BEGIN SELECT RAISE(ABORT,'PRIVATE-DATA'); END`); err != nil {
		t.Fatal(err)
	}
	req.JobID = "storage-failure"
	r := request(a, "POST", "/v1/print-jobs", req, a.config.APIKey, "")
	if r.Code != 500 {
		t.Fatal(r.Code, r.Body.String())
	}
	lastDiagnostic(t, a, "error", "database", "HTTP 500")
	if countTestJobs(t, a) != 1 {
		t.Fatal("unpersisted job queued")
	}
	page, err := a.store.ListDiagnostics(model.DiagnosticQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range page.Items {
		if strings.Contains(entry.Message, "PRIVATE-DATA") || strings.Contains(entry.Message, "WRONG-SECRET") || strings.Contains(entry.Message, a.config.APIKey) {
			t.Fatal("sensitive request logged")
		}
	}
}
