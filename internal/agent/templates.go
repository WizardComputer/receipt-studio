package agent

import (
	"encoding/json"
	"fmt"

	"receiptstudio.local/agent/internal/model"
)

type TemplateFile struct {
	Format     string          `json:"format"`
	Version    int             `json:"version"`
	Template   json.RawMessage `json:"template"`
	SampleData json.RawMessage `json:"sample_data"`
}

func (a *Agent) validateReceiptTemplate(t model.ReceiptTemplate) error {
	raw, err := json.Marshal(t)
	if err != nil || len(raw) > 1<<20 {
		return fmt.Errorf("Predložak je prevelik ili nije čitljiv.")
	}
	if err := a.encoder.Validate(t.Template); err != nil {
		return err
	}
	var content map[string]any
	_ = json.Unmarshal(t.Template, &content)
	if content["outputTarget"] == "virtual-printer" {
		return fmt.Errorf("Predložak mora koristiti izvoz za fizički printer.")
	}
	var sample map[string]any
	if err := json.Unmarshal(t.SampleData, &sample); err != nil || sample == nil {
		return fmt.Errorf("Primjer podataka mora biti JSON objekt.")
	}
	// An empty sample is valid after import. Real print jobs are always compiled and validated.
	if len(sample) > 0 {
		if _, err := a.encoder.Compile(t.Template, t.SampleData); err != nil {
			return fmt.Errorf("Primjer podataka: %w", err)
		}
	}
	return nil
}

func (a *Agent) SaveTemplate(t model.ReceiptTemplate, create bool) (saved model.ReceiptTemplate, err error) {
	defer func() { a.RecordActionFailure("agent", "Predložak nije sačuvan.", err) }()
	t = model.CloneTemplate(t)
	if !model.ValidID(t.ID) {
		return model.ReceiptTemplate{}, model.ActionFailure(model.InvalidInput, "Neispravan ID predloška.")
	}
	if err := a.validateReceiptTemplate(t); err != nil {
		return model.ReceiptTemplate{}, model.ActionFailure(model.InvalidInput, err.Error())
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.config
	c.Templates = append([]model.ReceiptTemplate{}, c.Templates...)
	found := false
	for i, old := range c.Templates {
		if old.ID != t.ID {
			continue
		}
		if create {
			return model.ReceiptTemplate{}, model.ActionFailure(model.Conflict, "Predložak s tim ID-jem već postoji.")
		}
		if old.Version != t.Version {
			return model.ReceiptTemplate{}, model.ActionFailure(model.Conflict, "Predložak je promijenjen. Ponovo otvori editor.")
		}
		t.Version = old.Version + 1
		c.Templates[i] = t
		found = true
		break
	}
	if !found {
		if !create {
			return model.ReceiptTemplate{}, model.ActionFailure(model.NotFound, "Predložak nije pronađen.")
		}
		var count int
		if err := a.store.TemplateRevisionCount(t.ID, &count); err != nil {
			return model.ReceiptTemplate{}, model.ActionFailure(model.StorageFailure, "Historija nije dostupna.")
		}
		if count > 0 {
			return model.ReceiptTemplate{}, model.ActionFailure(model.Conflict, "ID pripada ranije obrisanom predlošku. Koristi novi ID.")
		}
		t.Version = 1
		c.Templates = append(c.Templates, t)
	}
	if err := a.store.PersistConfig(c); err != nil {
		return model.ReceiptTemplate{}, model.ActionFailure(model.StorageFailure, "Predložak nije sačuvan.")
	}
	a.config = c
	message := "Predložak je sačuvan: %s (verzija %d)."
	if create {
		message = "Predložak je kreiran: %s (verzija %d)."
	}
	a.RecordDiagnostic("info", "agent", fmt.Sprintf(message, t.ID, t.Version), "")
	return model.CloneTemplate(t), nil
}

func (a *Agent) ExportTemplate(id string) (TemplateFile, error) {
	a.mu.Lock()
	t, ok := a.receiptTemplate(id)
	a.mu.Unlock()
	if !ok {
		return TemplateFile{}, model.ActionFailure(model.NotFound, "Predložak nije pronađen.")
	}
	// Transfer the design and its saved preview data, never agent credentials or jobs.
	t = model.CloneTemplate(t)
	return TemplateFile{Format: "receipt-studio-template", Version: 2, Template: t.Template, SampleData: t.SampleData}, nil
}

func (a *Agent) ImportTemplate(raw json.RawMessage) (saved model.ReceiptTemplate, err error) {
	defer func() { a.RecordActionFailure("agent", "Uvoz predloška nije uspio.", err) }()
	raw = append(json.RawMessage(nil), raw...)
	if len(raw) > 1<<20 {
		return model.ReceiptTemplate{}, model.ActionFailure(model.InvalidInput, "Predložak je prevelik ili nije čitljiv.")
	}
	var file TemplateFile
	if err := json.Unmarshal(raw, &file); err != nil || file.Format != "receipt-studio-template" || file.Version != 2 {
		return model.ReceiptTemplate{}, model.ActionFailure(model.InvalidInput, "Nepodržan format datoteke predloška.")
	}
	if file.SampleData == nil {
		return model.ReceiptTemplate{}, model.ActionFailure(model.InvalidInput, "Datoteci nedostaje primjer podataka.")
	}
	t := model.ReceiptTemplate{ID: "template-" + model.Secret()[:16], Version: 1, Template: file.Template, SampleData: file.SampleData}
	if err := a.validateReceiptTemplate(t); err != nil {
		return model.ReceiptTemplate{}, model.ActionFailure(model.InvalidInput, err.Error())
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.config
	c.Templates = append(append([]model.ReceiptTemplate{}, c.Templates...), t)
	if err := a.store.PersistConfig(c); err != nil {
		return model.ReceiptTemplate{}, model.ActionFailure(model.StorageFailure, "Predložak nije sačuvan.")
	}
	a.config = c
	a.RecordDiagnostic("info", "agent", fmt.Sprintf("Predložak je uvezen: %s (verzija %d).", t.ID, t.Version), "")
	return model.CloneTemplate(t), nil
}

func (a *Agent) DeleteTemplate(id string) (err error) {
	defer func() { a.RecordActionFailure("agent", "Predložak nije obrisan.", err) }()
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.config.Printers {
		if p.TemplateID == id {
			return model.ActionFailure(model.Conflict, "Predložak koristi printer "+p.Name+". Prvo odaberi drugi predložak.")
		}
	}
	c := a.config
	c.Templates = []model.ReceiptTemplate{}
	found := false
	for _, t := range a.config.Templates {
		if t.ID == id {
			found = true
		} else {
			c.Templates = append(c.Templates, t)
		}
	}
	if !found {
		return model.ActionFailure(model.NotFound, "Predložak nije pronađen.")
	}
	if err := a.store.PersistConfig(c); err != nil {
		return model.ActionFailure(model.StorageFailure, "Predložak nije obrisan.")
	}
	a.config = c
	a.RecordDiagnostic("info", "agent", fmt.Sprintf("Predložak je obrisan: %s.", id), "")
	return nil
}
