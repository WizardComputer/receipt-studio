package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"receiptstudio.local/agent/internal/model"
)

const DiagnosticTimeFormat = "2006-01-02T15:04:05.000000000Z"

const DiagnosticRetention = 15 * 24 * time.Hour

func (a *Store) PruneDiagnostics(ctx context.Context, now time.Time) (int64, error) {
	cutoff := now.Add(-DiagnosticRetention).UTC().Format(DiagnosticTimeFormat)
	result, err := a.db.ExecContext(ctx, `DELETE FROM diagnostics WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func WriteDiagnostic(db *sql.DB, level, component, message, jobID string) error {
	_, err := db.Exec(`INSERT INTO diagnostics(created_at,level,component,message,job_id) VALUES(?,?,?,?,?)`, time.Now().UTC().Format(DiagnosticTimeFormat), level, component, strings.TrimSpace(message), jobID)
	return err
}

type databaseLog struct{ db *sql.DB }

// The standard logger is reserved for unexpected agent errors.
func (l databaseLog) Write(p []byte) (int, error) {
	if err := WriteDiagnostic(l.db, "error", "agent", string(p), ""); err != nil {
		return 0, err
	}
	return len(p), nil
}

func scanDiagnostic(row interface{ Scan(...any) error }) (model.Diagnostic, error) {
	var d model.Diagnostic
	var created string
	err := row.Scan(&d.ID, &created, &d.Level, &d.Component, &d.Message, &d.JobID)
	if err == nil {
		d.CreatedAt, err = time.Parse(DiagnosticTimeFormat, created)
	}
	return d, err
}

const diagnosticColumns = `id,created_at,level,component,message,job_id`

func (a *Store) ListDiagnostics(q model.DiagnosticQuery) (model.DiagnosticPage, error) {
	switch q.Level {
	case "", "info", "warning", "error":
	default:
		return model.DiagnosticPage{}, model.ActionFailure(model.InvalidInput, "Nepoznat nivo poruke.")
	}
	switch q.Component {
	case "", "agent", "api", "printer", "database", "updater":
	default:
		return model.DiagnosticPage{}, model.ActionFailure(model.InvalidInput, "Nepoznata komponenta.")
	}
	if !q.From.IsZero() && !q.To.IsZero() && !q.From.Before(q.To) {
		return model.DiagnosticPage{}, model.ActionFailure(model.InvalidInput, "Početak raspona mora biti prije kraja.")
	}
	if q.Limit == 0 {
		q.Limit = 100
	}
	if q.Limit < 1 || q.Limit > 500 || q.Before < 0 || len(q.Search) > 500 {
		return model.DiagnosticPage{}, model.ActionFailure(model.InvalidInput, "Neispravni filteri dijagnostike.")
	}
	clauses := []string{"1=1"}
	args := []any{}
	add := func(clause string, value any) { clauses = append(clauses, clause); args = append(args, value) }
	if q.Level != "" {
		add("level=?", q.Level)
	}
	if q.Component != "" {
		add("component=?", q.Component)
	}
	if !q.From.IsZero() {
		add("created_at>=?", q.From.UTC().Format(DiagnosticTimeFormat))
	}
	if !q.To.IsZero() {
		add("created_at<?", q.To.UTC().Format(DiagnosticTimeFormat))
	}
	if q.Before > 0 {
		add("id<?", q.Before)
	}
	if search := strings.TrimSpace(q.Search); search != "" {
		clauses = append(clauses, `(instr(lower(message),lower(?))>0 OR instr(lower(job_id),lower(?))>0)`)
		args = append(args, search, search)
	}
	args = append(args, q.Limit+1)
	rows, err := a.db.Query(`SELECT `+diagnosticColumns+` FROM diagnostics WHERE `+strings.Join(clauses, " AND ")+` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return model.DiagnosticPage{}, model.ActionFailure(model.StorageFailure, "Dijagnostika nije dostupna.")
	}
	defer rows.Close()
	page := model.DiagnosticPage{Items: []model.Diagnostic{}}
	for rows.Next() {
		d, err := scanDiagnostic(rows)
		if err != nil {
			return model.DiagnosticPage{}, model.ActionFailure(model.StorageFailure, "Dijagnostika nije dostupna.")
		}
		page.Items = append(page.Items, d)
	}
	if rows.Err() != nil {
		return model.DiagnosticPage{}, model.ActionFailure(model.StorageFailure, "Dijagnostika nije dostupna.")
	}
	if len(page.Items) > q.Limit {
		page.Items = page.Items[:q.Limit]
		page.NextBefore = page.Items[len(page.Items)-1].ID
	}
	return page, nil
}

func (a *Store) DiagnosticDetails(id int64) (model.Diagnostic, error) {
	d, err := scanDiagnostic(a.db.QueryRow(`SELECT `+diagnosticColumns+` FROM diagnostics WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Diagnostic{}, model.ActionFailure(model.NotFound, "Zapis nije pronađen.")
	}
	if err != nil {
		return model.Diagnostic{}, model.ActionFailure(model.StorageFailure, "Dijagnostika nije dostupna.")
	}
	return d, nil
}

func NewLog(db *sql.DB) *databaseLog { return &databaseLog{db: db} }
