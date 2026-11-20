package agent

import (
	"receiptstudio.local/agent/internal/model"
)

func (a *Agent) SavePreferences(p model.Preferences) (err error) {
	defer func() { a.RecordActionFailure("agent", "Jezik nije sačuvan.", err) }()
	if p.Language != "bs" && p.Language != "en" {
		return model.ActionFailure(model.InvalidInput, "Odaberi bosanski ili engleski jezik.")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	before, err := a.store.Preferences()
	if err != nil {
		return model.ActionFailure(model.StorageFailure, "Jezik nije sačuvan.")
	}
	if before.Language == p.Language {
		return nil
	}
	if err := a.store.SaveLanguage(p.Language); err != nil {
		return model.ActionFailure(model.StorageFailure, "Jezik nije sačuvan.")
	}
	message := "Jezik interfejsa je promijenjen na bosanski."
	if p.Language == "en" {
		message = "Jezik interfejsa je promijenjen na engleski."
	}
	a.RecordDiagnostic("info", "agent", message, "")
	return nil
}
