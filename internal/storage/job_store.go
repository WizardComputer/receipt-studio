package storage

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"receiptstudio.local/agent/internal/model"
)

const JobTimeFormat = "2006-01-02T15:04:05.000000000Z"

const JobColumns = `id,printer_id,printer_name,template_version,status,message,created_at,updated_at,started_at,completed_at,original_job_id,byte_count`

const MaxJobPageSize = 500

type jobReader interface{ QueryRow(string, ...any) *sql.Row }

func scanJob(row interface{ Scan(...any) error }) (*model.Job, error) {
	j := new(model.Job)
	var created, updated string
	var started, completed sql.NullString
	if err := row.Scan(&j.ID, &j.PrinterID, &j.PrinterName, &j.TemplateVersion, &j.Status, &j.Message, &created, &updated, &started, &completed, &j.OriginalJobID, &j.ByteCount); err != nil {
		return nil, err
	}
	var err error
	if j.CreatedAt, err = time.Parse(JobTimeFormat, created); err != nil {
		return nil, err
	}
	if j.UpdatedAt, err = time.Parse(JobTimeFormat, updated); err != nil {
		return nil, err
	}
	if started.Valid {
		v, e := time.Parse(JobTimeFormat, started.String)
		if e != nil {
			return nil, e
		}
		j.StartedAt = &v
	}
	if completed.Valid {
		v, e := time.Parse(JobTimeFormat, completed.String)
		if e != nil {
			return nil, e
		}
		j.CompletedAt = &v
	}
	return j, nil
}

func JobReadFailure(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return model.ActionFailure(model.NotFound, "Posao nije pronađen.")
	}
	return model.ActionFailure(model.StorageFailure, "Posao nije dostupan.")
}

func nullableJobTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(JobTimeFormat)
}

func (a *Store) JobStatus(id string) (*model.Job, error) {
	return scanJob(a.db.QueryRow(`SELECT `+JobColumns+` FROM jobs WHERE id=?`, id))
}

func loadJobReceipt(db jobReader, j *model.Job) error {
	return db.QueryRow(`SELECT connection,queue,host,port,template,data,commands FROM job_receipts WHERE job_id=?`, j.ID).Scan(&j.Kind, &j.Queue, &j.Host, &j.Port, &j.Template, &j.Data, &j.Bytes)
}

func (a *Store) LoadJob(id string) (*model.Job, error) {
	j, err := a.JobStatus(id)
	if err != nil {
		return nil, err
	}
	if err := loadJobReceipt(a.db, j); err != nil {
		return nil, fmt.Errorf("receipt snapshot unavailable: %v", err)
	}
	return j, nil
}

// The queue entry and immutable receipt commit together. Inserts never overwrite
// an existing ID; only status and cached PNG have separate update operations.
func WriteJob(db sqlExecutor, j *model.Job) error {
	_, err := db.Exec(`INSERT INTO jobs (`+JobColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, j.ID, j.PrinterID, j.PrinterName, j.TemplateVersion, j.Status, j.Message, j.CreatedAt.UTC().Format(JobTimeFormat), j.UpdatedAt.UTC().Format(JobTimeFormat), nullableJobTime(j.StartedAt), nullableJobTime(j.CompletedAt), j.OriginalJobID, j.ByteCount)
	if err != nil {
		return err
	}
	nonNil := func(b []byte) []byte {
		if b == nil {
			return []byte{}
		}
		return b
	}
	_, err = db.Exec(`INSERT INTO job_receipts(job_id,connection,queue,host,port,template,data,commands) VALUES(?,?,?,?,?,?,?,?)`, j.ID, j.Kind, j.Queue, j.Host, j.Port, nonNil(j.Template), nonNil(j.Data), nonNil(j.Bytes))
	if err != nil {
		return err
	}
	if j.PNG != nil {
		return writeJobPNG(db, j.ID, j.PNG)
	}
	return nil
}

func (a *Store) InsertJob(j *model.Job) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := WriteJob(tx, j); err != nil {
		return err
	}
	return tx.Commit()
}

// Claim and read a single receipt in one transaction, before any transport I/O.
func (a *Store) ClaimNextJob(now time.Time) (*model.Job, error) {
	tx, err := a.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	at := now.UTC().Format(JobTimeFormat)
	j, err := scanJob(tx.QueryRow(`UPDATE jobs SET status='sending',message=?,updated_at=?,started_at=? WHERE id=(SELECT id FROM jobs WHERE status='queued' ORDER BY created_at,id LIMIT 1) AND status='queued' RETURNING `+JobColumns, "Slanje printeru…", at, at))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := tx.QueryRow(`SELECT connection,queue,host,port,commands FROM job_receipts WHERE job_id=?`, j.ID).Scan(&j.Kind, &j.Queue, &j.Host, &j.Port, &j.Bytes); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return j, nil
}

func (a *Store) FinishJob(j *model.Job) error {
	result, err := a.db.Exec(`UPDATE jobs SET status=?,message=?,updated_at=?,completed_at=? WHERE id=? AND status='sending'`, j.Status, j.Message, j.UpdatedAt.UTC().Format(JobTimeFormat), nullableJobTime(j.CompletedAt), j.ID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("print job is no longer sending")
	}
	return nil
}

func (a *Store) RecoverInterruptedJobs() error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := time.Now().UTC().Format(JobTimeFormat)
	message := "Agent je zaustavljen tokom slanja. Provjeri printer prije ponovnog printanja."
	// INSERT SELECT avoids materializing receipt data or all interrupted IDs in Go.
	if _, err := tx.Exec(`INSERT INTO diagnostics(created_at,level,component,message,job_id) SELECT ?,'warning','printer',?,id FROM jobs WHERE status='sending'`, at, message); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE jobs SET status='unknown',message=?,updated_at=? WHERE status='sending'`, message, at); err != nil {
		return err
	}
	return tx.Commit()
}

type jobCursor struct {
	Time time.Time `json:"time"`
	ID   string    `json:"id"`
}

func (a *Store) ListJobs(query model.JobQuery) (model.JobPage, error) {
	switch query.Status {
	case "", "queued", "sending", "sent", "failed", "unknown":
	default:
		return model.JobPage{}, model.ActionFailure(model.InvalidInput, "Nepoznat status posla.")
	}
	if !query.From.IsZero() && !query.To.IsZero() && !query.From.Before(query.To) {
		return model.JobPage{}, model.ActionFailure(model.InvalidInput, "Početak raspona mora biti prije kraja.")
	}
	limit := query.Limit
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > MaxJobPageSize {
		return model.JobPage{}, model.ActionFailure(model.InvalidInput, "Neispravan broj rezultata.")
	}
	var before jobCursor
	if query.Before != "" {
		raw, err := base64.RawURLEncoding.DecodeString(query.Before)
		if err != nil || json.Unmarshal(raw, &before) != nil || before.Time.IsZero() || !model.ValidID(before.ID) {
			return model.JobPage{}, model.ActionFailure(model.InvalidInput, "Neispravan cursor.")
		}
	}
	clauses := []string{"1=1"}
	args := []any{}
	add := func(clause string, value any) { clauses = append(clauses, clause); args = append(args, value) }
	if query.Status != "" {
		add("status=?", query.Status)
	}
	if !query.From.IsZero() {
		add("created_at>=?", query.From.UTC().Format(JobTimeFormat))
	}
	if !query.To.IsZero() {
		add("created_at<?", query.To.UTC().Format(JobTimeFormat))
	}
	page := model.JobPage{Items: []map[string]any{}, Counts: map[string]int{"sent": 0, "queued": 0, "sending": 0, "failed": 0, "unknown": 0}}
	tx, err := a.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return model.JobPage{}, model.ActionFailure(model.StorageFailure, "Historija poslova nije dostupna.")
	}
	defer tx.Rollback()
	fail := func() (model.JobPage, error) {
		return model.JobPage{}, model.ActionFailure(model.StorageFailure, "Historija poslova nije dostupna.")
	}
	rows, err := tx.Query(`SELECT status,count(*) FROM jobs WHERE `+strings.Join(clauses, " AND ")+` GROUP BY status`, args...)
	if err != nil {
		return fail()
	}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			rows.Close()
			return fail()
		}
		page.Counts[status] = count
		page.Total += count
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fail()
	}
	if query.Before != "" {
		clauses = append(clauses, `(created_at,id)<(?,?)`)
		args = append(args, before.Time.UTC().Format(JobTimeFormat), before.ID)
	}
	args = append(args, limit+1)
	rows, err = tx.Query(`SELECT `+JobColumns+` FROM jobs WHERE `+strings.Join(clauses, " AND ")+` ORDER BY created_at DESC,id DESC LIMIT ?`, args...)
	if err != nil {
		return fail()
	}
	var last *model.Job
	for rows.Next() {
		if len(page.Items) == limit {
			raw, _ := json.Marshal(jobCursor{last.CreatedAt, last.ID})
			page.Cursor = base64.RawURLEncoding.EncodeToString(raw)
			break
		}
		j, err := scanJob(rows)
		if err != nil {
			rows.Close()
			return fail()
		}
		page.Items = append(page.Items, model.JobSummary(j))
		last = j
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fail()
	}
	if err := tx.Commit(); err != nil {
		return fail()
	}
	return page, nil
}
