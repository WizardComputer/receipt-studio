package agent

import (
	"database/sql"
	"errors"

	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/storage"
)

func (a *Agent) existingJob(req model.PrintRequest) (*model.Job, bool, error) {
	j, err := a.store.JobStatus(req.JobID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, storage.JobReadFailure(err)
	}
	var data []byte
	if err := a.store.JobData(j.ID, &data); err != nil {
		return nil, false, model.ActionFailure(model.StorageFailure, "Posao nije dostupan.")
	}
	if (req.PrinterID != "" && j.PrinterID != req.PrinterID) || model.CompactJSON(data) != model.CompactJSON(req.Data) {
		return nil, false, model.ErrConflict
	}
	if req.ReturnPNG {
		png, err := a.JobPreview(j.ID)
		if err != nil {
			return nil, false, err
		}
		// Rendering may have overlapped a worker update; return the latest status.
		j, err = a.store.JobStatus(j.ID)
		if err != nil {
			return nil, false, storage.JobReadFailure(err)
		}
		j.PNG = png
	}
	return j, true, nil
}
