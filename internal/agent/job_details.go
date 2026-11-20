package agent

import (
	"database/sql"
	"encoding/json"
	"errors"

	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/storage"
)

func (a *Agent) JobDetails(id string) (map[string]any, error) {
	j, err := a.store.JobStatus(id)
	if err != nil {
		return nil, storage.JobReadFailure(err)
	}
	if err := a.store.ReadJobDetails(j); err != nil {
		return nil, storage.JobReadFailure(err)
	}
	details := model.JobSummary(j)
	details["data"] = append(json.RawMessage(nil), j.Data...)
	details["printer"] = model.Printer{Connection: j.Connection, ID: j.PrinterID, Name: j.PrinterName, Host: j.Host, Port: j.Port}
	var template JobTemplateInfo
	_ = json.Unmarshal(j.Template, &template)
	if template.Language == "" {
		template.Language = "esc-pos"
	}
	details["template"] = template
	details["started_at"] = model.CloneTime(j.StartedAt)
	details["completed_at"] = model.CloneTime(j.CompletedAt)
	details["original_job_id"] = j.OriginalJobID
	details["can_reprint"] = j.Status != "queued" && j.Status != "sending"
	return details, nil
}

type JobTemplateInfo struct {
	Name     string `json:"name"`
	Profile  string `json:"profile"`
	Language string `json:"language"`
	Cut      bool   `json:"cut"`
}

func (a *Agent) JobPreview(id string) (*model.ReceiptPNG, error) {
	cached, err := a.store.LoadJobPNG(id)
	if err == nil {
		return cached, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, model.ActionFailure(model.StorageFailure, "Pregled nije dostupan.")
	}
	var snapshot model.Job
	if err := a.store.ReadJobPreview(id, &snapshot); err != nil {
		return nil, storage.JobReadFailure(err)
	}
	// Rendering uses immutable receipt bytes. Updating only the PNG cannot overwrite
	// a worker's concurrent status change and never wakes the print queue.
	preview, err := a.encoder.RenderPNG(snapshot.Template, snapshot.Bytes)
	if err != nil {
		return nil, model.ActionFailure(model.InvalidReceipt, "Pregled nije dostupan: "+err.Error())
	}
	if err := a.store.SaveJobPNG(id, preview); err != nil {
		return nil, model.ActionFailure(model.StorageFailure, "Pregled nije moguće sačuvati.")
	}
	// Concurrent callers all return the first committed artifact.
	cached, err = a.store.LoadJobPNG(id)
	if err != nil {
		return nil, model.ActionFailure(model.StorageFailure, "Pregled nije dostupan.")
	}
	return cached, nil
}
