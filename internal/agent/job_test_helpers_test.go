package agent

import (
	"database/sql"
	"errors"
	"testing"

	"receiptstudio.local/agent/internal/model"
	"receiptstudio.local/agent/internal/storage"
)

func mustJob(t *testing.T, a *Agent, id string) *model.Job {
	t.Helper()
	j, err := a.store.LoadJob(id)
	if err != nil {
		t.Fatal(err)
	}
	j.PNG, err = a.store.LoadJobPNG(id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	return j
}

func countTestJobs(t *testing.T, a *Agent) int {
	t.Helper()
	var count int
	if err := a.db.QueryRow(`SELECT count(*) FROM jobs`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func persistTestJob(t *testing.T, a *Agent, j *model.Job) error {
	t.Helper()
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM jobs WHERE id=?`, j.ID); err != nil {
		return err
	}
	if err := storage.WriteJob(tx, j); err != nil {
		return err
	}
	return tx.Commit()
}

func putTestJob(t *testing.T, a *Agent, j *model.Job) {
	t.Helper()
	if err := persistTestJob(t, a, j); err != nil {
		t.Fatal(err)
	}
}

func editTestJob(t *testing.T, a *Agent, id string, edit func(*model.Job)) {
	t.Helper()
	j := mustJob(t, a, id)
	edit(j)
	putTestJob(t, a, j)
}

func allTestJobs(t *testing.T, a *Agent) map[string]*model.Job {
	t.Helper()
	page, err := a.store.ListJobs(model.JobQuery{Limit: storage.MaxJobPageSize})
	if err != nil {
		t.Fatal(err)
	}
	jobs := map[string]*model.Job{}
	for _, row := range page.Items {
		id := row["id"].(string)
		jobs[id] = mustJob(t, a, id)
	}
	return jobs
}
