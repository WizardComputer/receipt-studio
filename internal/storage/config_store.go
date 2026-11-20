package storage

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"receiptstudio.local/agent/internal/model"
)

type configReader interface {
	QueryRow(string, ...any) *sql.Row
	Query(string, ...any) (*sql.Rows, error)
}

// Config is a snapshot for the UI/encoder, not a persisted JSON document.
func ReadConfig(db configReader) (model.Config, error) {
	c := model.Config{Printers: []model.Printer{}, Templates: []model.ReceiptTemplate{}}
	if err := db.QueryRow(`SELECT value FROM settings WHERE key='api-key'`).Scan(&c.APIKey); err != nil {
		return c, err
	}
	if len(c.APIKey) < 32 {
		return c, fmt.Errorf("Neispravna konfiguracija u bazi.")
	}
	if err := db.QueryRow(`SELECT value FROM settings WHERE key='default-printer'`).Scan(&c.DefaultPrinter); err != nil {
		return c, fmt.Errorf("postavka podrazumijevanog printera nije dostupna: %v", err)
	}
	rows, err := db.Query(`SELECT p.id,p.name,p.connection,p.queue,p.host,p.port,p.template_id,p.revision FROM printers p ORDER BY p.position,p.id`)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var p model.Printer
		if err := rows.Scan(&p.ID, &p.Name, &p.Kind, &p.Queue, &p.Host, &p.Port, &p.TemplateID, &p.Revision); err != nil {
			rows.Close()
			return c, err
		}
		c.Printers = append(c.Printers, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return c, err
	}
	rows, err = db.Query(`SELECT t.id,t.current_version,v.template,v.sample_data FROM templates t LEFT JOIN template_versions v ON v.template_id=t.id AND v.version=t.current_version WHERE t.archived=0 ORDER BY t.position,t.id`)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var t model.ReceiptTemplate
		if err := rows.Scan(&t.ID, &t.Version, &t.Template, &t.SampleData); err != nil {
			rows.Close()
			return c, err
		}
		c.Templates = append(c.Templates, t)
	}
	err = rows.Err()
	rows.Close()
	return c, err
}

// Persist only changed rows, atomically with their immutable revisions. Reading
// the prior snapshot inside the transaction avoids rewriting unrelated entities.
func writeConfig(tx *sql.Tx, c model.Config, restoredFrom int) error {
	old, err := ReadConfig(tx)
	fresh := errors.Is(err, sql.ErrNoRows)
	if err != nil && !fresh {
		return err
	}
	if len(c.APIKey) < 32 {
		return fmt.Errorf("Neispravan API ključ.")
	}
	for _, setting := range []struct{ key, before, after string }{
		{"api-key", old.APIKey, c.APIKey}, {"default-printer", old.DefaultPrinter, c.DefaultPrinter},
	} {
		if fresh || setting.before != setting.after {
			if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, setting.key, setting.after); err != nil {
				return err
			}
		}
	}
	oldTemplates := map[string]int{}
	for i, t := range old.Templates {
		oldTemplates[t.ID] = i
	}
	active := map[string]bool{}
	for i, t := range c.Templates {
		if active[t.ID] {
			return fmt.Errorf("Dupli ID predloška.")
		}
		active[t.ID] = true
		index, exists := oldTemplates[t.ID]
		if exists {
			prior := old.Templates[index]
			if index == i && prior.Version == t.Version && bytes.Equal(prior.Template, t.Template) && bytes.Equal(prior.SampleData, t.SampleData) {
				continue
			}
		}
		if _, err := tx.Exec(`INSERT INTO templates(id,current_version,position,archived) VALUES(?,?,?,0) ON CONFLICT(id) DO UPDATE SET current_version=excluded.current_version,position=excluded.position,archived=0`, t.ID, t.Version, i); err != nil {
			return err
		}
		if err := writeTemplateRevision(tx, t, restoredFrom); err != nil {
			return err
		}
	}
	oldPrinters := map[string]int{}
	for i, p := range old.Printers {
		oldPrinters[p.ID] = i
	}
	printers := map[string]bool{}
	for i, p := range c.Printers {
		if printers[p.ID] || !active[p.TemplateID] {
			return fmt.Errorf("Neispravan printer ili predložak.")
		}
		printers[p.ID] = true
		if index, exists := oldPrinters[p.ID]; exists && index == i && old.Printers[index] == p {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO printers(id,name,connection,queue,host,port,template_id,revision,position) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,connection=excluded.connection,queue=excluded.queue,host=excluded.host,port=excluded.port,template_id=excluded.template_id,revision=excluded.revision,position=excluded.position`, p.ID, p.Name, p.Kind, p.Queue, p.Host, p.Port, p.TemplateID, p.Revision, i); err != nil {
			return err
		}
	}
	if c.DefaultPrinter != "" && !printers[c.DefaultPrinter] {
		return fmt.Errorf("Podrazumijevani printer ne postoji.")
	}
	for _, p := range old.Printers {
		if !printers[p.ID] {
			if _, err := tx.Exec(`DELETE FROM printers WHERE id=?`, p.ID); err != nil {
				return err
			}
		}
	}
	// Deletion hides a template; its identity and every past revision survive.
	for _, t := range old.Templates {
		if !active[t.ID] {
			if _, err := tx.Exec(`UPDATE templates SET archived=1 WHERE id=?`, t.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeTemplateRevision(tx *sql.Tx, t model.ReceiptTemplate, restoredFrom int) error {
	var template, sample []byte
	err := tx.QueryRow(`SELECT template,sample_data FROM template_versions WHERE template_id=? AND version=?`, t.ID, t.Version).Scan(&template, &sample)
	if err == nil {
		if model.CompactJSON(template) != model.CompactJSON(t.Template) || model.CompactJSON(sample) != model.CompactJSON(t.SampleData) {
			return fmt.Errorf("Verzija %d predloška već postoji s drugim sadržajem.", t.Version)
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.Exec(`INSERT INTO template_versions(template_id,version,template,sample_data,created_at,restored_from) VALUES(?,?,?,?,?,?)`, t.ID, t.Version, []byte(t.Template), []byte(t.SampleData), time.Now().UTC().Format(time.RFC3339Nano), restoredFrom)
	return err
}
