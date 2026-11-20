package agent

import (
	"bytes"
	"fmt"
	"testing"

	"receiptstudio.local/agent/internal/model"
)

func TestNormalizedConfigWritesOnlyChangedEntities(t *testing.T) {
	a := testAgent(t)
	p := addTestPrinter(t, a, "127.0.0.1", 1)
	other := p
	other.ID = "other"
	other.Name = "Other"
	if _, err := a.SavePrinter(other, true); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`CREATE TEMP TABLE changed_rows(entity TEXT,id TEXT);`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"settings", "printers", "templates", "template_versions"} {
		key := "id"
		if table == "settings" {
			key = "key"
		}
		if table == "template_versions" {
			key = "template_id"
		}
		for _, event := range []string{"INSERT", "UPDATE"} {
			query := fmt.Sprintf(`CREATE TEMP TRIGGER track_%s_%s AFTER %s ON %s BEGIN INSERT INTO changed_rows VALUES('%s',NEW.%s); END`, table, event, event, table, table, key)
			if _, err := a.db.Exec(query); err != nil {
				t.Fatal(err)
			}
		}
	}
	assertWrites := func(expected ...string) {
		t.Helper()
		rows, err := a.db.Query(`SELECT entity||':'||id FROM changed_rows ORDER BY entity,id`)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			got = append(got, s)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(got) != fmt.Sprint(expected) {
			t.Fatalf("writes: %v, expected %v", got, expected)
		}
		if _, err := a.db.Exec(`DELETE FROM changed_rows`); err != nil {
			t.Fatal(err)
		}
	}
	p.Name = "Changed"
	if _, err := a.SavePrinter(p, false); err != nil {
		t.Fatal(err)
	}
	assertWrites("printers:" + p.ID)
	if _, err := a.RotateAPIKey(); err != nil {
		t.Fatal(err)
	}
	assertWrites("settings:api-key")
	template := a.config.Templates[0]
	if _, err := a.SaveTemplate(template, false); err != nil {
		t.Fatal(err)
	}
	assertWrites("template_versions:"+template.ID, "templates:"+template.ID)
	var configRows int
	if err := a.db.QueryRow(`SELECT count(*) FROM settings WHERE key='config'`).Scan(&configRows); err != nil || configRows != 0 {
		t.Fatal("config blob remains", err)
	}
	if _, err := a.db.Exec(`UPDATE printers SET template_id='missing' WHERE id=?`, p.ID); err == nil {
		t.Fatal("printer accepted missing template")
	}
	restarted, err := openTestAgent(t, a.dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(testJSON(t, restarted.ConfigSnapshot()), testJSON(t, a.ConfigSnapshot())) {
		t.Fatal("normalized config changed on restart")
	}
}

func TestNormalizedTemplateDeletionKeepsAllRevisions(t *testing.T) {
	a := testAgent(t)
	id := a.config.Templates[0].ID
	if _, err := a.SaveTemplate(a.config.Templates[0], false); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteTemplate(id); err != nil {
		t.Fatal(err)
	}
	b, err := openTestAgent(t, a.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.config.Templates) != 0 {
		t.Fatal("deleted template reappeared")
	}
	var archived, version, count int
	if err := b.db.QueryRow(`SELECT archived,current_version FROM templates WHERE id=?`, id).Scan(&archived, &version); err != nil || archived != 1 || version != 2 {
		t.Fatal("identity not archived", err)
	}
	if err := b.db.QueryRow(`SELECT count(*) FROM template_versions WHERE template_id=?`, id).Scan(&count); err != nil || count != 2 {
		t.Fatal("history removed", err)
	}
	v, err := b.store.Revision(id, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.SaveTemplate(v.ReceiptTemplate, true)
	requireActionFailure(t, err, model.Conflict)
}

func TestNormalizedConfigAcceptsMoreThanOldMVPLimits(t *testing.T) {
	a := testAgent(t)
	c := a.ConfigSnapshot()
	for i := 1; i < 101; i++ {
		entry := model.CloneTemplate(c.Templates[0])
		entry.ID = fmt.Sprintf("template-%d", i)
		c.Templates = append(c.Templates, entry)
	}
	for i := 0; i < 11; i++ {
		c.Printers = append(c.Printers, model.Printer{ID: fmt.Sprintf("printer-%d", i), Name: "Printer", Host: "127.0.0.1", Port: 1, TemplateID: c.Templates[0].ID, Revision: 1})
	}
	c.DefaultPrinter = c.Printers[0].ID
	if err := a.store.PersistConfig(c); err != nil {
		t.Fatal(err)
	}
	a.config = c
	entry := model.CloneTemplate(c.Templates[0])
	entry.ID = "another-template"
	if _, err := a.SaveTemplate(entry, true); err != nil {
		t.Fatal(err)
	}
	p := c.Printers[0]
	p.ID = "another-printer"
	if _, err := a.SavePrinter(p, true); err != nil {
		t.Fatal(err)
	}
	b, err := openTestAgent(t, a.dir)
	if err != nil || len(b.config.Printers) != 12 || len(b.config.Templates) != 102 {
		t.Fatal("old limits remain", err)
	}
}
