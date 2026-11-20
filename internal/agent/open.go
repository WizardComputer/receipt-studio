package agent

import (
	"database/sql"
	"errors"
	"fmt"

	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/receipt"
	"receiptstudio.local/agent/internal/storage"
)

func NewAgent(dir string) (*Agent, error) {
	db, err := storage.OpenDatabase(dir)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			db.Close()
		}
	}()
	config, err := storage.ReadConfig(db)
	enc, encoderErr := receipt.NewEncoder()
	if encoderErr != nil {
		return nil, encoderErr
	}
	a := &Agent{db: db, store: storage.New(db), dir: dir, encoder: enc, wake: make(chan struct{}, 1), workerDone: make(chan struct{})}
	if errors.Is(err, sql.ErrNoRows) {
		// Only a freshly initialized, empty schema may create initial settings.
		// A missing API key in an existing database is damage, not a fresh install.
		var records int
		if err := a.store.RecordCount(&records); err != nil {
			return nil, err
		}
		if records != 0 {
			return nil, fmt.Errorf("Postojećoj bazi nedostaje API ključ. Podaci nisu promijenjeni; potrebno je vratiti ispravnu bazu iz backupa.")
		}
		template, sample, err := enc.Defaults()
		if err != nil {
			return nil, err
		}
		a.config = model.Config{APIKey: model.Secret(), Printers: []model.Printer{}, Templates: []model.ReceiptTemplate{{ID: "receipt-default", Version: 1, Template: template, SampleData: sample}}}
		if err = a.store.PersistConfig(a.config); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		a.config = config
	}
	if err := a.store.RecoverInterruptedJobs(); err != nil {
		return nil, err
	}
	ok = true
	return a, nil
}
