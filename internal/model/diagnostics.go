package model

import (
	"time"
)

type Diagnostic struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Level     string    `json:"level"`
	Component string    `json:"component"`
	Message   string    `json:"message"`
	JobID     string    `json:"job_id,omitempty"`
}

type DiagnosticQuery struct {
	Level, Component, Search string
	From, To                 time.Time
	Before                   int64
	Limit                    int
}

type DiagnosticPage struct {
	Items      []Diagnostic
	NextBefore int64
}
