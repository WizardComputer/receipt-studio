package storage

import (
	"receiptstudio.local/agent/internal/model"
)

// PNG bytes remain binary in SQLite. Base64 exists only in the API's JSON
// representation of ReceiptPNG; neither the sender nor details loads this row.

const jobPNGColumns = `data,width,height,dpi,paper_width_mm,margin_left,margin_right,scale`

func (a *Store) LoadJobPNG(id string) (*model.ReceiptPNG, error) {
	p := &model.ReceiptPNG{ContentType: "image/png", Encoding: "base64"}
	err := a.db.QueryRow(`SELECT `+jobPNGColumns+` FROM job_pngs WHERE job_id=?`, id).Scan(&p.Data, &p.Width, &p.Height, &p.DPI, &p.PaperWidthMM, &p.MarginLeft, &p.MarginRight, &p.Scale)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func writeJobPNG(db sqlExecutor, id string, p *model.ReceiptPNG) error {
	_, err := db.Exec(`INSERT INTO job_pngs(job_id,`+jobPNGColumns+`) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(job_id) DO NOTHING`, id, p.Data, p.Width, p.Height, p.DPI, p.PaperWidthMM, p.MarginLeft, p.MarginRight, p.Scale)
	return err
}

func (a *Store) SaveJobPNG(id string, p *model.ReceiptPNG) error {
	return writeJobPNG(a.db, id, p)
}

// Preserve an existing preview on reprint without loading its bytes into Go.
// If the original has no cached image, the copy can generate one on demand.
func (a *Store) InsertReprint(j *model.Job, originalID string) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := WriteJob(tx, j); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO job_pngs(job_id,`+jobPNGColumns+`) SELECT ?,`+jobPNGColumns+` FROM job_pngs WHERE job_id=?`, j.ID, originalID); err != nil {
		return err
	}
	return tx.Commit()
}
