package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/printer"
	"receiptstudio.local/agent/internal/storage"
)

func (a *Agent) ConfigSnapshot() model.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return model.CloneConfig(a.config)
}

func (a *Agent) RotateAPIKey() (key string, err error) {
	defer func() { a.RecordActionFailure("agent", "Ključ nije sačuvan.", err) }()
	a.mu.Lock()
	defer a.mu.Unlock()
	config := a.config
	config.APIKey = model.Secret()
	if err := a.store.PersistConfig(config); err != nil {
		return "", model.ActionFailure(model.StorageFailure, "Ključ nije sačuvan.")
	}
	a.config = config
	a.RecordDiagnostic("info", "agent", "API ključ je promijenjen.", "")
	return config.APIKey, nil
}

func (a *Agent) SavePrinter(p model.Printer, create bool) (saved model.Printer, err error) {
	defer func() { a.RecordActionFailure("printer", "Printer nije sačuvan.", err) }()
	if !model.ValidID(p.ID) || len(strings.TrimSpace(p.Name)) == 0 || len(p.Name) > 100 {
		return model.Printer{}, model.ActionFailure(model.InvalidInput, "Unesi naziv i važeći ID printera.")
	}
	if err := p.ValidateConnection(); err != nil {
		return model.Printer{}, model.ActionFailure(model.InvalidInput, err.Error())
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.receiptTemplate(p.TemplateID); !ok {
		return model.Printer{}, model.ActionFailure(model.InvalidInput, "Odaberi postojeći predložak.")
	}
	config := a.config
	config.Printers = append([]model.Printer{}, a.config.Printers...)
	found := false
	for i, existing := range config.Printers {
		if existing.ID == p.ID {
			if create {
				return model.Printer{}, model.ActionFailure(model.Conflict, "Printer s tim ID-jem već postoji.")
			}
			if p.Revision != existing.Revision {
				return model.Printer{}, model.ActionFailure(model.Conflict, "Postavke printera su promijenjene. Ponovo otvori postavke.")
			}
			p.Revision = existing.Revision + 1
			config.Printers[i] = p
			found = true
			break
		}
	}
	if !found {
		if !create {
			return model.Printer{}, model.ActionFailure(model.NotFound, "Printer nije pronađen.")
		}
		p.Revision = 1
		config.Printers = append(config.Printers, p)
		if config.DefaultPrinter == "" {
			config.DefaultPrinter = p.ID
		}
	}
	if err := a.store.PersistConfig(config); err != nil {
		return model.Printer{}, model.ActionFailure(model.StorageFailure, "Printer nije sačuvan.")
	}
	a.config = config
	message := "Postavke printera su sačuvane: %s (verzija %d)."
	if create {
		message = "Printer je dodan: %s (verzija %d)."
	}
	a.RecordDiagnostic("info", "printer", fmt.Sprintf(message, p.ID, p.Revision), "")
	return p, nil
}

func (a *Agent) ProbePrinter(ctx context.Context, p model.Printer) (string, error) {
	message, err := printer.ProbePrinter(ctx, p)
	if err != nil {
		a.RecordActionFailure("printer", "Provjera veze s printerom nije uspjela.", err)
	} else {
		a.RecordDiagnostic("info", "printer", message, "")
	}
	return message, err
}

func (a *Agent) PrintTestReceipt(id string) (result map[string]any, err error) {
	defer func() { a.RecordActionFailure("printer", "Probni račun nije dodan u red.", err) }()
	a.mu.Lock()
	p, ok := a.printer(id)
	a.mu.Unlock()
	if !ok {
		return nil, model.ActionFailure(model.NotFound, "Printer nije pronađen.")
	}
	a.mu.Lock()
	t, exists := a.receiptTemplate(p.TemplateID)
	a.mu.Unlock()
	if !exists {
		return nil, model.ActionFailure(model.NotFound, "Predložak nije pronađen.")
	}
	job, _, err := a.Enqueue(model.PrintRequest{Version: 1, JobID: "test-" + model.Secret()[:16], PrinterID: p.ID, Data: t.SampleData})
	if err != nil {
		return nil, err
	}
	a.RecordDiagnostic("info", "printer", "Probni račun je dodan u red.", job.ID)
	return model.JobSummary(job), nil
}

func (a *Agent) ReprintJob(id string) (result map[string]any, err error) {
	defer func() { a.RecordActionFailure("printer", "Ponovno printanje nije dodano u red.", err) }()
	a.mu.Lock()
	defer a.mu.Unlock()
	old, err := a.store.LoadJob(id)
	if err != nil {
		return nil, storage.JobReadFailure(err)
	}
	if old.Status == "queued" || old.Status == "sending" {
		return nil, model.ActionFailure(model.Conflict, "Posao je još u obradi.")
	}
	copy := *old
	copy.ID = "reprint-" + model.Secret()[:16]
	copy.OriginalJobID = old.ID
	copy.StartedAt = nil
	copy.CompletedAt = nil
	copy.Status = "queued"
	copy.Message = "Ponovno printanje iz sačuvanog predloška i podataka."
	copy.CreatedAt = time.Now().UTC()
	copy.UpdatedAt = copy.CreatedAt
	if err := a.store.InsertReprint(&copy, old.ID); err != nil {
		return nil, model.ActionFailure(model.StorageFailure, "Posao nije sačuvan.")
	}
	a.RecordDiagnostic("info", "printer", fmt.Sprintf("Ponovno printanje je dodano u red. Izvorni posao: %s.", old.ID), copy.ID)
	select {
	case a.wake <- struct{}{}:
	default:
	}
	return model.JobSummary(&copy), nil
}
