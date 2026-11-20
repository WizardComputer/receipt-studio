package storage

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"receiptstudio.local/agent/internal/model"
)

const DatabaseName = "receipt-studio.sqlite"

type sqlExecutor interface {
	Exec(string, ...any) (sql.Result, error)
}

func OpenDatabase(dir string) (*sql.DB, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, DatabaseName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*sql.DB, error) { db.Close(); return nil, err }
	if _, err = db.Exec(`PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON;`); err != nil {
		return fail(err)
	}
	if err := initializeDatabase(db); err != nil {
		return fail(err)
	}
	return db, nil
}

// Baseline schema 1. Never change a shipped baseline in place: future changes
// require ordered, transactional migrations and an increased user_version.
const DatabaseSchemaVersion = 1

const DatabaseSchema = `
CREATE TABLE settings (key TEXT PRIMARY KEY, value BLOB NOT NULL);
CREATE TABLE templates (
 id TEXT PRIMARY KEY, current_version INTEGER NOT NULL CHECK(current_version>0),
 position INTEGER NOT NULL, archived INTEGER NOT NULL DEFAULT 0 CHECK(archived IN (0,1)));
CREATE TABLE printers (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, connection TEXT NOT NULL CHECK(connection IN ('','tcp','usb')),
 queue TEXT NOT NULL, host TEXT NOT NULL, port INTEGER NOT NULL,
 template_id TEXT NOT NULL REFERENCES templates(id), revision INTEGER NOT NULL CHECK(revision>0), position INTEGER NOT NULL);
CREATE INDEX printers_template_id ON printers(template_id);
CREATE TABLE template_versions (
 template_id TEXT NOT NULL REFERENCES templates(id), version INTEGER NOT NULL CHECK(version>0),
 template BLOB NOT NULL, sample_data BLOB NOT NULL, created_at TEXT NOT NULL,
 restored_from INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(template_id, version));
CREATE TABLE jobs (
 id TEXT PRIMARY KEY, printer_id TEXT NOT NULL, printer_name TEXT NOT NULL,
 template_version INTEGER NOT NULL, status TEXT NOT NULL CHECK(status IN ('queued','sending','sent','failed','unknown')),
 message TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 started_at TEXT, completed_at TEXT, original_job_id TEXT NOT NULL DEFAULT '', byte_count INTEGER NOT NULL);
CREATE INDEX jobs_created_id ON jobs(created_at DESC,id DESC);
CREATE INDEX jobs_status_created_id ON jobs(status,created_at DESC,id DESC);
CREATE TABLE job_receipts (
 job_id TEXT PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
 connection TEXT NOT NULL, queue TEXT NOT NULL, host TEXT NOT NULL, port INTEGER NOT NULL,
 template BLOB NOT NULL, data BLOB NOT NULL, commands BLOB NOT NULL);
CREATE TABLE job_pngs (
 job_id TEXT PRIMARY KEY REFERENCES job_receipts(job_id) ON DELETE CASCADE,
 data BLOB NOT NULL, width INTEGER NOT NULL, height INTEGER NOT NULL,
 dpi REAL NOT NULL, paper_width_mm INTEGER NOT NULL,
 margin_left INTEGER NOT NULL, margin_right INTEGER NOT NULL, scale INTEGER NOT NULL);

CREATE TABLE diagnostics (
 id INTEGER PRIMARY KEY, created_at TEXT NOT NULL,
 level TEXT NOT NULL CHECK(level IN ('info','warning','error')),
 component TEXT NOT NULL CHECK(component IN ('agent','api','printer','database','updater')),
 message TEXT NOT NULL, job_id TEXT NOT NULL DEFAULT '');
CREATE INDEX diagnostics_level_id ON diagnostics(level,id);
CREATE INDEX diagnostics_component_id ON diagnostics(component,id);
CREATE INDEX diagnostics_created_at ON diagnostics(created_at);
`

func initializeDatabase(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(DatabaseSchema)))
	var version, tables, hasSettings int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if err := tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'`).Scan(&tables); err != nil {
		return err
	}
	if tables == 0 && version == 0 {
		if _, err := tx.Exec(DatabaseSchema); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES('schema-fingerprint',?)`, fingerprint); err != nil {
			return err
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version=%d`, DatabaseSchemaVersion)); err != nil {
			return err
		}
		return tx.Commit()
	}
	// No released migrations yet. Refuse unfamiliar/older development databases
	// and newer schemas rather than resetting them or attempting a downgrade.
	if version != DatabaseSchemaVersion {
		return fmt.Errorf("Baza koristi verziju %d; ova aplikacija podržava verziju %d. Podaci nisu obrisani. Koristi kompatibilnu verziju aplikacije ili migraciju baze.", version, DatabaseSchemaVersion)
	}
	if err := tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='settings'`).Scan(&hasSettings); err != nil {
		return err
	}
	if hasSettings == 0 {
		return fmt.Errorf("Nepoznata šema baze. Podaci nisu obrisani.")
	}
	var saved string
	if err := tx.QueryRow(`SELECT value FROM settings WHERE key='schema-fingerprint'`).Scan(&saved); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("Bazi nedostaje oznaka podržane šeme. Podaci nisu obrisani.")
		}
		return err
	}
	if saved != fingerprint {
		return fmt.Errorf("Šema baze nije kompatibilna s ovom verzijom aplikacije. Podaci nisu obrisani. Stara razvojna baza zahtijeva zaseban prijenos podataka ili novu bazu.")
	}
	return tx.Commit()
}

func (a *Store) PersistConfig(c model.Config) error { return a.PersistRevision(c, 0) }

func (a *Store) PersistRevision(c model.Config, restoredFrom int) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = writeConfig(tx, c, restoredFrom); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *Store) DesktopInitialized() bool {
	var value []byte
	return a.db.QueryRow(`SELECT value FROM settings WHERE key='desktop-initialized'`).Scan(&value) == nil
}

func (a *Store) MarkDesktopInitialized() error {
	_, err := a.db.Exec(`INSERT INTO settings(key,value) VALUES('desktop-initialized',?) ON CONFLICT(key) DO NOTHING`, []byte("1"))
	return err
}
