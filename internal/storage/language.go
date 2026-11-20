package storage

import (
	"database/sql"
	"errors"

	"receiptstudio.local/agent/internal/model"
)

func (a *Store) Preferences() (model.Preferences, error) {
	p := model.Preferences{Language: "bs"}
	var value string
	err := a.db.QueryRow(`SELECT value FROM settings WHERE key='language'`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if value == "en" {
		p.Language = "en"
	}
	return p, nil
}
