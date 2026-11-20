package storage

import (
	"database/sql"
)

// Store owns SQL operations. The caller controls the database lifetime.
type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }
