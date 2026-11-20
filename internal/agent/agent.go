package agent

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/receipt"
	"receiptstudio.local/agent/internal/storage"
)

type Agent struct {
	db         *sql.DB
	store      *storage.Store
	mu         sync.Mutex
	config     model.Config
	dir        string
	origin     string
	host       string
	encoder    *receipt.Encoder
	wake       chan struct{}
	workerDone chan struct{}
}

func (a *Agent) printer(id string) (model.Printer, bool) {
	if id == "" {
		id = a.config.DefaultPrinter
	}
	for _, p := range a.config.Printers {
		if p.ID == id {
			return p, true
		}
	}
	return model.Printer{}, false
}

func (a *Agent) receiptTemplate(id string) (model.ReceiptTemplate, bool) {
	for _, t := range a.config.Templates {
		if t.ID == id {
			return t, true
		}
	}
	return model.ReceiptTemplate{}, false
}

func (a *Agent) Enqueue(req model.PrintRequest) (*model.Job, bool, error) {
	if req.Version != 1 || !model.ValidID(req.JobID) || len(req.Data) == 0 {
		return nil, false, errors.New("Potrebni su version: 1, job_id (slova, brojevi, - ili _) i data objekt.")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if prior, duplicate, err := a.existingJob(req); duplicate || err != nil {
		return prior, duplicate, err
	}
	p, ok := a.printer(req.PrinterID)
	if !ok {
		return nil, false, errors.New("Printer nije podešen.")
	}
	t, ok := a.receiptTemplate(p.TemplateID)
	if !ok {
		return nil, false, errors.New("Predložak printera nije pronađen.")
	}
	bytes, err := a.encoder.Compile(t.Template, req.Data)
	if err != nil {
		return nil, false, fmt.Errorf("Podaci ne odgovaraju predlošku: %w", err)
	}
	now := time.Now().UTC()
	job := &model.Job{Connection: p.Connection, ID: req.JobID, PrinterID: p.ID, PrinterName: p.Name, TemplateVersion: t.Version, Status: "queued", Message: "Čeka slanje printeru.", CreatedAt: now, UpdatedAt: now, ByteCount: len(bytes), Host: p.Host, Port: p.Port, Template: append(json.RawMessage{}, t.Template...), Data: append(json.RawMessage{}, req.Data...), Bytes: bytes}
	if req.ReturnPNG {
		job.PNG, err = a.encoder.RenderPNG(job.Template, job.Bytes)
		if err != nil {
			return nil, false, fmt.Errorf("PNG nije moguće generisati; posao nije prihvaćen: %w", err)
		}
	}
	if err := a.store.InsertJob(job); err != nil {
		// The primary key also protects against concurrent insertions from another connection.
		if prior, duplicate, lookupErr := a.existingJob(req); duplicate || lookupErr != nil {
			return prior, duplicate, lookupErr
		}
		return nil, false, model.ActionFailure(model.StorageFailure, "Posao nije sačuvan.")
	}
	select {
	case a.wake <- struct{}{}:
	default:
	}
	copy := *job
	return &copy, false, nil
}
