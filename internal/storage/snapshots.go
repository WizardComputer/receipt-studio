package storage

import (
	"receiptstudio.local/agent/internal/model"
)

// These projections deliberately omit command or PNG blobs that callers do not need.
func (s *Store) JobData(id string, data *[]byte) error {
	return s.db.QueryRow(`SELECT data FROM job_receipts WHERE job_id=?`, id).Scan(data)
}
func (s *Store) ReadJobDetails(j *model.Job) error {
	return s.db.QueryRow(`SELECT connection,queue,host,port,template,data FROM job_receipts WHERE job_id=?`, j.ID).Scan(&j.Kind, &j.Queue, &j.Host, &j.Port, &j.Template, &j.Data)
}
func (s *Store) ReadJobPreview(id string, j *model.Job) error {
	return s.db.QueryRow(`SELECT template,commands FROM job_receipts WHERE job_id=?`, id).Scan(&j.Template, &j.Bytes)
}
func (s *Store) TemplateRevisionCount(id string, count *int) error {
	return s.db.QueryRow(`SELECT count(*) FROM template_versions WHERE template_id=?`, id).Scan(count)
}
func (s *Store) SaveLanguage(language string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES('language',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, language)
	return err
}
func (s *Store) RecordCount(count *int) error {
	return s.db.QueryRow(`SELECT
 (SELECT count(*) FROM settings WHERE key!='schema-fingerprint') +
 (SELECT count(*) FROM templates) + (SELECT count(*) FROM template_versions) +
 (SELECT count(*) FROM printers) + (SELECT count(*) FROM jobs) +
 (SELECT count(*) FROM job_receipts) + (SELECT count(*) FROM job_pngs) +
 (SELECT count(*) FROM diagnostics)`).Scan(count)
}
