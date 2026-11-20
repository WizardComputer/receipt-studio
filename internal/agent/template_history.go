package agent

import (
	"database/sql"
	"errors"
	"fmt"

	"receiptstudio.local/agent/internal/model"
)

func (a *Agent) TemplateHistory(id string, before int) (model.RevisionPage, error) {
	a.mu.Lock()
	_, exists := a.receiptTemplate(id)
	a.mu.Unlock()
	if !exists {
		return model.RevisionPage{}, model.ActionFailure(model.NotFound, "Predložak nije pronađen.")
	}
	if before < 0 {
		return model.RevisionPage{}, model.ActionFailure(model.InvalidInput, "Neispravna verzija stranice.")
	}
	if before == 0 {
		before = int(^uint(0) >> 1)
	}

	return a.store.TemplateHistory(id, before)
}

func (a *Agent) TemplateVersion(id string, version int) (model.TemplateRevision, error) {
	if version < 1 {
		return model.TemplateRevision{}, model.ActionFailure(model.InvalidInput, "Neispravna verzija.")
	}
	a.mu.Lock()
	_, exists := a.receiptTemplate(id)
	a.mu.Unlock()
	if !exists {
		return model.TemplateRevision{}, model.ActionFailure(model.NotFound, "Predložak nije pronađen.")
	}
	v, err := a.store.Revision(id, version)
	if errors.Is(err, sql.ErrNoRows) {
		return model.TemplateRevision{}, model.ActionFailure(model.NotFound, "Verzija nije pronađena.")
	}
	if err != nil {
		return model.TemplateRevision{}, model.ActionFailure(model.StorageFailure, "Verzija nije dostupna.")
	}
	return v, nil
}

func (a *Agent) TemplateVersionPreview(id string, version int) (*model.ReceiptPNG, error) {
	v, err := a.TemplateVersion(id, version)
	if err != nil {
		return nil, err
	}
	b, err := a.encoder.Compile(v.Template, v.SampleData)
	if err != nil {
		return nil, model.ActionFailure(model.InvalidReceipt, "Pregled nije dostupan za sačuvane podatke: "+err.Error())
	}
	png, err := a.encoder.RenderPNG(v.Template, b)
	if err != nil {
		return nil, model.ActionFailure(model.InvalidReceipt, "Pregled nije dostupan: "+err.Error())
	}
	return png, nil
}

func (a *Agent) RestoreTemplateVersion(id string, version, expectedVersion int) (saved model.ReceiptTemplate, err error) {
	defer func() { a.RecordActionFailure("agent", "Verzija nije vraćena.", err) }()
	v, err := a.TemplateVersion(id, version)
	if err != nil {
		return model.ReceiptTemplate{}, err
	}
	if err := a.validateReceiptTemplate(v.ReceiptTemplate); err != nil {
		return model.ReceiptTemplate{}, model.ActionFailure(model.InvalidReceipt, err.Error())
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	current, ok := a.receiptTemplate(v.ID)
	if !ok {
		return model.ReceiptTemplate{}, model.ActionFailure(model.NotFound, "Predložak nije pronađen.")
	}
	if current.Version != expectedVersion {
		return model.ReceiptTemplate{}, model.ActionFailure(model.Conflict, "Predložak je promijenjen. Ponovo otvori historiju.")
	}
	if current.Version == v.Version {
		return model.ReceiptTemplate{}, model.ActionFailure(model.Conflict, "Ovo je već trenutna verzija.")
	}
	restored := v.ReceiptTemplate
	restored.Version = current.Version + 1
	c := a.config
	c.Templates = append([]model.ReceiptTemplate{}, c.Templates...)
	for i, t := range c.Templates {
		if t.ID == v.ID {
			c.Templates[i] = restored
		}
	}
	if err := a.store.PersistRevision(c, v.Version); err != nil {
		return model.ReceiptTemplate{}, model.ActionFailure(model.StorageFailure, "Verzija nije vraćena.")
	}
	a.config = c
	a.RecordDiagnostic("info", "agent", fmt.Sprintf("Predložak %s: vraćena verzija %d kao nova verzija %d.", id, version, restored.Version), "")
	return model.CloneTemplate(restored), nil
}
