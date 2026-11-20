package storage

import (
	"receiptstudio.local/agent/internal/model"
)

func (a *Store) Revision(id string, version int) (model.TemplateRevision, error) {
	v := model.TemplateRevision{ReceiptTemplate: model.ReceiptTemplate{ID: id, Version: version}}
	var template, sample []byte
	err := a.db.QueryRow(`SELECT template,sample_data,created_at,restored_from FROM template_versions WHERE template_id=? AND version=?`, id, version).Scan(&template, &sample, &v.CreatedAt, &v.RestoredFrom)
	v.Template, v.SampleData = template, sample
	return v, err
}

func (a *Store) TemplateHistory(id string, before int) (model.RevisionPage, error) {
	rows, err := a.db.Query(`SELECT version,created_at,restored_from FROM template_versions WHERE template_id=? AND version<? ORDER BY version DESC LIMIT 51`, id, before)
	if err != nil {
		return model.RevisionPage{}, model.ActionFailure(model.StorageFailure, "Historija nije dostupna.")
	}
	defer rows.Close()
	page := model.RevisionPage{Versions: []model.RevisionSummary{}}
	for rows.Next() {
		var v model.RevisionSummary
		if err = rows.Scan(&v.Version, &v.CreatedAt, &v.RestoredFrom); err != nil {
			return model.RevisionPage{}, model.ActionFailure(model.StorageFailure, "Historija nije dostupna.")
		}
		page.Versions = append(page.Versions, v)
	}
	if rows.Err() != nil {
		return model.RevisionPage{}, model.ActionFailure(model.StorageFailure, "Historija nije dostupna.")
	}
	if len(page.Versions) > 50 {
		page.Versions = page.Versions[:50]
		page.NextBefore = page.Versions[49].Version
	}
	return page, nil
}
